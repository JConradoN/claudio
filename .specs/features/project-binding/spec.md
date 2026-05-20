# Persistent Project Binding — Specification

**Status:** ✅ **Implemented (v0.7.20+)** — 95% complete  
**Depende de:** Nada (implementado independente; pendente apenas integração com User Isolation)  
**Desbloqueia:** Plan Mode, Orchestration Execution, Project Memory, Learning Nudge

## Problem Statement

Hoje `/cwd` é armazenado no `session.Store` em memória:

```go
cwds map[SessionKey]string
cwdSeen map[SessionKey]time.Time
```

Isso torna a configuração do projeto limitada:

- some quando o daemon reinicia;
- pode expirar junto com `session_ttl_hours`;
- é tratada como estado de sessão, apesar de representar uma escolha mais permanente: “esta conversa trabalha neste projeto”.

Conceitualmente, `/cwd` não é sessão LLM. É um **binding persistente entre uma conversa Telegram e um projeto/repositório**.

```text
ConversationKey{chat_id, thread_id} → cwd/project_slug
```

Esse binding deve sobreviver a `/new`, troca de modelo, reset de sessão, restart do daemon e expiração da sessão PI. O usuário só deve perdê-lo ao trocar o `/cwd` ou limpar explicitamente.

## Goals

- [ ] Persistir `/cwd` manual por `ConversationKey{chat_id, thread_id}`
- [ ] Separar lifetime de sessão LLM do lifetime de project binding
- [ ] Manter fallback tópico → grupo (`thread_id` específico → `thread_id=0`)
- [ ] `/cwd` sem argumentos mostra origem, escopo e persistência do projeto ativo
- [ ] `/cwd clear` remove binding do tópico/conversa atual
- [ ] Auto-detect de projeto não persiste sem confirmação explícita
- [ ] Boot/restart restaura bindings persistidos
- [ ] Project binding dispara bootstrap de memória/projeto e invalidação de cache
- [ ] Plan Mode, Orchestration, Nudge e Project Memory usam o binding persistente como fonte de verdade

## Out of Scope

- Múltiplos projetos ativos simultâneos no mesmo tópico
- Histórico/favoritos de projetos
- UI avançada para escolher projeto por botão
- Permissões complexas por usuário para alterar `/cwd` no MVP
- Persistir auto-detect sem confirmação
- Resolver workspaces multi-root dentro do mesmo binding

---

## Core Concept

### Session vs Project Binding

| Conceito | Escopo | Persistência | Exemplo |
|---|---|---|---|
| LLM session | `SessionKey` | temporária/TTL | PI session id, warm/cold |
| Project binding | `ConversationKey` | persistente | `/cwd /repo/aurelia` |
| User memory | `user_id` | persistente | preferências pessoais |
| User project memory | `user_id × project_slug` | persistente | notas privadas no repo |
| Topic memory | `ConversationKey` | persistente | contexto do tópico |

### Resolution order

```text
1. Agent cwd explícito no agent markdown
2. Topic project binding: (chat_id, thread_id)
3. Group project binding: (chat_id, 0)
4. No project / chat mode
```

O cwd técnico do daemon (`os.Getwd()`) não deve ser apresentado como projeto ativo para conversas normais. Ele é apenas fallback operacional interno do processo.

---

## Data Model

```go
type ConversationKey struct {
    ChatID   int64
    ThreadID int
}

type ProjectBinding struct {
    Key         ConversationKey
    CWD         string
    ProjectSlug string
    Source      BindingSource
    CreatedBy   int64
    CreatedAt   time.Time
    UpdatedAt   time.Time
    LastUsedAt  time.Time
}

type BindingSource string

const (
    BindingManual       BindingSource = "manual"
    BindingConfirmedAuto BindingSource = "confirmed_auto"
)
```

SQLite:

```sql
CREATE TABLE IF NOT EXISTS conversation_project_binding (
    chat_id      INTEGER NOT NULL,
    thread_id    INTEGER NOT NULL,
    cwd          TEXT NOT NULL,
    project_slug TEXT NOT NULL,
    source       TEXT NOT NULL,
    created_by   INTEGER NOT NULL DEFAULT 0,
    created_at   INTEGER NOT NULL,
    updated_at   INTEGER NOT NULL,
    last_used_at INTEGER NOT NULL,
    PRIMARY KEY (chat_id, thread_id)
);

CREATE INDEX IF NOT EXISTS idx_project_binding_slug
ON conversation_project_binding(project_slug);
```

---

## User Stories

### P0: `/cwd` manual persiste por conversa ⭐ MVP

**User Story:** Como usuário, quando configuro `/cwd`, quero que Aurelia lembre esse projeto mesmo depois de reiniciar.

**Acceptance Criteria:**

1. WHEN user manda `/cwd <path>` THEN Aurelia SHALL validar o path e persistir binding para `(chat_id, thread_id)`.
2. WHEN daemon reinicia THEN `/cwd` sem argumentos SHALL mostrar o binding salvo.
3. WHEN próxima mensagem chega após restart THEN pipeline SHALL usar o cwd salvo.
4. WHEN `/new` é executado THEN sessão LLM SHALL resetar, mas project binding SHALL permanecer.
5. WHEN `/model` troca modelo THEN sessão atual MAY resetar, mas project binding SHALL permanecer.
6. WHEN session GC roda THEN SHALL NOT remover project bindings.

**Independent Test:** Setar `/cwd /tmp/repo`, recriar `Store/Service` com mesma SQLite, enviar mensagem; bridge request recebe `/tmp/repo`.

---

### P0: Fallback tópico → grupo preservado ⭐ MVP

**User Story:** Como usuário em fórum Telegram, quero configurar um projeto no grupo e deixar tópicos herdarem, podendo sobrescrever por tópico.

**Acceptance Criteria:**

1. WHEN binding existe para `(chat_id, thread_id)` THEN ele vence.
2. WHEN não existe binding do tópico e `thread_id != 0` THEN Aurelia SHALL usar binding `(chat_id, 0)` se existir.
3. WHEN tópico herda do grupo THEN `/cwd` SHALL mostrar origem “herdado do grupo”.
4. WHEN tópico define `/cwd <path>` THEN passa a sobrescrever o grupo.
5. WHEN grupo muda `/cwd` THEN tópicos sem override passam a herdar o novo valor; tópicos com override não mudam.

**Independent Test:** Grupo define `/repo/a`; tópico herda; tópico define `/repo/b`; grupo muda `/repo/c`; tópico continua `/repo/b`.

---

### P0: Clear explícito ⭐ MVP

**User Story:** Como usuário, quero remover o projeto ativo de uma conversa quando não quero mais que Aurelia opere naquele repo.

**Acceptance Criteria:**

1. WHEN user manda `/cwd clear` THEN binding do `(chat_id, thread_id)` atual SHALL ser removido.
2. WHEN tópico remove override e grupo tem binding THEN tópico volta a herdar grupo.
3. WHEN user manda `/cwd clear --group` THEN binding `(chat_id,0)` SHALL ser removido.
4. WHEN binding é removido THEN pipeline volta para chat mode se não houver fallback.
5. Clear SHALL NOT apagar memórias do projeto.

**Independent Test:** Set topic binding, clear topic, verify fallback group; clear group, verify no cwd.

---

### P1: Auto-detect não persiste sem confirmação ⭐ MVP

**User Story:** Como operador, quero evitar que Aurelia fixe um projeto errado só porque detectou algo uma vez.

**Acceptance Criteria:**

1. WHEN pipeline auto-detecta projeto THEN SHALL use it only for current turn or ask confirmation, not persist silently.
2. WHEN user confirma “fixar projeto” THEN source SHALL be `confirmed_auto` and binding persisted.
3. WHEN user ignores confirmation THEN no binding SHALL be written.
4. WHEN auto-detected cwd differs from existing manual binding THEN manual binding SHALL win.

**Independent Test:** Fake project detection returns `/repo/x`; no `/cwd` command; restart does not restore `/repo/x` unless confirmed.

---

### P1: Project bootstrap and memory cache integration ⭐ MVP

**User Story:** Como sistema, quando um projeto é fixado, quero preparar diretórios de memória e invalidar caches relevantes.

**Acceptance Criteria:**

1. WHEN binding is saved THEN project slug SHALL be computed and stored.
2. WHEN binding is saved THEN Aurelia SHALL bootstrap project team memory directories.
3. WHEN a user first uses a bound project THEN user project memory directory MAY be bootstrapped.
4. WHEN binding changes THEN memory cache SHALL be invalidated for old and new project layers.
5. WHEN path no longer exists on use THEN Aurelia SHALL warn and enter chat mode or ask user to update `/cwd`.

---

### P2: Owner/user permissions for shared groups

**User Story:** Como owner, quero controlar quem pode alterar o projeto fixado em grupos compartilhados.

**Acceptance Criteria:**

1. In MVP, any onboarded authorized user MAY change binding unless config says owner-only.
2. Future config MAY require owner-only for group-level binding.
3. Every binding change SHALL audit `created_by`/updated by user id.

---

## Edge Cases

- WHEN cwd is relative THEN resolve to absolute path before persisting.
- WHEN cwd does not exist THEN reject `/cwd <path>` with clear error.
- WHEN cwd exists but is not directory THEN reject.
- WHEN cwd is symlink THEN store cleaned resolved path or preserve original plus resolved path by design decision; MVP SHOULD store resolved absolute path.
- WHEN project is moved/deleted THEN `/cwd` shows stale binding and asks user to update or clear.
- WHEN private chat has `thread_id=0` THEN binding works normally.
- WHEN bot has agent markdown `cwd` THEN agent cwd wins for that turn but does not overwrite conversation binding.
- WHEN multi-user is enabled THEN binding remains conversation-scoped, not user-scoped.

---

## Success Criteria

- [ ] `/cwd <path>` survives daemon restart
- [ ] `/new`, `/model`, session GC do not remove project binding
- [ ] Topic override and group fallback work after restart
- [ ] `/cwd clear` removes binding without deleting memory
- [ ] Auto-detect does not persist silently
- [ ] Plan Mode and Orchestration use persisted binding
- [ ] `go build ./... && go vet ./... && go test ./...` clean when implemented
