# VPS Mount Manager (Go + Fyne)

Desktop GUI wrapping `sshfs` with in-app SSH terminal, dual-pane SFTP
browser, detached RDP, embedded local terminal, code editor with file
tree, and Burp Suite dark theme.

## This round — three SSH terminal fixes

### Fix 1 (priority): Host key verification (TOFU)

Previously the app used `ssh.InsecureIgnoreHostKey()` for both the SSH
terminal and SFTP browser. Now:

- `golang.org/x/crypto/ssh/knownhosts` reads/writes a
  `~/.config/vps-mount-gui/known_hosts` file (**assumption**: I chose
  this app-namespaced location rather than the user's own
  `~/.ssh/known_hosts` so the app's TOFU decisions don't tangle with
  the user's shell habits and vice versa; the file uses standard
  OpenSSH known_hosts syntax if you ever want to merge).
- **First connect to a new host**: a confirmation dialog shows the
  SHA256 fingerprint and asks to accept. On accept, the key is
  appended to known_hosts.
- **Later connects**: verified silently. If the key matches, no dialog.
- **Key changed (MITM case)**: connection refused, warning dialog
  shows both fingerprints and points at the offending file so a
  legitimate key rotation can be fixed by hand.
- Applies to **both** the SSH terminal and the SFTP browser — they
  share `sftpConnect()` as their connection path.

**Assumption on granularity**: TOFU is per-host, not per-key-algorithm.
If a host serves an ed25519 key today and later an ecdsa key, that's
flagged as a mismatch (safe default).

### Fix 2: Passphrase-protected private keys

Previously, encrypted keys failed silently. Now:

- If ssh-agent is available (`SSH_AUTH_SOCK` set and dialable), it's
  used **first** — no passphrase prompt if the agent has the key.
- Only if agent auth fails (or no agent) does the app read the
  default key files. Unencrypted keys load silently; encrypted keys
  trigger a Fyne password-entry dialog attached to the same window
  driving the connection.
- **Passphrases are cached in memory for the app-launch** (not
  persisted to disk), matching how ssh-agent behaves once you've
  unlocked a key — you get asked once, and reconnecting to the same
  server later that session doesn't re-prompt.
- Up to 3 attempts per key file, then the key is skipped (auth
  continues with other methods rather than nuking the whole
  connection attempt).

### Fix 3: Scrollback buffer (workaround at app level)

`fyne-io/terminal` upstream still doesn't have native scrollback
(open issues #30, #32, #54, #78, #103, #120, #125, #126 as of writing,
none marked closed on this). Workaround:

- Every SSH session tees its stdout stream through an in-memory ring
  buffer. Terminal behavior is unchanged; the buffer just gets a copy.
- **Buffer size**: 256 KB per session (~4000 lines of typical shell
  output). Wraps oldest-first when full, so long sessions don't leak
  memory.
- **ANSI escape sequences are stripped** in the copy that hits the
  buffer. Raw ANSI in a plain-text widget is unreadable (control
  codes show as literal `^[`), and reimplementing styled rendering
  would essentially fork the terminal library. Trade-off: colors and
  progress-bar-style redraws are lost in scrollback, but the linear
  character stream is preserved.
- **How to view**: a **Scrollback** button in the top-right of the
  SSH terminal window, or press **Ctrl+Shift+F** anywhere in the
  window. Opens a read-only scrollable view of the current buffer in
  its own window. A Refresh button re-snapshots it if you want to
  catch up to live output.
- Shortcut deliberately chosen to avoid `fyne-io/terminal`'s own
  `Ctrl+Shift+C` / `Ctrl+Shift+V` copy-paste bindings.

**Worth revisiting**: `github.com/scottpeterman/tetherssh` uses a
different Go terminal library called `gopyte` which advertises
"configurable scrollback (default 1000 lines)" as a stated feature.
If our tee-and-strip workaround becomes annoying (e.g. losing color
in scrollback matters), `gopyte` is a real drop-in candidate for a
future round. For now, keeping the workaround since it's zero-
dependency and avoids a swap of the actively-used terminal widget.

### Bonus fix caught while in there

Removed a bogus line from the previous round that set
`session.Stderr = mergedWriter{terminalOut: stdin}` — that misrouted
stderr writes into the ssh session's *stdin*, where they'd never
appear. When a PTY is allocated, the remote side already folds stderr
onto the PTY's stdout stream, so no separate Stderr handling is needed
for interactive shells.

## Layout / sidebar (unchanged from previous rounds)

- **Sidebar** (left, collapsible, tooltip on hover): Mount, Unmount,
  Servers, Targets, Terminal, SSH, RDP, SFTP, Editor, Settings, View.
- **SSH** → in-app terminal window with the tee-based scrollback.
- **SFTP** → in-app dual-pane browser (also inherits Fixes 1 & 2).
- **RDP** → detached external client. (Untouched this round.)

## Requirements (Linux)

```bash
sudo apt install sshfs fuse3 micro
```

- `micro` — for the Editor tab's file-tree editor.
- `code` on `$PATH` — for the VS Code shortcuts and auto-open-on-mount.
- An RDP client for RDP (e.g. `freerdp2-x11`).
- Nothing extra for SSH/SFTP — both are built into the app.

## Config files

- `~/.config/vps-mount-gui/config.json` — servers, targets, base
  mount folder.
- `~/.config/vps-mount-gui/known_hosts` — new this round; OpenSSH
  known_hosts format.

## Rebuilding from source

```bash
sudo apt install golang-go libgl1-mesa-dev xorg-dev pkg-config
go build -o vps-mount-gui .
```

## Verified how

Compiled clean, `go vet` clean, `gofmt -l` clean. I don't have a
display in my build environment, so I can't click through the
dialogs myself — the flows above are verified by careful reads of
`knownhosts.KeyError` semantics, `ssh.PassphraseMissingError`,
`fyne.Do` (async main-loop scheduling), and `desktop.CustomShortcut`
registration on the canvas.

## Assumptions worth calling out again

1. **known_hosts location**: `~/.config/vps-mount-gui/known_hosts`,
   not `~/.ssh/known_hosts`. Change `knownHostsPath()` in
   `ui_hostkey.go` if you'd rather share.
2. **Passphrase cache is per-process, memory-only.** Restarting the
   app re-prompts. This matches the ssh-agent equivalent.
3. **Ring buffer 256 KB, ANSI-stripped, per-session.** Change
   `scrollbackCapBytes` in `ui_scrollback.go` if you want more/less.
4. **Scrollback view is a separate window**, not an inline overlay on
   the terminal — kept simpler and lets you keep it open beside the
   live terminal.
5. **TOFU prompt granularity** is per-host, not per-key-algorithm —
   an algorithm swap is treated as a mismatch (safe default).
