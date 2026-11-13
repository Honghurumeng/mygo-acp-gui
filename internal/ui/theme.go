package ui

import (
	"strings"

	"github.com/egoist/mygo"
	mui "github.com/egoist/mygo/ui"
)

// Appearance is the light/dark choice. System follows the desktop; light and
// dark force the native window chrome and the standard palette.
const (
	AppearanceSystem = "system"
	AppearanceLight  = "light"
	AppearanceDark   = "dark"
)

// Theme families. Every family has a light and a dark palette; Appearance
// picks which one a window uses.
const (
	ThemeDefault    = "default"
	ThemeNord       = "nord"
	ThemeSolarized  = "solarized"
	ThemeCatppuccin = "catppuccin"
	ThemeClaude     = "claude"
)

// themePalette overrides MyGo's standard theme colors. Metrics and
// accessibility settings stay those of the light or dark base theme.
type themePalette struct {
	background     mui.Color
	surface        mui.Color
	surfaceHover   mui.Color
	surfacePressed mui.Color
	border         mui.Color
	text           mui.Color
	textMuted      mui.Color
	accent         mui.Color
	accentHover    mui.Color
	accentPressed  mui.Color
	accentText     mui.Color
	danger         mui.Color
	warning        mui.Color
	success        mui.Color
	selection      mui.Color
	focus          mui.Color
	scrollbar      mui.Color
}

// apply copies the palette's colors over a base theme. Zero colors keep the
// base's, which lets the default family use MyGo's standard theme unchanged.
func (p themePalette) apply(t *mui.Theme) {
	set := func(dst *mui.Color, value mui.Color) {
		if value != mui.Transparent {
			*dst = value
		}
	}
	set(&t.Background, p.background)
	set(&t.Surface, p.surface)
	set(&t.SurfaceHover, p.surfaceHover)
	set(&t.SurfacePressed, p.surfacePressed)
	set(&t.Border, p.border)
	set(&t.Text, p.text)
	set(&t.TextMuted, p.textMuted)
	set(&t.Accent, p.accent)
	set(&t.AccentHover, p.accentHover)
	set(&t.AccentPressed, p.accentPressed)
	set(&t.AccentText, p.accentText)
	set(&t.Danger, p.danger)
	set(&t.Warning, p.warning)
	set(&t.Success, p.success)
	set(&t.Selection, p.selection)
	set(&t.Focus, p.focus)
	set(&t.Scrollbar, p.scrollbar)
}

type themeFamily struct {
	id    string
	name  string
	note  string
	light themePalette
	dark  themePalette
}

// themeFamilies is the built-in theme catalog. Default uses MyGo's own
// light and dark palettes; the others only replace colors. Colors follow the
// published Nord (nordtheme.com), Solarized (ethanschoonover.com/solarized)
// and Catppuccin (catppuccin.com) schemes, with a few light-variant accents
// darkened for readable text. Claude adapts Anthropic's warm neutral
// interface palette for both appearances.
var themeFamilies = []themeFamily{
	{
		id:   ThemeDefault,
		name: "Default",
		note: "MyGo's standard light and dark colors.",
	},
	{
		id:   ThemeNord,
		name: "Nord",
		note: "Snow Storm and Polar Night, with frost accents.",
		light: themePalette{
			background:     mui.Hex("#eceff4"),
			surface:        mui.Hex("#e5e9f0"),
			surfaceHover:   mui.Hex("#d8dee9"),
			surfacePressed: mui.Hex("#c8d0dd"),
			border:         mui.Hex("#c2cad6"),
			text:           mui.Hex("#2e3440"),
			textMuted:      mui.Hex("#4c566a"),
			accent:         mui.Hex("#4c6a92"),
			accentHover:    mui.Hex("#5e81ac"),
			accentPressed:  mui.Hex("#3d5778"),
			accentText:     mui.Hex("#ffffff"),
			danger:         mui.Hex("#bf616a"),
			warning:        mui.Hex("#8f6a00"),
			success:        mui.Hex("#4f7a3a"),
			selection:      mui.RGBA(94, 129, 172, 0.22),
			focus:          mui.RGBA(94, 129, 172, 0.55),
			scrollbar:      mui.RGBA(46, 52, 64, 0.30),
		},
		dark: themePalette{
			background:     mui.Hex("#2e3440"),
			surface:        mui.Hex("#3b4252"),
			surfaceHover:   mui.Hex("#434c5e"),
			surfacePressed: mui.Hex("#4c566a"),
			border:         mui.Hex("#4c566a"),
			text:           mui.Hex("#eceff4"),
			textMuted:      mui.Hex("#aeb8c9"),
			accent:         mui.Hex("#88c0d0"),
			accentHover:    mui.Hex("#8fbcbb"),
			accentPressed:  mui.Hex("#5e81ac"),
			accentText:     mui.Hex("#2e3440"),
			danger:         mui.Hex("#bf616a"),
			warning:        mui.Hex("#d08770"),
			success:        mui.Hex("#a3be8c"),
			selection:      mui.RGBA(136, 192, 208, 0.28),
			focus:          mui.RGBA(136, 192, 208, 0.60),
			scrollbar:      mui.RGBA(216, 222, 233, 0.35),
		},
	},
	{
		id:   ThemeSolarized,
		name: "Solarized",
		note: "Ethan Schoonover's balanced light and dark colors.",
		light: themePalette{
			background:     mui.Hex("#fdf6e3"),
			surface:        mui.Hex("#eee8d5"),
			surfaceHover:   mui.Hex("#e4ddc8"),
			surfacePressed: mui.Hex("#d9d1ba"),
			border:         mui.Hex("#d3c9b0"),
			text:           mui.Hex("#586e75"),
			textMuted:      mui.Hex("#657b83"),
			accent:         mui.Hex("#1f73af"),
			accentHover:    mui.Hex("#268bd2"),
			accentPressed:  mui.Hex("#185d8f"),
			accentText:     mui.Hex("#fdf6e3"),
			danger:         mui.Hex("#dc322f"),
			warning:        mui.Hex("#8f6a00"),
			success:        mui.Hex("#6b7a00"),
			selection:      mui.RGBA(38, 139, 210, 0.20),
			focus:          mui.RGBA(38, 139, 210, 0.55),
			scrollbar:      mui.RGBA(88, 110, 117, 0.30),
		},
		dark: themePalette{
			background:     mui.Hex("#002b36"),
			surface:        mui.Hex("#073642"),
			surfaceHover:   mui.Hex("#0d3f4c"),
			surfacePressed: mui.Hex("#15505f"),
			border:         mui.Hex("#1a4b58"),
			text:           mui.Hex("#93a1a1"),
			textMuted:      mui.Hex("#7b8f94"),
			accent:         mui.Hex("#3aa0e8"),
			accentHover:    mui.Hex("#57b4f4"),
			accentPressed:  mui.Hex("#268bd2"),
			accentText:     mui.Hex("#002b36"),
			danger:         mui.Hex("#dc322f"),
			warning:        mui.Hex("#b58900"),
			success:        mui.Hex("#859900"),
			selection:      mui.RGBA(38, 139, 210, 0.28),
			focus:          mui.RGBA(38, 139, 210, 0.60),
			scrollbar:      mui.RGBA(147, 161, 161, 0.35),
		},
	},
	{
		id:   ThemeCatppuccin,
		name: "Catppuccin",
		note: "Latte by day, Mocha by night.",
		light: themePalette{
			background:     mui.Hex("#eff1f5"),
			surface:        mui.Hex("#e6e9ef"),
			surfaceHover:   mui.Hex("#dce0e8"),
			surfacePressed: mui.Hex("#ccd0da"),
			border:         mui.Hex("#ccd0da"),
			text:           mui.Hex("#4c4f69"),
			textMuted:      mui.Hex("#6c6f85"),
			accent:         mui.Hex("#8839ef"),
			accentHover:    mui.Hex("#7a2fe0"),
			accentPressed:  mui.Hex("#6826c0"),
			accentText:     mui.Hex("#ffffff"),
			danger:         mui.Hex("#d20f39"),
			warning:        mui.Hex("#8f6a00"),
			success:        mui.Hex("#2f7d1f"),
			selection:      mui.RGBA(136, 57, 239, 0.22),
			focus:          mui.RGBA(136, 57, 239, 0.55),
			scrollbar:      mui.RGBA(76, 79, 105, 0.30),
		},
		dark: themePalette{
			background:     mui.Hex("#1e1e2e"),
			surface:        mui.Hex("#313244"),
			surfaceHover:   mui.Hex("#45475a"),
			surfacePressed: mui.Hex("#585b70"),
			border:         mui.Hex("#45475a"),
			text:           mui.Hex("#cdd6f4"),
			textMuted:      mui.Hex("#a6adc8"),
			accent:         mui.Hex("#cba6f7"),
			accentHover:    mui.Hex("#d7b9ff"),
			accentPressed:  mui.Hex("#b48ae6"),
			accentText:     mui.Hex("#1e1e2e"),
			danger:         mui.Hex("#f38ba8"),
			warning:        mui.Hex("#f9e2af"),
			success:        mui.Hex("#a6e3a1"),
			selection:      mui.RGBA(203, 166, 247, 0.28),
			focus:          mui.RGBA(203, 166, 247, 0.60),
			scrollbar:      mui.RGBA(205, 214, 244, 0.35),
		},
	},
	{
		id:   ThemeClaude,
		name: "Claude",
		note: "Warm paper and charcoal, with Claude orange.",
		light: themePalette{
			background:     mui.Hex("#faf9f5"),
			surface:        mui.Hex("#f0eee6"),
			surfaceHover:   mui.Hex("#e9e6dc"),
			surfacePressed: mui.Hex("#dfdcd0"),
			border:         mui.Hex("#d9d5c8"),
			text:           mui.Hex("#1f1e1d"),
			textMuted:      mui.Hex("#6f6b63"),
			accent:         mui.Hex("#b8562f"),
			accentHover:    mui.Hex("#c15f3c"),
			accentPressed:  mui.Hex("#8f3f25"),
			accentText:     mui.Hex("#faf9f5"),
			danger:         mui.Hex("#b3261e"),
			warning:        mui.Hex("#9a6a1f"),
			success:        mui.Hex("#4f7a28"),
			selection:      mui.RGBA(193, 95, 60, 0.18),
			focus:          mui.RGBA(193, 95, 60, 0.50),
			scrollbar:      mui.RGBA(31, 30, 29, 0.28),
		},
		dark: themePalette{
			background:     mui.Hex("#262624"),
			surface:        mui.Hex("#30302e"),
			surfaceHover:   mui.Hex("#3a3a37"),
			surfacePressed: mui.Hex("#454542"),
			border:         mui.Hex("#454542"),
			text:           mui.Hex("#f5f4ef"),
			textMuted:      mui.Hex("#b0aea4"),
			accent:         mui.Hex("#d97757"),
			accentHover:    mui.Hex("#e08a6e"),
			accentPressed:  mui.Hex("#c46647"),
			accentText:     mui.Hex("#1f1e1d"),
			danger:         mui.Hex("#ef7b72"),
			warning:        mui.Hex("#e0b341"),
			success:        mui.Hex("#8fbc6f"),
			selection:      mui.RGBA(217, 119, 87, 0.30),
			focus:          mui.RGBA(217, 119, 87, 0.60),
			scrollbar:      mui.RGBA(245, 244, 239, 0.30),
		},
	},
}

// normalizeAppearance maps an unknown or empty value to system.
func normalizeAppearance(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case AppearanceLight:
		return AppearanceLight
	case AppearanceDark:
		return AppearanceDark
	default:
		return AppearanceSystem
	}
}

// normalizeTheme maps an unknown or empty value to the default family.
func normalizeTheme(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if _, ok := themeFamilyByID(value); ok {
		return value
	}
	return ThemeDefault
}

func themeFamilyByID(id string) (themeFamily, bool) {
	for _, family := range themeFamilies {
		if family.id == id {
			return family, true
		}
	}
	return themeFamily{}, false
}

func (f themeFamily) palette(dark bool) themePalette {
	if dark {
		return f.dark
	}
	return f.light
}

// themePreview builds a family's palette for a settings row. The default
// family uses MyGo's own theme so its preview follows the standard colors.
func themePreview(family themeFamily, dark bool) *mui.Theme {
	t := mui.LightTheme()
	if dark {
		t = mui.DarkTheme()
	}
	if family.id != ThemeDefault {
		family.palette(dark).apply(t)
	}
	return t
}

// resolvedTheme returns the window theme for a family and appearance, or nil
// when MyGo's default should be kept as is (system appearance of the default
// family). systemDark is the desktop's current appearance; it is only used
// when appearance is system.
func resolvedTheme(familyID, appearance string, systemDark bool, prefs mui.Preferences) *mui.Theme {
	familyID = normalizeTheme(familyID)
	appearance = normalizeAppearance(appearance)
	family, _ := themeFamilyByID(familyID)

	if family.id == ThemeDefault && appearance == AppearanceSystem {
		return nil
	}
	dark := appearance == AppearanceDark || (appearance == AppearanceSystem && systemDark)
	t := mui.LightTheme()
	if dark {
		t = mui.DarkTheme()
	}
	if family.id != ThemeDefault {
		family.palette(dark).apply(t)
	}
	// The default family follows the desktop accent, as MyGo's theme does.
	// A named family keeps its own accent and only follows accessibility.
	applyPreferences(t, prefs, family.id == ThemeDefault)
	return t
}

// applyPreferences mirrors MyGo's Theme.follow for themes built here. Custom
// palettes keep their accent but still respect high contrast and text scale.
func applyPreferences(t *mui.Theme, prefs mui.Preferences, systemAccent bool) {
	if systemAccent {
		applyAccent(t, prefs.Accent)
	}
	if prefs.HighContrast {
		t.Border = t.Border.Mix(t.Text, 0.45)
		t.TextMuted = t.TextMuted.Mix(t.Text, 0.4)
		t.Focus.A = 255
		t.Scrollbar = t.Scrollbar.Alpha(1.6)
	}
	if scale := prefs.TextScale; scale > 0 && scale != 1 {
		t.FontSize *= scale
	}
}

func applyAccent(t *mui.Theme, accent mui.Color) {
	if accent.A == 0 {
		return
	}
	accent.A = 255
	black := mui.Color{A: 255}
	white := mui.Color{R: 255, G: 255, B: 255, A: 255}
	t.Accent = accent
	if t.Dark {
		t.AccentHover = accent.Mix(white, 0.2)
		t.AccentPressed = accent.Mix(black, 0.15)
		t.Selection = accent.Alpha(0.4)
		t.Focus = accent.Mix(white, 0.2).Alpha(0.6)
	} else {
		t.AccentHover = accent.Mix(black, 0.12)
		t.AccentPressed = accent.Mix(black, 0.25)
		t.Selection = accent.Alpha(0.25)
		t.Focus = accent.Alpha(0.55)
	}
	t.AccentText = white
	luma := 0.2126*float32(accent.R) + 0.7152*float32(accent.G) + 0.0722*float32(accent.B)
	if luma > 165 {
		t.AccentText = mui.Color{R: 24, G: 24, B: 27, A: 255}
	}
}

// effectiveDark resolves the current light/dark variant without depending on
// the native appearance, so tests and forced appearances agree.
func (a *App) effectiveDark(c *mui.Context) bool {
	switch normalizeAppearance(a.Appearance) {
	case AppearanceLight:
		return false
	case AppearanceDark:
		return true
	default:
		return c.Theme().Dark
	}
}

// applyTheme installs the selected family before the window builds.
func (a *App) applyTheme(c *mui.Context) {
	if t := resolvedTheme(a.Theme, a.Appearance, c.Theme().Dark, c.Preferences()); t != nil {
		c.SetTheme(t)
	}
}

// ApplyAppearance gives the native window chrome and menus the selected
// appearance. Call it after the app is ready and before creating a window,
// and when the appearance setting changes.
func (a *App) ApplyAppearance() {
	if a == nil {
		return
	}
	mygo.Theme.SetSource(themeSourceFor(a.Appearance))
}

func themeSourceFor(appearance string) mygo.ThemeSource {
	switch normalizeAppearance(appearance) {
	case AppearanceLight:
		return mygo.ThemeLight
	case AppearanceDark:
		return mygo.ThemeDark
	default:
		return mygo.ThemeSystem
	}
}

func (a *App) setAppearance(value string) {
	value = normalizeAppearance(value)
	if value == a.Appearance {
		return
	}
	a.Appearance = value
	a.persistSettings()
	// Tests have no window and no MyGo app; leave the native theme alone.
	if a.window() != nil {
		a.ApplyAppearance()
	}
}

func (a *App) setTheme(value string) {
	value = normalizeTheme(value)
	if value == a.Theme {
		return
	}
	a.Theme = value
	a.persistSettings()
}
