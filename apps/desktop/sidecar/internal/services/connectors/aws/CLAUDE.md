# internal/services/connectors/aws — the AWS connector

**AWS's developer-tool sign-in, the flow `aws login` runs**: authorization code with PKCE on a
loopback redirect, under AWS's fixed client; the token endpoint answers SigV4 credentials and a
refresh token bound to the session's DPoP key. `Service` is an `aws.CredentialsProvider`; nothing
calls it yet. It shares `lib/oskeyring` and `lib/oauth/browser` with the engine, and imports
neither it nor `lib/oauth`.
→ [ADR](../../../../../docs/adr/2026-11-19-the-aws-connector-is-aws-login-with-a-dpop-bound-session.md),
[security record](../../../../../docs/security/2026-11-19-the-aws-connector.md).

- **Its keyring entry** is account `aws-session`, `{"v":1,"refresh","dpopKey","arn"}`.
- **While `Restoring`**, `Retrieve` waits.
- **Every change runs under one lock against one generation.** `Connect`, `Cancel`, `Disconnect`
  and a session's end bump it; tokens that arrive for an ended generation are dropped.
- **A sign-in mints the session's key.**
- **The token endpoint is the SDK's `signin` client** (`token.go`), retrying nothing, with
  `dpopMiddleware` adding a fresh proof (`dpop.go`). An API error is `Refused`, anything else
  `Unavailable`.
- **`Retrieve` refreshes a minute before expiry, one at a time**, under the service's own bound.
  A rotated refresh token is in the keyring before credentials are handed out. An
  `AccessDeniedException` or `InvalidGrantException` ends the session (`SessionEnded`).
- **`Disconnect` revokes nothing.**
- Every token, code verifier and state is registered with `safe.AddSecret` and forgotten when
  cleared, `Close` included; a sign-in's state and verifier before its outcome is published.
  Tests run against a fake AWS sign-in that checks each proof and binds each refresh token to its
  key (`testutil_test.go`).
- **Not yet checked against AWS**: the assumptions `docs/TODO.md` lists.
