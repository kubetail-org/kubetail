---
title: The cluster proxy reads a request as the API server will
date: 2026-10-17
scope: sidecar
status: Accepted
---

# The cluster proxy reads a request as the API server will

## Context

A sandboxed command reaches the chat's cluster only through `internal/run/kubeproxy`. The proxy
decides each request — pass, redact, ask the user, refuse — and the user approves what the proxy
shows. Every decision is worthless if the API server reads the request differently from the proxy.
These choices were made with the proxy (September–October 2026) and were recorded in
`sidecar/CLAUDE.md` until this file.

## Decision

**The run's token is proxy credentials.** A run's `proxy-url` is
`http://kubetail:<token>@127.0.0.1:<port>`, which Go's transport sends as `Proxy-Authorization` on
every request. The handler checks it in constant time and deletes it, and `Authorization`, before
forwarding.

**The policy reads the path the proxy forwards.** A path that is not canonical (a `RawPath`, or one
`path.Clean` changes) is refused before the parse, and the parse follows the API server's
`RequestInfoFactory`, never a segment found anywhere in the path.

**The write path refuses what would run as other than it is shown** (`write.go`): a query that
`url.ParseQuery` refuses; a body repeating a key in one object (`uniqueKeys`); a body with a
`Content-Encoding` or a media type other than JSON or apply YAML. A request is a dry run only for a
`POST`, `PUT` or `PATCH` whose every `dryRun` is `All`.

## Alternatives considered

- **A bearer token in the kubeconfig user.** clientcmd applies a kubeconfig user's credentials only
  to a server reached over TLS, and the proxy is plain HTTP on loopback. A `Proxy-` header also
  cannot be set by a browser page, so a page on the machine cannot reach the proxy with it.
- **Parse the decoded path as given.** The parse reads the decoded path while the proxy forwards the
  raw one; an encoded or `..` spelling could be classified as one resource and served as another.
- **Forward an unparseable query or a repeated key as is.** A query pair that does not parse is
  dropped on its way to the API server, so a selector shown on the request would not run.
  `decodeBody` keeps the last of a repeated key where the API server's typed decoder merges, so the
  body would be classified and shown as other than it runs.
- **Honour `dryRun` on a `DELETE`'s query.** The API server reads a `DELETE`'s options from its body
  when it has one, so the query's `dryRun` may not be the one that applies.

## Consequences

- A refusal here is cheap for the user and the model: each is a `Status` naming why.
- Any new way to send a request (a method, a media type, a subresource) must be read the API
  server's way before it passes, or refused.

## Revisit when

The API server changes how it parses a path, a query or a body.
