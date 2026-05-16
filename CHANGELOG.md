# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).

## [v0.6.7] - 2026-05-16

### Added
- `Makefile` com alvos `build`, `deploy` (atômico), `install-service`, `restart`, `stop`, `status`, `logs`
- `scripts/com.aurelia.agent.plist.tmpl` — template launchd com `KeepAlive` (auto-restart em crash) e `RunAtLoad` (start no login)
- `scripts/install-service.sh` — renderiza o plist e carrega o serviço (idempotente)
- `docs/OPERATIONS.md` — guia de deploy, recovery e troubleshooting do daemon
- `memoryCache.ttl` configurável (default 5s) para pular validação de mtime em chamadas rápidas
- `formatTokenCount()` — prefixa `~` somente quando o total é estimativa por turns

### Changed
- `ResilientBridge.validateChannel` agora valida só o primeiro evento e faz proxy live do restante — eventos `tool_use` voltam a chegar em tempo real ao `ProgressReporter` (antes ficavam buffered até o final da resposta)
- `progressReporter` aplica throttle de 1.5s entre edits para evitar `FloodError`
- `sendTextWithSender` / `sendTextReplyWithSender` pulam `sleep` de 200ms após o último chunk
- `routeAgent` pula classificação LLM quando há <2 agents ou texto curto (<10 chars); timeout reduzido de 15s → 5s
- TLC do orchestrator só é incluído no system prompt quando há `cwd` setado (economiza ~3-5k tokens em chats casuais)
- `MatchCommand` agora normaliza acentos — comandos funcionam com ou sem diacríticos
- `formatResetSummary` e `formatModelResetSummary` omitem `~` quando contagem de tokens é real
- `cmdCronCancel` distingue "ID não informado" de "ID não encontrado"

### Fixed
- `BotController` não cria `nudgeBuffer` redundante — ownership único no `pipeline.Service`

## [v0.6.6] - 2026-05-15

### Added
- Ack imediato 👀 com confirmação ✅ em todas as mensagens (middleware + pipeline)
- `/status` registrado como comando Telegram, com informações humanizadas (modelo, CWD, sessão, trabalho ativo, fila)
- Progress reporter com timer (⏱️ Xm Xs) e limite ampliado para 8 ferramentas
- Supressão de edits duplicados no progress reporter
- `/new` cancela processamento ativo (`pipeline.Cancel`) e mostra resumo da sessão resetada
- Active work status + queue info no `/status` via `pipeline.WorkStatus()`
- `pipeline.Service.Cancel()` e `runSupervisor.cancel()` para interromper execução ativa
- Mensagens de erro do bridge com dicas acionáveis (conexão, cooldown, timeout, retry)
- `FailureTracker.cooldownRemaining()` para mostrar tempo restante nas mensagens de cooldown
- Help com exemplos de comandos naturais
- Documentos não suportados com dica de conversão
- Fila transparente: mensagens incluem contexto do trabalho atual (`queueAdmittedMessage`, `queueStatusMessage`)
- `formatModelResetSummary()` com escopo (tópico/privado) e resumo de mensagens
- `humanBytes()` — bytes formatados como MB/KB/B legíveis
- Filtragem de formatos de imagem exóticos (`isSupportedImageMIME`)

### Changed
- `/model` agora limpa apenas a sessão do thread atual (`ClearSession(chatID, threadID)`, não `ClearAll`)
- `cmdSessionReset` refatorado para usar `resetCurrentSession` com captura de uso antes de limpar
- `cmdStatus` refatorado: remove session ID e warm/cold, adiciona CWD, resumo de sessão, emojis
- `progressReporter.startTime` inicializado no construtor
- `unsupportedDocumentMessage` atualizada com dica de conversão
- Mensagens de bridge error movidas para constantes centralizadas com dicas
- `imageTooLargeError.UserMessage()` usa `humanBytes()`

### Fixed
- Progress reporter não edita mensagem quando o texto não mudou (evita erro "message is not modified")
- `handleModelCommand` e handlers de comando usam `SendTextWithThread`/`SendErrorWithThread` (thread-aware)
- `handleCronCommand` usa `SendErrorWithThread` e `SendTextWithThread`
- `ReactToMessage` protege contra `bot` nulo
- `ackMiddleware` não reage a callbacks (só mensagens de texto/mídia)

### Validation
- **PI Resilience**: validation.md atualizado com evidências de todos os critérios (75 testes passando, circuit breaker, retry, fallback, error classification)
- **Agent Tools Fix**: validation.md atualizado, bundle rebuildado e instalado em `~/.aurelia/bridge/bundle.js`
- **UX Polish**: validation.md atualizado com status de cada user story e edge case

## [v0.6.5] - 2026-05-15

### Fixed
- `disallowed_tools` in agent frontmatter is now respected and filters tools sent to the PI SDK.
- Empty tool restriction (e.g. denylist removing all allowed tools) now returns `[]` instead of falling back to all default PI SDK tools.

### Added
- `Agent.IsReadOnly()` computes effective tool set considering both `allowed_tools` and `disallowed_tools`.
- Validation of unknown tool names in agent YAML frontmatter logs a warning instead of silently ignoring.
- `DisallowedTools` propagated through the full pipeline: pipeline, cron, orchestrator, and Telegram summaries.

## [v0.6.4] - 2026-05-15

### Added
- Run supervisor per chat/thread to serialize active Telegram agent work while allowing independent topics to run in parallel.
- Concurrent message handling for cancel, supersede/correction, status, and queued follow-up intents.
- Bridge cancel command for best-effort interruption of active PI SDK requests.

### Fixed
- Context cancellation and timeouts no longer look like bridge process death or trigger retry loops.
- Bridge pending requests are cleaned up when callers cancel.

## [v0.6.3] - 2026-05-14

### Refactor
- Extracted the LLM/message pipeline into `internal/pipeline.Service`, moving prompt building, project detection, memory cache, bridge execution, and event handling out of `internal/telegram`.
- Kept `BotController` focused on Telegram bootstrap, commands, and I/O through a `pipeline.Output` adapter.

### Changed
- Moved pipeline-focused tests for memory cache, prompt building, and project detection into `internal/pipeline`.
- Marked the optimization plan as fully complete after T14.

## [v0.6.2] - 2026-05-14

### Fixed
- Bridge first-run setup now embeds the TypeScript source, writes `index.ts`, installs `esbuild`, and builds `bundle.js` without requiring versioned JS bundles.
- Removed versioned bridge bundles from git while preserving runtime build support.
- Avoided nil-agent panics when the agent registry fails to load.
- Session GC now runs in production, uses configurable `session_ttl_hours`, and expires orphan CWD entries.
- Memory prompt injection now enforces the total character cap, including the first memory layer.
- Image uploads now honor configured `max_image_bytes` and return a clear user-facing error when oversized.
- Project detection fallback now respects cancellation and schedules debounced index rebuilds on misses.
- Bridge terminal events are preserved under backpressure so slow consumers do not turn dropped `result`/`error` events into false process deaths.

### Added
- Regression tests for bridge setup metadata, memory prompt cap, image size rejection, and orphan CWD GC.

## [v0.6.1] - 2026-05-14

### Added
- Memory cache by mtime — avoids redundant disk reads on every turn
- Project index for fast project lookup with background rebuild
- Album TTL GC — orphan albums cleaned up after 5 minutes
- Async album flush — handler returns immediately, no 900ms blocking
- Event drop logging + counter in bridge readLoop
- Structured logging (log/slog) with configurable level and format
- Image size limit (10 MB default) with validation
- Model list cache with 5-minute TTL
- ChatSender adapter — removes GetBot() leak
- Tests for album GC, memory cache, frontmatter extraction, dropped events

### Changed
- Whitelist lookup from O(n) slice to O(1) map
- SQLite DSN with busy_timeout=5000, synchronous=NORMAL, foreign_keys=ON
- Bridge readLoop: bufio.Scanner → bufio.Reader (no 1MB cap)
- Separated real tokens from estimated tokens in Tracker
- Session GC — periodic cleanup of stale entries
- Split input_pipeline.go (1138→5 files)
- Bundle.js removed from git — built from TS source on first use
- parseCronCreateResponse uses regex instead of manual fence stripping
- handleCwdCommand no longer triggers LLM classify
- deps.Check returns errors instead of log.Fatalf
- Normalized provider keys cached at startup

### Fixed
- Temp photo files now cleaned up after upload
- Bridge process.Kill checks ProcessState before killing
- SetOnDeath callback dispatched in goroutine
- Slice copy in bridgeFailureTracker to avoid backing array leak
- ResolveJobID rejects prefixes with % or _
- Silent event drops now logged + countable

## [v0.5.1] - 2026-05-14

### Changed
- Forum topic memory is now scoped per chat:
  `topics/chat_<chatID>/thread_<threadID>/` instead of `topics/<threadID>/`.
  Telegram threadIDs are only unique within a chat, so two groups with the
  same numeric topic id used to share memory. Existing memory under
  `topics/<id>/` will need to be moved manually (or left to be re-built by
  future nudges).
- `/cwd` display, memory layers, and Telegram instructions all resolve the
  effective working directory the same way the bridge does
  (`agent.Cwd > topic > group > none`). Previously the display claimed agent
  CWD was highest priority but only the bridge cwd and project-docs section
  honored it.
- `/model` (and its callback) now re-export the provider API key env vars
  after persisting, so the new provider's key is in place for the next query.
- Atomic write for `~/.aurelia/config/app.json` when `/model` changes the
  default — prevents truncated configs and lost API keys on mid-write crash.
- Bounded session-lookup cache in the bridge (256 entries, LRU-ish), so a
  long-running daemon does not grow it forever.

### Fixed
- `extractModelName` no longer falls back to the last word of the message.
  Messages misclassified as `CmdSetModel` (e.g. "olá tudo bem amigo") used to
  attempt model changes to garbage strings.
- `extractModelName` correctly handles leading whitespace; the prefix offset
  was computed off the trimmed text but slicing happened on the original.

### Refactor
- `NewBridgeCronRuntime` takes `defaultProvider string` instead of a variadic
  for a single optional argument; `startChatActionLoop` does the same for
  `threadID int`.
- `setupBridge` collapsed to a single `os.Stat` and a single return; the
  10 KB guard threshold now has a named constant and a comment explaining
  the reasoning (bundle is ~12 MB, anything tiny means a failed esbuild run).
- Dropped the unused `replyToID` parameter from `SendTextReply` /
  `SendTextReplyWithThread`.
- `gofmt` import order in `internal/dream/nudge.go`.

## [v0.5.0] - 2026-05-14

### Security
- **BREAKING:** Group chats now require both the group ID in
  `telegram_allowed_group_ids` AND the sender's user ID in
  `telegram_allowed_user_ids`. Previously, any member of a whitelisted group
  could interact with the bot regardless of the user allowlist. Existing
  groups will need user IDs added to keep working.

### Changed
- Removed bridge options that have no analogue in the PI SDK:
  `max_turns`, `permission_mode`, `mcp_servers`, `agents`, `disabled_tools`.
  These were silently ignored since v0.4.0; removing them prevents confusion
  in future development.
- `allowed_tools` no longer auto-includes `web_search`. Agents that need
  web search must list it explicitly in their markdown.

### Fixed
- Bridge no longer leaks PI sessions when `session.prompt()` throws
- Bundle.js is now written atomically (temp + rename) to avoid corruption
  during writes; startup fast-paths size check before reading 12 MB
- `setupBridge` falls back to tsx when bundle.js exists but is truncated
- Instance lock cleanup errors are logged instead of swallowed
- Session ID slicing in logs is now bounds-checked (was unsafe for tests)
- Bridge `duration_ms` reports real elapsed time (was hardcoded 0)

## [v0.4.2] - 2026-05-13

### Added
- Vision fallback model: configure `vision_model`/`vision_provider` in app.json
  for automatic model switching when images are present in the input
- Vision fallback step in onboarding TUI and prompt mode
- Bridge protocol for image attachments with proper PI AI SDK ImageContent format

### Fixed
- Bridge image format: was sending images in Anthropic API format
  (`source.media_type`/`source.data`), now uses PI AI SDK ImageContent
  (`data`/`mimeType`) — fixing silent vision API failures
- Removed invalid `deliverAs: "nextTurn"` from `sendUserMessage` call

## [v0.4.1] - 2026-04-06

### Added
- Runtime dependency checker: validates Node.js, npm, git, gh before startup
- Dependency checklist as Step 1 in onboarding TUI (blocks if required deps missing)
- Boot-time check with clear fatal/warning messages for missing dependencies
- Plain-text dependency check in non-TUI onboarding fallback

## [v0.4.0] - 2026-04-06

### Added
- Live model catalog for OpenRouter provider
- Periodic nudge review replacing per-turn extraction in dream system

### Fixed
- OpenRouter connectivity issues
- Nudge reliability for weak models
- Flush nudge state on session reset
- Windows bootstrap path resolution

## [v0.3.0] - 2026-03-27

### Added
- Project-scoped 3-layer memory system
- Persistent memory system with project context for Telegram
- Memory extraction and consolidation in dream system
- Feature specs for project memory and learning nudge

## [v0.2.0] - 2026-03-26

### Added
- Automatic bridge recovery with retry, session invalidation, and backoff
- LLM-generated bootstrap personas for Telegram
- Command layer for local system commands in Telegram
- Session token tracking with auto-reset and /usage command
- Cost, token, and session ID tracking per cron execution

### Changed
- Migrated documentation to .specs/ structure with CLAUDE.md
- Removed memory system from cron, added ResolveJobID to service
- Replaced magic numbers with named constants
- Broke bootstrapApp into focused sub-functions
- Encapsulated album buffering in dedicated struct
- Injected session.Store and session.Tracker via constructor
- Extracted LLM classification to registry with ClassifyFunc callback
- Extracted Telegram delivery to dedicated cron type
- Extracted session store and tracker from telegram package
- Removed dead code stubs from Telegram package
- Removed dead MemoryWindowSize config field

### Fixed
- Telegram typing indicator errors now logged instead of discarded
- Deactivate cron jobs with unknown schedule type
- Telegram reactions, chat index, and executable error handling
- Atomic transaction for RecordExecution and UpdateJob in cron
- Log swallowed Send and Close errors instead of discarding
- Normalize agent names to lowercase for case-insensitive routing
- Prevent bridge Stop() hang when called before Start()
- Prevent concurrent execution of same cron job

## [v0.1.0] - 2026-03-21

### Added
- TypeScript Bridge wrapping Claude Agent SDK
- Go client for the TypeScript Bridge
- Agent registry with markdown definitions
- Semantic memory with embeddings and cosine similarity
- Cron scheduler adapted to use Bridge for job execution
- Telegram bot with Bridge-based input pipeline
- End-to-end wiring tests
- App bootstrap wiring all components
- Long-lived Bridge process for session continuity
- Session resume for conversation continuity in Telegram
- Active session state tracking per chat
- Continue and agents options in Bridge request protocol
- Pre-fetch cloud MCPs from claude.ai for SDK queries
- Auto-update bundle.js on version mismatch
- LLM-based smart routing for agent classification
- Photo download and analysis via Bridge in Telegram
- Tool progress display during Bridge execution
- /cwd and /reset commands for session control
- Support for Anthropic subscription auth (Max plan)
- Full cron expression support via robfig/cron
- SDK-native agent delegation from Telegram
- BuildSDKAgents to convert registry to SDK format

### Changed
- Simplified persona loader, removed retrieval and memory dependencies
- Updated config schema for providers and embedding config
- Removed pkg/llm, inlined provider catalog for onboarding
- Removed replaced modules (agent, tools, llm, mcp, skill, observability, memory)
- Removed Voyage and Gemini embedders, kept local only

### Fixed
- Bridge SDK cli.js path resolution, always use ~/.aurelia/bridge
- Bridge tool_use emission, permissions flag, and SDK option mapping
- Telegram bypassPermissions for unattended execution
- Timeouts for bridge and memory operations
- Disabled session resume until Bridge became long-lived
- Bridge setup ensured on startup
