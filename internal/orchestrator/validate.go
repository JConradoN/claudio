package orchestrator

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/igormaneschy/aurelia/internal/bridge"
)

// ValidationResult holds the outcome of Aurelia's quality gate review.
type ValidationResult struct {
	Approved    bool     `json:"approved"`
	Issues      []string `json:"issues,omitempty"`
	ShouldRetry bool     `json:"should_retry"`
}

// Validate calls Aurelia to review a worker's result against the task criteria.
// Returns ValidationResult with approval status and any issues found.
func (o *Orchestrator) Validate(
	ctx context.Context,
	task Task,
	result TaskResult,
	validationPrompt string,
) (*ValidationResult, error) {
	if !result.Success {
		return &ValidationResult{
			Approved:    false,
			Issues:      []string{fmt.Sprintf("worker failed: %s", result.Error)},
			ShouldRetry: true,
		}, nil
	}

	req := bridge.Request{
		Command: "query",
		Prompt:  buildValidationUserPrompt(task, result),
		Options: bridge.RequestOptions{
			SystemPrompt:   validationPrompt,
			NoUserSettings: true,
			Cwd:            o.config.RepoRoot,
		},
	}

	ev, err := o.bridge.ExecuteSync(ctx, req)
	if err != nil {
		// If validation call fails, approve by default (don't block on infra issues)
		return &ValidationResult{Approved: true}, nil
	}

	content := ev.Content
	if content == "" {
		content = ev.Text
	}

	return parseValidationResponse(content), nil
}

// buildValidationUserPrompt creates the user prompt for the validation call.
func buildValidationUserPrompt(task Task, result TaskResult) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "## Task\n\n")
	fmt.Fprintf(&sb, "**ID**: %s\n", task.ID)
	fmt.Fprintf(&sb, "**Description**: %s\n", task.Description)
	fmt.Fprintf(&sb, "**Prompt**: %s\n\n", task.Prompt)
	fmt.Fprintf(&sb, "## Worker Result\n\n")
	fmt.Fprintf(&sb, "%s\n\n", result.Content)
	fmt.Fprintf(&sb, "## Instructions\n\n")
	fmt.Fprintf(&sb, "Review the worker's result against the task requirements.\n")
	fmt.Fprintf(&sb, "Respond with a JSON object:\n")
	fmt.Fprintf(&sb, "```json\n")
	fmt.Fprintf(&sb, `{"approved": true/false, "issues": ["issue1", "issue2"], "should_retry": true/false}`)
	fmt.Fprintf(&sb, "\n```\n")
	return sb.String()
}

// parseValidationResponse extracts a ValidationResult from Aurelia's response.
// Tries to parse JSON; falls back to heuristic (approved if contains "approved": true).
func parseValidationResponse(content string) *ValidationResult {
	// Try to extract JSON from the response
	jsonStr := extractJSON(content)
	if jsonStr != "" {
		var vr ValidationResult
		if err := json.Unmarshal([]byte(jsonStr), &vr); err == nil {
			return &vr
		}
	}

	// Heuristic fallback
	lower := strings.ToLower(content)
	if strings.Contains(lower, "approved") && !strings.Contains(lower, "not approved") {
		return &ValidationResult{Approved: true}
	}

	return &ValidationResult{
		Approved:    false,
		Issues:      []string{"could not parse validation response"},
		ShouldRetry: true,
	}
}

// extractJSON finds the first JSON object in a string (between { and }).
func extractJSON(s string) string {
	start := strings.Index(s, "{")
	if start == -1 {
		return ""
	}
	// Find matching closing brace
	depth := 0
	for i := start; i < len(s); i++ {
		switch s[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return s[start : i+1]
			}
		}
	}
	return ""
}
