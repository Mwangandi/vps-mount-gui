package main

import (
	"fmt"
	"image/color"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	fyneterm "github.com/fyne-io/terminal"
)

// openTerminalTab adds a new tab embedding a real terminal emulator.
// If server.Host is set, it auto-types an `ssh` command once the local
// shell is ready so you land straight in a session on that box.
func openTerminalTab(tabs *container.DocTabs, server Server) {
	term := fyneterm.New()

	label := "Local Shell"
	var initialCmd string
	if server.Host != "" {
		label = "SSH: " + server.Name
		port := server.Port
		if port == "" {
			port = "22"
		}
		initialCmd = fmt.Sprintf("ssh -p %s %s@%s", port, server.User, server.Host)
	}

	item := container.NewTabItem(label, term)
	tabs.Append(item)
	tabs.Select(item)

	go func() {
		_ = term.RunLocalShell()
	}()

	if initialCmd != "" {
		go func() {
			time.Sleep(400 * time.Millisecond)
			_, _ = term.Write([]byte(initialCmd + "\n"))
		}()
	}
}

// openTerminal opens a separate native window running a local shell.
// If server.Host is set, it will auto-type an ssh command after the shell
// starts so the user lands on that host (same behaviour as the tabbed
// version but in a standalone window).
func openTerminal(server Server) {
	title := "Local Shell"
	var initialCmd string
	if server.Host != "" {
		title = fmt.Sprintf("SSH: %s", server.Name)
		port := server.Port
		if port == "" {
			port = "22"
		}
		initialCmd = fmt.Sprintf("ssh -p %s %s@%s", port, server.User, server.Host)
	}

	w := fyne.CurrentApp().NewWindow(title)
	w.Resize(fyne.NewSize(900, 560))

	bg := canvas.NewRectangle(color.Black)
	term := fyneterm.New()
	w.SetContent(container.NewMax(bg, term))
	w.Show()

	go func() {
		_ = term.RunLocalShell()
	}()

	if initialCmd != "" {
		go func() {
			time.Sleep(400 * time.Millisecond)
			_, _ = term.Write([]byte(initialCmd + "\n"))
		}()
	}
}
