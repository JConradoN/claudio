# Operational Observability — Design

**Spec:** `.specs/features/operational-observability/spec.md`  
**Roadmap step:** 2 — Observability Foundation  
**Status:** 🔴 Spec pronta; implementação pendente  
**Depends on:** User Isolation + Security Guard-Rails + Project Binding

## Architecture Overview

Aurelia should get a small local observability layer rather than scattered logs. The proposed package is:

```text
internal/observability/
  context.go      # RunContext and field conventions
  logger.go       # slog setup and helpers
  event.go        # RunEvent type + recorder interface
  metrics.go      # local aggregation helpers
  redact.go       # shared redaction facade where needed
```

Existing stores stay in place, but `runlog` becomes the durable core:

```text
Telegram / Cron / Orchestration / Nudge
        ↓
Observability RunContext
run_id · entrypoint · chat/thread/user · cwd · agent · provider/model
        ↓
runlog.run_journal      runlog.run_events       security audit.log
one row per run         timeline per run        policy decisions
        ↓
/debug · aurelia debug · /status · metrics
```

## Correlation Model

### RunContext

```go
type RunContext struct {
    RunID      string
    EntryPoint string // telegram | cron | orchestration | nudge | cli
    RequestID  string
    ChatID     int64
    ThreadID   int
    UserID     int64
    MessageID  int
    CWD        string
    AgentName  string
    Provider   string
    Model      string
    Profile    security.CapabilityProfile
    StartedAt  time.Time
}
```

Rules:

- `run_id` is created once before the first durable run write.
- `request_id` remains the Bridge correlation id and may be created after `run_id`.
- `run_id` should be passed into Bridge `RequestOptions.Security.RequestID` only if Bridge protocol gains a distinct field; until then, keep `request_id` separate and include both in Go timeline.
- For cron, `ChatID`/`UserID` come from job target/owner.
- For orchestration, parent plan run and worker runs should share a parent id.

### Field names

Use stable lowercase snake_case keys across logs, SQLite metadata and JSON output:

```text
run_id
parent_run_id
request_id
entrypoint
chat_id
thread_id
user_id
message_id
cwd
agent
provider
model
capability_profile
phase
status
error_class
timeout_origin
duration_ms
input_tokens
output_tokens
cost_usd
used_fallback
session_file
```

## Storage Design

### Extend `run_journal`

Current table has a useful foundation. Add nullable/backfilled columns:

```sql
ALTER TABLE run_journal ADD COLUMN user_id INTEGER DEFAULT 0;
ALTER TABLE run_journal ADD COLUMN entrypoint TEXT DEFAULT 'telegram';
ALTER TABLE run_journal ADD COLUMN agent_name TEXT DEFAULT '';
ALTER TABLE run_journal ADD COLUMN provider TEXT DEFAULT '';
ALTER TABLE run_journal ADD COLUMN model TEXT DEFAULT '';
ALTER TABLE run_journal ADD COLUMN capability_profile TEXT DEFAULT '';
ALTER TABLE run_journal ADD COLUMN duration_ms INTEGER DEFAULT 0;
ALTER TABLE run_journal ADD COLUMN input_tokens INTEGER DEFAULT 0;
ALTER TABLE run_journal ADD COLUMN output_tokens INTEGER DEFAULT 0;
ALTER TABLE run_journal ADD COLUMN cost_usd REAL DEFAULT 0;
ALTER TABLE run_journal ADD COLUMN tool_count INTEGER DEFAULT 0;
ALTER TABLE run_journal ADD COLUMN error_class TEXT DEFAULT '';
ALTER TABLE run_journal ADD COLUMN timeout_origin TEXT DEFAULT '';
ALTER TABLE run_journal ADD COLUMN used_fallback INTEGER DEFAULT 0;
ALTER TABLE run_journal ADD COLUMN session_file TEXT DEFAULT '';
```

Indexes:

```sql
CREATE INDEX IF NOT EXISTS idx_run_journal_started ON run_journal(started_at DESC);
CREATE INDEX IF NOT EXISTS idx_run_journal_user_started ON run_journal(user_id, started_at DESC);
CREATE INDEX IF NOT EXISTS idx_run_journal_status_started ON run_journal(status, started_at DESC);
```

### Add `run_events`

```sql
CREATE TABLE IF NOT EXISTS run_events (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    run_id TEXT NOT NULL,
    ts INTEGER NOT NULL,
    phase TEXT NOT NULL,
    level TEXT NOT NULL DEFAULT 'info',
    message TEXT DEFAULT '',
    metadata_json TEXT DEFAULT '{}'
);

CREATE INDEX IF NOT EXISTS idx_run_events_run_ts ON run_events(run_id, ts, id);
CREATE INDEX IF NOT EXISTS idx_run_events_phase_ts ON run_events(phase, ts DESC);
```

`metadata_json` should be small and redacted. Large payloads are forbidden.

### Retention

MVP can keep data indefinitely. Follow-up config:

```json
"observability": {
  "retention_days": 30,
  "max_event_metadata_bytes": 4096,
  "log_format": "json"
}
```

## Runtime Event Phases

Minimum timeline phases for normal Telegram turns:

```text
telegram_received
user_gate_ok
agent_routed
prompt_built
bridge_request_started
bridge_system
bridge_tool_use
bridge_tool_result
bridge_result
reply_started
reply_sent
continuity_patched
dream_nudge_scheduled
run_completed
```

Error/edge phases:

```text
run_failed
run_canceled
run_timed_out
bridge_execute_error
bridge_process_death
retry_started
retry_failed
fallback_started
fallback_result
security_block
```

Cron phases:

```text
cron_due
cron_prompt_built
cron_bridge_started
cron_completed
cron_failed
```

Orchestration phases should reuse the same table:

```text
orchestration_preflight_started
orchestration_preflight_failed
worker_started
worker_tool_use
worker_validation_failed
worker_approved
worker_merged
orchestration_committed
orchestration_completed
```

## Logging Design

### Logger setup

`cmd/aurelia` should configure logging once after config load:

```go
logger := observability.NewLogger(observability.LoggerConfig{
    Level: cfg.LogLevel,
    Format: cfg.LogFormat, // text | json
})
slog.SetDefault(logger)
```

Migration path:

1. Keep existing `log.Printf` functional.
2. Add `slog` in new observability paths.
3. Gradually replace critical pipeline/bridge/telegram logs.

### Required fields

Every important log should include:

```go
logger.Info("bridge request started",
    "event", "bridge_request_started",
    "run_id", rc.RunID,
    "request_id", rc.RequestID,
    "chat_id", rc.ChatID,
    "thread_id", rc.ThreadID,
    "user_id", rc.UserID,
)
```

## Recorder Interface

```go
type Recorder interface {
    StartRun(ctx context.Context, rec RunRecord) error
    UpdateRun(ctx context.Context, update RunUpdate) error
    CompleteRun(ctx context.Context, runID string, status RunStatus, result RunResult) error
    RecordEvent(ctx context.Context, ev RunEvent) error
}
```

The pipeline should depend on an interface rather than directly on SQLite. Existing `runlog.Store` can evolve into this interface.

All recorder calls should be wrapped with small timeouts and fail open:

```go
ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
defer cancel()
if err := recorder.RecordEvent(ctx, ev); err != nil {
    slog.Warn("observability event dropped", "error", err, "run_id", ev.RunID)
}
```

## Telegram Debug Commands

Owner-only commands:

```text
/debug last
/debug run <run_id>
/debug errors
/debug metrics today
```

Suggested `/debug last` output:

```text
🔎 Última execução
run: 01HX... · status: failed · 48s
user: 123 · chat: -100... · thread: 42
model: opencode-go/deepseek-v4-flash
cwd: /Users/igor/aurelia
cost: $0.0042 · tokens: 12k in / 2k out
error: bridge_process_death

Timeline:
12:01:03 telegram_received
12:01:04 prompt_built
12:01:05 bridge_request_started req_42
12:01:27 bridge_process_death
12:01:28 retry_started
12:01:51 run_failed
```

## CLI Debug Commands

Add a new `cmd/aurelia/debug_cli.go`:

```bash
aurelia debug last
aurelia debug run <run_id>
aurelia debug errors --limit 20
aurelia debug metrics --today
aurelia debug run <run_id> --json
```

CLI should read the same local SQLite DBs. JSON output is for scripts and future dashboards.

## Metrics Design

Metrics are computed from `run_journal` and `cron_executions`.

MVP metrics:

```text
runs_total
runs_completed
runs_failed
runs_timed_out
success_rate
fallback_count
tokens_input_total
tokens_output_total
cost_usd_total
duration_p50_ms
duration_p95_ms
provider_breakdown
model_breakdown
cron_success_rate
```

Implementation can start with SQL queries over a time window; no background aggregation needed.

## Security and Privacy

- Use existing redaction before storing prompt/checkpoint/tool summaries.
- `metadata_json` must reject or truncate values over a configured byte limit.
- Debug Telegram output must never include full prompt or full tool output.
- CLI `--json` can include more metadata but still redacted.
- `audit.log` remains append-only JSONL-ish with rotation; run timeline links to audit by `request_id`/`run_id` when available.

## Migration Strategy

1. Add columns/tables idempotently; existing `runlog.db` keeps working.
2. Backfilled fields default to zero/empty.
3. `/status` should tolerate old rows.
4. Debug commands should show “unknown” for missing old fields.
5. Existing tests should pass without requiring old DB migration fixtures to change.

## Open Questions

1. Should `run_id` be visible to normal users or only owner/debug output?
2. Should the Bridge protocol get a separate `run_id` field in `SecurityContext` or `RequestOptions`?
3. Should audit log use pure JSONL without `[security]` prefix in a future breaking cleanup?
4. Should retention pruning be automatic on startup or explicit via `aurelia debug prune`?
5. Should `/status` show cost/tokens to non-owner users?
