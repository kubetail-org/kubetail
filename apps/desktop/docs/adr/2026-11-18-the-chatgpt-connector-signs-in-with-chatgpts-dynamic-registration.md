---
title: The ChatGPT connector signs in with ChatGPT's dynamic registration
date: 2026-11-18
scope: cross-cutting
status: Accepted
---

# The ChatGPT connector signs in with ChatGPT's dynamic registration

## Context

A user with a ChatGPT Plus or Pro plan wants to run Kubetail's agent on OpenAI models billed to the
plan, with no API key. OpenAI offers Sign in with ChatGPT for open-source and personal apps that
run locally: OAuth with PKCE, where OpenAI registers a client for each user during the first
sign-in. Requests go to the public Responses API with the access token as the bearer. Refresh
tokens rotate on every refresh.

## Decision

`services/connectors/internal/oauthconn` runs Sign in with ChatGPT's own flow, under the `Provider` that
`services/connectors/chatgpt` describes. The first sign-in names
`dynamic_agent_client` and saves the client id the callback returns before exchanging the code;
every later sign-in, refresh and revocation uses it. Each installation sends a stable host id.
The refresh token is in the OS keyring; the provider's row in `app.db` holds the registration
([ADR](2026-11-18-an-oauth-connection-is-a-row-matched-to-its-keyring-entry.md)). The access token lives in memory. Alongside:

1. **The sidecar opens the browser, and no mutation carries a URL.** Returning the authorize URL
   for the host to open needs a host command that opens a URL the webview supplies, a new
   capability for anything running in the webview. `services/auth` already opens the browser
   from the sidecar on a URL it built.
2. **No `id_token_hint`.** The ID token would ride the opener's command line, which other
   processes of the user can read. `login_hint` carries the email, so the ID token is not kept.
3. **A sign-in without plan permission keeps no tokens.** Kubetail has no use for the sign-in alone.
4. **One keyring entry, and the row says whether it is a session.** The registration is not a
   credential and must outlive a sign-out. A keyring entry and a row cannot be written as one,
   so the entry counts only while the row names its sign-in, and any other entry is deleted.
5. **An unsaved rotated refresh token hands out no access token.** Using it would keep the turn
   alive and lose the session at the next crash without a sign. Failing `Sign` shows the
   keyring's fault while the user can act on it.
6. **`Connect` while connected is refused.** Replacing a live session silently is how one
   account's catalog and another's token would meet.
7. **A turn is bound to its session, not cancelled with it.** Cancelling needs `agent/chat` to
   watch a connector; a bound signer fails the turn at its next model call with no new edge.
8. **A failed sign-in save revokes and starts over.** Holding live tokens to retry a write keeps a
   credential nothing has recorded.

## Alternatives considered

- **Codex's OAuth client.** It is OpenAI's own app's registration; borrowing it bills Kubetail's use
  to Codex and breaks when OpenAI changes it.
- **Reading `~/.codex/auth.json`.** It is another app's credential file, and reading it makes
  Kubetail's session whatever Codex last wrote.
- **Keeping the access token in the keyring.** It saves one refresh per launch and costs a second
  entry that can disagree with the first.

## Consequences

The sign-in is Kubetail's own registration, revocable in ChatGPT's settings. Plan usage is open to
open-source apps that run locally; if Kubetail ships paid features, it applies to OpenAI before
keeping this connector.

## Revisit when

OpenAI verifies the host id (a JWK thumbprint), or the route leaves preview.
