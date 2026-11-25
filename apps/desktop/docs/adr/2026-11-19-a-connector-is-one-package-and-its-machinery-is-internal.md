---
title: A connector is one package, and its machinery is internal
date: 2026-11-19
scope: cross-cutting
status: Accepted
amended_by:
  - [The Kubetail sign-in is a connector on the OAuth engine](2026-11-19-the-kubetail-sign-in-is-a-connector-on-the-oauth-engine.md)
  - [A provider's credential follows one order](2026-11-20-a-providers-credential-follows-one-order.md)
---

# A connector is one package, and its machinery is internal

## Context

With OpenRouter beside ChatGPT, `services/connectors` held four public packages named for
mechanisms: ChatGPT's OAuth provider, the OpenRouter key service, the OAuth engine (`oauthconn`)
and `catalogsync`. `app` and `graph` imported all four, and named `oauthconn` wherever
they meant the ChatGPT connector. Each connector declared its own `Phase`, `Failure`, `State` and
refusals, the wire carried four enums for them, and the webview kept two failure tables. The two
signers carried the same lines for the API-base check and the dead binding.

## Decision

**A connector is one public package**: `services/connectors/chatgpt` and
`services/connectors/openrouter`. Each has one surface: `New`, `State`, `Subscribe`, `Connect`,
`Cancel`, `Disconnect`, `Close`, `SyncCatalog`, and `llm.SessionSigner`. `chatgpt.Service` is the
engine under ChatGPT's provider.

**The machinery is internal**: `connectors/internal/oauthconn` (the engine, moved whole),
`connectors/internal/catalogsync`, and `connectors/internal/signer` (the API-base check and the
dead binding). The compiler keeps everything outside `services/connectors` to the two connectors
and the root package.

**One vocabulary.** The root package `connectors` declares `Phase`, `Failure`, `State` and the
refusals of `Connect` and `Disconnect`. The wire has one `ConnectorPhase` and one
`ConnectorFailure`, each value named as Go names it, and the webview one failure table over the
whole enum. Each connector keeps its own GraphQL type, watch and mutations.

This amends [a connector is an OAuth session or a granted
key](2026-11-19-a-connector-is-an-oauth-session-or-a-granted-key.md): a connector's kind no longer
shows in its package's name. The two kinds, the key as a session signer, the one store and the
confirmed `401` stand.

## Alternatives considered

- **Split the engine, its domain-free code joining `lib/oauth`.** `lib/oauth` already holds the
  protocol with no Kubetail domain. What `oauthconn` adds to each step is state (the discovery cache,
  the `oauth_connections` row, a refresh against a generation), so nothing more could leave.
- **One connection type, watch and set of mutations for every connector.** Their fields and actions
  differ (`registered`, `chatGPTConnect(fresh:)`, `openRouterKeysOpen`); one type would be the
  union, with fields null for one connector.
- **Keep each connector's enums on the wire.** Four enums for one set of values, renamed in places
  (`PlanUsageNotGranted`), with a table per connector in the webview.

## Consequences

- A second OAuth connector is a package beside `chatgpt`: its `Provider` and a `Service` over the
  engine.
- The Kubetail sign-in, under `services/auth`, cannot import the engine. Moving it onto the engine
  moves the engine first.
- A failure added in Go fails the webview's build until the table has its line.
- A connector's wire type admits phases and failures it never answers.
