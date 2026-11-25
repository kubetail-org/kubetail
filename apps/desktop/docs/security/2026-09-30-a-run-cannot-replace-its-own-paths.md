# Security record — a run cannot replace its own paths, 30 September 2026

**Subject:** on macOS a sandboxed command could replace its workspace or its kubectl cache with a
link into Kubetail's data, and a later run's profile would open the link's target. Found in a review
of the sandbox policy, confirmed on macOS, and closed. The living model is
[security-model.md](../security-model.md); the sandbox is recorded in
[Bash runs in a sandbox](2026-09-28-bash-runs-in-a-sandbox.md).

## The finding

A sandboxed command runs unasked, so the Seatbelt profile is its whole bound. The profile
allowed `file-write*` on each written path as a `subpath`, which matches the path itself. Under
the real `sandbox-exec`, a run could therefore remove its own workspace and link in its place:

```sh
cd /; rm -rf "$HOME"; ln -s <data> "$HOME"
```

`HOME` is the workspace. The kubectl cache fell the same way, and it is shared by every chat on
the cluster.

The profile resolves each path it is given. A later run built with the link in place would
compile a Write rule on `<data>` after the denial of Kubetail's directories, opening `app.db`,
every chat's files and the settings.

What stood in the way was `rootdir.Open`. Bash opens the workspace and the kubectl cache through
it before each run, and it refuses a link, so a link left in place failed the next run. The gap
was the race between that check and the profile's resolve. A background command, which also
runs unasked, can swap a directory and a link in a loop while the chat's next call starts. The
window is milliseconds wide, but a loop can hit it. We did not reproduce the race end to end.
The primitive alone was enough to act on.

## What changed

- **The profile keeps each written root in place.** Each Write rule is followed by
  `(deny file-write-unlink file-write-create (literal …))` on the same path. That denial refuses
  `rm`, `rmdir`, a rename away and a rename of a directory or a link over it. Everything inside
  stays writable, links included (`TestARunCannotReplaceItsWorkspace`,
  `TestTheProfileKeepsARunsOwnRootsInPlace`).
- **A run whose own path is a link is refused.** `Run.Check` `Lstat`s the workspace, `Writable`
  and `Readable`, and refuses one whose last component is a link. `sandboxedRunFor` calls it.
  A link higher up is the system's, such as macOS's `/var`
  (`TestARunsOwnPathThatIsALinkIsRefused`). This check alone would race. With the profile rule
  in place, a run can no longer plant the link.
- `rootdir.Open`'s refusal of a link is unchanged.

## Linux

Not affected, and checked under real bwrap. Each own path is a bind mount. Its parent is the
namespace's tmpfs over Kubetail's directory, remounted read-only. Unlinking a mount point fails,
and nothing a run does there reaches the host's directory. `TestARunCannotReplaceItsWorkspace`
runs on Linux too, with no rule added.

## Residuals

- A run can still `chmod` or `touch` its own roots. Neither replaces the root. A mode the run
  strips from the workspace can fail Kubetail's next open, which fails the run and opens nothing.
- `TMPDIR` and the run's own directory are fresh for each run, so replacing them affects
  nothing later. The rule covers `TMPDIR` anyway, as a written path.
