---
title: A sandboxed command reaches Azure through a token-injecting proxy
date: 2026-11-17
scope: sidecar
status: Accepted
---

# A sandboxed command reaches Azure through a token-injecting proxy

## Context

[Step 1](2026-11-17-a-sandboxed-command-reaches-a-cloud-platform-through-a-proxy-on-its-runs-one-relay.md)
gave a sandboxed run a door that serves a proxy per cloud platform behind its one relay, and decided
to lend a credential through a proxy rather than hand it in. Azure is #934. `az` sends its bearer
token only over `https` (azure-core's `_enforce_https`), so the door's plain HTTP is closed to it,
and `az` reads its login from private files under `AZURE_CONFIG_DIR` (an MSAL token cache and
`azureProfile.json`).

A spike ran a real `az` 2.93 (Python 3.13, which verifies strictly) against a throwaway listener
on Linux: it took its token from a managed-identity endpoint, trusted a per-run certificate
through `REQUESTS_CA_BUNDLE` alone, followed paged and polled links, ran in the real sandbox with
no network, wrote no token to its configuration directory, and retried `429` but not `403`. An
`az` before 2.73 asks IMDS for a managed identity's token instead, so it counts as no login.

## Decision

- **The run holds the grant's token, served as a managed identity.** The run's `azureProfile.json`
  signs each of the user's subscriptions in as a system-assigned identity, and
  `IDENTITY_ENDPOINT`/`IDENTITY_HEADER` point `az` at the door, which answers the grant's token.
  `az` sends it as its bearer; the proxy checks it, drops it, and adds a token the sidecar got
  from the user's `az` outside the sandbox (`services/azureauth`). A run serves the account it was
  mounted with, the one its `az` files were written from.
- **TLS on the door's one listener.** A route is keyed by transport and `Host`; the first byte of
  a connection tells TLS from HTTP. One certificate per run, ECDSA, naming `127.0.0.1` alone, its
  key in memory; `ca.pem` holds it and the system's roots.
- **Resource Manager alone**, in the three public clouds, by a fixed table. No data plane, no
  Microsoft Graph, no `az login`.
- **The gate reads Resource Manager's grammar.** A credential action is refused, an unnamed
  `list`/`get` action is refused, the documented secret-holding metadata is class 6, a short
  destructive list is class 5, and every other change is class 4. Every status the proxy writes
  for itself that `az` would retry is `403`.
- **The configuration directory is the run's `TMPDIR`.** `az` writes its command index and logs
  there; the run directory is read-only and the tool home is shared by a chat's runs.
- **No narrower role.** Entra cannot narrow a user's token per request; the gate is the narrowing.
  A user who wants a hard bound signs a Reader identity into its own `AZURE_CONFIG_DIR` and exports
  it from the login shell.

## Alternatives considered

- **Plain HTTP.** `az` refuses a bearer token on it.
- **A second listener for TLS.** It changes `run/sandbox` and, on macOS, opens a second port.
- **A hand-written MSAL token cache.** A private format, which `az` may change in any release.
- **A real token in the run.** A command could print it, and with network send it anywhere.
- **`HTTPS_PROXY` with interception.** It needs a certificate every client trusts for every host.

## Consequences

- A run with Azure trusts a certificate Kubetail made, through a variable every Python tool reads.
- Every Azure SDK in the run believes it has a managed identity.
- A `POST` that only reads asks as a change; a new list or get action is refused until a table of
  known reads exists.
- `az` cold-starts its command index in each run.
- macOS was not spiked by hand; the Linux spike and the e2e tests on one CI leg pin `az`'s formats.

## Revisit when

`az` drops managed identity over `IDENTITY_ENDPOINT`, a user needs the data plane or Graph, or a
cold start per run is too slow.
