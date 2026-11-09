---
title: GraphQL for user-facing APIs, gRPC to the agent
date: 2026-11-09
scope: cross-cutting
status: Accepted
---

# GraphQL for user-facing APIs, gRPC to the agent

## Context

GraphQL has been the browser's API since the first commit (`43df48a9`, February 2024). That schema already had a `Query` per Kubernetes list and a `Subscription` per Kubernetes watch (`appsV1DeploymentsWatch` and so on), and its `gqlgen.yml` bound the GraphQL types directly to the `k8s.io/api` structs. Why GraphQL was chosen is not recorded. What the code shows is that it maps Kubernetes list and watch onto query and subscription, and lets resolvers return client-go objects without hand-written models.

gRPC arrived with the per-node agent in #113 (September 2024), a Go service exposing `LogMetadataService`. #227 (April 2025) added `LogRecordsService` and `rgkl`, a Rust reader built on ripgrep, which the Go agent ran with `exec.Command`. The choice of gRPC for the agent is not recorded either. What the contract shows is that both services are server-streaming: a watch of file metadata, and forward and backward record streams.

Discussion #447 (June 2025) proposed rewriting the agent in Rust. It gives the reasons: talking to `rgkl` only through stdout and stderr "isn't ideal", the agent should be one language, and Rust makes "a lightweight tool that won't use much node resources". Its first goals were a Rust gRPC server implementing the existing `LogMetadataService` and `LogRecordsService`, with no new features. The rewrite (#498) was merged in August 2025 (`2d005c27`) once it reached parity, with the stated aim to "switch over without disrupting anything downstream".

#1096 (April 2026) added an SSE transport to the Cluster API so a browser could run authenticated subscriptions over POST. #1133 (May 2026) chose WebSocket over SSE for the CLI's follow, because the kube-apiserver cuts a long streamed POST at its non-watch deadline but hijacks an upgrade.

## Decision

The Dashboard and the Cluster API each serve GraphQL with gqlgen from `graph/schema.graphqls`. Queries and mutations go over POST; subscriptions go over WebSocket (`graphql-ws`). The Cluster API also accepts subscriptions over SSE. The UI generates its types from both schemas.

The Cluster API talks to the agents over gRPC with mutual TLS. `proto/cluster_agent.proto` defines `LogMetadataService` and `LogRecordsService`. The Go client is committed protoc output in `modules/shared/clusteragentpb`; the Rust server is generated at build time by `crates/types/build.rs`. `grpc-dispatcher-go` fans a metadata call out to every agent and sends a records call to the agent on the source's node.

The Cluster API's `gqlgen.yml` binds the `LogMetadata*` GraphQL types straight to the protobuf messages in `clusteragentpb`, as the Dashboard's binds Kubernetes types to `k8s.io/api`.

## Alternatives considered

**A Go agent that runs `rgkl` as a subprocess.** This was the agent from #227 until the rewrite. #447 rejected it because stdout and stderr are a poor interface between the two halves, and kept gRPC as the boundary so the rewrite stayed invisible to the Cluster API.

**SSE for long-lived subscriptions through the aggregation layer.** The Cluster API serves it, but #1133 rejected it for the CLI's follow: the kube-apiserver's request deadline tears the stream down after about a minute. WebSocket is used instead.

No alternative to GraphQL for the user-facing APIs is recorded.

## Consequences

The browser gets Kubernetes watches and log follows as subscriptions over one connection, with types generated from the schema. The agent's contract is language-neutral, which is what let it move from Go to Rust without touching the Cluster API.

Binding generated types into the schema saves hand-written models, but it ties the schema to them. A change to a `LogMetadata*` message is also a GraphQL schema change and a change to the UI's generated types. A Kubernetes client upgrade can change the Dashboard's schema the same way.

`LogMetadataWatchEvent.type` is a plain string in the proto. `ADDED`, `MODIFIED` and `DELETED` are spelled by hand in the agent and again in the UI, and the Cluster API passes them through.

Generated code sits in several places: committed Go protobuf and gqlgen output, the UI's committed `__generated__/`, and the Rust code generated on every build. CI regenerates none of them, so a stale file is caught only by a build or a reviewer.

GraphQL over WebSocket needs its own CSRF handling, since the browser cannot set headers on an upgrade: the token rides in `connection_init`.

## Revisit when

Something other than the Cluster API needs to call the agent, or the bound protobuf or Kubernetes types change often enough that the schema churns with them.
