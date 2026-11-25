# Security record — a sandboxed command reaches GitHub, 17 November 2026

**Subject:** step 2B of the cloud proxies. With `sandbox.github` on, a sandboxed run's `git` and
`gh` reach `github.com` through the run's door, which adds the user's token outside the sandbox.
The living model is [security-model.md](../security-model.md); the decision is
[the ADR](../adr/2026-11-17-a-sandboxed-command-reaches-github-over-plain-http-at-ghs-own-host-name.md),
over [the run's door](2026-11-17-the-runs-door.md).

## What changes

**Widened.** With GitHub on, a sandboxed command reads what the user's token reads on
`github.com`, unasked: private repositories and their content, issues, Actions metadata, the
user's profile and private email addresses. The answer reaches the model's provider, and with
network on the command can send it anywhere. A prompt injection in a repository's text can ask
for a change; the request is the gate.

**The sidecar holds a GitHub token.** `services/githubauth` runs `gh auth token --hostname
github.com` outside the sandbox, on the sidecar's `PATH`, with no stdin and prompts off, killed
past ten seconds. It holds the answer in memory for five minutes, under `safe.AddSecret`, and
logs neither the token nor what `gh` printed (`TestTheTokenIsNeverLogged`). `gh` runs at most once
a minute: a failure is held that long, and a token GitHub answers `401` is dropped only a minute
after `gh` last ran, so a command cannot drive `gh` (`TestRepeatedRefusalsRunGhOncePerInterval`,
`TestAFailedReadIsHeldForTheInterval`).

## What holds

- **No GitHub token is in the run.** The run's environment and files hold the grant's token and a
  placeholder login alone (`TestNoGitHubTokenIsInTheRun`). The denied-always list and `neverEnv`
  do not change. The run's `gitconfig` clears every credential helper for the door and the route,
  so no helper, the keychain's included, is handed the grant's token
  (`TestGitHubsGitconfigClearsCredentialHelpers`).
- **The upstream is fixed.** Each route forwards to its one origin, whatever the request's host,
  path, headers or query (`TestTheUpstreamIsFixed`). Only the listed headers pass
  (`TestOnlyTheListedHeadersReachGitHub`), and the placeholder `Authorization` never does
  (`TestTheTokenIsAddedOutside`).
- **A change asks.** An API change, a GraphQL document holding any mutation, and a push each wait
  on the user (`TestOnlyAnApprovedChangeReachesGitHub`, `TestAnyMutationIsAChange`,
  `TestASandboxedPushAsksAndRuns`). An API change shows the bytes sent; a push shows its
  repository, every ref with its old and new ids, and every push option
  (`TestAnAPIChangeShowsWhatIsSent`, `TestAPushAsksAndIsForwardedWhole`).
- **A credential is refused, never asked for**, by name or by id
  (`TestACredentialPathIsRefused`, `TestAPathByIDIsClassifiedAsItsRoute`).
- **A signed link reaches a command only through an approved data read**
  (`TestARedirectToAnotherHostNeedsAnApprovedDataRead`); a `Link` to another host is dropped from
  any other answer. A link to the upstream's own origin, however it is spelled, is handed back as
  the route's (`TestARedirectOnTheSameUpstreamNamesTheRoute`, `TestALinkHeaderNamesTheRoute`,
  `TestAnyLinkToTheUpstreamsOriginNamesTheRoute`).

## Residuals

Each is a **By decision** row in the model.

- A repository's content is read unasked, private ones included.
- A push's pack is not shown. The user approves the repository, the refs and the options.
- The token's reach is the proxy's reach. There is no repository fence.
- An answer's body can hold a signed link the lists do not name, such as a private file's
  `download_url`. It is short-lived and opens one download.
- A redirect on GitHub's own origin comes back through the proxy as a new request. An approved
  data read GitHub redirects there asks again, and a change it answers `307` or `308` is sent
  again only once the user approves it again.
- `credentialPaths` and `dataPaths` are lists kept by hand. A route GitHub adds whose answer is a
  credential or a secret is a read until it joins one.
- GraphQL is classified by Kubetail's parser, not GitHub's. The any-mutation rule and the strict body
  checks keep the two from disagreeing on a change; a query field serving `dataPaths`' data would
  be unasked.
- `HTTP_PROXY` hands the grant's token to every Go and Python program in the run. It opens
  GitHub's grant alone.
- Reads spend the user's rate limit. The grant bounds the rate, not the hour's total.
- `gh` on the sidecar's `PATH` runs with the user's authority, as a kubeconfig's `exec` plugin
  does.

## Hand checks

With gh 2.103.3 and git 2.47.3 on Linux, against a logging proxy:

- `gh api user`, with `GH_HOST=github.localhost`, `HTTP_PROXY` and the run's `hosts.yml` in a
  read-only config directory, sends absolute-form `http://api.github.localhost/user` with
  `Proxy-Authorization` on the first request and `Authorization: token kubetail-placeholder`, and
  leaves the directory unchanged. It sends `Accept`, `Content-Type`, `X-Github-Api-Version`,
  `Accept-Encoding` and `User-Agent`.
- `gh repo clone o/r` runs `git clone http://github.localhost/o/r.git`, which the run's
  `gitconfig` sends through the door.
- `gh api --paginate` and a `github.com` URL handed to `gh` dial `https://api.github.com`
  directly; the proxy's `Link` rewrite answers the first, and the prompt names a repository as
  `-R owner/repo` for the second.
- `git` reads the run's `gitconfig` through `GIT_CONFIG_COUNT` and `include.path`, applies all
  three `insteadOf`, and sends `Proxy-Authorization` only after a `407`.
- GitHub serves `/repositories/{id}/…` and `/organizations/{id}/…` for routes by name
  (`/repositories/{id}/tarball` answers a link to codeload).

On macOS these are still to be checked.

Built in the commit that adds this record.
