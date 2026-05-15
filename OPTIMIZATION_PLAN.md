# Optimization & Hardening Plan

Materialização da análise geral feita em 2026-05-14. Cobre 34 tarefas distribuídas em 4 fases.

- **Fase 1** — Quick wins seguros e isolados (1-2h cada).
- **Fase 2** — Melhorias médias com testes de regressão (meio dia cada).
- **Fase 3** — Refatorações estruturais (1-2 dias cada).
- **Fase 4** — Polimento / qualidade de código.

**Como usar este documento:** cada tarefa é independente salvo onde marcado. Executar em ordem de fase é o ideal, mas tarefas dentro da mesma fase podem ser paralelizadas (cada uma toca pacotes distintos). Marcar `[x]` em "Done when" ao concluir.

**Antes de qualquer tarefa:**
```bash
git status                  # branch limpo
go build ./...              # baseline compila
go test ./... -short        # baseline verde
go vet ./...                # baseline sem warnings
```

**Convenção de commits:** seguir `type(scope): description` do CLAUDE.md. Cada tarefa = 1 commit. Bump de versão + entrada em `CHANGELOG.md` deve ser aprovado pelo Igor antes do commit final que vai para `main`.

---

## Fase 1 — Quick Wins (alto valor, baixo risco)

### T1: Limpar arquivos temporários de fotos após upload  🔴

**Por quê**: `handlePhoto` e `handleImageDocument` baixam arquivos para `os.TempDir()` mas nunca removem. Em uso intenso enche o disco. Voz e documento já têm `defer os.Remove`.

**Onde**: `internal/telegram/input.go` (linhas ~75-86 em `processPhotoInput`, ~144-174 em `handleImageDocument`).

**Como**:
1. Em `processPhotoInput`, após `for _, p := range photos` adicionar slice `downloaded []string` e `defer` que itera removendo.
2. Em `handleImageDocument`, adicionar `defer os.Remove(filePath)` logo após `downloadTelegramFile`.
3. Garantir que o `defer` roda **depois** que `processInputWithImages` retorna (a chamada é síncrona — base64 já foi feita).

**Done when**:
- [x] `defer` adicionado nos dois caminhos.
- [x] Teste manual: enviar foto, conferir que arquivo `photo_<id>.jpg` não fica em `$TMPDIR`.
- [x] `go test ./internal/telegram/... -short` verde.

**Risco**: muito baixo. Atenção: a base64 é feita em `encodeImageAttachment` antes do `processInputWithImages` retornar — então remover depois é seguro.

---

### T2: TTL nos álbuns de fotos  🔴

**Por quê**: `albumBuffer.pending` nunca é limpo se o owner do álbum não chegar (whitelist bloqueia, request abortada). Memory leak silencioso.

**Onde**: `internal/telegram/input.go:238-269` (`albumBuffer.store`, `flush`).

**Como**:
1. Adicionar `time.AfterFunc(5*time.Minute, func(){ ab.gcExpired(albumID) })` dentro de `store` quando criar entrada nova.
2. Implementar `gcExpired(albumID string)`: lock, `delete(ab.pending, albumID)` se ainda presente.
3. Substituir `time.Sleep(900*time.Millisecond)` em `handlePhotoAlbum` (input.go:47) por um agendamento mais limpo — ver T3.

**Done when**:
- [x] Timer registrado por álbum criado.
- [x] Teste em `input_test.go`: criar álbum sem owner, esperar TTL, verificar `pending` vazio.
- [x] Logar quando GC remove órfão (`log.Printf("album: gc orphan %s", albumID)`).

**Risco**: baixo. Não invalidar o timer manualmente — `delete` no caminho normal já evita o GC operar em algo inexistente.

---

### T3: Substituir `time.Sleep(900ms)` por timer de flush  🟡

**Por quê**: handler do telebot fica parado 900ms por foto — bloqueia goroutine do bot, atrasa outras mensagens. Em álbum de 10 fotos, isso é 10 goroutines paradas.

**Onde**: `internal/telegram/input.go:40-54` (`handlePhotoAlbum`).

**Como**:
1. Em `store()`, quando primeira foto do álbum chega, agendar `time.AfterFunc(900ms, func(){ flushAndProcess(albumID) })`.
2. `flushAndProcess` chama `flush()` e, se ok, `processPhotoInput` num contexto isolado (sem o `telebot.Context` original — precisará armazenar `chat`, `threadID`, `senderID` no `pendingAlbum`).
3. Handler retorna `nil` imediatamente.
4. Cuidado: `telebot.Context` não é seguro fora do handler. Capturar só os campos necessários.

**Done when**:
- [x] `time.Sleep` removido.
- [x] Álbum de 5 fotos processa em uma única chamada (testar manualmente).
- [x] Bot responde outras mensagens em paralelo durante o flush window.
- [x] `input_pipeline_test.go` adicionado para fluxo de álbum.

**Risco**: médio — requer extrair contexto do telebot manualmente. Validar com flow real.

**Depende de**: T2 (timer infra já criada).

---

### T4: Whitelist como map + remover log barulhento  🟡

**Por quê**: `whitelistMiddleware` faz O(n) por mensagem e loga "allowed group ID: X (match=…)" para cada grupo em **toda** mensagem. Lixo no log, alocação desnecessária.

**Onde**: `internal/telegram/bot_middleware.go:18-50`, `internal/telegram/bot.go:168-190` (isAllowedUser/Group), `internal/telegram/bot.go:24-53` (struct).

**Como**:
1. Adicionar campos em `BotController`:
   ```go
   allowedUsers  map[int64]struct{}
   allowedGroups map[int64]struct{}
   ```
2. Em `NewBotController`, popular após `bc := &BotController{...}`:
   ```go
   bc.allowedUsers = make(map[int64]struct{}, len(cfg.TelegramAllowedUserIDs))
   for _, id := range cfg.TelegramAllowedUserIDs { bc.allowedUsers[id] = struct{}{} }
   // idem para groups
   ```
3. Reescrever `isAllowedUser`/`isAllowedGroup`: `_, ok := bc.allowedUsers[userID]; return ok`.
4. Remover o `log.Printf("  allowed group ID: %d ...)` (linha ~28). Manter apenas o log no caminho **denegado** (linha ~45).
5. Manter o `log.Printf("whitelist check: ...")` mas apenas se DEBUG=1 (opcional — pode remover de vez).

**Done when**:
- [x] Maps populados no constructor.
- [x] Lookups O(1).
- [x] Logs só aparecem em deny.
- [x] `commands_test.go` ainda verde.

**Risco**: baixo. Atenção: se o config muda em runtime (não acontece hoje), os maps ficariam stale.

---

### T5: SQLite com `busy_timeout` + `synchronous=NORMAL`  🔴

**Por quê**: `cron/store.go:16` abre só com `_journal_mode=WAL`. Sem `busy_timeout`, escritas concorrentes (scheduler + handler /cron) podem retornar `SQLITE_BUSY`. WAL pede `synchronous=NORMAL` para perf+durabilidade balanceada.

**Onde**: `internal/cron/store.go:15-28`.

**Como**:
1. Trocar DSN:
   ```go
   dsn := dbPath + "?_journal_mode=WAL&_busy_timeout=5000&_synchronous=NORMAL&_foreign_keys=ON"
   db, err := sql.Open("sqlite", dsn)
   ```
2. Setar:
   ```go
   db.SetMaxOpenConns(1)  // SQLite tem 1 writer; evita BEGIN IMMEDIATE conflict
   db.SetMaxIdleConns(1)
   db.SetConnMaxLifetime(0)
   ```
3. Verificar se `modernc.org/sqlite` aceita os pragmas via DSN (aceita — sintaxe `_param`).

**Done when**:
- [x] DSN atualizado.
- [x] Pool configurado.
- [x] `go test ./internal/cron/... -v` verde (cobre scheduler concorrente).
- [x] Teste manual: criar 10 jobs em paralelo via /cron, verificar zero erros de BUSY.

**Risco**: baixo. `SetMaxOpenConns(1)` serializa escritas mas leituras concorrentes ainda funcionam com WAL.

---

## Fase 2 — Melhorias médias

### T6: Cache de leitura de memória por mtime  🟡

**Por quê**: `buildSystemPrompt` lê o disco em cada turn: global + topic + project private + project team. Em chats ativos com memória grande, são dezenas de `ReadFile`/mensagem.

**Onde**: `internal/telegram/input_pipeline.go:994-1029` (`loadMemoryDir`).

**Como**:
1. Criar struct `memoryCache` em `input_pipeline.go` (ou novo `memory_cache.go`):
   ```go
   type memoryCache struct {
       mu      sync.RWMutex
       entries map[string]memoryCacheEntry // key = dir path
   }
   type memoryCacheEntry struct {
       content string
       mtimes  map[string]time.Time // filename → mtime
   }
   ```
2. Adicionar `*memoryCache` em `BotController`, inicializar no constructor.
3. `loadMemoryDir` consulta cache: se mtimes de todos os arquivos do dir batem, retorna `content` cached. Senão, relê tudo e atualiza.
4. Implementar `cache.invalidate(dir)` chamado em `handleCwdCommand` e em rotas que modificam memória (nudge/dream).

**Done when**:
- [x] Cache implementado e usado em `loadMemoryDir`.
- [x] Teste: chamar 100x com mesmo mtime, conferir só 1 `ReadDir` real (usar instrumentação ou mock).
- [x] Invalidação manual exposta para nudge/dream chamarem.

**Risco**: médio. Race: nudge escreve arquivo enquanto handler lê — cache pode retornar stale por uma chamada. Aceitável.

---

### T7: Buffer maior + log de drops no bridge readLoop  🔴

**Por quê**: `bridge.go:160-165` dropa eventos silenciosamente quando o canal está cheio. Chunks `assistant` perdidos = resposta truncada sem aviso.

**Onde**: `internal/bridge/bridge.go:16, 134-196`.

**Como**:
1. Aumentar `eventChannelBuffer` de 16 → 128 (memória trivial).
2. No select não-bloqueante (linha 161-165), trocar `default` por log de severidade:
   ```go
   select {
   case ch <- ev:
   default:
       log.Printf("bridge: dropped event type=%s rid=%s — consumer too slow", ev.Type, rid)
   }
   ```
3. Adicionar métrica simples: contador atômico `b.droppedEvents atomic.Uint64`, exposto via método `DroppedEvents()`.

**Done when**:
- [x] Buffer ampliado.
- [x] Drops logados.
- [x] Contador exposto.
- [x] `bridge_test.go` adicionado: stream rápido com consumer lento, verificar drops > 0 e log.

**Risco**: baixo. Aumentar buffer apenas posterga o problema — log torna visível.

**Depende de**: nenhum.

---

### T8: Trocar `bufio.Scanner` por `bufio.Reader` no readLoop  🔴

**Por quê**: `bridge.go:122` cap de 1 MB silenciosamente mata o reader em eventos grandes (resultado de `Read` de arquivo grande, output de comando). Dispara `onDeath` sem causa óbvia.

**Onde**: `internal/bridge/bridge.go:121, 134-196`.

**Como**:
1. Trocar:
   ```go
   b.scanner = bufio.NewScanner(stdoutPipe)
   b.scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
   ```
   por:
   ```go
   b.reader = bufio.NewReaderSize(stdoutPipe, 64*1024)
   ```
2. Reescrever `readLoop`: substituir `for b.scanner.Scan()` por loop com `b.reader.ReadBytes('\n')`. Tratar `io.EOF` como sair limpo; outros erros como morte.
3. Remover campo `scanner`, adicionar `reader *bufio.Reader`.

**Done when**:
- [x] Scanner removido.
- [x] Teste: enviar evento simulado de 5 MB via stdin, confirmar parse correto.
- [x] `bridge_test.go` existente continua verde.

**Risco**: médio — mexe no hot path. Cobrir com teste antes de mergear.

**Depende de**: T7 (mexem no mesmo arquivo; melhor um após o outro, no mesmo PR ou commits sequenciais).

---

### T9: Separar tokens reais de tokens estimados no Tracker  🟡

**Por quê**: `tracker.go:67-68` empurra estimativa de `numTurns*3000` para `InputTokens` quando não há tokens reais. `/usage` mostra números misturados/falsos.

**Onde**: `internal/session/tracker.go`.

**Como**:
1. Adicionar campo em `Usage`: `EstimatedTokens int` (separado de Input/Output).
2. `RecordUsage` no fallback: incrementar `EstimatedTokens`, não `InputTokens`.
3. `TotalTokens()` retorna `max(InputTokens+OutputTokens, EstimatedTokens)` — usa o maior para gate de auto-reset.
4. `String()` formata só os reais, mostra "estimado: N" entre parênteses se aplicável.

**Done when**:
- [x] Campo adicionado.
- [x] Lógica atualizada.
- [x] `tracker_test.go` cobre cenários: só real, só estimado, misto.
- [x] `/usage` no Telegram mostra valores corretos.

**Risco**: baixo. Quebra nada externo — campo novo é aditivo.

---

### T10: TTL/GC para `session.Store`  🟡

**Por quê**: maps `sessions` e `cwds` crescem indefinidamente. Bot em grupos públicos vira leak.

**Onde**: `internal/session/store.go`.

**Como**:
1. Adicionar campo `lastSeen time.Time` em `entry`.
2. Atualizar `lastSeen = time.Now()` em todo `Set`/`GetWithState` (cuidado: `GetWithState` é RLock; precisa upgrade ou uma versão de write).
3. Adicionar método `GC(maxAge time.Duration)` que percorre e remove entries antigas.
4. No `cmd/aurelia/app.go`, iniciar goroutine periódica:
   ```go
   go func() {
       ticker := time.NewTicker(1 * time.Hour)
       defer ticker.Stop()
       for { select {
           case <-ctx.Done(): return
           case <-ticker.C: sessions.GC(7 * 24 * time.Hour)
       }}
   }()
   ```
5. Tornar `maxAge` configurável via `AppConfig.SessionTTLHours` (default 168h).

**Done when**:
- [x] Campo `lastSeen` adicionado.
- [x] `GC` implementado e testado.
- [x] Goroutine iniciada no bootstrap.
- [x] Config opcional aceita.

**Risco**: baixo. Atenção: `Get`/`GetWithState` viram write-paths se atualizam `lastSeen` no RLock — usar `time.Now()` atômico ou aceitar atualizar só em `Set`.

---

### T11: Índice de projetos cacheado  🟡

**Por quê**: `scanForProject` percorre `$HOME` até depth=4 a cada turn de sessão nova. Lento e desperdício.

**Onde**: `internal/telegram/input_pipeline.go:638-733` (`scanForProject`), `internal/runtime/`.

**Como**:
1. Criar `internal/runtime/projects_index.go` com:
   ```go
   type ProjectIndex struct{
       mu sync.RWMutex
       projects map[string]string // name → abs path
       lastBuilt time.Time
   }
   func (p *ProjectIndex) Lookup(name string) string
   func (p *ProjectIndex) Rebuild(ctx context.Context) error
   ```
2. `Rebuild` faz o walk atual (uma vez), persiste em `~/.aurelia/projects.json`.
3. Carregar do disco no startup; rebuild em background a cada 6h e quando `lookup` falha (com debounce).
4. Em `input_pipeline.go`, `scanForProject` consulta `bc.projectIndex.Lookup` primeiro.
5. Manter o walk síncrono como fallback se o index não tiver o projeto e o usuário forçou.

**Done when**:
- [x] Index implementado e persistido.
- [x] Lookups < 1ms.
- [x] Rebuild não bloqueia handler.
- [x] Teste cobre miss → rebuild → hit.

**Risco**: médio — nova superfície de estado. Boa cobertura de teste é essencial.

---

### T12: Limite de tamanho no system prompt (memory)  🟡

**Por quê**: `loadMemoryContents` despeja todos os arquivos sem teto. Token cost cresce indefinidamente.

**Onde**: `internal/telegram/input_pipeline.go:952-992` (`loadMemoryContents`, `loadMemoryDir`).

**Como**:
1. Adicionar constantes:
   ```go
   const maxMemoryFileChars = 8000     // por arquivo
   const maxMemoryTotalChars = 40000   // por system prompt
   ```
2. Em `loadMemoryDir`, truncar arquivos > `maxMemoryFileChars` com sufixo `\n\n[...truncado, ver arquivo completo via Read tool]`.
3. Em `loadMemoryContents`, somar e parar quando exceder `maxMemoryTotalChars`, marcando arquivos não incluídos.
4. Logar uma vez quando trunca (não em loop).
5. Tornar configurável via `AppConfig.MaxMemoryChars`.

**Done when**:
- [x] Limites aplicados.
- [x] Teste: 20 arquivos de 5000 chars → conteúdo ≤ 40000.
- [x] Marcação clara no prompt do que foi truncado.

**Risco**: baixo. O modelo perde acesso "passivo" a arquivos truncados, mas pode lê-los sob demanda.

---

## Fase 3 — Refatorações estruturais

### T13: Dividir `input_pipeline.go` (1089 linhas)  🟡

**Por quê**: arquivo virou God file misturando 6 responsabilidades. Difícil revisar e testar.

**Onde**: `internal/telegram/input_pipeline.go`.

**Como** (dividir em):
1. `pipeline.go` — `processInput`, `processInputWithImages`, `executeAsync`, `processBridgeEvents*`, `routeAgent`, `classifyFunc`, `buildBridgeRequest` (~300 linhas).
2. `bridge_failure.go` — `bridgeFailureTracker` + constantes (~50 linhas).
3. `prompt_builder.go` — `buildSystemPrompt`, `buildMemoryInstructions`, `buildCronInstructions`, `buildTelegramInstructions`, `buildProjectDocsSection`, `loadMemoryContents`, `loadMemoryDir` (~400 linhas).
4. `project_detect.go` — `detectProjectPath`, `detectFromMemoryFiles`, `scanForProject`, `looksLikeProjectName`, `isStopWord`, `skipDirs`, `stopWords`, `extractFrontmatterField` (~200 linhas).
5. `planning_intent.go` — `looksLikePlanningIntent`, `planningKeywords` (~30 linhas).

**Done when**:
- [x] Arquivos criados, funções movidas com `git mv` (mesmo package — não muda imports externos).
- [x] `go build ./...` verde.
- [x] Todos os testes existentes passam sem alteração.
- [x] Nenhum arquivo novo > 500 linhas.

**Risco**: baixo se for puramente movimentação. Atenção: não alterar lógica no mesmo commit — split puro primeiro, refatorações depois.

---

### T14: Extrair `PipelineService` do `BotController`  🟡

**Por quê**: `BotController` carrega 20+ dependências e mistura "controller HTTP" (telebot routing) com lógica de negócio (executar bridge, gerenciar sessão).

**Onde**: `internal/telegram/bot.go`, `internal/telegram/pipeline.go` (após T13).

**Como**:
1. Criar `internal/pipeline/` package novo.
2. `pipeline.Service` recebe no constructor: `bridge`, `agents`, `persona`, `sessions`, `tracker`, `dreamer`, `orchestrator`, `resolver`, `memoryDir`, `config`.
3. Move métodos: `executeAsync`, `processBridgeEvents*`, `buildSystemPrompt`, `buildBridgeRequest`, `routeAgent`, `classifyFunc`.
4. `BotController` mantém só I/O do Telegram + dispatch para `pipelineService.Process(ctx, input)`.
5. Atualizar `cmd/aurelia/app.go` para construir Service antes do Controller.

**Done when**:
- [x] `internal/pipeline/` criado.
- [x] `BotController` reduzido a ~400 linhas (de ~700+).
- [x] Testes adaptados ou movidos.
- [x] Build + testes verdes.

**Risco**: alto — toca em muitos arquivos. Fazer em PR isolado, depois do T13.

**Depende de**: T13.

---

### T15: Bundle TS fora do repositório  🟡

**Por quê**: `internal/bridge/bundle.js` tem 12 MB e está versionado. Cada PR que toca `bridge/index.ts` infla o git.

**Onde**: `internal/bridge/embed.go`, `internal/bridge/setup.go`, `bridge/`, `Makefile` ou `bridge/package.json` scripts.

**Como** (opção A — rebuild on install):
1. Remover `bundle.js` do repo (`git rm internal/bridge/bundle.js`).
2. Adicionar `internal/bridge/bundle.js` ao `.gitignore`.
3. Em `embed.go`, mudar `//go:embed bundle.js` para um fallback: se arquivo não existe, embed vazio + setup roda `npm install && npm run build` no first run.
4. `cmd/aurelia/app.go:setupBridge` já tem `EnsureBridge` — estender para rodar build se bundle.js ausente.
5. CI: adicionar step de build TS antes de `go build`.

**Como** (opção B — git-lfs):
1. Configurar `.gitattributes` para `*.js filter=lfs`.
2. Migrar histórico (opcional, custo alto).
3. Documentar em README.

**Recomendação**: opção A é mais limpa. Opção B preserva offline-install.

**Done when**:
- [x] Bundle removido do repo (opção A) ou em LFS (opção B).
- [x] Build local funciona após `git clone`.
- [x] CI verde.
- [x] README atualizado.

**Risco**: médio — quebra workflows de quem clona sem Node. Comunicar mudança.

---

### T16: Migrar `log` para `log/slog`  🟢

**Por quê**: 122 ocorrências de `log.Printf` sem níveis nem estrutura. Difícil filtrar em prod.

**Onde**: todos os arquivos em `internal/` e `cmd/`.

**Como**:
1. Adicionar config em `AppConfig`: `LogLevel string` (debug/info/warn/error), `LogFormat string` (text/json).
2. No `main.go`, configurar `slog.SetDefault` com handler text ou JSON.
3. Helper script ou regex para migração em massa:
   - `log.Printf("foo: %s", x)` → `slog.Info("foo", "x", x)` (manual; alguns precisam virar `Warn`/`Error`).
4. Fazer pacote por pacote em PRs separados (não tudo de uma vez).
5. Manter `log.Fatalf` no startup (queremos crashar).

**Done when**:
- [x] `log/slog` integrado.
- [x] Pelo menos `bridge`, `cron`, `session` migrados.
- [x] Níveis adequados (warn para retries, error para falhas terminais).
- [x] JSON em prod testado.

**Risco**: baixo, mas tedioso. Pode ser feito incrementalmente sem urgência.

---

## Fase 4 — Qualidade de código

### T17: Robustez no `parseCronCreateResponse`  🟢

**Por quê**: `commands.go:306-316` quebra com fenced single-line ou variantes.

**Onde**: `internal/telegram/commands.go`.

**Como**: trocar a tira-fence manual por regex:
```go
var fenceRe = regexp.MustCompile("(?s)^```(?:json)?\\s*(.+?)\\s*```$")
if m := fenceRe.FindStringSubmatch(strings.TrimSpace(raw)); m != nil {
    cleaned = m[1]
}
```

**Done when**:
- [x] Regex em uso.
- [x] Testes cobrem: fence multilinha, fence single-line, sem fence, fence sem `json`.

---

### T18: Documentar/assertar reset do builder em `result`  🟢

**Por quê**: `input_pipeline.go:390-393` reseta `assistantText` quando `result.Content != ""`. Lógica frágil se o protocolo do bridge mudar.

**Onde**: `internal/telegram/input_pipeline.go`.

**Como**: adicionar comentário explicando contrato + se `len(prior) > 0 && len(content) > 0 && content != prior`, logar warning de divergência.

---

### T19: Cobrir `extractFrontmatterField` com testes  🟢

**Por quê**: implementação caseira de YAML — pode quebrar com `:` no valor, espaços, etc.

**Onde**: criar `internal/telegram/project_detect_test.go` (após T13).

**Como**: tabela de casos: valor com `:`, sem espaço, com aspas, com newline, sem frontmatter.

---

### T20: Timeout para `detectProjectPath`  🟢

**Por quê**: `os.Stat` em volumes lentos (SMB, externos) pode travar.

**Onde**: `internal/telegram/input_pipeline.go:537-567`.

**Como**: receber `ctx context.Context` com timeout (3s). Cada `os.Stat` em goroutine com select. Simplificar para apenas pular paths que não respondem rápido.

---

### T21: Copiar slice no `bridgeFailureTracker.record`  🟢

**Por quê**: `t.failures = t.failures[start:]` mantém backing array — pequeno leak.

**Onde**: `internal/telegram/input_pipeline.go:233`.

**Como**: trocar por `t.failures = append([]time.Time(nil), t.failures[start:]...)`.

---

### T22: Cache de `ListModels` unificado  🟡

**Por quê**: `cmdListModels`, `cmdSetModel` e `handleModelCommand` chamam `bridge.ListModels` independentemente. Só `handleModelCommand` cacheia.

**Onde**: `internal/telegram/bot_middleware.go:265-268`, `internal/telegram/commands.go:494,557`.

**Como**:
1. Criar `modelCache` com TTL de 5min: struct com `models`, `expiresAt`.
2. Método `bc.getModels(ctx)` consulta cache, refetch se expirado.
3. Substituir as 3 chamadas diretas a `bridge.ListModels` por `bc.getModels`.

**Done when**:
- [x] Cache unificado.
- [x] TTL respeitado.
- [x] Stress test: 100 chamadas → 1 ListModels real.

---

### T23: `handleCwdCommand` não deve chamar `routeAgent` (LLM)  🟡

**Por quê**: `bot_middleware.go:139-142` chama `routeAgent` (que pode rodar classify LLM) só para mostrar painel CWD. Lento e custoso.

**Onde**: `internal/telegram/bot_middleware.go:123-196`.

**Como**: trocar `bc.routeAgent(text)` por `bc.agents.Route(text)` — só prefix match, sem LLM.

**Done when**:
- [x] `Classify` não roda em `/cwd`.
- [x] Painel ainda mostra agente quando `@nome` está presente.

---

### T24: Adapter `ChatSender` em vez de `bot.GetBot()`  🟢

**Por quê**: `cmd/aurelia/app.go:259-262` faz `bot.GetBot()` para criar `telegramChatSender`. Quebra encapsulamento.

**Onde**: `internal/telegram/bot.go`, `cmd/aurelia/app.go`.

**Como**:
1. Adicionar método no `BotController`: `func (bc *BotController) ChatSender() cron.ChatSender`.
2. Remover `GetBot()`.
3. Atualizar callers.

**Done when**:
- [x] `GetBot()` removido.
- [x] Controller controla acesso ao bot interno.

---

### T25: Prompts de nudge/dream para arquivos `.tmpl` embedded  🟢

**Por quê**: `nudge.go:121-207` e `dream.go` têm prompts gigantes em `fmt.Sprintf`. Difícil revisar/i18n.

**Onde**: `internal/dream/`.

**Como**:
1. Criar `internal/dream/prompts/nudge_global.tmpl`, `nudge_project.tmpl`, `dream.tmpl`.
2. Usar `//go:embed prompts/*.tmpl` + `text/template`.
3. Renderizar com dados estruturados.

**Done when**:
- [x] Templates externos.
- [x] Testes cobrem render.

---

### T26: Onboarding para `internal/onboarding/`  🟢

**Por quê**: `cmd/aurelia/onboard*.go` ocupa ~1500 linhas no package `main` com lógica de UI.

**Onde**: `cmd/aurelia/onboard*.go` → `internal/onboarding/`.

**Como**: mover arquivos preservando exports necessários para `main` chamar.

**Risco**: baixo se for só `git mv` + ajustar package + exports.

---

### T27: Não `log.Fatalf` em `deps.Check` no startup  🟢

**Por quê**: `app.go:75-79` mata o processo. Hostil para testes e2e que embarcam o app.

**Onde**: `cmd/aurelia/app.go:72-83`.

**Como**: bootstrapApp retorna erro de deps em vez de `log.Fatalf`. `main.go` decide se aborta.

---

### T28: `Bridge.Stop` checa `ProcessState` antes de `Kill`  🟢

**Por quê**: `bridge.go:223-226` chama `Kill` mesmo se o processo já saiu limpo.

**Onde**: `internal/bridge/bridge.go`.

**Como**: `if cmd.ProcessState == nil || !cmd.ProcessState.Exited() { _ = cmd.Process.Kill() }`.

---

### T29: Escapar / validar input em `ResolveJobID`  🟡

**Por quê**: `cron/store_jobs.go:100` usa `LIKE prefix+"%"`. UUIDs hoje são seguros, mas `extractLastWord(text)` pode passar string arbitrária.

**Onde**: `internal/cron/store_jobs.go`, `internal/telegram/commands.go:238-249`.

**Como**:
1. Em `cmdCronCancel`, validar `looksLikeJobID` (já existe) antes de chamar service.
2. Em `ResolveJobID`, rejeitar prefix com `%` ou `_`:
   ```go
   if strings.ContainsAny(prefix, "%_") {
       return "", fmt.Errorf("invalid job id prefix")
   }
   ```

**Done when**:
- [x] Validação dupla (chamador + store).
- [x] Teste cobre input malicioso.

---

### T30: Despachar `SetOnDeath` callback em goroutine  🟢

**Por quê**: `bridge.go:180-182` executa callback sincronamente. Callback bloqueador atrasa cleanup.

**Onde**: `internal/bridge/bridge.go:178-182`.

**Como**: `if !stopping && cb != nil { go cb() }`.

---

### T31: Configuração de tamanho de imagem  🟡

**Por quê**: `processPhotoInput` base64-encode imagens sem limite. Imagens grandes inflam memória.

**Onde**: `internal/telegram/input.go:73-86`, `input.go:91-104`.

**Como**:
1. Adicionar `AppConfig.MaxImageBytes` (default 10 MB).
2. Em `encodeImageAttachment`, ler tamanho primeiro, rejeitar/redimensionar se exceder.
3. Para redimensionar: lib `image/jpeg` + `golang.org/x/image/draw` — opcional.

**Done when**:
- [x] Limite aplicado.
- [x] Mensagem clara ao usuário se rejeitar.

---

### T32: Cache normalizado em `ProviderConfig`  🟢

**Por quê**: `config.go:92-116` normaliza nome do provider em cada lookup.

**Onde**: `internal/config/config.go`.

**Como**: normalizar uma vez em `toAppConfig`, salvar map com keys já normalizadas.

---

### T33: Validar `go.mod` `golang.org/x/exp` versão futura  🟢

**Por quê**: `go.mod` cita `v0.0.0-20260312153236-...` — datestamp futuro suspeito.

**Onde**: `go.mod`.

**Como**: rodar `go mod tidy` + `go list -m -u all` para conferir. Se for legítimo (CI gerou), documentar; senão, downgrade.

---

### T34: Linter wired up em CI  🟢

**Por quê**: `.golangci.yml` existe (246 bytes) mas não há evidência de CI rodando.

**Onde**: `.github/workflows/`.

**Como**:
1. Adicionar workflow `.github/workflows/lint.yml` rodando `golangci-lint run`.
2. Habilitar pelo menos: `staticcheck`, `govet`, `errcheck`, `ineffassign`, `unused`.
3. Corrigir warnings iniciais.

**Done when**:
- [x] CI verde.
- [x] Warnings críticos resolvidos.

---

## Resumo / matriz de execução

| Fase | Tarefas | Concluídas | Pendentes |
|------|---------|-----------|-----------|
| 1 — Quick wins | T1-T5 | 5/5 | — |
| 2 — Médias | T6-T12 | 7/7 | — |
| 3 — Estruturais | T13-T16 | 4/4 | — |
| 4 — Qualidade | T17-T34 | 18/18 | — |
| **Total** | **34** | **34/34** | — |

**Status atual (última atualização: 2026-05-14):**
- ✅ 34 tarefas implementadas.
- ✅ T14 (PipelineService) extraído para `internal/pipeline/`; mudanças locais ainda não commitadas.

**T25** (nudge/dream .tmpl) e **T26** (onboarding move) — ✅ COMPLETOS nestas últimas sessões.

**Atalho recomendado** se tempo limitado: T1, T4, T5, T7, T8 (5 tarefas, ~1 dia, cobrem 80% do risco).

**Bloqueios entre tarefas:**
- T3 depende de T2
- T8 segue T7 (mesmo arquivo)
- T14 depende de T13
- T19 depende de T13 (paths mudam)
- T26 sem dependências (mover puro)

---

## Checklist global pré-merge

Para cada PR:
- [x] Versão bumpada em `internal/version/version.go` (Igor aprova patch/minor/major).
- [x] Entrada em `CHANGELOG.md` (Igor aprova texto).
- [x] `go build ./...` verde.
- [x] `go test ./... -short` verde.
- [x] `go vet ./...` sem warnings.
- [x] Cobertura nova para o caminho alterado.
- [x] Commit message segue Conventional Commits.
