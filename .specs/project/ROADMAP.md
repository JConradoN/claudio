# Roadmap

## Done

### 1. Bridge Recovery Automático ✓

**Shipped:** 2026-03-26 — commit `0484f08`
**Spec:** `.specs/features/bridge-recovery/`

Retry automático com session resume, feedback visual ("Reconectando..."), backoff com cooldown após 3 falhas consecutivas.

---

## Priority: High

---

### 2. Command Layer — Comandos Locais Antes do LLM

**Spec:** `.specs/features/command-layer/`

**Problem:** Toda mensagem passa pelo LLM classify, mesmo operações que o Go resolve sozinho. Gasta ~15s de latência e tokens desnecessariamente.

**Scope:**
- P1: CRUD de cron jobs local, reset de sessão
- P2: Status do sistema, listar agents, listar modelos/provedores
- Intercepta antes do LLM, fallback transparente

**Packages:** `internal/telegram/`, `internal/cron/`, `internal/agents/`

---

### 3. Orquestração de Agents via SDK

**Spec:** `.specs/features/agent-orchestration/`

**Problem:** A orquestração de agents é primitiva e invisível. O objetivo é transformar a Aurelia numa tech lead autônoma que decompõe tarefas, spawna workers em worktrees isolados, e entrega software completo — do planejamento ao PR.

**Modelo:** Composio-inspired (orchestrator + worker genérico). Duas fases: Aurelia planeja (LLM) → Go executa workers (bridge sessions paralelas) → Aurelia consolida.

**Scope:**
- P1: Decomposição de tarefas, worker genérico, worktrees isolados, feedback visual, ciclo git autônomo
- P2: maxTurns por worker, rastreamento de custo
- P3: Budget por worker

**Packages:** `internal/bridge/`, `internal/telegram/`, `internal/agents/`, `bridge/index.ts`

## Priority: Backlog

_(Vazio — features futuras serão adicionadas conforme necessidade)_
