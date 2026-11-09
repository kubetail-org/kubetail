---
title: Forward the caller's identity hop by hop
date: 2026-11-09
scope: cross-cutting
status: Accepted
---

# Forward the caller's identity hop by hop

## Context

A log request crosses up to four processes: the Dashboard, the kube-apiserver, the Cluster API and the Cluster Agent on the pod's node. The agent reads log files straight off the node's disk, so the Kubernetes API's own `pods/log` authorization never sees those reads.

The first design forwarded the user's bearer token. #227 (April 2025) made the Cluster API's log endpoints require one. #618 (August 2025, for issue #616: "when we serve a request we don't verify that the client has the required k8s permissions") made the Rust agent run a `SelfSubjectAccessReview` with that token. #1000 (March 2026, issue #999) fixed `auth-mode: token` by having the Dashboard send the session's token to the Cluster API as `X-Forwarded-Authorization`. #1113 (April 2026) then had to close an injection: a client-supplied `X-Forwarded-Authorization` could shadow the one the Dashboard set.

#1125 (merged May 2026) moved the Cluster API behind the kube-apiserver's aggregation layer ([Reach the Cluster API through the aggregation layer](2026-11-09-reach-the-cluster-api-through-the-aggregation-layer.md)). Its summary states the goal: the Cluster API "impersonates it on every downstream Kubernetes call, so RBAC is enforced as the originating user rather than as a shared service identity". Under aggregation the kube-apiserver authenticates the caller and passes on only a verified user, groups and extras in front-proxy headers. The Cluster API never sees a token, so a token can no longer be forwarded to the agent. Commit d4f6955 replaced the gRPC bearer interceptors with identity metadata, and aca40c3 switched the agent from `SelfSubjectAccessReview` to `SubjectAccessReview`. Neither commit records reasoning beyond that switch.

## Decision

Each hop receives the caller's identity from the hop before it and checks it for itself.

- The Dashboard sends the session's token as `Authorization` to the kube-apiserver (`/apis/api.kubetail.com/v1/...`) after stripping any client-supplied `Authorization`, `X-Forwarded-Authorization` and `Impersonate-*` headers. `desktop` goes through the kubeconfig's credentials instead. `kubetail logs` calls the kube-apiserver directly.
- The kube-apiserver authenticates the caller and forwards the identity to the Cluster API in front-proxy headers ([Authenticate the front proxy in middleware](2026-11-09-authenticate-the-front-proxy-in-middleware.md)).
- The Cluster API puts the identity on the request context as `*k8shelpers.ImpersonateInfo`. `ImpersonatingRoundTripper` (`modules/shared/k8shelpers/transport.go`) turns it into `Impersonate-*` headers on every client-go call. The connection itself authenticates as the Cluster API's ServiceAccount, which holds the `impersonate` permission.
- `ImpersonateUnaryClientInterceptor` and `ImpersonateStreamClientInterceptor` (`modules/shared/grpchelpers`) send it to the agent as `x-remote-user`, `x-remote-group` and `x-remote-extras` gRPC metadata. Extras travel as one JSON-encoded header, since gRPC metadata keys allow only `[0-9a-z-_.]` and cannot carry sub-keys such as `authentication.kubernetes.io%2fcredential-id` (9f710e1).
- The agent accepts that metadata only from a peer whose client certificate chains to its CA and, when `tls.allowed-names` is set, whose CN is listed (`crates/cluster_agent/src/auth.rs`). It then runs a `SubjectAccessReview` for `get` on `pods/log` as that identity before it opens a file (`authorizer.rs`).

## Alternatives considered

- **Forward the bearer token end to end.** This was the design until #1125. Under aggregation the Cluster API has no token to forward. It also carried the token through two more processes and needed the header-smuggling fix in #1113.
- **Run downstream calls as the component's ServiceAccount.** #1125 rejects this by name: RBAC would apply to "a shared service identity" rather than the user.
- **One `x-remote-extra-*` key per extra, as the HTTP front-proxy headers do.** It does not fit gRPC's metadata key charset (9f710e1).
- **Let the agent trust the Cluster API's authorization.** Not discussed in the record. The agent's own check predates aggregation (#616, #618), and the Cluster API authorizes nothing itself, so without the agent's review a file read would be unchecked.

## Consequences

The user's RBAC governs every Kubernetes call and every file read. Neither the Cluster API nor the agent holds a user credential.

The identity lives on the `context.Context`. A call made with `context.Background()`, or with a client built outside the connection manager, quietly runs as the Cluster API's ServiceAccount, which can impersonate anyone. `TestImpersonateInfoOnContextReachesAgentAsRemoteHeaders` and its streaming sibling pin the gRPC path.

The agent's forwarded-identity check is only as strong as its mTLS listener. Weaken the listener and the `x-remote-*` metadata becomes forgeable.

Each hop caches decisions differently. The agent caches for 30 seconds, denials included, keyed by user, groups and extras. The Go informer check caches allows only, for five minutes. A permission revoked mid-stream is not re-checked: a stream is authorized once, when it opens.

Each cache key must include the whole identity. 26f871e added extras after a user with the same name and groups but different extras could reuse a cached allow.

## Revisit when

The Cluster API stops being served through the aggregation layer, or Kubernetes offers a way for an aggregated server to pass a verifiable credential on to a backend.
