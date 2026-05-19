# Continuity Engine — Tasks

**Status:** Implemented (MVP)

## Phase 1 — Store MVP ✅

- [x] Create `internal/continuity` package.
- [x] Define `ConversationState`, `ConversationKey`, `StatePatch`.
- [x] Implement SQLite store with restricted DB/WAL/SHM permissions.
- [x] Add upsert/get/patch tests.
- [x] Wire store in `cmd/aurelia/app.go`.

## Phase 2 — Pipeline updates ✅

- [x] Patch state after successful turn.
- [x] Patch state on bridge error.
- [x] Patch state on timeout.
- [x] Patch state on empty result after work.
- [x] Patch state before auto-reset/session clear.
- [x] Patch cwd/session id on system events where needed.
- [x] Add tests for auto-reset preserving continuity.

## Phase 3 — Prompt injection ✅

- [x] Add continuity formatter with redaction and rune-safe caps.
- [x] Add `buildContinuitySection` in prompt assembly.
- [x] Inject section before memory contents.
- [x] Add continuation detector if not already reusable.
- [x] Add prompt tests for restart, huge memory, timeout and “continua”.

## Phase 4 — Observability ⏳

- [ ] Add `ContextBudgetReport` types.
- [ ] Record section inclusion/skipping in logs.
- [ ] Surface continuity presence and latest checkpoint in `/status`.
- [ ] Add `/status` tests.

## Phase 5 — Later search ⏳

- [ ] Evaluate SQLite FTS5 for run/conversation recall.
- [ ] Index runlog checkpoint/tool/user summaries.
- [ ] Scope search by chat/thread and cwd.
- [ ] Add retrieval budget and tests.

## Definition of Done for MVP ✅

- [x] After successful turn, state is persisted.
- [x] After auto-reset, state survives and marks session cold.
- [x] After timeout/empty result, checkpoint/tools are persisted.
- [x] After service restart, prompt can include continuity from SQLite.
- [x] Huge global memory does not evict continuity block.
- [x] Secret-like content is redacted before persistence and prompt injection.
- [x] `go test ./... -short`, `go vet ./...`, `go build ./...` pass.
