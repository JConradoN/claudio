# CWD binding must be one source of truth

**Date**: 2026-05-18
**Change**: persistent-cwd-binding
**Category**: pattern

## What happened

Memory bugs traced back to volatile/session CWD and multiple call sites resolving CWD differently.
Persisted binding had to feed prompt assembly, bridge requests, project docs, nudge, and cache invalidation.

## How to avoid

Add one effective-CWD resolver and route every project-aware feature through it; tests should recreate stores with empty sessions.

## Tags

#lesson #change-persistent-cwd-binding #pattern #cwd #memory
