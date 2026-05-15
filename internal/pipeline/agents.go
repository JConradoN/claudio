package pipeline

import "github.com/igormaneschy/aurelia/internal/orchestrator"

func (s *Service) buildAgentSummaries() []orchestrator.AgentSummary {
	if s.agents == nil {
		return nil
	}
	summaries := make([]orchestrator.AgentSummary, 0, len(s.agents.Agents()))
	for _, a := range s.agents.Agents() {
		summaries = append(summaries, orchestrator.AgentSummary{
			Name:        a.Name,
			Description: a.Description,
			Tools:       a.AllowedTools,
			ReadOnly:    a.IsReadOnly(),
		})
	}
	return summaries
}
