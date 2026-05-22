# PI model registry round-trip must be live-verified

**Date**: 2026-05-22
**Change**: model-selection-regression-recovery
**Category**: anti-pattern

## What happened

We assumed models from `ModelRegistry.getAvailable()` could be safely filtered or validated with the same `resolveModel()` path used at query time. That hypothesis was reasonable but insufficient: tests passed while the live PI catalog still did not restore Telegram selection.

## How to avoid

Treat PI model catalog behavior as a runtime contract. Verify with the installed PI SDK and live daemon logs before relying on source-level or mocked tests.

## Tags

#lesson #change-model-selection-regression-recovery #anti-pattern #pi-sdk #models #bridge
