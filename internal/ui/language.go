package ui

import (
	mui "github.com/egoist/mygo/ui"

	"github.com/reed/acp-gui/internal/i18n"
)

// applyLanguage shows the app's messages in the language the settings ask
// for, resolving System against the user's preferred languages, and hands the
// same choice to MyGo through OnLanguage so that its own controls, menus and
// dialogs agree. It runs when the settings load and when the user picks
// another language; native UI is drawn again every frame, so the change shows
// at once.
func (a *App) applyLanguage() {
	a.Language = i18n.Normalize(a.Language)
	i18n.Set(i18n.Resolve(a.Language, a.preferredLanguages))
	if a.onLanguage != nil {
		a.onLanguage(a.Language)
	}
}

// setLanguage is the Settings panel's choice. It saves the setting and shows
// the language at once; sessions and agent processes keep running.
func (a *App) setLanguage(value string) {
	value = i18n.Normalize(value)
	if value == a.Language {
		return
	}
	a.Language = value
	a.applyLanguage()
	a.persistSettings()
}

// languageSettings is the Language category: the system preference, English
// or Chinese. Each language is named in itself, as a language menu does.
func (a *App) languageSettings(c *mui.Context) {
	mui.Column(c).Gap(10).Children(func() {
		mui.Text(c, T("Language")).FontSize(15).Bold()
		mutedText(c, T("Choose the language of the app's texts and of the messages MyGo draws itself. System follows the desktop."), 10)
		mui.Row(c).Gap(6).Children(func() {
			a.languageChoice(c, i18n.System, T("System"))
			a.languageChoice(c, i18n.English, "English")
			a.languageChoice(c, i18n.Chinese, "简体中文")
		})
		effective := Tf("Shown now: %s.", languageName(i18n.Language()))
		caption(c, effective)
		mutedText(c, T("Conversations, agent processes and the settings file are untouched; only the texts change."), 10)
	})
}

// languageChoice draws one radio row of the Language category.
func (a *App) languageChoice(c *mui.Context, value, label string) {
	t := c.Theme()
	selected := i18n.Normalize(a.Language) == value
	choice := quietButton(c, label).FontSize(11).Role(mui.RoleRadio).AccessSelected(selected)
	if selected {
		choice.Background(t.Accent).TextColor(t.AccentText)
	} else {
		choice.Border(1, t.Border)
	}
	if choice.Clicked() {
		a.setLanguage(value)
	}
}

// PreferredLanguagesChanged takes the user's languages as MyGo reports them
// changing, and shows the System choice's new language at once. A fixed
// language setting is unaffected, and a change that resolves to the language
// already shown does nothing, since MyGo reports the app's own language
// changes too.
func (a *App) PreferredLanguagesChanged(preferred []string) {
	a.preferredLanguages = append(a.preferredLanguages[:0], preferred...)
	if i18n.Normalize(a.Language) != i18n.System {
		return
	}
	if i18n.Resolve(a.Language, a.preferredLanguages) != i18n.Language() {
		a.applyLanguage()
	}
}

// languageName names a language in itself, for the caption under the choice.
func languageName(tag string) string {
	switch tag {
	case i18n.Chinese:
		return "简体中文"
	case i18n.English:
		return "English"
	}
	return tag
}
