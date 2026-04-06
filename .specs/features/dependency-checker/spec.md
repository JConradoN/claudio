# Dependency Checker — Specification

## Problem Statement

Quando um usuário instala a Aurelia pela primeira vez e roda `aurelia setup`, o onboarding pede API keys e modelo, mas nunca verifica se as dependencias de runtime estao presentes (Node.js, npm, git). Se Node.js nao esta instalado, o usuario so descobre depois do onboarding, quando o bridge falha com um erro criptico como "exec: node: executable file not found in PATH".

No boot normal (sem onboarding), se o Node nao existe o bridge tambem falha sem explicacao clara.

## Goals

- [ ] Onboarding mostra checklist visual de dependencias antes de configurar
- [ ] Boot verifica dependencias criticas (node/npm) e falha com mensagem clara
- [ ] Dependencias opcionais (git, gh) sao sinalizadas mas nao bloqueiam
- [ ] Versoes minimas sao verificadas quando relevante (Node >= 18)
- [ ] Check eh rapido (< 2s total) e nao trava o startup

## Out of Scope

- Instalacao automatica de dependencias (nao somos package manager)
- Verificacao de versao do Go (se esta rodando o binario, ja compilou)
- Verificacao de conectividade de rede / API keys validas (ja coberto pelo onboarding)
- Health checks periodicos de dependencias apos boot

---

## Dependency Matrix

| Dependencia | Tipo | Obrigatoria | Versao Minima | Usado por | Detectar com |
|---|---|---|---|---|---|
| Node.js | CLI | Sim | v18.0.0 | Bridge (Claude Agent SDK) | `node --version` |
| npm | CLI | Sim | v8.0.0 | Bridge setup (npm install) | `npm --version` |
| git | CLI | Nao | v2.0.0 | Orchestrator (worktree, commits) | `git --version` |
| gh | CLI | Nao | qualquer | Orchestrator (criar PRs) | `gh --version` |

---

## User Stories

### P1: Checklist Visual no Onboarding

**User Story**: Como usuario novo, quero ver quais dependencias estao instaladas e quais faltam antes de configurar, pra saber se preciso instalar algo.

**Acceptance Criteria**:

1. WHEN o usuario roda `aurelia setup` THEN o primeiro passo do TUI mostra um checklist de dependencias com status (ok/faltando/versao baixa)
2. WHEN todas as dependencias obrigatorias estao presentes THEN o usuario pode prosseguir pro proximo passo normalmente
3. WHEN uma dependencia obrigatoria falta THEN o sistema mostra instrucoes de instalacao e bloqueia o avancar
4. WHEN uma dependencia opcional falta THEN o sistema mostra aviso amarelo mas permite continuar
5. WHEN as versoes sao insuficientes THEN o sistema mostra a versao encontrada e a minima necessaria

**Exemplo visual**:
```
Dependencies

  [ok]  Node.js v22.14.0 (requires >= 18.0.0)
  [ok]  npm v10.9.2 (requires >= 8.0.0)
  [!!]  git — not found (optional, needed for orchestrator)
  [ok]  gh v2.74.0

Press Enter to continue. Use left to go back.
```

### P2: Check no Boot

**User Story**: Como usuario existente, quero que a Aurelia me diga claramente se falta Node.js ao iniciar, em vez de mostrar um erro generico de bridge.

**Acceptance Criteria**:

1. WHEN o boot detecta que `node` nao esta no PATH THEN o sistema loga erro fatal: "Node.js is required but not found. Install it from https://nodejs.org/" e encerra
2. WHEN o boot detecta Node mas versao < 18 THEN o sistema loga erro fatal com a versao encontrada e a minima
3. WHEN `npm` nao esta no PATH mas `node` esta THEN o sistema loga warning: "npm not found — bridge setup may fail"
4. WHEN `git` nao esta no PATH THEN o sistema loga warning (nao fatal): "git not found — orchestrator features disabled"
5. WHEN todas as dependencias estao OK THEN nenhum log extra eh emitido (silencioso no caso feliz)

---

## Technical Notes

- Usar `exec.LookPath` pra verificar presenca no PATH
- Parsear versao do output de `--version` com regex simples
- Check deve rodar antes do `EnsureBridge` (que precisa de node/npm)
- No TUI, o check pode ser o Step 0 (antes de LLM Provider)
- Resultado do check pode ser cacheado na struct do onboarding pra nao rodar 2x
