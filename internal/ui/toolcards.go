package ui

import (
	"encoding/json"
	"net/url"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	mui "github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
	"github.com/reed/acp-gui/internal/files"
)

// toolDiffInlineMaxLines is how many lines of a change stay inline. A larger
// diff scrolls inside a fixed-height viewer instead of growing the card.
const toolDiffInlineMaxLines = 400

// toolCardMaxHeight is the tallest body a tool card shows before the rest
// scrolls inside it. Output, file text and reasoning arrive in unbounded
// amounts, and a card taller than the window would push the conversation away.
const toolCardMaxHeight float32 = 320

// toolCardScroll builds a card's body as a scroller that stops growing at
// toolCardMaxHeight. The header and the body's own buttons stay outside it, so
// the toggle and the actions never scroll out of reach.
func toolCardScroll(c *mui.Context, fn func()) {
	mui.Scroll(c).MaxHeight(toolCardMaxHeight).Children(fn)
}

// toolMessage draws one tool call as a card that starts collapsed: its header
// keeps the target and the result on one line, and the body stays hidden. A
// click on the header opens the body; another click closes it again.
func (a *App) toolMessage(c *mui.Context, m *Message) {
	status := toolStatusLabel(m.Status)
	statusColor := toolStatusColor(c.Theme(), m.Status)
	if !toolHasBody(m) {
		// Orchestration calls such as wait_agent and close_agent have no
		// user-facing payload. Draw them as a compact event instead of an
		// expandable card whose panel would otherwise be empty.
		surfaceCard(c, mui.Row(c).Gap(6).MaxWidth(chatMaxWidth).Padding(8, 10).AlignItems(mui.Center)).Children(func() {
			a.subagentLabel(c, m)
			mutedText(c, "•", a.chatFont(12)).Width(8).Shrink(0)
			if m.ToolKind == "execute" {
				mutedText(c, "$", a.chatFont(11)).Shrink(0).Font("monospace")
			}
			mui.Text(c, m.Title).Bold().FontSize(a.chatFont(12)).Grow(1).MinWidth(0).SingleLine()
			if m.ToolExit != nil {
				a.toolExitBadge(c, *m.ToolExit)
			} else if status != "" {
				mui.Text(c, status).FontSize(a.chatFont(11)).TextColor(statusColor).Shrink(0)
			}
		})
		return
	}
	if m.ToolKind == "execute" {
		a.executeToolCard(c, m, status, statusColor)
		return
	}
	if m.ToolKind == "read" && (strings.TrimSpace(m.Detail) != "" || len(m.ToolImages) > 0) {
		a.readToolCard(c, m, status, statusColor)
		return
	}
	if m.ToolKind == "search" && strings.TrimSpace(m.Detail) != "" {
		a.searchToolCard(c, m, status, statusColor)
		return
	}
	if m.ToolKind == "think" {
		a.thinkToolCard(c, m, status, statusColor)
		return
	}
	if m.ToolKind == "fetch" {
		a.fetchToolCard(c, m, status, statusColor)
		return
	}
	if toolDiffCard(m) {
		a.diffToolCard(c, m, status, statusColor)
		return
	}
	a.textToolCard(c, m, status, statusColor)
}

// toolDiffCard reports whether the call's change is shown as a diff instead of
// its flat text.
func toolDiffCard(m *Message) bool {
	if m == nil || len(m.ToolDiffs) == 0 {
		return false
	}
	switch m.ToolKind {
	case "edit", "delete", "move", "":
		return true
	}
	return false
}

// diffToolCard draws an edit, write, delete or move as an inline diff.
func (a *App) diffToolCard(c *mui.Context, m *Message, status string, statusColor mui.Color) {
	t := c.Theme()
	font := a.chatFont(11)
	stats := m.toolDiffStats(font)
	icon := toolDiffIcon(m)
	verb := toolDiffVerb(m)
	path := toolDiffPath(m)
	surfaceCard(c, mui.Column(c).Gap(3).MaxWidth(chatMaxWidth).Padding(8, 10)).Children(func() {
		a.subagentLabel(c, m)
		// Created inside the card: nodes join their parent as they are built,
		// so a trigger built before it would sit outside the card's surface.
		parts := mui.CollapsibleBase(c, &m.DetailOpen)
		parts.Trigger.Grow(1).Gap(6).AlignItems(mui.Center).
			Label(Tf("Toggle tool output: %s", m.Title)).Children(func() {
			mark := "▸"
			if m.DetailOpen {
				mark = "▾"
			}
			mutedText(c, mark, a.chatFont(10)).Width(10).Shrink(0)
			mui.Icon(c, icon).Size(14, 14).Shrink(0).TextColor(t.TextMuted)
			sectionLabel(c, verb)
			a.toolPathLabel(c, path)
			a.toolStatsLabel(c, stats)
			if elapsed := toolCardDuration(m); elapsed != "" {
				mutedText(c, elapsed, a.chatFont(9)).Shrink(0)
			}
			if status != "" {
				mui.Text(c, status).FontSize(a.chatFont(11)).TextColor(statusColor).Shrink(0)
			}
		})
		parts.Panel(func() {
			mui.Column(c).Gap(6).Children(func() {
				toolCardScroll(c, func() {
					mui.Column(c).Gap(6).Children(func() {
						states := m.toolDiffStates(font)
						for i, diff := range m.ToolDiffs {
							c.Key(i)
							if len(m.ToolDiffs) > 1 {
								mui.Row(c).Gap(5).AlignItems(mui.Center).Children(func() {
									mui.Icon(c, codeIcon).Size(12, 12).Shrink(0).TextColor(t.TextMuted)
									mui.Text(c, toolDiffDisplayPath(a.activeFileWorkspace(), diff.Path)).
										FontSize(a.chatFont(10)).Bold().SingleLine().MinWidth(0)
								})
							}
							view := editor.DiffView(c, states[i]).FillWidth()
							if !states[i].FitContent {
								view.Height(280)
							}
						}
						if m.Detail != "" {
							mutedText(c, m.Detail, a.chatFont(11)).Selectable()
						}
					})
				})
				a.toolCardActions(c, m)
			})
		}).PaddingStart(16)
	})
}

// textToolCard is the flat card: the tool title and its text output. It is the
// fallback for kinds without a dedicated body and for agents that send only
// text.
func (a *App) textToolCard(c *mui.Context, m *Message, status string, statusColor mui.Color) {
	surfaceCard(c, mui.Column(c).Gap(3).MaxWidth(chatMaxWidth).Padding(8, 10)).Children(func() {
		a.subagentLabel(c, m)
		parts := mui.CollapsibleBase(c, &m.DetailOpen)
		parts.Trigger.Grow(1).Gap(6).AlignItems(mui.Center).
			Label(Tf("Toggle tool output: %s", m.Title)).Children(func() {
			mark := "▸"
			if m.DetailOpen {
				mark = "▾"
			}
			// A fixed arrow width keeps the panel aligned with the command.
			mutedText(c, mark, a.chatFont(10)).Width(10)
			title := mui.Text(c, m.Title).Bold().FontSize(a.chatFont(12)).Grow(1)
			if !m.DetailOpen {
				// Long commands stay one line until the card is opened.
				title.SingleLine()
			}
			if elapsed := toolCardDuration(m); elapsed != "" {
				mutedText(c, elapsed, a.chatFont(9)).Shrink(0)
			}
			if status != "" {
				mui.Text(c, status).FontSize(a.chatFont(11)).TextColor(statusColor).Shrink(0)
			}
		})
		parts.Panel(func() {
			text := toolCardText(m)
			if len(m.ToolImages) > 0 {
				toolCardScroll(c, func() {
					a.toolImagePanel(c, m)
				})
			}
			switch {
			case text == "":
			case looksLikeJSON(m.Detail):
				toolCardScroll(c, func() {
					a.toolCodeBlock(c, "json", text, 1)
				})
				copyButton(c, m.Detail)
			default:
				toolCardScroll(c, func() {
					mutedText(c, text, a.chatFont(11)).Selectable()
				})
			}
			a.toolLongTextToggle(c, m)
		}).PaddingStart(16)
	})
}

// toolTextCardMaxRunes is the flat text a card shows before the whole body is
// asked for. Tool output and diffs have their own line budgets.
const toolTextCardMaxRunes = 2000

// toolCardText is the flat body, cut once while it is longer than the budget
// and the user has not asked for all of it.
func toolCardText(m *Message) string {
	text := m.Detail
	if m.toolOutputOpen {
		return text
	}
	runes := []rune(text)
	if len(runes) <= toolTextCardMaxRunes {
		return text
	}
	return string(runes[:toolTextCardMaxRunes]) + "…"
}

// toolLongTextToggle offers the whole flat body of a tool that shipped more
// text than a card shows, and puts the cut back.
func (a *App) toolLongTextToggle(c *mui.Context, m *Message) {
	if len([]rune(m.Detail)) <= toolTextCardMaxRunes {
		return
	}
	label := T("Show all")
	if m.toolOutputOpen {
		label = T("Show less")
	}
	button := quietButton(c, label).FontSize(a.chatFont(9)).Padding(2, 6)
	if button.Clicked() {
		m.toolOutputOpen = !m.toolOutputOpen
	}
}

// thinkToolCard draws reasoning as a quiet line instead of a card surface: it
// is the model thinking, not an action it took. Long reasoning stays
// expandable.
func (a *App) thinkToolCard(c *mui.Context, m *Message, status string, statusColor mui.Color) {
	t := c.Theme()
	mui.Column(c).Gap(2).MaxWidth(chatMaxWidth).Children(func() {
		a.subagentLabel(c, m)
		parts := mui.CollapsibleBase(c, &m.DetailOpen)
		parts.Trigger.Grow(1).Gap(6).AlignItems(mui.Center).
			Label(Tf("Toggle tool output: %s", m.Title)).Children(func() {
			mark := "▸"
			if m.DetailOpen {
				mark = "▾"
			}
			mutedText(c, mark, a.chatFont(10)).Width(10).Shrink(0)
			mui.Icon(c, sparklesIcon).Size(13, 13).Shrink(0).TextColor(t.TextMuted)
			mui.Text(c, m.Title).FontSize(a.chatFont(11)).TextColor(t.TextMuted).Grow(1).MinWidth(0).SingleLine()
			if elapsed := toolCardDuration(m); elapsed != "" {
				mutedText(c, elapsed, a.chatFont(9)).Shrink(0)
			}
			if status != "" {
				mui.Text(c, status).FontSize(a.chatFont(10)).TextColor(statusColor).Shrink(0)
			}
		})
		parts.Panel(func() {
			toolCardScroll(c, func() {
				mutedText(c, m.Detail, a.chatFont(11)).Selectable()
			})
		}).PaddingStart(16)
	})
}

// fetchToolCard draws a network fetch: the host on the header, the full link
// and the page's text below it.
func (a *App) fetchToolCard(c *mui.Context, m *Message, status string, statusColor mui.Color) {
	target := m.ToolURL
	if target == "" {
		target = m.Title
	}
	surfaceCard(c, mui.Column(c).Gap(3).MaxWidth(chatMaxWidth).Padding(8, 10)).Children(func() {
		a.subagentLabel(c, m)
		parts := mui.CollapsibleBase(c, &m.DetailOpen)
		parts.Trigger.Grow(1).Gap(6).AlignItems(mui.Center).
			Label(Tf("Toggle tool output: %s", m.Title)).Children(func() {
			mark := "▸"
			if m.DetailOpen {
				mark = "▾"
			}
			mutedText(c, mark, a.chatFont(10)).Width(10).Shrink(0)
			sectionLabel(c, T("Fetch"))
			mui.Text(c, toolURLHost(target)).FontSize(a.chatFont(12)).Bold().Grow(1).MinWidth(0).SingleLine()
			if elapsed := toolCardDuration(m); elapsed != "" {
				mutedText(c, elapsed, a.chatFont(9)).Shrink(0)
			}
			if status != "" {
				mui.Text(c, status).FontSize(a.chatFont(11)).TextColor(statusColor).Shrink(0)
			}
		})
		parts.Panel(func() {
			toolCardScroll(c, func() {
				mui.Column(c).Gap(4).Children(func() {
					if m.ToolURL != "" {
						mui.Link(c, m.ToolURL, m.ToolURL).FontSize(a.chatFont(10)).Wrap()
					}
					if m.Detail != "" {
						mutedText(c, m.Detail, a.chatFont(11)).Selectable()
					}
				})
			})
		}).PaddingStart(16)
	})
}

// toolURLHost is the part of a URL a header can name, falling back to the URL
// itself when it does not parse.
func toolURLHost(raw string) string {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Host == "" {
		return strings.TrimSpace(raw)
	}
	return parsed.Host
}

// looksLikeJSON reports whether a body is structured data worth highlighting.
func looksLikeJSON(text string) bool {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return false
	}
	if trimmed[0] != '{' && trimmed[0] != '[' {
		return false
	}
	return json.Valid([]byte(trimmed))
}

// readToolCard draws a file read as a highlighted code block. The header names
// the file and, for a read from an offset, the lines it covers.
func (a *App) readToolCard(c *mui.Context, m *Message, status string, statusColor mui.Color) {
	t := c.Theme()
	path := ""
	if len(m.ToolPaths) > 0 {
		path = m.ToolPaths[0]
	}
	if path == "" {
		path = toolPathFromTitle(m.Title)
	}
	start := m.ToolOffset
	if start < 1 {
		start = 1
	}
	surfaceCard(c, mui.Column(c).Gap(3).MaxWidth(chatMaxWidth).Padding(8, 10)).Children(func() {
		a.subagentLabel(c, m)
		parts := mui.CollapsibleBase(c, &m.DetailOpen)
		parts.Trigger.Grow(1).Gap(6).AlignItems(mui.Center).
			Label(Tf("Toggle tool output: %s", m.Title)).Children(func() {
			mark := "▸"
			if m.DetailOpen {
				mark = "▾"
			}
			mutedText(c, mark, a.chatFont(10)).Width(10).Shrink(0)
			mui.Icon(c, fileIconFor(filepath.Base(path))).Size(14, 14).Shrink(0).TextColor(t.TextMuted)
			sectionLabel(c, T("Read"))
			a.toolPathLabel(c, path)
			if lineRange := toolReadRange(m); lineRange != "" {
				mutedText(c, lineRange, a.chatFont(10)).Shrink(0)
			}
			if elapsed := toolCardDuration(m); elapsed != "" {
				mutedText(c, elapsed, a.chatFont(9)).Shrink(0)
			}
			if status != "" {
				mui.Text(c, status).FontSize(a.chatFont(11)).TextColor(statusColor).Shrink(0)
			}
		})
		parts.Panel(func() {
			tail, hidden := toolTextTail(m.Detail, toolReadMaxLines)
			displayStart := start
			if hidden > 0 {
				a.toolTailToggle(c, m, hidden)
				displayStart += hidden
			}
			if m.toolOutputOpen {
				tail = m.Detail
				displayStart = start
			}
			toolCardScroll(c, func() {
				if len(m.ToolImages) > 0 {
					a.toolImagePanel(c, m)
				}
				a.toolCodeBlock(c, toolLanguageName(path), tail, displayStart)
			})
			a.toolCardActions(c, m)
		}).PaddingStart(16)
	})
}

// searchToolCard draws a search result: the pattern in the header, the match
// count beside it, and the matching lines in a terminal block with the pattern
// highlighted.
func (a *App) searchToolCard(c *mui.Context, m *Message, status string, statusColor mui.Color) {
	t := c.Theme()
	surfaceCard(c, mui.Column(c).Gap(3).MaxWidth(chatMaxWidth).Padding(8, 10)).Children(func() {
		a.subagentLabel(c, m)
		parts := mui.CollapsibleBase(c, &m.DetailOpen)
		parts.Trigger.Grow(1).Gap(6).AlignItems(mui.Center).
			Label(Tf("Toggle tool output: %s", m.Title)).Children(func() {
			mark := "▸"
			if m.DetailOpen {
				mark = "▾"
			}
			mutedText(c, mark, a.chatFont(10)).Width(10).Shrink(0)
			mui.Icon(c, folderTreeIcon).Size(14, 14).Shrink(0).TextColor(t.TextMuted)
			sectionLabel(c, T("Search"))
			mui.Text(c, m.Title).FontSize(a.chatFont(12)).Grow(1).MinWidth(0).SingleLine()
			if matches, files, ok := toolSearchStats(m.Detail); ok {
				mutedText(c, toolSearchLabel(matches, files), a.chatFont(10)).Shrink(0)
			}
			if elapsed := toolCardDuration(m); elapsed != "" {
				mutedText(c, elapsed, a.chatFont(9)).Shrink(0)
			}
			if status != "" {
				mui.Text(c, status).FontSize(a.chatFont(11)).TextColor(statusColor).Shrink(0)
			}
		})
		parts.Panel(func() {
			tail, hidden := toolTextTail(m.Detail, toolOutputMaxLines)
			if hidden > 0 {
				a.toolTailToggle(c, m, hidden)
			}
			if m.toolOutputOpen {
				tail = m.Detail
			}
			toolCardScroll(c, func() {
				a.toolSearchBlock(c, tail, m.ToolPattern)
			})
		}).PaddingStart(16)
	})
}

// toolReadMaxLines is the file text a read card shows before the rest is
// asked for.
const toolReadMaxLines = 300

// toolReadRange is the line range a read covers, empty for a whole file.
func toolReadRange(m *Message) string {
	if m.ToolOffset <= 0 && m.ToolLimit <= 0 {
		return ""
	}
	start := m.ToolOffset
	if start < 1 {
		start = 1
	}
	if m.ToolLimit > 0 {
		return strconv.Itoa(start) + "–" + strconv.Itoa(start+m.ToolLimit-1)
	}
	return strconv.Itoa(start)
}

// toolPathFromTitle recovers a path from a title such as "read a.go" when the
// agent sent no locations.
func toolPathFromTitle(title string) string {
	title = strings.TrimSpace(title)
	if quoted := toolQuotedPath.FindStringSubmatch(title); quoted != nil {
		return quoted[1]
	}
	fields := strings.Fields(title)
	if len(fields) < 2 {
		return ""
	}
	rest := strings.Trim(strings.TrimSpace(title[len(fields[0]):]), "'\"`")
	if strings.HasPrefix(strings.ToLower(rest), "file ") {
		rest = strings.TrimSpace(rest[len("file "):])
	}
	return strings.Trim(rest, "'\"`")
}

// toolQuotedPath finds a path the agent wrapped in quotes, as codex writes
// "Read file 'a.go'".
var toolQuotedPath = regexp.MustCompile("['\"`]([^'\"`]+)['\"`]")

// toolLanguageName is the editor language of a path, for syntax highlighting.
func toolLanguageName(path string) string {
	if language := editor.LanguageFor(path); language != nil {
		return language.Name
	}
	return ""
}

// toolSearchStats counts `path:line:` result rows and the files they name.
func toolSearchStats(text string) (matches, files int, ok bool) {
	seen := make(map[string]bool)
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		match := toolSearchRow.FindStringSubmatch(line)
		if match == nil {
			continue
		}
		matches++
		if !seen[match[1]] {
			seen[match[1]] = true
			files++
		}
	}
	return matches, files, matches > 0
}

var toolSearchRow = regexp.MustCompile(`^(.+?):\d+:`)

// toolSearchLabel names the match count, with singular forms so the header does
// not read "1 matches".
func toolSearchLabel(matches, files int) string {
	switch {
	case matches == 1 && files == 1:
		return T("1 match in 1 file")
	case matches == 1:
		return Tf("1 match in %d files", files)
	case files == 1:
		return Tf("%d matches in 1 file", matches)
	default:
		return Tf("%d matches in %d files", matches, files)
	}
}

// toolTailToggle shows the lines a card left out, and hides them again.
func (a *App) toolTailToggle(c *mui.Context, m *Message, hidden int) {
	label := Tf("Show %d earlier lines", hidden)
	if m.toolOutputOpen {
		label = T("Show less")
	}
	button := quietButton(c, label).FontSize(a.chatFont(9)).Padding(2, 6)
	if button.Clicked() {
		m.toolOutputOpen = !m.toolOutputOpen
	}
}

// toolCodeBlock draws file text as numbered, highlighted code on the window's
// background.
func (a *App) toolCodeBlock(c *mui.Context, lang, code string, start int) {
	t := c.Theme()
	if strings.TrimSpace(code) == "" {
		return
	}
	mui.Column(c).FillWidth().Padding(6, 8).Radius(8).Background(t.Background).Children(func() {
		codeLinesFrom(c, lang, code, start)
	})
}

// toolSearchBlock draws search rows in monospace and highlights the pattern in
// them.
func (a *App) toolSearchBlock(c *mui.Context, text, pattern string) {
	t := c.Theme()
	if strings.TrimSpace(text) == "" {
		return
	}
	mui.Column(c).FillWidth().Padding(6, 8).Radius(8).Background(t.Background).Children(func() {
		for i, line := range strings.Split(text, "\n") {
			c.Key(i)
			mui.RichText(c, toolSearchSpans(line, pattern, t)...).
				Font("monospace").FontSize(a.chatFont(10)).LineHeight(1.5).Wrap()
		}
	})
}

// toolSearchSpans highlights every occurrence of pattern in one result line.
func toolSearchSpans(line, pattern string, t *mui.Theme) []mui.Span {
	if pattern == "" || line == "" {
		return []mui.Span{{Text: line, Color: t.Text}}
	}
	var spans []mui.Span
	for {
		before, after, found := strings.Cut(line, pattern)
		if !found {
			if line != "" {
				spans = append(spans, mui.Span{Text: line, Color: t.Text})
			}
			return spans
		}
		if before != "" {
			spans = append(spans, mui.Span{Text: before, Color: t.Text})
		}
		spans = append(spans, mui.Span{Text: pattern, Color: t.Warning, Background: t.Warning.Alpha(0.16)})
		line = after
	}
}

// executeToolCard draws a command and its output: the command in monospace on
// the header, the output in a terminal block below it. The exit code is the
// header's status, and a non-zero code reads as a failure even when the agent
// reported the call as completed.
func (a *App) executeToolCard(c *mui.Context, m *Message, status string, statusColor mui.Color) {
	t := c.Theme()
	command := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(m.Title), "$ "))
	surfaceCard(c, mui.Column(c).Gap(3).MaxWidth(chatMaxWidth).Padding(8, 10)).Children(func() {
		a.subagentLabel(c, m)
		parts := mui.CollapsibleBase(c, &m.DetailOpen)
		parts.Trigger.Grow(1).Gap(6).AlignItems(mui.Center).
			Label(Tf("Toggle tool output: %s", m.Title)).Children(func() {
			mark := "▸"
			if m.DetailOpen {
				mark = "▾"
			}
			mutedText(c, mark, a.chatFont(10)).Width(10).Shrink(0)
			mui.Icon(c, terminalIcon).Size(14, 14).Shrink(0).TextColor(t.TextMuted)
			mutedText(c, "$", a.chatFont(11)).Font("monospace").Shrink(0)
			commandElement := mui.Text(c, command).Font("monospace").FontSize(a.chatFont(12)).Grow(1).MinWidth(0)
			if !m.DetailOpen {
				commandElement.SingleLine()
			}
			if elapsed := toolCardDuration(m); elapsed != "" {
				mutedText(c, elapsed, a.chatFont(9)).Shrink(0)
			}
			if m.ToolExit != nil {
				a.toolExitBadge(c, *m.ToolExit)
			} else if status != "" {
				mui.Text(c, status).FontSize(a.chatFont(11)).TextColor(statusColor).Shrink(0)
			}
		})
		parts.Panel(func() {
			if m.Detail == "" {
				return
			}
			tail, hidden := toolTextTail(m.Detail, toolOutputMaxLines)
			if hidden > 0 {
				a.toolTailToggle(c, m, hidden)
				if !m.toolOutputOpen {
					tail = "…\n" + tail
				}
			}
			if m.toolOutputOpen {
				tail = m.Detail
			}
			toolCardScroll(c, func() {
				a.toolOutputBlock(c, tail)
			})
			mui.Row(c).Gap(6).AlignItems(mui.Center).Children(func() {
				if cwd := strings.TrimSpace(m.ToolCwd); cwd != "" {
					mutedText(c, cwd, a.chatFont(9)).Grow(1).MinWidth(0).SingleLine()
				}
				copyButton(c, command)
			})
		}).PaddingStart(16)
	})
}

// toolOutputMaxLines is the tail a command card shows before its earlier lines
// are asked for. Terminals are read from the end, and a build log's last lines
// are the result.
const toolOutputMaxLines = 200

// toolOutputTail returns the last max lines of text and how many lines were
// left out.
func toolTextTail(text string, max int) (string, int) {
	text = strings.TrimRight(text, "\n")
	lines := strings.Split(text, "\n")
	if max <= 0 || len(lines) <= max {
		return text, 0
	}
	hidden := len(lines) - max
	return strings.Join(lines[hidden:], "\n"), hidden
}

// toolOutputBlock draws command output as terminal text: monospace, wrapped,
// selectable, on the window's own background inside the card's surface.
func (a *App) toolOutputBlock(c *mui.Context, text string) {
	t := c.Theme()
	if strings.TrimSpace(text) == "" {
		return
	}
	mui.Column(c).FillWidth().Padding(6, 8).Radius(8).Background(t.Background).Children(func() {
		mui.Text(c, text).Font("monospace").FontSize(a.chatFont(10)).
			LineHeight(1.5).Wrap().Selectable()
	})
}

// toolExitBadge is a command's exit code: success when zero, failure otherwise.
func (a *App) toolExitBadge(c *mui.Context, code int) {
	t := c.Theme()
	color := t.Success
	if code != 0 {
		color = t.Danger
	}
	mui.Text(c, Tf("exit %d", code)).FontSize(a.chatFont(10)).TextColor(color).
		Background(color.Alpha(0.1)).Padding(1, 6).Radius(999).Shrink(0)
}

// toolCardActions offers the chat-side actions of a file tool: copy the change
// and open the file in the Files viewer. The buttons live in the panel, not in
// the header, so a click never doubles as the card's open/close.
func (a *App) toolCardActions(c *mui.Context, m *Message) {
	path := toolDiffPath(m)
	if path == "" {
		path = toolFirstPath(m)
	}
	copyText := toolDiffText(m.ToolDiffs)
	mui.Row(c).Gap(6).AlignItems(mui.Center).Wrap().Children(func() {
		if copyText != "" {
			copyButton(c, copyText)
		}
		a.toolOpenFileButton(c, path)
	})
}

// toolFirstPath is the tool's own file, for a read that carries no diff.
func toolFirstPath(m *Message) string {
	if len(m.ToolPaths) > 0 {
		return m.ToolPaths[0]
	}
	return toolPathFromTitle(m.Title)
}

// toolDiffText is the change as a patch the user can copy: each file's old
// lines as -, its new lines as +. Hunks keep the agent's own boundaries.
func toolDiffText(diffs []ToolDiff) string {
	if len(diffs) == 0 {
		return ""
	}
	var b strings.Builder
	for _, diff := range diffs {
		path := diff.Path
		if path == "" {
			path = "?"
		}
		b.WriteString("--- " + path + "\n+++ " + path + "\n")
		for _, line := range strings.Split(strings.TrimSuffix(diff.Old, "\n"), "\n") {
			if line != "" || strings.TrimSpace(diff.Old) != "" {
				b.WriteString("-" + line + "\n")
			}
		}
		for _, line := range strings.Split(strings.TrimSuffix(diff.New, "\n"), "\n") {
			if line != "" || strings.TrimSpace(diff.New) != "" {
				b.WriteString("+" + line + "\n")
			}
		}
	}
	return b.String()
}

// toolOpenFileButton opens the tool's file in the Files viewer, switching the
// right column to it. A path outside the workspace, or no workspace at all,
// shows no button.
func (a *App) toolOpenFileButton(c *mui.Context, path string) {
	entry, ok := a.toolWorkspaceEntry(path)
	if !ok {
		return
	}
	button := quietButton(c, T("Open file")).FontSize(a.chatFont(9)).Padding(2, 6)
	if button.Clicked() {
		a.GitPanelOpen = true
		a.RightTab = rightTabFiles
		a.ensureFilePanel()
		a.openFileViewer(entry)
	}
}

// toolWorkspaceEntry maps a tool path onto the Files panel's workspace-relative
// entry, refusing paths that leave the workspace.
func (a *App) toolWorkspaceEntry(path string) (files.Entry, bool) {
	workspace := a.activeFileWorkspace()
	path = strings.TrimSpace(path)
	if workspace == "" || path == "" {
		return files.Entry{}, false
	}
	rel := path
	if filepath.IsAbs(path) {
		inside, err := filepath.Rel(workspace, path)
		if err != nil || inside == ".." || strings.HasPrefix(inside, ".."+string(filepath.Separator)) {
			return files.Entry{}, false
		}
		rel = inside
	}
	rel = strings.TrimPrefix(filepath.ToSlash(rel), "./")
	if rel == "" || rel == "." {
		return files.Entry{}, false
	}
	return files.Entry{Path: rel}, true
}

// toolPathLabel draws the call's target: the directory muted and the file name
// bold, on one line.
func (a *App) toolPathLabel(c *mui.Context, path string) {
	name := toolDiffDisplayPath(a.activeFileWorkspace(), path)
	dir, base := filepath.Split(name)
	row := mui.Row(c).Gap(4).AlignItems(mui.Center).Grow(1).MinWidth(0)
	row.Children(func() {
		if dir != "" {
			mutedText(c, strings.TrimSuffix(dir, "/")+"/", a.chatFont(10)).
				SingleLine().Shrink(1).MinWidth(0)
		}
		mui.Text(c, base).Bold().FontSize(a.chatFont(12)).SingleLine().Shrink(0)
	})
}

// toolStatsLabel draws +N / −M for a change. A zero side is left out.
func (a *App) toolStatsLabel(c *mui.Context, stats DiffStats) {
	t := c.Theme()
	if stats.Added > 0 {
		mui.Text(c, "+"+strconv.Itoa(stats.Added)).FontSize(a.chatFont(10)).
			TextColor(t.Success).Shrink(0)
	}
	if stats.Removed > 0 {
		mui.Text(c, "−"+strconv.Itoa(stats.Removed)).FontSize(a.chatFont(10)).
			TextColor(t.Danger).Shrink(0)
	}
}

// toolDiffVerb names the change, and stays generic when one call mixes
// operations (codex sends one block per file).
func toolDiffVerb(m *Message) string {
	operation := ""
	for _, diff := range m.ToolDiffs {
		if operation == "" {
			operation = diff.Operation
			continue
		}
		if operation != diff.Operation {
			return T("Edited")
		}
	}
	switch operation {
	case "add":
		return T("Created")
	case "delete":
		return T("Deleted")
	case "move":
		return T("Moved")
	default:
		return T("Edited")
	}
}

func toolDiffIcon(m *Message) *mui.SVG {
	operation := ""
	for _, diff := range m.ToolDiffs {
		if operation == "" {
			operation = diff.Operation
			continue
		}
		if operation != diff.Operation {
			return codeIcon
		}
	}
	switch operation {
	case "add":
		return filePlusIcon
	case "move":
		return gitCompareIcon
	default:
		return codeIcon
	}
}

// toolDiffPath is the path the header names: the first change's, or the agent's
// title when the blocks carry none.
func toolDiffPath(m *Message) string {
	for _, diff := range m.ToolDiffs {
		if diff.Path != "" {
			return diff.Path
		}
	}
	if len(m.ToolPaths) > 0 {
		return m.ToolPaths[0]
	}
	return m.Title
}

// toolDiffDisplayPath shortens an absolute path to the workspace-relative one,
// so the header names the file the conversation works on.
func toolDiffDisplayPath(workspace, path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	if workspace != "" && filepath.IsAbs(path) {
		if rel, err := filepath.Rel(workspace, path); err == nil && !strings.HasPrefix(rel, "..") {
			return filepath.ToSlash(rel)
		}
	}
	return filepath.ToSlash(path)
}

// toolDiffStats counts the change once per diff text and font, and reads it
// from the same state the panel draws.
func (m *Message) toolDiffStats(font float32) DiffStats {
	var stats DiffStats
	for _, state := range m.toolDiffStates(font) {
		added, removed := state.Stats()
		stats.Added += added
		stats.Removed += removed
	}
	return stats
}

// toolDiffStates builds one diff viewer state per change and keeps it while the
// diffs and the font stay the same. A change larger than toolDiffInlineMaxLines
// scrolls inside the card instead of fitting its content.
func (m *Message) toolDiffStates(font float32) []*editor.DiffState {
	if len(m.toolDiffState) == len(m.ToolDiffs) && m.toolDiffFont == font &&
		toolDiffsEqual(m.toolDiffSource, m.ToolDiffs) {
		return m.toolDiffState
	}
	inline := toolDiffLineCount(m.ToolDiffs) <= toolDiffInlineMaxLines
	states := make([]*editor.DiffState, len(m.ToolDiffs))
	for i, diff := range m.ToolDiffs {
		oldBuffer := editor.NewBuffer(diff.Old)
		newBuffer := editor.NewBuffer(diff.New)
		if lang := editor.LanguageFor(diff.Path); lang != nil {
			oldBuffer.SetLanguage(lang)
			newBuffer.SetLanguage(lang)
		}
		state := &editor.DiffState{
			Old:        oldBuffer,
			New:        newBuffer,
			Layout:     editor.DiffUnified,
			Inline:     editor.DiffInlineWordAlt,
			Wrap:       true,
			FitContent: inline,
			LineHeight: 1.4,
			Font:       mui.Font{Size: font},
			// A hunk carries no offsets, so its line numbers would be the
			// block's, not the file's.
			NoLineNumbers: diff.Partial,
		}
		if !diff.Partial {
			state.ContextLines = 3
		} else {
			state.ContextLines = -1
		}
		states[i] = state
	}
	m.toolDiffState = states
	m.toolDiffSource = append([]ToolDiff(nil), m.ToolDiffs...)
	m.toolDiffFont = font
	return states
}

func toolDiffsEqual(a, b []ToolDiff) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func toolDiffLineCount(diffs []ToolDiff) int {
	total := 0
	for _, diff := range diffs {
		total += strings.Count(diff.Old, "\n") + strings.Count(diff.New, "\n") + 2
	}
	return total
}

// toolCardDuration is the runtime shown in a tool card's header. Calls shorter
// than a tenth of a second keep the header quiet.
func toolCardDuration(m *Message) string {
	if m.ToolStart.IsZero() || m.ToolEnd.IsZero() {
		return ""
	}
	elapsed := m.ToolEnd.Sub(m.ToolStart)
	if elapsed < 100*time.Millisecond {
		return ""
	}
	return formatElapsed(elapsed)
}

// toolStatusColor is the ink of a call's status: success, failure, or the
// warning of work in progress.
func toolStatusColor(t *mui.Theme, status string) mui.Color {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "completed", "complete":
		return t.Success
	case "failed", "error":
		return t.Danger
	case "in_progress", "running", "pending":
		return t.Warning
	default:
		return t.TextMuted
	}
}
