# Security record — Kubetail's folders are resolved once, 24 November 2026

**Subject:** how the sidecar follows a link to one of Kubetail's folders
([#1145](https://github.com/kstackhq/kstack/issues/1145); `sidecar/internal/app/folders.go`,
`subfolders.go`, `sidecar/internal/lib/realpath/followed.go`). The living model is
[security-model.md](../security-model.md); the decision is the
[ADR](../adr/2026-11-24-follow-a-link-to-a-kubetail-folder-once-at-start-and-refuse-one-below-it.md).

## The gaps it closes

- **A linked subfolder was checked only at start.** One linked while Kubetail ran was not fenced
  until the next start. Now a link at any subfolder fails the start, and the subfolders are made
  through an `os.Root`, so the check and the making are one step.
- **A folder inside another was fenced twice**, since a link could lead the inner one elsewhere.
  The fence now holds resolved paths. Step 3 drops the second entry.
- **A linked config folder kept its mode**, and nothing said what such a link meant. Step 2 states
  it for the host.
- **A linked folder's own name was not fenced.** With `~/.cache/kubetail` linked elsewhere, a
  read-write grant of `~/.cache` was accepted, and a sandboxed command could repoint the link for
  the next start to follow. Now no read-write grant is on or above a link parent, and a run's
  policy with such a Write rule does not compile.

## The rule

A link is followed in one place, at one time, and refused everywhere else. A layout folder may be
a link, followed once at start. The Unix runtime folder may not itself be one. A subfolder must be
a real folder. The sidecar refuses a layout whose kinds collide, comparing folders by identity, and
on Windows one a link leads out of the user profile.

## What it assumes

Kubetail assumes every folder on the path to a layout folder, and to a link's target, can be written
only by the user or by root. A root pointed at a folder another account can write, and a process
of the user's own outside the sandbox, are outside what it protects; either can still repoint a
link.

It checks one folder itself, the Unix runtime folder, because its default parent is shared:

- **At launch** the host adopts it only when it is a folder, not a link, that this user owns
  (`ensure_private_dir`). The sidecar then refuses it at start if it is a link
  (`layout.ResolveRuntime`).
- **Before each sandboxed run** the sidecar refuses it when it is a link, another user owns it, or
  group or others can write it (`checkPrivate`).

Neither check reads a folder above it.

## Who can still write where a run writes

A sandboxed run writes its workspace, its tool home, its kubectl cache and its `TMPDIR`, each
below a subfolder of the data or cache folder (`Always.Write`), and the folders granted read-write
(`Files.Write`). None is a layout folder's parent or a link parent: the first are inside Kubetail's
folders, and a grant on or above a link parent is refused at the grant and again when the run's
policy compiles.

## Windows

`realpath.IsLink` counts a symlink and every reparse point Go reports as irregular. CI's Windows
runner pins that a junction and a directory symlink are each a link to `IsLink`
(`TestAJunctionIsALink`, `TestADirectorySymlinkIsALink`), to `rootdir.Open`
(`TestOpenRefusesAJunctionAndADirectorySymlink`) and to Rust's `is_symlink()`
(`a_junction_and_a_directory_symlink_are_links`).

**Open:** a folder OneDrive syncs, hydrated and not, has not yet been checked against the three by
hand. It is settled before the sequence lands.
