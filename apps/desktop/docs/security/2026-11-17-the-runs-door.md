# Security record — the run's door, 17 November 2026

**Subject:** step 1 of the cloud proxies. A sandboxed run's one relay leads to a door that routes
by `Host` to the cluster's grant and to each cloud platform's; the permissions engine decides a
cloud platform's action; the approval request draws a cloud platform's request. No cloud platform is
built, so a command reaches nothing new. The living model is
[security-model.md](../security-model.md); the decision is [the
ADR](../adr/2026-11-17-a-sandboxed-command-reaches-a-cloud-platform-through-a-proxy-on-its-runs-one-relay.md).

## What changes

**The relay leads to a mux, not one grant.** The door hands a request to its `Host`'s route and
answers anything else as the cluster grant answers a request it does not hold: an unknown host,
`localhost:<port>`, a `CONNECT`, a cloud platform whose switch is off (`TestTheDoorRoutesByHost`).
It authenticates nothing. Each grant checks its own token, so one cloud platform's token on
another's route is that grant's refusal (`TestAWrongRouteIsUnauthorized`).

**The engine decides a second kind of action.** A cloud platform's action is `Forbid`, whatever the
mode (`TestACloudPlatformActionAlwaysAsks`), and no cluster rule, nor `Refused` or `RefusedSecrets`,
reaches it (`TestNoRuleReachesACloudPlatformAction`). A session that never asks refuses it, and one
that never reads Secret data refuses its data reads.

**The request draws a second shape.** A cloud platform's request draws its summary, host, details
and raw request through `VisibleText`, labels included, and offers Once and Deny alone.

**A switch per cloud platform widens what a run reaches.** `sandbox.<platform>`, off by default,
read at the run's start and on every request (`TestSwitchingACloudPlatformOffClosesItsRoute`,
`TestARunStartedWhileOffNeverGetsARoute`). Settings draws no switch yet, so only the settings file
or the wire turns one on, behind the same boundary as every settings mutation.

## What holds

- A command cannot reach the sidecar's API: the door serves only its routes.
- No credential is in the sandbox. A grant's token opens one cloud platform's proxy for one run, and
  dies with it (`TestTheRunEndsEveryGrant`).
- An approval is a click on the drawn request. No approval path or rule is added, and a cloud
  platform's action is never grantable.
- `run/sandbox` is untouched: one relay, one port, the same profiles.

## Residuals

Each is a **By decision** row in the model.

- On macOS a run with a cloud platform and no cluster holds a loopback port any local process can
  dial. Without a token every route answers `401`.
- A browser page can send the loopback authority as `Host`. It cannot send the token.
- A read of metadata is unasked, so a secret a user put in metadata no cloud platform's list names
  reaches the model.
- A background command reads the account after its turn ends, until it ends or the switch turns
  off.
- No kube-context's mode reaches a cloud platform, so a read-only context does not stop a cloud
  platform's change. Each one asks.
- A request the user approves is the method, host, path, media type and body; the summary and
  details are a reading of it. A request with an `Unshown` body is approved on the cloud platform's
  sentence for it.
