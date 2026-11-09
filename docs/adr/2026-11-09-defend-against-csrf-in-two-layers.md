---
title: Defend against CSRF in two layers
date: 2026-11-09
scope: dashboard
status: Accepted
---

# Defend against CSRF in two layers

## Context

The Dashboard authenticates browsers with a session cookie. Any page the user visits can make the browser send that cookie. The current defence took shape between April and May 2026, after two earlier designs.

The first design was a token. #6 (February 2024) added `gorilla/csrf` tokens to stop cross-site WebSocket hijacking. #570 (August 2025) removed them in favour of `Sec-Fetch-Site`, giving three reasons. The GraphQL data requests were not themselves considered CSRF-prone. A token on a WebSocket must be sent after the upgrade, which is cumbersome. And `gorilla/csrf`'s fix for CVE-2025-24358 had made the library unusable for Kubetail. Issue #404 adds that tokens made third-party GraphQL clients harder to use.

The second design, `Sec-Fetch-Site` alone, ran into gaps. #1092 (April 2026) applied it to every dynamic route on the premise that "modern browsers always send `Sec-Fetch-Site`". #1097 found that Chrome does not send it on WebSocket upgrades. It moved the WebSocket defence to the `Origin` header and skipped the check for safe methods. #1106 (April 2026) brought back a session-bound token, now hand-rolled, for unsafe methods and in the WebSocket `connection_init` payload. Its description does not say why a token was needed again beside the other checks.

Later fixes refined both layers:

- #1112 accepted the token from a form field, because the log download submits an HTML form that cannot set headers.
- #1114 compared in constant time and rotated the token on login.
- #1117 stopped trusting `X-Forwarded-*` headers in the origin check, and #1118 added `allowed-origins` for deployments behind a proxy.
- #1155 (issue #1150) let an absent `Sec-Fetch-Site` through, because browsers omit it in non-secure contexts such as a plain-HTTP ingress.
- #1158 added `same-site` to the rejected values.

## Decision

`csrfProtectionMiddleware` (`modules/dashboard/pkg/app/middleware.go`) skips GET, HEAD and OPTIONS (`safeMethods`). For every other method:

1. It refuses `Sec-Fetch-Site` values of `cross-site` or `same-site` (`rejectedSecFetchSite`). An absent header passes.
2. It requires `X-CSRF-Token` to equal the session's token (`getOrCreateCSRFToken`), compared with `subtle.ConstantTimeCompare`. An `application/x-www-form-urlencoded` body may carry it as `csrfToken` instead. The body is read and restored, not parsed with `c.PostForm`, so the Cluster API proxy can still forward it.

The GraphQL servers have no GET transport.

A WebSocket upgrade is a GET, so it is gated separately:

- `CheckOrigin` calls `httphelpers.IsAllowedOrigin`. A missing `Origin` fails.
- The `connection_init` payload's `csrfToken` must equal the session's token.
- For `/cluster-api-proxy`, `websocketCSRFContextMiddleware` deletes any client-supplied `X-Forwarded-CSRF-Token` and stamps the session's. `rejectForbiddenUpgrade` refuses an upgrade without it.
- The Cluster API refuses any upgrade that carries an `Origin`, since the proxy strips it. When `X-Forwarded-CSRF-Token` is present, the Cluster API checks it against `connection_init` (9555b7d).

`IsSameOrigin` (`modules/shared/httphelpers`) compares `Origin` with `r.Host` and `r.TLS` only. Host case and default ports are normalized. `allowed-origins` lists any further origins the Dashboard is served at.

## Alternatives considered

- **A token alone, through `gorilla/csrf`.** Dropped in #570 for the reasons in the context. The current token is implemented in the Dashboard, with no library.
- **`Sec-Fetch-Site` alone.** Chrome omits it on WebSocket upgrades (#1097), and browsers omit it in non-secure contexts (#1155). Requiring `same-origin` rejected valid requests in both cases (#1150).
- **Allow `same-site`.** A sibling subdomain on the same eTLD+1 is still cross-origin for CSRF purposes (#1158).
- **Trust `X-Forwarded-Host`, `X-Forwarded-Proto` and `Forwarded` in the origin check.** #1097 did this at first. #1117 dropped it: a direct-access client can set those headers, and a proxy that appends rather than overwrites passes them through.
- **`Sec-Fetch-Site` on the Cluster API.** #1096 called it "the wrong primitive" for a server meant for programmatic clients. The Cluster API refuses any `Origin` instead.
- **No WebSocket token check on the Cluster API.** 93830bf dropped it on the grounds that the listener was mTLS-only. 9555b7d restored it to close "the gap where cluster-api subscriptions had no CSRF enforcement".

## Consequences

The token is the primary defence on HTTP. `Sec-Fetch-Site` only rejects requests that the browser has marked cross-origin.

Safe methods are unchecked. Cross-origin reads are left to the browser's Same-Origin Policy, and no GET route may change state.

Every unsafe request needs a session's CSRF token, including one that authenticates with an `Authorization` header. The Dashboard's API is for its own UI (#1092: "The dashboard is browser-only").

A missing `Origin` on an upgrade is refused. Browsers always send `Origin` on upgrades, so the check is only for WebSocket upgrades. Browsers omit `Origin` on same-origin safe fetches, so the check cannot be used on ordinary HTTP requests.

A deployment behind a proxy that rewrites `Host` or terminates TLS without passing the scheme on fails the same-origin check. It must set `allowed-origins`.

The UI has to fetch the token before its first unsafe request, and fetch it again after a 403 (`csrfLink`).

## Revisit when

Every supported browser sends `Sec-Fetch-Site` on every request, upgrades and non-secure contexts included. The token layer might then be redundant.
