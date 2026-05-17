import { createInterface } from "node:readline";
import { existsSync } from "node:fs";
import { homedir } from "node:os";
import { join } from "node:path";
import {
  AuthStorage,
  createAgentSession,
  DefaultResourceLoader,
  getAgentDir,
  ModelRegistry,
  SessionManager,
  SettingsManager,
} from "@earendil-works/pi-coding-agent";

// ── Types ────────────────────────────────────────────────────────────────────

interface ImageAttachment {
  path?: string;
  data?: string;
  media_type?: string;
}

// Mirrors RequestOptions in internal/bridge/protocol.go. The legacy Claude SDK
// fields (max_turns, permission_mode, mcp_servers, agents, disabled_tools)
// have no analogue in the PI SDK and were dropped.
interface RequestOptions {
  provider?: string;
  model?: string;
  cwd?: string;
  system_prompt?: string;
  resume?: string;
  allowed_tools?: string[];
  disallowed_tools?: string[];
  continue?: boolean;
  no_user_settings?: boolean;
  persist_session?: boolean;
  images?: ImageAttachment[];
}

interface Request {
  command: string;
  prompt: string;
  request_id?: string;
  target_request_id?: string;
  options?: RequestOptions;
}

interface OutEvent {
  event: string;
  request_id?: string;
  [key: string]: unknown;
}

interface SessionLookup {
  id: string;
  file?: string;
}

// Bounded LRU-ish cache of session id → session file. The bridge is long-lived
// in daemon mode, so an unbounded map would grow over time. Insertion-ordered
// Map iteration lets us evict the oldest entry once we hit the cap.
const MAX_SESSION_CACHE = 256;
const sessionByID = new Map<string, SessionLookup>();
let lastSessionID = "";

interface ActiveRequest {
  cancel(reason: string): void;
}

const activeRequests = new Map<string, ActiveRequest>();

function rememberSession(id: string, lookup: SessionLookup): void {
  // Touch on update so re-resumed sessions stay warm.
  if (sessionByID.has(id)) sessionByID.delete(id);
  sessionByID.set(id, lookup);
  while (sessionByID.size > MAX_SESSION_CACHE) {
    const oldest = sessionByID.keys().next().value;
    if (oldest === undefined) break;
    sessionByID.delete(oldest);
  }
}

// ── Helpers ──────────────────────────────────────────────────────────────────

function emit(obj: OutEvent): void {
  process.stdout.write(JSON.stringify(obj) + "\n");
}

function log(msg: string): void {
  process.stderr.write(`[bridge] ${msg}\n`);
}

function piAgentDir(): string {
  return process.env.PI_CODING_AGENT_DIR || join(homedir(), ".pi", "agent");
}

function mapProvider(provider: string | undefined): string | undefined {
  if (!provider) return undefined;
  const normalized = provider.trim().toLowerCase();
  const aliases: Record<string, string> = {
    kimi: "kimi-coding",
    kilo: "opencode-go",
    alibaba: "opencode-go",
    google: "google",
    anthropic: "anthropic",
    openrouter: "openrouter",
    zai: "zai",
    ollama: "ollama",
  };
  return aliases[normalized] ?? normalized;
}

function mapModelForProvider(provider: string | undefined, model: string): string {
  const normalized = model.trim();
  if (provider === "kimi-coding" && (normalized === "k2.5" || normalized === "kimi-k2.5")) {
    return "kimi-for-coding";
  }
  return normalized;
}

function translateToolName(name: string): string {
  const normalized = name.trim();
  const toolMap: Record<string, string> = {
    Read: "read",
    Write: "write",
    Edit: "edit",
    Bash: "bash",
    Grep: "grep",
    Glob: "find",
    LS: "ls",
    List: "ls",
    WebSearch: "web_search",
    WebSearchPremium: "web_search_premium",
    WebFetch: "web_search",
  };
  return toolMap[normalized] ?? normalized.toLowerCase();
}

// Full list of PI SDK built-in tools for denylist-only mode.
const allBuiltinTools = [
  "read", "write", "edit", "bash", "grep", "find", "ls",
  "web_search", "web_search_premium",
];

function translateAllowedTools(
  allowed: string[] | undefined,
  disallowed: string[] | undefined,
): string[] | undefined {
  const hasRestriction = (allowed && allowed.length > 0) || (disallowed && disallowed.length > 0);

  // Start with allowed list (or all built-ins if none specified)
  let result: string[] | undefined;
  if (allowed && allowed.length > 0) {
    result = [...new Set(allowed.map(translateToolName))];
  }

  // Apply denylist
  if (disallowed && disallowed.length > 0) {
    const denied = new Set(disallowed.map(translateToolName));
    if (result) {
      result = result.filter((t) => !denied.has(t));
    } else {
      // No allowlist: denylist means "all except these"
      result = allBuiltinTools.filter((t) => !denied.has(t));
    }
  }

  if (!result || result.length === 0) {
    // If the user explicitly restricted tools and the result is empty,
    // return an empty array so the PI SDK receives no tools instead of
    // falling back to all defaults.
    return hasRestriction ? [] : undefined;
  }
  return result;
}

function textFromContent(content: unknown): string {
  if (!Array.isArray(content)) return "";
  return content
    .map((item) => {
      if (typeof item !== "object" || item === null) return "";
      const block = item as Record<string, unknown>;
      if (block.type !== "text") return "";
      return typeof block.text === "string" ? block.text : "";
    })
    .join("");
}

function resolveModelFromRegistry(
  registry: ModelRegistry,
  provider: string | undefined,
  modelID: string | undefined,
) {
  if (!modelID) return undefined;

  const allModels = registry.getAll();
  const mappedProvider = mapProvider(provider);
  const mappedModel = mapModelForProvider(mappedProvider, modelID);

  if (mappedProvider) {
    const direct = registry.find(mappedProvider, mappedModel);
    if (direct) return direct;
  }

  const canonical = allModels.find(
    (model) => `${model.provider}/${model.id}`.toLowerCase() === mappedModel.toLowerCase(),
  );
  if (canonical) return canonical;

  const exactIDMatches = allModels.filter((model) => model.id.toLowerCase() === mappedModel.toLowerCase());
  const configuredExact = exactIDMatches.find((model) => registry.hasConfiguredAuth(model));
  if (configuredExact) return configuredExact;
  if (exactIDMatches.length === 1) return exactIDMatches[0];

  if (mappedModel.includes("/") && !mappedProvider) {
    const [maybeProvider, ...rest] = mappedModel.split("/");
    const inferredProvider = mapProvider(maybeProvider);
    const inferredModel = rest.join("/");
    const inferred = registry.find(inferredProvider ?? maybeProvider, inferredModel);
    if (inferred) return inferred;
  }

  const fuzzy = allModels.filter(
    (model) =>
      model.id.toLowerCase().includes(mappedModel.toLowerCase()) ||
      model.name?.toLowerCase().includes(mappedModel.toLowerCase()),
  );
  const configuredFuzzy = fuzzy.find((model) => registry.hasConfiguredAuth(model));
  return configuredFuzzy ?? fuzzy[0];
}

async function resolveSessionManager(opts: RequestOptions | undefined): Promise<SessionManager> {
  const cwd = opts?.cwd || process.cwd();
  if (opts?.persist_session === false) {
    return SessionManager.inMemory(cwd);
  }

  const target = opts?.resume || (opts?.continue ? lastSessionID : "");
  if (target) {
    const cached = sessionByID.get(target);
    if (cached?.file && existsSync(cached.file)) {
      return SessionManager.open(cached.file, undefined, cwd);
    }

    if (existsSync(target)) {
      return SessionManager.open(target, undefined, cwd);
    }

    const sessions = await SessionManager.listAll();
    const match = sessions.find((session) => session.id === target || session.id.startsWith(target));
    if (match) {
      return SessionManager.open(match.path, undefined, cwd);
    }

    log(`session not found for resume=${target}; starting a new session`);
  }

  return SessionManager.create(cwd);
}

async function createPiSession(opts: RequestOptions | undefined) {
  const cwd = opts?.cwd || process.cwd();
  const agentDir = piAgentDir() || getAgentDir();
  const settingsManager = opts?.no_user_settings
    ? SettingsManager.inMemory({
        compaction: { enabled: false },
        retry: { enabled: true, maxRetries: 2 },
      })
    : SettingsManager.create(cwd, agentDir);

  const authStorage = AuthStorage.create(join(agentDir, "auth.json"));
  const modelRegistry = ModelRegistry.create(authStorage, join(agentDir, "models.json"));
  const model = resolveModelFromRegistry(modelRegistry, opts?.provider, opts?.model);
  if (opts?.model && !model) {
    log(`model not found in PI registry: provider=${opts.provider ?? ""} model=${opts.model}`);
  }

  const resourceLoader = new DefaultResourceLoader({
    cwd,
    agentDir,
    settingsManager,
    noContextFiles: true,
    noExtensions: opts?.no_user_settings ?? false,
    noSkills: opts?.no_user_settings ?? false,
    noPromptTemplates: opts?.no_user_settings ?? false,
    noThemes: true,
    systemPromptOverride: () => opts?.system_prompt || undefined,
  });
  await resourceLoader.reload();

  const sessionManager = await resolveSessionManager(opts);
  return createAgentSession({
    cwd,
    agentDir,
    authStorage,
    modelRegistry,
    model,
    resourceLoader,
    sessionManager,
    settingsManager,
    tools: translateAllowedTools(opts?.allowed_tools, opts?.disallowed_tools),
  });
}

// ── Handle a single query command ────────────────────────────────────────────

async function handleQuery(req: Request): Promise<void> {
  const reqId = req.request_id || "";
  const opts = req.options;
  const emitReq = (obj: OutEvent) => emit({ ...obj, request_id: reqId });

  log(
    `query start — rid=${reqId} provider=${opts?.provider ?? "default"} model=${opts?.model ?? "default"} resume=${opts?.resume ?? "none"} prompt="${req.prompt.slice(0, 80)}..."`,
  );

  const timeoutMs = 10 * 60 * 1000;
  let timeout: ReturnType<typeof setTimeout> | undefined;
  let canceled = false;
  let terminalEmitted = false;
  let turnCount = 0;
  let session: Awaited<ReturnType<typeof createPiSession>>["session"] | undefined;
  const startedAt = Date.now();

  const emitTerminalError = (message: string): void => {
    if (terminalEmitted) return;
    terminalEmitted = true;
    emitReq({ event: "error", message });
  };

  const cancelActive = (reason: string): void => {
    canceled = true;
    log(`query cancel — rid=${reqId} reason=${reason}`);
    try {
      session?.dispose();
    } catch (disposeErr) {
      log(`session cancel dispose failed: ${disposeErr instanceof Error ? disposeErr.message : String(disposeErr)}`);
    }
    emitTerminalError(reason);
    activeRequests.delete(reqId);
  };

  activeRequests.set(reqId, { cancel: cancelActive });
  timeout = setTimeout(() => {
    log(`query timeout — rid=${reqId} no result after 10 minutes`);
    cancelActive("query timeout: no result after 10 minutes");
  }, timeoutMs);

  try {
    ({ session } = await createPiSession(opts));
    if (canceled) throw new Error("request canceled");

    const sessionID = session.sessionId;
    lastSessionID = sessionID;
    rememberSession(sessionID, { id: sessionID, file: session.sessionFile });

    emitReq({
      event: "system",
      session_id: sessionID,
      tools: session.getActiveToolNames(),
      model: session.model ? `${session.model.provider}/${session.model.id}` : "",
    });

    const unsubscribe = session.subscribe((event) => {
      if (terminalEmitted) return;
      switch (event.type) {
        case "message_update": {
          const update = event.assistantMessageEvent;
          if (update.type === "text_delta") {
            emitReq({ event: "assistant", text: update.delta });
          }
          break;
        }
        case "tool_execution_start": {
          emitReq({
            event: "tool_use",
            id: event.toolCallId,
            name: event.toolName,
            input: event.args,
          });
          break;
        }
        case "tool_execution_end": {
          emitReq({
            event: "tool_result",
            content: textFromContent(event.result?.content),
          });
          break;
        }
        case "turn_end": {
          turnCount += 1;
          break;
        }
        default:
          break;
      }
    });

    try {
      const images = opts?.images;
      if (images && images.length > 0) {
        const contentBlocks: Record<string, unknown>[] = [{ type: "text", text: req.prompt }];
        for (const img of images) {
          contentBlocks.push({
            type: "image",
            data: img.data,
            mimeType: img.media_type,
          });
        }
        await session.sendUserMessage(contentBlocks);
      } else {
        await session.prompt(req.prompt, { source: "rpc" });
      }
    } finally {
      unsubscribe();
    }

    if (!terminalEmitted) {
      const stats = session.getSessionStats();
      const content = session.getLastAssistantText() ?? "";
      terminalEmitted = true;
      emitReq({
        event: "result",
        content,
        cost_usd: stats.cost,
        session_id: sessionID,
        duration_ms: Date.now() - startedAt,
        num_turns: turnCount || stats.assistantMessages,
        input_tokens: stats.tokens.input,
        output_tokens: stats.tokens.output,
      });
    }
  } catch (err: unknown) {
    if (!terminalEmitted) {
      const errMsg = err instanceof Error ? err.message : String(err);
      log(`query error: rid=${reqId} ${errMsg}`);
      emitTerminalError(errMsg);
    }
  } finally {
    if (timeout) clearTimeout(timeout);
    activeRequests.delete(reqId);
    if (session) {
      try {
        session.dispose();
      } catch (disposeErr) {
        log(`session dispose failed: ${disposeErr instanceof Error ? disposeErr.message : String(disposeErr)}`);
      }
    }
  }
}

// ── Handle incoming request ──────────────────────────────────────────────────

async function handleRequest(line: string): Promise<void> {
  let req: Request;

  try {
    req = JSON.parse(line) as Request;
  } catch {
    emit({ event: "error", message: `invalid JSON: ${line.slice(0, 200)}` });
    return;
  }

  if (!req.command) {
    emit({ event: "error", request_id: req.request_id || "", message: "missing 'command' field" });
    return;
  }

  const reqId = req.request_id || "";
  const emitReq = (obj: OutEvent) => emit({ ...obj, request_id: reqId });

  switch (req.command) {
    case "query": {
      if (!req.prompt) {
        emit({ event: "error", request_id: reqId, message: "missing 'prompt' field for query command" });
        return;
      }
      await handleQuery(req);
      break;
    }

    case "ping": {
      emit({ event: "pong", request_id: reqId });
      break;
    }

    case "cancel": {
      const target = req.target_request_id || "";
      if (!target) {
        emitReq({ event: "error", message: "missing 'target_request_id' for cancel command" });
        return;
      }
      const active = activeRequests.get(target);
      if (!active) {
        emitReq({ event: "result", content: `request ${target} is not active` });
        return;
      }
      active.cancel("request canceled");
      emitReq({ event: "result", content: `request ${target} canceled` });
      break;
    }

    case "list-models": {
      try {
        const agentDir = piAgentDir() || getAgentDir();
        const authStorage = AuthStorage.create(join(agentDir, "auth.json"));
        const modelRegistry = ModelRegistry.create(authStorage, join(agentDir, "models.json"));
        const allModels = modelRegistry.getAll();
        // Only show models from providers that have configured auth
        const available = allModels.filter((m) => modelRegistry.hasConfiguredAuth(m));
        const summary = available.map((m) => ({
          provider: m.provider,
          id: m.id,
          name: m.name ?? m.id,
          supportsImages: m.supportsImageInput ?? false,
        }));
        emitReq({ event: "result", content: JSON.stringify(summary) });
      } catch (err: unknown) {
        const errMsg = err instanceof Error ? err.message : String(err);
        log(`list-models error: ${errMsg}`);
        emitReq({ event: "error", message: `list-models failed: ${errMsg}` });
      }
      break;
    }

    default: {
      emit({ event: "error", request_id: reqId, message: `unknown command: ${req.command}` });
    }
  }
}

// ── Main loop ────────────────────────────────────────────────────────────────

function main(): void {
  log("bridge started — waiting for commands on stdin");

  const rl = createInterface({
    input: process.stdin,
    terminal: false,
  });

  rl.on("line", (line: string) => {
    const trimmed = line.trim();
    if (!trimmed) return;

    handleRequest(trimmed).catch((err: unknown) => {
      const errMsg = err instanceof Error ? err.message : String(err);
      log(`unhandled error in request processing: ${errMsg}`);
      emit({ event: "error", message: `internal bridge error: ${errMsg}` });
    });
  });

  rl.on("close", () => {
    log("stdin closed — shutting down");
    process.exit(0);
  });

  process.on("unhandledRejection", (reason: unknown) => {
    const msg = reason instanceof Error ? reason.message : String(reason);
    log(`unhandled rejection: ${msg}`);
    emit({ event: "error", message: `unhandled rejection: ${msg}` });
  });

  process.on("uncaughtException", (err: Error) => {
    log(`uncaught exception: ${err.message}`);
    emit({ event: "error", message: `uncaught exception: ${err.message}` });
    process.exit(1);
  });
}

main();
