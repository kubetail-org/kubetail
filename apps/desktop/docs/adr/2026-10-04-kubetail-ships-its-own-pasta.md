---
title: Ship Kubetail's own pasta, with passt's profile for it
date: 2026-10-04
scope: cross-cutting
status: Accepted
amended_by: [Linux ships only the .deb and the .rpm](2026-11-07-linux-ships-only-the-deb-and-the-rpm.md)
---

# Ship Kubetail's own pasta, with passt's profile for it

## Context

On Linux a sandboxed command reaches the internet through `pasta`, from the passt package
([network is the user's switch](2026-10-04-network-is-the-users-switch.md)). Every major
distribution packages passt, and Podman pulls it in, but few install it by default. Where it is
missing, the chat's network switch is disabled.

Where it is installed, Ubuntu loads passt's own AppArmor profile for `/usr/bin/pasta`, which lets
pasta start only programs under `/usr/bin`. On Ubuntu 23.10 and 24.04 the run's bwrap is
Kubetail's own, at `/usr/lib/kubetail-desktop/bwrap`
([Kubetail ships its own bwrap](2026-09-28-kubetail-ships-its-own-bwrap.md)), so the system's pasta
cannot start it there either.

## Decision

Kubetail's `.deb`, `.rpm` and AppImage carry a pasta of their own, built from one pinned passt
release (`scripts/build-pasta.sh`) at `/usr/lib/kubetail-desktop/pasta`. The `.deb` carries passt's own
AppArmor profile for pasta, its abstractions written out, renamed `kubetail_desktop_pasta` and attached to
that path (`src-tauri/linux/kubetail-desktop-pasta`), loaded by its postinst. pasta may make a user
namespace and use its capabilities there, and may start the sandbox's bwrap and nothing else:
Kubetail's under `kubetail_desktop_bwrap`, or the system's under its own profile where it has one.

The sidecar tries the system's pasta first and its own in its place, as it does bwrap: the
distribution patches its pasta, while Kubetail's changes only when the user installs a new release.

The profiles are checked on a stock Ubuntu 24.04, by the release after installing the `.deb` and
by every pull request after building both: the restriction on, the whole chain run through the
bundled pasta and bwrap as an ordinary user, reaching an address outside.

## Alternatives considered

- **Ask the user to install passt.** It works on every distribution, but a switch that is disabled
  until the user installs a package is a feature most users never get, and on Ubuntu 23.10 and
  24.04 the installed pasta still cannot start Kubetail's bwrap.
- **The internet through a proxy.** The run keeps no network, and an HTTP and SOCKS proxy behind
  the forwarder carries what it sends. It needs nothing installed and lets the sidecar see each
  host, but only programs that honour a proxy reach anything, and a run has no DNS.
- **A userspace network stack in the sidecar** (gVisor's netstack, as `gvisor-tap-vsock` uses).
  Full IP with no dependency, but Kubetail would own a TCP/IP stack and a helper inside the
  namespace that makes the tap device and hands it out.
- **slirp4netns.** It does pasta's job, is in maintenance, and is installed no more widely.
- **Ubuntu's profile for `/usr/bin/pasta`, widened.** A profile for a system path meets the
  distribution's own on every upgrade, as a bwrap one would.

## Consequences

Installing the `.deb` on Ubuntu 24.04 lets any local program run `/usr/lib/kubetail-desktop/pasta` and get
a user and network namespace in which pasta holds capabilities, and in which it can start only a
bwrap. passt's own profile, which any user who installs passt gets, makes a wider grant. It is
recorded as a **By decision** row in `docs/security-model.md`.

Kubetail builds and ships a second C binary under the GPL: the packages carry passt's licenses, and
the release attaches its source. A new passt release is a change to the script's pinned version
and checksum, and a new profile taken from that release's `contrib/apparmor/`. Upstream publishes
its source as snapshots of its git tags, generated on request: if a server change alters the
bytes, the checksum fails the build and the pin has to be renewed.

The AppImage's pasta, like its bwrap, sits under a mount no profile names, so on Ubuntu 23.10 and
24.04 it gives no network.

## Revisit when

Ubuntu's default install carries passt with a profile that can start Kubetail's bwrap, or the
sandbox moves to a network that needs no external program.
