# Redact before truncate, never after

**Date**: 2026-05-20
**Change**: code-review-remediation
**Category**: pattern

## What happened

`startRunLog` fazia `redactSecrets(truncatePrompt(prompt))` — truncava o prompt primeiro e depois redigia secrets. Se uma API key estava posicionada após o limite de truncamento (500 bytes), ela era cortada ao meio e o regex de redaction não detectava o padrão incompleto, resultando em vazamento parcial de credenciais para o SQLite de runlog. O fix foi inverter a ordem: `truncatePrompt(redactSecrets(prompt))`.

## How to avoid

Sempre aplicar sanitização (redaction, escaping, validation) **antes** de qualquer operação que modifique o tamanho/estrutura do dado (truncamento, splitting, slicing). Isso vale para credenciais, PII, e qualquer dado sensível que precise ser persistido ou logado.

## Tags

#lesson #change-code-review-remediation #pattern #security #redaction
