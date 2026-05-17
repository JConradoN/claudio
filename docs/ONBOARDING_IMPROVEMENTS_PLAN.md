# Implementation Plan: Onboarding Improvements

## Overview

This document provides the exact code changes needed to implement the onboarding improvements approved by Igor. Each section contains:
- **Goal**: What problem this solves
- **Files to modify**: Exact file paths
- **Changes**: Precise diffs (old → new)
- **Validation**: How to verify the change works

---

## P0: Onboarded() Guardrail

### Goal
Prevent the daemon from starting with an incomplete configuration. Instead of a cryptic Telegram bot error, show a friendly message directing the user to run the onboarding wizard.

### Files to Modify

#### 1. `internal/config/config.go`

Add `Onboarded()` method to `AppConfig` (after `VisionFallback()`):

```go
// Onboarded returns true if the config has the minimum required fields to run.
func (c *AppConfig) Onboarded() bool {
	return c.TelegramBotToken != "" && len(c.TelegramAllowedUserIDs) > 0 && c.DefaultProvider != ""
}
```

Insert after line 102 (after `VisionFallback`), before `Editable()`.

#### 2. `cmd/aurelia/main.go`

Add guardrail check after `bootstrapApp()` returns, before `setupSlog`:

```go
	app, err := bootstrapApp()
	if err != nil {
		log.Fatalf("Failed to bootstrap Aurelia: %v", err)
	}

	// Guardrail: require onboarding before starting daemon
	if !app.config.Onboarded() {
		log.Println("Aurelia is not configured yet.")
		log.Println("Run the onboarding wizard first:")
		log.Println("")
		log.Println("    go run ./cmd/aurelia/ onboard")
		log.Println("")
		log.Println("Then start the daemon:")
		log.Println("")
		log.Println("    go run ./cmd/aurelia/")
		os.Exit(1)
	}

	// Set up structured logging from config.
```

**Note**: Add `"os"` to imports if not already present (it is already imported on line 8).

### Validation

1. Delete or rename `~/.aurelia/config/app.json`
2. Run `go run ./cmd/aurelia/`
3. **Expected**: Process exits with code 1 and prints the onboarding instructions
4. Run `go run ./cmd/aurelia/ onboard` and complete the wizard
5. Run `go run ./cmd/aurelia/` again
6. **Expected**: Daemon starts normally

---

## P1: Telegram Token Validation

### Goal
Validate the Telegram bot token during onboarding by calling the Telegram `getMe` API. Catch invalid tokens before saving the config, rather than failing at daemon startup.

### Files to Modify

#### 1. `internal/onboarding/onboard.go`

Add import for `net/http` and `encoding/json`:

```go
import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/igormaneschy/aurelia/internal/config"
	"github.com/igormaneschy/aurelia/internal/deps"
	"github.com/igormaneschy/aurelia/internal/runtime"
	"golang.org/x/term"
)
```

Add token validation function at the end of the file (before `termFd`):

```go
// validateTelegramToken calls Telegram's getMe API to verify a bot token.
// Returns the bot's username on success, or an error if the token is invalid.
func validateTelegramToken(token string) (string, error) {
	if token == "" {
		return "", fmt.Errorf("token is empty")
	}
	url := fmt.Sprintf("https://api.telegram.org/bot%s/getMe", token)
	resp, err := http.Get(url)
	if err != nil {
		return "", fmt.Errorf("failed to reach Telegram API: %w", err)
	}
	defer resp.Body.Close()

	var result struct {
		OK     bool `json:"ok"`
		Result struct {
			Username string `json:"username"`
		} `json:"result"`
		Description string `json:"description"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("failed to decode Telegram response: %w", err)
	}
	if !result.OK {
		return "", fmt.Errorf("invalid token: %s", result.Description)
	}
	return result.Result.Username, nil
}
```

#### 2. `internal/onboarding/onboard.go` — Prompt Mode

In `RunOnboardPrompt`, after the token prompt (around line 162), add validation:

Replace:
```go
	current.TelegramBotToken, _ = promptString(reader, stdout, "Telegram bot token", current.TelegramBotToken, true)
```

With:
```go
	for {
		current.TelegramBotToken, _ = promptString(reader, stdout, "Telegram bot token", current.TelegramBotToken, true)
		if current.TelegramBotToken == "" {
			if err := writeln(stdout, "Token is required."); err != nil {
				return err
			}
			continue
		}
		if err := writeln(stdout, "Validating token with Telegram..."); err != nil {
			return err
		}
		username, err := validateTelegramToken(current.TelegramBotToken)
		if err != nil {
			if err := writef(stdout, "Invalid token: %v\nPlease try again.\n\n", err); err != nil {
				return err
			}
			continue
		}
		if err := writef(stdout, "Token valid! Bot: @%s\n\n", username); err != nil {
			return err
		}
		break
	}
```

#### 3. `internal/onboarding/onboard_ui.go` — TUI Mode

In the TUI `HandleKey` method, when advancing from `stepTelegramToken` to `stepTelegramUsers`, add validation. This requires checking the token before allowing the step transition.

Locate the step transition logic (likely in `advanceStep` or similar). After the user enters the token and presses Enter, validate it:

```go
// In the TUI step handler for stepTelegramToken, after capturing input:
if ui.step == stepTelegramToken && ui.input != "" {
	ui.message = "Validating token with Telegram..."
	// Trigger a re-render
	if username, err := validateTelegramToken(ui.input); err != nil {
		ui.message = fmt.Sprintf("Invalid token: %v", err)
		ui.input = "" // clear so user can re-enter
	} else {
		ui.cfg.TelegramBotToken = ui.input
		ui.input = ""
		ui.message = fmt.Sprintf("Token valid! Bot: @%s", username)
		ui.step = stepTelegramUsers
	}
}
```

**Note**: The exact integration point depends on the TUI architecture. The TUI uses a state machine in `onboard_ui.go`. Find where `stepTelegramToken` transitions occur and insert validation there.

### Validation

1. Run `go run ./cmd/aurelia/ onboard`
2. Enter an invalid token (e.g., `123:invalid`)
3. **Expected**: Error message "Invalid token: ...", prompt repeats
4. Enter a valid token from @BotFather
5. **Expected**: "Token valid! Bot: @your_bot_name"
6. Complete onboarding
7. Run `go run ./cmd/aurelia/`
8. **Expected**: Daemon starts successfully

---

## P2: Internationalization (i18n)

### Goal
Enable multi-language support with Portuguese (pt-BR) as default and English as fallback. All user-facing Telegram messages should use the i18n system.

### Files to Create/Modify

#### 1. Create `internal/i18n/i18n.go`

```go
package i18n

import (
	"fmt"
	"strings"
)

// Locale represents a supported language.
type Locale string

const (
	LocalePTBR Locale = "pt-BR"
	LocaleEN   Locale = "en"
)

// DefaultLocale is the fallback when no locale is specified.
var DefaultLocale = LocalePTBR

// Bundle holds translations for a locale.
type Bundle struct {
	locale       Locale
	translations map[string]string
}

// NewBundle creates a translation bundle for the given locale.
func NewBundle(locale Locale) *Bundle {
	b := &Bundle{
		locale:       locale,
		translations: make(map[string]string),
	}
	b.load()
	return b
}

// T returns the translation for the given key, or the key itself if not found.
func (b *Bundle) T(key string) string {
	if v, ok := b.translations[key]; ok {
		return v
	}
	return key
}

// Tf returns a formatted translation.
func (b *Bundle) Tf(key string, args ...any) string {
	return fmt.Sprintf(b.T(key), args...)
}

func (b *Bundle) load() {
	switch b.locale {
	case LocalePTBR:
		b.loadPTBR()
	case LocaleEN:
		b.loadEN()
	default:
		b.loadPTBR()
	}
}
```

#### 2. Create `internal/i18n/pt_br.go`

```go
package i18n

func (b *Bundle) loadPTBR() {
	b.translations = map[string]string{
		"unsupported_document": "⚠️ **Formato não suportado**\n\n" +
			"No momento eu consigo processar:\n" +
			"- arquivos `.md`\n" +
			"- arquivos `.pdf`\n" +
			"- imagens em `.jpg`, `.png`, `.gif` ou `.webp`\n" +
			"- áudio e voz\n\n" +
			"💡 Dica: converta para `.pdf` ou copie o texto diretamente.",

		"download_failure": "❌ **Falha no download**\n\n" +
			"Não consegui baixar o arquivo enviado pelo Telegram. Tente novamente.",

		"audio_not_configured": "⚠️ **Áudio indisponível**\n\n" +
			"Meu módulo de transcrição não está configurado.\n\n" +
			"Configure `groq_api_key` no arquivo `~/.aurelia/config/app.json`.",

		"audio_processing_failure": "❌ **Falha na transcrição**\n\n" +
			"Não consegui compreender o áudio. Tente falar mais claro ou mais perto do microfone.",

		"empty_audio": "⚠️ **Áudio vazio**\n\n" +
			"Não captei conteúdo útil. Pode reenviar?",

		"already_configured": "✅ **Aurelia online**\n\n" +
			"Já estou configurado e pronto. Como posso ajudar?",

		"bootstrap_welcome": "# Boas-vindas\n\n" +
			"Eu sou o **Aurelia** recém-iniciado.\n\n" +
			"Escolha como você quer que eu atue primariamente hoje.",

		"bootstrap_failure": "❌ **Falha no bootstrap**\n\n" +
			"Não consegui criar os arquivos base de persona.",

		"bootstrap_assistant": "✅ **Modo inicial selecionado**\n\n" +
			"Agora descreva como você quer que eu seja: personalidade, tom, estilo.\n\n" +
			"Exemplo: `Quero um assistente direto, sem floreios, que use humor seco quando apropriado.`",

		"bootstrap_profile": "✅ **Personalidade configurada**\n\n" +
			"Agora me diga seu nome e como prefere que eu trabalhe com você.\n\n" +
			"Exemplo: `Me chamo Igor, sou dev e quero respostas diretas.`",

		"bootstrap_success": "✅ **Personas criadas**\n\n" +
			"Suas configurações base foram salvas em `~/.aurelia/memory/personas/`.\n\n" +
			"Você já pode conversar comigo ou editar:\n" +
			"- `IDENTITY.md`\n" +
			"- `SOUL.md`\n" +
			"- `USER.md`\n\n" +
			"para refinar nosso comportamento.",
	}
}
```

#### 3. Create `internal/i18n/en.go`

```go
package i18n

func (b *Bundle) loadEN() {
	b.translations = map[string]string{
		"unsupported_document": "⚠️ **Unsupported format**\n\n" +
			"I can currently process:\n" +
			"- `.md` files\n" +
			"- `.pdf` files\n" +
			"- images in `.jpg`, `.png`, `.gif`, or `.webp`\n" +
			"- audio and voice\n\n" +
			"💡 Tip: convert to `.pdf` or copy the text directly.",

		"download_failure": "❌ **Download failed**\n\n" +
			"I couldn't download the file sent via Telegram. Please try again.",

		"audio_not_configured": "⚠️ **Audio unavailable**\n\n" +
			"My transcription module is not configured.\n\n" +
			"Set `groq_api_key` in `~/.aurelia/config/app.json`.",

		"audio_processing_failure": "❌ **Transcription failed**\n\n" +
			"I couldn't understand the audio. Try speaking more clearly or closer to the microphone.",

		"empty_audio": "⚠️ **Empty audio**\n\n" +
			"I didn't catch any useful content. Can you resend?",

		"already_configured": "✅ **Aurelia online**\n\n" +
			"I'm configured and ready. How can I help?",

		"bootstrap_welcome": "# Welcome\n\n" +
			"I'm **Aurelia**, freshly started.\n\n" +
			"Choose how you want me to act primarily today.",

		"bootstrap_failure": "❌ **Bootstrap failed**\n\n" +
			"I couldn't create the base persona files.",

		"bootstrap_assistant": "✅ **Initial mode selected**\n\n" +
			"Now describe how you want me to be: personality, tone, style.\n\n" +
			"Example: `I want a direct assistant, no fluff, who uses dry humor when appropriate.`",

		"bootstrap_profile": "✅ **Personality configured**\n\n" +
			"Now tell me your name and how you prefer me to work with you.\n\n" +
			"Example: `My name is Igor, I'm a dev and I want direct answers.`",

		"bootstrap_success": "✅ **Personas created**\n\n" +
			"Your base settings have been saved to `~/.aurelia/memory/personas/`.\n\n" +
			"You can now chat with me or edit:\n" +
			"- `IDENTITY.md`\n" +
			"- `SOUL.md`\n" +
			"- `USER.md`\n\n" +
			"to refine our behavior.",
	}
}
```

#### 4. Modify `internal/telegram/messages.go`

Replace the hardcoded constants with i18n-backed functions:

```go
package telegram

import "github.com/igormaneschy/aurelia/internal/i18n"

var bundle = i18n.NewBundle(i18n.DefaultLocale)

func unsupportedDocumentMessage() string  { return bundle.T("unsupported_document") }
func downloadFailureMessage() string      { return bundle.T("download_failure") }
func audioNotConfiguredMessage() string   { return bundle.T("audio_not_configured") }
func audioProcessingFailureMessage() string { return bundle.T("audio_processing_failure") }
func emptyAudioMessage() string           { return bundle.T("empty_audio") }
func alreadyConfiguredMessage() string    { return bundle.T("already_configured") }
func bootstrapWelcomeMessage() string     { return bundle.T("bootstrap_welcome") }
func bootstrapFailureMessage() string      { return bundle.T("bootstrap_failure") }
func bootstrapAssistantMessage() string   { return bundle.T("bootstrap_assistant") }
func bootstrapProfileMessage() string     { return bundle.T("bootstrap_profile") }
func bootstrapSuccessMessage() string     { return bundle.T("bootstrap_success") }
```

**Note**: All call sites in `internal/telegram/*.go` that reference these constants must be updated to function calls (e.g., `unsupportedDocumentMessage` → `unsupportedDocumentMessage()`).

#### 5. Modify `internal/telegram/*.go` — Update Call Sites

Search for all references to the message constants and add `()`:

Files likely affected:
- `internal/telegram/input.go`
- `internal/telegram/bootstrap.go`
- `internal/telegram/bot.go`

Example change in `input.go`:
```go
// Before:
s.bot.Send(msg.Chat, unsupportedDocumentMessage, ...)

// After:
s.bot.Send(msg.Chat, unsupportedDocumentMessage(), ...)
```

### Validation

1. Run `go build ./...` — should compile
2. Run `go test ./internal/i18n/...` — create basic tests
3. Send `/start` to bot — messages should appear in Portuguese
4. (Future) Set locale to `en` — messages should appear in English

---

## P3: Linux systemd Service

### Goal
Provide first-class Linux deployment support alongside the existing macOS launchd setup.

### Files to Create

#### 1. Create `scripts/aurelia.service.tmpl`

```ini
[Unit]
Description=Aurelia OS Agent Daemon
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
ExecStart=__BINARY__
Restart=always
RestartSec=5
StandardOutput=journal
StandardError=journal
SyslogIdentifier=aurelia
Environment="PATH=__PATH__"
Environment="HOME=__HOME__"
WorkingDirectory=__HOME__

[Install]
WantedBy=default.target
```

#### 2. Create `scripts/install-systemd.sh`

```bash
#!/bin/bash
# install-systemd.sh — install Aurelia as a user systemd service.
#
# Idempotent: safe to re-run. Existing service file is overwritten.
#
# Paths follow the convention used by ~/.aurelia/bin/aurelia:
#   service → ~/.config/systemd/user/aurelia.service
#   binary  → ~/.aurelia/bin/aurelia

set -euo pipefail

SERVICE_NAME="aurelia"
SERVICE_FILE="$HOME/.config/systemd/user/${SERVICE_NAME}.service"
BINARY="$HOME/.aurelia/bin/aurelia"

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
TEMPLATE="${SCRIPT_DIR}/aurelia.service.tmpl"

if [[ ! -f "$TEMPLATE" ]]; then
    echo "error: template not found: $TEMPLATE" >&2
    exit 1
fi

mkdir -p "$HOME/.config/systemd/user" "$(dirname "$BINARY")"

# Render template
sed \
    -e "s|__HOME__|${HOME}|g" \
    -e "s|__BINARY__|${BINARY}|g" \
    -e "s|__PATH__|${PATH}|g" \
    "$TEMPLATE" > "${SERVICE_FILE}.new"
mv "${SERVICE_FILE}.new" "$SERVICE_FILE"

echo "installed: $SERVICE_FILE"

# Reload systemd and enable/start service
systemctl --user daemon-reload
systemctl --user enable "$SERVICE_NAME"

if systemctl --user is-active "$SERVICE_NAME" >/dev/null 2>&1; then
    systemctl --user restart "$SERVICE_NAME"
    echo "service restarted: $SERVICE_NAME"
else
    systemctl --user start "$SERVICE_NAME"
    echo "service started: $SERVICE_NAME"
fi

echo ""
echo "View logs: journalctl --user -u aurelia -f"
echo "Stop:      systemctl --user stop aurelia"
echo "Disable:   systemctl --user disable aurelia"
```

Make executable: `chmod +x scripts/install-systemd.sh`

#### 3. Update `Makefile`

Add Linux targets alongside macOS ones:

```makefile
# Service installation
install-service:
ifeq ($(shell uname -s),Darwin)
	./scripts/install-service.sh
else
	./scripts/install-systemd.sh
endif

install-service-macos:
	./scripts/install-service.sh

install-service-linux:
	./scripts/install-systemd.sh
```

### Validation

1. On Linux machine: `make install-service-linux`
2. Check status: `systemctl --user status aurelia`
3. Check logs: `journalctl --user -u aurelia -f`
4. Stop: `systemctl --user stop aurelia`

---

## P4: README Update

### Goal
Make the README clearly guide new users from clone to running bot, with dependency checks and correct command order.

### Changes to `README.md`

1. **Add "Prerequisites" section right after badges** (before "Why Aurelia OS"):

```markdown
## Prerequisites

Before installing, ensure you have:

- **Go** `1.25+` — [go.dev](https://go.dev/)
- **Node.js** `18+` and **npm** `8+` — [nodejs.org](https://nodejs.org/)
- **git** `2+`
- **gh** (GitHub CLI) — optional but recommended
- A **Telegram bot token** from [@BotFather](https://t.me/botfather)
- An **LLM provider account** (Anthropic, Kimi, OpenRouter, Z.ai, or Alibaba)
```

2. **Update "Quick Start" section** to emphasize onboarding first:

```markdown
### Quick Start

1. **Clone** the repository:
   ```bash
   git clone https://github.com/igormaneschy/aurelia.git
   cd aurelia
   ```

2. **Configure PI auth** (if you haven't already):
   ```bash
   pi /login
   ```

3. **Run the onboarding wizard** (required before first start):
   ```bash
   go run ./cmd/aurelia/ onboard
   ```
   This interactive wizard will guide you through:
   - Dependency verification
   - LLM provider selection
   - API key configuration
   - Telegram bot token setup
   - User access control

4. **Start the daemon**:
   ```bash
   go run ./cmd/aurelia/
   ```

5. **Send `/start`** to your bot on Telegram.

> **Note**: If you skip step 3 and run the daemon directly, it will exit with instructions to complete onboarding first.
```

3. **Update "Running as a service" section** to include Linux:

```markdown
## Running as a Service

### macOS (launchd)

```bash
make install-service  # one-time: install launchd plist (auto-restart, RunAtLoad)
make deploy           # build atomically + kick the daemon
make logs             # tail daemon stderr
make status           # show launchd state
```

### Linux (systemd)

```bash
make install-service-linux  # one-time: install user systemd service
make deploy                 # build atomically + restart service
journalctl --user -u aurelia -f  # tail logs
```

Full guide: [docs/OPERATIONS.md](docs/OPERATIONS.md).
```

4. **Add "Troubleshooting" section** before "Current State":

```markdown
## Troubleshooting

| Problem | Solution |
|---------|----------|
| Daemon exits immediately | Run `go run ./cmd/aurelia/ onboard` first |
| "Token is invalid" during onboard | Verify token with @BotFather, ensure bot is not already running elsewhere |
| Bridge fails to build | Check `node --version` ≥ 18 and `npm --version` ≥ 8 |
| "Dependency missing" error | Install the missing tool and re-run onboarding |
```

### Validation

1. Review README in rendered Markdown
2. Verify all links work
3. Ensure quickstart steps are in correct order

---

## Implementation Order

Recommended sequence to minimize conflicts:

1. **P0** — Add `Onboarded()` to `AppConfig` + guardrail in `main.go`
2. **P1** — Add token validation function + integrate in prompt/TUI modes
3. **P2** — Create `internal/i18n/` package + migrate `messages.go`
4. **P3** — Create systemd files + update `Makefile`
5. **P4** — Update `README.md`
6. **Validate** — `go vet ./...`, `go test ./... -short`, `go build ./...`

---

## Rollback Plan

If any change causes issues:

1. Revert the specific file changes using git
2. For i18n: if message lookup fails, the `T()` function returns the key itself — safe fallback
3. For token validation: if Telegram API is unreachable, the validation will fail — consider adding a `--skip-validation` flag to the onboard command for offline/air-gapped setups

---

## Success Criteria

- [ ] Running daemon without onboarding prints friendly instructions and exits cleanly
- [ ] Onboarding validates Telegram token before saving config
- [ ] All user-facing Telegram messages use i18n bundle (pt-BR default)
- [ ] Linux systemd service installs and runs correctly
- [ ] README quickstart is clear and in correct order
- [ ] `go vet ./...`, `go test ./... -short`, `go build ./...` all pass
