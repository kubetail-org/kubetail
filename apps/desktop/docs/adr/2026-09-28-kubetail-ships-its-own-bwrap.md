---
title: Ship Kubetail's own bwrap, with Ubuntu's profile for it
date: 2026-09-28
scope: cross-cutting
status: Accepted
amended_by: [Linux ships only the .deb and the .rpm](2026-11-07-linux-ships-only-the-deb-and-the-rpm.md)
---

# Ship Kubetail's own bwrap, with Ubuntu's profile for it

## Context

On Linux the sidecar's sandbox is bubblewrap, run unprivileged: bwrap makes a user namespace and
builds the sandbox inside it. Ubuntu 23.10 and 24.04 restrict unprivileged user namespaces through
AppArmor (`kernel.apparmor_restrict_unprivileged_userns=1`): a process without a profile that
allows `userns` gets a namespace in which it holds no capability, so bwrap fails at
`setting up uid map`. Ubuntu 24.04 loads no profile for `/usr/bin/bwrap`; 25.04 and later load
`bwrap-userns-restrict` for it. bwrap is also not installed everywhere, and the flags the sandbox
uses (`--as-pid-1`, `--unshare-cgroup-try`) are missing from older releases.

The sandbox must need nothing from the user: a Bash that asks no one for a sandboxed command is only as common as
the sandbox behind it.

## Decision

Kubetail's `.deb`, `.rpm` and AppImage carry a bwrap of their own, built from one pinned bubblewrap
release (`scripts/build-bwrap.sh`) at `/usr/lib/kubetail-desktop/bwrap`, never setuid. The `.deb` carries
AppArmor's `bwrap-userns-restrict` renamed `kubetail_desktop_bwrap`/`kubetail_desktop_unpriv_bwrap` and attached to
that path (`src-tauri/linux/kubetail-desktop-bwrap`), loaded by its postinst. bwrap may make a user namespace
and use its capabilities there; everything it starts runs stacked under a profile that denies every
capability. The sidecar tries the system's bwrap first and its own only in its place: the
distribution patches its bwrap, while Kubetail's changes only when the user installs a new release,
and Kubetail has no in-app updates. So its own runs where the system has no bwrap, or one that
fails, as on 23.10 and 24.04, where AppArmor blocks the system's.

A release checks it on a stock 24.04: the restriction on, the `.deb` installed, the whole chain run
through the bundled bwrap as an ordinary user.

## Alternatives considered

- **Ask the user to lift the restriction** (the sysctl). It works everywhere, but it lifts it for
  every program on the machine, and a setting the user must find and change is a sandbox most users
  never get.
- **Use the distribution's bwrap alone.** No bwrap on 24.04 can make a user namespace without a
  profile, and a profile for `/usr/bin/bwrap` from Kubetail would meet Ubuntu's own after an upgrade
  to 25.04. It also leaves the flag set to whatever the distribution ships.
- **A profile on the sidecar that allows `userns`.** The sidecar would make the namespace itself,
  but the grant would pass to everything `sandbox-init` starts, the model's command included,
  unless the profile also stripped capabilities under it, which is what Ubuntu's bwrap profile
  already does and has had reviewed.
- **Kubetail's own bwrap first, the system's in its place.** One known version everywhere, the one
  the release checks. But a flaw in it stays on every machine until its user downloads a new
  release, while a distribution's fix arrives with its updates; and the probe runs every flag the
  sandbox passes, so a system bwrap too old for one fails and Kubetail's own takes over anyway.
- **A setuid bwrap.** It needs no profile, but a setuid binary shipped by an app is a larger grant
  than a profile and one Kubetail would own the security of.

## Consequences

Installing the `.deb` on Ubuntu 24.04 lets any local program run `/usr/lib/kubetail-desktop/bwrap` and get a
user namespace whose processes hold no capability — the same grant 25.04 makes for every program
through `/usr/bin/bwrap`, but one the user's 24.04 system did not make before. It is recorded as a
**By decision** row in `docs/security-model.md`.

Kubetail builds and ships a C binary and a GPL-2.0 profile: the packages carry bubblewrap's and
AppArmor's licenses, and the release attaches bwrap's source. A new bubblewrap release is a change
to the script's pinned version and checksum. The profile's names must all be renamed together: a
name left over parses, and on 25.04 would stack against Ubuntu's profile.

On Ubuntu 23.10 and 24.04 the AppImage and a dev build have no sandbox, and Bash is offered as it
is without one.

## Revisit when

Ubuntu 24.04 loads a profile for `/usr/bin/bwrap`, or its support ends: the system's bwrap then
works wherever Kubetail's does.
