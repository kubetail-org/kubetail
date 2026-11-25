# Security record — coverage reports go to Codecov, 30 October 2026

**Subject:** CI uploads its coverage reports to Codecov, which posts the `codecov/patch` and
`codecov/project` statuses that gate a pull request. It is the first CI job to send data to a
third party for its verdict. Reviewed at `88ef33ad`. The living model is
[security-model.md](../security-model.md); the decision is the
[ADR](../adr/2026-10-30-codecov-gates-the-changed-lines.md).

## What leaves

Three kinds of report, each named by path in the upload step with `disable_search: true`, so the
action sends that file and nothing else from the runner:

- a Go cover profile per OS: import paths, line and column ranges, statement counts, hit counts;
- vitest's `lcov.info`: file paths and per-line hit counts;
- `cargo llvm-cov`'s lcov per OS: the same.

None carries source text, an environment value or a secret. The action also sends the commit SHA,
branch, pull request number and the job's identity, all public for this repository.

## How an upload authenticates

`use_oidc: true`: the job asks GitHub for a short-lived OIDC token with Codecov's audience, which
needs `id-token: write`, granted on the three coverage jobs alone. No Codecov token is stored in
the repository's secrets. A fork's pull request cannot mint the token; the action detects the fork
and uploads tokenless, which Codecov accepts for a public repository. A tokenless upload can claim
only the fork's own branch.

## What can act on the result

Codecov's GitHub app writes commit statuses and check-run annotations; `comment: false`
keeps it from commenting on pull requests.
It does not push and changes no code. The statuses are not required checks: a reviewer
reads them. A compromised or down Codecov can show a wrong or missing status, never alter what
merges.

## Pinning

`codecov/codecov-action` is pinned by SHA, checked by zizmor in `YAML · Lint`, and moved by
Dependabot's `github-actions` group. At run time it downloads the Codecov CLI and verifies its
signature (`skip_validation` stays false). `cargo-llvm-cov` comes from the pinned
`taiki-e/install-action`, whose release fixes its version and checksums.

## Residual

- The action downloads the CLI at run time. Its signature is checked against Codecov's key, so a
  compromise of Codecov's key and CDN together would run code in the coverage jobs, which hold
  `contents: read` and `id-token: write` and no other secret.
