# internal/services/connectors/github — the GitHub connector

**GitHub's device flow under Kubetail's OAuth App, whose token the sandbox's GitHub proxy sends.**
GitHub's exchange needs a client secret a desktop app cannot keep, so this is no `oauthconn`
provider: it is a granted key, like `openrouter`, and shares `lib/oskeyring` with it.
→ [ADR](../../../../../docs/adr/2026-11-19-the-github-connector-signs-in-with-githubs-device-flow.md),
[security record](../../../../../docs/security/2026-11-19-the-github-connector.md).

- **One store: an entry present is connected.** Account `github-token`, `{"v":1,"token","login"}`.
  The restore reads it and reaches no network. A newer `v` is left in place; an entry that does
  not parse is deleted; a keyring that cannot be read writes nothing.
- **`New` is `Restoring` and makes no keyring call**; the restore reads on a goroutine. Until it
  publishes, `Token` waits (`Tokens` reaches `gh` only once signed out) and every change answers
  `ErrRestoring`. `Close` leaves a keyring call that never answers behind.
- **`Connect` is refused with no client id** (`connectors.ErrNoClient`), during an attempt, while connected
  and in `Failed{NotDeleted}`. It checks the keyring, asks for a code, publishes `SigningIn` with
  the `UserCode`, and opens the browser only on a page on `AuthURL`'s origin. It returns once the
  browser opens; polling runs on a goroutine.
- **Polling follows RFC 8628**: `authorization_pending` waits, `slow_down` adds 5 s or takes
  GitHub's `interval`, `access_denied` is `Declined`, `expired_token` and the code's lifetime are
  `TimedOut`. `pace` turns GitHub's seconds into a duration; tests pass milliseconds.
- **A granted token is kept only once `/user` names its login**, and only for the generation it
  began under. The device code is registered with `safe` while it polls, and forgotten before the
  outcome is published.
- **`Tokens(fallback)` is the proxy's source**: the connector's token while connected, `gh`'s
  otherwise. `Refused` checks `/user` once at a time; only its own `401` drops the token
  (`KeyRejected`).
- **`Disconnect` drops the token from memory first**, then deletes the entry; the token stays
  authorized on GitHub. A refused delete is `Failed{NotDeleted}` and returns the error.
- Test seams are unexported options; tests run against a fake GitHub (`testutil_test.go`) whose
  tokens are not `gho_`-shaped, since `safe` blanks that shape unregistered.
