---
title: Follow a link to a Kubetail folder once, at start, and refuse one below it
date: 2026-11-24
scope: cross-cutting
status: Accepted
---

# Follow a link to a Kubetail folder once, at start, and refuse one below it

## Context

Kubetail's folders are the fence a sandboxed command must not read. A symlink or a Windows junction
gives a folder a second name, and Kubetail followed links in several places and at several times
([#1145](https://github.com/kstackhq/kstack/issues/1145)):

- A linked subfolder (`chats`, `kube`, `runs` and the rest) was fenced where it led, but only if it
  was a link at start (the ADR this one supersedes).
- A folder inside another by name was fenced twice, since a link could lead the inner one
  elsewhere.
- The folder holding a followed link was not fenced. With `~/.cache/kubetail` linked to
  `/mnt/big/kubetail`, a read-write grant of `~/.cache` was accepted, a sandboxed command could
  repoint the link, and the next start followed it into a folder the command wrote.

## Decision

A link is followed in one place, at one time, and refused everywhere else.

- **A layout folder** (one `layout.Of` names) may be a link. Each program follows its links once,
  at start, checks the result, and only then makes its folders. Nothing after that reads an
  unresolved path. The sidecar's `app.ResolveFolders` does this; the fence holds the resolved
  paths.
- **The Unix runtime folder** must not itself be a link, since its parent can be shared between
  users. The path above it is followed.
- **A subfolder** made below those (`chats`, `kube`, `tmp`, `runs` and the rest) must be a real
  folder, checked where it is made, through an `os.Root`. A link there fails the start, naming
  the layout folder to link instead.
- **No write is granted on or above a link parent**, the folder that holds a followed link: a
  folder grant is refused (`link-parent`), and a run's policy with such a Write rule does not
  compile.
- **The sidecar checks the resolved layout.** Two kinds that are one folder, or one inside
  another where the platform's layout does not put it, fail the start. Folders are compared by
  identity (`realpath.Relation`), never by text.
- **On Windows a link may not lead a layout folder out of the user profile**, whose ACL keeps
  it private ([profile ACL ADR](2026-09-02-windows-cache-files-rely-on-the-profile-acl.md)). A
  junction to another drive fails the start. A folder Windows itself names outside the profile,
  as Folder Redirection does, is the administrator's, and starts.
- A walk that meets more than 255 links fails, so a cycle stops the start.

Refusing a linked subfolder is safe now:
[each folder kind has its own folder on every platform](2026-11-22-each-folder-kind-has-its-own-folder-on-every-platform.md)
records that `desktop/v0.0.1` reached no users, so no installed build has one to break.

The sidecar goes first, since it owns the fence and also runs without a host. The host resolves
its own folders the same way; each program checks only its own writes.

## Alternatives considered

**Keep fencing linked subfolders where they lead.** It leaves a link planted while Kubetail runs
unfenced until the next start, and fences a folder twice when it is nested by name.

**Resolve every path each time it is used.** It is what Kubetail did, in several places; each place
could follow a link to a different target.

**Set an ACL on a Windows folder outside the profile.** The profile-ACL ADR rejected setting one.

## Consequences

A dotfiles manager's linked `~/.config/kubetail`, macOS's `/var`, ostree's `/home` and a junction
inside the profile all start. A linked subfolder, a junction to another drive, and a layout whose
kinds collide do not.

Kubetail assumes every folder on the path to a layout folder, and to a link's target, can be written
only by the user or by root. It checks the runtime folder alone, because its default parent is
shared. A process of the user's own outside the sandbox, or another account that can write a
folder on the path, can still repoint a link.

On macOS a run's socket is under the resolved `/private/var`, eight bytes longer than `/var`, so
the forwarder's socket name is two bytes.
