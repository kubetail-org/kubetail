---
title: The sandbox is the gate for a sandboxed command
date: 2026-09-28
scope: cross-cutting
status: Accepted
amended_by: [Leaving the sandbox is the user's switch for a chat, never the model's flag](2026-09-30-leaving-the-sandbox-is-the-users-switch-for-a-chat.md)
---

# The sandbox is the gate for a sandboxed command

## Context

Every Bash command waited on the user, who read it and pressed Approve
([the bash tool](../security/2026-09-18-bash-tool.md)). There was no allowlist and no read exempt,
since a command the user did not read was a command the model ran. That serves a chat badly — a
question about the cluster is a string of reads, each put to the user — and a monitoring agent,
which has no user to ask, not at all.

The sandboxed Bash work built a sandbox that confines a
command on macOS (Seatbelt) and Linux (bubblewrap): no network, no credential, no file outside the
workspace, and the chat's cluster read-only through a proxy that redacts Secret values. Every call
still asked. The decisions below were settled on 2026-09-27.

## Decision

**A sandboxed command runs without asking.** `bash.Tool.Approval` skips a call whose sandbox
`Confines`; a call with `dangerouslyDisableSandbox`, and every call on a machine with no sandbox,
asks as before. For a sandboxed command the bound is what the process can reach, not what the
command says. A change to the cluster comes back `Forbidden` from the proxy. Putting each cluster
write to the user is [a decision of its own](2026-09-29-a-sandboxed-command-asks-for-each-cluster-write.md).

**The home is unreadable in the sandbox, except the trees holding `PATH` entries.** A tool that
needs its own config under the home fails in the sandbox and runs outside.

**Cluster reads leave the machine on the model's word.** Everything the user's credentials can
read, less Secret values, can reach the provider with no per-call approval.

**`Write` and `Edit` in the chat's workspace ask no one, with a sandbox or without one.** The
workspace is Kubetail's own directory, made for the chat, and the gate skips on the same test by name
(`fileguard.Under`) that makes `Fence.File` open the workspace's root, so the write cannot leave
it. What an unasked write put there is drawn open in the transcript.

## Alternatives considered

**An allowlist of read-only commands.** `kubectl get` looks safe until a plugin, an alias or a
`--kubeconfig` points it elsewhere, and a list that lets a tool through lets every subcommand
through. The sandbox bounds what a command can reach whatever it is called.

**Keep asking for every sandboxed command, and show that it is sandboxed.** The user would approve
a stream of reads they cannot judge better than the sandbox does, and a monitoring agent could not
run at all.

**Read the whole home but a denylist**, as Claude Code's sandbox does. A denylist is only as good
as the credential paths someone thought of; an allowlist of `PATH` trees fails closed.

**Ask for a write in the workspace where there is no sandbox.** The workspace is Kubetail's own and
the root keeps a write inside it. A command outside the sandbox that later names the file is the
risk, and drawing the write open answers it better than asking twice.

## Consequences

- An instruction in cluster text can make a command run, not only ask for one. It chooses which
  cluster data reaches the provider, which already has the chat.
- `Confines` is load-bearing: a sandbox that reports it wrongly runs a command unasked and
  unconfined. Each platform's test pins it.
- A command outside the sandbox that names a workspace file names text the user may not have seen,
  and one that reads config from its working directory can run what a sandboxed command or an
  unasked write planted there. Both are accepted residuals in
  [the security record](../security/2026-09-28-bash-runs-in-a-sandbox.md), and closing the second
  is in `TODO.md`.

## Revisit when

A tool the user needs routinely cannot run in the sandbox for want of its config under the home, or
a sandbox escape shows the bound does not hold.
