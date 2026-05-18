package telegram

import (
	"context"
	"fmt"
	"log"
	"strings"
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

// cwdSetTarget holds the parsed result of /cwd [--group|--topic] <path>.
type cwdSetTarget struct {
	ThreadID int
	Path     string
	Scope    string // "group" or "topic" for response message wording
	Explicit bool   // true if --group or --topic was explicitly provided
}

// parseCwdSetTarget parses scope flags from the beginning of args:
//
//	--group <path>  → ThreadID=0, Scope="group", Explicit=true
//	--topic <path>  → ThreadID=currentThreadID, Scope="topic", Explicit=true
//	<path>          → ThreadID=currentThreadID, Scope determined by currentThreadID, Explicit=false
//
// Flags after path are not supported; the whole rest after the flag is treated as path.
func parseCwdSetTarget(args string, currentThreadID int) (cwdSetTarget, error) {
	args = strings.TrimSpace(args)
	if args == "" {
		return cwdSetTarget{}, fmt.Errorf("uso: /cwd [--group|--topic] <path>")
	}

	// Check for --group flag
	if rest, ok := strings.CutPrefix(args, "--group "); ok {
		rest = strings.TrimSpace(rest)
		if rest == "" {
			return cwdSetTarget{}, fmt.Errorf("uso: /cwd --group <path>")
		}
		return cwdSetTarget{ThreadID: 0, Path: rest, Scope: "group", Explicit: true}, nil
	}
	if args == "--group" {
		return cwdSetTarget{}, fmt.Errorf("uso: /cwd --group <path>")
	}

	// Check for --topic flag
	if rest, ok := strings.CutPrefix(args, "--topic "); ok {
		rest = strings.TrimSpace(rest)
		if rest == "" {
			return cwdSetTarget{}, fmt.Errorf("uso: /cwd --topic <path>")
		}
		return cwdSetTarget{ThreadID: currentThreadID, Path: rest, Scope: "topic", Explicit: true}, nil
	}
	if args == "--topic" {
		return cwdSetTarget{}, fmt.Errorf("uso: /cwd --topic <path>")
	}

	// No flag: default to current thread
	scope := "topic"
	if currentThreadID == 0 {
		scope = "group"
	}
	return cwdSetTarget{ThreadID: currentThreadID, Path: args, Scope: scope, Explicit: false}, nil
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
