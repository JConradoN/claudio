# Validation hardening must preserve existing workflows

**Date**: 2026-05-18
**Change**: cwd-scope-and-workspace-fix
**Category**: anti-pattern

## What happened

`/cwd` persistence hardening required project markers, which broke the previous workflow of setting a workspace directory as operational cwd.
The valid safety boundary was sensitive-path rejection, not requiring `.git` or language manifests.

## How to avoid

Before tightening validation, check existing user workflows and encode compatibility tests for previously accepted but safe inputs.

## Tags

#lesson #change-cwd-scope-and-workspace-fix #anti-pattern #validation #cwd
