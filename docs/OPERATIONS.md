# Operations Guide

How to build, deploy, and manage the Aurelia daemon on macOS.

## TL;DR

```bash
make install-service   # one-time: install launchd plist
make deploy            # every code change: atomic build + restart
make logs              # tail stderr to see what the daemon is doing
```

## Layout

| Path | Purpose |
|------|---------|
| `~/.aurelia/bin/aurelia` | Deployed binary (running daemon) |
| `~/.aurelia/logs/aurelia.stderr.log` | Daemon stderr (logs go here) |
| `~/.aurelia/logs/aurelia.stdout.log` | Daemon stdout |
| `~/Library/LaunchAgents/com.aurelia.agent.plist` | launchd job definition |
| `scripts/com.aurelia.agent.plist.tmpl` | Template (in repo) |
| `scripts/install-service.sh` | Renders template, loads service |
| `~/bin/aurelia` | Thin shell wrapper (start/stop/logs convenience) |

The daemon runs under launchd as **user agent** (not root), label
`com.aurelia.agent`, service path `gui/$(id -u)/com.aurelia.agent`.

## First-time setup

```bash
make install-service
```

What this does:
1. Renders `scripts/com.aurelia.agent.plist.tmpl` substituting `__HOME__`,
   `__BINARY__`, `__PATH__` from the current shell.
2. Writes it to `~/Library/LaunchAgents/com.aurelia.agent.plist`.
3. `bootout` any existing instance, then `bootstrap` + `kickstart -k` so it
   starts immediately.

Re-run this any time you change the plist template (env vars, log paths, etc).

## Daily workflow

```bash
make deploy
```

Atomic deploy:
1. `go build -o ~/.aurelia/bin/aurelia.new ./cmd/aurelia`
2. `mv aurelia.new aurelia` — atomic rename. The running daemon keeps its
   mmap of the old inode and isn't disturbed by the swap.
3. `launchctl kickstart -k` — graceful restart picks up the new binary.

If the service isn't loaded, `make deploy` just updates the binary and warns.

## Service lifecycle

| Need | Command |
|------|---------|
| Restart without rebuild | `make restart` |
| Stop | `make stop` |
| Show state | `make status` |
| Live stderr | `make logs` |
| Live stdout | `make stdout` |
| Remove plist + unload | `make uninstall-service` |

## Auto-restart on crash & on boot

The plist enables:
- `RunAtLoad=true` — starts at login.
- `KeepAlive.Crashed=true` — restarts on non-zero exit (crash).
- `KeepAlive.SuccessfulExit=false` — does NOT restart after `make stop` or
  any clean exit.
- `ThrottleInterval=10` — wait 10s between restarts (avoids tight loops if
  something is broken).

## Recovering from "the plist is gone but the process is running"

Symptom: `make status` says "service not loaded" but `ps aux | grep aurelia`
shows a live process (often `PPID=1`).

Cause: someone ran `launchctl bootout` while the binary kept running, or the
plist was deleted by hand.

Fix:
```bash
pkill -f '\.aurelia/bin/aurelia$'   # graceful TERM to the orphan
make install-service                # reinstall plist + start fresh
make status                         # confirm pid is owned by launchd now
```

## Bridge rebuild (TypeScript)

The bundle in `internal/bridge/bundle.js` is embedded into the Go binary
via `go:embed`. After editing `bridge/index.ts`:

```bash
make bridge
make deploy
```

## What lives where, summarized

```
Repo                                      Runtime
─────────────────────────────────────     ─────────────────────────────────────
cmd/aurelia/         (Go sources)    →    ~/.aurelia/bin/aurelia
bridge/index.ts                      →    internal/bridge/bundle.js → embedded
scripts/*.plist.tmpl                 →    ~/Library/LaunchAgents/*.plist
Makefile             (targets)
docs/OPERATIONS.md   (this file)
```

## Troubleshooting

**`make deploy` fails with `bootstrap failed: 5: Input/output error`**
The plist exists but launchd is in an inconsistent state. Run:
```bash
launchctl bootout gui/$(id -u)/com.aurelia.agent || true
make install-service
```

**Two daemons fighting over Telegram polling**
Symptom: bot answers some messages, ignores others; logs show 409 Conflict.
Cause: an orphan instance is running alongside the launchd-managed one.
Fix: `pkill -f '\.aurelia/bin/aurelia$'` then `make restart`.

**`make logs` says log not found**
The service has never started. Run `make install-service` first.

**Binary updated but behavior unchanged**
You forgot the restart step. Use `make deploy` (build + restart) instead of
`make install` or `make build` alone.

**Daemon won't start after edit to plist template**
Validate the plist:
```bash
plutil -lint ~/Library/LaunchAgents/com.aurelia.agent.plist
```
Check stderr: `make logs`.

## Why a Mac launch agent (and not the obvious alternatives)

- **Native**: no extra runtime, autostart at login is built in.
- **Per-user**: no `sudo`, runs with your env and home dir.
- **Survives crashes** without us writing a restart loop.

Trade-off: your Mac must be on. If 24/7 uptime matters more than free, deploy
to a small VPS with systemd — same `make deploy` pattern works with a unit
file in place of the plist.
