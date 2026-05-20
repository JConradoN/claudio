# User Isolation — Tasks

**Design:** `.specs/features/multi-user-profiles/design.md`
**Roadmap step:** 1 — Sprint A
**Status:** 🔴 A implementar (0%)
**Depende de:** Nada (fundação do roadmap)
**Desbloqueia:** Todo o resto (Orchestration, Plan Mode, Memory, Wiki, Nudge, Comms, Skills)

---

## Execution Plan

### Phase 0: Scoped-key refactor (Decisões #2, #13, #15) — antes de tudo

Zero feature change. Introduz `TurnContext`, separa `SessionKey` de `ConversationKey`, e prepara usage/run state para user isolation sem mudar comportamento ainda. Garante que adicionar UserID em seguida não quebra `/cwd` compartilhado.

```
T0a → T0b → T0c
```

### Phase 1: Foundation (sequential)

Pacote `internal/users/` sem dependência de runtime ainda. Profile já com `IsOwner`.

```
T1 → T2 → T3
```

### Phase 2: Migration command (depends on Phase 1)

Comando CLI standalone — rodável antes de tocar no resto. Inclui mover `memory/topics/` → `topics/`, setar `default_owner_user_id` em app.json, setar `Profile.IsOwner=true` no target, e boot check de recovery.

```
T3 → T4 → T5 → T6
```

### Phase 3: Cron ownership (parallel-friendly após Phase 2)

```
T7 → T8 → T9
```

### Phase 4: Session isolation + Persona per-user (parallel com Phase 3)

```
T10 → T11
```

### Phase 5: Memory + dream + NudgeBuffer por SessionKey (depends Phase 4)

```
T11 → T12 → T13
```

### Phase 6: Logging + UserGate + Pipeline integration + onboarding (depends Phase 5)

```
T13 → T13b → T14 → T15 → T16
```

### Phase 7: Comandos extras + validação

```
T16 → T17 → T18 → T18b → T19 → T20
```

---

## Task Breakdown

### T0a: `TurnContext`, `SessionKey`, `ConversationKey` (Decisões #2, #13)

**What:** Definir `TurnContext{ChatID, ThreadID, UserID}` com helpers `SessionKey()`, `ConversationKey()` e `Logger()`. Definir `session.SessionKey` com `UserID` e `session.ConversationKey` sem `UserID`.
**Where:** `internal/pipeline/turn_context.go`, `internal/session/store.go`
**Depends on:** None
**Reuses:** `internal/session`, `log/slog`

**Done when:**
- [ ] Struct definida
- [ ] `SessionKey()` retorna `session.SessionKey{ChatID, ThreadID, UserID}`
- [ ] `ConversationKey()` retorna `session.ConversationKey{ChatID, ThreadID}`
- [ ] `Logger()` retorna `*slog.Logger` com `user_id`, `chat_id`, `thread_id` attrs
- [ ] Test: `TestTurnContext_SessionKey`, `TestTurnContext_ConversationKey`, `TestTurnContext_Logger`

**Verify:** `go test ./internal/pipeline/... -run TestTurnContext -v`

---

### T0b: Refactor pipeline signatures para receber `*TurnContext`

**What:** Substitui parâmetros `(chatID int64, threadID int)` em `pipeline.Service` por `*TurnContext`. Durante transição, call sites podem usar `UserID=0`, mas CWD já deve usar `ConversationKey` para não virar per-user acidentalmente.
**Where:** `internal/pipeline/service.go`, `pipeline.go`, `prompt_builder.go`, `memory_cache.go`, callers em `internal/telegram/`, `internal/session/store.go`
**Depends on:** T0a
**Reuses:** TurnContext

**Done when:**
- [ ] `Service.Run/Process/Cancel/WorkStatus/InvalidateMemoryDirs/...` aceitam `*TurnContext`
- [ ] Telegram callers constroem `&TurnContext{ChatID: c.Chat().ID, ThreadID: c.Message().ThreadID, UserID: 0}` (UserID será populado em T14)
- [ ] `session.Store` usa `SessionKey` para sessões e `ConversationKey` para `GetCwd/SetCwd`
- [ ] Herança de CWD tópico → grupo continua igual usando `ConversationKey{ChatID, 0}`
- [ ] Test: `TestConversationKey_CwdSharedAcrossUsers`
- [ ] Zero mudança de comportamento — `go test ./...` continua verde
- [ ] `go vet ./...` limpo

**Verify:** `go build ./... && go test ./... -short`

---

### T0c: Refactor dream/cron/tracker/run signatures para scoped keys

**What:** Mesma mudança em `internal/dream/`, `internal/cron/runtime.go`, `internal/session/tracker.go` e `internal/pipeline/run_supervisor.go`.
**Where:** `internal/dream/dream.go`, `nudge.go`, `internal/cron/runtime.go`, `internal/session/tracker.go`, `internal/pipeline/run_supervisor.go`
**Depends on:** T0a, T0b
**Reuses:** TurnContext, SessionKey, ConversationKey

**Done when:**
- [ ] `Dreamer.AfterTurnNudge`, `FlushNudge` aceitam `*TurnContext`
- [ ] Cron runtime expõe assinatura/inputs owner-aware; wiring real com `DefaultOwnerUserID` acontece em T9
- [ ] `session.Tracker` usa `SessionKey`, não `chatID`
- [ ] `runSupervisor` usa `SessionKey`, não `(chatID, threadID)`
- [ ] `runSupervisor.cancelByUser(userID)` disponível para `/forget-me`
- [ ] Tests: `TestTracker_PerUserUsage`, `TestRunSupervisor_PerUserIsolation`
- [ ] Tests existentes ainda passam

**Verify:** `go test ./internal/dream/... ./internal/cron/... ./internal/session/... ./internal/pipeline/... -v`

---

### T1: `internal/users/profile.go` — Profile struct + JSON I/O

**What:** Definir `Profile` (UserID, Name, Language, **IsOwner**, OnboardedAt, LastSeenAt) e métodos `Load(path)` / `Save(path)`.
**Where:** `internal/users/profile.go`
**Depends on:** None
**Reuses:** `encoding/json`, padrão de `internal/config/`

**Done when:**
- [ ] Struct com tags JSON, incluindo `IsOwner bool` (Decisão #8)
- [ ] `Load` retorna `(*Profile, error)`; `nil, nil` se arquivo não existe
- [ ] `Save` cria diretórios necessários
- [ ] Test: `TestProfile_Roundtrip` (cobre IsOwner)

**Verify:** `go test ./internal/users/... -run TestProfile -v`

---

### T2: `internal/users/resolver.go` — paths absolutos por user

**What:** `Resolver` struct com métodos `UserRoot/MemoryDir/PersonasDir/UserMdPath/ProfilePath/ProjectMemoryDir/SkillsDir/EnsureUserDir`, além de helper global `TopicsDir()` para `~/.aurelia/topics/`.
**Where:** `internal/users/resolver.go`
**Depends on:** None
**Reuses:** `path/filepath`, `internal/runtime`

**Done when:**
- [ ] Todos os métodos retornam paths absolutos consistentes
- [ ] `EnsureUserDir` cria estrutura básica (`memory/`, `personas/`, `projects/`, `skills/`)
- [ ] `TopicsDir()` retorna `~/.aurelia/topics/` e não fica sob `memory/`
- [ ] Test: `TestResolver_Paths` (table-driven)

**Verify:** `go test ./internal/users/... -run TestResolver -v`

---

### T3: `internal/users/store.go` — Store (List/Get/Save/Delete/Exists)

**What:** Operações em cima do Resolver. `Store` é o entry-point que o resto do código vai usar.
**Where:** `internal/users/store.go`
**Depends on:** T1, T2
**Reuses:** `os.ReadDir` pra `List`

**Done when:**
- [ ] `Get(userID)` lê `profile.json` ou retorna `nil, nil`
- [ ] `Save(p)` persiste
- [ ] `Exists(userID)` checa rapidamente (stat de `profile.json`)
- [ ] `List()` varre `~/.aurelia/users/` e retorna profiles válidos
- [ ] `Delete(userID)` remove `~/.aurelia/users/<id>/` recursivo
- [ ] Tests: `TestStore_*` cobrindo cada método

**Verify:** `go test ./internal/users/... -v`

---

### T4: `cmd/aurelia/migrate.go` — esqueleto do comando

**What:** Adicionar subcomando `migrate-multi-user` ao dispatcher principal. Suporta `--user-id` e `--dry-run`.
**Where:** `cmd/aurelia/migrate.go`, `cmd/aurelia/main.go`
**Depends on:** T3
**Reuses:** Padrão de `cron_cli.go` / `telegram_cli.go`

**Done when:**
- [ ] `aurelia migrate-multi-user --help` mostra flags
- [ ] Resolve `targetID` (flag, `default_owner_user_id`, ou primeiro da whitelist como fallback inicial)
- [ ] `config.AppConfig` e schema JSON incluem `DefaultOwnerUserID int64`
- [ ] Detecta marker `.multi-user-migrated` e aborta
- [ ] Detecta whitelist vazia e aborta com erro claro

**Verify:** `go build ./cmd/aurelia && ./aurelia migrate-multi-user --help`

---

### T5: Migration plan builder + executor

**What:** `buildMigrationPlan(home, targetID)` produz lista de `moveOp` + count de cron updates. `Execute()` faz two-phase moves. Inclui boot check de recovery (Decisão #10).
**Where:** `cmd/aurelia/migrate.go`, `cmd/aurelia/app.go` (boot check)
**Depends on:** T4
**Reuses:** `os`, `path/filepath`, `database/sql`

**Done when:**
- [ ] Lista todos os `.md` em `~/.aurelia/memory/` exceto `personas/IDENTITY.md`, `personas/SOUL.md`, `OWNER_PLAYBOOK.md`
- [ ] `personas/USER.md` mapeado pra `users/<id>/personas/USER.md`
- [ ] `~/.aurelia/memory/topics/` movido pra `~/.aurelia/topics/` (Decisão #6 — sai de memory/ pra deixar explícito que é global)
- [ ] `~/.aurelia/projects/*/` mapeado pra `users/<id>/projects/*/`
- [ ] Cron update: `UPDATE cron_jobs SET owner_user_id=? WHERE owner_user_id IS NULL OR owner_user_id='' OR owner_user_id='0'`
- [ ] `app.json` ganha `default_owner_user_id: <target_id>` (Decisão #7); idempotente se já presente
- [ ] `profile.json` do target gerado com `IsOwner: true` (Decisão #8)
- [ ] Two-phase move: copy → verify (hash ou size) → delete original
- [ ] Conflict detection: dst já existe → abort com lista
- [ ] Lock file `.multi-user-migrating` durante execução
- [ ] Marker `.multi-user-migrated` (JSON com timestamp + target_id + counts + `default_owner_user_id_set` + `schema_version`) ao final
- [ ] **Flags `--resume` e `--force`** (Decisão #10): `--resume` aceita arquivos já no destino como esperado; `--force` apaga lock+marker e refaz do zero
- [ ] **Boot check em app.go**: lock presente sem marker → recusa start com mensagem "rode --resume ou --force"

**Verify:** Manual em scratch `~/.aurelia/` simulado

---

### T6: Migration tests

**What:** Tests deterministicos usando `t.TempDir()` simulando `~/.aurelia/`.
**Where:** `cmd/aurelia/migrate_test.go`
**Depends on:** T5
**Reuses:** Padrão de tests existente

**Done when:**
- [ ] `TestMigration_DryRunListsAllOps`
- [ ] `TestMigration_AfterMigrationProfileExists` (cobre `IsOwner: true`)
- [ ] `TestMigration_IdempotentWithMarker`
- [ ] `TestMigration_ConflictsAbortCleanly`
- [ ] `TestMigration_CronOwnerIDPopulated`
- [ ] `TestMigration_EmptyWhitelistAborts`
- [ ] `TestMigration_TopicsMovedToGlobal` (Decisão #6 — `memory/topics/` → `topics/`)
- [ ] `TestMigration_DefaultOwnerIDPersistedInAppConfig` (Decisão #7)
- [ ] `TestMigration_RecoversFromInterruptedLock` (Decisão #10 — `--resume` ok, `--force` ok, boot recusa sem flag)

**Verify:** `go test ./cmd/aurelia/... -run TestMigration -v`

---

### T7: Cron owner normalization + index

**What:** Normalizar a coluna existente `owner_user_id TEXT NOT NULL` em `cron_jobs` + garantir índice. Não adicionar coluna `INTEGER` paralela. Nenhum job novo pode ser criado com owner vazio após migração.
**Where:** `internal/cron/store_sqlite.go` (ou `migrations.go`)
**Depends on:** None (mas idealmente roda após T5 pra marker estar definido)
**Reuses:** Padrão de DDL existente

**Done when:**
- [ ] DDL roda no boot e é idempotente
- [ ] Se a coluna não existir em DB legado, ela é criada como `TEXT NOT NULL DEFAULT ''`
- [ ] Index criado
- [ ] `CronJob.OwnerUserID string` continua sendo a fonte de verdade
- [ ] Migration CLI faz backfill de `NULL`/`''`/`'0'` para `DefaultOwnerUserID`
- [ ] Inserts novos exigem owner não vazio em runtime
- [ ] Test: `TestCronStore_CreateRejectsEmptyOwnerAfterMigration` ou equivalente no service

**Verify:** `go test ./internal/cron/... -v`

---

### T8: Cron store owner-scoped lifecycle methods

**What:** Métodos de filtragem e lifecycle por owner.
**Where:** `internal/cron/store.go`, `store_sqlite.go`
**Depends on:** T7
**Reuses:** Queries existentes

**Done when:**
- [ ] `ListByOwner(userID string) ([]CronJob, error)`
- [ ] `GetByOwnerAndID(userID string, jobID string) (*CronJob, error)` retorna `nil, nil` se não pertence
- [ ] `DeleteByOwnerAndID`, `PauseByOwnerAndID`, `ResumeByOwnerAndID` usam owner no WHERE/lookup
- [ ] Short-ID resolution é owner-scoped para evitar colisão/vazamento
- [ ] Tests: `TestCronStore_ListByOwnerFiltering`, `TestCronStore_GetByOwnerAndIDRejects`, `TestCronStore_DeleteByOwnerRejectsForeignJob`

**Verify:** `go test ./internal/cron/... -v`

---

### T9: Cron handlers, CLI e prompts filtram por owner

**What:** `/cron list`, `/cron cancel/pause/resume N`, cron CLI e prompt-injected CLI usam owner explícito.
**Where:** `internal/telegram/cron_handlers.go`, `internal/cron/handler.go` (se aplicável), `cmd/aurelia/cron_cli.go`, `internal/pipeline/prompt_builder.go`, `internal/cron/runtime.go`
**Depends on:** T8
**Reuses:** `TurnContext.UserID`, `job.OwnerUserID`, `DefaultOwnerUserID`

**Done when:**
- [ ] List filtra por owner
- [ ] Cancel/pause/resume rejeitam ID que não pertence ao sender
- [ ] Mensagem "não encontrado" preserva privacidade (não diz "esse pertence a outro user")
- [ ] Cron de agent automático recebe `cfg.DefaultOwnerUserID` (Decisão #7 — lê do app.json, **não** deriva de whitelist[0])
- [ ] `aurelia cron add|once|list|del|pause|resume` aceita `--owner-user-id`
- [ ] CLI sem `--owner-user-id` usa `DefaultOwnerUserID` ou rejeita com erro claro; nunca grava owner vazio
- [ ] Pipeline prompt injeta `--owner-user-id <TurnContext.UserID>`
- [ ] Cron runtime prompt injeta `--owner-user-id <job.OwnerUserID>` para follow-ups
- [ ] Tests: `TestCronCLI_InjectsOwnerUserID`, `TestCronRuntime_FollowupUsesJobOwner`

**Verify:** Test integrado com 2 users em SQLite real

---

### T10: `persona.BuildPromptForUser(userID, opts)`

**What:** Novo método que lê IDENTITY/SOUL global + USER.md per-user e injeta docs do owner apenas para owner.
**Where:** `internal/persona/canonical_service.go`
**Depends on:** T2, T3
**Reuses:** `persona.BuildPrompt()` existente como base

**Done when:**
- [ ] Lê `~/.aurelia/memory/personas/IDENTITY.md` e `SOUL.md` (global)
- [ ] Lê `~/.aurelia/users/<userID>/personas/USER.md` (per-user)
- [ ] USER.md ausente → injeta stub minimal (`# User\n\nUser id: <id>`)
- [ ] `OWNER_PLAYBOOK.md` e `LESSONS_LEARNED.md` só entram quando `opts.IsOwner=true`
- [ ] `BuildPrompt()` legacy continua funcionando, com log de deprecation
- [ ] Test: `TestPersona_BuildPromptForUser_PerUserUSERmd`
- [ ] Test: `TestPersona_BuildPromptForUser_OwnerDocsOnlyForOwner`

**Verify:** `go test ./internal/persona/... -v`

---

### T11: SessionKey + ConversationKey + memory cache callers per-user

**What:** `session.SessionKey` ganha `UserID` (Decisão #1), CWD permanece em `ConversationKey` (Decisão #13). `memoryCache` **não muda** (Decisão #5 — isolamento vem do path). Callers (`loadMemoryContents`, `InvalidateMemoryDirs`) usam `Resolver.MemoryDir(tc.UserID)` para escolher qual diretório.
**Where:** `internal/session/store.go`, `internal/pipeline/memory_cache.go` (sem mudança na struct), `internal/pipeline/prompt_builder.go` (callers)
**Depends on:** T2, T10
**Reuses:** Resolver

**Done when:**
- [ ] `session.SessionKey` ganha `UserID int64`
- [ ] Todos os usos de `SessionKeyFor()` recebem userID (default 0 onde TurnContext ainda não chegou — vai pra real em T14)
- [ ] `GetCwd/SetCwd` usam `ConversationKey`, não `SessionKey`
- [ ] Topic memory usa `Resolver.TopicsDir()` + `ConversationKey`
- [ ] `loadMemoryContents` aceita `*TurnContext`, resolve dir via `Resolver.MemoryDir(tc.UserID)`
- [ ] `InvalidateMemoryDirs` aceita `*TurnContext`
- [ ] `memoryCache` continua keyed por path (sem mudança)
- [ ] Tests existentes atualizados
- [ ] Test novo: `TestMemoryCache_PerUserIsolationViaPath` — chamadas com userID A e B resolvem para paths distintos, cada um com sua entrada de cache
- [ ] Test novo: `TestPipeline_GroupSessionsIsolated` (Decisão #1 + #12) — 2 users mesmo (chatID, threadID) → SessionKey diferente → Resume IDs distintos
- [ ] Test novo: `TestConversationKey_CwdSharedAcrossUsers`

**Verify:** `go test ./internal/session/... ./internal/pipeline/... -v`

---

### T12: Dream e nudge consolidam em user dir

**What:** `Dreamer.AfterTurn`, `FlushNudge` e `AfterTurnNudge` aceitam `TurnContext` e escrevem em `~/.aurelia/users/<id>/memory/`. Nudge buffers e running locks são por `SessionKey`/user, não globais.
**Where:** `internal/dream/dream.go`, `nudge.go`
**Depends on:** T2
**Reuses:** Fluxo existente, só muda o destino

**Done when:**
- [ ] Signature de `AfterTurn`, `FlushNudge` + `AfterTurnNudge` recebe `*TurnContext`
- [ ] Path resolvido via `Resolver.MemoryDir(userID)`
- [ ] Dream principal não roda mais sobre `~/.aurelia/memory/` após migração
- [ ] `turns`, `running` e `nudgeRunning` são por user/session, não globais
- [ ] Nudge prompt usa user memory dir + topic dir global + project dir per-user
- [ ] Tests existentes atualizados (fake user_id)
- [ ] Tests: `TestDreamer_PerUserMemoryDir`, `TestService_NudgeBufferPerSessionKey`

**Verify:** `go test ./internal/dream/... -v`

---

### T13: Project memory paths per-user

**What:** `internal/runtime` (ou wherever `BootstrapProjectMemory` está) usa `Resolver.ProjectMemoryDir(userID, slug)`.
**Where:** `internal/runtime/`, `internal/pipeline/memory_cache.go` (se aplicável)
**Depends on:** T2
**Reuses:** Lógica existente de slug derivation

**Done when:**
- [ ] BootstrapProjectMemory aceita userID
- [ ] Pasta resolvida pra `~/.aurelia/users/<id>/projects/<slug>/`
- [ ] Tests cobrem isolamento (A em projeto X ≠ B em projeto X)

**Verify:** `go test ./internal/runtime/... -v`

---

### T13b: Logging estruturado com `TurnContext`

**What:** Padronizar logs de entrada, pipeline, cron e dream com `user_id`, `chat_id`, `thread_id`.
**Where:** `internal/telegram/`, `internal/pipeline/`, `internal/dream/`, `internal/cron/`
**Depends on:** T0a
**Reuses:** `TurnContext.Logger()`, `slog`

**Done when:**
- [ ] Entry points Telegram criam logger com `tc.Logger()`
- [ ] Pipeline/run supervisor logam `user_id`, `chat_id`, `thread_id`, `run_id`
- [ ] Dream/nudge logam `user_id` e `session_key`
- [ ] Cron runtime loga `owner_user_id`
- [ ] Tests ajustados quando capturam output/logs

**Verify:** `go test ./internal/telegram/... ./internal/pipeline/... ./internal/dream/... ./internal/cron/... -short`

---

### T14: Pipeline.Service ganha `TurnContext` + dependências de Users

**What:** `Input` struct ganha `TurnContext`. `Service` recebe `users.Store`/ProfileReader e `users.Resolver`. Todos os call-sites de bridge/persona/cache/dream/cron prompt usam o userID. Onboarding fica no UserGate, não no pipeline.
**Where:** `internal/pipeline/service.go`, `pipeline.go`, `prompt_builder.go`
**Depends on:** T10, T11, T12, T13, T13b
**Reuses:** Constructor injection pattern

**Done when:**
- [ ] `Input` ou `Process` recebe `*TurnContext` validado
- [ ] `pipeline.Service` construtor recebe `*users.Store`/ProfileReader e `*users.Resolver`
- [ ] `buildSystemPrompt` chama `BuildPromptForUser(userID, opts)` com `IsOwner` do profile
- [ ] Memory cache + dream usam userID
- [ ] `buildCronInstructions` inclui `--owner-user-id <userID>`
- [ ] `go build ./...` clean
- [ ] Tests existentes do pipeline ainda passam

**Verify:** `go test ./internal/pipeline/... -v && go build ./...`

---

### T15: Onboarder + SQLite state

**What:** Estrutura `Onboarder` com `Begin`/`Step`/`Active`/`Cleanup`. Tabela `user_onboarding` em SQLite.
**Where:** `internal/users/onboarding.go`, `internal/users/onboarding_store.go`, schema migration
**Depends on:** T3
**Reuses:** SQLite handle já injetado no startup

**Done when:**
- [ ] DDL da tabela (idempotente)
- [ ] Begin armazena state, retorna saudação localizada (pt default)
- [ ] Step avança a state machine (`name` → `bio` → `done`; idioma é inferido no Begin)
- [ ] Done: cria `profile.json` + `USER.md` template + delete row
- [ ] Active checa state existente
- [ ] Cleanup TTL 24h
- [ ] Tests: `TestOnboarder_FullFlow`, `TestOnboarder_TTLCleanup`, `TestOnboarder_HandlesResume`

**Verify:** `go test ./internal/users/... -run TestOnboarder -v`

---

### T16: Telegram UserGate integra onboarding antes de comandos

**What:** Em `internal/telegram`, depois da whitelist e do bootstrap inicial do deployment, mas antes de `MatchCommand`:
- Se user não tem profile e não está em onboarding → `Onboarder.Begin`, envia saudação, retorna
- Se está em onboarding → `Onboarder.Step`, se done==false retorna; se done==true reenvia a mensagem original ao roteador normal com `TurnContext`
**Where:** `internal/telegram/user_gate.go`, `pipeline.go`, command handlers
**Depends on:** T14, T15
**Reuses:** `output.SendReply`

**Done when:**
- [ ] First-contact path testado
- [ ] Mid-onboarding step path testado
- [ ] Completion path: turno normal segue com profile fresh
- [ ] User novo enviando `/cron list` entra no onboarding, não no command router
- [ ] Commands passam a receber `*TurnContext` validado
- [ ] Tests: `TestPipeline_NewUserTriggersOnboarding`, `TestPipeline_MidOnboardingProcessesAsStep`
- [ ] Test: `TestUserGate_InterceptsCommandBeforeRouter`

**Verify:** `go test ./internal/telegram/... ./internal/pipeline/... -run 'Test.*(Onboarding|UserGate)' -v`

---

### T17: Comando `/users`

**What:** Handler Telegram que lista profiles autorizados (com nome, idioma, count de cron). Só permitido pra owner (`Profile.IsOwner` ou `app.json.default_owner_user_id`).
**Where:** `internal/telegram/commands.go` (ou handler dedicado)
**Depends on:** T3, T8
**Reuses:** `bc.cron.Store.ListByOwner`

**Done when:**
- [ ] Listing formatado em markdown
- [ ] Non-owner recebe "permissão negada"
- [ ] Test: `TestUsersCommand_OwnerOnly`

**Verify:** `go test ./internal/telegram/... -run TestUsersCommand -v`

---

### T18: Comando `/forget-me`

**What:** User apaga a si mesmo após confirmação inline.
**Where:** `internal/telegram/commands.go`
**Depends on:** T3, T7
**Reuses:** Botões inline (mesmo padrão de `/model`)

**Done when:**
- [ ] Manda mensagem com botões "Confirmar / Cancelar"
- [ ] Confirmar → marca user como deleting, `runSupervisor.cancelByUser(userID)`, aguarda drain até 30s, cancela cron do user e só então `users.Store.Delete(userID)`
- [ ] User é o único na whitelist → recusa com mensagem
- [ ] Próxima mensagem do user (já sem profile) → dispara onboarding (re-uso da T16)
- [ ] Test: `TestForgetMeCommand_FullFlow`
- [ ] Test: `TestForgetMe_CancelsActiveRunsBeforeDelete`

**Verify:** `go test ./internal/telegram/... -run TestForgetMe -v`

---

### T18b: Comandos globais owner-only

**What:** Proteger comandos que mutam configuração global do deployment.
**Where:** `internal/telegram/commands.go`, `bot_middleware.go`, command tests
**Depends on:** T3, T16
**Reuses:** `Profile.IsOwner`, `DefaultOwnerUserID`

**Done when:**
- [ ] `/model <x>` exige owner
- [ ] Qualquer comando futuro que altere `app.json` usa helper `requireOwner(tc)`
- [ ] Comandos informativos continuam liberados a users autorizados, com status/usage escopado por user
- [ ] Test: `TestModelCommand_OwnerOnly`

**Verify:** `go test ./internal/telegram/... -run TestModelCommand_OwnerOnly -v`

---

### T19: Bump de versão + CHANGELOG

**What:** Per CLAUDE.md, propor bump (minor — feature visível ao user) + entrada no CHANGELOG. Aguardar aprovação do Igor.
**Where:** `internal/version/version.go`, `CHANGELOG.md`
**Depends on:** T1-T18b

**Done when:**
- [ ] Proposta + entry submetidos
- [ ] Após aprovação: aplicados

---

### T20: Validação final + smoke

**What:** Build, vet, test, e smoke test manual no Telegram.
**Where:** projeto inteiro
**Depends on:** T19

**Done when:**
- [ ] `go build ./...` limpo
- [ ] `go vet ./...` limpo
- [ ] `go test ./... -v` todos green
- [ ] **Smoke 1**: você (1 user) roda `aurelia migrate-multi-user`, restart bot, conversa normal — comportamento idêntico
- [ ] **Smoke 2**: adicione um user_id fake na whitelist (fake = sua conta secundária), mande mensagem dele, onboarding completa, gere fato, valide isolamento
- [ ] **Smoke 3**: `/forget-me` no user fake, próxima mensagem dispara onboarding fresh
- [ ] **Smoke 4**: cron criado por user A não aparece pra user B

**Verify:** Manual + checklist

---

## Parallel Execution Map

```
Phase 1 (sequential):
  T1 → T2 → T3

Phase 2 (sequential, sai logo após Phase 1):
  T3 → T4 → T5 → T6

Phase 3 (paralela a Phase 4):
  T7 → T8 → T9

Phase 4 (paralela a Phase 3):
  T3 → T10 → T11

Phase 5 (sequencial após Phase 4):
  T11 → T12 → T13

Phase 6 (sequencial após Phase 5):
  T13 → T14 → T15 → T16

Phase 7 (sequencial):
  T16 → T17 → T18 → T18b → T19 → T20
```

---

## Task Granularity Check

| Task | Scope | Status |
|---|---|---|
| T1: Profile | 1 file, struct + 2 funcs | ✅ Granular |
| T2: Resolver | 1 file, ~8 methods | ✅ Granular |
| T3: Store | 1 file, 5 methods | ✅ Granular |
| T4: Migrate skeleton | 1-2 files, flag parsing | ✅ Granular |
| T5: Migrate plan + execute | 1 file, ~3 funcs | ⚠️ Médio — mas coeso |
| T6: Migrate tests | 1 file, 6 tests | ✅ Granular |
| T7: Cron schema | 1 DDL + struct field | ✅ Granular |
| T8: Cron store methods | 2 methods | ✅ Granular |
| T9: Cron handlers | 1 file changes | ✅ Granular |
| T10: BuildPromptForUser | 1 method | ✅ Granular |
| T11: Cache key | 1 struct + 3 method sigs | ✅ Granular |
| T12: Dream userID | sig change + path | ✅ Granular |
| T13: Project memory | 1-2 file changes | ✅ Granular |
| T13b: logging estruturado | log callsites | ✅ Granular |
| T14: Pipeline integration | 3 files, wiring | ⚠️ Médio — mas coeso |
| T15: Onboarder | 1 package, full state machine | ⚠️ Médio |
| T16: Pipeline onboarding | 1 modification | ✅ Granular |
| T17: /users command | 1 handler | ✅ Granular |
| T18: /forget-me | 1 handler com botões | ✅ Granular |
| T18b: comandos owner-only | 1 helper + /model | ✅ Granular |
| T19: Version/changelog | 2 files | ✅ Granular |
| T20: Final validation | suite | ✅ Granular |
