# Migrating constants to functions requires updating all call sites

**Date**: 2026-05-17
**Change**: onboarding-improvements
**Category**: pattern

## What happened

Migrated hardcoded string constants in `messages.go` to i18n-backed functions. Every reference across 7 files (including tests) needed `()` appended. Used `replaceAll` in the edit tool to handle this efficiently.

## How to avoid

When changing from a constant to a function (or vice versa), use `grep` first to catalog ALL call sites, then make targeted edits. `replaceAll` in the edit tool is effective when the name is unique.

## Tags

#lesson #change-onboarding-improvements #pattern #refactoring
