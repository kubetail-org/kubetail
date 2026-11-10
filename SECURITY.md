# Security Policy

## Supported Versions

Use this section to tell people about which versions of your project are
currently being supported with security updates.

| Version | Supported          |
| ------- | ------------------ |
| ALL     | :white_check_mark: |

## Reporting a Vulnerability

If you find a security vulnerability please send us an email
(hello@kubetail.com) and we will respond to you ASAP.

## Verifying a Release

Every CLI download and every image carries a build provenance attestation,
signed through Sigstore for the GitHub Actions run that built it. Verify one
with the [GitHub CLI](https://cli.github.com/), naming the release's tag and
the workflow that published it.

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
