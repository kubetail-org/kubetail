---
title: Read the login shell's answer by markers, from a session of its own
date: 2026-10-17
scope: sidecar
status: Accepted
---

# Read the login shell's answer by markers, from a session of its own

## Context

`internal/run/loginshell` runs the user's login shell once per launch, and again on Refresh PATH,
to read `PATH`, the allowlisted environment and the provider keys. The shell runs the user's
startup files, which Kubetail does not control: they may `cd`, spawn daemons that inherit stdout,
`set -e`, or touch the terminal. This decision predates this file. It was recorded in
`sidecar/CLAUDE.md` alongside the package, and moved here when that file was compacted.

## Decision

`Launch` starts the shell in a **session** of its own (`Setsid`), stdin at `/dev/null`, in the
user's home, and kills the whole session on the way out. The command prints one frame per value —
the shell's cwd first, then one `printenv` per variable, each followed by `|| true` — separated by
a marker, and `parse` reads until the **last marker** the kind prints (N variables, N+2 markers),
never until EOF. The marker begins with a letter and the separator is printed as `\000`. Each
frame is kept byte for byte, less the one trailing newline `printenv` adds. The shell's exit
status matters only when nothing usable was captured. `DefaultTimeout` (ten seconds) bounds the
whole run.

## Alternatives considered

- **A process group instead of a session.** One kill still reaches what the startup files spawned,
  but an interactive shell in a background process group is stopped by `SIGTTIN` the first time it
  touches the controlling terminal, and the answer never comes. A GUI launch inherits no terminal,
  so this only shows when the app runs from one (`pnpm tauri dev`).
- **Read until the pipe closes.** A startup file that backgrounds a daemon leaves it holding
  stdout, which would let it stall every launch until the timeout.
- **A marker starting with a digit, or `\0` as the separator.** `printf` reads `\0` as the start of
  an octal escape, so a marker starting `0`–`7` is swallowed into it and vanishes.
- **Trim or clean each value.** A directory name may hold anything but NUL and the separator,
  newlines included, so anything beyond the one newline `printenv` adds would change a real path.
- **Bare `printenv`.** It exits 1 for an unset variable, which aborts a shell whose startup file
  ran `set -e`. `|| true` is the only syntax beyond the plain command, and sets the floor at fish
  3.0.

## Consequences

The command is built only from the allowlist and the key names: nothing from a kubeconfig, cluster
data or the socket may reach it. An empty frame and an unset variable are the same and install
nothing, except `PATH`, whose absence is `bad output`. Each failure is one fixed reason, logged
without a value.
