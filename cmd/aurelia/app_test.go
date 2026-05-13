package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestHasClaudeSubscriptionAuth_AcceptsLegacyCredentialsFile(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	claudeDir := filepath.Join(home, ".claude")
	if err := os.MkdirAll(claudeDir, 0o700); err != nil {
		t.Fatalf("create claude dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(claudeDir, ".credentials.json"), []byte("{}\n"), 0o600); err != nil {
		t.Fatalf("write credentials: %v", err)
	}

	if !hasClaudeSubscriptionAuth(home) {
		t.Fatal("expected legacy credentials file to be accepted")
	}
}

func TestHasClaudeSubscriptionAuth_AcceptsClaudeAIAuthStatus(t *testing.T) {
	original := runClaudeAuthStatus
	t.Cleanup(func() { runClaudeAuthStatus = original })
	runClaudeAuthStatus = func() ([]byte, error) {
		return []byte(`{"loggedIn":true,"authMethod":"claude.ai"}`), nil
	}

	if !hasClaudeSubscriptionAuth(t.TempDir()) {
		t.Fatal("expected claude.ai auth status to be accepted")
	}
}

func TestHasClaudeSubscriptionAuth_RejectsLoggedOutStatus(t *testing.T) {
	original := runClaudeAuthStatus
	t.Cleanup(func() { runClaudeAuthStatus = original })
	runClaudeAuthStatus = func() ([]byte, error) {
		return []byte(`{"loggedIn":false,"authMethod":"none"}`), nil
	}

	if hasClaudeSubscriptionAuth(t.TempDir()) {
		t.Fatal("expected logged out auth status to be rejected")
	}
}
