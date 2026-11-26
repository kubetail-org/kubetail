# Security record — a run's AWS identity follows its cluster, 18 November 2026

**Subject:** a sandboxed run on a cluster reaches AWS as the profile the cluster's kube-context
names in its `exec` block, not the sidecar's default chain.
[ADR](../adr/2026-11-18-a-runs-aws-identity-follows-its-chats-cluster.md). The living model is
[security-model.md](../security-model.md). Issue [#971](https://github.com/kstackhq/kstack/issues/971).

## What it widens

A chat on an EKS cluster reaches AWS as that cluster's profile, which may be another account than
the default chain's. It reaches nothing the user's machine could not already reach as that
profile: the `exec` plugin signs in as it on every probe. Every other control of
[the AWS record](2026-11-17-sandboxed-aws.md) holds.

## What holds

- **The profile is the user's kubeconfig's.** It is read from the context's `exec` block alone
  (`TestAWSProfileReadsTheExecProvider`); cluster data, the model and the run name none. A run with
  no cluster, or a context naming none, reaches AWS as before (`TestTheAWSMountFollowsTheClustersProfile`).
- **The name cannot rewrite the run's config file.** A name holding a bracket, space, line break,
  `=`, `;` or `#` is no profile (`TestAWSProfile`, `TestTheAWSMountFollowsTheClustersProfile`).
- **The file holds no credential.** The profile's keys are the run's key id, the grant's token, and
  the placeholder secret, as its environment holds (`TestTheAWSConfigFileDeclaresTheProfile`).
- **Each profile's secrets are its own.** Each login has its own turn, cache and `safe` hold
  (`TestALoginReachesAWSAsItsProfile`), and its own bucket list
  (`TestEachProfileHasItsOwnLoginAndBuckets`), so one profile's buckets never pass as another's.
- **The refusal shows no SDK text.** It names the profile tried and, for an SSO profile alone, the
  sign-in command (`TestNoLoginNamesTheProfileTried`, `TestAnSSOProfileWithNoSessionSaysToSignIn`);
  any other error is the proxy's fixed sentence (`TestAnUpstreamFailureCarriesNoText`).

## Residuals

- A profile name reaches the model in a refusal. It is the user's own kubeconfig's text, and
  passes `sandbox.AWSProfile` first.
- A role ARN or another plugin's identity in the `exec` block is not read; such a run uses the
  default chain.
