package main

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
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

	if !expanded {
		rail := container.NewVBox(
			toggleBtn,
			widget.NewSeparator(),
		)
		for _, section := range sections {
			section := section
			btn := widget.NewButtonWithIcon(" "+section.Title, section.Icon, nil)
			btn.Alignment = widget.ButtonAlignLeading
			btn.Importance = widget.LowImportance
			btn.OnTapped = func() {
				popup := widget.NewPopUpMenu(sectionToFyneMenu(section), w.Canvas())
				pos := fyne.CurrentApp().Driver().AbsolutePositionForObject(btn)
				popup.ShowAtPosition(fyne.NewPos(pos.X+btn.Size().Width+4, pos.Y))
			}
			rail.Add(btn)
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
