# UX Polish — Design

**Spec**: `.specs/features/ux-polish/spec.md`
**Status**: Draft

---

## Architecture Overview

Todas as melhorias são **apenas na camada de apresentação do Telegram** (`internal/telegram/`). Nenhuma altera a lógica de negócio, o pipeline, o bridge, ou o session store. São ajustes em:
- Mensagens de resposta (strings e formatação)
- Pequenos comportamentos no BotController (reações, escopo de clear)
- Progress reporter (timer, limites)
- Helper functions (formatação de bytes, formatação de status)

```
User Message
    │
    ├─► Ack Reaction (👀) — imediato, antes de qualquer processamento
    │
    ├─► Command Match (/new, /status, /help, /model) — mensagens ajustadas
    │
    ├─► Pipeline Process — se fila, mensagem com posição
    │       │
    │       ├─► Progress Reporter — timer + 8 ferramentas
    │       │
    │       └─► Bridge Events — erros com dicas, retry com tempo
    │
    └─► Output — unidades humanas, status amigável, help rica
```

---

## Code Reuse Analysis

### Existing Components to Leverage

| Component | Location | How to Use |
|-----------|----------|------------|
| `BotController` | `internal/telegram/bot.go` | Adicionar método `ackMessage()`; modificar `handleModelCommand`, `handleResetCommand`, `handleHelpCommand`, `handleStatusCommand` |
| `progressReporter` | `internal/telegram/progress.go` | Adicionar campo `startTime`; aumentar limite de display; prefixar com timer |
| `runSupervisor` | `internal/pipeline/run_supervisor.go` | Expor `queueSize()` e `activeDescription()` para mensagens de fila |
| `session.Tracker` | `internal/session/tracker.go` | Usar `Get(chatID)` para resumo no reset e no status |
| `imageTooLargeError` | `internal/telegram/input.go` | Modificar `UserMessage()` para usar `humanBytes()` |
| `cmdStatus`, `cmdSessionReset` | `internal/telegram/commands.go` | Refatorar mensagens de saída |
| `SendText`, `SendError` | `internal/telegram/output.go` | Adicionar `ReactToMessage` no path de ack |
| `saveDefaultModel` | `internal/telegram/bot_middleware.go` | Mudar `ClearAll` para `Clear` com threadID |

### Integration Points

| System | Integration Method |
|--------|--------------------|
| Telegram Bot API | `bot.React()` para ack; `bot.Edit()` para progress; `bot.Send()` para mensagens |
| Session Store | `Get(chatID)` para resumo de tokens/mensagens; `Clear(chatID, threadID)` para model switch local |
| Run Supervisor | Nova interface `QueueInfo(chatID, threadID)` para posição na fila |

---

## Components

### 1. Ack de Recebimento (`ackMessage`)

- **Purpose**: Reagir com 👀 na mensagem do usuário imediatamente
- **Location**: `internal/telegram/bot.go` (novo método), usado em todos os handlers
- **Changes**:
  - Novo método `ackMessage(msg *telebot.Message)` no BotController
  - Adicionar reação 👀 via `bot.React()` antes de qualquer processamento
  - Guardar `messageID` do ack para remoção posterior (opcional — Telegram permite múltiplas reações, mas 👀 pode ser substituída por ✅ ou removida)
- **Reuses**: `telebot.ReactionOptions` já usado em `ReactToMessage`

```go
// bot.go — novo método
func (bc *BotController) ackMessage(msg *telebot.Message) {
    if msg == nil {
        return
    }
    err := bc.bot.React(msg.Chat, msg, telebot.ReactionOptions{
        Reactions: []telebot.Reaction{{Type: "emoji", Emoji: "👀"}},
    })
    if err != nil {
        log.Printf("ack reaction error: %v", err)
    }
}
```

**Caveat**: Telegram Bot API não permite "remover" uma reação via API diretamente de bots (bots só podem setar reações). A solução é deixar a 👀 persistir — é aceitável visualmente. Se o bot puder, substituir por ✅ ao finalizar.

Atualização: a API do Telegram suporta `setMessageReaction` com `is_big=false`. O bot pode substituir a reação. Vamos substituir 👀 por ✅ quando a resposta for enviada.

```go
func (bc *BotController) confirmMessage(msg *telebot.Message) {
    _ = bc.bot.React(msg.Chat, msg, telebot.ReactionOptions{
        Reactions: []telebot.Reaction{{Type: "emoji", Emoji: "✅"}},
    })
}
```

### 2. Fila Transparente (`QueueInfo`)

- **Purpose**: Expor informações da fila para mensagens de status e admissão
- **Location**: `internal/pipeline/run_supervisor.go`
- **Changes**:
  - Novos métodos: `queueSize(key runKey) int`, `activeDescription(key runKey) string`
- **Reuses**: Estrutura existente `activeRun.description()`

```go
// run_supervisor.go — novos métodos
func (rs *runSupervisor) queueSize(key runKey) int {
    rs.mu.Lock()
    defer rs.mu.Unlock()
    // A fila é por key, mas hoje só armazena 1 item (rs.queued[key]).
    // Para posição real, precisaríamos de fila global ordenada.
    // Simplificação: retornar 1 se houver item na fila deste key.
    if _, ok := rs.queued[key]; ok {
        return 1
    }
    return 0
}

func (rs *runSupervisor) activeDescription(key runKey) string {
    rs.mu.Lock()
    defer rs.mu.Unlock()
    if run := rs.active[key]; run != nil {
        return run.description()
    }
    return ""
}
```

**Nota**: O runSupervisor atual tem fila de 1 item por key. A "posição na fila" é binária (0 ou 1). A mensagem de `admitQueued` pode ser ajustada para: `"📥 Ainda estou processando seu pedido anterior. Sua mensagem será a próxima."` — mais honesto do que inventar posições.

### 3. Model Switch Local (`Clear` com threadID)

- **Purpose**: Trocar de modelo sem destruir sessões de outros tópicos
- **Location**: `internal/telegram/bot_middleware.go:setModelFromCallback`, `internal/telegram/commands.go:cmdSetModel`
- **Changes**:
  - `setModelFromCallback`: trocar `bc.sessions.ClearAll(chatID)` por `bc.sessions.Clear(chatID, threadID)` (obter threadID do contexto)
  - `cmdSetModel`: mesma mudança, mas `cmdSetModel` não tem threadID — precisa receber ou usar 0
- **Reuses**: `session.Store.Clear()` já existe

```go
// bot_middleware.go — modificar setModelFromCallback
func (bc *BotController) setModelFromCallback(c telebot.Context, data string) error {
    // ... existing parsing ...
    chatID := c.Chat().ID
    threadID := c.Message().ThreadID // ou c.Callback().Message.ThreadID
    bc.sessions.Clear(chatID, threadID) // ← era ClearAll(chatID)
    // ... resto ...
    return c.Edit(fmt.Sprintf("✅ Modelo alterado para **%s**\nProvedor: **%s**\n\nSessão deste tópico foi resetada.", modelID, provider))
}
```

**Caveat**: Callbacks em inline keyboards não têm `Message` diretamente no `telebot.Context` — precisa usar `c.Callback().Message`. O `Callback.Message` tem `ThreadID` no Telegram, mas a telebot library pode não expor. Precisa verificar.

Verificação: em `telebot.v3`, `c.Callback().Message` retorna `*telebot.Message`, que tem `ThreadID`. Funciona.

### 4. Erros Actionable (`ErrorHints`)

- **Purpose**: Adicionar dicas contextuais às mensagens de erro
- **Location**: `internal/telegram/messages.go` (novas constantes), `internal/pipeline/pipeline.go`, `internal/telegram/commands.go`
- **Changes**:
  - Novas constantes de erro com dicas em `messages.go`
  - Substituir mensagens hardcoded nos handlers
- **Reuses**: Padrão de mensagens centralizadas já existente

```go
// messages.go — novas mensagens
const (
    bridgeConnectErrorHint = "❌ **Falha ao conectar com o processador**\n\n" +
        "Dica: verifique se o daemon está rodando.\n" +
        "Se persistir, tente /new para reiniciar a sessão."

    bridgeCooldownHint = "⏳ **Processador em recuperação**\n\n" +
        "Tente novamente em ~%d segundos."

    cronParseHint = "Não entendi o agendamento.\n\n" +
        "Tente algo como: \"agenda todo dia às 9h revisar emails\""

    timeoutHint = "⏱️ **Tempo limite atingido**\n\n" +
        "A solicitação foi muito complexa. Tente dividir em partes menores."
)
```

### 5. Progresso Rico (`progressReporter` v2)

- **Purpose**: Timer e mais ferramentas no progresso
- **Location**: `internal/telegram/progress.go`
- **Changes**:
  - Campo `startTime time.Time`
  - Limite de display de 5 → 8
  - Prefixar mensagem com timer formatado
- **Reuses**: Estrutura existente, apenas adicionar campo

```go
// progress.go — modificações
type progressReporter struct {
    bot       *telebot.Bot
    chat      *telebot.Chat
    msg       *telebot.Message
    tools     []string
    threadID  int
    mu        sync.Mutex
    startTime time.Time // ← novo
}

func newProgressReporter(bot *telebot.Bot, chat *telebot.Chat) *progressReporter {
    return &progressReporter{bot: bot, chat: chat, startTime: time.Now()} // ←
}

func (p *progressReporter) ReportTool(toolName string) {
    p.mu.Lock()
    defer p.mu.Unlock()

    label := toolDisplayName(toolName)
    p.tools = append(p.tools, label)

    display := p.tools
    if len(display) > 8 { // ← de 5 para 8
        display = display[len(display)-8:]
    }

    elapsed := time.Since(p.startTime).Round(time.Second)
    timerStr := formatDuration(elapsed)
    text := "⏱️ " + timerStr + "\n" + strings.Join(display, "\n")

    // ... resto igual ...
}

func formatDuration(d time.Duration) string {
    if d < time.Minute {
        return fmt.Sprintf("%ds", int(d.Seconds()))
    }
    m := int(d.Minutes())
    s := int(d.Seconds()) % 60
    return fmt.Sprintf("%dm %ds", m, s)
}
```

### 6. Unidades Humanas (`humanBytes`)

- **Purpose**: Converter bytes para MB/KB/B legíveis
- **Location**: `internal/telegram/input.go` (helper), usado em `imageTooLargeError.UserMessage()`
- **Changes**:
  - Nova função `humanBytes(n int) string`
  - Modificar `imageTooLargeError.UserMessage()`
- **Reuses**: Apenas formatação

```go
// input.go — nova função
func humanBytes(n int) string {
    if n < 1024 {
        return fmt.Sprintf("%d B", n)
    }
    if n < 1024*1024 {
        return fmt.Sprintf("%.1f KB", float64(n)/1024)
    }
    return fmt.Sprintf("%.1f MB", float64(n)/(1024*1024))
}

// modificar imageTooLargeError.UserMessage
func (e imageTooLargeError) UserMessage() string {
    return fmt.Sprintf("Imagem muito grande (%s). O limite configurado é %s.", humanBytes(e.size), humanBytes(e.limit))
}
```

### 7. Status para Humanos (`cmdStatus` refatorado)

- **Purpose**: Simplificar saída de /status
- **Location**: `internal/telegram/commands.go:cmdStatus`
- **Changes**:
  - Remover session ID e flag warm/cold
  - Adicionar contagem de mensagens/tokens da sessão
  - Adicionar diretório atual (CWD)
  - Manter informações úteis: bridge status, modelo, agendamentos
- **Reuses**: `session.Tracker.Get()`, `session.Store.GetCwd()`

```go
// commands.go — cmdStatus refatorado
func (bc *BotController) cmdStatus(chatID int64, threadID int) (string, error) {
    var lines []string
    lines = append(lines, "**Status do Aurelia**\n")

    // Bridge status
    bridgeStatus := "desligado"
    if bc.bridge != nil {
        ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
        defer cancel()
        if err := bc.bridge.Ping(ctx); err == nil {
            bridgeStatus = "online"
        } else {
            bridgeStatus = "offline"
        }
    }
    lines = append(lines, fmt.Sprintf("🧠 Processador: **%s**", bridgeStatus))

    // Model
    if bc.config != nil && bc.config.DefaultModel != "" {
        lines = append(lines, fmt.Sprintf("⚙️ Modelo: **%s**", bc.config.DefaultModel))
    }

    // CWD / Projeto
    if cwd := bc.sessions.GetCwd(chatID, threadID); cwd != "" {
        lines = append(lines, fmt.Sprintf("📁 Projeto: `%s`", cwd))
    }

    // Session summary
    usage := bc.tracker.Get(chatID)
    if usage.NumTurns > 0 {
        lines = append(lines, fmt.Sprintf("💬 Sessão: **%d mensagens** • ~**%d tokens**", usage.NumTurns, usage.TotalTokens))
    } else {
        lines = append(lines, "💬 Sessão: nenhuma conversa ativa")
    }

    // Cron jobs
    if bc.cronHandler != nil {
        ctx := context.Background()
        jobs, err := bc.cronHandler.service.ListJobs(ctx, chatID)
        if err == nil {
            active := 0
            for _, j := range jobs {
                if j.Active {
                    active++
                }
            }
            lines = append(lines, fmt.Sprintf("⏰ Agendamentos ativos: **%d**", active))
        }
    }

    return strings.Join(lines, "\n"), nil
}
```

### 8. Reset com Memória (`cmdSessionReset` refatorado)

- **Purpose**: Mostrar resumo ao resetar sessão
- **Location**: `internal/telegram/commands.go:cmdSessionReset`, `internal/telegram/bot_middleware.go:handleResetCommand`
- **Changes**:
  - Antes de limpar, capturar `usage` do tracker
  - Incluir na mensagem se houver dados
- **Reuses**: `session.Tracker.Get()`

```go
// commands.go — cmdSessionReset refatorado
func (bc *BotController) cmdSessionReset(chatID int64, threadID int) (string, error) {
    // Capture usage before flush/clear
    usage := bc.tracker.Get(chatID)

    if bc.dreamer != nil {
        cwd := bc.sessions.GetCwd(chatID, threadID)
        bc.dreamer.FlushNudge(chatID, threadID, cwd, bc.nudgeBuffer)
    }
    bc.sessions.Clear(chatID, threadID)
    bc.tracker.Clear(chatID)
    log.Printf("command: session reset for chat=%d thread=%d", chatID, threadID)

    if usage.NumTurns > 0 {
        return fmt.Sprintf("🗑️ Sessão resetada (%d mensagens, ~%d tokens).\nPróxima mensagem inicia conversa nova.", usage.NumTurns, usage.TotalTokens), nil
    }
    return "Sessão resetada. Próxima mensagem inicia conversa nova.", nil
}
```

### 9. Help Rica (`handleHelpCommand` refatorado)

- **Purpose**: Incluir exemplos naturais no /help
- **Location**: `internal/telegram/bot_middleware.go:handleHelpCommand`
- **Changes**:
  - Adicionar seção de exemplos práticos após a lista de comandos
- **Reuses**: Constante de mensagem existente

```go
// bot_middleware.go — handleHelpCommand refatorado
func (bc *BotController) handleHelpCommand(c telebot.Context) error {
    help := "Comandos disponíveis:\n\n" +
        "/new — Nova sessão (limpa contexto)\n" +
        "/usage — Ver uso de tokens da sessão\n" +
        "/cwd <path> — Definir diretório de trabalho\n" +
        "/cron — Gerenciar agendamentos\n" +
        "/agents — Listar agentes disponíveis\n" +
        "/model — Ver/trocar modelo ativo\n" +
        "/help — Mostrar esta mensagem\n\n" +
        "💡 *Também entendo comandos naturais:*\n" +
        "• \"agenda todo dia às 9h revisar emails\"\n" +
        "• \"muda modelo para claude-sonnet\"\n" +
        "• \"limpa o contexto\"\n" +
        "• \"quais modelos\"\n\n" +
        "Ou simplesmente envie uma mensagem e eu respondo."
    return SendText(bc.bot, c.Chat(), help)
}
```

### 10. Documentos com Dica (`unsupportedDocumentMessage` atualizada)

- **Purpose**: Sugerir workaround para formatos não suportados
- **Location**: `internal/telegram/messages.go`
- **Changes**:
  - Adicionar dica de conversão à constante

```go
// messages.go — atualizada
const (
    unsupportedDocumentMessage = "⚠️ **Formato não suportado**\n\n" +
        "No momento eu consigo processar:\n" +
        "- arquivos `.md`\n" +
        "- arquivos `.pdf`\n" +
        "- áudio e voz\n\n" +
        "💡 *Dica: converta .docx/.xlsx para .pdf ou copie o texto diretamente.*"
)
```

---

## Data Models

Nenhum modelo novo. Apenas:
- Campo `startTime time.Time` no `progressReporter`
- Novas constantes de mensagem em `messages.go`
- Novo helper `humanBytes(n int) string`
- Novo helper `formatDuration(d time.Duration) string`

---

## Error Handling Strategy

| Error Scenario | Handling | User Impact |
|----------------|----------|-------------|
| Falha ao enviar reação 👀 | Log + continuar | Nenhum — ack é best-effort |
| Falha ao editar mensagem de progresso | Log + continuar | Progresso pode ficar desatualizado |
| Tracker vazio no reset | Omitir resumo | Comportamento anterior (sem resumo) |
| Callback sem message (inline keyboard) | Usar chatID=0, threadID=0 | ClearAll como fallback — aceitável |
| Formatação de bytes com valor negativo | Retornar "0 B" | Não deve acontecer, defensivo |

---

## Tech Decisions

| Decision | Choice | Rationale |
|----------|--------|-----------|
| Reação 👀 ao invés de mensagem de ack | Reação é mais leve e não polui o chat | Mensagem adicional teria que ser deletada, complicando o flow |
| Limite de progresso de 5 → 8 | 8 é suficiente para a maioria das tarefas | 10 seria muito longo para Telegram; 8 é bom equilíbrio |
| Timer no progresso, não no bridge | Bridge não sabe quando o usuário enviou | Progress reporter é criado no início do executeAsync, ponto correto |
| Model switch local (Clear, não ClearAll) | Preserva contexto de outros tópicos | ClearAll é surpreendente e destrutivo; Clear é o comportamento esperado |
| Resumo no reset usando tracker | Tracker já tem turns/tokens | Sem custo adicional, apenas formatar |
| Unidades humanas em helper separado | Reutilizável para outros limites no futuro | PDF size, audio size, etc. |
| Dicas de erro em constantes centralizadas | Facilita manutenção e revisão | Mensagens de UX devem ser centralizadas |
| Status simplificado, não removido | Usuários avançados ainda precisam de info | Removemos apenas jargão, não utilidade |

---

## Scope Summary

**Arquivos a modificar:**
1. `internal/telegram/messages.go` — novas mensagens de erro com dicas, mensagem de documento atualizada
2. `internal/telegram/progress.go` — `startTime`, timer, limite 8, `formatDuration`
3. `internal/telegram/input.go` — `humanBytes()`, modificar `imageTooLargeError.UserMessage()`
4. `internal/telegram/commands.go` — `cmdStatus`, `cmdSessionReset`, `cmdCronCreate` (mensagens), `cmdSetModel` (threadID)
5. `internal/telegram/bot_middleware.go` — `handleHelpCommand`, `handleResetCommand`, `setModelFromCallback`, `handleUsageCommand`
6. `internal/telegram/bot.go` — `ackMessage()`, `confirmMessage()`, chamadas nos handlers
7. `internal/pipeline/pipeline.go` — mensagens de fila (`admitQueued`, `admitReplacedQueued`)
8. `internal/pipeline/run_supervisor.go` — `queueSize()`, `activeDescription()` (opcional, para fila)
9. `internal/session/tracker.go` — verificar se `TotalTokens` existe (se não, usar estimativa)

**Arquivos de teste:**
1. `internal/telegram/progress_test.go` — timer, limite 8
2. `internal/telegram/input_test.go` — `humanBytes()`
3. `internal/telegram/commands_test.go` — `cmdStatus`, `cmdSessionReset`
4. `internal/pipeline/run_supervisor_test.go` — mensagens de fila

**Estimativa de mudança:** ~120 linhas de produção, ~80 linhas de teste
