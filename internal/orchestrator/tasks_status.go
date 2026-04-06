package orchestrator

import (
	"fmt"
	"os"
	"strings"
)

// UpdateTasksStatus updates a tasks.md file, marking checkboxes based on results.
// Finds "- [ ]" checkboxes under each task section and marks completed ones as "- [x]".
func UpdateTasksStatus(tasksPath string, results []TaskResult) error {
	data, err := os.ReadFile(tasksPath)
	if err != nil {
		return fmt.Errorf("reading tasks.md: %w", err)
	}

	content := string(data)
	resultMap := make(map[string]TaskResult)
	for _, r := range results {
		resultMap[r.TaskID] = r
	}

	lines := strings.Split(content, "\n")
	var currentTask string

	for i, line := range lines {
		// Detect task header (### T1: ..., ### T2: ...)
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "### ") {
			// Extract task ID from header (e.g., "### T1: Create interface" → "T1")
			header := strings.TrimPrefix(trimmed, "### ")
			if colonIdx := strings.Index(header, ":"); colonIdx > 0 {
				currentTask = strings.TrimSpace(header[:colonIdx])
			}
		}

		// Update checkboxes for completed tasks
		if currentTask != "" {
			if r, ok := resultMap[currentTask]; ok && r.Success {
				if strings.Contains(line, "- [ ]") {
					lines[i] = strings.Replace(line, "- [ ]", "- [x]", 1)
				}
			}
		}
	}

	// Append status summary at the end
	var sb strings.Builder
	sb.WriteString("\n\n---\n\n## Execution Status\n\n")
	for _, r := range results {
		status := "✅ Done"
		if !r.Success {
			status = fmt.Sprintf("❌ Failed: %s", r.Error)
		}
		fmt.Fprintf(&sb, "- **%s**: %s (%.0fms)\n", r.TaskID, status, float64(r.DurationMs))
	}

	updated := strings.Join(lines, "\n") + sb.String()
	return os.WriteFile(tasksPath, []byte(updated), 0o644)
}
