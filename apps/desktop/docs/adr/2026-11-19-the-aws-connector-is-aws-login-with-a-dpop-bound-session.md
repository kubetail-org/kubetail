---
title: The AWS connector is AWS login with a DPoP-bound session
date: 2026-11-19
scope: sidecar
status: Accepted
---

# The AWS connector is AWS login with a DPoP-bound session

## Context

Settings' Connectors tab has held an AWS placeholder since the tab was drawn. AWS offers developer
tools an OAuth sign-in of its own, the flow `aws login` runs: authorization code with PKCE on a
loopback redirect at `<region>.signin.aws.amazon.com/v1/authorize`, under AWS's fixed client
`arn:aws:signin:::devtools/same-device`. Its token endpoint, `signin`'s `CreateOAuth2Token`, takes
JSON, requires a DPoP proof (RFC 9449, ES256) on every call, and answers temporary SigV4
credentials of at most fifteen minutes, a refresh token bound to the proof's key, and at sign-in
an ID token whose `sub` is the principal's ARN. It has no discovery document and publishes no key
to verify the ID token.

The OAuth engine (`connectors/internal/oauthconn`) runs OIDC: discovery, a JWKS-verified ID token
with a nonce, form-encoded token requests, revocation, and a bearer it signs onto one API base.
`openrouter` is the other kind: a sign-in that grants a key
([a connector is an OAuth session or a granted key](2026-11-19-a-connector-is-an-oauth-session-or-a-granted-key.md)).

## Decision

**The AWS connector is a package of its own, `services/connectors/aws`, beside `openrouter`.** It
shares `lib/oskeyring` and `lib/oauth/browser`, imports neither the engine nor `lib/oauth`, and
calls the token endpoint through the AWS SDK's `signin` client, which already speaks its wire,
with a finalize middleware that adds the proof.

**One store: one keyring entry is the session.** Account `aws-session`, `{"v":1,"refresh",
"dpopKey","arn"}`: the refresh token, the session's P-256 key in SEC 1 PEM (as the AWS CLI keeps
it), and the ARN. No `app.db` row: there is no registration to outlive a sign-out. A key is minted
per sign-in. The credentials are memory's alone.

**The connector is an `aws.CredentialsProvider`.** `Retrieve` answers the credentials until a
minute before expiry, then refreshes, one at a time. A rotated refresh token is in the keyring
before any credential is handed out. `AccessDeniedException` and `InvalidGrantException` end the
session; anything else keeps it for the next `Retrieve`. Nothing calls `Retrieve` yet.

**The ID token is read, not verified.** It is the token endpoint's own answer, over TLS, to an
exchange this process made; the ARN is display only and must name an account.

**`Disconnect` revokes nothing.** AWS's revocation is an IAM-signed call outside this flow; the
refresh token stays valid at AWS until the session expires, and the confirm says so.

**One region.** A session is signed in to and refreshed in `us-east-1`; a debug build moves the
sign-in's origin with `KUBETAIL_AWS_SIGNIN_URL`.

## Alternatives considered

- **An `oauthconn.Provider`.** The engine would need a branch for no discovery, an unverifiable ID
  token, JSON token requests, DPoP, no revocation and credentials that are no bearer: every path
  but the loopback.
- **IAM Identity Center's OIDC (`sso-oidc`).** It needs the organization's start URL and region
  from the user, then a role pick per account; `aws login` serves any console user with neither.
- **Write `~/.aws/login/cache` and a `login_session` profile, as `aws login` does.** The sidecar
  would write the user's AWS configuration, and a refresh token would sit in a plain file.
- **Our own HTTP for the token endpoint.** The SDK's client already has its serialization and
  typed errors; the proof is the one thing it lacks for a session it does not cache itself.

## Consequences

- A consumer of the credentials (Bedrock's row, the sandbox's AWS proxy) is a change of its own,
  and decides which wins over a profile's.
- The flow is built on AWS's documented API and the AWS CLI's behaviour; the by-hand checks are
  in `docs/TODO.md`.
- A disconnected session is live at AWS until it expires.

## Revisit when

AWS lets a developer tool revoke its own refresh token, or a consumer needs another region.
