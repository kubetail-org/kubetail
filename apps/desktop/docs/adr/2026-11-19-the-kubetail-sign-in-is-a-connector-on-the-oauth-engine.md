---
title: The Kubetail sign-in is a connector on the OAuth engine
date: 2026-11-19
scope: sidecar
status: Accepted
---

# The Kubetail sign-in is a connector on the OAuth engine

## Context

The sidecar had two OAuth sign-ins. `services/auth` signed in to Kubetail cloud;
`services/connectors/internal/oauthconn` was the engine under the ChatGPT connector. Both ran an
authorization code with PKCE on a loopback redirect, kept a refresh token in the OS keyring,
refreshed lazily and revoked on sign-out, each with its own code.

The engine had what `services/auth` lacked: a nonce on the ID token, the refresh token alone in the
keyring, every token registered with `safe.AddSecret`, a refresh under the service's own bound, a
keyring check before the browser opens, goroutines joined on `Close`, and a failure the state
carries. It assumed a client the provider registers at the first sign-in, a provider whose service
is a model provider, and one API base to sign. The Kubetail sign-in has a fixed client id, is no
model provider, and nothing reads its token yet. An installed Kubetail (`desktop/v0.0.1`) keeps the
whole token as JSON under the keyring account `auth-token`. The design was the Kubetail Cloud
connector spec (#1057), answering #1046.

## Decision

**A connector is an account on a service that Kubetail signs in to, and whose requests it may then
sign.** A model provider is one case. ChatGPT and OpenRouter have a catalog row and `SyncCatalog`
because their service is a model provider, not because they are connectors. The surface every
connector has is `New`, `State`, `Subscribe`, `Connect`, `Disconnect` and `Close`, over
`connectors`' vocabulary; `Cancel`, `SyncCatalog` and `llm.SessionSigner` belong to the connectors
that need them.

**The Kubetail sign-in is a connector, `services/connectors/kubetailcloud` (`Provider.ID`
`kubetail-cloud`), run by the engine.**
`services/auth` is deleted. The engine's `Provider` gains `ClientID`, a client fixed in code, for
which it registers none and leaves the row's `client_id` NULL. `Provider.ID` is the row's key in
`oauth_connections` and the keyring accounts' prefix; for a model provider it is also the catalog's
provider id. The connector has no API base and exports no signer, and an engine with no API base
signs no request. The wire (`authState`, `authLoginStart`, `authLogout`, the gRPC `AuthService`)
stays, mapped from the connector's state.

**The old entry is dropped, not converted.** `kubetailcloud.New` deletes `auth-token` at every launch and
does not revoke its refresh token. The user signs in once more after the update.

This amends:

- [local-first auth](2026-08-09-local-first-auth-settings.md): the sign-in is the engine's, and
  degraded mode goes. The keyring service, issuer and client id all have defaults, and a machine
  with no keyring fails `NotSaved`.
- [an OAuth connection is a row](2026-11-18-an-oauth-connection-is-a-row-matched-to-its-keyring-entry.md):
  a row may belong to a provider whose client is fixed, and keeps the account's `name` beside
  `subject` and `email` (migration `0003`).
- [a connector is one package](2026-11-19-a-connector-is-one-package-and-its-machinery-is-internal.md):
  a connector's surface is the six methods above, not the model provider's whole surface.

## Alternatives considered

- **Share the small pieces and keep two services.** The keyring entry, the loopback and the
  discovery are already shared through `lib/oauth`. What would stay twice is the generation and
  ordering code, the part that drifts.
- **Move ChatGPT onto `services/auth`.** `services/auth` lacks most of what the engine checks, and
  ChatGPT's token is read on every turn while the Kubetail token has no reader.
- **Convert the `auth-token` entry.** It holds no session id and no row names it, so it would need a
  one-time path through the engine's restore for a token nothing reads. Signing in once more costs
  the user one browser round-trip.
- **Revoke the old entry's refresh token.** It needs discovery and the network at launch, for a
  token that is in no store once deleted.

## Consequences

- A sign-in to a service that is no model provider is a `Provider` with a fixed or registered
  client and no API base.
- The engine's tests run under a fixed-client provider beside the registering one.
- The old entry's refresh token stays live at the issuer until it expires. The delete is removed
  once no supported build wrote that entry (`docs/TODO.md`).
- A session nothing signs with is never refreshed, so the app can show an account whose refresh
  token the issuer has already expired.

## Revisit when

The Kubetail token gains a reader: it is then one API base, which the engine signs, and the connector
exports a signer for it.
