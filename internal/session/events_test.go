package session

import (
	"encoding/json"
	"testing"
)

func TestParseEventPiAcpMeta(t *testing.T) {
	tests := []struct {
		name        string
		meta        string
		startupInfo bool
		notice      *Notice
	}{
		{
			name:        "startup info",
			meta:        `{"piAcp":{"startupInfo":true}}`,
			startupInfo: true,
		},
		{
			name:   "adapter notice",
			meta:   `{"piAcp":{"notice":{"kind":"auto_retry","level":"warning"}}}`,
			notice: &Notice{Kind: "auto_retry", Level: "warning"},
		},
		{
			name:   "extension notify",
			meta:   `{"piAcp":{"notify":{"level":"error"}}}`,
			notice: &Notice{Kind: "extension", Level: "error"},
		},
		{name: "no pi meta"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			update := `{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"status"}`
			if tc.meta != "" {
				update += `,"_meta":` + tc.meta
			}
			update += `}`
			ev := parseEvent(nil, json.RawMessage(`{"sessionId":"s","update":`+update+`}`))
			if ev == nil {
				t.Fatal("parseEvent returned nil")
			}
			if ev.Text != "status" {
				t.Errorf("text = %q", ev.Text)
			}
			if ev.StartupInfo != tc.startupInfo {
				t.Errorf("startupInfo = %v, want %v", ev.StartupInfo, tc.startupInfo)
			}
			if tc.notice == nil {
				if ev.Notice != nil {
					t.Errorf("notice = %+v, want nil", ev.Notice)
				}
				return
			}
			if ev.Notice == nil || *ev.Notice != *tc.notice {
				t.Errorf("notice = %+v, want %+v", ev.Notice, tc.notice)
			}
		})
	}
}

func TestParseEventReplayMeta(t *testing.T) {
	update := `{"sessionUpdate":"user_message_chunk","content":{"type":"text","text":"hi"},` +
		`"_meta":{"piAcp":{"replay":{"key":"replay-3","timestamp":1700000000000}}}}`
	ev := parseEvent(nil, json.RawMessage(`{"sessionId":"s","update":`+update+`}`))
	if ev == nil || ev.Replay == nil {
		t.Fatalf("replay meta not parsed: %+v", ev)
	}
	if ev.Replay.Key != "replay-3" {
		t.Errorf("key = %q", ev.Replay.Key)
	}
	if got, want := ev.Replay.Timestamp.UnixMilli(), int64(1700000000000); got != want {
		t.Errorf("timestamp = %d, want %d", got, want)
	}
}

func TestParseEventKeepsImageContentBlocks(t *testing.T) {
	update := `{"sessionUpdate":"user_message_chunk","content":{"type":"image","mimeType":"image/png","data":"aGVsbG8="}}`
	ev := parseEvent(nil, json.RawMessage(`{"sessionId":"s","update":`+update+`}`))
	if ev == nil {
		t.Fatal("parseEvent returned nil")
	}
	if ev.Text != "" {
		t.Errorf("text = %q", ev.Text)
	}
	if len(ev.Content) != 1 || ev.Content[0].Type != "image" || ev.Content[0].Data != "aGVsbG8=" {
		t.Fatalf("content = %+v", ev.Content)
	}
}

func TestParseEventKeepsTextAndImageOrder(t *testing.T) {
	update := `{"sessionUpdate":"user_message_chunk","content":[` +
		`{"type":"text","text":"look"},` +
		`{"type":"image","mimeType":"image/png","data":"aGVsbG8="},` +
		`{"type":"text","text":" please"}]}`
	ev := parseEvent(nil, json.RawMessage(`{"sessionId":"s","update":`+update+`}`))
	if ev == nil {
		t.Fatal("parseEvent returned nil")
	}
	if ev.Text != "look please" {
		t.Errorf("text = %q", ev.Text)
	}
	if len(ev.Content) != 3 || ev.Content[1].Type != "image" {
		t.Fatalf("content = %+v", ev.Content)
	}
}

func TestParseEventSubagentUpdate(t *testing.T) {
	update := `{"sessionUpdate":"subagent_update","sessionId":"child-1",` +
		`"title":"Researcher","description":"Inspects files","cwd":"/tmp/work",` +
		`"capabilities":{"cancel":{}},"state":{"state":"idle","stopReason":"end_turn"}}`
	ev := parseEvent(nil, json.RawMessage(`{"sessionId":"parent","update":`+update+`}`))
	if ev == nil || ev.Subagent == nil {
		t.Fatalf("subagent update not parsed: %+v", ev)
	}
	sub := ev.Subagent
	if sub.SessionID != "child-1" || sub.Title == nil || *sub.Title != "Researcher" {
		t.Errorf("subagent identity = %+v", sub)
	}
	if sub.Description == nil || *sub.Description != "Inspects files" || !sub.CWDKnown || sub.CWD != "/tmp/work" {
		t.Errorf("subagent metadata = %+v", sub)
	}
	if !sub.StateKnown || sub.State != "idle" || sub.StopReason != "end_turn" {
		t.Errorf("subagent state = %+v", sub)
	}
	if !sub.CancelKnown || !sub.CanCancel {
		t.Errorf("subagent cancel capability = %+v", sub)
	}
}

func TestParseEventLegacySubagentUpdates(t *testing.T) {
	spawned := parseEvent(nil, json.RawMessage(`{"sessionId":"parent","update":`+
		`{"sessionUpdate":"subagent_spawned","subagentSessionId":"child-1","name":"Worker","task":"Run tests"}}`))
	if spawned == nil || spawned.Subagent == nil || spawned.Subagent.SessionID != "child-1" ||
		spawned.Subagent.Title == nil || *spawned.Subagent.Title != "Worker" || spawned.Subagent.State != "running" {
		t.Fatalf("legacy spawn not normalized: %+v", spawned)
	}
	finished := parseEvent(nil, json.RawMessage(`{"sessionId":"parent","update":`+
		`{"sessionUpdate":"subagent_state_update","subagentSessionId":"child-1","state":"completed"}}`))
	if finished == nil || finished.Subagent == nil || !finished.Subagent.StateKnown || finished.Subagent.State != "completed" {
		t.Fatalf("legacy state not normalized: %+v", finished)
	}
}

func TestParseEventSessionMessage(t *testing.T) {
	update := `{"sessionUpdate":"session_message","messageId":"child-message",` +
		`"senderSessionId":"parent","recipientSessionId":"child-1",` +
		`"senderMessageId":"parent-message","content":[{"type":"text","text":"Inspect files"}]}`
	ev := parseEvent(nil, json.RawMessage(`{"sessionId":"child-1","update":`+update+`}`))
	if ev == nil || ev.SessionMessage == nil {
		t.Fatalf("session message not parsed: %+v", ev)
	}
	message := ev.SessionMessage
	if message.MessageID != "child-message" || message.SenderSessionID != "parent" ||
		message.RecipientSessionID != "child-1" || message.SenderMessageID != "parent-message" {
		t.Errorf("session message identity = %+v", message)
	}
	if ev.Text != "Inspect files" || len(ev.Content) != 1 {
		t.Errorf("session message content = %q / %+v", ev.Text, ev.Content)
	}
}
