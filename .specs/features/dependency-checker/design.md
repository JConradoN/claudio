# Dependency Checker — Design

**Spec**: `.specs/features/dependency-checker/spec.md`

---

## Architecture

### New Package: `internal/deps`

Package pequeno e focado. Sem dependencias externas alem da stdlib.

```
internal/deps/
  check.go       — logica de deteccao (LookPath + version parse)
  check_test.go  — testes unitarios
```

### Structs

```go
// DepStatus represents the check result for a single dependency.
type DepStatus struct {
    Name       string // "Node.js", "npm", "git", "gh"
    Command    string // "node", "npm", "git", "gh"
    Required   bool   // true = fatal if missing
    Found      bool   // binary exists in PATH
    Version    string // parsed version string (e.g. "22.14.0")
    MinVersion string // minimum required (e.g. "18.0.0")
    VersionOK  bool   // version >= min (true if no min specified)
    InstallURL string // help URL for installation
}

// CheckResult holds results for all dependencies.
type CheckResult struct {
    Deps    []DepStatus
    AllOK   bool // true when all required deps are found and version-ok
}
```

### Core Function

```go
// CheckAll runs all dependency checks and returns the result.
// Each check runs exec.LookPath + "<cmd> --version" with a 5s timeout.
// Safe to call from any goroutine.
func CheckAll() CheckResult
```

### Version Parsing

- `node --version` → `v22.14.0` → parse `22.14.0`
- `npm --version` → `10.9.2` → parse `10.9.2`
- `git --version` → `git version 2.43.0.windows.1` → parse `2.43.0`
- `gh --version` → `gh version 2.74.0 (2025-01-01)` → parse `2.74.0`

Regex simples: `(\d+\.\d+\.\d+)` — pega o primeiro match de semver.

### Version Comparison

Comparacao simples de 3 segmentos (major.minor.patch). Sem suporte a pre-release ou build metadata — nao eh necessario.

```go
// compareVersions returns -1, 0, or 1.
func compareVersions(a, b string) int
```

---

## Integration Points

### 1. Onboarding TUI (Step 0)

**Arquivo**: `cmd/aurelia/onboard_ui.go`

Novo step `stepDependencies` como primeiro step (antes de `stepLLMProvider`):

```
stepDependencies → stepLLMProvider → stepAnthropicAuthMode → ...
```

- Roda `deps.CheckAll()` uma vez ao entrar no step
- Renderiza checklist com cores (verde = ok, vermelho = faltando, amarelo = opcional faltando)
- Se `AllOK` == true: Enter avanca pro proximo step
- Se `AllOK` == false (required dep missing): Enter desabilitado, mostra instrucoes
- Setas e left/right nao fazem nada nesse step (nao eh menu)

**Contagem de steps**: Muda de "Step X/11" para "Step X/12" (ou ajustar indices).

### 2. Boot Check

**Arquivo**: `cmd/aurelia/app.go`

Chamar antes de `EnsureBridge`:

```go
result := deps.CheckAll()
for _, d := range result.Deps {
    if d.Required && !d.Found {
        log.Fatalf("%s is required but not found. Install: %s", d.Name, d.InstallURL)
    }
    if d.Required && !d.VersionOK {
        log.Fatalf("%s %s found but >= %s required. Update: %s", d.Name, d.Version, d.MinVersion, d.InstallURL)
    }
    if !d.Required && !d.Found {
        log.Printf("Warning: %s not found — %s features may be limited", d.Name, d.Command)
    }
}
```

### 3. Onboarding Prompt (non-TUI fallback)

**Arquivo**: `cmd/aurelia/onboard_helpers.go`

Antes de iniciar os prompts, imprimir o checklist em texto simples:

```
Checking dependencies...
  [ok]  Node.js v22.14.0
  [!!]  git — not found (optional)
```

---

## Rendering

### TUI Checklist

```go
func renderDepsCheck(result deps.CheckResult) string {
    var b strings.Builder
    b.WriteString("Dependencies\n\n")
    for _, d := range result.Deps {
        switch {
        case d.Found && d.VersionOK:
            // green checkmark
            fmt.Fprintf(&b, "  %s %s v%s\n", colorize("[ok]", colorGreen), d.Name, d.Version)
        case d.Found && !d.VersionOK:
            // red version warning
            fmt.Fprintf(&b, "  %s %s v%s (requires >= %s)\n", colorize("[!!]", colorRed), d.Name, d.Version, d.MinVersion)
        case !d.Found && d.Required:
            // red missing
            fmt.Fprintf(&b, "  %s %s — not found\n", colorize("[!!]", colorRed), d.Name)
            fmt.Fprintf(&b, "        Install: %s\n", d.InstallURL)
        case !d.Found && !d.Required:
            // yellow optional missing
            fmt.Fprintf(&b, "  %s %s — not found (optional)\n", colorize("[--]", colorYellow), d.Name)
        }
    }
    return b.String()
}
```

### Colors

Adicionar `colorGreen` e `colorYellow` ao `onboard_helpers.go`:

```go
const (
    colorBlue   = "\x1b[94m"
    colorGreen  = "\x1b[92m"
    colorYellow = "\x1b[93m"
    colorRed    = "\x1b[91m"
    colorReset  = "\x1b[0m"
)
```

---

## Error Messages

| Cenario | Mensagem |
|---|---|
| Node nao encontrado | `Node.js is required but not found. Install from https://nodejs.org/` |
| Node versao baixa | `Node.js v16.0.0 found but v18.0.0+ is required. Update from https://nodejs.org/` |
| npm nao encontrado | `npm is required but not found. It usually comes with Node.js — reinstall from https://nodejs.org/` |
| git nao encontrado (boot) | `Warning: git not found — orchestrator features will be limited` |
| gh nao encontrado (boot) | Silencioso (muito opcional) |
