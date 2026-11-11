package ui

import (
	"fmt"
	"math"
	"strings"

	mui "github.com/egoist/mygo/ui"
	"github.com/reed/acp-gui/internal/acp"
)

const (
	defaultComposerHeight = 148
	minComposerHeight     = 120
	maxComposerHeight     = 340
	commandPanelWidth     = 520
)

// composer draws the draggable input card and its row of session controls.
func (a *App) composer(c *mui.Context) {
	chat := a.activeChat()
	if chat == nil {
		return
	}
	t := c.Theme()
	mui.Column(c).Padding(0, 16, 12, 16).Children(func() {
		a.planPanel(c, chat)
		a.attachmentPicker(c, chat)

		card := mui.Column(c).Gap(6).Padding(4, 8, 8, 8).Radius(12).
			Background(t.Surface).Border(1, t.Border).
			Height(chat.composerHeight())
		card.Children(func() {
			grip := mui.Box(c).FillWidth().Height(5).Cursor(mui.CursorResizeNS)
			grip.Draw(func(p *mui.Painter, r mui.Rect) {
				color := t.Border
				if grip.Hovered() {
					color = t.Accent
				}
				w := min(float32(40), r.W)
				p.Fill(mui.Rect{X: r.X + (r.W-w)/2, Y: r.Y + 1, W: w, H: 2}, color, 1)
			})
			if _, dy, held := grip.Dragged(); held && dy != 0 {
				chat.ComposerHeight = clampComposer(chat.ComposerHeight - dy)
			}

			mui.TextAreaBase(c, &chat.Input).
				Placeholder("Message").
				Label("Message").
				Lines(1, 20).
				Grow(1).
				Padding(2, 2).
				Disabled(!chat.canType()).
				OnTextKey(func(k mui.TextKey) bool {
					if k.Key == mui.KeyEnter && k.Mods == 0 {
						chat.Input = k.Text()
						if a.send() {
							k.SetText("")
						}
						return true
					}
					return false
				})

			mui.Row(c).Gap(6).AlignItems(mui.Center).Children(func() {
				attach := mui.Button(c, "＋").Label("Attach images").FontSize(14).Tooltip("Attach images")
				attach.Disabled(!chat.canAttach())
				if attach.Clicked() {
					a.chooseImages()
				}

				if branch := chat.gitBranch(); branch != "" {
					chip := a.composerChip(c)
					chip.Children(func() {
						mui.Text(c, "⑂ "+truncate(branch, 24)).FontSize(9).SingleLine()
					})
					chip.Tooltip("Git branch in " + chat.Workspace)
				}

				a.modeChip(c, chat)
				if text := formatTurnUsage(chat.TurnUsage); text != "" {
					mui.Text(c, text).FontSize(9).TextColor(t.TextMuted).SingleLine()
				}
				mui.Spacer(c)
				a.usageChip(c, chat)
				a.modelChip(c, chat)
				a.sendButton(c, chat)
			})
		})
	})
}

func (a *App) composerChip(c *mui.Context) mui.Element {
	t := c.Theme()
	return mui.ButtonBase(c).Padding(3, 8).Gap(3).Radius(8).
		Background(t.Background).Border(1, t.Border)
}

func (c *Chat) composerHeight() float32 {
	if c == nil || c.ComposerHeight <= 0 {
		return defaultComposerHeight
	}
	return clampComposer(c.ComposerHeight)
}

func clampComposer(v float32) float32 {
	if v < minComposerHeight {
		return minComposerHeight
	}
	if v > maxComposerHeight {
		return maxComposerHeight
	}
	return v
}

func (a *App) modeChip(c *mui.Context, chat *Chat) {
	if len(chat.Info.Modes) == 0 {
		return
	}
	current := ""
	for _, mode := range chat.Info.Modes {
		if mode.ID == chat.Info.CurrentModeID {
			current = modeLabel(mode)
			break
		}
	}
	if current == "" {
		current = modeLabel(chat.Info.Modes[0])
	}
	chip := a.composerChip(c)
	chip.Children(func() {
		mui.Text(c, current).FontSize(9).SingleLine()
	})
	chip.Tooltip("Session mode")
	chip.Disabled(chat.ConfigBusy != "" || chat.Connecting || chat.Streaming || chat.Busy != "" || chat.Info.SessionID == "")
	if chip.Clicked() {
		chat.ModeOpen = !chat.ModeOpen
	}
	mui.Popover(c, chip, &chat.ModeOpen, func() {
		mui.Column(c).Width(170).Gap(1).Children(func() {
			for _, mode := range chat.Info.Modes {
				mode := mode
				item := mui.ButtonBase(c).FillWidth().Padding(4, 8).Radius(6)
				if mode.ID == chat.Info.CurrentModeID {
					item.Background(c.Theme().Surface)
				}
				item.Children(func() {
					mui.Text(c, modeLabel(mode)).FontSize(11).SingleLine()
				})
				if mode.Description != "" {
					item.Tooltip(mode.Description)
				}
				if item.Clicked() && mode.ID != chat.Info.CurrentModeID {
					a.setMode(mode.ID)
					chat.ModeOpen = false
				}
			}
		})
	})
}

func modeLabel(mode acp.Mode) string {
	if mode.Name != "" {
		return mode.Name
	}
	return mode.ID
}

func (a *App) modelChip(c *mui.Context, chat *Chat) {
	model := ""
	thinking := ""
	if option := chat.modelOption(); option != nil {
		model = configChoiceName(option, option.CurrentValue)
	}
	if model == "" {
		if chat.Session != nil {
			model = "Model"
		} else {
			model = chat.agentName()
		}
	}
	if option := chat.thinkingOption(); option != nil {
		thinking = configChoiceName(option, option.CurrentValue)
	}
	if model == "" {
		model = "Agent"
	}
	chip := a.composerChip(c).MaxWidth(230)
	chip.Children(func() {
		mui.Text(c, truncate(model, 28)).FontSize(9).SingleLine()
		if thinking != "" {
			mui.Text(c, "· "+truncate(thinking, 12)).FontSize(9).TextColor(c.Theme().TextMuted).SingleLine()
		}
	})
	chip.Tooltip("Agent, provider, model and thinking")
	if chip.Clicked() {
		chat.ModelOpen = !chat.ModelOpen
	}
	mui.Popover(c, chip, &chat.ModelOpen, func() {
		a.modelPanel(c, chat)
	})
}

func (a *App) modelPanel(c *mui.Context, chat *Chat) {
	t := c.Theme()
	option := chat.modelOption()
	thinking := chat.thinkingOption()
	locked := chat.ConfigBusy != "" || chat.Connecting || chat.Streaming || chat.Busy != ""
	configLocked := locked || chat.Info.SessionID == ""

	mui.Row(c).Height(252).Gap(1).Children(func() {
		a.modelColumn(c, "Agent", 122, func(inner *mui.Context) {
			if len(a.Agents) == 0 {
				mui.Text(inner, "No agents").FontSize(9).TextColor(t.TextMuted)
				return
			}
			agentLocked := chat.Session != nil || chat.Connecting
			for i, spec := range a.Agents {
				i, spec := i, spec
				selected := spec.ID == chat.Agent.ID
				item := a.modelItem(inner, spec.Name, selected, agentLocked)
				if item.Clicked() {
					chat.Agent = spec
					a.AgentIndex = i
					a.preferredAgentID = spec.ID
					chat.WorkspaceError = ""
					chat.ConnectError = ""
					a.persistSettings()
				}
			}
		})

		a.modelColumn(c, "Provider", 112, func(inner *mui.Context) {
			providers := modelProviders(option)
			if len(providers) == 0 {
				mui.Text(inner, "Connect first").FontSize(9).TextColor(t.TextMuted)
				return
			}
			current := chat.currentProvider()
			if !containsString(providers, current) {
				current = providers[0]
				chat.ModelProvider = current
			}
			for _, provider := range providers {
				provider := provider
				item := a.modelItem(inner, provider, provider == current, false)
				if item.Clicked() && provider != current {
					chat.ModelProvider = provider
				}
			}
		})

		a.modelColumn(c, "Model", 162, func(inner *mui.Context) {
			if option == nil {
				mui.Text(inner, "No model option").FontSize(9).TextColor(t.TextMuted)
				return
			}
			provider := chat.currentProvider()
			choices := modelChoices(option, provider)
			if len(choices) == 0 {
				mui.Text(inner, "No models").FontSize(9).TextColor(t.TextMuted)
				return
			}
			for _, choice := range choices {
				choice := choice
				selected := choice.Value == option.CurrentValue
				item := a.modelItem(inner, configChoiceName(option, choice.Value), selected, configLocked)
				if item.Clicked() && !selected {
					chat.ModelProvider = providerOf(choice.Value)
					if chat.ModelProvider == "" {
						chat.ModelProvider = provider
					}
					a.setConfigOption(option.ID, choice.Value)
				}
			}
		})

		a.modelColumn(c, "Thinking", 132, func(inner *mui.Context) {
			if thinking == nil || len(thinking.Options) == 0 {
				mui.Text(inner, "No thinking options").FontSize(9).TextColor(t.TextMuted)
			} else {
				for _, choice := range thinking.Options {
					choice := choice
					selected := choice.Value == thinking.CurrentValue
					item := a.modelItem(inner, configChoiceName(thinking, choice.Value), selected, configLocked)
					if item.Clicked() && !selected {
						a.setConfigOption(thinking.ID, choice.Value)
					}
				}
			}
			for _, extra := range chat.extraConfigOptions() {
				extra := extra
				mui.Text(inner, configOptionLabel(extra)).FontSize(9).Bold().TextColor(t.TextMuted).Padding(6, 4, 0, 4)
				for _, choice := range extra.Options {
					choice := choice
					selected := choice.Value == extra.CurrentValue
					item := a.modelItem(inner, configChoiceName(&extra, choice.Value), selected, configLocked)
					if item.Clicked() && !selected {
						a.setConfigOption(extra.ID, choice.Value)
					}
				}
			}
		})
	})
}

func (a *App) modelColumn(c *mui.Context, title string, width float32, build func(*mui.Context)) {
	t := c.Theme()
	mui.Column(c).Width(width).FillHeight().Gap(2).Padding(4, 4, 6, 4).Children(func() {
		mui.Text(c, title).FontSize(9).Bold().TextColor(t.TextMuted).Padding(0, 4)
		mui.Scroll(c).Grow(1).Gap(1).Children(func() { build(c) })
	})
}

func (a *App) modelItem(c *mui.Context, label string, selected, disabled bool) mui.Element {
	t := c.Theme()
	item := mui.ButtonBase(c).FillWidth().Gap(4).Padding(3, 6).Radius(6).Disabled(disabled)
	switch {
	case selected:
		item.Background(t.SurfaceHover)
	case item.Hovered():
		item.Background(t.Surface)
	}
	item.Children(func() {
		mui.Text(c, truncate(label, 28)).FontSize(10).SingleLine()
		if selected {
			mui.Text(c, "✓").FontSize(9).TextColor(t.Accent)
		}
	})
	return item
}

func (a *App) usageChip(c *mui.Context, chat *Chat) {
	if chat.Usage == nil {
		return
	}
	t := c.Theme()
	pct, known := usagePercent(chat.Usage)
	chip := a.composerChip(c).Padding(3, 7)
	chip.Children(func() {
		ring := mui.Box(c).Size(14, 14)
		ring.Draw(func(p *mui.Painter, r mui.Rect) {
			drawUsageRing(p, r, pct, t.Border, t.Accent)
		})
		label := "—"
		if known {
			label = fmt.Sprintf("%d%%", pct)
		}
		mui.Text(c, label).FontSize(9)
	})
	tip := "Context " + formatCount(chat.Usage.Used) + " · window size not reported"
	if known {
		tip = "Context " + formatCount(chat.Usage.Used)
		if chat.Usage.Size > 0 {
			tip += " / " + formatCount(chat.Usage.Size)
		}
		tip += fmt.Sprintf(" (%d%%)", pct)
		if chat.Usage.Cost != nil && chat.Usage.Cost.Amount > 0 {
			tip += " · " + formatMoney(chat.Usage.Cost)
		}
	}
	chip.Tooltip(tip)
}

func usagePercent(usage *acp.Usage) (int, bool) {
	if usage == nil || usage.Size <= 0 {
		return 0, false
	}
	pct := int(math.Round(float64(usage.Used) / float64(usage.Size) * 100))
	if pct < 0 {
		pct = 0
	}
	if pct > 100 {
		pct = 100
	}
	return pct, true
}

func drawUsageRing(p *mui.Painter, r mui.Rect, percent int, track, fill mui.Color) {
	cx, cy := r.X+r.W/2, r.Y+r.H/2
	radius := min(r.W, r.H)/2 - 1.5
	p.StrokePath((&mui.Path{}).Circle(cx, cy, radius), 2, track)
	if percent <= 0 {
		return
	}
	steps := max(4, percent)
	path := &mui.Path{}
	for i := 0; i <= steps; i++ {
		angle := -math.Pi/2 + 2*math.Pi*float64(percent)/100*float64(i)/float64(steps)
		x := cx + radius*float32(math.Cos(angle))
		y := cy + radius*float32(math.Sin(angle))
		if i == 0 {
			path.MoveTo(x, y)
		} else {
			path.LineTo(x, y)
		}
	}
	p.StrokePath(path, 2, fill)
}

func (a *App) sendButton(c *mui.Context, chat *Chat) {
	t := c.Theme()
	label, background := "Send", t.Accent
	disabled := !chat.canSend() || !chat.hasPrompt()
	if chat.Streaming {
		label, background = "Stop", t.Danger
		disabled = chat.Cancelling
	}
	button := mui.ButtonBase(c).Padding(4, 12).Radius(8).
		Background(background).TextColor(t.AccentText).Label(label).Disabled(disabled)
	button.Children(func() {
		mui.Text(c, label).FontSize(10).Bold()
	})
	if button.Clicked() {
		if chat.Streaming {
			a.stop()
		} else {
			a.send()
		}
	}
}

// commandOpen reports whether the slash-command list should be shown above
// the composer.
func (a *App) commandOpen(chat *Chat) bool {
	if chat == nil || !chat.canType() || len(chat.Commands) == 0 {
		return false
	}
	if !strings.HasPrefix(chat.Input, "/") {
		return false
	}
	return !strings.ContainsAny(chat.Input[1:], " \t\n")
}

func (a *App) commandPalette(c *mui.Context, chat *Chat) {
	t := c.Theme()
	query := strings.ToLower(strings.TrimPrefix(chat.Input, "/"))
	var matches []acp.AvailableCommand
	for _, command := range chat.Commands {
		if query == "" || strings.HasPrefix(strings.ToLower(command.Name), query) {
			matches = append(matches, command)
		}
	}
	if len(matches) == 0 {
		return
	}
	mui.Row(c).Padding(0, 16, 6, 16).Children(func() {
		mui.Column(c).Width(commandPanelWidth).MaxHeight(300).Gap(2).Padding(4).
			Radius(10).Background(t.Background).Border(1, t.Border).
			Children(func() {
				mui.Scroll(c).MaxHeight(280).Gap(1).Children(func() {
					for _, command := range matches {
						command := command
						item := mui.ButtonBase(c).FillWidth().Gap(8).Padding(4, 8).Radius(6)
						item.Children(func() {
							mui.Text(c, "/"+command.Name).FontSize(11).Bold().Shrink(0)
							if command.Description != "" {
								mui.Text(c, truncate(command.Description, 90)).FontSize(9).
									TextColor(t.TextMuted).Grow(1).SingleLine()
							} else {
								mui.Spacer(c)
							}
							if command.Input != nil && command.Input.Hint != "" {
								mui.Text(c, command.Input.Hint).FontSize(9).TextColor(t.TextMuted)
							}
						})
						if item.Clicked() {
							acceptCommand(chat, command)
						}
					}
				})
			})
	})
}

func acceptCommand(chat *Chat, command acp.AvailableCommand) {
	chat.Input = "/" + command.Name
	if command.Input != nil {
		chat.Input += " "
	}
}

// modelOption is the config option that selects the model.
func (c *Chat) modelOption() *acp.ConfigOption {
	if c == nil {
		return nil
	}
	for i := range c.Info.ConfigOptions {
		option := &c.Info.ConfigOptions[i]
		if option.ID == "model" || option.Category == "model" {
			return option
		}
	}
	return nil
}

// thinkingOption is the config option that selects reasoning effort.
func (c *Chat) thinkingOption() *acp.ConfigOption {
	if c == nil {
		return nil
	}
	for _, id := range []string{"thought_level", "thinking", "reasoning", "effort"} {
		if option := c.Info.ConfigOption(id); option != nil {
			return option
		}
	}
	for i := range c.Info.ConfigOptions {
		option := &c.Info.ConfigOptions[i]
		category := strings.ToLower(option.Category)
		id := strings.ToLower(option.ID)
		if strings.Contains(category, "thought") || strings.Contains(category, "reason") ||
			strings.Contains(id, "thought") || strings.Contains(id, "reason") || strings.Contains(id, "effort") {
			return option
		}
	}
	return nil
}

func (c *Chat) extraConfigOptions() []acp.ConfigOption {
	if c == nil {
		return nil
	}
	model := c.modelOption()
	thinking := c.thinkingOption()
	var extras []acp.ConfigOption
	for _, option := range c.Info.ConfigOptions {
		if len(option.Options) == 0 || (option.Type != "" && option.Type != "select") {
			continue
		}
		if model != nil && option.ID == model.ID {
			continue
		}
		if thinking != nil && option.ID == thinking.ID {
			continue
		}
		extras = append(extras, option)
	}
	return extras
}

func (c *Chat) currentProvider() string {
	if c == nil {
		return ""
	}
	if c.ModelProvider != "" {
		return c.ModelProvider
	}
	option := c.modelOption()
	if option == nil {
		return ""
	}
	return providerOf(option.CurrentValue)
}

func modelProviders(option *acp.ConfigOption) []string {
	if option == nil {
		return nil
	}
	var providers []string
	for _, choice := range option.Options {
		provider := providerOf(choice.Value)
		if provider == "" {
			provider = "Models"
		}
		if !containsString(providers, provider) {
			providers = append(providers, provider)
		}
	}
	return providers
}

func modelChoices(option *acp.ConfigOption, provider string) []acp.ConfigOptionValue {
	if option == nil {
		return nil
	}
	var choices []acp.ConfigOptionValue
	for _, choice := range option.Options {
		valueProvider := providerOf(choice.Value)
		if valueProvider == "" {
			valueProvider = "Models"
		}
		if provider == "" || valueProvider == provider {
			choices = append(choices, choice)
		}
	}
	return choices
}

func providerOf(value string) string {
	if idx := strings.IndexAny(value, "/:"); idx > 0 {
		return value[:idx]
	}
	return ""
}

func configChoiceName(option *acp.ConfigOption, value string) string {
	if option != nil {
		for _, choice := range option.Options {
			if choice.Value == value {
				return firstNonEmpty(choice.Name, choice.Value)
			}
		}
	}
	return value
}

func configOptionLabel(option acp.ConfigOption) string {
	return firstNonEmpty(option.Name, option.ID)
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

// canAttach reports whether images can be added to the draft.
func (c *Chat) canAttach() bool {
	if c == nil || c.Closed || c.Connecting || c.Busy != "" {
		return false
	}
	if c.Session == nil {
		return true
	}
	return c.Info.AgentCapabilities.PromptCapabilities.Image &&
		c.Info.SessionID != ""
}

// canType reports whether the composer accepts text. Drafts type before
// their first connect; sending connects and then submits.
func (c *Chat) canType() bool {
	return c != nil && !c.Closed && !c.Connecting && c.Busy == ""
}

func (c *Chat) canSend() bool {
	if c == nil || c.Closed || c.Connecting || c.Streaming || c.Busy != "" || c.ConfigBusy != "" {
		return false
	}
	if c.Session == nil {
		return c.Agent.ID != ""
	}
	return c.Info.SessionID != ""
}

func (c *Chat) hasPrompt() bool {
	return c != nil && (strings.TrimSpace(c.Input) != "" || len(c.Attachments) > 0)
}
