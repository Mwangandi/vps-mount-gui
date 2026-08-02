package main

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"github.com/pkg/sftp"
)

// paneBackend abstracts the local filesystem and the remote SFTP
// filesystem behind the same shape, so one tree-building function serves
// both panes instead of duplicating it.
type paneBackend struct {
	rootLabel    string
	listChildren func(dir string) ([]string, error) // dir == "" means root
	isDir        func(path string) bool
	displayName  func(path string) string
}

type treeCache struct {
	backend  paneBackend
	mu       sync.RWMutex
	children map[string][]string
	isDir    map[string]bool
	loading  map[string]bool
}

func newTreeCache(backend paneBackend) *treeCache {
	return &treeCache{
		backend:  backend,
		children: make(map[string][]string),
		isDir:    make(map[string]bool),
		loading:  make(map[string]bool),
	}
}

func (c *treeCache) loadChildren(id string, tree *widget.Tree, appendLog func(string)) {
	c.mu.Lock()
	if c.loading[id] {
		c.mu.Unlock()
		return
	}
	c.loading[id] = true
	c.mu.Unlock()

	children, err := c.backend.listChildren(id)
	if err != nil {
		appendLog(fmt.Sprintf("✘ Listing %s failed: %v", id, err))
	}

	c.mu.Lock()
	c.children[id] = children
	c.loading[id] = false
	for _, child := range children {
		isDir := c.backend.isDir(child)
		c.isDir[child] = isDir
	}
	c.mu.Unlock()

	fyne.CurrentApp().Driver().DoFromGoroutine(func() {
		tree.Refresh()
	}, false)
}

func (c *treeCache) listChildren(id string, tree *widget.Tree) []widget.TreeNodeID {
	c.mu.RLock()
	children, ok := c.children[id]
	c.mu.RUnlock()
	if !ok {
		go c.loadChildren(id, tree, func(string) {})
		return nil
	}
	ids := make([]widget.TreeNodeID, len(children))
	for i, child := range children {
		ids[i] = widget.TreeNodeID(child)
	}
	return ids
}

func (c *treeCache) isDirCached(id string) bool {
	if id == "" {
		return true
	}
	c.mu.RLock()
	isDir, ok := c.isDir[id]
	c.mu.RUnlock()
	if ok {
		return isDir
	}
	return true
}

// buildPane builds a lazily-loaded tree for a backend. onSelect fires with
// the selected node's id (a full path) whenever the user clicks a row.
func buildPane(backend paneBackend, onSelect func(id string)) *widget.Tree {
	cache := newTreeCache(backend)
	var tree *widget.Tree
	tree = widget.NewTree(
		func(id widget.TreeNodeID) []widget.TreeNodeID {
			return cache.listChildren(string(id), tree)
		},
		func(id widget.TreeNodeID) bool {
			return cache.isDirCached(string(id))
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
			label.SetText(backend.displayName(string(id)))
			if branch {
				icon.SetResource(theme.FolderIcon())
			} else {
				icon.SetResource(theme.FileIcon())
			}
		},
	)
	tree.Root = ""
	cache.loadChildren("", tree, func(string) {})
	tree.OnSelected = func(id widget.TreeNodeID) { onSelect(string(id)) }
	return tree
}

// promptText shows a single-field form dialog and calls onConfirm with the
// entered text if the user confirms with a non-empty value.
func promptText(w fyne.Window, title, label, initial string, onConfirm func(string)) {
	entry := widget.NewEntry()
	entry.SetText(initial)
	form := []*widget.FormItem{widget.NewFormItem(label, wideField(entry, 320))}
	dialog.ShowForm(title, "OK", "Cancel", form, func(ok bool) {
		if !ok || entry.Text == "" {
			return
		}
		onConfirm(entry.Text)
	}, w)
}

func showBookmarkJumpDialog(w fyne.Window, title string, options []string, onSelect func(string)) {
	if len(options) == 0 {
		return
	}
	selectWidget := widget.NewSelect(options, nil)
	selectWidget.SetSelected(options[0])
	form := []*widget.FormItem{widget.NewFormItem("Bookmark", wideField(selectWidget, 320))}
	dialog.ShowForm(title, "Jump", "Cancel", form, func(ok bool) {
		if !ok || selectWidget.Selected == "" {
			return
		}
		onSelect(selectWidget.Selected)
	}, w)
}

// progressReader wraps a reader and reports cumulative bytes read, used to
// drive the transfer progress bar for both uploads and downloads.
type progressReader struct {
	r          io.Reader
	read       int64
	total      int64
	onProgress func(read, total int64)
}

func (p *progressReader) Read(b []byte) (int, error) {
	n, err := p.r.Read(b)
	p.read += int64(n)
	if p.onProgress != nil {
		p.onProgress(p.read, p.total)
	}
	return n, err
}

func uploadFile(client *sftp.Client, localPath, remotePath string, progress func(float64)) error {
	lf, err := os.Open(localPath)
	if err != nil {
		return err
	}
	defer lf.Close()
	info, err := lf.Stat()
	if err != nil {
		return err
	}
	rf, err := client.Create(remotePath)
	if err != nil {
		return err
	}
	defer rf.Close()
	pr := &progressReader{r: lf, total: info.Size(), onProgress: func(read, total int64) {
		if total > 0 {
			progress(float64(read) / float64(total))
		}
	}}
	_, err = io.Copy(rf, pr)
	return err
}

func downloadFile(client *sftp.Client, remotePath, localPath string, size int64, progress func(float64)) error {
	rf, err := client.Open(remotePath)
	if err != nil {
		return err
	}
	defer rf.Close()
	if err := os.MkdirAll(filepath.Dir(localPath), 0o755); err != nil {
		return err
	}
	lf, err := os.Create(localPath)
	if err != nil {
		return err
	}
	defer lf.Close()
	pr := &progressReader{r: rf, total: size, onProgress: func(read, total int64) {
		if total > 0 {
			progress(float64(read) / float64(total))
		}
	}}
	_, err = io.Copy(lf, pr)
	return err
}

// uploadRecursive walks a local directory and recreates it on the remote
// side under destDir, uploading every file. Directories become
// client.MkdirAll calls; files are copied with progress.
//
// LIMITATION: no cancel button and no per-file list -- status text says
// which file is currently transferring, but a large tree gives no overall
// ETA. Symlinks are not followed (same behavior as the local editor's file
// tree) -- they're skipped rather than copied as links or dereferenced.
func uploadRecursive(client *sftp.Client, localRoot, remoteDestDir string, status func(string), progress func(float64)) error {
	base := filepath.Base(localRoot)
	remoteRoot := path.Join(remoteDestDir, base)
	return filepath.WalkDir(localRoot, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Type()&os.ModeSymlink != 0 {
			return nil // symlinks skipped, not followed or copied
		}
		rel, err := filepath.Rel(localRoot, p)
		if err != nil {
			return err
		}
		remotePath := remoteRoot
		if rel != "." {
			remotePath = path.Join(remoteRoot, filepath.ToSlash(rel))
		}
		if d.IsDir() {
			return client.MkdirAll(remotePath)
		}
		status("Uploading " + rel + "...")
		return uploadFile(client, p, remotePath, progress)
	})
}

// downloadRecursive mirrors uploadRecursive in the other direction, using
// the SFTP client's own Walk (which also does not follow symlinks).
func downloadRecursive(client *sftp.Client, remoteRoot, localDestDir string, status func(string), progress func(float64)) error {
	base := path.Base(remoteRoot)
	localRoot := filepath.Join(localDestDir, base)
	walker := client.Walk(remoteRoot)
	for walker.Step() {
		if err := walker.Err(); err != nil {
			return err
		}
		p := walker.Path()
		rel := strings.TrimPrefix(strings.TrimPrefix(p, remoteRoot), "/")
		localPath := localRoot
		if rel != "" {
			localPath = filepath.Join(localRoot, filepath.FromSlash(rel))
		}
		info := walker.Stat()
		if info.IsDir() {
			if err := os.MkdirAll(localPath, 0o755); err != nil {
				return err
			}
			continue
		}
		status("Downloading " + rel + "...")
		if err := downloadFile(client, p, localPath, info.Size(), progress); err != nil {
			return err
		}
	}
	return nil
}

// buildSFTPBrowserContent builds the dual-pane browser UI once a connection
// is already established.
//
// ASSUMPTIONS (flagged per the task): uploads/downloads silently overwrite
// an existing file at the destination (no conflict prompt). Remote rename
// tries the POSIX-rename extension first (overwrites the destination if
// the server supports that extension) and falls back to plain SFTP rename
// (which fails if the destination already exists) if the server doesn't
// support it. Hidden/dotfiles ARE shown on both sides -- there's no
// show/hide toggle. Symlinks are shown as plain files/folders based on
// their target's type but are not followed during recursive
// upload/download (see uploadRecursive/downloadRecursive above).
func buildSFTPBrowserContent(w fyne.Window, client *sftp.Client, s Server, cfg *Config, appendLog func(string)) fyne.CanvasObject {
	localHome, err := os.UserHomeDir()
	if err != nil || localHome == "" {
		localHome = "/"
	}
	remoteHome, err := client.Getwd()
	if err != nil || remoteHome == "" {
		remoteHome = "/"
	}

	statusLabel := widget.NewLabel("Ready.")
	progressBar := widget.NewProgressBar()
	progressBar.Hide()
	setStatus := func(text string) { statusLabel.SetText(text) }
	setProgress := func(f float64) {
		if f <= 0 {
			progressBar.Hide()
			return
		}
		progressBar.Show()
		progressBar.SetValue(f)
	}

	localSelected, remoteSelected := localHome, remoteHome
	localSelectedIsDir, remoteSelectedIsDir := true, true

	localPaneHolder := container.NewStack()
	remotePaneHolder := container.NewStack()
	var refreshLocal, refreshRemote func()

	localBackend := paneBackend{
		rootLabel: localHome,
		listChildren: func(dir string) ([]string, error) {
			d := dir
			if d == "" {
				d = localHome
			}
			entries, err := os.ReadDir(d)
			if err != nil {
				appendLog("✘ Local: " + err.Error())
				return nil, err
			}
			sort.Slice(entries, func(i, j int) bool {
				if entries[i].IsDir() != entries[j].IsDir() {
					return entries[i].IsDir()
				}
				return entries[i].Name() < entries[j].Name()
			})
			out := make([]string, 0, len(entries))
			for _, e := range entries {
				out = append(out, filepath.Join(d, e.Name()))
			}
			return out, nil
		},
		isDir: func(p string) bool {
			info, err := os.Lstat(p)
			return err == nil && info.IsDir()
		},
		displayName: func(p string) string {
			if p == "" || p == localHome {
				return localHome
			}
			return filepath.Base(p)
		},
	}

	remoteBackend := paneBackend{
		rootLabel: remoteHome,
		listChildren: func(dir string) ([]string, error) {
			d := dir
			if d == "" {
				d = remoteHome
			}
			entries, err := client.ReadDir(d)
			if err != nil {
				appendLog("✘ Remote: " + err.Error())
				return nil, err
			}
			sort.Slice(entries, func(i, j int) bool {
				if entries[i].IsDir() != entries[j].IsDir() {
					return entries[i].IsDir()
				}
				return entries[i].Name() < entries[j].Name()
			})
			out := make([]string, 0, len(entries))
			for _, e := range entries {
				out = append(out, path.Join(d, e.Name()))
			}
			return out, nil
		},
		isDir: func(p string) bool {
			info, err := client.Lstat(p)
			return err == nil && info.IsDir()
		},
		displayName: func(p string) string {
			if p == "" || p == remoteHome {
				return remoteHome
			}
			return path.Base(p)
		},
	}

	refreshLocal = func() {
		tree := buildPane(localBackend, func(id string) {
			localSelected = id
			localSelectedIsDir = localBackend.isDir(id)
		})
		localPaneHolder.Objects = []fyne.CanvasObject{tree}
		localPaneHolder.Refresh()
	}
	refreshRemote = func() {
		tree := buildPane(remoteBackend, func(id string) {
			remoteSelected = id
			remoteSelectedIsDir = remoteBackend.isDir(id)
		})
		remotePaneHolder.Objects = []fyne.CanvasObject{tree}
		remotePaneHolder.Refresh()
	}

	dirForLocalAction := func() string {
		if localSelected == "" {
			return localHome
		}
		if localSelectedIsDir {
			return localSelected
		}
		return filepath.Dir(localSelected)
	}
	dirForRemoteAction := func() string {
		if remoteSelected == "" {
			return remoteHome
		}
		if remoteSelectedIsDir {
			return remoteSelected
		}
		return path.Dir(remoteSelected)
	}

	// ── Local toolbar ──────────────────────────────────────────
	localNewFolder := widget.NewButtonWithIcon("", theme.FolderNewIcon(), func() {
		promptText(w, "New Local Folder", "Folder name", "", func(name string) {
			dir := dirForLocalAction()
			if err := os.Mkdir(filepath.Join(dir, name), 0o755); err != nil {
				appendLog("✘ Could not create local folder: " + err.Error())
				return
			}
			appendLog("✔ Created local folder " + filepath.Join(dir, name))
			refreshLocal()
		})
	})
	localRename := widget.NewButtonWithIcon("", theme.DocumentCreateIcon(), func() {
		if localSelected == "" || localSelected == localHome {
			appendLog("✘ Select a local file or folder to rename first.")
			return
		}
		promptText(w, "Rename", "New name", filepath.Base(localSelected), func(name string) {
			newPath := filepath.Join(filepath.Dir(localSelected), name)
			if err := os.Rename(localSelected, newPath); err != nil {
				appendLog("✘ Local rename failed: " + err.Error())
				return
			}
			appendLog("✔ Renamed to " + newPath)
			localSelected = newPath
			refreshLocal()
		})
	})
	localDelete := widget.NewButtonWithIcon("", theme.DeleteIcon(), func() {
		if localSelected == "" || localSelected == localHome {
			appendLog("✘ Select a local file or folder to delete first.")
			return
		}
		target := localSelected
		dialog.ShowConfirm("Delete", "Delete '"+filepath.Base(target)+"'? This cannot be undone.", func(ok bool) {
			if !ok {
				return
			}
			if err := os.RemoveAll(target); err != nil {
				appendLog("✘ Local delete failed: " + err.Error())
				return
			}
			appendLog("✔ Deleted " + target)
			localSelected, localSelectedIsDir = localHome, true
			refreshLocal()
		}, w)
	})
	localBookmark := widget.NewButtonWithIcon("", theme.ContentAddIcon(), func() {
		bookmarkPath := localSelected
		if bookmarkPath == "" {
			bookmarkPath = localHome
		}
		if localSelected != "" && !localSelectedIsDir {
			bookmarkPath = filepath.Dir(localSelected)
		}
		if cfg != nil {
			cfg.AddLocalBookmark(bookmarkPath)
			appendLog("✔ Bookmarked local path " + bookmarkPath)
		} else {
			appendLog("✘ No config available to save local bookmark")
		}
	})
	localJump := widget.NewButtonWithIcon("", theme.NavigateNextIcon(), func() {
		if cfg == nil || len(cfg.LocalBookmarks) == 0 {
			appendLog("✘ No local bookmarks available")
			return
		}
		showBookmarkJumpDialog(w, "Jump to Local Bookmark", cfg.LocalBookmarks, func(selected string) {
			if selected == "" {
				return
			}
			localSelected = selected
			localSelectedIsDir = true
			refreshLocal()
			appendLog("✔ Jumped to local bookmark " + selected)
		})
	})
	localRefreshBtn := widget.NewButtonWithIcon("", theme.ViewRefreshIcon(), func() { refreshLocal() })
	localToolbar := container.NewHBox(
		widget.NewLabelWithStyle("Local", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		layout.NewSpacer(),
		localBookmark, localJump, localNewFolder, localRename, localDelete, localRefreshBtn,
	)

	// ── Remote toolbar ─────────────────────────────────────────
	remoteNewFolder := widget.NewButtonWithIcon("", theme.FolderNewIcon(), func() {
		promptText(w, "New Remote Folder", "Folder name", "", func(name string) {
			dir := dirForRemoteAction()
			p := path.Join(dir, name)
			if err := client.Mkdir(p); err != nil {
				appendLog("✘ Could not create remote folder: " + err.Error())
				return
			}
			appendLog("✔ Created remote folder " + p)
			refreshRemote()
		})
	})
	remoteRename := widget.NewButtonWithIcon("", theme.DocumentCreateIcon(), func() {
		if remoteSelected == "" || remoteSelected == remoteHome {
			appendLog("✘ Select a remote file or folder to rename first.")
			return
		}
		promptText(w, "Rename", "New name", path.Base(remoteSelected), func(name string) {
			newPath := path.Join(path.Dir(remoteSelected), name)
			// Try the overwrite-capable POSIX rename extension first; fall
			// back to plain SFTP rename if the server doesn't support it
			// (plain rename fails if newPath already exists).
			err := client.PosixRename(remoteSelected, newPath)
			if err != nil {
				err = client.Rename(remoteSelected, newPath)
			}
			if err != nil {
				appendLog("✘ Remote rename failed: " + err.Error())
				return
			}
			appendLog("✔ Renamed to " + newPath)
			remoteSelected = newPath
			refreshRemote()
		})
	})
	remoteDelete := widget.NewButtonWithIcon("", theme.DeleteIcon(), func() {
		if remoteSelected == "" || remoteSelected == remoteHome {
			appendLog("✘ Select a remote file or folder to delete first.")
			return
		}
		target := remoteSelected
		dialog.ShowConfirm("Delete", "Delete '"+path.Base(target)+"'? This cannot be undone.", func(ok bool) {
			if !ok {
				return
			}
			if err := client.RemoveAll(target); err != nil {
				appendLog("✘ Remote delete failed: " + err.Error())
				return
			}
			appendLog("✔ Deleted " + target)
			remoteSelected, remoteSelectedIsDir = remoteHome, true
			refreshRemote()
		}, w)
	})
	remoteBookmark := widget.NewButtonWithIcon("", theme.ContentAddIcon(), func() {
		bookmarkPath := remoteSelected
		if bookmarkPath == "" {
			bookmarkPath = remoteHome
		}
		if remoteSelected != "" && !remoteSelectedIsDir {
			bookmarkPath = path.Dir(remoteSelected)
		}
		if cfg != nil {
			cfg.AddRemoteBookmark(s.Name, bookmarkPath)
			appendLog("✔ Bookmarked remote path " + bookmarkPath)
		} else {
			appendLog("✘ No config available to save remote bookmark")
		}
	})
	remoteJump := widget.NewButtonWithIcon("", theme.NavigateNextIcon(), func() {
		if cfg == nil || len(cfg.Servers) == 0 {
			appendLog("✘ No remote bookmarks available")
			return
		}
		server, ok := cfg.ServerByName(s.Name)
		if !ok {
			appendLog("✘ Server not found")
			return
		}
		if len(server.Bookmarks) == 0 {
			appendLog("✘ No remote bookmarks available for this server")
			return
		}
		showBookmarkJumpDialog(w, "Jump to Remote Bookmark", server.Bookmarks, func(selected string) {
			if selected == "" {
				return
			}
			remoteSelected = selected
			remoteSelectedIsDir = true
			refreshRemote()
			appendLog("✔ Jumped to remote bookmark " + selected)
		})
	})
	remoteRefreshBtn := widget.NewButtonWithIcon("", theme.ViewRefreshIcon(), func() { refreshRemote() })
	remoteToolbar := container.NewHBox(
		widget.NewLabelWithStyle("Remote", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		layout.NewSpacer(),
		remoteBookmark, remoteJump, remoteNewFolder, remoteRename, remoteDelete, remoteRefreshBtn,
	)

	// ── Transfer buttons ───────────────────────────────────────
	uploadBtn := widget.NewButtonWithIcon("Upload »", theme.UploadIcon(), func() {
		if localSelected == "" {
			appendLog("✘ Select something on the local side to upload.")
			return
		}
		src := localSelected
		destDir := dirForRemoteAction()
		go func() {
			info, err := os.Lstat(src)
			if err != nil {
				appendLog("✘ Upload failed: " + err.Error())
				return
			}
			setStatus("Uploading " + filepath.Base(src) + "...")
			var uerr error
			if info.IsDir() {
				uerr = uploadRecursive(client, src, destDir, setStatus, setProgress)
			} else {
				uerr = uploadFile(client, src, path.Join(destDir, filepath.Base(src)), setProgress)
			}
			setProgress(0)
			if uerr != nil {
				appendLog("✘ Upload failed: " + uerr.Error())
				setStatus("Upload failed.")
			} else {
				appendLog(fmt.Sprintf("✔ Uploaded '%s' to %s", filepath.Base(src), destDir))
				setStatus("Ready.")
				refreshRemote()
			}
		}()
	})
	uploadBtn.Importance = widget.HighImportance

	downloadBtn := widget.NewButtonWithIcon("« Download", theme.DownloadIcon(), func() {
		if remoteSelected == "" {
			appendLog("✘ Select something on the remote side to download.")
			return
		}
		src := remoteSelected
		destDir := dirForLocalAction()
		go func() {
			info, err := client.Lstat(src)
			if err != nil {
				appendLog("✘ Download failed: " + err.Error())
				return
			}
			setStatus("Downloading " + path.Base(src) + "...")
			var derr error
			if info.IsDir() {
				derr = downloadRecursive(client, src, destDir, setStatus, setProgress)
			} else {
				derr = downloadFile(client, src, filepath.Join(destDir, path.Base(src)), info.Size(), setProgress)
			}
			setProgress(0)
			if derr != nil {
				appendLog("✘ Download failed: " + derr.Error())
				setStatus("Download failed.")
			} else {
				appendLog(fmt.Sprintf("✔ Downloaded '%s' to %s", path.Base(src), destDir))
				setStatus("Ready.")
				refreshLocal()
			}
		}()
	})
	downloadBtn.Importance = widget.HighImportance

	transferCol := container.NewVBox(layout.NewSpacer(), uploadBtn, downloadBtn, layout.NewSpacer())

	refreshLocal()
	refreshRemote()

	localSide := container.NewBorder(localToolbar, nil, nil, nil, container.NewScroll(localPaneHolder))
	remoteSide := container.NewBorder(remoteToolbar, nil, nil, nil, container.NewScroll(remotePaneHolder))

	innerSplit := container.NewHSplit(transferCol, remoteSide)
	innerSplit.Offset = 0.12
	outerSplit := container.NewHSplit(localSide, innerSplit)
	outerSplit.Offset = 0.45

	connLabel := widget.NewLabelWithStyle(
		fmt.Sprintf("Connected to %s (%s@%s) — Drop files here to upload", s.Name, s.User, s.Host),
		fyne.TextAlignLeading, fyne.TextStyle{Bold: true},
	)
	bottomBar := container.NewBorder(nil, nil, statusLabel, nil, progressBar)

	w.SetOnDropped(func(pos fyne.Position, uris []fyne.URI) {
		if len(uris) == 0 {
			return
		}
		destDir := dirForRemoteAction()
		go func() {
			for _, uri := range uris {
				pathValue := uri.Path()
				if pathValue == "" {
					continue
				}
				info, err := os.Lstat(pathValue)
				if err != nil {
					appendLog("✘ Drop upload failed: " + err.Error())
					continue
				}
				setStatus("Uploading " + filepath.Base(pathValue) + "...")
				var uerr error
				if info.IsDir() {
					uerr = uploadRecursive(client, pathValue, destDir, setStatus, setProgress)
				} else {
					uerr = uploadFile(client, pathValue, path.Join(destDir, filepath.Base(pathValue)), setProgress)
				}
				setProgress(0)
				if uerr != nil {
					appendLog("✘ Drop upload failed: " + uerr.Error())
					setStatus("Upload failed.")
				} else {
					appendLog(fmt.Sprintf("✔ Dropped '%s' to %s", filepath.Base(pathValue), destDir))
					setStatus("Ready.")
					refreshRemote()
				}
			}
		}()
	})

	return container.NewBorder(connLabel, bottomBar, nil, nil, outerSplit)
}
