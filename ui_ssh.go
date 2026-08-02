package main

import (
	"fmt"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	fyneterm "github.com/fyne-io/terminal"
	"golang.org/x/crypto/ssh"
)

// openSSHTerminal opens a new native, in-app window with a real embedded
// terminal emulator, connects to the server over SSH (reusing sftpConnect
// so host-key TOFU + passphrase prompts + auth all match the SFTP
// browser), requests an interactive PTY + shell, and pipes the session
// through fyne-io/terminal.
//
// LIBRARY CHOICE: fyne-io/terminal.
//   - Already vendored (used for Local Shell and the Editor tab's
//     file-tree-plus-terminal split), so no new dependency.
//   - Its RunWithConnection(in, out) API is designed exactly for
//     driving the emulator from arbitrary stdin/stdout, not just a
//     local pty.
//   - Real xterm-family emulation with 256-color support, mouse
//     selection (DoubleTapped for word, Dragged for range), and an
//     AddListener hook for resize events.
//
// KNOWN LIMITATIONS (flagged):
//  1. Native scrollback: fyne-io/terminal still does not expose one
//     (upstream open issues #30, #32, #54, #78, #103, #120, #125, #126
//     as of this check). We work around it by teeing the PTY output
//     into an in-memory ring buffer -- click the Scrollback button in
//     the top-right of the SSH window (or press Ctrl+Shift+F) to open
//     a read-only view. See ui_scrollback.go for the assumptions
//     around ANSI stripping and buffer size.
//  2. Copy/paste: mouse selection + widget's own Ctrl+Shift+C /
//     Ctrl+Shift+V shortcuts. Ctrl+C is deliberately passed through as
//     SIGINT to the remote shell.
//  3. Escape sequences: what fyne-io/terminal handles, we handle.
//     Common TUI programs (nano, htop, vim in basic mode) work.
//  4. Session errors: connection/auth failures and mid-session drops
//     surface in-window AND in the main app's action log.
//  5. Resize propagation: Config listener -> session.WindowChange().
//     If it fires after session close, the error is harmless.
//
// FIX from previous round: a bogus `session.Stderr = ... stdin` line
// was removed. When a PTY is allocated, the remote side already writes
// stderr onto the PTY's stdout stream; a separate Stderr writer is
// wrong for PTY sessions and would misroute stderr bytes into the SSH
// session's stdin (which they'd never appear on).
func openSSHTerminal(s Server, appendLog func(string)) {
	title := fmt.Sprintf("SSH: %s (%s@%s)", s.Name, s.User, s.Host)
	w := fyne.CurrentApp().NewWindow(title)
	w.Resize(fyne.NewSize(900, 560))

	statusLabel := widget.NewLabel(fmt.Sprintf("Connecting to %s@%s...", s.User, s.Host))
	w.SetContent(container.NewCenter(statusLabel))
	w.Show()

	go func() {
		// sftpConnect() (in ui_sftp.go) implements the shared auth path:
		// TOFU host-key check, ssh-agent-first, passphrase prompts for
		// encrypted keys. We reuse the *ssh.Client and immediately close
		// the sftp.Client we don't need.
		sshClient, sftpClient, err := sftpConnect(s, w)
		if err != nil {
			msg := fmt.Sprintf("SSH connection to '%s' failed: %v", s.Name, err)
			appendLog("✘ " + msg)
			statusLabel.SetText(msg)
			return
		}
		_ = sftpClient.Close()

		session, err := sshClient.NewSession()
		if err != nil {
			sshClient.Close()
			msg := fmt.Sprintf("Could not open SSH session on '%s': %v", s.Name, err)
			appendLog("✘ " + msg)
			statusLabel.SetText(msg)
			return
		}

		termModes := ssh.TerminalModes{
			ssh.ECHO:          1,
			ssh.TTY_OP_ISPEED: 14400,
			ssh.TTY_OP_OSPEED: 14400,
		}
		if err := session.RequestPty("xterm-256color", 40, 120, termModes); err != nil {
			session.Close()
			sshClient.Close()
			msg := fmt.Sprintf("Could not request PTY on '%s': %v", s.Name, err)
			appendLog("✘ " + msg)
			statusLabel.SetText(msg)
			return
		}

		stdin, err := session.StdinPipe()
		if err != nil {
			session.Close()
			sshClient.Close()
			appendLog("✘ SSH stdin pipe: " + err.Error())
			statusLabel.SetText("SSH stdin pipe error: " + err.Error())
			return
		}
		stdout, err := session.StdoutPipe()
		if err != nil {
			session.Close()
			sshClient.Close()
			appendLog("✘ SSH stdout pipe: " + err.Error())
			statusLabel.SetText("SSH stdout pipe error: " + err.Error())
			return
		}

		if err := session.Shell(); err != nil {
			session.Close()
			sshClient.Close()
			appendLog("✘ SSH shell start: " + err.Error())
			statusLabel.SetText("SSH shell start failed: " + err.Error())
			return
		}

		// Scrollback: tee stdout through a ring buffer before it reaches
		// the terminal widget. Terminal behaviour is unchanged; the
		// buffer just gets a stripped copy of everything that flows past.
		scrollback := newScrollbackBuffer()
		teedStdout := newScrollbackTee(stdout, scrollback)

		term := fyneterm.New()

		// Resize propagation.
		cfgCh := make(chan fyneterm.Config, 4)
		term.AddListener(cfgCh)
		go func() {
			for cfg := range cfgCh {
				if cfg.Rows == 0 || cfg.Columns == 0 {
					continue
				}
				_ = session.WindowChange(int(cfg.Rows), int(cfg.Columns))
			}
		}()

		scrollbackTitle := fmt.Sprintf("%s (%s@%s)", s.Name, s.User, s.Host)
		scrollbackBtn := widget.NewButtonWithIcon("Scrollback", theme.HistoryIcon(), func() {
			showScrollbackWindow(scrollbackTitle, scrollback)
		})
		toolbar := container.NewHBox(layout.NewSpacer(), scrollbackBtn)

		// Ctrl+Shift+F keyboard shortcut for scrollback (avoids
		// clashing with fyne-io/terminal's Ctrl+Shift+C / Ctrl+Shift+V
		// copy-paste bindings). The window-level canvas focus handler
		// is what makes the shortcut work regardless of terminal focus.
		w.Canvas().AddShortcut(&desktop.CustomShortcut{
			KeyName:  fyne.KeyF,
			Modifier: fyne.KeyModifierControl | fyne.KeyModifierShift,
		}, func(_ fyne.Shortcut) {
			showScrollbackWindow(scrollbackTitle, scrollback)
		})

		w.SetOnClosed(func() {
			term.RemoveListener(cfgCh)
			close(cfgCh)
			_ = session.Close()
			_ = sshClient.Close()
		})

		w.SetContent(container.NewBorder(toolbar, nil, nil, nil, term))
		appendLog(fmt.Sprintf("Connected SSH session to '%s' (%s@%s).", s.Name, s.User, s.Host))

		go func() {
			// Pass the teed reader to the widget: bytes flow through
			// scrollbackTee.Read (which populates the ring buffer) then
			// on to the terminal renderer unchanged.
			err := term.RunWithConnection(stdin, teedStdout)
			if err != nil {
				appendLog(fmt.Sprintf("SSH session '%s' ended: %v", s.Name, err))
			} else {
				appendLog(fmt.Sprintf("SSH session '%s' ended.", s.Name))
			}
		}()
	}()
}
