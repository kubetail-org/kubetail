---
title: Helm's release Secrets pass, redacted inside
date: 2026-09-28
scope: sidecar
status: Accepted
---

# Helm's release Secrets pass, redacted inside

## Context

A sandboxed command reads the chat's cluster through `kubeproxy`, which redacts a Secret's values
as the response streams. Helm keeps each release as a Secret typed `helm.sh/release.v1`, whose
`data.release` is the whole release gzipped and base64'd twice: the chart, the user's values, and
the rendered manifests, Secrets among them. `helm list`, `helm status` and `helm get` read those
Secrets. Redacted like any other, every one of those reads fails in the sandbox. Passed whole,
they hand the user's values and every rendered Secret to the provider. Decided on 2026-09-27; written down here.

## Decision

A release Secret passes with its insides redacted (`kubeproxy/redact_helm.go`): `data.release` is
decoded, every scalar of `config` replaced, every `Secret` in the manifest and each hook's
redacted, with every `last-applied-configuration`, then encoded again. The chart, `info` and the
other fields pass as they came. A release that does not decode fails closed with a 502. Helm
changes run outside the sandbox.

## Alternatives considered

**Redact a release like any Secret.** Safe, and helm's reads stop working in the sandbox, so every
`helm list` is a request to run outside it.

**Pass releases whole.** Helm works, and the user's values — often holding passwords — and every
rendered Secret reach the provider on the model's word.

## Consequences

- A release's chart, default values, rendered notes and other objects' manifests pass; a value can
  be rendered into an env var there. A full audit of what else carries secrets is in `TODO.md`.
- A Secret typed `helm.sh/release.v1` by hand passes as a release does, all but its `config` and
  its manifests' Secrets, since anyone who can create a Secret sets its `type`.
- The rewriter must follow helm's release format. A format it cannot read fails closed.
