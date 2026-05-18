package telegram

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/igormaneschy/aurelia/internal/projectbinding"
	"github.com/igormaneschy/aurelia/internal/runtime"
)

func (bc *BotController) currentCwd(chatID int64, threadID int) string {
	if bc.bindings != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		resolved, err := bc.bindings.Resolve(ctx, projectbinding.ConversationKey{ChatID: chatID, ThreadID: threadID})
		if err == nil && resolved != nil && resolved.Binding != nil {
			return resolved.Binding.CWD
		}
		if err != nil {
			log.Printf("cwd: currentCwd failed to resolve binding chat=%d thread=%d: %v", chatID, threadID, err)
		}
	}
	if bc.sessions == nil {
		return ""
	}
	return bc.sessions.GetCwd(chatID, threadID)
}

func (bc *BotController) setCurrentCwd(chatID int64, threadID int, userID int64, rawPath string) (string, error) {
	cwd, err := runtime.ResolveProjectCwd(rawPath)
	if err != nil {
		return "", err
	}
	if bc.bindings != nil {
		binding := projectbinding.ProjectBinding{
			Key:         projectbinding.ConversationKey{ChatID: chatID, ThreadID: threadID},
			CWD:         cwd,
			ProjectSlug: runtime.ProjectSlug(cwd),
			Source:      projectbinding.BindingManual,
			CreatedBy:   userID,
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := bc.bindings.Set(ctx, binding); err != nil {
			return "", err
		}
	}
	if bc.sessions != nil {
		bc.sessions.SetCwd(chatID, threadID, cwd)
	}
	return cwd, nil
}

func (bc *BotController) clearCurrentCwd(chatID int64, threadID int) error {
	key := projectbinding.ConversationKey{ChatID: chatID, ThreadID: threadID}
	if bc.bindings != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := bc.bindings.Delete(ctx, key); err != nil {
			return err
		}
	}
	if bc.sessions != nil {
		bc.sessions.ClearCwd(chatID, threadID)
	}
	return nil
}

func cwdClearThread(args string, threadID int) (int, bool, error) {
	switch args {
	case "clear":
		return threadID, true, nil
	case "clear --group":
		return 0, true, nil
	case "clear --topic":
		return threadID, true, nil
	default:
		if len(args) >= len("clear") && args[:len("clear")] == "clear" {
			return 0, false, fmt.Errorf("uso: /cwd clear ou /cwd clear --group")
		}
		return 0, false, nil
	}
}
