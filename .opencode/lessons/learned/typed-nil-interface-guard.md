# Typed nils crossing interfaces need explicit guards

**Date**: 2026-05-22
**Change**: pi-model-catalog-refresh
**Category**: anti-pattern

## What happened

A nil `*bridge.Bridge` returned as a `modelLister` interface became non-nil and let tests call methods on a nil receiver.
This surfaced when replacing direct bridge access with a small interface for deterministic refresh tests.

## How to avoid

When returning concrete pointers as interfaces, check the pointer for nil before returning it.
Keep nil-interface boundary tests around old nil behavior.

## Tags

#lesson #change-pi-model-catalog-refresh #anti-pattern #go #interfaces
