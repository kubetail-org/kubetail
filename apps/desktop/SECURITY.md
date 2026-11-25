# Security policy

## Supported versions

kubetail is pre-1.0 and ships from a single line. Fixes land in the next release; there are no
backports to earlier tags, so the latest release is the only supported one.

## Reporting a vulnerability

Report it privately through GitHub's
[private vulnerability reporting](https://github.com/kubetail-org/kubetail/security/advisories/new), or
email **hello@kubetail.com**. Please don't open a public issue for a suspected vulnerability — a
private report gives us the chance to ship a fix before the details are public.

Useful in a report: the version or commit, the platform, what an attacker has to already hold
(a local account, a kubeconfig you can write, a cluster you control), and the steps that show the
behaviour. A proof of concept helps but isn't required.

We'll acknowledge your report and tell you whether we consider it a vulnerability, what the fix
looks like, and when it ships. Credit in the release notes if you'd like it.

## What we already know about

The app's boundaries, and which protections a test actually pins, are in
[`docs/security-model.md`](docs/security-model.md). Dated review records are in
[`docs/security/`](docs/security/) and the open findings are tracked in
[`docs/TODO.md`](docs/TODO.md#security).

Some properties are known and recorded rather than unnoticed, so a report about one tells us
nothing new. The main ones:

- **A kubeconfig is a trust decision.** Enabled contexts are connected automatically and their
  `exec` credential plugins run local programs, so importing a file an attacker controls is code
  execution. Use trusted kubeconfigs only.
- **The cache is plaintext on disk.** It holds full object bodies, may carry credentials outside
  the redaction table, and survives cloud sign-out. It is protected by file mode and ACL, not
  encryption — [by decision](docs/adr/2026-09-02-the-cache-is-ordinary-application-data.md).
- **Code running in the webview holds the full cluster surface**, because the host forwards
  GraphQL operations unexamined — [by decision](docs/adr/2026-09-03-no-graphql-operation-allowlist.md).
- **Anyone with your OS account inherits the app's access.** The controls here do not isolate the
  app from a compromised user account or host process.

A finding that defeats one of these *as designed* — a way to read the cache from another account,
say, or to reach the IPC socket without the user's privileges — is very much in scope.
