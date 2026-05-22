# Delegate to PI SDK Native — Tasks

Baseado no design: `.specs/features/delegate-to-pi-sdk/design.md`  
Dependency graph: 0→1→2→3→4→5→6→7  

**Current status (2026-05-21):** Tasks 0–5 and bridge security hook correction are effectively complete in v0.13.x. Task 6 is no longer the recommended next step as written; keep `internal/agents` as an Aurelia product-layer registry for now and investigate PI-native parsing/discovery via `agentsFilesOverride` later. The remaining near-term work is docs/spec cleanup plus E2E validation.

---

## Task 0: PI SDK API Validation

**Done When:**
- [ ] PI SDK version anotado (`npm list @earendil-works/pi-coding-agent`)
- [ ] `ModelRegistry.find(provider, model)` retorna modelo correto
- [ ] `ModelRegistry.getAll()` lista todos os modelos
- [ ] `SessionManager.create(cwd)` cria sessão persistente
- [ ] `SessionManager.open(path)` reabre sessão existente
- [ ] `SettingsManager.inMemory({ compaction: { enabled: true } })` funciona
- [ ] `DefaultResourceLoader` com `noContextFiles: false` carrega `CLAUDE.md`/`AGENTS.md`
- [x] `createAgentSession` não expõe `beforeToolCall` como opção; Bridge usa `session.agent.beforeToolCall`
- [ ] Investigar agent markdown discovery/parsing via PI `agentsFilesOverride` sem migrar arquivos do Aurélia
- [ ] Documentação salva em `docs/pi-sdk-api-validation.md`

**Comandos:**
```bash
cd bridge && npm list @earendil-works/pi-coding-agent
```

---

## Task 1: Bridge — Simplify Model Resolution

**Arquivo:** `bridge/index.ts`  
**Depends on:** Task 0

**Done When:**
- [ ] `resolveModelFromRegistry()` eliminado
- [ ] Nova função `resolveModel()` usa `registry.find()` nativo + fallback `getAll()`
- [ ] Fuzzy matching por `model.name` removido
- [ ] Aliases de provider (`mapProvider()`) mantidos — são Aurélia-specific
- [ ] `mapModelForProvider()` mantido — Kimi mapping
- [ ] `npm run build` passa (bundle.js)
- [ ] `go build ./...` passa com novo bundle

---

## Task 2: Go — Remove Security Policy Engine

**Arquivos:** `internal/security/policy.go`, `internal/security/security_test.go`  
**Depends on:** Task 1 (Bridge security hooks devem estar estáveis)

**Done When:**
- [ ] `internal/security/policy.go` removido
- [ ] `internal/security/security_test.go` removido
- [ ] `internal/security/profiles.go` reduzido a apenas constantes (type + consts)
- [ ] `internal/security/audit.go` mantido
- [ ] Nenhum call site de `security.EvaluateToolCall` restante em Go (exceto testes removidos)
- [ ] `go build ./...` passa
- [ ] `go vet ./...` passa
- [ ] `go test ./... -short` passa (testes de security removidos, outros passam)

**Call sites a verificar:**
```bash
grep -r "security\.EvaluateToolCall\|security\.PolicyDecision\|security\.DefaultConfig" --include="*.go" . | grep -v "_test.go"
```

---

## Task 3: Go — Simplify Session Store

**Arquivo:** `internal/session/store.go`  
**Depends on:** Task 2

**Done When:**
- [ ] `entry` struct reduzido: remove `sessionID`, adiciona `sessionFile`
- [ ] `Get()` retorna `sessionFile` em vez de `sessionID`
- [ ] `Set()` aceita `sessionFile` em vez de `sessionID`
- [ ] `GetSession()` / `SetSession()` atualizados
- [ ] `Deactivate()` / `DeactivateAll()` mantidos
- [ ] `GC()` mantido
- [ ] `go build ./...` passa
- [ ] `go vet ./...` passa
- [ ] `go test ./internal/session/... -v` passa

**Arquivos de teste a ajustar:**
- `internal/session/store_test.go`

---

## Task 4: Go — Remove Token Tracker

**Arquivos:** `internal/session/tracker.go`, `internal/session/tracker_test.go`  
**Depends on:** Task 3

**Done When:**
- [ ] `internal/session/tracker.go` removido
- [ ] `internal/session/tracker_test.go` removido
- [ ] Bridge habilita `compaction: { enabled: true }` em `SettingsManager`
- [ ] `/usage` command refatorado: extrai stats do último `result` event (acumula para display)
- [ ] `go build ./...` passa
- [ ] `go vet ./...` passa
- [ ] `go test ./internal/session/... -v` passa

**Nota:** Verificar todos os call sites de `tracker.`:
```bash
grep -r "tracker\." --include="*.go" . | grep -v "_test.go"
```

---

## Task 5: Go — Refactor Prompt Builder

**Arquivo:** `internal/pipeline/prompt_builder.go`  
**Depends on:** Task 4

**Done When:**
- [ ] `buildProjectDocsSection()` removido
- [ ] Carregamento manual de `CLAUDE.md`/`AGENTS.md` removido do assembly
- [ ] Bridge `DefaultResourceLoader` usa `noContextFiles: false`
- [ ] System prompt assembly reduzido a 6 seções (ver design)
- [ ] `go build ./...` passa
- [ ] `go vet ./...` passa
- [ ] `go test ./internal/pipeline/... -v` passa
- [ ] E2E: agente vê CLAUDE.md quando cwd está setado
- [ ] E2E: agente vê AGENTS.md quando cwd está setado

---

## Task 6: Agent Registry Boundary Decision — Keep Product Layer, Investigate PI Discovery

**Arquivos:** `internal/agents/registry.go`, `internal/agents/types.go`, `bridge/index.ts`  
**Depends on:** Task 5

**Done When:**
- [ ] Decisão documentada: especialistas Aurelia permanecem em `~/.aurelia/agents/` no curto prazo
- [ ] Nenhuma migração obrigatória para `~/.pi/agent/agents/` no MVP
- [ ] Investigar se `DefaultResourceLoader.agentsFilesOverride` permite PI-native parsing sem mover arquivos
- [ ] Se viável, criar design separado para delegar parsing/discovery ao PI mantendo storage Aurelia
- [ ] `internal/agents` continua responsável por routing/classification/metadata de produto até existir alternativa validada
- [ ] `go build ./...` passa
- [ ] `go vet ./...` passa
- [ ] `go test ./... -short` passa

**Call sites a verificar:**
```bash
grep -r "agents\." --include="*.go" . | grep -v "_test.go"
```

---

## Task 7: Bridge Rebuild + Integration Validation

**Depends on:** Tasks 1–6

**Done When:**
- [ ] `cd bridge && npm run build` succeeds
- [ ] Bundle copied to `internal/bridge/bundle.js`
- [ ] `go build ./...` passes with new bundle
- [ ] `go vet ./...` passes
- [ ] `go test ./... -short` passes
- [ ] `go test ./e2e/...` passes
- [ ] Integration: Telegram message → response funciona
- [ ] Integration: `@prospector` routes to correct agent
- [ ] Integration: `cat ~/.aurelia/config/app.json` blocked by security hook
- [ ] Integration: `go test ./...` works in coding context
- [ ] Integration: Session resume after bridge crash works
- [ ] Integration: `/usage` shows tokens/cost
- [ ] Integration: CLAUDE.md visible to agent when cwd set

**Comandos:**
```bash
cd bridge && npm run build
cp bundle.js ../internal/bridge/bundle.js
cd ..
go build ./...
go vet ./...
go test ./... -short
go test ./e2e/...
```

---

## Task 8: Documentation Update

**Depends on:** Task 7

**Done When:**
- [ ] README.md updated: remove references to `~/.aurelia/agents/` (now `~/.pi/agent/agents/`)
- [ ] CHANGELOG.md updated with v0.x.y entry
- [ ] Migration guide added: how to move agents
- [ ] Security docs updated: policy engine moved to Bridge
- [ ] `docs/pi-sdk-api-validation.md` completed with findings

---

## Task Graph

```
Task 0: API Validation
    │
    ▼
Task 1: Bridge Model Resolution
    │
    ▼
Task 2: Go Security Cleanup
    │
    ▼
Task 3: Session Store Simplify
    │
    ▼
Task 4: Token Tracker Remove
    │
    ▼
Task 5: Prompt Builder Refactor
    │
    ▼
Task 6: Agent Registry Boundary Decision
    │
    ▼
Task 7: Integration Validation
    │
    ▼
Task 8: Documentation
```

**Cada task pode ser desenvolvida em uma branch separada e merged via PR.**  
**Ordem recomendada:** Tasks 0→1→2→3→4→5→6→7→8

---

## Notes

- **Não alterar:** `internal/persona/`, `internal/dream/`, `internal/cron/`, `internal/telegram/`, `internal/orchestrator/`, `internal/continuity/`, `internal/runlog/`
- **Não remover agora:** `internal/agents/`; ele ainda é produto Aurélia até validação específica de PI-native discovery/parsing
- **Manter:** `internal/bridge/bridge.go` (protocolo NDJSON), `internal/bridge/protocol.go`
- **Verificar antes de cada task:** `grep` por call sites do código a ser removido
- **Se blocker encontrado:** documentar em `docs/pi-sdk-api-validation.md` e pular task
