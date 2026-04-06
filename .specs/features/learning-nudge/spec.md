# Learning Nudge System — Specification

## Problem Statement

A Aurelia extrai memórias após cada turn usando um agente Haiku leve. Porém essa extração é superficial — recebe apenas um snippet truncado (user message + assistant response) sem acesso ao contexto completo da conversa. Isso causa:

1. **Perda de informação**: detalhes de tool calls, decisões intermediárias e raciocínio são perdidos
2. **Falta de profundidade**: o extrator não tem contexto suficiente pra entender o que realmente importa
3. **Sem aprendizado de padrões**: não identifica abordagens reutilizáveis ou erros repetidos

O Hermes Agent resolve isso com um sistema de "nudge" — a cada N turns, spawna um agente em background com **acesso ao histórico completo da conversa** que faz uma revisão profunda e salva memórias/skills de forma autônoma.

## Goals

- [ ] Background review agent que roda a cada N turns (configurável, default 10)
- [ ] Acesso ao contexto completo da sessão (não apenas snippets)
- [ ] Duas revisões: memória (fatos do user + projeto) e skills (padrões reutilizáveis)
- [ ] Nunca interrompe o fluxo principal — roda em background
- [ ] Resultado da revisão atualiza memórias e opcionalmente cria/atualiza skills
- [ ] Substituir o extrator por-turn pelo nudge periódico (menos chamadas, mais qualidade)

## Out of Scope

- Sistema completo de skills com diretórios, templates e scripts (v2)
- Hub de skills compartilhados entre usuários
- Auto-tuning do intervalo de nudge baseado em atividade
- Múltiplos modelos de review (usar o mesmo modelo configurado)

## Architecture

### Nudge Trigger

```
User turn #1  → normal
User turn #2  → normal
...
User turn #N  → nudge triggered (background)
User turn #N+1 → normal (nudge running in parallel)
...
User turn #2N → nudge triggered again
```

**Gates (checadas a cada turn):**
1. `turns_since_nudge >= config.NudgeTurns` (default 10)
2. `nudge_running == false` (prevent concurrent)
3. `config.NudgeEnabled == true`

### Review Agent

O nudge spawna um agente via bridge com:
- **Prompt**: resumo do que revisar (memory + optional skill review)
- **Contexto**: histórico da conversa (obtido via session transcript do SDK)
- **Tools**: Read, Write, Edit, Glob, Grep (para memória), Bash (para deletion)
- **persistSession**: false (efêmero)
- **Modelo**: configurável (default: mesmo modelo do dream)

### Prompts de Review

**Memory Review:**
```
Review the conversation above and save things worth remembering:
- Facts the user revealed about themselves (persona, preferences, workflow habits)
- Decisions made about the project or approach
- Work completed and current state
- Problems encountered and how they were resolved

Only save what will help in future conversations. Update existing memories
rather than creating duplicates.
```

**Skill Review (v2):**
```
Review the conversation above and consider if a reusable approach emerged:
- Was a non-trivial technique used that required trial and error?
- Did the approach change course due to experiential findings?
- Would this be useful in future similar tasks?

If yes, create or update a skill file describing the approach.
```

### Session Transcript Access

O nudge precisa do histórico completo. Duas opções:

**Opção A — Acumular no Go**: o `processBridgeEventsAsync` já recebe todos os events. Acumular user/assistant messages num buffer e passar ao nudge.

**Opção B — Ler session file do SDK**: as sessões são JSONL em `~/.claude/projects/<cwd>/<session-id>.jsonl`. O nudge pode ler direto.

**Recomendação**: Opção A — mais simples, não depende de paths internos do SDK.

### Substituição do Extrator

O extrator por-turn atual (`ExtractMemories`) seria substituído pelo nudge:

| | Extrator atual | Nudge |
|---|---|---|
| Frequência | Cada turn | Cada N turns |
| Contexto | Snippet truncado | Histórico completo |
| Modelo | Haiku (barato) | Sonnet/configurável |
| Custo por chamada | ~$0.02 | ~$0.05-0.10 |
| Custo total (10 turns) | ~$0.20 | ~$0.05-0.10 |
| Qualidade | Superficial | Profunda |

O nudge é **mais barato** no total (1 chamada profunda vs 10 chamadas superficiais) e **mais eficaz** (contexto completo).

### Configuração

```go
type DreamConfig struct {
    // ... campos existentes ...
    NudgeEnabled  bool   // habilitar nudge (default true)
    NudgeTurns    int    // intervalo em turns (default 10)
    NudgeModel    string // modelo pro nudge (default: DreamModel)
}
```

No `app.json`:
```json
{
    "nudge_enabled": true,
    "nudge_turns": 10,
    "nudge_model": "claude-sonnet-4-6"
}
```

## Pacotes afetados

| Pacote | Mudança |
|---|---|
| `internal/dream/` | Novo `nudge.go` com lógica de nudge, substituir `extract.go` |
| `internal/dream/` | `DreamConfig` com campos de nudge |
| `internal/telegram/` | Acumular histórico de mensagens, passar ao nudge |
| `internal/telegram/bot.go` | Interface do dreamer atualizada |
| `internal/config/` | Novos campos no AppConfig |
| `cmd/aurelia/app.go` | Wiring dos novos campos de config |

## Acceptance Criteria

1. Nudge roda em background a cada N turns sem interromper o chat
2. Review agent tem acesso ao histórico completo da conversa (não snippet)
3. Memórias salvas pelo nudge são mais completas que as do extrator atual
4. Custo total é menor ou igual ao extrator por-turn
5. Configurável via app.json (enabled, interval, model)
6. persistSession: false — não polui sessões do SDK
7. Gate system previne nudges concorrentes
