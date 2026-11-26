# internal/services/connectors/kubetailcloud — the Kubetail Cloud connector

**The Kubetail Cloud account, signed in to by the OAuth engine under a fixed client.** It backs the
wire's `authStateWatch`, `authLoginStart`, `authLogout` and the gRPC `AuthService`, and signs no
request: the engine is an unexported field, and the provider has no API base.
→ [ADR](../../../../../docs/adr/2026-11-19-the-kubetail-sign-in-is-a-connector-on-the-oauth-engine.md),
[security record](../../../../../docs/security/2026-11-19-the-kubetail-sign-in-on-the-engine.md).

Every data access here is named for its contract: [`docs/data-access.md`](../../../../../docs/data-access.md).

- **`Connect` cancels, then starts fresh**, so a second request opens a new tab and a sign-in may
  change accounts. A mutex runs one at a time, so overlapping requests (the avatar's, the tray's)
  run in turn and never answer `ErrSigningIn`. A failure `Start` answers is `Connect`'s error.
- **`WatchState` sends only a change of account**: restoring, connected, subject, email, name. The
  wire draws a sign-in under way, failed or cancelled as signed out. `State` is the engine's whole.
- Tests fake the engine (`engine`) and the keyring (`testutil_test.go`); the flow itself is
  `oauthconn`'s to test.
