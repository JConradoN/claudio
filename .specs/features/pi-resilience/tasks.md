# PI Resilience — Tasks

**Design**: `.specs/features/pi-resilience/design.md`
**Status**: Draft

---

## Execution Plan

### Phase 1: Foundation (Parallel)

```
T1 ──┐
T2 ──┼──→ (independentes)
T3 ──┘
```

### Phase 2: Core (Parallel)

```
     ┌→ T4 ─┐
T1 ──┤      ├──→ T6
     └→ T5 ─┘
T2 ───────────→ T6
T3 ───────────→ T6
```

### Phase 3: Integration (Sequential)

```
T6 → T7 → T8
```

---

## Task Breakdown

### T1: Error Classifier (`pierror.Classify`)

**What**: Implementar classificação de mensagens de erro do PI em categorias (transient, auth, model not found, context length, content policy, permanent).
**Where**: `internal/pierror/classifier.go`
**Depends on**: None
**Reuses**: Apenas pattern matching em strings

**Implementation details**:
- Tipo `Category int` com constantes: `CatTransient`, `CatAuth`, `CatModelNotFound`, `CatContextLength`, `CatContentPolicy`, `CatPermanent`
- Tipo `ClassifiedError` com `Original`, `Category`, `Provider`, `Model`
- Função `Classify(errMsg, provider, model string) ClassifiedError`
- Pattern matching em lowercase com substring matching
- Métodos: `IsTransient()`, `IsRetryable()`

**Erros a mapear**:
- Transient: "rate limit", "timeout", "timed out", "deadline exceeded", "503", "502", "504", "econnrefused", "econnreset", "network"
- Auth: "unauthorized", "invalid api key", "api key", "authentication", "auth"
- ModelNotFound: "model not found", "unknown model", "invalid model"
- ContextLength: "context length", "context too long", "maximum context"
- ContentPolicy: "content policy", "moderation", "safety", "blocked"
- Permanent: tudo o mais

**Done when**:
- [ ] `classifier.go` implementado
- [ ] Teste: cada categoria de erro é classificada corretamente
- [ ] Teste: erro desconhecido → `CatPermanent`
- [ ] Teste: erro vazio → `CatPermanent`
- [ ] Teste: case insensitive
- [ ] Tests pass: `go test ./internal/pierror/...`

**Verify:**
```bash
go test ./internal/pierror/ -run TestClassify -v
```

---

### T2: Error Translator (`pierror.UserMessage`)

**What**: Traduzir erros classificados para mensagens amigáveis em português.
**Where**: `internal/pierror/translator.go`
**Depends on**: T1
**Reuses**: Estrutura `ClassifiedError`

**Implementation details**:
- Método `UserMessage()` com switch por categoria
- Mensagens em português com emoji e ação sugerida
- Método `FallbackMessage()` para notificação de fallback
- Método `FinalErrorMessage()` para quando todos os fallbacks falham

**Done when**:
- [ ] `translator.go` implementado
- [ ] Teste: cada categoria gera mensagem em português
- [ ] Teste: mensagens incluem provider/model quando relevante
- [ ] Teste: `FallbackMessage()` contém provider original
- [ ] Teste: `FinalErrorMessage()` é genérica e acionável
- [ ] Tests pass: `go test ./internal/pierror/...`

**Verify:**
```bash
go test ./internal/pierror/ -run TestTranslator -v
```

---

### T3: Bridge Fix — Prevenção de Silenciamento

**What**: Modificar o catch block de `handleQuery` no bridge TypeScript para emitir erro mesmo quando `terminalEmitted=true`.
**Where**: `bridge/index.ts`
**Depends on**: None
**Reuses**: `emitTerminalError()` existente

**Implementation details**:
- No catch block, remover a condição `if (!terminalEmitted)` como guarda única
- Adicionar branch else que emite erro com prefixo "processing interrupted:"
- Rebuild do bundle com `npm run build`
- Copiar `bundle.js` para `internal/bridge/bundle.js`

**Done when**:
- [ ] `bridge/index.ts` modificado
- [ ] Bundle rebuildado e copiado
- [ ] Teste manual: simular erro após terminal emitted
- [ ] Build compila: `go build ./cmd/aurelia/`

**Verify:**
```bash
cd bridge && npm run build
cp bundle.js ../internal/bridge/bundle.js
go build ./cmd/aurelia/
```

---

### T4: Circuit Breaker

**What**: Implementar circuit breaker por provider.
**Where**: `internal/pipeline/circuit_breaker.go`
**Depends on**: None
**Reuses**: Padrão de `bridgeFailureTracker`

**Implementation details**:
- Struct `circuitBreaker` com estado (closed/open/half-open), lista de falhas, timestamps
- `recordFailure()`: adiciona timestamp, limpa antigos, abre se threshold atingido
- `recordSuccess()`: limpa falhas, fecha se half-open
- `allow()`: retorna true/false baseado no estado; transiciona open→half-open após timeout
- `stateString()`: para debug/logs
- `circuitBreakerFor(provider string)` no Service

**Constants**:
- Threshold: 5 falhas
- Window: 2 minutos
- Open duration: 5 minutos
- Half-open max: 1 request

**Done when**:
- [ ] `circuit_breaker.go` implementado
- [ ] Teste: 5 falhas em 2min → circuito abre
- [ ] Teste: circuito aberto → `allow()` retorna false
- [ ] Teste: após 5min → half-open, `allow()` retorna true (1x)
- [ ] Teste: half-open sucesso → circuito fecha
- [ ] Teste: half-open falha → circuito reabre
- [ ] Tests pass: `go test ./internal/pipeline/...`

**Verify:**
```bash
go test ./internal/pipeline/ -run TestCircuitBreaker -v
```

---

### T5: Retry Engine

**What**: Implementar retry com exponential backoff para erros transitórios.
**Where**: `internal/pipeline/retry.go`
**Depends on**: T1
**Reuses**: `bridge.Execute()`, `ProcessBridgeEvents()`

**Implementation details**:
- `executeWithRetry(ctx, chatID, threadID, req, progress, userText) (Outcome, bool)`
- Loop de 0 a maxRetries (3)
- Delay: attempt * 2s (2s, 4s, 8s)
- Cancelável via context
- Retorna `OutcomeSuccess`, `OutcomeProcessDeath`, `OutcomeCanceled`, ou `(OutcomeLLMError/OutcomeBridgeError, shouldFallback bool)`
- Popula `s.lastErrorMessage` para uso posterior

**Done when**:
- [ ] `retry.go` implementado
- [ ] Teste: erro transient → 3 retries com delays crescentes
- [ ] Teste: retry bem-sucedido na 2ª tentativa → sucesso
- [ ] Teste: todos os retries falham → shouldFallback=true
- [ ] Teste: cancelamento durante backoff → aborta imediatamente
- [ ] Teste: erro não-transient → sem retry, shouldFallback=true
- [ ] Tests pass: `go test ./internal/pipeline/...`

**Verify:**
```bash
go test ./internal/pipeline/ -run TestRetry -v
```

---

### T6: Fallback Engine

**What**: Implementar fallback para OpenRouter free.
**Where**: `internal/pipeline/fallback.go`
**Depends on**: T1, T2, T5
**Reuses**: `config.AppConfig.Providers`, `bridge.Execute()`

**Implementation details**:
- `shouldAttemptFallback()` — verifica se OpenRouter está configurado
- `buildFallbackRequest(original, modelIdx)` — cria request com provider/model alternativo
- `attemptFallback(ctx, chatID, threadID, originalReq, progress, userText, classifiedErr)` — tenta modelos em ordem
- Modelos fallback:
  1. `openrouter/free`
  2. `qwen/qwen3-coder:free`
  3. `nvidia/nemotron-3-super-120b-a12b:free`
- Notifica usuário brevemente antes de tentar
- Se OpenRouter não configurado → erro com dica de configuração

**Done when**:
- [ ] `fallback.go` implementado
- [ ] Teste: fallback com OpenRouter configurado → sucesso
- [ ] Teste: fallback com OpenRouter não configurado → erro com dica
- [ ] Teste: fallback router falha → tenta coder → tenta nemotron
- [ ] Teste: todos os fallbacks falham → erro final traduzido
- [ ] Teste: fallback notifica usuário brevemente
- [ ] Tests pass: `go test ./internal/pipeline/...`

**Verify:**
```bash
go test ./internal/pipeline/ -run TestFallback -v
```

---

### T7: Pipeline Integration

**What**: Integrar classifier, retry, fallback, e circuit breaker no `executeAsync`.
**Where**: `internal/pipeline/pipeline.go` (executeAsync), `internal/pipeline/service.go`
**Depends on**: T1, T2, T3, T4, T5, T6
**Reuses**: Todo o fluxo existente

**Implementation details**:
- `Service`: adicionar `circuitBreakers map[string]*circuitBreaker`, `cbMu sync.RWMutex`, `lastErrorMessage string`
- `executeAsync` refatorado:
  1. Verificar circuit breaker (`cb.allow()`)
  2. Se circuito aberto → fallback direto
  3. Executar com retry (`executeWithRetry`)
  4. Se sucesso → `cb.recordSuccess()`
  5. Se falha → `cb.recordFailure()`
  6. Se shouldFallback → `attemptFallback`
  7. Se não-retryable sem fallback → `SendError` com mensagem traduzida
- `handleErrorEvent`: popular `s.lastErrorMessage` antes de enviar erro
- `handleResultEvent`: limpar `s.lastErrorMessage`

**Done when**:
- [ ] `executeAsync` integrado com retry + fallback + CB
- [ ] `handleErrorEvent` popula `lastErrorMessage`
- [ ] `handleResultEvent` limpa `lastErrorMessage`
- [ ] Teste: fluxo completo: sucesso normal
- [ ] Teste: fluxo completo: erro transient → retry → sucesso
- [ ] Teste: fluxo completo: erro transient → retry → fallback → sucesso
- [ ] Teste: fluxo completo: circuito aberto → fallback direto
- [ ] Teste: fluxo completo: erro não-retryable → erro traduzido
- [ ] Tests pass: `go test ./internal/pipeline/...`

**Verify:**
```bash
go test ./internal/pipeline/ -run TestExecuteAsync -v
```

---

### T8: Integration & Regression

**What**: Testes de integração end-to-end e verificação de regressão.
**Where**: Todos os pacotes
**Depends on**: T7
**Reuses**: Testes existentes

**Implementation details**:
- `go test ./... -short`
- `go build ./cmd/aurelia/`
- Verificar que bridge recovery (feature existente) ainda funciona
- Verificar que cron jobs (que usam ExecuteSync) não são afetados
- Verificar que classify (que usa ExecuteSync) não é afetado

**Done when**:
- [ ] `go test ./... -short` passa
- [ ] `go build ./cmd/aurelia/` compila
- [ ] Teste manual: simular rate limit → retry → resposta
- [ ] Teste manual: simular provider down → fallback OpenRouter
- [ ] Nenhuma regressão nos testes existentes

**Verify:**
```bash
go test ./... -short
go build ./cmd/aurelia/
```

---

## Parallel Execution Map

```
Phase 1 (Parallel, independentes):
  T1 ── Error Classifier
  T2 ── Error Translator
  T3 ── Bridge Fix (TS)

Phase 2 (Parallel, T1/T2 são pré-requisitos de T5/T6; T4 independente):
  T4 ── Circuit Breaker
  T5 ── Retry Engine (depende T1)
  T6 ── Fallback Engine (depende T1, T2)

Phase 3 (Sequential, após T1-T6):
  T7 ── Pipeline Integration
  T8 ── Integration & Regression
```

**Ordem real de execução:**
```
T1 ────────────────────────────────────────────────┐
T2 ────────────────────────────────────────────────┤
T3 ────────────────────────────────────────────────┤
T4 ────────────────────────────────────────────────┼→ T7 → T8
T5 (depois T1) ────────────────────────────────────┤
T6 (depois T1, T2) ────────────────────────────────┘
```

---

## Task Granularity Check

| Task | Scope | Status |
|------|-------|--------|
| T1: Classifier | 1 arquivo, ~60 linhas | Granular |
| T2: Translator | 1 arquivo, ~50 linhas | Granular |
| T3: Bridge Fix | 5 linhas TS + rebuild | Granular |
| T4: Circuit Breaker | 1 arquivo, ~120 linhas | Granular |
| T5: Retry Engine | 1 arquivo, ~80 linhas | Granular |
| T6: Fallback Engine | 1 arquivo, ~100 linhas | Granular |
| T7: Integration | Refactor de executeAsync + service | Granular |
| T8: Integration Test | Suite completa | Granular |
