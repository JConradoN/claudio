# Error handling: use slog.Warn for best-effort operations

**Date**: 2026-05-17
**Change**: pi-sdk-cli-independence
**Category**: pattern

## What happened

Added `os.MkdirAll` for `~/.pi/agent/` in bridge setup. Initially swallowed both the
`UserHomeDir` and `MkdirAll` errors silently (`_ =`). Code-reviewer flagged this as
violating the project convention ("Errors treated explicitly — no silent swallowing").

## How to avoid

When an operation is best-effort (should not block execution), use `slog.Warn` to
log the error instead of swallowing it silently. This keeps the non-blocking intent
while making failures visible in logs.

## Tags

#lesson #change-pi-sdk-cli-independence #pattern #error-handling
