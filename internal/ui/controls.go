package ui

import (
	"fmt"
	"strconv"
	"strings"
	"time"

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

// formatElapsed is the compact runtime of one turn. Sub-minute turns keep one
// decimal below ten seconds, longer turns use minutes and hours so the
// caption stays short even for an all-day task.
func formatElapsed(d time.Duration) string {
	if d <= 0 {
		return ""
	}
	d = d.Round(100 * time.Millisecond)
	if d < 10*time.Second {
		return trimFloat(d.Seconds()) + "s"
	}
	total := int(d.Round(time.Second) / time.Second)
	switch {
	case total < 60:
		return strconv.Itoa(total) + "s"
	case total < 3600:
		return fmt.Sprintf("%dm %02ds", total/60, total%60)
	default:
		minutes := int(d.Round(time.Minute) / time.Minute)
		return fmt.Sprintf("%dh %02dm", minutes/60, minutes%60)
	}
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
