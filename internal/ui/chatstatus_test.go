package ui

import (
	"testing"

	mui "github.com/egoist/mygo/ui"
)

// TestChatRowStatePriority locks the order the sidebar shows: a permission
// request outranks the turn that raised it, a turn that ended elsewhere
// outranks plain selection.
func TestChatRowStatePriority(t *testing.T) {
	cases := []struct {
		name     string
		chat     *Chat
		selected bool
		want     chatRowState
	}{
		{"idle", &Chat{}, false, chatRowIdle},
		{"selected", &Chat{}, true, chatRowSelected},
		{"unread beats selection", &Chat{Unread: true}, true, chatRowFinished},
		{"streaming beats unread", &Chat{Streaming: true, Unread: true}, true, chatRowStreaming},
		{"permission beats streaming", &Chat{Attention: true, Streaming: true}, true, chatRowNeedsPermission},
		{"nothing to show", nil, true, chatRowIdle},
	}
	for _, tc := range cases {
		if got := chatRowStateOf(tc.chat, tc.selected); got != tc.want {
			t.Errorf("%s: state = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestChatRowBackgrounds(t *testing.T) {
	t.Parallel()
	theme := mui.LightTheme()
	if got := chatRowBackground(theme, chatRowSelected); got != theme.Selection {
		t.Errorf("selected background = %v, want the theme's selection color", got)
	}
	if got := chatRowBackground(theme, chatRowFinished); got != theme.Success.Alpha(0.30) {
		t.Errorf("finished background = %v, want a green tint", got)
	}
	for _, state := range []chatRowState{chatRowIdle, chatRowStreaming, chatRowNeedsPermission} {
		if got := chatRowBackground(theme, state); got != theme.Background {
			t.Errorf("state %v background = %v, want the plain background", state, got)
		}
	}
}
