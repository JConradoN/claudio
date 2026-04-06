package orchestrator

import (
	"testing"

	"github.com/kocar/aurelia/internal/agents"
)

func TestResolveAgentConfig_NoRegistry(t *testing.T) {
	cfg := ResolveAgentConfig(nil, "worker")
	if cfg.Model != "sonnet" {
		t.Errorf("model = %q, want sonnet", cfg.Model)
	}
	if cfg.MaxTurns != 25 {
		t.Errorf("maxTurns = %d, want 25", cfg.MaxTurns)
	}
	if len(cfg.Tools) != 6 {
		t.Errorf("tools count = %d, want 6", len(cfg.Tools))
	}
}

func TestResolveAgentConfig_SpecializedAgent(t *testing.T) {
	reg := buildTestRegistry(map[string]*agents.Agent{
		"qa": {
			Name:         "qa",
			Description:  "test specialist",
			Model:        "opus",
			MaxTurns:     15,
			AllowedTools: []string{"Read", "Write", "Bash"},
			Prompt:       "You write tests.",
		},
	})

	cfg := ResolveAgentConfig(reg, "qa")
	if cfg.Model != "opus" {
		t.Errorf("model = %q, want opus", cfg.Model)
	}
	if cfg.MaxTurns != 15 {
		t.Errorf("maxTurns = %d, want 15", cfg.MaxTurns)
	}
	if len(cfg.Tools) != 3 {
		t.Errorf("tools count = %d, want 3", len(cfg.Tools))
	}
	if cfg.Prompt != "You write tests." {
		t.Errorf("prompt = %q", cfg.Prompt)
	}
}

func TestResolveAgentConfig_WorkerOverride(t *testing.T) {
	reg := buildTestRegistry(map[string]*agents.Agent{
		"worker": {
			Name:     "worker",
			Model:    "opus",
			MaxTurns: 40,
			Prompt:   "Custom worker prompt.",
		},
	})

	cfg := ResolveAgentConfig(reg, "worker")
	if cfg.Model != "opus" {
		t.Errorf("model = %q, want opus", cfg.Model)
	}
	if cfg.MaxTurns != 40 {
		t.Errorf("maxTurns = %d, want 40", cfg.MaxTurns)
	}
	// Tools should remain default since worker.md didn't specify
	if len(cfg.Tools) != 6 {
		t.Errorf("tools count = %d, want 6 (default)", len(cfg.Tools))
	}
	if cfg.Prompt != "Custom worker prompt." {
		t.Errorf("prompt = %q", cfg.Prompt)
	}
}

func TestResolveAgentConfig_UnknownFallsToDefault(t *testing.T) {
	reg := buildTestRegistry(map[string]*agents.Agent{
		"qa": {Name: "qa", Model: "opus", Prompt: "qa"},
	})

	cfg := ResolveAgentConfig(reg, "nonexistent")
	if cfg.Model != "sonnet" {
		t.Errorf("model = %q, want sonnet (default)", cfg.Model)
	}
}

func TestResolveAgentConfig_SpecializedFillsDefaults(t *testing.T) {
	reg := buildTestRegistry(map[string]*agents.Agent{
		"reviewer": {
			Name:        "reviewer",
			Description: "reviews code",
			// No model, maxTurns, or tools — should fill from defaults
			Prompt: "Review carefully.",
		},
	})

	cfg := ResolveAgentConfig(reg, "reviewer")
	if cfg.Model != "sonnet" {
		t.Errorf("model = %q, want sonnet (default fill)", cfg.Model)
	}
	if cfg.MaxTurns != 25 {
		t.Errorf("maxTurns = %d, want 25 (default fill)", cfg.MaxTurns)
	}
	if len(cfg.Tools) != 6 {
		t.Errorf("tools count = %d, want 6 (default fill)", len(cfg.Tools))
	}
}

// buildTestRegistry creates a Registry from a map for testing.
func buildTestRegistry(agentMap map[string]*agents.Agent) *agents.Registry {
	return agents.NewTestRegistry(agentMap)
}
