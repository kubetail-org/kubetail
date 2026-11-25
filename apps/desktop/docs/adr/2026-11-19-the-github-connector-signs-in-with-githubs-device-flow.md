---
title: The GitHub connector signs in with GitHub's device flow
date: 2026-11-19
scope: sidecar
status: Accepted
---

# The GitHub connector signs in with GitHub's device flow

## Context

A sandboxed `git` or `gh` reaches GitHub through `run/githubproxy`, which sends the user's token
([a sandboxed command reaches GitHub over plain HTTP](2026-11-17-a-sandboxed-command-reaches-github-over-plain-http-at-ghs-own-host-name.md)).
The token comes from `gh auth token` alone, so a user with no `gh` login has no GitHub in the
sandbox.

GitHub's OAuth has no discovery document and no ID token, and its authorization code exchange
takes the OAuth App's client secret, which a desktop app cannot keep. GitHub's device flow
(RFC 8628) takes the client id alone. An OAuth App's token does not expire and has no refresh
token, and revoking it also takes the client secret.

## Decision

**The connector is GitHub's device flow under Kubetail's OAuth App**, a package of its own,
`services/connectors/github`, beside `openrouter`
([a connector is an OAuth session or a granted key](2026-11-19-a-connector-is-an-oauth-session-or-a-granted-key.md)).
`Connect` asks `github.com/login/device/code` for a code under the scopes `repo`, `read:org` and
`workflow`, shows the user code on the state, and opens the browser on the verification page.
It then polls `github.com/login/oauth/access_token` once an interval, as RFC 8628 says.

**The token is a granted key.** One keyring entry, `{"v":1,"token","login"}` under account
`github-token`; an entry present is connected. The login, from `GET api.github.com/user`, is
display only.

**The proxy prefers the connector.** `Service.Tokens(fallback)` is the proxy's token source: the
connector's token while connected, `githubauth`'s `gh` token otherwise. A `401` is told to both,
and each acts on its own token alone. The connector drops its token only once `/user` answers
`401` too, since Kubetail cannot revoke it.

**The client id is an argument.** `--github-client-id`, empty by default; with none, `Connect` is
refused. A debug build reads `KUBETAIL_GITHUB_CLIENT_ID` and `KUBETAIL_GITHUB_AUTH_URL`.

**The browser opens only on GitHub's origin.** A verification URI on any other scheme or host,
and a user code that is not GitHub's shape, fail the sign-in `Refused`.

## Alternatives considered

- **The OAuth engine (`oauthconn`).** It needs discovery, an ID token and a refresh token, none of
  which GitHub has; widening `Provider` for it would branch every path.
- **Authorization code with PKCE on a loopback redirect.** GitHub's exchange still takes the client
  secret, and a secret shipped in the app is no secret.
- **A GitHub App's expiring user tokens.** Their refresh also takes the client secret.

## Consequences

- A disconnected token stays authorized on GitHub until the user revokes Kubetail in GitHub's
  settings. The row says so and opens the page.
- The user types a code shown in Kubetail on GitHub's page, a step the other connectors lack.
- Kubetail needs an OAuth App with the device flow on, and its client id passed by the host, before
  a release offers the connector.

## Revisit when

GitHub lets a public client exchange a code or revoke a token without the client secret.
