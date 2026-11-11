package ui

import (
	"testing"

	"github.com/reed/acp-gui/internal/acp"
	"github.com/reed/acp-gui/internal/session"
)

func TestSavedSessionsGroupedByWorkspace(t *testing.T) {
	app := &App{
		Workspace:  "/work/b",
		Workspaces: []string{"/work/a", "/work/b"},
		SavedSessions: []savedSession{
			{SessionSummary: acp.SessionSummary{SessionID: "a1", Cwd: "/work/a", Title: "A one"}, workspace: "/work/a"},
			{SessionSummary: acp.SessionSummary{SessionID: "b1", Cwd: "/work/b", Title: "B one"}, workspace: "/work/b"},
			{SessionSummary: acp.SessionSummary{SessionID: "a2", Cwd: "/work/a", Title: "A two"}, workspace: "/work/a"},
		},
	}
	app.Chats = []*Chat{{
		Workspace: "/work/a",
		Info:      session.Info{SessionID: "a1"},
	}}

	aRows := app.savedSessionsFor("/work/a")
	if len(aRows) != 1 || aRows[0].SessionID != "a2" {
		t.Fatalf("workspace a rows = %+v", aRows)
	}
	bRows := app.savedSessionsFor("/work/b")
	if len(bRows) != 1 || bRows[0].SessionID != "b1" {
		t.Fatalf("workspace b rows = %+v", bRows)
	}
}

func TestSavedSessionWithoutCwdUsesCurrentWorkspace(t *testing.T) {
	app := &App{
		Workspace: "/work/a",
		SavedSessions: []savedSession{
			{SessionSummary: acp.SessionSummary{SessionID: "a1", Title: "A one"}},
			{SessionSummary: acp.SessionSummary{SessionID: "b1", Cwd: "/work/b", Title: "B one"}, workspace: "/work/b"},
		},
	}
	rows := app.savedSessionsFor("/work/a")
	if len(rows) != 1 || rows[0].SessionID != "a1" {
		t.Fatalf("rows = %+v", rows)
	}
}
