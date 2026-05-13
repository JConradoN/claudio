package telegram

import (
	"context"
	"log"
	"path/filepath"
	"strings"
	"time"

	"github.com/igormaneschy/aurelia/internal/orchestrator"

	"gopkg.in/telebot.v3"
)

const orchestrationTimeout = 15 * time.Minute

// executeApprovedPlan runs the full TLC Implement+Validate cycle:
// ensure docs → spawn workers per wave → validate → merge → consolidate.
func (bc *BotController) executeApprovedPlan(chat *telebot.Chat, messageID int, plan *orchestrator.Plan) {
	ctx, cancel := context.WithTimeout(context.Background(), orchestrationTimeout)
	defer cancel()

	repoRoot := bc.getRepoRoot()

	// 0. Ensure CLAUDE.md and AGENTS.md exist
	agentSummaries := bc.buildAgentSummaries()
	if err := orchestrator.EnsureClaudeMd(repoRoot); err != nil {
		log.Printf("EnsureClaudeMd: %v", err)
	}
	if err := orchestrator.EnsureAgentsMd(repoRoot, agentSummaries); err != nil {
		log.Printf("EnsureAgentsMd: %v", err)
	}

	// 1. Send plan summary
	status := newWorkerStatusReporter(bc.bot, chat)
	status.SendPlanSummary(plan, messageID)

	// 2. Read context files for worker prompts
	claudeMd := orchestrator.ReadFileContent(filepath.Join(repoRoot, "CLAUDE.md"))
	agentsMd := orchestrator.ReadFileContent(filepath.Join(repoRoot, "AGENTS.md"))
	specContent := bc.findFeatureDoc(repoRoot, "spec.md")
	designContent := bc.findFeatureDoc(repoRoot, "design.md")

	// 3. Execute workers
	results, err := bc.orchestrator.ExecutePlan(
		ctx,
		plan,
		bc.agents,
		func(task orchestrator.Task, cfg orchestrator.WorkerConfig) string {
			waves, _ := plan.ExecutionOrder()
			var siblings []orchestrator.Task
			for _, wave := range waves {
				for _, t := range wave {
					if t.ID != task.ID {
						siblings = append(siblings, t)
					}
				}
			}
			return orchestrator.BuildWorkerPrompt(cfg.Prompt, claudeMd, agentsMd, specContent, designContent, task, siblings)
		},
		func(ev orchestrator.WorkerEvent) {
			switch ev.Type {
			case "start":
				status.SendStart(ev.TaskID, ev.Message)
			case "progress":
				status.UpdateProgress(ev.TaskID, ev.ToolName)
			case "done":
				status.MarkDone(ev.TaskID, 0)
			case "error":
				status.MarkError(ev.TaskID, ev.Message)
			}
		},
	)

	if err != nil {
		log.Printf("ExecutePlan error: %v", err)
		_ = SendError(bc.bot, chat, "Erro na execução do plano: "+err.Error())
		return
	}

	// 4. Validate each result
	validationPrompt := orchestrator.BuildValidationPrompt(specContent, designContent)
	for i, r := range results {
		task := findTaskInPlan(plan, r.TaskID)
		vr, err := bc.orchestrator.Validate(ctx, task, r, validationPrompt)
		if err != nil {
			log.Printf("Validate error for %s: %v", r.TaskID, err)
			continue
		}
		if vr.Approved {
			status.MarkDone(r.TaskID, r.DurationMs)
		} else {
			issues := strings.Join(vr.Issues, "; ")
			status.MarkError(r.TaskID, "Reprovado: "+issues)
			results[i].Success = false
			results[i].Error = issues
		}
	}

	// 5. Consolidate and respond
	persona := ""
	if bc.persona != nil {
		persona, _ = bc.persona.BuildPrompt()
	}
	consolidationPrompt := orchestrator.BuildConsolidationPrompt(persona, plan, results)
	finalText, err := bc.orchestrator.Consolidate(ctx, plan, results, consolidationPrompt)
	if err != nil {
		log.Printf("Consolidate error: %v", err)
	}
	if finalText == "" {
		finalText = buildFallbackConsolidation(results)
	}

	if err := SendTextReply(bc.bot, chat, finalText, messageID); err != nil {
		log.Printf("Failed to send consolidation: %v", err)
	}
}

func findTaskInPlan(plan *orchestrator.Plan, taskID string) orchestrator.Task {
	for _, t := range plan.Tasks {
		if t.ID == taskID {
			return t
		}
	}
	return orchestrator.Task{ID: taskID}
}

func buildFallbackConsolidation(results []orchestrator.TaskResult) string {
	var sb strings.Builder
	sb.WriteString("**Resultado da execução:**\n\n")
	for _, r := range results {
		if r.Success {
			sb.WriteString("✅ " + r.TaskID + " — Concluído\n")
		} else {
			sb.WriteString("❌ " + r.TaskID + " — " + r.Error + "\n")
		}
	}
	return sb.String()
}

func (bc *BotController) getRepoRoot() string {
	// Use the chat's cwd if set, otherwise fall back to working dir
	// In practice this comes from the orchestrator config
	if bc.orchestrator != nil {
		return bc.orchestrator.Config().RepoRoot
	}
	return "."
}

func (bc *BotController) buildAgentSummaries() []orchestrator.AgentSummary {
	if bc.agents == nil {
		return nil
	}
	var summaries []orchestrator.AgentSummary
	for _, a := range bc.agents.Agents() {
		readOnly := true
		for _, t := range a.AllowedTools {
			if t == "Write" || t == "Edit" || t == "Bash" {
				readOnly = false
				break
			}
		}
		summaries = append(summaries, orchestrator.AgentSummary{
			Name:        a.Name,
			Description: a.Description,
			Tools:       a.AllowedTools,
			ReadOnly:    readOnly,
		})
	}
	return summaries
}

func (bc *BotController) findFeatureDoc(repoRoot, filename string) string {
	// Try to find the most recent feature spec/design
	// Simple approach: look in .specs/features/*/filename
	pattern := filepath.Join(repoRoot, ".specs", "features", "*", filename)
	matches, _ := filepath.Glob(pattern)
	if len(matches) == 0 {
		return ""
	}
	// Return the last one (most recently modified directory tends to be last alphabetically)
	return orchestrator.ReadFileContent(matches[len(matches)-1])
}
