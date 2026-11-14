---
title: One implementation per environment behind an interface
date: 2026-11-09
scope: cross-cutting
status: Accepted
---

# One implementation per environment behind an interface

## Context

Until January 2025 one config key, `auth-mode`, decided two things at once. Its values were `cluster` (in-cluster credentials), `local` (the kubeconfig, set by `kubetail serve`) and `token` (in-cluster credentials, falling back to the kubeconfig, with the caller's token on each request). The backend held a single `rest.Config` for a single cluster. Code branched on `auth-mode` where it needed to: `k8shelpers` to build the config, the server's authentication middleware and auth handlers, and `grpchelpers`.

#149 (January 2025) was a large refactor. It split the server into the Dashboard and the Cluster API and added installing the Cluster API from the UI. It also introduced `config.Environment` with the values `desktop` and `cluster`, narrowed `auth-mode` to `auto` and `token`, and added `ConnectionManager` with a desktop and an in-cluster implementation. Kubernetes calls began to take a kube context, so the desktop Dashboard could serve every context in the kubeconfig. #151, merged the same day, refused the desktop-only operations (`kubeConfigGet`, `kubeConfigWatch`, the Cluster API install) outside `desktop`. #157 (January 2025) gave the Cluster API health monitor the same shape.

The PRs carry no written rationale; the original reasoning is not recorded. What the code shows is that the two environments differ in where credentials come from, how many clusters there are, how the Cluster API is reached, and how its health is watched.

## Decision

`config.Environment` (`modules/shared/config/config.go`) has two values. In `desktop` credentials come from the kubeconfig and a request names its kube context, the current context by default. In `cluster` there is one cluster, the kube context is the empty string, and a request runs as the token it carries, or as the Dashboard's ServiceAccount when `auth-mode` is `auto` and it carries none.

Each seam that differs by environment is an interface with one implementation per environment, picked by a factory that switches on `config.Environment`:

- `k8shelpers.NewConnectionManager` returns `DesktopConnectionManager` or `InClusterConnectionManager`.
- `newClusterAPIProxy` (`modules/dashboard/pkg/app/helpers.go`) returns `DesktopProxy` or `InClusterProxy`.
- `clusterapi.NewHealthMonitor` returns `DesktopHealthMonitor` or `InClusterHealthMonitor`.

A factory panics or returns an error on an unknown value. A resolver checks the environment itself only to refuse an operation that exists in one environment alone (`modules/dashboard/graph/schema.resolvers.go`). The Cluster API always runs in `cluster`. `kubetail serve` and `kubetail logs` run in `desktop`, or in `cluster` with `--in-cluster` (#819). The Dashboard passes the environment to the UI, which branches on it too (`appConfig.environment`).

## Alternatives considered

**One `auth-mode` key for both credential source and authentication.** This was the design before #149. It named three combinations of two separate questions: where credentials come from, and whether a request carries its own token. #149 split them into `environment` and `auth-mode`; why is not written down.

**Branch on the environment at each call site.** This was the shape of the pre-#149 code, which switched on `auth-mode` in several packages. The current code keeps such checks for refusals only and puts every behavioural difference in an implementation.

## Consequences

Callers hold an interface and do not know the environment. Code that differs by environment lives in two places side by side, so each implementation can be read and tested alone.

A change to environment-specific behaviour goes in both implementations, and nothing enforces that. The two proxies, for example, each strip client-supplied auth and impersonation headers on their own. A new seam that differs by environment needs its own interface and factory.

Desktop-only GraphQL operations stay in the schema in both environments. Each one's resolver must refuse it outside `desktop`; one that forgets is reachable in the cluster.

The environment says where credentials come from, not where the binary runs. `kubetail serve --in-cluster` runs the Dashboard in `cluster` mode from a pod.

## Revisit when

A third environment appears, such as a cluster-hosted Dashboard that serves several clusters, which neither implementation describes.
