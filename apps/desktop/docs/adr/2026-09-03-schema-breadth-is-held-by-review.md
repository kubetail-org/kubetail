---
title: The schema's breadth is held by review; the capability file is pinned
date: 2026-09-03
scope: cross-cutting
status: Accepted
---

# The schema's breadth is held by review; the capability file is pinned

## Context

*No authority granted ahead of a consumer* is a row in
[`security-model.md`](../security-model.md): a permission or a field exists because a call site
needs it, not because it might one day. It matters because the webview is fully trusted — a
compromised page holds whatever the capability file grants and whatever the schema serves.

The row covered two declarative files, and they are not alike. `src-tauri/capabilities/default.json`
is six permissions that change once a year; `sidecar/graph/schema.graphqls` is the app's whole
feature surface and changes with nearly every feature.

## Decision

**Pin the capability file, and leave the schema to review.**

`default_capability_grants_only_window_chrome` (`src-tauri/src/window_manager.rs`, beside the CSP
test, because `build_window` creates the windows the capability names) asserts the permission list
exactly. A new grant has to be written twice, once in a diff that is about authority rather than
about a feature.

The schema gets no such pin. What compensates is that the two things worth catching are already
caught somewhere narrower: a credential reaching the wire is held by
`TestAuthProjectionCarriesNoTokens`, which pins the field sets of `AuthState` and `Identity` — the
types gqlgen binds onto the Go values that hold the token set — and the read surface is already the
whole mirror, which is [why there is no operation
allowlist](2026-09-03-no-graphql-operation-allowlist.md). A query field the app does not call
therefore hands an attacker close to nothing they cannot reach through the generic objects watch.

What is left unfenced is a **mutation** declared ahead of its consumer. That is real authority, and
review is what holds it.

## Alternatives considered

**A golden snapshot of `schema.graphqls`.** It judges nothing — it cannot tell a token field from a
new list field, it only forces an acknowledgment. Since the schema changes with most features, the
acknowledgment would be given reflexively, and the habit of re-blessing it is exactly the habit that
would wave through the one edit that mattered. A pin is worth having only where it fires rarely, and
that is what makes the capability file a good candidate and the schema a bad one.

**A name-pattern scan** (fail on a field matching `token`, `secret`, `credential`). It catches only
the mistake careless enough to name itself, and the place where a credential could actually be bound
with no Go code — the auth projection — is already pinned by field set.

**Pinning `Mutation`'s field list** the way `AuthState`'s is pinned. This is the honest near-miss:
mutations are the authority half, and there are few of them. It loses today only because the set is
still filling out with the cluster-record lifecycle, so the pin would fire on ordinary feature work
before it ever fired on a surprise. See *Revisit when*.

## Consequences

The security-model row splits: the capability file moves to *Enforced*, and the schema half stays a
decision pointing here — so the gap reads as one we looked at rather than one nobody noticed.

The obligation this creates is on review: a pull request adding a mutation is a pull request about
authority, and a mutation with no caller in `src/` is the thing to ask about.

## Revisit when

The mutation set stops growing with routine features, or a mutation lands that does something
irreversible or off-box. Then pin `Mutation`'s field list beside the auth projection's.
