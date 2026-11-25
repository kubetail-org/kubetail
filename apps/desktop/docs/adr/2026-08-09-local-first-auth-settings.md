---
title: Sidecar-owned local-first auth; cloud settings sync depends on auth, not the reverse
date: 2026-08-09
scope: sidecar
status: Accepted
amended_by:
  - [Remove cloud settings sync until settings have a design](2026-10-06-remove-cloud-settings-sync.md)
  - [The Kubetail sign-in is a connector on the OAuth engine](2026-11-19-the-kubetail-sign-in-is-a-connector-on-the-oauth-engine.md)
---

# Sidecar-owned local-first auth; cloud settings sync depends on auth, not the reverse

## Context

User accounts are backed by kubetail-cloud's Hydra OAuth server; cloud-synced settings ride its
API. A desktop app must stay useful offline: a signed-in user should remain signed in, and a
settings edit should apply immediately and survive restart, whether or not the cloud is
reachable. The auth flow needs a browser and a redirect target; the tokens need durable,
secure storage.

## Decision

**The whole OAuth flow is sidecar-owned** (`internal/services/auth`): the sidecar opens the system
browser (auth-code + PKCE, ephemeral 127.0.0.1 loopback redirect), runs the loopback
listener, exchanges and verifies the code, and persists the refresh token in the OS keyring
itself (`keyringStore` over `zalando/go-keyring`). Loopback OAuth already pins the browser to
this machine, so there is no host round-trip and **no gRPC credentials channel**. Identity
comes from the verified ID token (go-oidc against Hydra's JWKS) — no profile round-trip.
Local-first: signed-in ⇔ a refresh token is present; identity reads back from the stored ID
token offline. The service degrades to signed-out when unconfigured. The OAuth2/OIDC protocol
layer is the one carved-out sub-package (`auth/oauth`, a leaf that must not import `auth`);
the root package stays flat, organized by file.

**Settings sync (`internal/services/cloud`) depends on `auth`, never the reverse**: it authenticates
from `authSvc.TokenSource` and wakes its engine by observing `authSvc.Subscribe()` — tracking
only the `Authenticated` bit, so a routine token refresh is a non-event. Edits are
local-first: applied to a local JSON file immediately, queued durably
(`mutationqueue`, atomicjson-backed FIFO surviving restart), drained to the cloud when live;
incoming snapshots get still-pending patches re-layered on top (`prefs.Merge`) so a stale
cloud snapshot can't clobber an unacked local edit. `Settings` uses pointer fields +
omitempty so absent ≠ cleared under the cloud's deep-merge.

Both subsystems use the same construction pattern: exported `New` takes only production
knobs; test seams are **unexported functional options** on an unexported `newWithOptions`
builder, reachable only from white-box tests — nothing on the public config can bypass the
real flow. External consumers fake the `Service` interface instead.

## Alternatives considered

**Host-owned auth (Rust runs the flow, passes tokens over gRPC).** Rejected: the sidecar is
the token consumer (cloud API calls), so host ownership adds a credentials channel — the
most sensitive data on the wire — purely to move the loopback listener to the wrong process.

**Webview-embedded login.** Rejected: OAuth providers actively break embedded webview logins,
and the system browser carries the user's existing session and password manager.

**Online-checked sessions.** Rejected: signed-out-when-offline is wrong for a desktop app;
the refresh token's presence is the honest local criterion.

**Cloud-authoritative settings (fetch-then-render).** Rejected: settings must work offline
and apply instantly; reconcile-with-pending-patches is strictly more robust than
last-write-wins fetch.

**`oauth2.ReuseTokenSource` for caching/refresh.** Rejected: it bakes a construction-time
context into refreshes; the hand-rolled grant keeps per-call `ctx` (bounded timeouts per
refresh), delegating only the HTTP exchange to `golang.org/x/oauth2`.

## Consequences

Offline behavior is first-class in both layers. The dependency direction is an invariant:
`auth` must never learn about `cloud`. The token-source shape exposes the refresh token on
the returned `*oauth2.Token` — consumers read `AccessToken` only; the GraphQL projection
drops tokens entirely (`AuthState { authenticated, identity }`). Logout revokes fire-and-forget
(RFC 7009) after clearing locally — a keychain-write failure keeps you signed in with an
error rather than half-out.
