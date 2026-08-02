package main

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// Sidebar widths.
//
// sidebarExpandedWidth: fully-expanded width when the user opens the
// sidebar with the arrow toggle.
//
// sidebarMinimizedWidth: partial-width state used as the default on
// launch and when the user collapses the sidebar with the toggle. Sized
// wide enough for icon buttons + short truncated labels at ~40% of the
// expanded width.
const (
	sidebarExpandedWidth  = 230
	sidebarMinimizedWidth = 92 // ~40% of expanded, comfortable for icon + tooltip trigger
)

// ── tooltipButton ────────────────────────────────────────────────────
//
// A Fyne button that shows a text tooltip in a small PopUp when the
// mouse hovers over it. Uses Fyne's own widget.PopUp (non-modal) rather
// than a hand-rolled overlay -- PopUp manages its own lifecycle
// (add/remove from the canvas overlay stack), so this does not create
// the modal-blocking / stuck-content bugs a raw overlay would.
//
// Click behavior is unchanged from a normal Button: OnTapped fires and
// any tooltip currently shown is dismissed.
type tooltipButton struct {
	widget.Button
	tooltipText string
	canvas      fyne.Canvas
	popup       *widget.PopUp
}

func newTooltipButton(icon fyne.Resource, tooltip string, canvas fyne.Canvas, tapped func()) *tooltipButton {
	b := &tooltipButton{tooltipText: tooltip, canvas: canvas}
	b.ExtendBaseWidget(b)
	b.Icon = icon
	b.OnTapped = func() {
		b.hideTooltip()
		if tapped != nil {
			tapped()
		}
	}
	return b
}

func (b *tooltipButton) MouseIn(*desktop.MouseEvent) {
	if b.tooltipText == "" || b.canvas == nil {
		return
	}
	label := widget.NewLabel(b.tooltipText)
	b.popup = widget.NewPopUp(label, b.canvas)
	pos := fyne.CurrentApp().Driver().AbsolutePositionForObject(b)
	b.popup.ShowAtPosition(fyne.NewPos(pos.X+b.Size().Width+4, pos.Y))
}

func (b *tooltipButton) MouseMoved(*desktop.MouseEvent) {}

func (b *tooltipButton) MouseOut() {
	b.hideTooltip()
}

func (b *tooltipButton) hideTooltip() {
	if b.popup != nil {
		b.popup.Hide()
		b.popup = nil
	}
}

// ── sectionToFyneMenu / buildSidebar ─────────────────────────────────

func sectionToFyneMenu(section MenuSection) *fyne.Menu {
	items := make([]*fyne.MenuItem, 0, len(section.Actions))
	for _, a := range section.Actions {
		a := a
		item := fyne.NewMenuItem(a.Label, a.Action)
		item.Disabled = a.Disabled
		items = append(items, item)
	}
	return fyne.NewMenu(section.Title, items...)
}

// buildSidebar returns the sidebar canvas object for the current state.
// expanded=true gives the full labeled, collapsible-sections view at
// sidebarExpandedWidth. expanded=false gives an icon-only rail at
// sidebarMinimizedWidth. Icons in the rail show a tooltip with the
// section name on hover and open the section's actions as a popup menu
// on click.
func buildSidebar(w fyne.Window, sections []MenuSection, expanded bool, toggle func(), openSection string, onToggleSection func(string), onZoomIn func(), onZoomOut func()) fyne.CanvasObject {
	var toggleIcon fyne.Resource
	if expanded {
		toggleIcon = theme.NavigateBackIcon()
	} else {
		toggleIcon = theme.NavigateNextIcon()
	}
	toggleBtn := widget.NewButtonWithIcon("", toggleIcon, toggle)
	zoomOutBtn := widget.NewButtonWithIcon("", theme.ZoomOutIcon(), onZoomOut)
	zoomInBtn := widget.NewButtonWithIcon("", theme.ZoomInIcon(), onZoomIn)

	if !expanded {
		rail := container.NewVBox(
			toggleBtn,
			container.NewHBox(zoomOutBtn, zoomInBtn),
			widget.NewSeparator(),
		)
		for _, section := range sections {
			section := section
			var iconBtn *tooltipButton
			iconBtn = newTooltipButton(section.Icon, section.Title, w.Canvas(), func() {
				popup := widget.NewPopUpMenu(sectionToFyneMenu(section), w.Canvas())
				pos := fyne.CurrentApp().Driver().AbsolutePositionForObject(iconBtn)
				popup.ShowAtPosition(fyne.NewPos(pos.X+iconBtn.Size().Width+4, pos.Y))
			})
			rail.Add(iconBtn)
		}
		bg := canvas.NewRectangle(colorPanel)
		sized := container.NewStack(bg, container.NewVBox(rail))
		return container.New(&fixedWidthLayout{width: sidebarMinimizedWidth}, sized)
	}

	sectionsBox := container.NewVBox()
	for _, section := range sections {
		section := section
		isOpen := section.Title == openSection
		arrow := "▸ "
		if isOpen {
			arrow = "▾ "
		}
		header := widget.NewButton(arrow+section.Title, func() { onToggleSection(section.Title) })
		header.Alignment = widget.ButtonAlignLeading
		sectionsBox.Add(header)
		if isOpen {
			box := container.NewVBox()
			for _, a := range section.Actions {
				a := a
				btn := widget.NewButton(a.Label, a.Action)
				btn.Alignment = widget.ButtonAlignLeading
				if a.Disabled {
					btn.Disable()
				}
				box.Add(btn)
			}
			sectionsBox.Add(container.NewPadded(box))
		}
	}

	titleRow := container.NewHBox(
		toggleBtn,
		widget.NewLabelWithStyle("Menu", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		layout.NewSpacer(),
		zoomOutBtn,
		zoomInBtn,
	)
	topBar := container.NewVBox(titleRow, widget.NewSeparator())

	scroll := container.NewVScroll(sectionsBox)
	full := container.NewBorder(topBar, nil, nil, nil, scroll)

	bg := canvas.NewRectangle(colorPanel)
	sized := container.NewStack(bg, full)

	return container.New(&fixedWidthLayout{width: sidebarExpandedWidth}, sized)
}

// fixedWidthLayout pins a single child to a fixed width while letting it
// take the full available height — used to keep the sidebar from
// growing to fill the window.
type fixedWidthLayout struct {
	width float32
}

func (f *fixedWidthLayout) MinSize(objects []fyne.CanvasObject) fyne.Size {
	if len(objects) == 0 {
		return fyne.NewSize(f.width, 0)
	}
	return fyne.NewSize(f.width, objects[0].MinSize().Height)
}

func (f *fixedWidthLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	if len(objects) == 0 {
		return
	}
	objects[0].Resize(fyne.NewSize(f.width, size.Height))
	objects[0].Move(fyne.NewPos(0, 0))
}
