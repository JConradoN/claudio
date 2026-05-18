package telegram

import (
	"context"
	"fmt"
	"log"
	"sort"
	"strconv"
	"strings"
	"time"

	"gopkg.in/telebot.v3"

	"github.com/igormaneschy/aurelia/internal/bridge"
	"github.com/igormaneschy/aurelia/internal/runtime"
)

func (bc *BotController) whitelistMiddleware() telebot.MiddlewareFunc {
	return func(next telebot.HandlerFunc) telebot.HandlerFunc {
		return func(c telebot.Context) error {
			sender := c.Sender()
			chat := c.Chat()

			switch chat.Type {
			case telebot.ChatPrivate:
				if sender != nil && bc.isAllowedUser(sender.ID) {
					return next(c)
				}
			case telebot.ChatGroup, telebot.ChatSuperGroup:
				// Require both: group on allowlist AND sender on user allowlist.
				// Group-only check would let any group member talk to the bot,
				// including users added after the group was whitelisted.
				if sender != nil && bc.isAllowedGroup(chat.ID) && bc.isAllowedUser(sender.ID) {
					return next(c)
				}
			}

			log.Printf("blocked unauthorized access: user=%d chat=%d type=%q\n",
				safeSenderID(sender), chat.ID, chat.Type)
			return nil
		}
	}
}

func safeSenderID(u *telebot.User) int64 {
	if u == nil {
		return 0
	}
	return u.ID
}

func (bc *BotController) registerContentRoutes() {
	bc.bot.Handle("/help", bc.handleHelpCommand)
	bc.bot.Handle("/cwd", bc.handleCwdCommand)
	bc.bot.Handle("/reset", bc.handleResetCommand)
	bc.bot.Handle("/new", bc.handleResetCommand)
	bc.bot.Handle("/compact", bc.handleResetCommand)
	bc.bot.Handle("/usage", bc.handleUsageCommand)
	bc.bot.Handle("/status", bc.handleStatusCommand)
	bc.bot.Handle("/cron", bc.handleCronCommand)
	bc.bot.Handle("/agents", bc.handleAgentsCommand)
	bc.bot.Handle("/model", bc.handleModelCommand)
	bc.bot.Handle(telebot.OnCallback, bc.handleModelCallback)
	bc.bot.Handle(telebot.OnText, bc.handleText)
	bc.bot.Handle(telebot.OnPhoto, bc.handlePhoto)
	bc.bot.Handle(telebot.OnDocument, bc.handleDocument)
	bc.bot.Handle(telebot.OnVoice, bc.handleVoice)
	bc.bot.Handle(telebot.OnAudio, bc.handleVoice)
}

func (bc *BotController) registerSlashMenu() {
	commands := []telebot.Command{
		{Text: "new", Description: "Nova sessão (limpa contexto)"},
		{Text: "usage", Description: "Ver uso de tokens da sessão"},
		{Text: "status", Description: "Ver estado atual da Aurelia"},
		{Text: "cwd", Description: "Definir diretório de trabalho"},
		{Text: "cron", Description: "Gerenciar agendamentos"},
		{Text: "agents", Description: "Listar agentes disponíveis"},
		{Text: "model", Description: "Ver/trocar modelo ativo"},
		{Text: "help", Description: "Mostrar comandos disponíveis"},
	}
	if err := bc.bot.SetCommands(commands); err != nil {
		log.Printf("Failed to set bot commands: %v", err)
	}
}

func (bc *BotController) handleHelpCommand(c telebot.Context) error {
	defer bc.confirmMessage(c.Message())
	return SendTextWithThread(bc.bot, c.Chat(), helpMessage(), c.Message().ThreadID)
}

func helpMessage() string {
	return "Comandos disponíveis:\n\n" +
		"/new — Nova sessão (limpa contexto, cancela o que estiver em andamento)\n" +
		"/usage — Ver uso de tokens da sessão\n" +
		"/status — Ver estado atual + trabalho ativo + fila\n" +
		"/cwd <path> — Definir diretório de trabalho (tópicos herdam do grupo)\n" +
		"/cron — Gerenciar agendamentos\n" +
		"/agents — Listar agentes disponíveis (também roteáveis com @nome)\n" +
		"/model — Ver/trocar modelo ativo\n" +
		"/help — Mostrar esta mensagem\n\n" +
		"---\n\n" +
		"💡 Também entendo comandos naturais:\n" +
		"• \"agenda todo dia às 9h revisar emails\"\n" +
		"• \"muda modelo para claude-sonnet\"\n" +
		"• \"limpa o contexto\"\n" +
		"• \"quais modelos\"\n\n" +
		"🛑 Enquanto eu processo, você pode:\n" +
		"• \"para\" / \"cancela\" — interrompe o pedido atual\n" +
		"• \"na verdade...\" / \"corrigindo\" — substitui pelo novo pedido\n" +
		"• \"conseguiu?\" / \"status\" — pergunta sem entrar na fila\n" +
		"• outras mensagens entram na fila e rodam depois\n\n" +
		"Ou simplesmente envie uma mensagem e eu respondo."
}

func (bc *BotController) handleAgentsCommand(c telebot.Context) error {
	defer bc.confirmMessage(c.Message())
	if bc.agents == nil || len(bc.agents.Agents()) == 0 {
		return SendText(bc.bot, c.Chat(), "Nenhum agente configurado. Crie arquivos .md em ~/.aurelia/agents/")
	}
	var lines []string
	for _, a := range bc.agents.Agents() {
		line := fmt.Sprintf("• %s — %s", a.Name, a.Description)
		if a.Model != "" {
			line += fmt.Sprintf(" [%s]", a.Model)
		}
		if a.Schedule != "" {
			line += fmt.Sprintf(" (cron: %s)", a.Schedule)
		}
		lines = append(lines, line)
	}
	return SendText(bc.bot, c.Chat(), "Agentes disponíveis:\n\n"+strings.Join(lines, "\n"))
}

func (bc *BotController) handleCwdCommand(c telebot.Context) error {
	defer bc.confirmMessage(c.Message())
	chatID := c.Chat().ID
	threadID := c.Message().ThreadID
	args := strings.TrimSpace(c.Message().Payload)

	if args == "" {
		// Show full CWD hierarchy
		topicCwd := ""
		if threadID > 0 {
			topicCwd = bc.currentCwd(chatID, threadID)
		}
		groupCwd := bc.currentCwd(chatID, 0)
		defaultCwd := bc.botCwd

		// Determine agent cwd if an agent is active
		agentCwd := ""
		text := c.Message().Text
		if bc.agents != nil {
			if agent := bc.agents.Route(text); agent != nil && agent.Cwd != "" {
				agentCwd = agent.Cwd
			}
		}

		var b strings.Builder
		b.WriteString("📂 **CWD Resolution Chain**\n\n")
		b.WriteString("(from lowest to highest priority)\n\n")

		// Base: bot default
		fmt.Fprintf(&b, "1. ⚙️ Bot default: `%s`\n", defaultCwd)
		b.WriteString("   (operational cwd only; not a project binding)\n\n")

		// Then: group
		if groupCwd != "" {
			fmt.Fprintf(&b, "2. 👥 Group: `%s`\n", groupCwd)
		} else {
			b.WriteString("2. 👥 Group: *(not set)*\n")
		}
		b.WriteString("   (configure with /cwd <path> in the general topic)\n\n")

		// Then: topic (if applicable)
		if threadID > 0 {
			if topicCwd != "" && topicCwd != groupCwd {
				fmt.Fprintf(&b, "3. 📌 This topic: `%s`\n", topicCwd)
				b.WriteString("   (overrides group for this topic)\n\n")
			} else {
				b.WriteString("3. 📌 This topic: *(inherited from group)*\n\n")
			}
		}

		// Finally: agent (highest priority)
		if agentCwd != "" {
			fmt.Fprintf(&b, "4. 🤖 Agent: `%s`\n", agentCwd)
			b.WriteString("   (defined in agent markdown — highest priority)\n\n")
		}

		if groupCwd == "" && agentCwd == "" {
			b.WriteString("💡 Set a persistent project binding with: `/cwd /path/to/project`\n")
		}

		return SendTextWithThread(bc.bot, c.Chat(), b.String(), threadID)
	}

	if clearThreadID, clear, err := cwdClearThread(args, threadID); err != nil {
		return SendTextWithThread(bc.bot, c.Chat(), "❌ "+err.Error(), threadID)
	} else if clear {
		if err := bc.clearCurrentCwd(chatID, clearThreadID); err != nil {
			return SendErrorWithThread(bc.bot, c.Chat(), err.Error(), threadID)
		}
		msg := "✅ Binding de projeto removido deste tópico."
		if clearThreadID == 0 {
			msg = "✅ Binding de projeto removido do grupo."
		}
		return SendTextWithThread(bc.bot, c.Chat(), msg, threadID)
	}

	// Set persistent CWD for current thread/topic.
	userID := int64(0)
	if c.Sender() != nil {
		userID = c.Sender().ID
	}
	cwd, err := bc.setCurrentCwd(chatID, threadID, userID, args)
	if err != nil {
		log.Printf("cwd: rejected binding chat=%d thread=%d", chatID, threadID)
		return SendTextWithThread(bc.bot, c.Chat(), "❌ Diretório inválido, inexistente ou não permitido.", threadID)
	}
	if bc.resolver != nil {
		if err := runtime.BootstrapConversationProjectMemory(bc.resolver, cwd, chatID, threadID); err != nil {
			log.Printf("cwd: failed to bootstrap project memory for %s: %v", cwd, err)
		}
	}
	// Invalidate memory cache — project changed, memory files may differ
	bc.invalidateMemoryDirs(chatID, threadID, cwd)

	msg := fmt.Sprintf("✅ Projeto fixado para este tópico: `%s`\n\nEssa configuração é persistente até você trocar ou limpar com `/cwd clear`.", cwd)
	if threadID == 0 {
		msg = fmt.Sprintf("✅ Projeto fixado para o grupo: `%s`\n\nOutros tópicos herdarão este caminho automaticamente. Essa configuração é persistente.", cwd)
	}
	return SendTextWithThread(bc.bot, c.Chat(), msg, threadID)
}

func (bc *BotController) handleResetCommand(c telebot.Context) error {
	defer bc.confirmMessage(c.Message())
	chatID := c.Chat().ID
	threadID := c.Message().ThreadID
	reply, err := bc.resetCurrentSession(chatID, threadID, true)
	if err != nil {
		return SendErrorWithThread(bc.bot, c.Chat(), err.Error(), threadID)
	}
	return SendTextWithThread(bc.bot, c.Chat(), reply, threadID)
}

func (bc *BotController) handleUsageCommand(c telebot.Context) error {
	defer bc.confirmMessage(c.Message())
	threadID := c.Message().ThreadID
	usage := bc.tracker.Get(c.Chat().ID)
	if usage.NumTurns == 0 {
		return SendTextWithThread(bc.bot, c.Chat(), "Nenhum uso registrado na sessão atual.", threadID)
	}
	maxTokens := bc.config.MaxSessionTokens
	msg := fmt.Sprintf("📊 Sessão atual:\n\n%s\nAuto-reset em: %d tokens", usage, maxTokens)
	return SendTextWithThread(bc.bot, c.Chat(), msg, threadID)
}

func (bc *BotController) handleStatusCommand(c telebot.Context) error {
	defer bc.confirmMessage(c.Message())
	threadID := c.Message().ThreadID
	reply, err := bc.cmdStatus(c.Chat().ID, threadID)
	if err != nil {
		return SendErrorWithThread(bc.bot, c.Chat(), err.Error(), threadID)
	}
	return SendTextWithThread(bc.bot, c.Chat(), reply, threadID)
}

func (bc *BotController) handleCronCommand(c telebot.Context) error {
	defer bc.confirmMessage(c.Message())
	if bc.cronHandler == nil {
		return SendText(bc.bot, c.Chat(), "Cron não está disponível.")
	}
	userID := fmt.Sprintf("%d", c.Sender().ID)
	chatID := c.Chat().ID
	text := c.Message().Text

	reply, err := bc.cronHandler.HandleText(context.Background(), userID, chatID, text)
	if err != nil {
		return SendErrorWithThread(bc.bot, c.Chat(), err.Error(), c.Message().ThreadID)
	}
	if reply != "" {
		return SendTextWithThread(bc.bot, c.Chat(), reply, c.Message().ThreadID)
	}
	return nil
}

const modelsPerPage = 10

func (bc *BotController) handleModelCommand(c telebot.Context) error {
	defer bc.confirmMessage(c.Message())
	args := strings.TrimSpace(c.Message().Payload)
	threadID := c.Message().ThreadID
	if args != "" {
		reply, err := bc.cmdSetModel(c, "/model "+args)
		if err != nil {
			return SendErrorWithThread(bc.bot, c.Chat(), fmt.Sprintf("Erro: %v", err), threadID)
		}
		return SendTextWithThread(bc.bot, c.Chat(), reply, threadID)
	}

	currentLine := fmt.Sprintf("Modelo atual: **%s** (provedor: **%s**)", bc.config.DefaultModel, bc.config.DefaultProvider)
	if bc.bridge == nil {
		return SendTextWithThread(bc.bot, c.Chat(), currentLine+"\n\nBridge indisponível.", threadID)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	models, err := bc.getModels(ctx)
	if err != nil {
		return SendTextWithThread(bc.bot, c.Chat(), currentLine+fmt.Sprintf("\n\nLista não disponível: %v", err), threadID)
	}
	if len(models) == 0 {
		return SendTextWithThread(bc.bot, c.Chat(), currentLine+"\n\nNenhum modelo disponível.", threadID)
	}

	bc.modelCacheMu.Lock()
	bc.modelCache = models
	bc.modelCacheMu.Unlock()

	return bc.sendProviderMenu(c, false)
}

func (bc *BotController) sendProviderMenu(c telebot.Context, edit bool) error {
	bc.modelCacheMu.Lock()
	models := bc.modelCache
	bc.modelCacheMu.Unlock()

	grouped := make(map[string]bool)
	for _, m := range models {
		grouped[m.Provider] = true
	}
	var providers []string
	for p := range grouped {
		providers = append(providers, p)
	}
	sort.Strings(providers)

	menu := &telebot.ReplyMarkup{}
	var rows []telebot.Row
	for _, prov := range providers {
		rows = append(rows, menu.Row(menu.Data(prov, "mdl_prov_"+prov)))
	}
	rows = append(rows, menu.Row(menu.Data("❌ Cancelar", "mdl_cancel")))
	menu.Inline(rows...)

	currentLine := fmt.Sprintf("Modelo atual: **%s** (**%s**)", bc.config.DefaultModel, bc.config.DefaultProvider)
	msg := currentLine + "\n\n**Selecione o provedor:**"
	if edit {
		return c.Edit(msg, menu)
	}
	_, err := bc.bot.Send(c.Chat(), msg, &telebot.SendOptions{ThreadID: c.Message().ThreadID}, menu)
	return err
}

func (bc *BotController) handleModelCallback(c telebot.Context) error {
	// Acknowledge callback to stop Telegram loading spinner.
	// This runs for ALL callbacks (bootstrap, model, etc.), which is harmless.
	_ = bc.bot.Respond(c.Callback(), &telebot.CallbackResponse{})

	data := c.Data()

	// Telebot sends callback data as "\f<unique>|<payload>"
	// c.Data() returns the full callback data from Telegram (with \f prefix)
	// Strip the leading \f before checking
	if len(data) > 0 && data[0] == '\f' {
		data = data[1:]
	}

	// Split by | to get just the unique part (ignore payload we don't use)
	if idx := strings.IndexByte(data, '|'); idx >= 0 {
		data = data[:idx]
	}

	// Route by the model identifier prefix
	switch {
	case strings.HasPrefix(data, "mdl_prov_"):
		return bc.showModelPage(c, strings.TrimPrefix(data, "mdl_prov_"), 0)
	case strings.HasPrefix(data, "mdl_set_"):
		return bc.setModelFromCallback(c, strings.TrimPrefix(data, "mdl_set_"))
	case strings.HasPrefix(data, "mdl_next_"):
		return bc.showModelPage(c, strings.TrimPrefix(data, "mdl_next_"), 1)
	case strings.HasPrefix(data, "mdl_prev_"):
		return bc.showModelPage(c, strings.TrimPrefix(data, "mdl_prev_"), -1)
	case data == "mdl_back":
		return bc.sendProviderMenu(c, true)
	case data == "mdl_cancel":
		return c.Edit("✅ Operação cancelada. O modelo continua: **" + bc.config.DefaultModel + "**.")
	default:
		return nil
	}
}

func (bc *BotController) showModelPage(c telebot.Context, data string, dir int) error {
	// data: "provider" for initial, "provider_PAGE" for pagination
	lastUnderscore := strings.LastIndex(data, "_")
	page := 0
	provider := data
	if lastUnderscore > 0 {
		if p, err := strconv.Atoi(data[lastUnderscore+1:]); err == nil {
			page = p + dir
			provider = data[:lastUnderscore]
		}
	}
	if page < 0 {
		page = 0
	}

	bc.modelCacheMu.Lock()
	models := bc.modelCache
	bc.modelCacheMu.Unlock()

	var filtered []bridge.ModelInfo
	for _, m := range models {
		if m.Provider == provider {
			filtered = append(filtered, m)
		}
	}
	if len(filtered) == 0 {
		return nil
	}

	totalPages := (len(filtered) + modelsPerPage - 1) / modelsPerPage
	if page >= totalPages {
		page = totalPages - 1
	}

	start := page * modelsPerPage
	end := start + modelsPerPage
	if end > len(filtered) {
		end = len(filtered)
	}

	menu := &telebot.ReplyMarkup{}
	var rows []telebot.Row
	for _, m := range filtered[start:end] {
		label := m.ID
		if m.SupportsImages {
			label += " 📷"
		}
		rows = append(rows, menu.Row(menu.Data(label, "mdl_set_"+provider+"_"+m.ID)))
	}

	// Navigation row
	navRow := []telebot.Btn{}
	if page > 0 {
		navRow = append(navRow, menu.Data("◀", "mdl_prev_"+provider+"_"+strconv.Itoa(page)))
	}
	navRow = append(navRow, menu.Data(fmt.Sprintf("%d/%d", page+1, totalPages), "mdl_nop"))
	if page < totalPages-1 {
		navRow = append(navRow, menu.Data("▶", "mdl_next_"+provider+"_"+strconv.Itoa(page)))
	}
	rows = append(rows, navRow)
	rows = append(rows, menu.Row(menu.Data("← Provedores", "mdl_back")))
	rows = append(rows, menu.Row(menu.Data("❌ Cancelar", "mdl_cancel")))
	menu.Inline(rows...)

	return c.Edit(fmt.Sprintf("**%s** — página %d/%d:", provider, page+1, totalPages), menu)
}

func (bc *BotController) setModelFromCallback(c telebot.Context, data string) error {
	firstUnderscore := strings.Index(data, "_")
	if firstUnderscore < 0 {
		return nil
	}
	provider := data[:firstUnderscore]
	modelID := data[firstUnderscore+1:]

	bc.config.DefaultModel = modelID
	bc.config.DefaultProvider = provider

	chatID := c.Chat().ID
	threadID := callbackThreadID(c)
	resetMsg := bc.resetCurrentModelSession(chatID, threadID)

	if err := bc.saveDefaultModel(provider, modelID); err != nil {
		log.Printf("model callback persist: %v", err)
	}

	if bc.refreshProviderEnv != nil {
		bc.refreshProviderEnv()
	}

	return c.Edit(fmt.Sprintf("✅ Modelo alterado para **%s**\nProvedor: **%s**\n\n%s", modelID, provider, resetMsg))
}

func callbackThreadID(c telebot.Context) int {
	cb := c.Callback()
	if cb == nil || cb.Message == nil {
		return 0
	}
	return cb.Message.ThreadID
}
