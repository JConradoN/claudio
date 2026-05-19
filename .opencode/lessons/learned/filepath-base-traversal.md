# filepath.Base is not enough for path traversal defense

**Date**: 2026-05-20
**Change**: code-review-remediation
**Category**: anti-pattern

## What happened

`downloadTelegramFile` usava `filepath.Join(os.TempDir(), filepath.Base(filename))` como defesa contra path traversal. Porém, `filepath.Base("..")` retorna `".."`, e `filepath.Join("/tmp", "..")` resolve para `"/"`. Isso permitia que um atacante com controle do `filename` (via Telegram) escrevesse arquivos fora do diretório temporário. O fix foi usar `os.CreateTemp` (SO gera o nome) + rejeição explícita de `.`/`..`.

## How to avoid

Nunca confiar apenas em `filepath.Base` para sanitização de paths quando o input vem de fonte não confiável. Preferir sempre `os.CreateTemp`/`os.MkdirTemp` para arquivos temporários. Se o nome original precisa ser preservado, armazená-lo como metadados, nunca como componente do path no filesystem.

## Tags

#lesson #change-code-review-remediation #anti-pattern #security #path-traversal
