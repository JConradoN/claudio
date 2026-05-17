# Roadmap

## Done

| Feature | Spec | Notes |
|---------|------|-------|
| Bridge Recovery | `.specs/features/bridge-recovery/` | Auto retry with session resume, cooldown after 3 consecutive failures (commit `0484f08`) |
| Command Layer | `.specs/features/command-layer/` | Local interception of cron CRUD, reset, status, list agents/models — saves ~15s per call |
| Agent Tools Fix | `.specs/features/agent-tools-fix/` | `disallowed_tools` now honored end-to-end (commit `1bf7572`) |
| Project Memory | `.specs/features/project-memory/` | Project-scoped 3-layer memory + nudge integration (commit `fa53095`) |
| Learning Nudge | `.specs/features/learning-nudge/` | Periodic dream/nudge review replaces per-turn extraction (commit `81a58d4`) |
| PI Resilience | `.specs/features/pi-resilience/` | Resilient bridge wrapper + per-class circuit breaker + run supervisor in `internal/pipeline/` |
| Dependency Checker | `.specs/features/dependency-checker/` | Pre-flight check for Node/npm/git/gh during onboarding (commit `3f9dd19`) |
| UX Polish | `.specs/features/ux-polish/` | Ack, status, progress, queue, reset summary, help, error hints (commit `da8abeb`) |

---

## In Flight — Evolution Track

Aurelia hoje funciona como personal agent de um deployment. A próxima onda de evolução mantém essa identidade, mas endurece o isolamento por identidade Telegram: cada `user_id` autorizado pela whitelist tem memória, sessão, cron e skills privados. Isso não transforma Aurelia em plataforma multi-tenant; é proteção contra vazamento em DMs, grupos e deployments com mais de uma pessoa autorizada.

**Ordem é importante**: cada spec depende da anterior. A dependência é tanto técnica (estruturas, paths, `user_id`) quanto conceitual (cada uma encaixa peças que a próxima usa).

### 1. Agent Orchestration — Execution

**Spec:** `.specs/features/agent-orchestration-execution/`
**Status:** ~80% no código (`internal/orchestrator/`); spec reescrita para fechar gaps remanescentes
**Tamanho:** ~12 tasks

**Problem:** Aurelia já decompõe planos em workers e executa em worktrees, mas o ciclo não fecha: `currentBranch` é stub, validation não tem retry, `CommitChanges`/`CreatePR`/`UpdateTasksStatus` existem mas não são chamados, worktrees órfãos não são limpos no startup, `task.Prompt` aparece duplicado no system+user, e o lookup de spec.md/design.md por feature pega o último alfabético.

**Scope (P1):** real `git rev-parse` para base branch, retry-with-feedback no Quality Gate (até 3x antes de escalar), wiring de commit/PR/tasks-status, orphan cleanup no startup, plan carrega campo `feature` e `create_pr`.

**Scope (P2):** per-worker `max_turns`, cost tracking, partial-failure polish.

**Scope (P3):** budget enforcement por worker.

**Packages:** `internal/orchestrator/`, `internal/pipeline/`, `internal/telegram/`

---

### 2. User Isolation

**Spec:** `.specs/features/multi-user-profiles/`
**Status:** A implementar
**Tamanho:** ~20 tasks
**Depende de:** Nenhuma (mas executar **antes** de 3 e 4)

**Problem:** A whitelist (`TelegramAllowedUserIDs`) está pronta para vários users, mas internamente Aurelia ainda precisa tratar cada sender como uma identidade isolada. `~/.aurelia/memory/` é global, `USER.md` é único, sessões ainda são por chat/thread, e cron owner precisa ser normalizado. Adicionar uma segunda conta autorizada hoje pode resultar em vazamento de contexto.

**Scope (P1):** `TurnContext` com `user_id`, sessão LLM por `(chat_id, thread_id, user_id)`, memória pessoal por `user_id` (`~/.aurelia/users/<id>/`), `USER.md` per-user (IDENTITY/SOUL continuam globais), cron jobs filtrados por `owner_user_id`, onboarding conversacional via Telegram para users novos, comando CLI `aurelia migrate-multi-user` idempotente para migrar deployment existente.

**Scope (P2):** comandos `/users` e `/forget-me`.

**Por que primeiro entre as próximas:** Plan Mode e Auto-Skills ambos precisam de `user_id` propagado e de paths per-user (planning state é por user; skills são privadas por user). Construir essas duas sem isolamento pronto causa retrofit caro.

**Packages:** `internal/users/` (novo), `internal/persona/`, `internal/dream/`, `internal/cron/`, `internal/pipeline/`, `cmd/aurelia/`

---

### 3. Plan Mode Architecture

**Spec:** `.specs/features/plan-mode-architecture/`
**Status:** A implementar
**Tamanho:** ~17 tasks
**Depende de:** User Isolation (precisa de `user_id` no `pipeline.Input` e nas chaves de state)

**Problem:** Hoje a heurística `looksLikePlanningIntent` injeta um prompt de TLC quando keywords gatilho aparecem, mas Plan Mode não é um modo — é um prompt que evapora na próxima mensagem. Sem estado persistente, sem discovery do contexto do projeto, sem rastreabilidade do que foi materializado.

**Scope (P1):** estado de Plan Mode em SQLite (`planning_state` por chat × thread × user), context discovery automático (CLAUDE.md, AGENTS.md, padrões TLC/RFC/ADR, stack), `BuildPlanningPrompt` informa LLM sobre o contexto e oferece opções de materialização — **Go não força layout**, LLM decide via tools do PI. Observer captura paths de Write/Edit em `materialized`. Handoff pro executor via `aurelia-plan` (mecânica existente) deleta o state. Resume nativo após restart.

**Scope (P2):** comandos `/plan`, `/plan status`, `/plan list`, `/cancel`.

**Scope (P3):** materialização sob demanda ("salva isso como spec agora").

**Por que depois de User Isolation:** State persistente é por user. Sem isso, ou todos os users compartilham planning_state (vazamento), ou degrade pra single-user (retrofit depois).

**Packages:** `internal/orchestrator/planning/` (novo), `internal/pipeline/`, `internal/telegram/`

---

### 4. Auto-Skills

**Spec:** `.specs/features/auto-skills/`
**Status:** A implementar
**Tamanho:** ~17 tasks
**Depende de:** User Isolation (skills privadas por user em `~/.aurelia/users/<id>/skills/`)

**Problem:** Tarefas complexas bem-sucedidas viram conhecimento perdido. Hermes mostrou que skill auto-creation é uma das features mais valorizadas — o agente "cresce com você". Aurelia tem o esqueleto (agent registry, `~/.aurelia/agents/`) mas não captura skills automaticamente.

**Scope (P1):** captura manual explícita (`/skill save <slug>`) do último turn/execução bem-sucedido, geração via LLM auto-resumo (`BuildSkillCapturePrompt` + bloco `aurelia-skill`), storage privado em `~/.aurelia/users/<id>/skills/<slug>.md`, registry per-user (skills sobrescrevem agents globais), comandos básicos `/skills`.

**Scope (P2):** detector heurístico de tarefa complexa (N+ tool calls OR duração OR diversidade de tools) e oferta com botões inline no Telegram após turn candidato, atrás de config para evitar ruído.

**Scope (P3):** tuning fino dos thresholds e UX de ofertas.

**Out of scope desta spec:** auto-improvement de skills (Hermes faz; aqui MVP é capture), skill sharing entre users, versioning.

**Por que por último:** depende de User Isolation (paths) e ganha mais valor se Plan Mode estiver entregue (planos viram skills mais ricas).

**Packages:** `internal/skills/` (novo), `internal/agents/`, `internal/pipeline/`, `internal/telegram/`

---

## Sequenciamento e Dependências

```
Done foundation
     │
     ▼
1. Agent Orchestration — Execution   ──→  fecha trabalho já 80% feito
     │
     ▼
2. User Isolation                    ──→  fundação pra 3 e 4
     │
     ├──────────────────────────────┐
     ▼                              ▼
3. Plan Mode Architecture       4. Auto-Skills
                                    ↑
                  (ganha mais valor se 3 estiver pronto, mas não bloqueia)
```

Ordem de entrega proposta:

1. **Agent Orchestration — Execution** — 1 semana, fecha gaps que já se manifestam
2. **User Isolation** — 1-2 semanas, mexe em várias camadas mas com fases pequenas e plano claro de migração
3. **Plan Mode Architecture** — 1 semana, depende de (2)
4. **Auto-Skills** — 1 semana, depende de (2); melhor se (3) estiver pronto

## Backlog

_(Vazio — features futuras serão adicionadas quando emergirem.)_

## Notas de visão

O Aurelia ocupa o nicho de **personal agent persistente via Telegram**, estilo Hermes Agent (Nous Research) e OpenClaw. Não é IDE, não é coding agent in-editor — é um agente que vive em background no seu deployment (laptop ou VPS), aceita tarefas via mensagem, executa, e mantém memória entre sessões. PI SDK é o motor de inferência; Go é o orquestrador, o observador, e a camada de persistência.

A evolução proposta aqui mantém essa identidade: nada de UI web, nada de multi-tenant, nada de coding-only. “User Isolation” é apenas isolamento de dados entre `user_id`s autorizados, não uma virada para SaaS. A capacidade de planejamento e auto-skills se aplica tanto a tarefas de desenvolvimento quanto a outras (pesquisa, organização, operações) — a LLM decide o tipo de tarefa com base no contexto, Go fornece a infraestrutura.
