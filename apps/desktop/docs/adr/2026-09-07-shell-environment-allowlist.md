---
title: Import the login shell's environment through an allowlist, and leave a `~` verbatim
date: 2026-09-07
scope: sidecar
status: Accepted
---

# Import the login shell's environment through an allowlist, and leave a `~` verbatim

## Context

A macOS GUI launch inherits launchd's minimal environment, not the one the user's shell builds.
`internal/loginshell` already ran the login shell to recover `PATH`, without which a kubeconfig
`exec` credential plugin is not found at all. The rest of the environment was still missing, and
two things need it: a `KUBECONFIG` holding a `:`-joined list of files, and the variables a
credential plugin reads to choose an identity (`AWS_PROFILE`, `CLOUDSDK_CONFIG`, and their
neighbours). Finding `aws-iam-authenticator` and then running it against the wrong account is the
same bug one layer up.

We spawn credential plugins as children, so whatever we import becomes their environment too.

## Decision

**A compile-time allowlist, not the whole environment.** `imported` in
`sidecar/internal/loginshell/loginshell.go` names every variable we carry and its kind — `plain`,
`path`, or `pathList` — and the shell command is built from that list. No name is read from a
kubeconfig, cluster data, or the socket. Adding a row is a security change.

**A `path` or `pathList` value is resolved against the shell's own cwd, and a leading `~` is left
verbatim.** A relative entry is meaningless outside the shell that reported it, so it is prefixed
with the directory that shell ended in — which the shell reports itself, in the frame before the
variables, because a startup file may `cd` after we start it. Prefixed, not `filepath.Join`d: Join
cleans `link/..` away lexically, which names a different file than the kernel resolves when `link`
is a symlink. An empty entry is dropped. A leading `~` is answered before either rule, because
`filepath.IsAbs` calls it relative.

## Alternatives considered

**Import the whole environment.** The obvious shortcut, and the reason the list exists.
`DYLD_INSERT_LIBRARIES` and `LD_PRELOAD` would turn whatever reached the user's login environment
into code execution inside every plugin we run; `KUBERNETES_SERVICE_HOST` and
`KUBERNETES_SERVICE_PORT` would send client-go down the in-cluster path on a desktop; and a startup
file would be able to overwrite the environment the Tauri host deliberately gave the sidecar. A
blanket import cannot tell any of that apart from a user's setting.

**Prefix wildcards (`AWS_*`, `CLOUDSDK_*`).** Cheaper to maintain and it imports names nobody has
weighed, including ones a future SDK release invents. The list is short enough that a row per
variable, with its reason, costs less than the exposure.

**A user-editable list.** The list is the security boundary; a file that widens it is a file an
attacker widens it with.

**Carry credentials themselves** — `AWS_ACCESS_KEY_ID` and family, `AWS_ROLE_ARN` /
`AWS_WEB_IDENTITY_TOKEN_FILE`. We carry what *selects* an identity, not what *is* one: a secret
imported here would sit in our process environment, and every child's, for the life of the app.

**Expand a leading `~`.** It reaches us only from a quoted `export KUBECONFIG="~/..."`, where the
quotes stopped the shell expanding it — so it is a literal, and the user's own `kubectl` reads it
as one and fails. Expanding it would make the same config file work in the app and fail in the
terminal, and a user with no way to explain that difference is worse off than a user with one
broken line in one file. The invariant we keep instead: we never invent a path the shell did not
produce — which is also why a rebased entry keeps every component it arrived with.

## Consequences

The list is a maintenance obligation with a security character. A request for a variable we do not
carry is a review, with its reason recorded beside the row, not a configuration change.

Everything imported is process-wide, so it reaches every child — `internal/services/auth`'s browser opener
resolves `open` against the same PATH. That is what makes the deny-by-default posture load-bearing
rather than tidy.

Two things the shell reports are still not answered: a value it never exported (kubie's per-terminal
`KUBECONFIG` lives in that shell's process and no GUI process can read it), and a change made after
launch (the import runs once, so editing `.zshrc` takes effect on the next start).

## Revisit when

A credential plugin needs a variable that is itself a secret. The rule above says no; a plugin that
cannot work any other way is the case that would reopen it.
