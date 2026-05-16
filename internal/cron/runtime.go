package cron

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/igormaneschy/aurelia/internal/agents"
	"github.com/igormaneschy/aurelia/internal/bridge"
)

// BridgeCronRuntime executes cron jobs via the Claude Code bridge,
// resolving agent config from the registry and injecting persona prompt.
type BridgeCronRuntime struct {
	bridge          BridgeExecutor
	agents          AgentRegistry
	persona         PersonaBuilder
	memoryDir       string
	defaultProvider string
	exePath         string // path to the aurelia binary for CLI instructions
}

// AgentRegistry resolves agent definitions by name.
type AgentRegistry interface {
	Get(name string) *agents.Agent
}

// PersonaBuilder builds the base system prompt from persona files.
type PersonaBuilder interface {
	BuildPrompt() (string, error)
}

// NewBridgeCronRuntime creates a runtime that executes jobs via Bridge
// with agent config and persona prompt.
func NewBridgeCronRuntime(
	b BridgeExecutor,
	ag AgentRegistry,
	p PersonaBuilder,
	memoryDir string,
	defaultProvider string,
) *BridgeCronRuntime {
	return &BridgeCronRuntime{
		bridge:          b,
		agents:          ag,
		persona:         p,
		memoryDir:       memoryDir,
		defaultProvider: defaultProvider,
	}
}

// SetExePath configures the path to the aurelia binary used in the cron
// scheduling instructions injected into the system prompt. Optional — if
// unset, the prompt uses the bare "aurelia" command name.
func (r *BridgeCronRuntime) SetExePath(path string) {
	r.exePath = path
}

// ExecuteJob builds the system prompt with persona, agent, scheduling
// instructions and global memory, then executes via Bridge.
func (r *BridgeCronRuntime) ExecuteJob(ctx context.Context, job CronJob) (*ExecutionResult, error) {
	basePrompt, err := r.persona.BuildPrompt()
	if err != nil {
		return nil, fmt.Errorf("build persona prompt: %w", err)
	}

	sections := []string{basePrompt}

	agent := r.agents.Get(job.AgentName)
	if agent != nil && agent.Prompt != "" {
		sections = append(sections, agent.Prompt)
	}

	// Cron-spawned agents need the scheduling instructions to be able to
	// create follow-up jobs (e.g. "remind me again in 1 hour"). Without
	// this section the LLM would invent non-existent internal tools.
	if cron := r.buildCronInstructions(job.TargetChatID); cron != "" {
		sections = append(sections, cron)
	}

	// Inject global memory so the agent has continuity across runs. Per-project
	// memory layers are intentionally skipped — cron jobs are not tied to a
	// working directory.
	if mem := r.loadGlobalMemory(); mem != "" {
		sections = append(sections, mem)
	}

	opts := bridge.RequestOptions{
		Provider:     r.defaultProvider,
		SystemPrompt: strings.Join(sections, "\n\n"),
	}

	if agent != nil {
		opts.Model = agent.Model
		opts.Cwd = agent.Cwd
		opts.AllowedTools = agent.AllowedTools
		opts.DisallowedTools = agent.DisallowedTools
	}

	ev, err := r.bridge.Execute(ctx, bridge.Request{
		Command: "query",
		Prompt:  job.Prompt,
		Options: opts,
	})
	if err != nil {
		return nil, fmt.Errorf("bridge execute: %w", err)
	}
	if ev.Type == "error" {
		return nil, fmt.Errorf("bridge error: %s", ev.Message)
	}

	return &ExecutionResult{
		Output:    ev.Content,
		SessionID: ev.SessionID,
		CostUSD:   ev.CostUSD,
		NumTurns:  ev.NumTurns,
	}, nil
}

// buildCronInstructions mirrors the text injected by the telegram pipeline so
// agents triggered by cron can schedule follow-up jobs. Returns empty when no
// target chat is set (the --chat-id flag is required).
func (r *BridgeCronRuntime) buildCronInstructions(targetChatID int64) string {
	if targetChatID == 0 {
		return ""
	}
	bin := "aurelia"
	if r.exePath != "" {
		bin = r.exePath
	}
	chatFlag := fmt.Sprintf("--chat-id %d", targetChatID)
	return fmt.Sprintf(`## Scheduling Tasks

Use the Aurelia cron CLI for ALL scheduling. Internal scheduling tools die with the session — only the CLI persists.

- Recurring: `+"`%s cron add \"<cron-expr>\" \"<prompt>\" %s`"+`
- One-time: `+"`%s cron once \"<ISO-timestamp>\" \"<prompt>\" %s`"+`
- List: `+"`%s cron list %s`"+` | Delete: `+"`%s cron del <id>`"+`

Cron prompts are ACTION instructions (not content). They run in isolated sessions with no history. The --chat-id flag is required.`,
		bin, chatFlag,
		bin, chatFlag,
		bin, chatFlag,
		bin,
	)
}

// loadGlobalMemory reads MEMORY.md (if present) plus the first ~16KB of every
// .md file in the global memory directory. Heavier per-project layers are
// intentionally omitted — keeps the prompt bounded for cron jobs.
func (r *BridgeCronRuntime) loadGlobalMemory() string {
	if r.memoryDir == "" {
		return ""
	}
	entries, err := os.ReadDir(r.memoryDir)
	if err != nil {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("## Persistent Memory\n\nYou have memory across cron runs. Below is what you remember:\n")

	const perFileCap = 8000
	wrote := 0

	if data, err := os.ReadFile(filepath.Join(r.memoryDir, "MEMORY.md")); err == nil && len(data) > 0 {
		sb.WriteString("\n**MEMORY.md (index):**\n")
		sb.WriteString(cap8k(string(data), perFileCap))
		wrote++
	}

	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || name == "MEMORY.md" || !strings.HasSuffix(name, ".md") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(r.memoryDir, name))
		if err != nil || len(data) == 0 {
			continue
		}
		fmt.Fprintf(&sb, "\n**%s:**\n%s", name, cap8k(strings.TrimSpace(string(data)), perFileCap))
		wrote++
	}

	if wrote == 0 {
		return ""
	}
	return sb.String()
}

func cap8k(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "\n\n[...truncado]"
}

// BridgeAdapter wraps *bridge.Bridge to satisfy BridgeExecutor.
type BridgeAdapter struct {
	B *bridge.Bridge
}

// Execute calls bridge.ExecuteSync and returns the terminal event.
func (a *BridgeAdapter) Execute(ctx context.Context, req bridge.Request) (*bridge.Event, error) {
	return a.B.ExecuteSync(ctx, req)
}

// DeliveryFunc is called after a job completes to deliver its output.
type DeliveryFunc func(ctx context.Context, job CronJob, result *ExecutionResult, execErr error) error

// NotifyingRuntime wraps a Runtime and delivers results after execution.
type NotifyingRuntime struct {
	inner   Runtime
	deliver DeliveryFunc
}

// NewNotifyingRuntime wraps an inner runtime with delivery notification.
func NewNotifyingRuntime(inner Runtime, deliver DeliveryFunc) *NotifyingRuntime {
	return &NotifyingRuntime{
		inner:   inner,
		deliver: deliver,
	}
}

// ExecuteJob runs the inner runtime and delivers the result.
func (r *NotifyingRuntime) ExecuteJob(ctx context.Context, job CronJob) (*ExecutionResult, error) {
	if r.inner == nil {
		return nil, fmt.Errorf("inner runtime is required")
	}

	result, err := r.inner.ExecuteJob(ctx, job)
	if r.deliver != nil {
		if deliverErr := r.deliver(ctx, job, result, err); deliverErr != nil {
			return result, deliverErr
		}
	}
	return result, err
}
