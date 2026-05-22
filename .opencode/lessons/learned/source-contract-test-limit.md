# Source contract tests are not runtime proof

**Date**: 2026-05-22
**Change**: model-selection-regression-recovery
**Category**: anti-pattern

## What happened

The first regression test only checked for a diagnostic string and would pass even if the filter was not actually used. It was later strengthened, but source-contract tests still could not prove live Telegram/PI model selection worked.

## How to avoid

Use source-contract tests only as guardrails. For regressions involving SDK/runtime behavior, require a live validation step or an executable integration test that exercises the real contract.

## Tags

#lesson #change-model-selection-regression-recovery #anti-pattern #testing #runtime
