---
title: No GraphQL operation allowlist; the shipped set converges on the schema
date: 2026-09-03
scope: cross-cutting
status: Accepted
---

# No GraphQL operation allowlist; the shipped set converges on the schema

## Context

**H-1** in [`security/2026-09-02-threat-model.md`](../security/2026-09-02-threat-model.md) is that
`graphql_query` and `graphql_subscribe` forward whatever operation string the page hands them, so a
compromised dependency in the webview holds the whole cluster surface. **S-9** names the control
that would blunt it: an operation allowlist.

A spec worked the design out in full. The page would send only a hash; the host would look the hash
up in a table generated from `src/gql/` and forward the table's document, so an operation the app
does not ship has no document and nothing is sent. Substitution rather than comparison, because the
string the page sends is not the string codegen saw — urql's `cacheExchange` injects `__typename` —
and two normalizers agreeing byte-for-byte across two languages is the part most likely to fail
closed on legitimate traffic in release builds only.

The design is sound. What it is worth depends entirely on the gap between the set of operations the
app ships and the set the schema offers, and that gap is closing.

## Decision

**Do not build the allowlist.** The host keeps forwarding the operation the page sends, and the CSP
in `src-tauri/tauri.conf.json` remains the containment for H-1.

The reason is the ceiling, not the mechanism. An allowlist caps a compromised page at what the app
ships, and this app is a general Kubernetes client: it already streams every discovered kind's full
native body through one generic objects watch (`ClusterCachedDataObject.rawJSON`), so the read
ceiling is already the whole mirror. The mutation set is the cluster-record lifecycle and grows with
every feature. As the app fills out — the remaining built-in kinds, log tail, exec — the shipped set
converges on the schema, and an attacker confined to it is confined to nearly everything. The
allowlist would spend permanent, load-bearing complexity to hold a line that keeps moving to meet
it.

That complexity is not small: a codegen document transform whose churn touches every generated
type and every frame fixture, an invariant that persisted documents carry `__typename` or urql
silently stops invalidating cached queries, a debug/release fork in the host so `codegen:watch`
still works, and a CI gate keeping the table in step with the operations. Each is a thing that can
break the app while protecting less every quarter.

## Alternatives considered

**Build it as specced (persisted documents, host-side substitution).** Rejected on the ratio above.
Worth recording that it was rejected on value, not on feasibility — the design's two traps (hash the
document codegen printed, never the text the page sends; apply the client preset's typename
transform ahead of hashing) were resolved, so a revisit starts from those two answers.

**Hash the incoming operation and compare, with no table.** Rejected inside the spec and again here:
it needs a normalizer on both sides of the boundary, in two languages, agreeing exactly. Its failure
mode is every operation rejected in release builds, which is worse than the exposure it addresses.

**A coarse split instead — allow queries and subscriptions, gate mutations behind a gesture.**
Rejected because it misreads the risk. The cache is a full offline mirror of every kind the cluster
serves; reading it is the larger half of H-1, and this leaves reading entirely open while adding a
prompt to the half an attacker needs least.

## Consequences

**H-1 stands undiminished, and that is the residual risk this ADR accepts:** anything that executes
in the webview can read every mirrored object and call every mutation. Two properties are now the
whole protection rather than a second layer, and both must be defended as such — the CSP never
widens to a remote origin and the bundle never loads a script it does not ship, and `src/` keeps no
HTML sink (the `custom/no-html-sinks` config object in `eslint.config.ts` is the fence). A
dependency compromise is the crossing this leaves open, which makes advisory scanning in CI (**X-1**)
the mitigation that now carries the most weight.

`security-model.md` keeps the row, as **By decision** pointing here, so the exposure stays visible
rather than reading as an oversight.

## Revisit when

The webview stops being the only client — a second consumer of the sidecar's socket, or an
extension surface where the page's operations are no longer all ours — since the ceiling argument
rests on the page and the schema serving one app. Also if the schema grows a class of operation the
app will never ship, destructive or expensive enough that capping the page below the schema is worth
the machinery again.
