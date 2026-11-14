---
title: Authenticate the front proxy in middleware
date: 2026-11-09
scope: cluster-api
status: Accepted
---

# Authenticate the front proxy in middleware

## Context

#1125 (merged May 2026) registered the Cluster API as the APIService `v1.api.kubetail.com` ([Reach the Cluster API through the aggregation layer](2026-11-09-reach-the-cluster-api-through-the-aggregation-layer.md)). Every caller now reaches it through the kube-apiserver. The kube-apiserver connects with its front-proxy client certificate and states the caller's user, groups and extras in request headers. Those headers are trustworthy only if the peer really is the front proxy.

Not every route can demand that certificate. Kubelet probes call `/healthz`, and the discovery and OpenAPI routes (`/apis`, `/openapi/v2`, `/openapi/v3`) sit outside the authenticated group. The comment on `BuildTLSConfig` (`internal/helpers/tls.go`) gives this as the reason the listener does not require a client certificate: "kubelet probes and pre-auth discovery routes share the listener".

The design settled over several commits in #1125. f16f9b9 first accepted either the front proxy or any certificate from the cluster's client CA, with the CN as the user. ff97e2c added `tls.client-ca-file` and `tls.client-auth-type` so operators could require client certificates at the listener. 93830bf described the result as "mTLS at the listener gates all access". Two days later 674435d dropped direct client-certificate auth, and 03ab0f0 fixed the listener at `RequestClientCert` with "the auth middleware is the boundary", removing the knobs. The PR's summary was written before those two commits and still describes the configurable mTLS. The reasoning for the final shape lives in the commit bodies cited here.

## Decision

The HTTPS listener is fixed at TLS 1.2 or later with `tls.RequestClientCert` (`helpers.BuildTLSConfig`, wired in `cmd/main.go`). It asks for a client certificate and completes the handshake without one. TLS is always on.

`newAggregationAuthMiddleware` (`internal/app/middleware.go`) on the `/apis/api.kubetail.com/v1` group is the only authentication:

- It rejects a request with no peer certificate.
- It verifies the leaf (`PeerCertificates[0]`) against `requestheader-client-ca-file`, with the rest of the presented chain used only as intermediates.
- It checks the leaf's CN against `requestheader-allowed-names` when that list is not empty. An empty list accepts any CN, as the kube-apiserver does.
- It reads the user, groups and extras from the headers that config names, and sets `*k8shelpers.ImpersonateInfo` on the request context.

The config comes from `kube-system/extension-apiserver-authentication`, read once in `NewApp` (`loadAggregationAuthConfig`).

`removedTLSKeys` (`pkg/config/config.go`) makes startup fail if `tls.enabled`, `tls.client-auth-type`, `tls.client-ca-file` or `cluster-agent.tls.enabled` is set.

## Alternatives considered

- **Require and verify client certificates at the listener.** Probes and discovery need to connect without one (`tls.go`). The operator-configurable form of this existed briefly (ff97e2c) and was removed in 03ab0f0.
- **Accept any certificate from the cluster's client CA, CN as the user.** Dropped in 674435d: it accepted kubectl admin, controller and `system:node` certificates and "bypasses kube-apiserver entirely". The Cluster API is meant to be reachable only through the front-proxy chain.
- **Keep bearer-token auth in the resolvers.** The Cluster API checked a forwarded token per resolver until #1125 (#1096, `getBearerTokenRequired`). Aggregation hands it an identity, not a token, and 9346ef7 removed that path as dead code.
- **Ignore the removed TLS keys.** The `removedTLSKeys` comment rejects this: a targeted error makes "operators notice the migration". 03ab0f0 adds "instead of silently losing TLS".

## Consequences

Probes and discovery work over the same port as the API. In exchange, authentication is a property of the route, not the connection. A route registered on `root` instead of the group is open to anything that can reach the pod.

The middleware is not mounted under `gin.TestMode`, since the loader needs a live cluster. App tests say nothing about who may call a route. Changes to who is accepted are tested in `internal/app/middleware_test.go`.

The middleware has invariants that are easy to undo:

- Only the leaf is verified as the identity. The handshake proves possession of the leaf's key alone. Verifying any certificate in the presented slice would let a client append the trusted proxy CA as a "chain" entry and be accepted (8c7e552, `TestAggregationAuth_RejectsTrustedCertAppendedAsChainEntry`).
- Groups are read with `Header.Values`. The kube-apiserver sends one `X-Remote-Group` header per group, and `Get` drops all but the first (f548b8c).
- Extra-header prefixes are matched case-insensitively. net/http canonicalizes header keys, while the kube-apiserver configures the prefixes in lowercase (84b7f27).

The front-proxy CA and header names are read once at startup. A change to the ConfigMap takes effect only after a restart. This is a property of the design; the record does not say whether it was weighed.

An operator upgrading with an old config gets a startup error, not a silent change in behaviour.

## Revisit when

Probes and discovery can move to a separate listener, so the API port could require client certificates. Also revisit if the Cluster API must be reachable other than through the kube-apiserver.
