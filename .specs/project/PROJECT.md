# Project

## Vision

Aurelia OS is an autonomous agent operating system accessible via Telegram. The goal is not to reimplement what PI already does — it's to **orchestrate it**, adding persistence, scheduling, multi-project support, and a natural Telegram interface on top.

One persistent Go daemon, many projects, many agents.

## Goals

- **Natural interface** — Talk to an AI assistant via Telegram with text, photos, voice, documents. No CLI required for daily use.
- **Agent orchestration** — Route messages to specialist agents, schedule autonomous execution, deliver results back to Telegram.
- **Local-first** — Single binary, SQLite, no cloud dependencies beyond LLM providers. Runs on your machine, owns your data.
- **Stay light** — Don't rebuild what the PI SDK already provides. Wrap it, orchestrate it, extend it.
- **Multi-provider** — Not locked to Anthropic. Support Kimi, OpenRouter, Zai, Alibaba, and whatever comes next.

## Constraints

- **Single user** — Personal assistant, not a multi-tenant platform
- **Telegram-only interface** — No web UI, no other chat platforms (for now)
- **Bridge dependency** — LLM reasoning requires Node.js runtime for the PI SDK bridge
- **Cross-platform** — CI and development target macOS, Windows, and Linux
- **No Docker** — Single binary deployment, no container orchestration

## Current State (May 2026)

### Core operational
- Core loop working: Telegram → Agent routing → Bridge → PI SDK → Response
- Persona system: IDENTITY.md + SOUL.md + USER.md assembled into system prompts
- Cron scheduler: SQLite-backed, recurring and one-time jobs, Telegram delivery
- Multi-modal input: text, photos (albums), voice (Groq STT), documents
- Session continuity: resume via PI `session_file`; context pruning delegated to PI SDK compaction
- Agent registry: markdown-defined Aurelia specialists with model/tool/MCP overrides (migration to PI-native agents remains open)
- Onboarding CLI: interactive setup for providers, tokens, and configuration
- Vision model fallback + Groq STT + bridge image format (PI SDK compatible)

### Recently completed (v0.11.0–v0.13.0)
- **User Isolation MVP**: user profiles, owner gate, per-user persona/memory loading, user-scoped sessions, cron ownership, `/users`, `/forget-me`, migration CLI.
- **Delegate to PI SDK Native — core slice**: PI model resolution, PI context-file loading, PI compaction, `session_file` resume, Bridge-side session lifecycle (`steer`/`followUp`/`abort`).
- **Security Guard-Rails**: CapabilityProfile governance, PI tool_call hooks in the Bridge, audit trail, fail-closed. 5 profiles: observe→privileged.
- **Persistent Project Binding**: SQLite-backed `/cwd` that survives restart, topic→group fallback, explicit clear, pipeline integration.
- **Continuity Engine v1**: Persistent conversation state, progressive summarization, checkpoint/run journal.
- **UX Polish**: Streaming text, idle timeout, live progress metrics, `/stop`, `/status`, queue system, Telegram ack flow.
- **Bridge Resilience**: Circuit breaker, retry with backoff, translated error messages, scanner-based NDJSON with 10MB limit.
- **Orchestrator scaffold** (~40%): Worktree management, wave execution, validate prompts, git helpers, tasks status — but the cycle doesn't close.

### In progress
- Closing the conceptual boundary: PI owns model/session/context/tool execution; Aurelia owns Telegram UX, identity/persona, persistence, scheduling, memory, project binding and orchestration.
- User Isolation hardening: bridge active session commands are now user-scoped; remaining audit is `CancelAllForUser` broadcast semantics and stale spec checkboxes.
- Agent registry migration decision: keep Aurelia specialists as product-layer feature or migrate parsing/execution to PI-native agents.
- Orchestration Cycle: existing scaffold must be connected to validation, commit/PR, task-status updates and artifact manifests.

## Roadmap

Ver `.specs/project/ROADMAP.md` para o sequenciamento completo. Resumo:

```
Sprint 0 → Delegate to PI SDK Native core ✅; remaining: agent registry boundary + docs/spec cleanup
Sprint A → User Isolation MVP ✅; active session scoping fixed; remaining: cleanup/audit
Sprint B → Close Orchestration Cycle (conectar scaffold existente)
Sprint C → Plan Mode Architecture explícito
Sprint D → User-Scoped Project Memory
Sprint E → Wiki Memory Gateway (MCP)
Sprint F → Learning Nudge escopado
Sprint G → Agent Comms
Sprint H → Auto-Skills
```
