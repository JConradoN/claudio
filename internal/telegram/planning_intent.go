package telegram

import "strings"

var planningKeywords = []string{
	// Portuguese
	"implementa", "implemente", "implanta", "crie", "criar", "construa", "construir",
	"planejar", "planeje", "planeja", "plano", "spec", "design", "tarefa",
	"refatorar", "refatore", "migrar", "migre", "reescrever", "reescreva",
	"adicionar", "adicione", "feature", "funcionalidade",
	"aprovado", "pode fazer", "manda ver", "bora", "execute",
	// English
	"implement", "build", "create", "plan", "refactor", "migrate", "rewrite",
	"add feature", "approved", "execute", "ship it",
}

// looksLikePlanningIntent returns true when the user message suggests they want
// to plan, implement, or execute something — not just chat or ask questions.
func looksLikePlanningIntent(text string) bool {
	lower := strings.ToLower(text)
	for _, kw := range planningKeywords {
		if strings.Contains(lower, kw) {
			return true
		}
	}
	return false
}
