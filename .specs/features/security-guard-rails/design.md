# Security Guard-Rails — Design (Fase 1 + 2)

**Baseado na spec:** `.specs/features/security-guard-rails/spec.md`
**Roadmap step:** Foundation (✅ 100% done)
**Status:** Implemented (Fase 1 Warn + Fase 2 Block) — Capability Profiles, PI tool_call hooks, Bash/Filesystem policy, audit logging, perfis mínimos.
**Implementado em:** v0.8.0 — 44 testes unitários

---

## Visão Geral

```
Agent/Context → CapabilityProfile → RequestOptions.tools
                                        ↓
                              ┌─────────────────┐
                              │  PI tool_call    │ ← Security Hook (TS)
                              │  hook no bridge  │
                              └────────┬────────┘
                                       ↓
                              ┌─────────────────┐
                              │  evaluateTool    │ ← Policy Engine (TS)
                              │  Policy()        │
                              └────────┬────────┘
                            allow / block / rewrite
                                       ↓
                              ┌─────────────────┐
                              │  Audit Log      │ ← structured, redacted
                              └─────────────────┘
                                       ↓
                              PI executa tool ou
                              retorna block ao modelo
```

## Pacote Go: `internal/security/`

Novo pacote com 3 arquivos. Sem dependências externas novas.

### `internal/security/profiles.go` — CapabilityProfiles

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

// ProfileTools returns the definitive tool allowlist for a profile.
// This is the SOURCE OF TRUTH for what tools each profile may use.
// Tools not in this list are always blocked regardless of agent overrides.
func ProfileTools(p CapabilityProfile) []string {
    switch p {
    case ProfileObserve:
        return []string{} // no tools, no filesystem
    case ProfileReadOnly:
        return []string{"Read", "Grep", "Glob", "LS"}
    case ProfileEditProject:
        return []string{"Read", "Grep", "Glob", "LS", "Write", "Edit"}
    case ProfileExecuteSafe:
        return []string{"Read", "Write", "Edit", "Bash", "Grep", "Glob", "LS", "WebSearch", "WebSearchPremium", "WebFetch"}
    case ProfilePrivileged:
        return []string{"Read", "Write", "Edit", "Bash", "Grep", "Glob", "LS", "WebSearch", "WebSearchPremium", "WebFetch"}
    default:
        return nil
    }
}

// ResolveProfile resolves the effective capability profile for an execution
// context. Returns the profile and the effective tool list after intersecting
// agent-level allowed_tools with profile limits.
func ResolveProfile(agentProfile CapabilityProfile, agentAllowed, agentDisallowed []string, hasCWD bool) (CapabilityProfile, []string)

// DefaultProfileForContext returns the recommended profile for a given context.
func DefaultProfileForContext(hasCWD bool, isInternal bool, needsWrite bool) CapabilityProfile
```

### `internal/security/policy.go` — Policy Config + Validation

```go
package security

type PolicyMode string
const (
    PolicyWarn  PolicyMode = "warn"   // Fase 1: log + allow, exceto casos extremos
    PolicyBlock PolicyMode = "block"  // Fase 2: block sensível, allow build/test
)

type SecurityConfig struct {
    Mode                  PolicyMode       `json:"mode"`              // "warn" | "block"
    DefaultProfile        CapabilityProfile `json:"default_profile"`  // "execute_safe" p/ cwd
    AllowPrivilegedAgents bool             `json:"allow_privileged"`
    SensitivePathPatterns []string         `json:"sensitive_paths"`   // glob patterns
    AllowedOutsideCWDPaths []string        `json:"allowed_outside_cwd"`
    BashRules             BashPolicy
}

type BashPolicy struct {
    AllowBuild     bool `json:"allow_build"`      // go build, npm run, etc
    AllowGitSafe   bool `json:"allow_git_safe"`   // git status, diff, log
    AllowTest      bool `json:"allow_test"`       // go test, npm test
    Destructive    bool `json:"destructive"`       // rm -rf, sudo, chmod
    AllowEnvAccess bool `json:"allow_env_access"` // env, printenv, $VAR
}

// DefaultConfig returns safe defaults for Fase 2 Block mode.
func DefaultConfig() SecurityConfig

// ToolDecision is the result of evaluating a tool call against the policy.
type ToolDecision string
const (
    DecisionAllow            ToolDecision = "allow"
    DecisionBlock            ToolDecision = "block"
    DecisionRewrite          ToolDecision = "rewrite"
    DecisionApprovalRequired ToolDecision = "approval_required"
)

type PolicyDecision struct {
    Decision ToolDecision
    Reason   string
    // Redacted copy of the input for audit (secrets stripped)
    RedactedInput map[string]any
}

// EvaluateToolCall checks a single tool call against the security policy.
// Called from the bridge tool_call hook.
func EvaluateToolCall(toolName string, input map[string]any, cwd string, profile CapabilityProfile, cfg SecurityConfig) PolicyDecision

// IsSensitivePath checks if a path matches any sensitive pattern.
func IsSensitivePath(path string, patterns []string) bool

// IsDestructiveCommand checks if a bash command uses destructive patterns.
func IsDestructiveCommand(command string) bool

// IsExfiltrationCommand checks if a command pattern suggests data exfiltration.
func IsExfiltrationCommand(command string, args map[string]any) bool
```

### `internal/security/audit.go` — Audit Logging

```go
package security

type AuditEvent struct {
    Timestamp   time.Time
    Decision    ToolDecision
    ToolName    string
    Reason      string
    ChatID      int64
    ThreadID    int
    UserID      int64
    AgentName   string
    RequestID   string
    Profile     CapabilityProfile
    CWD         string
    Redacted    bool
}

// LogAudit writes a structured audit event to stderr in JSON lines format.
// In future: write to SQLite audit store for queryability.
func LogAudit(ev AuditEvent)
```

### Arquivos a criar

| File | Lines (est.) |
|---|---|
| `internal/security/profiles.go` | ~120 |
| `internal/security/policy.go` | ~250 |
| `internal/security/audit.go` | ~80 |
| `internal/security/security_test.go` | ~200 |

---

## Mudanças no Bridge (`bridge/index.ts`)

### 1. SecurityContext no Request

Adicionar ao `interface RequestOptions`:

```ts
interface SecurityContext {
  enabled: boolean;
  profile: "observe" | "read_only" | "edit_project" | "execute_safe" | "privileged";
  mode: "warn" | "block";
  cwd: string;
  sensitive_paths: string[];
  allowed_outside_cwd: string[];
  chat_id?: number;
  thread_id?: number;
  user_id?: number;
  agent_name?: string;
  request_id?: string;
}
```

### 2. PI tool_call Hook

Registrar hook via `pi.on("tool_call", ...)` imediatamente após `createPiSession()` — **antes** do primeiro `session.prompt()`:

```ts
// Dentro de handleQuery, após createPiSession:
if (opts?.security?.enabled) {
  const unsubscribeHook = session.on("tool_call", async (event, ctx) => {
    const decision = evaluateToolPolicy(event, opts.security);
    logAudit(decision, opts.security);

    if (decision.decision === "block") {
      return { block: true, reason: decision.reason };
    }
    if (decision.decision === "rewrite") {
      // Modify args before execution
      Object.assign(event.args, decision.input);
    }
    // "allow" → default: let through
  });
  // Store unsubscribe for cleanup
}
```

### 3. evaluateToolPolicy() Function

Nova função pura no `bridge/index.ts`:

```ts
interface ToolPolicyInput {
  toolName: string;
  input: Record<string, unknown>;
  security: SecurityContext;
}

function evaluateToolPolicy(input: ToolPolicyInput): {
  decision: "allow" | "block" | "rewrite";
  reason?: string;
  input?: Record<string, unknown>;
}
```

Lógica:

| Tool | Condição de Block | Condição de Warn |
|---|---|---|
| `bash` | Comando acessa env/secrets (`env`,`printenv`,`echo $TOKEN`,`cat ~/.aurelia/config/`) | Qualquer comando ambíguo |
| `bash` | Comando destrutivo (`rm -rf /`, `sudo`, `chmod -R`, `chown -R`, `dd`, fork bomb) | — |
| `bash` | Exfiltração (`curl/wget/nc/scp/rsync` + arquivo local) | — |
| `bash` | Git destrutivo (`git push --force`, remote add com token) | — |
| `read` | Path sensível (`.env`, `.env.*`, keys, `~/.ssh`, `~/.pi`, `~/.aurelia/config`) | `go build ./...` (sempre allow) |
| `read` | Path resolve fora do `cwd` (fora allowlist) | — |
| `write/edit` | Path resolve fora do `cwd` | — |
| `write/edit` | Path sensível (`.env`, private key paths) | — |
| `grep/glob/ls` | Sem restrições em `execute_safe` (dados de diretório são baixo risco) | — |

### 4. Audit no Bridge

```ts
interface AuditEntry {
  timestamp: string;
  decision: "allow" | "block" | "rewrite";
  tool_name: string;
  reason: string;
  chat_id?: number;
  thread_id?: number;
  agent_name?: string;
  profile: string;
  cwd: string;
  redacted: boolean;
}

function logAudit(decision: AuditEntry, security: SecurityContext): void {
  // Write to stderr as JSON line: [security] { ... }
  // Secrets already redacted by evaluateToolPolicy
  const entry = { ...decision, timestamp: new Date().toISOString() };
  process.stderr.write(`[security] ${JSON.stringify(entry)}\n`);
}
```

### 5. Fail-Closed

Se `opts.security.enabled` for `true` mas o hook não puder ser registrado (erro na API PI), a sessão **não deve iniciar**. A criação da sessão retorna erro.

```ts
if (opts?.security?.enabled && !session.on) {
  throw new Error("security hook not available: PI SDK version too old");
}
```

### Arquivo alterado

| File | Mudança |
|---|---|
| `bridge/index.ts` | +SecurityContext interface, +evaluateToolPolicy(), +logAudit(), +hook registration |
| `internal/bridge/protocol.go` | +SecurityContext struct serializável |

---

## Mudanças no Protocolo Go

### `internal/bridge/protocol.go`

Adicionar ao `RequestOptions`:

```go
type SecurityContext struct {
    Enabled             bool               `json:"enabled"`
    Profile             string             `json:"profile"`   // observe | read_only | edit_project | execute_safe
    Mode                string             `json:"mode"`      // warn | block
    Cwd                 string             `json:"cwd"`
    SensitivePaths      []string           `json:"sensitive_paths,omitempty"`
    AllowedOutsideCWD   []string           `json:"allowed_outside_cwd,omitempty"`
    ChatID              int64              `json:"chat_id,omitempty"`
    ThreadID            int                `json:"thread_id,omitempty"`
    UserID              int64              `json:"user_id,omitempty"`
    AgentName           string             `json:"agent_name,omitempty"`
    RequestID           string             `json:"request_id,omitempty"`
}
```

Adicionar campo em `RequestOptions`:
```go
Security *SecurityContext `json:"security,omitempty"`
```

---

## Integração no Pipeline

### `internal/agents/types.go`

Adicionar campo ao `Agent`:
```go
type Agent struct {
    // ... existing fields ...
    CapabilityProfile string `yaml:"capability_profile,omitempty"` // observe | read_only | edit_project | execute_safe | privileged
}
```

Atualizar `IsReadOnly()` para considerar `CapabilityProfile`:
- Se `CapabilityProfile == "observe"` ou `"read_only"` → true
- Se `CapabilityProfile == "edit_project"` → false (tem Write/Edit, sem Bash)
- Se `CapabilityProfile == "execute_safe"` → false

### `internal/pipeline/pipeline.go`

Em `buildBridgeRequest()`, após montar o request:

```go
// Resolver security context
profile := security.DefaultProfileForContext(
    cwd != "",
    agent == nil || agent.NoUserSettings,
    needsWriteTools(agent),
)

// Allow agent-level profile override
if agent != nil && agent.CapabilityProfile != "" {
    profile = security.CapabilityProfile(agent.CapabilityProfile)
}

// Intersect agent allowed_tools with profile limits
effectiveProfile, effectiveTools := security.ResolveProfile(
    profile,
    req.Options.AllowedTools,
    req.Options.DisallowedTools,
    cwd != "",
)

// Replace allowed_tools with profile-limited set
req.Options.AllowedTools = effectiveTools

// Attach security context
secCfg := s.getSecurityConfig() // from AppConfig or defaults
req.Options.Security = &bridge.SecurityContext{
    Enabled:    true,
    Profile:    string(effectiveProfile),
    Mode:       string(secCfg.Mode),
    Cwd:        cwd,
    ChatID:     chatID,
    ThreadID:   threadID,
    UserID:     0, // from input
    AgentName:  agentName,
    RequestID:  req.RequestID,
}
```

### `internal/pipeline/prompt_builder.go`

Adicionar seção `Security Boundaries` ao system prompt quando security está ativo:

```go
func (bc *Service) buildSecurityPromptSection(profile security.CapabilityProfile) string {
    if profile == security.ProfileObserve {
        return ""
    }
    return `## Security Boundaries

You operate under a security policy. Rules that are enforced:

1. Stay within the current working directory — do not read/write outside it.
2. Do NOT attempt to read sensitive files (.env, secrets, keys, config).
3. Do NOT run destructive commands (rm -rf, sudo, chmod -R).
4. Do NOT exfiltrate data via curl/wget/nc.
5. For ambiguous or risky operations, ask the user for confirmation.

Your current profile: ` + string(profile)

    // These rules are enforced by the PI tool_call hook — violations are blocked
    // before the tool executes.
}
```

Inserir no `buildSystemPrompt()` entre telegram e continuity sections.

### `internal/pipeline/service.go`

Adicionar método:
```go
func (s *Service) getSecurityConfig() security.SecurityConfig {
    if s.config != nil {
        return s.config.SecurityConfig // novo campo em AppConfig
    }
    return security.DefaultConfig()
}
```

---

## Config (`internal/config/config.go`)

Adicionar ao `AppConfig`:
```go
type AppConfig struct {
    // ... existing fields ...
    SecurityConfig security.SecurityConfig `json:"security,omitempty"`
}
```

Default: `{ Mode: "block", DefaultProfile: "execute_safe", AllowPrivilegedAgents: false }`

Adicionar ao `fileConfig` e `defaultFileConfig` também.

---

## Perfis Mínimos para Processos Internos

| Processo | Profile | Tools |
|---|---|---|
| Dream (consolidação) | `edit_project` | Read, Grep, Glob, LS, Write, Edit |
| Nudge | `edit_project` | Read, Grep, Glob, LS, Write, Edit |
| Classify/Router | `observe` | nenhuma (já é sem tools) |
| Worker (orchestrator) | `execute_safe` | Read, Write, Edit, Bash governado |
| Reviewer/Validator | `read_only` | Read, Grep, Glob, LS |
| Auto-Skills generator | `observe` | nenhuma |

**Mudanças necessárias:**

| Arquivo | Mudança |
|---|---|
| `internal/dream/dream.go` | Mudar de `AllowedTools: []{}` + `DisallowedTools: [...]` para enviar `profile: "edit_project"` no security context. **Comportamento atual já é restritivo** — só formalizar via profile. |
| `internal/dream/nudge.go` | Mesma abordagem: enviar `profile: "edit_project"` |
| `internal/orchestrator/defaults.go` | Adicionar `CapabilityProfile: "execute_safe"` no `DefaultWorkerConfig` |
| `internal/orchestrator/execute.go` | Passar security context com profile do worker |

---

## Planos de Rollout (Fase 1 → Fase 2)

### Fase 1: Warn mode (default)

- Security config: `{ mode: "warn" }`
- Hook avalia TODOS os tool calls
- Bloqueia **apenas** casos extremos: `rm -rf /`, paths sensíveis óbvios (`.env`, `~/.ssh`)
- Demais violações: LOG + ALLOW (não bloqueia)
- Audit já registra tudo

**Objetivo:** Validar que o hook não quebra fluxos legítimos sem bloquear nada crítico.

### Fase 2: Block mode (default)

- Security config: `{ mode: "block" }`
- Bloqueia:
  - Leitura de `.env`, `~/.ssh`, `~/.aurelia/config/`
  - Comandos destrutivos (`rm -rf`, `sudo`, `chmod -R`)
  - Exfiltração (`curl`/`wget` + arquivo local)
  - Escrita fora do `cwd`
  - Acesso a env vars
- Permite: `go build`, `go test`, `npm test`, `git status/diff/log`, `go vet`
- Audit: registra allow + block

**Migração:** warn → block = mudar 1 bool no config. Pode ser feito via `app.json`.

---

## Testes

### Testes Go

| Teste | Arquivo |
|---|---|
| ProfileTools retorna tools corretas | `internal/security/security_test.go` |
| ResolveProfile intersecciona corretamente | `internal/security/security_test.go` |
| IsSensitivePath detecta `.env`, `~/.ssh`, etc | `internal/security/security_test.go` |
| IsDestructiveCommand detecta `rm -rf /` | `internal/security/security_test.go` |
| IsExfiltrationCommand detecta `curl ...` + path | `internal/security/security_test.go` |
| DefaultProfileForContext retorna correto | `internal/security/security_test.go` |
| Pipeline injeta SecurityContext no request | `internal/pipeline/pipeline_test.go` |
| Agent CapabilityProfile é lido do frontmatter | `internal/agents/agents_test.go` |

### Testes Bridge (TS)

| Teste | Como |
|---|---|
| Hook bloqueia `read` de `.env` | Teste integrado com PI SDK fake |
| Hook permite `go test ./...` | Teste integrado |
| Hook bloqueia `cat ~/.ssh/id_rsa` | Teste integrado |
| Hook retorna block reason sem vazar input | Validação de redação |
| Fail-closed: hook não disponível → sessão não cria | Mock |

### Testes de Regressão

```bash
go build ./...        # limpo
go vet ./...          # limpo
go test ./... -short  # passando
```

---

## Resumo dos Arquivos a Criar/Modificar

### Criar (4 arquivos)

| Arquivo | Conteúdo |
|---|---|
| `internal/security/profiles.go` | CapabilityProfile, ProfileTools, ResolveProfile, DefaultProfileForContext |
| `internal/security/policy.go` | SecurityConfig, BashPolicy, EvaluateToolCall, IsSensitivePath, IsDestructiveCommand, IsExfiltrationCommand |
| `internal/security/audit.go` | AuditEvent, LogAudit |
| `internal/security/security_test.go` | Testes unitários completos |

### Modificar (8 arquivos)

| Arquivo | Mudança |
|---|---|
| `bridge/index.ts` | +SecurityContext, +evaluateToolPolicy(), +logAudit(), +hook registration |
| `internal/bridge/protocol.go` | +SecurityContext struct |
| `internal/agents/types.go` | +CapabilityProfile field, atualizar IsReadOnly() |
| `internal/pipeline/pipeline.go` | Resolver profile, passar security context |
| `internal/pipeline/prompt_builder.go` | +buildSecurityPromptSection() |
| `internal/config/config.go` | +SecurityConfig field |
| `internal/dream/dream.go` | Usar `edit_project` profile (consistente) |
| `internal/dream/nudge.go` | Usar `edit_project` profile (consistente) |
| `internal/orchestrator/defaults.go` | +CapabilityProfile no DefaultWorkerConfig |

---

## Dependências entre Tarefas

```
1. internal/security/profiles.go     ← base (sem dependências)
2. internal/security/policy.go       ← depende de profiles.go
3. internal/security/audit.go        ← independente
4. internal/security/security_test.go ← depende de 1, 2, 3
5. internal/bridge/protocol.go       ← adicionar struct
6. internal/agents/types.go          ← adicionar field
7. bridge/index.ts                   ← depende de 5 (protocolo)
8. internal/pipeline/pipeline.go     ← depende de 1, 5, 6
9. internal/pipeline/prompt_builder.go ← depende de 1
10. internal/config/config.go        ← depende de 1
11. internal/dream/dream.go          ← mudança pequena
12. internal/dream/nudge.go          ← mudança pequena
13. internal/orchestrator/defaults.go ← mudança pequena
14. Bridge rebuild + deploy          ← integração final
```

**Ordem recomendada:** 1→2→3→4 (testes) →5→6→7 (bridge) →8→9→10→11→12→13→14
