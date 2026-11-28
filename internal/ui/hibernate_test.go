package ui

import (
	"testing"
	"time"
)

func countText(texts []string, want string) int {
	count := 0
	for _, text := range texts {
		if text == want {
			count++
		}
	}
	return count
}

func TestIdleChatSleepsKeepsTranscriptAndWakesOnSend(t *testing.T) {
	app, tt := newFakeApp(t)
	chat := connectTab(t, app, tt)
	sendTo(t, app, tt, chat, "hello")
	waitFor(t, app, tt, func() bool {
		return !chat.Streaming && chat.Busy == "" && chatAssistantHas(chat, "收到：hello")
	})
	sessionID := chat.Info.SessionID
	messageCount := len(chat.Messages)
	tt.Frame()
	clocks := countText(tt.Texts(), "◷")

	chat.lastActiveAt = time.Now().Add(-idleHibernateAfter - time.Second)
	app.hibernateIdleChats(time.Now())
	waitFor(t, app, tt, func() bool { return chat.Session == nil && !chat.Connected })

	if !chat.sleeping() {
		t.Fatal("chat is not sleeping after hibernation")
	}
	if chat.Info.SessionID != sessionID {
		t.Fatalf("sleeping chat lost its session id: %q, want %q", chat.Info.SessionID, sessionID)
	}
	if len(chat.Messages) != messageCount || !chatAssistantHas(chat, "收到：hello") {
		t.Fatalf("sleeping chat lost its transcript: %+v", chat.Messages)
	}
	if got := countText(tt.Texts(), "◷"); got != clocks+1 {
		t.Fatalf("sleeping conversation has no clock marker: %d, want %d; texts: %q", got, clocks+1, tt.Texts())
	}

	chat.Input = "wake up"
	app.setConfigOption("model", "demo/large")
	if chat.PendingConfig["model"] != "demo/large" {
		t.Fatalf("sleeping chat did not keep the model pick: %+v", chat.PendingConfig)
	}
	tt.Frame()
	if err := tt.Click("Send"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, app, tt, func() bool {
		return chat.Session != nil && chat.Connected && !chat.Streaming && chat.Busy == "" && !chat.replaying &&
			chatAssistantHas(chat, "收到：wake up") && hasRoleText(app, RoleUser, "Question from history")
	})
	if chat.Info.SessionID != sessionID {
		t.Fatalf("wake created a new session: %q, want %q", chat.Info.SessionID, sessionID)
	}
	if !lastAssistantHas(app, "当前模型 `demo/large`") {
		t.Fatalf("wake did not apply the model pick; messages: %+v", chat.Messages)
	}
	if chat.sleeping() {
		t.Fatal("chat still reports sleeping after the wake prompt")
	}
}

func TestIdleHibernateSkipsWorkInProgress(t *testing.T) {
	app, tt := newFakeApp(t)
	chat := connectTab(t, app, tt)
	chat.lastActiveAt = time.Now().Add(-idleHibernateAfter - time.Minute)

	chat.Streaming = true
	app.hibernateIdleChats(time.Now())
	if chat.Session == nil {
		t.Fatal("a streaming chat was hibernated")
	}
	chat.Streaming = false

	chat.Attention = true
	app.hibernateIdleChats(time.Now())
	if chat.Session == nil {
		t.Fatal("a chat waiting for permission was hibernated")
	}
	chat.Attention = false

	chat.Busy = "config"
	app.hibernateIdleChats(time.Now())
	if chat.Session == nil {
		t.Fatal("a busy chat was hibernated")
	}
	chat.Busy = ""

	app.hibernateIdleChats(time.Now())
	waitFor(t, app, tt, func() bool { return chat.Session == nil && !chat.Connected })
}
