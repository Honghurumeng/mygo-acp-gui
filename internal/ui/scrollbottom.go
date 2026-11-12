package ui

import mui "github.com/egoist/mygo/ui"

// jumpToLatestButton floats over the conversation while the list is scrolled
// away from its end. It reads ListState.AtEnd, which marks the state read so
// MyGo builds another frame when scrolling changes whether the end shows:
// the button appears in that frame and goes once the list follows again.
// FollowEnd stays on throughout, so rows that arrive while the button shows
// do not pull the list back down.
func (a *App) jumpToLatestButton(c *mui.Context, chat *Chat) {
	if chat == nil || chat.list.AtEnd() {
		return
	}
	t := c.Theme()
	button := buttonBase(c, "Jump to latest").
		Padding(5, 11).
		Radius(14).
		Background(t.Surface).
		BorderWidth(1).
		BorderColor(t.Border).
		Shadow(0, 2, 10, 0, mui.RGBA(0, 0, 0, 0.18))
	switch {
	case button.Pressed():
		button.Background(t.SurfacePressed)
	case button.Hovered():
		button.Background(t.SurfaceHover)
	}
	button.Children(func() {
		mui.Text(c, "↓ Latest").FontSize(10).TextColor(t.Text)
	})
	if button.Clicked() {
		chat.list.ScrollToEnd()
	}
	// Centered just above the list's lower edge, over the messages rather
	// than in the column's layout, so showing it never shifts the list.
	button.Absolute().LeftPercent(50).Bottom(14).TranslatePercent(-50, 0)
}
