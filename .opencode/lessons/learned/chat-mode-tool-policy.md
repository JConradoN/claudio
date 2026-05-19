# Chat mode needs tool policy enforcement

**Date**: 2026-05-18
**Change**: persistent-cwd-binding
**Category**: anti-pattern

## What happened

Prompt-only chat mode was insufficient: the bridge still had a daemon CWD, so filesystem tools needed explicit denial.
The memory prompt also had to stop telling no-CWD chats to use Write.

## How to avoid

When declaring a no-project/chat mode, enforce it at tool policy level and keep system-prompt instructions consistent with allowed tools.

## Tags

#lesson #change-persistent-cwd-binding #anti-pattern #security #tools
