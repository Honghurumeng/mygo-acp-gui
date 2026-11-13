package ui

import (
	"time"

	mui "github.com/egoist/mygo/ui"
)

// turnTimer floats the elapsed time of the running turn over the lower right
// of the conversation. It is a status pill, not a control: IgnoreInput keeps
// clicks on the messages underneath, and it disappears as soon as the prompt
// result clears turn.startedAt / chat.Streaming.
//
// The pill asks for another frame every 100 ms so formatElapsed's sub-second
// part keeps moving. The final Took row is separate and persisted.
func (a *App) turnTimer(c *mui.Context, chat *Chat) {
	if chat == nil || !chat.Streaming || chat.turn == nil || chat.turn.startedAt.IsZero() {
		return
	}
	elapsed := c.Now().Sub(chat.turn.startedAt)
	if elapsed < 0 {
		elapsed = 0
	}
	t := c.Theme()
	pill := mui.Row(c).Gap(5).AlignItems(mui.Center).Padding(5, 11).Radius(14).
		Background(t.Surface).
		BorderWidth(1).
		BorderColor(t.Border).
		Shadow(0, 2, 10, 0, mui.RGBA(0, 0, 0, 0.18)).
		Absolute().Right(14).Bottom(14).
		IgnoreInput(true)
	pill.Children(func() {
		mui.Text(c, "Working "+formatElapsed(elapsed)).FontSize(10).TextColor(t.Text)
	})
	c.After(100 * time.Millisecond)
}
