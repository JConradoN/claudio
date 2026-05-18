package pipeline

import (
	"slices"
	"testing"

	"github.com/igormaneschy/aurelia/internal/config"
	"github.com/igormaneschy/aurelia/internal/session"
)

func TestBuildBridgeRequest_DisablesFileToolsInChatMode(t *testing.T) {
	svc := &Service{
		config:   &config.AppConfig{},
		sessions: session.NewStore(),
		botCwd:   "/tmp/aurelia-daemon",
	}

	req := svc.buildBridgeRequest("oi", "system", nil, 42, 0)
	for _, tool := range chatModeDisallowedTools {
		if !slices.Contains(req.Options.DisallowedTools, tool) {
			t.Fatalf("expected %s to be disallowed in chat mode, got %v", tool, req.Options.DisallowedTools)
		}
	}
}

func TestBuildBridgeRequest_AllowsFileToolsWhenCwdBound(t *testing.T) {
	sessions := session.NewStore()
	sessions.SetCwd(42, 0, "/repo/aurelia")
	svc := &Service{
		config:   &config.AppConfig{},
		sessions: sessions,
		botCwd:   "/tmp/aurelia-daemon",
	}

	req := svc.buildBridgeRequest("oi", "system", nil, 42, 0)
	if len(req.Options.DisallowedTools) != 0 {
		t.Fatalf("expected no chat-mode disallowed tools when cwd is bound, got %v", req.Options.DisallowedTools)
	}
	if req.Options.Cwd != "/repo/aurelia" {
		t.Fatalf("Cwd = %q, want bound cwd", req.Options.Cwd)
	}
}
