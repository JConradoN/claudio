package onboarding

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/igormaneschy/aurelia/internal/config"
)

// ModelOption is a selectable model entry for onboarding and config UIs.
type ModelOption struct {
	ID                 string
	Name               string
	SupportsImageInput bool
	SupportsTools      bool
	IsFree             bool
}

// Label returns a display label for the model option.
func (m ModelOption) Label() string {
	badges := make([]string, 0, 3)
	if m.SupportsImageInput {
		badges = append(badges, "vision")
	}
	if m.SupportsTools {
		badges = append(badges, "tools")
	}
	if m.IsFree {
		badges = append(badges, "free")
	}
	suffix := ""
	if len(badges) != 0 {
		suffix = " [" + strings.Join(badges, ", ") + "]"
	}
	if m.Name == "" || m.Name == m.ID {
		return m.ID + suffix
	}
	return fmt.Sprintf("%s (%s)%s", m.Name, m.ID, suffix)
}

// ModelCatalogCredentials carries provider-specific credentials used by model catalogs.
type ModelCatalogCredentials struct {
	AnthropicAPIKey  string
	KimiAPIKey       string
	OpenRouterAPIKey string
	ZAIAPIKey        string
	AlibabaAPIKey    string
}

// ProviderSpec describes a supported LLM provider for onboarding and config.
type ProviderSpec struct {
	ID                  string
	Label               string
	DefaultModel        string
	APIKeyLabel         string
	APIKeyHelp          string
	SupportsModelSearch bool
	AuthModes           []string
}

var providerSpecs = []ProviderSpec{
	{
		ID:           "kimi",
		Label:        "Kimi (Moonshot)",
		DefaultModel: "k2.5",
		APIKeyLabel:  "Kimi API key",
		APIKeyHelp:   "Anthropic-compatible. Base URL: https://api.kimi.com/coding/",
	},
	{
		ID:           "opencode-go",
		Label:        "opencode-go",
		DefaultModel: "openai/gpt-5.4",
		APIKeyLabel:  "OpenCode API key",
		APIKeyHelp:   "OpenCode API key for opencode-go provider",
	},
	{
		ID:           "anthropic",
		Label:        "Anthropic",
		DefaultModel: "claude-sonnet-4-6",
		APIKeyLabel:  "Anthropic API key",
		APIKeyHelp:   "Native Anthropic API. Supports subscription (Max plan) or API key.",
	},
	{
		ID:                  "openrouter",
		Label:               "OpenRouter",
		DefaultModel:        "openrouter/auto",
		APIKeyLabel:         "OpenRouter API key",
		APIKeyHelp:          "Proxy multi-modelo. Base URL: https://openrouter.ai/api/v1",
		SupportsModelSearch: true,
	},
	{
		ID:           "zai",
		Label:        "Z.ai (GLM)",
		DefaultModel: "glm-5",
		APIKeyLabel:  "Z.ai API key",
		APIKeyHelp:   "Anthropic-compatible. Base URL: https://api.z.ai/api/anthropic",
	},
	{
		ID:           "alibaba",
		Label:        "Alibaba (Qwen)",
		DefaultModel: "qwen3-coder-plus",
		APIKeyLabel:  "Alibaba DashScope API key",
		APIKeyHelp:   "Anthropic-compatible. Base URL: https://dashscope-intl.aliyuncs.com/apps/anthropic",
	},
}

// providers returns a copy of the available provider specs.
func providers() []ProviderSpec {
	specs := make([]ProviderSpec, len(providerSpecs))
	copy(specs, providerSpecs)
	return specs
}

// provider returns the spec for the given provider name.
func provider(name string) (ProviderSpec, bool) {
	normalized := config.NormalizeProvider(name)
	for _, spec := range providerSpecs {
		if spec.ID == normalized {
			return spec, true
		}
	}
	return ProviderSpec{}, false
}

// providerChoices returns the list of provider IDs.
func providerChoices() []string {
	specs := providers()
	choices := make([]string, 0, len(specs))
	for _, spec := range specs {
		choices = append(choices, spec.ID)
	}
	return choices
}

// providerLabels returns the list of provider display labels.
func providerLabels() []string {
	specs := providers()
	labels := make([]string, 0, len(specs))
	for _, spec := range specs {
		labels = append(labels, spec.Label)
	}
	return labels
}

// listModels returns the available model options for a provider.
// For OpenRouter, fetches live models from the API when an API key is available.
func listModels(ctx context.Context, p string, creds ModelCatalogCredentials) ([]ModelOption, error) {
	p = config.NormalizeProvider(p)

	if p == "openrouter" && creds.OpenRouterAPIKey != "" {
		models, err := fetchOpenRouterModels(ctx, creds.OpenRouterAPIKey)
		if err == nil && len(models) > 0 {
			return models, nil
		}
		// Fall through to curated list on error.
	}

	models := fallbackModels(p)
	if models == nil {
		return nil, fmt.Errorf("unsupported llm provider %q", p)
	}
	return models, nil
}

// openRouterModel mirrors the relevant fields from the OpenRouter /api/v1/models response.
type openRouterModel struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	ContextLen   int    `json:"context_length"`
	Architecture struct {
		InputModalities  []string `json:"input_modalities"`
		OutputModalities []string `json:"output_modalities"`
	} `json:"architecture"`
	Pricing struct {
		Prompt     string `json:"prompt"`
		Completion string `json:"completion"`
	} `json:"pricing"`
}

// fetchOpenRouterModels calls the OpenRouter models API and returns parsed ModelOptions.
func fetchOpenRouterModels(ctx context.Context, apiKey string) ([]ModelOption, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://openrouter.ai/api/v1/models", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("openrouter models request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("openrouter models API returned %d", resp.StatusCode)
	}

	var body struct {
		Data []openRouterModel `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, fmt.Errorf("openrouter models decode: %w", err)
	}

	// Prepend the meta-routing options.
	options := []ModelOption{
		{ID: "openrouter/auto", Name: "OpenRouter Auto (router)", SupportsTools: true},
		{ID: "openrouter/free", Name: "OpenRouter Free Router", IsFree: true},
	}

	for _, m := range body.Data {
		if m.ID == "" {
			continue
		}
		opt := ModelOption{
			ID:   m.ID,
			Name: m.Name,
		}
		for _, mod := range m.Architecture.InputModalities {
			if mod == "image" {
				opt.SupportsImageInput = true
				break
			}
		}
		// Models with $0 prompt+completion pricing are free.
		if m.Pricing.Prompt == "0" && m.Pricing.Completion == "0" {
			opt.IsFree = true
		}
		options = append(options, opt)
	}

	return options, nil
}

// fallbackModelList returns curated default models when discovery is unavailable.
func fallbackModelList(p string) []ModelOption {
	return fallbackModels(config.NormalizeProvider(p))
}

func fallbackModels(p string) []ModelOption {
	switch p {
	case "anthropic":
		return []ModelOption{
			{ID: "claude-sonnet-4-6", Name: "Claude Sonnet 4.6", SupportsImageInput: true},
			{ID: "claude-opus-4-6", Name: "Claude Opus 4.6", SupportsImageInput: true},
			{ID: "claude-haiku-4-5", Name: "Claude Haiku 4.5", SupportsImageInput: true},
		}
	case "openrouter":
		return []ModelOption{
			{ID: "openrouter/auto", Name: "OpenRouter Auto"},
			{ID: "openrouter/free", Name: "OpenRouter Free Router", IsFree: true},
		}
	case "zai":
		return []ModelOption{
			{ID: "glm-5", Name: "GLM-5"},
			{ID: "glm-4.7", Name: "GLM-4.7"},
			{ID: "glm-4.6v", Name: "GLM-4.6V", SupportsImageInput: true},
			{ID: "glm-4.5-air", Name: "GLM-4.5 Air"},
		}
	case "alibaba":
		return []ModelOption{
			{ID: "qwen3-coder-plus", Name: "Qwen3 Coder Plus"},
			{ID: "qwen3-coder-next", Name: "Qwen3 Coder Next"},
			{ID: "qwen-vl-max", Name: "Qwen VL Max", SupportsImageInput: true},
			{ID: "qwen3.5-plus", Name: "Qwen3.5 Plus"},
		}
	case "kimi":
		return []ModelOption{
			{ID: "kimi-k2-thinking", Name: "Kimi K2 Thinking"},
			{ID: "kimi-k2-thinking-turbo", Name: "Kimi K2 Thinking Turbo"},
			{ID: "k2.5", Name: "Kimi K2.5"},
			{ID: "moonshot-v1-vision", Name: "Moonshot Vision", SupportsImageInput: true},
			{ID: "moonshot-v1-8k", Name: "Moonshot v1 8K"},
			{ID: "moonshot-v1-32k", Name: "Moonshot v1 32K"},
			{ID: "moonshot-v1-128k", Name: "Moonshot v1 128K"},
		}
	case "opencode-go":
		return []ModelOption{
			{ID: "openai/gpt-5.4", Name: "GPT-5.4 · openai", SupportsImageInput: true},
			{ID: "openai/gpt-4.7", Name: "GPT-4.7 · openai", SupportsImageInput: true},
			{ID: "openai/o4-4", Name: "o4-4 · openai"},
			{ID: "openai/o3", Name: "o3 · openai"},
			{ID: "anthropic/claude-sonnet-4-6", Name: "Claude Sonnet 4.6 · anthropic", SupportsImageInput: true},
			{ID: "anthropic/claude-opus-4-6", Name: "Claude Opus 4.6 · anthropic", SupportsImageInput: true},
			{ID: "google/gemini-2.5-pro", Name: "Gemini 2.5 Pro · google", SupportsImageInput: true},
		}
	default:
		return nil
	}
}

