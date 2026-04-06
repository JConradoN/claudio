# Agent Orchestration — Tasks

**Design**: `.specs/features/agent-orchestration/design.md`
**Status**: Draft

---

## Execution Plan

### Phase 1: Foundation (Sequential)

Structs, data models, e componentes base sem dependências externas.

```
T1 → T2 → T3 → T4 → T5
```

### Phase 2: Orchestrator Core (Sequential, depende de Phase 1)

Lógica central: parsear plano, executar workers, validar resultados.

```
T5 → T6 → T7 → T8 → T9 → T10
```

### Phase 3: Prompt Builders (Parallel, depende de T6)

Todos os prompts podem ser construídos em paralelo.

```
      ┌→ T11 ─┐
      ├→ T12 ─┤
T6 ──→├→ T13 ─┼──→ T16
      ├→ T14 ─┤
      └→ T15 ─┘
```

### Phase 4: Telegram Integration (Sequential, depende de Phase 3)

Conecta o orchestrator ao fluxo existente do Telegram.

```
T16 → T17 → T18 → T19
```

### Phase 5: Documentation & Git (Parallel, depende de T6)

Geração de CLAUDE.md, AGENTS.md, tasks status update, git operations.

```
      ┌→ T20 ─┐
T6 ──→├→ T21 ─┼──→ T23
      └→ T22 ─┘
```

### Phase 6: Integration & Validation (Sequential, depende de tudo)

Wiring final, testes de integração, E2E.

```
T19 + T23 → T24 → T25 → T26
```

---

## Task Breakdown

### T1: Adicionar campos ao Agent struct

**What**: Adicionar `DisallowedTools` e `MaxTurns` ao struct Agent em `types.go`
**Where**: `internal/agents/types.go`
**Depends on**: None
**Reuses**: Struct existente, parsing YAML já funciona

**Done when**:
- [ ] `DisallowedTools []string` com tag `yaml:"disallowed_tools,omitempty"` adicionado
- [ ] `MaxTurns int` com tag `yaml:"max_turns,omitempty"` adicionado
- [ ] `go build ./...` compila sem erro

**Verify**:
```bash
go build ./internal/agents/...
```

---

### T2: Atualizar BuildSDKAgents com novos campos

**What**: Mapear `DisallowedTools` → `disallowedTools` e `MaxTurns` → `maxTurns` no output do SDK
**Where**: `internal/agents/sdk.go`
**Depends on**: T1
**Reuses**: Lógica existente de `BuildSDKAgents()`

**Done when**:
- [ ] `disallowedTools` incluído no mapa quando `len(a.DisallowedTools) > 0`
- [ ] `maxTurns` incluído no mapa quando `a.MaxTurns > 0`
- [ ] Testes existentes continuam passando
- [ ] Novo teste: agent com `disallowed_tools` e `max_turns` gera mapa correto

**Verify**:
```bash
go test ./internal/agents/... -v
```

---

### T3: Criar pacote orchestrator com Plan data model

**What**: Criar `internal/orchestrator/` com structs `Plan`, `Task`, `TaskResult`, `WorkerEvent`, `WorkerConfig` e método `ExecutionOrder()`
**Where**: `internal/orchestrator/plan.go`
**Depends on**: None
**Reuses**: Nenhum — tipos novos

**Done when**:
- [ ] Structs `Plan`, `Task`, `TaskResult`, `WorkerEvent`, `WorkerConfig` definidos
- [ ] `Plan.ExecutionOrder()` retorna tasks agrupadas por wave (topological sort)
- [ ] Teste: plano com dependências gera waves corretas
- [ ] Teste: plano sem dependências → todas as tasks na wave 1
- [ ] Teste: dependência circular → retorna erro

**Verify**:
```bash
go test ./internal/orchestrator/... -v -run TestExecutionOrder
```

---

### T4: Criar WorktreeManager

**What**: Gerenciar git worktrees — Create, Merge, Cleanup, CleanupAll
**Where**: `internal/orchestrator/worktree.go`
**Depends on**: None
**Reuses**: Nenhum — git CLI via `os/exec`

**Done when**:
- [ ] `Create(taskID, baseBranch)` cria worktree em `.worktrees/worker-<taskID>/` com branch `worker/<taskID>-<slug>`
- [ ] `Merge(wt, baseBranch)` faz `git merge --no-ff` do branch do worktree
- [ ] `Cleanup(wt)` remove worktree e branch
- [ ] `CleanupAll()` remove todos os worktrees no padrão `.worktrees/worker-*`
- [ ] Teste: create → verificar diretório e branch existem
- [ ] Teste: cleanup → verificar diretório e branch removidos
- [ ] Teste: merge → verificar alterações no branch base

**Verify**:
```bash
go test ./internal/orchestrator/... -v -run TestWorktree
```

---

### T5: Criar DefaultWorkerConfig e ResolveAgentConfig

**What**: Worker default hardcoded + resolução cascata (agent → worker.md → default)
**Where**: `internal/orchestrator/defaults.go`
**Depends on**: T1, T3
**Reuses**: `agents.Registry.Get()`

**Done when**:
- [ ] `DefaultWorkerConfig` com model=sonnet, maxTurns=25, tools completas, prompt genérico
- [ ] `ResolveAgentConfig(registry, "qa")` retorna config do `qa.md` se existir
- [ ] `ResolveAgentConfig(registry, "worker")` retorna worker.md override ou default
- [ ] `ResolveAgentConfig(registry, "inexistente")` retorna default
- [ ] Teste: sem registry → retorna default
- [ ] Teste: com qa.md → retorna config do qa
- [ ] Teste: com worker.md → retorna default merged com override

**Verify**:
```bash
go test ./internal/orchestrator/... -v -run TestResolveAgentConfig
```

---

### T6: Criar Orchestrator struct e interfaces

**What**: Struct principal do Orchestrator com `BridgeExecutor` interface e `OrchestratorConfig`
**Where**: `internal/orchestrator/orchestrator.go`
**Depends on**: T3, T4, T5
**Reuses**: `bridge.Bridge` implementa `BridgeExecutor`

**Done when**:
- [ ] `BridgeExecutor` interface com `Execute()` e `ExecuteSync()`
- [ ] `Orchestrator` struct com bridge, worktree, config
- [ ] `NewOrchestrator()` constructor
- [ ] Compila sem erro

**Verify**:
```bash
go build ./internal/orchestrator/...
```

---

### T7: Implementar ExtractPlan

**What**: Detectar e parsear bloco ` ```aurelia-plan ` da resposta da Aurelia
**Where**: `internal/orchestrator/extract.go`
**Depends on**: T3, T6
**Reuses**: Nenhum — parsing de string

**Done when**:
- [ ] Extrai JSON entre ` ```aurelia-plan\n ` e ` ``` `
- [ ] Retorna `*Plan` se válido, `nil` se não encontrado
- [ ] Retorna erro se JSON malformado
- [ ] Teste: resposta com plano válido → retorna Plan
- [ ] Teste: resposta sem marcador → retorna nil
- [ ] Teste: resposta com JSON inválido → retorna erro
- [ ] Teste: resposta com texto antes/depois do marcador → extrai corretamente

**Verify**:
```bash
go test ./internal/orchestrator/... -v -run TestExtractPlan
```

---

### T8: Implementar ExecutePlan

**What**: Spawnar workers por wave, coletar resultados, gerenciar worktrees
**Where**: `internal/orchestrator/execute.go`
**Depends on**: T4, T5, T6, T7
**Reuses**: `bridge.Execute()`, `WorktreeManager`

**Done when**:
- [ ] Executa tasks por wave (paralelo dentro da wave, sequencial entre waves)
- [ ] Cria worktree pra tasks com `NeedsWorktree=true`
- [ ] Resolve agent config via `ResolveAgentConfig()` por task
- [ ] Chama `onEvent` callback pra cada `WorkerEvent`
- [ ] Coleta `TaskResult` de cada worker
- [ ] Respeita `MaxConcurrentWorkers` (semáforo)
- [ ] Teste com fake bridge: 3 tasks em 2 waves → executa corretamente

**Verify**:
```bash
go test ./internal/orchestrator/... -v -run TestExecutePlan
```

---

### T9: Implementar ExecuteTask

**What**: Executar uma task individual — montar request, chamar bridge, processar events
**Where**: `internal/orchestrator/execute.go`
**Depends on**: T6, T8
**Reuses**: `bridge.Execute()`, event processing de `processBridgeEventsAsync`

**Done when**:
- [ ] Monta `bridge.Request` com worker prompt, cwd, model, tools, maxTurns
- [ ] Processa event channel: emite `WorkerEvent` pra cada `tool_use`
- [ ] Retorna `TaskResult` com content, success, duration, cost
- [ ] Timeout via context
- [ ] Teste: fake bridge retorna result → TaskResult correto
- [ ] Teste: fake bridge retorna error → TaskResult com erro

**Verify**:
```bash
go test ./internal/orchestrator/... -v -run TestExecuteTask
```

---

### T10: Implementar Quality Gate (Validate)

**What**: Aurelia valida resultado de worker contra critérios da task
**Where**: `internal/orchestrator/validate.go`
**Depends on**: T6, T9
**Reuses**: `bridge.ExecuteSync()`

**Done when**:
- [ ] Monta prompt de validação com task, resultado, spec, design
- [ ] Chama Aurelia via `ExecuteSync()`
- [ ] Parseia resposta como `ValidationResult` (approved/issues/shouldRetry)
- [ ] Retry automático até 3x, depois escala (retorna shouldRetry=false)
- [ ] Teste: resultado bom → approved=true
- [ ] Teste: resultado com problemas → approved=false, issues preenchidas
- [ ] Teste: 3 retries falhando → approved=false, shouldRetry=false

**Verify**:
```bash
go test ./internal/orchestrator/... -v -run TestValidate
```

---

### T11: BuildOrchestratorPrompt [P]

**What**: System prompt da Aurelia com metodologia TLC, anti-overengineering, lista de agents
**Where**: `internal/orchestrator/prompt.go`
**Depends on**: T6
**Reuses**: `persona.BuildPrompt()` padrão de camadas

**Done when**:
- [ ] Inclui instruções TLC (Specify → Design → Tasks → Implement → Validate)
- [ ] Inclui regras anti-overengineering
- [ ] Inclui lista de agents disponíveis com descrições
- [ ] Inclui instruções de quando emitir bloco `aurelia-plan`
- [ ] Inclui JSON schema do Plan pra structured output
- [ ] Teste: gera prompt com persona + TLC + agents

**Verify**:
```bash
go test ./internal/orchestrator/... -v -run TestBuildOrchestratorPrompt
```

---

### T12: BuildExecutionPrompt [P]

**What**: Prompt pra Aurelia gerar plano de execução JSON baseado no tasks.md
**Where**: `internal/orchestrator/prompt.go`
**Depends on**: T6
**Reuses**: Nenhum

**Done when**:
- [ ] Recebe conteúdo do tasks.md + agents disponíveis
- [ ] Inclui JSON schema do Plan
- [ ] Instruções claras de como mapear tasks.md → Plan JSON
- [ ] Teste: gera prompt com tasks.md content e agents

**Verify**:
```bash
go test ./internal/orchestrator/... -v -run TestBuildExecutionPrompt
```

---

### T13: BuildWorkerPrompt [P]

**What**: System prompt de worker com contexto completo (CLAUDE.md + AGENTS.md + spec + design + task + siblings)
**Where**: `internal/orchestrator/prompt.go`
**Depends on**: T6
**Reuses**: Padrão de prompt layering de `input_pipeline.go`

**Done when**:
- [ ] Recebe: agent prompt base, CLAUDE.md, AGENTS.md, spec.md, design.md, task, siblings
- [ ] Monta prompt em camadas claras com headers
- [ ] Inclui regras de foco ("only touch files in your task")
- [ ] Inclui contexto de siblings ("other workers are doing X")
- [ ] Teste: gera prompt com todas as camadas

**Verify**:
```bash
go test ./internal/orchestrator/... -v -run TestBuildWorkerPrompt
```

---

### T14: BuildValidationPrompt [P]

**What**: Prompt pra Aurelia validar resultado de worker
**Where**: `internal/orchestrator/prompt.go`
**Depends on**: T6
**Reuses**: Nenhum

**Done when**:
- [ ] Recebe: task (com "Done When"), resultado do worker, spec, design
- [ ] Instruções de validação: critérios, scope creep, overengineering, padrões
- [ ] Formato de resposta: JSON com approved/issues/shouldRetry
- [ ] Teste: gera prompt com task e resultado

**Verify**:
```bash
go test ./internal/orchestrator/... -v -run TestBuildValidationPrompt
```

---

### T15: BuildConsolidationPrompt [P]

**What**: Prompt pra Aurelia sintetizar resultados finais
**Where**: `internal/orchestrator/prompt.go`
**Depends on**: T6
**Reuses**: Nenhum

**Done when**:
- [ ] Recebe: plano, resultados de todos os workers
- [ ] Instruções: resumir o que foi feito, listar problemas, próximos passos
- [ ] Teste: gera prompt com plano e resultados

**Verify**:
```bash
go test ./internal/orchestrator/... -v -run TestBuildConsolidationPrompt
```

---

### T16: Criar WorkerStatusReporter

**What**: Mensagens de status por worker no Telegram (send, edit, mark done/error)
**Where**: `internal/telegram/worker_status.go`
**Depends on**: T3 (WorkerEvent)
**Reuses**: Padrão do `progressReporter` (Send + Edit)

**Done when**:
- [ ] `SendStart(taskID, description)` envia mensagem formatada
- [ ] `UpdateProgress(taskID, toolName)` edita mensagem com tool display name
- [ ] `MarkDone(taskID, durationMs)` edita com ✅ e duração
- [ ] `MarkError(taskID, errMsg)` edita com ❌ e erro
- [ ] Thread-safe (sync.Mutex)
- [ ] Teste com fake bot: verifica Send e Edit chamados corretamente

**Verify**:
```bash
go test ./internal/telegram/... -v -run TestWorkerStatus
```

---

### T17: Criar executeApprovedPlan no input pipeline

**What**: Método que executa o ciclo completo: ensure docs → spawn workers → validate → merge → consolidate
**Where**: `internal/telegram/input_pipeline.go`
**Depends on**: T8, T10, T16, T20
**Reuses**: `orchestrator.ExecutePlan()`, `WorkerStatusReporter`

**Done when**:
- [ ] Chama `EnsureClaudeMd` e `EnsureAgentsMd`
- [ ] Chama `sendPlanSummary` com fases
- [ ] Executa workers por wave com feedback visual
- [ ] Chama `Validate` após cada worker
- [ ] Merge worktrees aprovados
- [ ] Atualiza tasks.md
- [ ] Consolida e envia resposta final
- [ ] Tratamento de erro: falha parcial, conflito de merge

**Verify**:
```bash
go build ./internal/telegram/...
```

---

### T18: Modificar processBridgeEventsAsync pra detectar plano

**What**: No handler de `result`, checar se contém marcador `aurelia-plan` e disparar Execution Mode
**Where**: `internal/telegram/input_pipeline.go`
**Depends on**: T7, T17
**Reuses**: `orchestrator.ExtractPlan()`

**Done when**:
- [ ] Após receber `result` event, chama `ExtractPlan(ev.Content)`
- [ ] Se plano encontrado → filtra bloco do texto, envia texto restante, dispara `executeApprovedPlan`
- [ ] Se plano não encontrado → fluxo existente (envia resposta normal)
- [ ] Teste: resposta com marcador → detecta plano
- [ ] Teste: resposta sem marcador → fluxo normal

**Verify**:
```bash
go test ./internal/telegram/... -v -run TestDetectPlan
```

---

### T19: Criar sendPlanSummary

**What**: Formata e envia resumo do plano de execução no Telegram
**Where**: `internal/telegram/worker_status.go`
**Depends on**: T3, T16
**Reuses**: `SendTextReply()`

**Done when**:
- [ ] Formata plano como lista numerada com fases e agents
- [ ] Envia como mensagem de reply no Telegram
- [ ] Teste: plano com 3 tasks → mensagem formatada corretamente

**Verify**:
```bash
go test ./internal/telegram/... -v -run TestSendPlanSummary
```

---

### T20: Criar EnsureAgentsMd e EnsureClaudeMd [P]

**What**: Gerar AGENTS.md e CLAUDE.md na raiz do projeto se não existirem
**Where**: `internal/orchestrator/agents_md.go`
**Depends on**: T6
**Reuses**: Nenhum — geração de markdown

**Done when**:
- [ ] `EnsureClaudeMd(root)` cria CLAUDE.md com template básico se não existir; NÃO sobrescreve se já existir
- [ ] `EnsureAgentsMd(root, agents)` cria AGENTS.md com squad table; atualiza se agents mudaram
- [ ] Teste: diretório vazio → ambos criados
- [ ] Teste: CLAUDE.md já existe → não sobrescreve
- [ ] Teste: AGENTS.md com squad diferente → atualiza

**Verify**:
```bash
go test ./internal/orchestrator/... -v -run TestEnsureDocs
```

---

### T21: Criar UpdateTasksStatus [P]

**What**: Atualizar `.specs/features/<feat>/tasks.md` marcando tasks como done/failed
**Where**: `internal/orchestrator/tasks_status.go`
**Depends on**: T3
**Reuses**: Nenhum — manipulação de markdown

**Done when**:
- [ ] Lê tasks.md existente
- [ ] Encontra checkboxes `- [ ]` dos critérios "Done When"
- [ ] Marca como `- [x]` pra tasks aprovadas
- [ ] Adiciona nota de falha pra tasks que falharam
- [ ] Teste: tasks.md com 3 tasks, 2 done, 1 failed → atualizado corretamente

**Verify**:
```bash
go test ./internal/orchestrator/... -v -run TestUpdateTasksStatus
```

---

### T22: Git commit e PR logic [P]

**What**: Lógica de git commit (conventional commits) e PR creation via `gh` CLI
**Where**: `internal/orchestrator/git.go`
**Depends on**: T4 (WorktreeManager)
**Reuses**: Git CLI via `os/exec`

**Done when**:
- [ ] `CommitChanges(repoRoot, message)` faz `git add -A` + `git commit -m`
- [ ] `CreatePR(repoRoot, title, body, baseBranch)` chama `gh pr create`
- [ ] `IsGHAvailable()` verifica se `gh` CLI está instalado e autenticado
- [ ] Teste: commit cria commit com mensagem correta
- [ ] Teste: gh indisponível → retorna erro gracioso

**Verify**:
```bash
go test ./internal/orchestrator/... -v -run TestGit
```

---

### T23: Wiring no BotController

**What**: Adicionar `Orchestrator` e `WorktreeManager` como dependências do BotController
**Where**: `internal/telegram/bot.go`, `cmd/aurelia/app.go`
**Depends on**: T6, T17, T18
**Reuses**: Padrão de constructor injection existente

**Done when**:
- [ ] `BotController` recebe `*orchestrator.Orchestrator`
- [ ] `app.go` cria Orchestrator com bridge e config
- [ ] Orchestrator prompt inclui persona + TLC + agents disponíveis
- [ ] System prompt da Aurelia atualizado com instruções de orquestração
- [ ] `go build ./...` compila

**Verify**:
```bash
go build ./...
```

---

### T24: Testes de integração

**What**: Testes que validam o fluxo completo com fake bridge
**Where**: `internal/orchestrator/orchestrator_test.go`
**Depends on**: T1-T23
**Reuses**: Padrão de testes existente com fakes

**Done when**:
- [ ] Teste: mensagem simples → Aurelia responde direto (sem plano)
- [ ] Teste: mensagem com aprovação → ExtractPlan detecta, ExecutePlan roda workers
- [ ] Teste: ExecutePlan com 2 waves → executa sequencialmente
- [ ] Teste: Validate aprovado → merge worktree
- [ ] Teste: Validate reprovado → retry + escalação
- [ ] Teste: worker falha → erro parcial reportado
- [ ] Todos os testes passam

**Verify**:
```bash
go test ./internal/orchestrator/... -v
```

---

### T25: Testes do Telegram integration

**What**: Testes que validam o fluxo Telegram com plan detection e status reporting
**Where**: `internal/telegram/input_pipeline_test.go`
**Depends on**: T16-T19, T23
**Reuses**: Testes existentes de `input_pipeline_test.go`

**Done when**:
- [ ] Teste: resposta com `aurelia-plan` marcador → dispara executeApprovedPlan
- [ ] Teste: resposta sem marcador → fluxo normal
- [ ] Teste: WorkerStatusReporter envia e edita mensagens corretamente
- [ ] Teste: sendPlanSummary formata plano
- [ ] Todos os testes passam

**Verify**:
```bash
go test ./internal/telegram/... -v
```

---

### T26: Validação final — build + vet + full test suite

**What**: Garantir que tudo compila, passa vet, e todos os testes passam
**Where**: Projeto inteiro
**Depends on**: T24, T25
**Reuses**: Comandos existentes de CLAUDE.md

**Done when**:
- [ ] `go build ./...` sem erros
- [ ] `go vet ./...` sem warnings
- [ ] `go test ./... -v` todos passam
- [ ] Zero regressão nos testes existentes

**Verify**:
```bash
go build ./... && go vet ./... && go test ./... -v
```

---

## Parallel Execution Map

```
Phase 1 (Sequential):
  T1 → T2
  T3 (parallel com T1)
  T4 (parallel com T1)

Phase 2 (Sequential, após Phase 1):
  T5 → T6 → T7 → T8 → T9 → T10

Phase 3 (Parallel, após T6):
  ├── T11 [P]
  ├── T12 [P]
  ├── T13 [P]
  ├── T14 [P]
  └── T15 [P]

Phase 4 (Sequential, após Phase 3):
  T16 → T17 → T18 → T19

Phase 5 (Parallel, após T6):
  ├── T20 [P]
  ├── T21 [P]
  └── T22 [P]

Phase 6 (Sequential, após Phase 4 + 5):
  T23 → T24 → T25 → T26
```

---

## Task Granularity Check

| Task | Scope | Status |
|------|-------|--------|
| T1: Agent struct fields | 1 file, 2 fields | ✅ Granular |
| T2: BuildSDKAgents update | 1 function | ✅ Granular |
| T3: Plan data model | 1 file, structs + 1 method | ✅ Granular |
| T4: WorktreeManager | 1 file, 4 methods | ✅ Granular |
| T5: DefaultWorkerConfig | 1 file, 2 functions | ✅ Granular |
| T6: Orchestrator struct | 1 file, struct + interface | ✅ Granular |
| T7: ExtractPlan | 1 function | ✅ Granular |
| T8: ExecutePlan | 1 function | ⚠️ Médio — mas coeso |
| T9: ExecuteTask | 1 function | ✅ Granular |
| T10: Validate | 1 function | ✅ Granular |
| T11-T15: Prompt builders | 1 function cada | ✅ Granular |
| T16: WorkerStatusReporter | 1 file, 4 methods | ✅ Granular |
| T17: executeApprovedPlan | 1 function | ⚠️ Médio — orquestra tudo |
| T18: Plan detection | 1 modificação pontual | ✅ Granular |
| T19: sendPlanSummary | 1 function | ✅ Granular |
| T20: Ensure docs | 2 functions | ✅ Granular |
| T21: UpdateTasksStatus | 1 function | ✅ Granular |
| T22: Git logic | 3 functions | ✅ Granular |
| T23: Wiring | 2 files, constructor changes | ✅ Granular |
| T24-T26: Tests | Validação | ✅ Granular |
