# User Isolation Hardening — Análise e Plano de Correção

Data original: 2026-05-21  
Atualização: 2026-05-22  
Escopo: hardening pós-v0.13.0 para garantir isolamento de sessão PI por usuário e corrigir timeouts indevidos em sessões longas gerenciadas pelo PI.

## Status 2026-05-22

O hardening de sessão/runtime descrito neste documento foi aplicado e auditado.

Validação de documentação/código:

```bash
rg "sessions\.(Get|Set|ClearSession|Deactivate|GetWithState)\(" internal --glob '!**/*_test.go'
```

Resultado: nenhuma chamada runtime legacy encontrada em `internal/` fora de testes.

Estado atual:

- `session.Store` expõe e o runtime usa APIs com `userID`: `GetSession`, `GetSessionWithState`, `SetSession`, `ClearSessionForUser`, `DeactivateSession`.
- `pipeline.Service.activeSessions` usa `chatID:threadID:userID`.
- `Cancel`, `WorkStatus` e `CancelAllForUser` propagam `UserID` para o Bridge.
- Bridge `chatKey(chatID, threadID, userID)` é usado por `query`, `steer`, `follow-up`, `abort` e `get-state`.
- Reset, retry pós-bridge-death, timeout, empty-result e continuity session-id patch usam sessão user-scoped.
- Timeouts agora logam origem/duração no pipeline (`max_execution_timeout`, cancelamento etc.); a investigação de sessões longas deve partir desses logs.

Este arquivo fica como histórico de análise e checklist de regressão. O estado canônico de roadmap está em `.specs/project/ROADMAP.md` e `.specs/features/multi-user-profiles/tasks.md`.

## Resumo executivo histórico

A base possuía suporte parcial a `userID` no `session.Store`, mas ainda havia chamadas runtime usando APIs legacy sem `userID`. Isso podia causar divergência entre sessão real do usuário e sessão legacy, especialmente em reset, retry, timeout, empty-result e continuity.

Também havia suspeita operacional de sessões longas caírem por timeout após alguns minutos. A análise local mostrou timeout máximo aparente de 30 minutos no Go e no Bridge TypeScript; quedas por volta de 10 minutos deveriam ser tratadas como bug de origem de timeout, não como simples ausência de constante.

## Riscos principais

Dois usuários no mesmo `chatID` + `threadID` podem não ter a sessão correta usada/limpa/desativada se algum fluxo acessar `UserID=0`.

Exemplo de risco:

```text
chat=123, thread=10, user=100 -> sessão A
chat=123, thread=10, user=200 -> sessão B

reset/timeout/retry usando apenas chat+thread ignora userID
=> pode limpar/desativar a sessão errada ou não operar sobre a sessão real.
```

## Evidência local encontrada — User Isolation

Busca executada:

```bash
grep -R "sessions\.\(Get\|Set\|ClearSession\|Deactivate\)(" internal
```

Ocorrências runtime relevantes:

| Arquivo | Linha aproximada | Chamada | Problema |
|---|---:|---|---|
| `internal/telegram/commands.go` | 278 | `bc.sessions.ClearSession(chatID, threadID)` | `/session reset` ignora `userID` |
| `internal/telegram/commands.go` | 809 | `bc.sessions.ClearSession(chatID, threadID)` | reset após troca de modelo ignora `userID` |
| `internal/pipeline/pipeline.go` | 663 | `s.sessions.Get(chatID, threadID)` | retry após bridge death resume usando sessão legacy |
| `internal/pipeline/pipeline.go` | 725 | `s.sessions.Deactivate(chatID, threadID)` | timeout desativa sessão legacy |
| `internal/pipeline/pipeline.go` | 934 | `s.sessions.Deactivate(chatID, threadID)` | empty-result com trabalho desativa sessão legacy |
| `internal/pipeline/pipeline.go` | 1219 | `s.sessions.Get(chatID, threadID)` | continuity success patch grava sessão legacy |
| `internal/pipeline/pipeline.go` | 1276 | `s.sessions.Get(chatID, threadID)` | continuity failure patch grava sessão legacy |

Ocorrências em testes também usam APIs legacy; devem ser migradas ou duplicadas com cenários multi-user.

## Evidência local encontrada — timeouts de sessões longas

Buscas executadas:

```bash
grep -R "bridgeExecutionTimeout\|idleBridgeTimeout\|WithTimeout\|timeoutMs" internal bridge
grep -R "10 \* time.Minute\|600000\|10 \* 60" internal bridge
```

Pontos relevantes:

| Arquivo | Linha aproximada | Valor | Observação |
|---|---:|---:|---|
| `internal/pipeline/pipeline.go` | 48 | `bridgeExecutionTimeout = 30 * time.Minute` | timeout máximo Go do request principal |
| `internal/pipeline/pipeline.go` | 49 | `idleBridgeTimeout = 2 * time.Minute` | cancela se o bridge ficar sem emitir eventos; pode derrubar execução PI longa/silenciosa |
| `bridge/index.ts` | 122 | `30 * 60 * 1000` | cleanup idle de sessão armazenada após 30 min |
| `bridge/index.ts` | 711 | `timeoutMs = 30 * 60 * 1000` | timeout máximo TypeScript do query |
| `internal/telegram/orchestration.go` | 15 | `orchestrationTimeout = 15 * time.Minute` | limite separado para execução de plano aprovado/TLC |
| `internal/cron/scheduler.go` | 92 | `30 * time.Minute` | limite de job cron |

Não foi encontrado um hard limit runtime explícito de 10 minutos na pipeline principal. Portanto, a correção deve tratar este item como bug de timeout efetivo, não apenas aumentar um constante já definido como 30 minutos.

Hipóteses mais prováveis:

1. `idleBridgeTimeout = 2min` cancela sessões longas quando o PI/SDK fica sem eventos durante pensamento, tool call, compaction ou provider wait.
2. Alguma camada do PI SDK/provider impõe timeout interno próximo de 10 minutos e o erro só é propagado pela Aurelia.
3. Execuções via orquestração/TLC estão batendo no `orchestrationTimeout = 15min`, confundido com timeout de sessão PI.
4. O cancelamento por contexto em `cancelBridgeOnContextDone` pode transformar timeout local em abort no bridge, encerrando a sessão PI antes dela terminar.

## Objetivo da correção

Garantir que todos os fluxos de sessão PI usem a chave completa:

```go
(chatID, threadID, userID)
```

CWD pode continuar conversation-scoped por enquanto se essa for a decisão arquitetural atual; esta análise foca em sessão PI.

## Plano histórico de correção aplicado

### 1. Migrar reset de sessão

Arquivo: `internal/telegram/commands.go`

Trocar:

```go
bc.sessions.ClearSession(chatID, threadID)
```

Por:

```go
bc.sessions.ClearSessionForUser(chatID, threadID, userID)
```

Aplicar em:

- `resetCurrentSession(...)`
- `resetCurrentModelSession(...)`

Observação: `resetCurrentModelSession` já recebe `userID ...int64`, mas não usa. Tornar explícito ou normalizar:

```go
func (bc *BotController) resetCurrentModelSession(chatID int64, threadID int, userID int64) string
```

Se manter varargs, resolver com fallback seguro:

```go
uid := int64(0)
if len(userID) > 0 {
    uid = userID[0]
}
bc.sessions.ClearSessionForUser(chatID, threadID, uid)
```

Preferível: remover varargs em runtime e ajustar testes.

### 2. Migrar retry pós bridge death

Arquivo: `internal/pipeline/pipeline.go`

Trocar:

```go
if sid := s.sessions.Get(chatID, threadID); sid != "" {
    retryReq.Options.Resume = sid
}
```

Por:

```go
if sid := s.sessions.GetSession(chatID, threadID, userID); sid != "" {
    retryReq.Options.Resume = sid
}
```

Esse bloco já está dentro de fluxo que possui `userID`; basta propagar corretamente.

### 3. Migrar timeout/context outcome

Arquivo: `internal/pipeline/pipeline.go`

Hoje:

```go
func (s *Service) handleContextOutcome(parentCtx context.Context, ctx context.Context, chatID int64, threadID int) bool
```

Recomendado:

```go
func (s *Service) handleContextOutcome(parentCtx context.Context, ctx context.Context, chatID int64, threadID int, userID int64) bool
```

E trocar:

```go
s.sessions.Deactivate(chatID, threadID)
```

Por:

```go
s.sessions.DeactivateSession(chatID, threadID, userID)
```

Atualizar as duas chamadas de `handleContextOutcome(...)` no pipeline.

### 4. Migrar empty-result com trabalho

Arquivo: `internal/pipeline/pipeline.go`

Função já recebe `userID`:

```go
func (s *Service) handleEmptyResult(..., userID int64) Outcome
```

Trocar:

```go
s.sessions.Deactivate(chatID, threadID)
```

Por:

```go
s.sessions.DeactivateSession(chatID, threadID, userID)
```

### 5. Migrar continuity patch para sessão user-scoped

Arquivo: `internal/pipeline/pipeline.go`

Funções hoje não recebem `userID`:

```go
patchContinuityAfterSuccess(chatID, threadID, userText, assistantText, runID)
patchContinuityFailure(chatID, threadID, status, errMsg)
```

Recomendado:

```go
patchContinuityAfterSuccess(chatID, threadID, userID, userText, assistantText, runID)
patchContinuityFailure(chatID, threadID, userID, status, errMsg)
```

E trocar internamente:

```go
sessionID = s.sessions.Get(chatID, threadID)
sid = s.sessions.Get(chatID, threadID)
```

Por:

```go
sessionID = s.sessions.GetSession(chatID, threadID, userID)
sid = s.sessions.GetSession(chatID, threadID, userID)
```

Atenção: `continuity.ConversationKey` ainda é `chatID + threadID`. Se continuity deve ser user-scoped, isso é outra mudança maior. Para este hardening mínimo, apenas evitar gravar `SessionID` errado.

### 6. Revisar prompt builder / last run state

Arquivo: `internal/pipeline/prompt_builder.go`

Ocorrências:

```go
bc.sessions.GetWithState(chatID, threadID)
```

Essas funções montam estado de continuidade/sessão ativa. Se forem usadas em runtime com usuário real, precisam receber `userID` e usar:

```go
bc.sessions.GetSessionWithState(chatID, threadID, userID)
```

Ação histórica recomendada:

- identificar call chain de `buildLastRunStateSection(...)`;
- adicionar `userID` na assinatura se fizer parte do prompt runtime;
- manter wrappers legacy apenas para testes, se necessário.

### 7. Manter APIs legacy apenas para compatibilidade/testes

As APIs legacy podem permanecer temporariamente, mas o runtime não deve depender delas para sessão PI:

- permitido em testes antigos ou migração;
- evitar em `internal/pipeline/*.go` e `internal/telegram/*.go`, exceto CWD (`GetCwd`, `SetCwd`) que é outro escopo.

### 8. Corrigir timeouts indevidos em sessões longas PI

Arquivos principais:

- `internal/pipeline/pipeline.go`
- `internal/pipeline/idle_timeout.go`
- `bridge/index.ts`
- `internal/telegram/orchestration.go`, se o problema ocorrer em execução de plano aprovado

Ação histórica recomendada:

1. **Instrumentar origem do timeout** antes de só aumentar constantes:
   - logar duração real do run no timeout;
   - distinguir `max_execution_timeout`, `idle_bridge_timeout`, `bridge_query_timeout`, `orchestration_timeout` e erro vindo do PI/provider;
   - persistir essa origem no runlog/continuity.

2. **Revisar `idleBridgeTimeout`**:
   - o valor atual de 2 minutos é agressivo para sessões PI longas;
   - para execução gerenciada pelo PI, ausência de eventos não deve significar falha se a sessão ainda está viva;
   - recomendação mínima: aumentar para 10–15 minutos ou desabilitar idle timeout durante runs ativos com PI SDK;
   - recomendação mais robusta: transformar em watchdog com heartbeat/status, sem abortar automaticamente enquanto o bridge/processo estiver vivo.

3. **Alinhar timeouts Go e TypeScript**:
   - manter `bridgeExecutionTimeout` e `bridge/index.ts timeoutMs` com o mesmo valor;
   - default sugerido: pelo menos 30 minutos, já presente hoje;
   - se o caso real exige tarefas maiores, elevar ambos para 60 minutos ou tornar configurável em `app.json`.

4. **Separar timeout de orquestração do timeout PI**:
   - `orchestrationTimeout = 15 * time.Minute` pode derrubar plano aprovado antes do limite do bridge;
   - se execução TLC precisa suportar tarefas longas, alinhar para o mesmo limite da pipeline ou criar limite próprio documentado.

5. **Não matar sessão PI antes de confirmar timeout local real**:
   - revisar `cancelBridgeOnContextDone` para garantir que cancelamento por watchdog indevido não chame `CancelRequest` e aborte a sessão PI sem necessidade;
   - manter abort explícito do usuário funcionando.

Resultado esperado:

```text
Sessões PI longas não devem cair aos ~10 minutos.
Se houver timeout, a mensagem/runlog deve indicar exatamente a origem: idle, max execution, orchestration, bridge query ou provider/PI.
```

## Testes de regressão necessários

### Store

Adicionar/garantir:

```text
TestStore_UserSessionIsolation
TestStore_ClearSessionForUser_PreservesOtherUsers
TestStore_DeactivateSession_PreservesOtherUsers
```

### Telegram commands

Testes esperados:

```text
/session reset limpa apenas a sessão do sender
/model reset limpa apenas a sessão do sender
reset de user 100 preserva sessão de user 200 no mesmo chat/thread
cancelActiveRun recebe userID correto
```

### Pipeline

Testes esperados:

```text
retry pós bridge death usa GetSession(chat, thread, user)
timeout desativa apenas sessão do user correto
empty-result com work desativa apenas sessão do user correto
continuity patch usa session ID do user correto
idle timeout não cancela sessão PI ativa antes do limite esperado
max execution timeout registra origem e duração corretas
cancelBridgeOnContextDone não aborta sessão por watchdog indevido
```

### Cenário mínimo obrigatório

```go
chatID := int64(42)
threadID := 99
userA := int64(100)
userB := int64(200)

sessions.SetSession(chatID, threadID, userA, "sess-A")
sessions.SetSession(chatID, threadID, userB, "sess-B")

// ação de userA

// assert:
// userA foi afetado conforme esperado
// userB permanece intacto
```

## Validação recomendada para regressões futuras

Quando alterar código de sessão/Bridge/pipeline/Telegram commands:

```bash
rg "sessions\.(Get|Set|ClearSession|Deactivate|GetWithState)\(" internal --glob '!**/*_test.go'
go test ./internal/session/... ./internal/pipeline/... ./internal/telegram/... -short
go build ./...
```

Se houver alteração em `bridge/index.ts`:

```bash
cd bridge && npm run build
cp bundle.js ../internal/bridge/bundle.js
```

## Critério de aceite atual

- ✅ Nenhuma chamada runtime de sessão PI usa `sessions.Get`, `sessions.Set`, `sessions.ClearSession`, `sessions.Deactivate` ou `sessions.GetWithState` fora de compat/testes.
- ✅ Reset, retry, timeout, empty-result e continuity session-id patch usam `userID`.
- ✅ Testes multi-user cobrem mesmo `chatID` + mesmo `threadID` + usuários diferentes para store, active sessions e comandos críticos.
- ✅ Timeouts de pipeline logam origem/duração; novas investigações devem usar esses logs.
- ➡️ Sessões longas E2E continuam sendo validação operacional/live, não documentação.

## Próxima implementação relacionada

Não reabrir este sprint para memória privada de projeto. O próximo escopo relacionado é `Sprint D — User-Scoped Project Memory`, onde paths de projeto devem virar `(user_id, project_slug)` antes de Wiki/Nudge profundo.
