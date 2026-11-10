package main

import (
	"testing"

	"github.com/egoist/mygo/ui"
)

// TestViewRenders is a smoke test of the native view without a window.
func TestViewRenders(t *testing.T) {
	state := &appState{version: "test"}
	tt := ui.NewTester(state.view, 980, 680)
	for _, want := range []string{"ACP GUI", "vtest", "No agent connected yet"} {
		if !tt.HasText(want) {
			t.Errorf("view does not show %q; texts: %q", want, tt.Texts())
		}
	}
}
