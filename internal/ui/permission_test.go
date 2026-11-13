package ui

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestToolDescription(t *testing.T) {
	cases := []struct {
		raw  string
		want string
	}{
		{`{"command":"ls","description":"  List files  "}`, "List files"},
		{`{"command":"ls"}`, ""},
		{`{"description":42}`, ""},
		{`null`, ""},
		{``, ""},
		{`not json`, ""},
	}
	for _, tc := range cases {
		if got := toolDescription(json.RawMessage(tc.raw)); got != tc.want {
			t.Errorf("toolDescription(%q) = %q, want %q", tc.raw, got, tc.want)
		}
	}
}

// The tool input is a titled code block whose copy button writes it.
func TestPermissionCodeBlockCopiesInput(t *testing.T) {
	app, tt := newFakeApp(t)
	current(app).Input = "/permission"
	tt.Frame()
	if err := tt.Click("Send"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, app, tt, func() bool { return app.Permission != nil })
	tt.Frame()
	if !tt.HasText("Input") || !strings.Contains(strings.Join(tt.Texts(), "\n"), `"command"`) {
		t.Fatalf("tool input is missing; texts: %q", tt.Texts())
	}
	if err := tt.Click("Copy"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if got := tt.Clipboard(); !strings.Contains(got, `"command"`) {
		t.Errorf("clipboard = %q", got)
	}
}

func TestToolTitleAddsInfo(t *testing.T) {
	cases := []struct {
		title       string
		description string
		want        bool
	}{
		{"Shell: git status", "Show the working tree", true},
		{"Agent: Explore auth", "Explore auth", false},
		{"Agent: Explore the authent...", "Explore the authentication code", false},
		{"Explore auth", "Explore auth", false},
		{"", "Explore auth", false},
	}
	for _, tc := range cases {
		if got := toolTitleAddsInfo(tc.title, tc.description); got != tc.want {
			t.Errorf("toolTitleAddsInfo(%q, %q) = %v, want %v", tc.title, tc.description, got, tc.want)
		}
	}
}
