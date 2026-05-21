package agentmesh

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

const (
	// DefaultTimeout is the maximum time to wait for a CLI agent to respond.
	DefaultTimeout = 5 * time.Minute

	// maxResultBytes caps how much output we store/display (Telegram limit).
	maxResultBytes = 3800
)

// AgentName identifies which CLI to invoke.
type AgentName string

const (
	AgentClaude AgentName = "claude"
	AgentGemini AgentName = "gemini"
)

// ExecResult holds the outcome of a CLI invocation.
type ExecResult struct {
	Stdout   string
	Stderr   string
	Duration time.Duration
	Err      error
}

// Success returns true when the subprocess exited cleanly with output.
func (r ExecResult) Success() bool { return r.Err == nil && strings.TrimSpace(r.Stdout) != "" }

// TelegramText returns a Telegram-safe summary of the result.
func (r ExecResult) TelegramText(agent AgentName) string {
	if r.Err != nil {
		stderr := strings.TrimSpace(r.Stderr)
		if stderr == "" {
			stderr = r.Err.Error()
		}
		return fmt.Sprintf("Erro ao executar %s (%.0fs):\n`%s`", agent, r.Duration.Seconds(), truncate(stderr, 500))
	}
	out := strings.TrimSpace(r.Stdout)
	if out == "" {
		return fmt.Sprintf("%s executou em %.0fs mas não retornou saída.", agent, r.Duration.Seconds())
	}
	return out
}

// RunClaude invokes `claude -p "<prompt>"` and captures stdout.
func RunClaude(ctx context.Context, prompt string) ExecResult {
	return runSubprocess(ctx, "claude", []string{"-p", prompt})
}

// RunGemini invokes gemini via stdin to avoid OS arg-length limits.
func RunGemini(ctx context.Context, prompt string) ExecResult {
	cmd := exec.CommandContext(ctx, "gemini", "-p", "", "-o", "text", "--approval-mode", "yolo")
	cmd.Stdin = strings.NewReader(prompt)
	return captureSubprocess(cmd)
}

// Run dispatches to the correct executor based on agent name.
func Run(ctx context.Context, agent AgentName, prompt string) ExecResult {
	switch agent {
	case AgentClaude:
		return RunClaude(ctx, prompt)
	case AgentGemini:
		return RunGemini(ctx, prompt)
	default:
		return ExecResult{Err: fmt.Errorf("agente desconhecido: %s", agent)}
	}
}

func runSubprocess(ctx context.Context, name string, args []string) ExecResult {
	cmd := exec.CommandContext(ctx, name, args...)
	return captureSubprocess(cmd)
}

func captureSubprocess(cmd *exec.Cmd) ExecResult {
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	start := time.Now()
	err := cmd.Run()
	dur := time.Since(start)

	out := truncate(stdout.String(), maxResultBytes)
	errOut := truncate(stderr.String(), 1000)

	return ExecResult{
		Stdout:   out,
		Stderr:   errOut,
		Duration: dur,
		Err:      err,
	}
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "\n…[truncado]"
}
