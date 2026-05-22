# Do not let accessory features block core recovery

**Date**: 2026-05-22
**Change**: model-selection-regression-recovery
**Category**: anti-pattern

## What happened

The model refresh button was accessory, but fixes kept entangling refresh behavior with core model selection. The real priority was restoring `/model` selection and message execution, even if refresh/local catalog visibility had to be rolled back.

## How to avoid

When core functionality is down, explicitly drop accessory scope. Restore the smallest known-good path first; reintroduce refresh/cache behavior only after live model selection works.

## Tags

#lesson #change-model-selection-regression-recovery #anti-pattern #scope #telegram #models
