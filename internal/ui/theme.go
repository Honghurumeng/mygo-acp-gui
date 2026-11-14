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
	ThemeGreen      = "green"
	ThemeGreenSoft  = "green-soft"
	ThemeVSCode     = "vscode"
	ThemeEverforest = "everforest"
	ThemeOneDark    = "one-dark"
	ThemeTokyoNight = "tokyo-night"
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
// interface palette; Green is a low-glare eye-care palette built around the
// classic paper-green background (#c9ecd0) with a deep forest night, and
// Green Soft lowers that saturation toward a near-neutral paper. VS Code
// adapts Dark Modern / Light Modern, Everforest follows sainnhe/everforest,
// One Dark / Light follow Atom's One themes, and Tokyo Night / Day follow
// folke/tokyonight.nvim. Light accents are darkened where needed for text
// contrast.
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
	{
		id:   ThemeGreen,
		name: "Green",
		note: "Eye-care paper green, with a soft forest night.",
		light: themePalette{
			background:     mui.Hex("#c9ecd0"),
			surface:        mui.Hex("#bfe6c7"),
			surfaceHover:   mui.Hex("#b3ddbc"),
			surfacePressed: mui.Hex("#a7d4b1"),
			border:         mui.Hex("#9bc9a6"),
			text:           mui.Hex("#1e3327"),
			textMuted:      mui.Hex("#45634f"),
			accent:         mui.Hex("#24704c"),
			accentHover:    mui.Hex("#2b8259"),
			accentPressed:  mui.Hex("#1a5a3c"),
			accentText:     mui.Hex("#ffffff"),
			danger:         mui.Hex("#b02a25"),
			warning:        mui.Hex("#7a5b00"),
			success:        mui.Hex("#3f6b2a"),
			selection:      mui.RGBA(36, 112, 76, 0.20),
			focus:          mui.RGBA(36, 112, 76, 0.55),
			scrollbar:      mui.RGBA(30, 51, 39, 0.30),
		},
		dark: themePalette{
			background:     mui.Hex("#1b241e"),
			surface:        mui.Hex("#243029"),
			surfaceHover:   mui.Hex("#2c3a31"),
			surfacePressed: mui.Hex("#35453a"),
			border:         mui.Hex("#3b4c40"),
			text:           mui.Hex("#dde8de"),
			textMuted:      mui.Hex("#a2b4a6"),
			accent:         mui.Hex("#7fbf95"),
			accentHover:    mui.Hex("#94cfa7"),
			accentPressed:  mui.Hex("#6bab82"),
			accentText:     mui.Hex("#16201a"),
			danger:         mui.Hex("#e2807a"),
			warning:        mui.Hex("#d8b46b"),
			success:        mui.Hex("#9bc97b"),
			selection:      mui.RGBA(127, 191, 149, 0.26),
			focus:          mui.RGBA(127, 191, 149, 0.58),
			scrollbar:      mui.RGBA(221, 232, 222, 0.32),
		},
	},
	{
		id:   ThemeGreenSoft,
		name: "Green Soft",
		note: "A muted, low-saturation take on the eye-care green.",
		light: themePalette{
			background:     mui.Hex("#e8f1ea"),
			surface:        mui.Hex("#e0ece2"),
			surfaceHover:   mui.Hex("#d7e5d9"),
			surfacePressed: mui.Hex("#ccdccf"),
			border:         mui.Hex("#c2d5c6"),
			text:           mui.Hex("#26332a"),
			textMuted:      mui.Hex("#4d6355"),
			accent:         mui.Hex("#2f7250"),
			accentHover:    mui.Hex("#3a8560"),
			accentPressed:  mui.Hex("#265f43"),
			accentText:     mui.Hex("#ffffff"),
			danger:         mui.Hex("#b02a25"),
			warning:        mui.Hex("#7a5b00"),
			success:        mui.Hex("#3f6b2a"),
			selection:      mui.RGBA(47, 114, 80, 0.18),
			focus:          mui.RGBA(47, 114, 80, 0.50),
			scrollbar:      mui.RGBA(38, 51, 42, 0.26),
		},
		dark: themePalette{
			background:     mui.Hex("#171b18"),
			surface:        mui.Hex("#1f2521"),
			surfaceHover:   mui.Hex("#272e29"),
			surfacePressed: mui.Hex("#303831"),
			border:         mui.Hex("#38423b"),
			text:           mui.Hex("#e0e4e0"),
			textMuted:      mui.Hex("#a6b0a8"),
			accent:         mui.Hex("#8cc4a0"),
			accentHover:    mui.Hex("#9fd1b0"),
			accentPressed:  mui.Hex("#79b28e"),
			accentText:     mui.Hex("#16201a"),
			danger:         mui.Hex("#e2807a"),
			warning:        mui.Hex("#d8b46b"),
			success:        mui.Hex("#9bc97b"),
			selection:      mui.RGBA(140, 196, 160, 0.24),
			focus:          mui.RGBA(140, 196, 160, 0.55),
			scrollbar:      mui.RGBA(224, 228, 224, 0.30),
		},
	},
	{
		id:   ThemeVSCode,
		name: "VS Code",
		note: "Dark Modern and Light Modern, the editor defaults.",
		light: themePalette{
			background:     mui.Hex("#ffffff"),
			surface:        mui.Hex("#f8f8f8"),
			surfaceHover:   mui.Hex("#f2f2f2"),
			surfacePressed: mui.Hex("#e8e8e8"),
			border:         mui.Hex("#e0e0e0"),
			text:           mui.Hex("#3b3b3b"),
			textMuted:      mui.Hex("#6e6e6e"),
			accent:         mui.Hex("#005fb8"),
			accentHover:    mui.Hex("#006cc4"),
			accentPressed:  mui.Hex("#004a91"),
			accentText:     mui.Hex("#ffffff"),
			danger:         mui.Hex("#c72e2e"),
			warning:        mui.Hex("#8a6100"),
			success:        mui.Hex("#2f7a2c"),
			selection:      mui.RGBA(0, 95, 184, 0.16),
			focus:          mui.RGBA(0, 95, 184, 0.55),
			scrollbar:      mui.RGBA(59, 59, 59, 0.26),
		},
		dark: themePalette{
			background:     mui.Hex("#1f1f1f"),
			surface:        mui.Hex("#1d1d1d"),
			surfaceHover:   mui.Hex("#2a2d2e"),
			surfacePressed: mui.Hex("#37373d"),
			border:         mui.Hex("#3c3c3c"),
			text:           mui.Hex("#cccccc"),
			textMuted:      mui.Hex("#9d9d9d"),
			accent:         mui.Hex("#0078d4"),
			accentHover:    mui.Hex("#006cc4"),
			accentPressed:  mui.Hex("#0060aa"),
			accentText:     mui.Hex("#ffffff"),
			danger:         mui.Hex("#f85149"),
			warning:        mui.Hex("#cca700"),
			success:        mui.Hex("#73c991"),
			selection:      mui.RGBA(0, 120, 212, 0.28),
			focus:          mui.RGBA(0, 120, 212, 0.60),
			scrollbar:      mui.RGBA(121, 121, 121, 0.40),
		},
	},
	{
		id:   ThemeEverforest,
		name: "Everforest",
		note: "Muted forest greens, made for long reading sessions.",
		light: themePalette{
			background:     mui.Hex("#fffbef"),
			surface:        mui.Hex("#f3f0e0"),
			surfaceHover:   mui.Hex("#ece8d6"),
			surfacePressed: mui.Hex("#e5e0cc"),
			border:         mui.Hex("#d9d3bf"),
			text:           mui.Hex("#5c6a72"),
			textMuted:      mui.Hex("#5c6d60"),
			accent:         mui.Hex("#6b7c00"),
			accentHover:    mui.Hex("#7d9000"),
			accentPressed:  mui.Hex("#576600"),
			accentText:     mui.Hex("#ffffff"),
			danger:         mui.Hex("#c43c39"),
			warning:        mui.Hex("#8a6a00"),
			success:        mui.Hex("#1f7a5a"),
			selection:      mui.RGBA(107, 124, 0, 0.16),
			focus:          mui.RGBA(107, 124, 0, 0.50),
			scrollbar:      mui.RGBA(92, 106, 114, 0.28),
		},
		dark: themePalette{
			background:     mui.Hex("#2d353b"),
			surface:        mui.Hex("#343f44"),
			surfaceHover:   mui.Hex("#3d484d"),
			surfacePressed: mui.Hex("#475258"),
			border:         mui.Hex("#4f585e"),
			text:           mui.Hex("#d3c6aa"),
			textMuted:      mui.Hex("#a8b3aa"),
			accent:         mui.Hex("#a7c080"),
			accentHover:    mui.Hex("#b8cf97"),
			accentPressed:  mui.Hex("#8fae66"),
			accentText:     mui.Hex("#2d353b"),
			danger:         mui.Hex("#e67e80"),
			warning:        mui.Hex("#dbbc7f"),
			success:        mui.Hex("#83c092"),
			selection:      mui.RGBA(167, 192, 128, 0.26),
			focus:          mui.RGBA(167, 192, 128, 0.58),
			scrollbar:      mui.RGBA(211, 198, 170, 0.28),
		},
	},
	{
		id:   ThemeOneDark,
		name: "One Dark",
		note: "Atom's One Dark by night, One Light by day.",
		light: themePalette{
			background:     mui.Hex("#fafafa"),
			surface:        mui.Hex("#f0f0f1"),
			surfaceHover:   mui.Hex("#e7e7e9"),
			surfacePressed: mui.Hex("#dedee1"),
			border:         mui.Hex("#d6d6da"),
			text:           mui.Hex("#383a42"),
			textMuted:      mui.Hex("#696c77"),
			accent:         mui.Hex("#3a6fe0"),
			accentHover:    mui.Hex("#4a7fe8"),
			accentPressed:  mui.Hex("#2f5cc4"),
			accentText:     mui.Hex("#ffffff"),
			danger:         mui.Hex("#c9403a"),
			warning:        mui.Hex("#986801"),
			success:        mui.Hex("#3d7d39"),
			selection:      mui.RGBA(58, 111, 224, 0.16),
			focus:          mui.RGBA(58, 111, 224, 0.50),
			scrollbar:      mui.RGBA(56, 58, 66, 0.26),
		},
		dark: themePalette{
			background:     mui.Hex("#282c34"),
			surface:        mui.Hex("#21252b"),
			surfaceHover:   mui.Hex("#2c313a"),
			surfacePressed: mui.Hex("#353b45"),
			border:         mui.Hex("#3a3f4b"),
			text:           mui.Hex("#abb2bf"),
			textMuted:      mui.Hex("#8f97a5"),
			accent:         mui.Hex("#61afef"),
			accentHover:    mui.Hex("#74bdf5"),
			accentPressed:  mui.Hex("#4a9de0"),
			accentText:     mui.Hex("#1b1f27"),
			danger:         mui.Hex("#e06c75"),
			warning:        mui.Hex("#e5c07b"),
			success:        mui.Hex("#98c379"),
			selection:      mui.RGBA(97, 175, 239, 0.24),
			focus:          mui.RGBA(97, 175, 239, 0.55),
			scrollbar:      mui.RGBA(171, 178, 191, 0.28),
		},
	},
	{
		id:   ThemeTokyoNight,
		name: "Tokyo Night",
		note: "Neon Tokyo after dark, soft blue day.",
		light: themePalette{
			background:     mui.Hex("#e1e2e7"),
			surface:        mui.Hex("#d6dae6"),
			surfaceHover:   mui.Hex("#cbd1e0"),
			surfacePressed: mui.Hex("#c4c8da"),
			border:         mui.Hex("#b9c0d3"),
			text:           mui.Hex("#343b58"),
			textMuted:      mui.Hex("#4a5a96"),
			accent:         mui.Hex("#2568d0"),
			accentHover:    mui.Hex("#3579e0"),
			accentPressed:  mui.Hex("#1f58b8"),
			accentText:     mui.Hex("#ffffff"),
			danger:         mui.Hex("#b81a45"),
			warning:        mui.Hex("#8f4a00"),
			success:        mui.Hex("#4a642f"),
			selection:      mui.RGBA(37, 104, 208, 0.18),
			focus:          mui.RGBA(37, 104, 208, 0.52),
			scrollbar:      mui.RGBA(52, 59, 88, 0.26),
		},
		dark: themePalette{
			background:     mui.Hex("#1a1b26"),
			surface:        mui.Hex("#1d1e28"),
			surfaceHover:   mui.Hex("#23253a"),
			surfacePressed: mui.Hex("#2b2e47"),
			border:         mui.Hex("#2f3350"),
			text:           mui.Hex("#c0caf5"),
			textMuted:      mui.Hex("#9aa5ce"),
			accent:         mui.Hex("#7aa2f7"),
			accentHover:    mui.Hex("#89b0ff"),
			accentPressed:  mui.Hex("#6791e0"),
			accentText:     mui.Hex("#1a1b26"),
			danger:         mui.Hex("#f7768e"),
			warning:        mui.Hex("#e0af68"),
			success:        mui.Hex("#9ece6a"),
			selection:      mui.RGBA(122, 162, 247, 0.28),
			focus:          mui.RGBA(122, 162, 247, 0.60),
			scrollbar:      mui.RGBA(192, 202, 245, 0.30),
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
