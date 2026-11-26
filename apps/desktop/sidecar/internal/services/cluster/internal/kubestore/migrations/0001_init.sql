-- Copyright 2024 The Kubetail Authors
--
-- Licensed under the Apache License, Version 2.0 (the "License");
-- you may not use this file except in compliance with the License.
-- You may obtain a copy of the License at
--
--     http://www.apache.org/licenses/LICENSE-2.0
--
-- Unless required by applicable law or agreed to in writing, software
-- distributed under the License is distributed on an "AS IS" BASIS,
-- WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
-- See the License for the specific language governing permissions and
-- limitations under the License.

-- A cluster's cache: one SQLite file per cluster, owned by kubestore. Every
-- Kubernetes object the cluster serves lands in one table under one shape, with
-- its owner refs and labels as rows a query can join, its events beside it, and
-- the full body kept for deep questions. Timestamps are unix millis.
--
-- auto_vacuum=INCREMENTAL, which the janitor's PRAGMA incremental_vacuum needs to
-- return freed pages to the OS, cannot be set here: it must run before any table
-- exists, the migration runner's own schema_migrations included, and SQLite
-- silently ignores it from inside a migration. It is on the writer pool's DSN
-- (sqlitemigrate.OpenPool).

-- cluster_meta: a key/value bag for sync bookkeeping (a kind's last LIST
-- resourceVersion and when it ran), so new metadata needs no migration.
-- WITHOUT ROWID because the row is barely more than its key, which a rowid table
-- would store a second time in an autoindex; nothing indexes it, so no index grows
-- to pay the saving back.
CREATE TABLE cluster_meta (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL
) STRICT, WITHOUT ROWID;

-- seq is the file's write counter, seeded here so nothing initializes it at open.
-- Every write transaction takes the next number and stamps what it writes with it.
INSERT INTO cluster_meta (key, value) VALUES ('seq', '0');

-- objects: one row per Kubernetes object, built-in or CRD — the one entry point for
-- "any object".
--
-- created_at is the object's creationTimestamp; updated_at is the last sync write.
-- write_seq is the position of the last write that changed the object, off the
-- file's counter: it moves only when resource_version does, so a relist that
-- rewrites a kind unchanged leaves every stamp alone. changed_at is that write's
-- time, moving under the same rule; the first list counts as a change, so an object
-- unchanged since the mirror first listed it reads that list's time. A delete takes the row with
-- it; the deletes table keeps the uid.
--
-- status_summary, ready_count, total_count, restart_count and host are cross-kind
-- readings objectsync materializes at write time (its status.go) so the cache is
-- queryable in SQL — "which pods in this namespace are not ready" — without
-- unpacking a body per row. They are not what the dashboard renders: the webview
-- derives its cells from raw_json, which is why the body is served verbatim. Each
-- is nullable, and a kind with no meaningful reading stores NULL rather than a zero
-- that would read as "none ready". Per kind:
--
--   Pod         status: phase or container state ("Running",
--                       "CrashLoopBackOff", "ImagePullBackOff")
--               ready/total: ready vs total containers
--               restart_count: sum across containers
--               host: spec.nodeName
--   Deployment  status: "Available 3/3" / "Progressing 1/3"
--               ready/total: readyReplicas / replicas
--   StatefulSet same as Deployment
--   DaemonSet   ready/total: numberReady / desiredNumberScheduled
--   ReplicaSet  ready/total: readyReplicas / replicas
--   Node        status: "Ready" / "NotReady (DiskPressure)"
--               ready: 1 if Ready=True else 0
--   Service     status: type + cluster_ip
--   PVC         status: phase ("Bound" / "Pending")
--               ready: 1 if Bound else 0
--   Job         status: condition summary
--               ready: 1 if Complete else 0
--   CronJob     status: "<n> active, last run <when>"
--   CRD         status: best-effort from .status.conditions[].type/status
--
-- raw_json is the full object, zlib-compressed (kubestore's compressRaw /
-- decompressRaw), so a BLOB. The format is self-identifying — a zlib stream begins
-- with 0x78, plain JSON with '{' — so it carries no version prefix; one can be
-- added without a migration.
--
-- A rowid table, unlike the key-only tables here: WITHOUT ROWID wants small rows,
-- and raw_json sitting in overflow pages the identity read never touches is what
-- keeps the objects watch's per-ping read cheap.
CREATE TABLE objects (
    uid              TEXT PRIMARY KEY,
    api_version      TEXT NOT NULL,
    kind             TEXT NOT NULL,
    namespace        TEXT NOT NULL DEFAULT '',
    name             TEXT NOT NULL,
    resource_version TEXT NOT NULL,
    generation       INTEGER NOT NULL DEFAULT 0,
    created_at       INTEGER NOT NULL,
    updated_at       INTEGER NOT NULL,
    write_seq        INTEGER NOT NULL,
    changed_at       INTEGER NOT NULL,
    status_summary   TEXT,
    ready_count      INTEGER,
    total_count      INTEGER,
    restart_count    INTEGER,
    host             TEXT,
    raw_json         BLOB NOT NULL
) STRICT;
CREATE INDEX objects_kind_ns_name ON objects(api_version, kind, namespace, name);
CREATE INDEX objects_kind_host    ON objects(api_version, kind, host);
CREATE INDEX objects_kind_ready   ON objects(api_version, kind, ready_count, total_count) WHERE ready_count IS NOT NULL;
CREATE INDEX objects_ns_kind      ON objects(namespace, api_version, kind);
-- "What moved in this kind past position X" as a range scan, not a scan of the kind.
CREATE INDEX objects_kind_seq     ON objects(api_version, kind, write_seq);

-- owner_refs: the ownership graph (Deployment → ReplicaSet → Pod, Job → Pod,
-- CRD → CRD), extracted from metadata.ownerReferences so a JOIN replaces JSON
-- parsing.
--
-- WITHOUT ROWID: the row is its key plus a flag, and ordering rows by that key puts
-- one child's refs contiguous, so the write path's WHERE child_uid = ? is a single
-- descent of the table rather than an autoindex probe and a rowid fetch per row.
-- owner_refs_owner pays part of it back, carrying the full key where a rowid table
-- would carry 8 bytes; a wide index here is what would turn the trade.
CREATE TABLE owner_refs (
    child_uid     TEXT NOT NULL,
    owner_uid     TEXT NOT NULL,
    is_controller INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (child_uid, owner_uid)
) STRICT, WITHOUT ROWID;
CREATE INDEX owner_refs_owner ON owner_refs(owner_uid);

-- labels: one row per (object, key), so Service-selector resolution — "which Pods
-- match these key/value pairs?" — is a JOIN.
--
-- WITHOUT ROWID, for the reason owner_refs is: a small row keyed by a uid prefix,
-- so rewriting one object's labels is one contiguous descent. labels_kv carries
-- (key, value, uid) where a rowid table's would carry (key, value, rowid).
CREATE TABLE labels (
    uid   TEXT NOT NULL,
    key   TEXT NOT NULL,
    value TEXT NOT NULL,
    PRIMARY KEY (uid, key)
) STRICT, WITHOUT ROWID;
CREATE INDEX labels_kv ON labels(key, value);

-- containers: one row per container of a core Pod, init containers included, its spec and
-- its status joined by name, written with the Pod as its labels are. The quantities are
-- parsed: cores for CPU, bytes for memory. container_id is the runtime's id of the instance
-- the state describes, NULL while it waits, and last_container_id that of the one before it:
-- each names one log.
-- Read from the sanitized body, so the table holds nothing the stored body does not.
--
-- A rowid table, as objects is: a row is about 250 bytes, past the size WITHOUT ROWID
-- suits, and a column added later only widens it.
CREATE TABLE containers (
    pod_uid           TEXT NOT NULL,
    init              INTEGER NOT NULL,
    position          INTEGER NOT NULL,
    name              TEXT NOT NULL,
    sidecar           INTEGER NOT NULL,
    image             TEXT,
    image_id          TEXT,
    container_id      TEXT,
    ready             INTEGER,
    restarts          INTEGER,
    state             TEXT,
    reason            TEXT,
    exit_code         INTEGER,
    last_reason       TEXT,
    last_exit_code    INTEGER,
    last_container_id TEXT,
    cpu_request       REAL,
    cpu_limit         REAL,
    memory_request    REAL,
    memory_limit      REAL,
    PRIMARY KEY (pod_uid, init, position)
) STRICT;

-- refs: one row per by-name reference an object makes — a Pod's volumes and env, an
-- Ingress's backends, a binding's role — written with the object as its labels are, read
-- from the sanitized body by refs.go's table. path is where the reference was found, indices
-- included; to_namespace is empty for a cluster-scoped target; key is a key reference's key.
--
-- WITHOUT ROWID, for the reason labels is: a small row keyed by a uid prefix. refs_to is the
-- reverse question, "who names this object".
CREATE TABLE refs (
    uid          TEXT NOT NULL,
    path         TEXT NOT NULL,
    to_group     TEXT NOT NULL,
    to_kind      TEXT NOT NULL,
    to_namespace TEXT NOT NULL,
    to_name      TEXT NOT NULL,
    key          TEXT,
    optional     INTEGER NOT NULL,
    PRIMARY KEY (uid, path)
) STRICT, WITHOUT ROWID;
CREATE INDEX refs_to ON refs(to_group, to_kind, to_namespace, to_name);

-- selectors and selector_terms: the Pod selector an object carries — a Service's, a
-- workload's, a PodDisruptionBudget's, a NetworkPolicy's — as apimachinery reads it, written
-- with the object by selectors.go. A selector is its namespace and every requirement a Pod
-- must meet, one term each: op is In, NotIn, Exists or DoesNotExist, and vals a JSON array.
-- A selector with no terms matches every Pod in its namespace; one that selects nothing has
-- no row.
--
-- WITHOUT ROWID, for the reason labels is. selectors_ns finds the selectors in one Pod's
-- namespace.
CREATE TABLE selectors (
    uid       TEXT PRIMARY KEY,
    namespace TEXT NOT NULL
) STRICT, WITHOUT ROWID;
CREATE INDEX selectors_ns ON selectors(namespace);

CREATE TABLE selector_terms (
    uid  TEXT NOT NULL,
    term INTEGER NOT NULL,
    key  TEXT NOT NULL,
    op   TEXT NOT NULL,
    vals TEXT NOT NULL,
    PRIMARY KEY (uid, term)
) STRICT, WITHOUT ROWID;

-- events: apart from objects because an event has its own columns (first_seen,
-- last_seen, count) and its own access pattern — filtered by involved_uid and time
-- range, at high volume. Core v1 and events.k8s.io/v1 events land in this one row
-- shape, so the row carries no api_version of its own.
--
-- resource_version and write_seq are the pair objects carries, so "did it change"
-- is one question on both tables. involved_uid is nullable: an event can name its
-- object before that object's UID has been observed. type is Normal or Warning.
-- raw_json is the zlib-compressed event, like an object's.
--
-- Keeps its rowid: events_fts is declared content_rowid='rowid' and its triggers
-- insert new.rowid/old.rowid, which a WITHOUT ROWID table does not have.
CREATE TABLE events (
    uid              TEXT PRIMARY KEY,
    resource_version TEXT NOT NULL,
    write_seq        INTEGER NOT NULL,
    involved_uid     TEXT,
    involved_kind    TEXT,
    involved_ns      TEXT,
    involved_name    TEXT,
    type             TEXT,
    reason           TEXT,
    message          TEXT,
    first_seen       INTEGER,
    last_seen        INTEGER,
    count            INTEGER,
    raw_json         BLOB NOT NULL,
    updated_at       INTEGER NOT NULL
) STRICT;
CREATE INDEX events_involved        ON events(involved_uid, last_seen DESC);
CREATE INDEX events_kind_ns_name    ON events(involved_kind, involved_ns, involved_name, last_seen DESC);
-- The events snapshot's whole order, tiebreak included and in the same direction:
-- the read is unbounded by design, and an index that stops at last_seen leaves the
-- uid term to a temp b-tree — a full sort of the table on every snapshot.
CREATE INDEX events_last_seen       ON events(last_seen DESC, uid DESC);
CREATE INDEX events_seq             ON events(write_seq);
-- The namespace scope counts the events about a namespace's objects off this index
-- alone. events_kind_ns_name holds involved_ns too, but behind involved_kind.
CREATE INDEX events_involved_ns     ON events(involved_ns);

-- events_fts: full-text search over reason and message, so "anything
-- ImagePullBackOff anywhere?" is one query. The triggers keep it in step with
-- events.
CREATE VIRTUAL TABLE events_fts USING fts5(
    reason, message,
    content='events',
    content_rowid='rowid'
);
CREATE TRIGGER events_fts_insert AFTER INSERT ON events BEGIN
    INSERT INTO events_fts(rowid, reason, message) VALUES (new.rowid, new.reason, new.message);
END;
CREATE TRIGGER events_fts_delete AFTER DELETE ON events BEGIN
    INSERT INTO events_fts(events_fts, rowid, reason, message) VALUES('delete', old.rowid, old.reason, old.message);
END;
CREATE TRIGGER events_fts_update AFTER UPDATE ON events BEGIN
    INSERT INTO events_fts(events_fts, rowid, reason, message) VALUES('delete', old.rowid, old.reason, old.message);
    INSERT INTO events_fts(rowid, reason, message) VALUES (new.rowid, new.reason, new.message);
END;

-- status_history: the status_summary transitions of each object, so "when did this
-- Pod start CrashLooping?" needs no log-scraping. A row is written only when the
-- summary changes, never per sync write; the writer drops consecutive duplicates.
-- A plain rowid table with no (uid, at) key, so two transitions landing in the same
-- millisecond both survive.
CREATE TABLE status_history (
    uid     TEXT NOT NULL,
    at      INTEGER NOT NULL,
    summary TEXT NOT NULL
) STRICT;
CREATE INDEX status_history_uid_at ON status_history(uid, at DESC);

-- deletes: one entry per row a reader can no longer reach — a deleted object or
-- event, and an object whose identity moved out of the kind it was read under
-- (objects_identity_change). A write's position rides the row itself as write_seq,
-- but the row is gone by the time a reader learns of a delete, so the uid is kept
-- here. Identity only: the reader holds the row's last-known state and keys the
-- removal by uid. seq is the counter write_seq is stamped from; at is for the
-- retention sweep. An event logs under the fixed ('v1', 'Event') the count
-- triggers use, since the events table has no api_version of its own.
--
-- The janitor trims by age and records how far per kind, so a cursor at or below
-- its kind's mark can no longer be trusted to have seen every delete above it.
CREATE TABLE deletes (
    seq          INTEGER NOT NULL,
    api_version  TEXT NOT NULL,
    kind         TEXT NOT NULL,
    uid          TEXT NOT NULL,
    at           INTEGER NOT NULL
) STRICT;
CREATE INDEX deletes_kind_seq ON deletes(api_version, kind, seq);

-- kind_catalog: the kinds this cache holds, built-ins and CRDs. Each kind's sync
-- registers its row when its worker starts and removes it when the kind stops
-- being synced, so the catalog is "what is mirrored here", not "what the cluster
-- advertises". It answers "what kinds exist?" and "is there a CRD called
-- Application?" without another discovery, and carries a CRD's OpenAPI schema
-- when one is available.
--
-- The object reads depend on it: objects is keyed by kind, while a watch is opened
-- on the plural resource, and the reads resolve one to the other through this
-- table. A kind with rows and no catalog row reads as empty, under an empty Kind,
-- which tells a cursor-holding reader its identity is gone rather than that
-- nothing moved.
--
-- resource is the plural lowercase URL form ("pods"); scope is "Namespaced" or
-- "Cluster". schema_json is a CRD's OpenAPI v3 schema, NULL for a built-in.
-- printer_columns is a CRD's additionalPrinterColumns for this version, as the
-- JSON the webview renders from; NULL for a built-in and for a CRD declaring none.
--
-- A rowid table: schema_json holds a CRD's whole schema, the wide row WITHOUT ROWID
-- is wrong for.
CREATE TABLE kind_catalog (
    api_version     TEXT NOT NULL,
    kind            TEXT NOT NULL,
    resource        TEXT NOT NULL,
    scope           TEXT NOT NULL,
    is_crd          INTEGER NOT NULL,
    schema_json     TEXT,
    printer_columns TEXT,
    PRIMARY KEY (api_version, kind)
) STRICT;

-- The plural is the other direction of the same identity and must be unique too:
-- the object reads resolve (api_version, resource) back to a Kind through it, and
-- with two matching rows SQLite would answer with an arbitrary index-first one —
-- the kind's table then reads empty forever while its sync is perfectly healthy.
-- Within one api group-version a plural names exactly one Kind, so this is the
-- invariant: a CRD whose Kind is renamed while the sidecar is down would otherwise
-- leave the old row beside the new, since the in-process cleanup that drops it
-- needs the previous worker running to know what it was. The upsert clears the
-- loser as it writes (stmtResolveKindRename).
CREATE UNIQUE INDEX kind_catalog_api_resource ON kind_catalog(api_version, resource);
-- KubeQuery's refs view finds a target's versions by its kind.
CREATE INDEX kind_catalog_kind ON kind_catalog(kind);

-- kind_counts: the object count per (api_version, kind), maintained by the
-- triggers below. The dashboard nav shows one per kind, and a grouped COUNT of
-- objects joined against kind_catalog would be O(objects) on every write ping;
-- this makes it O(kinds), a point join with no object scan.
--
-- It is kept apart from kind_catalog because the triggers must work whether or not
-- the kind has a catalog row at that moment: dropping a kind deletes its catalog
-- row in the same transaction as its objects, and the events triggers key on a
-- fixed ('v1', 'Event') that only the Event sync's own registration puts in the
-- catalog. Keyed on (api_version, kind) alone, it stays exactly consistent with
-- objects within each write transaction, whatever the catalog is doing.
--
-- WITHOUT ROWID: two key columns and a counter, one row per kind, and no secondary
-- index to pay the saving back.
CREATE TABLE kind_counts (
    api_version TEXT NOT NULL,
    kind        TEXT NOT NULL,
    count       INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (api_version, kind)
) STRICT, WITHOUT ROWID;

-- A new object bumps its kind's counter. An update of an existing object goes
-- through INSERT ... ON CONFLICT(uid) DO UPDATE, which fires the UPDATE trigger
-- and not this one, so only a genuinely new row counts.
CREATE TRIGGER objects_kind_count_insert AFTER INSERT ON objects BEGIN
    INSERT INTO kind_counts (api_version, kind, count)
    VALUES (new.api_version, new.kind, 1)
    ON CONFLICT(api_version, kind) DO UPDATE SET count = count + 1;
END;

-- A deleted object (watch delete, relist prune, or orphaned-kind eviction) takes
-- one off its kind's counter. The row is left at 0 rather than removed: a kind
-- still advertised but empty should read 0, and re-adding is cheaper than churning
-- the row.
CREATE TRIGGER objects_kind_count_delete AFTER DELETE ON objects BEGIN
    UPDATE kind_counts SET count = count - 1
    WHERE api_version = old.api_version AND kind = old.kind;
END;

-- An identity change is a departure from one kind and an arrival in another: a
-- group-version flip reaches the upsert before the old kind's ClearKind. One
-- trigger owns everything the departure owes, so the WHEN defining "identity
-- changed" is stated once. The count moves to the new kind, and a delete is logged
-- under the kind the row left — a reader takes a kind's rows and its deletes by
-- (api_version, kind), so without the entry the row would be in neither range: it
-- stops matching the old kind's rows, and nothing took it away. The position is
-- new.write_seq, which the upsert's CASE moves for exactly this case.
CREATE TRIGGER objects_identity_change AFTER UPDATE ON objects
WHEN old.api_version <> new.api_version OR old.kind <> new.kind BEGIN
    UPDATE kind_counts SET count = count - 1
    WHERE api_version = old.api_version AND kind = old.kind;
    INSERT INTO kind_counts (api_version, kind, count)
    VALUES (new.api_version, new.kind, 1)
    ON CONFLICT(api_version, kind) DO UPDATE SET count = count + 1;
    INSERT INTO deletes (seq, api_version, kind, uid, at)
    VALUES (new.write_seq, old.api_version, old.kind, old.uid, new.updated_at);
END;

-- Events live in their own table, so the objects triggers never count them and
-- the nav's Events kind would read 0 forever; these two keep its kind_counts row.
-- The key is the fixed ('v1', 'Event'): the events table holds core v1 and
-- events.k8s.io/v1 events in one row shape, and both the Event sync's kind_catalog
-- row and the webview's curated leaf join on core ('v1', 'Event'), so every cached
-- event rolls into that one key. That catalog row is what makes the count
-- reachable — store.Kinds is a kind_catalog LEFT JOIN, so a count with no catalog
-- entry beside it is invisible. There is no UPDATE trigger: an event's kind never
-- changes, and a re-observed event upserts via ON CONFLICT(uid) DO UPDATE, which
-- fires no trigger here and leaves the count alone.
CREATE TRIGGER events_kind_count_insert AFTER INSERT ON events BEGIN
    INSERT INTO kind_counts (api_version, kind, count)
    VALUES ('v1', 'Event', 1)
    ON CONFLICT(api_version, kind) DO UPDATE SET count = count + 1;
END;

CREATE TRIGGER events_kind_count_delete AFTER DELETE ON events BEGIN
    UPDATE kind_counts SET count = count - 1
    WHERE api_version = 'v1' AND kind = 'Event';
END;
