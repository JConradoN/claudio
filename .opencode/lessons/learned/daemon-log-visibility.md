# Daemon logs must stay visible after restart

**Date**: 2026-05-18
**Change**: cwd-scope-and-workspace-fix
**Category**: tool

## What happened

The first restart sent daemon stdout/stderr to `/dev/null`, hiding the new `/cwd` diagnostic that contained the real failing path.
Once logs were redirected to `~/.aurelia/logs/`, the root cause was immediately visible.

## How to avoid

Restart long-lived daemons with explicit log redirection and verify the startup version line before asking for live testing.

## Tags

#lesson #change-cwd-scope-and-workspace-fix #tool #logs #daemon
