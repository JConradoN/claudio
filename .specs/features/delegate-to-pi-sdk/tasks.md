# Delegate to PI SDK Native — Tasks

Baseado no design: `.specs/features/delegate-to-pi-sdk/design.md`  
Dependency graph: 0→1→2→3→4→5→6→7  

**Status: Sprint 0 concluído em 2026-05-22.** Tasks 0–5 e 7 implementados e validados. Task 6 adiada por decisão (manter `internal/agents/`). Task 8 parcial (CHANGELOG + specs, sem migração de agentes).

---

## Task 0: PI SDK API Validation

**Done When:**
- [x] PI SDK version anotado (`npm list @earendil-works/pi-coding-agent`)
- [x] `ModelRegistry.find(provider, model)` retorna modelo correto
- [x] `ModelRegistry.getAll()` lista todos os modelos
- [x] `SessionManager.create(cwd)` cria sessão persistente
- [x] `SessionManager.open(path)` reabre sessão existente
- [x] `SettingsManager.inMemory({ compaction: { enabled: true } })` funciona
- [x] `DefaultResourceLoader` com `noContextFiles: false` carrega `CLAUDE.md`/`AGENTS.md`
- [x] `createAgentSession` não expõe `beforeToolCall` como opção; Bridge usa `session.agent.beforeToolCall`
- [ ] ~~Investigar agent markdown discovery/parsing via PI~~ (adiado — Task 6)
- [x] Documentação salva em `docs/pi-sdk-api-validation.md`

---

## Task 1: Bridge — Simplify Model Resolution

**Done When:**
- [x] `resolveModelFromRegistry()` substituído por `resolveModel()`
- [x] Nova função usa `registry.find()` nativo + fallback `getAll()`
- [x] Aliases de provider (`mapProvider()`) mantidos
- [x] `mapModelForProvider()` mantido
- [x] `npm run build` passa
- [x] `go build ./...` passa

---

## Task 2: Go — Remove Security Policy Engine

**Done When:**
- [x] `internal/security/policy.go` simplificado (mantém apenas tipos/config)
- [x] `internal/security/security_test.go` removido
- [x] `internal/security/profiles.go` reduzido a constantes
- [x] `internal/security/audit.go` mantido
- [x] Avaliação de política movida para Bridge (TS `session.agent.beforeToolCall`)
- [x] `go build ./...` passa
- [x] `go vet ./...` passa
- [x] `go test ./... -short` passa

---

## Task 3: Go — Simplify Session Store

**Done When:**
- [x] `entry` struct usa `sessionFile` em vez de `sessionID`
- [x] `Get/Set` trabalham com `sessionFile`
- [x] `Deactivate()`, `GC()` mantidos
- [x] `go build ./...` passa
- [x] `go vet ./...` passa
- [x] `go test ./internal/session/...` passa

---

## Task 4: Go — Remove Token Tracker

**Done When:**
- [x] `internal/session/tracker.go` simplificado (removeu auto-reset)
- [x] `internal/session/tracker_test.go` removido
- [x] Bridge usa `compaction: { enabled: true }` no `SettingsManager`
- [x] `/usage` command refatorado: não depende mais de tracker
- [x] `go build ./...` passa
- [x] `go test ./internal/session/...` passa

---

## Task 5: Go — Refactor Prompt Builder

**Done When:**
- [x] Carregamento manual de `CLAUDE.md`/`AGENTS.md` removido do assembly
- [x] Bridge `DefaultResourceLoader` usa `noContextFiles: false`
- [x] System prompt assembly mantém 6 seções Aurelia-specific (persona, memória, Telegram, segurança, continuidade, cron)
- [x] `go build ./...` passa
- [x] `go vet ./...` passa
- [x] `go test ./... -short` passa
- [x] E2E: agente vê CLAUDE.md/AGENTS.md via PI SDK quando cwd setado

---

## Task 6: Agent Registry Boundary Decision

**Status:** Adiado. `internal/agents/` permanece como produto Aurelia.

**Decisão:** Manter `internal/agents/` como feature de produto. PI-native discovery via `agentsFilesOverride` é tecnicamente viável mas não agrega valor hoje — o registry atual é estável, testado e tem 0 bugs conhecidos. Revisitar quando houver demanda por agentes cross-PI.

---

## Task 7: Bridge Rebuild + Integration Validation

**Done When:**
- [x] `cd bridge && npm run build` succeeds
- [x] `go build ./...` passa com novo bundle
- [x] `go vet ./...` passa
- [x] `go test ./... -short` passa (todos verdes)
- [x] Integração: Telegram message → response funciona (validado ao vivo)
- [x] Integração: `/stop` com userID funciona
- [x] Integração: Auth symlink → credentials sempre em sync
- [x] Integração: Modelo não encontrado → erro claro
- [x] Integração: Segurança bloqueia `rm -rf /` via Bridge
- [x] Integração: Session resume funciona
- [x] Integração: Grupos funcionam (com `telegram_allowed_group_ids`)

---

## Task 8: Documentation Update

**Done When:**
- [x] CHANGELOG.md atualizado (v0.13.7)
- [x] Branch policy adicionada ao AGENTS.md
- [ ] ~~README.md: migração de agentes~~ (não aplicável — Task 6 adiada)
- [ ] ~~Migration guide~~ (não aplicável — Task 6 adiada)
- [x] `docs/pi-sdk-api-validation.md` salvo com findings

---

## Notas finais

- **internal/agents/** mantido como produto Aurelia — sem migração para PI SDK
- **internal/persona/** mantido — sem equivalente no PI SDK
- **internal/dream/** mantido — memória cross-session Aurelia
- **internal/cron/** mantido — scheduling
- **internal/orchestrator/** mantido — fluxo de execução orquestrada (Sprint B)
- **internal/telegram/** mantido — interface de usuário
- **internal/bridge/bridge.go** mantido — protocolo NDJSON Go↔TS
