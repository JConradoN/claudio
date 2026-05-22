# Auth symlink instead of copy

The daemon creates an isolated PI SDK config directory at `~/.aurelia/pi-agent/`
to prevent session/settings collisions with interactive PI CLI usage.

Initially, the daemon **copied** the entire `~/.pi/agent/` content into
`~/.aurelia/pi-agent/` on first run. This meant credentials could go stale:
if the user updated `auth.json` via `pi auth` or token renewal, the daemon
would keep using the old key — resulting in silent hang failures (the model
"resolved" but the API call hung because the key was invalid).

## Solution

Replace the one-time copy with a **symlink** for `auth.json` only:

```
~/.aurelia/pi-agent/auth.json → ~/.pi/agent/auth.json
```

Other config files (sessions, settings, models) remain isolated in
`~/.aurelia/pi-agent/` to avoid collisions.

The symlink is verified on every daemon restart: if it's missing, broken, or
points to the wrong path, it is replaced. If it's already correct, it's left
alone.

## Symptoms of stale auth

- Bridge receives the query and resolves the model (system event)
- No PI SDK events arrive for 30+ seconds (streaming stall)
- Direct bridge test via pipe works because it uses the PI CLI's `auth.json`
- Daemon logs show `streaming stall: no PI SDK events for 30s`

## Implementation

`internal/bridge/setup.go` — `EnsureBridge()`:
- Check `~/.pi/agent/auth.json` exists
- If `~/.aurelia/pi-agent/auth.json` is not a symlink to it, remove and re-create
- The old `hasNonEmptyAuth()` and `copyDirContents()` functions were removed
