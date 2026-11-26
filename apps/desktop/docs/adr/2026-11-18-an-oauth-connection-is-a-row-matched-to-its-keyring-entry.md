---
title: An OAuth connection is a row in app.db matched to its keyring entry
date: 2026-11-18
scope: sidecar
status: Accepted
amended_by:
  - [The Kubetail sign-in is a connector on the OAuth engine](2026-11-19-the-kubetail-sign-in-is-a-connector-on-the-oauth-engine.md)
  - [A launch restores keyring credentials in the background](2026-11-20-a-launch-restores-keyring-credentials-in-the-background.md)
---

# An OAuth connection is a row in app.db matched to its keyring entry

## Context

Sign in with ChatGPT keeps two things across a restart: a refresh token, which belongs in the OS
keyring, and a registration (the issued client id, the account, whether a session exists), which
is not a credential and must outlive a sign-out. The two cannot be written as one. Whatever says
"a session exists" can disagree with the keyring after a crash, a failed delete, or a restore of
either from a backup.

ChatGPT is the first of several model providers the user will sign in to with OAuth.

`app.db` runs WAL with `synchronous=NORMAL`: a commit survives a crash of the process, and a power
loss can drop the last ones.

## Decision

**One table, `oauth_connections`, holds a row per provider**, keyed by the catalog's provider id.
`client_id`, `subject`, `email` and `session_id` are columns every provider fills; `provider_data`
is a JSON object in one provider's own shape (ChatGPT's host id). The next provider adds a row.

**Each sign-in mints a random session id, written to the keyring entry beside the refresh token
and to the row.** A session exists only when the two match, and a NULL `session_id` is signed
out. An entry that names no session the row holds is ignored and deleted at launch. The entry is
a versioned JSON object, `{"v":1,"token":…,"session":…}`.

**One engine runs every such sign-in.** `services/connectors/internal/oauthconn` holds the flow, the generations and
both stores, with no provider's name in it. A provider is a `Provider` value: its issuer, API
base, scopes, the scope a sign-in is kept for, its own authorize parameters and data, and the
token-endpoint codes that end a session. `services/connectors/chatgpt` is ChatGPT's. `Provider` holds
only what ChatGPT's flow needs, a client registered at the first sign-in on a loopback redirect;
a provider with another flow widens it then.

Correctness then needs no durable write. A lost commit, a crash between the two writes, a failed
keyring delete and a restored backup all leave a pair that does not match, which reads as signed
out.

## Alternatives considered

- **A JSON file per provider, written with an fsync of the file and its directory.** It orders
  the writes against a power loss, at the cost of a new synced writer and a file per provider. It
  does nothing for a restored backup, and a second provider repeats all of it.
- **A row in `app.db` and a boolean.** A power loss that drops a sign-out's commit after its
  keyring delete failed restores the session, and one that drops a sign-in's leaves a new
  account's token under the previous account's row.
- **`PRAGMA synchronous=FULL` around these commits.** The writer is one shared connection, so the
  setting slows every service's commits while it is on and stays on if the reset is missed.
- **A table per provider.** Every provider needs the same four columns.
- **A sign-in service per provider.** All but about eighty lines of ChatGPT's would be repeated.

## Consequences

One case still restores a signed-out session: a sign-out whose keyring delete failed and whose
commit a power loss dropped. `Disconnect` revokes the refresh token, so that session ends at its
first refresh.

A provider whose state does not fit the shared columns keeps it in `provider_data`, whose shape
changes by an `app.db` migration like any JSON column.

## Revisit when

A provider needs more than one account at a time: the key becomes the provider and the account.
