# Goroutine recovery: every background goroutine needs defer recover()

**Date**: 2026-05-20
**Change**: code-review-remediation
**Category**: pattern

## What happened

Durante o code review completo, identificamos 8 goroutines de background sem `recover()` — incluindo `readLoop` do bridge, dispatch de fila do pipeline, callbacks `onDeath`, e proxy de canais. Um único panic em qualquer uma delas mataria o daemon inteiro ou causaria leak de estado. A revisão pós-implementação ainda encontrou 3 goroutines no bridge que a implementação inicial não cobriu.

## How to avoid

Adotar regra de ouro: **toda goroutine lançada por um pacote deve ter `defer recover()` no topo**, sem exceção. Se a goroutine é crítica, o recover deve também fazer cleanup de recursos (fechar canais, limpar maps, nilar ponteiros). Considerar linter customizado ou code review checklist para detectar goroutines sem recover.

## Tags

#lesson #change-code-review-remediation #pattern #concurrency #reliability
