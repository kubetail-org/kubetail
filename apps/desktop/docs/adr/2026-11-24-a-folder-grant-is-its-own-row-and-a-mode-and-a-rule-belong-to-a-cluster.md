---
title: A folder grant is its own row, and a mode and a rule belong to a cluster
date: 2026-11-24
scope: sidecar
status: Accepted
amended_by:
  - [Decide every target's request by access level, a boundary, a mode and rules over one action](2026-11-25-permissions-are-access-levels-a-boundary-modes-and-rules-over-one-action.md)
---

# A folder grant is its own row, and a mode and a rule belong to a cluster

## Context

The approval modes and the always rules lived in `settings.json`, and a chat's rules in
`chat_grants` as JSON. A folder grant was a `permissions.Rule` naming a `Folder`
([a folder grant is a rule](2026-10-04-a-folder-grant-is-a-rule.md)), so every rule check
also had to say that such a rule matches no action. A mode and a rule were keyed by a
kube-context pattern, though Kubetail only ever wrote an entry naming one context exactly. A rule
outlived its cluster, so a context added again under an old name inherited its `allow` rules. The file was built
to survive a hand edit, and nobody edits it, so a value Kubetail could not read held the whole
field in its strictest state ([settings are rows in app.db](2026-11-24-settings-are-rows-in-app-db.md)).

## Decision

The modes, the rules and the folder grants are rows in `app.db` (migration `0006`):

- **A mode is a `permissions.approval_mode` row of `settings`**
  ([settings are rows in app.db](2026-11-24-settings-are-rows-in-app-db.md)): one per cluster,
  keyed by the cluster's record (`clusters.id`), and a default row with `cluster_id` NULL. A
  cluster with no row takes the default, else `ask`. The mode goes with the cluster's record.
- **`kube_rules`**: every Kubernetes rule, an always rule with `chat_id` NULL, a chat's with it
  set. A rule names one cluster's record (`cluster_id`) and goes with it, or names none and
  applies to every cluster. A rule for every cluster may only restrict: no `allow` names none.
  Its `CHECK`s refuse every shape Kubetail cannot apply, so no stored rule is refused on read.
- **`folder_grants`**: a folder grant is a `FolderGrant` of its own, an always one with `chat_id`
  NULL, one row per folder in each place. `permissions.Rule` loses `Folder`.

`services/settings` writes the rows whose `chat_id` is NULL and `agent/chat` a chat's. A read
that fails is never an empty result: rules read as `permissions.Unread`, a mode as read-only,
the folder grants as an error. `session.Session.Policy` takes the action's cluster id, and
`kubeproxy.NewGrant` takes it beside the context. A `permissions.Action` carries both: the id
for its mode and rules, the context for its words. This replaces the rule half of
[a folder grant is a rule](2026-10-04-a-folder-grant-is-a-rule.md). Its walk by handle and its
checks stand.

## Alternatives considered

- **Keep the modes and rules as patterns over a context.** Kubetail writes one exact name per
  entry, so the order and the patterns served only a hand edit. A cluster's id also survives a
  context leaving the kubeconfig and coming back, and is never another cluster's.
- **Keep a rule's context beside its cluster.** A rule naming many clusters by pattern
  (`prod-*`) would then have two ways to name one. Nothing writes a pattern but the Settings
  form, so a pattern can come back as its own change.
- **Make `cluster_id` NOT NULL.** A deny or ask rule for every cluster would have to be written
  once per cluster, and a cluster added later would start without it.
- **Keep a folder grant a rule, in `kube_rules`.** The table's class and Secret-read `CHECK`s
  would each need a folder case, and every reader would still filter folders out.
- **Copy the old rows into the tables.** Kubetail has not shipped to users, [by decision](2026-11-24-settings-are-rows-in-app-db.md).

## Consequences

Deleting a cluster deletes its mode and its rules. A context added again under an old name is a
new cluster: it takes the default mode and no rule but those for every cluster. A rule's line
names its cluster by the record's kube-context, so the graph reads the record to word it. The
Settings form picks a cluster, not a pattern. A stored rule always applies, and no rule holds
another back. A failed read of a chat's rules now refuses Secret reads too. An install of
`desktop/v0.0.1` loses its modes, its rules and every chat's grants. The `CHECK`s and
`ruleRefusal` must refuse the same rules (`TestTheGoCheckAndTheTableRefuseTheSameRules`).

## Revisit when

A rule or a mode is wanted for a group of clusters, by pattern or by name.
