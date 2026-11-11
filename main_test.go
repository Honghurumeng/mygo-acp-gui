package main

import (
	"os"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/reed/acp-gui/internal/agent"
	appui "github.com/reed/acp-gui/internal/ui"
)

// TestViewRenders is a smoke test of the native view without a window.
func TestViewRenders(t *testing.T) {
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	state := appui.New(appui.Options{
		Version:   "test",
		Workspace: t.TempDir(),
		Agents: []agent.LaunchSpec{{
			ID:      "test",
			Name:    "Test agent",
			Command: self,
		}},
	})
	tt := ui.NewTester(state.View, 1060, 760)
	for _, want := range []string{"ACP GUI", "vtest", "Start the conversation."} {
		if !tt.HasText(want) {
			t.Errorf("view does not show %q; texts: %q", want, tt.Texts())
		}
	}
}
