package ui

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/reed/acp-gui/internal/acp"
)

func formatCount(n int) string {
	switch {
	case n >= 1_000_000:
		return trimFloat(float64(n)/1_000_000) + "M"
	case n >= 1_000:
		return trimFloat(float64(n)/1_000) + "k"
	default:
		return strconv.Itoa(n)
	}
}

func trimFloat(v float64) string {
	return strings.TrimRight(strings.TrimRight(strconv.FormatFloat(v, 'f', 1, 64), "0"), ".")
}

func formatTurnUsage(usage *acp.PromptUsage) string {
	if usage == nil {
		return ""
	}
	if usage.InputTokens == 0 && usage.OutputTokens == 0 {
		if usage.TotalTokens > 0 {
			return "turn " + formatCount(usage.TotalTokens) + " tokens"
		}
		return ""
	}
	text := "turn " + formatCount(usage.InputTokens) + " in · " + formatCount(usage.OutputTokens) + " out"
	if usage.ThoughtTokens > 0 {
		text += " · " + formatCount(usage.ThoughtTokens) + " thought"
	}
	return text
}

func formatMoney(cost *acp.Cost) string {
	if cost == nil {
		return ""
	}
	return fmt.Sprintf("%.4f %s", cost.Amount, cost.Currency)
}
