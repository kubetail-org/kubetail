---
title: A cluster is a row in app.db, mirrored into a runtime object it is deleted through
date: 2026-09-16
scope: cross-cutting
status: Accepted
---

# A cluster is a row in app.db, mirrored into a runtime object it is deleted through

## Context

Clusters lived in beehive. The importer created a `Cluster` object per kube-context, the
object's spec held the user's toggles beside the source, its beehive `ObjectID` was the
cluster's identity, and `clusterDelete` swept the cluster's chats by hand and then deleted
the object. Three things pushed against that. Chats are rows in `app.db` filed under a
cluster, so their foreign key pointed at nothing the file could enforce, and the
sweep-then-delete order had a crash window nothing retried. A cloud-synced cluster is
coming, with an id the cloud mints and choices that sync, which a beehive `AUTOINCREMENT`
id and a spec only this process writes cannot carry. And every user choice written into a
beehive spec woke a controller pass, for a rename or a monitoring toggle no pass acts on.

## Decision

**The `clusters` table in `app.db` owns a cluster**: its id (a UUIDv7 the app mints, or the
cloud's), source and source key, display name, three toggles, two stamps and the deletion
mark. **Beehive holds one runtime object per row**, named by the row's id, whose spec is the
four fields the cluster and cache passes act on (source, source key, enabled, sync enabled)
and nothing else. The mirror (`services/cluster/mirror.go`) is the only writer of that spec. A
read projects the row plus the object's status and conditions; the list watch rereads both
on either source's signal and diffs with `deltafold`. `ClusterID` is text on the wire, its
own scalar; `ObjectID` still names a cache or a per-kind record.

**Deletion is a mark and two loops.** `clusterDelete` marks the row and returns. The mirror
asks the runtime object to go and, once beehive has collected it, removes the row, guarded
on no chat still referencing it. The chat service subscribes to the same key, deletes a
marked cluster's chats (cancelling and joining turns) and notifies after real cleanup, which
brings the mirror back. Every send checks the cluster's mark inside its writer transaction,
so acceptance is serialized against the mark. A cache carries a finalizer from creation that
its controller clears only after the store file is removed, so an absent cluster object
proves every cache file is gone. A marked row keeps its `(source, source_key)` claim, so the
importer cannot re-create the context mid-teardown.

This replaces the identity and deletion halves of
[beehive owner chain with ObjectID identity](2026-08-09-beehive-control-plane.md). The owner
chain, the finalizers and the creation-only importer rule stand.

## Alternatives considered

**Keep beehive as the source of truth and add a chat finalizer on the cluster object.**
Rejected: the cluster controller would own a cascade over rows it cannot name, the chat
foreign key would still point outside the file, and a cloud cluster's id and choices would
still have no home a sync could write.

**Keep the user's choices in the runtime spec too.** Rejected as a dual write: two owners of
one field, and a rename or a monitoring toggle waking passes that do not read them.

**Delete synchronously in the resolver: sweep chats, delete the object, delete the row.**
Rejected: a crash between the steps leaves work nothing retries, and a send can land behind
a sweep that has read the chat ids, which the resolver's per-cluster lock was patching.

**A beehive watch as the list watch.** Rejected: it shows only what the spec holds, and the
served record has two sources.

## Consequences

Deletion is crash-recoverable at every durable boundary and needs no resolver gate; both
loops recover at startup from the rows they find marked. A row without an object yet serves
zero status, so a cluster exists the moment the importer inserts it. The mark is promised to
a watcher only when it saw the row unmarked: a mark and a removal landing before one reread
surface as the `Deleted` alone, which the frontend already folds as a removal. `app.db` now
holds kube-context names, which the security model records. The kubeconn trigger finds the
record to wake through the controller's lease index, since a record's name no longer says
which context it tracks.

## Revisit when

A second source (cloud) lands: its rows arrive by sync rather than import, and the mirror's
orphan rule, that an object with no row is torn down, has to hold for rows that appear and
disappear on someone else's clock.
