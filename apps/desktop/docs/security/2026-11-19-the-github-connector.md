# Security record — the GitHub connector, 19 November 2026

**Subject:** the sidecar signs in to GitHub with the device flow and keeps the OAuth token GitHub
grants, which acts as the user on their repositories, organizations and workflow files. A
sandboxed command's GitHub proxy sends it while it is connected, in place of `gh`'s. The design is
[the GitHub connector signs in with GitHub's device flow](../adr/2026-11-19-the-github-connector-signs-in-with-githubs-device-flow.md).
The living model is [security-model.md](../security-model.md).

## What changes

- A new credential: a GitHub OAuth token with the scopes `repo`, `read:org` and `workflow`, which
  does not expire, held by the sidecar (`services/connectors/github`).
- New egress: the sidecar posts the OAuth App's client id to `github.com/login/device/code`, polls
  `github.com/login/oauth/access_token` with the device code, and reads `api.github.com/user` to
  name the account and to confirm a refused token.
- The proxy's token is the connector's while connected; `gh auth token` runs only otherwise.
- New GraphQL: `gitHubConnection`, `gitHubConnectionWatch`, `gitHubConnect`,
  `gitHubConnectCancel`, `gitHubDisconnect`, `gitHubSettingsOpen`.

## What holds

- **The token is in the OS keyring**, service `Config.KeychainService`, account `github-token`,
  as reachable as the other connectors' credentials: any process of the user can ask the keyring
  for it, and a sandboxed command cannot reach the keyring.
- **No token or device code is logged, projected or written to a file.** Each is registered with
  `safe.AddSecret` on receipt and forgotten when cleared (`TestNoTokenLeavesTheKeyring`).
  `GitHubConnection` binds to `connectors.State`, which holds no token.
- **The token reaches a sandboxed command only through the proxy**, whose origins, headers and
  asks are unchanged: the run holds the grant's token alone (`TestNoGitHubTokenIsInTheRun`).
- **The browser opens on GitHub's origin alone**, and the user code drawn is GitHub's shape, spelled
  out through `VisibleText` (`TestABadCodeAnswerIsRefused`).
- **No request follows a redirect**, and `/user` is the only API request the connector sends.
- **The keyring is checked before GitHub is asked for a code**, so a machine that cannot keep a
  token is not granted one (`TestNoCodeIsAskedForWithoutAKeyring`).
- **A token after a Cancel, or one whose account cannot be named, is dropped, never saved**
  (`TestATokenAfterACancelIsDropped`, `TestATokenWithNoAccountIsNotKept`).
- **A token is dropped only once GitHub confirms it dead.** A proxied `401` starts one `GET /user`;
  only its own `401` deletes the entry (`TestARefusedTokenIsCheckedBeforeItIsDropped`).
- **The webview holds no token and opens no URL.** The sidecar opens the verification page GitHub
  named on its own origin, and the settings page, a constant (`TestGitHubMutations`).

## Not yet checked against GitHub

The connector is built on GitHub's documented device flow: the device code endpoint answers
`device_code`, `user_code`, `verification_uri`, `expires_in` and `interval` as JSON under
`Accept: application/json`; the token endpoint answers `200` with `error` while pending; and
`/user` answers a revoked token `401`. Kubetail has no OAuth App yet. Registering one with the device
flow on, and one sign-in by hand, are in `docs/TODO.md`.

## Residual risk

- A disconnected token stays authorized on GitHub until the user revokes Kubetail there: revoking
  takes the client secret. Kubetail says so and opens the page.
- A token GitHub granted after a Cancel, or whose keyring write failed, is authorized and in no
  store. The user revokes Kubetail on GitHub.
- A token whose delete failed is used again after a restart. The row says so.
- The device flow can be phished: a page that shows someone else's code asks the user to authorize
  that person. Kubetail shows only codes it asked for, and the user enters it on GitHub's own page.
