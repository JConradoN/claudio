# Agent Comms — Tasks

**Design:** `.specs/features/agent-comms/design.md`  
**Status:** Draft

---

## Execution Plan

### Phase 0: Contracts and limits

Define plan schema, bus contracts, limits and policy boundaries.

```text
T0 → T1 → T2
```

### Phase 1: Local Agent Bus

Implement in-memory bus, peer permissions, send/respond/await and manifest events.

```text
T2 → T3 → T4
```

### Phase 2: Orchestrator integration

Wire Agent Bus into execution runs without changing merge/validation semantics.

```text
T4 → T5 → T6
```

### Phase 3: Worker protocol and UX

Expose peer comms to PI workers using prompt protocol first, then prepare future tool integration.

```text
T6 → T7 → T8
```

### Phase 4: Hardening and release validation

Add tests, docs, audit verification and ensure no network/cross-device behavior is introduced.

```text
T8 → T9
```

---

## Task Breakdown

### T0: Extend plan schema with explicit peers

**What:** Add optional `Task.Peers []string` and update plan prompt/schema docs.  
**Where:** `internal/orchestrator/plan.go`, `internal/orchestrator/prompt.go`  
**Depends on:** None

**Done when:**
- [ ] `Task` has `Peers []string` with JSON tag `json:"peers,omitempty"`
- [ ] `Task` supports optional `CapabilityProfile`/`capability_profile` from guard-rails without granting peer-specific tools
- [ ] plan parsing remains backward compatible when `peers` is absent
- [ ] `BuildExecutionPrompt` documents `peers` as opt-in by `task_id`
- [ ] invalid peer references are detectable before execution
- [ ] tests cover plan with and without peers

**Verify:**
```bash
go test ./internal/orchestrator/... -run "TestExtractPlan|TestBuildExecutionPrompt|TestPlanPeers" -v
```

---

### T1: Define `internal/agentcomms` core types and limits

**What:** Create core package with message, status, limits and errors.  
**Where:** `internal/agentcomms/message.go`, `internal/agentcomms/limits.go`  
**Depends on:** T0

**Done when:**
- [ ] `Message`, `MessageStatus`, `Peer`, `Receipt`, `Response` types exist
- [ ] `Limits` includes run/task message caps, hop cap, payload cap and await timeout
- [ ] default limits are conservative
- [ ] errors distinguish unauthorized peer, closed bus, timeout, payload too large and budget exceeded
- [ ] tests cover default limits and error classification

**Verify:**
```bash
go test ./internal/agentcomms/... -run "TestDefaultLimits|TestErrors" -v
```

---

### T2: Implement in-memory Agent Bus

**What:** Implement local run-scoped bus with peer permissions, send, respond and await.  
**Where:** `internal/agentcomms/bus.go`  
**Depends on:** T1

**Done when:**
- [ ] `NewBus(runID, permissions, limits, policy)` creates bus
- [ ] `ListPeers(taskID)` returns only authorized peers
- [ ] `Send(ctx, from, to, body)` validates peer, policy, payload and budgets
- [ ] `Respond(ctx, messageID, from, body)` only allows the addressed peer to answer
- [ ] `Await(ctx, messageID)` returns response, timeout or cancellation
- [ ] `Close()` rejects new sends and wakes pending awaits
- [ ] tests cover happy path, unauthorized peer, timeout, close and wrong responder

**Verify:**
```bash
go test ./internal/agentcomms/... -run TestBus -v
```

---

### T3: Add security policy and audit hooks

**What:** Add lightweight message policy and structured audit logging.  
**Where:** `internal/agentcomms/policy.go`, `internal/agentcomms/audit.go`  
**Depends on:** T2

**Done when:**
- [ ] payload policy rejects obvious secrets: token, password, secret, api_key, bearer, high-entropy strings
- [ ] policy rejects known sensitive paths: `.env`, `~/.ssh`, `~/.aurelia/config`, `~/.pi`, keychain references
- [ ] audit logs send/reject/respond/timeout with run/task metadata and payload size
- [ ] audit does not log full sensitive payload on rejection
- [ ] tests cover secret/path rejection and safe audit fields

**Verify:**
```bash
go test ./internal/agentcomms/... -run "TestPolicy|TestAudit" -v
```

---

### T4: Record peer events in `ExecutionManifest`

**What:** Add manifest event model for agent-to-agent messages.  
**Where:** `internal/orchestrator/manifest.go`, `internal/agentcomms/manifest.go`  
**Depends on:** T2

**Done when:**
- [ ] manifest can record peer event metadata
- [ ] event stores from/to/message/status/size/hash, not raw sensitive body
- [ ] bus emits callbacks or adapter events for manifest registration
- [ ] consolidation summary can count peer messages per run
- [ ] tests cover event recording and no raw secret persistence

**Verify:**
```bash
go test ./internal/orchestrator/... ./internal/agentcomms/... -run "Test.*Manifest|TestPeerEvent" -v
```

---

### T5: Wire Agent Bus into `ExecutePlan`

**What:** Create bus per execution run and pass peer messenger to worker execution.  
**Where:** `internal/orchestrator/execute.go`, `internal/orchestrator/orchestrator.go`  
**Depends on:** T4

**Done when:**
- [ ] `ExecutePlan` builds peer permissions from `Task.Peers`
- [ ] invalid peer references fail before workers start or are reported as preflight warning per design decision
- [ ] bus closes on successful finish, error and cancellation
- [ ] skipped/failed tasks cannot send peer messages after terminal state
- [ ] existing execution tests pass without peers
- [ ] new tests cover run-scoped peer bus lifecycle

**Verify:**
```bash
go test ./internal/orchestrator/... -run "TestExecutePlan|TestAgentComms" -v
```

---

### T6: Add worker prompt protocol for peer messages

**What:** Teach worker prompts how to request peer comms with fenced structured blocks.  
**Where:** `internal/orchestrator/prompt.go`, `internal/orchestrator/peer_protocol.go`  
**Depends on:** T5

**Done when:**
- [ ] `BuildWorkerPrompt` includes Agent Comms instructions only when task has peers
- [ ] parser extracts `aurelia-peer-message` blocks safely
- [ ] parser validates `to`, `body`, `await`
- [ ] invalid blocks are ignored or returned as structured worker feedback, not panic
- [ ] tests cover valid, malformed and unauthorized blocks

**Verify:**
```bash
go test ./internal/orchestrator/... -run "TestBuildWorkerPrompt.*Peer|TestParsePeer" -v
```

---

### T7: Integrate peer protocol in worker attempt loop

**What:** When worker emits peer message block, route it through bus and provide response/failure back to the worker.  
**Where:** `internal/orchestrator/execute.go`, `internal/orchestrator/peer_protocol.go`  
**Depends on:** T6

**Done when:**
- [ ] worker output is scanned for peer message blocks before finalizing attempt
- [ ] `await=true` waits up to configured timeout
- [ ] response is appended as bounded feedback to the worker continuation/retry
- [ ] timeout does not fail the whole task by default
- [ ] budgets reached are reported to worker as non-retryable peer comms errors
- [ ] tests cover response, timeout and budget exceeded

**Verify:**
```bash
go test ./internal/orchestrator/... -run "TestExecutePlan.*Peer|TestPeerProtocol" -v
```

---

### T8: Telegram progress and consolidation summary

**What:** Show concise collaboration status without flooding Telegram.  
**Where:** `internal/telegram/worker_status.go`, `internal/telegram/orchestration.go`, `internal/orchestrator/prompt.go`  
**Depends on:** T7

**Done when:**
- [ ] Telegram shows at most summarized peer activity per run/wave
- [ ] final consolidation mentions number of peer messages and participants
- [ ] raw peer message bodies are not posted by default
- [ ] tests or fakes verify thread_id is preserved for status updates

**Verify:**
```bash
go test ./internal/telegram/... ./internal/orchestrator/... -run "Test.*Peer|TestWorkerStatus" -v
```

---

### T9: Full validation and documentation pass

**What:** Validate feature behavior and document that network/cross-device is out of scope.  
**Where:** `README.md` or relevant `.specs/` docs if behavior becomes user-visible  
**Depends on:** T8

**Done when:**
- [ ] no network listener/server is introduced
- [ ] all agent comms are local and run-scoped
- [ ] tests cover anti-loop limits and security rejection
- [ ] validation commands pass
- [ ] docs mention Agent Comms as optional execution enhancement, not default behavior

**Verify:**
```bash
go build ./...
go vet ./...
go test ./... -short
go test ./... -v
```

---

## MVP Definition of Done

- [ ] Plan schema supports explicit peers
- [ ] In-memory local Agent Bus works per run
- [ ] Unauthorized peers are denied
- [ ] Limits prevent loops and oversized payloads
- [ ] Payload policy rejects obvious secrets/sensitive paths
- [ ] Manifest and audit record peer activity
- [ ] Existing orchestration works unchanged when no peers are declared
- [ ] No cross-device/network capability exists in MVP
