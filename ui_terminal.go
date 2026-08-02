package main

import (
	"fmt"
	"time"

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
