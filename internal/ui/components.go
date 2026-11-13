// Components that keep state across frames or animate, shared by more than
// one view; the plain looks are in widgets.go. Everything takes its colors,
// sizes and corners from c.Theme(), so light and dark need no branches here.

package ui

import (
	"strconv"
	"strings"
	"time"

	mui "github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
)

// shade is black at alpha a, for the modal's dim and its shadow.
func shade(a float32) mui.Color { return mui.RGBA(0, 0, 0, a) }

// arrive eases e from a pose to rest on first build: the panel of the
// permission dialog comes in slightly smaller and transparent, the dim
// fades in from black.
func arrive(e mui.Element, from mui.Pose) mui.Element {
	p := e.Keyframes("ui.arrive",
		[]mui.Keyframe{{Value: 1}, {At: 200 * time.Millisecond, Value: 0, Ease: mui.EaseOut}})
	scale := from.Scale
	if scale == 0 {
		scale = 1
	}
	k := 1 + (scale-1)*p
	return e.Translate(from.X*p, from.Y*p).Scale(k, k).Opacity(1 - from.Fade*p)
}

// centered centers content in the room it is given, easing it in.
func centered(c *mui.Context, content func()) mui.Element {
	return mui.Column(c).Fill().Center().Children(func() {
		arrive(mui.Box(c), mui.Pose{Scale: 0.96, Fade: 1}).Children(content)
	})
}

// busyLine is a spinner and what it waits for, read out as a status. It is
// the look of the list's loading states and of a reply still streaming.
func busyLine(c *mui.Context, text string) mui.Element {
	return mui.Row(c).Gap(8).AlignItems(mui.Center).Role(mui.RoleStatus).Label(text).Children(func() {
		mui.Spinner(c)
		mutedText(c, text, 12)
	})
}

// emptyState is the middle of an empty list: a tinted disc, a title, and a
// line saying what to do. It is one status for assistive technology, named
// by the title.
func emptyState(c *mui.Context, icon *mui.SVG, title, text string) mui.Element {
	t := c.Theme()
	root := mui.Column(c).AlignSelf(mui.Center).MaxWidth(t.Space(100)).
		AlignItems(mui.Center).Gap(t.Space(2)).Padding(t.Space(6)).
		Role(mui.RoleStatus).Label(title)
	arrive(root, mui.Pose{Y: t.Space(3), Fade: 1})
	root.Children(func() {
		mui.Box(c).Size(t.Space(12), t.Space(12)).Center().Radius(999).
			Background(t.TextMuted.Alpha(0.12)).TextColor(t.TextMuted).
			Margin(0, 0, t.Space(1), 0).Children(func() {
			mui.Icon(c, icon).Size(t.Space(6), t.Space(6))
		})
		mui.Text(c, title).Bold().FontSize(t.Rem(1.15)).TextAlign(mui.Center)
		if text != "" {
			mutedText(c, text, t.Rem(1)).TextAlign(mui.Center)
		}
	})
	return root
}

// dangerAlert is a failure worth its own box: the theme's danger color tints
// the background, the border and the icon, and colors the body slightly.
func dangerAlert(c *mui.Context, title, body string) mui.Element {
	t := c.Theme()
	col := t.Danger
	root := mui.Row(c).FillWidth().AlignItems(mui.Start).Gap(t.Space(3)).
		Padding(t.Space(3)).Radius(t.Radius).
		Background(col.Alpha(0.08)).Border(1, col.Alpha(0.3)).
		Role(mui.RoleStatus).Label(title)
	root.Children(func() {
		mui.Icon(c, circleXIcon).Size(16, 16).TextColor(col).Margin(t.Space(0.5), 0, 0, 0)
		mui.Column(c).Grow(1).Gap(t.Space(1)).Children(func() {
			if title != "" {
				mui.Text(c, title).Bold()
			}
			if body != "" {
				mui.Text(c, body).TextColor(t.Text.Mix(col, 0.2))
			}
		})
	})
	return arrive(root, mui.Pose{Y: -6, Fade: 1})
}

// codeBlock shows a tool's input: a titled, highlighted block with a copy
// button. It is only what the permission dialog needs, not a code browser:
// one language, wrapped lines, no scrolling.
func codeBlock(c *mui.Context, lang, code, title string) mui.Element {
	t := c.Theme()
	root := mui.Column(c).FillWidth().Clip().Radius(t.Radius).Background(t.Surface).
		Border(1, t.Border).Role(mui.RoleGroup).Label(title)
	arrive(root, mui.Pose{Y: t.Space(1), Fade: 1})
	root.Children(func() {
		mui.Row(c).FillWidth().Gap(t.Space(2)).AlignItems(mui.Center).
			Padding(t.Space(1), t.Space(1), t.Space(1), t.Space(3)).
			Background(t.SurfaceHover).BorderWidth(0, 0, 1, 0).BorderColor(t.Border).
			Children(func() {
				mui.Icon(c, codeIcon).Size(14, 14).Shrink(0).TextColor(t.TextMuted)
				mui.Text(c, title).Font("monospace").FontSize(t.Rem(0.8)).Bold().
					Grow(1).MinWidth(0).SingleLine()
				copyButton(c, code)
			})
		codeLines(c, lang, code)
	})
	return root
}

// copyButton writes code to the clipboard and shows a check for two seconds.
// The label stays "Copy": the icon says what happened, the label names the
// button.
func copyButton(c *mui.Context, code string) mui.Element {
	t := c.Theme()
	b := buttonBase(c, "Copy").Gap(6).Border(1, t.Border).Background(t.Surface)
	copied := mui.Local(b, "copied", func() bool { return false })
	until := mui.Local(b, "until", func() time.Time { return time.Time{} })
	if *copied {
		if !c.Now().Before(*until) {
			*copied = false
		} else {
			c.After(until.Sub(c.Now()))
		}
	}
	if b.Clicked() {
		c.WriteClipboard(code)
		*copied = true
		*until = c.Now().Add(2 * time.Second)
		c.After(2 * time.Second)
	}
	b.Children(func() {
		glyph := copyIcon
		if *copied {
			glyph = checkIcon
		}
		mui.Icon(c, glyph).Size(14, 14)
		mui.Text(c, "Copy").FontSize(t.Rem(0.8)).SingleLine()
	})
	return b
}

// codeLines draws code one line per row, numbered and highlighted, with long
// lines wrapped. The tokenizer carries its state from line to line, so a
// string or comment spanning lines keeps its color.
func codeLines(c *mui.Context, lang, code string) {
	t := c.Theme()
	lines := strings.Split(code, "\n")
	if n := len(lines); n > 1 && lines[n-1] == "" {
		lines = lines[:n-1]
	}
	digits := float32(len(strconv.Itoa(len(lines))))
	theme := editor.DefaultTheme(t)
	var tok editor.Tokenizer
	if l := editor.LanguageNamed(lang); l != nil {
		tok = l.Tokenizer
	}
	state := uint64(0)
	mui.Column(c).FillWidth().Padding(t.Space(2), 0).Children(func() {
		for i, line := range lines {
			c.Key(i)
			var spans []mui.Span
			spans, state = codeSpans(tok, theme, state, line)
			mui.Row(c).FillWidth().Gap(t.Space(3)).Padding(0, t.Space(3)).Children(func() {
				mui.Text(c, strconv.Itoa(i+1)).Font("monospace").FontSize(t.Rem(0.8)).
					TextColor(t.TextMuted.Alpha(0.7)).Width(t.Space(2 * digits)).Shrink(0).
					TextAlign(mui.End).LineHeight(1.6)
				mui.RichText(c, spans...).Font("monospace").FontSize(t.Rem(0.8)).
					LineHeight(1.6).Grow(1).MinWidth(0).Wrap()
			})
		}
	})
}

// codeSpans tokenizes one line at state and returns its colored spans with
// the state the next line starts in. Tabs become four spaces; a line the
// language does not know shows in the theme's text color.
func codeSpans(tok editor.Tokenizer, theme *editor.Theme, state uint64, line string) ([]mui.Span, uint64) {
	if tok == nil {
		return []mui.Span{{Text: untab(line), Color: theme.Text}}, 0
	}
	toks, next := tok.Tokenize(line, state, nil)
	out := make([]mui.Span, 0, len(toks)+1)
	at := 0
	for _, tk := range toks {
		if tk.End > len(line) || tk.End <= at {
			continue
		}
		ink := theme.Tokens[tk.Kind].Color
		if ink.A == 0 {
			ink = theme.Text
		}
		out = append(out, mui.Span{Text: untab(line[at:tk.End]), Color: ink})
		at = tk.End
	}
	if at < len(line) {
		out = append(out, mui.Span{Text: untab(line[at:]), Color: theme.Text})
	}
	return out, next
}

// untab replaces a line's tabs so that token offsets and drawn text agree.
func untab(s string) string { return strings.ReplaceAll(s, "\t", "    ") }
