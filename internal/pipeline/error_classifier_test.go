package pipeline

import (
	"strings"
	"testing"
)

func TestClassifyError(t *testing.T) {
	tests := []struct {
		msg      string
		expected ErrorCategory
	}{
		// Transient
		{"Rate limit exceeded", ErrTransient},
		{"429 Too Many Requests", ErrTransient},
		{"Request timeout", ErrTransient},
		{"connection refused", ErrTransient},
		{"Service unavailable (503)", ErrTransient},
		{"network error: no such host", ErrTransient},
		{"context deadline exceeded", ErrTransient},
		{"ETIMEDOUT", ErrTransient},

		// Auth
		{"Invalid API key", ErrAuth},
		{"Authentication failed", ErrAuth},
		{"401 Unauthorized", ErrAuth},
		{"Access denied", ErrAuth},
		{"Forbidden: 403", ErrAuth},

		// Model not found
		{"Model not found: foo-bar", ErrModelNotFound},
		{"Invalid model ID", ErrModelNotFound},

		// Context length
		{"Context length exceeded", ErrContextLength},
		{"Maximum context length reached", ErrContextLength},
		{"Token limit exceeded", ErrContextLength},

		// Unknown
		{"Something weird happened", ErrUnknown},
		{"", ErrUnknown},
	}

	for _, tt := range tests {
		t.Run(tt.msg, func(t *testing.T) {
			got := ClassifyError(tt.msg)
			if got != tt.expected {
				t.Errorf("ClassifyError(%q) = %v, want %v", tt.msg, got, tt.expected)
			}
		})
	}
}

func TestIsRetryable(t *testing.T) {
	if !ErrTransient.IsRetryable() {
		t.Error("ErrTransient should be retryable")
	}
	if ErrAuth.IsRetryable() {
		t.Error("ErrAuth should NOT be retryable")
	}
	if ErrModelNotFound.IsRetryable() {
		t.Error("ErrModelNotFound should NOT be retryable")
	}
	if ErrContextLength.IsRetryable() {
		t.Error("ErrContextLength should NOT be retryable")
	}
	if ErrUnknown.IsRetryable() {
		t.Error("ErrUnknown should NOT be retryable")
	}
}

func TestTranslateError(t *testing.T) {
	// Verify each category produces a non-empty Portuguese message containing expected keywords.
	cases := []struct {
		cat      ErrorCategory
		raw      string
		contains string
	}{
		{ErrTransient, "rate limit", "sobrecarregado"},
		{ErrAuth, "invalid key", "autenticação"},
		{ErrModelNotFound, "model not found", "não encontrado"},
		{ErrContextLength, "context length", "muito longa"},
		{ErrUnknown, "random error", "Erro no processador"},
	}

	for _, c := range cases {
		// We test TranslateError with the real classifier, so use a raw string that maps to the expected category.
		te := TranslateError("kimi", "kimi-k2", c.raw)
		if te.Category != c.cat {
			t.Errorf("TranslateError category for %q = %v, want %v", c.raw, te.Category, c.cat)
		}
		if !strings.Contains(te.Message, c.contains) {
			t.Errorf("TranslateError message for %q does not contain %q:\n%s", c.raw, c.contains, te.Message)
		}
	}
}

func TestFallbackMessage(t *testing.T) {
	msg := FallbackMessage("kimi")
	if !strings.Contains(msg, "kimi") {
		t.Error("FallbackMessage should mention the provider")
	}
	if !strings.Contains(msg, "OpenRouter") {
		t.Error("FallbackMessage should mention OpenRouter")
	}
}

func TestFinalErrorMessage(t *testing.T) {
	msg := FinalErrorMessage()
	if msg == "" {
		t.Error("FinalErrorMessage should not be empty")
	}
}

func TestOpenRouterNotConfiguredMessage(t *testing.T) {
	msg := OpenRouterNotConfiguredMessage()
	if !strings.Contains(msg, "OpenRouter") {
		t.Error("OpenRouterNotConfiguredMessage should mention OpenRouter")
	}
}
