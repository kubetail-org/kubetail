# Security record — the AWS connector, 19 November 2026

**Subject:** the sidecar signs in to AWS with AWS's developer-tool sign-in, the flow `aws login`
runs, and keeps a refresh token that yields temporary credentials for the signed-in principal,
with every permission it has. The design is
[the AWS connector is AWS login with a DPoP-bound session](../adr/2026-11-19-the-aws-connector-is-aws-login-with-a-dpop-bound-session.md).
The living model is [security-model.md](../security-model.md).

## What changes

- A new credential: an AWS sign-in session — a refresh token and the P-256 key it is bound to —
  held by the sidecar (`services/connectors/aws`), and fifteen-minute SigV4 credentials in memory.
- New egress: the sidecar posts a sign-in's code and PKCE verifier, and later the refresh token,
  to `us-east-1.signin.aws.amazon.com/v1/token`, each with a DPoP proof.
- New GraphQL: `awsConnection`, `awsConnectionWatch`, `awsConnect`, `awsConnectCancel`,
  `awsDisconnect`.
- Nothing uses the credentials yet.

## What holds

- **The session is in the OS keyring**, service `Config.KeychainService`, account `aws-session`,
  as reachable as the ChatGPT refresh token: any process of the user can ask the keyring for it,
  and a sandboxed command cannot reach the keyring. The refresh token alone is not enough: it is
  bound to the key beside it.
- **No token, secret key, code or verifier is logged, projected, stored in `app.db` or written to
  a file.** Each is registered with `safe.AddSecret` on receipt and forgotten when cleared;
  `AWSConnection` binds to `connectors.State`, which holds none (`TestNoCredentialReachesTheLog`).
- **Every token request carries a fresh proof under the session's key**, naming the endpoint it
  is sent to (`TestAProofNamesTheRequestAndVerifiesUnderItsKey`, the fake's check in every
  sign-in and refresh test). The SDK retries nothing, so a refresh token is spent once.
- **The redirect is checked**: our `state` before anything else, exactly one `code`, on the
  listener this sign-in started, bound to `127.0.0.1` (`TestARedirectWithTwoCodesIsRefused`,
  `TestListenerRejectsInvalidCallbackWithoutConsuming`).
- **The webview holds no credential and opens no URL.** The sidecar opens the authorize page it
  built (`TestAWSMutations`; no mutation takes an address).
- **The keyring is checked before the browser opens** (`TestAKeyringThatCannotHoldASessionOpensNoBrowser`).
- **A rotated refresh token is in the keyring before credentials are handed out**
  (`TestARotatedTokenIsSavedBeforeCredentialsAreHandedOut`).
- **Tokens that arrive after a Cancel or a Disconnect are dropped, never saved**
  (`TestTokensThatArriveAfterACancelAreDropped`, `TestARefreshThatEndsAfterADisconnectIsDropped`).
- **A failed delete is not a sign-out.** The process forgets the session at once, `Disconnect`
  errors, and the row says it returns on restart until a retry deletes it
  (`TestADeleteTheKeyringRefusesIsNotDeletedUntilARetry`).

## Not yet checked against AWS

The connector follows the AWS CLI's flow and the SDK's model of the token endpoint. Each
assumption in `docs/TODO.md` is checked once by hand on a real account.

## Residual risk

- The ID token's signature is not checked; the ARN it names is display only.
- A disconnected session stays valid at AWS until it expires: Kubetail does not revoke it.
- A session whose delete failed is used again after a restart. The row says so.
- The credentials carry the principal's whole permission. Nothing uses them yet; a consumer
  brings its own record.
