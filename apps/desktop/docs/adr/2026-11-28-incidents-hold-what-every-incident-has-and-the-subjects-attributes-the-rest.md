---
title: Keep what every incident has in columns, and the rest in the subject's attributes
date: 2026-11-28
scope: sidecar
status: Accepted
---

# Keep what every incident has in columns, and the rest in the subject's attributes

## Context

A Kubernetes subject has an API group, a kind, a namespace and a name. An AWS resource would have
an account, a region and an ARN; a host, a hostname. The screens filter and draw incidents, and the
triage brief describes the subject to a model. The monitor core knows no integration
([ADR](2026-11-28-the-monitor-core-knows-no-integration.md)).

## Decision

**`incidents` holds what every incident has**: its target and the target's UID, its signal with the
signal's label and severity, its subject's key and label, its times, its count, and later its alert
and its triage. **`subject_attributes` holds the rest**, a JSON object of strings that only the
incident's integration writes and the core never reads. For Kubernetes the keys are `apiGroup`,
`kind`, `namespace` and `name`, a stored format that `persisted-data.md` lists.

An occurrence's `detail`, what the reading saw that time (a pod, a container, a condition's
message), is a JSON object of strings in the same way. An incident copies its signal's label and
severity when it opens, so a row whose signal a later build renames or drops still reads whole.

## Alternatives considered

- **Columns for the Kubernetes subject** (`namespace`, `kind`, `name`). Each integration would add
  its own, nullable for every other, and each would be a migration.
- **A table of attributes per integration.** It spreads one incident over tables the core cannot
  join without knowing the integration.
- **Attributes as typed values.** Every consumer draws them as text, and strings keep the format
  the same for any integration.

## Consequences

A new integration migrates nothing. The webview draws the attributes as key–value text and knows an
integration in one place, its label. A screen can filter by an attribute only in the webview, and
by a detail only within one incident's occurrences. Each attribute key is a persisted format all
the same: renaming one needs the same care as a column.

## Revisit when

A query the sidecar must run, such as a filter across many incidents, needs an attribute as a
column.
