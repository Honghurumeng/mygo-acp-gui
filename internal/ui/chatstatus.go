package ui

import (
	"time"

	mui "github.com/egoist/mygo/ui"
)

// chatRowRadius is the corner radius of a conversation row, shared by its
// background and the drawings painted over it.
const chatRowRadius = 8

// chatRowState is how a conversation row shows what its agent is doing. The
// row's background carries it, so the list needs no status glyphs.
type chatRowState int

const (
	// chatRowIdle is a conversation that needs no attention.
	chatRowIdle chatRowState = iota
	// chatRowSelected is the conversation on screen.
	chatRowSelected
	// chatRowFinished is a turn that ended, or failed, while the user was in
	// another conversation.
	chatRowFinished
	// chatRowStreaming is a turn running right now.
	chatRowStreaming
	// chatRowNeedsPermission waits for the user to answer a permission
	// request.
	chatRowNeedsPermission
)

// chatRowStateOf ranks what a row must show: a permission request comes
// before the turn that raised it, a turn that ended elsewhere before plain
// selection.
func chatRowStateOf(chat *Chat, selected bool) chatRowState {
	switch {
	case chat == nil:
		return chatRowIdle
	case chat.Attention:
		return chatRowNeedsPermission
	case chat.Streaming:
		return chatRowStreaming
	case chat.Unread:
		return chatRowFinished
	case selected:
		return chatRowSelected
	}
	return chatRowIdle
}

// chatRowBackground is the solid fill of a row state. The moving states paint
// over it in chatRowOverlay.
func chatRowBackground(t *mui.Theme, state chatRowState) mui.Color {
	switch state {
	case chatRowSelected:
		return t.Selection
	case chatRowFinished:
		// Green marks a turn that ended while the user was elsewhere.
		return t.Success.Alpha(0.30)
	}
	return t.Background
}

// chatRowOverlay paints what moves over a row's background: green diagonal
// stripes running left to right while the agent works, and a blue and white
// flash while it waits for a permission answer.
func chatRowOverlay(p *mui.Painter, r mui.Rect, state chatRowState) {
	t := p.Theme()
	switch state {
	case chatRowStreaming:
		drawRowStripes(p, r, t.Success.Alpha(0.35))
	case chatRowNeedsPermission:
		drawRowFlash(p, r, t.Accent, t.Background)
	}
}

// drawRowStripes paints evenly spaced green diagonals that travel from left
// to right, one stripe of each spacing a second, for a turn in progress.
func drawRowStripes(p *mui.Painter, r mui.Rect, ink mui.Color) {
	const (
		spacing = 14 // DIPs between stripes, along the row
		width   = 5
		turn    = 700 * time.Millisecond
	)
	ms := p.Now().UnixMilli()
	// The phase moves the pattern one spacing a turn, to the right.
	shift := float32(ms%turn.Milliseconds()) / float32(turn.Milliseconds()) * spacing
	p.Clip(r, chatRowRadius, func() {
		// A stripe starts a whole row below the left edge, so the diagonals
		// reaching into the row from below it are drawn too.
		for x := r.X - r.H - spacing + shift; x < r.X+r.W; x += spacing {
			var path mui.Path
			path.MoveTo(x, r.Y+r.H).LineTo(x+r.H, r.Y)
			p.StrokePath(&path, width, ink)
		}
	})
	p.AnimationFrame()
}

// drawRowFlash alternates a row between blue and the surface's paper color
// while a permission request waits for an answer.
func drawRowFlash(p *mui.Painter, r mui.Rect, blue, paper mui.Color) {
	const hold = 500 * time.Millisecond
	ms := p.Now().UnixMilli()
	inHold := ms % hold.Milliseconds()
	fill := paper
	if (ms/hold.Milliseconds())%2 == 0 {
		fill = blue
	}
	p.Clip(r, chatRowRadius, func() {
		p.Fill(r, fill, chatRowRadius)
	})
	// Ask again when the flash turns over.
	p.After(time.Duration(hold.Milliseconds()-inHold) * time.Millisecond)
}
