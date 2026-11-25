# Security record — no run reads another process's launch strings, 13 October 2026

**Subject:** on macOS a sandboxed command can no longer read the arguments or environment of a
process outside its run through `kern.procargs2`. The living model is
[security-model.md](../security-model.md); the decision is
[a macOS run leads its own session](../adr/2026-10-13-a-macos-run-leads-its-own-session-so-no-run-reads-another-processs-launch-strings.md).

## The problem

macOS has no PID namespace, and `kern.procargs2` answered a sandboxed run with any of the user's
processes' arguments, and before macOS 27 their environment, where credentials ride: a token
exported in a terminal, or a provider key the sidecar was launched with. A command the sandbox
confines runs unasked, and what it prints reaches the model.

## What changed

- **The profile denies process information outside the run and `kern.procargs*`.** Together, and
  only together, the two refuse the read for a process in another process group.
- **`Sandbox.Command` starts every run in a session of its own** (`Setsid`), so every process
  outside the run is in another group. The profile refuses `setsid` and `setpgid`, so the run
  cannot move back.

## Why this is safe

- **`TestAnotherProcessesLaunchStringsAreRefused`** reads a process outside the run, through
  the run's own `Command`, and finds neither its arguments nor its environment, on every macOS
  CI runs. Its target is not an Apple binary, whose environment macOS 27 withholds anyway.
- **The session is the sandbox's, not the caller's**, so a run made by any caller gets it.

## What stays

- **A process the run itself started is readable**, being inside the run.
- **The guarantee rests on the session**: a caller that replaces `Command`'s `SysProcAttr`
  without `Setsid` loses it. The Bash tool and the login shell both keep it.
- **A command outside the sandbox** runs as the user, unconfined, and reads what any of the
  user's processes can.
