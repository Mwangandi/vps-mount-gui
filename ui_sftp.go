package main

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
)

// passphraseCache holds decrypted signers by key filename for the lifetime
// of this process. This matches how ssh-agent behaves once you've unlocked
// a key -- you get asked once and it stays unlocked -- rather than
// prompting on every reconnect. The passphrase itself is discarded as soon
// as the signer is constructed; only the resulting ssh.Signer is retained.
var (
	passphraseCacheMu  sync.Mutex
	passphraseCache    = map[string]ssh.Signer{}
	passphrasePromptMu sync.Mutex // serialize passphrase dialogs
)

// promptPassphrase shows a Fyne password dialog for a keyfile and returns
// the entered passphrase (or an error if the user cancels). Serialized so
// two simultaneous connections don't stack passphrase dialogs.
func promptPassphrase(parentWindow fyne.Window, keyfile string) (string, error) {
	if parentWindow == nil {
		return "", fmt.Errorf("no UI available to prompt for passphrase for %s", keyfile)
	}
	passphrasePromptMu.Lock()
	defer passphrasePromptMu.Unlock()

	result := make(chan string, 1)
	cancelled := make(chan struct{}, 1)

	fyne.Do(func() {
		entry := widget.NewPasswordEntry()
		entry.SetPlaceHolder("passphrase")
		form := []*widget.FormItem{
			widget.NewFormItem("Key file", widget.NewLabel(keyfile)),
			widget.NewFormItem("Passphrase", wideField(entry, 320)),
		}
		d := dialog.NewForm("Passphrase Required", "Unlock", "Cancel", form, func(ok bool) {
			if !ok {
				cancelled <- struct{}{}
				return
			}
			result <- entry.Text
		}, parentWindow)
		d.Show()
	})

	select {
	case p := <-result:
		return p, nil
	case <-cancelled:
		return "", fmt.Errorf("passphrase entry cancelled for %s", keyfile)
	}
}

// loadPrivateKey reads a key file and returns its signer. If the key is
// passphrase-protected, it consults the in-process cache first and only
// then prompts. A successfully-decrypted signer is cached so subsequent
// connections don't re-prompt.
func loadPrivateKey(parentWindow fyne.Window, path string) (ssh.Signer, error) {
	passphraseCacheMu.Lock()
	if s, ok := passphraseCache[path]; ok {
		passphraseCacheMu.Unlock()
		return s, nil
	}
	passphraseCacheMu.Unlock()

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	signer, err := ssh.ParsePrivateKey(data)
	if err == nil {
		passphraseCacheMu.Lock()
		passphraseCache[path] = signer
		passphraseCacheMu.Unlock()
		return signer, nil
	}

	var pmErr *ssh.PassphraseMissingError
	if !errors.As(err, &pmErr) {
		// Not a passphrase issue -- corrupt/unsupported key.
		return nil, err
	}

	// Retry loop: allow one wrong-passphrase attempt to reprompt.
	for attempt := 0; attempt < 3; attempt++ {
		pass, perr := promptPassphrase(parentWindow, path)
		if perr != nil {
			return nil, perr
		}
		signer, err = ssh.ParsePrivateKeyWithPassphrase(data, []byte(pass))
		if err == nil {
			passphraseCacheMu.Lock()
			passphraseCache[path] = signer
			passphraseCacheMu.Unlock()
			return signer, nil
		}
	}
	return nil, fmt.Errorf("could not decrypt %s: %w", path, err)
}

// sftpAuthMethods builds SSH auth methods:
//   - If an ssh-agent is running (SSH_AUTH_SOCK is set and dialable), it
//     goes FIRST so the user isn't prompted for a passphrase that the
//     agent already holds.
//   - Then any of the standard default key files that exist. Unencrypted
//     keys load silently; passphrase-protected keys trigger a Fyne
//     password dialog attached to parentWindow (once per process; see
//     passphraseCache). If parentWindow is nil, passphrase-protected keys
//     are skipped, matching the pre-fix behaviour for headless callers.
func sftpAuthMethods(parentWindow fyne.Window) []ssh.AuthMethod {
	var methods []ssh.AuthMethod

	if sock := os.Getenv("SSH_AUTH_SOCK"); sock != "" {
		if conn, err := net.Dial("unix", sock); err == nil {
			ag := agent.NewClient(conn)
			methods = append(methods, ssh.PublicKeysCallback(ag.Signers))
		}
	}

	home, err := os.UserHomeDir()
	if err == nil {
		// Use a PublicKeysCallback rather than eagerly loading every key
		// up front, so a passphrase dialog only appears when the auth is
		// actually attempted (i.e. after agent auth has failed) rather
		// than at connection start.
		methods = append(methods, ssh.PublicKeysCallback(func() ([]ssh.Signer, error) {
			var signers []ssh.Signer
			for _, name := range []string{"id_ed25519", "id_rsa", "id_ecdsa", "id_dsa"} {
				p := filepath.Join(home, ".ssh", name)
				if _, statErr := os.Stat(p); statErr != nil {
					continue
				}
				s, lerr := loadPrivateKey(parentWindow, p)
				if lerr != nil {
					// A cancelled passphrase prompt shouldn't nuke every
					// other key -- keep going.
					continue
				}
				signers = append(signers, s)
			}
			return signers, nil
		}))
	}

	return methods
}

// sftpConnect dials the server over SSH and opens an SFTP session on top.
//
// parentWindow is where host-key TOFU/MITM dialogs are attached. Pass the
// browser/terminal window that's driving the connection.
func sftpConnect(s Server, parentWindow fyne.Window) (*ssh.Client, *sftp.Client, error) {
	port := s.Port
	if port == "" {
		port = "22"
	}

	methods := sftpAuthMethods(parentWindow)
	if len(methods) == 0 {
		return nil, nil, fmt.Errorf("no SSH auth available: no ssh-agent running (SSH_AUTH_SOCK unset) " +
			"and no readable, unencrypted key in ~/.ssh (id_ed25519/id_rsa/id_ecdsa/id_dsa)")
	}

	config := &ssh.ClientConfig{
		User:            s.User,
		Auth:            methods,
		HostKeyCallback: hostKeyCallback(parentWindow),
		Timeout:         10 * time.Second,
	}

	addr := net.JoinHostPort(s.Host, port)
	sshClient, err := ssh.Dial("tcp", addr, config)
	if err != nil {
		return nil, nil, fmt.Errorf("SSH connection to %s failed: %w", addr, err)
	}

	sftpClient, err := sftp.NewClient(sshClient)
	if err != nil {
		sshClient.Close()
		return nil, nil, fmt.Errorf("SFTP session failed: %w", err)
	}

	return sshClient, sftpClient, nil
}

// openSFTPBrowser opens a new native window with a dual-pane file browser
// for the given server. Connecting happens in the background so the window
// appears immediately with a "Connecting..." indicator; connection or auth
// errors are shown in that window AND logged, rather than failing silently.
func openSFTPBrowser(s Server, appendLog func(string)) {
	w := fyne.CurrentApp().NewWindow(fmt.Sprintf("SFTP: %s (%s@%s)", s.Name, s.User, s.Host))
	w.Resize(fyne.NewSize(1040, 660))

	statusLabel := widget.NewLabel(fmt.Sprintf("Connecting to %s@%s...", s.User, s.Host))
	w.SetContent(container.NewCenter(statusLabel))
	w.Show()

	go func() {
		sshClient, sftpClient, err := sftpConnect(s, w)
		if err != nil {
			msg := fmt.Sprintf("SFTP connection to '%s' failed: %v", s.Name, err)
			appendLog("✘ " + msg)
			statusLabel.SetText(msg)
			return
		}
		appendLog(fmt.Sprintf("Connected SFTP session to '%s' (%s@%s).", s.Name, s.User, s.Host))

		w.SetOnClosed(func() {
			sftpClient.Close()
			sshClient.Close()
		})

		content := buildSFTPBrowserContent(w, sftpClient, s, appendLog)
		w.SetContent(content)
	}()
}
