# Delegate responsibility to dependencies instead of wrapping

**Date**: 2026-05-20
**Change**: remove-token-tracker-enable-compaction
**Category**: pattern

## What happened

Removed Go `session.Tracker` (200+ lines + test file) because PI SDK `SettingsManager.compaction` already handles context pruning. The tracker duplicated functionality with turn-based estimation and threshold-based auto-reset — complexity that the SDK already provides.

## How to avoid

When adding a wrapper around a dependency, first verify the dependency doesn't already handle the use case. Research the dependency's capabilities before building abstraction layers. If the dependency already handles it, remove the wrapper.

## Tags

#lesson #change-remove-token-tracker #pattern #dependency
