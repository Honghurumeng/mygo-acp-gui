package ui

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestTruncateCutsRunes(t *testing.T) {
	title := "左侧的workspace面板需要在右侧显示更多信息"
	got := truncate(title, 10)
	if !utf8.ValidString(got) {
		t.Fatalf("truncate broke UTF-8: %q", got)
	}
	if !strings.HasSuffix(got, "…") {
		t.Errorf("missing ellipsis: %q", got)
	}
	if n := utf8.RuneCountInString(strings.TrimSuffix(got, "…")); n != 10 {
		t.Errorf("kept %d runes, want 10: %q", n, got)
	}

	if got := truncate("  short  ", 10); got != "short" {
		t.Errorf("short text = %q", got)
	}
	if got := truncate("anything", 0); got != "" {
		t.Errorf("zero budget = %q", got)
	}
}

func TestChatNameKeepsFullTitle(t *testing.T) {
	title := "左侧的workspace面板需要在右侧显示更多信息"
	chat := &Chat{Title: title}
	if got := chat.name(); got != title {
		t.Errorf("name() = %q, want the whole title", got)
	}
	if got := (&Chat{Title: "two\nlines"}).name(); got != "two lines" {
		t.Errorf("name() = %q, want one line", got)
	}
}
