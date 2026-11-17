package ui

import (
	"strings"
	"testing"
)

func TestPermissionRejectSettlesRequest(t *testing.T) {
	app, tt := newFakeApp(t)
	current(app).Input = "/permission"
	tt.Frame()
	if err := tt.Click("Send"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, app, tt, func() bool { return app.Permission != nil && app.PermissionOpen })
	tt.Frame()
	if err := tt.Click("Reject"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, app, tt, func() bool {
		return app.Permission == nil && !current(app).Streaming && lastAssistantHas(app, "permission denied")
	})
	if current(app).LastStop != "end_turn" {
		t.Errorf("stop reason = %q, want end_turn", current(app).LastStop)
	}
}

func TestPermissionAllowAlwaysPassesSelectedOption(t *testing.T) {
	app, tt := newFakeApp(t)
	current(app).Input = "/permission"
	tt.Frame()
	if err := tt.Click("Send"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, app, tt, func() bool { return app.Permission != nil && app.PermissionOpen })
	tt.Frame()
	if err := tt.Click("Allow always"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, app, tt, func() bool {
		return app.Permission == nil && !current(app).Streaming && lastAssistantHas(app, "permission granted always")
	})
}

func TestAgentUnexpectedExitShowsError(t *testing.T) {
	app, tt := newFakeApp(t)
	connect(t, app, tt)
	chat := current(app)
	chat.Input = "/exit"
	tt.Frame()
	if err := tt.Click("Send"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, app, tt, func() bool {
		return !chat.Streaming && chat.Session == nil && !chat.Connected &&
			hasRoleText(app, RoleError, "Agent exited unexpectedly")
	})
	if !tt.HasText("Agent exited unexpectedly") {
		t.Errorf("exit error is not rendered; texts: %q", tt.Texts())
	}
	if chat.LastStop != "" && chat.LastStop != "end_turn" {
		t.Errorf("unexpected stop reason = %q", chat.LastStop)
	}
	var exitMessage string
	for _, message := range chat.Messages {
		if message.Role == RoleError && strings.Contains(message.Text, "Agent exited unexpectedly") {
			exitMessage = message.Text
			break
		}
	}
	if !strings.Contains(exitMessage, "exit status 7") {
		t.Errorf("exit error = %q, want the process status", exitMessage)
	}
	if chat.Attention {
		t.Error("crashed chat was not marked unread/attention")
	}
}

func TestAgentExitCancelsPendingPermission(t *testing.T) {
	app, tt := newFakeApp(t)
	connect(t, app, tt)
	current(app).Input = "/permission-exit"
	tt.Frame()
	if err := tt.Click("Send"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, app, tt, func() bool { return app.Permission != nil && app.PermissionOpen })
	chat := current(app)
	waitFor(t, app, tt, func() bool {
		return app.Permission == nil && !chat.Streaming && chat.Session == nil &&
			hasRoleText(app, RoleError, "Agent exited unexpectedly")
	})
	if chat.Attention {
		t.Error("crashed permission chat was not marked unread/attention")
	}
}
