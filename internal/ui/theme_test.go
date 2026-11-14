package ui

import (
	"testing"

	"github.com/egoist/mygo"
	mui "github.com/egoist/mygo/ui"
	"github.com/reed/acp-gui/internal/agent"
)

func TestThemeCatalog(t *testing.T) {
	seen := make(map[string]bool, len(themeFamilies))
	for _, family := range themeFamilies {
		if family.id == "" || family.name == "" {
			t.Errorf("theme family without id or name: %+v", family)
		}
		if seen[family.id] {
			t.Errorf("duplicate theme family %q", family.id)
		}
		seen[family.id] = true
		if family.id == ThemeDefault {
			continue
		}
		for _, dark := range []bool{false, true} {
			theme := themePreview(family, dark)
			if theme.Dark != dark {
				t.Errorf("%s/%v: Dark = %v", family.id, dark, theme.Dark)
			}
			if theme.Background == theme.Surface || theme.Text == theme.TextMuted || theme.Accent == theme.Background {
				t.Errorf("%s/%v: palette is missing colors", family.id, dark)
			}
		}
	}
	for _, id := range []string{ThemeDefault, ThemeNord, ThemeSolarized, ThemeCatppuccin, ThemeClaude} {
		if !seen[id] {
			t.Errorf("theme family %q is not in the catalog", id)
		}
	}
}

func TestNormalizeAppearanceAndTheme(t *testing.T) {
	for input, want := range map[string]string{
		"":              AppearanceSystem,
		" system ":      AppearanceSystem,
		"LIGHT":         AppearanceLight,
		"dark":          AppearanceDark,
		"high-contrast": AppearanceSystem,
	} {
		if got := normalizeAppearance(input); got != want {
			t.Errorf("normalizeAppearance(%q) = %q, want %q", input, got, want)
		}
	}
	for input, want := range map[string]string{
		"":           ThemeDefault,
		" NORD ":     ThemeNord,
		"Solarized":  ThemeSolarized,
		"catppuccin": ThemeCatppuccin,
		"claude":     ThemeClaude,
		"unknown":    ThemeDefault,
	} {
		if got := normalizeTheme(input); got != want {
			t.Errorf("normalizeTheme(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestResolvedTheme(t *testing.T) {
	if got := resolvedTheme(ThemeDefault, AppearanceSystem, true, mui.Preferences{}); got != nil {
		t.Fatalf("system default should keep MyGo's theme, got %+v", got)
	}

	tests := []struct {
		name       string
		family     string
		appearance string
		systemDark bool
		dark       bool
		background mui.Color
	}{
		{"default light", ThemeDefault, AppearanceLight, true, false, mui.LightTheme().Background},
		{"default dark", ThemeDefault, AppearanceDark, false, true, mui.DarkTheme().Background},
		{"nord system light", ThemeNord, AppearanceSystem, false, false, mui.Hex("#eceff4")},
		{"nord system dark", ThemeNord, AppearanceSystem, true, true, mui.Hex("#2e3440")},
		{"solarized light", ThemeSolarized, AppearanceLight, true, false, mui.Hex("#fdf6e3")},
		{"solarized dark", ThemeSolarized, AppearanceDark, false, true, mui.Hex("#002b36")},
		{"catppuccin latte", ThemeCatppuccin, AppearanceLight, true, false, mui.Hex("#eff1f5")},
		{"catppuccin mocha", ThemeCatppuccin, AppearanceDark, false, true, mui.Hex("#1e1e2e")},
		{"claude light", ThemeClaude, AppearanceLight, true, false, mui.Hex("#faf9f5")},
		{"claude dark", ThemeClaude, AppearanceDark, false, true, mui.Hex("#262624")},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			theme := resolvedTheme(test.family, test.appearance, test.systemDark, mui.Preferences{TextScale: 1})
			if theme == nil {
				t.Fatal("resolvedTheme returned nil")
			}
			if theme.Dark != test.dark {
				t.Errorf("Dark = %v, want %v", theme.Dark, test.dark)
			}
			if theme.Background != test.background {
				t.Errorf("Background = %v, want %v", theme.Background, test.background)
			}
		})
	}
}

func TestViewAppliesSelectedTheme(t *testing.T) {
	app := New(Options{
		Version:   "test",
		Workspace: t.TempDir(),
		Agents:    []agent.LaunchSpec{},
	})
	t.Cleanup(func() {
		app.Shutdown()
		app.Flush()
	})
	app.Appearance = AppearanceDark
	app.Theme = ThemeNord

	var built *mui.Theme
	tt := mui.NewTester(func(c *mui.Context) {
		app.View(c)
		built = c.Theme()
	}, 800, 560)
	tt.Frame()

	if built == nil || !built.Dark {
		t.Fatalf("window theme = %+v, want Nord dark", built)
	}
	if built.Background != mui.Hex("#2e3440") {
		t.Fatalf("background = %v, want Nord dark", built.Background)
	}
}

func TestSunkenSurfaceKeepsRailDistinct(t *testing.T) {
	luma := func(c mui.Color) float32 {
		return 0.2126*float32(c.R) + 0.7152*float32(c.G) + 0.0722*float32(c.B)
	}
	step := func(a, b mui.Color) float32 {
		d := luma(a) - luma(b)
		if d < 0 {
			return -d
		}
		return d
	}
	for _, family := range themeFamilies {
		for _, dark := range []bool{false, true} {
			theme := themePreview(family, dark)
			rail := sunkenSurface(theme)
			if luma(rail) >= luma(theme.Surface) {
				t.Errorf("%s/%v: rail is not darker than the panel", family.id, dark)
			}
			if step(rail, theme.Surface) < 5 {
				t.Errorf("%s/%v: rail %v is too close to surface %v", family.id, dark, rail, theme.Surface)
			}
			// The rail's own hover and pressed fills must stay visible on
			// top of the rail, whatever the palette's surface states are.
			if step(theme.Text.Alpha(0.04).Over(rail), rail) < 3 {
				t.Errorf("%s/%v: hover fill blends into the rail", family.id, dark)
			}
			if step(theme.Text.Alpha(0.10).Over(rail), rail) < 6 {
				t.Errorf("%s/%v: pressed fill blends into the rail", family.id, dark)
			}
		}
	}
}

func TestThemeFollowsTextScale(t *testing.T) {
	theme := resolvedTheme(ThemeNord, AppearanceDark, false, mui.Preferences{TextScale: 1.5})
	if theme == nil {
		t.Fatal("resolvedTheme returned nil")
	}
	if want := mui.DarkTheme().FontSize * 1.5; theme.FontSize != want {
		t.Errorf("FontSize = %v, want %v", theme.FontSize, want)
	}
}

func TestThemeSourceFor(t *testing.T) {
	tests := map[string]mygo.ThemeSource{
		AppearanceSystem: mygo.ThemeSystem,
		AppearanceLight:  mygo.ThemeLight,
		AppearanceDark:   mygo.ThemeDark,
		"":               mygo.ThemeSystem,
		"nope":           mygo.ThemeSystem,
	}
	for appearance, want := range tests {
		if got := themeSourceFor(appearance); got != want {
			t.Errorf("themeSourceFor(%q) = %q, want %q", appearance, got, want)
		}
	}
}
