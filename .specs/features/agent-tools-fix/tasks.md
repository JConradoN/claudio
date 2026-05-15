# Agent Tools Fix — Tasks

**Design**: `.specs/features/agent-tools-fix/design.md`
**Status**: Draft

---

## Execution Plan

### Phase 1: Protocol + Bridge (Sequential)

```
T1 → T2
```

### Phase 2: Validation + Tests (Sequential)

```
T2 → T3
```

---

## Task Breakdown

### T1: Add `disallowed_tools` to Protocol

**What**: Adicionar campo `disallowed_tools` ao `RequestOptions` em `protocol.go`.
**Where**: `internal/bridge/protocol.go`
**Depends on**: None
**Reuses**: Estrutura existente

**Done when**:
- [ ] Campo `DisallowedTools []string` adicionado ao `RequestOptions`
- [ ] Tag JSON correta: `json:"disallowed_tools,omitempty"`
- [ ] Build compila: `go build ./...`

**Verify:**
```bash
go build ./...
```

---

### T2: Implement Disallowed Tools no Bridge

**What**: Modificar `translateAllowedTools` para aceitar denylist e aplicar filtragem.
**Where**: `bridge/index.ts`
**Depends on**: T1
**Reuses**: `translateAllowedTools` existente

**Implementation details**:
- Mudar assinatura: `translateAllowedTools(allowed, disallowed)`
- Se `allowed` existe: usa ele, depois remove `disallowed`
- Se só `disallowed` existe: constrói lista completa de built-ins, remove `disallowed`
- Atualizar chamada em `createPiSession`
- Rebuild do bundle

**Lista de built-ins do PI SDK (para denylist-only):**
```typescript
const allBuiltins = [
  "read", "write", "edit", "bash", "grep", "find", "ls",
  "web_search", "web_search_premium"
];
```

**Done when**:
- [ ] `translateAllowedTools` aceita denylist
- [ ] Denylist-only funciona (constrói lista completa)
- [ ] Allowlist + denylist funciona (interseção)
- [ ] Bundle rebuildado e copiado
- [ ] Build compila: `go build ./cmd/aurelia/`

**Verify:**
```bash
cd bridge && npm run build
cp bundle.js ../internal/bridge/bundle.js
go build ./cmd/aurelia/
```

---

### T3: Validar Nomes de Ferramentas

**What**: Adicionar validação de nomes de ferramentas ao carregar agentes.
**Where**: `internal/agents/registry.go` (no `parseAgentFile`)
**Depends on**: None (pode rodar em paralelo com T1)
**Reuses**: Parse existente

**Implementation details**:
- Função `validateToolNames(tools []string) []string`
- Mapa de nomes conhecidos
- No `parseAgentFile`, validar `AllowedTools` e `DisallowedTools`
- Logar warning para nomes desconhecidos

**Done when**:
- [ ] Validação implementada no parse
- [ ] Teste: ferramenta conhecida → sem warning
- [ ] Teste: ferramenta desconhecida → warning
- [ ] Tests pass: `go test ./internal/agents/...`

**Verify:**
```bash
go test ./internal/agents/ -run TestToolNames -v
```

---

## Parallel Execution Map

```
Phase 1 (Sequential):
  T1 ── Protocol extension
  T2 ── Bridge logic (depois T1)

Phase 2 (Parallel a T2):
  T3 ── Validation (independente)
```

**Ordem real de execução:**
```
T1 ──→ T2 ──→ (done)
T3 ──→ (done, paralelo)
```

---

## Task Granularity Check

| Task | Scope | Status |
|------|-------|--------|
| T1: Protocol | 1 campo em struct | Granular |
| T2: Bridge logic | 1 função + rebuild | Granular |
| T3: Validation | 1 função + testes | Granular |
