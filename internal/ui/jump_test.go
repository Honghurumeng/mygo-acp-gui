package ui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	mui "github.com/egoist/mygo/ui"
	"github.com/reed/acp-gui/internal/agent"
)

func TestUserHistoryFiltersAndOrdersOldestFirst(t *testing.T) {
	chat := &Chat{Messages: []*Message{
		{ID: 1, Role: RoleSystem, Text: "agent notice"},
		{ID: 2, Role: RoleUser, Text: "first question", Attachments: []Attachment{{Name: "a.png"}}},
		{ID: 3, Role: RoleAssistant, Text: "first answer"},
		{ID: 4, Role: RoleTool, Text: "tool output"},
		{ID: 5, Role: RoleUser, Text: "second\nquestion"},
		{ID: 6, Role: RoleError, Text: "boom"},
	}}
	entries := chat.userHistory()
	if len(entries) != 2 {
		t.Fatalf("entries = %d, want 2", len(entries))
	}
	// Oldest first, numbered by the user's own turn.
	if entries[0].number != 1 || entries[0].row != 1 {
		t.Errorf("oldest = %+v, want number 1 on row 1", entries[0])
	}
	if got := jumpEntryText(entries[0].msg); !strings.Contains(got, "1 image") ||
		!strings.HasPrefix(got, "first question") {
		t.Errorf("image preview = %q", got)
	}
	if entries[1].number != 2 || entries[1].row != 4 {
		t.Errorf("newest = %+v, want number 2 on row 4", entries[1])
	}
	if got := jumpEntryText(entries[1].msg); got != "second question" {
		t.Errorf("newest preview = %q", got)
	}
	if got := chat.userMessageCount(); got != 2 {
		t.Errorf("count = %d, want 2", got)
	}
}

func TestJumpEntryPreviewStaysOnRuneBoundary(t *testing.T) {
	got := jumpEntryText(&Message{Text: strings.Repeat("你好", 100)})
	if !strings.HasSuffix(got, "…") || !strings.HasPrefix(got, "你好") {
		t.Fatalf("preview = %q", got)
	}
	if runes := []rune(strings.TrimSuffix(got, "…")); len(runes) != 180 {
		t.Errorf("preview has %d runes, want 180", len(runes))
	}
}

func TestJumpTimeLabel(t *testing.T) {
	now := time.Date(2026, 5, 20, 12, 0, 0, 0, time.Local)
	cases := []struct {
		name   string
		sentAt time.Time
		want   string
	}{
		{"today", time.Date(2026, 5, 20, 9, 41, 0, 0, time.Local), "09:41"},
		{"same year", time.Date(2026, 1, 2, 9, 41, 0, 0, time.Local), "Jan 2 09:41"},
		{"older year", time.Date(2020, 11, 11, 9, 41, 0, 0, time.Local), "Nov 11, 2020 09:41"},
	}
	for _, tc := range cases {
		if got := formatJumpTime(tc.sentAt, now); got != tc.want {
			t.Errorf("%s: label = %q, want %q", tc.name, got, tc.want)
		}
	}
	if got := jumpEntryTime(&Message{}); got != "—" {
		t.Errorf("unknown time = %q, want a dash", got)
	}
}

func TestJumpListMaxHeight(t *testing.T) {
	cases := []struct {
		window, want float32
	}{
		{760, 380},
		{580, 380},
		{420, 220},
		{300, 100},
		{200, 80},
	}
	for _, tc := range cases {
		if got := jumpListMaxHeight(tc.window); got != tc.want {
			t.Errorf("window %v: max height = %v, want %v", tc.window, got, tc.want)
		}
	}
}

func TestJumpDialogOpensScrolledToNewest(t *testing.T) {
	app := New(Options{
		Version:   "test",
		Workspace: t.TempDir(),
		Agents:    []agent.LaunchSpec{{ID: "fake", Name: "fake-agent"}},
	})
	t.Cleanup(func() {
		app.Shutdown()
		app.Flush()
	})
	chat := current(app)
	for i := 0; i < 40; i++ {
		app.addMessage(chat, &Message{ID: app.nextID(), Role: RoleUser, Done: true,
			Text: fmt.Sprintf("question %d", i)})
	}
	// A preview long enough to be cut makes the dialog's label distinct
	// from the full text in the transcript behind it.
	chat.Messages[len(chat.Messages)-1].Text = "newest " + strings.Repeat("n", 200)
	sentAt := time.Date(2020, 11, 11, 9, 41, 0, 0, time.Local)
	chat.Messages[len(chat.Messages)-1].SentAt = sentAt
	tt := mui.NewTester(app.View, 1060, 760)
	tt.Frame()
	if err := tt.Click("Jump"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if want := formatJumpTime(sentAt, time.Now()); !tt.HasText(want) {
		t.Errorf("send time %q missing from jump dialog; texts: %q", want, tt.Texts())
	}
	if chat.jumpScroll.MaxY <= 0 {
		t.Fatalf("history does not overflow its scroll area: MaxY=%v", chat.jumpScroll.MaxY)
	}
	if chat.jumpScroll.Y < chat.jumpScroll.MaxY-1 {
		t.Errorf("dialog opens at y=%v, want the end %v", chat.jumpScroll.Y, chat.jumpScroll.MaxY)
	}
	entries := chat.userHistory()
	newest, ok := tt.Find(jumpEntryText(entries[len(entries)-1].msg))
	if !ok {
		t.Fatalf("newest prompt missing from history; texts: %q", tt.Texts())
	}
	if newest.Y < 0 || newest.Y > 760 {
		t.Errorf("newest prompt is outside the window at y=%v", newest.Y)
	}
}

func TestJumpButtonOpensHistoryAndScrollsToMessage(t *testing.T) {
	if testing.Short() {
		t.Skip("builds a long conversation")
	}
	app := New(Options{
		Version:   "test",
		Workspace: t.TempDir(),
		Agents:    []agent.LaunchSpec{{ID: "fake", Name: "fake-agent"}},
	})
	t.Cleanup(func() {
		app.Shutdown()
		app.Flush()
	})
	chat := current(app)
	for i := 0; i < 12; i++ {
		app.addMessage(chat, &Message{ID: app.nextID(), Role: RoleAssistant, Done: true,
			Text: fmt.Sprintf("answer %d", i)})
		text := fmt.Sprintf("question %d", i)
		if i == 0 || i == 11 {
			// Long prompts are cut in the dialog but not in the transcript,
			// so their previews identify the dialog's own labels.
			text += " " + strings.Repeat(string(rune('a'+i)), 200)
		}
		app.addMessage(chat, &Message{ID: app.nextID(), Role: RoleUser, Done: true, Text: text})
	}
	tt := mui.NewTester(app.View, 1060, 760)
	tt.Frame()

	// The attach button comes first; Jump is the second control at the left.
	attach, ok := tt.Find("Attach images")
	if !ok {
		t.Fatal("attach button missing")
	}
	jumpButton, ok := tt.Find("Jump to your message")
	if !ok {
		t.Fatal("Jump button missing")
	}
	if attach.X >= jumpButton.X {
		t.Errorf("Jump is at x=%v, want right of attach at x=%v", jumpButton.X, attach.X)
	}

	// The chat starts at its end, so the first prompt is out of view.
	if first, _ := chat.list.Visible(); first == 0 {
		t.Fatalf("list starts with the first row; expected the end")
	}

	if err := tt.Click("Jump"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !chat.JumpOpen {
		t.Fatal("jump dialog did not open")
	}
	if !tt.HasText("Jump to a message") {
		t.Fatalf("jump dialog missing; texts: %q", tt.Texts())
	}
	entries := chat.userHistory()
	oldestEntry := entries[0]
	newestEntry := entries[len(entries)-1]
	oldest, ok := tt.Find(jumpEntryText(oldestEntry.msg))
	if !ok {
		t.Fatalf("oldest prompt missing from history; texts: %q", tt.Texts())
	}
	newest, ok := tt.Find(jumpEntryText(newestEntry.msg))
	if !ok {
		t.Fatalf("newest prompt missing from history; texts: %q", tt.Texts())
	}
	if oldest.Y >= newest.Y {
		t.Errorf("history is not oldest first: oldest y=%v, newest y=%v", oldest.Y, newest.Y)
	}

	if err := tt.Click("Jump to message 1"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if chat.JumpOpen {
		t.Error("jump dialog stayed open")
	}
	first, last := chat.list.Visible()
	if first != oldestEntry.row {
		t.Errorf("first visible row = %d, want the jumped-to row %d (last %d)", first, oldestEntry.row, last)
	}
}
