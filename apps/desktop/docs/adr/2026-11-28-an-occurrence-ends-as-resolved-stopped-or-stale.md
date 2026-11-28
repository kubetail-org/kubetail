---
title: End an occurrence as resolved, stopped or stale
date: 2026-11-28
scope: sidecar
status: Accepted
---

# End an occurrence as resolved, stopped or stale

## Context

The monitor sees a target only while it can read it. A cache can be syncing or unreachable, a kind
can be paused, the app can be quit and the machine asleep. A signal that is unknown is not absent:
a cut answer or a stalled sync would otherwise read as every failure gone. The user also switches
reads off, on the target, the cluster or a kind, and then nothing will say whether a failure went on.

## Decision

**An occurrence ends in one of three ways**, each stamped with its last-seen time and recorded in
`incident_occurrences.end_reason`:

- **`resolved`**: its signal was judged absent for its resolve-after window. Only an interval
  between two judged folds, at most two cadences apart, counts toward it.
- **`stopped`**: the user switched the read off, or the target's UID changed. It ends at once.
- **`stale`**: nothing saw it for 24 hours, so an incident on a target that never answers again
  does not stay open for good.

A fold applies a new UID first, then stale, then its reading. An unknown signal's occurrences
neither start nor end, and reopening is measured from `ended_at` whatever the reason.

## Alternatives considered

- **Resolve on any reading that does not see the signal.** A syncing cache or a truncated answer
  would resolve every open incident, and the next judged reading would open them again and alert.
- **One end, with no reason.** The screens could not tell a failure that recovered from one nobody
  watched, and a triage could not say which it was.
- **No stale bound.** A cluster deleted from the kubeconfig but kept as a record would hold its
  incidents open, in the bell, for good.

## Consequences

Only *Resolved* means a reading saw the failure recover; the screens say *Last seen* for the others.
Because a stale end is already 24 hours past its stamp, a failure that returns after one always
opens a new incident, and alerts. Unwatched time earns nothing, so a restart resumes each window
from the stored times without counting the gap.

## Revisit when

An integration reports its vendor's own end times, which a reason of the core's would have to carry.
