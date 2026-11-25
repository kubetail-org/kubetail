---
title: Route all webview GraphQL through Tauri IPC — no network access
date: 2026-08-09
scope: cross-cutting
status: Accepted
---

# Route all webview GraphQL through Tauri IPC — no network access

## Context

The webview needs the sidecar's GraphQL API. The sidecar listens on a user-restricted Unix
socket (named pipe on Windows) with no TCP listener at all — a deliberate posture: nothing on
the machine can reach the API except processes that can open that socket. A webview cannot
speak to a Unix socket directly. This decision predates this file.

## Decision

The webview has **no network access**. urql is configured (`src/lib/graphql/`) so every
operation rides Tauri IPC: queries and mutations through `invoke('graphql_query')`
(`invoke-fetch.ts`), subscriptions through `invoke('graphql_subscribe'/'graphql_unsubscribe')`
over a Tauri `Channel` (`subscribe-exchange.ts`, which owns its own capped-backoff reconnect).
The Rust host forwards queries as plain HTTP/1.1 POSTs over the socket and opens one SSE
stream per subscription (`src-tauri/src/services/sidecar/graphql/`), parsing gqlgen's SSE
frames into the `{type,payload}` channel envelopes the renderer consumes. Connections are not
pooled — UDS connect is sub-millisecond.

## Alternatives considered

**A localhost TCP listener the webview fetches directly.** Rejected: it re-opens the surface
the socket closes (any local process could connect), requires a port + auth token scheme, and
still needs host cooperation for lifecycle.

**Tauri's HTTP plugin / custom protocol handler.** Rejected: custom protocol responses are
poor fits for server-streamed subscriptions, which are the majority of this app's traffic; the
`Channel` API is the supported streaming primitive.

**graphql-ws over a WebSocket to the host.** There is no WebSocket server in the host to speak
to; building one just to avoid `invoke` adds a protocol layer with no consumer.

## Consequences

The webview is fully sandboxed from the network, and the socket stays the single
authorization boundary. Transport and retry logic concentrates in `src/lib/graphql/client.ts`
and `subscribe-exchange.ts` — read those (and their tests) before touching it. A future web
deployment swaps the exchanges for `graphql-ws`/`graphql-sse` clients; the urql layer above
and the connection-status registry (see
[ADR: transport-status generation](2026-08-09-transport-status-generation.md)) are designed to
port across that swap.

Tests must mock the bridge (`mockTauriCore()` from `@/test-utils`) rather than any HTTP layer.
