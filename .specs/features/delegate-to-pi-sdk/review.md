# Revisão Completa das Specs vs. Delegate to PI SDK

**Data:** 2026-05-20  
**Baseado em:** Análise das specs existentes + documentação do PI SDK (`@earendil-works/pi-coding-agent`)  
**Escopo:** Avaliar coerência entre `delegate-to-pi-sdk` e todas as specs planejadas no roadmap

---

## Resumo Executivo

A spec `delegate-to-pi-sdk` foi validada contra todas as specs existentes. **Nenhuma spec precisa ser reescrita ou descartada.** Todas permanecem válidas, com ajustes menores de coerência. Identificamos **5 oportunidades adicionais de delegação** não cobertas na spec original e **3 ajustes de detalhe** em specs existentes.

### Matriz de Impacto

| Spec | Impacto da Delegate | Ação Necessária |
|------|---------------------|-----------------|
| `delegate-to-pi-sdk` | — | Adicionar alternativas menos disruptivas (see below) |
| `multi-user-profiles` | Baixo | Nota sobre session store simplificado |
| `agent-orchestration-execution` | Nenhum | — |
| `plan-mode-architecture` | Nenhum | — |
| `project-memory` | Nenhum | — |
| `wiki-memory` | Baixo | Nota sobre redaction (security package) |
| `learning-nudge` | Baixo | Nota sobre profiles.go reduzido |
| `agent-comms` | Nenhum | — |
| `auto-skills` | Baixo | Confirmar independência do registry per-user |

---

## Análise Detalhada por Spec

### 1. `delegate-to-pi-sdk` (nova)

**Status:** Válida. Nenhuma inconsistência encontrada com outras specs.

**Melhorias sugeridas:**

#### 1.1 Alternativa menos disruptiva: `agentsFilesOverride`

A spec propõe mover agentes de `~/.aurelia/agents/` para `~/.pi/agent/agents/`. Isso é **user-facing** e requer migração.

**Alternativa PI-native:**
```typescript
const resourceLoader = new DefaultResourceLoader({
  agentsFilesOverride: () => ({
    agentsFiles: globSync("~/.aurelia/agents/*.md"),
  }),
});
```

**Vantagem:** Mantém agentes em `~/.aurelia/agents/` (menos disruptivo para usuários), mas delega parsing/discovery ao PI SDK.

**Recomendação:** Adicionar "Opção B" na Phase 6 da spec: usar `agentsFilesOverride` em vez de mover diretório. Implementar Opção B primeiro; Opção A (migração) é P3 futuro.

#### 1.2 Security como PI Extension

A spec propõe manter security hooks no Bridge (`session.on("tool_call")`). O PI SDK suporta **extensions** que podem registrar hooks globalmente:

```typescript
// ~/.pi/agent/extensions/aurelia-security.ts
export default function (pi: ExtensionAPI) {
  pi.on("tool_call", async (event, ctx) => {
    // Security policy nativa do PI
  });
}
```

**Vantagem:** Security policy vira código PI-native, não Bridge-specific.

**Recomendação:** Adicionar "Phase 1.5: Investigar PI Extension para security" como task opcional. Se viável, mover policy do Bridge para extension.

#### 1.3 `transformContext` para pruning avançado

O PI SDK `Agent` tem `transformContext` para pruning customizado de mensagens:

```typescript
const agent = new Agent({
  transformContext: async (messages, signal) => {
    // Prune old messages based on custom logic
    return messages.slice(-50);
  },
});
```

**Vantagem:** Substitui `SettingsManager.compaction` simples por pruning com lógica customizada (ex: preservar mensagens com tool calls, remover mensagens de status).

**Recomendação:** Adicionar nota na Phase 4: investigar `transformContext` como alternativa/companheiro à compaction.

---

### 2. `multi-user-profiles` (User Isolation)

**Status:** ✅ Válida. Nenhum conflito com delegate-to-pi-sdk.

**Detalhe de coerência:**

A spec propõe `SessionKey{chat_id, thread_id, user_id}` para sessão LLM. A delegate-to-pi-sdk propõe simplificar `internal/session/store.go` para não rastrear `sessionID` em memória (PI SDK já persiste).

**Coerência:** ✅ Compatível.
- `SessionKey` continua necessário para mapear `(chat, thread, user) → sessionFile` (path do arquivo de sessão do PI)
- O PI `SessionManager` persiste a sessão em disco, mas o Aurélia ainda precisa saber **qual** sessão reabrir para cada usuário
- Simplificação: em vez de rastrear `sessionID` (string opaca), rastreamos `sessionFile` (path determinístico)

**Ajuste sugerido:**
Adicionar nota na spec de User Isolation:
> "Com a delegate-to-pi-sdk, session store não rastrega sessionID em memória. SessionKey mapeia para sessionFile (path PI nativo), não para sessionID. Isso simplifica o store sem perder isolamento por usuário."

---

### 3. `agent-orchestration-execution`

**Status:** ✅ Válida. Zero impacto da delegate-to-pi-sdk.

**Nota:** Orchestrator é específico do Aurélia (worktrees, waves, git ops, PRs). O PI SDK não tem equivalente.

---

### 4. `plan-mode-architecture`

**Status:** ✅ Válida. Zero impacto.

**Nota:** Plan Mode é conceito do Aurélia (modo explícito de planejamento persistente). PI SDK é motor de execução, não de planejamento estruturado.

---

### 5. `project-memory`

**Status:** ✅ Válida. Zero impacto.

**Nota:** Camadas de memória (user, user×project, team, topic) são diferencial do Aurélia. PI SDK não tem memória cross-session.

---

### 6. `wiki-memory`

**Status:** ✅ Válida. Impacto baixo.

**Detalhe:** A wiki-memory menciona `internal/security/` para redaction e capability checks. A delegate-to-pi-sdk propõe remover `internal/security/policy.go`.

**Coerência:** ✅ Compatível.
- `internal/security/audit.go` é mantido (audit logging)
- `internal/security/profiles.go` é reduzido a constantes (type + consts)
- Redaction pode ser movida para pacote dedicado (ex: `internal/redact/`) ou mantida em audit.go

**Ajuste sugerido:**
Na spec wiki-memory, trocar referência de `internal/security/` para "redaction engine (localizada em `internal/security/audit.go` ou pacote dedicado)".

---

### 7. `learning-nudge`

**Status:** ✅ Válida. Impacto baixo.

**Detalhe:** A spec usa `CapabilityProfile=edit_project` sem `Bash`. Os profiles são definidos em `internal/security/profiles.go`.

**Coerência:** ✅ Compatível.
- A delegate-to-pi-sdk reduz `profiles.go` a constantes (type + consts), o que é suficiente
- `CapabilityProfile` continua sendo usado pelo pipeline, nudge, e security context
- Nenhuma lógica de resolução de profile é necessária em Go (delegada ao Bridge)

**Ajuste sugerido:** Nenhum. A spec já é compatível com profiles como constantes.

---

### 8. `agent-comms`

**Status:** ✅ Válida. Zero impacto.

**Nota:** Agent Bus é infraestrutura do Aurélia (comunicação entre workers). PI SDK não tem equivalente.

**Oportunidade futura (P3):** Expor Agent Comms como `customTool` no PI SDK para que workers PI possam usar nativamente. Isso é fora do escopo do MVP.

---

### 9. `auto-skills`

**Status:** ✅ Válida. Impacto baixo.

**Detalhe:** A spec propõe skills em `~/.aurelia/users/<id>/skills/<slug>/SKILL.md` e registry per-user. A delegate-to-pi-sdk propõe eliminar `internal/agents/registry.go`.

**Coerência:** ✅ Compatível.
- Auto-skills NÃO usa `internal/agents/registry.go`. Tem seu próprio registry per-user.
- Auto-skills são skills procedurais (como fazer X), não agent definitions.
- O PI SDK pode carregar skills de `~/.pi/agent/skills/`, mas a spec auto-skills explicitamente diz NÃO usar isso no MVP.
- Layout `~/.aurelia/users/<id>/skills/` é independente do diretório de agentes.

**Ajuste sugerido:** Nenhum. A spec já trata registry per-user como entidade separada.

---

## Oportunidades Adicionais de Delegação ao PI

### Oportunidade 1: `transformContext` para Pruning Customizado

**PI SDK API:** `Agent({ transformContext: async (messages) => ... })`

**Onde usar:** Substituir ou complementar `SettingsManager.compaction` com pruning inteligente:
- Preservar mensagens com tool calls
- Remover mensagens de progress/status
- Preservar N últimas mensagens + mensagens com resultado

**Impacto:** Baixo. Melhoria incremental.
**Prioridade:** P2 (pós-MVP).

---

### Oportunidade 2: PI Extensions para Security Policy

**PI SDK API:** `pi.on("tool_call", ...)` dentro de uma extension

**Onde usar:** Mover security policy do Bridge para uma extension PI nativa:
```typescript
// ~/.pi/agent/extensions/aurelia-security.ts
export default function (pi: ExtensionAPI) {
  pi.on("tool_call", async (event, ctx) => {
    // Policy evaluation
  });
}
```

**Impacto:** Médio. Simplifica Bridge, reutiliza mecanismo PI.
**Prioridade:** P1 (pode ser feito junto com delegate-to-pi-sdk Phase 1).
**Risco:** Extensions requerem versão do PI SDK que suporte carregamento de extensions programático (não apenas via CLI).

---

### Oportunidade 3: `createAgentSessionRuntime` para Session Lifecycle

**PI SDK API:** `createAgentSessionRuntime({ newSession, switchSession, fork })`

**Onde usar:** Substituir gerenciamento manual de sessões no Bridge:
- `newSession()` → `/new`
- `switchSession(path)` → resume
- `fork(entryId)` → branch

**Impacto:** Alto. Simplifica Bridge significativamente.
**Prioridade:** P2 (requer validação de API).
**Nota:** O Aurélia já usa `createAgentSession`, não `Agent` diretamente. Verificar se `createAgentSession` expõe runtime.

---

### Oportunidade 4: `customTools` para Agent Comms

**PI SDK API:** `pi.registerTool({ name, execute })`

**Onde usar:** Expor Agent Bus como tool PI nativa:
```typescript
pi.registerTool({
  name: "agent_comms",
  parameters: Type.Object({ to: Type.String(), message: Type.String() }),
  execute: async (id, params) => {
    // Envia mensagem via Agent Bus
  },
});
```

**Impacto:** Médio. Workers PI usariam comunicação nativamente.
**Prioridade:** P3 (futuro, fora do MVP).

---

### Oportunidade 5: `systemPromptOverride` como Fonte Única

**PI SDK API:** `DefaultResourceLoader({ systemPromptOverride: () => "..." })`

**Onde usar:** Unificar persona + contexto do Aurélia no system prompt do PI:
- Hoje: Aurélia monta prompt em Go → envia para Bridge → PI SDK recebe como `systemPromptOverride`
- Futuro: Bridge monta prompt usando `systemPromptOverride` + context files nativos

**Impacto:** Baixo. Refatoração interna.
**Prioridade:** P2 (pode ser feito junto com prompt builder refactor).

---

## Ajustes nas Specs

### Ajuste 1: `delegate-to-pi-sdk/spec.md` — Adicionar Opção B para Agent Registry

**Seção:** Phase 6 — Agent Registry Migration  
**Adicionar:**

```markdown
### Alternativa: `agentsFilesOverride` (Opção B, recomendada)

Em vez de mover arquivos para `~/.pi/agent/agents/`, manter em `~/.aurelia/agents/` e usar:

```typescript
const resourceLoader = new DefaultResourceLoader({
  agentsFilesOverride: () => ({
    agentsFiles: globSync(join(agentDir, "agents", "*.md")),
  }),
});
```

**Vantagens:**
- Zero migração de dados para usuários
- Agentes continuam em local familiar
- PI SDK faz parsing/discovery nativo

**Desvantagens:**
- Path não é "PI-native"
- Agentes não aparecem em `pi /agents` quando rodado diretamente

**Recomendação:** Implementar Opção B no MVP. Opção A (migração para `~/.pi/agent/`) é P3 futuro.
```

---

### Ajuste 2: `delegate-to-pi-sdk/design.md` — Adicionar Phase 1.5

**Adicionar fase:**

```markdown
### Phase 1.5: PI Extension para Security (Opcional, P2)

**Objetivo:** Investigar se security policy pode ser implementada como PI Extension.

**Passos:**
1. Verificar se PI SDK suporta carregamento programático de extensions via `extensionFactories`
2. Se suportado: criar `~/.pi/agent/extensions/aurelia-security.ts`
3. Mover `evaluateToolPolicy()` do Bridge para extension
4. Bridge simplificado: apenas carrega extension via `extensionFactories`

**Rollback:** Se PI não suportar, manter no Bridge (Phase 1).
```

---

### Ajuste 3: `multi-user-profiles/spec.md` — Nota sobre Session Store

**Adicionar na seção de Goals:**

```markdown
> **Nota sobre delegate-to-pi-sdk:** Com a simplificação do session store, `SessionKey` mapeia para `sessionFile` (path da sessão PI no disco) em vez de `sessionID` (string opaca em memória). O isolamento por `user_id` continua válido — cada usuário tem seu próprio `sessionFile`.
```

---

### Ajuste 4: `wiki-memory/spec.md` — Referência a Security Package

**Atualizar seção "Affected Packages":**

```markdown
| `internal/security/` | Redaction and capability checks for Wiki writes (reduced to constants + audit after delegate-to-pi-sdk) |
```

---

## Recomendações para o Roadmap

### Ordem de Execução Recomendada

```text
Foundation (Security Guard-Rails ✅, Project Binding ✅)
    │
    ├──→ Sprint 0: Delegate to PI SDK Native (pode rodar em PARALELO com User Isolation)
    │    ├─ Phase 0: API Validation (1 dia)
    │    ├─ Phase 1: Bridge Cleanup (2 dias)
    │    ├─ Phase 2: Go Security Cleanup (2 dias)
    │    ├─ Phase 3: Session Store (2 dias)
    │    ├─ Phase 4: Token Tracker (1 dia)
    │    ├─ Phase 5: Prompt Builder (3 dias)
    │    └─ Phase 6: Validation (2 dias)
    │
    ▼
Sprint A: User Isolation MVP
    │
    ▼
Sprint B: Close Orchestration Cycle
    │
    ▼
Sprint C: Plan Mode Architecture
    │
    ▼
Sprint D: User-Scoped Project Memory
    │
    ▼
Sprint E: Wiki Memory Gateway
    │
    ▼
Sprint F: Learning Nudge
    │
    ▼
Sprint G: Agent Comms
    │
    ▼
Sprint H: Auto-Skills
```

### Por que Sprint 0 pode paralelizar com Sprint A:

- **Delegate** toca principalmente em Bridge e código infra (security, session, prompt builder)
- **User Isolation** toca em pipeline, cron, users store, onboarding
- **Interseção mínima:** ambos tocam `internal/session/store.go`, mas em aspectos diferentes (delegate simplifica; User Isolation adiciona user_id)
- **Resolução:** Sprint 0 merge primeiro (simplifica store), Sprint A merge depois (adiciona user_id ao store simplificado)

---

## Conclusão

Todas as specs permanecem **coerentes e válidas**. A spec `delegate-to-pi-sdk` é compatível com todo o roadmap sem requerer reescrita de nenhuma outra spec.

**Ações recomendadas:**
1. Aprovar `delegate-to-pi-sdk` como está, com ajustes menores listados acima
2. Adicionar Opção B (`agentsFilesOverride`) à spec delegate
3. Adicionar Phase 1.5 (PI Extension) como investigação futura
4. Adicionar notas de coerência às specs `multi-user-profiles` e `wiki-memory`
5. Executar Sprint 0 em paralelo com Sprint A, mergeando Sprint 0 primeiro

**Nenhuma spec precisa ser descartada ou reescrita.**
