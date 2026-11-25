---
title: The Google connector signs Vertex AI in the user's project
date: 2026-11-19
scope: sidecar
status: Accepted
amended_by: [A provider's credential follows one order](2026-11-20-a-providers-credential-follows-one-order.md)
---

# The Google connector signs Vertex AI in the user's project

## Context

Gemini works only with `GEMINI_API_KEY`, on the Gemini API's OpenAI-compatible endpoint. A user
signed in to Google should get Gemini without pasting a key, as ChatGPT and OpenRouter work.

Google's sign-in is OAuth with PKCE on a loopback redirect, which the OAuth engine runs
([an OAuth connection is a row matched to its keyring entry](2026-11-18-an-oauth-connection-is-a-row-matched-to-its-keyring-entry.md)).
Three things about it differ from the providers the engine served so far. Google's token and JWKS
endpoints are on other origins than its issuer, which `oauth.Discover` refuses. Its installed-app
clients carry a client secret that the token endpoint requires and that no installed app can keep.
And it grants a refresh token only to `access_type=offline`, once per consent.

A user token on the Gemini API bills the OAuth client's own project unless each request names
another. Vertex AI bills the project its URL names, under the `cloud-platform` scope.

## Decision

**The connector signs Vertex AI, not the Gemini API.** `services/connectors/google` runs the engine
under Kubetail's client with the scopes `openid email profile cloud-platform`, and keeps no sign-in
that did not grant `cloud-platform`. Its tokens sign Vertex AI's OpenAI-compatible endpoint,
`https://[<location>-]aiplatform.googleapis.com/v1/projects/<project>/locations/<location>/endpoints/openapi`.

**The project is the one Google's own tools use.** `GOOGLE_CLOUD_PROJECT`, then
`CLOUDSDK_CORE_PROJECT`, then the active gcloud configuration's `[core] project`; the location is
`GOOGLE_CLOUD_LOCATION`, else `global`. Each is read at launch, in every build, after the
login shell's import, so on macOS a variable a startup file exports counts; each must be a
well-formed id, so it cannot move the host off `aiplatform.googleapis.com` or change the path.
With no project the connector still signs in, signs nothing, and lists no models.

**Its models are a row of their own, `google`.** The `gemini` row keeps its key and endpoint. The
`google` row lists the Gemini models under Vertex AI's ids (`google/<model>`) while connected. When
`GEMINI_API_KEY` is set the key serves Gemini, and the `google` row is left out.

**The engine grows three `Provider` fields.** `Endpoints` writes the endpoints out and skips
discovery. `ClientSecret` goes on the exchange and the refresh. `NoResource` sends no RFC 8707
`resource`. The client id and secret are arguments, `--google-client-id` and
`--google-client-secret`, empty by default; with no id, `Connect` is refused.

**Every sign-in asks for consent.** The authorize request carries `access_type=offline` and
`prompt=consent`, so each sign-in, a second one after a lost session included, returns a refresh
token.

**"Cloud resources" stay out.** The token reaches the model row alone: no sandboxed command sees it.

## Alternatives considered

- **The Gemini API with a bearer token.** It keeps the existing row and URL, but a user token there
  bills Kubetail's client project unless every request names the user's, and whether its
  OpenAI-compatible endpoint takes a token at all is unchecked.
- **Listing the user's projects after sign-in.** It needs a picker and a stored choice; the
  environment and gcloud already name the project most users have.
- **Discovery with the origin check relaxed for Google.** The check is what keeps a document from
  sending a code elsewhere; writing Google's endpoints out keeps it for everyone else.

## Consequences

- A user with no project sees the connector connected and no models, and the row says how to set
  one. A change to the project takes a restart.
- Kubetail needs a Google OAuth client for installed apps, and `cloud-platform` is a sensitive scope:
  Google verifies the app before it serves users outside its test list.
- The row's model ids and efforts on Vertex AI are unchecked; `docs/TODO.md` lists them.

## Revisit when

The provider setup form (#1000) can hold a project per connector, or the sandbox needs the user's
Google Cloud credentials.
