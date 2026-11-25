---
title: Redact credentials on the way into the cache, and never store a function of a secret
date: 2026-08-30
scope: sidecar
status: Accepted
---

# Redact credentials on the way into the cache, and never store a function of a secret

## Context

A cache is one SQLite file per cluster, unencrypted, under the user's data directory, holding
the body of every object a kind sync mirrors (`raw_json`). It outlives the session, it is
readable by anything running as the user, and it follows the user's machine into backups. That
is a materially different exposure from `kubectl`, which holds nothing between invocations.

The app is also an agent surface. The mirror is what an AI reads to answer questions, so
whatever the cache holds can reach a model context and a chat transcript without a person
deciding it should. Anything stored in the clear is stored for both readers.

Against that: `v1/Secret` is one of the kinds most worth mirroring. "Does `db-creds` exist,
and does it have the `password` key the Deployment mounts?" is among the most common causes
of a pod that will not start, and answering it needs the object, not the values in it.

## Decision

**Bodies are redacted at write time, in `sanitize` (`internal/kubestore/objects.go`), before
the body is marshalled into `raw_json`.** The plaintext never reaches disk. Reads stay pure
pass-through, which is what lets `ClusterCachedDataObject` serve `raw_json` verbatim.

**Keys survive; values are replaced with `[redacted]`.** Structure is the diagnostic payload
and is not itself sensitive, so a Secret keeps its key names and loses every value.

What gets redacted is named by the `redactions` table, keyed by **(api group, Kind)** and
carrying explicit paths into the body with one of three modes — replace a scalar, replace a
map's values, or drop the field. `v1/Secret` is an ordinary entry in it (`data` by value,
`stringData` dropped — it is write-only server-side, so a stored body claiming to carry it
would be a lie).

**The lookup reads the body's own `apiVersion` and `kind`**, never the kind the worker was
configured with, so redaction cannot be bypassed by the collection an object was addressed
through. A `example.com/v1 Secret` from a CRD is not a core Secret and is not redacted by the
core entry.

Entries are read off the CRD's published schema, never inferred from a field's name. The seed
is `v1/Secret`, `cert-manager.io/Certificate` (the two keystore passwords, which the schema
documents as "a literal password", the alternative to `passwordSecretRef`),
`grafana.integreatly.org/GrafanaDatasource` (`secureJsonData`, Grafana's own name for the
credential half of a datasource) and `grafana.integreatly.org/GrafanaDashboard`
(`spec.publicSharing.accessToken`).

**Nothing derived from a secret value is stored** — no hash, no HMAC, no length, no
fingerprint. "Did this Secret change, and when" is answered from `resource_version` and
`updated_at`, which every row already carries and which are not functions of the plaintext.

## Alternatives considered

**Exclude Secrets from the mirror entirely.** This was the initial instinct and it is strictly
worse. It gives up the existence-and-keys questions that are the reason to mirror the kind, and
it buys nothing over redaction, which already keeps values off disk. It is also worse than it
looks: a dropped kind is *invisible*, not deferred, because no read path reaches a cluster
object except the mirror. Sensitivity is therefore not a reason to sit in `notMirrored`.

**Redact at read time, in the resolver.** Leaves plaintext at rest — the exposure this ADR
exists to remove — and puts the guarantee in every read path rather than in one write path, so
a new field or a debug dump silently bypasses it.

**Hash secret values so a change is visible.** Rejected, and the reasoning generalizes: a hash
of a secret is a *credential oracle*. Secret values are frequently low-entropy — passwords,
short tokens, usernames, base64 of a short string — so an attacker holding the cache file can
enumerate candidates offline and confirm hits. That converts a file with no credential material
into one worth stealing, which is the exact property this design buys. A keyed HMAC narrows it
only if the key lives somewhere the file does not, which means an OS keychain dependency and a
key-management story.

The gain does not justify it, because **the question is already answered without touching the
plaintext**: any write to a Secret bumps `metadata.resourceVersion`, which is stored per row.
That signal has *no false negatives* — a data change always bumps it — and over-reports only on
metadata-only edits. For "did the credentials rotate around the time this started failing", a
false positive costs a glance; a credential oracle costs the credential. The one thing a hash
adds is per-key granularity ("*which* key rotated"), and that is also the most brute-forceable
form, since it isolates one low-entropy value per digest.

**Match sensitive fields by name heuristic** (any leaf called `password`, `token`, `apiKey`…).
Attractive because no table enumerates every operator, and rejected for false positives: the
mirror exists to be read, and silently blanking a field that merely sounds sensitive destroys
diagnostic value with no signal that it happened. A survey of the published schemas is what
settled it — in external-secrets alone, 68 credential-shaped fields are references (a
`secretRef`, a `key`/`name` selector) that need no redaction at all, against 22 that permit an
inline value. A name-matcher cannot tell those apart and would blank all 90.

## Consequences

Values never reach disk, so there is no window, no migration to repair an existing cache, and
no way for a new read path to expose what the write path removed.

The table is **necessarily incomplete**, and this is the maintenance obligation the design
creates. Most operators reference credentials rather than inlining them, so most need no entry;
but the long tail that offers an inline value beside the reference cannot be enumerated, and
new operators ship constantly. The mitigation is that an entry is cheap (a path that is absent
costs nothing) and that adding one is a single line. The obligation is that entries must be
*verified against the schema* rather than guessed, or the table starts destroying data.

Two kinds are knowingly uncovered, both for the same reason — the sensitive values are
indistinguishable from the diagnostic ones, and blanket redaction would gut the kind:

- `v1/ConfigMap`, which is where people put things that should have been Secrets.
- Free-form blobs in GitOps CRs, notably `HelmRelease.spec.values`, which routinely carries
  inline chart passwords.

Both are better addressed by a user-facing setting than by a guess. A Helm release Secret
(`data.release`, a gzipped blob containing rendered manifests and their Secret values) is
already covered, since it is a `v1/Secret` and the entry redacts every value under `data`.

`redactionsFor` reading the body's own identity is load-bearing and easy to break: routing the
lookup through the worker's configured kind would reintroduce the bypass.

## Revisit when

A cache file gains encryption at rest, or the store moves somewhere with an OS-enforced access
boundary. That would not change the redaction decision — defence in depth, and the agent-surface
argument is independent of the file's permissions — but it would reopen the hashing question,
since the brute-force premise is an attacker holding the file.
