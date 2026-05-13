package orchestrator

import "github.com/igormaneschy/aurelia/internal/agents"

// DefaultWorkerConfig is the built-in worker that works without any .md file.
var DefaultWorkerConfig = WorkerConfig{
	Model:    "sonnet",
	MaxTurns: 25,
	Tools:    []string{"Read", "Write", "Edit", "Bash", "Grep", "Glob"},
	Prompt: `You are an implementation worker in a software development squad.

You receive atomic tasks from the orchestrator and execute them thoroughly.

Rules:
- Focus exclusively on the assigned task
- Do not make changes outside the scope of your task
- Use conventional commits if asked to commit
- Report completion clearly with a summary of what was done
- If blocked, explain what's blocking and what you tried`,
}

// ResolveAgentConfig returns the config for a given agent name.
// Resolution cascade:
//  1. If agentName exists in registry as a specialized agent → use its config
//  2. If agentName is "worker" or doesn't exist → use worker.md override if available
//  3. Fallback → DefaultWorkerConfig
func ResolveAgentConfig(registry *agents.Registry, agentName string) WorkerConfig {
	// Specialized agent exists?
	if agentName != "" && agentName != "worker" && registry != nil {
		if a := registry.Get(agentName); a != nil {
			return agentToWorkerConfig(a)
		}
	}
	// Fallback: worker.md override or default
	if registry != nil {
		if w := registry.Get("worker"); w != nil {
			return mergeWorkerConfig(DefaultWorkerConfig, w)
		}
	}
	return DefaultWorkerConfig
}

// agentToWorkerConfig converts an Agent definition to a WorkerConfig.
func agentToWorkerConfig(a *agents.Agent) WorkerConfig {
	cfg := WorkerConfig{
		Model:    a.Model,
		MaxTurns: a.MaxTurns,
		Prompt:   a.Prompt,
	}
	if len(a.AllowedTools) > 0 {
		cfg.Tools = a.AllowedTools
	} else {
		cfg.Tools = DefaultWorkerConfig.Tools
	}
	if cfg.Model == "" {
		cfg.Model = DefaultWorkerConfig.Model
	}
	if cfg.MaxTurns == 0 {
		cfg.MaxTurns = DefaultWorkerConfig.MaxTurns
	}
	if cfg.Prompt == "" {
		cfg.Prompt = DefaultWorkerConfig.Prompt
	}
	return cfg
}

// mergeWorkerConfig merges an Agent override onto the default config.
// Non-empty fields from the agent override the defaults.
func mergeWorkerConfig(base WorkerConfig, override *agents.Agent) WorkerConfig {
	if override.Model != "" {
		base.Model = override.Model
	}
	if override.MaxTurns > 0 {
		base.MaxTurns = override.MaxTurns
	}
	if len(override.AllowedTools) > 0 {
		base.Tools = override.AllowedTools
	}
	if override.Prompt != "" {
		base.Prompt = override.Prompt
	}
	return base
}
