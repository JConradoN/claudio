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

			log.Printf("whitelist check: user=%d chat=%d type=%q title=%q\n",
				safeSenderID(sender), chat.ID, chat.Type, chat.Title)

			for _, id := range bc.config.TelegramAllowedGroupIDs {
				log.Printf("  allowed group ID: %d (match=%v)", id, id == chat.ID)
			}

			switch chat.Type {
			case telebot.ChatPrivate:
				if sender != nil && bc.isAllowedUser(sender.ID) {
					return next(c)
				}
			case telebot.ChatGroup, telebot.ChatSuperGroup:
				if bc.isAllowedGroup(chat.ID) {
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
	help := "Comandos disponíveis:\n\n" +
		"/new — Nova sessão (limpa contexto)\n" +
		"/usage — Ver uso de tokens da sessão\n" +
		"/cwd <path> — Definir diretório de trabalho\n" +
		"/cron — Gerenciar agendamentos\n" +
		"/agents — Listar agentes disponíveis\n" +
		"/model — Ver/trocar modelo ativo\n" +
		"/help — Mostrar esta mensagem\n\n" +
		"Ou simplesmente envie uma mensagem e eu respondo."
	return SendText(bc.bot, c.Chat(), help)
}

func (bc *BotController) handleAgentsCommand(c telebot.Context) error {
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
	chatID := c.Chat().ID
	threadID := c.Message().ThreadID
	args := strings.TrimSpace(c.Message().Payload)
	if args == "" {
		cwd := bc.sessions.GetCwd(chatID, threadID)
		if cwd == "" {
			return SendTextWithThread(bc.bot, c.Chat(), "Nenhum diretório configurado. Use: /cwd C:\\path\\to\\project", threadID)
		}
		return SendTextWithThread(bc.bot, c.Chat(), fmt.Sprintf("Diretório atual: %s", cwd), threadID)
	}
	bc.sessions.SetCwd(chatID, threadID, args)
	if bc.resolver != nil {
		if err := runtime.BootstrapProjectMemory(bc.resolver, args); err != nil {
			log.Printf("cwd: failed to bootstrap project memory for %s: %v", args, err)
		}
	}
	return SendTextWithThread(bc.bot, c.Chat(), fmt.Sprintf("Diretório configurado: %s", args), threadID)
}

func (bc *BotController) handleResetCommand(c telebot.Context) error {
	chatID := c.Chat().ID
	threadID := c.Message().ThreadID
	// Flush pending nudge buffer so conversation memories are saved.
	if bc.dreamer != nil {
		cwd := bc.sessions.GetCwd(chatID, threadID)
		bc.dreamer.FlushNudge(chatID, threadID, cwd, bc.nudgeBuffer)
	}
	bc.sessions.Clear(chatID, threadID)
	bc.tracker.Clear(chatID)
	return SendTextWithThread(bc.bot, c.Chat(), "Sessão resetada. Próxima mensagem inicia conversa nova.", threadID)
}

func (bc *BotController) handleUsageCommand(c telebot.Context) error {
	usage := bc.tracker.Get(c.Chat().ID)
	if usage.NumTurns == 0 {
		return SendText(bc.bot, c.Chat(), "Nenhum uso registrado na sessão atual.")
	}
	maxTokens := bc.config.MaxSessionTokens
	msg := fmt.Sprintf("📊 Sessão atual:\n\n%s\nAuto-reset em: %d tokens", usage, maxTokens)
	return SendText(bc.bot, c.Chat(), msg)
}

func (bc *BotController) handleCronCommand(c telebot.Context) error {
	if bc.cronHandler == nil {
		return SendText(bc.bot, c.Chat(), "Cron não está disponível.")
	}
	userID := fmt.Sprintf("%d", c.Sender().ID)
	chatID := c.Chat().ID
	text := c.Message().Text

	reply, err := bc.cronHandler.HandleText(context.Background(), userID, chatID, text)
	if err != nil {
		return SendError(bc.bot, c.Chat(), err.Error())
	}
	if reply != "" {
		return SendText(bc.bot, c.Chat(), reply)
	}
	return nil
}

const modelsPerPage = 10

func (bc *BotController) handleModelCommand(c telebot.Context) error {
	args := strings.TrimSpace(c.Message().Payload)
	if args != "" {
		reply, err := bc.cmdSetModel(c, "/model "+args)
		if err != nil {
			return SendError(bc.bot, c.Chat(), fmt.Sprintf("Erro: %v", err))
		}
		return SendText(bc.bot, c.Chat(), reply)
	}

	currentLine := fmt.Sprintf("Modelo atual: **%s** (provedor: **%s**)", bc.config.DefaultModel, bc.config.DefaultProvider)
	if bc.bridge == nil {
		return SendText(bc.bot, c.Chat(), currentLine+"\n\nBridge indisponível.")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	models, err := bc.bridge.ListModels(ctx)
	if err != nil {
		return SendText(bc.bot, c.Chat(), currentLine+fmt.Sprintf("\n\nLista não disponível: %v", err))
	}
	if len(models) == 0 {
		return SendText(bc.bot, c.Chat(), currentLine+"\n\nNenhum modelo disponível.")
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
	_, err := bc.bot.Send(c.Chat(), msg, menu)
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
	bc.sessions.ClearAll(chatID)

	if err := bc.saveDefaultModel(provider, modelID); err != nil {
		log.Printf("model callback persist: %v", err)
	}

	return c.Edit(fmt.Sprintf("✅ Modelo alterado para **%s**\nProvedor: **%s**\n\nSessão resetada.", modelID, provider))
}
