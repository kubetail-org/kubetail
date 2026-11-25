---
title: Build every service into one runtime, in order, and never hand it to a service
date: 2026-10-06
scope: sidecar
status: Accepted
---

# Build every service into one runtime, in order, and never hand it to a service

## Context

`app.New` built each service as a local variable, passed each one to the constructors after it,
and then copied them again into `graph.Resolver`, the gRPC server and `App.parts`. A failure
partway closed a hand-kept `built` list, separate from `parts`, so the order of construction, the
order of start and close, and the set closed on failure were three lists kept in step by hand.
Tests had no way to reach a built service, and the tool box took six positional arguments that
most tests passed as `nil`.

## Decision

`internal/app` builds a `Runtime`: one field per service, in build order. `build` fills it top to
bottom and appends each service's `lifecycle.Part` as the service is made, so start order is build
order and a failed constructor closes exactly what was built before it, through the same
`Runtime.Close` that ends a normal run. `New` is `build` plus the two servers: the runtime fills
`graph.Resolver` (`resolver()`) and hands `grpcserver.NewServer` the two services it takes.

Services never receive the runtime. Each still declares what it needs as constructor arguments,
narrowed to an interface in its own package. The runtime lives in `app`, which imports `graph`,
which imports the services, so a service importing it is an import cycle the compiler refuses.

An edge from an earlier service to a later one is a setter called once both exist, at the end of
`build`. A part that runs through such an edge is added there too, so it closes before the
service it reads (the bash tool's executable probe reads the chat service's folders).

## Alternatives considered

**Hand the runtime to every service.** A service would read what it needs off one struct. That is
a service locator: the dependency graph leaves the constructor signatures, a test must build the
whole runtime to build one service, and an edge to a service built later compiles and fails at
run time instead.

**A separate `runtime` package that `graph` and `grpc` take.** About fifty graph tests build a
`graph.Resolver` from fakes, field by field, and the gRPC server needs two services. Both would
take a struct holding everything to use a fraction of it, and the separate package would need a
convention, not the compiler, to keep services from importing it.

## Consequences

Adding a service is one field in its place, one constructor call, and one `add` if it has a
lifecycle. The field order is a rule a reader can check, not one a test pins: a field out of order
compiles. A backward edge is visible as the one block at the end of `build`.
