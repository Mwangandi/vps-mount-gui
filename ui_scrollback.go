package main

import (
	"io"
	"sync"
	"unicode"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// scrollbackCapBytes is the maximum size of the ring buffer per session.
// 256 KB (~4000 lines of typical shell output) balances "you can go back
// pretty far" with "no unbounded memory growth per long-running session".
const scrollbackCapBytes = 256 * 1024

// scrollbackBuffer is a fixed-size, thread-safe circular byte buffer that
// stores stripped (ANSI-free) copies of the PTY output stream for later
// display in a plain text scrollback view.
//
// ASSUMPTION: stripping ANSI/OSC/CSI escape sequences is the right trade-
// off for the scrollback view. A raw ANSI dump is unreadable in a plain
// text widget (control codes render as literal ^[), and rendering styled
// text would essentially require reimplementing what fyne-io/terminal
// already does. The trade-off: colors and cursor-repositioned updates
// (progress bars, htop-style redraws) are lost in scrollback -- you see
// the linear stream of *characters* that reached the terminal, not the
// final displayed frame.
type scrollbackBuffer struct {
	mu    sync.Mutex
	data  []byte
	full  bool // whether the buffer has wrapped at least once
	write int  // write index into data
}

func newScrollbackBuffer() *scrollbackBuffer {
	return &scrollbackBuffer{data: make([]byte, scrollbackCapBytes)}
}

// Write appends stripped bytes to the ring, overwriting old bytes when
// full. Never blocks and never fails.
func (b *scrollbackBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	cap := len(b.data)
	for _, c := range p {
		b.data[b.write] = c
		b.write++
		if b.write == cap {
			b.write = 0
			b.full = true
		}
	}
	return len(p), nil
}

// Snapshot returns a copy of the buffer's contents in write order (oldest
// first, newest last). Safe to call while writes are ongoing.
func (b *scrollbackBuffer) Snapshot() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.full {
		return string(b.data[:b.write])
	}
	out := make([]byte, 0, len(b.data))
	out = append(out, b.data[b.write:]...)
	out = append(out, b.data[:b.write]...)
	return string(out)
}

// ── ANSI-stripping tee reader ────────────────────────────────────────

// scrollbackTee is an io.Reader that wraps another reader. Bytes pass
// through unchanged to whoever's reading (the fyne-io/terminal widget),
// but a stripped copy of the same bytes is appended to buf on the way
// past.
type scrollbackTee struct {
	src    io.Reader
	buf    *scrollbackBuffer
	stripS stripState // running ANSI parser state, shared across reads
}

func newScrollbackTee(src io.Reader, buf *scrollbackBuffer) *scrollbackTee {
	return &scrollbackTee{src: src, buf: buf}
}

func (t *scrollbackTee) Read(p []byte) (int, error) {
	n, err := t.src.Read(p)
	if n > 0 {
		stripped := t.stripS.process(p[:n])
		if len(stripped) > 0 {
			_, _ = t.buf.Write(stripped)
		}
	}
	return n, err
}

// stripState is a tiny stateful ANSI/OSC/CSI stripper. It's not a full
// terminal emulator; it just recognises the common escape families
// well enough to drop them without leaving stray bytes behind:
//   - CSI:  ESC [ ... final-byte (@..~)
//   - OSC:  ESC ] ... BEL or ESC \
//   - simple two-byte escapes: ESC <single char>
//
// It runs across multiple Read() calls so an escape that straddles a
// read boundary doesn't leak.
type stripState struct {
	inEsc  bool
	inCSI  bool
	inOSC  bool
	simple bool
}

func (s *stripState) process(in []byte) []byte {
	buf := make([]byte, 0, len(in))
	for _, c := range in {
		if s.inCSI {
			// CSI parameter bytes: 0x30..0x3F ("0-9:;<=>?"),
			// intermediate bytes 0x20..0x2F, final byte 0x40..0x7E.
			if c >= 0x40 && c <= 0x7E {
				s.inCSI = false
				s.inEsc = false
			}
			continue
		}
		if s.inOSC {
			// OSC ends on BEL (0x07) or ESC \ (ST, 0x1B 0x5C).
			if c == 0x07 {
				s.inOSC = false
				s.inEsc = false
			}
			// ESC inside OSC: keep waiting for the '\' that terminates ST.
			continue
		}
		if s.simple {
			s.simple = false
			s.inEsc = false
			continue
		}
		if s.inEsc {
			switch c {
			case '[':
				s.inCSI = true
			case ']':
				s.inOSC = true
			case '(', ')', '*', '+':
				// Charset designator: takes one more byte.
				s.simple = true
			default:
				// Anything else is a one-byte escape.
				s.inEsc = false
			}
			continue
		}
		if c == 0x1B {
			s.inEsc = true
			continue
		}
		if c == 0x07 {
			// Bare BEL: swallow, it just makes noise in a text view.
			continue
		}
		// Printable + common whitespace (CR/LF/TAB) survive; other
		// C0 control bytes are dropped.
		if c == '\n' || c == '\r' || c == '\t' || c >= 0x20 {
			if r := rune(c); r == unicode.ReplacementChar {
				continue
			}
			buf = append(buf, c)
		}
	}
	return buf
}

// showScrollbackWindow opens a read-only, scrollable dump of the current
// buffer contents in a new window. Cheap to open; the snapshot is a
// point-in-time copy, so live output keeps flowing in the terminal but
// the scrollback view stays put until the user reopens it.
func showScrollbackWindow(title string, buf *scrollbackBuffer) {
	w := fyne.CurrentApp().NewWindow("Scrollback: " + title)
	w.Resize(fyne.NewSize(800, 500))

	entry := widget.NewMultiLineEntry()
	entry.TextStyle = fyne.TextStyle{Monospace: true}
	entry.Wrapping = fyne.TextWrapWord
	entry.SetText(buf.Snapshot())
	entry.CursorRow = 1 << 30 // scroll to end
	entry.Disable()           // read-only, but selection/copy still work

	refresh := widget.NewButtonWithIcon("Refresh", theme.ViewRefreshIcon(), func() {
		entry.SetText(buf.Snapshot())
		entry.CursorRow = 1 << 30
	})
	close := widget.NewButton("Close", func() { w.Close() })
	toolbar := container.NewHBox(refresh, close)

	w.SetContent(container.NewBorder(toolbar, nil, nil, nil, entry))
	w.Show()
}
