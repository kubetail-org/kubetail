---
title: Serve GraphQL and gRPC over one socket with the standard library's h2c
date: 2026-10-26
scope: cross-cutting
status: Accepted
---

# Serve GraphQL and gRPC over one socket with the standard library's h2c

## Context

The host and sidecar share one channel: a per-instance Unix domain socket (named pipe on
Windows), user-restricted, with no TLS to negotiate protocols over. It carries the webview's
GraphQL traffic (queries, mutations, SSE subscriptions — HTTP/1.1), forwarded by the host, and a
host-internal control channel (the tray's auth-state watch, login/logout, the resync poke) on
gRPC, which needs HTTP/2.

single-socket-h2c multiplexed the two with
`h2c.NewHandler` from `golang.org/x/net/http2/h2c`. That package is now deprecated: since Go 1.24
`net/http` serves unencrypted HTTP/2 itself. `h2c.NewHandler` also hijacks each HTTP/2
connection, so `http.Server.Shutdown` never saw the gRPC streams, and the sidecar carried its own
drain (`DrainWithContext`, a WaitGroup per transport) to wait them out.

## Decision

One socket carries both, served by `net/http` alone. `main.go` gives its `http.Server`
`app.Protocols()`: HTTP/1.1 plus unencrypted HTTP/2. The handler in `internal/app` dispatches on
`grpcserver.IsGRPCRequest`: HTTP/2 `application/grpc` goes to the gRPC server, everything else to
the GraphQL mux. The predicate lives in `sidecar/grpc/server.go` because it *is* the definition of
a gRPC request; the composition root owns the topology. tonic on the host dials with HTTP/2 prior
knowledge, which is the only unencrypted HTTP/2 the standard library accepts.

The cluster surface stays **GraphQL-only**; gRPC carries only what the host itself consumes.

Shutdown (`sidecar/shutdown.go`) is `Shutdown` → `CloseRequests` → `WaitRequests` → `stop` →
`Close`. Streams end at once: `Shutdown` stops listening and then runs `CancelStreams`, which ends
gRPC watches and SSE subscriptions with their normal terminal frames (OK trailers,
`event: complete`). A stream never finishes on its own, so waiting for one is pointless. A unary
request does finish on its own, and once the sidecar is shared by the CLI and the TUI, a person
in another terminal may be waiting on it, so `Shutdown` gives requests a 10 s grace
(`requestGrace`). The streams' cancel is per-request, never `http.Server.BaseContext`: a
`BaseContext` cancel would tear down the shared connection carrying gRPC mid-stream.

`Shutdown` counts connections and HTTP/2 streams, not handlers: a stream the client reset leaves
its count while the handler runs on. So after every `Shutdown`, timed out or not, the app closes
its gate (a request after it gets a 503), cancels every handler still running, and joins them
through a counter before `stop` stops the services under them. Past the grace, the join, the
drain and the close have no deadline of their own; one watchdog bounds the whole stop at 20 s
(`shutdownBudget`, the grace plus 10 s, so a request that holds up the stop for the whole grace
still leaves the services 10 s), logs every goroutine's stack, and exits 1. A wedge gets a stack
trace, not a number. The host's kill (`SIDECAR_SHUTDOWN_GRACE`, 23 s) sits above the budget, as
a backstop.

`TestAppShutdownEndsBothTransports` pins that subscriptions do not hold `Shutdown` up: with an
SSE subscription and a gRPC watch open, it returns at once and the watch ends with OK trailers.
`TestShutdownEndsWatchesAndFinishesCalls` pins that a call in flight is waited on while a watch
on the same connection ends. `TestShutdownReturnsWhileAResetStreamsHandlerRuns` pins why the join
follows every `Shutdown`.

## Alternatives considered

**Keep `x/net`'s `h2c.NewHandler`.** Rejected: deprecated, and its hijack is what made the
sidecar's own drain necessary.

**Keep `DrainWithContext` beside `Shutdown`.** Rejected: once `Shutdown` tracks the HTTP/2
streams it waits on everything the drain waited on, against the same deadline. The handlers it
misses are joined by the app's request counter instead.

**A deadline per step after the grace.** Rejected: each step is a cancel-and-wait that returns at
once when its work honours its cancellation. A deadline would only turn a wedge into a number;
the watchdog's stack dump says what was stuck.

**Two sockets, one per protocol.** Rejected: doubles the lifecycle surface (two binds, two
readiness signals, two things to clean up per instance) for no isolation benefit — both ends are
the same two processes on the same machine.

**gRPC for everything, including the webview surface.** Rejected: the webview consumes GraphQL
through urql, and gqlgen's schema is also the frontend codegen source of truth. Rebuilding that on
gRPC-web adds a proxy layer for zero gain on a local socket.

**GraphQL for everything, including the host control channel.** Workable, but the host is Rust: a
typed proto contract (`proto/auth.proto`, `proto/poke.proto`) compiled by `tonic-prost-build`
gives the tray a generated client, where GraphQL would mean hand-written Rust types mirroring the
schema.

## Consequences

One socket to bind, ready-signal, and tear down. A streaming handler must return when
`CancelStreams` fires, or `Shutdown` runs to the end of the grace. A handler or a loop that
ignores its cancellation ends the process at the budget, with exit code 1 and its stack in
`sidecar.log`. Nothing may cancel through `BaseContext`. A client must dial HTTP/2 with
prior knowledge; an `Upgrade: h2c` request is served as HTTP/1.1.

## Revisit when

A web (non-Tauri) deployment needs the GraphQL surface on TCP with TLS — ALPN then does the
protocol split natively and h2c stops being load-bearing.
