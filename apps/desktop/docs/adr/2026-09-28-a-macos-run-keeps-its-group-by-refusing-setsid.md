---
title: Keep a macOS run's processes in its group by refusing setsid and setpgid
date: 2026-09-28
scope: sidecar
status: Accepted
---

# Keep a macOS run's processes in its group by refusing setsid and setpgid

## Context

A sandboxed run on macOS is `sandbox-exec` over a Seatbelt profile, in a process group the Bash
tool makes and kills with `SIGKILL` once the run ends. macOS has no PID namespace, so a descendant
that leaves the group outlives the run, still under the run's profile.

The Seatbelt sandbox accepted that survivor because it could only write the workspace and the kubectl cache. A
review showed it keeps one more thing: the profile's `network-outbound` to the forwarder's
loopback port. The forwarder exits with the run and frees the port. Any local service can bind it
later, and the survivor can then reach that service, for as long as it lives.

On macOS a process leaves its group in three ways: `setsid(2)`, `setpgid(2)`, and `posix_spawn`
with `POSIX_SPAWN_SETSID` or `POSIX_SPAWN_SETPGROUP`. Seatbelt's `syscall-unix` operation can
refuse the first two by number. The third is a flag on `posix_spawn`, which the kernel acts on
without either syscall, and Seatbelt has no filter for it.

## Decision

`profile_darwin.sb` denies `syscall-unix` for `SYS_setsid` and `SYS_setpgid`. A process in a
sandboxed run cannot take itself out of the run's group, so the group's kill ends it before the
port is free. `TestAProcessCannotLeaveItsGroup` pins both refusals.

A process spawned with `POSIX_SPAWN_SETSID` or `POSIX_SPAWN_SETPGROUP` still leaves the group, and
we accept that. `TestAProcessThatLeavesTheGroupStaysConfined` shows that it stays confined.

## Alternatives considered

**Deny `posix_spawn` as well.** This closes the gap. It also breaks Python's `subprocess`, `awk`'s
`system()`, `xargs` and `find -exec` in the sandbox, since libc and those tools spawn through it.
A sandbox that cannot run `find -exec` pushes commands outside it, which is worse.

**Kill survivors from the sidecar.** Tag each run's profile with a marker only that run allows, and
at the run's end kill every process whose sandbox passes `sandbox_check` against it. This closes
the gap while the app runs. `sandbox_check` is a private libsystem call, and the sidecar builds
with `CGO_ENABLED=0`, so calling it needs hand-written assembly trampolines. We may still build it
(below).

**Keep the port reserved.** The sidecar would hold the port once the forwarder frees it. It cannot
know how long a survivor lives, and the reservation ends when the app quits.

**Accept every survivor.** `setsid` is how most programs daemonize (`daemon(3)`, Python's
`os.setsid`), so refusing it closes the common path for one profile rule.

## Consequences

- A shell with job control on (`set -m`) warns that `setpgid` failed and runs its jobs in the
  run's group. Nothing on the listed-programs list needs `setsid` or `setpgid`.
- A command that spawns a detached child through `posix_spawn` leaves it running after the run. It
  can write the workspace and the kubectl cache, and reach whatever later listens on the run's
  forwarder port over TCP on `127.0.0.1`. The Seatbelt profile's network rule is `tcp4` alone, so
  UDP and IPv6 at that port stay out of reach.
- The Bash tool's `TestTheGrantDiesWithTheRun` needs a watch client that outlives the group. It
  detaches through `posix_spawn` when `setpgid` is refused, so it needs `/usr/bin/python3`, which
  CI's runners have.

## Revisit when

Seatbelt gains a filter on `posix_spawn`'s flags, or the sidecar gains a way to call `libsystem`
without cgo. Either makes the survivor killable.
