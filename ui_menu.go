package main

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/theme"
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

	// ── Servers ──
	serverActions := []MenuAction{
		{Label: "+ Add Server...", Action: func() { showAddServerDialog(w, cfg, onChange) }},
	}
	for i, s := range cfg.Servers {
		i, s := i, s
		serverActions = append(serverActions,
			MenuAction{Label: "Edit: " + s.Name, Action: func() { showEditServerDialog(w, cfg, i, onChange) }},
			MenuAction{Label: "Remove: " + s.Name, Action: func() { confirmRemoveServer(w, cfg, i, onChange) }},
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
		terminalActions = append(terminalActions, MenuAction{Label: "SSH: " + s.Name, Action: func() { openTerminal(s) }})
	}

	// ── SSH: opens a real in-app terminal window per server, driven
	//    by a native SSH session (see ui_ssh.go for library choice/
	//    limitations). No external terminal launched anymore. ──
	sshActions := make([]MenuAction, 0, len(cfg.Servers))
	for _, s := range cfg.Servers {
		s := s
		sshActions = append(sshActions, MenuAction{Label: s.Name, Action: func() { openSSHTerminal(s, appendLog) }})
	}
	if len(cfg.Servers) == 0 {
		sshActions = append(sshActions, MenuAction{Label: "(add a server first)", Disabled: true})
	}

	// ── RDP: opens a detached remote-desktop client per server ──
	rdpActions := make([]MenuAction, 0, len(cfg.Servers))
	for _, s := range cfg.Servers {
		s := s
		rdpActions = append(rdpActions, MenuAction{Label: s.Name, Action: func() { launchRDPWindow(s, appendLog) }})
	}
	if len(cfg.Servers) == 0 {
		rdpActions = append(rdpActions, MenuAction{Label: "(add a server first)", Disabled: true})
	}

	// ── SFTP: opens a native, in-app dual-pane browser per server ──
	sftpActions := make([]MenuAction, 0, len(cfg.Servers))
	for _, s := range cfg.Servers {
		s := s
		sftpActions = append(sftpActions, MenuAction{Label: s.Name, Action: func() { openSFTPBrowser(s, appendLog) }})
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
