# Delegate to PI SDK Native — Design

**Baseado na spec:** `.specs/features/delegate-to-pi-sdk/spec.md`  
**Roadmap step:** Sprint entre Foundation (done) e Sprint A (User Isolation)  
**Status:** 🟡 Core implementado em v0.13.0; docs/E2E/agent-registry boundary pendentes  
**Depends on:** `security-guard-rails` (v0.8.0) estar estável  

---

## Visão Geral

```text
┌─────────────────────────────────────────────────────────────────────────────┐
│                         AURELIA ARCHITECTURE (ATUAL)                        │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                             │
│   Telegram → Pipeline → Prompt Builder (861 linhas) → Bridge → PI SDK      │
│                             │                                               │
│                    ┌────────┴────────┐                                      │
│                    │                 │                                      │
│              Go Security      Session Store                                 │
│              (~514 linhas)    (~265 linhas)                                 │
│                    │                 │                                      │
│              Agent Registry   Token Tracker                                 │
│              (~211 linhas)    (~131 linhas)                                 │
│                                                                             │
└─────────────────────────────────────────────────────────────────────────────┘

┌─────────────────────────────────────────────────────────────────────────────┐
│                      AURELIA ARCHITECTURE (DEPOIS)                          │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                             │
│   Telegram → Pipeline → Prompt Builder (~500 linhas) → Bridge → PI SDK     │
│                             │                                               │
│                    ┌────────┘                                               │
│                    │                                                        │
│              Session Store (~80 linhas)                                     │
│              (apenas cwd + sessionFile)                                     │
│                                                                             │
│   PI SDK nativo:                                                            │
│   • ModelRegistry.find()                                                    │
│   • beforeToolCall hook                                                     │
│   • SessionManager (persistência)                                           │
│   • SettingsManager.compaction                                              │
│   • DefaultResourceLoader (CLAUDE.md, AGENTS.md)                            │
│   • Agent discovery em ~/.pi/agent/agents/                                  │
│                                                                             │
└─────────────────────────────────────────────────────────────────────────────┘
```

---

## Phase 1: Bridge Cleanup

### 1.1 — Eliminar `resolveModelFromRegistry()`

**Arquivo:** `bridge/index.ts`
**Linhas removidas:** ~40

**Antes:**
```typescript
function resolveModelFromRegistry(
  registry: ModelRegistry,
  provider: string | undefined,
  modelID: string | undefined,
) {
  if (!modelID) return undefined;
  const mappedProvider = mapProvider(provider);
  const mappedModel = mapModelForProvider(mappedProvider, modelID);

  if (mappedProvider) {
    const direct = registry.find(mappedProvider, mappedModel);
    if (direct) return direct;
  }

  const allModels = registry.getAll();
  const canonical = allModels.find(
    (model) => `${model.provider}/${model.id}`.toLowerCase() === mappedModel.toLowerCase(),
  );
  if (canonical) return canonical;

  const exactIDMatches = allModels.filter((model) => model.id.toLowerCase() === mappedModel.toLowerCase());
  // ... mais fuzzy matching
}
```

**Depois:**
```typescript
function resolveModel(
  registry: ModelRegistry,
  provider: string | undefined,
  modelID: string | undefined,
) {
  if (!modelID) return undefined;
  const mappedProvider = mapProvider(provider);
  const mappedModel = mapModelForProvider(mappedProvider, modelID);

  // Native PI SDK resolution
  const found = registry.find(mappedProvider, mappedModel);
  if (found) return found;

  // Fallback: exact ID match among configured providers
  return registry.getAll().find((m) => m.id === mappedModel);
}
```

**Notas:**
- `mapProvider()` e `mapModelForProvider()` são mantidos — são aliases Aurélia para nomes de provider
- Eliminar fuzzy matching por nome (ex: `model.name.includes(mappedModel)`) — usar apenas ID exato
- Eliminar matching por `provider/model` composite string — o PI SDK já faz isso

### 1.2 — Simplificar Security Hooks

**Arquivo:** `bridge/index.ts`
**Linhas removidas:** Até ~260 (se `beforeToolCall` disponível)

**Análise da API PI SDK:**

O PI SDK expõe `beforeToolCall` no `Agent` usado pela sessão. O `createAgentSession` não recebe `beforeToolCall` como opção de criação, então o Aurélia instala a policy depois da criação da sessão, envolvendo `session.agent.beforeToolCall` e preservando o hook original instalado pelo runner de extensions.

**Otimização possível:**
- Mover `evaluateToolPolicy()` para uma PI **extension** separada (ex: `~/.pi/agent/extensions/aurelia-security.ts`)
- Isso deixa o Bridge limpo e permite reutilização em outros projetos

**Mudança mínima entregue em v0.13.x:**
- Usar `session.agent.beforeToolCall` no Bridge; `session.on("tool_call")` não existe na API atual.
- Eliminar a duplicação em Go (`internal/security/policy.go`) — a fonte da verdade fica no Bridge.
- Manter `CapabilityProfile`/config em Go apenas como contrato enviado ao Bridge.

### 1.3 — Bundle Impact

```bash
cd bridge && npm run build
# Verificar se bundle.js diminuiu de tamanho (menos código = bundle menor)
ls -la bundle.js
```

---

## Phase 1.5: PI Extension para Security (Opcional, P2)

**Objetivo:** Investigar se security policy pode ser implementada como PI Extension nativa.

**Contexto:**
O PI SDK suporta extensions que podem registrar hooks globalmente:

```typescript
// ~/.pi/agent/extensions/aurelia-security.ts
export default function (pi: ExtensionAPI) {
  pi.on("tool_call", async (event, ctx) => {
    // Security policy evaluation nativa do PI
    if (event.toolName === "bash" && isDestructive(event.args.command)) {
      return { block: true, reason: "destructive command blocked" };
    }
  });
}
```

**Passos:**
1. Verificar se PI SDK suporta `extensionFactories` em `createAgentSession`
2. Se suportado: criar `~/.pi/agent/extensions/aurelia-security.ts`
3. Mover `evaluateToolPolicy()` do Bridge para extension
4. Bridge simplificado: apenas carrega extension via `extensionFactories`

**Vantagens:**
- Código PI-native, reutilizável
- Bridge fica mais limpo
- Policy roda no nível certo (PI SDK)

**Riscos:**
- Extensions podem requerer versão específica do PI SDK
- Carregamento programático pode não ser suportado em `createAgentSession`

**Rollback:** Se PI não suportar, manter no Bridge (Phase 1).

**Prioridade:** P2 — investigar após Phase 1, implementar se viável.

---

## Phase 2: Go Security Cleanup

### 2.1 — Remover `internal/security/policy.go`

**Arquivo removido:** `internal/security/policy.go` (~514 linhas)

**Rationale:**
- A policy em Go é duplicada com a policy no Bridge TS
- A fonte da verdade deve estar no Bridge (onde o PI SDK executa as tools)
- Go não precisa avaliar policy — apenas construir e enviar `SecurityContext` para o Bridge

**Call sites a verificar:**
```bash
grep -r "security\.EvaluateToolCall" --include="*.go" .
grep -r "security\.PolicyDecision" --include="*.go" .
grep -r "security\.DefaultConfig" --include="*.go" .
```

**Se nenhum call site fora do pacote `internal/security/`:**
- Remover todo o pacote exceto `audit.go`

**Se houver call sites:**
- Verificar se são testes (remover testes também)
- Se são usados em produção, refatorar para delegar ao Bridge

### 2.2 — Reduzir `internal/security/profiles.go`

**Opção A:** Remover completamente
**Opção B:** Manter apenas constantes (`CapabilityProfile` type + consts)

**Recomendação: Opção B**

**Rationale:**
- `CapabilityProfile` é usado em `internal/agents/types.go` e no pipeline
- Mas `ProfileTools()`, `ResolveProfile()`, `DefaultProfileForContext()` podem ser delegados ao Bridge
- Manter o type + consts permite que o código compile sem mudanças drásticas

**Novo `internal/security/profiles.go` (~20 linhas):**
```go
package security

type CapabilityProfile string

const (
    ProfileObserve     CapabilityProfile = "observe"
    ProfileReadOnly    CapabilityProfile = "read_only"
    ProfileEditProject CapabilityProfile = "edit_project"
    ProfileExecuteSafe CapabilityProfile = "execute_safe"
    ProfilePrivileged  CapabilityProfile = "privileged"
)
```

### 2.3 — Manter `internal/security/audit.go`

**Rationale:**
- Audit logging é uma responsabilidade do Aurélia (orquestração)
- O PI SDK não tem audit nativo
- Mas o audit deve ser chamado pelo Bridge, não por Go
- Futuro: mover audit para Bridge também, ou manter em Go para centralização

---

## Phase 3: Session Store Simplification

### 3.1 — Reduzir `internal/session/store.go`

**Arquivo:** `internal/session/store.go`
**Linhas removidas:** ~185

**Análise atual:**
```go
type entry struct {
    sessionID string
    active    bool
    lastSeen  time.Time
}
```

**Problema:** O PI SDK `SessionManager` já persiste sessions em disco (`.jsonl` files). O Aurélia não precisa rastrear `sessionID` em memória.

**O que o Aurélia precisa:**
1. **sessionFile path**: Para bridge recovery (`SessionManager.open(path)`)
2. **cwd**: Para contexto do agente
3. **lastSeen**: Para garbage collection

**Novo `entry`:**
```go
type entry struct {
    sessionFile string  // path to PI session file
    active      bool
    lastSeen    time.Time
}
```

**Mudanças nos métodos:**
- `Get()` → retorna `sessionFile` em vez de `sessionID`
- `Set()` → aceita `sessionFile` em vez de `sessionID`
- `GetSession()` / `SetSession()` → mesma mudança
- `Deactivate()` / `DeactivateAll()` → mantido (bridge recovery)
- `GC()` → mantido

### 3.2 — Impacto no Pipeline

**Arquivo:** `internal/pipeline/pipeline.go` (ou quem chama `sessions.Get()`)

**Antes:**
```go
sessionID := bc.sessions.Get(chatID, threadID)
req.Options.Resume = sessionID
```

**Depois:**
```go
sessionFile := bc.sessions.Get(chatID, threadID)
req.Options.Resume = sessionFile  // PI SessionManager aceita path ou ID
```

**Nota:** O PI `SessionManager.open()` aceita tanto path quanto ID parcial. Verificar compatibilidade.

---

## Phase 4: Token Tracker Elimination

### 4.1 — Remover `internal/session/tracker.go`

**Arquivos removidos:**
- `internal/session/tracker.go` (131 linhas)
- `internal/session/tracker_test.go` (? linhas)

**Rationale:**
- `SettingsManager.compaction` do PI SDK já faz pruning automático de contexto
- Auto-reset manual por token threshold é desnecessário
- Reduz complexidade e evita duplicação de responsabilidade

### 4.2 — Habilitar Compaction no Bridge

**Arquivo:** `bridge/index.ts`

**Antes:**
```typescript
const settingsManager = SettingsManager.inMemory({
  compaction: { enabled: false },
  retry: { enabled: true, maxRetries: 2 },
});
```

**Depois:**
```typescript
const settingsManager = SettingsManager.inMemory({
  compaction: { enabled: true },  // auto-prune old messages
  retry: { enabled: true, maxRetries: 2 },
});
```

### 4.3 — Manter `/usage` Command

**Arquivo:** `internal/telegram/commands.go` (ou onde `/usage` é implementado)

**Mudança:**
- Antes: `tracker.Get(key)` retorna usage acumulado
- Depois: Extrair do último evento `result` do Bridge

**Bridge já envia:**
```json
{"event":"result","content":"...","cost_usd":0.12,"input_tokens":1500,"output_tokens":800,"num_turns":5}
```

**Go:** Acumular os valores do `result` event para display, mas NÃO usar para decisão de reset.

---

## Phase 5: Prompt Builder Refactor

### 5.1 — Delegar CLAUDE.md/AGENTS.md ao PI

**Arquivo:** `internal/pipeline/prompt_builder.go`

**Antes:**
```go
func (bc *Service) buildProjectDocsSection(chatID int64, agent *agents.Agent, threadID int) string {
    cwd := bc.effectiveCwd(agent, chatID, threadID)
    if cwd == "" { return "" }
    
    claudeMd, _ := os.ReadFile(filepath.Join(cwd, "CLAUDE.md"))
    agentsMd, _ := os.ReadFile(filepath.Join(cwd, "AGENTS.md"))
    // ... monta manualmente
}
```

**Depois:**
- No Bridge, `DefaultResourceLoader` com `noContextFiles: false` já carrega `CLAUDE.md` e `AGENTS.md`
- Remover `buildProjectDocsSection()` do prompt builder
- Remover `CLAUDE.md` e `AGENTS.md` do system prompt assembly

**Bridge já faz isso:**
```typescript
const resourceLoader = new DefaultResourceLoader({
  cwd,
  noContextFiles: false,  // auto-discover CLAUDE.md and AGENTS.md
  // ...
});
```

### 5.2 — Simplificar System Prompt Assembly

**Antes:** 15 seções montadas manualmente (~861 linhas)

**Depois:** 6 seções Aurélia-específicas (~500 linhas)

| Seção | Fonte | Manter? |
|-------|-------|---------|
| Runtime Identity | Go | ✅ Sim |
| Persona | Go (`internal/persona/`) | ✅ Sim |
| Agent Instructions | Go (agent prompt) | ✅ Sim |
| Orchestrator TLC | Go (quando planning intent) | ✅ Sim |
| Cron Instructions | Go | ✅ Sim |
| Telegram Context | Go | ✅ Sim |
| Security Boundaries | Go | ✅ Sim |
| Conversation Continuity | Go (`internal/continuity/`) | ✅ Sim |
| Last Run State | Go (`internal/runlog/`) | ✅ Sim |
| Memory Instructions | Go | ✅ Sim |
| Memory Contents | Go (memory loading) | ✅ Sim |
| Long Task Guidance | Go | ✅ Sim |
| **Project Docs** | **PI nativo** | ❌ Remover |
| **Context Files** | **PI nativo** | ❌ Remover |

**Nova ordem de assembly:**
```go
func (bc *Service) buildSystemPrompt(...) (string, error) {
    sections := []string{
        bc.buildRuntimeIdentity(),           // 1
        bc.buildPersonaSection(),            // 2
        bc.buildAgentSection(agent),         // 3
        bc.buildOrchestratorSection(text),   // 4 (condicional)
        bc.buildCronSection(chatID),         // 5
        bc.buildTelegramSection(...),        // 6
        bc.buildSecuritySection(...),        // 7
        bc.buildContinuitySection(...),      // 8 (condicional)
        bc.buildLastRunSection(...),         // 9 (condicional)
        bc.buildMemorySection(...),          // 10
        bc.buildLongTaskSection(text),       // 11 (condicional)
    }
    // PI SDK injeta CLAUDE.md + AGENTS.md automaticamente
    return strings.Join(sections, "\n\n"), nil
}
```

### 5.3 — Memory Cache

**Manter:** `internal/pipeline/memory_cache.go`

**Rationale:**
- Memory cache é otimização específica do pipeline do Aurélia
- PI SDK não tem cache de memória
- O TTL de 5s e invalidação por mtime são valiosos

---

## Phase 6: Agent Registry Migration

### 6.1 — PI Native Agent Discovery

**PI SDK behavior:**
- Descobre agentes em `~/.pi/agent/agents/*.md`
- Também descobre `AGENTS.md` no projeto (se `agentScope` habilitado)
- Mesmo formato: markdown com YAML frontmatter

**Diferenças de formato:**

| Campo | Aurélia | PI SDK |
|-------|---------|--------|
| `name` | ✅ Igual | ✅ Igual |
| `description` | ✅ Igual | ✅ Igual |
| `model` | ✅ Igual | ✅ Igual |
| `schedule` | ✅ Custom | ❓ Verificar |
| `cwd` | ✅ Custom | ❓ Verificar |
| `allowed_tools` | ✅ Igual | `tools` (no PI) |
| `capability_profile` | ✅ Custom | ❓ Verificar |

**Action item:** Verificar na Phase 0 se PI SDK aceita `schedule`, `cwd`, e `capability_profile` no frontmatter.

### 6.2 — Script de Migração

**Arquivo:** `scripts/migrate-agents.sh`

```bash
#!/bin/bash
set -euo pipefail

AURELIA_AGENTS="${HOME}/.aurelia/agents"
PI_AGENTS="${HOME}/.pi/agent/agents"

if [ ! -d "$AURELIA_AGENTS" ]; then
    echo "No agents to migrate."
    exit 0
fi

mkdir -p "$PI_AGENTS"

for f in "$AURELIA_AGENTS"/*.md; do
    [ -e "$f" ] || continue
    basename=$(basename "$f")
    cp "$f" "$PI_AGENTS/$basename"
    echo "Migrated: $basename"
done

echo "Migration complete. Agents copied to $PI_AGENTS"
echo "You may remove $AURELIA_AGENTS after validation."
```

### 6.3 — Eliminar `internal/agents/`

**Arquivos removidos:**
- `internal/agents/registry.go`
- `internal/agents/types.go`
- `internal/agents/registry_test.go`

**Call sites a verificar:**
```bash
grep -r "agents\." --include="*.go" . | grep -v "internal/agents/"
```

**Se usado em pipeline:**
- Substituir por chamada ao Bridge: `bridge.ExecuteSync(ctx, { command: "list-agents" })`
- Ou manter um wrapper minimalista que delega ao Bridge

---

## Testes

### Testes a manter

| Teste | Pacote | Motivo |
|-------|--------|--------|
| Session store (simplificado) | `internal/session/` | Ainda precisa de cwd tracking |
| Prompt builder (refatorado) | `internal/pipeline/` | Ainda monta prompt, mas menor |
| Bridge protocol | `internal/bridge/` | Protocolo NDJSON inalterado |
| Security audit | `internal/security/` | Audit logging continua |

### Testes a remover

| Teste | Pacote | Motivo |
|-------|--------|--------|
| Policy evaluation | `internal/security/security_test.go` | Policy movida para Bridge |
| Profile resolution | `internal/security/security_test.go` | Delegado ao PI |
| Token tracker | `internal/session/tracker_test.go` | Eliminado |
| Agent registry | `internal/agents/registry_test.go` | Eliminado |

### Testes Bridge (TypeScript)

| Teste | Como |
|-------|------|
| Model resolution | Unit test: `registry.find()` retorna modelo correto |
| Security hook | Integration: `beforeToolCall` bloqueia comando destrutivo |
| Compaction | Verificar que `SettingsManager.compaction.enabled=true` funciona |

---

## Rollback Strategy

### Phase 1 (Bridge)
- `git checkout bridge/index.ts`
- `make bridge`
- Rebuild + restart

### Phase 2 (Go Security)
- `git checkout internal/security/`
- `git checkout internal/agents/types.go` (se alterado)
- Rebuild Go

### Phase 3 (Session)
- `git checkout internal/session/`
- Rebuild Go

### Phase 4 (Prompt Builder)
- `git checkout internal/pipeline/prompt_builder.go`
- Rebuild Go

### Phase 5 (Agent Registry)
- Copiar agentes de volta: `cp ~/.pi/agent/agents/*.md ~/.aurelia/agents/`
- `git checkout internal/agents/`
- Rebuild Go

---

## Resumo de Arquivos

### Criar (2)

| Arquivo | Conteúdo |
|---------|----------|
| `docs/pi-sdk-api-validation.md` | Documentação da validação das APIs nativas |
| `scripts/migrate-agents.sh` | Script de migração de agentes |

### Modificar (3)

| Arquivo | Mudança |
|---------|---------|
| `bridge/index.ts` | Simplificar model resolution, manter hooks |
| `internal/pipeline/prompt_builder.go` | Remover project docs loading, simplificar assembly |
| `internal/session/store.go` | Simplificar para apenas cwd + sessionFile |

### Reduzir (2)

| Arquivo | Mudança |
|---------|---------|
| `internal/security/profiles.go` | Reduzir a apenas constantes |
| `internal/security/audit.go` | Manter (possívelmente mover para Bridge no futuro) |

### Eliminar (7)

| Arquivo | Linhas (est.) |
|---------|---------------|
| `internal/security/policy.go` | ~514 |
| `internal/security/security_test.go` | ~200 |
| `internal/session/tracker.go` | ~131 |
| `internal/session/tracker_test.go` | ~? |
| `internal/agents/registry.go` | ~211 |
| `internal/agents/types.go` | ~58 |
| `internal/agents/registry_test.go` | ~? |

---

## Dependências entre Fases

```
Phase 0: API Validation
    │
    ▼
Phase 1: Bridge Cleanup
    │
    ▼
Phase 2: Go Security Cleanup
    │
    ▼
Phase 3: Session Store
    │
    ▼
Phase 4: Token Tracker
    │
    ▼
Phase 5: Prompt Builder
    │
    ▼
Phase 6: Agent Registry
    │
    ▼
Phase 7: Validation
```

**Cada phase pode ser merged independentemente**, mas a ordem recomendada é:
1. Bridge primeiro (baixo risco, fácil rollback)
2. Go cleanup depois (médio risco, mais testes)
3. Agent registry por último (alto risco, user-facing)
