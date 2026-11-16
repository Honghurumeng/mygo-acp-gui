package ui

import (
	"fmt"
	"strings"

	mui "github.com/egoist/mygo/ui"
)

const (
	settingsSectionAppearance    = "appearance"
	settingsSectionFonts         = "fonts"
	settingsSectionNotifications = "notifications"
)

func (a *App) openSettings() {
	if a.SettingsSection == "" {
		a.SettingsSection = settingsSectionNotifications
	}
	a.NotificationTestNotice = ""
	a.loadCodexProviderDraft()
	a.SettingsOpen = true
}

// settingsDialog is the general settings panel: a category list on the left
// and the selected category's settings on the right.
func (a *App) settingsDialog(c *mui.Context) {
	if !a.SettingsOpen {
		return
	}
	t := c.Theme()
	mui.Modal(c, &a.SettingsOpen, func() {
		mui.Column(c).Gap(12).Width(720).Children(func() {
			mui.Text(c, "Settings").FontSize(18).Bold()
			mui.Row(c).Height(390).Clip().Radius(10).Border(1, t.Border).Children(func() {
				mui.Column(c).Width(176).FillHeight().Gap(2).Padding(8).
					Background(t.Surface).BorderWidth(0, 1, 0, 0).BorderColor(t.Border).
					Children(func() {
						a.settingsCategory(c, settingsSectionAppearance, "Appearance")
						a.settingsCategory(c, settingsSectionFonts, "Fonts")
						a.settingsCategory(c, settingsSectionNotifications, "Notifications")
						a.settingsCategory(c, settingsSectionCodexProvider, "Codex provider")
					})
				mui.Column(c).Grow(1).FillHeight().Children(func() {
					// The appearance list keeps growing with the theme catalog,
					// so the whole pane scrolls and every row stays reachable.
					mui.Scroll(c).Grow(1).Padding(18).Children(func() {
						switch a.SettingsSection {
						case settingsSectionAppearance:
							a.appearanceSettings(c)
						case settingsSectionFonts:
							a.fontSettings(c)
						case settingsSectionNotifications:
							a.notificationSettings(c)
						case settingsSectionCodexProvider:
							a.codexProviderSettings(c)
						}
					})
				})
			})
			mui.Row(c).Gap(8).Justify(mui.End).Children(func() {
				if primaryButton(c, "Close").Clicked() {
					a.SettingsOpen = false
				}
			})
		})
	})
}

func (a *App) settingsCategory(c *mui.Context, id, label string) {
	t := c.Theme()
	item := mui.ButtonBase(c).FillWidth().Padding(7, 10).Radius(8).Label(label)
	switch {
	case a.SettingsSection == id:
		item.Background(t.Accent).TextColor(t.AccentText)
	case item.Hovered():
		item.Background(t.SurfaceHover)
	}
	item.Children(func() {
		mui.Text(c, label).FontSize(11).SingleLine()
	})
	if item.Clicked() && a.SettingsSection != id {
		a.SettingsSection = id
		a.NotificationTestNotice = ""
	}
}

func (a *App) appearanceSettings(c *mui.Context) {
	dark := a.effectiveDark(c)
	mui.Column(c).Gap(8).Children(func() {
		mui.Text(c, "Appearance").FontSize(15).Bold()
		mutedText(c, "System follows the desktop. Theme picks the colors used in light and dark.", 10)
		mui.Row(c).Gap(6).Children(func() {
			a.appearanceChoice(c, AppearanceSystem, "System")
			a.appearanceChoice(c, AppearanceLight, "Light")
			a.appearanceChoice(c, AppearanceDark, "Dark")
		})
		mui.Row(c).Gap(6).AlignItems(mui.Center).Children(func() {
			mui.Text(c, "Theme").FontSize(12).Bold()
			caption(c, "Previewed in the current appearance")
		})
		mui.Column(c).Gap(3).Children(func() {
			for _, family := range themeFamilies {
				a.themeChoice(c, family, dark)
			}
		})
	})
}

// fontSettings scales four regions independently. A percentage keeps every
// region's internal hierarchy (titles, rows and captions) and its existing
// layout intact while making the change easy to read in the settings file.
func (a *App) fontSettings(c *mui.Context) {
	mui.Column(c).Gap(8).Children(func() {
		mui.Text(c, "Fonts").FontSize(15).Bold()
		mutedText(c, "Scale text in each area from 80% to 150%. Changes apply immediately and are saved.", 10)
		a.fontScaleRow(c, "Workspace list",
			"Groups, conversations and saved sessions in the sidebar.",
			&a.Fonts.WorkspaceList)
		a.fontScaleRow(c, "Conversation",
			"Messages, tool calls and Markdown in the chat.",
			&a.Fonts.Chat)
		a.fontScaleRow(c, "Input box",
			"The message box, its chips and its menus.",
			&a.Fonts.Composer)
		a.fontScaleRow(c, "Git panel",
			"Changes, history, commit details and the diff card.",
			&a.Fonts.Git)
		if quietButton(c, "Reset font sizes").FontSize(10).Clicked() {
			a.Fonts = defaultFontScales()
			a.persistSettings()
		}
	})
}

func (a *App) fontScaleRow(c *mui.Context, title, note string, value *int) {
	t := c.Theme()
	*value = normalizeFontScale(*value)
	mui.Row(c).Gap(10).AlignItems(mui.Center).Padding(8, 12).Radius(8).
		Background(t.Surface).Children(func() {
		mui.Column(c).Gap(2).Grow(1).Children(func() {
			mui.Text(c, title).FontSize(12)
			mutedText(c, note, 10)
		})
		smaller := quietButton(c, "−").Label("Decrease " + title + " font size").FontSize(11)
		smaller.Disabled(*value <= minFontScale)
		if smaller.Clicked() && *value > minFontScale {
			*value = normalizeFontScale(*value - fontScaleStep)
			a.persistSettings()
		}
		mui.Text(c, fmt.Sprintf("%d%%", *value)).FontSize(11).
			Width(42).TextAlign(mui.Center).Shrink(0)
		larger := quietButton(c, "+").Label("Increase " + title + " font size").FontSize(11)
		larger.Disabled(*value >= maxFontScale)
		if larger.Clicked() && *value < maxFontScale {
			*value = normalizeFontScale(*value + fontScaleStep)
			a.persistSettings()
		}
	})
}

func (a *App) appearanceChoice(c *mui.Context, value, label string) {
	t := c.Theme()
	selected := normalizeAppearance(a.Appearance) == value
	choice := quietButton(c, label).FontSize(11).Role(mui.RoleRadio).AccessSelected(selected)
	if selected {
		choice.Background(t.Accent).TextColor(t.AccentText)
	} else {
		choice.Border(1, t.Border)
	}
	if choice.Clicked() {
		a.setAppearance(value)
	}
}

func (a *App) themeChoice(c *mui.Context, family themeFamily, dark bool) {
	t := c.Theme()
	selected := normalizeTheme(a.Theme) == family.id
	row := mui.ButtonBase(c).FillWidth().Gap(8).Padding(4, 6).Radius(6).
		Role(mui.RoleRadio).Label("Theme: " + family.name).AccessSelected(selected)
	switch {
	case selected:
		row.Background(t.Accent.Alpha(0.14))
	case row.Hovered():
		row.Background(t.SurfaceHover)
	}
	row.Children(func() {
		preview := themePreview(family, dark)
		mui.Row(c).Gap(3).Shrink(0).Children(func() {
			themeSwatch(c, preview.Background, preview.Border)
			themeSwatch(c, preview.Surface, preview.Border)
			themeSwatch(c, preview.Accent, preview.Accent)
			themeSwatch(c, preview.Text, preview.Text)
		})
		mui.Column(c).Gap(0).Grow(1).MinWidth(0).Children(func() {
			mui.Row(c).Gap(5).AlignItems(mui.Center).Children(func() {
				mui.Text(c, family.name).FontSize(11).Bold()
				if selected {
					mui.Text(c, "✓").FontSize(9).TextColor(t.Accent)
				}
			})
			mutedText(c, family.note, 9).SingleLine()
		})
	})
	if row.Clicked() {
		a.setTheme(family.id)
	}
}

func themeSwatch(c *mui.Context, fill, border mui.Color) {
	mui.Box(c).Size(12, 12).Radius(3).Background(fill).Border(1, border)
}

func (a *App) notificationSettings(c *mui.Context) {
	t := c.Theme()
	mui.Column(c).Gap(12).Children(func() {
		mui.Text(c, "Notifications").FontSize(15).Bold()
		mutedText(c, "Choose when ACP GUI asks for attention outside the window.", 11)
		mui.Row(c).Gap(16).AlignItems(mui.Center).Padding(10, 12).Radius(8).
			Background(t.Surface).Children(func() {
			mui.Column(c).Gap(3).Grow(1).Children(func() {
				mui.Text(c, "System notifications").FontSize(12)
				mutedText(c, "Notify on permission requests and finished turns while the window is closed, unfocused, or showing another conversation.", 10)
			})
			sw := mui.Switch(c, &a.NotificationsEnabled).Label("Toggle system notifications")
			if sw.Changed() {
				a.NotificationTestNotice = ""
				a.persistSettings()
			}
		})
		mui.Row(c).Gap(16).AlignItems(mui.Center).Padding(10, 12).Radius(8).
			Background(t.Surface).Children(func() {
			mui.Column(c).Gap(3).Grow(1).Children(func() {
				mui.Text(c, "Dock badge").FontSize(12)
				mutedText(c, "Show the number of conversations waiting for a permission answer or that finished while you were elsewhere. On the Dock (macOS) or launcher (Linux).", 10)
			})
			sw := mui.Switch(c, &a.DockBadgeEnabled).Label("Toggle Dock badge")
			if sw.Changed() {
				a.NotificationTestNotice = ""
				a.persistSettings()
				a.updateDockBadge()
			}
		})
		mui.Row(c).Gap(10).AlignItems(mui.Center).Children(func() {
			test := quietButton(c, "Send test notification").FontSize(11).Border(1, t.Border)
			test.Disabled(!a.NotificationsEnabled)
			if test.Clicked() {
				a.sendTestNotification()
			}
			if !a.NotificationsEnabled {
				caption(c, "Turn on system notifications to test.")
			}
		})
		if notice := strings.TrimSpace(a.NotificationTestNotice); notice != "" {
			mutedText(c, notice, 10).Selectable()
		}
	})
}
