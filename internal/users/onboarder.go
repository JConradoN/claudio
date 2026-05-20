package users

import (
	"fmt"
	"os"
	"strings"
	"time"
)

// MessageSender abstracts sending messages during onboarding.
// The implementation is provided by the Telegram layer.
type MessageSender interface {
	SendText(chatID int64, threadID int, text string) (any, error)
}

// Onboarder manages the conversational onboarding flow for new users.
type Onboarder struct {
	store   *Store
	obStore *OnboardingStore
	sender  MessageSender
}

// NewOnboarder creates an Onboarder.
func NewOnboarder(store *Store, obStore *OnboardingStore, sender MessageSender) *Onboarder {
	return &Onboarder{store: store, obStore: obStore, sender: sender}
}

// Begin starts onboarding for a new user. Returns the greeting message.
func (o *Onboarder) Begin(userID, chatID int64, threadID int, firstMsg string) (string, error) {
	lang := detectLanguage(firstMsg)
	state := &OnboardingState{
		UserID: userID, ChatID: chatID, ThreadID: threadID,
		Step: "name", Language: lang, FirstMsg: firstMsg,
		StartedAt: time.Now(), UpdatedAt: time.Now(),
	}
	if err := o.obStore.Begin(state); err != nil {
		return "", fmt.Errorf("begin onboarding state: %w", err)
	}
	return o.greeting(lang), nil
}

func (o *Onboarder) greeting(lang string) string {
	if lang == "en" {
		return "Hello! I'm Aurelia. What should I call you?"
	}
	return "Olá! Eu sou a Aurelia. Como devo te chamar?"
}

// Step processes one reply from the user during onboarding.
// Returns (nextMessage, done, error).
func (o *Onboarder) Step(userID int64, reply string) (string, bool, error) {
	state, err := o.obStore.Get(userID)
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
		if err := o.obStore.Update(state); err != nil {
			return "", false, fmt.Errorf("update onboarding: %w", err)
		}
		if state.Language == "en" {
			return fmt.Sprintf("Nice to meet you, %s! Want to tell me something about yourself or your work? (Optional, you can skip)", state.Name), false, nil
		}
		return fmt.Sprintf("Prazer, %s! Quer me contar algo sobre você ou seu trabalho? (Opcional, pode pular)", state.Name), false, nil

	case "bio":
		bio := strings.TrimSpace(reply)
		if bio != "" && !strings.EqualFold(bio, "pular") && !strings.EqualFold(bio, "skip") {
			state.Bio = bio
		}
		state.Step = "done"
		state.UpdatedAt = time.Now()

		// Save profile
		profile := &Profile{
			UserID: userID, Name: state.Name, Language: state.Language,
			IsOwner: false, OnboardedAt: time.Now(), LastSeenAt: time.Now(),
		}
		if err := o.store.Save(profile); err != nil {
			return "", false, fmt.Errorf("save profile: %w", err)
		}
		// Write USER.md
		if err := o.writeUserMd(profile, state.Bio); err != nil {
			return "", false, fmt.Errorf("write user md: %w", err)
		}
		// Clean up onboarding state
		if err := o.obStore.Delete(userID); err != nil {
			return "", false, fmt.Errorf("delete onboarding state: %w", err)
		}

		if state.Language == "en" {
			return fmt.Sprintf("Thanks, %s! You're all set. I'll now process your first message.", state.Name), true, nil
		}
		return fmt.Sprintf("Obrigada, %s! Tudo pronto. Vou processar sua primeira mensagem agora.", state.Name), true, nil

	default:
		return "", false, fmt.Errorf("unknown onboarding step: %s", state.Step)
	}
}

func (o *Onboarder) writeUserMd(p *Profile, bio string) error {
	path := o.store.resolver.UserMdPath(p.UserID)
	dir := o.store.resolver.PersonasDir(p.UserID)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create personas dir: %w", err)
	}

	name := yamlEscape(p.Name)
	bioEscaped := yamlEscape(bio)
	var content string
	if bio == "" {
		content = fmt.Sprintf("---\nuser_id: %d\nname: %s\nlanguage: %s\n---\n\n# User Profile\n\nUser id: %d\n", p.UserID, name, p.Language, p.UserID)
	} else {
		content = fmt.Sprintf("---\nuser_id: %d\nname: %s\nlanguage: %s\n---\n\n# User Profile\n\n%s\n", p.UserID, name, p.Language, bioEscaped)
	}
	return os.WriteFile(path, []byte(content), 0o600)
}

// yamlEscape escapes a user-supplied string for safe embedding in YAML frontmatter.
func yamlEscape(s string) string {
	if s == "" {
		return "''"
	}
	escaped := strings.ReplaceAll(s, "'", "''")
	return "'" + escaped + "'"
}

// Active returns true if the user is mid-onboarding (has state and step != "done").
func (o *Onboarder) Active(userID int64) bool {
	state, err := o.obStore.Get(userID)
	return err == nil && state != nil && state.Step != "done"
}

// detectLanguage infers language from first message text using a simple token heuristic.
func detectLanguage(text string) string {
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
