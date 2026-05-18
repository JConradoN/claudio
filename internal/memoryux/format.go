package memoryux

import (
	"fmt"
	"strings"
	"time"
)

// FormatStatus renders the memory status as a human-readable string for Telegram.
func FormatStatus(s Status) string {
	var b strings.Builder
	b.WriteString("🧠 **Memory Status**\n\n")

	if s.CWD != "" {
		fmt.Fprintf(&b, "📂 CWD: `%s`\n\n", s.CWD)
	}

	b.WriteString("**Active Layers:**\n")
	for _, l := range s.Layers {
		status := "❌ not created yet"
		if l.Exists {
			age := ""
			if !l.LatestMod.IsZero() {
				age = fmt.Sprintf(" (last modified %s)", l.LatestMod.Format("2006-01-02 15:04"))
			}
			files := fmt.Sprintf("%d .md files", l.MarkdownFiles)
			status = fmt.Sprintf("✅ %s%s", files, age)
		}
		fmt.Fprintf(&b, "• **%s** (%s): %s\n", l.Name, l.Scope, status)
		fmt.Fprintf(&b, "  `%s`\n", l.Dir)
	}

	b.WriteString("\n")
	layerName := s.CheckpointLayer
	if layerName == "" {
		layerName = "none"
	}
	fmt.Fprintf(&b, "📝 Checkpoint target: **%s**\n", layerName)

	if s.LatestReceipt != nil {
		r := s.LatestReceipt
		b.WriteString("\n**Last Memory Activity:**\n")

		statusEmoji := map[string]string{
			"applied": "✅",
			"noop":    "⏭️",
			"invalid": "⚠️",
			"error":   "❌",
		}
		emoji := statusEmoji[r.Status]
		if emoji == "" {
			emoji = "❓"
		}

		summary := fmt.Sprintf("%s • %s: %s %d/%d",
			emoji, r.Source, r.Status, r.Applied, r.Total)
		if r.Duration != "" {
			summary += fmt.Sprintf(", duration %s", r.Duration)
		}
		if r.CostUSD > 0 {
			summary += fmt.Sprintf(", cost $%.4f", r.CostUSD)
		}
		if r.Turns > 0 {
			summary += fmt.Sprintf(", turns %d", r.Turns)
		}
		b.WriteString(summary)
		b.WriteString("\n")
	} else {
		b.WriteString("\n**Last Memory Activity:**\nNo memory activity recorded yet.\n")
	}

	b.WriteString("\n💡 Use `/memory checkpoint [note]` to write a task checkpoint.")
	return b.String()
}

// FormatCheckpoint renders the checkpoint result for Telegram.
func FormatCheckpoint(r CheckpointResult) string {
	action := "updated"
	if r.Created {
		action = "created"
	}
	return fmt.Sprintf("✅ Checkpoint %s\n\n📝 Layer: **%s**\n📁 `%s`\n🕐 %s",
		action, r.Layer, r.Path, r.UpdatedAt.Format(time.RFC3339))
}
