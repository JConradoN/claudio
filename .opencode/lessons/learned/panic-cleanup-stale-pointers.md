# Panic recovery: always nil out stale process references

**Date**: 2026-05-19
**Change**: 3-sprint-remediation
**Category**: pattern

## What happened

The `cleanupAfterPanic` method in `bridge.go` closed channels and set `started=false` but did not nil out `cmd`, `stdin`, or `reader`. A subsequent `Stop()` call could attempt `cmd.Process.Kill()` on an already-exited process. The `Stop()` method already does this cleanup correctly.

## How to avoid

When writing a panic recovery/cleanup method for a resource manager, audit all pointer/resource fields and nil/close them in the same way the normal shutdown path does. Extract a shared `resetState()` helper if the cleanup logic is duplicated.

## Tags

#lesson #change-3-sprint-remediation #pattern #panic #cleanup
