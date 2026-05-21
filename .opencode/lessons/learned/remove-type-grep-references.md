# Grep all references before removing a type

**Date**: 2026-05-20
**Change**: remove-token-tracker-enable-compaction
**Category**: process

## What happened

Removing `session.Tracker` required changes across 14 files (Go services, commands, middleware, tests, CLI entrypoint). Missed references in `result_event_test.go` initially — caught by `go vet` but could have been caught earlier with a thorough grep.

## How to avoid

Before removing a type, `grep -r` the entire codebase for `New<Type>` and `.tracker`-style field access patterns. Build and vet before considering the change done. Track all affected files upfront.

## Tags

#lesson #change-remove-token-tracker #process #refactoring
