# PI Resilience — Especificação

## Problem Statement

O Aurelia usa o PI SDK (`@earendil-works/pi-coding-agent`) como seu "cérebro". Quando o PI falha — seja por rate limit do provider, API key inválida, modelo indisponível, ou erro de rede — a experiência do usuário é ruim:

1. **Mensagens em inglês cru**: O PI emite erros como `"Rate limit exceeded"` ou `"Context length exceeded"`. O bridge repassa diretamente. O usuário brasileiro vê jargão técnico em inglês sem contexto.
2. **Sem distinção de severidade**: Rate limit (transitório, resolve em segundos) e API key inválida (permanente, só resolve com ação humana) são tratados igual — erro terminal, fim.
3. **Sem retry**: Erros transitórios (rate limit, timeout, 503) não são retentados. O usuário precisa reenviar manualmente.
4. **Sem fallback**: Se o provider principal (ex: Kimi) está instável, não há alternativa. O usuário fica sem resposta.
5. **Sem circuit breaker**: Se um provider está falhando repetidamente, cada nova mensagem do usuário tenta e falha individualmente. Não há proteção contra storm de erros.
6. **Erros podem ser silenciados**: Se o bridge já emitiu eventos parciais (`terminalEmitted=true`), um erro posterior no PI catch é completamente ignorado.
7. **Sem contexto de custo**: O usuário não sabe que o fallback para OpenRouter free pode ser mais lento ou ter limites de rate mais agressivos.

O impacto é perda de confiança: o usuário não sabe se o bot está quebrado, se é culpa dele, ou se deve esperar.

## Goals

- [ ] Erros do PI são classificados, traduzidos para português, e vêm com ação sugerida
- [ ] Erros transitórios (rate limit, timeout, 503) são retentados com exponential backoff
- [ ] Falha persistente de um provider dispara fallback automático para OpenRouter (modelos free)
- [ ] Circuit breaker por provider evita retry storms em falhas massivas
- [ ] Fallback é transparente — usuário é informado brevemente que o modelo alternativo está sendo usado
- [ ] Erros não são silenciados pelo flag `terminalEmitted`

## Out of Scope

- Mudanças no PI SDK em si (código de terceiro)
- Fallback para providers pagos (ex: Anthropic, Google, Kimi) — foco é OpenRouter free
- Persistência de estado do circuit breaker em disco (memória apenas)
- Health check proativo / polling de status de providers
- Retry de execuções do orchestrator (escopo separado)
- Retry de cron jobs (escopo separado)

---

## User Stories

### P1: Erros Traduzidos e Actionable — MVP

**User Story**: Como usuário do Telegram, quando o PI retorna um erro, quero entender o que aconteceu e o que fazer, em português.

**Why P1**: É a base de toda resiliência. Sem entender o erro, o usuário não pode reagir.

**Acceptance Criteria**:

1. WHEN o PI retorna um erro THEN a mensagem enviada ao usuário SHALL estar em português e SHALL classificar o erro em uma categoria amigável
2. WHEN o erro é de autenticação (API key inválida, não configurada) THEN a mensagem SHALL ser: `"🔐 Erro de autenticação com o provider {provider}\n\nVerifique se a API key está configurada corretamente em ~/.aurelia/config/app.json"`
3. WHEN o erro é rate limit THEN a mensagem SHALL ser: `"⏳ O provider {provider} está sobrecarregado no momento.\n\nVou tentar novamente automaticamente..."` (e o sistema SHALL retry)
4. WHEN o erro é "model not found" THEN a mensagem SHALL ser: `"⚠️ Modelo {model} não encontrado no provider {provider}.\n\nUse /model para ver os modelos disponíveis."`
5. WHEN o erro é "context length exceeded" THEN a mensagem SHALL ser: `"📄 A conversa ficou muito longa para o modelo {model}.\n\nUse /new para iniciar uma nova sessão."`
6. WHEN o erro é desconhecido/não mapeado THEN a mensagem SHALL ser: `"❌ Erro no processador: {mensagem_original}\n\nSe persistir, tente /new ou mude de modelo com /model."`

**Independent Test**: Mockar bridge para retornar cada tipo de erro. Verificar mensagem em português com categoria correta.

---

### P1: Retry com Backoff para Erros Transitórios — MVP

**User Story**: Como usuário, quando o provider dá rate limit ou timeout, quero que o sistema tente novamente sozinho, sem eu precisar reenviar.

**Why P1**: Rate limit e timeout são comuns e transientes. Reenvio manual é frustração desnecessária.

**Acceptance Criteria**:

1. WHEN o PI retorna erro classificado como `transient` (rate limit, timeout, 503, network error) THEN o sistema SHALL fazer até 3 retries no **mesmo** provider/modelo
2. WHEN um retry é executado THEN o delay entre tentativas SHALL seguir exponential backoff: 2s, 4s, 8s (total máximo ~14s de espera)
3. WHEN o retry é bem-sucedido THEN a resposta SHALL ser entregue normalmente, sem informar o usuário das tentativas falhas (silencioso)
4. WHEN todos os 3 retries falham no mesmo provider THEN o sistema SHALL passar para o fallback (OpenRouter free) ou, se fallback indisponível, enviar erro final traduzido
5. WHEN o erro não é transient (auth, model not found, content policy, context length) THEN o sistema SHALL NOT fazer retry — erro imediato

**Independent Test**: Configurar bridge para falhar 2x com "Rate limit exceeded" e depois suceder. Verificar que resposta chega sem reenvio manual.

---

### P1: Fallback Automático para OpenRouter Free — MVP

**User Story**: Como usuário, quando meu provider principal está instável, quero que o bot use automaticamente uma alternativa free, pra não ficar sem resposta.

**Why P1**: É o diferencial de resiliência. Sem fallback, provider down = bot morto.

**Acceptance Criteria**:

1. WHEN o provider principal falha após retries (3 tentativas) OR o circuit breaker está aberto para o provider THEN o sistema SHALL tentar fallback para OpenRouter free
2. WHEN o fallback é ativado THEN a request SHALL usar provider=`openrouter` e model=`openrouter/free` (router automático de modelos free)
3. WHEN o fallback é ativado THEN o usuário SHALL ser informado brevemente: `"⚡ Provider {original} instável no momento. Usando modelo alternativo (OpenRouter free) enquanto isso..."`
4. WHEN o fallback (OpenRouter free) também falha THEN o sistema SHALL enviar erro final: `"❌ Não consegui processar sua mensagem. Todos os providers disponíveis estão instáveis no momento.\n\nTente novamente em alguns minutos ou verifique /status."`
5. WHEN o OpenRouter não está configurado (sem API key) THEN o fallback SHALL ser pulado e o sistema SHALL ir direto para erro final, sugerindo configurar OpenRouter como alternativa
6. WHEN o fallback é bem-sucedido THEN a resposta SHALL ser entregue normalmente e a sessão SHALL ser mantida (session ID do OpenRouter é usado daí em diante para continuidade)

**Independent Test**: Desabilitar provider principal (Kimi). Enviar mensagem. Verificar que OpenRouter free responde.

**OpenRouter Free Models para fallback**:
- **Primário**: `openrouter/free` — Router automático que escolhe o melhor modelo free disponível (200K context, vision + tools)
- **Secundário** (se router falhar): `qwen/qwen3-coder:free` — Qwen3 Coder 480B A35B, foco em coding (262K context, tools)
- **Terciário** (se secundário falhar): `nvidia/nemotron-3-super-120b-a12b:free` — Nemotron 3 Super, modelo generalista (262K context, tools)

---

### P2: Circuit Breaker por Provider — Should Have

**User Story**: Como sistema, quero parar de tentar um provider que está falhando repetidamente, pra não piorar a situação e gastar tempo do usuário.

**Why P2**: Protege contra retry storms. Se Kimi está down, não faz sentido tentar 3x a cada mensagem.

**Acceptance Criteria**:

1. WHEN um provider acumula 5 erros consecutivos em menos de 2 minutos THEN o circuit breaker SHALL abrir para esse provider
2. WHEN o circuit breaker está aberto para um provider THEN novas requests para esse provider SHALL ser imediatamente redirecionadas para fallback (OpenRouter free), SEM tentar o provider original
3. WHEN o circuit breaker está aberto THEN uma mensagem breve SHALL ser enviada ao usuário na primeira vez: `"⚠️ Provider {provider} está com instabilidade. Usando alternativa temporariamente."`
4. WHEN passam 5 minutos desde a abertura do circuit breaker THEN o sistema SHALL tentar "half-open" — enviar 1 request de teste para o provider original
5. WHEN o request de teste (half-open) é bem-sucedido THEN o circuit breaker SHALL fechar e o provider volta a ser usado normalmente
6. WHEN o request de teste (half-open) falha THEN o circuit breaker SHALL reabrir por mais 5 minutos

**Independent Test**: Simular 5 erros consecutivos do provider. Verificar que a 6ª mensagem vai direto para fallback sem tentar o provider original.

---

### P2: Prevenção de Silenciamento de Erro — Should Have

**User Story**: Como usuário, quando algo dá errado durante o processamento, quero sempre saber, mesmo se parte da resposta já foi enviada.

**Why P2**: Hoje, se o PI emitiu eventos parciais e depois deu erro no catch, o erro é ignorado. O usuário fica esperando uma resposta que nunca vem.

**Acceptance Criteria**:

1. WHEN o bridge processa uma request e `terminalEmitted=true` (result parcial já enviado) THEN um erro subsequente no catch SHALL ainda ser propagado como evento terminal
2. WHEN um erro é detectado após `terminalEmitted=true` THEN o sistema SHALL enviar uma mensagem de erro ao usuário: `"❌ O processamento foi interrompido antes de concluir. Erro: {mensagem}"`
3. WHEN não há erro mas `terminalEmitted=true` sem evento `result` final (edge case) THEN o sistema SHALL tratar como erro: `"❌ O processador encerrou sem resposta completa."`

**Independent Test**: Mockar bridge que emite eventos parciais e depois dá erro. Verificar que erro chega ao usuário.

---

### P3: Fallback Inteligente por Tipo de Tarefa — Nice to Have

**User Story**: Como usuário, quando o fallback é ativado, quero que o modelo alternativo seja adequado para o tipo de tarefa que estou fazendo.

**Why P3**: Modelos free têm especializações diferentes. Coding task → Qwen Coder. General task → router automático.

**Acceptance Criteria**:

1. WHEN o fallback é ativado E a tarefa parece ser coding (presença de ferramentas Read/Write/Bash) THEN o sistema SHALL preferir `qwen/qwen3-coder:free` ao invés do router genérico
2. WHEN o fallback é ativado E há imagens anexadas THEN o sistema SHALL preferir um modelo com vision support (ex: `google/gemma-4-31b-it:free`) se disponível
3. WHEN o fallback é ativado e o modelo especializado falha THEN o sistema SHALL fallback para o router genérico `openrouter/free`

**Independent Test**: Enviar tarefa de coding com provider principal down. Verificar que Qwen Coder free é usado.

---

## Edge Cases

- WHEN o provider principal e o OpenRouter estão ambos sem API key configurada THEN o sistema SHALL enviar erro de autenticação geral, sugerindo configurar pelo menos um provider
- WHEN o circuit breaker abre durante uma execução em andamento (pipeline já começou) THEN a execução atual continua; o circuit breaker só afeta requests novas
- WHEN o fallback OpenRouter free atinge seu próprio rate limit THEN o sistema SHALL fazer 1 retry no OpenRouter e depois desistir — não há segundo nível de fallback
- WHEN a sessão foi criada no provider principal e o fallback é ativado para a próxima mensagem THEN a sessão do provider principal é preservada; a mensagem no fallback inicia uma sessão nova (limitação do PI SDK — sessões não são portáveis entre providers)
- WHEN o circuit breaker fecha (provider recuperado) e há uma sessão ativa no fallback THEN a próxima mensagem volta ao provider principal, iniciando sessão nova se necessário
- WHEN o PI retorna erro vazio (`""`) ou `null` THEN o sistema SHALL tratar como "Erro desconhecido no processador" com sugestão genérica
- WHEN o retry é cancelado pelo usuário ("para", "cancela") durante o backoff THEN o retry SHALL ser abortado imediatamente

---

## Success Criteria

- [ ] Erros do PI traduzidos para português com classificação e ação sugerida
- [ ] Retry com exponential backoff funciona para rate limit, timeout, 503
- [ ] Fallback para OpenRouter free ativado após falha persistente do provider principal
- [ ] Circuit breaker abre após 5 erros em 2 minutos; fecha após half-open bem-sucedido
- [ ] Erros não são silenciados pelo `terminalEmitted`
- [ ] Usuário é informado brevemente quando fallback é ativado
- [ ] Testes unitários cobrem: classificação de erros, retry, fallback, circuit breaker
- [ ] Nenhuma regressão nos testes existentes
