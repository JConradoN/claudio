# Persistent Project Binding — Tasks

**Design:** `.specs/features/project-binding/design.md`  
**Status:** Draft

---

## Execution Plan

### Phase 0: Store and contracts

```text
T0 → T1
```

### Phase 1: `/cwd` persistence and resolution

```text
T1 → T2 → T3
```

### Phase 2: Pipeline/orchestration integration

```text
T3 → T4 → T5
```

### Phase 3: Validation and docs

```text
T5 → T6
```

---

## Task Breakdown

### T0: Add `ConversationKey` and project binding types

**What:** Define conversation-scoped key and binding models.  
**Where:** `internal/projectbinding/store.go` or shared conversation package  
**Depends on:** None

**Done when:**
- [ ] `ConversationKey{ChatID, ThreadID}` exists outside user-scoped session concepts
- [ ] `ProjectBinding`, `BindingSource`, `ResolvedBinding` defined
- [ ] project slug field included
- [ ] tests cover key equality/string formatting if implemented

**Verify:**
```bash
go test ./internal/projectbinding/... -run TestConversationKey -v
```

---

### T1: Implement SQLite `ProjectBindingStore`

**What:** Persist bindings with upsert, get, resolve, delete and touch.  
**Where:** `internal/projectbinding/store_sqlite.go`  
**Depends on:** T0

**Done when:**
- [ ] table is created automatically
- [ ] `Set` upserts by `(chat_id, thread_id)`
- [ ] `Resolve` returns topic binding before group fallback
- [ ] `Delete` removes only specified key
- [ ] `Touch` updates `last_used_at`
- [ ] no TTL/GC deletes bindings
- [ ] tests cover restart by reopening DB

**Verify:**
```bash
go test ./internal/projectbinding/... -v
```

---

### T2: Update `/cwd` command

**What:** Make manual `/cwd` persistent and add clear operations.  
**Where:** `internal/telegram/bot_middleware.go`  
**Depends on:** T1

**Done when:**
- [ ] `/cwd <path>` validates existing directory, resolves absolute path and persists binding
- [ ] `/cwd` displays topic/group/agent resolution with persistence wording
- [ ] `/cwd clear` deletes current topic/private binding
- [ ] `/cwd clear --group` deletes group binding
- [ ] clear does not delete memory directories
- [ ] tests cover set/show/clear/fallback

**Verify:**
```bash
go test ./internal/telegram/... -run "Test.*Cwd|Test.*ProjectBinding" -v
```

---

### T3: Decouple session GC/reset from cwd

**What:** Ensure session cleanup never removes project bindings.  
**Where:** `internal/session/store.go`, callers in commands/pipeline  
**Depends on:** T2

**Done when:**
- [ ] `ClearSession` only clears session
- [ ] `/new` preserves binding
- [ ] `/model` reset preserves binding
- [ ] session `GC` does not affect project binding store
- [ ] transitional cwd maps are removed or no longer source of truth
- [ ] tests cover restart/reset/GC preserving binding

**Verify:**
```bash
go test ./internal/session/... ./internal/telegram/... -run "Test.*Clear|Test.*GC|Test.*Cwd" -v
```

---

### T4: Use project binding in pipeline effective cwd

**What:** Resolve cwd from agent override → topic binding → group binding.  
**Where:** `internal/pipeline/prompt_builder.go`, `internal/pipeline/pipeline.go`  
**Depends on:** T3

**Done when:**
- [ ] bridge request uses persisted binding after daemon restart
- [ ] prompt working directory section reflects persisted binding
- [ ] topic/group fallback works in pipeline
- [ ] missing/deleted path produces clear warning/chat mode behavior
- [ ] auto-detect no longer silently persists

**Verify:**
```bash
go test ./internal/pipeline/... -run "Test.*Cwd|Test.*ProjectBinding|Test.*AutoDetect" -v
```

---

### T5: Wire Plan Mode, Orchestration and Memory bootstrap

**What:** Ensure dependent features use binding store.  
**Where:** `internal/telegram/orchestration.go`, planning code when added, runtime memory bootstrap  
**Depends on:** T4

**Done when:**
- [ ] execution handoff uses persisted effective cwd
- [ ] Plan Mode requires persisted/effective cwd and reports binding source
- [ ] setting binding bootstraps project/team memory
- [ ] memory cache invalidates on binding change
- [ ] nudge/project-memory specs are referenced in docs/tests where applicable

**Verify:**
```bash
go test ./internal/telegram/... ./internal/pipeline/... -run "Test.*ExecuteApprovedPlan|Test.*Memory.*Cwd" -v
```

---

### T6: Full validation and docs

**What:** Validate and document persistent `/cwd`.  
**Where:** `README.md`, changelog after approval  
**Depends on:** T5

**Done when:**
- [ ] README says `/cwd` persists until changed/cleared
- [ ] validation commands pass
- [ ] changelog/version bump proposed to Igor before commit

**Verify:**
```bash
go build ./...
go vet ./...
go test ./... -short
go test ./... -v
```

---

## MVP Definition of Done

- [ ] Manual `/cwd` persists across restart
- [ ] `/new`, `/model`, session TTL do not clear it
- [ ] Topic override and group inheritance remain intact
- [ ] Auto-detect does not silently persist
- [ ] Dependent roadmap specs can rely on stable effective cwd
