# Model Refresh Error Resolution (2026-05-22)

## Problem

The bot's `/refresh` command was silently failing when the Telegram button was edited after the message was deleted or timing issues occurred.

## Solution

### 1. Cache Preservation

**Before:**
```go
models, err := bc.getModels(ctx, true)
...
bc.invalidateModelCache()
return bc.sendProviderMenu(c, true)
```

**After:**
```go
models, err := bc.getModels(ctx, true)
...
return bc.sendProviderMenu(c, true)  // Uses the cache populated above
```

### 2. Visible Errors

Changed from `c.Edit()` to `SendTextWithThread()` for all errors:
- Permission denied
- Bridge unavailable
- No providers available

## Files Modified

- `internal/telegram/bot_middleware.go:427-446`
- `internal/telegram/bot_middleware.go:409-413`

## Test Results

```bash
go test ./internal/telegram -run "TestRefreshModelsFromCallback_RedrawsProvidersWithNewModel"
--- PASS: TestRefreshModelsFromCallback_RedrawsProvidersWithNewModel (0.18s)
```
