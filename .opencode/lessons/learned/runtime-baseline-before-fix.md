# Establish runtime baseline before fixing regressions

**Date**: 2026-05-22
**Change**: model-selection-regression-recovery
**Category**: process

## What happened

We implemented and deployed a plausible bridge-side fix before proving a last-known-good runtime baseline. Tests passed, but Telegram model selection still failed, so the team lost time validating the wrong hypothesis.

## How to avoid

For user-blocking regressions, first deploy a temporary known-good commit via isolated worktree, verify live behavior, then bisect or restore from that baseline.

## Tags

#lesson #change-model-selection-regression-recovery #process #regression #runtime
