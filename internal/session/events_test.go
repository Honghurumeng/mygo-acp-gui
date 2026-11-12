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
