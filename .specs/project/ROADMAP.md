# Roadmap

## Done / Validated Foundation

Estas features já foram implementadas ou têm validação registrada. Elas são base do roadmap atual.

| Feature | Spec | Status | Notes |
|---------|------|:------:|-------|
| Bridge Recovery | `.specs/features/bridge-recovery/` | Validated | Auto retry with session resume, cooldown after consecutive bridge failures |
| Command Layer | `.specs/features/command-layer/` | Done | Local interception of deterministic commands; avoids unnecessary LLM calls |
| Agent Tools Fix | `.specs/features/agent-tools-fix/` | Validated | `disallowed_tools` honored end-to-end; future governance moved to guard-rails |
| PI Resilience | `.specs/features/pi-resilience/` | Validated | Retry, fallback, circuit breaker, translated actionable errors |
| Dependency Checker | `.specs/features/dependency-checker/` | Validated | Pre-flight checks for Node/npm/git/gh |
| UX Polish | `.specs/features/ux-polish/` | Mostly validated | Ack, queue/status/progress polish, better errors and help |
| **Security Guard-Rails** | `.specs/features/security-guard-rails/` | **✅ 100%** | CapabilityProfile, policy engine, bridge hooks, audit, 44 tests. Profiles: observe→privileged. Fail-closed. |
| **Persistent Project Binding** | `.specs/features/project-binding/` | **✅ 95%** | SQLite store, `/cwd` persistente via restart, fallback tópico→grupo, pipeline resolve. Só falta integração com User Isolation. |

---

## Current Evolution Track

Aurelia continua sendo um **personal agent persistente via Telegram**, com PI como motor de execução e Go como camada de orquestração, persistência, segurança e memória.

A próxima onda foca em tornar o sistema seguro e estável para trabalho autônomo em projetos reais:

1. **delegar reimplementações ao PI SDK nativo** — eliminar ~1.816 linhas de código duplicado;
2. isolar usuários autorizados e propagar `user_id` no runtime;
3. fechar o ciclo de execução orquestrada (o scaffold de ~80% precisa ser conectado);
4. transformar plan mode em modo explícito, persistente e retomável;
5. escopar memória por usuário;
6. promover a memória para uma Wiki transversal via MCP;
7. só então ativar nudge profundo, agent comms e auto-skills.

**Ordem é importante:** cada spec depende da anterior, técnica e conceitualmente. O refactoring do PI SDK pode ser feito em paralelo com User Isolation, mas deve ser merged antes para reduzir a superfície de código.

> **Nota sobre o delta real:** Security Guard-Rails e Project Binding já foram implementados (revisão de Maio 2026), então o roadmap foi reordenado para refletir o estado real da codebase. Agent Orchestration subiu do passo 8 para o passo 2 porque ~40% do código já existe em `internal/orchestrator/` e precisa ser fechado antes de Plan Mode poder fazer handoff.

---

## 0. Delegate to PI SDK Native

**Spec:** `.specs/features/delegate-to-pi-sdk/`  
**Design:** `.specs/features/delegate-to-pi-sdk/design.md`  
**Tasks:** `.specs/features/delegate-to-pi-sdk/tasks.md`  
**Status:** 🔴 A implementar  
**Prioridade:** P0 (pode rodar em paralelo com User Isolation)  
**Depends on:** `security-guard-rails` (✅ v0.8.0)  

**Problem:** O Aurélia reimplementa em Go e TypeScript funcionalidades que o PI SDK já oferece nativamente: agent registry, security policy engine, session management, token tracking, model registry resolution, system prompt assembly. Isso quebra o princípio do projeto: "não reimplementar o que o PI já faz".

**Scope:**
- Bridge: simplificar model resolution, manter security hooks
- Go: remover `internal/security/policy.go` (~514 linhas), simplificar `internal/session/store.go`, eliminar `internal/session/tracker.go`, refatorar `prompt_builder.go`, remover `internal/agents/`
- **Preservar:** persona, memory, cron, telegram, orchestrator (não têm equivalente PI)

**Por que agora:** Reduz ~1.816 linhas de código duplicado antes de User Isolation, diminuindo a superfície a isolar. Security-guard-rails está estável (100%), então a policy engine Go pode ser removida com confiança.

---

## 1. User Isolation

**Spec:** `.specs/features/multi-user-profiles/`
**Design:** `.specs/features/multi-user-profiles/design.md`
**Tasks:** `.specs/features/multi-user-profiles/tasks.md`
**Status:** 🔴 A implementar
**Prioridade:** P0 foundation

**Problem:** A whitelist já permite múltiplos `user_id`s, mas o runtime ainda mistura estado pessoal: sessões, memória, persona USER, cron, usage e comandos de controle.

**MVP Scope (Phase 0–3 do tasks.md):**

- `TurnContext` com `user_id`;
- `SessionKey{chat_id, thread_id, user_id}` para sessão LLM/usage/active run;
- `ConversationKey{chat_id, thread_id}` para `/cwd` e topic memory (já existe em `internal/projectbinding/`);
- `internal/users/` — Profile, Resolver, Store;
- `UserGate` antes de comandos/pipeline;
- USER/persona/memória pessoal por usuário;
- cron owner normalizado;
- Comando CLI `migrate-multi-user`.

**Por que primeiro:** sem `user_id` propagado, Plan Mode, Auto-Skills, memória e nudge vazam estado entre usuários autorizados. Tudo depende disso.

---

## 2. Close Orchestration Cycle

**Spec:** `.specs/features/agent-orchestration-execution/`
**Design:** `.specs/features/agent-orchestration-execution/design.md`
**Tasks:** `.specs/features/agent-orchestration-execution/tasks.md`
**Status:** 🟡 Parcial (scaffold ~40%, ciclo não fecha)
**Depende de:** User Isolation partial para `TurnContext`/`user_id` no handoff; Project Binding já disponível

**Problem:** Aurelia já tem `internal/orchestrator/` com worktree, waves, git.go, validate.go, tasks_status.go (80% do código), mas **o ciclo não fecha**: `Validate`, `CommitChanges`, `CreatePR`, `UpdateTasksStatus` não são chamados no fluxo real. `currentBranch()` retorna hardcoded `"HEAD"`. Thread ID é perdido no handoff. O executor funcional prometido pela spec nunca foi entregue.

**Scope:**

- `ExecutionContext` com cwd persistente, thread/user/security context;
- git preflight (recusa dirty base, detached HEAD);
- validation com diff/verify real + retry com feedback;
- merge serial com dependentes skipped;
- update `tasks.md`, commit seguro e PR opcional;
- orphan worktree cleanup no startup;
- artifact collection + manifest.

**Por que agora:** o scaffold já existe e ~40% do esforço total foi investido, mas o ciclo não fecha. Plan Mode precisa do handoff funcionando. É mais rápido conectar o que já existe do que reconstruir depois.

---

## 3. Plan Mode Architecture

**Spec:** `.specs/features/plan-mode-architecture/`
**Design:** `.specs/features/plan-mode-architecture/design.md`
**Tasks:** `.specs/features/plan-mode-architecture/tasks.md`
**Status:** 🟡 Parcial (detecção heurística + parsing de `aurelia-plan` existem)
**Depende de:** User Isolation + Orchestration Cycle (ExecutionContext, handoff)

**Problem:** hoje planejamento é implícito: keywords disparam prompt de orquestração sem permissão, e `aurelia-plan` executa sem estado persistente. Precisa virar modo explícito, persistente e retomável.

**Scope:**

- `/plan`, `/plan status`, `/plan cancel`, `/plan list`;
- state persistente em SQLite por `SessionKey`;
- discovery baseado no project binding;
- materialização observada via Write/Edit (`observer.go`);
- offer-only heuristic (oferece, nunca impõe);
- handoff seguro para executor via `ExecutionContext`.

**Por que depois da orquestração:** Plan Mode produz o plano que o executor consome. O handoff depende do `ExecutionContext` e do preflight definidos na Orchestration spec.

---

## 4. User-Scoped Project Memory

**Spec:** `.specs/features/project-memory/`
**Status:** 🟡 Parcial (70% — layers existem, mas não são isoladas por usuário)
**Depende de:** User Isolation (para paths `users/<id>/`)

**Problem:** a versão atual de memória por projeto é global por `cwd`. Com User Isolation, precisa ser escopada por `(user_id, project_slug)`. As camadas já existem (global, topic, project-private, team), mas os paths não são per-user.

**Scope:**

- user global memory em `~/.aurelia/users/<id>/memory/`;
- user × project private memory em `~/.aurelia/users/<id>/projects/<slug>/memory/`;
- project team memory em `~/.aurelia/projects/<slug>/team/` (já existe);
- topic memory em `~/.aurelia/topics/chat_<id>/thread_<id>/` (já existe);
- prompt assembly com camadas corretas por `TurnContext`;
- runtime.PathResolver com métodos `User*`.

**Por que antes da Wiki:** a Wiki MCP vai expor as mesmas camadas; precisa estar correta primeiro.

---

## 5. Wiki Memory Gateway

**Spec:** `.specs/features/wiki-memory/`
**Status:** 🔴 Spec arquitetural apenas
**Depende de:** User Isolation + Project Memory

**Problem:** a memória atual é útil dentro do Aurelia, mas não é transversal para outros pontos de entrada como PI/PI Code/opencode.

**Scope:**

- Wiki como LLM Wiki local-first do Aurelia;
- MCP gateway para query/save/ingest/lint/status;
- markdown como fonte auditável, SQLite/FTS como índice opcional;
- escopos fortes: user, user×project private, project team, topic, procedural;
- query-before-inject para reduzir prompt bloat;
- receipts/audit para escritas externas.

**Princípio:** acesso transversal, memória escopada.

---

## 6. Learning Nudge — Scoped Memory Review

**Spec:** `.specs/features/learning-nudge/`
**Status:** 🔴 Draft revisado
**Depende de:** User Isolation + Project Memory + Security Guard-Rails + Wiki Memory Gateway

**Problem:** extração por-turn/snippet perde contexto; nudge profundo precisa ser escopado para não vazar entre usuários/projetos e deve escrever através da Wiki.

**Scope:**

- transcript recorder por `SessionKey`;
- redaction antes de chamar PI;
- `CapabilityProfile=edit_project`, sem `Bash`;
- escrita nas camadas de memória corretas via Wiki;
- sugestões de Auto-Skills, sem criar skills automaticamente.

---

## 7. Agent Comms

**Spec:** `.specs/features/agent-comms/`
**Status:** 🔴 Draft
**Depende de:** Orchestration Cycle + Security Guard-Rails

**Problem:** workers especializados ganham qualidade quando podem consultar peers, mas precisa ser local, auditado e com limites.

**Scope:**

- Agent Bus local por run;
- peers explícitos por task;
- anti-loop/budget/timeouts;
- payload policy e audit;
- manifest com peer message counts;
- sem rede/cross-device no MVP.

**Por que depois da execução:** é melhoria da orquestração, não requisito para o primeiro executor seguro.

---

## 8. Auto-Skills

**Spec:** `.specs/features/auto-skills/`
**Status:** 🔴 Draft revisado
**Depende de:** User Isolation + Security Guard-Rails; ganha valor com as features anteriores

**Problem:** tarefas bem-sucedidas viram conhecimento perdido; Auto-Skills transforma procedimentos úteis em skills privadas, PI-compatible (`SKILL.md`), gerenciadas pelo Aurelia.

**Scope:**

- recorder de último turno bem-sucedido;
- `/skill save <slug>` explícito;
- geração via LLM sem tools;
- validação rígida de frontmatter Agent Skills/PI + adapter Aurelia;
- storage privado por user em layout `<slug>/SKILL.md`;
- `capability_profile` obrigatório/validado;
- registry per-user.

**Decisão:** Opção A — Aurelia-native, PI-compatible. Não usar `pi-hermes-memory` nem escrever em `~/.pi/agent` no MVP.

---

## Sequenciamento resumido

```text
Foundation validada (inclui Security Guard-Rails + Project Binding)
      │
      ├──→ 0. Delegate to PI SDK Native (paralelo, reduz LOC)
      │
      ▼
1. User Isolation
      │
      ▼
2. Close Orchestration Cycle
      │
      ▼
3. Plan Mode Architecture
      │
      ▼
4. User-Scoped Project Memory
      │
      ▼
5. Wiki Memory Gateway
      │
      ▼
6. Learning Nudge
      │
      ▼
7. Agent Comms
      │
      ▼
8. Auto-Skills
```

## Mapa de implementação por sprint

```
Sprint 0: Delegate to PI SDK Native
  ├─ Bridge: simplify model resolution
  ├─ Go: remove security/policy.go (~514 lines)
  ├─ Go: simplify session store (265→80 lines)
  ├─ Go: remove token tracker (131 lines)
  ├─ Go: refactor prompt builder (861→500 lines)
  ├─ Go: remove agent registry (~300 lines)
  ├─ Script: migrate agents ~/.aurelia/ → ~/.pi/agent/
  └─ Validation: build/test/e2e clean

Sprint A: User Isolation MVP (Phase 0–3)
  ├─ TurnContext + SessionKey/ConversationKey
  ├─ internal/users/ (Profile, Resolver, Store)
  ├─ CLI migrate-multi-user
  ├─ cron owner normalizado
  ├─ session isolation + persona per-user
  ├─ memory/dream per-user
  ├─ pipeline integration + UserGate
  └─ owner-only commands

Sprint B: Close Orchestration Cycle (T0–T12 do tasks.md)
  ├─ ExecutionContext com cwd+threadID
  ├─ git preflight
  ├─ artifact collection + verify command
  ├─ fail-closed validation com retry
  ├─ merge serial + skip dependents
  ├─ commit + PR + tasks.md update
  ├─ orphan cleanup no startup
  └─ integration smoke test

Sprint C: Plan Mode (T0–T13 do tasks.md)
  ├─ internal/planning/ types + SQLite store
  ├─ context discovery
  ├─ BuildPlanningPrompt + observer
  ├─ offer-only heuristic
  ├─ /plan commands
  └─ handoff com ExecutionContext

Sprint D: User-Scoped Project Memory
  ├─ runtime.PathResolver per-user
  ├─ prompt assembly com camadas corretas
  └─ dream/nudge com targets escopados

Sprint E: Wiki Memory Gateway
  ├─ MCP server interno
  ├─ wiki_query/save/status
  └─ query-before-inject

Sprint F: Learning Nudge
  ├─ transcript recorder por SessionKey
  ├─ redaction + profile edit_project
  └─ escrita via Wiki scopes

Sprint G: Agent Comms
  ├─ Agent Bus local por run
  ├─ peers explícitos + limites
  └─ manifest + audit

Sprint H: Auto-Skills
  ├─ skill recorder
  ├─ /skill save + generator
  ├─ validator de SKILL.md
  └─ registry per-user
```

## Nota de implementação incremental

`User Isolation` é grande. Não é necessário implementar tudo antes de seguir. O primeiro slice deve entregar apenas a base que desbloqueia o resto:

```text
TurnContext
SessionKey com user_id
ConversationKey (já existe)
internal/users/ básico
UserGate
cron owner
migração CLI
```

Depois disso, `Orchestration Cycle` já pode usar `ExecutionContext` com `user_id` opcional (default=0 durante transição) e estabilizar a execução.

## Backlog futuro

- Cross-device Agent Comms seguro
- Human approval flow para guard-rails ambíguos
- OS sandbox para Bridge
- Project history/favorites para `/cwd`
- Team memory sync via git

## Notas de visão

Aurelia ocupa o nicho de **personal agent persistente via Telegram**. Não é IDE, não é SaaS multi-tenant, não é apenas coding agent. PI SDK é o motor de inferência/execução; Go é a camada de orquestração, segurança, memória, persistência e UX Telegram.
