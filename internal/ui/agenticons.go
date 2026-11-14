package ui

import (
	_ "embed"
	"strings"

	mui "github.com/egoist/mygo/ui"
	"github.com/reed/acp-gui/internal/agent"
)

// Agent brand marks, drawn in the text color so they follow light/dark themes.
// Sources: https://pi.dev/logo-auto.svg, the commandcode-vscode extension and
// Lucide's square-terminal for Codex.
//
//go:embed icons/pi.svg
var piIconSVG []byte

//go:embed icons/command-code.svg
var commandCodeIconSVG []byte

//go:embed icons/codex.svg
var codexIconSVG []byte

var (
	piIcon          = mui.MustParseSVG(piIconSVG)
	commandCodeIcon = mui.MustParseSVG(commandCodeIconSVG)
	codexIcon       = mui.MustParseSVG(codexIconSVG)
)

// agentIcon returns the brand mark for a built-in agent. Custom or test
// agents have no icon and show only their name.
func agentIcon(agentID string) *mui.SVG {
	switch strings.TrimSpace(agentID) {
	case agent.PiID:
		return piIcon
	case agent.CommandCodeID:
		return commandCodeIcon
	case agent.CodexID:
		return codexIcon
	}
	return nil
}

// agentIconName is the agent name an icon stands for.
func agentIconName(agentID string) string {
	switch strings.TrimSpace(agentID) {
	case agent.PiID:
		return "pi"
	case agent.CommandCodeID:
		return "Command Code"
	case agent.CodexID:
		return "Codex"
	}
	return ""
}

// agentBadge draws an agent's icon before a conversation title or the model
// chip, so rows from different agents are easy to tell apart. It draws
// nothing for agents without a mark.
func agentBadge(c *mui.Context, agentID string, size float32) {
	icon := agentIcon(agentID)
	name := agentIconName(agentID)
	if icon == nil || name == "" {
		return
	}
	mui.Icon(c, icon).FontSize(size).TextColor(c.Theme().TextMuted).
		Label(name + " agent").Tooltip(name + " agent")
}
