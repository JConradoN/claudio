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
