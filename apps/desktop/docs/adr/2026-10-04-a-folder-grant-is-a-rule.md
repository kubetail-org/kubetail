---
title: A folder grant is a rule, and the file tools walk it by handle
date: 2026-10-04
scope: cross-cutting
status: Accepted
amended_by: [A folder grant is its own row, and a mode and a rule belong to a cluster](2026-11-24-a-folder-grant-is-its-own-row-and-a-mode-and-a-rule-belong-to-a-cluster.md)
---

# A folder grant is a rule, and the file tools walk it by handle

## Context

A sandboxed command read the system, the toolchain folders and its workspace, and nothing else
of the user's. A tool that keeps its data under the home, or a repository the user wanted the
agent to read, was out of reach until the user switched the chat outside the sandbox, after
which every command asks. The user needed to grant a folder — read, or read and write, for one
chat or always — without lowering the floor the sandbox keeps: the denied-always list and
Kubetail's directories, which no rule opens. `Read`, `Write` and `Edit` reach the workspace
unasked through a root on it; a granted folder had no such route.

## Decision

A folder grant is a `permissions.Rule` of class 1 (read) or 2 (read-write) naming a `Folder`. A
chat's is a `chat_grants` row; an always one is in `securityconfig`'s `Rules`, beside the cluster
rules. It matches no action, so it never reaches a verdict.

A grant names its folder by its resolved path: a link on it, or on its way, is refused with its
target. A grant is checked when written and every time it is read (`securityconfig.CheckFolder`
and `CheckStoredFolder`): not `/`, not on a mount every run has its own of, not in a never-readable path
or Kubetail's directories, not exactly a closed folder, and, read-write, not the home, nor on, under
or over anything Kubetail knows runs outside the sandbox — a `PATH` entry, a system or toolchain
Read path, `sandbox.NoWrite`. `chat.foldersFor` is the one builder of a session's folders and
answers only those that pass, and none where no sandbox confines the run. The bash tool checks
each again at the run's start and adds it to the Workspace policy as a Read or Write rule, the
wider grant winning where two nest.

`Read`, `Write` and `Edit` skip the request for a path in a granted folder (`Write` and `Edit`
in a read-write one) that the sandbox does not keep shut there, and carry the folder from the
gate to the run (`tools.Approval.Folder`, `tools.ApprovedRunner`). The run reaches the path through
`fileguard.Walk`: from `/` by handle, each folder opened without following a link and checked
against the closed paths, a link inside the grant read and walked again from the grant.

## Alternatives considered

- **Ask for every write outside the workspace, as before.** A sandboxed command can already write
  a read-write grant unasked, so a `Write` asking would gate a change the shell does not.
- **Check a grant only when it is written.** A folder can move, a command with a read-write grant
  above it can replace it with a link, and `security.json` can be edited by hand.
- **Store the resolved path while showing what was typed.** A planted link would then turn
  `~/code/svc` into the home with the user reading `~/code/svc`.
- **Refuse every link under a granted folder.** Simpler, and it breaks ordinary repositories,
  whose `node_modules/.bin` and tool shims are links.
- **A second list of folders, in `host.json`.** Every always rule already lives in one store that
  holds a file Kubetail cannot read; a second list would need its own hold.
- **Refuse nested grants when written.** A chat's grant and an always one are written in
  different places, and neither writer sees the other's; the wider grant is what the user granted.

## Consequences

The user can open a folder to the agent in their own terms, and the denied-always list and
Kubetail's directories stay closed inside it, pinned over a real grant on both platforms. Every
read of the grants costs a `stat` and an `EvalSymlinks` per grant, bounded by `SyncTimeout`. The
file tools carry a second route, the walk, beside the workspace's root and the path form.
`NoWrite` is curated as the denied-always list is: what it misses is a residual. The home is
read-only. Kubetail's log directory is one of its directories, so a grant of the home does not
open it.

## Revisit when

A sandbox gains a way to deny by inode rather than by path, or a tool needs a grant of a single
file.
