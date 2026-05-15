# PI Resilience — Design

**Spec**: `.specs/features/pi-resilience/spec.md`
**Status**: Draft

---

## Architecture Overview

A resiliência vive em **dois níveis**: Go (decisão) e Bridge (detecção). O Go layer classifica erros, decide retry/fallback, e gerencia o circuit breaker. O Bridge layer garante que erros não sejam silenciados.

```
User Message
    │
    ▼
Pipeline.Process()
    │
    ├─► bridge.Execute(req) ──► ch
    │       │
    │       ├─► PI Success ──► result event ──► resposta normal
    │       │
    │       ├─► PI Error (transient) ──► error event
    │       │       │
    │       │       ├─► Retry (max 3, backoff 2s→4s→8s)
    │       │       │       ├─► Success ──► resposta normal
    │       │       │       └─► Fail ──► Fallback
    │       │       │
    │       ├─► PI Error (permanent) ──► error event
    │       │       └─► NO retry ──► Fallback ou erro final
    │       │
    │       ├─► Circuit Breaker OPEN ──► SKIP provider ──► Fallback
    │       │
    │       └─► Fallback (OpenRouter free)
    │               ├─► Success ──► "⚡ usando alternativa..." + resposta
    │               └─► Fail ──► erro final traduzido
    │
    └─► ProcessBridgeEvents
            ├─► result ──► success
            ├─► error ──► classify ──► retry/fallback/error
            └─► channel closed ──► process death (bridge recovery)
```

---

## Code Reuse Analysis

### Existing Components to Leverage

| Component | Location | How to Use |
|-----------|----------|------------|
| `bridge.Execute()` | `internal/bridge/bridge.go:225` | Já existe; retry/fallback chama novamente com diferentes options |
| `bridge.ExecuteSync()` | `internal/bridge/bridge.go` | Usado por classify/cron; NÃO aplica retry (éidempotente, caller decide) |
| `ProcessBridgeEvents` | `internal/pipeline/pipeline.go:331` | Modificar `handleErrorEvent` para classificar e decidir retry/fallback |
| `bridgeFailureTracker` | `internal/pipeline/bridge_failure.go` | Reusar padrão de cooldown para circuit breaker |
| `session.Store` | `internal/session/store.go` | Preservar session ID do provider principal quando fallback ativa |
| `runSupervisor` | `internal/pipeline/run_supervisor.go` | Garantir que retry/fallback não conflita com cancelamento/supersede |
| `config.AppConfig` | `internal/config/config.go` | Verificar se OpenRouter está configurado antes de fallback |

### Integration Points

| System | Integration Method |
|--------|--------------------|
| Go → Bridge | `bridge.RequestOptions.Provider/Model` alterados para fallback |
| Go → Session Store | Sessão do provider principal preservada; fallback cria sessão nova |
| Go → Telegram | Mensagens breves de fallback via `output.SendText` |
| Go → Config | Verificação de `providers["openrouter"]` antes de fallback |

---

## Components

### 1. Error Classifier (`pierror` package)

- **Purpose**: Classificar mensagens de erro do PI em categorias (transient, permanent, auth, etc.)
- **Location**: Novo pacote `internal/pierror/classifier.go`
- **Changes**: Pacote novo, puro Go, sem dependências externas
- **Reuses**: Apenas pattern matching em strings

```go
// internal/pierror/classifier.go
package pierror

type Category int

const (
	CatUnknown Category = iota
	CatTransient     // rate limit, timeout, 503, network error
	CatAuth          // API key invalid, not configured, unauthorized
	CatModelNotFound // model not found in registry
	CatContextLength // context length exceeded
	CatContentPolicy // content policy violation
	CatPermanent     // other permanent errors
)

type ClassifiedError struct {
	Original string
	Category Category
	Provider string
	Model    string
}

func Classify(errMsg string, provider, model string) ClassifiedError {
	lower := strings.ToLower(errMsg)
	ce := ClassifiedError{Original: errMsg, Provider: provider, Model: model}

	switch {
	case containsAny(lower, "rate limit", "rate_limit", "too many requests", "429"):
		ce.Category = CatTransient
	case containsAny(lower, "timeout", "timed out", "deadline exceeded", "etimedout"):
		ce.Category = CatTransient
	case containsAny(lower, "503", "service unavailable", "bad gateway", "502", "504"):
		ce.Category = CatTransient
	case containsAny(lower, "econnrefused", "econnreset", "enotfound", "network"):
		ce.Category = CatTransient
	case containsAny(lower, "unauthorized", "invalid api key", "api key", "authentication", "auth"):
		ce.Category = CatAuth
	case containsAny(lower, "model not found", "unknown model", "invalid model"):
		ce.Category = CatModelNotFound
	case containsAny(lower, "context length", "context too long", "maximum context"):
		ce.Category = CatContextLength
	case containsAny(lower, "content policy", "moderation", "safety", "blocked"):
		ce.Category = CatContentPolicy
	default:
		ce.Category = CatPermanent
	}
	return ce
}

func (ce ClassifiedError) IsTransient() bool {
	return ce.Category == CatTransient
}

func (ce ClassifiedError) IsRetryable() bool {
	return ce.Category == CatTransient
}

func containsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}
```

### 2. Error Translator (`pierror/translator.go`)

- **Purpose**: Traduzir erros classificados para mensagens amigáveis em português
- **Location**: `internal/pierror/translator.go`
- **Changes**: Pacote novo

```go
// internal/pierror/translator.go
package pierror

import "fmt"

func (ce ClassifiedError) UserMessage() string {
	switch ce.Category {
	case CatAuth:
		return fmt.Sprintf("🔐 Erro de autenticação com o provider %s\n\n"+
			"Verifique se a API key está configurada corretamente em `~/.aurelia/config/app.json`.", ce.Provider)
	case CatTransient:
		return fmt.Sprintf("⏳ O provider %s está sobrecarregado no momento.\n\n"+
			"Vou tentar novamente automaticamente...", ce.Provider)
	case CatModelNotFound:
		return fmt.Sprintf("⚠️ Modelo %s não encontrado no provider %s.\n\n"+
			"Use /model para ver os modelos disponíveis.", ce.Model, ce.Provider)
	case CatContextLength:
		return fmt.Sprintf("📄 A conversa ficou muito longa para o modelo %s.\n\n"+
			"Use /new para iniciar uma nova sessão.", ce.Model)
	case CatContentPolicy:
		return "🛡️ A mensagem foi bloqueada pela política de conteúdo do provider.\n\n" +
			"Tente reformular sua solicitação."
	case CatPermanent:
		return fmt.Sprintf("❌ Erro no processador: %s\n\n"+
			"Se persistir, tente /new ou mude de modelo com /model.", ce.Original)
	default:
		return fmt.Sprintf("❌ Erro desconhecido no processador: %s\n\n"+
			"Tente /new para reiniciar a sessão.", ce.Original)
	}
}

func (ce ClassifiedError) FallbackMessage() string {
	return fmt.Sprintf("⚡ Provider %s instável no momento. Usando modelo alternativo (OpenRouter free) enquanto isso...", ce.Provider)
}

func (ce ClassifiedError) FinalErrorMessage() string {
	return "❌ Não consegui processar sua mensagem. Todos os providers disponíveis estão instáveis no momento.\n\n" +
		"Tente novamente em alguns minutos ou verifique /status."
}
```

### 3. Retry Engine (`internal/pipeline/retry.go`)

- **Purpose**: Executar retries com exponential backoff para erros transitórios
- **Location**: `internal/pipeline/retry.go`
- **Changes**: Novo arquivo
- **Reuses**: `context.Context` para cancelamento

```go
// internal/pipeline/retry.go
package pipeline

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/igormaneschy/aurelia/internal/bridge"
	"github.com/igormaneschy/aurelia/internal/pierror"
)

const (
	maxRetries     = 3
	baseRetryDelay = 2 * time.Second
)

type retryResult struct {
	outcome Outcome
	events  []bridge.Event // accumulated events for potential use
}

// executeWithRetry runs bridge.Execute with retry logic for transient errors.
// It returns the final outcome and a flag indicating if fallback should be attempted.
func (s *Service) executeWithRetry(
	ctx context.Context,
	chatID int64,
	threadID int,
	req bridge.Request,
	progress ProgressReporter,
	userText string,
) (outcome Outcome, shouldFallback bool) {
	var lastErrMsg string

	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			delay := time.Duration(attempt) * baseRetryDelay // 2s, 4s, 8s
			log.Printf("pi-resilience: retry %d/%d for chat=%d after %v", attempt, maxRetries, chatID, delay)
			
			select {
			case <-ctx.Done():
				log.Printf("pi-resilience: retry canceled for chat=%d", chatID)
				return OutcomeCanceled, false
			case <-time.After(delay):
			}
		}

		ch, err := s.bridge.Execute(ctx, req)
		if err != nil {
			log.Printf("pi-resilience: bridge execute error on attempt %d: %v", attempt, err)
			if attempt == maxRetries {
				return OutcomeBridgeError, true // fallback
			}
			continue
		}

		outcome = s.ProcessBridgeEvents(chatID, threadID, 0, ch, progress, userText)
		
		switch outcome {
		case OutcomeSuccess:
			return OutcomeSuccess, false
		case OutcomeProcessDeath:
			return OutcomeProcessDeath, false // bridge recovery handles this
		case OutcomeLLMError:
			// Check if error is transient
			lastErrMsg = s.lastErrorMessage // populated by handleErrorEvent
			classified := pierror.Classify(lastErrMsg, req.Options.Provider, req.Options.Model)
			if !classified.IsRetryable() {
				log.Printf("pi-resilience: non-retryable error on attempt %d: %s", attempt, classified.Category)
				return outcome, true // fallback for permanent errors too (if fallback configured)
			}
			if attempt < maxRetries {
				log.Printf("pi-resilience: transient error on attempt %d, will retry", attempt)
				continue
			}
			return outcome, true // max retries exceeded, fallback
		}
	}

	return OutcomeMaxRetriesExceeded, true
}
```

**Note**: Precisamos de um mecanismo para `s.lastErrorMessage` ser populado. Alternativa: `ProcessBridgeEvents` retorna o último evento de erro também.

### 4. Fallback Engine (`internal/pipeline/fallback.go`)

- **Purpose**: Gerenciar fallback para OpenRouter free
- **Location**: `internal/pipeline/fallback.go`
- **Changes**: Novo arquivo
- **Reuses**: `config.AppConfig.Providers` para verificar se OpenRouter está configurado

```go
// internal/pipeline/fallback.go
package pipeline

import (
	"fmt"
	"log"

	"github.com/igormaneschy/aurelia/internal/bridge"
	"github.com/igormaneschy/aurelia/internal/pierror"
)

var fallbackModels = []fallbackModel{
	{provider: "openrouter", model: "openrouter/free", desc: "router automático"},
	{provider: "openrouter", model: "qwen/qwen3-coder:free", desc: "Qwen3 Coder"},
	{provider: "openrouter", model: "nvidia/nemotron-3-super-120b-a12b:free", desc: "Nemotron 3 Super"},
}

type fallbackModel struct {
	provider string
	model    string
	desc     string
}

// shouldAttemptFallback checks if OpenRouter is configured and returns true if so.
func (s *Service) shouldAttemptFallback() bool {
	if s.config == nil || s.config.Providers == nil {
		return false
	}
	_, ok := s.config.Providers["openrouter"]
	return ok && s.config.Providers["openrouter"].APIKey != ""
}

// buildFallbackRequest creates a new request for the fallback model.
func (s *Service) buildFallbackRequest(original bridge.Request, modelIdx int) (bridge.Request, string) {
	if modelIdx >= len(fallbackModels) {
		modelIdx = 0
	}
	fb := fallbackModels[modelIdx]
	
	req := original
	req.Options.Provider = fb.provider
	req.Options.Model = fb.model
	req.Options.Continue = false // fallback não pode continuar sessão do provider original
	req.Options.Resume = ""      // sessão nova no fallback
	req.RequestID = ""           // novo request ID
	
	return req, fb.desc
}

// attemptFallback tries OpenRouter free models in order.
func (s *Service) attemptFallback(
	ctx context.Context,
	chatID int64,
	threadID int,
	originalReq bridge.Request,
	progress ProgressReporter,
	userText string,
	classifiedErr pierror.ClassifiedError,
) Outcome {
	if !s.shouldAttemptFallback() {
		log.Printf("pi-resilience: fallback skipped, openrouter not configured")
		_ = s.output.SendError(chatID, threadID, classifiedErr.UserMessage()+"\n\n"+
			"💡 Dica: configure o OpenRouter em `~/.aurelia/config/app.json` para ter uma alternativa.")
		return OutcomeFallbackUnavailable
	}

	// Notify user
	_ = s.output.SendText(chatID, threadID, classifiedErr.FallbackMessage())

	for i, fb := range fallbackModels {
		req, desc := s.buildFallbackRequest(originalReq, i)
		log.Printf("pi-resilience: fallback attempt %d/%d — %s/%s (%s)", i+1, len(fallbackModels), fb.provider, fb.model, desc)

		ch, err := s.bridge.Execute(ctx, req)
		if err != nil {
			log.Printf("pi-resilience: fallback execute error for %s/%s: %v", fb.provider, fb.model, err)
			continue
		}

		outcome := s.ProcessBridgeEvents(chatID, threadID, 0, ch, progress, userText)
		if outcome == OutcomeSuccess {
			log.Printf("pi-resilience: fallback success with %s/%s", fb.provider, fb.model)
			return OutcomeSuccess
		}
		if outcome == OutcomeProcessDeath {
			return OutcomeProcessDeath
		}
		// If error, try next fallback model
	}

	log.Printf("pi-resilience: all fallback models failed")
	_ = s.output.SendError(chatID, threadID, classifiedErr.FinalErrorMessage())
	return OutcomeFallbackFailed
}
```

### 5. Circuit Breaker (`internal/pipeline/circuit_breaker.go`)

- **Purpose**: Rastrear falhas por provider e abrir circuito quando threshold é atingido
- **Location**: `internal/pipeline/circuit_breaker.go`
- **Changes**: Novo arquivo
- **Reuses**: Padrão similar ao `bridgeFailureTracker`

```go
// internal/pipeline/circuit_breaker.go
package pipeline

import (
	"fmt"
	"log"
	"sync"
	"time"
)

const (
	cbFailureThreshold   = 5
	cbWindowDuration     = 2 * time.Minute
	cbOpenDuration       = 5 * time.Minute
	cbHalfOpenMax        = 1 // max requests to test in half-open
)

type circuitState int

const (
	cbClosed circuitState = iota   // normal operation
	cbOpen                         // failing fast, redirect to fallback
	cbHalfOpen                     // testing if provider recovered
)

type circuitBreaker struct {
	mu        sync.RWMutex
	state     circuitState
	failures  []time.Time
	openSince time.Time
	halfOpenTries int
}

func newCircuitBreaker() *circuitBreaker {
	return &circuitBreaker{state: cbClosed}
}

func (cb *circuitBreaker) recordFailure() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	now := time.Now()
	cb.failures = append(cb.failures, now)
	
	// Remove failures outside the window
	cutoff := now.Add(-cbWindowDuration)
	var kept []time.Time
	for _, t := range cb.failures {
		if t.After(cutoff) {
			kept = append(kept, t)
		}
	}
	cb.failures = kept

	if len(cb.failures) >= cbFailureThreshold && cb.state == cbClosed {
		cb.state = cbOpen
		cb.openSince = now
		cb.halfOpenTries = 0
		log.Printf("circuit-breaker: OPEN after %d failures", len(cb.failures))
	}
}

func (cb *circuitBreaker) recordSuccess() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.failures = nil
	
	if cb.state == cbHalfOpen {
		cb.state = cbClosed
		cb.halfOpenTries = 0
		log.Printf("circuit-breaker: CLOSED (recovered)")
	} else if cb.state == cbOpen {
		// Success while open shouldn't happen (we fast-fail), but handle it
		cb.state = cbClosed
		log.Printf("circuit-breaker: CLOSED (unexpected success while open)")
	}
}

func (cb *circuitBreaker) allow() bool {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	switch cb.state {
	case cbClosed:
		return true
	case cbOpen:
		if time.Since(cb.openSince) > cbOpenDuration {
			cb.state = cbHalfOpen
			cb.halfOpenTries = 0
			log.Printf("circuit-breaker: HALF-OPEN (timeout expired)")
			return true
		}
		return false
	case cbHalfOpen:
		if cb.halfOpenTries < cbHalfOpenMax {
			cb.halfOpenTries++
			return true
		}
		// Max half-open tries reached, stay in half-open until success/failure
		return false
	}
	return true
}

func (cb *circuitBreaker) stateString() string {
	cb.mu.RLock()
	defer cb.mu.RUnlock()
	switch cb.state {
	case cbClosed:
		return "closed"
	case cbOpen:
		return fmt.Sprintf("open (since %s)", time.Since(cb.openSince).Round(time.Second))
	case cbHalfOpen:
		return "half-open"
	}
	return "unknown"
}
```

**Circuit Breaker por Provider**: Precisamos de um mapa de provider → circuit breaker. Vamos adicionar ao Service:

```go
// service.go — adicionar ao Service struct
type Service struct {
	// ... existing fields ...
	circuitBreakers map[string]*circuitBreaker
	cbMu            sync.RWMutex
}

func (s *Service) circuitBreakerFor(provider string) *circuitBreaker {
	s.cbMu.Lock()
	defer s.cbMu.Unlock()
	if s.circuitBreakers == nil {
		s.circuitBreakers = make(map[string]*circuitBreaker)
	}
	cb, ok := s.circuitBreakers[provider]
	if !ok {
		cb = newCircuitBreaker()
		s.circuitBreakers[provider] = cb
	}
	return cb
}
```

### 6. Bridge Layer — Prevenção de Silenciamento

- **Purpose**: Garantir que erros no catch do bridge sejam sempre propagados
- **Location**: `bridge/index.ts:handleQuery` (TypeScript)
- **Changes**: Pequena modificação no catch block
- **Reuses**: Lógica existente de `emitTerminalError`

```typescript
// bridge/index.ts — modificar catch block em handleQuery
catch (err: unknown) {
  if (!terminalEmitted) {
    const errMsg = err instanceof Error ? err.message : String(err);
    log(`query error: rid=${reqId} ${errMsg}`);
    emitTerminalError(errMsg);
  } else {
    // ERROR WAS SILENCED BEFORE — now we emit it even after partial output
    const errMsg = err instanceof Error ? err.message : String(err);
    log(`query error after terminal: rid=${reqId} ${errMsg}`);
    emitTerminalError("processing interrupted: " + errMsg);
  }
}
```

**Nota**: Esta mudança no bridge TypeScript requer rebuild do bundle:
```bash
cd bridge && npm run build
cp bundle.js ../internal/bridge/bundle.js
```

### 7. Pipeline Integration — `executeAsync` Modificado

- **Purpose**: Integrar retry, fallback, e circuit breaker no flow principal
- **Location**: `internal/pipeline/pipeline.go:executeAsync`
- **Changes**: Refatorar `executeAsync` para usar `executeWithRetry` e `attemptFallback`

```go
// pipeline.go — executeAsync refatorado (simplificado)
func (s *Service) executeAsync(parentCtx context.Context, chatID int64, threadID int, messageID int, req bridge.Request, userText string) {
	stopTyping := s.output.StartTyping(chatID, threadID)
	defer stopTyping()

	progress := s.output.NewProgress(chatID, threadID)
	defer progress.Delete()

	ctx, cancel := context.WithTimeout(parentCtx, bridgeExecutionTimeout)
	defer cancel()
	cancelDone := s.cancelBridgeOnContextDone(ctx, req.RequestID)
	defer cancelDone()

	// Check circuit breaker
	provider := req.Options.Provider
	cb := s.circuitBreakerFor(provider)
	if !cb.allow() {
		log.Printf("pi-resilience: circuit breaker OPEN for provider %s, skipping to fallback", provider)
		classified := pierror.ClassifiedError{Provider: provider, Model: req.Options.Model, Category: pierror.CatTransient}
		s.attemptFallback(ctx, chatID, threadID, req, progress, userText, classified)
		return
	}

	// Execute with retry
	outcome, shouldFallback := s.executeWithRetry(ctx, chatID, threadID, req, progress, userText)
	
	if outcome == OutcomeSuccess {
		cb.recordSuccess()
		s.bridgeFailures.reset()
		return
	}
	
	if outcome == OutcomeProcessDeath {
		// Bridge recovery handles this — don't record as PI failure
		return
	}
	
	if outcome == OutcomeCanceled {
		return
	}

	// Failure — record for circuit breaker
	cb.recordFailure()
	
	if shouldFallback {
		lastErrMsg := s.lastErrorMessage // populated during event processing
		classified := pierror.Classify(lastErrMsg, provider, req.Options.Model)
		s.attemptFallback(ctx, chatID, threadID, req, progress, userText, classified)
	} else {
		// Non-retryable error without fallback
		classified := pierror.Classify(s.lastErrorMessage, provider, req.Options.Model)
		_ = s.output.SendError(chatID, threadID, classified.UserMessage())
	}
}
```

---

## Data Models

### Novos tipos no pipeline

```go
type Outcome int

const (
	OutcomeSuccess Outcome = iota
	OutcomeLLMError
	OutcomeProcessDeath
	OutcomeCanceled
	OutcomeBridgeError
	OutcomeMaxRetriesExceeded
	OutcomeFallbackUnavailable
	OutcomeFallbackFailed
)
```

### Novo campo no Service

```go
type Service struct {
	// ... existing fields ...
	circuitBreakers  map[string]*circuitBreaker
	cbMu             sync.RWMutex
	lastErrorMessage string // populated by handleErrorEvent
}
```

---

## Error Handling Strategy

| Error Scenario | Handling | User Impact |
|----------------|----------|-------------|
| Rate limit (transient) | Retry 3x com backoff 2s→4s→8s | Delay de até 14s, depois fallback |
| Timeout (transient) | Retry 3x | Mesmo que rate limit |
| 503/502 (transient) | Retry 3x | Mesmo que rate limit |
| Auth error (permanent) | NO retry, fallback se OpenRouter configurado | Erro traduzido imediato |
| Model not found (permanent) | NO retry, NO fallback | Erro sugerindo /model |
| Context length (permanent) | NO retry, NO fallback | Erro sugerindo /new |
| Circuit breaker OPEN | Skip provider, vai direto fallback | Mensagem breve de instabilidade |
| Fallback OpenRouter sucesso | Resposta normal | "⚡ usando alternativa..." + resposta |
| Fallback OpenRouter falha | Erro final | "❌ Todos os providers instáveis" |
| Bridge process death | Bridge recovery (existente) | Não afeta PI resilience |
| Erro após terminalEmitted=true | Emitir erro adicional | Mensagem de interrupção |

---

## Tech Decisions

| Decision | Choice | Rationale |
|----------|--------|-----------|
| Retry no Go layer, não no bridge | Pipeline Service | Bridge é genérico; decisão de retry/fallback é de negócio |
| Circuit breaker em memória apenas | `map[string]*circuitBreaker` | Persistência em disco é overkill para um daemon single-instance |
| Fallback só para OpenRouter free | `openrouter/free` | Foco do escopo; modelos free são suficientes para resiliência básica |
| Não retry em `ExecuteSync` | Pipeline layer | ExecuteSync é usado por classify/cron, que são idempotentes e curtos |
| Bridge TypeScript modificado | catch block | Erros silenciados são um bug real; fix requer rebuild do bundle |
| Reset de session no fallback | `Continue=false, Resume=""` | Sessões PI não são portáveis entre providers; sessão nova é aceitável |
| Mensagens breves de fallback | 1 mensagem por fallback | Evita spam; usuário precisa saber que alternativa está sendo usada |
| Circuit breaker threshold: 5 erros / 2min | Configurado em código | Valor conservador — evita abrir por picos isolados |
| Circuit breaker recovery: 5min half-open | Configurado em código | Tempo suficiente para provider se recuperar |
| Fallback models ordenados | Router → Coder → Nemotron | Router é mais flexível; Coder é especializado; Nemotron é generalista |

---

## Scope Summary

**Arquivos a criar:**
1. `internal/pierror/classifier.go` — Classificação de erros do PI
2. `internal/pierror/translator.go` — Tradução para português
3. `internal/pipeline/retry.go` — Engine de retry com backoff
4. `internal/pipeline/fallback.go` — Engine de fallback para OpenRouter
5. `internal/pipeline/circuit_breaker.go` — Circuit breaker por provider

**Arquivos a modificar:**
1. `internal/pipeline/pipeline.go` — `executeAsync` integrado com retry/fallback/CB; `handleErrorEvent` popula `lastErrorMessage`
2. `internal/pipeline/service.go` — Adicionar `circuitBreakers` e `lastErrorMessage` ao Service
3. `bridge/index.ts` — Fix silenciamento de erro no catch block
4. `internal/bridge/bundle.js` — Rebuild após mudança no TS

**Arquivos de teste:**
1. `internal/pierror/classifier_test.go` — Testes de classificação
2. `internal/pierror/translator_test.go` — Testes de tradução
3. `internal/pipeline/retry_test.go` — Testes de retry
4. `internal/pipeline/fallback_test.go` — Testes de fallback
5. `internal/pipeline/circuit_breaker_test.go` — Testes de circuit breaker
6. `internal/pipeline/pipeline_test.go` — Testes de integração

**Estimativa de mudança:** ~300 linhas de produção, ~400 linhas de teste
