package ui

import "github.com/egoist/mygo"

// updateDockBadge shows how many conversations wait for the user on the app
// icon: permission requests still open, and turns that ended while another
// conversation was on screen. A background chat that is still streaming is
// not counted yet; it joins the badge when its turn finishes and Unread
// becomes visible in the sidebar. Zero clears the badge.
//
// It runs on the UI thread. The last count is cached so an update arriving
// for every session event does not call the platform unless the badge
// actually changes. With no window ever created (tests) there is no app to
// badge unless a test injected setDockBadge.
// appFocused reports whether the window is on screen with keyboard focus.
// Without a window ever created (tests) it reports true, so a turn in the
// active chat stays read as it did before window focus mattered.
func (a *App) appFocused() bool {
	if a.isFocused != nil {
		return a.isFocused()
	}
	if a.updateWin == nil {
		return true
	}
	win := a.window()
	return win != nil && win.IsFocused()
}

// markChatUnread records a turn, error or exit the user was not looking at.
// A finished turn in the active chat still counts when the window is closed
// or in the background, so the Dock badge and the sidebar show it until the
// window is focused again or the chat is selected.
func (a *App) markChatUnread(chat *Chat) {
	if chat == nil {
		return
	}
	if chat != a.activeChat() || !a.appFocused() {
		chat.Unread = true
	}
	a.updateDockBadge()
}

// clearActiveUnread marks what the user is looking at as read when the
// window comes back: selecting a chat already does this, but reopening or
// reactivating the window is just as much seeing the conversation.
func (a *App) clearActiveUnread() {
	chat := a.activeChat()
	if chat == nil || !chat.Unread {
		return
	}
	chat.Unread = false
	a.updateDockBadge()
}

func (a *App) updateDockBadge() {
	if a == nil {
		return
	}
	count := 0
	if a.DockBadgeEnabled {
		for _, chat := range a.chatsSnapshot() {
			if chat == nil || chat.Closed {
				continue
			}
			if chat.Attention || (chat.Unread && !chat.Streaming) {
				count++
			}
		}
	}
	if count == a.dockBadgeCount {
		return
	}
	a.dockBadgeCount = count
	if a.setDockBadge != nil {
		a.setDockBadge(count)
		return
	}
	if a.updateWin == nil {
		return
	}
	mygo.App.SetBadgeCount(count)
}
