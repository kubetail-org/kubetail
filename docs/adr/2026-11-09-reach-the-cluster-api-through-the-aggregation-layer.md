---
title: Reach the Cluster API through the aggregation layer
date: 2026-11-09
scope: cross-cutting
status: Accepted
---

# Reach the Cluster API through the aggregation layer

## Context

Until May 2026 the Cluster API ran its own authentication. Its `authenticationMiddleware` took a bearer token from `X-Forwarded-Authorization` or `Authorization`, used it for its own Kubernetes calls and forwarded it to the agents. The Dashboard reached it two ways. In `cluster` it dialed the Cluster API Service at `cluster-api-endpoint` and forwarded the session user's token as `X-Forwarded-Authorization` (#1000, March 2026). In `desktop` it went through the kube-apiserver's Service proxy (`/api/v1/namespaces/<ns>/services/<svc>:http/proxy/…`) with the kubeconfig's credentials, and injected a token for the `kubetail-cli` ServiceAccount as `X-Forwarded-Authorization`. A desktop request to the Cluster API therefore ran as that ServiceAccount, not as the user.

Trusting a forwarded header was fragile. #1113 (April 2026) fixed a bug where the desktop proxy appended its token after a client-supplied `X-Forwarded-Authorization`, so a client could shadow it with its own bearer. The proxy URL also had to name the namespace and Service of the Cluster API, and the Dashboard listed Cluster API Services per cluster.

#1125, merged on 1 May 2026, moved the Cluster API behind the aggregation layer. Its summary gives the reason: the kube-apiserver authenticates the caller and forwards a verified identity in front-proxy headers, and the Cluster API impersonates that identity downstream, "so RBAC is enforced as the originating user rather than as a shared service identity". #1133 (May 2026) then gave `kubetail logs` a Cluster API backend over the same path.

## Decision

The Cluster API is registered as the APIService `v1.api.kubetail.com`, backed by the `kubetail-cluster-api` Service on port 443 (`hack/tilt/kubetail.yaml`). It mounts its data routes under `/apis/api.kubetail.com/v1` and serves the discovery and OpenAPI documents an aggregated API needs (`internal/app/ext.go`, `openapi.go`).

`newAggregationAuthMiddleware` (`modules/cluster-api/internal/app/middleware.go`) is its only authentication. It verifies the front-proxy client certificate against the CA in kube-system's `extension-apiserver-authentication` ConfigMap, reads `X-Remote-User`, `X-Remote-Group` and `X-Remote-Extra-*`, and puts a `k8shelpers.ImpersonateInfo` on the request context. Client-go calls impersonate it; the gRPC interceptors send it to the agent.

Every caller goes through the kube-apiserver. `DesktopProxy` (`modules/dashboard/internal/cluster-api/proxy.go`) rewrites `/cluster-api-proxy/<kubeContext>/<rel>` to `APIServicePath/<rel>` and sends it with the kubeconfig's credentials. `InClusterProxy` sends it to the kube-apiserver with the session token as `Authorization`, or as the Dashboard's ServiceAccount when there is none. Both strip client-supplied `Authorization`, `X-Forwarded-Authorization` and `Impersonate-*` headers and refuse `..` segments. `HealthMonitor` watches the APIService's `Available` condition. The CLI's `modules/cli/internal/clusterapi` probes the APIService and talks to it through the kubeconfig's REST config.

## Alternatives considered

**Dial the Cluster API Service with a forwarded token.** This was the `cluster` design before #1125. The Cluster API had to trust a header for identity, which #1113 showed was easy to get wrong, and it was a second authentication surface beside the kube-apiserver's.

**Reach it through the kube-apiserver's Service proxy with a ServiceAccount token.** This was the `desktop` design before #1125. The Service proxy does not tell the backend who the caller is, so the Dashboard supplied a `kubetail-cli` ServiceAccount token and every desktop user acted as that ServiceAccount. #1125 replaced it so RBAC applies to the originating user.

## Consequences

Authentication and the caller's identity come from the kube-apiserver, so the Cluster API never handles a user's token, and the same path serves the Dashboard and the CLI. A client outside the cluster needs only its kubeconfig.

There is exactly one Cluster API per cluster, since the APIService name is cluster-wide. #1125's follow-up commit `7b8085bd` removed the per-Service routing and the Service list from the Dashboard and the UI.

The Cluster API needs more RBAC: `system:auth-delegator`, the `extension-apiserver-authentication-reader` Role, and `impersonate`. The APIService needs the Cluster API's CA in `caBundle` in production; the Tilt manifest skips verification. Installing it needs permission to create a cluster-scoped APIService.

Every request takes an extra hop through the kube-apiserver, which applies its non-watch request deadline of about a minute. A long streamed response over POST is cut at that deadline. A follow therefore uses a WebSocket, which the kube-apiserver hijacks and does not time out (#1133, `modules/cli/internal/clusterapi/client.go`).

Two invariants are easy to break. A Cluster API route that returns data must sit in the `/apis/api.kubetail.com/v1` group, since a route on the root is open to anything that can reach the pod. The Dashboard proxies must keep stripping client auth and impersonation headers and rejecting `..`, or they become a general kube-apiserver proxy carrying the session's token.

## Revisit when

Kubetail needs more than one Cluster API in a cluster, or a workload the aggregation layer cannot carry, such as a long-lived stream that cannot be an upgrade.
