package ui

import (
	"testing"
	"time"

	mui "github.com/egoist/mygo/ui"
	"github.com/reed/acp-gui/internal/agent"
)

// TestTurnTimerShowsLiveElapsedTime is the floating status pill: it appears
// only for a running turn in the lower-right of the conversation, keeps
// counting without user input, and disappears when the turn ends.
func TestTurnTimerShowsLiveElapsedTime(t *testing.T) {
	app := New(Options{
		Version:   "test",
		Workspace: t.TempDir(),
		Agents:    []agent.LaunchSpec{{ID: "fake", Name: "fake-agent"}},
	})
	t.Cleanup(func() {
		app.Shutdown()
		app.Flush()
	})
	chat := current(app)
	tt := mui.NewTester(app.View, 1060, 760)
	tt.Frame()
	if tt.HasText("Working ") {
		t.Fatalf("timer shows before a turn starts; texts: %q", tt.Texts())
	}

	chat.Streaming = true
	chat.turn = &turnState{startedAt: tt.Now().Add(-12 * time.Second)}
	tt.Frame()
	if !tt.HasText("Working 12s") {
		t.Fatalf("timer does not show the elapsed time; texts: %q", tt.Texts())
	}
	rect, ok := tt.Find("Working 12s")
	if !ok {
		t.Fatalf("timer is missing; texts: %q", tt.Texts())
	}
	if rect.X+rect.W < 530 || rect.Y+rect.H < 380 {
		t.Errorf("timer is not in the lower right at x=%v y=%v w=%v h=%v", rect.X, rect.Y, rect.W, rect.H)
	}

	// The pill asks for another frame every 100 ms, so advancing the
	// tester's clock updates the label without any chat event.
	tt.Advance(2 * time.Second)
	tt.Frame()
	if !tt.HasText("Working 14s") {
		t.Errorf("timer did not advance; texts: %q", tt.Texts())
	}

	// A finished turn clears its start time and hides the pill immediately.
	chat.Streaming = false
	chat.turn.startedAt = time.Time{}
	tt.Frame()
	if tt.HasText("Working ") {
		t.Errorf("timer stayed after the turn ended; texts: %q", tt.Texts())
	}
}
