---
title: Give a sandboxed run a kubeconfig whose server is fixed and whose proxy holds the token
date: 2026-10-17
scope: sidecar
status: Accepted
---

# Give a sandboxed run a kubeconfig whose server is fixed and whose proxy holds the token

## Context

This decision predates this file and was recorded in `sidecar/CLAUDE.md`. A sandboxed command
reaches its cluster through the run's `kubeproxy` grant, which a forwarder exposes on a loopback
port that moves every run. kubectl keys its discovery cache on the server URL, so a URL that
named the port would start every run with a cold cache. The proxy speaks plain HTTP, and
client-go's clientcmd applies a user's credentials only over TLS.

## Decision

`writeKubeconfig` (`internal/tools/bash/kubeconfig.go`) writes one context, named as the cluster
card names it (`kubetail` when the record names none), whose cluster is
`http://cluster.kubetail.invalid` and whose `proxy-url` is
`http://kubetail:<token>@127.0.0.1:<Port()>`: the grant's token rides as the proxy's password. The
user holds nothing. The file is encoded by `clientcmd.Write` (the context name is the user's
text), mode 0600. `.invalid` is never resolved, since client-go sends each request to the proxy
in absolute form.

## Alternatives considered

**The server is the forwarder's port.** The cache under `KUBECACHEDIR` would be cold on every run.

**The token as the user's bearer token.** clientcmd drops it over plain HTTP.

## Consequences

`safe.Redact` must blank the token wherever a command prints the kubeconfig. The kubectl cache is
keyed by the cluster's server UID (`serverKey`), not by the URL, so a context repointed at another
cluster gets a cache of its own.
