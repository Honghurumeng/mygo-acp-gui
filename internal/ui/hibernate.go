package ui

import "time"

const (
	// idleHibernateAfter is how long a conversation can stay quiet before
	// its agent process is closed. The transcript, Info (session id, config,
	// usage) and composer stay in memory; the next prompt resumes the
	// session with session/load.
	idleHibernateAfter = 15 * time.Minute
	// idleHibernateCheck is how often the background sleeper looks for idle
	// conversations.
	idleHibernateCheck = time.Minute
)

// startIdleSleeper closes the agent processes of idle conversations so a
// background tab does not keep a whole agent runtime alive forever. It stops
// when Shutdown cancels lifeCtx.
func (a *App) startIdleSleeper() {
	if a.lifeCtx == nil {
		return
	}
	a.cleanup.Add(1)
	go func() {
		defer a.cleanup.Done()
		ticker := time.NewTicker(idleHibernateCheck)
		defer ticker.Stop()
		for {
			select {
			case <-a.lifeCtx.Done():
				return
			case now := <-ticker.C:
				a.Post(func() {
					if !a.closed.Load() {
						a.hibernateIdleChats(now)
					}
				})
			}
		}
	}()
}

// hibernateIdleChats closes the process of every conversation that has been
// idle for idleHibernateAfter. Chats that are connecting, streaming, waiting
// for permission, replaying or owned by a task runner are left alone.
func (a *App) hibernateIdleChats(now time.Time) {
	for _, chat := range a.chatsSnapshot() {
		if chat.canHibernate(now) {
			a.hibernateChat(chat)
		}
	}
}

// canHibernate reports whether closing chat's process now can be resumed
// later. A chat without a saved ACP session id has nothing to load, and one
// with an unfinished turn or dialog would lose work.
func (c *Chat) canHibernate(now time.Time) bool {
	if c == nil || c.Closed || c.taskActive || c.Session == nil {
		return false
	}
	if c.Connecting || c.Streaming || c.Busy != "" || c.ConfigBusy != "" || c.replaying {
		return false
	}
	if c.Attention || c.Info.SessionID == "" {
		return false
	}
	// turn stays on the chat after a completed prompt; only a live turn has
	// a start time. Streaming already covers most of it, but an agent that
	// pauses between chunks must not lose the turn either.
	if c.turn != nil && !c.turn.startedAt.IsZero() {
		return false
	}
	last := c.lastActiveAt
	if last.IsZero() {
		last = c.createdAt
	}
	return !last.IsZero() && now.Sub(last) >= idleHibernateAfter
}

// hibernateChat closes chat's process but keeps the conversation in memory.
// It only runs on idle chats, so no turn is interrupted: the transcript and
// the ACP session id stay, the row switches to the saved-session clock icon,
// and the next prompt reloads the session.
func (a *App) hibernateChat(chat *Chat) {
	s := chat.Session
	if s == nil {
		return
	}
	a.cacheAgentConfigs(chat.Agent.ID, chat.Info.ConfigOptions)
	a.cacheAgentModes(chat.Agent.ID, chat.Info.Modes, chat.Info.CurrentModeID)
	a.rememberSelection(chat)
	// Sleeping is not conversation activity, so only create a missing record;
	// an existing one keeps its last-activity time and sidebar position.
	if a.archiveIndex(chat.Agent.ID, chat.Info.SessionID) < 0 {
		a.activateArchivedSession(chat.Agent.ID, chat.Info.SessionID, chat.Workspace, chat.Info.Title)
	}
	chat.Session = nil
	chat.Connected = false
	chat.Busy = ""
	chat.Streaming = false
	chat.Cancelling = false
	chat.replaying = false
	chat.turn = nil
	chat.ModelOpen = false
	chat.ModeOpen = false
	chat.BranchPicker.close()
	a.detachSubagents(chat)
	a.closeInBackground(chat, s)
}

// detachSubagents marks every running child as disconnected and closes its
// stream, because the parent agent process is going away.
func (a *App) detachSubagents(chat *Chat) {
	if chat == nil {
		return
	}
	for _, sub := range chat.Subagents {
		if !subagentStateTerminal(sub.State) {
			sub.State = "disconnected"
			a.refreshSubagent(chat, sub)
		}
		a.finishSubagentMessages(sub)
	}
}
