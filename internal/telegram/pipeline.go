package telegram

import (
	"time"

	"gopkg.in/telebot.v3"

	"github.com/igormaneschy/aurelia/internal/agents"
	"github.com/igormaneschy/aurelia/internal/bridge"
	"github.com/igormaneschy/aurelia/internal/orchestrator"
	pipelinepkg "github.com/igormaneschy/aurelia/internal/pipeline"
)

const typingIndicatorInterval = 4 * time.Second

func (bc *BotController) processInput(c telebot.Context, text string) error {
	return bc.processInputWithImages(c, text, nil)
}

func (bc *BotController) processInputWithImages(c telebot.Context, text string, images []bridge.ImageAttachment) error {
	if state, ok := bc.popPendingBootstrap(c.Sender().ID); ok {
		switch state.Step {
		case bootstrapStepAssistant:
			return bc.completeBootstrapAssistant(c, state, text)
		default:
			return bc.completeBootstrapProfile(c, state, text)
		}
	}

	if cmd := MatchCommand(text); cmd != nil {
		return bc.handleCommand(c, cmd)
	}

	return bc.runPipeline(c.Chat().ID, c.Message().ThreadID, c.Message().ID, text, images)
}

func (bc *BotController) runPipeline(chatID int64, threadID int, messageID int, text string, images []bridge.ImageAttachment) error {
	return bc.ensurePipeline().Process(chatID, threadID, messageID, text, images)
}

func (bc *BotController) buildSystemPrompt(userText string, agent *agents.Agent, chatID int64, messageID int, threadID int) (string, error) {
	return bc.ensurePipeline().BuildSystemPrompt(userText, agent, chatID, messageID, threadID)
}

func (bc *BotController) processBridgeEventsAsync(chat *telebot.Chat, ch <-chan bridge.Event, progress *progressReporter, userText string, messageID int) bridgeOutcome {
	return bc.processBridgeEventsAsyncWithThread(chat, ch, progress, userText, messageID, 0)
}

func (bc *BotController) processBridgeEventsAsyncWithThread(chat *telebot.Chat, ch <-chan bridge.Event, progress *progressReporter, userText string, messageID int, threadID int) bridgeOutcome {
	return bridgeOutcome(bc.ensurePipeline().ProcessBridgeEvents(chat.ID, threadID, messageID, ch, progress, userText))
}

func (bc *BotController) invalidateMemoryDirs(chatID int64, threadID int, cwd string) {
	bc.ensurePipeline().InvalidateMemoryDirs(chatID, threadID, cwd)
}

func (bc *BotController) ensurePipeline() *pipelinepkg.Service {
	if bc.pipeline != nil {
		return bc.pipeline
	}
	bc.pipeline = pipelinepkg.NewService(pipelinepkg.Config{
		AppConfig:    bc.config,
		Bridge:       bc.bridge,
		Agents:       bc.agents,
		Persona:      bc.persona,
		Sessions:     bc.sessions,
		Tracker:      bc.tracker,
		Resolver:     bc.resolver,
		MemoryDir:    bc.memoryDir,
		ExePath:      bc.exePath,
		BotCwd:       bc.botCwd,
		Output:       telegramPipelineOutput{bc: bc},
		Orchestrator: bc.orchestrator,
		Dreamer:      bc.dreamer,
		ProjectIndex: bc.projectIndex,
	})
	bc.nudgeBuffer = bc.pipeline.NudgeBuffer()
	return bc.pipeline
}

type telegramPipelineOutput struct {
	bc *BotController
}

func (o telegramPipelineOutput) StartTyping(chatID int64, threadID int) func() {
	if o.bc == nil || o.bc.bot == nil {
		return func() {}
	}
	return startChatActionLoop(o.bc.bot, &telebot.Chat{ID: chatID}, telebot.Typing, typingIndicatorInterval, threadID)
}

func (o telegramPipelineOutput) NewProgress(chatID int64, threadID int) pipelinepkg.ProgressReporter {
	if o.bc == nil || o.bc.bot == nil {
		return noopPipelineProgress{}
	}
	return newProgressReporterWithThread(o.bc.bot, &telebot.Chat{ID: chatID}, threadID)
}

func (o telegramPipelineOutput) SendError(chatID int64, threadID int, text string) error {
	if o.bc == nil || o.bc.bot == nil {
		return nil
	}
	return SendErrorWithThread(o.bc.bot, &telebot.Chat{ID: chatID}, text, threadID)
}

func (o telegramPipelineOutput) SendReply(chatID int64, threadID int, text string) error {
	if o.bc == nil || o.bc.bot == nil {
		return nil
	}
	return SendTextReplyWithThread(o.bc.bot, &telebot.Chat{ID: chatID}, text, threadID)
}

func (o telegramPipelineOutput) SendText(chatID int64, threadID int, text string) (any, error) {
	if o.bc == nil || o.bc.bot == nil {
		return nil, nil
	}
	return o.bc.bot.Send(&telebot.Chat{ID: chatID}, text, &telebot.SendOptions{ThreadID: threadID})
}

func (o telegramPipelineOutput) DeleteMessage(message any) {
	msg, ok := message.(*telebot.Message)
	if !ok || msg == nil || o.bc == nil || o.bc.bot == nil {
		return
	}
	_ = o.bc.bot.Delete(msg)
}

func (o telegramPipelineOutput) ExecuteApprovedPlan(chatID int64, messageID int, plan *orchestrator.Plan) {
	if o.bc == nil {
		return
	}
	o.bc.executeApprovedPlan(&telebot.Chat{ID: chatID}, messageID, plan)
}

type noopPipelineProgress struct{}

func (noopPipelineProgress) ReportTool(string) {}
func (noopPipelineProgress) Delete()           {}
