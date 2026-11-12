package ui

import (
	"strings"
)

// imageSessionTitle replaces attachment-only titles. The agent fallback and
// this package must agree so the same session does not flip between two
// placeholders after a sync.
const imageSessionTitle = "[Image]"

// cleanSessionTitle removes attachment paths from an agent-provided title.
// Some agents build the title from the first user message; when the prompt
// starts with a pasted image path, the stored title is just
// "/var/folders/.../pi-clipboard-....png". Keep the text after the path, and
// use [Image] when only paths remain.
func cleanSessionTitle(raw string) string {
	text := singleLine(raw)
	if text == "" {
		return ""
	}
	cleaned, hadPaths := stripLeadingAttachmentPaths(text)
	if cleaned != "" {
		return cleaned
	}
	if hadPaths {
		return imageSessionTitle
	}
	return text
}

// stripLeadingAttachmentPaths removes whitespace-delimited absolute paths
// from the start of text. It requires at least two slashes and rejects URLs so
// a slash command such as "/name" or "https://..." stays intact.
func stripLeadingAttachmentPaths(text string) (string, bool) {
	text = strings.TrimSpace(text)
	hadPaths := false
	for text != "" {
		token := text
		if index := strings.IndexByte(text, ' '); index >= 0 {
			token = text[:index]
		}
		if !looksLikeAttachmentPath(token) {
			return text, hadPaths
		}
		hadPaths = true
		text = strings.TrimSpace(strings.TrimPrefix(text, token))
	}
	return "", hadPaths
}

func looksLikeAttachmentPath(token string) bool {
	if token == "" || strings.Contains(token, "://") {
		return false
	}
	if !strings.HasPrefix(token, "/") && !strings.HasPrefix(token, "~/") {
		return false
	}
	return strings.Count(token, "/") >= 2
}
