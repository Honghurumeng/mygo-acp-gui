package ui

import (
	"encoding/json"
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
