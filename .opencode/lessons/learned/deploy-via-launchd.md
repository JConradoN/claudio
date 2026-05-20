# Deploy: use `make deploy` via launchd, never manual kill+nohup

**Date**: 2026-05-20
**Change**: deploy-automation-post-commit
**Category**: process

## What happened

The coder agent ran `kill + sleep 2 + nohup` to restart the daemon after a
commit. This failed silently because (1) the coder runs inside the daemon it's
trying to kill, creating a race condition, and (2) the startup guard
("outra instância já está rodando") blocked the new process while the old was
still alive. Result: daemon never restarted after deploy.

## How to avoid

Always use `make deploy` (atomic build via `make install` + `launchctl
kickstart -k`). This delegates restart lifecycle to launchd, which handles
SIGTERM gracefully and respawns via KeepAlive. Even better, use the
post-commit git hook (`.git/hooks/post-commit`) so deploy happens
automatically outside the coder's process context.

## Tags

#lesson #change-deploy-automation-post-commit #process #deploy #launchd
