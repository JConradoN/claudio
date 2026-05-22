# AGENTS.md

Instructions for coding agents working in this repository.

## Development Commands

```bash
go build ./...           # compile check
go test ./... -short     # fast tests
go test ./... -v         # full test suite
go vet ./...             # static analysis
```

Bridge rebuild (after modifying `bridge/index.ts`):

```bash
cd bridge && npm run build
cp bundle.js ../internal/bridge/bundle.js
```

Note: The `npm run build` script includes `--banner:js` with `createRequire` to support PI SDK dependencies that use dynamic `require()`. Do not remove it.

Explicit equivalent:
```bash
cd bridge && npx esbuild index.ts --bundle --platform=node --target=node18 --outfile=bundle.js --format=esm --banner:js="import { createRequire as __piCreateRequire } from 'module';const require = __piCreateRequire(import.meta.url);"
cp bundle.js ../internal/bridge/bundle.js
```

## Workflow

1. **Plan** — Understand the problem, break into atomic tasks
2. **Review** — Question the plan before executing
3. **Execute** — One atomic task at a time, test-first
4. **Validate** — Run tests, verify completion criteria
5. **Commit** — Conventional Commits: `type(scope): description`
6. **Build & Restart daemon** — Rebuild binary and restart the daemon (see below)
7. **Test live** — Send a test message in Telegram, verify the change works end-to-end

For trivial tasks, implement directly and validate.

### Step 6: Build & Restart (mandatory after every commit)

After every commit that changes Go or Bridge code, the binary **must** be rebuilt and the daemon restarted before considering the work done. This prevents testing with a stale binary.

```bash
# Atomic build + restart via launchd (KeepAlive so launchd respawns automatically)
make deploy
```

This uses `make install` (build → `.new` → `mv` — never corrupts a running binary) followed by `launchctl kickstart -k` which sends SIGTERM and lets launchd restart the daemon with the new binary.

> **Fallback** (if service is not loaded): `make install` then manually kill + restart via the old sequence below.

**Failure to rebuild + restart will produce false negatives during testing.** Treat this as part of "done".

> **Pro tip:** A `post-commit` git hook is installed at `.git/hooks/post-commit` that runs `make deploy` automatically after every commit. If enabled, step 6 is automatic — just commit and the daemon updates itself.

## Rules

- Service layer for business logic — never in handlers or entrypoints
- Errors treated explicitly — no silent swallowing
- `context.Context` with timeout on external operations
- Secrets never in repository — use `~/.aurelia/config/app.json`
- Tests required before marking work complete
- No new dependencies without justification
- Prefer editing over rewriting
- Keep interfaces small
- Update docs when behavior changes

## Key Packages

| Package | Responsibility |
|---------|---------------|
| `cmd/aurelia/` | Entrypoint, wiring, onboarding |
| `internal/bridge/` | Go client for the TS Bridge process |
| `internal/agents/` | Agent registry (load markdown definitions) |
| `internal/session/` | PI session_file resume, cwd state, nudge buffers |
| `internal/persona/` | Identity files, prompt assembly |
| `internal/cron/` | Schedule store, scheduler, bridge-backed runtime |
| `internal/telegram/` | Telegram bot handlers |
| `internal/config/` | Config loading and validation |
| `internal/runtime/` | Instance and project path resolution |
| `bridge/` | TypeScript Bridge (PI SDK adapter) |
| `pkg/stt/` | Speech-to-text |

## Versioning & Changelog

Every change that goes into `main` **must** bump the version and update
`CHANGELOG.md`. The version bump (patch/minor/major) and changelog entry
**must be approved by Igor before committing** — propose the bump and
entry text, wait for confirmation, then commit.

## Lessons Learned

Historical lessons from prior implementations live in `.opencode/lessons/learned/`. Check `lessons/index.md` before implementing changes in related areas.

**Critical patterns from the 2026-05-20 code review remediation:**

- **Goroutine recovery**: Every background goroutine launched by a package must have `defer recover()` at the top. If it panics, the daemon dies or leaks state. See `goroutine-recovery-mandatory.md`.
- **Redaction before truncation**: Always redact secrets (`redactSecrets`, escaping) **before** truncating/slicing data. A secret sliced in half evades regex detection. See `redaction-before-truncation.md`.
- **Path traversal**: `filepath.Base("..")` returns `".."`. Never rely on `Base` alone for untrusted input — use `os.CreateTemp` for temp files and store original names as metadata only. See `filepath-base-traversal.md`.
- **Post-implementation review**: Self-review + passing build is not sufficient. After non-trivial changes, trigger specialized reviewers (security + backend) with an explicit validation checklist. See `post-impl-review-gaps.md`.

## Reference

- Architecture and codebase details: `.specs/codebase/`
- Project vision and roadmap: `.specs/project/`
- Lessons learned index: `.opencode/lessons/index.md`
