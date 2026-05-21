# PI SDK: verify API exists in installed version before using

**Date**: 2026-05-21
**Change**: fix-security-hook-pi-sdk-api
**Category**: anti-pattern

## What happened

Assumed `session.on("tool_call", handler)` existed in the PI SDK and used it for security guard-rail hook. The `AgentSession` class has no `on()` method — only `session.subscribe()` (observation-only) and `session.agent.beforeToolCall` (can block). The version-check guard `typeof session.on !== "function"` always evaluated to `true`, making the security feature always fail with "PI SDK version too old".

## How to avoid

When using PI SDK APIs, always verify the method/property exists by checking the **installed version's type definitions** (`node_modules/@earendil-works/*/dist/*.d.ts`). Don't assume an API exists because the naming makes sense conceptually. Check both the class interface and the actual runtime behavior.

## Tags

#lesson #change-fix-security-hook-pi-sdk-api #anti-pattern #pi-sdk #bridge
