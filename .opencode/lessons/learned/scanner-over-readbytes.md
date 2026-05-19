# NDJSON: Use bufio.Scanner instead of ReadBytes for size-limited reads

**Date**: 2026-05-19
**Change**: 3-sprint-remediation
**Category**: pattern

## What happened

The `maxNDJSONLineSize` check was first implemented using `bufio.Reader.ReadBytes` with a post-allocation length check. This allowed oversized lines to allocate memory before the check fired, creating a partial OOM mitigation. The security reviewer flagged this as HIGH risk.

## How to avoid

Use `bufio.Scanner` with `scanner.Buffer(buf, maxTokenSize)` for any stream reading where token size must be bounded. `Scanner` returns `ErrTooLong` without allocating beyond the buffer when a token exceeds the limit. Reserve `ReadBytes` for cases where the delimiter is guaranteed to appear within reasonable bounds.

## Tags

#lesson #change-3-sprint-remediation #pattern #security #ndjson
