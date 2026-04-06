package orchestrator

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// EnsureClaudeMd checks if CLAUDE.md exists at the repo root.
// If it doesn't exist, creates a minimal template. Never overwrites existing files.
func EnsureClaudeMd(repoRoot string) error {
	path := filepath.Join(repoRoot, "CLAUDE.md")
	if _, err := os.Stat(path); err == nil {
		return nil // already exists — don't overwrite
	}

	content := `# CLAUDE.md

## Development Commands

` + "```bash" + `
# Add your build/test/lint commands here
` + "```" + `

## Workflow

1. Plan — Understand the problem
2. Review — Question the plan
3. Execute — One task at a time, test-first
4. Validate — Run tests, verify
5. Commit — Conventional Commits

## Rules

- Tests required before marking work complete
- Errors treated explicitly — no silent swallowing
- Prefer editing over rewriting
`
	return os.WriteFile(path, []byte(content), 0o644)
}

// EnsureAgentsMd creates or updates AGENTS.md at the repo root with the squad config.
func EnsureAgentsMd(repoRoot string, agents []AgentSummary) error {
	path := filepath.Join(repoRoot, "AGENTS.md")

	var sb strings.Builder
	sb.WriteString("# AGENTS.md\n\n")
	sb.WriteString("Agent orchestration configuration for this project.\n\n")

	sb.WriteString("## Squad\n\n")
	sb.WriteString("| Agent | Description | Tools | Type |\n")
	sb.WriteString("|-------|-------------|-------|------|\n")

	// Always include default worker
	sb.WriteString("| worker (default) | Generic implementation worker | Read, Write, Edit, Bash, Grep, Glob | read-write |\n")

	for _, a := range agents {
		if a.Name == "worker" {
			continue // already listed
		}
		agentType := "read-write"
		if a.ReadOnly {
			agentType = "read-only"
		}
		fmt.Fprintf(&sb, "| %s | %s | %s | %s |\n",
			a.Name, a.Description, strings.Join(a.Tools, ", "), agentType)
	}

	sb.WriteString("\n## Workflow\n\n")
	sb.WriteString("1. **Planning**: Aurelia specifies, designs and creates tasks with the user\n")
	sb.WriteString("2. **Execution**: Workers execute atomic tasks in isolated worktrees\n")
	sb.WriteString("3. **Validation**: Aurelia validates results before accepting\n")
	sb.WriteString("4. **Delivery**: Merge, commit, PR\n\n")

	sb.WriteString("## Conventions\n\n")
	sb.WriteString("- Workers receive: CLAUDE.md + AGENTS.md + spec.md + design.md + task\n")
	sb.WriteString("- Worktrees: `.worktrees/worker-<taskID>/`\n")
	sb.WriteString("- Branches: `worker/<taskID>-<slug>`\n")
	sb.WriteString("- Commits: Conventional Commits (`type(scope): description`)\n")

	return os.WriteFile(path, []byte(sb.String()), 0o644)
}

// ReadFileContent reads a file and returns its content, or empty string if not found.
func ReadFileContent(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(data)
}
