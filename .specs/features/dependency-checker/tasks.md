# Dependency Checker — Tasks

**Design**: `.specs/features/dependency-checker/design.md`
**Status**: Pending

---

## Execution Plan

### Phase 1: Core Package (Sequential)

```
T1 → T2 → T3
```

### Phase 2: Integration (Parallel)

```
     ┌→ T4 ─┐
T3 ──┤      ├──→ T6
     └→ T5 ─┘
```

### Phase 3: Polish (Sequential)

```
T6 → T7
```

---

## Task Breakdown

### T1: Create `internal/deps/check.go` — structs and version parsing

**What**: Definir structs `DepStatus`, `CheckResult` e implementar `parseVersion()` + `compareVersions()`.
**Where**: `internal/deps/check.go`
**Depends on**: None

**Done when**:
- [ ] Structs `DepStatus` e `CheckResult` definidas
- [ ] `parseVersion(output string) string` extrai semver de output arbitrario (`v22.14.0` → `22.14.0`, `git version 2.43.0.windows.1` → `2.43.0`)
- [ ] `compareVersions(a, b string) int` compara corretamente (`22.14.0` > `18.0.0`, `2.0.0` == `2.0.0`, `8.0.0` < `10.9.2`)
- [ ] Tests pass: `go test ./internal/deps/ -run TestParseVersion -v`
- [ ] Tests pass: `go test ./internal/deps/ -run TestCompareVersions -v`

**Verify:**
```bash
go test ./internal/deps/ -run "TestParse|TestCompare" -v
```

---

### T2: Implement `CheckAll()` — dependency detection

**What**: Implementar `CheckAll()` que roda `exec.LookPath` + `<cmd> --version` com timeout pra cada dependencia.
**Where**: `internal/deps/check.go`
**Depends on**: T1

**Dependencies to check**:
| Name | Command | Required | MinVersion | VersionFlag | InstallURL |
|---|---|---|---|---|---|
| Node.js | node | true | 18.0.0 | --version | https://nodejs.org/ |
| npm | npm | true | 8.0.0 | --version | https://nodejs.org/ |
| git | git | false | 2.0.0 | --version | https://git-scm.com/ |
| gh | gh | false | (none) | --version | https://cli.github.com/ |

**Done when**:
- [ ] `CheckAll()` retorna status correto pra todas as 4 dependencias
- [ ] Timeout de 5s por check (nao trava se comando trava)
- [ ] `AllOK` eh `true` quando todos os required estao Found + VersionOK
- [ ] `AllOK` eh `false` quando qualquer required falta ou tem versao baixa
- [ ] Optional missing nao afeta `AllOK`
- [ ] Tests pass: `go test ./internal/deps/ -run TestCheckAll -v`

**Verify:**
```bash
go test ./internal/deps/ -v
```

---

### T3: Add helper `checkOne()` with unit test

**What**: Extrair logica de check individual pra facilitar teste e reuso. `checkOne(name, cmd, required, minVer, flag, url) DepStatus`.
**Where**: `internal/deps/check.go`
**Depends on**: T1

**Done when**:
- [ ] `checkOne()` encapsula LookPath + exec + parse + compare
- [ ] Testavel com mock (aceitar um `lookPathFn` ou testar com binarios reais do sistema)
- [ ] Tests pass: `go test ./internal/deps/ -v`

**Verify:**
```bash
go test ./internal/deps/ -v
```

---

### T4: Onboarding TUI — Step Dependencies

**What**: Adicionar `stepDependencies` como primeiro step do onboarding TUI. Mostra checklist visual com cores.
**Where**: `cmd/aurelia/onboard.go`, `cmd/aurelia/onboard_ui.go`, `cmd/aurelia/onboard_helpers.go`
**Depends on**: T2, T3

**Changes**:
1. Novo valor `stepDependencies` no enum `onboardStep` (posicao 0, antes de `stepLLMProvider`)
2. Ajustar contagem de steps (11 → 12)
3. Adicionar cores `colorGreen`, `colorYellow`, `colorRed` em `onboard_helpers.go`
4. Renderizar checklist no `View()` — step `stepDependencies`
5. Handler de key: Enter avanca se `AllOK`, bloqueia se required faltando
6. Cachear `CheckResult` na struct `onboardingUI` (nao rodar 2x)

**Done when**:
- [ ] Step aparece como primeiro step ao rodar `aurelia setup`
- [ ] Deps encontradas mostram `[ok]` em verde com versao
- [ ] Deps required faltando mostram `[!!]` em vermelho com URL de instalacao
- [ ] Deps optional faltando mostram `[--]` em amarelo
- [ ] Enter avanca quando todas required estao ok
- [ ] Enter bloqueado + mensagem quando required falta
- [ ] Left nao faz nada (eh o primeiro step)
- [ ] Contagem de steps atualizada (Step 1/12)
- [ ] Tests pass: `go test ./cmd/aurelia/ -short`

**Verify:**
```bash
go run ./cmd/aurelia setup
# Visual: checklist aparece como Step 1/12
```

---

### T5: Boot Check em `app.go`

**What**: Chamar `deps.CheckAll()` antes de `EnsureBridge` e falhar com mensagem clara se dependencia critica falta.
**Where**: `cmd/aurelia/app.go`
**Depends on**: T2, T3

**Changes**:
1. Importar `internal/deps`
2. Chamar `deps.CheckAll()` antes de `bridge.EnsureBridge()`
3. `log.Fatalf` pra required deps faltando ou com versao baixa
4. `log.Printf` warning pra optional deps faltando
5. Silencioso quando tudo ok

**Done when**:
- [ ] App falha com mensagem clara quando `node` nao esta no PATH
- [ ] App falha com mensagem clara quando `node` versao < 18
- [ ] App loga warning quando `git` nao esta no PATH
- [ ] App nao loga nada extra quando tudo esta ok
- [ ] Tests pass: `go test ./cmd/aurelia/ -short`

**Verify:**
```bash
# Testar manualmente: renomear node temporariamente e rodar
go run ./cmd/aurelia
# Deve mostrar: "Node.js is required but not found..."
```

---

### T6: Non-TUI Fallback (Prompt Mode)

**What**: Mostrar checklist em texto simples quando o terminal nao suporta TUI (pipe/redirect).
**Where**: `cmd/aurelia/onboard_helpers.go`
**Depends on**: T2, T4

**Changes**:
1. Adicionar funcao `printDepsCheck(w io.Writer, result deps.CheckResult)`
2. Chamar no inicio de `runOnboardPrompt()` antes dos prompts
3. Se required dep falta, abortar com erro

**Done when**:
- [ ] Checklist aparece em modo prompt (non-TUI)
- [ ] Formato texto simples sem ANSI colors
- [ ] Aborta se required dep falta
- [ ] Tests pass: `go test ./cmd/aurelia/ -short`

**Verify:**
```bash
echo "" | go run ./cmd/aurelia setup
# Deve mostrar checklist em texto simples
```

---

### T7: Validation End-to-End

**What**: Validacao manual end-to-end dos 3 pontos de integracao.
**Where**: N/A (manual)
**Depends on**: T4, T5, T6

**Checklist**:
- [ ] `aurelia setup` (TUI) — Step 1 mostra checklist, Enter avanca
- [ ] `aurelia setup` (piped) — checklist em texto simples
- [ ] `aurelia` (boot normal) — silencioso quando tudo ok
- [ ] Boot com node ausente — erro fatal claro
- [ ] Boot com git ausente — warning, continua rodando
- [ ] Onboarding com node ausente — bloqueado no step 1 com instrucoes
- [ ] Onboarding com git ausente — aviso amarelo, pode continuar
- [ ] Check total < 2s em condicoes normais
