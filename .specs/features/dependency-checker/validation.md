# Dependency Checker — Validation

**Spec**: `.specs/features/dependency-checker/spec.md`

---

## Automated Tests

```bash
# Unit tests — version parsing, comparison, check logic
go test ./internal/deps/ -v

# Integration tests — onboarding UI
go test ./cmd/aurelia/ -short -v
```

## Manual Tests

### Scenario 1: Clean Install (all deps present)

1. Run `go run ./cmd/aurelia setup`
2. **Expected**: Step 1/12 shows all deps with `[ok]` in green
3. Press Enter → advances to Step 2 (LLM Provider)

### Scenario 2: Missing Node.js

1. Temporarily rename/remove `node` from PATH
2. Run `go run ./cmd/aurelia setup`
3. **Expected**: Step 1 shows `[!!] Node.js — not found` in red with install URL
4. Enter is blocked, message says to install Node.js
5. Run `go run ./cmd/aurelia` (boot)
6. **Expected**: Fatal error: "Node.js is required but not found..."

### Scenario 3: Old Node.js (< 18)

1. If possible, install Node 16 temporarily
2. Run `go run ./cmd/aurelia setup`
3. **Expected**: `[!!] Node.js v16.x.x (requires >= 18.0.0)` in red

### Scenario 4: Missing git (optional)

1. Temporarily rename/remove `git` from PATH
2. Run `go run ./cmd/aurelia setup`
3. **Expected**: `[--] git — not found (optional)` in yellow
4. Enter works — can advance to next step
5. Run `go run ./cmd/aurelia` (boot)
6. **Expected**: Warning logged, bot starts normally

### Scenario 5: Non-TUI mode

1. Run `echo "" | go run ./cmd/aurelia setup`
2. **Expected**: Text-only checklist printed (no ANSI colors)

### Scenario 6: Performance

1. Time the check: `time go run ./cmd/aurelia setup` (cancel after step 1)
2. **Expected**: Step 1 renders in < 2s
