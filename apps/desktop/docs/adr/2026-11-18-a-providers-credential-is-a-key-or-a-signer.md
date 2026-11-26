---
title: A provider's credential is a key or a signer
date: 2026-11-18
scope: sidecar
status: Accepted
amended_by: [A provider's credential follows one order](2026-11-20-a-providers-credential-follows-one-order.md)
---

# A provider's credential is a key or a signer

## Context

`llm.Provider.Key` is fixed at launch. The ChatGPT plan's token expires every hour and rotates its
refresh token; Bedrock signs each request over its body (SigV4); Vertex and Foundry mint tokens
from the platform's own credentials. A cloud platform is auth on a vendor's
wire planned a `Provider.Auth` enum
switched on in each wire's client builder.

## Decision

`llm.Provider` has a `Signer` beside `Key`, never both (`llm.New` panics). A signer sets each
request's authorization (`Sign`) and is told of a `401` (`Refused`). Every wire sets the credential,
key or signer, in one middleware (`helpers.go`'s `send`), with an `http.Client` that follows no
redirect; the SDK is given no key. A key goes in the header its row names, else its dialect's own
(`authorize`), read off the target's provider, so a model on another dialect gets that dialect's
header. A failed `Sign` is an `llm.Error` with no response: `signed out` or `credential
unavailable`. A `SessionSigner` binds: `Resolve` puts the current session's signer on the target,
and `ResolveUnder` keeps a parent's on a subagent of the same provider. `llm` names no vendor; a
signer is a value on the row.

What stands of the earlier ADR: a platform is entries on a vendor's wire, never a dialect; the
platform's own conventions supply every value, read on the entry's turn; the Claude and GPT
entries reuse the vendor catalogs under the platform's spelling. A platform's credential chain is
its signer's to walk.

## Alternatives considered

- **`Provider.Auth`, an enum each wire switches on.** Every wire grows a branch per platform, and
  `llm` names every vendor's signing.
- **A key source (`func() string`).** It cannot sign over a body, and it hears no `401`.
- **A key as one more `Signer`.** A key's header follows the dialect, which a model may override,
  and `Sign` is not told the dialect.

## Consequences

A platform is data plus a signer. The conformance table runs under a signer as under a key
(`TestEveryDialectSignsARequestUnderASigner` and the cases beside it). A keyed request follows no
redirect either, so its key and its body reach the row's endpoint alone
(`TestEveryDialectFollowsNoRedirect`).
