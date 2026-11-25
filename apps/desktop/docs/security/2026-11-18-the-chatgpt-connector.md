# Security record — the ChatGPT connector, 18 November 2026

**Subject:** the sidecar signs in with ChatGPT and keeps a refresh token that spends the user's
ChatGPT plan, to run the agent on OpenAI models with no API key. The design is
[the ChatGPT connector ADR](../adr/2026-11-18-the-chatgpt-connector-signs-in-with-chatgpts-dynamic-registration.md)
and [a provider's credential is a key or a signer](../adr/2026-11-18-a-providers-credential-is-a-key-or-a-signer.md).
The living model is [security-model.md](../security-model.md).

## What changes

- A new credential: a refresh token for `https://api.openai.com/v1`, with the plan permission
  `chatgpt.tokens.use.direct`, held by the sidecar (`services/connectors/oauthconn`, under `services/connectors/chatgptauth`'s provider). Its access token
  lasts an hour and lives in memory alone.
- New egress: the sidecar reaches `auth.openai.com` (discovery, the JWKS, the token and revocation
  endpoints) and `api.openai.com` (`/v1/responses`, `/v1/models`) under the plan's bearer.
- A new provider row, `chatgpt`, offered `ours`: the same tools every keyed provider is offered.
- New GraphQL: `chatGPTConnection`, `chatGPTConnectionWatch`, `chatGPTConnect`,
  `chatGPTConnectCancel`, `chatGPTDisconnect`, `chatGPTUsageOpen`, and `modelsWatch`.
- A new table in `app.db`, `oauth_connections`, a row per provider: for ChatGPT the
  installation's host id, the client id OpenAI issued, the account's subject and email, and the
  id of the sign-in its keyring entry must name. No token.

## What holds

- **The refresh token is in the OS keyring**, service `Config.KeychainService`, account
  `chatgpt-refresh-token`, as reachable as the cloud sign-in's token: any process of the user can
  ask the keyring for it, and a sandboxed command cannot reach the keyring (macOS denies it; on
  Linux the sandbox's filter opens no Unix socket).
- **A stored session is one the row and the keyring both name.** Each sign-in's random id is in
  the row and in the entry beside the refresh token; an entry naming any other sign-in, or none,
  is ignored and deleted at launch (`TestNewRestoresWhatTheStoresHold`). →
  [ADR](../adr/2026-11-18-an-oauth-connection-is-a-row-matched-to-its-keyring-entry.md).
- **No token is logged, projected, stored in `app.db` or written to a file.** Every token is
  registered with `safe.AddSecret` as it is read or received and forgotten once replaced or
  cleared. `ChatGPTConnection` binds to `oauthconn.State`, which holds no token.
  `TestNoTokenLeavesTheKeyring` reads the raw log and every file under the data directory
  after a sign-in, a refresh and a refused refresh.
- **The bearer goes to the API base alone, and follows no redirect.** `Sign` refuses a URL whose
  scheme, host, effective port or path prefix is not the base's
  (`TestTheBearerGoesNowhereElse`). Every wire's client refuses every redirect, and a
  `3xx` fails the call (`TestEveryDialectFollowsNoRedirect`); so does the models read
  (`TestReadChatGPTModelsFails`). The issuer's client refuses every redirect, so no form holding a
  code or a refresh token is sent twice (`TestTheIssuersClientFollowsNoRedirect`).
- **Discovery is checked.** The issuer is a constant; its document must name it exactly, and every
  endpoint must be on its origin (`TestDiscoverRefusesADocumentItCannotTrust`).
- **The sign-in is OAuth with PKCE (S256), a fresh state and nonce, and a loopback redirect** on
  `127.0.0.1` at a free port. The callback checks state before anything else
  (`TestListenerRejectsInvalidCallbackWithoutConsuming`). The ID token is verified: signature,
  issuer, audience (the issued client), expiry and nonce (`TestVerifyChecksTheNonce`). A sign-in
  without `chatgpt.tokens.use.direct` keeps no token (`TestASignInWithoutTheRequiredScopeKeepsNoTokens`).
- **A turn never signs under an account it did not start with.** A target holds a signer bound to
  its session; once that session ends it fails `signed out`, whatever session replaced it
  (`TestABindingEndsWithItsSession`, `TestABindingWithNoSessionIsDead`, `TestResolveBindsASessionSigner`,
  `TestResolveUnderKeepsTheParentsBindingOnItsProvider`).
- **A late result belongs to no session.** An exchange or a refresh that finishes after a Cancel,
  a Disconnect or a new Connect is revoked and never saved
  (`TestAnExchangeThatFinishesLateIsRevoked`, `TestDisconnectDuringARefresh`).
- **The webview holds no token and opens no URL.** The sidecar opens the browser on the authorize
  URL it built and on `https://chatgpt.com/settings/usage`, a constant; no mutation carries an
  address (`TestChatGPTMutations`).
- **The email is OpenAI's text**, drawn through `VisibleText`, and at rest in `app.db`
  beside the subject, owner-only like every file under the data directory.

## Residual risk

- A crash between the refresh token's keyring write and the row's leaves a
  token nobody revokes; the next launch deletes the entry, and the token expires in thirty days.
- A sign-out whose keyring delete failed and whose commit a power loss dropped returns at the
  next launch. Its refresh token was sent for revocation, so the session ends at its first refresh.
- A revocation OpenAI never confirms leaves the session live at OpenAI until it expires. The
  connector says so on its row and links to ChatGPT's settings.
- The host id is a UUID, not a key OpenAI can verify: anyone who reads `app.db` can claim to
  be this installation when signing in, which grants nothing without the user's sign-in.
