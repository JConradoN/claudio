# Agent Tools Fix — Design

**Spec**: `.specs/features/agent-tools-fix/spec.md`
**Status**: Draft

---

## Architecture Overview

A correção é simples e concentrada no bridge TypeScript. Não requer mudanças no Go (exceto validação de nomes).

```
Agent file (coder.md)
    ├─ allowed_tools: [Read, Write]
    └─ disallowed_tools: [Bash, WebSearch]
              │
              ▼
    Go: agents.Load() → Agent struct
              │
              ▼
    Bridge: translateAllowedTools(opts?.allowed_tools)
            + NOVO: applyDisallowedTools(result, opts?.disallowed_tools)
              │
              ▼
    PI SDK: tools: ["read", "write"]  (Bash e WebSearch removidos)
```

---

## Code Reuse Analysis

### Existing Components to Leverage

| Component | Location | How to Use |
|-----------|----------|------------|
| `translateAllowedTools` | `bridge/index.ts:137` | Retorna lista de strings permitidas |
| `Agent.AllowedTools` | `internal/agents/types.go:11` | Já usado no protocolo |
| `Agent.DisallowedTools` | `internal/agents/types.go:12` | Campo morto a ativar |
| `RequestOptions` | `internal/bridge/protocol.go:25` | Adicionar campo `disallowed_tools` |

---

## Components

### 1. Protocol Extension (`protocol.go`)

Adicionar `disallowed_tools` ao `RequestOptions`:

```go
// internal/bridge/protocol.go
type RequestOptions struct {
    // ... existing fields ...
    AllowedTools    []string `json:"allowed_tools,omitempty"`
    DisallowedTools []string `json:"disallowed_tools,omitempty"`  // ← NOVO
    // ...
}
```

### 2. Bridge Logic (`bridge/index.ts`)

Modificar `translateAllowedTools` para aceitar denylist:

```typescript
// bridge/index.ts

function translateAllowedTools(
  allowed: string[] | undefined,
  disallowed: string[] | undefined
): string[] | undefined {
  // Start with allowed list (or all built-ins if none specified)
  let result: string[];
  if (allowed && allowed.length > 0) {
    result = [...new Set(allowed.map(translateToolName))];
  } else {
    // undefined = PI SDK uses all built-ins
    result = undefined as any;
  }

  // Apply denylist
  if (disallowed && disallowed.length > 0) {
    const denied = new Set(disallowed.map(translateToolName));
    if (result) {
      result = result.filter(t => !denied.has(t));
    } else {
      // No allowlist: denylist means "all except these"
      // We can't express this directly to PI SDK with string allowlist
      // So we need to build the full list of built-ins minus denied
      const allBuiltins = ["read", "write", "edit", "bash", "grep", "find", "ls", "web_search", "web_search_premium"];
      result = allBuiltins.filter(t => !denied.has(t));
    }
  }

  return result && result.length > 0 ? result : undefined;
}
```

**Caveat**: Quando não há `allowed_tools` e há `disallowed_tools`, precisamos construir a lista completa de built-ins. Isso é frágil — se o PI SDK adicionar novas ferramentas, nossa lista fica desatualizada.

**Alternativa melhor**: O PI SDK não suporta denylist nativamente (apenas allowlist de strings). A solução mais robusta é:
- Se `allowed_tools` existe: usa ele, depois remove `disallowed_tools`
- Se só `disallowed_tools` existe: loga warning dizendo que denylist-only requer explicitar todas as ferramentas permitidas

Mas para MVP, vamos implementar com a lista de built-ins conhecidos.

### 3. RequestOptions Update (`bridge/index.ts`)

```typescript
// bridge/index.ts — modificar createPiSession call
tools: translateAllowedTools(opts?.allowed_tools, opts?.disallowed_tools),
```

### 4. Validation de Nomes (`internal/agents/registry.go` ou novo pacote)

Adicionar validação ao carregar agentes:

```go
func validateToolNames(tools []string) []string {
    known := map[string]bool{
        "Read": true, "Write": true, "Edit": true, "Bash": true,
        "Grep": true, "Glob": true, "LS": true, "List": true,
        "WebSearch": true, "WebSearchPremium": true, "WebFetch": true,
    }
    var unknown []string
    for _, t := range tools {
        if !known[t] {
            unknown = append(unknown, t)
        }
    }
    return unknown
}
```

Chamar no `Load()` ou no `parseAgentFile()` para logar warnings.

---

## Scope Summary

**Arquivos a modificar:**
1. `internal/bridge/protocol.go` — adicionar `disallowed_tools` ao `RequestOptions`
2. `bridge/index.ts` — modificar `translateAllowedTools` para aceitar denylist; atualizar chamada
3. `bridge/index.ts` — adicionar lista completa de built-ins para denylist-only
4. `internal/agents/registry.go` — validar nomes de ferramentas no load

**Arquivos de teste:**
1. `internal/agents/registry_test.go` — testar parsing de `disallowed_tools`
2. `internal/bridge/` — testes de integração (se existirem)

**Estimativa de mudança:** ~30 linhas de produção, ~20 linhas de teste

**Requires bundle rebuild**: Sim (`cd bridge && npm run build`)
