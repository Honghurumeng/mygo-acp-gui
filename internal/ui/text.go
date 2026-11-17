package ui

import "github.com/reed/acp-gui/internal/i18n"

// T says one of the app's own messages in the language the app shows. The
// English text is the key, so a message without a translation is shown as
// written instead of as a missing key.
func T(message string) string { return i18n.T(message) }

// Tf says a message with fmt's verbs, translated before it is formatted.
func Tf(format string, args ...any) string { return i18n.Tf(format, args...) }

// TS picks the Chinese or the English text of a string that is written inline
// rather than in the catalog: a unit, a punctuation mark, a word whose two
// spellings differ in no other place.
func TS(zh, en string) string { return i18n.S(zh, en) }
