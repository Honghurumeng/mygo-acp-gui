package acp

import "testing"

func TestPermissionOutcomes(t *testing.T) {
	selected := SelectedPermission("allow-once")
	if selected.Outcome.Outcome != "selected" || selected.Outcome.OptionID != "allow-once" {
		t.Errorf("selected = %+v", selected)
	}
	cancelled := CancelledPermission()
	if cancelled.Outcome.Outcome != "cancelled" || cancelled.Outcome.OptionID != "" {
		t.Errorf("cancelled = %+v", cancelled)
	}
}
