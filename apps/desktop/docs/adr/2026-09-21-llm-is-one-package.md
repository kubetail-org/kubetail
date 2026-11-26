---
title: The llm package wires itself, in one package
date: 2026-09-21
scope: sidecar
status: Accepted
amended_by:
  - [Every tool the model can call is in one box, and what it is follows from what it implements](2026-09-24-every-tool-is-in-the-box.md)
  - [The catalog is its own package, and lists every tool each provider is offered](2026-09-24-the-catalog-is-its-own-package.md)
---

# The llm package wires itself, in one package

## Context

`internal/llm` held the vocabulary of a model call — `Request`, `Response`, `Block`,
`Provider`, `Model` — and the `Service` that resolves a send onto a provider, a model and an
adapter. The adapters lived under it as `llm/dialect/messages` and `llm/dialect/fake`, and the
catalog rows as `llm/provider`. Each sub-package imported `llm` for the vocabulary, so `llm`
could not import them back, and the composition — one adapter per dialect, the rows the config
read — had to happen above both, in `app.go`. That forced two things into the public surface
that nothing outside the package had a reason to name: `llm.Adapter`, the seam the adapters
implement, and `NewService(map[Dialect]Adapter, ...Provider)`, the constructor that takes it.
Every test that needed a service repeated the wiring, and three of them hand-rolled adapters
to reach behaviour the fake already had or could have.

## Decision

`internal/llm` is **one package**. Each dialect is a `dialect_*.go` file holding one function
that speaks its wire — `streamMessages(ctx, req, idle, emit)` — and each provider a
`provider_*.go` file of data, grouped by prefix rather than by directory. `New(Options)` wires
the package itself: the keyed providers whose key `Options.Keys` holds (by provider id, the
table `KeyVars()` publishes to `config.go`), and the fake provider after them when
`Options.AddFake` asks for it.

**A provider is data, and its dialect is the name that selects the code.** `Target.Stream`
is one `switch` on `Provider.Dialect`, calling that dialect's function with the request;
`Resolve` refuses a provider on a dialect the switch does not speak. There is no interface
between them and no field of code on a provider — except the fake, whose code is a value a
test steers, so `Provider.fake` holds it and `FakeProvider(f)` is the one constructor that
sets it. `Provider.Fake()` hands it back, so a test steers the one its service answers on,
and `Fake` is the only name a consumer needs: the dev seam and the test seam are the same
thing.

The invariant that a model SDK is imported by a dialect alone becomes a rule over a file
glob — *only a `dialect_*.go` file imports a model SDK* — pinned by
`TestOnlyAWireFileImportsAModelSDK`, which parses the package's files and refuses any
other importer of a path in `sdkPaths`.

## Alternatives considered

- **Keep the sub-packages and add a builder above them** (`internal/llmsvc`, holding `Service`
  and `New`). Cleans up `app.go`, and the seam can be a private interface there since Go's
  interfaces are structural. But the test seam is then gone with nothing in its place, and
  every consumer imports two packages for one idea.
- **Move the vocabulary to `llm/internal/core` and re-export it from `llm` by type alias.**
  Lets `llm` import its own dialects with the seam hidden under `internal/`. The price is a
  forwarding file for every type, function and constant the vocabulary holds, kept in step by
  hand, so that callers can keep spelling `llm.Block`.
- **A registry the dialects fill from `init()`**, `database/sql` style, with blank imports in
  `app.go`. Global mutable state and import-order semantics for four dialects, and the
  registration function still takes an exported seam.
- **An unexported interface, one value per dialect.** Two shapes of it were built and
  replaced. First a `map[Dialect]adapter` on the service, joining a provider's dialect to its
  value at `Resolve`; then the value on the provider itself, set by its constructor, so the
  map had nothing to join. Both kept a thing that needed a name — adapter, client, driver —
  and none of the names was right, because the thing was neither a client (it held no
  endpoint; the provider does) nor a driver in the `database/sql` sense. The value on the
  provider also made a provider carry code, which a provider typed into Settings cannot, and
  made Bedrock a constructor per platform in every dialect file rather than a field the
  dialect reads. A `switch` on the dialect's name is what both shapes were computing; writing
  it once, in `Target.Stream`, deletes the concept instead of naming it.

The package boundary bought one thing: the compiler enforced that only a dialect imports an
SDK. A twenty-line test enforces the same rule over file names, and unlike the boundary it
catches a dialect file added under the wrong name.

## Consequences

Adding a dialect is one `dialect_*.go` file, one `case` in `Target.Stream` and in `spoken`,
and its SDK's path in `sdkPaths`. Adding a provider is one `provider_*.go` file of data and
one entry in `keyedProviders()`; a platform that authenticates differently (Bedrock's SigV4)
is a field on the provider that its dialect's function reads. `app.go` builds the service in
one call and holds nothing of the catalog; `config.go` reads `KeyVars()` and never sees a
`Provider`. A provider written in Settings later is the same struct, since nothing on it is
code.

The obligations: the two switches in `service.go` name the same dialects, and
`TestEveryKeyedProviderResolves` catches a real provider's dialect missing from either; the
fake is the only scripted dialect, so behaviour a test needs from the wire — a failure after a
chunk, a gate, a panic — is a knob on `Fake`, never a second fake in a consumer's test; and a
service whose providers differ from what `New` builds is a white-box test inside `llm`, on
`newService`, since `Provider.fake` cannot be set from outside the package.
