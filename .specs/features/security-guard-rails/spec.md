# Security Guard-Rails — Capability Profiles & PI Tool Hooks

**Status:** ✅ **Implemented (v0.8.0)** — 100% complete  
**Companion specs:** `.specs/features/agent-orchestration-execution/`, `.specs/features/agent-comms/`, `.specs/features/pi-resilience/`  
**Related:** `SECURITY.md`

---

## Problem Statement

O Aurelia delega execução real para o **PI SDK** (`@earendil-works/pi-coding-agent`) através do Bridge TypeScript. Isso é uma força do produto: o PI é o motor de execução, com tools, sessões, extensões e streaming. Mas o processo bridge roda com os mesmos privilégios do usuário local, então tools como `bash`, `read`, `write` e `edit` podem acessar dados sensíveis se não forem governadas.

A versão anterior desta spec propunha remover `Bash` do default e permitir apenas por opt-in explícito. Isso é seguro, mas restritivo demais para um coding agent: sem `Bash`, muitos fluxos legítimos quebram (`go test`, `go build`, `npm test`, `git diff`, validators, scripts locais).

A documentação atual do PI mostra uma abordagem melhor:

- `createAgentSession({ tools: [...] })` permite controlar a allowlist ativa por sessão.
- Extensões/hooks `pi.on("tool_call", ...)` rodam antes da execução da tool.
- O hook pode bloquear chamadas perigosas ou ajustar argumentos.
- Custom tools e `setActiveTools` permitem evoluir para ambientes ainda mais governados.

Portanto, a direção correta para Aurelia não é “desligar Bash”, e sim **governar tools por perfil de capacidade e interceptar chamadas perigosas via PI tool hooks**.

## Goals

- [ ] Introduzir `CapabilityProfile` como fonte da verdade para tools disponíveis por contexto
- [ ] Manter `Bash` disponível em contextos de coding (`execute_safe`), mas bloqueado por policy quando perigoso
- [ ] Usar PI `tool_call` hooks no bridge para validar `bash`, `read`, `write`, `edit`, `grep`, `find/Glob`, `ls`
- [ ] Bloquear acesso a secrets e paths sensíveis antes da execução da tool
- [ ] Bloquear ou exigir aprovação para comandos destrutivos, exfiltração e operações fora do `cwd`
- [ ] Dream/nudge/validator/reviewer usam perfis mínimos, normalmente sem `Bash`
- [ ] Audit logging estruturado para tool calls permitidas, bloqueadas e futuramente aprovadas
- [ ] Fail-closed: se a policy não carregar, sessões com tools perigosas não iniciam sem proteção
- [ ] Prompt hardening continua como camada auxiliar, não como enforcement principal
- [ ] Zero regressão para fluxo normal de coding seguro: build/test/lint continuam possíveis

## Out of Scope

- Sandbox de SO completo (Landlock, Seatbelt, seccomp) — fase futura
- Containerização obrigatória de todas as execuções
- DLP perfeito ou classificação robusta de PII — MVP usa heurísticas conservadoras
- Aprovação humana completa no MVP; pode começar como “block” para comandos ambíguos
- Rede/cross-device Agent Comms — coberto futuramente por `.specs/features/agent-comms/`
- Criptografia de secrets em disco
- Substituir todas as tools built-in por wrappers custom no MVP

---

## Updated Security Model

Segurança em camadas:

```text
Agent/Context → Capability Profile → RequestOptions.tools → PI tool_call hook → Audit → Result
```

1. **Capability Profile** define quais tools podem existir naquela sessão.
2. **PI tool hook** decide se uma chamada específica é permitida.
3. **Audit** registra decisão, contexto e argumentos redigidos.
4. **Prompt hardening** orienta o modelo, mas não substitui enforcement.

---

## Capability Profiles

| Profile | Tools típicas | Uso |
|---|---|---|
| `observe` | nenhuma ou tools internas sem filesystem | classificação, roteamento, geração sem tools |
| `read_only` | `Read`, `Grep`, `Glob/Find`, `LS` | reviewer, validator, discovery, análise |
| `edit_project` | read-only + `Write`, `Edit` | Plan Mode materializando docs, dream/nudge, docs |
| `execute_safe` | edit_project + `Bash` governado | coding normal, workers de implementação |
| `privileged` | tools amplas + aprovações explícitas | manutenção avançada, opt-in futuro |

Defaults recomendados:

| Contexto | Profile default |
|---|---|
| Chat normal com `cwd` | `execute_safe` |
| Chat sem `cwd` | `observe` ou `read_only` |
| Plan Mode antes de materializar | `read_only` |
| Plan Mode materializando spec/design/tasks | `edit_project` |
| Worker de implementação | `execute_safe` |
| Worker reviewer/validator | `read_only` |
| Dream/nudge | `edit_project` |
| Auto-Skills generator | `observe` ou no-tools |
| Agent Comms | sem tools diretas; policy de payload |

Agentes podem declarar profile:

```yaml
---
name: backend-coder
capability_profile: execute_safe
allowed_tools: [Read, Grep, Glob, LS, Write, Edit, Bash]
---
```

`allowed_tools` e `disallowed_tools` continuam suportados, mas são interpretados dentro dos limites do profile e da policy global.

---

## Threat Model

| Vetor | Risco | Mitigação MVP |
|---|---|---|
| `bash` lendo secrets | crítico | hook bloqueia comandos/paths/env access |
| `bash` exfiltrando dados | crítico | hook bloqueia `curl/wget/nc/scp/rsync` suspeitos |
| `read` fora do projeto | alto | hook bloqueia sensitive paths e, por profile, paths fora do `cwd` |
| `write/edit` fora do projeto | alto | hook bloqueia escrita fora do `cwd` salvo allowlist explícita |
| destructive commands | alto | hook bloqueia ou exige aprovação |
| prompt injection | alto | enforcement no hook, não só prompt |
| agent config inseguro | médio | validação de profile/tools no load |
| audit sem contexto | médio | audit estruturado por chat/thread/user/agent/task |

---

## User Stories

### P0: Capability profiles como fonte da verdade ⭐ MVP

**User Story:** Como desenvolvedor, quero controlar capacidade por contexto, não por bloqueios globais que quebram o coding agent.

**Acceptance Criteria:**

1. WHEN `CapabilityProfile` é resolvido THEN ele SHALL produzir uma allowlist efetiva de tools.
2. WHEN um agente declara `capability_profile` THEN esse profile SHALL limitar suas `allowed_tools`.
3. WHEN um agente não declara profile THEN Aurelia SHALL escolher default pelo contexto de execução.
4. WHEN `allowed_tools` contém tool fora do profile THEN policy SHALL remover ou rejeitar conforme modo configurado.
5. WHEN profile é `read_only` THEN `Write`, `Edit` e `Bash` SHALL estar indisponíveis.
6. WHEN profile é `execute_safe` THEN `Bash` MAY estar disponível, mas sempre governado por hook.

**Independent Test:** Agent `read_only` com `allowed_tools: [Read, Bash]` não recebe `bash`; worker `execute_safe` recebe `bash` mas policy hook está ativo.

---

### P0: PI `tool_call` hook obrigatório para tools perigosas ⭐ MVP

**User Story:** Como operador, quero que o Bridge bloqueie chamadas perigosas antes de o PI executar a tool.

**Acceptance Criteria:**

1. WHEN uma sessão com `Bash`, `Read`, `Write` ou `Edit` é criada THEN bridge SHALL registrar policy hook antes do primeiro prompt.
2. WHEN hook não inicializa THEN sessão com tools perigosas SHALL falhar fechada.
3. WHEN PI dispara `tool_call` THEN hook SHALL avaliar `toolName`, `input`, `cwd`, profile e contexto da request.
4. WHEN policy retorna `block` THEN tool SHALL não executar e o modelo SHALL receber motivo seguro.
5. WHEN policy retorna `allow` THEN tool executa normalmente.
6. WHEN policy retorna `rewrite` THEN input modificado SHALL ser usado e auditado.

**Independent Test:** Fake hook bloqueia `bash: cat ~/.aurelia/config/app.json` antes da execução; `go test ./...` passa.

---

### P0: Bash policy granular, não default-off ⭐ MVP

**User Story:** Como usuário, quero que Aurelia continue conseguindo rodar build/test/lint, mas sem permitir comandos perigosos.

**Acceptance Criteria:**

1. WHEN command é claramente seguro (`go test ./...`, `go build ./...`, `go vet ./...`, `npm test`, `npm run build`, `git status`, `git diff`) THEN policy SHALL permitir em `execute_safe`.
2. WHEN command acessa env/secrets (`env`, `printenv`, `echo $TOKEN`, `cat ~/.aurelia/config/app.json`) THEN policy SHALL bloquear.
3. WHEN command usa destructive patterns (`rm -rf /`, `sudo`, `chmod -R`, `chown -R`, `dd`, fork bomb) THEN policy SHALL bloquear.
4. WHEN command tenta exfiltração (`curl`, `wget`, `nc`, `scp`, `rsync`) combinada com arquivo/local secret/stdin suspeito THEN policy SHALL bloquear.
5. WHEN command altera git remoto (`git push --force`, remote add com token, credential helpers) THEN policy SHALL bloquear ou exigir aprovação futura.
6. WHEN command é ambíguo mas não obviamente malicioso THEN MVP SHALL bloquear com mensagem sugerindo confirmação humana futura.

**Independent Test:** Tabela de comandos permitidos/bloqueados em `execute_safe`.

---

### P0: Filesystem policy para Read/Write/Edit ⭐ MVP

**User Story:** Como operador, quero impedir acesso a secrets e escrita fora do projeto, mesmo sem Bash.

**Acceptance Criteria:**

1. WHEN `Read` aponta para sensitive path THEN policy SHALL bloquear.
2. WHEN `Write` ou `Edit` aponta fora do `cwd` THEN policy SHALL bloquear, salvo allowlist explícita.
3. WHEN path usa `..`, symlink ou path absoluto THEN policy SHALL resolver caminho real antes de decidir.
4. WHEN path é `.env`, `.env.*`, private key, shell history, `~/.ssh`, `~/.pi`, `~/.aurelia/config` THEN bloquear.
5. WHEN path está dentro do `cwd` e não é sensível THEN permitir conforme profile.

**Independent Test:** `Read` de `./README.md` passa; `Read` de `~/.ssh/id_rsa` falha; `Write` em `../outside.txt` falha.

---

### P0: Dream, nudge, validator e generators com perfis mínimos ⭐ MVP

**User Story:** Como operador, quero que processos internos e background tenham só as tools necessárias.

**Acceptance Criteria:**

1. Dream/nudge SHALL usar `edit_project` sem `Bash`.
2. Validator/reviewer SHALL usar `read_only` por padrão.
3. Auto-Skills generator SHALL usar `observe` ou no-tools com `NoUserSettings=true`.
4. Classify/router SHALL usar `observe` ou no-tools.
5. Qualquer tentativa de adicionar `Bash` nesses contextos SHALL falhar em teste.

**Independent Test:** Requests enviados por dream/nudge/validator/generator têm tools compatíveis com profile esperado.

---

### P1: Audit logging estruturado e redigido ⭐ MVP

**User Story:** Como operador, quero auditar o que o agente tentou fazer sem vazar secrets no próprio log.

**Acceptance Criteria:**

1. WHEN tool call é avaliada THEN audit SHALL registrar `allowed`, `blocked`, `rewritten` ou `approval_required`.
2. Audit SHALL conter `tool_name`, `chat_id`, `thread_id`, `user_id`, `agent_name`, `request_id`, `task_id`, `cwd`, `profile`, decisão e razão.
3. Inputs SHALL ser redigidos para tokens, API keys, bearer, passwords e high-entropy strings.
4. Audit MAY preservar comandos/paths seguros para forense.
5. WHEN audit falha THEN execução continua, mas log interno registra warning.

**Independent Test:** Tool call com bearer token é bloqueada e audit não contém token literal.

---

### P1: Agent definition validation ⭐ MVP

**User Story:** Como operador, quero detectar configurações de agente incoerentes ou inseguras no load.

**Acceptance Criteria:**

1. WHEN agent declara profile desconhecido THEN load SHALL rejeitar esse agente ou logar erro e não registrá-lo.
2. WHEN agent declara unknown tool THEN load SHALL warning e preservar compatibilidade, como comportamento atual.
3. WHEN agent declara `allowed_tools: []` explicitamente THEN isso SHALL significar no-tools, não fallback para defaults.
4. WHEN agent pede `privileged` THEN deve exigir flag/config explícita do operador.
5. WHEN agent combina `read_only` com `Write/Edit/Bash` THEN policy SHALL remover ou rejeitar conforme modo configurado.

**Independent Test:** Agent `privileged` sem config é rejeitado; agent `allowed_tools: []` recebe tools vazias.

---

### P1: Prompt hardening como camada auxiliar

**User Story:** Como operador, quero que o modelo saiba as regras, mesmo que enforcement real esteja no hook.

**Acceptance Criteria:**

1. System prompt SHALL incluir seção curta `Security Boundaries` quando tools estão disponíveis.
2. Prompt SHALL dizer para operar dentro do `cwd` e não acessar secrets.
3. Prompt SHALL orientar a pedir confirmação para comandos destrutivos/ambíguos.
4. Prompt SHALL refletir o profile ativo de forma simples.
5. Tests SHALL validar presença da seção nos prompts relevantes.

---

### P2: Aprovação humana para comandos ambíguos

**User Story:** Como usuário, quero poder aprovar uma ação arriscada uma única vez pelo Telegram.

**Acceptance Criteria:**

1. WHEN policy retorna `approval_required` THEN execution SHALL pausar a tool call e enviar prompt ao Telegram.
2. User pode permitir uma vez ou negar.
3. Aprovação SHALL expirar rapidamente e valer só para request/tool_call específica.
4. Decisão SHALL ser auditada.
5. Sem resposta até timeout, tool SHALL ser negada.

---

### P3: Sandbox de SO

**User Story:** Como operador avançado, quero enforcement de filesystem no sistema operacional, não apenas hook lógico.

**Acceptance Criteria:**

1. Linux MAY usar Landlock/seccomp.
2. macOS MAY usar Seatbelt/sandbox-exec quando viável.
3. Bridge SHALL ter acesso apenas a `cwd`, temp dir e paths explicitamente permitidos.
4. Hooks continuam existindo como defense in depth.

---

## Data Models

### Go policy model

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

type PolicyMode string

const (
    PolicyWarn  PolicyMode = "warn"
    PolicyBlock PolicyMode = "block"
)

type SecurityPolicy struct {
    Mode                  PolicyMode
    DefaultProfile         CapabilityProfile
    AllowPrivilegedAgents  bool
    SensitivePathPatterns  []string
    AllowedOutsideCWDPaths []string
    BashRules              BashPolicy
}

type ToolDecision string

const (
    DecisionAllow            ToolDecision = "allow"
    DecisionBlock            ToolDecision = "block"
    DecisionRewrite          ToolDecision = "rewrite"
    DecisionApprovalRequired ToolDecision = "approval_required"
)

type ToolPolicyDecision struct {
    Decision ToolDecision
    Reason   string
    Input    any
}
```

### Bridge request additions

`RequestOptions` should carry policy context:

```ts
interface SecurityContext {
  enabled: boolean;
  profile: "observe" | "read_only" | "edit_project" | "execute_safe" | "privileged";
  cwd: string;
  chat_id?: number;
  thread_id?: number;
  user_id?: number;
  agent_name?: string;
  task_id?: string;
  request_id?: string;
}

interface RequestOptions {
  allowed_tools?: string[];
  disallowed_tools?: string[];
  security?: SecurityContext;
}
```

### Bridge hook sketch

```ts
pi.on("tool_call", async (event, ctx) => {
  const decision = evaluateToolPolicy({
    toolName: event.toolName,
    input: event.input,
    security: opts.security,
  });

  emitAudit(decision, event);

  if (decision.decision === "block") {
    return { block: true, reason: decision.reason };
  }

  if (decision.decision === "rewrite") {
    Object.assign(event.input as object, decision.input);
  }
});
```

---

## Implementation Map

| File | Action | Responsibility |
|---|---|---|
| `internal/security/profiles.go` | Create | Capability profiles and tool mapping |
| `internal/security/policy.go` | Create | Policy config and validation contracts |
| `internal/security/audit.go` | Create | Audit event model and redaction helpers |
| `internal/agents/types.go` | Modify | Add `CapabilityProfile` frontmatter field |
| `internal/agents/registry.go` | Modify | Validate profile/tool combinations |
| `internal/pipeline/pipeline.go` | Modify | Resolve profile and pass security context to bridge |
| `internal/orchestrator/defaults.go` | Modify | Assign worker defaults using profiles, not raw tools only |
| `internal/orchestrator/execute.go` | Modify | Pass task security context and task id |
| `internal/dream/dream.go` | Modify | Use `edit_project`, no Bash |
| `internal/dream/nudge.go` | Modify | Use `edit_project`, no Bash |
| `bridge/index.ts` | Modify | Add security context, hook registration, policy evaluator |
| `internal/bridge/protocol.go` | Modify | Add security context fields |
| `README.md` / `SECURITY.md` | Modify | Document profiles and guard-rails |

---

## Rollout

### Phase 1: Warn mode

- Implement profiles and audit.
- Hook evaluates policy but only blocks extreme cases: secrets paths, `rm -rf /`, env dump.
- Log what would be blocked by stricter mode.

### Phase 2: Block mode default

- Block sensitive paths, env access, destructive commands and exfiltration patterns.
- Keep normal build/test/lint working.

### Phase 3: Human approval

- Replace some ambiguous blocks with Telegram approval flow.

### Phase 4: OS sandbox

- Add process-level restrictions as defense in depth.

---

## Edge Cases

- WHEN PI hook API changes THEN bridge tests SHALL fail before release.
- WHEN hook blocks a tool THEN model receives safe reason, not sensitive raw input.
- WHEN path resolution fails THEN block by default for read/write/edit.
- WHEN symlink points outside `cwd` THEN block unless allowlisted.
- WHEN command is safe but long-running THEN existing timeout/cancel flow still applies.
- WHEN no `cwd` is configured THEN profiles with write/bash SHALL be downgraded or refused.
- WHEN `NoUserSettings=true` THEN security hook still loads; user extensions/settings do not.
- WHEN Agent Comms sends payloads THEN Agent Comms policy handles payload; no filesystem tools are implied.

---

## Success Criteria

- [ ] Normal coding worker can still run `go test ./...`, `go build ./...`, `go vet ./...`
- [ ] `cat ~/.aurelia/config/app.json` is blocked before execution
- [ ] `Read` of `~/.ssh/id_rsa` is blocked before execution
- [ ] `Write` outside `cwd` is blocked by default
- [ ] Dream/nudge/validator/generator use minimal profiles
- [ ] Tool decisions are audit logged with redaction
- [ ] Security hook fail-closed is tested
- [ ] Existing `allowed_tools`/`disallowed_tools` behavior remains backward compatible
- [ ] No broad breaking change requiring all existing agents to add `Bash`
- [ ] `go build ./... && go vet ./... && go test ./...` clean when implemented

---

## Validation Commands

```bash
go build ./...
go vet ./...
go test ./... -short
go test ./... -v
```

Bridge rebuild required if `bridge/index.ts` changes:

```bash
cd bridge && npm run build
cp bundle.js ../internal/bridge/bundle.js
```
