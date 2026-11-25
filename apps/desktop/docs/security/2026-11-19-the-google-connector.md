# Security record — the Google connector, 19 November 2026

**Subject:** the sidecar signs in to Google with OAuth and keeps a refresh token for the
`cloud-platform` scope, which acts as the user on every Google Cloud project they can reach. Its
access tokens sign Vertex AI's OpenAI-compatible endpoint in one project. The design is
[the Google connector signs Vertex AI in the user's project](../adr/2026-11-19-the-google-connector-signs-vertex-ai-in-the-users-project.md).
The living model is [security-model.md](../security-model.md).

## What changes

- A new credential: a Google refresh token with the scopes `openid`, `email`, `profile` and
  `cloud-platform`, and its access tokens, held by the sidecar under the OAuth engine.
- New egress: `accounts.google.com` (authorize, in the browser), `oauth2.googleapis.com` (token and
  revocation), `www.googleapis.com/oauth2/v3/certs` (the JWKS), and a chat's transcript to
  `[<location>-]aiplatform.googleapis.com` under the bearer, as boundary 5 says of any provider.
- New inputs: `GOOGLE_CLOUD_PROJECT`, `CLOUDSDK_CORE_PROJECT`, `CLOUDSDK_CONFIG`,
  `CLOUDSDK_ACTIVE_CONFIG_NAME`, `GOOGLE_CLOUD_LOCATION` and gcloud's configuration file, read in
  every build after the login shell's import. `GOOGLE_CLOUD_PROJECT`, `GOOGLE_CLOUD_LOCATION` and
  `CLOUDSDK_ACTIVE_CONFIG_NAME` join the macOS shell-environment allowlist, as `plain`.
- New GraphQL: `googleConnection`, `googleConnectionWatch`, `googleConnect`,
  `googleConnectCancel`, `googleDisconnect`.

## What holds

- **The tokens are the engine's**: the refresh token in the OS keyring (`google-refresh-token`),
  the access token in memory, each registered with `safe` and in no log line, file, row or GraphQL
  field (`TestNoTokenLeavesTheKeyring`). `GoogleConnection` binds to `connectors.State`.
- **The bearer reaches the project's Vertex AI endpoint alone**: `Sign` covers its base URL and
  nothing else, and follows no redirect (`TestTheBearerGoesNowhereElse`, `TestNewSignsTheConfiguredBase`).
- **The environment cannot move the bearer off Google.** The project and location must be
  well-formed ids, or no endpoint is built (`TestBaseURLRefusesWhatIsNotAProjectOrLocation`).
- **The endpoints are written in code**, so no document can point a code or a token elsewhere
  (`TestAnInstalledAppSignsInUnderItsWrittenEndpoints`).
- **No sign-in is kept without `cloud-platform`**, and the ID token's nonce and audience are
  verified (`TestASignInWithoutTheRequiredScopeKeepsNoTokens`, `TestVerifyChecksTheNonce`).
- **The webview holds no token and opens no URL**; no mutation takes an address
  (`TestGoogleMutations`).

## Not yet checked against Google

Kubetail has no Google OAuth client yet. The rest is built on Google's documentation: the token
endpoint takes the client secret in the form and ignores no `resource`; the authorize endpoint
takes `nonce`, `access_type=offline` and `prompt=consent`; the token response's `scope` lists
`https://www.googleapis.com/auth/cloud-platform` when granted; a revoked or expired grant refreshes
`invalid_grant`; and Vertex AI's OpenAI-compatible endpoint serves `google/<model>` under the
bearer. `docs/TODO.md` lists the checks.

## Residual risk

- `cloud-platform` is broad: the refresh token reaches every project the user can, not only the one
  Kubetail signs. Google offers no narrower scope for Vertex AI's endpoint.
- The client secret ships in the app and is no secret; anyone can run a sign-in under Kubetail's
  client. It authorizes nothing without the user's consent.
- A revocation Google does not confirm leaves the grant live until the user removes Kubetail from
  their Google Account. The row says so.
