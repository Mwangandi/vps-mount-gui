package main

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sync"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/dialog"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

// Host-key policy for SSH and SFTP.
//
// ASSUMPTIONS:
//   - known_hosts file location: <config-dir>/vps-mount-gui/known_hosts
//     next to the existing config.json, so it's namespaced to this app
//     rather than mixing with the user's own ~/.ssh/known_hosts (which
//     could be surprising and would tangle the app's TOFU decisions
//     with the user's ssh CLI habits). The file uses standard OpenSSH
//     known_hosts syntax and is written via knownhosts.Line, so if
//     someone wanted to migrate entries either direction they could.
//   - TOFU prompt granularity: per-host, not per-key-algorithm — if a
//     host presents an ed25519 key today and later an ecdsa key, that
//     shows as a mismatch (MITM warning), which is the safe default.
//   - Concurrency: prompts are serialized behind a mutex so two
//     simultaneous connection attempts to two new hosts don't stack
//     confirmation dialogs on top of each other.
var hostKeyPromptMu sync.Mutex

func knownHostsPath() string {
	return filepath.Join(filepath.Dir(configPath()), "known_hosts")
}

// ensureKnownHostsFile makes sure the file exists so knownhosts.New()
// doesn't fail on first run.
func ensureKnownHostsFile() (string, error) {
	path := knownHostsPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND, 0o600)
	if err != nil {
		return "", err
	}
	_ = f.Close()
	return path, nil
}

// appendKnownHost appends a new host key to the known_hosts file using
// OpenSSH-standard syntax.
func appendKnownHost(hostAddr string, key ssh.PublicKey) error {
	path, err := ensureKnownHostsFile()
	if err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	line := knownhosts.Line([]string{knownhosts.Normalize(hostAddr)}, key)
	if _, err := f.WriteString(line + "\n"); err != nil {
		return err
	}
	return nil
}

// hostKeyCallback returns an ssh.HostKeyCallback that implements TOFU
// against the app's own known_hosts file. It must run without an active
// UI, so it uses a synchronous channel round-trip through the main
// window's dialog helpers. parentWindow is the Fyne window to attach
// dialogs to (may be nil for headless/background connections, in which
// case unknown hosts are refused rather than prompted).
func hostKeyCallback(parentWindow fyne.Window) ssh.HostKeyCallback {
	return func(hostname string, remote net.Addr, key ssh.PublicKey) error {
		path, err := ensureKnownHostsFile()
		if err != nil {
			return fmt.Errorf("could not initialise known_hosts: %w", err)
		}
		cb, err := knownhosts.New(path)
		if err != nil {
			return fmt.Errorf("could not load known_hosts: %w", err)
		}
		err = cb(hostname, remote, key)
		if err == nil {
			return nil // key matches -- silent, common path
		}

		var kerr *knownhosts.KeyError
		if !errors.As(err, &kerr) {
			return err
		}

		if len(kerr.Want) > 0 {
			// Known host, but the key changed. THIS IS THE MITM CASE.
			// Refuse the connection outright; show a warning if we have
			// a window to attach a dialog to.
			fp := ssh.FingerprintSHA256(key)
			knownFp := ssh.FingerprintSHA256(kerr.Want[0].Key)
			msg := fmt.Sprintf(
				"HOST KEY CHANGED for %s.\n\n"+
					"Received key fingerprint:\n  %s\n\n"+
					"Stored key fingerprint:\n  %s\n\n"+
					"This may indicate a man-in-the-middle attack. Connection refused.\n"+
					"If this is a legitimate key rotation, remove the offending line from:\n"+
					"  %s",
				hostname, fp, knownFp, path,
			)
			if parentWindow != nil {
				fyne.Do(func() {
					dialog.ShowError(errors.New(msg), parentWindow)
				})
			}
			return fmt.Errorf("host key verification failed for %s: key mismatch (possible MITM)", hostname)
		}

		// Unknown host -- prompt for TOFU accept.
		if parentWindow == nil {
			return fmt.Errorf("host key for %s is not in known_hosts and no UI is available to prompt", hostname)
		}

		hostKeyPromptMu.Lock()
		defer hostKeyPromptMu.Unlock()

		fp := ssh.FingerprintSHA256(key)
		message := fmt.Sprintf(
			"The authenticity of host %s can't be established.\n\n"+
				"%s key fingerprint is:\n  %s\n\n"+
				"Continue connecting? The key will be saved and used to verify future "+
				"connections; if it ever changes, you'll get a warning.",
			hostname, key.Type(), fp,
		)

		accepted := make(chan bool, 1)
		fyne.Do(func() {
			d := dialog.NewConfirm("Unknown Host", message, func(ok bool) {
				accepted <- ok
			}, parentWindow)
			d.SetConfirmText("Accept and Save")
			d.SetDismissText("Cancel")
			d.Show()
		})
		if !<-accepted {
			return fmt.Errorf("host key for %s rejected by user", hostname)
		}
		if err := appendKnownHost(hostname, key); err != nil {
			return fmt.Errorf("could not save host key: %w", err)
		}
		return nil
	}
}
