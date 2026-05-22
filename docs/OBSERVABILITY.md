# Observability Guide

How to inspect runs, debug errors, and monitor operational metrics in Aurelia.

## Overview

Aurelia's observability layer provides structured logs, durable run timelines,
Telegram debug commands, and local CLI queries — all local-first (SQLite + JSON
logs), no external telemetry dependency.

### Data flow

```
User message → pipeline → RunContext (run_id)
                              ↓
                    run_journal (metadata)
                    run_events  (timeline phases)
                    slog         (structured logs)
                    audit.log   (security decisions)
```

Each run gets a unique `run_id` (UUID) created before Bridge execution. Every
Bridge request, tool use, result, error, timeout, and fallback is recorded as
a structured event in the `run_events` table.

## Where data lives

| Data | Location | Format |
|------|----------|--------|
| Run metadata | `~/.aurelia/data/runlog.db` → `run_journal` table | SQLite |
| Event timeline | same DB → `run_events` table | SQLite |
| Structured logs | stderr (launchd) or `~/.aurelia/logs/aurelia.stderr.log` | JSON (opt-in) |
| Security audit | `~/.aurelia/audit.log` | JSONL |

## Configuration

In `~/.aurelia/config/app.json`:

```json
{
  "log_level": "info",
  "log_format": "text"
}
```

- **`log_level`**: `"debug"`, `"info"` (default), `"warn"`, `"error"`
- **`log_format`**: `"text"` (default, human-readable), `"json"` (machine-parseable)

Example JSON mode:
```json
{
  "log_level": "debug",
  "log_format": "json"
}
```

JSON log output can be filtered with `jq`:
```bash
grep -E '"msg":"observability"' ~/.aurelia/logs/aurelia.stderr.log | jq '{time, msg, run_id, phase}'
```

## CLI debug commands

Run from the command line while the daemon is stopped or from another terminal:

```bash
# Latest run
aurelia debug last

# Latest run as JSON
aurelia debug last --json

# Specific run
aurelia debug run 01HXabcD1234

# Recent errors
aurelia debug errors --limit 20

# Metrics for today
aurelia debug metrics --days 1

# Metrics as JSON
aurelia debug metrics --days 7 --json
```

### Output examples

```
$ aurelia debug last
🔎 Última execução
run: 01HXabcD · status: completed · 5s
user: 123 · chat: -1001234567 · thread: 42
model: kimi/kimi-k2-thinking
cost: $0.0042 · tokens: 1200 in / 300 out
entrypoint: telegram
agent: coder

Timeline:
  14:23:01   telegram_received
  14:23:02   prompt_built
  14:23:03   bridge_request_started
  14:23:05   bridge_system model=kimi-k2-thinking session=abc123
  14:23:06   bridge_tool_use Read
  14:23:07   bridge_tool_result [file content...]
  14:23:08   bridge_result tokens_in=900 tokens_out=280
  14:23:08   run_completed
```

```
$ aurelia debug errors
❌ Últimas 3 execuções com erro:

  run=01HXerr1  status=failed      dur=48s  chat=-1001234567  error=bridge_process_death
  run=01HXerr2  status=timed_out   dur=15m  chat=-1001234567  error=idle_bridge_timeout
  run=01HXerr3  status=failed      dur=2s   chat=-1001234567  error=rate_limit_exceeded
```

```
$ aurelia debug metrics --days 1
📊 Métricas (últimas 24 horas)

  Execuções:     47
  ✅ Completas:  42 (89.4%)
  ❌ Falhas:     3
  ⏰ Timeout:    1
  🛑 Canceladas: 1
  Tokens in:    45000
  Tokens out:   12000
  Custo total:  $0.1870

  Duração p50:  12s
  Duração p95:  45s

  Por Provider:
    kimi         42 (89.4%)
    anthropic     3 (6.4%)
    openrouter    2 (4.3%)

  Por Entrypoint:
    telegram     44 (93.6%)
    cron          3 (6.4%)
```

## Telegram debug commands (owner only)

Available in any chat where Aurelia is active:

```
/debug last        → latest run summary (status, provider, cost, duration)
/debug run <id>    → full metadata + timeline for a specific run
/debug errors      → recent failed/timed-out runs
```

Non-owners receive a permission-denied message.

## /status upgrade

The `/status` command now includes observability details:

```
✅ Última execução: **completed** · `01HXabcD` (5s atrás) · 5s
⚙️ Modelo: **kimi/kimi-k2**
💰 tokens: 1200 in / 300 out · $0.0042
📋 Checkpoint: `Status: completed...`
```

For failed runs:
```
❌ Última execução: **failed** · `01HXxyz9` (30s atrás) · 48s
⚙️ Modelo: **opencode-go/deepseek-v4** ⚠️ fallback
❌ Erro: `bridge_process_death`
📋 Checkpoint: `Status: failed...`
💡 Digite "continua" para retomar de onde parou.
```

## Phase timeline reference

The `run_events` table records these phases during a run:

| Phase | When | Level |
|-------|------|-------|
| `bridge_request_started` | Before Bridge execution | info |
| `bridge_system` | Bridge system event (model, tools) | info |
| `bridge_tool_use` | Each tool call | info |
| `bridge_tool_result` | Each tool result | info |
| `bridge_result` | Terminal result (tokens, cost) | info |
| `run_completed` | Run completed successfully | info |
| `run_failed` | Run terminated with error | error |
| `run_timed_out` | Max execution or idle timeout | error |
| `run_canceled` | User cancelled | info |
| `bridge_execute_error` | Bridge execution error | error |
| `bridge_process_death` | Bridge process exited | error |
| `retry_started` | Retry after process death | warn |
| `retry_failed` | Retry attempt failed | error |
| `fallback_started` | Fallback to secondary provider | warn |
| `fallback_result` | Fallback completed | warn |

## Privacy & redaction

- All secrets (API keys, tokens, passwords) are redacted before storage
- Prompts are truncated to 500 bytes in `run_journal`
- Tool results are truncated to 1 KB in events
- Event metadata is limited to 4 KB
- Full prompt/checkpoint/tool output is never stored raw
- Debug output in Telegram is always redacted

## Data retention

For the MVP, run data is kept indefinitely. A future release will add
configurable retention pruning:
```
"observability": {
  "retention_days": 30,
  "max_event_metadata_bytes": 4096
}
```

## Troubleshooting

**Q: Debug commands say "sem runlog"?**
A: Ensure the daemon has processed at least one message since the observability
upgrade was deployed. Old rows have empty extended fields.

**Q: Events show up but no provider/model info?**
A: Rows created before the observability migration have empty provider/model
fields. Run a new message to populate them.

**Q: `/debug errors` shows nothing but I know there were errors?**
A: The command shows the most recent runs across all chats. Use
`aurelia debug errors` from CLI to see global errors including from cron.
