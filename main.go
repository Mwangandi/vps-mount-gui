package main

import (
	"fmt"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

func main() {
	cfg := LoadConfig()

	a := app.NewWithID("com.pantech.vps-mount-gui")
	appTheme := &burpTheme{}
	a.Settings().SetTheme(appTheme)

	w := a.NewWindow("VPS Mount Manager")
	w.Resize(fyne.NewSize(780, 520))

	var refreshStatusBar func()

	// ── Zoom (scales text/icon sizes app-wide via the theme) ──
	applyZoom := func(delta float32) {
		if adjustZoom(delta) {
			a.Settings().SetTheme(appTheme) // re-applying forces a full re-layout
			refreshStatusBar()
		}
	}
	onZoomIn := func() { applyZoom(zoomStep) }
	onZoomOut := func() { applyZoom(-zoomStep) }

	// ── Log pane ─────────────────────────────────────────────
	logBox := widget.NewMultiLineEntry()
	logBox.Wrapping = fyne.TextWrapWord
	// Keep the entry disabled to avoid edits; provide a Copy button instead.
	logBox.Disable()
	latestActivity := ""

	appendLog := func(msg string) {
		ts := time.Now().Format("15:04:05")
		logBox.SetText(logBox.Text + fmt.Sprintf("[%s] %s\n", ts, msg))
		logBox.CursorRow = len(logBox.Text)
		latestActivity = msg
		if refreshStatusBar != nil {
			refreshStatusBar()
		}
	}
	clearLog := func() {
		logBox.SetText("")
	}

	// ── Status bar ───────────────────────────────────────────
	statusBar := widget.NewLabel("")
	zoomPercentLabel := widget.NewLabel("")
	zoomOutBtn := widget.NewButtonWithIcon("", theme.ZoomOutIcon(), onZoomOut)
	zoomInBtn := widget.NewButtonWithIcon("", theme.ZoomInIcon(), onZoomIn)
	copyLogBtn := widget.NewButtonWithIcon("Copy Log", theme.ContentCopyIcon(), func() {
		fyne.CurrentApp().Clipboard().SetContent(logBox.Text)
		appendLog("✔ Copied log to clipboard.")
	})
	statusBarContent := container.NewHBox(statusBar, layout.NewSpacer(), zoomPercentLabel, zoomOutBtn, zoomInBtn, copyLogBtn)
	refreshStatusBar = func() {
		mounted := 0
		for _, t := range cfg.Targets {
			if isMounted(targetMountPath(&cfg, t)) {
				mounted++
			}
		}
		activity := latestActivity
		if activity == "" {
			activity = fmt.Sprintf("●  %d / %d mounted    |    base folder: %s", mounted, len(cfg.Targets), cfg.BaseMountDir)
		} else {
			activity = fmt.Sprintf("●  %d / %d mounted    |    %s", mounted, len(cfg.Targets), activity)
		}
		statusBar.SetText(activity)
		zoomPercentLabel.SetText(fmt.Sprintf("Zoom: %.0f%%", uiZoom*100))
	}

	// ── Target status list: shows ONLY currently-mounted targets,
	//    each with a quick "Open in VS Code" button. mountedIdx maps
	//    list row -> index into cfg.Targets, recomputed on every
	//    refresh so it always reflects live mount state. ──
	var mountedIdx []int
	recomputeMountedIdx := func() {
		mountedIdx = mountedIdx[:0]
		for i, t := range cfg.Targets {
			if isMounted(targetMountPath(&cfg, t)) {
				mountedIdx = append(mountedIdx, i)
			}
		}
	}

	var targetList *widget.List
	targetList = widget.NewList(
		func() int { return len(mountedIdx) },
		func() fyne.CanvasObject {
			nameLabel := widget.NewLabelWithStyle("", fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
			infoLabel := widget.NewLabel("")
			statusLabel := widget.NewLabel("")
			vscodeBtn := widget.NewButtonWithIcon("VS Code", theme.ComputerIcon(), nil)
			return container.NewHBox(nameLabel, infoLabel, statusLabel, vscodeBtn)
		},
		func(id widget.ListItemID, obj fyne.CanvasObject) {
			if id < 0 || id >= len(mountedIdx) {
				return
			}
			row := obj.(*fyne.Container)
			nameLabel := row.Objects[0].(*widget.Label)
			infoLabel := row.Objects[1].(*widget.Label)
			statusLabel := row.Objects[2].(*widget.Label)
			vscodeBtn := row.Objects[3].(*widget.Button)

			t := cfg.Targets[mountedIdx[id]]
			server, _ := cfg.ServerByName(t.Server)
			mountPath := targetMountPath(&cfg, t)

			nameLabel.SetText(t.Name)
			infoLabel.SetText(fmt.Sprintf("%s@%s:%s", server.User, server.Host, t.RemotePath))
			statusLabel.SetText("●  mounted at " + mountPath)
			vscodeBtn.OnTapped = func() {
				openInVSCode(mountPath, appendLog)
			}
		},
	)

	emptyLabel := widget.NewLabel("Nothing mounted yet. Use the Mount section in the sidebar.")
	listArea := container.NewStack(targetList)

	logScroll := container.NewScroll(logBox)
	split := container.NewVSplit(listArea, logScroll)
	split.Offset = 0.5

	// ── Tabs: Status first, terminals/editors appended dynamically.
	//    DocTabs scrolls/overflows extra tabs instead of wrapping the
	//    header to a second row, so opening more tabs never pushes the
	//    content area further down. ──
	tabs := container.NewDocTabs(
		container.NewTabItem("Status", container.NewBorder(nil, statusBarContent, nil, nil, split)),
	)

	var refreshAll func()
	openTerminalAction := func(server Server) {
		if server.Host != "" {
			cfg.MarkServerUsed(server.Name)
			refreshAll()
		}
		openTerminal(server)
	}

	// ── Sidebar (expandable / minimizable, replaces top menu) ──
	// Starts minimized (icon rail) by default.
	sidebarExpanded := false
	openSection := ""
	sidebarHolder := container.NewStack()

	var rebuildSidebar func()

	rebuildSidebar = func() {
		sections := buildMenuSections(w, &cfg, appendLog, refreshAll, clearLog, openTerminalAction)
		sidebarHolder.Objects = []fyne.CanvasObject{buildSidebar(w, sections, sidebarExpanded, func() {
			sidebarExpanded = !sidebarExpanded
			rebuildSidebar()
		}, openSection, func(title string) {
			if openSection == title {
				openSection = ""
			} else {
				openSection = title
			}
			rebuildSidebar()
		}, onZoomIn, onZoomOut)}
		sidebarHolder.Refresh()
	}

	refreshAll = func() {
		recomputeMountedIdx()
		targetList.Refresh()
		refreshStatusBar()
		if len(mountedIdx) == 0 {
			listArea.Objects = []fyne.CanvasObject{emptyLabel}
		} else {
			listArea.Objects = []fyne.CanvasObject{targetList}
		}
		listArea.Refresh()
		rebuildSidebar()
	}

	rebuildSidebar()
	content := container.NewBorder(nil, nil, sidebarHolder, nil, tabs)
	w.SetContent(content)

	refreshAll()
	appendLog("VPS Mount Manager ready. Config: " + configPath())

	// Periodic poller: detect external mount/unmount changes (e.g. user
	// unmounted via file manager) and refresh UI. Runs every 5 seconds.
	go func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			a.Driver().DoFromGoroutine(func() { refreshAll() }, false)
		}
	}()

	w.ShowAndRun()
}
