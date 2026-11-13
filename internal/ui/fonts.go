package ui

import (
	mui "github.com/egoist/mygo/ui"
	"github.com/reed/acp-gui/internal/settings"
)

// Per-area text scales. The settings store percentages so the values stay
// readable and independent of the built-in sizes; 100% keeps the layout the
// app had before the setting existed.
const (
	minFontScale  = 80
	maxFontScale  = 150
	fontScaleStep = 10
)

// defaultFontScales is the scale of a new app, also used when there is no
// settings file (tests, portable runs).
func defaultFontScales() settings.Fonts {
	return settings.DefaultFonts()
}

// normalizeFontScales makes a loaded object safe to draw: a missing or
// nonsensical field becomes 100%, and out-of-range values are clamped.
func normalizeFontScales(fonts settings.Fonts) settings.Fonts {
	return settings.Fonts{
		WorkspaceList: normalizeFontScale(fonts.WorkspaceList),
		Chat:          normalizeFontScale(fonts.Chat),
		Composer:      normalizeFontScale(fonts.Composer),
		Git:           normalizeFontScale(fonts.Git),
	}
}

func normalizeFontScale(value int) int {
	switch {
	case value <= 0:
		return settings.DefaultFontScale
	case value < minFontScale:
		return minFontScale
	case value > maxFontScale:
		return maxFontScale
	default:
		return value
	}
}

// scaleFont applies one area's percentage to a built-in size. Sizes stay
// proportional, so captions and labels keep their place in the hierarchy.
func scaleFont(size float32, percent int) float32 {
	if percent <= 0 {
		percent = settings.DefaultFontScale
	}
	return size * float32(percent) / float32(settings.DefaultFontScale)
}

func (a *App) workspaceFont(size float32) float32 {
	return scaleFont(size, a.Fonts.WorkspaceList)
}

func (a *App) chatFont(size float32) float32 {
	return scaleFont(size, a.Fonts.Chat)
}

func (a *App) composerFont(size float32) float32 {
	return scaleFont(size, a.Fonts.Composer)
}

// composerControl fits a button or chip to the composer's action row. The
// row grows with the input font setting so large text does not clip inside
// the fixed default height.
func (a *App) composerControl(e mui.Element) mui.Element {
	return e.Height(a.composerFont(composerControlHeight)).PaddingY(0)
}

func (a *App) gitFont(size float32) float32 {
	return scaleFont(size, a.Fonts.Git)
}

// Body sizes start from the window theme so MyGo's accessibility text scale
// keeps working; the area's percentage then applies on top.
func (a *App) chatBodyFont(c *mui.Context) float32 {
	return a.chatFont(c.Theme().FontSize)
}

func (a *App) composerBodyFont(c *mui.Context) float32 {
	return a.composerFont(c.Theme().FontSize)
}

func (a *App) gitBodyFont(c *mui.Context) float32 {
	return a.gitFont(c.Theme().FontSize)
}

// Captions and section labels exist in a fixed size inside widgets.go.
// These area variants apply the same scaling to their built-in size.
func (a *App) workspaceCaption(c *mui.Context, text string) mui.Element {
	return mutedText(c, text, a.workspaceFont(9)).SingleLine()
}

func (a *App) chatCaption(c *mui.Context, text string) mui.Element {
	return mutedText(c, text, a.chatFont(9)).SingleLine()
}

func (a *App) composerCaption(c *mui.Context, text string) mui.Element {
	return mutedText(c, text, a.composerFont(9)).SingleLine()
}

func (a *App) gitCaption(c *mui.Context, text string) mui.Element {
	return mutedText(c, text, a.gitFont(9)).SingleLine()
}

func (a *App) composerSectionLabel(c *mui.Context, text string) mui.Element {
	return mui.Text(c, text).FontSize(a.composerFont(9)).Bold().TextColor(c.Theme().TextMuted)
}

func (a *App) gitSectionLabel(c *mui.Context, text string) mui.Element {
	return mui.Text(c, text).FontSize(a.gitFont(9)).Bold().TextColor(c.Theme().TextMuted)
}
