package agents

// Agent represents a loaded agent definition from a markdown file.
type Agent struct {
	Name         string         `yaml:"name"`
	Description  string         `yaml:"description"`
	Model        string         `yaml:"model,omitempty"`
	Schedule     string         `yaml:"schedule,omitempty"`
	Cwd          string         `yaml:"cwd,omitempty"`
	MCPServers   map[string]any `yaml:"mcp_servers,omitempty"`
	AllowedTools    []string       `yaml:"allowed_tools,omitempty"`
	DisallowedTools []string       `yaml:"disallowed_tools,omitempty"`
	MaxTurns        int            `yaml:"max_turns,omitempty"`
	Prompt          string         `yaml:"-"` // body after frontmatter
}

// IsReadOnly reports whether the agent has no write-capable tools.
// It considers both AllowedTools and DisallowedTools.
func (a *Agent) IsReadOnly() bool {
	// Determine the effective set of tools.
	var effective []string
	if len(a.AllowedTools) > 0 {
		effective = make([]string, len(a.AllowedTools))
		copy(effective, a.AllowedTools)
	} else {
		// No allowlist means all built-ins are available before denylist.
		effective = []string{"Read", "Write", "Edit", "Bash", "Grep", "Glob", "LS", "List", "WebSearch", "WebSearchPremium", "WebFetch"}
	}

	// Apply denylist.
	denied := make(map[string]bool, len(a.DisallowedTools))
	for _, t := range a.DisallowedTools {
		denied[t] = true
	}

	for _, t := range effective {
		if denied[t] {
			continue
		}
		if t == "Write" || t == "Edit" || t == "Bash" {
			return false
		}
	}
	return true
}
