# Testable validators: use overridable function variables

**Date**: 2026-05-17
**Change**: onboarding-improvements
**Category**: pattern

## What happened

Adding Telegram token validation broke existing tests because they tried to call the real Telegram API with dummy tokens. Following the existing `llmModelCatalog` pattern (var pointing to a function) made the validator overridable in tests.

## How to avoid

When adding any function that makes external network/API calls, always create an overridable package-level variable (`var validateToken = validateTelegramToken`) from the start so tests can replace it without network access.

## Tags

#lesson #change-onboarding-improvements #pattern #testability
