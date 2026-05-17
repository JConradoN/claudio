# User Isolation — Design

**Spec:** `.specs/features/multi-user-profiles/spec.md`
**Status:** Revised (decisões 1-17 aplicadas após review)

> Directory name remains `multi-user-profiles`, but this design should be read as **User Isolation**. The whitelist remains the auth boundary; this feature isolates runtime state for already-authorized Telegram `user_id`s.

---

## Architecture Overview

Mudança em quatro dimensões:

1. **Tipo `TurnContext`** — substitui o par `(chatID, threadID)` solto por uma struct `{ChatID, ThreadID, UserID}`. Refactor introdutório feito **antes** do resto, sem mudança de comportamento. Reduz risco de "esqueci de passar userID" em call sites futuros.
2. **Separação de chaves** — `SessionKey{chatID, threadID, userID}` isola sessão LLM, usage e active runs. `ConversationKey{chatID, threadID}` preserva `/cwd` e memória de tópico como estado compartilhado da conversa.
3. **Layout do filesystem** — `~/.aurelia/` ganha subdiretório `users/<user_id>/` que hospeda tudo que é pessoal. `~/.aurelia/memory/` passa a hospedar só personas globais. `~/.aurelia/topics/` é extraído de `memory/topics/` e fica explicitamente global (memória de tópicos de grupo é compartilhada).
4. **Propagação do `TurnContext` no código** — toda peça que hoje recebe `(chat_id, thread_id)` passa a receber `TurnContext` ou uma das chaves derivadas. Inclui session store, usage tracker, run supervisor, memory cache callers, dream, persona, cron store e Telegram commands.

Não há mudança de protocolo bridge (PI continua single-call por turn). Não há mudança de auth (whitelist continua sendo o gate). Mudança é **interna** ao Aurelia, com **uma exceção visível**: em grupos, cada user passa a ter sessão LLM independente (ver Decisão #1 abaixo).

```
~/.aurelia/                               ANTES                  DEPOIS
├── config/app.json                       (sem mudança)          + default_owner_user_id
├── data/cron.db                          (sem mudança)          schema migra (owner_user_id)
├── memory/
│   ├── personas/
│   │   ├── IDENTITY.md                   ✓ global               ✓ global (inalterado)
│   │   ├── SOUL.md                       ✓ global               ✓ global (inalterado)
│   │   └── USER.md                       ✗ era único            → migra pra user dir
│   ├── *.md (fatos diversos)             ✗ eram únicos          → migra pra user dir
│   ├── topics/                           ✗ aqui dentro          → MOVE pra ~/.aurelia/topics/
│   └── OWNER_PLAYBOOK.md                 ✓ global               ✓ global (inalterado)
├── topics/                               NOVO LOCAL             ✓ global (compartilhado em grupos)
│   └── chat_<id>/thread_<id>/*.md        (memória de tópico)
├── projects/<slug>/                      ✗ eram globais         → migra pra user dir
├── agents/                               ✓ global               ✓ global (compartilhado)
├── bridge/                               (sem mudança)
├── users/                                NOVO
│   └── <user_id>/
│       ├── profile.json                  inclui is_owner
│       ├── personas/USER.md
│       ├── memory/                       fatos pessoais
│       ├── projects/<slug>/              project memory pessoal
│       └── skills/                       (placeholder; auto-skills spec)
├── .multi-user-migrating                 NOVO (lock durante migração)
└── .multi-user-migrated                  NOVO (marker final, com JSON de metadata)
```

### Key Decisions (resumo do review)

| # | Decisão | Resumo |
|---|---------|--------|
| 1 | **SessionKey inclui userID** | Em grupos cada user tem sessão LLM independente. Breaking change documentada no CHANGELOG |
| 2 | **TurnContext type** | `{ChatID, ThreadID, UserID}` substitui args soltos. Refactor Phase 0 (antes do resto) |
| 3 | **/forget-me drain antes de delete** | Cancel todos os runs do user → wait → delete pasta |
| 4 | **NudgeBuffer por SessionKey** | `map[SessionKey]*NudgeBuffer` em `pipeline.Service`; não mistura users nem tópicos |
| 5 | **Memory cache continua path-keyed** | Sem mudança na struct. Isolamento vem do path (`users/A/memory/` ≠ `users/B/memory/`) |
| 6 | **`memory/topics/` move pra `~/.aurelia/topics/`** | Topic memory é per-conversation, fica global. Migração move |
| 7 | **DefaultOwnerUserID persistido em app.json** | Não derivado de whitelist[0] — gravado uma vez, nunca recomputado |
| 8 | **Profile.IsOwner bool** | Campo explícito. Migration seta true no target; novos onboards setam false |
| 9 | **Onboarding infere idioma da 1ª mensagem** | Não pergunta — heurística simples, armazena como `language: "pt"` ou `"en"` |
| 10 | **Migration recovery loud** | Boot detecta `.multi-user-migrating` sem `.multi-user-migrated` → recusa start, sugere `--resume` ou `--force` |
| 11 | **`slog.With("user_id", uid, "chat_id", cid)`** | Padrão em todo o pipeline. Logger via context |
| 12 | **Testes adicionais de isolamento** | Concorrência onboarding, race `/forget-me`, grupo com 2 users, migração parcial, NudgeBuffer/usage/run isolation |
| 13 | **ConversationKey separada** | `/cwd` e topic memory continuam por `(chatID, threadID)`, não por user |
| 14 | **UserGate antes de comandos** | User sem profile passa por onboarding antes de `/cron`, `/model`, `/cwd` ou pipeline |
| 15 | **Usage/run supervisor por user** | `/usage`, `/new`, cancel/status e fila usam `SessionKey` |
| 16 | **Cron CLI owner-aware** | Prompt injeta `--owner-user-id`; follow-ups herdam `job.OwnerUserID`; nenhum job novo fica ownerless |
| 17 | **Owner docs owner-only** | `OWNER_PLAYBOOK.md` e docs pessoais do owner só entram no prompt quando `Profile.IsOwner=true` |

---

## Component Changes

### 0. **Phase 0** — `TurnContext`, `SessionKey` e `ConversationKey`

**Location:** `internal/pipeline/turn_context.go` (novo)

**Responsibility:** Carrega a tripla `(ChatID, ThreadID, UserID)` como invariante e deixa explícito qual estado é pessoal e qual estado pertence à conversa.

Não usar `SessionKey` para tudo. O código atual usa a chave de sessão também para `cwd`; isso é correto no mundo single-user, mas vira bug se `UserID` for adicionado sem separar o escopo.

```go
// TurnContext identifies the chat, thread, and user a single turn belongs to.
// All pipeline-level functions accept *TurnContext instead of separate IDs to
// make the invariant compiler-enforced — you can't construct one without all
// three fields.
type TurnContext struct {
    ChatID   int64
    ThreadID int   // 0 for private chats / non-forum groups
    UserID   int64 // Telegram sender ID; 0 means "not set" and must be rejected
}

func (tc *TurnContext) SessionKey() session.SessionKey {
    return session.SessionKey{ChatID: tc.ChatID, ThreadID: tc.ThreadID, UserID: tc.UserID}
}

func (tc *TurnContext) ConversationKey() session.ConversationKey {
    return session.ConversationKey{ChatID: tc.ChatID, ThreadID: tc.ThreadID}
}

func (tc *TurnContext) Logger() *slog.Logger {
    return slog.Default().With("user_id", tc.UserID, "chat_id", tc.ChatID, "thread_id", tc.ThreadID)
}
```

`internal/session/store.go`:

```go
type SessionKey struct {
    ChatID   int64
    ThreadID int
    UserID   int64
}

type ConversationKey struct {
    ChatID   int64
    ThreadID int
}
```

**Key ownership:**
- PI resume session: `SessionKey`
- usage tracker: `SessionKey`
- active run + queued message: `SessionKey`
- `/new`, `/usage`, status/cancel: `SessionKey`
- `/cwd`: `ConversationKey`
- topic memory path: `ConversationKey`
- group/topic CWD inheritance: `ConversationKey` with fallback from `(chatID, threadID)` to `(chatID, 0)`

**Refactor sequence (Phase 0, no behavior change):**
- Introduce type with `UserID=0` everywhere callsites don't have one yet (transition phase).
- Update `pipeline.Service` methods to accept `*TurnContext`.
- Update session/cache/dream/tracker/run-supervisor signatures to accept `TurnContext`, `SessionKey`, or `ConversationKey` according to the ownership above.
- Compile + tests stay green throughout (no behavior change yet).

Only after Phase 0 lands do we enable real `UserID` propagation in Phase 1+.

### 1. Session key change — **breaking change in grupos**

**Location:** `internal/session/store.go`

```go
type SessionKey struct {
    ChatID   int64
    ThreadID int
    UserID   int64 // NEW — required for multi-user isolation
}
```

**Impact:** Em chats privados (1 user por chat), zero mudança. Em grupos do Telegram com ≥ 2 users autorizados, **cada user passa a ter sessão LLM independente** (PI Resume, conversation history, persona injection). Memória escrita em `~/.aurelia/topics/chat_<id>/thread_<id>/` continua compartilhada (é markdown legível por todos os participantes do tópico).

**Important:** `cwds` and `cwdSeen` in `session.Store` move to `map[ConversationKey]...`, not `map[SessionKey]...`. This preserves current `/cwd` behavior: a topic has one working directory shared by authorized users. Only session IDs and active/inactive state use `SessionKey`.

**Migration note:** sessões existentes em memória (in-process Map) não persistem entre restarts — só `cron_jobs` precisa de schema migration. O upgrade do binário "limpa" sessões antigas naturalmente.

### 1b. Usage tracker and run supervisor use `SessionKey`

**Location:** `internal/session/tracker.go`, `internal/pipeline/run_supervisor.go`

Current code keys token usage by `chatID` and active runs by `(chatID, threadID)`. After User Isolation:

```go
type UsageTracker interface {
    RecordUsage(key session.SessionKey, ...)
    Get(key session.SessionKey) Usage
    Clear(key session.SessionKey)
}

type runKey = session.SessionKey
```

`runSupervisor` also needs:

```go
func (rs *runSupervisor) cancelByUser(userID int64) int
```

This supports `/forget-me`: mark the profile as deleting, cancel all runs owned by that user, drain up to 30s, then delete user files.

`/usage`, `/new`, status queries, natural cancel messages, and queue replacement all use `TurnContext.SessionKey()`. A user in a group cannot observe or cancel another user's active run.

### 1c. UserGate before command routing

**Location:** `internal/telegram/user_gate.go` (new), `internal/telegram/pipeline.go`

Current order is:

```
pending deployment bootstrap → MatchCommand → pipeline
```

New order:

```
whitelist middleware
  → pending deployment bootstrap (existing IDENTITY/SOUL setup)
  → UserGate (profile/onboarding + TurnContext)
  → MatchCommand
  → pipeline
```

`UserGate` builds `TurnContext` from `c.Chat().ID`, `c.Message().ThreadID`, and `c.Sender().ID`; rejects `UserID=0`; checks `users.Store`; and either runs onboarding or passes the message onward with a validated context.

Commands should accept `*pipeline.TurnContext` or a lightweight Telegram-local equivalent. Do not let command handlers call `c.Sender().ID` ad hoc after this point; the gate is the source of truth.

### 2. New package `internal/users/`

**Location:** `internal/users/`

**Responsibility:** Tudo que diz respeito a profile de user — load, save, onboarding state, paths.

```go
// Profile is the persistent metadata about one authorized user.
type Profile struct {
    UserID       int64     `json:"user_id"`
    Name         string    `json:"name"`             // como o user pediu pra ser chamado
    Language     string    `json:"language"`         // "pt" | "en" — inferido da 1ª mensagem
    IsOwner      bool      `json:"is_owner"`         // true só pro owner do deployment
    OnboardedAt  time.Time `json:"onboarded_at"`
    LastSeenAt   time.Time `json:"last_seen_at"`
}

// Store loads and persists profiles.
type Store struct {
    root string // ~/.aurelia/users
}

func NewStore(root string) *Store

func (s *Store) Get(userID int64) (*Profile, error)
func (s *Store) Save(p *Profile) error
func (s *Store) Exists(userID int64) bool
func (s *Store) Delete(userID int64) error           // para /forget-me
func (s *Store) List() ([]*Profile, error)           // para /users

// Resolver returns absolute paths for a given user_id.
type Resolver struct {
    root string
}

func (r *Resolver) UserRoot(userID int64) string                     // ~/.aurelia/users/<id>/
func (r *Resolver) MemoryDir(userID int64) string                    // .../memory/
func (r *Resolver) PersonasDir(userID int64) string                  // .../personas/
func (r *Resolver) UserMdPath(userID int64) string                   // .../personas/USER.md
func (r *Resolver) ProfilePath(userID int64) string                  // .../profile.json
func (r *Resolver) ProjectMemoryDir(userID int64, slug string) string
func (r *Resolver) SkillsDir(userID int64) string                    // .../skills/ (auto-skills spec)
func (r *Resolver) EnsureUserDir(userID int64) error                 // mkdir -p
```

### 2. Onboarding (conversational, via Telegram)

**Location:** `internal/users/onboarding.go`

State persiste em SQLite (mesma DB do cron, schema novo):

```sql
CREATE TABLE IF NOT EXISTS user_onboarding (
    user_id     INTEGER PRIMARY KEY,
    chat_id     INTEGER NOT NULL,
    thread_id   INTEGER NOT NULL,
    step        TEXT NOT NULL,     -- "name" | "bio" | "done"
    name        TEXT,
    language    TEXT,              -- inferred at Begin(), stored alongside state
    bio         TEXT,
    first_msg   TEXT,              -- the original message that triggered onboarding
    started_at  INTEGER NOT NULL,
    updated_at  INTEGER NOT NULL
);
```

State machine (idioma é inferido no `Begin`, não perguntado):

```
(no row)        → Begin(): detect language from first msg → ask name → step=name
step=name       → user replies: store name, offer to share something about themselves → step=bio
step=bio        → user replies or "pular": store, finalize → step=done; delete row
```

**Language inference** (`detectLanguage`):
- Conta tokens portugueses comuns vs. ingleses (`o`, `a`, `de`, `que`, `não`, `sim`, `obrigado` vs. `the`, `is`, `and`, `you`, `thanks`, `please`).
- Empate ou texto < 3 palavras → default `pt` (o produto é primariamente PT-BR).
- Decisão fica gravada como `language: "pt"` ou `"en"` no profile; user pode editar USER.md depois pra mudar.

```go
type Onboarder struct {
    store     *Store
    onboarding OnboardingStore   // SQLite
    bot        TelegramSender   // sends/edit messages
}

// Begin returns the prompt to send and stores initial state.
func (o *Onboarder) Begin(userID, chatID int64, threadID int) (greeting string, err error)

// Step processes one user reply, returns next prompt + done flag.
func (o *Onboarder) Step(userID, chatID int64, threadID int, reply string) (next string, done bool, err error)

// Active returns true if user_id is mid-onboarding.
func (o *Onboarder) Active(userID int64) bool

// Cleanup expires onboarding state older than 24h.
func (o *Onboarder) Cleanup(ctx context.Context) error
```

Quando `done=true`, o `Onboarder`:
1. Cria `~/.aurelia/users/<id>/personas/USER.md` com template:
   ```markdown
   ---
   user_id: <id>
   name: <name>
   language: <lang>
   ---

   # User Profile

   <bio se houver, ou frase placeholder>
   ```
2. Salva `profile.json`
3. Deleta linha de `user_onboarding`
4. Sinaliza ao UserGate que a mensagem original pode voltar ao roteador normal

### 3. Pipeline integration

**Location:** `internal/pipeline/service.go`, `pipeline.go`, `prompt_builder.go`, `memory_cache.go`

Mudanças:

```go
type Service struct {
    // ... existing fields ...
    resolver  *users.Resolver
    profiles  users.ProfileReader
}

// Input já carrega TurnContext validado pelo UserGate.
type Input struct {
    Turn      *TurnContext
    MessageID int
    Text      string
    // ...
}
```

Onboarding não vive dentro do pipeline normal. Ele é interceptado no `UserGate`, antes de `MatchCommand`. Isso evita o bug em que user novo executa `/cron`, `/model` ou `/cwd` sem profile.

O pipeline pode assumir:
- `TurnContext.UserID != 0`
- profile existe, exceto em testes de transição
- comandos já receberam o mesmo contexto

```go
func (s *Service) Process(ctx *TurnContext, messageID int, text string, images []bridge.ImageAttachment) error
```

`prompt_builder.go`:

```go
func (s *Service) buildSystemPrompt(input Input, agent *agents.Agent) string {
    sections := []string{}

    // Persona: IDENTITY + SOUL (global) + USER (per-user)
    if s.persona != nil {
        profile, _ := s.profiles.Get(input.Turn.UserID)
        personaPrompt, _ := s.persona.BuildPromptForUser(input.Turn.UserID, persona.PromptOptions{
            IsOwner: profile != nil && profile.IsOwner,
        })
        sections = append(sections, personaPrompt)
    }

    // ... rest unchanged ...
}
```

`internal/persona/`:

```go
// New method that respects user_id.
func (p *CanonicalIdentityService) BuildPromptForUser(userID int64, opts PromptOptions) (string, error) {
    // Reads IDENTITY.md + SOUL.md from global memory dir.
    // Reads USER.md from ~/.aurelia/users/<user_id>/personas/USER.md.
    // Falls back to a stub if USER.md missing (shouldn't happen after onboarding).
    // Injects OWNER_PLAYBOOK.md / LESSONS_LEARNED.md only when opts.IsOwner.
}

// Old BuildPrompt() remains for back-compat during transition; emits a deprecation log.
```

`memory_cache.go` — **sem mudança na struct** (Decisão #5):

A `memoryCache` continua keyed só por **path**. O isolamento entre users vem naturalmente do fato de que `users/A/memory/` e `users/B/memory/` são diretórios diferentes — cada um tem sua própria entrada no cache. Adicionar `UserID` na chave criaria oportunidade de bug (mesmo path cacheado duas vezes sob IDs diferentes) sem benefício.

A mudança real é nos **callers**: `loadMemoryContents` e `InvalidateMemoryDirs` recebem `*TurnContext` para resolver qual diretório consultar (via `Resolver.MemoryDir(userID)`), depois delegam pro cache existente. Topic memory usa `Resolver.TopicsDir()` + `ConversationKey`, não `memoryDir/topics`.

`internal/dream/` — **NudgeBuffer por SessionKey** (Decisão #4 revisada):

```go
// pipeline.Service hospeda buffers por SessionKey em vez de um único compartilhado.
// Sem isso, turns de A acumulam no mesmo buffer que turns de B → quando o threshold
// dispara, o dream consolida facts misturados de ambos.
type Service struct {
    // ... existing ...
    nudgeBuffers   map[session.SessionKey]*session.NudgeBuffer
    nudgeBuffersMu sync.Mutex
}

func (s *Service) nudgeBufferFor(key session.SessionKey) *session.NudgeBuffer {
    s.nudgeBuffersMu.Lock()
    defer s.nudgeBuffersMu.Unlock()
    b, ok := s.nudgeBuffers[key]
    if !ok {
        b = session.NewNudgeBuffer()
        s.nudgeBuffers[key] = b
    }
    return b
}

// Dreamer signatures take TurnContext to resolve the correct user dir for writes.
func (d *Dreamer) FlushNudge(tc *TurnContext, cwd string, buffer *NudgeBuffer) error
func (d *Dreamer) AfterTurnNudge(tc *TurnContext, cwd string, buffer *NudgeBuffer)
```

Dream consolidation also becomes per-user:

```go
func (d *Dreamer) AfterTurn(tc *TurnContext)
func (d *Dreamer) runUserDream(userID int64, memoryDir string)
```

Implementation notes:
- `turns` is no longer a single `atomic.Int32`; use a small `map[int64]*dreamState` guarded by mutex or an equivalent concurrency-safe structure.
- `running` and `nudgeRunning` are keyed by user/session, not global. A nudge for user A must not block a nudge for user B.
- Dream lock files live inside the user memory dir (`users/<id>/memory/.dream.lock`), not in global `memory/`.
- Nudge prompt `GlobalDir` is renamed conceptually to `UserMemoryDir`; it points to `users/<id>/memory/`.
- Topic dir remains global under `~/.aurelia/topics/...`, and project dir resolves to `users/<id>/projects/<slug>/...`.
- No automated consolidation should run over `~/.aurelia/memory/` after migration; that directory contains global identity/deployment docs, not personal memory.

### 4. Cron ownership normalization

**Location:** `internal/cron/store_sqlite.go`, `internal/cron/migrations.go` (existe? confirmar — senão criar)

Current code already creates `cron_jobs.owner_user_id TEXT NOT NULL`. Older deployments or intermediate branches may have `NULL`, empty string, or placeholder values. Do **not** add a second integer owner column. Normalize the existing column and add/ensure an index.

```sql
CREATE INDEX IF NOT EXISTS idx_cron_jobs_owner ON cron_jobs(owner_user_id);
```

`cron.Store` interface ganha métodos:

```go
type Store interface {
    // ... existing methods ...

    // ListByOwner returns active cron jobs for a specific user.
    ListByOwner(userID string) ([]Job, error)

    // GetByOwnerAndID returns a job only if owner_user_id matches.
    GetByOwnerAndID(userID string, jobID string) (*Job, error)

    DeleteByOwnerAndID(userID string, jobID string) error
    PauseByOwnerAndID(userID string, jobID string) error
    ResumeByOwnerAndID(userID string, jobID string) error
}
```

`internal/telegram/cron_handlers.go`:

```go
// Filter listing and lifecycle actions by TurnContext.UserID.
func (bc *BotController) handleCronCommand(tc *pipeline.TurnContext, c telebot.Context) error {
    senderID := strconv.FormatInt(tc.UserID, 10)
    // ... call store.ListByOwner(senderID) instead of List()
}
```

Cron criado por agent automático (`agent.Schedule != ""`):

```go
// In cron registration, set owner_user_id to default owner.
ownerID := bc.config.DefaultOwnerUserID  // persisted; migration sets it once
```

Cron CLI becomes owner-aware:

```bash
aurelia cron add "<cron-expr>" "<prompt>" --chat-id <chat_id> --owner-user-id <user_id>
aurelia cron once "<timestamp>" "<prompt>" --chat-id <chat_id> --owner-user-id <user_id>
aurelia cron list --owner-user-id <user_id>
aurelia cron del <id> --owner-user-id <user_id>
```

Rules:
- Prompt-injected CLI commands **always** include `--owner-user-id`.
- `BridgeCronRuntime.buildCronInstructions(job)` includes the original `job.OwnerUserID` for follow-up jobs.
- Direct CLI without `--owner-user-id` may use `DefaultOwnerUserID` for operator ergonomics, but never writes an empty owner.
- `/cron pause`, `/cron resume`, `/cron del`, natural cancel, and CLI lifecycle commands must resolve the short id under the owner scope. If not found, return a generic "not found".
- Cron runtime builds persona/memory for `job.OwnerUserID`, not the global `USER.md`.

### 5. CLI migration command

**Location:** `cmd/aurelia/migrate.go` (new)

```go
func runMigrateMultiUser(args []string) error {
    var targetID int64
    var dryRun bool
    fs := flag.NewFlagSet("migrate-multi-user", flag.ExitOnError)
    fs.Int64Var(&targetID, "user-id", 0, "target user_id (default: first in whitelist)")
    fs.BoolVar(&dryRun, "dry-run", false, "print actions without changing state")
    fs.Parse(args)

    cfg := loadConfig()
    if targetID == 0 {
        if len(cfg.TelegramAllowedUserIDs) == 0 {
            return errors.New("configure TelegramAllowedUserIDs first")
        }
        targetID = cfg.TelegramAllowedUserIDs[0]
    }

    home, _ := runtime.AureliaHome()
    if _, err := os.Stat(filepath.Join(home, ".multi-user-migrated")); err == nil {
        return errors.New("already migrated (marker present)")
    }

    plan := buildMigrationPlan(home, targetID)
    if dryRun {
        plan.Print(os.Stdout)
        return nil
    }
    return plan.Execute()
}

type migrationPlan struct {
    targetID int64
    moves    []moveOp     // src → dst
    cronUpdates int       // how many rows
}
```

Operações de move são **two-phase** (copy + verify + delete original) — não usar `os.Rename` direto, porque pode estar entre filesystems (raro no `~/.aurelia/` mas seguro).

**Detalhamento do que se move:**

| Origem | Destino |
|---|---|
| `~/.aurelia/memory/*.md` (exceto `personas/IDENTITY.md`, `personas/SOUL.md`, `OWNER_PLAYBOOK.md`, `MEMORY.md` global) | `~/.aurelia/users/<id>/memory/` |
| `~/.aurelia/memory/personas/USER.md` | `~/.aurelia/users/<id>/personas/USER.md` |
| `~/.aurelia/memory/topics/` (Decisão #6 — fica global, sai de `memory/`) | `~/.aurelia/topics/` |
| `~/.aurelia/projects/*/` | `~/.aurelia/users/<id>/projects/*/` |
| Profile do target ganha `IsOwner: true` (Decisão #8) | `~/.aurelia/users/<id>/profile.json` |
| `app.json` ganha `default_owner_user_id: <id>` (Decisão #7) | in-place |
| `UPDATE cron_jobs SET owner_user_id=<id> WHERE owner_user_id IS NULL OR owner_user_id='' OR owner_user_id='0'` | (in-place) |

Marker file final:
```json
{
  "migrated_at": "2026-05-15T10:30:00Z",
  "target_user_id": 12345,
  "items_moved": 47,
  "cron_updated": 3,
  "default_owner_user_id_set": true,
  "schema_version": 1
}
```

### 5b. Migration recovery (Decisão #10)

**Boot check** em `cmd/aurelia/app.go`, antes de inicializar pipeline:

```go
home := resolver.AureliaHome()
lockPath := filepath.Join(home, ".multi-user-migrating")
markerPath := filepath.Join(home, ".multi-user-migrated")

if _, err := os.Stat(lockPath); err == nil {
    if _, mErr := os.Stat(markerPath); mErr != nil {
        // Lock present, no marker → migration was interrupted.
        return fmt.Errorf("migration in inconsistent state: lock present at %s with no marker. "+
            "Run 'aurelia migrate-multi-user --resume' to retry safely, or "+
            "'aurelia migrate-multi-user --force' to discard partial state and start over",
            lockPath)
    }
    // Lock + marker means we crashed AFTER success but BEFORE removing lock — safe to clean.
    _ = os.Remove(lockPath)
}
```

**`--resume` flag:** re-roda o plan; arquivos já movidos no destino são detectados via `conflict check` mas como "esperado", não erro. Marker final só grava quando 100% dos moves succeed.

**`--force` flag:** apaga lock + marker (se houver) + recomeça do zero. Mais perigoso, requer flag explícita. Para casos de "perdi a paciência".

---

## Data Models

### Profile (new)

Já mostrado acima. Vive em `~/.aurelia/users/<id>/profile.json`.

### OnboardingState (new, SQLite)

Já mostrado acima.

### Cron Job (normalized)

```go
type Job struct {
    ID            int64
    OwnerUserID   string          // existing column; Telegram IDs are stored as decimal strings
    TargetChatID  int64
    // ... existing fields ...
}
```

### Session and conversation keys

```go
type SessionKey struct {
    ChatID   int64
    ThreadID int
    UserID   int64
}

type ConversationKey struct {
    ChatID   int64
    ThreadID int
}
```

`SessionKey` is for private user runtime state. `ConversationKey` is for shared topic/chat state.

### Pipeline Input (modified)

```go
type Input struct {
    Turn      *TurnContext
    // ... existing ...
}
```

### AppConfig (modified)

```go
type AppConfig struct {
    // ... existing ...
    DefaultOwnerUserID int64 `json:"default_owner_user_id,omitempty"`
}
```

Migration sets this once. Runtime must not recompute it from `TelegramAllowedUserIDs[0]`, because whitelist order is not ownership.

---

## Error Handling

| Cenário | Tratamento | UX |
|---|---|---|
| `user_id == 0` em qualquer ponto | Log error, reject | (silencioso — não deveria acontecer com middleware ativo) |
| User autorizado sem profile envia comando | UserGate intercepta antes de command router | Onboarding inicia; comando original roda só após profile |
| User autorizado mas profile.json corrompido | Log, force re-onboarding | "Vi que algo deu errado com seu perfil. Vamos começar de novo." |
| `users/<id>/` parcialmente criado | Tratado como ausente; onboarding refaz | (transparente) |
| Cron com owner_user_id NULL no listing | Não mostrar pra ninguém (segurança); log warning | (silencioso) |
| Cron CLI sem owner após migração | Usar `DefaultOwnerUserID` ou rejeitar; nunca owner vazio | "Informe --owner-user-id ou configure default_owner_user_id." |
| Non-owner tenta `/model` | Rejeitar | "Permissão negada." |
| `/forget-me` quando user é o único | Recusar | "Você é o único user configurado. Use o comando CLI pra resetar." |
| `/forget-me` com runs ativos (Decisão #3) | Setar flag "deleting" → cancel todos os runs do user (todas as threads) → drain até 30s → delete pasta + DB rows | "Limpando seus dados... ✅ Pronto. Próxima mensagem inicia do zero." |
| Owner playbook para non-owner | Não injetar `OWNER_PLAYBOOK.md`/lessons | (transparente) |
| Migration: arquivo de mesmo nome em ambos os lados | Parar, listar conflitos, pedir resolução manual | "Conflito em foo.md — existe em memory/ e em users/<id>/memory/. Resolva e rode de novo." |
| Migration: marker presente | Abortar | "Já migrado em 2026-05-15. Use --force pra ignorar (perigoso)." |
| **Migration: lock + sem marker no boot** (Decisão #10) | Recusar start do daemon | "Migration interrompida. Rode 'aurelia migrate-multi-user --resume' ou '--force'." |
| Onboarding: user reseta sessão no meio | Mantém state em SQLite até 24h | (continua de onde parou) |
| Onboarding: user some por 24h | TTL limpa state | Próxima mensagem dispara onboarding do zero |
| Onboarding concorrente de 2 users novos | SQLite PK `(user_id)` previne race | (transparente; cada um tem sua row) |

---

## Tech Decisions

| Decisão | Escolha | Justificativa |
|---|---|---|
| Onde mora `users/` | `~/.aurelia/users/<id>/` | Padrão já estabelecido pelo project-memory; previsível |
| Profile = JSON ou SQLite | JSON | Inspeção fácil, edição manual, baixo throughput, ergonomia melhor que SQL |
| Onboarding state = JSON ou SQLite | **SQLite** | Curto-prazo, frequente, precisa lock, TTL — combina com SQLite |
| IDENTITY/SOUL globais | Sim | Aurelia é uma só. Compartilhar evita drift e inconsistência |
| USER.md por user | Sim, obrigatório | "Quem é o user" é por definição pessoal |
| `owner_user_id` type | **TEXT/string** | O schema atual já usa `TEXT NOT NULL`; Telegram IDs são comparados como decimal strings. Evita ALTER perigoso e dupla fonte de verdade |
| `owner_user_id` ausente/vazio/legado | Migration preenche com `DefaultOwnerUserID`; listings escondem valores inválidos até migration | Compat ergonômico; jobs sem owner confiável não aparecem após migração (segurança) |
| Migration command vs auto-migrate | **CLI explícito** (você pediu) | Mais controlado, dry-run possível, sem surpresa no boot |
| Two-phase move (copy+verify+delete) | Sim | Mais seguro que `os.Rename`; permite rollback em caso de falha |
| Lock file durante migração | Sim, `.multi-user-migrating` (vs marker final `.multi-user-migrated`) | Previne duas migrações concorrentes |
| `BuildPromptForUser` é método novo | Sim, mantém `BuildPrompt()` deprecated por 1 release | Soft transition; quebra só quando todos os callers migrarem |
| Cache key inclui user_id | **NÃO** (Decisão #5) | Isolamento vem do path (`users/A/memory/` ≠ `users/B/memory/`); adicionar UserID na key cria duplicação |
| Onboarding pode ser pulado | Não nesta spec | Onboarding é leve (2 perguntas após Decisão #9); pulável vira complexidade desnecessária |
| Idioma é perguntado ou inferido | **Inferido** (Decisão #9) | Heurística simples acerta >95%; 1 pergunta a menos; user corrige editando USER.md |
| Default owner pra cron de agent | **`AppConfig.DefaultOwnerUserID` persistido** (Decisão #7) | Derivar de `whitelist[0]` muda se whitelist for reordenada; persistir explicitamente é estável |
| `IsOwner` no Profile | **Sim, campo explícito** (Decisão #8) | Conceito "owner" aparece em 3+ lugares; convenção implícita ("primeiro da whitelist") é frágil |
| SessionKey em grupos | **`(chatID, threadID, userID)`** (Decisão #1) | Sem isolamento de sessão, multi-user em grupo é incoerente. Breaking change deliberada vs. v0.6.x |
| ConversationKey | **`(chatID, threadID)` sem user** (Decisão #13) | `/cwd` e topic memory são estado da conversa/tópico, não da pessoa |
| Usage tracker / run supervisor | **Por `SessionKey`** (Decisão #15) | `/new`, `/usage`, status/cancel e fila não podem cruzar users |
| NudgeBuffer compartilhado vs por SessionKey | **Por `SessionKey`** (Decisão #4 revisada) | Buffer único mistura facts de A e B; buffer só por user ainda mistura tópicos |
| TurnContext type | **Sim, Phase 0** (Decisão #2) | Tripla `(chatID, threadID, userID)` precisa ser compiler-enforced; refactor antes do resto |
| UserGate | **Antes do command router** (Decisão #14) | Onboarding no pipeline não intercepta `/cron`, `/model` nem `/cwd` |
| Log estruturado com user_id | **Sim, padrão** (Decisão #11) | `slog.With("user_id", uid, "chat_id", cid)` no entry do pipeline; debug 30s vs 30min |
| Migration recovery de lock | **Recusa loud, requer flag** (Decisão #10) | Silent retry pode causar double-move; operador decide explicitamente |
| `memory/topics/` → onde mora | **`~/.aurelia/topics/` global** (Decisão #6) | Topic memory é per-conversation, não per-user; em grupos faz sentido compartilhar |
| Cron CLI owner | **`--owner-user-id` obrigatório nos prompts** (Decisão #16) | A LLM cria cron por CLI; sem owner flag o job nasce órfão |
| Owner docs | **Owner-only** (Decisão #17) | `OWNER_PLAYBOOK.md` pode conter preferências pessoais do owner; non-owner só vê docs globais seguros |

---

## Testing Strategy

| Test | Where | Validates |
|---|---|---|
| `TestProfile_Roundtrip` | `internal/users/profile_test.go` | Save → Get devolve o mesmo Profile |
| `TestResolver_Paths` | `internal/users/resolver_test.go` | Todos os métodos retornam paths absolutos consistentes |
| `TestOnboarder_FullFlow` | `internal/users/onboarding_test.go` | Begin → 3 Steps → done flag + USER.md gerado |
| `TestOnboarder_TTLCleanup` | same | Estado > 24h é removido |
| `TestPersona_BuildPromptForUser_PerUserUSERmd` | `internal/persona/persona_test.go` | Dois users → dois USER.md diferentes; IDENTITY/SOUL iguais |
| `TestPipeline_NewUserTriggersOnboarding` | `internal/pipeline/pipeline_test.go` | Sem profile → primeira mensagem dispara Begin |
| `TestPipeline_MidOnboardingProcessesAsStep` | same | Onboarding em curso → mensagem vai pra Step, não pro bridge |
| `TestCronStore_ListByOwnerFiltering` | `internal/cron/store_test.go` | List por owner A não retorna jobs de B |
| `TestCronStore_GetByOwnerAndIDRejects` | same | Job de A não é acessível com owner_id=B |
| `TestMigration_DryRunListsAllOps` | `cmd/aurelia/migrate_test.go` | Plan inclui todos os arquivos + count cron |
| `TestMigration_IdempotentWithMarker` | same | Segunda run aborta com marker presente |
| `TestMigration_ConflictsAbortCleanly` | same | Arquivos duplicados → erro + estado inalterado |
| `TestMigration_AfterMigrationProfileExists` | same | profile.json e USER.md no destino correto |
| `TestEndToEnd_TwoUsersIsolation` | `e2e/multi_user_test.go` | User A grava fato; user B não enxerga |
| `TestEndToEnd_GroupWithMultipleUsers` | same | Em grupo, cada user mantém persona e **sessão LLM** independente; topic memory (`~/.aurelia/topics/`) é compartilhada |
| `TestPipeline_GroupSessionsIsolated` (Decisão #1) | `internal/pipeline/pipeline_test.go` | 2 users no mesmo `(chatID, threadID)` → `SessionKey` diferente → cada um vê só seu próprio Resume |
| `TestConversationKey_CwdSharedAcrossUsers` (Decisão #13) | `internal/session/store_test.go` | 2 users no mesmo tópico compartilham `/cwd` via `ConversationKey` |
| `TestTracker_PerUserUsage` (Decisão #15) | `internal/session/tracker_test.go` | Usage de A e B no mesmo tópico não se mistura |
| `TestRunSupervisor_PerUserIsolation` (Decisão #15) | `internal/pipeline/run_supervisor_test.go` | Cancel/status/fila de B não afeta run de A |
| `TestUserGate_InterceptsCommandBeforeRouter` (Decisão #14) | `internal/telegram/user_gate_test.go` | User novo envia `/cron list` → onboarding, não comando |
| `TestCronCLI_InjectsOwnerUserID` (Decisão #16) | `cmd/aurelia/cron_cli_test.go` | `add/once` nunca gravam owner vazio |
| `TestCronRuntime_FollowupUsesJobOwner` (Decisão #16) | `internal/cron/runtime_test.go` | Prompt de cron follow-up inclui `--owner-user-id job.OwnerUserID` |
| `TestModelCommand_OwnerOnly` | `internal/telegram/commands_test.go` | Non-owner não altera config global |
| `TestPersona_BuildPromptForUser_OwnerDocsOnlyForOwner` (Decisão #17) | `internal/persona/persona_test.go` | OWNER_PLAYBOOK não aparece no prompt de non-owner |
| `TestForgetMe_CancelsActiveRunsBeforeDelete` (Decisão #3) | `internal/telegram/commands_test.go` | Run em curso para A → `/forget-me` → run é cancelado antes de a pasta ser apagada (sem corrupção) |
| `TestService_NudgeBufferPerSessionKey` (Decisão #4) | `internal/pipeline/service_test.go` | A e B em turns/tópicos intercalados → buffers não se misturam; dreamer consolida só facts do dono daquele `SessionKey` |
| `TestOnboarder_ConcurrentNewUsers` (Decisão #12) | `internal/users/onboarding_test.go` | 2 users novos disparam Begin simultaneamente → cada um tem state row próprio sem race |
| `TestMigration_RecoversFromInterruptedLock` (Decisão #10) | `cmd/aurelia/migrate_test.go` | Lock + no marker → `--resume` completa sem perder arquivos já movidos |
| `TestOnboarder_DetectsLanguage` (Decisão #9) | `internal/users/onboarding_test.go` | "olá tudo bem" → `language: pt`; "hello how are you" → `language: en`; texto curto/ambíguo → `pt` default |

---

## Rollout

Ordem de implementação (a tasks.md detalha):

0. **Phase 0 — Scoped-key refactor** (Decisões #2, #13, #15): introduz `TurnContext`, `SessionKey` e `ConversationKey`; refatora signatures; mantém comportamento idêntico inicialmente. Garante que adicionar UserID não quebre `/cwd` compartilhado nem misture usage/run state.
1. **Fundação**: pacote `internal/users/` (`Profile` com `IsOwner`, `Resolver`, `Store`). Sem onboarding ainda.
2. **Migration**: comando `aurelia migrate-multi-user`. **Antes** de tocar no resto.
   - Inclui move de `memory/topics/` → `topics/` (Decisão #6).
   - Inclui set de `app.json.default_owner_user_id` (Decisão #7) e `Profile.IsOwner=true` no target (Decisão #8).
   - Inclui boot check de lock + marker recovery (Decisão #10).
   - Por quê primeiro: dá pra rodar `--dry-run` e verificar a estratégia antes de mudar código de runtime.
3. **Cron ownership**: normalização/backfill do `owner_user_id` existente + métodos owner-scoped no store + filtros nos handlers + `DefaultOwnerUserID` lido de app.json + CLI `--owner-user-id` + follow-ups herdando `job.OwnerUserID`.
4. **SessionKey + Persona per-user**: `SessionKey` ganha `UserID` (Decisão #1). `BuildPromptForUser` lê USER.md per-user e injeta owner docs só para `IsOwner`.
5. **Memory cache callers + dream + NudgeBuffer por SessionKey**: callers usam `Resolver.MemoryDir(userID)`. `NudgeBuffer` vira map por `SessionKey` em `pipeline.Service` (Decisão #4 revisada). Dream/nudge escrevem no user dir.
6. **slog estruturado** (Decisão #11): `logger := tc.Logger()` no entry do Telegram/pipeline, propaga via context.
7. **UserGate + Onboarding**: `Onboarder` com inferência de idioma (Decisão #9) + integração em `internal/telegram` antes de comandos/pipeline.
8. **Comandos extras e políticas globais**: `/users` (só `IsOwner`), `/forget-me` (com drain de runs ativos — Decisão #3), `/model` owner-only.
9. **Tests + smoke + bump**: inclui os 5 testes novos (Decisão #12).

Marcador-chave: depois da fase 2 (migration command), você pode rodar a migração no seu deployment, ficar no layout novo, e o resto do código continua funcionando porque eu mantenho `BuildPrompt()` legacy + `users.Resolver` opcional. Aí cada fase seguinte aciona o uso do novo layout um pacote por vez. Isso minimiza risco de "tudo quebrou de uma vez".

**Breaking change a comunicar no CHANGELOG**: a partir desta release, em grupos do Telegram com ≥ 2 users autorizados, cada user tem sessão LLM independente (cada um vê apenas seu próprio histórico de conversa). Memória escrita em `~/.aurelia/topics/chat_<id>/thread_<id>/` continua compartilhada. Quem usa 1 user só não nota diferença.
