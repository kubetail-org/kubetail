---
title: Share informers and gate each caller with an access review
date: 2026-11-09
scope: shared
status: Accepted
---

# Share informers and gate each caller with an access review

## Context

The Dashboard and the Cluster API watch workloads, pods and nodes with client-go informers. `logs.SourceWatcher` starts one per namespace and workload type, plus one on nodes, for every log stream. Callers have different RBAC: each user in `desktop`, and each token or impersonated identity in `cluster`.

Informer factories were shared per namespace before any per-caller check existed (`shared-informer-factory-manager.go`, removed in #227). #227 (April 2025) moved log watching from the UI to the backend. It added `IsAllowedInformer`, a `SelfSubjectAccessReview` for `list` and `watch` run as the caller before an informer is handed out. #234, a day later, added a five-minute cache of allowed answers. Neither PR records why the factories are shared, nor why denials are not cached.

#1000 (March 2026) tried the other approach in its first revision: factory keys that included the bearer token. Its description still lists "Isolate informer factories per bearer token ... to prevent privilege bleed". A review comment on the PR pointed out the cost. Each distinct token would create a factory, with its background watches, that lives until the process exits, with no eviction. Under `auth-mode: token`, with rotating tokens or many users, that growth is unbounded. The merged commit (4214e81) keys factories by namespace only. The record does not say whether that comment was the reason, but it is the only discussion of the choice.

## Decision

Informer factories are shared per kube context and namespace (`factoryCacheKey` in `DesktopConnectionManager`; `factoryCache[namespace]` in `InClusterConnectionManager`). They run on the process's own credentials: the kubeconfig's user in `desktop`, the component's ServiceAccount in `cluster`. They carry no request's identity, since an informer outlives the request that started it.

`NewInformer(ctx, kubeContext, token, namespace, gvr)` first calls the authorizer (`modules/shared/k8shelpers/authorizer.go`). The authorizer runs `SelfSubjectAccessReview`s for `list` and `watch` on the resource in that namespace, as the caller:

- `DefaultInClusterAuthorizer` uses the given token as the bearer. Under aggregation the token is empty, and the impersonated identity on the context makes the review "self" for that user.
- `DefaultDesktopAuthorizer` uses the kubeconfig's clientset and ignores the token.

An allowed answer is cached for five minutes (`cacheTTL`). A denial is not cached. In the cluster the cache key includes the token, or the whole impersonated identity (user, sorted groups, sorted extras: `impersonateCacheKeyPrefix`).

## Alternatives considered

- **A factory per caller, with the caller's credentials.** Tried in #1000's first revision. The review comment there names the cost: one long-lived set of watches per token, never evicted.
- **No per-caller check.** This was the shape before #227. With shared factories it hands every caller whatever the process's credentials can see. The record has no discussion beyond adding the check.

## Consequences

Each namespace and resource is watched once per process, however many users stream logs from it. The process's credentials must cover every namespace and resource any caller might use.

The access review is the only thing between a caller and the informer's data. A new path to informer data must make the same check. A caller must pass the request's token, or keep the impersonated identity on the context. Otherwise, the review runs as the process and is allowed.

A revoked permission can take up to five minutes to apply to new informers. An informer already handed out keeps delivering events for the life of the subscription.

Denials are always re-checked, so a newly granted permission applies at once. Repeated denied calls each cost a review.

The cache key must name the whole identity. 9346ef7 fixed a key that, under aggregation, was the empty token for every caller, so the first user's answer served everyone. 26f871e added extras to the key so a credential with fewer extras could not reuse a cached allow. `TestInClusterAuthorizer_IsAllowedInformer_CachePerImpersonated*` pin both.

In `desktop` the review always runs as the kubeconfig's user, who also owns the informers. There the check reports missing permissions early. It is not a privilege boundary.

## Revisit when

Informer factories gain reference counting or eviction, which would make per-caller factories affordable. Also revisit if many identities hit denials often enough that uncached denials cost more than their prompt recovery is worth.
