---
title: A run's AWS identity follows its chat's cluster
date: 2026-11-18
scope: sidecar
status: Accepted
---

# A run's AWS identity follows its chat's cluster

## Context

[A sandboxed command reaches AWS through a re-signing proxy](2026-11-17-a-sandboxed-command-reaches-aws-through-a-re-signing-proxy.md)
signs with the SDK's default chain in the sidecar's environment: `AWS_PROFILE` if the login shell
exports it, else the `default` profile. An EKS kubeconfig usually names its profile in the
context's `exec` block instead (`env: AWS_PROFILE=…`, or `--profile …`). A user with no
`AWS_PROFILE` and no `[default]` profile saw `kubectl` read the cluster while every AWS call was
refused, and the model's `aws --profile <name>` failed in the run, whose config file defined
`[default]` alone. Issue #971.

This ADR replaces the parent's "the SDK's default chain" in its first decision; the rest stands.

## Decision

- **The profile is the cluster's.** For a run on a cluster, the AWS mount reads the profile the
  kube-context's `exec` provider names (`kubeconfig.Service.AWSProfile`: `AWS_PROFILE` in `env`,
  else `--profile` in `args`) and reaches AWS as it. A run with no cluster, or a context naming no
  profile, uses the default chain.
- **One login per profile.** The mount keeps an `awsauth.Service` per profile, made on first use,
  each its own chain (`config.WithSharedConfigProfile`), turn, cache and `safe` hold. Each profile
  has its own `awsproxy.Buckets`.
- **The run declares its profile.** `<run>/aws/config` holds `[profile <name>]` with the run's key
  id and placeholder secret, since the AWS CLI ignores the environment's keys once `--profile` names
  a profile. `aws --profile <name>` works; another name fails, as before. A name the file cannot
  hold as a header (`sandbox.AWSProfile`) is no profile.
- **The refusal names what failed.** `awsauth.NoLoginError` names the profile tried, and suggests
  `aws sso login --profile <name>` only for an SSO profile. The proxy shows an `awsproxy.Refusal`'s
  text, and its own fixed sentence for any other error.

## Alternatives considered

- **A profile picker in Settings.** It asks the user for what the kubeconfig already says, and
  leaves a chat on one cluster reaching AWS as another's account.
- **An empty `[profile <name>]` section.** The AWS CLI then finds no credentials, since an explicit
  `--profile` drops the environment's.

## Consequences

- A chat's AWS calls and its `kubectl` act as one identity, in the common case.
- The run's config file holds the grant's token, as its environment already does.
- A profile resolves the first time a run on its cluster reaches AWS; its chain stays loaded, as
  the default chain's does.

## Revisit when

An EKS kubeconfig names its identity some other way the mount should read: a role ARN in the
`exec` args, or a plugin that is not the AWS CLI.
