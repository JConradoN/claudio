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
- Session continuity: resume via session ID, auto-reset on token threshold
- Agent registry: markdown-defined agents with model/tool/MCP overrides
- Onboarding CLI: interactive setup for providers, tokens, and configuration
- Vision model fallback + Groq STT + bridge image format (PI SDK compatible)

### Recently completed (v0.7.16–v0.8.0)
- **Security Guard-Rails** (100%): CapabilityProfile governance, PI tool_call hooks, policy engine, audit trail, fail-closed. 5 profiles: observe→privileged.
- **Persistent Project Binding** (95%): SQLite-backed `/cwd` that survives restart, topic→group fallback, explicit clear, pipeline integration.
- **Continuity Engine v1**: Persistent conversation state, progressive summarization, checkpoint/run journal.
- **UX Polish**: Streaming text, idle timeout, live progress metrics, `/stop`, `/status`, queue system, Telegram ack flow.
- **Bridge Resilience**: Circuit breaker, retry with backoff, translated error messages, scanner-based NDJSON with 10MB limit.
- **Orchestrator scaffold** (~40%): Worktree management, wave execution, validate prompts, git helpers, tasks status — but the cycle doesn't close.

### In progress
- **~8.5K Go LOC + ~550 TS LOC**, comprehensive test coverage (200+ tests)
- Continuity Engine with Progressive Summarization, thinking heartbeat
- 3-message queue per chat/thread

## Roadmap

Ver `.specs/project/ROADMAP.md` para o sequenciamento completo. Resumo:

```
Sprint A → User Isolation MVP
Sprint B → Close Orchestration Cycle (conectar scaffold existente)
Sprint C → Plan Mode Architecture explícito
Sprint D → User-Scoped Project Memory
Sprint E → Wiki Memory Gateway (MCP)
Sprint F → Learning Nudge escopado
Sprint G → Agent Comms
Sprint H → Auto-Skills
```
