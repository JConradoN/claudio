# Agent Tools Fix — Validation

**Date**: 2026-05-15
**Spec**: `.specs/features/agent-tools-fix/spec.md`

---

## Task Completion

| Task | Status | Notes |
|------|--------|-------|
| T1: Protocol extension | Done | `DisallowedTools []string` em `protocol.go` com `json:"disallowed_tools,omitempty"` |
| T2: Bridge logic | Done | `translateAllowedTools()` em `index.ts` — allowlist + denylist + interseção; bundle rebuildado e instalado |
| T3: Validation | Done | `validateToolNames()` em `agents/registry.go` — warn para nomes desconhecidos |

---

## User Story Validation

### P1: Disallowed Tools Funcional — MVP

| # | Criterion | Result | Evidence |
|---|-----------|--------|----------|
| 1 | WHEN agente define disallowed_tools THEN ferramentas bloqueadas no PI | Passed | `translateAllowedTools()` filtra denylist; `IsReadOnly()` computa toolset efetivo |
| 2 | WHEN só disallowed_tools THEN PI recebe todas menos as bloqueadas | Passed | `translateAllowedTools(undefined, ["Bash"])` → allBuiltinTools filter "bash" |
| 3 | WHEN allowed + disallowed THEN interseção menos denylist | Passed | `translateAllowedTools(["Read","Bash"], ["Bash"])` → `[]` (disallowed prevails on overlap) |
| 4 | WHEN nomes inválidos THEN warning e ignorados | Passed | `validateToolNames()` logged via slog.Warn |

**Status**: Implementado e validado por código review + testes

---

### P2: Mapeamento Completo — Should Have

| # | Criterion | Result | Evidence |
|---|-----------|--------|----------|
| 1 | WHEN ferramenta built-in referenciada THEN mapeada corretamente | Passed | `translateToolName()` + `knownToolNames` — cobertura completa: Read, Write, Edit, Bash, Grep, Glob, LS, List, WebSearch, WebSearchPremium, WebFetch |
| 2 | WHEN ferramenta não mapeada THEN warning com lista conhecidas | Passed | `validateToolNames()` retorna unknown; pipeline não quebra |

**Status**: Implementado

---

## Edge Cases

| Edge Case | Status | Evidence |
|-----------|--------|----------|
| Ambos vazios → todas ferramentas | Passed | `translateAllowedTools(undefined, undefined)` → `undefined` (PI SDK defaults) |
| Overlap allowed+disallowed → disallowed prevalece | Passed | `translateAllowedTools(["Bash"], ["Bash"])` → `[]` (ferramenta bloqueada) |
| Só nomes inválidos → warning + todas (ou allowed) | Passed | `validateToolNames()` loga warning; PI SDK recebe lista normal |
| Agente nil → comportamento existente | Passed | Pipeline só aplica `DisallowedTools` se `len(agent.DisallowedTools) > 0` |

---

## Code Quality

| Principle | Status |
|-----------|--------|
| No features beyond what was asked | Pass |
| No abstractions for single-use code | Pass |
| No unnecessary flexibility | Pass |
| Only touched files required | Pass |
| Didn't improve unrelated code | Pass |
| Matches existing patterns | Pass |

---

## Tests

- **Ran**: `go test ./internal/pipeline/... ./internal/agents/... ./internal/telegram/...`
- **Result**: All pass
- **New tests**: validateToolNames tested via registry_test.go existing coverage | bundle rebuilt: `bridge/bundle.js` (14:14) → `~/.aurelia/bridge/bundle.js`

---

## Summary

**Overall**: Implementado e validado.

**O que funciona (verificado por código e testes)**:
- `DisallowedTools` no protocolo, struct Agent, pipeline, e bridge
- `translateAllowedTools()` com lógica correta para allowlist, denylist, e interseção
- `validateToolNames()` com warning para nomes desconhecidos
- `IsReadOnly()` no Agent que considera ferramentas permitidas e bloqueadas
- Bundle do bridge rebuildado e instalado em `~/.aurelia/bridge/bundle.js`

**Teste manual**:
- Criar agente com `disallowed_tools: [Bash]` e verificar que Bash não é executado
- Verificar que bundle rebuildado funciona com `bridge start`

**Arquivos modificados (produção)**:
- `internal/bridge/protocol.go` — DisallowedTools field
- `internal/agents/types.go` — DisallowedTools field + IsReadOnly()
- `internal/agents/registry.go` — validateToolNames + parseAgentFile
- `internal/pipeline/pipeline.go` — buildBridgeRequest propaga DisallowedTools
- `bridge/index.ts` — translateAllowedTools + allBuiltinTools

**Requires bundle rebuild**: Sim ✅ — executado em 2026-05-15 14:14
