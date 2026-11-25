---
title: Paths are compared as files
date: 2026-11-22
scope: sidecar
status: Accepted
---

# Paths are compared as files

## Context

The sandbox decides by comparing paths: `Policy.check` refuses a rule beneath a Write rule,
`Policy.rules` drops a Deny no Read reaches, Settings compares a folder grant with the zones, and
the file tools compare a path with what a grant hides. Compared by text, each is sound only when
one folder has one name. A link gives it a second, and on a volume that folds names so do another
case and another composition of an accent. Following links in user space
(`filepath.EvalSymlinks`) keeps the link's own spelling of its target, so it does not settle the
name.

macOS can answer a path's stored name in one lookup. Linux cannot: nothing returns the spelling a
folder stores but its listing.

The sandboxes still take text. Seatbelt checks a file on the path the kernel holds for it, and
bwrap builds a mount tree from the paths it is given.

## Decision

**Two paths are compared as files.** `sandbox.Under` and `sandbox.Same`
(`sidecar/internal/run/sandbox/paths.go`) follow the links of both paths, then look for the file
one names among the folders on the other (`os.SameFile`), so a folder is one folder however it is
spelled. Names past the deepest part that exists are compared as given. They take paths as
written, and every comparison outside one policy goes through them: Settings' grant check, the
`PATH` filter, the lists every run starts from, and the file tools' grants.

**The kernel names a path only for the sandbox.** `sandbox.Canonical` (`kernelPath`) is a path
with its links followed, as the kernel resolves it: a link before a `..` is followed first. On
macOS it is `getattrlist` with `ATTR_CMN_FULLPATH` (`kernelpath_darwin.go`), which answers each
name as the volume stores it and needs only the access a `stat` needs. Seatbelt's profile is
written from those names, and Settings refuses a grant spelled any other way, naming the stored
one. Elsewhere it follows links alone.

**One policy names no folder two ways.** `Policy.compile` names every rule path once and refuses
the policy where two of them hold a file its folder names twice (`aliased`). Within a policy that
passes, text and files agree, so its rules are checked, ordered and compiled by text. bwrap's
mount tree is text, so that is the comparison its arguments need.

## Alternatives considered

**Spell each component from its directory's listing.** It rebuilds the volume's name matching in
user space: case, composition and hard links each need their own rule, and a folder that can be
searched and not listed has no spelling.

**Check that each component's folder lists it (Linux).** Exact, but every run lists every folder
on every rule path, a name under a folder that cannot be listed is unchecked, and every caller of
`Canonical` gains a failure it must not drop.

**Look each component up in another case (Linux).** It guesses the volume's folding: a casefold
directory also folds an accent's composition, and a name with no letters has no other case to
try.

**Read the path off an open descriptor (`F_GETPATH`).** The same answer as `getattrlist`, but
opening needs read access: it fails on another user's home and on a file with no read bit, and
opening a folder macOS guards can prompt the user.

**Refuse every link.** The system's own paths are links (`/var`, `/tmp`, `/bin`), as are most
`PATH` entries a package manager makes, and a link is not what makes a second spelling.

## Consequences

A comparison reads the disk: it follows links and stats the folders on a path.

A comparison is true when it is made. `Policy.check` refuses a rule beneath a Write rule of the
same policy, so a run cannot rename a folder on a rule's canonical path. Three changes can still
leave a rule on a name the disk has stopped using, until the next run compiles:

- A process outside the sandbox renames a folder on the path. It runs as the user, and can
  already read what a Deny hides.
- A link on a rule's path as written is repointed. `Policy.check` compares canonical paths, so it
  does not see whether the link's own folder is writable to a run.
- Another run, holding a Write rule over a folder on the path, renames it. `Policy.check`
  compares the rules of one policy.

What is not compared as a file:

- A path that does not exist, past its deepest folder that does. Two spellings of a folder not
  yet made are two paths.
- A file with more than one hard link, within a policy: each name is one its folder holds, and
  the two are two rules.
- One folder mounted in two places, within a policy: each is a place of its own in bwrap's tree.

On Linux a policy that names a folder in two spellings is refused, and its run with it, where
macOS compiles both to the stored name.

## Revisit when

A platform's sandbox takes open folders instead of paths.
