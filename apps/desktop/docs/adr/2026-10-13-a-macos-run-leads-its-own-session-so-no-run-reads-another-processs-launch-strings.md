---
title: A macOS run leads its own session, so no run reads another process's launch strings
date: 2026-10-13
scope: sidecar
status: Accepted
---

# A macOS run leads its own session, so no run reads another process's launch strings

Supersedes Accept that a sandboxed command on macOS reads other processes' arguments.

## Context

On macOS there is no PID namespace, and `kern.procargs2`, which `ps` reads, answers with a
process's arguments and, before macOS 27, its environment. The earlier ADR accepted the arguments
and left the environment an open gap, because no Seatbelt rule we tried refused the read.

Those trials had the reader and its target in one process group. The profile needs two rules,
and both together refuse the read only for a target in **another process group**:

```
(deny process-info* (target others))
(allow process-info* (target same-sandbox))
(deny sysctl-read (sysctl-name-prefix "kern.procargs"))
```

Neither rule refuses anything alone, and `(deny default)` does not stand in for the first. A
target in the reader's own group stays readable, arguments and environment, on every macOS from
15 to 27.0.1. A new group alone is not enough when the run can call `setpgid` back into its
parent's group; a session leader cannot, since the kernel refuses the move. This was measured on
GitHub's macOS runners, 15.7.9 through 27.0.1, Apple silicon and Intel
([amorey/seatbelt-procargs-probe](https://github.com/amorey/seatbelt-procargs-probe),
[anthropics/sandbox-runtime#160](https://github.com/anthropics/sandbox-runtime/issues/160)).

## Decision

**`Sandbox.Command` on macOS starts every run in a session of its own** (`Setsid`), and the
profile carries both rules. The profile already refuses `setsid` and `setpgid`
([a macOS run keeps its group](2026-09-28-a-macos-run-keeps-its-group-by-refusing-setsid.md)),
so the run stays in its group and every process outside it is in another.
`TestAnotherProcessesLaunchStringsAreRefused` pins the read refused, arguments and environment,
on every version CI runs, against a target that is not an Apple binary: with SIP on, macOS 27
withholds an Apple binary's environment from every reader, sandboxed or not.

The session is `Command`'s, not each caller's, so the protection holds for every run the sandbox
makes. The Bash tool and the login shell already started their runs in a session of their own.

## Alternatives considered

- **Leave the session to each caller.** Both callers set it today, but a third that did not
  would read every process's launch strings with no test failing.
- **A new process group, not a session.** It works here, since the profile refuses `setpgid`, but
  it rests on that one rule; a session also leaves the run no controlling terminal.

## Consequences

- A sandboxed command reads neither the arguments nor the environment of any process outside its
  run, on every supported macOS. The security model's row is **Enforced**.
- The provider keys a sidecar was launched with are out of a run's reach, so re-execing the
  sidecar to drop them from its exec-time environment is no longer needed for the sandbox.
- The guarantee rests on the session: a caller that replaces `Command`'s `SysProcAttr` must keep
  `Setsid`, which `Command`'s comment says.

## Revisit when

A macOS release answers `kern.procargs2` across process groups under these rules, which
`TestAnotherProcessesLaunchStringsAreRefused` would show.
