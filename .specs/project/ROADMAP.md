# Roadmap

## Done / Validated Foundation

Estas features já foram implementadas ou têm validação registrada. Elas são base do roadmap atual, não entram como próximas fases principais.

| Feature | Spec | Status | Notes |
|---|---|---|---|
| Bridge Recovery | `.specs/features/bridge-recovery/` | Validated | Auto retry with session resume, cooldown after consecutive bridge failures |
| Command Layer | `.specs/features/command-layer/` | Done | Local interception of deterministic commands; avoids unnecessary LLM calls |
| Agent Tools Fix | `.specs/features/agent-tools-fix/` | Validated | `disallowed_tools` honored end-to-end; future governance moved to guard-rails |
| PI Resilience | `.specs/features/pi-resilience/` | Validated | Retry, fallback, circuit breaker, translated actionable errors |
| Dependency Checker | `.specs/features/dependency-checker/` | Validation plan exists | Pre-flight checks for Node/npm/git/gh |
| UX Polish | `.specs/features/ux-polish/` | Mostly validated | Ack, queue/status/progress polish, better errors and help |

---

## Current Evolution Track

Aurelia continua sendo um **personal agent persistente via Telegram**, com PI como motor de execução e Go como camada de orquestração, persistência, segurança e memória.

A próxima onda foca em tornar o sistema seguro e estável para trabalho autônomo em projetos reais:

1. isolar usuários autorizados;
2. tornar `/cwd` um vínculo persistente de conversa com projeto;
3. governar tools do PI sem quebrar o fluxo de coding;
4. reconstruir memória/nudge sobre escopos corretos;
5. só então ativar planejamento, execução, agent comms e auto-skills.

**Ordem é importante:** cada spec abaixo depende da anterior, técnica e conceitualmente.

---

## 1. User Isolation

**Spec:** `.specs/features/multi-user-profiles/`  
**Status:** A implementar  
**Prioridade:** P0 foundation

**Problem:** A whitelist já permite múltiplos `user_id`s, mas o runtime ainda mistura estado pessoal: sessões, memória, persona USER, cron, usage e comandos de controle.

**Scope mínimo recomendado primeiro:**

- `TurnContext` com `user_id`;
- `SessionKey{chat_id, thread_id, user_id}` para sessão LLM/usage/active run;
- `ConversationKey{chat_id, thread_id}` para `/cwd` e topic memory;
- `UserGate` antes de comandos/pipeline;
- USER/persona/memória pessoal por usuário;
- cron owner normalizado.

**Por que primeiro:** sem `user_id` propagado, Plan Mode, Auto-Skills, memória e nudge vazam estado entre usuários autorizados.

---

## 2. Persistent Project Binding

**Spec:** `.specs/features/project-binding/`  
**Status:** A implementar  
**Depende de:** User Isolation slice mínimo (`ConversationKey` separado de `SessionKey`)

**Problem:** hoje `/cwd` vive no `session.Store` em memória e pode sumir no restart ou GC. Mas `/cwd` representa “esta conversa trabalha neste projeto”, não uma sessão temporária.

**Scope:**

- persistir `/cwd` manual por `ConversationKey{chat_id, thread_id}`;
- manter fallback tópico → grupo;
- `/cwd clear` e `/cwd clear --group`;
- auto-detect não persiste sem confirmação;
- Plan Mode/Orchestration/Nudge/Memory usam binding persistente.

**Por que agora:** todas as features de projeto precisam de um `cwd` confiável e permanente.

---

## 3. Security Guard-Rails

**Spec:** `.specs/features/security-guard-rails/`  
**Status:** Draft revisado  
**Depende de:** Project Binding para `cwd` confiável

**Problem:** PI executa tools com privilégios do usuário local. Desligar `Bash` globalmente é restritivo demais; o correto é governar tools por contexto.

**Scope:**

- `CapabilityProfile`: `observe`, `read_only`, `edit_project`, `execute_safe`, `privileged`;
- PI `tool_call` hooks no bridge;
- policy para `bash`, `read`, `write`, `edit` e paths sensíveis;
- audit redigido;
- fail-closed se policy/hook não carregar;
- manter build/test/lint funcionando em `execute_safe`.

**Por que antes de autonomia:** Plan Mode, Orchestration, Nudge e Auto-Skills vão acionar tools; precisam de governança antes de escalar.

---

## 4. User-Scoped Project Memory

**Spec:** `.specs/features/project-memory/`  
**Status:** Draft revisado  
**Depende de:** User Isolation + Project Binding

**Problem:** a versão antiga de memória por projeto era global por `cwd`. Com mais de um usuário autorizado, isso mistura notas pessoais entre users.

**Scope:**

- user global memory;
- user × project private memory;
- project team memory por repositório/cwd;
- topic memory por `ConversationKey`;
- prompt assembly com camadas corretas;
- UX mínima de memória/checkpoints para fluxos longos;
- migration explícita do layout single-user.

**Por que antes do nudge:** nudge precisa saber onde salvar cada memória.

---

## 5. Learning Nudge — Scoped Memory Review

**Spec:** `.specs/features/learning-nudge/`  
**Status:** Draft revisado  
**Depende de:** User Isolation + Project Binding + Project Memory + Security Guard-Rails

**Problem:** extração por-turn/snippet perde contexto; nudge profundo precisa ser escopado para não vazar entre usuários/projetos.

**Scope:**

- transcript recorder por `SessionKey`;
- redaction antes de chamar PI;
- `CapabilityProfile=edit_project`, sem `Bash`;
- escrita nas camadas de memória corretas;
- sugestões de Auto-Skills, sem criar skills automaticamente.

**Por que aqui:** usa as camadas de memória e guard-rails já definidos.

---

## 6. Plan Mode Architecture

**Spec:** `.specs/features/plan-mode-architecture/`  
**Status:** Revised after code review + roadmap updates  
**Depende de:** User Isolation + Project Binding + Security Guard-Rails

**Problem:** hoje planejamento é heurístico e evapora; precisa virar modo explícito, persistente e retomável.

**Scope:**

- `/plan`, `/plan status`, `/plan cancel`, `/plan list`;
- state persistente por `SessionKey`;
- discovery baseado no project binding;
- materialização observada via Write/Edit;
- handoff seguro para executor;
- tasks podem declarar `capability_profile` e `peers`.

**Por que antes da execução:** produz o plano aprovado que a execução consome.

---

## 7. Agent Orchestration — Execution

**Spec:** `.specs/features/agent-orchestration-execution/`  
**Status:** Draft gap-closing; parte já existe em `internal/orchestrator/`  
**Depende de:** Project Binding + Security Guard-Rails; idealmente Plan Mode para produção dos artefatos

**Problem:** Aurelia já tem parte do executor, mas o ciclo não fecha com segurança: cwd errado, retry ausente, validação fraca, merge concorrente, commit/PR não chamados, staging amplo.

**Scope:**

- `ExecutionContext` com cwd persistente, thread/user/security context;
- git preflight;
- workers por wave;
- validation com diff/verify real;
- retry com feedback;
- merge serial;
- update `tasks.md`, commit seguro e PR opcional;
- manifest com security decisions e peer metadata.

**Por que depois do Plan Mode:** pode ser implementado em paralelo parcialmente, mas o fluxo completo depende de plano aprovado e cwd estável.

---

## 8. Agent Comms

**Spec:** `.specs/features/agent-comms/`  
**Status:** Draft  
**Depende de:** Agent Orchestration + Security Guard-Rails

**Problem:** workers especializados ganham qualidade quando podem consultar peers, mas isso precisa ser local, auditado e com limites.

**Scope:**

- Agent Bus local por run;
- peers explícitos por task;
- anti-loop/budget/timeouts;
- payload policy e audit;
- manifest com peer message counts;
- sem rede/cross-device no MVP.

**Por que depois da execução:** é melhoria da orquestração, não requisito para o primeiro executor seguro.

---

## 9. Auto-Skills

**Spec:** `.specs/features/auto-skills/`  
**Status:** Revised after code review + roadmap updates  
**Depende de:** User Isolation + Security Guard-Rails; ganha valor com Nudge, Plan Mode, Orchestration e Agent Comms

**Problem:** tarefas bem-sucedidas viram conhecimento perdido; Auto-Skills transforma procedimentos úteis em skills privadas, PI-compatible (`SKILL.md`), mas gerenciadas pelo Aurelia.

**Scope:**

- recorder de último turno bem-sucedido;
- `/skill save <slug>` explícito;
- geração via LLM sem tools;
- validação rígida de frontmatter Agent Skills/PI + adapter Aurelia;
- storage privado por user em layout `<slug>/SKILL.md`;
- `capability_profile` obrigatório/validado;
- registry per-user.

**Decisão:** seguir a Opção A — Aurelia-native, PI-compatible. Não usar `pi-hermes-memory` nem escrever em `~/.pi/agent` no MVP; o Aurelia mantém escopo, segurança e UX, reaproveitando apenas o formato/conceito de skills do PI.

**Por que por último:** depende de isolamento e segurança; fica muito mais valioso quando pode aprender com nudge/orchestration/agent-comms.

---

## Sequenciamento resumido

```text
Validated foundation
      │
      ▼
1. User Isolation
      │
      ▼
2. Persistent Project Binding
      │
      ▼
3. Security Guard-Rails
      │
      ▼
4. User-Scoped Project Memory
      │
      ▼
5. Learning Nudge
      │
      ▼
6. Plan Mode Architecture
      │
      ▼
7. Agent Orchestration — Execution
      │
      ▼
8. Agent Comms
      │
      ▼
9. Auto-Skills
```

## Nota de implementação incremental

`User Isolation` é grande. Não é necessário implementar tudo antes de seguir. O primeiro slice deve entregar apenas a base que desbloqueia o resto:

```text
TurnContext
SessionKey com user_id
ConversationKey sem user_id
UserGate básico
paths mínimos por user
```

Depois disso, `project-binding` já pode ser implementado e estabilizar o conceito de projeto/cwd para o restante do roadmap.

## Backlog futuro

- Cross-device Agent Comms seguro
- Human approval flow para guard-rails ambíguos
- OS sandbox para Bridge
- Project history/favorites para `/cwd`
- Team memory sync via git

## Notas de visão

Aurelia ocupa o nicho de **personal agent persistente via Telegram**. Não é IDE, não é SaaS multi-tenant, não é apenas coding agent. PI SDK é o motor de inferência/execução; Go é a camada de orquestração, segurança, memória, persistência e UX Telegram.
