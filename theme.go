package main

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/theme"
)

// burpTheme mimics Burp Suite's dark UI: near-black background,
// slightly lighter panel/button grey, dark input/log fields, and
// Burp's signature orange used for primary actions, focus, and
// hover highlights.
type burpTheme struct{}

var (
	colorBackground = color.NRGBA{R: 0x26, G: 0x26, B: 0x26, A: 0xff} // near-black app background
	colorPanel      = color.NRGBA{R: 0x32, G: 0x32, B: 0x32, A: 0xff} // menu bar / panel surface
	colorForeground = color.NRGBA{R: 0xdc, G: 0xdc, B: 0xdc, A: 0xff} // light grey text
	colorAccent     = color.NRGBA{R: 0xff, G: 0x66, B: 0x33, A: 0xff} // Burp orange
	colorButton     = color.NRGBA{R: 0x3c, G: 0x3c, B: 0x3c, A: 0xff} // button grey
	colorInputBg    = color.NRGBA{R: 0x1a, G: 0x1a, B: 0x1a, A: 0xff} // input/log/terminal background
	colorDisabled   = color.NRGBA{R: 0x6e, G: 0x6e, B: 0x6e, A: 0xff}
	colorSeparator  = color.NRGBA{R: 0x40, G: 0x40, B: 0x40, A: 0xff}
	colorHover      = color.NRGBA{R: 0xff, G: 0x66, B: 0x33, A: 0x35} // translucent orange
	colorSuccess    = color.NRGBA{R: 0x4c, G: 0xaf, B: 0x50, A: 0xff}
	colorError      = color.NRGBA{R: 0xe5, G: 0x39, B: 0x35, A: 0xff}
)

// uiZoom scales text/icon sizes for the Zoom In/Out controls. 1.0 = 100%.
// Start at 70% zoom so the app opens in a compact view by default.
var uiZoom float32 = 0.7

const (
	zoomMin  = float32(0.7)
	zoomMax  = float32(1.8)
	zoomStep = float32(0.1)
)

// adjustZoom nudges uiZoom by delta, clamped to [zoomMin, zoomMax].
// Returns true if the value actually changed.
func adjustZoom(delta float32) bool {
	next := uiZoom + delta
	if next < zoomMin {
		next = zoomMin
	}
	if next > zoomMax {
		next = zoomMax
	}
	if next == uiZoom {
		return false
	}
	uiZoom = next
	return true
}
func (t *burpTheme) Color(name fyne.ThemeColorName, variant fyne.ThemeVariant) color.Color {
	switch name {
	case theme.ColorNameBackground:
		return colorBackground
	case theme.ColorNameForeground:
		return colorForeground
	case theme.ColorNamePrimary:
		return colorAccent
	case theme.ColorNameButton:
		return colorButton
	case theme.ColorNameInputBackground:
		return colorInputBg
	case theme.ColorNameDisabled:
		return colorDisabled
	case theme.ColorNameDisabledButton:
		return colorButton
	case theme.ColorNameSeparator:
		return colorSeparator
	case theme.ColorNameHover:
		return colorHover
	case theme.ColorNameFocus:
		return colorAccent
	case theme.ColorNameSelection:
		return colorHover
	case theme.ColorNamePlaceHolder:
		return colorDisabled
	case theme.ColorNameScrollBar:
		return colorSeparator
	case theme.ColorNameShadow:
		return color.NRGBA{R: 0, G: 0, B: 0, A: 0x77}
	case theme.ColorNameSuccess:
		return colorSuccess
	case theme.ColorNameError:
		return colorError
	case theme.ColorNameMenuBackground:
		return colorPanel
	case theme.ColorNameOverlayBackground:
		return colorPanel
	case theme.ColorNameHeaderBackground:
		return colorPanel
	}
	return theme.DefaultTheme().Color(name, variant)
}

func (t *burpTheme) Font(style fyne.TextStyle) fyne.Resource {
	return theme.DefaultTheme().Font(style)
}

func (t *burpTheme) Icon(name fyne.ThemeIconName) fyne.Resource {
	return theme.DefaultTheme().Icon(name)
}

func (t *burpTheme) Size(name fyne.ThemeSizeName) float32 {
	switch name {
	case theme.SizeNamePadding:
		return 6
	case theme.SizeNameInputBorder:
		return 1
	case theme.SizeNameText, theme.SizeNameCaptionText, theme.SizeNameHeadingText,
		theme.SizeNameSubHeadingText, theme.SizeNameInlineIcon:
		return theme.DefaultTheme().Size(name) * uiZoom
	}
	return theme.DefaultTheme().Size(name)
}
