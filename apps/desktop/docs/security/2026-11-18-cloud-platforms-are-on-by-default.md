# Security record — cloud platforms are on by default, 18 November 2026

**Subject:** each cloud platform's switch, `sandbox.<platform>`, is on until the user turns it off.
[ADR](../adr/2026-11-18-every-cloud-platform-is-on-until-the-user-turns-it-off.md). The living model
is [security-model.md](../security-model.md).

## What it widens

A sandboxed command in any chat reaches every cloud platform whose login is on the machine, as the
user, with no step by the user first. It reads the metadata and state each cloud platform's records
list unasked ([AWS](2026-11-17-sandboxed-aws.md), [GitHub](2026-11-17-a-sandboxed-command-reaches-github.md),
[Google Cloud](2026-11-17-google-cloud-through-the-proxy.md), [Azure](2026-11-17-azure-through-the-proxy.md)).
What it reads reaches the model's provider, and with network on a command can send it anywhere.

## What holds

- A change, and a read of stored data, still ask, Once or Deny. No rule or mode reaches them.
- No credential enters the sandbox. The sidecar resolves it, outside, once a run mounts its cloud
  platform.
- A switch set to `false` blocks new requests at once (`TestSwitchingACloudPlatformOffClosesItsRoute`).
- A value the store cannot read is off, held until a write names the field
  (`TestARefusedSwitchIsOff`), so a hand edit can cost access, never grant it.
- Unset is on (`TestEachCloudPlatformIsOnWhenUnset`); `false` is off
  (`TestASwitchTheFileTurnsOffIsOff`).

## Residual risk

A login on the machine is now consent. A user who signed into `gh`, `gcloud`, `az` or AWS for
another purpose gives every chat's agent read access to that account until they set the switch to
`false` in `settings.json`, the one way to turn it off until Settings draws the switches.
