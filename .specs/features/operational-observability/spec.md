# Operational Observability

**Roadmap step:** 2 — Observability Foundation before autonomous execution  
**Depends on:** User Isolation + Security Guard-Rails + Project Binding  
**Desbloqueia:** safer Orchestration Cycle, Plan Mode debugging, live operations, support for more users/chats

## Problem Statement

Aurelia already has useful observability pieces, but they are fragmented:

- `internal/runlog/` persists one row per pipeline run, but it lacks user/model/provider/cost/timing/fallback details.
- `/status` can show the latest run, but cannot reconstruct a full run timeline.
- Telegram progress messages show tool activity live, but most detail disappears after the final reply.
- The Bridge emits `request_id`, session file, tokens, duration and tool events, but Go does not persist a structured event timeline.
- Security audit now writes to `~/.aurelia/audit.log`, but it is separate from run lifecycle data.
- Cron has execution records, but normal chat, cron, orchestration and nudge/dream do not share a common correlation model.
- Logs are mostly free-form `log.Printf`, which makes production debugging dependent on grep and human interpretation.

As Aurelia moves toward autonomous orchestration, multiple users/chats, persistent plan state and memory/wiki flows, we need to answer quickly:

```text
Which Telegram message created this run?
Which user/chat/thread/cwd/agent/model/provider did it use?
Which Bridge request/session_file was involved?
Which tools ran, in what order, with what policy decisions?
Did retry/fallback/timeout happen? Why?
How long did each phase take and how much did it cost?
What should I inspect next?
```

The target is not a full SaaS telemetry platform. Aurelia remains local-first. The goal is an operator-grade local observability layer with structured logs, durable run timelines, concise Telegram/CLI debug commands and aggregate operational metrics.

## Goals

- [ ] Propagate a single `run_id` / correlation context from Telegram input through pipeline, Bridge, runlog, audit, cron and orchestration.
- [ ] Replace critical free-form logs with structured `slog` events using consistent field names.
- [ ] Expand `run_journal` so each run records user, provider/model, agent, capability profile, timing, tokens, cost, fallback and error classification.
- [ ] Add a durable `run_events` timeline table for phase-by-phase reconstruction.
- [ ] Connect security audit events to `run_id`/`request_id` wherever possible.
- [ ] Add owner-only debug surfaces in Telegram and CLI: latest run, specific run, recent errors and daily metrics.
- [ ] Track aggregate local metrics: runs, success/failure, latency, tokens, cost, fallback, provider/model breakdown and cron failure rate.
- [ ] Keep sensitive content redacted before persistence and display.
- [ ] Preserve local-first operation: SQLite + JSON logs/files, no external telemetry dependency.

## Non-Goals

- No cloud telemetry service in the MVP.
- No Prometheus/Grafana dependency in the MVP.
- No web UI in the MVP.
- No full transcript persistence beyond existing session/run summaries.
- No storage of raw secrets, full prompts, full tool outputs or unredacted document contents.
- No change to PI SDK session ownership; Aurelia observes/orchestrates, PI remains the execution engine.

## Current Observability Inventory

| Area | Current state | Gap |
|---|---|---|
| Telegram input | handler logs and progress messages | no durable message/run correlation row |
| Pipeline | request id, runlog start/complete, status handling | incomplete fields, no event timeline |
| Bridge | NDJSON events include system/tool/result/error | no persistent per-event timeline, limited Bridge process health summary |
| Runlog | one row per run | lacks user/model/provider/cost/timing/fallback; latest-only UI |
| Security audit | JSONL audit log to stderr/file | not unified with run timeline, missing run_id in Bridge audit |
| Cron | `cron_executions` records output/status/cost/tokens | separate from runlog; hard to compare with normal runs |
| Orchestrator | worker status reporter + partial manifest | not tied to central run timeline yet |
| Logs | mixed `log.Printf`, some `slog` | inconsistent fields and levels |

## Observability Principles

1. **One run, one correlation spine.** Every operator artifact should include `run_id` when a run exists.
2. **Events over prose.** Persist phase changes as structured events, not only text logs.
3. **Redaction before persistence.** Store summaries and metadata, never raw secrets.
4. **Local-first, queryable.** SQLite is the source of operational truth; files are append-only audit/debug streams.
5. **Telegram concise, CLI detailed.** `/debug last` gives a short operator view; CLI can dump full JSON.
6. **Fail-open observability.** Observability errors must not block user work.
7. **No double runtime.** Do not reimplement PI internals; capture what Bridge and PI expose.

## User Stories

### P0: Correlate a Telegram message to a full run

**User Story:** As the operator, when a user reports that Aurelia behaved strangely, I want to identify the exact run from the Telegram chat/thread/user and inspect its provider/model/session/tool/timeout data.

**Acceptance Criteria:**

1. WHEN a Telegram message enters the pipeline THEN a `run_id` SHALL be created before Bridge execution.
2. WHEN the Bridge request is built THEN `run_id`, `request_id`, chat/thread/user, cwd, provider, model and agent SHALL be logged/persisted together.
3. WHEN `/status` shows the latest run THEN it SHALL include a short run id and terminal status.
4. WHEN `aurelia debug run <run_id>` is called THEN it SHALL show the full run metadata and timeline.
5. WHEN a run fails or times out THEN the failure record SHALL include `error_class` or `timeout_origin`.

**Independent Test:** Run a fake pipeline turn, capture `run_id`, and assert the SQLite run record has chat/thread/user/request/provider/model/status and at least start/result events.

---

### P0: Durable phase timeline

**User Story:** As the operator, I want a chronological timeline of important phases so I can see where a run got stuck.

**Acceptance Criteria:**

1. `run_events` SHALL record `run_id`, timestamp, phase, level, message and `metadata_json`.
2. Pipeline SHALL emit at least: `telegram_received`, `prompt_built`, `bridge_request_started`, `bridge_system`, `tool_use`, `tool_result`, `bridge_result`, `reply_sent`, `run_completed`.
3. Error paths SHALL emit terminal events: `run_failed`, `run_canceled`, `run_timed_out`, or `bridge_process_death`.
4. Retry/fallback SHALL emit `retry_started`, `retry_failed`, `fallback_started`, `fallback_result`.
5. Event writes SHALL be best-effort and never block the main run.

**Independent Test:** Feed fake Bridge events through `ProcessBridgeEvents` and assert expected event phases are persisted in order.

---

### P0: Structured logs with stable fields

**User Story:** As the operator, I want logs to be filterable by run id, request id, user id, provider and phase.

**Acceptance Criteria:**

1. Critical runtime paths SHALL use `slog` with stable fields, not ad-hoc `log.Printf`.
2. Config `log_format=json` SHALL output JSON logs suitable for grep/jq.
3. Config `log_level` SHALL control debug/info/warn/error.
4. Secrets SHALL be redacted before logging.
5. Existing plain text behavior MAY remain default for local development if explicitly documented.

**Independent Test:** Initialize logger with JSON format and assert a sample event contains `run_id`, `chat_id`, `thread_id`, `user_id`, `phase`.

---

### P1: Operator debug commands

**User Story:** As the operator, I want to debug from Telegram or CLI without manually opening SQLite files.

**Acceptance Criteria:**

1. `/debug last` SHALL show latest run status, provider/model, duration, tokens/cost, error/timeout and last phases.
2. `/debug run <id>` SHALL show a compact timeline for that run.
3. `/debug errors` SHALL show recent failed/timed-out runs.
4. `aurelia debug run <id> --json` SHALL output machine-readable metadata and timeline.
5. Debug commands SHALL be owner-only in Telegram.

**Independent Test:** Seed runlog rows/events and assert Telegram command output redacts prompt/error secrets and includes timeline phases.

---

### P1: Local operational metrics

**User Story:** As the operator, I want to see daily usage, cost and failure trends before they become production issues.

**Acceptance Criteria:**

1. `aurelia debug metrics --today` SHALL show runs, success rate, p50/p95 duration, tokens, cost and fallback count.
2. Metrics SHALL break down by provider/model and entrypoint (`telegram`, `cron`, `orchestration`, `nudge`).
3. Cron failure rate SHALL be visible separately.
4. Metrics SHALL come from SQLite run records/events, not external services.
5. Output SHALL support text and JSON modes.

**Independent Test:** Seed sample runs with durations/cost/failures and assert aggregate metrics match expected values.

---

### P1: Bridge/process health view

**User Story:** As the operator, I want to know whether the Bridge is healthy, stuck, restarting or dropping events.

**Acceptance Criteria:**

1. Bridge lifecycle SHALL emit structured events for start, stop, unexpected death and restart.
2. Dropped events count SHALL be exposed in debug output.
3. Bridge request stalls SHALL record last event age and timeout origin.
4. Model cache prewarm/list-models status SHALL be visible in debug output.
5. Bridge stderr logging SHALL be routed or summarized without leaking secrets.

**Independent Test:** Simulate Bridge death and assert run timeline includes process death and retry/recovery events.

## Data Retention and Privacy

- Run metadata and event timelines are local SQLite data.
- Prompts/checkpoints/tool summaries remain truncated and redacted.
- Full tool output is not persisted by default.
- Audit logs rotate by size and remain local.
- Future retention config may prune old `run_events` and completed runs after N days.

## Success Metrics

- Operator can answer “what happened to this message?” in under 2 minutes.
- Every Bridge request has a durable `run_id` + `request_id` link.
- Failed/timed-out runs have a visible phase and reason.
- `/debug last` and CLI debug commands work without direct SQLite access.
- Orchestration implementation can rely on the same timeline rather than inventing a separate one.
