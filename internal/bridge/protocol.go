package bridge

// Request sent to Bridge process via stdin as JSON.
type Request struct {
	Command         string         `json:"command"`
	Prompt          string         `json:"prompt,omitempty"`
	RequestID       string         `json:"request_id,omitempty"`
	TargetRequestID string         `json:"target_request_id,omitempty"`
	Options         RequestOptions `json:"options,omitempty"`
}

// ImageAttachment represents a base64-encoded image to send alongside the prompt.
type ImageAttachment struct {
	Path      string `json:"path,omitempty"`
	Data      string `json:"data,omitempty"`
	MediaType string `json:"media_type,omitempty"`
}

// RequestOptions configures how the Bridge executes a query.
//
// The PI SDK does not expose hooks for MaxTurns, PermissionMode, MCP servers,
// sub-agent registries, or per-tool disablement that the legacy Claude SDK
// supported. Those fields were dropped during the migration; revisit if PI
// adds equivalents in a future release.
type RequestOptions struct {
	Provider       string            `json:"provider,omitempty"`
	Model          string            `json:"model,omitempty"`
	Cwd            string            `json:"cwd,omitempty"`
	SystemPrompt   string            `json:"system_prompt,omitempty"`
	Resume         string            `json:"resume,omitempty"`
	AllowedTools    []string          `json:"allowed_tools,omitempty"`
	DisallowedTools []string          `json:"disallowed_tools,omitempty"`
	Continue        bool              `json:"continue,omitempty"`
	NoUserSettings bool              `json:"no_user_settings,omitempty"`
	PersistSession *bool             `json:"persist_session,omitempty"`
	Images         []ImageAttachment `json:"images,omitempty"`
}
