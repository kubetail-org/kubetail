---
title: Status mirrors the kubeconfig; the frontend draws the verdicts
date: 2026-09-03
scope: cross-cutting
status: Accepted
---

# Status mirrors the kubeconfig; the frontend draws the verdicts

## Context

Surfacing an unverified cluster connection needed one bit in the UI: this context reaches its
server without checking a certificate. Two file-stated facts produce it — the cluster entry's
`insecure-skip-tls-verify`, and a plain `http://` server URL — and the first shape we shipped was a
single derived `tlsUnverified` boolean on `ClusterStatusSourceKubeconfig`.

That put a consumer's question inside the record. It also put a fact about the *cluster entry* into
the block that describes the *reference* to it: `cluster` and `user` are entry names, `isPresent`
and `isDefault` are properties of the context's reference, and `tlsUnverified` was neither.

Support for `exec` credential plugins, planned next, adds a second observation of the same kind,
so the shape chosen here is the one the next few will copy.

## Decision

**A kubeconfig-observed value is mirrored onto status as the file states it, and a verdict over it
is drawn where it is rendered.** `ClusterStatusSourceKubeconfig` holds a half per reference —
`Cluster{Name, Entry}` and `User{Name}` — so an entry name and the entry it resolves to sit
together and go missing independently: the context always states a name, and `Entry` is nil when
the file defines none. `ClusterStatusSourceKubeconfigClusterEntry{Server, InsecureSkipTLSVerify}`
is populated in `observeKubeconfig` (`sidecar/internal/services/cluster/clusters.go`) and cached on
absence like the rest of the block. The "is this connection verified?" question is answered once in
the webview, by `tlsUnverifiedReason` in `src/lib/kube-config.tsx`.

**The two halves are not symmetric, and must not be made so.** A cluster entry holds no
credential, so it is mirrored and says which fields it carries. An authInfo entry holds tokens,
client keys and exec environments, so the user half carries the name alone today, and anything
served from it later (the planned `execPlugin`) is a chosen projection — never "the entry, as the
file states it".

Kubeconfig facts go on **status**, never on `ClusterSpec` and never on a kubeconfig surface of
their own. Status is where observations live: spec is user intent, and a copy of the file in spec
goes stale the moment someone edits `~/.kube/config`.

## Alternatives considered

**Keep the derived boolean on the record.** One place decides what "unverified" means, and no
consumer can forget the `http://` arm. Rejected because it is lossy in a way that hides a real
state — a context naming an undefined cluster entry read as verified — and because each new
question would add another field, each stating a conclusion the record has no way to revisit.

**Stream the kubeconfig itself** (a `kubeConfigWatch`, frontend joins entries by name). The most
faithful option, and the one this ADR is closest to reversing. Rejected on timing: status rides the
same delta stream as the record, so an entry's values arrive in the same frame as the
`isPresent`/`isDefault` that qualify them, already ordered against the reconcile that produced
them. A second stream is a second clock — every consumer would carry its own answer for the window
where one has updated and the other has not — and the file stream has no equivalent of the
cache-on-absence that keeps an orphaned record identifiable.

**Expose `insecureSkipTLSVerify` alone**, without `server`. Faithful to the field, but not to the
question: a plain-`http` context would read as verified.

**Flat `cluster`/`user` name fields beside a `clusterEntry`.** The shape this replaced. It put a
fact about the entry into the block describing the reference to it, and left no obvious home for
the user side's equivalent.

## Consequences

The security-model row *"An unverified cluster connection is visible in the UI"* is now pinned by a
frontend test, not a sidecar one. That is the cost of moving the verdict: a webview that forgets an
arm shows nothing, and no sidecar test would notice. The obligation this creates is that the
derivation stays in **one** exported helper with its own tests — a second inline copy in a future
view is the way this breaks.

`Cluster.Entry` being nullable makes "the file defines no such entry" expressible, which the
boolean could not say.

## Revisit when

Several unrelated views need different verdicts over the same kubeconfig facts, or a consumer
outside the webview (the host, a CLI) needs the verdict — at that point the derivation has more
than one home, and belongs back on the sidecar as a computed field beside the mirrored one.
