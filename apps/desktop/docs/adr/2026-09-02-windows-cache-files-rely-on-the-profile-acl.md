---
title: Let the profile ACL protect the cache files on Windows
date: 2026-09-02
scope: cross-cutting
status: Accepted
---

# Let the profile ACL protect the cache files on Windows

## Context

The sidecar's SQLite files hold full object bodies for every synced kind — the cluster mirror,
one of the app's named assets. On Unix they were landing at 0644 inside a 0755 directory: any
other account on the machine could read them. Three changes closed that — a 0o077 process umask
in `sidecar/umask_unix.go`, a chmod of the database and its `-wal`/`-shm` siblings in
`sqlitepool.OpenWriter`, and a 0700 data directory from `ensure_data_dir` in
`src-tauri/src/paths.rs`.

None of the three has a Windows equivalent. There is no umask; Go's `os.Chmod` on Windows only
toggles the read-only attribute and cannot express "owner only"; and the Rust directory mode is
`#[cfg(unix)]`. So the question the Unix work raised: does Windows need its own mechanism?

## Decision

No. On Windows the data directory lives under `%LOCALAPPDATA%`, inside the user profile, whose
default DACL grants the user, `SYSTEM` and `Administrators` — and new files inherit it. We rely on
that inheritance and set no ACL of our own. `umask_windows.go` is an empty body, and
`ensure_data_dir` falls through to a plain `create_dir_all`.

## Alternatives considered

**Set a protected DACL on the data directory.** `D:P(A;;GA;;;OW)` with inheritance flags, the way
`ipc/listen_windows.go` already restricts the named pipe. It would state the policy rather than
inherit it, and drop `Administrators` from the ACE list. Rejected because it moves no boundary:
another standard user is already excluded, and an administrator can take ownership and rewrite any
DACL we set — the same reason 0600 does not stop root. The cost is a platform-specific code path
and a test only a Windows runner can execute.

**Encrypt the files (EFS, or an app-level key).** Answers a different threat — a stolen disk — and
the answer there is full-disk encryption, which is on by default on current Windows hardware.
Whether the cache deserves credential-grade storage is
[a separate question](2026-09-02-the-cache-is-ordinary-application-data.md), not this one.

## Consequences

The Windows protection is a platform default, not something we assert, so no test pins it and
nothing fails if a future change moves the data directory somewhere with a laxer ACL. That is the
obligation this creates: the guarantee is "inside the user profile", so keep it there.

`docs/security-model.md` counts this row as Enforced on the strength of the Unix tests. The
Windows half rests on this ADR.

## Revisit when

The data directory moves outside the user profile, the app runs somewhere the profile ACL does not
apply (a service account, a shared or roaming profile), or the cache becomes credential-bearing
storage — which would raise the bar past what any DACL answers.
