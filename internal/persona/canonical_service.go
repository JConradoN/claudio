package persona

import (
	"fmt"
	"log"
	"os"
)

// CanonicalIdentityService loads persona files and builds a system prompt.
type CanonicalIdentityService struct {
	identityPath        string
	soulPath            string
	userPath            string
	ownerPlaybookPath   string
	lessonsLearnedPath  string
	projectPlaybookPath string
}

// NewCanonicalIdentityService creates a persona loader.
func NewCanonicalIdentityService(
	identityPath, soulPath, userPath string,
	ownerPlaybookPath, lessonsLearnedPath string,
	projectPlaybookPath string,
) *CanonicalIdentityService {
	return &CanonicalIdentityService{
		identityPath:        identityPath,
		soulPath:            soulPath,
		userPath:            userPath,
		ownerPlaybookPath:   ownerPlaybookPath,
		lessonsLearnedPath:  lessonsLearnedPath,
		projectPlaybookPath: projectPlaybookPath,
	}
}

// PromptOptions controls optional prompt injections.
type PromptOptions struct {
	IsOwner bool
}

// BuildPrompt loads persona files and returns the assembled system prompt.
// Deprecated: Use BuildPromptForUser for per-user persona isolation.
func (s *CanonicalIdentityService) BuildPrompt() (string, error) {
	p, err := LoadPersona(s.identityPath, s.soulPath, s.userPath)
	if err != nil {
		return "", err
	}

	prompt := p.SystemPrompt

	ownerBlock := s.buildOwnerDocsBlock()
	if ownerBlock != "" {
		prompt = prompt + "\n\n" + ownerBlock
	}

	projectBlock := s.buildProjectBlock()
	if projectBlock != "" {
		prompt = prompt + "\n\n" + projectBlock
	}

	return prompt, nil
}

// BuildPromptForUser loads IDENTITY + SOUL (global) and USER.md (per-user),
// injecting owner docs only when isOwner is true.
func (s *CanonicalIdentityService) BuildPromptForUser(userID int64, resolver interface{ UserMdPath(userID int64) string }, isOwner bool) (string, error) {
	identityBytes, err := readPersonaFile(s.identityPath, "identity")
	if err != nil {
		return "", fmt.Errorf("build prompt for user: %w", err)
	}
	soulBytes, err := readPersonaFile(s.soulPath, "soul")
	if err != nil {
		return "", fmt.Errorf("build prompt for user: %w", err)
	}

	// Load per-user USER.md, falling back to a minimal stub if missing.
	userMdPath := resolver.UserMdPath(userID)
	userBytes, err := os.ReadFile(userMdPath)
	if err != nil {
		log.Printf("persona: per-user USER.md not found at %q for user %d, using stub", userMdPath, userID)
		userBytes = []byte(fmt.Sprintf("---\nuser_id: %d\nname: User\nlanguage: pt\n---\n\n# User\n\nUser id: %d\n", userID, userID))
	}

	config, identityBody, err := parseIdentityFrontmatter(identityBytes)
	if err != nil {
		return "", fmt.Errorf("build prompt for user: %w", err)
	}

	promptBody := buildPromptBody(identityBody, soulBytes, userBytes)

	canonicalIdentity := CanonicalIdentity{
		AgentName: canonicalValue(config.Name),
		AgentRole: canonicalValue(config.Role),
		UserName:  canonicalUserName(userBytes),
	}

	prompt := buildSystemPrompt(canonicalIdentity, promptBody)

	if isOwner {
		ownerBlock := s.buildOwnerDocsBlock()
		if ownerBlock != "" {
			prompt = prompt + "\n\n" + ownerBlock
		}
	}

	projectBlock := s.buildProjectBlock()
	if projectBlock != "" {
		prompt = prompt + "\n\n" + projectBlock
	}

	return prompt, nil
}
