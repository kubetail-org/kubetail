---
title: A sandboxed command reaches AWS through a re-signing proxy
date: 2026-11-17
scope: sidecar
status: Accepted
amended_by: [A run's AWS identity follows its chat's cluster](2026-11-18-a-runs-aws-identity-follows-its-chats-cluster.md)
---

# A sandboxed command reaches AWS through a re-signing proxy

## Context

[The run's door](2026-11-17-a-sandboxed-command-reaches-a-cloud-platform-through-a-proxy-on-its-runs-one-relay.md)
lets a cloud platform's proxy serve a sandboxed run, holding the credential outside it. AWS signs
every request with the caller's secret (SigV4), so a proxy cannot add a credential to a request the way
a bearer token is added: the signature covers the host, the date and the body. Issue #931 asked
for `aws` and the SDKs to work in the sandbox.

## Decision

- **The proxy re-signs.** `run/awsproxy` reads the service and region from the request's SigV4
  scope, drops the sandbox's signature, builds the request for a host of its own table and signs
  it with the user's credentials (`services/awsauth`, the SDK's default chain).
- **The key id is the token.** The run's `AWS_ACCESS_KEY_ID` is `KUBETAIL` and the grant's token;
  its secret is a constant that signs nothing AWS accepts. A client puts the key id in every
  request, so no other slot is needed, and the sandbox's signature is never verified.
- **The services are a list.** Thirteen services, each a row naming its host and the operations
  its classifier lists. A service off the list is refused by name. Adding a row is a security
  change.
- **No upload is approved unshown.** A body is shown or refused: a streamed or multipart upload,
  and any body that is not text, are refused, so a sandbox with no network has no unseen way out.
- **A read of another account's resource asks.** A request is recorded where its resource's owner
  reads it, so a read naming another account's bucket, registry or template URL could carry data
  out with network off. The proxy lists the login's buckets itself; a read of any other asks.
- **`Run.check` passes a placeholder.** `neverEnv` still names every `AWS_` variable, and the
  sandboxed login shell drops them all. `Run.check` lets a name through only with the value a
  mount writes, so a real credential fails the run whoever put it in the environment.

## Rejected

- **Handing the run short-lived credentials** (`AWS_CONTAINER_CREDENTIALS_FULL_URI`, a
  credentials file): the run would hold a credential that works anywhere, for its lifetime.
- **An exception by name in `neverEnv`**: the sandboxed login shell starts from the process's
  environment, so it would pass a key the user exported.
- **Every service the SDK knows**: a read the classifier cannot name would run unasked.

## Consequences

- `aws` and the SDKs work in the sandbox for the listed services; `aws s3 cp` to a bucket, ECR
  logins and `sts assume-role` do not.
- The sidecar carries the SDK's `config` module, with its SSO and STS clients: about 4.6 MB.
- The classifier and the table are hand-written, so an operation AWS adds is classified by its
  name's prefix until a row names it.
