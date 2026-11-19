package ui

import (
	"encoding/json"
	"testing"

	"github.com/reed/acp-gui/internal/acp"
	"github.com/reed/acp-gui/internal/session"
)

func TestParseAskQuestion(t *testing.T) {
	req := session.PermissionRequest{
		ToolCall: session.ToolCall{
			Title:    "Approach: Patch the CLI now?",
			Kind:     "other",
			RawInput: json.RawMessage(`{"question":"Patch the CLI now?","options":["Retry with the patch","Update the doc"]}`),
		},
		Options: []acp.PermissionOption{
			{OptionID: "option_0", Name: "Retry with the patch: Apply the local patch.", Kind: "allow_once"},
			{OptionID: "option_1", Name: "Update the doc", Kind: "allow_once"},
		},
	}
	question, ok := parseAskQuestion(req)
	if !ok {
		t.Fatal("question request was not recognized")
	}
	if question.Header != "Approach" || question.Question != "Patch the CLI now?" {
		t.Fatalf("question = %#v", question)
	}
	if len(question.Options) != 2 {
		t.Fatalf("options = %#v", question.Options)
	}
	first := question.Options[0]
	if first.Label != "Retry with the patch" || first.Description != "Apply the local patch." {
		t.Errorf("first option = %#v", first)
	}
	second := question.Options[1]
	if second.Label != "Update the doc" || second.Description != "" {
		t.Errorf("second option = %#v", second)
	}
}

func TestParseAskQuestionRejectsOtherRequests(t *testing.T) {
	base := session.PermissionRequest{
		ToolCall: session.ToolCall{
			Title:    "Run a command",
			Kind:     "execute",
			RawInput: json.RawMessage(`{"question":"Patch now?","options":["Yes"]}`),
		},
		Options: []acp.PermissionOption{{OptionID: "option_0", Name: "Yes", Kind: "allow_once"}},
	}
	if _, ok := parseAskQuestion(base); ok {
		t.Error("a command permission was treated as a question")
	}
	base.ToolCall.Kind = "other"
	base.Options[0].OptionID = "allow-once"
	if _, ok := parseAskQuestion(base); ok {
		t.Error("an option outside option_<i> was treated as a question")
	}
	base.Options[0].OptionID = "option_1"
	if _, ok := parseAskQuestion(base); ok {
		t.Error("an out-of-range option index was treated as a question")
	}
	base.Options[0].OptionID = "option_0"
	base.ToolCall.RawInput = json.RawMessage(`{"options":["Yes"]}`)
	if _, ok := parseAskQuestion(base); ok {
		t.Error("a request without a question was treated as a question")
	}
}

func TestQuestionPanelAnswersWithOption(t *testing.T) {
	app, tt := newFakeApp(t)
	connect(t, app, tt)
	chat := current(app)
	chat.Input = "/question"
	tt.Frame()
	if err := tt.Click("Send"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, app, tt, func() bool { return app.Permission != nil && app.PermissionOpen })
	tt.Frame()

	for _, want := range []string{
		"Agent question",
		"Approach",
		"Patch the CLI now?",
		"Retry with the local patch",
		"Patch the globally installed CLI and restart the app to verify the change, then keep the patch in sync with every Command Code upgrade.",
		"Skip",
	} {
		if !tt.HasText(want) {
			t.Errorf("question panel is missing %q; texts: %q", want, tt.Texts())
		}
	}
	if tt.HasText("Permission required") || tt.HasText("\"options\"") {
		t.Errorf("question still uses the permission dialog; texts: %q", tt.Texts())
	}

	if err := tt.Click("Retry with the local patch"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, app, tt, func() bool {
		return app.Permission == nil && !chat.Streaming && lastAssistantHas(app, "answer: Retry with the local patch")
	})
}

func TestQuestionPanelSkipDeclines(t *testing.T) {
	app, tt := newFakeApp(t)
	connect(t, app, tt)
	chat := current(app)
	chat.Input = "/question"
	tt.Frame()
	if err := tt.Click("Send"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, app, tt, func() bool { return app.Permission != nil && app.PermissionOpen })
	tt.Frame()
	if err := tt.Click("Skip"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, app, tt, func() bool {
		return app.Permission == nil && !chat.Streaming && lastAssistantHas(app, "Question skipped")
	})
}

func TestQuestionPanelLayout(t *testing.T) {
	app, tt := newFakeApp(t)
	connect(t, app, tt)
	chat := current(app)
	chat.Input = "/question"
	tt.Frame()
	if err := tt.Click("Send"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, app, tt, func() bool { return app.Permission != nil && app.PermissionOpen })
	tt.Frame()

	question, ok := tt.Find("Patch the CLI now?")
	if !ok {
		t.Fatal("question text not found")
	}
	input, ok := tt.Find("Message")
	if !ok {
		t.Fatal("composer input not found")
	}
	if question.Y+question.H > input.Y {
		t.Errorf("question ends at %v, below the composer input at %v", question.Y+question.H, input.Y)
	}

	description := "Patch the globally installed CLI and restart the app to verify the change, then keep the patch in sync with every Command Code upgrade."
	detail, ok := tt.Find(description)
	if !ok {
		t.Fatalf("%q not found", description)
	}
	if detail.H < 20 {
		t.Errorf("option description did not wrap: %+v", detail)
	}
	if detail.X+detail.W > input.X+input.W {
		t.Errorf("option description overflows the composer width: %+v, composer %+v", detail, input)
	}
}
