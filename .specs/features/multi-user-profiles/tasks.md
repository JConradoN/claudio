# User Isolation — Tasks

**Design:** `.specs/features/multi-user-profiles/design.md`  
**Roadmap step:** 1 — Sprint A  
**Status:** ✅ Implementado + runtime hardening auditado (2026-05-22)  
**Prioridade:** P0 foundation  
**Desbloqueia:** Orchestration, Plan Mode, Memory/Wiki, Nudge, Comms, Skills

---

## Escopo fechado neste sprint

User Isolation cobre isolamento de identidade, sessão PI, comandos de sessão, estado ativo de execução, cron ownership, persona/user memory base e onboarding para múltiplos usuários autorizados no mesmo deployment pessoal.

A regra atual é:

```text
Session/runtime state = SessionKey{chat_id, thread_id, user_id}
Conversation/project binding = ConversationKey{chat_id, thread_id}
```

Ou seja, `/cwd` e topic binding continuam compartilhados por conversa/tópico. Sessões PI, cancelamento, `/stop`, `/status`, reset, active commands do Bridge e persona/memória pessoal usam `user_id`.

---

## Evidência da auditoria 2026-05-22

Comandos usados para validar o estado real da codebase:

```bash
rg "sessions\.(Get|Set|ClearSession|Deactivate|GetWithState)\(" internal --glob '!**/*_test.go'
rg "CancelAllForUser|scopedAbortRequest|GetSessionWithState|ClearSessionForUser|DeactivateSession" internal bridge/index.ts
rg "Test.*(User|Session|Isolation|CancelAll|Scoped|ConversationKey)" internal
```

Resultado relevante:

- Nenhuma chamada runtime legacy de sessão PI foi encontrada em `internal/` fora de testes.
- `session.SessionKey` inclui `UserID`; `session.ConversationKey` não inclui `UserID` por decisão arquitetural.
- `pipeline.Service.activeSessions` usa chave `chatID:threadID:userID`.
- `Cancel`, `WorkStatus`, `CancelAllForUser` e `scopedAbortRequest` propagam `UserID` para o Bridge.
- Bridge `chatKey(chatID, threadID, userID)` é usado em `query`, `steer`, `follow-up`, `abort` e `get-state`.
- Reset de sessão e reset por troca de modelo usam `ClearSessionForUser`.
- Retry pós-bridge-death, timeout, empty-result e continuity session-id patch usam `GetSession`/`DeactivateSession` com `userID`.
- Testes multi-user existem para store, active sessions, reset/model reset, nudge buffer, prompt/persona e comandos.

---

## Task status auditado

| Task antiga | Status real | Nota |
|---|:---:|---|
| T0a — `TurnContext`, `SessionKey`, `ConversationKey` | ✅ | `internal/pipeline/turn_context.go`, `internal/session/store.go` |
| T0b — Pipeline signatures com contexto/user | ✅ | Pipeline recebe e propaga `userID`; `/cwd` continua conversation-scoped |
| T0c — Dream/cron/session/active-run scoped keys | ✅ | Active sessions e nudge são user-scoped; cron é owner-scoped |
| T1 — `internal/users/profile.go` | ✅ | `Profile` inclui `IsOwner` |
| T2 — `internal/users/resolver.go` | ✅ | Paths de usuário, memória, persona, projetos e skills |
| T3 — `internal/users/store.go` | ✅ | List/Get/Save/Delete/Exists |
| T4 — CLI `migrate-multi-user` skeleton | ✅ | Dispatcher em `cmd/aurelia/main.go` |
| T5 — Migration plan/executor | ✅ | Marker/lock, resume/force, app config, cron owner |
| T6 — Migration tests | ✅ | `cmd/aurelia/migrate_test.go` |
| T7 — Cron owner normalization/index | ✅ | `owner_user_id TEXT NOT NULL`, índice owner |
| T8 — Cron store owner-scoped methods | ✅ | `ListJobsByOwner`, `GetJobByOwnerAndID`, lifecycle scoped |
| T9 — Cron handlers/CLI/prompts por owner | ✅ | Owner explícito; runtime usa owner do job |
| T10 — `BuildPromptForUser` | ✅ | Persona global + `USER.md` per-user + owner docs condicionais |
| T11 — SessionKey/memory cache callers per-user | ✅ | Sessões user-scoped; user memory por path; CWD conversation-scoped |
| T12 — Dream/nudge em user dir | ✅ | `AfterTurn`, `AfterTurnNudge`, `FlushNudge` recebem `userID` |
| T13 — Project memory paths per-user | ➡️ Movido | Fechado como Sprint D: User-Scoped Project Memory |
| T13b — Logging estruturado com `TurnContext` | ✅ | Runtime logs incluem `user_id` nos caminhos críticos |
| T14 — Pipeline.Service + Users deps | ✅ | Pipeline usa profile, resolver e persona per-user |
| T15 — Onboarder + SQLite state | ✅ | `internal/users/onboarder.go`, `onboarding_store.go` |
| T16 — Telegram UserGate antes de comandos | ✅ | `internal/telegram/user_gate.go` |
| T17 — `/users` | ✅ | Owner-only |
| T18 — `/forgetme` | ✅ | Confirmação, cancelamento de runs e delete de profile |
| T18b — comandos globais owner-only | ✅ | `/model` protegido por owner |
| T19 — versão/changelog | ✅ | Já entregue em releases v0.11–v0.13 |
| T20 — validação final/smoke | ✅ histórico | Revalidar apenas quando mexer em runtime; esta limpeza altera docs |

---

## Gaps fora deste sprint

### 1. User-Scoped Project Memory — Sprint D

A memória pessoal por usuário existe, mas a memória privada de projeto ainda não está totalmente escopada como `(user_id, project_slug)` no runtime principal.

Evidência atual:

- `internal/users.Resolver.ProjectMemoryDir(userID, slug)` existe.
- `internal/runtime.PathResolver.ProjectMemoryDir(cwd)` e `ConversationProjectMemoryDir(cwd, chatID, threadID)` ainda são usados por pipeline/dream/memory UX.

Esse trabalho pertence a `.specs/features/project-memory/` e ao roadmap Sprint D.

### 2. Continuity store privado por usuário — decisão futura

Os patches de continuidade já usam o `session_file` do usuário correto, mas o estado de continuidade em si ainda é keyed por `continuity.ConversationKey{ChatID, ThreadID}`.

Isso preserva a semântica atual de continuidade por conversa/tópico. Se a continuidade precisar ser privada por usuário, abrir spec própria ou incluir em User-Scoped Project Memory/Wiki antes de Nudge profundo.

### 3. `userID=0` é compatibilidade, não caminho normal

Runtime Telegram deve passar por UserGate e chegar com usuário real. `userID=0` permanece para compatibilidade/testes/transição e não deve ser usado para criar novos fluxos multi-user.

---

## Guardrail para mudanças futuras

Antes de mexer em sessão, Bridge, comandos de sessão, continuidade ou active-run state, rodar:

```bash
rg "sessions\.(Get|Set|ClearSession|Deactivate|GetWithState)\(" internal --glob '!**/*_test.go'
go test ./internal/session/... ./internal/pipeline/... ./internal/telegram/... -short
```

Aceite esperado:

- nenhuma chamada runtime legacy de sessão PI;
- dois usuários no mesmo `chatID/threadID` não compartilham `session_file`, active run, reset, `/stop`, `/status`, Bridge `abort`, `steer`, `follow-up` ou `get-state`;
- CWD continua compartilhado por conversa/tópico até decisão contrária.
