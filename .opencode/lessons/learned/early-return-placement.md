# Early returns: place setup code before them

**Date**: 2026-05-17
**Change**: pi-sdk-cli-independence
**Category**: pattern

## What happened

Added `~/.pi/agent/` dir creation after `os.MkdirAll(targetDir, 0700)` in `EnsureBridge`.
But there's an early return (lines 50-52) for cached setups, which means the new dir
creation only runs on first-time setup, not on subsequent runs.

## How to avoid

When adding setup/initialization code to a function with early returns, place it before
the earliest return that should trigger it, or verify the early return path still covers
the new code's intent.

## Tags

#lesson #change-pi-sdk-cli-independence #pattern #early-return
