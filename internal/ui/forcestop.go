package ui

import (
	mui "github.com/egoist/mygo/ui"
	"github.com/reed/acp-gui/internal/session"
)

// openForceStop asks before killing a conversation's agent process. The
// dialog exists because the force stop itself cannot be undone: the current
// turn is interrupted, while the transcript and session id stay in the tab.
func (a *App) openForceStop(chat *Chat) {
	if !chat.canForceStop() {
		return
	}
	a.ForceStopChat = chat
	a.ForceStopOpen = true
}

func (a *App) closeForceStop() {
	a.ForceStopOpen = false
	a.ForceStopChat = nil
}

func (a *App) confirmForceStop() {
	chat := a.ForceStopChat
	a.closeForceStop()
	a.forceStopChat(chat)
}

// forceStopChat closes a process now instead of waiting for idle sleep. It
// keeps the transcript, Info (session id, config, usage) and composer, so the
// next prompt resumes the session with session/load, exactly like waking a
// sleeping chat. An in-flight turn is marked as stopped.
func (a *App) forceStopChat(chat *Chat) {
	if chat == nil || chat.Closed || chat.taskActive {
		return
	}
	s := chat.Session
	if s == nil && !chat.Connecting {
		return
	}
	interrupted := chat.Streaming || (chat.turn != nil && !chat.turn.startedAt.IsZero())
	chat.cancelConnect()
	elapsed := a.finishTurnRuntime(chat, chat.turn)
	if interrupted {
		// Close or remove the eager assistant row before the notice lands.
		a.addNotice(chat, T("Stopped by user."), &session.Notice{Kind: "force_stop", Level: "warning"})
	}
	chat.turn = nil
	chat.Streaming = false
	chat.Cancelling = false
	chat.Connecting = false
	chat.Connected = false
	chat.Busy = ""
	chat.replaying = false
	chat.replayKey = ""
	chat.ModelOpen = false
	chat.ModeOpen = false
	chat.BranchPicker.close()
	a.detachSubagents(chat)
	a.cancelPermissionsFor(chat)
	chat.Session = nil
	chat.clearPendingSession()
	if interrupted {
		a.addTurnSummary(chat, elapsed)
	}
	if s != nil {
		a.closeInBackground(chat, s)
	}
	a.refreshGitAfterTurn(chat)
	a.refreshFilesAfterTurn(chat)
}

func (a *App) forceStopDialog(c *mui.Context) {
	if !a.ForceStopOpen || a.ForceStopChat == nil {
		return
	}
	chat := a.ForceStopChat
	if !chat.canForceStop() {
		a.closeForceStop()
		return
	}
	t := c.Theme()
	hint := T("The conversation stays in the tab and resumes on the next message.")
	if chat.Streaming || (chat.turn != nil && !chat.turn.startedAt.IsZero()) {
		hint = T("The current turn will be interrupted. The conversation stays in the tab and resumes on the next message.")
	}
	mui.Modal(c, &a.ForceStopOpen, func() {
		mui.Column(c).Gap(10).Width(380).Children(func() {
			mui.Text(c, T("Force stop this agent process?")).FontSize(a.workspaceFont(16)).Bold()
			mui.Text(c, chat.name()).FontSize(a.workspaceFont(12)).Bold().SingleLine()
			mui.Text(c, hint).FontSize(a.workspaceFont(10)).TextColor(t.TextMuted)
			mui.Row(c).Gap(8).Justify(mui.End).Children(func() {
				if quietButton(c, T("Cancel")).Clicked() {
					a.closeForceStop()
				}
				if dangerButton(c, T("Force stop")).Clicked() {
					a.confirmForceStop()
				}
			})
		})
	})
	if !a.ForceStopOpen {
		a.closeForceStop()
	}
}
