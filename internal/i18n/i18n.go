// Package i18n says the app's own messages in the language it shows.
//
// The app is written in English: every message is its own key, so a message
// without a translation keeps the English text instead of showing a raw key.
// The translations live in zh-Hans.json, embedded into the binary, and are
// read once. MyGo's own strings, the native menus and dialogs among them, are
// a separate catalog that MyGo links (see locales/zh-Hans in the MyGo module)
// and that follows the same language through mygo.App.SetLocale.
//
// The language is the one the user picked in Settings, or, for System, the
// best of the app's languages by the user's preferred languages. Native UI is
// drawn again every frame, so Set shows a different language at once; text
// the app stored as a string when it was produced (a notice, a warning) keeps
// the language it was produced in until it is replaced.
package i18n

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"

	mygoi18n "github.com/egoist/mygo/i18n"
)

// The languages the app is written in, its own (English) first.
const (
	English = "en"
	Chinese = "zh-Hans"
	// System makes the app follow the user's preferred languages instead of
	// a language the user picked.
	System = "system"
)

// Languages lists the languages the app is written in, the source first:
// what mygo.App.SetLanguages and the Settings panel work from.
var Languages = []string{English, Chinese}

//go:embed zh-Hans.json
var zhHansJSON []byte

// translations is the Chinese catalog, read on first use. It maps a message
// in the app's own language to its Chinese text; a message it lacks is shown
// in English.
var (
	once         sync.Once
	translations map[string]string

	current atomic.Pointer[string]
)

func catalog() map[string]string {
	once.Do(func() {
		translations = make(map[string]string, 512)
		if err := json.Unmarshal(zhHansJSON, &translations); err != nil {
			// A broken catalog must not take the app down: English remains.
			translations = map[string]string{}
		}
	})
	return translations
}

// Resolve returns the language to show for a stored setting: System (or an
// empty value) picks the first of the user's preferred languages that the app
// is written in, else English; any other value is normalized, and a language
// the app is not written in falls back to English.
func Resolve(setting string, preferred []string) string {
	setting = strings.TrimSpace(setting)
	if setting == "" || setting == System {
		// The first language the user reads that the app is written in wins:
		// any Chinese, Traditional included, gets the Simplified catalog,
		// since that is the only Chinese the app ships.
		for _, want := range preferred {
			switch tag := mygoi18n.Normalize(want); {
			case strings.HasPrefix(tag, "zh"):
				return Chinese
			case tag == English || strings.HasPrefix(tag, English+"-"):
				return English
			}
		}
		return English
	}
	switch tag := mygoi18n.Normalize(setting); {
	case strings.HasPrefix(tag, "zh"):
		return Chinese
	case tag == English || strings.HasPrefix(tag, English+"-"):
		return English
	}
	return English
}

// Set makes T and Tf say messages in language from now on, which Resolve
// returns for the setting the app saved. It is safe to call from the main
// thread and read from any goroutine.
func Set(language string) {
	language = Resolve(language, nil)
	current.Store(&language)
}

// Language returns the language T and Tf say messages in, English until Set
// is called.
func Language() string {
	if language := current.Load(); language != nil {
		return *language
	}
	return English
}

// T says message in the language the app shows. A message without a
// translation is returned unchanged.
func T(message string) string {
	if message == "" || Language() == English {
		return message
	}
	if text, ok := catalog()[message]; ok && text != "" {
		return text
	}
	return message
}

// Tf says a message with fmt's verbs, translated before it is formatted, so
// the verbs are those of the message's language. The translation keeps the
// passed arguments in the same order.
func Tf(format string, args ...any) string {
	if len(args) == 0 {
		return T(format)
	}
	return fmt.Sprintf(T(format), args...)
}

// S picks one of two texts written inline: zh when the app shows Chinese,
// en otherwise. It suits a string that is not worth a catalog entry, such as
// a symbol spelled out differently or a single word given both ways.
func S(zh, en string) string {
	if Language() == Chinese {
		return zh
	}
	return en
}

// Normalize maps a stored setting to what the Settings panel shows: System,
// English or Chinese. Unknown values follow the system, so an edited or old
// settings file keeps working.
func Normalize(setting string) string {
	setting = strings.TrimSpace(setting)
	if setting == "" || setting == System {
		return System
	}
	if tag := mygoi18n.Normalize(setting); tag != "" {
		switch {
		case tag == English || strings.HasPrefix(tag, English+"-"):
			return English
		case strings.HasPrefix(tag, "zh"):
			return Chinese
		}
	}
	return System
}
