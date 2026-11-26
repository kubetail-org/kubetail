---
title: Remove cloud settings sync until settings have a design
date: 2026-10-06
scope: sidecar
status: Accepted
---

# Remove cloud settings sync until settings have a design

## Context

`internal/services/cloud` synced a settings file to kubetail-cloud: a local JSON file, a durable
queue of patches beside it, an API client and a reconcile engine
([local-first auth](2026-08-09-local-first-auth-settings.md)). It carried `theme` and `locale`, and
nothing in the app read either: the color scheme lives in the host's `host.json`, and the security
settings in `security.json`. Kubetail will launch without settings sync, and when sync comes back it
may sit on rows in `app.db` rather than on files.

## Decision

Delete `internal/services/cloud` and everything that existed only for it: the `--cloud-url` flag
and its `KUBETAIL_CLOUD_API_URL` override, the host's `CLOUD_URL`, and `settings.json` /
`settings-queue.json` in the data directory. This replaces the settings-sync half of
[local-first auth](2026-08-09-local-first-auth-settings.md); its auth half is unchanged.

Sign-in stays: `internal/services/auth`, the tray's and the app bar's account items, and the
gRPC `AuthService`. So does `internal/services/poke`, which the kubeconfig watch and the cluster
sync subscribe to.

## Alternatives considered

**Keep it, unwired.** Dead code still needs its tests kept green and its docs kept true, and its
file-backed shape is the part most likely to change.

## Consequences

Nothing leaves the machine for kubetail-cloud but the OAuth flow. `auth.Service.TokenSource` has no
caller in production until something authenticates against the cloud API again. A future sync
needs its own ADR; the open questions are in `docs/TODO.md` under grouping `security.json`'s keys.
