---
title: Make an incident a target, a subject and a signal, holding occurrences
date: 2026-11-28
scope: sidecar
status: Accepted
---

# Make an incident a target, a subject and a signal, holding occurrences

## Context

A signal holds for something: a crash loop for a pod's containers, a missing replica for a
Deployment. Pods are replaced all the time, so a failing workload shows new pod names at every
restart. A failure also stops and starts: a crash loop runs between back-offs, and a fixed bug can
come back the next day. The user needs to see one failure as one thing, and to know how often it has
happened, without an alert for each pod.

## Decision

**An incident is a target under one UID, a subject and a signal.** The subject is the integration's:
a key the core compares and never parses, a label every screen shows, and attributes. For a pod's
signal, the subject is the pod's top controller (Pod → ReplicaSet → Deployment), and a pod with no
controller is its own. A subject is never named by its own UID, so a recreated workload keeps its
incident.

**An incident holds occurrences.** An occurrence starts once its signal has held for its open-after
window and ends once it has been absent for its resolve-after window. A signal that returns within
24 hours of the last occurrence's end adds an occurrence to the same incident; after 24 quiet hours
it opens a new incident. An incident keeps its newest 100 occurrences, its first-seen time and a
count of every occurrence.

## Alternatives considered

- **The pod as the subject.** Every restart of a crash-looping pod would be a new incident, and a
  workload of ten replicas ten of them.
- **An incident per episode, with no occurrences.** A failure that flickers would be a new incident,
  and a new alert, each time it came back.
- **A subject keyed by its UID.** A Deployment deleted and applied again would open a new incident
  for the same failure.

## Consequences

The bell counts failures, not pods, and an alert fires once per incident. A custom controller the
owner walk does not judge, such as an Argo `Rollout`, moves its pods' subject once its kind has
listed, which opens a second incident: accepted, since judging every owner kind would leave pods
unknown for good. Re-creating a workload under the same name continues its incident, so a test
that wants a fresh one needs a fresh name or namespace.

## Revisit when

Users need incidents grouped by cause across subjects, or a subject that is not an object's name.
