package ui

import (
	"strings"
	"testing"
)

func recordNotifications(app *App) *[]desktopNotification {
	notes := new([]desktopNotification)
	app.notify = func(n desktopNotification) {
		*notes = append(*notes, n)
	}
	return notes
}

func TestPermissionRequestSendsDesktopNotification(t *testing.T) {
	app, tt := newFakeApp(t)
	notes := recordNotifications(app)

	current(app).Input = "/permission"
	tt.Frame()
	if err := tt.Click("Send"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, app, tt, func() bool { return app.Permission != nil })

	if len(*notes) != 1 {
		t.Fatalf("notifications = %+v, want one permission request", *notes)
	}
	note := (*notes)[0]
	if note.Title != "Permission needed" {
		t.Errorf("title = %q", note.Title)
	}
	if note.Subtitle != current(app).name() {
		t.Errorf("subtitle = %q, want %q", note.Subtitle, current(app).name())
	}
	if !strings.Contains(note.Body, "Echo a fake tool command") {
		t.Errorf("body = %q, missing the tool description", note.Body)
	}
	if !strings.HasPrefix(note.ID, "acp-gui:permission:") {
		t.Errorf("id = %q", note.ID)
	}
}

func TestFinishedTurnSendsDesktopNotification(t *testing.T) {
	app, tt := newFakeApp(t)
	notes := recordNotifications(app)
	connect(t, app, tt)

	current(app).Input = "hello"
	tt.Frame()
	if err := tt.Click("Send"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, app, tt, func() bool {
		return !current(app).Streaming && lastAssistantHas(app, "收到：hello")
	})

	var complete *desktopNotification
	for i := range *notes {
		if (*notes)[i].Title == "Task complete" {
			complete = &(*notes)[i]
			break
		}
	}
	if complete == nil {
		t.Fatalf("notifications = %+v, want a completed turn", *notes)
	}
	if !strings.Contains(complete.Body, "收到：hello") {
		t.Errorf("body = %q, missing the answer", complete.Body)
	}
	if !strings.HasPrefix(complete.ID, "acp-gui:turn:") {
		t.Errorf("id = %q", complete.ID)
	}
}

func TestNotificationsCanBeDisabled(t *testing.T) {
	app, tt := newFakeApp(t)
	notes := recordNotifications(app)
	app.NotificationsEnabled = false
	connect(t, app, tt)

	current(app).Input = "hello"
	tt.Frame()
	if err := tt.Click("Send"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, app, tt, func() bool {
		return !current(app).Streaming && lastAssistantHas(app, "收到：hello")
	})

	if len(*notes) != 0 {
		t.Errorf("disabled notifications fired: %+v", *notes)
	}
}

// Cancelling is the user's own action, so it needs no desktop notification.
func TestCancelledTurnStaysSilent(t *testing.T) {
	app, tt := newFakeApp(t)
	notes := recordNotifications(app)
	connect(t, app, tt)

	current(app).Input = "/slow cancel me"
	tt.Frame()
	if err := tt.Click("Send"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, app, tt, func() bool { return current(app).Streaming })
	if err := tt.Click("Stop"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, app, tt, func() bool {
		return !current(app).Streaming && current(app).LastStop == "cancelled"
	})

	if len(*notes) != 0 {
		t.Errorf("cancelled turn notified: %+v", *notes)
	}
}
