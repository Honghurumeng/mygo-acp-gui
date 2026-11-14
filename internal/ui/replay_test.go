package ui

import (
	"testing"
	"time"

	"github.com/reed/acp-gui/internal/agent"
	"github.com/reed/acp-gui/internal/archive"
	"github.com/reed/acp-gui/internal/session"
)

// replayTestApp builds the minimum UI state for driving session/load updates
// without an agent process.
func replayTestApp(t *testing.T) (*App, *Chat, *session.Session) {
	t.Helper()
	s := &session.Session{}
	chat := &Chat{
		ID:        1,
		Agent:     agent.LaunchSpec{ID: "fake"},
		Session:   s,
		Connected: true,
		Info:      session.Info{SessionID: "s"},
		replaying: true,
	}
	app := &App{Active: 0, Chats: []*Chat{chat}}
	return app, chat, s
}

func replayEvent(s *session.Session, kind, text, key string, at time.Time) session.Event {
	ev := session.Event{
		Session:   s,
		SessionID: "s",
		Kind:      kind,
		Text:      text,
	}
	if key != "" {
		ev.Replay = &session.Replay{Key: key}
		if !at.IsZero() {
			ev.Replay.Timestamp = at
		}
	}
	return ev
}

// Two user prompts separated by a failed assistant turn must stay two rows.
// The failed turn produces no assistant content, so without the replay key
// they used to merge and shift every later timestamp/runtime by one turn.
func TestReplayKeepsFailedTurnBoundariesAndTimes(t *testing.T) {
	app, chat, s := replayTestApp(t)
	firstAt := time.Date(2026, 11, 12, 9, 0, 0, 0, time.UTC)
	secondAt := firstAt.Add(2 * time.Hour)
	app.Archived = []archive.Session{{
		AgentID:   "fake",
		SessionID: "s",
		UserTurns: []archive.UserTurn{
			{Number: 1, SentAt: firstAt.Format(time.RFC3339Nano), DurationMs: 10_000},
			{Number: 2, SentAt: secondAt.Format(time.RFC3339Nano), DurationMs: 20_000},
		},
	}}

	app.applyUpdate(chat, replayEvent(s, session.UpdateUserChunk, "first", "r1", firstAt))
	failed := replayEvent(s, session.UpdateMessageChunk, "Turn failed: boom", "r2", time.Time{})
	failed.Notice = &session.Notice{Kind: "turn_error", Level: "error"}
	app.applyUpdate(chat, failed)
	app.applyUpdate(chat, replayEvent(s, session.UpdateUserChunk, "second", "r3", secondAt))
	app.applyUpdate(chat, replayEvent(s, session.UpdateMessageChunk, "answer", "r4", time.Time{}))
	app.finishReplay(chat)

	var users []*Message
	var notices []*Message
	var turns []*Message
	for _, message := range chat.Messages {
		switch message.Role {
		case RoleUser:
			users = append(users, message)
		case RoleSystem:
			notices = append(notices, message)
		case RoleTurn:
			turns = append(turns, message)
		}
	}
	if len(users) != 2 {
		t.Fatalf("user rows = %d, want 2: %+v", len(users), chat.Messages)
	}
	if users[0].Text != "first" || users[1].Text != "second" {
		t.Errorf("user texts = %q, %q", users[0].Text, users[1].Text)
	}
	if !users[0].SentAt.Equal(firstAt) || !users[1].SentAt.Equal(secondAt) {
		t.Errorf("sent times = %v, %v; want %v, %v", users[0].SentAt, users[1].SentAt, firstAt, secondAt)
	}
	if len(notices) != 1 || notices[0].Text != "Turn failed: boom" {
		t.Errorf("failed-turn notice = %+v", notices)
	}
	if len(turns) != 2 {
		t.Fatalf("runtime rows = %d, want 2: %+v", len(turns), turns)
	}
	if turns[0].Elapsed != 10*time.Second || turns[1].Elapsed != 20*time.Second {
		t.Errorf("runtimes = %v, %v; want 10s, 20s", turns[0].Elapsed, turns[1].Elapsed)
	}
	// The first runtime belongs before the second prompt, not at the end.
	secondIndex, firstTurnIndex := -1, -1
	for i, message := range chat.Messages {
		if message == users[1] {
			secondIndex = i
		}
		if message == turns[0] {
			firstTurnIndex = i
		}
	}
	if firstTurnIndex < 0 || secondIndex < 0 || firstTurnIndex > secondIndex {
		t.Errorf("first Took row index = %d, second prompt index = %d", firstTurnIndex, secondIndex)
	}
}

// An empty persisted user message still has to count as one turn, otherwise
// archive times and runtimes shift for every later prompt.
func TestReplayEmptyUserMessageKeepsTurnCount(t *testing.T) {
	app, chat, s := replayTestApp(t)
	at := time.Date(2026, 11, 12, 9, 0, 0, 0, time.UTC)

	app.applyUpdate(chat, replayEvent(s, session.UpdateUserChunk, "", "u1", at))
	app.applyUpdate(chat, replayEvent(s, session.UpdateMessageChunk, "answer", "a1", time.Time{}))
	app.applyUpdate(chat, replayEvent(s, session.UpdateUserChunk, "second", "u2", at.Add(time.Minute)))
	app.finishReplay(chat)

	count := 0
	for _, message := range chat.Messages {
		if message.Role == RoleUser {
			count++
		}
	}
	if count != 2 {
		t.Fatalf("user rows = %d, want 2: %+v", count, chat.Messages)
	}
}
