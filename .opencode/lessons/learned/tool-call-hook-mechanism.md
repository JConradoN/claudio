# PI SDK: use beforeToolCall for tool interception, not session.on()

**Date**: 2026-05-21
**Change**: fix-security-hook-pi-sdk-api
**Category**: pattern

## What happened

The security guard-rail feature needs to intercept tool calls before they execute (block or rewrite args). The code used `session.on("tool_call", handler)` which doesn't exist. The correct mechanism is `session.agent.beforeToolCall`, a property on the underlying `Agent` class that receives `BeforeToolCallContext` and can return `{ block: true, reason }` to prevent execution.

## How to avoid

For tool call interception in the PI SDK bridge:

1. **To block tools**: wrap `session.agent.beforeToolCall` — returns `{ block: true, reason }` to prevent execution. Save the original hook (set by AgentSession for extension runner) and chain to it after security checks.
2. **To rewrite args**: mutate `context.args` in place (shared reference with extension runner and tool execution).
3. **To observe only**: use `session.subscribe()` and filter for `tool_execution_start` events.
4. Cleanup: restore the original `beforeToolCall` on session teardown.

## Tags

#lesson #change-fix-security-hook-pi-sdk-api #pattern #pi-sdk #bridge #security
