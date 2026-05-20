package telegram

import (
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/igormaneschy/aurelia/internal/users"
)

// UserGateState describes a user's relationship to the onboarding gate.
type UserGateState int

const (
	// UserGateOK means the user has a profile — proceed to normal routing.
	UserGateOK UserGateState = iota
	// UserGateNeedsOnboarding means the user has no profile — start onboarding.
	UserGateNeedsOnboarding
	// UserGateOnboarding means the user is mid-onboarding — continue the flow.
	UserGateOnboarding
)

// UserGate intercepts messages from users without profiles and routes them
// through the conversational onboarding flow before they can interact normally.
type UserGate struct {
	store   *users.Store
	obStore *users.OnboardingStore
}

// NewUserGate creates a UserGate.
func NewUserGate(store *users.Store, obStore *users.OnboardingStore) *UserGate {
	return &UserGate{store: store, obStore: obStore}
}

// Check determines what state the user is in relative to the gate.
func (g *UserGate) Check(userID int64) UserGateState {
	if g.store.Exists(userID) {
		return UserGateOK
	}
	state, err := g.obStore.Get(userID)
	if err != nil {
		slog.Default().Error("user_gate: check onboarding state", "user_id", userID, "error", err)
		return UserGateNeedsOnboarding
	}
	if state != nil && state.Step != "done" {
		return UserGateOnboarding
	}
	return UserGateNeedsOnboarding
}

// Begin starts the onboarding flow for a new user. Returns the greeting message.
func (g *UserGate) Begin(userID, chatID int64, threadID int, firstMsg string) (string, error) {
	lang := detectLanguageForGate(firstMsg)
	state := &users.OnboardingState{
		UserID: userID, ChatID: chatID, ThreadID: threadID,
		Step: "name", Language: lang, FirstMsg: firstMsg,
		StartedAt: time.Now(), UpdatedAt: time.Now(),
	}
	if err := g.obStore.Begin(state); err != nil {
		return "", fmt.Errorf("begin onboarding state: %w", err)
	}
	return greetingForLanguage(lang), nil
}

// Step processes one reply from the user during onboarding.
// Returns (replyText, done, error).
func (g *UserGate) Step(userID int64, reply string) (string, bool, error) {
	state, err := g.obStore.Get(userID)
	if err != nil {
		return "", false, fmt.Errorf("get onboarding state: %w", err)
	}
	if state == nil {
		return "", false, fmt.Errorf("onboarding state not found for user %d", userID)
	}

	switch state.Step {
	case "name":
		state.Name = strings.TrimSpace(reply)
		state.Step = "bio"
		state.UpdatedAt = time.Now()
		if err := g.obStore.Update(state); err != nil {
			return "", false, fmt.Errorf("update onboarding: %w", err)
		}
		if state.Language == "en" {
			return fmt.Sprintf("Nice to meet you, %s! Want to tell me something about yourself or your work? (Optional, you can skip)", state.Name), false, nil
		}
		return fmt.Sprintf("Prazer, %s! Quer me contar algo sobre você ou seu trabalho? (Opcional, pode pular)", state.Name), false, nil

	case "bio":
		bio := strings.TrimSpace(reply)
		if bio != "" && !equalsFoldAny(bio, "pular", "skip") {
			state.Bio = bio
		}
		state.Step = "done"
		state.UpdatedAt = time.Now()

		// Save profile
		profile := &users.Profile{
			UserID: userID, Name: state.Name, Language: state.Language,
			IsOwner: false, OnboardedAt: time.Now(), LastSeenAt: time.Now(),
		}
		if err := g.store.Save(profile); err != nil {
			return "", false, fmt.Errorf("save profile: %w", err)
		}
		// Write USER.md
		if err := writeUserMd(g.store, profile, state.Bio); err != nil {
			return "", false, fmt.Errorf("write user md: %w", err)
		}
		// NOTE: onboarding state is NOT deleted here — the caller (processInputWithImages)
		// reads FirstMsg() before calling Complete(), which deletes the state.

		if state.Language == "en" {
			return fmt.Sprintf("Thanks, %s! You're all set. I'll now process your first message.", state.Name), true, nil
		}
		return fmt.Sprintf("Obrigada, %s! Tudo pronto. Vou processar sua primeira mensagem agora.", state.Name), true, nil

	default:
		return "", false, fmt.Errorf("unknown onboarding step: %s", state.Step)
	}
}

// FirstMsg returns the original first message that triggered onboarding.
// Used after onboarding completes to re-process the message.
func (g *UserGate) FirstMsg(userID int64) string {
	state, err := g.obStore.Get(userID)
	if err != nil || state == nil {
		return ""
	}
	return state.FirstMsg
}

// Complete deletes the onboarding state after successful completion.
func (g *UserGate) Complete(userID int64) error {
	return g.obStore.Delete(userID)
}

// -- helpers --

func equalsFoldAny(s string, candidates ...string) bool {
	for _, c := range candidates {
		if strings.EqualFold(s, c) {
			return true
		}
	}
	return false
}

func greetingForLanguage(lang string) string {
	if lang == "en" {
		return "Hello! I'm Aurelia. What should I call you?"
	}
	return "Olá! Eu sou a Aurelia. Como devo te chamar?"
}

func detectLanguageForGate(text string) string {
	words := strings.Fields(strings.ToLower(text))
	ptScore, enScore := 0, 0
	ptTokens := map[string]bool{
		"o": true, "a": true, "de": true, "que": true, "não": true,
		"sim": true, "obrigado": true, "olá": true, "tudo": true, "bem": true,
		"para": true, "com": true, "por": true, "uma": true, "um": true,
	}
	enTokens := map[string]bool{
		"the": true, "is": true, "and": true, "you": true,
		"thanks": true, "please": true, "hello": true, "hi": true,
		"good": true, "how": true, "are": true, "what": true,
	}
	for _, w := range words {
		if ptTokens[w] {
			ptScore++
		}
		if enTokens[w] {
			enScore++
		}
	}
	if enScore > ptScore {
		return "en"
	}
	return "pt"
}

func writeUserMd(store *users.Store, p *users.Profile, bio string) error {
	path := store.Resolver().UserMdPath(p.UserID)
	dir := store.Resolver().PersonasDir(p.UserID)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create personas dir: %w", err)
	}
	// Escape YAML special characters in user-supplied fields
	name := yamlEscape(p.Name)
	var content string
	if bio == "" {
		content = fmt.Sprintf("---\nuser_id: %d\nname: %s\nlanguage: %s\n---\n\n# User Profile\n\nUser id: %d\n", p.UserID, name, p.Language, p.UserID)
	} else {
		bioEscaped := yamlEscape(bio)
		content = fmt.Sprintf("---\nuser_id: %d\nname: %s\nlanguage: %s\n---\n\n# User Profile\n\n%s\n", p.UserID, name, p.Language, bioEscaped)
	}
	return os.WriteFile(path, []byte(content), 0o600)
}

// yamlEscape escapes a user-supplied string for safe embedding in YAML frontmatter.
// Single-quote wraps the value and doubles any embedded single quotes.
func yamlEscape(s string) string {
	if s == "" {
		return "''"
	}
	escaped := strings.ReplaceAll(s, "'", "''")
	return "'" + escaped + "'"
}
