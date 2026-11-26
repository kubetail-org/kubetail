# Sign-in, connectors and stored credentials

[← Security model](../security-model.md)

A credential stays in the keyring and in memory. No log line, file, `app.db` row or GraphQL field holds one. Each credential signs requests to its own provider's address alone, redirects included. A sign-in that finishes after a sign-out, a Cancel or a newer sign-in keeps nothing. These rows defend against a hostile callback, a redirecting or misbehaving issuer, and code that reads the log, the data directory or the GraphQL API. Code in the webview can call every mutation, so it can still replace a saved key. Several connectors are built and tested, and wait on live checks against the real provider.

## Protections

| ID | Protection | Status | Residual |
| --- | --- | --- | --- |
| CRED-1 | The loopback callback checks `state` before it consumes a code or an error. | **Enforced** | — |
| CRED-2 | A Kubetail sign-in that completes after a sign-out or a newer sign-in cannot restore the session, and its token is revoked. | **Enforced** | — |
| CRED-3 | The loopback callback bounds its reads. | **Enforced** | — |
| CRED-4 | A repeated error cannot park the loopback callback's handler. | **Enforced** | — |
| CRED-5 | The ChatGPT plan's tokens never leave the keyring and memory. No log line, file, `app.db` row or GraphQL field holds one. | **Enforced** | — |
| CRED-6 | The ChatGPT plan's bearer reaches `api.openai.com/v1` alone, redirects included, and the issuer's forms are never resent. | **Enforced** | — |
| CRED-7 | Sign in with ChatGPT verifies the ID token's nonce and audience, and keeps no token without plan permission. | **Enforced** | — |
| CRED-8 | A turn never signs under a ChatGPT session it did not start in. | **Enforced** | — |
| CRED-9 | A late ChatGPT exchange or refresh is revoked, never saved. | **Enforced** | — |
| CRED-10 | A rotated ChatGPT refresh token is in the keyring before its access token is used. | **Enforced** | — |
| CRED-11 | The webview opens no URL for the ChatGPT connector. The sidecar opens the authorize URL it built and one constant address. | **Enforced** | — |
| CRED-12 | The OpenRouter connector's key stays in the keyring and memory. | **Built**, not yet checked end to end | The source's status is "Built, not yet checked end to end": Enforced once the OpenRouter live checks in `TODO.md` pass. |
| CRED-13 | The OpenRouter connector's key signs `openrouter.ai/api/v1` alone. | **Built**, not yet checked end to end | Enforced once the OpenRouter live checks in `TODO.md` pass. |
| CRED-14 | The OpenRouter connector's key is dropped only once OpenRouter confirms it dead. | **Built**, not yet checked end to end | Enforced once the OpenRouter live checks in `TODO.md` pass. |
| CRED-15 | A saved provider key stays in the keyring and memory. No log line, file, `app.db` row or GraphQL field holds one. | **Enforced** | Code in the webview can still replace a key, as it can call every mutation. |
| CRED-16 | A saved provider key signs its row's base URLs alone. | **Enforced** | — |
| CRED-17 | The AWS connector's session stays in the keyring and memory. | **Built**, not yet checked end to end | The source's status is "Built, not yet checked end to end": Enforced once the AWS live checks in `TODO.md` pass. |
| CRED-18 | Every AWS token request carries a proof under the session's key. | **Built**, not yet checked end to end | Enforced once the AWS live checks in `TODO.md` pass. |
| CRED-19 | A rotated AWS refresh token is saved before credentials are handed out. | **Built**, not yet checked end to end | Enforced once the AWS live checks in `TODO.md` pass. |
| CRED-20 | AWS tokens that arrive after a Cancel or Disconnect are dropped. | **Built**, not yet checked end to end | Enforced once the AWS live checks in `TODO.md` pass. |
| CRED-21 | A disconnected AWS session is not revoked. | **By decision** | The session stays valid at AWS until it expires. |
| CRED-22 | The AWS ID token's signature is not checked. | **By decision** | — |
| CRED-23 | The GitHub connector's token stays in the keyring and memory. | **Built**, not yet checked end to end | The source's status is "Built, not yet checked end to end": Enforced once the GitHub live checks in `TODO.md` pass. |
| CRED-24 | The GitHub connector's token reaches a sandboxed command only through GitHub's proxy, and is granted only after the browser opened on GitHub's own origin. | **Built**, not yet checked end to end | Enforced once the GitHub live checks in `TODO.md` pass. |
| CRED-25 | The GitHub connector's token is dropped only once GitHub confirms it dead. | **Built**, not yet checked end to end | Enforced once the GitHub live checks in `TODO.md` pass. |
| CRED-26 | A disconnected GitHub token stays authorized on GitHub until the user revokes Kubetail there. | **By decision** | — |
| CRED-27 | The Google connector's bearer reaches Vertex AI in the user's project alone, under endpoints written in code. The environment's project and location cannot move it off `aiplatform.googleapis.com`. | **Built**, not yet checked end to end | The source's status is "Built, not yet checked end to end": Enforced once the Google live checks in `TODO.md` pass. |
| CRED-28 | Sign-in ID tokens are verified against JWKS and the authorize request's nonce. | **Enforced** for the check | — |
| CRED-29 | A restored account is display-only. | **Held by review** for authorization use | At launch the account is read from the `oauth_connections` row, unverified, and nothing keeps it out of an authorization decision but review. |
| CRED-30 | Auth tokens are absent from the GraphQL auth projection. | **Enforced** | — |
| CRED-31 | Sign-out clears the local credential, and nothing is revoked before it. | **Enforced** | The server-side revoke beside it is not a promise. A failed revoke, or a quit before it finishes, leaves the grant live while the app reports signed out. |
| CRED-32 | A sign-out holds when one store's write fails, and errs only when the row's and the entry's both do. | **Enforced** | — |

## Evidence

### CRED-1

- **Where:** `sidecar/internal/lib/oauth/browser/browser.go`
- `TestListenerRejectsInvalidCallbackWithoutConsuming` — an invalid callback is rejected without consuming a code or an error.

### CRED-2

- **Where:** `sidecar/internal/services/connectors/internal/oauthconn/connect.go`, `sidecar/internal/services/connectors/kubetailcloud/connect.go`
- `TestDisconnectDuringAnAttempt`, `TestAnExchangeThatFinishesLateIsRevoked`, `TestALateExchangeDoesNotDisturbTheNextAttempt`, `TestConnectSupersedesAnAttempt` — a sign-in completing after a sign-out or a newer sign-in restores no session, and its token is revoked.
- Note: every change runs under one lock against one generation.

### CRED-3

- **Where:** `sidecar/internal/lib/oauth/browser/browser.go`
- `TestListenerBoundsItsReads` — the callback bounds its reads.

### CRED-4

- **Where:** `sidecar/internal/lib/oauth/browser/browser.go`
- `TestListenerSurvivesRepeatedAuthorizationErrors` — a repeated error does not park the handler.

### CRED-5

- **Where:** `sidecar/internal/services/connectors/internal/oauthconn`, `sidecar/graph/`
- **Records:** [the ChatGPT connector](../security/2026-11-18-the-chatgpt-connector.md)
- `TestNoTokenLeavesTheKeyring` — reads the raw log and every file under the data directory after a sign-in, a refresh and a refused refresh.
- Note: `ChatGPTConnection` binds to `connectors.State`, which holds no token.

### CRED-6

- **Where:** `sidecar/internal/services/connectors/internal/oauthconn/sign.go`, `sidecar/internal/services/providercredentials/sign.go`, `sidecar/internal/services/internal/signer`, `sidecar/internal/llm/helpers.go`, `sidecar/internal/lib/oauth/oauth.go`
- `TestTheBearerGoesNowhereElse`, `TestCovers`, `TestEveryDialectFollowsNoRedirect`, `TestReadChatGPTModelsFails`, `TestTheIssuersClientFollowsNoRedirect`, `TestDiscoverRefusesADocumentItCannotTrust` — the bearer reaches `api.openai.com/v1` alone, redirects included, and the issuer's forms are never resent.

### CRED-7

- **Where:** `sidecar/internal/services/connectors/internal/oauthconn/connect.go`, `sidecar/internal/lib/oauth/oauth.go`
- `TestVerifyChecksTheNonce`, `TestASignInWithoutTheRequiredScopeKeepsNoTokens`, `TestAFirstSignInWithoutAnIssuedClientIsRefused` — the ID token's nonce and audience are verified, and no token is kept without plan permission.

### CRED-8

- **Where:** `sidecar/internal/services/connectors/internal/oauthconn`, `sidecar/internal/services/providercredentials/sign.go`, `sidecar/internal/llm/service.go`
- `TestABindingEndsWithItsSession`, `TestABindingWithNoSessionHasEnded`, `TestResolveBindsASessionSigner`, `TestResolveUnderKeepsTheParentsBindingOnItsProvider` — a turn never signs under a ChatGPT session it did not start in.

### CRED-9

- **Where:** `sidecar/internal/services/connectors/internal/oauthconn`, `sidecar/internal/services/providercredentials/sign.go`, `sidecar/internal/llm/service.go`
- `TestAnExchangeThatFinishesLateIsRevoked`, `TestDisconnectDuringARefresh` — a late exchange or refresh is revoked, never saved.

### CRED-10

- **Where:** `sidecar/internal/services/connectors/internal/oauthconn/sign.go`
- `TestSignRefreshesAnExpiredToken`, `TestARotatedTokenSurvivesACrash`, `TestAnUnsavedRotationHoldsTheAccessTokenBack` — a rotated refresh token is in the keyring before its access token is used.

### CRED-11

- **Where:** `sidecar/graph/schema.resolvers.go`, `sidecar/internal/services/connectors/internal/oauthconn/connect.go`
- `TestChatGPTMutations` — the ChatGPT connector's mutations.
- Note: no mutation takes an address.

### CRED-12

- **Where:** `sidecar/internal/services/connectors/openrouter`, `sidecar/graph/`
- **Records:** [`TODO.md`](../TODO.md), [the OpenRouter connector](../security/2026-11-19-the-openrouter-connector.md)
- `TestNoKeyLeavesTheKeyring` — the key stays in the keyring and memory.
- `TestTheCallbackTable`, `TestTheExchangeTable`, `TestABindingEndsWithItsSession`, `TestAFailedDeleteIsNotASignOut`, `TestNoBrowserOpensWithoutAKeyring`, `TestOpenRouterMutations` — the rest of the tests the source cites for the OpenRouter connector as a whole.
- Note: the source gives this row the status **Built, not yet checked end to end**. It is Enforced once the OpenRouter live checks in `TODO.md` pass.

### CRED-13

- **Where:** `sidecar/internal/services/connectors/openrouter`, `sidecar/graph/`
- **Records:** [`TODO.md`](../TODO.md), [the OpenRouter connector](../security/2026-11-19-the-openrouter-connector.md)
- `TestTheKeyGoesNowhereElse`, `TestCovers` — the key signs `openrouter.ai/api/v1` alone.
- Note: the source's status is **Built, not yet checked end to end**, as CRED-12.

### CRED-14

- **Where:** `sidecar/internal/services/connectors/openrouter`, `sidecar/graph/`
- **Records:** [`TODO.md`](../TODO.md), [the OpenRouter connector](../security/2026-11-19-the-openrouter-connector.md)
- `TestARejectedKeyIsConfirmedFirst` — the key is dropped only once OpenRouter confirms it dead.
- Note: the source's status is **Built, not yet checked end to end**, as CRED-12.

### CRED-15

- **Where:** `sidecar/internal/services/providercredentials`, `sidecar/graph/`, `src/lib/provider-credentials.tsx`, `src-tauri/src/services/sidecar/graphql/query.rs`
- **Records:** [saved provider keys](../security/2026-11-20-saved-provider-keys.md)
- `TestNoSavedKeyLeavesTheKeyring`, `TestProviderKeySaveReturnsAndLogsNoKey`, `TestTheAccountNames` — a saved key stays in the keyring and memory, and no log line, file, `app.db` row or GraphQL field holds one.
- `puts no saved key on the error bus or in the error log when a save fails` — the webview's test.
- `a_query_logs_no_body`, `a_timed_out_query_logs_no_body` — the host's tests.

### CRED-16

- **Where:** `sidecar/internal/services/providercredentials`, `sidecar/graph/`
- **Records:** [saved provider keys](../security/2026-11-20-saved-provider-keys.md)
- `TestTheKeyGoesNowhereElse`, `TestABindingEndsWithItsKey` — a saved key signs its row's base URLs alone.

### CRED-17

- **Where:** `sidecar/internal/services/connectors/aws`, `sidecar/graph/`
- **Records:** [`TODO.md`](../TODO.md), [the AWS connector](../security/2026-11-19-the-aws-connector.md)
- `TestNoCredentialReachesTheLog` — the session stays in the keyring and memory.
- `TestADeleteTheKeyringRefusesIsNotDeletedUntilARetry`, `TestAKeyringThatCannotHoldASessionOpensNoBrowser`, `TestAWSMutations` — the rest of the tests the source cites for the AWS connector as a whole.
- Note: the source gives this row the status **Built, not yet checked end to end**. It is Enforced once the AWS live checks in `TODO.md` pass.

### CRED-18

- **Where:** `sidecar/internal/services/connectors/aws`
- **Records:** [`TODO.md`](../TODO.md), [the AWS connector](../security/2026-11-19-the-aws-connector.md)
- `TestAProofNamesTheRequestAndVerifiesUnderItsKey` — every token request carries a proof under its key.
- Note: the source's status is **Built, not yet checked end to end**, as CRED-17.

### CRED-19

- **Where:** `sidecar/internal/services/connectors/aws`
- **Records:** [`TODO.md`](../TODO.md), [the AWS connector](../security/2026-11-19-the-aws-connector.md)
- `TestARotatedTokenIsSavedBeforeCredentialsAreHandedOut` — a rotated refresh token is saved before credentials are handed out.
- Note: the source's status is **Built, not yet checked end to end**, as CRED-17.

### CRED-20

- **Where:** `sidecar/internal/services/connectors/aws`
- **Records:** [`TODO.md`](../TODO.md), [the AWS connector](../security/2026-11-19-the-aws-connector.md)
- `TestTokensThatArriveAfterACancelAreDropped`, `TestARefreshThatEndsAfterADisconnectIsDropped` — tokens that arrive after a Cancel or Disconnect are dropped.
- Note: the source's status is **Built, not yet checked end to end**, as CRED-17.

### CRED-21

- **Where:** `sidecar/internal/services/connectors/aws`
- **Records:** [the AWS connector is AWS login with a DPoP-bound session](../adr/2026-11-19-the-aws-connector-is-aws-login-with-a-dpop-bound-session.md)
- Note: AWS's revocation is an IAM-signed call outside the flow, so the session stays valid at AWS until it expires.
- Note: the confirm says so.

### CRED-22

- **Where:** `sidecar/internal/services/connectors/aws`
- **Records:** [the AWS connector is AWS login with a DPoP-bound session](../adr/2026-11-19-the-aws-connector-is-aws-login-with-a-dpop-bound-session.md)
- Note: the ID token is the token endpoint's own answer over TLS.
- Note: its ARN is display only.

### CRED-23

- **Where:** `sidecar/internal/services/connectors/github`, `sidecar/graph/`
- **Records:** [`TODO.md`](../TODO.md), [the GitHub connector](../security/2026-11-19-the-github-connector.md)
- `TestNoTokenLeavesTheKeyring` — the token stays in the keyring and memory.
- Note: the source gives this row the status **Built, not yet checked end to end**. It is Enforced once the GitHub live checks in `TODO.md` pass.

### CRED-24

- **Where:** `sidecar/internal/services/connectors/github`, `sidecar/graph/`
- **Records:** [`TODO.md`](../TODO.md), [the GitHub connector](../security/2026-11-19-the-github-connector.md)
- `TestABadCodeAnswerIsRefused`, `TestNoCodeIsAskedForWithoutAKeyring`, `TestATokenAfterACancelIsDropped`, `TestATokenWithNoAccountIsNotKept`, `TestTokensPreferTheConnector`, `TestGitHubMutations` — the rest of the tests the source cites for the GitHub connector as a whole.
- Note: the source's status is **Built, not yet checked end to end**, as CRED-23.

### CRED-25

- **Where:** `sidecar/internal/services/connectors/github`
- **Records:** [`TODO.md`](../TODO.md), [the GitHub connector](../security/2026-11-19-the-github-connector.md)
- `TestARefusedTokenIsCheckedBeforeItIsDropped` — the token is dropped only once GitHub confirms it dead.
- Note: the source's status is **Built, not yet checked end to end**, as CRED-23.

### CRED-26

- **Where:** `sidecar/internal/services/connectors/github/disconnect.go`
- **Records:** [the GitHub connector signs in with GitHub's device flow](../adr/2026-11-19-the-github-connector-signs-in-with-githubs-device-flow.md)
- `connectors.test.tsx` — the Disconnect confirm says so and opens GitHub's settings.
- Note: revoking takes the OAuth App's secret.

### CRED-27

- **Where:** `sidecar/internal/services/connectors/google`, `sidecar/internal/services/providercredentials/sign.go`, `sidecar/internal/services/connectors/internal/oauthconn`, `sidecar/graph/`
- **Records:** [`TODO.md`](../TODO.md), [the Google connector](../security/2026-11-19-the-google-connector.md)
- `TestAnInstalledAppSignsInUnderItsWrittenEndpoints`, `TestBaseURLRefusesWhatIsNotAProjectOrLocation`, `TestNewSignsTheConfiguredBase`, `TestConnectWithNoClientIsRefused`, `TestTheGoogleRowIsLeftOut`, `TestGoogleMutations` — the bearer reaches Vertex AI in the user's project alone, under endpoints written in code.
- Note: the source gives this row the status **Built, not yet checked end to end**. It is Enforced once the Google live checks in `TODO.md` pass.

### CRED-28

- **Where:** `sidecar/internal/lib/oauth/oauth.go`, `sidecar/internal/services/connectors/internal/oauthconn/connect.go`
- `TestVerifyChecksTheNonce`, `TestVerifyBadSignature`, `TestAWrongNonceIsRefused` — the ID token is verified against JWKS and the authorize request's nonce.

### CRED-29

- **Where:** `sidecar/internal/lib/oauth/oauth.go`, `sidecar/internal/services/connectors/internal/oauthconn/connect.go`
- Note: at launch the account is read from the `oauth_connections` row, unverified.
- Note: nothing keeps it out of an authorization decision but review.
- Note: startup `authenticated` means the row and the keyring name one session, not that the issuer has just validated it.

### CRED-30

- **Where:** `sidecar/graph/schema.graphqls`
- `TestAuthProjectionCarriesNoTokens` — pins `AuthState`'s and `Identity`'s field sets, the two types gqlgen binds onto the Go values that hold the token set.

### CRED-31

- **Where:** `sidecar/internal/services/connectors/internal/oauthconn/disconnect.go`
- `TestDisconnectRevokesAndClears` — the revoke starts after the clear, under the lock the clear ran under.
- `TestAnUnconfirmedRevocationSaysSo` — the server-side revoke is tried twice.
- `TestCloseEndsASignInAndARevocation` — the server-side revoke is cancelled by a quit.
- Note: the server-side revoke is **not a promise**. A failed revoke, or a quit before it finishes, leaves the grant live while the app reports signed out.

### CRED-32

- **Where:** `sidecar/internal/services/connectors/internal/oauthconn/disconnect.go`
- `TestADisconnectThatCannotWrite` — a sign-out holds when one store's write fails, and errs only when the row's and the entry's both do.
