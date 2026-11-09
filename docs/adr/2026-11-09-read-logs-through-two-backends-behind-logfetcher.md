---
title: Read logs through two backends behind LogFetcher
date: 2026-11-09
scope: cross-cutting
status: Accepted
---

# Read logs through two backends behind LogFetcher

## Context

Kubetail started in February 2024 as a dashboard that read everything, logs included, from the Kubernetes API. The pod log endpoint returns a container's lines and nothing else: no file size, no time of the last write, no server-side filtering. Any search has to pull every line to the client and filter it there.

#113 (September 2024) added a small agent on each node, reached over gRPC, to report log file sizes and last-event times. #227 (April 2025) added search. It moved log watching from the browser to the Dashboard backend, gave both the Dashboard (Kubernetes API) and the Cluster API (the agents) log fetch and follow operations, and added `rgkl`, a ripgrep-based reader in Rust, so the agent could grep the files on disk. The PR states the aim as "efficient log search on-disk rather than in the client". `LogFetcher` was introduced in the same PR.

The README states the install model: by default Kubetail uses only the Kubernetes API, and a user who installs the Cluster API and the Cluster Agents (together "the Kubetail API") gains log search, file sizes and last-event timestamps. The CLI followed the same line. `kubetail logs` (#173, March 2025) read only the Kubernetes API and required `--force` with `--grep` "because the command may unexpectedly download more log records than expected". #1133 (May 2026) added `--backend` and dropped the `--force` gate.

Why search runs in a per-node agent rather than in some other in-cluster component is not recorded beyond #227's one line.

## Decision

`LogFetcher` (`modules/shared/logs/log-fetcher.go`) has two methods, `StreamForward` and `StreamBackward`, each returning a channel of `LogRecord` for one `LogSource`. It has two implementations.

`KubeLogFetcher` reads the pod log endpoint through a clientset. It applies the time bounds and the grep regex in process, and walks backward by requesting growing `TailLines` batches.

`AgentLogFetcher` calls `LogRecordsService` on the agent of the source's node through the gRPC dispatcher. The agent applies the time bounds and the grep on the node, so only matching lines cross the network.

`logs.NewStream` uses `KubeLogFetcher` unless given `logs.WithLogFetcher`. The Dashboard always uses the default. The Cluster API always passes `logs.NewAgentLogFetcher` (`graph/schema.resolvers.go`, `internal/app/download.go`). The UI picks a server per kube context with `useIsClusterAPIEnabled` and sends it the same log documents. The CLI picks with `--backend auto|kubernetes|kubetail` (`selectBackend`, `modules/cli/cmd/logs.go`): `auto` probes the APIService and falls back to the Kubernetes API with a warning.

## Alternatives considered

**Kubernetes API only.** This was the design until #227. It needs nothing installed in the cluster, but a grep downloads every line to the client, which is why the CLI gated it behind `--force`. File sizes and last-event times are not available at all.

**Require the agents.** The README's install model rules this out: Kubetail works against any cluster with the Kubernetes API alone, and the Kubetail API is an add-on. `--backend=auto` and the UI's per-context check exist to serve both cases.

## Consequences

Kubetail works on any cluster the user can read, and gets faster search and file metadata where the Kubetail API is installed. Callers of `logs.Stream` do not know which backend they use.

The two backends must agree on ordering, time bounds and grep semantics, and nothing checks that they do. They match with different engines: Go's `regexp` in `KubeLogFetcher`, ripgrep in `rgkl`. File metadata (`logMetadataList`, `logMetadataWatch`) exists only with the agents.

The log operations exist in both GraphQL schemas with the same signature, because the UI sends one set of documents to whichever server is in use. A change to one schema needs the same change in the other.

Each backend carries the caller's identity its own way: `KubeLogFetcher` through the caller's clientset, `AgentLogFetcher` through the forwarded identity the agent checks with a `SubjectAccessReview`. Every resolver and handler that opens a stream passes `logs.WithAllowedNamespaces`, whichever backend it uses.

Without the Kubetail API, a backward stream costs repeated `TailLines` requests, and a grep still transfers every line.

## Revisit when

The Kubernetes log endpoint gains server-side filtering, or a core feature starts to depend on the agents, so that the Kubernetes API alone no longer gives a working product.
