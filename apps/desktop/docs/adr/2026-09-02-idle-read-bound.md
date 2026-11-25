---
title: Every non-watch request carries an idle-read bound
date: 2026-09-02
scope: sidecar
status: Accepted
---

# Every non-watch request carries an idle-read bound

## Context

A kind sync holds a supervisor start slot until its watch is open, which is past the cold list
(→ [jobs and workers](2026-08-28-jobs-and-workers.md)). A LIST that wedges therefore costs a
permanent fraction of the fleet's start capacity. HTTP/2's `READ_IDLE_TIMEOUT` is connection-level
keepalive: it detects a dead peer, not a live one that has stopped sending, which is what a wedged
LIST is. A plain request deadline is wrong the other way: a slow but streaming LIST of a large
collection would be killed while making progress.

## Decision

`idletimeout.go` installs a per-request idle-read bound on the connection's config, beside the
QPS/burst tuning.

- **Progress, never a deadline.** Headers and every body chunk count, so a streaming LIST always
  completes. The watchdog ticks once per window, so idle-to-cancel lands in `[timeout, 2*timeout]`.
  The timer re-arms only from inside its own callback, never from the read path, so a read landing
  as the timer fires cannot race the cancel.
- **Watches are exempt**, matched by `watch=true` as a substring of `RawQuery`. A healthy watch is
  legitimately silent between bookmarks; `RetryWatcher` and the HTTP/2 keepalive govern one.
- **A cancelled request reports `ErrIdleTimeout`**, not the transport's bare `context canceled`,
  because that string is what a stalled cold list ends its run with, as the `SyncFailed` message a
  user reads. The caller's own cancel still reports itself.

## Alternatives considered

- **Per-request deadline.** Kills large LISTs that are progressing.
- **Rely on HTTP/2 keepalive.** Does not fire for a live peer that has gone quiet mid-response.
- **A bound on watches too.** Kills healthy quiet watches.

## Consequences

A wedged LIST releases its start slot within two windows. Anything that adds a long-poll request
other than a watch must exempt it the same way, or it will be cancelled for being quiet.
