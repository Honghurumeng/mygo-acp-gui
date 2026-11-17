package ui

import (
	"regexp"
	"strings"

	"github.com/reed/acp-gui/internal/i18n"
)

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

// ansiEscape matches the terminal escapes an agent's output can carry: CSI
// sequences (colors, cursor moves), OSC sequences (titles, hyperlinks) and the
// two-byte escapes.
var ansiEscape = regexp.MustCompile(`\x1b(?:\[[0-?]*[ -/]*[@-~]|\][^\x07\x1b]*(?:\x07|\x1b\\)?|[@-Z\\\-_])`)

// stripANSI removes terminal escapes from text that is drawn, not run.
func stripANSI(text string) string {
	if !strings.ContainsRune(text, 0x1b) {
		return text
	}
	return ansiEscape.ReplaceAllString(text, "")
}

// sanitizeToolText normalizes line endings and drops the control characters
// that would break a text layout. Newlines and tabs stay: they carry meaning in
// command output and code.
func sanitizeToolText(text string) string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	clean := true
	for _, r := range text {
		if r < 0x20 && r != '\n' && r != '\t' {
			clean = false
			break
		}
		if r == 0x7f {
			clean = false
			break
		}
	}
	if clean {
		return text
	}
	var b strings.Builder
	b.Grow(len(text))
	for _, r := range text {
		if r == 0x7f || (r < 0x20 && r != '\n' && r != '\t') {
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}
