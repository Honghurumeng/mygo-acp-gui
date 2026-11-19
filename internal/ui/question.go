package ui

import (
	"encoding/json"
	"strconv"
	"strings"

	mui "github.com/egoist/mygo/ui"
	"github.com/reed/acp-gui/internal/acp"
	"github.com/reed/acp-gui/internal/session"
)

// askQuestion is a Command Code ask_user_question request seen through ACP.
// The agent bridges the tool to session/request_permission: rawInput carries
// the question and the plain option labels, while every permission option is
// named "label: description" and its id is option_<index>. The generic
// permission dialog would show that transport structure, so these requests
// get their own panel above the composer.
type askQuestion struct {
	Header   string
	Question string
	Options  []askQuestionOption
}

type askQuestionOption struct {
	OptionID    string
	Label       string
	Description string
}

type askQuestionInput struct {
	Question string   `json:"question"`
	Options  []string `json:"options"`
}

// parseAskQuestion recognizes the request shape Command Code emits for
// ask_user_question. Anything unexpected keeps the generic permission dialog,
// so another tool that happens to carry a question field is never answered by
// accident.
func parseAskQuestion(req session.PermissionRequest) (askQuestion, bool) {
	if req.ToolCall.Kind != "other" || len(req.Options) == 0 {
		return askQuestion{}, false
	}
	var input askQuestionInput
	if err := json.Unmarshal(req.ToolCall.RawInput, &input); err != nil {
		return askQuestion{}, false
	}
	question := strings.TrimSpace(input.Question)
	if question == "" || len(input.Options) == 0 {
		return askQuestion{}, false
	}
	out := askQuestion{Question: question}
	for _, option := range req.Options {
		index, ok := askQuestionOptionIndex(option.OptionID)
		if !ok || index >= len(input.Options) {
			return askQuestion{}, false
		}
		label := strings.TrimSpace(input.Options[index])
		if label == "" {
			label = strings.TrimSpace(option.Name)
		}
		out.Options = append(out.Options, askQuestionOption{
			OptionID:    option.OptionID,
			Label:       label,
			Description: askQuestionOptionDescription(option.Name, label),
		})
	}
	out.Header = askQuestionHeader(req.ToolCall.Title, question)
	return out, true
}

// askQuestionOptionIndex reads the question index from an option id such as
// option_2.
func askQuestionOptionIndex(id string) (int, bool) {
	rest, ok := strings.CutPrefix(id, "option_")
	if !ok || rest == "" {
		return 0, false
	}
	index, err := strconv.Atoi(rest)
	if err != nil || index < 0 {
		return 0, false
	}
	return index, true
}

// askQuestionOptionDescription splits the description the agent appended to
// the label. A permission option without that suffix keeps its full name as
// the description so no text is lost.
func askQuestionOptionDescription(name, label string) string {
	name = strings.TrimSpace(name)
	if name == "" || name == label {
		return ""
	}
	if detail, ok := strings.CutPrefix(name, label+": "); ok {
		return strings.TrimSpace(detail)
	}
	return name
}

// askQuestionHeader recovers the short header the agent puts before the
// question in the tool call title.
func askQuestionHeader(title, question string) string {
	title = strings.TrimSpace(title)
	if title == "" || title == question {
		return ""
	}
	if header, ok := strings.CutSuffix(title, ": "+question); ok {
		return strings.TrimSpace(header)
	}
	return ""
}

// questionPanel answers ask_user_question above the composer instead of in
// the permission dialog: the card spans the composer's width, the question and
// every option wrap, and the agent's JSON transport stays hidden.
func (a *App) questionPanel(c *mui.Context, chat *Chat) {
	p := a.Permission
	if !a.PermissionOpen || p == nil || p.Chat != chat {
		return
	}
	question, ok := parseAskQuestion(p.Request)
	if !ok {
		return
	}
	t := c.Theme()
	c.Key("question-panel")
	mui.Column(c).Padding(0, 16, 8, 16).Children(func() {
		mui.Column(c).FillWidth().Gap(8).Padding(10, 12).Radius(12).
			Background(t.Surface).Border(1, t.Border).Children(func() {
			mui.Row(c).Gap(6).AlignItems(mui.Center).Children(func() {
				mui.Text(c, T("Agent question")).FontSize(a.composerFont(9)).Bold().TextColor(t.Accent)
				if question.Header != "" {
					mui.Row(c).Padding(1, 6).Radius(4).Background(t.Background).Children(func() {
						mutedText(c, question.Header, a.composerFont(9))
					})
				}
				mui.Spacer(c)
				mutedText(c, T("Waiting for your answer…"), a.composerFont(9))
			})
			mui.Text(c, question.Question).FontSize(a.composerFont(12))
			mui.Column(c).Gap(4).Children(func() {
				for _, option := range question.Options {
					option := option
					a.questionOption(c, p, option)
				}
			})
			mui.Row(c).Gap(8).AlignItems(mui.Center).Children(func() {
				mutedText(c, T("Pick an option to answer; skip lets the agent decide."), a.composerFont(9)).Grow(1)
				if quietButton(c, T("Skip")).FontSize(a.composerFont(10)).Clicked() {
					a.finishPermission(p, acp.CancelledPermission())
				}
			})
		})
	})
}

// questionOption is one full-width answer row. Label and description share a
// growing column, so long text wraps inside the card instead of stretching the
// row or cutting the answer.
func (a *App) questionOption(c *mui.Context, p *permissionPrompt, option askQuestionOption) {
	t := c.Theme()
	label := option.Label
	if label == "" {
		label = option.OptionID
	}
	button := mui.ButtonBase(c).FillWidth().AlignItems(mui.Start).Padding(7, 10).Radius(8).
		Border(1, t.Border).Label(label)
	fill := t.Background
	switch {
	case button.Pressed():
		fill = t.SurfacePressed
	case button.Hovered():
		fill = t.SurfaceHover
	}
	button.Background(fill)
	button.Children(func() {
		mui.Column(c).Grow(1).MinWidth(0).Gap(1).Children(func() {
			mui.Text(c, label).FontSize(a.composerFont(11)).Bold()
			if option.Description != "" {
				mutedText(c, option.Description, a.composerFont(9))
			}
		})
	})
	if button.Clicked() {
		a.finishPermission(p, acp.SelectedPermission(option.OptionID))
	}
}
