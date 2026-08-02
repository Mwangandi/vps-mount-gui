package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	fyneterm "github.com/fyne-io/terminal"
)

// buildFileTree returns a lazily-loaded directory tree rooted at root.
// Directories are only read when expanded, so it stays fast even inside
// large mounted repos. onOpenFile fires when a leaf (non-directory) node
// is selected.
func buildFileTree(root string, onOpenFile func(path string)) *widget.Tree {
	tree := widget.NewTree(
		func(id widget.TreeNodeID) []widget.TreeNodeID {
			dir := root
			if id != "" {
				dir = id
			}
			entries, err := os.ReadDir(dir)
			if err != nil {
				return nil
			}
			sort.Slice(entries, func(i, j int) bool {
				if entries[i].IsDir() != entries[j].IsDir() {
					return entries[i].IsDir() // directories first
				}
				return entries[i].Name() < entries[j].Name()
			})
			children := make([]widget.TreeNodeID, 0, len(entries))
			for _, e := range entries {
				children = append(children, filepath.Join(dir, e.Name()))
			}
			return children
		},
		func(id widget.TreeNodeID) bool {
			info, err := os.Stat(id)
			return err == nil && info.IsDir()
		},
		func(branch bool) fyne.CanvasObject {
			icon := widget.NewIcon(theme.FileIcon())
			label := widget.NewLabel("")
			return container.NewHBox(icon, label)
		},
		func(id widget.TreeNodeID, branch bool, obj fyne.CanvasObject) {
			row := obj.(*fyne.Container)
			icon := row.Objects[0].(*widget.Icon)
			label := row.Objects[1].(*widget.Label)
			label.SetText(filepath.Base(id))
			if branch {
				icon.SetResource(theme.FolderIcon())
			} else {
				icon.SetResource(theme.FileIcon())
			}
		},
	)
	tree.Root = ""
	tree.OnSelected = func(id widget.TreeNodeID) {
		info, err := os.Stat(id)
		if err != nil || info.IsDir() {
			return
		}
		onOpenFile(id)
	}
	return tree
}

// openInVSCode launches the external `code` binary pointed at path.
func openInVSCode(path string, log func(string)) {
	cmd := exec.Command("code", path)
	if err := cmd.Start(); err != nil {
		log(fmt.Sprintf("✘ Could not launch VS Code for %s: %v (is 'code' on your PATH?)", path, err))
		return
	}
	log("Opened in VS Code: " + path)
}

// openEditorTab adds a new tab with a file tree on the left and an embedded
// terminal on the right, cd'd into rootPath. Double-clicking (selecting) a
// file in the tree runs `micro <file>` in that terminal.
func openEditorTab(tabs *container.DocTabs, rootPath string, label string) {
	term := fyneterm.New()

	openFile := func(path string) {
		_, _ = term.Write([]byte(fmt.Sprintf("micro %q\n", path)))
	}

	tree := buildFileTree(rootPath, openFile)
	treeScroll := container.NewScroll(tree)
	treeScroll.SetMinSize(fyne.NewSize(240, 0))

	split := container.NewHSplit(treeScroll, term)
	split.Offset = 0.22

	item := container.NewTabItem("Edit: "+label, split)
	tabs.Append(item)
	tabs.Select(item)

	go func() {
		_ = term.RunLocalShell()
	}()

	go func() {
		time.Sleep(400 * time.Millisecond)
		_, _ = term.Write([]byte(fmt.Sprintf("cd %q && clear\n", rootPath)))
	}()
}
