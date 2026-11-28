---
title: Gate detection with the target's switch, and unattended runs with the triage setting
date: 2026-11-28
scope: cross-cutting
status: Accepted
---

# Gate detection with the target's switch, and unattended runs with the triage setting

## Context

`clusters.monitoring_enabled` was a per-cluster switch with nothing behind it, and `RunMonitor`
checked it in the transaction that inserts a run. Detection reads the local cache and costs nothing,
so it should be on without the user asking. A monitor run spends model calls and reads the cluster
with nobody watching, so it must not start because detection is on.

## Decision

**`monitor_targets.enabled` gates detection.** It defaults to on, and turning it off ends the
target's open occurrences as `stopped` and drops its baseline. It still gates every run: the
monitor's admit, called by `RunMonitor` inside the run's insert, refuses a target that is off, or
was switched since the triage was queued.

**What starts a run is separate.** A manual triage needs the user's press. An automatic one needs
`monitoring.triage.model` set, off by default and turned on only by ticking the box beside the press,
and is checked again when each run starts: a write of the setting fails the automatic triages not
yet started, as `setting_changed`. At most 6 automatic triages start per target per hour.

## Alternatives considered

- **One switch for both.** With detection on by default, every cluster would start runs and spend
  model calls unasked; with it off by default, nothing would be detected.
- **Automatic triage per target.** It is a model, a cost and a trust decision, which the user makes
  once; a per-target setting can come later.
- **A setting read only when a triage is queued.** A queued triage would run under a model the user
  had since changed or turned off.

## Consequences

Detection is free and on everywhere; spending is the user's choice, in one place. The cap and the
recheck are written to `monitor_triage_starts` and the triage's rows, so they outlive a restart. A
model's `DISMISS` from an automatic triage silences the notification, by decision; the incident stays
listed with its report.

## Revisit when

Users want triage limited to some targets, or a spending budget in place of the cap.
