---
title: A connector is an OAuth session or a granted key
date: 2026-11-19
scope: sidecar
status: Accepted
amended_by:
  - [A connector is one package, and its machinery is internal](2026-11-19-a-connector-is-one-package-and-its-machinery-is-internal.md)
  - [A provider's credential follows one order](2026-11-20-a-providers-credential-follows-one-order.md)
---

# A connector is an OAuth session or a granted key

## Context

`services/connectors/internal/oauthconn` runs Sign in with ChatGPT: a client registered at the first
sign-in, discovery, a refresh token that rotates, an ID token, revocation, and a row in `app.db`
matched to a keyring entry ([an OAuth connection is a row matched to its keyring
entry](2026-11-18-an-oauth-connection-is-a-row-matched-to-its-keyring-entry.md)).

OpenRouter's sign-in is PKCE alone: no client, no issuer document, no refresh token. Its exchange
answers an ordinary API key that does not expire. The `openrouter` row is already keyed by
`OPENROUTER_API_KEY`, on one dialect (Chat Completions, a bearer).

[A provider's credential is a key or a signer](2026-11-18-a-providers-credential-is-a-key-or-a-signer.md)
refused a key as one more `Signer`, because a key's header follows the model's dialect.

## Decision

**A connector is one of two kinds, and its package says which.** An OAuth session is an
`oauthconn.Provider` value. A sign-in that grants a key is a service of its own beside
`services/connectors/openrouter`, sharing `lib/oskeyring` and `lib/oauth/browser`.

**The granted key is a session signer.** `openrouter.Service` implements `llm.SessionSigner`:
`Sign` sets `Authorization: Bearer <key>` on the API base alone, and `Bind` is dead once the
generation it began under ends. This is correct only because the `openrouter` row is one dialect
and no entry overrides it, which `catalog_test.go` pins.

**One store: an entry present is connected.** The key is one keyring entry,
`{"v":1,"key":…}`, under account `openrouter-api-key`. There is no `app.db` row: OpenRouter has no
registration to outlive a sign-out, and the label is a constant.

**The row keeps its place among the keyed providers.** A keyed provider may name a connector.
With no key but that connector's signer, `catalog.New` lists the row in its place, signed, with an
empty catalog that `catalogsync` fills while connected. A key from the environment wins.

**A `401` is confirmed before the key is dropped.** Kubetail cannot revoke the key, so dropping one
OpenRouter still honours would strand it. `GET /api/v1/key` decides: its `401` drops the key, any
other answer keeps it.

## Alternatives considered

- **Widen `oauthconn.Provider` to a flow with no issuer, client or refresh token.** Every path in
  the engine would gain a branch for a provider that has none of what it manages.
- **A row in `oauth_connections` beside the entry.** The row exists to keep a registration across
  a sign-out; OpenRouter has none, and two stores would be two things to keep in step.
- **Set the connector's key on `Provider.Key` and rebuild the catalog on a connect.** `llm.Service`
  holds a fixed list, and a key fixed at launch cannot follow a sign-out.

## Consequences

- The next provider that grants a key adds a service like `openrouter`, not an `oauthconn`
  provider.
- A connector-signed keyed row must stay on one dialect with a bearer. A model entry that states
  its own dialect or base URL breaks the signer's header.
- A key OpenRouter issued that Kubetail dropped, after a Cancel or a failed write, is live and in no
  store; the user deletes it by its label.

## Revisit when

OpenRouter offers a key a user key can revoke, or the `openrouter` row gains a second dialect.
