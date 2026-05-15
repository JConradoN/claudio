# Agent Tools Fix — Especificação

## Problem Statement

O sistema de agentes do Aurelia permite definir `allowed_tools` e `disallowed_tools` no frontmatter YAML dos arquivos de agente. Porém, apenas `allowed_tools` é efetivamente usado. O campo `disallowed_tools` é definido no struct `Agent` mas nunca é lido nem aplicado em nenhum lugar do pipeline ou bridge.

Isso cria uma falsa expectativa: o usuário configura um agente para bloquear ferramentas perigosas (ex: `Bash` para um agente de leitura-only), mas o PI SDK continua usando essas ferramentas normalmente.

Além disso, o mapeamento de nomes de ferramentas entre o Aurelia e o PI SDK (`translateToolName`) pode estar desatualizado, não cobrindo ferramentas built-in adicionais do PI SDK.

## Goals

- [ ] `disallowed_tools` no agente é respeitado e as ferramentas são efetivamente bloqueadas no PI SDK
- [ ] Mapeamento de ferramentas entre Aurelia e PI SDK está completo e atualizado
- [ ] Validação de ferramentas desconhecidas gera warning em vez de silenciosamente ignorar

## Out of Scope

- Novas ferramentas custom (fora das built-in do PI SDK)
- Mudanças no comportamento de `allowed_tools` (já funciona)
- UI para visualizar quais ferramentas estão ativas

---

## User Stories

### P1: Disallowed Tools Funcional — MVP

**User Story**: Como usuário, quando configuro `disallowed_tools` no meu agente, quero que essas ferramentas sejam realmente bloqueadas, pra evitar que o PI execute ações indesejadas.

**Why P1**: Campo morto é bug. O usuário confia na configuração e fica vulnerável.

**Acceptance Criteria**:

1. WHEN um agente define `disallowed_tools` no frontmatter THEN o bridge SHALL filtrar essas ferramentas da lista enviada ao PI SDK
2. WHEN um agente define apenas `disallowed_tools` (sem `allowed_tools`) THEN o PI SDK SHALL receber todas as ferramentas built-in EXCETO as listadas em `disallowed_tools`
3. WHEN um agente define ambos `allowed_tools` e `disallowed_tools` THEN o sistema SHALL aplicar ambos: permite apenas `allowed_tools` e depois remove `disallowed_tools` do resultado (interseção menos denylist)
4. WHEN `disallowed_tools` contém nomes inválidos/inexistentes THEN o sistema SHALL logar um warning e ignorar os nomes inválidos

**Independent Test**: Criar agente com `disallowed_tools: [Bash]`. Enviar mensagem que normalmente usaria Bash. Verificar que Bash não é chamado.

---

### P2: Mapeamento de Ferramentas Completo — Should Have

**User Story**: Como desenvolvedor, quero que todas as ferramentas built-in do PI SDK sejam mapeáveis nos agentes, pra ter controle total sobre o que o PI pode fazer.

**Why P2**: Ferramentas novas do PI SDK podem não estar cobertas pelo mapeamento atual.

**Acceptance Criteria**:

1. WHEN uma ferramenta built-in do PI SDK é referenciada em `allowed_tools` ou `disallowed_tools` THEN ela SHALL ser mapeada corretamente para o nome interno do PI
2. WHEN uma ferramenta não mapeada é usada THEN o sistema SHALL logar warning listando as ferramentas conhecidas
3. A lista de mapeamento SHALL cobrir pelo menos: read, write, edit, bash, grep, find, ls, web_search, web_search_premium, browser, canvas, image (e outras built-in do PI)

**Independent Test**: Verificar que todas as ferramentas built-in do PI SDK têm mapeamento válido no Aurelia.

---

## Edge Cases

- WHEN ambos `allowed_tools` e `disallowed_tools` estão vazios/nil THEN o PI SDK recebe todas as ferramentas (comportamento atual)
- WHEN `allowed_tools` e `disallowed_tools` têm overlap (mesma ferramenta em ambos) THEN `disallowed_tools` prevalece (ferramenta é bloqueada)
- WHEN `disallowed_tools` contém apenas nomes inválidos THEN loga warning e PI recebe todas as ferramentas (ou as do `allowed_tools`)
- WHEN agente é nil (nenhum agente selecionado) THEN comportamento existente se mantém

---

## Success Criteria

- [ ] `disallowed_tools` efetivamente bloqueia ferramentas no PI SDK
- [ ] Comportamento correto quando `allowed_tools` e `disallowed_tools` coexistem
- [ ] Warnings para nomes de ferramentas inválidos/desconhecidos
- [ ] Testes cobrem: denylist only, allowlist+denylist, overlap, nomes inválidos
- [ ] Nenhuma regressão no comportamento existente de `allowed_tools`
