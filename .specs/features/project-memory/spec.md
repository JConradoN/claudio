# Project-Scoped Memory — Specification

## Problem Statement

A Aurelia atualmente tem apenas memória global (`~/.aurelia/memory/`). Todos os fatos — pessoais, de projeto, preferências — ficam num único diretório. Isso causa poluição quando o usuário trabalha em múltiplos projetos: fatos do projeto A aparecem no contexto do projeto B, consumindo tokens e confundindo o modelo.

O Claude Code resolve isso com memória em 3 camadas por projeto:
1. **Auto Memory (privada)** — fatos pessoais do dev naquele projeto
2. **Team Memory** — convenções e decisões compartilhadas com o time
3. **Managed/Policy** — configurações corporativas

O auto-memory do SDK está desabilitado via feature flag server-side (`tengu_auto_mode_config`), então não há conflito — toda a gestão de memória é da Aurelia.

## Goals

- [ ] Memória em 3 camadas: global + projeto (privada) + projeto (team)
- [ ] Global: fatos pessoais, preferências, estilo de comunicação — cross-projeto
- [ ] Projeto privada: contexto pessoal do usuário naquele projeto (anotações, decisões individuais)
- [ ] Projeto team: stack, convenções, padrões, decisões arquiteturais — compartilhável com o time
- [ ] System prompt injeta as 3 camadas (global + projeto privada + projeto team)
- [ ] Extrator classifica automaticamente cada fato na camada correta
- [ ] Dream consolida cada camada independentemente
- [ ] Troca de projeto carrega memórias diferentes sem perder as globais

## Out of Scope

- Busca full-text em sessões anteriores (FTS5 como Hermes)
- Providers externos de memória (Honcho, Mem0)
- Memória por usuário em cenários multi-tenant
- Sincronização de team memory via git (v2)

## Architecture

### Diretórios

```
~/.aurelia/
├── memory/                                    # GLOBAL — cross-projeto
│   ├── MEMORY.md                              # Índice global
│   ├── user_preferences.md                    # Preferências, idioma, estilo
│   ├── user_facts.md                          # Fatos pessoais
│   └── ...
│
├── projects/
│   ├── <sanitized-cwd>/
│   │   └── memory/                            # PROJETO — privada
│   │       ├── MEMORY.md                      # Índice privado
│   │       ├── my_notes.md                    # Anotações pessoais
│   │       ├── work_log.md                    # O que EU fiz
│   │       └── team/                          # PROJETO — team (compartilhável)
│   │           ├── MEMORY.md                  # Índice team
│   │           ├── architecture.md            # Decisões de arquitetura
│   │           ├── conventions.md             # Padrões do time
│   │           ├── stack.md                   # Stack e dependências
│   │           └── ...
│   └── <outro-cwd>/
│       └── memory/
│           ├── ...
│           └── team/
│               └── ...
```

### Camadas e escopo

| Camada | Diretório | Escopo | Exemplos |
|---|---|---|---|
| Global | `~/.aurelia/memory/` | Cross-projeto, pessoal | Idioma, pets, preferências de comunicação |
| Projeto (privada) | `~/.aurelia/projects/<cwd>/memory/` | Por projeto, pessoal | Anotações, work log, decisões individuais |
| Projeto (team) | `~/.aurelia/projects/<cwd>/memory/team/` | Por projeto, compartilhável | Stack, convenções, arquitetura, padrões |

### Sanitização do path

Mesmo padrão do Claude Code: substitui `/` por `-`, remove prefixo de drive.

Exemplo: `/media/rafael/projetos/app-test-cep` → `-media-rafael-projetos-app-test-cep`

### Injeção no System Prompt

```
## Your Memory

### Global (cross-project)
<conteúdo de ~/.aurelia/memory/>

### Project: app-test-cep (private)
<conteúdo de ~/.aurelia/projects/<cwd>/memory/>

### Project: app-test-cep (team)
<conteúdo de ~/.aurelia/projects/<cwd>/memory/team/>
```

Quando não há cwd definido, apenas a memória global é injetada.

### Extração (background)

O extrator recebe o cwd ativo e classifica cada fato:

| Tipo de informação | Camada |
|---|---|
| Fatos pessoais (nome, pets, hobbies) | Global |
| Preferências de comunicação, idioma | Global |
| Estilo de trabalho pessoal | Global |
| Stack, dependências, versões | Projeto → team |
| Convenções de código, padrões | Projeto → team |
| Decisões de arquitetura | Projeto → team |
| Bugs conhecidos e workarounds | Projeto → team |
| Anotações pessoais ("preciso revisar X") | Projeto → privada |
| Work log ("hoje implementei Y") | Projeto → privada |
| Estado de tarefas individuais | Projeto → privada |

O prompt do extrator recebe 3 diretórios (global, projeto-privada, projeto-team) e decide onde cada fato vai.

### Dream (consolidação)

- Dream global: consolida `~/.aurelia/memory/` (a cada 24h/5 turns)
- Dream projeto: consolida `~/.aurelia/projects/<cwd>/memory/` + `team/` (mesma cadência, só quando projeto ativo)

### Criação automática de diretórios

Quando um cwd é setado pela primeira vez, o sistema cria automaticamente:
- `~/.aurelia/projects/<sanitized-cwd>/memory/`
- `~/.aurelia/projects/<sanitized-cwd>/memory/team/`
- `MEMORY.md` vazio em cada um

### Migração

A memória global atual (`~/.aurelia/memory/`) é preservada. Arquivos de projeto existentes (como `project_app_test_cep.md`) podem ser migrados para o diretório do projeto correspondente na primeira vez que o cwd é setado.

## Pacotes afetados

| Pacote | Mudança |
|---|---|
| `internal/runtime/` | `ProjectMemoryDir(cwd)` e `ProjectTeamMemoryDir(cwd)` no PathResolver |
| `internal/dream/` | Extrator com 3 targets, dream per-project |
| `internal/dream/` | Prompt de extração com classificação por camada |
| `internal/telegram/` | `buildMemoryInstructions` injeta 3 camadas |
| `internal/telegram/` | `loadMemoryContents` lê global + projeto + team |
| `internal/session/` | `SetCwd` cria diretórios de memória do projeto |

## Acceptance Criteria

1. Com cwd definido, model recebe memória global + projeto privada + projeto team
2. Sem cwd, model recebe apenas memória global
3. Extrator classifica fatos nas 3 camadas corretamente
4. `/new` reseta sessão mas mantém todas as memórias
5. Trocar de cwd carrega memórias do novo projeto, mantém globais
6. Dream consolida cada camada independentemente
7. Diretórios de projeto criados automaticamente no primeiro uso
8. Team memory fica em subpasta separada (futuramente compartilhável via git)
9. Memória global existente preservada sem migração forçada
