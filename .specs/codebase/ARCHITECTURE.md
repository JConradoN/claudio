# Architecture

**Pattern:** Modular Monolith — single binary with well-separated internal packages

## High-Level Structure

```
┌─────────────┐    Telegram     ┌──────────────┐
│  Telegram   │ ◄────────────► │   telebot.v3  │
│   Users     │   Bot API      │  Long Polling  │
└─────────────┘                └──────┬───────┘
                                      │
                         ┌────────────▼────────────┐
                         │     BotController       │
                         │  (internal/telegram)     │
                         │  Input → Pipeline → Out  │
                         └────────────┬────────────┘
                                      │
                         ┌────────────▼────────────┐
                         │   Pipeline Service       │
                         │  (internal/pipeline)     │
                         │  Prompt + Bridge + Plan  │
                         │  Resilience + Supervisor │
                         └────┬───────┬───────┬─────┘
                              │       │       │
                ┌─────────────▼─┐   ┌─▼───┐ ┌─▼──────────────┐
                │    Persona    │   │ Bridge │ Orchestrator   │
                │  (identity,   │   │  Go↔TS │ plan/workers/  │
                │   soul, user) │   │  NDJSON│ validate/git    │
                └───────────────┘   └───┬───┘ └────────────────┘
                                        │ stdin/stdout
                               ┌────────▼────────┐
                               │  bridge/index.ts │
                               │  PI SDK adapter  │
                               └────────┬────────┘
                                        │
            ┌─────────────────────┬─────┴─────────┬──────────────┐
            │                     │               │              │
   ┌────────▼──────┐    ┌────────▼──────┐  ┌─────▼──────┐  ┌────▼────┐
   │  Cron Scheduler│   │ Session Store │  │ Dream     │  │  PI     │
   │  (SQLite, poll │   │ (in-memory,   │  │ (memory   │  │ skills/ │
   │   every 15s)   │   │ session_file  │  │  nudges)  │  │ exts    │
   └───────────────┘   └───────────────┘  └───────────┘  └─────────┘
```

## Identified Patterns

### PI SDK Boundary
**Location:** `bridge/index.ts`, `internal/pipeline/prompt_builder.go`, `internal/session/store.go`
**Purpose:** Keep the PI SDK as the cognitive/execution engine while Aurelia owns product continuity.
**Implementation:** The Bridge uses PI-native `ModelRegistry`, `SessionManager`, `SettingsManager.compaction`, `DefaultResourceLoader(noContextFiles=false)`, and `session.agent.beforeToolCall`. Go tracks `session_file` per `SessionKey` for resume, injects Aurelia-specific prompt layers (persona, Telegram, memory, security context, continuity), and does not own model routing or context compaction.
**Rule:** If PI already owns an engine concern, Aurelia adapts/orchestrates it. If the concern is identity, UX, memory, Wiki, project/user scoping, scheduling, audit or workflow state, Aurelia owns it.

### NDJSON Request Multiplexing
**Location:** `internal/bridge/bridge.go`
**Purpose:** Multiple concurrent LLM requests over a single long-lived process
**Implementation:** Atomic request counter generates IDs. `readLoop()` goroutine routes events to per-request buffered channels (cap=16). Terminal events (`result`, `error`) close the channel.
**Example:** `Bridge.Execute()` → creates channel → sends JSON → returns `<-chan Event`

### Fire-and-Forget Async Execution
**Location:** `internal/telegram/input_pipeline.go` → `internal/pipeline/service.go`
**Purpose:** Non-blocking Telegram message handling — handler returns immediately, results sent asynchronously
**Implementation:** The Telegram input handler launches a goroutine that delegates to the `pipeline.Service`. The service builds the prompt, calls the bridge, processes streaming events, runs plan detection, and sends the Telegram reply on completion.

### Pipeline Service as Reusable Turn Driver
**Location:** `internal/pipeline/`
**Purpose:** Decouple "one conversational turn" from any particular entrypoint so Telegram, cron, and CLI can share the same turn semantics
**Implementation:** `Service.Run()` accepts a chat-shaped input and orchestrates: prompt assembly → resilient bridge call (retry + circuit breaker) → user-scoped active-run tracking → event loop → plan dispatch.
**Example:** `pipeline.go:tryExecutePlan` detects an `aurelia-plan` block and hands control to the orchestrator.

### Agent Orchestration (Plan → Workers → Validate)
**Location:** `internal/orchestrator/`, dispatched from `internal/pipeline/pipeline.go` and `internal/telegram/orchestration.go`
**Purpose:** When the model emits a structured execution plan, fan out atomic tasks to isolated workers, validate their output, and ship the result
**Implementation:** `ExtractPlan` parses the `aurelia-plan` code block. `ExecutionOrder` topologically sorts tasks into waves. `ExecutePlan` spawns workers per wave with bounded concurrency, each in its own git worktree when `needs_worktree` is set. Validation, commit/PR, task-status updates and artifact manifests are partially scaffolded but not yet wired into a closed production cycle.
**Example:** `pipeline.go:tryExecutePlan` → `BotController.executeApprovedPlan` → `Orchestrator.ExecutePlan`.

### Constructor Injection with Interfaces
**Location:** All packages
**Purpose:** Testable, loosely coupled components
**Implementation:** Every struct receives dependencies via `New()` constructor. Key interfaces: `cron.Store`, `cron.Runtime`, `BridgeExecutor`, `ChatSender`, `PersonaBuilder`, `pipeline.ProgressReporter`. Tests use hand-written fakes.
**Example:** `cron.NewScheduler(store, runtime, clock, config)`

### Persona-Based System Prompt Assembly
**Location:** `internal/persona/`, `internal/pipeline/prompt_builder.go`
**Purpose:** Dynamic system prompt construction from identity files + agent config + context
**Implementation:** Layers: Persona (IDENTITY+SOUL+USER) → Agent instructions → Cron instructions → Telegram context. Each layer is optional. Workers receive a different layered prompt (CLAUDE.md + AGENTS.md + spec + design + task + siblings) built by `orchestrator.BuildWorkerPrompt`.

### Embedded Bridge Bundle
**Location:** `internal/bridge/embed.go`, `internal/bridge/setup.go`
**Purpose:** Self-contained binary with TypeScript bridge included
**Implementation:** `go:embed` bundles the TS code. On first run, writes to `~/.aurelia/bridge/`, installs npm deps. Auto-updates when embedded bundle changes.

### Resilient Bridge & Active Run Tracking
**Location:** `internal/pipeline/resilient_bridge.go`, `circuit_breaker.go`, `service.go`
**Purpose:** Recover gracefully from PI failures (rate limits, dead processes, transient network errors) and avoid duplicate concurrent runs for the same chat
**Implementation:** `ResilientBridge` wraps `bridge.Bridge` with retry-with-backoff and a per-error-class circuit breaker. `Service.activeSessions` tracks active work by `chatID:threadID:userID`; `Cancel`, `WorkStatus`, `CancelAllForUser` and bridge-side commands (`abort`, `follow-up`, `steer`, `get-state`) carry user scope.

## Data Flow

### Telegram Message → LLM Response

1. **Input:** Telegram long poller receives message → `handleText/Photo/Voice/Document`
2. **Bootstrap:** Check if user needs first-run persona setup (`popPendingBootstrap`)
3. **Command layer:** Local commands intercept before the LLM (cron CRUD, reset, status — see `internal/telegram/commands.go`)
4. **Routing:** `routeAgent()` — `@name` prefix match OR LLM classification
5. **Pipeline:** `pipeline.Service.Run()` takes over: builds layered prompt, opens a supervised run, calls the resilient bridge
6. **Streaming:** Pipeline accumulates assistant text, tracks tool use, drives a `ProgressReporter` for typing/progress feedback, and stores PI `session_file` for resume
7. **Plan dispatch:** If the final response contains an `aurelia-plan` code block, `tryExecutePlan` strips the block, sends the visible reply, and hands off to `BotController.executeApprovedPlan`
8. **Output:** `SendTextReply()` chunks at 3900 chars, converts MD→HTML, handles rate limits
9. **Session:** PI `session_file` stored for context resumption on next message; SDK compaction handles pruning; dream/nudge consolidation runs in the background

### Approved Plan → Workers → Delivery

1. **Detection:** `orchestrator.ExtractPlan` reads the `aurelia-plan` JSON block from the assistant response
2. **Ensure docs:** `EnsureClaudeMd` and `EnsureAgentsMd` write project conventions and squad config if missing
3. **Plan summary:** `WorkerStatusReporter.SendPlanSummary` posts the wave layout to Telegram
4. **Waves:** `ExecutionOrder` topologically sorts tasks; each wave runs in parallel up to `MaxConcurrentWorkers`
5. **Worker spawn:** For tasks with `needs_worktree`, `WorktreeManager.Create` cuts a git worktree on a `worker/<slug>` branch; otherwise the worker runs in the repo root
6. **Worker prompt:** `BuildWorkerPrompt` layers agent base + `CLAUDE.md` + `AGENTS.md` + `spec.md` + `design.md` + task + sibling context
7. **Streaming:** `ExecuteTask` opens a bridge request per worker, forwarding `tool_use` events as `WorkerEvent` updates to the status reporter
8. **Quality gate:** `Validate` exists as scaffold, but fail-closed validation and retry are not fully wired into the live execution cycle yet
9. **Merge:** Worktree merge/cleanup exists, but branch detection currently needs production hardening before autonomous use
10. **Consolidate:** `BuildConsolidationPrompt`/`Consolidate` exist; commit/PR/task-status/artifact closure remains roadmap work

### Cron Job Execution

1. **Poll:** Scheduler ticks every 15s, queries `ListDueJobs(now, limit=50)`
2. **Dedup:** `sync.Map.LoadOrStore(jobID)` prevents concurrent runs of same job
3. **Execute:** `BridgeCronRuntime.ExecuteJob()` builds persona+agent prompt, calls `bridge.ExecuteSync()`
4. **Record:** Atomic transaction: `RecordExecutionTx` + `UpdateJobTx`
5. **Deliver:** `TelegramDelivery.Deliver()` sends result to `target_chat_id`
6. **Schedule:** Compute `nextRunAt` (cron) or deactivate (once)

## Code Organization

**Approach:** Feature-based packages under `internal/`

**Module boundaries:**
- `cmd/aurelia/` — CLI entry points, dependency wiring, lifecycle
- `internal/telegram/` — Telegram I/O, message processing, rendering, command layer
- `internal/pipeline/` — Reusable turn processor (prompt + bridge + plan dispatch + resilience + supervision)
- `internal/orchestrator/` — Plan→workers→validate cycle, worktree management, quality gate, git/PR delivery
- `internal/bridge/` — TypeScript process management, NDJSON protocol (PI SDK)
- `internal/cron/` — Scheduler, store, runtime, delivery (self-contained with SQLite)
- `internal/dream/` — Background memory consolidation and nudges
- `internal/agents/` — Agent definition loading, routing, classification
- `internal/persona/` — Identity file parsing, system prompt building
- `internal/session/` — PI `session_file` resume mapping, cwd tracking and nudge buffers
- `internal/config/` — App configuration loading, provider management
- `internal/runtime/` — Path resolution, directory bootstrapping
- `internal/onboarding/` — Interactive setup wizard
- `internal/deps/` — Runtime dependency checks (Node, npm, git, gh)
- `internal/version/` — Build version constant
- `pkg/stt/` — Speech-to-text client (Groq)
- `bridge/` — TypeScript source for the PI SDK adapter (`@earendil-works/pi-coding-agent`)
