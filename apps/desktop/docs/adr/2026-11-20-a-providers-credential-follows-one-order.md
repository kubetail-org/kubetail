---
title: A provider's credential follows one order
date: 2026-11-20
scope: cross-cutting
status: Accepted
amended_by: [Every provider is one row, signed by one service](2026-11-20-every-provider-is-one-row-signed-by-one-service.md)
---

# A provider's credential follows one order

## Context

A keyed provider's key reached Kubetail only through its environment variable, read once per launch.
The Providers pane drew a *Saved key* source it could not save. Two rules decided who signed a
provider: the OpenRouter connector stood aside whenever `OPENROUTER_API_KEY` was set ([a connector
is an OAuth session or a granted
key](2026-11-19-a-connector-is-an-oauth-session-or-a-granted-key.md)), and a `GEMINI_API_KEY` hid
the `google` provider ([the Google connector signs Vertex AI in the user's
project](2026-11-19-the-google-connector-signs-vertex-ai-in-the-users-project.md)). A key was
`llm.Provider.Key`, its header chosen by the model's dialect, and a provider with no key was not
listed at all.

## Decision

**A provider's credential follows one order, lowest to highest: environment key < saved key <
connector.** The highest one present signs. The order is per provider: a connector is in a
provider's order only when it signs that provider's own endpoint, which today is OpenRouter's alone.
`gemini` (the Gemini API) and `google` (Vertex AI) are two providers, so a Gemini key no longer
hides Vertex AI.

**Every keyed provider is signed by one service, `services/providercredentials`.** It holds each provider's
environment key and saved key (in the OS keyring, one entry per provider), knows the provider's
connector, and is the provider's `llm.SessionSigner`. Every keyed provider is in `llm.Service` from
launch and lists its written catalog only while it has a credential, so a saved key needs no
restart.

**A key's header is chosen by the request's base URL**, not by the model's dialect. Each provider
lists its routes, every base its models are sent to with the header a key goes in there
(`catalog.Providers`); a request under none of them is refused unsigned. This amends *a key as
one more `Signer`* in [a provider's credential is a key or a
signer](2026-11-18-a-providers-credential-is-a-key-or-a-signer.md): the dialect still picks the
header, once per route, through `llm.Provider.AuthHeader`.

**`signer` and `catalogsync` move to `services/internal`**, since a service that is not a connector
now needs them. This amends [a connector is one package, and its machinery is
internal](2026-11-19-a-connector-is-one-package-and-its-machinery-is-internal.md); `oauthconn`
stays under `connectors/internal`.

## Consequences

- A saved key is used unchecked: the eleven vendors share no check call. A `401` marks it rejected,
  and it keeps signing until the user replaces or removes it.
- The OpenRouter connector loses its `FromEnvironment` phase. Connect works whatever keys exist;
  Disconnect falls back to the saved key, then the environment key.
- A binding ends when the credential that signs its provider changes: a key's when the saved key
  changes or the connector connects, a connector's when its session ends. So a save or a sign-in
  mid-turn fails that turn *credential changed*, and a disconnect fails it *signed out*. A token or
  key rotated within a connector's session ends nothing.
- `llm.Provider.Key` is set by no production provider; its removal is a follow-up.
