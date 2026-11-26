---
title: Run the login shell in the sandbox
date: 2026-10-04
scope: sidecar
status: Accepted
---

# Run the login shell in the sandbox

## Context

Kubetail runs the user's login shell itself three times: the `PATH` resolution at launch, Refresh
PATH, and the shell snapshot that commands outside the sandbox source. Each run executes the
user's startup files, and each ran with full access: Kubetail's own data, the sidecar's socket and
the network were all in reach of a startup file changed to do harm. The sandbox that confines a
model's command already existed, with a `Policy` that can close Kubetail's directories and the
denied-always list. The three runs differ in where their output goes: the resolution's `PATH`
reaches every sandboxed run and, on macOS, its imported variables reach the sidecar's own
process; the snapshot reaches only commands run outside the sandbox.

## Decision

All three run through `loginshell.In`, which builds one policy: everything readable but
Kubetail's data, cache and runtime directories, no network, and one write, a `TMPDIR` of the
run's own under `<cache>/tmp/`, where a sandboxed command's is, removed when the shell exits. **The resolution and the
refresh also shut the denied-always list** (`sb.Never(home)`), since a credential their shell
read could leave the sandbox in what it answers. **The snapshot's shell reads the whole home**:
its answer reaches only commands that read the home themselves, and hiding `~/.ssh/config` or
`~/.kube/config` would change what it builds.

**A start the sandbox refuses fails closed**: the run reports a `Fault` and the shell never runs
unconfined on a machine whose sandbox answered the probe. On macOS that loses the environment
import with the `PATH`, and the fallback applies. The shell's environment drops every variable
the sandbox refuses to pass, `SSH_AUTH_SOCK` included. `app.New` probes the sandbox once, runs the
launch resolution in it before anything that reads the environment, and on Linux with no sandbox
skips the resolution, which nothing reads there. `main` only says whether to run the login shell
at all (`Config.RunLoginShell`), so no test spawns a developer's real one.

Two residuals are accepted. A command outside the sandbox still runs the files every `-c` shell
reads (`/etc/zshenv`, `~/.zshenv`, `BASH_ENV`) unconfined, and what the login shell outputs —
the snapshot's functions and `PATH`, and on macOS the imported variables — still shapes what
runs outside the sandbox, so a startup file can postpone its harm there. On macOS the import can
also carry a secret read from a file the denied-always list does not name.

## Alternatives considered

- **Shut the denied-always list for the snapshot too.** One policy for all three runs is
  simpler, but it would change what the snapshot saves for the profiles that read `~/.ssh/config`
  for a completion or `~/.kube/config` for a prompt, and it protects nothing: the commands that
  source the snapshot read those files themselves.
- **Fall back to running the shell unconfined when the sandbox refuses the policy.** That keeps
  the macOS import working in a case that should not occur, at the price of a silent full-access
  run exactly when something is wrong. On a machine whose sandbox answered the probe, a refused
  policy is a bug, and the fallback `PATH` is the safe answer.
- **Keep `SSH_AUTH_SOCK` in the shell's environment.** It is copied so a startup file does not
  start an agent that outlives the kill. Inside the sandbox such an agent cannot start or
  outlive the run, and the sandbox refuses the variable anyway.

## Consequences

A startup file cannot read Kubetail's files, reach the sidecar's socket, write outside its scratch
or use the network while Kubetail runs it. Startup files that write the home (`~/.zcompdump`,
history) fail quietly; fish complains and goes on, and a nushell that cannot write its state may
exit, which the resolution reads as a `Fault`. A tool that caches under `TMPDIR` starts cold.

`In` is the one place that builds the login shell's policy; a fourth caller of the login shell
must go through it. The probe and the shell import now sit at the top of `app.New`, so the order
there is load-bearing: nothing above them may read the environment. The probe runs under the
startup context, the shutdown signal's. `sandbox.Probe` answers that context's error when it ends
first, never a verdict of no sandbox, and `New` stops on it.

## Revisit when

A command outside the sandbox stops being `<shell> -c` with the process environment, or the
macOS import stops feeding the sidecar's own process — either moves a residual.
