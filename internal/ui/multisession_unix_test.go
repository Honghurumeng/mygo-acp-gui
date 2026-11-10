//go:build unix

package ui

import "testing"

func TestShutdownClosesEveryChat(t *testing.T) {
	app, tt := newFakeApp(t)
	first := connectTab(t, app, tt)
	second := newTab(t, app, tt)
	second = connectTab(t, app, tt)

	firstPID, secondPID := first.Session.PID(), second.Session.PID()
	app.Shutdown()
	waitGone(t, firstPID)
	waitGone(t, secondPID)
}
