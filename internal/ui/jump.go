package ui

import (
	"fmt"
	"math"
	"strings"
	"time"

	mui "github.com/egoist/mygo/ui"
)

// jumpEntry is one of the user's own messages, with the row it takes in
// Chat.Messages: jumpDialog lists them oldest first and scrolls the message
// list to the row chosen.
type jumpEntry struct {
	row    int
	number int
	msg    *Message
}

// userHistory returns the chat's user messages oldest first. number counts
// the user's own turns from 1, so the list reads like a table of contents
// even when assistant, tool and notice rows sit between them.
func (c *Chat) userHistory() []jumpEntry {
	if c == nil {
		return nil
	}
	var entries []jumpEntry
	for i, m := range c.Messages {
		if m.Role != RoleUser {
			continue
		}
		entries = append(entries, jumpEntry{row: i, number: len(entries) + 1, msg: m})
	}
	return entries
}

// userMessageCount counts the rows userHistory would list without building
// the slice the composer would throw away.
func (c *Chat) userMessageCount() int {
	if c == nil {
		return 0
	}
	count := 0
	for _, m := range c.Messages {
		if m.Role == RoleUser {
			count++
		}
	}
	return count
}

// jumpEntryText is the one-line preview of a prompt: its text, with the
// images it carried, or just the images for a prompt sent without text.
func jumpEntryText(m *Message) string {
	if m == nil {
		return ""
	}
	text := truncateRunes(singleLine(m.Text), 180)
	images := len(m.Attachments)
	switch {
	case text == "" && images > 0:
		return imageCountLabel(images)
	case images > 0:
		return text + " · " + imageCountLabel(images)
	default:
		return text
	}
}

func imageCountLabel(n int) string {
	if n == 1 {
		return "1 image"
	}
	return fmt.Sprintf("%d images", n)
}

// jumpEntryTime is the compact send time shown at the right of a history
// row. Today shows only the clock, other days add the date, and messages
// replayed from an agent session without a local record show a dash.
func jumpEntryTime(m *Message) string {
	if m == nil || m.SentAt.IsZero() {
		return "—"
	}
	return formatJumpTime(m.SentAt, time.Now())
}

// formatJumpTime takes now as an argument so the date rules stay testable
// across midnight and year boundaries.
func formatJumpTime(sentAt, now time.Time) string {
	local := sentAt.Local()
	now = now.Local()
	switch {
	case local.Year() == now.Year() && local.YearDay() == now.YearDay():
		return local.Format("15:04")
	case local.Year() == now.Year():
		return local.Format("Jan 2 15:04")
	default:
		return local.Format("Jan 2, 2006 15:04")
	}
}

// truncateRunes cuts text at a rune boundary so a preview never splits a
// multi-byte character, as byte slicing would.
func truncateRunes(text string, limit int) string {
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	return strings.TrimSpace(string(runes[:limit])) + "…"
}

// jumpButton is the composer's jump-to-message action. It sits at the bottom
// left of the input card, after the attach button.
func (a *App) jumpButton(c *mui.Context, chat *Chat) {
	count := chat.userMessageCount()
	button := quietButton(c, "Jump").Label("Jump to your message").FontSize(10)
	if count == 0 {
		button.Tooltip("No messages of yours to jump to yet")
	} else {
		button.Tooltip("Jump to one of your previous messages")
	}
	button.Disabled(count == 0)
	if button.Clicked() {
		// The dialog lists oldest first, so open it at the newest message,
		// where the user is most likely to jump.
		chat.jumpScroll.Y = math.MaxFloat32
		chat.JumpOpen = true
	}
}

// jumpListHeightCap bounds the jumping list on a tall window. A shorter
// window subtracts the room the modal needs for its title, footer and
// margins, so the list scrolls instead of clipping the dialog.
const jumpListHeightCap = 380

func jumpListMaxHeight(windowHeight float32) float32 {
	return min(float32(jumpListHeightCap), max(float32(80), windowHeight-200))
}

// jumpDialog lists the current conversation's user messages and scrolls the
// message list to the one chosen. It reads Chat.Messages, so replayed history
// and the prompt being answered are included without a second log.
func (a *App) jumpDialog(c *mui.Context, chat *Chat) {
	if chat == nil || !chat.JumpOpen {
		return
	}
	t := c.Theme()
	entries := chat.userHistory()
	_, windowHeight := c.Size()
	listMaxHeight := jumpListMaxHeight(windowHeight)
	mui.Modal(c, &chat.JumpOpen, func() {
		mui.Column(c).Width(560).Gap(8).Children(func() {
			mui.Row(c).Gap(8).AlignItems(mui.Center).Children(func() {
				mui.Text(c, "Jump to a message").FontSize(16).Bold().Grow(1)
				if len(entries) > 0 {
					label := fmt.Sprintf("%d message", len(entries))
					if len(entries) != 1 {
						label += "s"
					}
					caption(c, label)
				}
			})
			mutedText(c, "Your messages in this conversation, oldest first. Choose one to scroll to it.", 10)
			switch {
			case len(entries) == 0:
				mutedText(c, "No messages of yours yet.", 11)
			default:
				mui.Scroll(c).TrackScroll(&chat.jumpScroll).MaxHeight(listMaxHeight).Gap(2).Children(func() {
					for _, entry := range entries {
						entry := entry
						item := mui.ButtonBase(c).FillWidth().Gap(8).AlignItems(mui.Center).
							Justify(mui.Start).Padding(6, 8).Radius(6).
							Label(fmt.Sprintf("Jump to message %d", entry.number))
						if item.Hovered() {
							item.Background(t.Surface)
						}
						item.Children(func() {
							mutedText(c, fmt.Sprintf("%d", entry.number), 9).
								Width(24).TextAlign(mui.End).Shrink(0)
							text := jumpEntryText(entry.msg)
							if text == "" {
								text = "(empty message)"
							}
							mui.Text(c, text).FontSize(11).Grow(1).SingleLine()
							stamp := mutedText(c, jumpEntryTime(entry.msg), 9).Shrink(0)
							if entry.msg != nil && !entry.msg.SentAt.IsZero() {
								stamp.Tooltip("Sent " + entry.msg.SentAt.Local().Format("2006-01-02 15:04:05"))
							} else {
								stamp.Tooltip("Send time not recorded; this message came from the agent's history")
							}
							mutedText(c, "→", 10).Shrink(0)
						})
						if item.Clicked() {
							chat.list.ScrollTo(entry.row, mui.Start)
							chat.JumpOpen = false
						}
					}
				})
			}
			mui.Row(c).Justify(mui.End).Children(func() {
				if quietButton(c, "Close").Clicked() {
					chat.JumpOpen = false
				}
			})
		})
	})
}
