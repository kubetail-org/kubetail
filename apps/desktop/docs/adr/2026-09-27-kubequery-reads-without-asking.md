---
title: KubeQuery reads without asking
date: 2026-09-27
scope: sidecar
status: Accepted
---

# KubeQuery reads without asking

## Context

Every command the model runs through Bash waits for the user's approval. KubeQuery reads the chat's
cluster's mirror, bodies included, and its answers go to the turn's model provider. A query per
approval would make the tool as slow to use as kubectl, and most of what it answers is the kind of
question a user asks in bulk: counts, joins, history. The cluster card already leaves the machine
on every send without a per-item gesture.

## Decision

**The user's send is the gesture.** A KubeQuery call runs without asking, like the card: it reads
the mirror of the chat's own cluster, and nothing else, redacted as a command's output is. The
transcript shows every query, its SQL and what the model read, so what went can be seen. An off
switch, a setting beside memory's, lands before a release build ships.

This is a risk accepted by decision, recorded in `security-model.md` and in
[the KubeQuery record](../security/2026-09-27-kubequery-reads-the-mirror.md).

## Alternatives considered

- **Approve every query.** The request would show SQL, which says little about what bodies it
  reads, and a turn that asks ten questions would stop ten times.
- **Approve body reads only.** A statement's text does not say reliably which columns it reads,
  so the line would be drawn by parsing SQL.

## Consequences

- Object bodies, events and annotations leave the machine on the model's request.
- A model steered by cluster text can choose what of the mirror reaches the provider, which already
  has the chat.

## Revisit when

- The off switch lands, or a user asks for per-query approval.
