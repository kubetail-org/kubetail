---
title: Offer no sandbox on native Windows; WSL2 runs the Linux one
date: 2026-09-28
scope: sidecar
status: Accepted
---

# Offer no sandbox on native Windows; WSL2 runs the Linux one

## Context

On macOS and Linux a Bash call runs in an OS sandbox, and a sandboxed call asks no one
([the sandbox is the gate](2026-09-28-the-sandbox-is-the-gate-for-a-sandboxed-command.md)). The
sandboxed-Bash sequence planned Windows as three more steps, after `sandbox-runtime`'s alpha
Windows design: a local `kubetail-sandbox` account the command runs as, a machine-wide WFP filter
set keyed on its SID, NTFS ACEs Kubetail writes and records, a two-hop runner under a restricted
token, and an elevated setup from the Settings dialog.

Reviewing the first of those steps turned up costs the design had not priced:

- **Setup changes the machine** and needs an administrator: an account, a group, persistent
  filters, ACEs on the user's files, and an uninstall that must find and remove all of it.
- **The account is shared by every user of the machine.** Anyone who can start a process as it
  reaches every ACE Kubetail granted it, for every user: their workspaces, and a live run's
  kubeconfig holding its proxy token. `sandbox-runtime` accepts this as rare.
- **Group membership reaches a token only at sign-in**, so gating the password on a group of real
  users leaves the installing user unable to use the sandbox until they sign out.
- **DNS leaves the machine**: the system resolver is a service of its own, which the filters cannot
  attribute, so a sandboxed command can spell data into the names it resolves.
- **The stock ACLs reach further than the profile**: `Users` and `Authenticated Users` can write
  `%ProgramData%`, `%PUBLIC%` and folders created off a drive root, which the account inherits.
- **We would own a WFP binding** over `fwpuclnt.dll` with hand-laid structs, and its tests change
  the machine they run on.

Claude Code, whose sandbox the design follows, does not sandbox on native Windows. Its
documentation sends a Windows user to WSL2, where its Linux sandbox runs.

## Decision

Native Windows has no sandbox. `sandbox_windows.go`'s `Probe` answers none, with the reason
*no sandbox on native Windows; run Kubetail in WSL2*, and `Command` fails rather than run anything
in the sandbox's name. Bash is offered there without `dangerouslyDisableSandbox`, and every call
asks, as it did before the sandbox existed. `Write` and `Edit` in the workspace still ask no one,
since that rests on the workspace's root, not on a sandbox.

A Windows user who wants the sandbox runs Kubetail's Linux build in WSL2. There it is Linux:
bubblewrap in a user namespace, and the seccomp filter, which admits only IP sockets, refuses the
vsock and Unix sockets WSL's interop reaches Windows through, so a Windows program started from
inside the sandbox cannot start. No test runs there yet; `docs/TODO.md` carries the check by hand.
WSL1 has no user namespaces, so its probe fails and every call asks.

The planned Windows sandbox work (a restricted account and filters, a Windows runner, and setup
in the app) is dropped. This replaces the forecast in
[a sandboxed forwarder holds a loopback port on macOS](2026-09-28-a-sandboxed-forwarder-holds-a-loopback-port-on-macos.md)
that Windows gets the same door: the loopback port stays macOS's alone.

## Alternatives considered

- **Build `sandbox-runtime`'s design in Go, as planned.** Everything under *Context*, and it would
  still carry a DNS residual that decides whether a sandboxed call on Windows may skip the user.
- **Ship `srt-win.exe`.** Its source is now in `sandbox-runtime`'s repository, but it is Rust, and
  Kubetail's release would sign a binary whose security it does not own. It carries the same shared
  account and the same DNS residual.
- **An AppContainer for the command.** A process it starts out of band (Task Scheduler, BITS,
  out-of-process COM) runs as the user, outside the container and outside any filter keyed on it.
- **Run each command in WSL2 from the native app.** The workspace would sit on `/mnt/c`, the run's
  Unix socket cannot cross from Windows into the WSL2 VM, and the command would run a different
  shell against a different filesystem from the one an approved command sees.

## Consequences

On native Windows every Bash call waits on the user, and a Windows user gets unasked reads only by
running the app in WSL2. Nothing changes the machine, and no uninstall has anything to find.

The sandbox's code stays two implementations. `forward_windows.go`, `sandbox_windows.go` and their
tests are the whole of Windows' part, and they must keep starting nothing.

WSL2 has no CI job: GitHub's hosted Windows runners do not run it. It is checked by hand, per
`docs/TODO.md`.

## Revisit when

Claude Code or `sandbox-runtime` ships a native Windows sandbox out of alpha, or Windows gains a
per-process network and file fence that needs no machine-wide account.
