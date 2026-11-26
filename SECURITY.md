# Security Policy

## Supported Versions

Use this section to tell people about which versions of your project are
currently being supported with security updates.

| Version | Supported          |
| ------- | ------------------ |
| ALL     | :white_check_mark: |

## Reporting a Vulnerability

If you find a security vulnerability please report it privately through
GitHub's [private vulnerability reporting](https://github.com/kubetail-org/kubetail/security/advisories/new),
or send us an email (hello@kubetail.com), and we will respond to you ASAP.
Please don't open a public issue for a suspected vulnerability.

Useful in a report: the version or commit, the platform, what an attacker has
to already hold (a local account, a kubeconfig you can write, a cluster you
control), and the steps that show the behaviour. A proof of concept helps but
isn't required.

## Kubetail Desktop

The desktop app's boundaries, and which protections a test actually pins, are
in [`apps/desktop/docs/security-model.md`](apps/desktop/docs/security-model.md).
Dated review records are in [`apps/desktop/docs/security/`](apps/desktop/docs/security/)
and the open findings are tracked in
[`apps/desktop/docs/TODO.md`](apps/desktop/docs/TODO.md#security).

Some properties are known and recorded rather than unnoticed, so a report
about one tells us nothing new. The main ones:

- **A kubeconfig is a trust decision.** Enabled contexts are connected
  automatically and their `exec` credential plugins run local programs, so
  importing a file an attacker controls is code execution. Use trusted
  kubeconfigs only.
- **The cache is plaintext on disk.** It holds full object bodies, may carry
  credentials outside the redaction table, and survives cloud sign-out. It is
  protected by file mode and ACL, not encryption:
  [by decision](apps/desktop/docs/adr/2026-09-02-the-cache-is-ordinary-application-data.md).
- **Code running in the webview holds the full cluster surface**, because the
  host forwards GraphQL operations unexamined:
  [by decision](apps/desktop/docs/adr/2026-09-03-no-graphql-operation-allowlist.md).
- **Anyone with your OS account inherits the app's access.** The controls here
  do not isolate the app from a compromised user account or host process.

A finding that defeats one of these *as designed*, such as a way to read the
cache from another account or to reach the IPC socket without the user's
privileges, is very much in scope.

## Verifying a Release

Every CLI download, every image and every desktop release file carries a
build provenance attestation, signed through Sigstore for the GitHub Actions
run that built it. Verify one with the [GitHub CLI](https://cli.github.com/),
naming the release's tag and the workflow that published it.

A CLI download, here from `cli/v0.18.0`:

```sh
gh attestation verify kubetail-linux-amd64 \
  -R kubetail-org/kubetail \
  --source-ref refs/tags/cli/v0.18.0 \
  --signer-workflow kubetail-org/kubetail/.github/workflows/release-cli.yml
```

An image, here the Dashboard from `dashboard/v0.17.0`, on Docker Hub or
`ghcr.io/kubetail-org`:

```sh
gh attestation verify oci://kubetail/kubetail-dashboard:0.17.0 \
  -R kubetail-org/kubetail \
  --source-ref refs/tags/dashboard/v0.17.0 \
  --signer-workflow kubetail-org/kubetail/.github/workflows/release-docker.yml
```

The other images are `kubetail-cluster-api`, `kubetail-cluster-agent` and
`kubetail-cli`, each from its own component's tag. An `-alpine` tag is attested
the same way.

A Kubetail Desktop release file, here from `desktop/v0.1.0`:

```sh
gh attestation verify kubetail-desktop-0.1.0-darwin-universal.dmg \
  -R kubetail-org/kubetail \
  --source-ref refs/tags/desktop/v0.1.0 \
  --signer-workflow kubetail-org/kubetail/.github/workflows/release-desktop.yml
```
