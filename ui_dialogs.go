package main

import (
	"fmt"
	"path/filepath"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
)

// wideField pins a form field to a minimum width, since Fyne's default Entry
// and Select widgets are narrow regardless of the dialog's own size —
// without this, longer values (paths, hostnames) get clipped from view.
func wideField(field fyne.CanvasObject, width float32) fyne.CanvasObject {
	return container.New(&fixedWidthLayout{width: width}, field)
}

// showBrowseFolderDialog lets the user pick any folder on disk (not just a
// mounted target) to open in the Editor tab.
func showBrowseFolderDialog(w fyne.Window, openEditor func(rootPath, label string)) {
	d := dialog.NewFolderOpen(func(uri fyne.ListableURI, err error) {
		if err != nil || uri == nil {
			return
		}
		path := uri.Path()
		openEditor(path, filepath.Base(path))
	}, w)
	d.Show()
}

func showAddServerDialog(w fyne.Window, cfg *Config, onDone func()) {
	nameEntry := widget.NewEntry()
	nameEntry.SetPlaceHolder("e.g. secondary")
	userEntry := widget.NewEntry()
	userEntry.SetPlaceHolder("e.g. frappe")
	hostEntry := widget.NewEntry()
	hostEntry.SetPlaceHolder("e.g. 41.90.x.x")
	portEntry := widget.NewEntry()
	portEntry.SetText("22")
	rdpPortEntry := widget.NewEntry()
	rdpPortEntry.SetText("3389")
	favoriteCheck := widget.NewCheck("Favorite (show in Quick Connect)", nil)

	form := []*widget.FormItem{
		widget.NewFormItem("Server Name", wideField(nameEntry, 320)),
		widget.NewFormItem("SSH User", wideField(userEntry, 320)),
		widget.NewFormItem("Host / IP", wideField(hostEntry, 320)),
		widget.NewFormItem("SSH Port", wideField(portEntry, 120)),
		widget.NewFormItem("RDP Port", wideField(rdpPortEntry, 120)),
		widget.NewFormItem("Favorite", wideField(favoriteCheck, 320)),
	}

	dialog.ShowForm("Add Server", "Add", "Cancel", form, func(ok bool) {
		if !ok {
			return
		}
		if nameEntry.Text == "" || hostEntry.Text == "" || userEntry.Text == "" {
			dialog.ShowError(fmt.Errorf("name, user, and host are required"), w)
			return
		}
		for _, s := range cfg.Servers {
			if s.Name == nameEntry.Text {
				dialog.ShowError(fmt.Errorf("a server named '%s' already exists", nameEntry.Text), w)
				return
			}
		}
		port := portEntry.Text
		if port == "" {
			port = "22"
		}
		rdpPort := rdpPortEntry.Text
		if rdpPort == "" {
			rdpPort = "3389"
		}
		cfg.Servers = append(cfg.Servers, Server{
			Name:     nameEntry.Text,
			User:     userEntry.Text,
			Host:     hostEntry.Text,
			Port:     port,
			RDPPort:  rdpPort,
			Favorite: favoriteCheck.Checked,
		})
		cfg.Save()
		onDone()
	}, w)
}

// showEditServerDialog edits cfg.Servers[index] in place. If the name
// changes, every target referencing the old name is updated to the new one.
func showEditServerDialog(w fyne.Window, cfg *Config, index int, onDone func()) {
	if index < 0 || index >= len(cfg.Servers) {
		return
	}
	original := cfg.Servers[index]

	nameEntry := widget.NewEntry()
	nameEntry.SetText(original.Name)
	userEntry := widget.NewEntry()
	userEntry.SetText(original.User)
	hostEntry := widget.NewEntry()
	hostEntry.SetText(original.Host)
	portEntry := widget.NewEntry()
	portEntry.SetText(original.Port)
	rdpPortEntry := widget.NewEntry()
	rdpPortEntry.SetText(original.RDPPortOrDefault())
	favoriteCheck := widget.NewCheck("Favorite (show in Quick Connect)", nil)
	favoriteCheck.SetChecked(original.Favorite)

	form := []*widget.FormItem{
		widget.NewFormItem("Server Name", wideField(nameEntry, 320)),
		widget.NewFormItem("SSH User", wideField(userEntry, 320)),
		widget.NewFormItem("Host / IP", wideField(hostEntry, 320)),
		widget.NewFormItem("SSH Port", wideField(portEntry, 120)),
		widget.NewFormItem("RDP Port", wideField(rdpPortEntry, 120)),
		widget.NewFormItem("Favorite", wideField(favoriteCheck, 320)),
	}

	dialog.ShowForm(fmt.Sprintf("Edit Server: %s", original.Name), "Save", "Cancel", form, func(ok bool) {
		if !ok {
			return
		}
		if nameEntry.Text == "" || hostEntry.Text == "" || userEntry.Text == "" {
			dialog.ShowError(fmt.Errorf("name, user, and host are required"), w)
			return
		}
		for i, s := range cfg.Servers {
			if i != index && s.Name == nameEntry.Text {
				dialog.ShowError(fmt.Errorf("a server named '%s' already exists", nameEntry.Text), w)
				return
			}
		}
		port := portEntry.Text
		if port == "" {
			port = "22"
		}
		rdpPort := rdpPortEntry.Text
		if rdpPort == "" {
			rdpPort = "3389"
		}
		newName := nameEntry.Text
		if newName != original.Name {
			for i, t := range cfg.Targets {
				if t.Server == original.Name {
					cfg.Targets[i].Server = newName
				}
			}
		}
		cfg.Servers[index] = Server{
			Name:          newName,
			User:          userEntry.Text,
			Host:          hostEntry.Text,
			Port:          port,
			RDPPort:       rdpPort,
			Favorite:      favoriteCheck.Checked,
			LastConnected: original.LastConnected,
			Bookmarks:     original.Bookmarks,
		}
		cfg.Save()
		onDone()
	}, w)
}

func showAddTargetDialog(w fyne.Window, cfg *Config, onDone func()) {
	if len(cfg.Servers) == 0 {
		dialog.ShowInformation("No servers yet", "Add a server first, then add a target for it.", w)
		return
	}
	nameEntry := widget.NewEntry()
	nameEntry.SetPlaceHolder("e.g. new-portal")
	pathEntry := widget.NewEntry()
	pathEntry.SetPlaceHolder("/home/frappe/new-portal")
	serverSelect := widget.NewSelect(cfg.ServerNames(), nil)
	serverSelect.SetSelected(cfg.ServerNames()[0])

	form := []*widget.FormItem{
		widget.NewFormItem("Target Name", wideField(nameEntry, 320)),
		widget.NewFormItem("Server", wideField(serverSelect, 320)),
		widget.NewFormItem("Remote Path", wideField(pathEntry, 380)),
	}

	dialog.ShowForm("Add Mount Target", "Add", "Cancel", form, func(ok bool) {
		if !ok {
			return
		}
		if nameEntry.Text == "" || pathEntry.Text == "" {
			dialog.ShowError(fmt.Errorf("target name and remote path are required"), w)
			return
		}
		for _, t := range cfg.Targets {
			if t.Name == nameEntry.Text {
				dialog.ShowError(fmt.Errorf("a target named '%s' already exists", nameEntry.Text), w)
				return
			}
		}
		cfg.Targets = append(cfg.Targets, Target{
			Name:       nameEntry.Text,
			Server:     serverSelect.Selected,
			RemotePath: pathEntry.Text,
		})
		cfg.Save()
		onDone()
	}, w)
}

// showEditTargetDialog edits cfg.Targets[index] in place. If the target is
// currently mounted, it must be unmounted first to avoid an orphaned mount
// under a stale name/path.
func showEditTargetDialog(w fyne.Window, cfg *Config, index int, onDone func()) {
	if index < 0 || index >= len(cfg.Targets) {
		return
	}
	original := cfg.Targets[index]

	if isMounted(targetMountPath(cfg, original)) {
		dialog.ShowInformation("Currently mounted",
			fmt.Sprintf("Unmount '%s' before editing it.", original.Name), w)
		return
	}
	if len(cfg.Servers) == 0 {
		dialog.ShowInformation("No servers", "Add a server first.", w)
		return
	}

	nameEntry := widget.NewEntry()
	nameEntry.SetText(original.Name)
	pathEntry := widget.NewEntry()
	pathEntry.SetText(original.RemotePath)
	serverSelect := widget.NewSelect(cfg.ServerNames(), nil)
	if _, ok := cfg.ServerByName(original.Server); ok {
		serverSelect.SetSelected(original.Server)
	} else {
		serverSelect.SetSelected(cfg.ServerNames()[0])
	}

	form := []*widget.FormItem{
		widget.NewFormItem("Target Name", wideField(nameEntry, 320)),
		widget.NewFormItem("Server", wideField(serverSelect, 320)),
		widget.NewFormItem("Remote Path", wideField(pathEntry, 380)),
	}

	dialog.ShowForm(fmt.Sprintf("Edit Target: %s", original.Name), "Save", "Cancel", form, func(ok bool) {
		if !ok {
			return
		}
		if nameEntry.Text == "" || pathEntry.Text == "" {
			dialog.ShowError(fmt.Errorf("target name and remote path are required"), w)
			return
		}
		for i, t := range cfg.Targets {
			if i != index && t.Name == nameEntry.Text {
				dialog.ShowError(fmt.Errorf("a target named '%s' already exists", nameEntry.Text), w)
				return
			}
		}
		cfg.Targets[index] = Target{
			Name:       nameEntry.Text,
			Server:     serverSelect.Selected,
			RemotePath: pathEntry.Text,
		}
		cfg.Save()
		onDone()
	}, w)
}

func confirmRemoveServer(w fyne.Window, cfg *Config, index int, onDone func()) {
	if index < 0 || index >= len(cfg.Servers) {
		return
	}
	s := cfg.Servers[index]
	inUse := false
	for _, t := range cfg.Targets {
		if t.Server == s.Name {
			inUse = true
			break
		}
	}
	remove := func() {
		cfg.Servers = append(cfg.Servers[:index], cfg.Servers[index+1:]...)
		cfg.Save()
		onDone()
	}
	msg := fmt.Sprintf("Remove server '%s'?", s.Name)
	if inUse {
		msg = fmt.Sprintf("One or more targets still use '%s'. Remove it anyway?", s.Name)
	}
	dialog.ShowConfirm("Remove Server", msg, func(confirmed bool) {
		if confirmed {
			remove()
		}
	}, w)
}

func confirmRemoveTarget(w fyne.Window, cfg *Config, index int, onDone func()) {
	if index < 0 || index >= len(cfg.Targets) {
		return
	}
	t := cfg.Targets[index]
	if isMounted(targetMountPath(cfg, t)) {
		dialog.ShowInformation("Currently mounted",
			fmt.Sprintf("Unmount '%s' before removing it.", t.Name), w)
		return
	}
	dialog.ShowConfirm("Remove Target", fmt.Sprintf("Remove '%s' from the list?", t.Name), func(confirmed bool) {
		if !confirmed {
			return
		}
		cfg.Targets = append(cfg.Targets[:index], cfg.Targets[index+1:]...)
		cfg.Save()
		onDone()
	}, w)
}

func showBaseDirDialog(w fyne.Window, cfg *Config, onDone func()) {
	entry := widget.NewEntry()
	entry.SetText(cfg.BaseMountDir)

	form := []*widget.FormItem{
		widget.NewFormItem("Base mount folder", wideField(entry, 420)),
	}

	dialog.ShowForm("Base Mount Folder", "Save", "Cancel", form, func(ok bool) {
		if !ok || entry.Text == "" {
			return
		}
		cfg.BaseMountDir = entry.Text
		cfg.Save()
		onDone()
	}, w)
}
