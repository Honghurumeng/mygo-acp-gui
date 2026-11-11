package ui

import (
	"testing"

	mui "github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/markdown"
	"github.com/reed/acp-gui/internal/agent"
)

func TestAgentIconsCoverBuiltins(t *testing.T) {
	for _, builtin := range agent.Builtins {
		if agentIcon(builtin.ID) == nil {
			t.Errorf("no icon for builtin agent %q", builtin.ID)
		}
		if agentIconName(builtin.ID) == "" {
			t.Errorf("no icon name for builtin agent %q", builtin.ID)
		}
	}
	if agentIcon("fake") != nil || agentIcon("") != nil {
		t.Error("unknown agents should not have an icon")
	}
}

func TestAgentBadgesOnConversations(t *testing.T) {
	workspace := t.TempDir()
	app := New(Options{
		Version:   "test",
		Workspace: workspace,
		Agents: []agent.LaunchSpec{
			{ID: agent.PiID, Name: "pi"},
			{ID: agent.CommandCodeID, Name: "Command Code"},
		},
	})
	app.MD.Reveal = &markdown.Reveal{Unit: markdown.AtOnce}
	piChat := app.activeChat()
	piChat.Title = "Pi chat"
	codeChat := app.newChatTabIn(workspace)
	codeChat.Agent = app.Agents[1]
	codeChat.Title = "Command Code chat"
	tt := mui.NewTester(app.View, 1060, 760)
	t.Cleanup(func() {
		app.Shutdown()
		app.Flush()
	})
	tt.Frame()
	for _, label := range []string{"pi agent", "Command Code agent"} {
		if _, ok := tt.Find(label); !ok {
			t.Errorf("missing %q badge; labels: %q", label, tt.Texts())
		}
	}
}
