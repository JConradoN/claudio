# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).

## [0.7.2] - 2026-05-18

### Corrigido
- Corrigido falso sucesso quando o Bridge retorna resultado vazio, evitando resposta "(sem resposta)" e contaminação da memória.
- Endurecido o sistema de nudge/dream para executar extração e consolidação de memória sem ferramentas de arquivo do PI SDK.
- Adicionado writer seguro em Go para memória, com validação de paths, bloqueio de `personas/`, proteção contra symlinks e sanitização de fatos/títulos.
- Adicionado rate limit por chat/thread para nudge, incluindo tentativas com erro ou JSON inválido.
- Memórias carregadas no prompt agora são marcadas como dados não confiáveis para reduzir risco de prompt injection persistente.

### Adicionado
- Testes de regressão para resposta vazia, parsing JSON de memória, path traversal, symlinks, sanitização, rate limit e consolidação segura.

## [0.7.1] - 2026-05-18

### Corrigido
- O lock do Dream agora preserva o timestamp da última consolidação entre conclusões (internal/dream/lock.go)
- As chaves do NudgeBuffer agora incluem chatID e threadID para evitar vazamento entre grupos (internal/session/nudge_buffer.go)
- A memória privada do projeto agora está isolada por conversa/thread, impedindo que anotações de um grupo/tópico do Telegram vazem para outra conversa vinculada ao mesmo repositório (internal/runtime/*, internal/pipeline/*, internal/telegram/bot_middleware.go)

### Adicionado
- Testes para preservação de timestamp do lock do Dream, isolamento do NudgeBuffer e memória de projeto escopo por conversa (internal/*_test.go)

## [v0.7.0] - 2026-05-17

### Added
- **Onboarding guardrail**: daemon now exits cleanly with instructions if run before `onboard` completes (`AppConfig.Onboarded()` check in `main.go`).
- **Telegram token validation**: onboarding wizard calls `getMe` API to verify bot tokens before saving config — catches invalid tokens immediately instead of failing at daemon startup.
- **Internationalization (i18n)**: new `internal/i18n/` package with Portuguese (pt-BR) default and English fallback. All user-facing Telegram messages now use the i18n bundle.
- **Linux systemd support**: `scripts/aurelia.service.tmpl` and `scripts/install-systemd.sh` for user-mode systemd installation. `Makefile` auto-detects OS (`install-service` works on both macOS and Linux).
- **Onboarding testability**: `validateToken` is overridable in tests to avoid real HTTP calls during onboarding unit tests.
- **Local models support**: Ollama provider added to onboarding wizard and configuration. README now includes a "Local Models" section with setup instructions for Ollama and OpenAI-compatible local inference servers.

### Changed
- `README.md` restructured with Prerequisites section, improved Quick Start flow, Linux service instructions, and Troubleshooting table.
- `internal/telegram/messages.go` migrated from hardcoded Portuguese constants to i18n-backed functions.
- **Provider rename**: "kilo" renamed to "opencode-go" throughout the codebase — provider ID, API key field, config migration, and onboarding UI all updated.
- **Documentation clarity**: README and onboarding wizard now explicitly state that the PI SDK installs automatically via npm on first run — no manual PI CLI installation required.
- **PI CLI isolation**: Aurelia now uses its own PI agent directory (`~/.aurelia/pi-agent/`) instead of sharing `~/.pi/agent/` with PI CLI. On first run, existing PI CLI auth/models config is automatically copied to the isolated directory. Credential conflicts between Aurelia and PI CLI are eliminated.

### Fixed
- **UX**: running daemon without onboarding produced cryptic Telegram API errors — now shows friendly step-by-step instructions.
- **UX**: invalid Telegram tokens were only discovered at runtime — now caught during onboarding wizard.
- **Reliability**: bridge setup now creates `~/.aurelia/pi-agent/` directory (instead of `~/.pi/agent/`) to ensure PI SDK has an isolated writable agent directory even when the user has never installed the PI CLI.

## [v0.6.9] - 2026-05-17

### Fixed
- **Security**: path traversal em `downloadTelegramFile` — sanitiza `filename` com `filepath.Base()` antes de `os.TempDir()`.
- **Crash**: panic não recuperado em `pipeline.processRun` goroutine — adiciona `recover()` com log.
- **Crash**: panic não recuperado em `orchestrator.ExecutePlan` worker goroutine — adiciona `recover()` que loga task ID e registra resultado falho.
- **Deadlock**: `cron.WithTx` sem `defer tx.Rollback()` — transação vazava conexão SQLite em panic, deadlockando o scheduler.
- **Hang**: `bridge.Stop()` esperava `<-done` sem timeout — adiciona 10s timeout antes de forçar kill.
- **Race**: `memoryCache.get()` validava mtimes fora do lock e retornava conteúdo stale se `invalidate()` deletasse a entrada no meio.
- **Leak**: erros de `worktree.Merge` e `worktree.Cleanup` eram descartados com `_` — agora logados explicitamente.
- **Data loss**: `dreamer.run()` zerava o turn counter no fim, perdendo turns que chegaram durante o dream — agora subtrai apenas os turns consumidos via CAS.
- **Logic**: `tryExecutePlan` retornava `OutcomeSuccess` sem chamar `afterSuccessfulTurn`, pulando dreamer update e memory invalidation.
- **Reliability**: `cron.scheduler.Start()` morria no primeiro erro do SQLite — agora loga e continua o loop.
- **Burst**: `computeNextRun` usava `now` (início do poll) em vez de `finishedAt` — jobs longos causavam reexecução imediata.
- **Resilience**: `agents.Load` abortava todo o registro no primeiro arquivo `.md` malformado — agora loga e pula o arquivo.
- **Thundering herd**: `getModels` tinha race no cache expiry — múltiplas goroutines batiam no bridge simultaneamente; agora o lock cobre toda a operação.
- **Silent errors**: `json.Unmarshal` no bridge, `os.Getwd` em `app.go` e `bot.go`, `os.UserHomeDir` em `app.go` — todos agora tratados ou logados.
- **Timeout**: `cmdCronCreate` usava `context.Background()` sem deadline para SQLite — agora usa 30s timeout.
- **Cleanup**: `worktree.Cleanup` não tentava deletar o branch se `git worktree remove` falhasse — agora tenta sempre.
- **Crash**: `onNotify` callbacks em `resilient_bridge.go` sem `recover()` — panic no output layer matava o daemon.

## [v0.6.8] - 2026-05-16

### Added
- `internal/telegram/cron_fast_parse.go` — regex parser for the common scheduling phrasings (`todo dia às Nh ...`, `toda <weekday> às Nh ...`, `amanhã às Nh ...`, `hoje às Nh ...`, `daqui N min ...`, `em N horas ...`). Bypasses the LLM round-trip in ~70% of cron creates — saves 1-3s and ~$0.001 per scheduled reminder.
- `BridgeCronRuntime` now injects scheduling instructions and global memory into the system prompt — cron-spawned agents can create follow-up jobs and have continuity across runs (parity with the Telegram pipeline).
- `BridgeCronRuntime.SetExePath()` so cron-injected CLI commands reference the real binary path.
- Album partial-success messages — when N of M photos fail to download or encode, the user gets a concrete `"⚠️ Consegui processar apenas X de Y imagens"` instead of silent log lines.
- `AppConfig.DiskScanEnabled` — opt-in flag for the disk-walking project auto-detection fallback.
- `collectPhotoAttachments` helper consolidating the album/single-photo download+encode loop.

### Changed
- `cmdCronCreate` tries `cronFastParse` before paying the LLM round-trip; falls through gracefully when the message doesn't match a supported pattern.
- `helpMessage` now documents cancel/supersede/status during processing and CWD inheritance between forum topics.
- `splitTelegramMarkdown` rune handling rewritten — converts to `[]rune` once and slices via rune index instead of re-decoding the tail per chunk (was O(n²) on long replies).
- `scanForProject` disk walk now gated by `DiskScanEnabled` (default false) — removes up to 3s of latency on the first message of a session. Project index and memory-file lookup still run.
- `sendProviderMenu` send arguments reordered so the inline keyboard markup is applied after send options (pre-existing fix in the working tree).

### Fixed
- N/A (no bug fixes in this release; all changes are quality-of-life improvements).

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
