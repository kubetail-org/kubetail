---
title: The kubeconfig fingerprint digests the whole file
date: 2026-09-30
scope: sidecar
status: Accepted
---

# The kubeconfig fingerprint digests the whole file

## Context

[Discovery as a beehive kind](2026-08-18-discovery-as-a-beehive-kind.md) gave the kubeconfig
source an anchor whose status is one fingerprint, and every `Cluster` depends on that anchor, so a
fingerprint change wakes every record. That ADR computed the fingerprint by running
`observeKubeconfig` over each context, so it would move exactly when some record's observation
would. Its first obligation was that the fingerprint stay derived from that fold.

That obligation is enforced by nothing. The digest is correct only while it reads what every
record's pass reads, and a pass that starts reading one more field wakes nobody for it, with no
test to catch it. The change was made on 2026-08-20 and written down on 2026-09-30.

## Decision

**`kubeconfigFingerprint` (`clustersources.go`) digests the whole parsed config**: SHA-256 over
its JSON, every field of every entry, whether or not a record reads it today. It is deliberately
coarser than what the records observe. An edit no record cares about wakes them all, each to
compare, find nothing moved, and settle.

## Alternatives considered

**Derive it from the fold, as before.** Rejected for the coupling above: it is precise only while
someone remembers to keep two readings in step, and the wakes it saves are cheap.

**List the fields the passes read.** The same coupling, stated by hand.

## Consequences

A false wake costs each record a map lookup and a no-op settle, which is cheap while no pass dials.
Adding a field to what a pass reads needs no change here. The other obligations of the discovery
ADR stand: the import runs ahead of the fingerprint gate, and the anchor's status gains no field
that moves every pass.
