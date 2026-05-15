package pipeline

import (
	"strings"
)

// ErrorCategory classifies PI errors for resilience handling.
type ErrorCategory int

const (
	// ErrTransient — retryable (rate limit, timeout, 503, network)
	ErrTransient ErrorCategory = iota
	// ErrAuth — authentication failure (API key, credentials)
	ErrAuth
	// ErrModelNotFound — requested model does not exist on provider
	ErrModelNotFound
	// ErrContextLength — conversation exceeds model context window
	ErrContextLength
	// ErrUnknown — unmapped error
	ErrUnknown
)

// IsRetryable returns true if the error category supports automatic retry.
func (c ErrorCategory) IsRetryable() bool {
	return c == ErrTransient
}

// ClassifyError inspects a PI error message and returns its category.
func ClassifyError(msg string) ErrorCategory {
	lower := strings.ToLower(msg)

	// Transient errors
	transientPatterns := []string{
		"rate limit", "rate_limit", "too many requests", "429",
		"timeout", "timed out", "deadline exceeded", "context deadline",
		"503", "service unavailable", "502", "bad gateway", "504", "gateway timeout",
		"network error", "connection refused", "no such host", "temporary",
		"econnrefused", "enetunreach", "etimedout",
	}
	for _, p := range transientPatterns {
		if strings.Contains(lower, p) {
			return ErrTransient
		}
	}

	// Authentication errors
	authPatterns := []string{
		"api key", "apikey", "authentication", "unauthorized", "401",
		"invalid key", "wrong api", "access denied", "forbidden", "403",
		"credentials", "not authenticated", "auth error",
	}
	for _, p := range authPatterns {
		if strings.Contains(lower, p) {
			return ErrAuth
		}
	}

	// Model not found
	modelPatterns := []string{
		"model not found", "model not available", "unknown model",
		"invalid model", "model does not exist", "not a valid model",
	}
	for _, p := range modelPatterns {
		if strings.Contains(lower, p) {
			return ErrModelNotFound
		}
	}

	// Context length
	contextPatterns := []string{
		"context length", "context too long", "maximum context",
		"token limit", "too many tokens", "context window exceeded",
		"input is too long", "max_tokens", "exceeds limit",
	}
	for _, p := range contextPatterns {
		if strings.Contains(lower, p) {
			return ErrContextLength
		}
	}

	return ErrUnknown
}

// TranslatedError holds a user-facing message in Portuguese and a suggested action.
type TranslatedError struct {
	Category ErrorCategory
	Message  string // message sent to the user
}

// TranslateError converts a raw PI error into a user-friendly Portuguese message.
func TranslateError(provider, model, raw string) TranslatedError {
	cat := ClassifyError(raw)

	switch cat {
	case ErrTransient:
		return TranslatedError{
			Category: cat,
			Message: "⏳ O provider " + provider + " está sobrecarregado no momento.\n\n" +
				"Vou tentar novamente automaticamente...",
		}

	case ErrAuth:
		return TranslatedError{
			Category: cat,
			Message: "🔐 Erro de autenticação com o provider " + provider + "\n\n" +
				"Verifique se a API key está configurada corretamente em ~/.aurelia/config/app.json",
		}

	case ErrModelNotFound:
		return TranslatedError{
			Category: cat,
			Message: "⚠️ Modelo " + model + " não encontrado no provider " + provider + ".\n\n" +
				"Use 'lista modelos' para ver os modelos disponíveis.",
		}

	case ErrContextLength:
		return TranslatedError{
			Category: cat,
			Message: "📄 A conversa ficou muito longa para o modelo " + model + ".\n\n" +
				"Use 'nova conversa' para iniciar uma sessão nova.",
		}

	default:
		return TranslatedError{
			Category: cat,
			Message: "❌ Erro no processador: " + raw + "\n\n" +
				"Se persistir, tente 'nova conversa' ou mude de modelo com 'lista modelos'.",
		}
	}
}

// FallbackMessage returns the brief notification shown when fallback is activated.
func FallbackMessage(originalProvider string) string {
	return "⚡ Provider " + originalProvider + " instável no momento. " +
		"Usando modelo alternativo (OpenRouter free) enquanto isso..."
}

// FinalErrorMessage returns the message when all providers failed.
func FinalErrorMessage() string {
	return "❌ Não consegui processar sua mensagem. Todos os providers disponíveis estão instáveis no momento.\n\n" +
		"Tente novamente em alguns minutos ou verifique 'status'."
}

// OpenRouterNotConfiguredMessage returns the message when fallback is unavailable.
func OpenRouterNotConfiguredMessage() string {
	return "❌ Provider principal indisponível e OpenRouter não configurado.\n\n" +
		"Configure uma API key do OpenRouter em ~/.aurelia/config/app.json como alternativa."
}
