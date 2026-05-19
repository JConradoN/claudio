# Security Guard-Rails — Tasks

Baseado no design: `.specs/features/security-guard-rails/design.md`
Dependency graph: 1→2→3→4→5→6→7→8→9→10→11→12→13→14

---

## Task 1: `internal/security/profiles.go`

**Done When:**
- [x] `CapabilityProfile` type with 5 constants (observe, read_only, edit_project, execute_safe, privileged)
- [x] `ProfileTools(p)` returns correct tool allowlist per profile
- [x] `ResolveProfile()` intersects agent allowed_tools/disallowed_tools with profile limits
- [x] `DefaultProfileForContext()` returns correct profile based on cwd, isInternal, needsWrite
- [x] `go build ./...` passes
- [x] `go vet ./...` passes

---

## Task 2: `internal/security/policy.go`

**Done When:**
- [x] `PolicyMode` type with Warn and Block constants
- [x] `SecurityConfig` struct with all fields (Mode, DefaultProfile, AllowPrivilegedAgents, SensitivePathPatterns, AllowedOutsideCWDPaths, BashRules)
- [x] `BashPolicy` struct with rule booleans
- [x] `DefaultConfig()` returns safe defaults for Block mode
- [x] `EvaluateToolCall()` evaluates tool against policy and returns decision
- [x] `IsSensitivePath()` detects .env, ~/.ssh, ~/.aurelia/config/
- [x] `IsDestructiveCommand()` detects rm -rf /, sudo, chmod -R
- [x] `IsExfiltrationCommand()` detects curl/wget/nc + local file
- [x] `go build ./...` passes
- [x] `go vet ./...` passes

---

## Task 3: `internal/security/audit.go`

**Done When:**
- [x] `AuditEvent` struct with all required fields
- [x] `LogAudit()` writes structured JSON to stderr
- [x] Redaction of secrets in audit logs
- [x] `go build ./...` passes
- [x] `go vet ./...` passes

---

## Task 4: `internal/security/security_test.go`

**Done When:**
- [x] ProfileTools tests for each profile
- [x] ResolveProfile intersection tests
- [x] IsSensitivePath tests for .env, ~/.ssh, etc
- [x] IsDestructiveCommand tests
- [x] IsExfiltrationCommand tests
- [x] DefaultProfileForContext tests
- [x] EvaluateToolCall test cases (allow/block/rewrite)
- [x] AuditEvent redaction tests
- [x] `go test ./internal/security/... -v` passes (44/44)

---

## Task 5: `internal/bridge/protocol.go` — Add SecurityContext struct

**Done When:**
- [x] `SecurityContext` struct added with all fields
- [x] `Security` field added to `RequestOptions`
- [x] `go build ./...` passes
- [x] `go vet ./...` passes

---

## Task 6: `internal/agents/types.go` — Add CapabilityProfile

**Done When:**
- [x] `CapabilityProfile` field added to `Agent` struct
- [x] `IsReadOnly()` updated to consider capability_profile
- [x] Validation during agent load detects unknown profiles
- [x] `go build ./...` passes
- [x] `go vet ./...` passes

---

## Task 7: `bridge/index.ts` — Security hook

**Done When:**
- [x] `SecurityContext` interface added
- [x] `evaluateToolPolicy()` function implemented
- [x] `logAudit()` function implemented
- [x] Hook registration after `createPiSession()` — before first prompt
- [x] Block logic for bash (env, destructive, exfiltration)
- [x] Block logic for read/write/edit (sensitive paths, outside cwd)
- [x] Fail-closed: session creation fails if hook unavailable
- [x] `npm run build` passes (bundle.js)

---

## Task 8: `internal/pipeline/pipeline.go` — Resolve profile, pass security context

**Done When:**
- [x] `buildBridgeRequest()` resolves profile via `DefaultProfileForContext()`
- [x] Agent `CapabilityProfile` override applied
- [x] `ResolveProfile()` called to intersect allowed_tools
- [x] `SecurityContext` attached to request options
- [x] `getSecurityConfig()` method returns config from AppConfig or defaults
- [x] `go build ./...` passes
- [x] `go vet ./...` passes

---

## Task 9: `internal/pipeline/prompt_builder.go` — Security section in system prompt

**Done When:**
- [x] `buildSecurityPromptSection()` generates appropriate prompt for profile
- [x] Section inserted in `buildSystemPrompt()` between telegram and continuity
- [x] Profile not in system prompt for `observe` profile
- [x] Tests validate presence in relevant prompts
- [x] `go build ./...` passes
- [x] `go vet ./...` passes

---

## Task 10: `internal/config/config.go` — Add SecurityConfig

**Done When:**
- [x] `SecurityConfig` field added to `AppConfig`
- [x] Same field added to `fileConfig` and `defaultFileConfig`
- [x] Default values: `{ Mode: "block", DefaultProfile: "execute_safe", AllowPrivilegedAgents: false }`
- [x] `go build ./...` passes
- [x] `go vet ./...` passes

---

## Task 11: `internal/dream/dream.go` — Use edit_project profile

**Done When:**
- [x] Dream requests use `profile: "edit_project"` instead of manual tool lists
- [x] `Bash` not available in dream context
- [x] `go build ./...` passes
- [x] `go vet ./...` passes

---

## Task 12: `internal/dream/nudge.go` — Use edit_project profile

**Done When:**
- [x] Nudge requests use `profile: "edit_project"` instead of manual tool lists
- [x] `Bash` not available in nudge context
- [x] `go build ./...` passes
- [x] `go vet ./...` passes

---

## Task 13: `internal/orchestrator/defaults.go` — Add CapabilityProfile to workers

**Done When:**
- [x] `DefaultWorkerConfig` includes `CapabilityProfile: "execute_safe"`
- [x] Reviewer workers use `read_only`
- [x] Worker security context passed in execute
- [x] `go build ./...` passes
- [x] `go vet ./...` passes

---

## Task 14: Bridge rebuild + integration validation

**Done When:**
- [x] `cd bridge && npm run build` succeeds
- [x] Bundle copied to `internal/bridge/bundle.js`
- [x] `go build ./...` passes with new bundle
- [x] `go vet ./...` passes
- [x] `go test ./... -short` passes
- [x] Integration smoke test: agent with `execute_safe` can run `go test ./...`
- [x] Integration smoke test: `cat ~/.aurelia/config/app.json` is blocked
