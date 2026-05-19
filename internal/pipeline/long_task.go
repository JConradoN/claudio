package pipeline

import "strings"

// longTaskKeywords are words that suggest a task may take many steps.
var longTaskKeywords = []string{
	// Portuguese — coding/implementation verbs
	"implementa", "implemente", "implementar",
	"corrigi", "corrige", "corrigir", "arruma", "arrumar", "conserta", "concertar",
	"refatora", "refatore", "refatorar",
	"cria", "crie", "criar", "constroi", "construa", "construir",
	"adiciona", "adicione", "adicionar",
	"migra", "migre", "migrar",
	"configura", "configure", "configurar",
	"valida", "valide", "validar",
	"investiga", "investigue", "investigar",
	"ativa", "ative", "ativar",
	"roda", "rode", "rodar",
	"testa", "teste", "testar",
	"faz", "faca", "fazer",
	"continua", "continue", "continuar",
	"siga", "segue", "seguir",
	// English
	"implement", "implementing",
	"fix", "fixing", "fixes",
	"refactor", "refactoring",
	"create", "creating",
	"add", "adding",
	"migrate", "migrating",
	"configure", "configuring",
	"validate", "validating",
	"investigate", "investigating",
	"activate", "activating",
	"run", "running",
	"test", "testing",
	"continue",
}

// multiStepMarkers suggest the task has multiple steps or sub-tasks.
var multiStepMarkers = []string{
	// Portuguese
	"e depois", "também", "tambem", "alem disso", "alem disto",
	"primeiro", "segundo", "terceiro", "em seguida",
	"passo", "etapa", "parte", "sequencia",
	"cada", "todos os", "tudo que", "o seguinte",
	"lista", "relacao", "cada um",
	// English
	"and then", "also", "furthermore", "moreover",
	"first", "second", "third", "next",
	"step", "stage", "phase", "sequence",
	"each", "every", "all the", "the following",
	"list", "enumerate",
}

// looksLikeLongTask returns true when the user message suggests a complex,
// multi-step task that would benefit from checkpointing.
func looksLikeLongTask(text string, hasCwd bool) bool {
	lower := strings.ToLower(strings.TrimSpace(text))
	if lower == "" {
		return false
	}

	// Without a cwd, only surface-level tasks are possible.
	// Require a coding verb for long-task detection.
	if !hasCwd {
		return false
	}

	hasVerb := false
	for _, kw := range longTaskKeywords {
		if strings.Contains(lower, kw) {
			hasVerb = true
			break
		}
	}
	if !hasVerb {
		return false
	}

	// Check for multi-step markers
	for _, marker := range multiStepMarkers {
		if strings.Contains(lower, marker) {
			return true
		}
	}

	return false
}

// longTaskGuidance returns a system prompt snippet that tells the model to
// break work into small checkpointed steps.
func longTaskGuidance() string {
	return `This appears to be a multi-step task. Work through it step by step:

1. Break the task into clear, small steps.
2. After each step, provide a brief status summary.
3. If the task is complex, ask before making major changes.

Use the Write tool to save checkpoint notes if you need to track progress across steps.
Keep each step focused and report status after completion.`
}
