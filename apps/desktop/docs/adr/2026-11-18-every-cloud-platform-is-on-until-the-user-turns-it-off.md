---
title: Every cloud platform is on until the user turns it off
date: 2026-11-18
scope: sidecar
status: Accepted
amended_by: [Keep every setting as rows in app.db](2026-11-24-settings-are-rows-in-app-db.md)
---

# Every cloud platform is on until the user turns it off

## Context

[A sandboxed command reaches a cloud platform through a proxy on its run's one
relay](2026-11-17-a-sandboxed-command-reaches-a-cloud-platform-through-a-proxy-on-its-runs-one-relay.md)
made each cloud platform's switch, `sandbox.<platform>`, off by default. Nothing draws the switches
yet, so a user who wants an agent to reach GitHub has to edit `settings.json` by hand, and one who
does not know the switches exist gets an agent that says it has no login.

## Decision

This replaces that ADR's *Consent is a switch* bullet.

- **Every switch is on until the user turns it off.** An unset `sandbox.<platform>` reads on; only
  `false` turns it off. A sandboxed run mounts every cloud platform the build has whose switch is
  not off.
- **A refused value reads off.** Off is now the field's restriction, so `strictest` holds a value
  the store cannot read as `false`, as it does for every field that restricts. The value may have
  been the user's off.
- **The rest stands.** A change still asks, Once or Deny, and no rule or mode reaches it. A
  credential stays outside the sandbox. Turning a switch off still blocks new requests at once.

## Alternatives considered

- **Off by default, with a Settings switch first.** It keeps consent explicit, but it leaves the
  feature unreachable until the Settings panel ships.
- **On only where a login is found.** It is the same default in practice, and it runs each CLI to
  decide what to mount.

## Consequences

- A login on the machine is now what lets an agent read the account. On EKS, GKE and AKS the cluster
  credential and the cloud login are one identity, so a chat on such a cluster can read the
  account's metadata unasked, and with network on send it anywhere.
- Every sandboxed run mounts all four cloud platforms, so its first run waits on `gcloud` (up to
  5 s) and `az` (up to 10 s) where they are installed. Later runs use what the services cached.
- Turning one off needs a hand edit until Settings draws the switches (`docs/TODO.md`).

## Revisit when

Settings draws the switches, or a user reports an agent reading an account they did not mean it to.
