package main

import (
	"fmt"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// MenuAction is one clickable line inside a sidebar section (or, when the
// sidebar is minimized, one line inside that section's popup menu).
type MenuAction struct {
	Label    string
	Action   func()
	Disabled bool
}

// MenuSection is one collapsible group in the sidebar (Mount, Unmount,
// Servers, Targets, Terminal, Settings, View).
type MenuSection struct {
	Title   string
	Icon    fyne.Resource
	Actions []MenuAction
}

func showQuickConnectDialog(w fyne.Window, cfg *Config, server Server, appendLog func(string), onChange func(), openTerminal func(Server)) {
	if cfg == nil {
		return
	}
	btnStyle := func(label string, action func()) *widget.Button {
		btn := widget.NewButton(label, action)
		btn.Importance = widget.HighImportance
		return btn
	}
	content := container.NewVBox(
		widget.NewLabelWithStyle(fmt.Sprintf("Quick Connect: %s", server.Name), fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		widget.NewLabel(fmt.Sprintf("%s@%s", server.User, server.Host)),
		container.NewGridWithColumns(2,
			btnStyle("SSH Terminal", func() {
				cfg.MarkServerUsed(server.Name)
				onChange()
				openSSHTerminal(server, cfg, appendLog)
			}),
			btnStyle("SFTP Browser", func() {
				cfg.MarkServerUsed(server.Name)
				onChange()
				openSFTPBrowser(server, cfg, appendLog)
			}),
			btnStyle("RDP", func() {
				cfg.MarkServerUsed(server.Name)
				onChange()
				launchRDPWindow(server, appendLog)
			}),
			btnStyle("Local + Auto-SSH", func() {
				cfg.MarkServerUsed(server.Name)
				onChange()
				openTerminal(server)
			}),
		),
		widget.NewButton("Toggle Favorite", func() {
			cfg.ToggleFavorite(server.Name)
			onChange()
		}),
	)
	d := dialog.NewCustom("Quick Connect", "Close", content, w)
	d.Show()
}

// buildMenuSections constructs the sidebar contents fresh from current
// state. Call it again (and rebuild the sidebar) any time targets/servers/
// mount status change, so Mount/Unmount always reflect reality.
func buildMenuSections(w fyne.Window, cfg *Config, appendLog func(string), onChange func(), clearLog func(), openTerminal func(Server), openEditor func(rootPath, label string)) []MenuSection {
	// ── Mount: lists targets that are NOT currently mounted ──
	mountActions := []MenuAction{
		{Label: "Mount All", Action: func() {
			go func() {
				for _, t := range cfg.Targets {
					mountTarget(cfg, t, appendLog)
				}
				onChange()
			}()
		}},
	}
	anyUnmounted := false
	for _, t := range cfg.Targets {
		t := t
		if isMounted(targetMountPath(cfg, t)) {
			continue
		}
		anyUnmounted = true
		mountActions = append(mountActions, MenuAction{Label: t.Name, Action: func() {
			go func() {
				mountTarget(cfg, t, appendLog)
				onChange()
			}()
		}})
	}
	if len(cfg.Targets) == 0 {
		mountActions = append(mountActions, MenuAction{Label: "(no targets yet)", Disabled: true})
	} else if !anyUnmounted {
		mountActions = append(mountActions, MenuAction{Label: "(everything is mounted)", Disabled: true})
	}

	// ── Unmount: lists targets that ARE currently mounted ──
	unmountActions := []MenuAction{
		{Label: "Unmount All", Action: func() {
			go func() {
				for _, t := range cfg.Targets {
					unmountTarget(cfg, t, appendLog)
				}
				onChange()
			}()
		}},
	}
	anyMounted := false
	for _, t := range cfg.Targets {
		t := t
		if !isMounted(targetMountPath(cfg, t)) {
			continue
		}
		anyMounted = true
		unmountActions = append(unmountActions, MenuAction{Label: t.Name, Action: func() {
			go func() {
				unmountTarget(cfg, t, appendLog)
				onChange()
			}()
		}})
	}
	if !anyMounted {
		unmountActions = append(unmountActions, MenuAction{Label: "(nothing mounted)", Disabled: true})
	}

	// ── Favorites ──
	favoriteActions := make([]MenuAction, 0)
	for _, s := range cfg.Favorites() {
		s := s
		favoriteActions = append(favoriteActions, MenuAction{Label: "★ " + s.Name, Action: func() {
			showQuickConnectDialog(w, cfg, s, appendLog, onChange, openTerminal)
		}})
	}
	for _, s := range cfg.RecentServers(5) {
		s := s
		favoriteActions = append(favoriteActions, MenuAction{Label: "⏱ " + s.Name, Action: func() {
			showQuickConnectDialog(w, cfg, s, appendLog, onChange, openTerminal)
		}})
	}
	if len(favoriteActions) == 0 {
		favoriteActions = append(favoriteActions, MenuAction{Label: "(no favorites or recent servers)", Disabled: true})
	}

	// ── Servers ──
	serverActions := []MenuAction{
		{Label: "+ Add Server...", Action: func() { showAddServerDialog(w, cfg, onChange) }},
	}
	for i, s := range cfg.Servers {
		i, s := i, s
		label := "☆ Toggle Fav: " + s.Name
		if s.Favorite {
			label = "★ Toggle Fav: " + s.Name
		}
		serverActions = append(serverActions,
			MenuAction{Label: "Edit: " + s.Name, Action: func() { showEditServerDialog(w, cfg, i, onChange) }},
			MenuAction{Label: "Remove: " + s.Name, Action: func() { confirmRemoveServer(w, cfg, i, onChange) }},
			MenuAction{Label: label, Action: func() {
				cfg.ToggleFavorite(s.Name)
				onChange()
			}},
		)
	}

	// ── Targets ──
	targetActions := []MenuAction{
		{Label: "+ Add Target...", Action: func() { showAddTargetDialog(w, cfg, onChange) }},
	}
	for i, t := range cfg.Targets {
		i, t := i, t
		targetActions = append(targetActions,
			MenuAction{Label: "Edit: " + t.Name, Action: func() { showEditTargetDialog(w, cfg, i, onChange) }},
			MenuAction{Label: "Remove: " + t.Name, Action: func() { confirmRemoveTarget(w, cfg, i, onChange) }},
		)
	}

	// ── Terminal: embedded local shell + one auto-SSH tab per server ──
	terminalActions := []MenuAction{
		{Label: "Local Shell", Action: func() { openTerminal(Server{}) }},
	}
	for _, s := range cfg.Servers {
		s := s
		terminalActions = append(terminalActions, MenuAction{Label: "SSH: " + s.Name, Action: func() {
			cfg.MarkServerUsed(s.Name)
			onChange()
			openTerminal(s)
		}})
	}

	// ── SSH: opens a real in-app terminal window per server, driven
	//    by a native SSH session (see ui_ssh.go for library choice/
	//    limitations). No external terminal launched anymore. ──
	sshActions := make([]MenuAction, 0, len(cfg.Servers))
	for _, s := range cfg.Servers {
		s := s
		sshActions = append(sshActions, MenuAction{Label: s.Name, Action: func() {
			cfg.MarkServerUsed(s.Name)
			onChange()
			openSSHTerminal(s, cfg, appendLog)
		}})
	}
	if len(cfg.Servers) == 0 {
		sshActions = append(sshActions, MenuAction{Label: "(add a server first)", Disabled: true})
	}

	// ── RDP: opens a detached remote-desktop client per server ──
	rdpActions := make([]MenuAction, 0, len(cfg.Servers))
	for _, s := range cfg.Servers {
		s := s
		rdpActions = append(rdpActions, MenuAction{Label: s.Name, Action: func() {
			cfg.MarkServerUsed(s.Name)
			onChange()
			launchRDPWindow(s, appendLog)
		}})
	}
	if len(cfg.Servers) == 0 {
		rdpActions = append(rdpActions, MenuAction{Label: "(add a server first)", Disabled: true})
	}

	// ── SFTP: opens the in-app SFTP browser for the selected server ──
	sftpActions := make([]MenuAction, 0, len(cfg.Servers))
	for _, s := range cfg.Servers {
		s := s
		sftpActions = append(sftpActions, MenuAction{Label: s.Name, Action: func() {
			cfg.MarkServerUsed(s.Name)
			onChange()
			openSFTPBrowser(s, cfg, appendLog)
		}})
	}
	if len(cfg.Servers) == 0 {
		sftpActions = append(sftpActions, MenuAction{Label: "(add a server first)", Disabled: true})
	}

	// ── Editor: browse any currently-mounted target, or any folder ──
	editorActions := []MenuAction{
		{Label: "Browse Folder...", Action: func() { showBrowseFolderDialog(w, openEditor) }},
	}
	anyMountedForEditor := false
	for _, t := range cfg.Targets {
		t := t
		if !isMounted(targetMountPath(cfg, t)) {
			continue
		}
		anyMountedForEditor = true
		editorActions = append(editorActions, MenuAction{Label: "Browse: " + t.Name, Action: func() {
			openEditor(targetMountPath(cfg, t), t.Name)
		}})
		editorActions = append(editorActions, MenuAction{Label: "VS Code: " + t.Name, Action: func() {
			openInVSCode(targetMountPath(cfg, t), appendLog)
		}})
	}
	if !anyMountedForEditor {
		editorActions = append(editorActions, MenuAction{Label: "(mount a target to browse it here)", Disabled: true})
	}

	// ── Settings ──
	settingsActions := []MenuAction{
		{Label: "Base Mount Folder...", Action: func() { showBaseDirDialog(w, cfg, onChange) }},
	}

	// ── View ──
	viewActions := []MenuAction{
		{Label: "Refresh Status", Action: onChange},
		{Label: "Clear Log", Action: clearLog},
	}

	return []MenuSection{
		{Title: "Favorites", Icon: theme.ConfirmIcon(), Actions: favoriteActions},
		{Title: "Mount", Icon: theme.MoveUpIcon(), Actions: mountActions},
		{Title: "Unmount", Icon: theme.MoveDownIcon(), Actions: unmountActions},
		{Title: "Servers", Icon: theme.StorageIcon(), Actions: serverActions},
		{Title: "Targets", Icon: theme.FolderIcon(), Actions: targetActions},
		{Title: "Terminal", Icon: theme.ComputerIcon(), Actions: terminalActions},
		{Title: "SSH", Icon: theme.LoginIcon(), Actions: sshActions},
		{Title: "RDP", Icon: theme.ViewFullScreenIcon(), Actions: rdpActions},
		{Title: "SFTP", Icon: theme.UploadIcon(), Actions: sftpActions},
		{Title: "Editor", Icon: theme.FileTextIcon(), Actions: editorActions},
		{Title: "Settings", Icon: theme.SettingsIcon(), Actions: settingsActions},
		{Title: "View", Icon: theme.ViewRefreshIcon(), Actions: viewActions},
	}
}
