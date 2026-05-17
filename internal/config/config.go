package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/igormaneschy/aurelia/internal/runtime"
)

// NormalizeProvider returns a canonical lowercase provider name.
func NormalizeProvider(provider string) string {
	normalized := strings.TrimSpace(strings.ToLower(provider))
	if normalized == "" {
		return "kimi"
	}
	return normalized
}

// defaultModelForProvider returns the default model for the given provider.
func defaultModelForProvider(provider string) string {
	switch NormalizeProvider(provider) {
	case "anthropic":
		return "claude-sonnet-4-6"
	case "google":
		return "gemini-2.5-pro"
	case "opencode-go":
		return "openai/gpt-5.4"
	case "openrouter":
		return "openrouter/auto"
	case "zai":
		return "glm-5"
	case "alibaba":
		return "qwen3-coder-plus"
	default:
		return "kimi-k2-thinking"
	}
}

const (
	defaultMaxIterations    = 500
	defaultMaxSessionTokens = 100000
	defaultMaxImageBytes    = 10 * 1024 * 1024
	defaultSessionTTLHours  = 168
	defaultLLMProvider      = "kimi"
	defaultLLMModel         = "kimi-k2-thinking"
	defaultSTTProvider      = "groq"
)

// ProviderConfig holds credentials and endpoint for a single LLM provider.
type ProviderConfig struct {
	APIKey   string `json:"api_key"`
	BaseURL  string `json:"base_url,omitempty"`
	AuthMode string `json:"auth_mode,omitempty"`
}

// AppConfig holds all runtime configuration needed for the application.
type AppConfig struct {
	DefaultProvider string                    `json:"default_provider"`
	DefaultModel    string                    `json:"default_model"`
	Providers       map[string]ProviderConfig `json:"providers"`

	TelegramBotToken        string  `json:"telegram_bot_token"`
	TelegramAllowedUserIDs  []int64 `json:"telegram_allowed_user_ids"`
	TelegramAllowedGroupIDs []int64 `json:"telegram_allowed_group_ids,omitempty"`

	STTProvider string `json:"stt_provider"`

	MaxIterations    int    `json:"max_iterations"`
	MaxSessionTokens int    `json:"max_session_tokens"`
	MaxImageBytes    int    `json:"max_image_bytes,omitempty"`
	SessionTTLHours  int    `json:"session_ttl_hours,omitempty"`
	DBPath           string `json:"db_path"`
	MCPConfigPath    string `json:"mcp_servers_config_path"`

	DreamModel   string `json:"dream_model,omitempty"`
	ExtractModel string `json:"extract_model,omitempty"`

	NudgeEnabled *bool  `json:"nudge_enabled,omitempty"` // nil = default true
	NudgeTurns   int    `json:"nudge_turns,omitempty"`
	NudgeModel   string `json:"nudge_model,omitempty"`

	VisionModel    string `json:"vision_model,omitempty"`
	VisionProvider string `json:"vision_provider,omitempty"`
	LogLevel       string `json:"log_level,omitempty"`
	LogFormat      string `json:"log_format,omitempty"`

	// DiskScanEnabled toggles the fallback that walks $HOME (and /media, /mnt)
	// looking for a directory whose name matches a word in the user's first
	// message. Disabled by default — adds up to 3s of latency on session start
	// and the projectIndex already covers the common cases. Opt-in for users
	// who really want fuzzy disk discovery.
	DiskScanEnabled bool `json:"disk_scan_enabled,omitempty"`
}

// VisionFallback returns the configured vision model and provider for image inputs.
// Returns empty strings when no vision fallback is configured.
func (c *AppConfig) VisionFallback() (model, provider string) {
	return c.VisionModel, c.VisionProvider
}

// Onboarded returns true if the config has the minimum required fields to run.
func (c *AppConfig) Onboarded() bool {
	return c.TelegramBotToken != "" && len(c.TelegramAllowedUserIDs) > 0 && c.DefaultProvider != ""
}

// Editable returns a mutable copy of the user-editable configuration subset.
func (c *AppConfig) Editable() *EditableConfig {
	return appConfigToEditable(c)
}

// ProviderAPIKey returns the API key for the given provider, or empty string.
func (c *AppConfig) ProviderAPIKey(provider string) string {
	p, ok := c.Providers[NormalizeProvider(provider)]
	if !ok {
		return ""
	}
	return p.APIKey
}

// ProviderBaseURL returns the base URL for the given provider, or empty string.
func (c *AppConfig) ProviderBaseURL(provider string) string {
	p, ok := c.Providers[NormalizeProvider(provider)]
	if !ok {
		return ""
	}
	return p.BaseURL
}

// ProviderAuthMode returns the auth mode for the given provider, or empty string.
func (c *AppConfig) ProviderAuthMode(provider string) string {
	p, ok := c.Providers[NormalizeProvider(provider)]
	if !ok {
		return ""
	}
	return p.AuthMode
}

// fileConfig is the JSON structure written to disk (new schema).
type fileConfig struct {
	DefaultProvider string                    `json:"default_provider"`
	DefaultModel    string                    `json:"default_model"`
	Providers       map[string]ProviderConfig `json:"providers"`

	TelegramBotToken        string  `json:"telegram_bot_token"`
	TelegramAllowedUserIDs  []int64 `json:"telegram_allowed_user_ids"`
	TelegramAllowedGroupIDs []int64 `json:"telegram_allowed_group_ids,omitempty"`

	STTProvider string `json:"stt_provider"`

	MaxIterations    int    `json:"max_iterations"`
	MaxSessionTokens int    `json:"max_session_tokens"`
	MaxImageBytes    int    `json:"max_image_bytes,omitempty"`
	SessionTTLHours  int    `json:"session_ttl_hours,omitempty"`
	DBPath           string `json:"db_path"`
	MCPConfigPath    string `json:"mcp_servers_config_path"`

	VisionModel     string `json:"vision_model,omitempty"`
	VisionProvider  string `json:"vision_provider,omitempty"`
	LogLevel        string `json:"log_level,omitempty"`
	LogFormat       string `json:"log_format,omitempty"`
	DiskScanEnabled bool   `json:"disk_scan_enabled,omitempty"`
}

// Load reads the instance-local JSON config, creates it with defaults when
// missing, and returns the normalized runtime config.
func Load(r *runtime.PathResolver) (*AppConfig, error) {
	path := r.AppConfig()
	defaults := defaultFileConfig(r)

	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			if err := writeConfigFile(path, defaults); err != nil {
				return nil, err
			}
			return toAppConfig(defaults), nil
		}
		return nil, fmt.Errorf("stat app config: %w", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read app config: %w", err)
	}

	cfg := defaults
	if len(data) != 0 {
		// Try new schema first
		if err := json.Unmarshal(data, &cfg); err != nil {
			return nil, fmt.Errorf("decode app config: %w", err)
		}

		// Detect legacy format: if providers map is empty but legacy fields present
		if len(cfg.Providers) == 0 {
			var legacy legacyFileConfig
			if err := json.Unmarshal(data, &legacy); err == nil {
				cfg = migrateLegacy(legacy)
			}
		}
	}

	normalized := normalizeFileConfig(cfg, r)
	if !sameFileConfig(normalized, cfg) {
		if err := writeConfigFile(path, normalized); err != nil {
			return nil, err
		}
	}

	return toAppConfig(normalized), nil
}

func defaultFileConfig(r *runtime.PathResolver) fileConfig {
	return fileConfig{
		DefaultProvider:         defaultLLMProvider,
		DefaultModel:            defaultModelForProvider(defaultLLMProvider),
		Providers:               map[string]ProviderConfig{},
		STTProvider:             defaultSTTProvider,
		TelegramAllowedUserIDs:  []int64{},
		TelegramAllowedGroupIDs: []int64{},
		MaxIterations:           defaultMaxIterations,
		MaxSessionTokens:        defaultMaxSessionTokens,
		MaxImageBytes:           defaultMaxImageBytes,
		SessionTTLHours:         defaultSessionTTLHours,
		DBPath:                  filepath.Join(r.Data(), "aurelia.db"),
		MCPConfigPath:           filepath.Join(r.Config(), "mcp_servers.json"),
	}
}

func normalizeFileConfig(cfg fileConfig, r *runtime.PathResolver) fileConfig {
	defaults := defaultFileConfig(r)
	if cfg.TelegramAllowedUserIDs == nil {
		cfg.TelegramAllowedUserIDs = defaults.TelegramAllowedUserIDs
	}
	if cfg.TelegramAllowedGroupIDs == nil {
		cfg.TelegramAllowedGroupIDs = defaults.TelegramAllowedGroupIDs
	}
	if cfg.DefaultProvider == "" {
		cfg.DefaultProvider = defaults.DefaultProvider
	}
	if cfg.DefaultModel == "" {
		cfg.DefaultModel = defaultModelForProvider(cfg.DefaultProvider)
	}
	if cfg.STTProvider == "" {
		cfg.STTProvider = defaults.STTProvider
	}
	if cfg.MaxIterations <= 0 {
		cfg.MaxIterations = defaults.MaxIterations
	}
	if cfg.DBPath == "" {
		cfg.DBPath = defaults.DBPath
	}
	if cfg.MaxSessionTokens <= 0 {
		cfg.MaxSessionTokens = defaults.MaxSessionTokens
	}
	if cfg.MaxImageBytes <= 0 {
		cfg.MaxImageBytes = defaults.MaxImageBytes
	}
	if cfg.SessionTTLHours <= 0 {
		cfg.SessionTTLHours = defaults.SessionTTLHours
	}
	if cfg.MCPConfigPath == "" {
		cfg.MCPConfigPath = defaults.MCPConfigPath
	}
	if cfg.Providers == nil {
		cfg.Providers = map[string]ProviderConfig{}
	}
	return cfg
}

func writeConfigFile(path string, cfg fileConfig) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create app config dir: %w", err)
	}

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("encode app config: %w", err)
	}
	data = append(data, '\n')

	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("write app config: %w", err)
	}
	return nil
}

func toAppConfig(cfg fileConfig) *AppConfig {
	// Normalize provider keys once so lookups don't need repeated normalization.
	normalized := make(map[string]ProviderConfig, len(cfg.Providers))
	for name, pc := range cfg.Providers {
		normalized[NormalizeProvider(name)] = pc
	}
	return &AppConfig{
		DefaultProvider:         cfg.DefaultProvider,
		DefaultModel:            cfg.DefaultModel,
		Providers:               normalized,
		TelegramBotToken:        cfg.TelegramBotToken,
		TelegramAllowedUserIDs:  cfg.TelegramAllowedUserIDs,
		TelegramAllowedGroupIDs: cfg.TelegramAllowedGroupIDs,
		STTProvider:             cfg.STTProvider,
		MaxIterations:           cfg.MaxIterations,
		MaxSessionTokens:        cfg.MaxSessionTokens,
		MaxImageBytes:           cfg.MaxImageBytes,
		SessionTTLHours:         cfg.SessionTTLHours,
		DBPath:                  cfg.DBPath,
		MCPConfigPath:           cfg.MCPConfigPath,
		VisionModel:             cfg.VisionModel,
		VisionProvider:          cfg.VisionProvider,
		LogLevel:                cfg.LogLevel,
		LogFormat:               cfg.LogFormat,
		DiskScanEnabled:         cfg.DiskScanEnabled,
	}
}
