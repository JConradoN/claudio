# Agent Orchestration — Specification

## Problem Statement

A Aurelia já passa agents pelo bridge PI SDK, mas a orquestração é primitiva: routing manual e delegação invisível. O objetivo é transformar a Aurelia numa **tech lead autônoma** que segue a metodologia **spec-driven** (TLC) de ponta a ponta: Specify → Design → Tasks → Implement + Validate. No Planning Mode, ela colabora com o usuário pra criar specs e design. No Execution Mode, ela decompõe em tasks atômicas, spawna workers em worktrees isolados, valida os resultados, e entrega até o PR.

**Modelo arquitetural**: Múltiplas sessões do bridge, Go orquestra. Inspirado no [Composio agent-orchestrator](https://github.com/ComposioHQ/agent-orchestrator) — orchestrator + workers, worktrees isolados, decomposição LLM-driven. Nosso diferencial: bridge NDJSON com streaming real-time e metodologia spec-driven integrada.

**Workflow TLC completo da Aurelia**:

```
PLANNING MODE (colaborativo)                    EXECUTION MODE (autônomo)
Aurelia + Usuário                               Aurelia + Workers

Phase 1: SPECIFY                                Phase 4: IMPLEMENT + VALIDATE
  Cria .specs/features/<feat>/spec.md             Gera plano de execução (JSON)
  Discute, questiona overengineering              Spawna workers em worktrees
  Usuário aprova spec                             Workers executam tasks atômicas
        ↓                                         Aurelia valida resultados
Phase 2: DESIGN                                   Quality gate antes do merge
  Cria .specs/features/<feat>/design.md           Commit + PR se aprovado
  Define componentes, interfaces, reuso
  Usuário aprova design                   Workers recebem como contexto:
        ↓                                   - CLAUDE.md (convenções)
Phase 3: TASKS                                - AGENTS.md (squad/orchestration)
  Cria .specs/features/<feat>/tasks.md        - spec.md + design.md da feature
  Tasks atômicas, dependências, fases         - Sua task específica
  Usuário aprova tasks ──────────────────►    - Contexto dos siblings
```

## Goals

- [ ] Aurelia segue metodologia TLC: Specify → Design → Tasks → Implement + Validate
- [ ] Planning Mode cria artefatos `.specs/` que viram contexto dos workers
- [ ] CLAUDE.md e AGENTS.md na raiz do projeto como referência central
- [ ] Workers recebem contexto estruturado (docs do projeto + spec + design + task)
- [ ] Quality gate: Aurelia valida resultados dos workers antes de aceitar
- [ ] Ciclo completo: spec → design → tasks → implementação → validação → commit → PR

## Out of Scope

- Routing manual pelo usuário (@mention) — Aurelia decide sozinha quais agents usar
- UI web ou dashboard — feedback é 100% via Telegram
- Delegação em cadeia (worker delega pra outro worker) — 1 nível
- Persistência de estado entre workers — cada worker é stateless
- Multi-repo — workers operam no mesmo repositório (em worktrees)

---

## User Stories

### P1: Planning Mode — Specify + Design + Tasks ⭐ MVP

**User Story**: Como usuário, quero que a Aurelia siga a metodologia TLC comigo — criando spec, design e tasks como artefatos `.specs/` antes de executar qualquer código.

**Why P1**: Sem planejamento estruturado, workers não têm contexto e o resultado é imprevisível. Os artefatos TLC são o "contrato" entre o que o usuário quer e o que os workers executam.

**Acceptance Criteria**:

1. WHEN o usuário pede uma feature complexa THEN Aurelia SHALL entrar em Planning Mode e iniciar a fase Specify — perguntar sobre o problema, propor user stories, definir acceptance criteria
2. WHEN Aurelia completa a especificação THEN SHALL criar/atualizar `.specs/features/<feat>/spec.md` seguindo o template TLC (Problem, Goals, User Stories com WHEN/THEN/SHALL, Edge Cases)
3. WHEN o usuário aprova a spec THEN Aurelia SHALL avançar pra fase Design — definir arquitetura, componentes, interfaces, code reuse
4. WHEN Aurelia completa o design THEN SHALL criar `.specs/features/<feat>/design.md` seguindo o template TLC (Architecture, Code Reuse, Components, Data Models, Error Handling)
5. WHEN o usuário aprova o design THEN Aurelia SHALL avançar pra fase Tasks — quebrar em tasks atômicas com dependências, fases, e critérios de verificação
6. WHEN Aurelia completa as tasks THEN SHALL criar `.specs/features/<feat>/tasks.md` seguindo o template TLC (Execution Plan com fases, Task Breakdown com What/Where/DependsOn/DoneWhen/Verify)
7. WHEN o usuário aprova as tasks THEN Aurelia SHALL transicionar pro Execution Mode
8. WHEN a mensagem é simples (pergunta, bug trivial) THEN Aurelia SHALL pular planning e responder/executar direto
9. WHEN Aurelia identifica overengineering THEN SHALL questionar: "Você vai precisar disso nos próximos 3 meses?"

**Independent Test**: Enviar "implementa autenticação JWT" e verificar que Aurelia cria spec.md, espera aprovação, cria design.md, espera aprovação, cria tasks.md, espera aprovação — antes de executar qualquer código.

---

### P1: CLAUDE.md e AGENTS.md como Referência Central ⭐ MVP

**User Story**: Como operador, quero que o projeto tenha `CLAUDE.md` (convenções pra workers) e `AGENTS.md` (configuração do squad/orquestração) na raiz, pra que todo agent tenha contexto.

**Why P1**: Workers são stateless — o único contexto que têm é o que recebem no prompt. Sem docs padronizados, cada worker reinventa decisões que já foram tomadas.

**CLAUDE.md** — Convenções do projeto (já existe na maioria dos projetos):
- Comandos de build/test/lint
- Workflow (plan → review → execute → validate → commit)
- Regras (service layer, errors explícitos, context.Context, etc.)
- Packages e responsabilidades

**AGENTS.md** — Configuração do squad (novo):
- Agents disponíveis e suas especialidades
- Regras de orquestração (planning antes de execução, quality gate)
- Convenções de worktree e branching
- Referências aos artefatos `.specs/`

**Acceptance Criteria**:

1. WHEN Aurelia inicia Execution Mode THEN SHALL verificar que `CLAUDE.md` existe; se não, SHALL criar com as convenções básicas do projeto
2. WHEN Aurelia inicia Execution Mode THEN SHALL verificar que `AGENTS.md` existe; se não, SHALL criar com a configuração do squad
3. WHEN um worker é spawnado THEN SHALL receber `CLAUDE.md` + `AGENTS.md` como contexto no system prompt
4. WHEN um worker é spawnado pra uma feature THEN SHALL receber também `spec.md` + `design.md` + sua task de `tasks.md`
5. WHEN o projeto já tem `CLAUDE.md` THEN Aurelia SHALL respeitar e não sobrescrever

**Independent Test**: Iniciar Execution Mode num projeto sem CLAUDE.md, verificar que são criados. Spawnar worker e verificar que recebe os docs no system prompt.

---

### P1: Execution Mode — Implement + Validate ⭐ MVP

**User Story**: Como usuário, depois de aprovar as tasks, quero que a Aurelia execute autonomamente — spawnando workers, validando resultados, e entregando código pronto.

**Why P1**: É o core da orquestração. Planning gera os artefatos, Execution os consome.

**Acceptance Criteria**:

1. WHEN o usuário aprova as tasks THEN Aurelia SHALL gerar plano de execução estruturado (JSON) baseado no `tasks.md`
2. WHEN Aurelia gera o plano THEN cada task SHALL referenciar: agent (nome do registry ou "worker"), prompt, dependências, e se precisa de worktree
3. WHEN Go recebe o plano THEN SHALL informar no Telegram as fases de execução
4. WHEN Go executa THEN SHALL spawnar workers por fase, respeitando dependências (paralelo dentro da fase, sequencial entre fases)
5. WHEN cada worker é spawnado THEN SHALL receber: system prompt do agent + CLAUDE.md + AGENTS.md + spec.md + design.md + task específica + contexto de siblings
6. WHEN um worker completa THEN Go SHALL coletar resultado e passar pra Aurelia validar
7. WHEN Aurelia valida THEN SHALL verificar: critérios "Done When" da task, testes passam, sem scope creep, sem overengineering
8. WHEN validação passa THEN Aurelia SHALL aprovar e Go faz merge do worktree
9. WHEN validação falha THEN Aurelia SHALL spawnar worker de correção com feedback específico
10. WHEN todas as tasks completam e validam THEN Aurelia SHALL atualizar `tasks.md` marcando tasks como done

**Independent Test**: Aprovar tasks de "implementa /health", verificar que workers executam, Aurelia valida, e tasks.md é atualizado.

---

### P1: Workers Genéricos com Agents Opcionais ⭐ MVP

**User Story**: Como operador, quero que funcione out-of-the-box com worker default, mas que agents especializados (se existirem) sejam usados quando relevante.

**Why P1**: Worker default garante zero-config. Agents especializados permitem otimização gradual.

**Cascata de resolução**: agent do registry → worker.md override → worker default hardcoded.

**Acceptance Criteria**:

1. WHEN o sistema inicia THEN SHALL ter worker default hardcoded que funciona sem nenhum `.md`
2. WHEN `worker.md` existe no registry THEN SHALL sobrescrever o default
3. WHEN agents especializados existem (ex: `qa.md`, `code-reviewer.md`) THEN Aurelia SHALL conhecê-los e referenciá-los nas tasks
4. WHEN task referencia agent que existe → usa config dele; que não existe → fallback worker default
5. WHEN worker executa THEN SHALL ter acesso completo (Read, Write, Edit, Bash, Grep, Glob)
6. WHEN Aurelia (orquestradora) executa THEN SHALL ter apenas ferramentas read-only

**Independent Test**: Sem `.md`: worker default executa. Com `qa.md`: task de teste usa agent qa.

---

### P1: Git Worktrees Isolados ⭐ MVP

**User Story**: Como operador, quero que cada worker trabalhe num worktree isolado.

**Why P1**: Workers paralelos sem worktrees pisam nos arquivos um do outro.

**Acceptance Criteria**:

1. WHEN Go spawna worker de implementação/teste THEN SHALL criar worktree com branch próprio
2. WHEN worktree é criado THEN Go SHALL passar como `cwd` no `bridge.Execute()`
3. WHEN worker termina com sucesso e Aurelia valida THEN Go SHALL fazer merge
4. WHEN worker termina THEN Go SHALL limpar worktree
5. WHEN task é read-only THEN SHALL usar diretório principal
6. WHEN merge conflita THEN Go SHALL informar e tentar resolver ou pedir ajuda

**Independent Test**: 2 workers paralelos, worktrees separados, merge sem conflito.

---

### P1: Feedback Visual no Telegram ⭐ MVP

**User Story**: Como usuário, quero ver as fases TLC e status dos workers em tempo real.

**Acceptance Criteria**:

1. WHEN Aurelia muda de fase (Specify → Design → Tasks) THEN SHALL informar no Telegram
2. WHEN Go recebe plano de execução THEN SHALL enviar fases e tasks planejadas
3. WHEN worker é spawnado THEN SHALL enviar mensagem de status
4. WHEN worker progride (tool_use) THEN SHALL editar mensagem de status
5. WHEN worker termina THEN SHALL editar pra conclusão ou erro
6. WHEN Aurelia valida THEN SHALL informar resultado da validação
7. WHEN tudo completa THEN SHALL enviar resposta final consolidada

---

### P1: Ciclo Git Autônomo ⭐ MVP

**User Story**: Como usuário, quero que a Aurelia complete até commit e PR após validação.

**Acceptance Criteria**:

1. WHEN workers completam e Aurelia valida THEN Go SHALL merge worktrees e commitar
2. WHEN commit THEN SHALL usar Conventional Commits
3. WHEN usuário pede PR ou Aurelia julga apropriado THEN SHALL criar via `gh` CLI
4. WHEN PR é criado THEN SHALL informar URL no Telegram
5. WHEN validação identifica problemas THEN Aurelia SHALL delegar correções antes de commitar

---

### P2: Controle de Profundidade e Limites

**Acceptance Criteria**:

1. WHEN agent/worker não define `max_turns` THEN usa default global (25)
2. WHEN worker atinge `maxTurns` THEN bridge retorna erro, Go edita status

---

### P2: Rastreamento de Custo por Worker

**Acceptance Criteria**:

1. WHEN worker termina THEN bridge inclui `duration_ms` e `cost_usd`
2. WHEN status editado pra "Concluído" THEN inclui duração

---

### P3: Budget por Worker

**Acceptance Criteria**:

1. WHEN config define `max_budget_usd` THEN bridge respeita
2. WHEN worker atinge budget THEN trata como erro

---

## Edge Cases

- WHEN bridge morre durante worker THEN Go segue recovery, limpa worktrees e status
- WHEN conversa simples THEN responde direto sem planning
- WHEN status falha (rate limit) THEN loga e continua
- WHEN não há mudanças pra commit THEN informa
- WHEN `gh` CLI não configurado THEN commita local e informa
- WHEN merge conflita THEN informa e tenta resolver ou pede ajuda
- WHEN Aurelia retorna plano JSON inválido THEN pede retry ou erro
- WHEN worker falha mas outros succedem THEN informa falha parcial, consolida o que funcionou
- WHEN usuário muda de ideia durante Planning THEN itera sem perder contexto (session resume)
- WHEN task trivial THEN pula planning e executa direto
- WHEN projeto não tem CLAUDE.md THEN Aurelia cria com convenções básicas antes de spawnar workers
- WHEN validação falha repetidamente (3x) THEN Aurelia escala pro usuário em vez de ficar em loop

---

## Success Criteria

- [ ] Aurelia segue TLC end-to-end: Specify → Design → Tasks → Implement → Validate
- [ ] Artefatos `.specs/` criados no Planning Mode viram contexto dos workers
- [ ] CLAUDE.md + AGENTS.md existem na raiz como referência central
- [ ] Workers recebem contexto completo (docs + spec + design + task)
- [ ] Quality gate: Aurelia valida antes de aceitar merge
- [ ] Worker default funciona out-of-the-box, agents especializados são opcionais
- [ ] Worktrees isolados — workers paralelos não conflitam
- [ ] Feedback visual em tempo real — fases TLC + status por worker
- [ ] Ciclo completo: spec → design → tasks → implementação → validação → commit → PR
- [ ] Zero regressão — conversas simples funcionam identicamente ao atual
