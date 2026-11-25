# Security record — the Kubetail sign-in on the engine, 19 November 2026

**Subject:** the sidecar's sign-in to Kubetail cloud moves from `services/auth` onto the OAuth
engine (`services/connectors/internal/oauthconn`), as the connector `services/connectors/kubetailcloud`.
Written before the code, as the design is
[the Kubetail sign-in is a connector on the OAuth engine](../adr/2026-11-19-the-kubetail-sign-in-is-a-connector-on-the-oauth-engine.md);
the plan was the Kubetail Cloud connector spec ([#1057](https://github.com/kstackhq/kstack/issues/1057)). The living model is
[security-model.md](../security-model.md).

## What changes

- The Kubetail refresh token moves to a new keyring account, `kubetail-cloud-refresh-token`. The old
  account, `auth-token`, is deleted at every launch.
- The account's subject, email and name are kept in `app.db`'s `oauth_connections` row `kubetail-cloud`,
  beside a session id. No token is in the row.
- The engine's `Provider` takes a fixed client id, and an engine with no API base signs nothing.

## What holds

- **The keyring holds the refresh token alone**, account `kubetail-cloud-refresh-token`, as
  `{"v":1,"token","session"}`. The access and ID tokens are memory's. Before, all three were in
  the entry.
- **The ID token's nonce is checked.** Before, it was not sent.
- **No token is logged**: each is registered with `safe.AddSecret`.
- **The Kubetail credential reaches no request.** The package exports no signer, the provider has
  no API base, and the engine signs nothing without one (`TestNoAPIBaseSignsNothing`). A connector
  that hands credentials to tool runs never includes this one.
- **Sign-out is local first**, and a failed keyring delete no longer keeps the session.
- **Revocation is not a promise.** It is tried twice while the app runs and cancelled by a quit,
  so a signed-out token can stay live at the issuer until it expires.

## Not yet checked against the issuer

The spec's §0 asks four questions of `oauth.kubetail.com` (the issuer's exact value, the revocation
endpoint, the nonce's echo, the code a dead refresh token answers). The work proceeds on the
assumption that each answers as the spec expects. Each is checked once by hand before the work
reaches `main`.

## Residual risk

- The old entry's refresh token is not revoked, so it stays live at the issuer until it expires.
  Once the entry is deleted it is in no store; while the delete keeps failing it is still in the
  keyring, as reachable as before the update.
- A session nothing signs with is never refreshed, so the app can show an account whose refresh
  token the issuer has already expired.
