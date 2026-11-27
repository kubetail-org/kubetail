# Step 1 — Detect

No prerequisite. [The sequence's README](README.md) holds the decisions.

The sidecar reads each monitored cluster's cache on a fixed cadence, folds the signals it finds into incidents and occurrences, and stores them in `app.db`. Nothing reads them yet but tests.

## Design

**The package is `apps/desktop/sidecar/internal/agent/monitor`**, a `lifecycle.Part` built in `app/runtime.go` after `rt.Chat`. `New` takes the `*appdb.DB` and the cluster service; the unexported `newMonitor` also takes its cadence, windows, read bounds and clock, so tests shrink them.

**Three layers, each a file with its test:**

- `signals.go` — one reading of one cluster: `cluster.CachedData().ReadActive` over the tables `main.objects`, `main.containers` and `main.owner_refs`, answering the set of `(subject, signal)` pairs true now and the set of signals the reading could judge. Pure but for the read.
- `fold.go` — a pure function from the last state, a reading and the time to the changes to write: occurrences to start, occurrences to end, incidents to open. It holds the windows: counts-after, the 5-minute absence, the 24-hour reopen.
- `store.go` — the tables, a `sqlstmt.Set`, one transaction per fold, then `Notify(appdb.KeyIncidents)` after commit. A fold that only advances `last_seen_at` changes nothing step 2 serves, and sends no notice.

**The loop** runs one reading per monitored cluster every 30 seconds, and once more when `KeyClusters` pings. A cluster is monitored while its row is live, enabled and `monitoring_enabled`.

### What a reading can judge

`ReadActive` refuses only a cache that has never listed or has failed as a whole: `Trusted()` keeps a stale cache's rows, and a paused kind keeps its rows too. So the reading checks each kind itself.

- `ErrNoCache` and `ErrSyncing` are *no reading*: nothing is judged. A cache whose cluster sync is off reads `Paused` with no kind listed, and judges nothing either.
- **A signal is judged only while every kind it reads is `Watching`.** The reading takes the health `ReadActive` admitted it under, from `CacheReading.Health()`, read just before the rows' snapshot. A kind it reads that is in `UnhealthyKindRefs` (`Stale`, `Resuming`, `Resyncing`, `SyncFailed`, a connection lost) or in `PausedKindRefs` leaves that signal unjudged.
- **`ClusterCacheHealth` gains `PausedKindRefs`**, the kinds the user has switched off, sorted. The health fold skips a paused kind before it reads its state, so no other field names it. Like `UnansweredKindRefs`, it is not on the wire.

  | Signal | Kinds it reads |
  | --- | --- |
  | `CrashLoopBackOff`, `ImagePullBackOff`, `PodPending` | Pod |
  | `NodeNotReady` | Node |
  | `ReplicasUnavailable` | Deployment, StatefulSet, DaemonSet, ReplicaSet, Pod |

  The owner walk reads more kinds, but an owner kind that is not `Watching` only moves a pod signal's subject, as a custom controller does: a user who may not list CronJobs still hears of a crash loop. A kind the cluster does not serve has no record, so it is in neither list and is no obstacle.
- **A statement whose answer reports `More` makes its signals unjudged**: a cut answer would read as signals gone. The bounds are 50,000 rows and 16 MiB per statement.
- **The fold leaves an unjudged signal's pairs as they are**: their occurrences neither start nor end, and their absence time does not run. A reading that fails folds as one that judges nothing.

### The statements

- **Every statement names its tables `main.`** The own-query connection carries KubeQuery's TEMP views, and a bare `objects` or `containers` resolves to the view, which renames `raw_json`, `ready_count` and `total_count`, and takes no `INDEXED BY`.
- **The pod signals read `containers.state` and `containers.reason`**, init containers included. `PodPending` reads the phase from `body(o.raw_json) ->> '$.status.phase'`: `status_summary` holds the worst container reason in place of the phase.
- **A body is decompressed only for a candidate**: a Pod, read through `objects_kind_ns_name`, whose `status_summary` is not `Running`, `Succeeded` or `Failed`, with no container waiting for one of the first two signals. Its age is no test: the window is the fold's, from when the subject was first seen Pending, so a workload whose Pending Pods are replaced keeps its window, and a young Pending Pod still keeps its workload out of `ReplicasUnavailable`. The candidates are a `MATERIALIZED` CTE, and `body()` runs only over it. A healthy Pod reads `Running`, so a reading opens a handful of bodies however many Pods the cluster runs. An unscheduled Pod has no container statuses and reads `ready_count = total_count = 0`, so no test may lean on the counts.
- **The owner walk is a recursive CTE over `main.owner_refs` with `is_controller = 1`**, depth at most 8. The `ancestors` view follows every owner reference, not only controllers. The subject is the last owner the cache holds; a pod with no controller is its own.
- **A custom controller can move a subject.** A pod under a kind the walk does not judge, such as an Argo `Rollout`, takes the `ReplicaSet` as its subject until that kind has listed, then the `Rollout`, which opens a second incident. That gap is accepted: judging every kind an owner reference names would leave the pods of a kind Kubetail cannot list unjudged for good.
- **`ReplicasUnavailable` reads `ready_count < total_count`** (`readyReplicas` against `spec.replicas`, `numberReady` against `desiredNumberScheduled`) and skips a workload whose rollout is in progress, read from its body: a Deployment whose `Progressing` condition is `True` with a reason other than `NewReplicaSetAvailable`, a StatefulSet whose `updateRevision` differs from its `currentRevision` while `updatedReplicas` is below `spec.replicas` less its `partition`, a DaemonSet whose `updatedNumberScheduled` is below `desiredNumberScheduled`. An `OnDelete` workload is never mid-rollout: its update waits on each pod being deleted by hand. A Deployment past its deadline (`ProgressDeadlineExceeded`) counts. A StatefulSet or DaemonSet has no deadline, so one mid-rollout is reported, marked rolling, and waits 15 minutes instead of 5: a rollout that stalls still opens an incident.
- `NodeNotReady` reads `ready_count = 0` on Nodes.

### The windows

The fold keeps, per pair, a first-seen and a last-seen time in memory. While an incident is open, each fold that sees its pair writes the last-seen time to `incidents.last_seen_at`, so a restart keeps it.

- **A pair holds from its first-seen time until it has been absent for 5 minutes.** A shorter gap keeps its first-seen time: a crash loop's `Running` spells between back-offs do not restart the window.
- An occurrence starts when the pair has held for its signal's counts-after window, counting only time its signal was judged, short gaps included, stamped with its first-seen time. It ends when the pair has been absent for 5 minutes, stamped with its last-seen time. The same 5 minutes rule both, so a pair that holds keeps one occurrence open.
- A started occurrence within 24 hours of the incident's last occurrence's end joins that incident; later, it opens a new one. An incident is matched within its server: another server's never joins it.

- **A pair's absence counts only while its signal is judged.** Each judged fold adds the time since the later of the pair's last-seen time and the signal's last judged fold.

**A restart loses the memory.** A cluster's first reading after a start, which names its server, ends any other server's open occurrences and seeds a pair for each of its server's: its first-seen is the occurrence's `started_at`, its last-seen the incident's `last_seen_at`, and the time from then to the start, the app down, is absence already. The time the cache then takes to answer is unjudged. The rules above then apply, so an occurrence whose pair has been absent 5 judged minutes ends when it was last seen, and a crash loop caught between back-offs keeps its occurrence. A pair with no open occurrence starts its window again.

**A context that comes to name another server** ends the old server's open occurrences, each stamped with its last-seen time, and seeds from the new server's, so its windows start over. Both servers' incidents stay listed under the cluster row.

**A fold that fails to commit changes nothing in memory**, so the next reading folds again from the same state.

**Turning a cluster's monitoring off** ends its open occurrences, each stamped with its last-seen time, and drops its pairs from memory. The loop sees the switch only at a tick: turned off and on again between two, it resets nothing. A cluster's row deleted takes its incidents with it.

### Schema

Added to `appdb/migrations/0001_init.sql`:

```sql
CREATE TABLE incidents (
  id                TEXT    PRIMARY KEY,
  cluster_id        TEXT    NOT NULL REFERENCES clusters(id) ON DELETE CASCADE,
  server_uid        TEXT    NOT NULL,
  subject_group     TEXT    NOT NULL,
  subject_kind      TEXT    NOT NULL,
  subject_namespace TEXT    NOT NULL,
  subject_name      TEXT    NOT NULL,
  signal            TEXT    NOT NULL,
  first_seen_at     INTEGER NOT NULL,
  last_seen_at      INTEGER NOT NULL,
  occurrence_count  INTEGER NOT NULL CHECK (occurrence_count >= 1)
) STRICT;

CREATE INDEX incidents_subject ON incidents (cluster_id, server_uid, subject_group, subject_kind, subject_namespace, subject_name, signal);
CREATE INDEX incidents_last_seen ON incidents (last_seen_at);

CREATE TABLE incident_occurrences (
  id          TEXT    PRIMARY KEY,
  incident_id TEXT    NOT NULL REFERENCES incidents(id) ON DELETE CASCADE,
  started_at  INTEGER NOT NULL,
  ended_at    INTEGER,
  UNIQUE (incident_id, started_at)
) STRICT, WITHOUT ROWID;

CREATE UNIQUE INDEX incident_occurrences_one_open ON incident_occurrences (incident_id) WHERE ended_at IS NULL;
```

Both ids are `appdb.NewID()`; an occurrence's is its identity on step 2's event feed. An incident is open while its newest occurrence has no `ended_at`. Its `last_seen_at` is the last time a reading saw its signal; once the incident recovers, it equals the newest occurrence's `ended_at`. The loop deletes an incident that is not open and whose `last_seen_at` is more than 30 days ago, and trims an incident to its newest 100 occurrences in the fold that adds one. `occurrence_count` is every occurrence the incident has had: 1 when it opens, and one more in the transaction that starts each later occurrence. The trim leaves it alone.

`desktop/v0.1.0` has not shipped, so the tables go in `0001_init.sql`. An `app.db` written by `desktop/v0.1.0-rc.1` records version 1 and never gains them: `sqlitemigrate` skips it. A release candidate's data is not carried forward. The monitor prepares its statements when the app builds, so on an `rc.1` `app.db` the sidecar does not start and the start screen names the missing table; the release notes say to delete `app.db`.

### The `monitoring_enabled` switch

**`clusters.monitoring_enabled` defaults to 1 and gates detection.** Its comment in `0001_init.sql`, and the descriptions of `Cluster.monitoringEnabled` and `clusterMonitoringEnabledSet` in `schema.graphqls`, say so.

It does not gate model spending on its own. `chat.RunMonitor` keeps its check, so a cluster with monitoring off takes no run, but from this step every cluster passes it by default. What keeps a run from starting unasked is that `RunMonitor` has no caller: step 4's only caller is the user's press, and step 5 gates unattended runs on the triage setting. A security record says so in this step, and names [the monitoring session record](../../../apps/desktop/docs/security/2026-10-05-the-monitoring-session.md), which describes a run as gated by the switch.

## Tasks

| Task | Status | PR |
| --- | --- | --- |
| 1. Schema and switch | Done | — |
| 2. Signals | Done | — |
| 3. Fold | Done | — |
| 4. Store and loop | Done | — |

1. **Schema and switch.** The two tables and their indexes, `PausedKindRefs` and `CacheReading.Health()`, the default and its descriptions, `KeyIncidents`, the new checksum in `appdb_test.go`'s `shippedMigrations`, the rows in `docs/persisted-data.md`, and the security record.
2. **Signals.** The reading and its judged set, one statement per signal, tested against a real cache store from `cluster/clustertest` seeded with each case, kinds in each state. Each statement that reads `objects` by kind names its index with `INDEXED BY`, so a plan that cannot use it fails to prepare and every statement test pins it. `BenchmarkAReadingOf5000Pods` reads a cache seeded with 5,000 Pods, a few of them failing; the PR records its result, and a reading over 20 ms moves the loop to push from the cache. It reads in about 5 ms.
3. **Fold.** Table-driven tests over the windows and unjudged signals, with time as an argument.
4. **Store and loop.** The transaction, the notice, the retention and the trim, and the wiring into `Runtime`.

## Acceptance

- A Deployment whose pod crash-loops for 2 minutes opens one incident on the Deployment, however many pods it has.
- A crash loop whose container runs for 40 seconds between back-offs opens its incident 2 minutes after it was first seen.
- A container that alternates between `Running` and `CrashLoopBackOff` holds one occurrence open.
- A Deployment whose Pending Pods are replaced every 3 minutes opens a `PodPending` incident, not `ReplicasUnavailable`.
- With CronJobs forbidden, a crash loop is still detected.
- The incident's occurrence ends 5 minutes after the pods recover, stamped with the time the signal was last seen.
- The same signal 1 hour later adds a second occurrence; 25 hours later it opens a second incident.
- A syncing, unreachable or stale cache opens and ends nothing, and nor does one whose cluster sync is off.
- After a restart, 6 minutes of a cache still syncing count as no absence.
- A context whose server changes, while the app runs or while it is down, ends the old server's open occurrences at their last-seen times, and the new server's failure opens an incident of its own.
- A crash loop seen once, then unjudged for minutes, then seen again opens nothing until it has held 2 judged minutes.
- A StatefulSet whose rollout stalls short of ready opens an incident after 15 minutes.
- After a restart 6 minutes after an occurrence was last seen, a first reading that judges nothing keeps the time down: the next judged reading ends it.
- An `OnDelete` DaemonSet and a partitioned StatefulSet that has updated as far as its partition count when short.
- With Pods paused, or the Pods kind `Stale` or `SyncFailed`, no pod signal opens or ends; `NodeNotReady` still does.
- With the Pods kind unjudged for 10 minutes, an open pod incident whose signal is then gone ends 5 judged minutes later, not at once.
- A statement whose answer is cut at the row bound opens and ends nothing for its signals.
- `ReplicasUnavailable` stays quiet for a Deployment whose pod holds `CrashLoopBackOff`, and for one mid-rollout; it counts for one past its progress deadline.
- A `PodPending` Pod whose summary reads `ContainerCreating` is detected, and so is an unscheduled Pod with no container statuses.
- The `PodPending` statement reads Pods through `objects_kind_ns_name` and decompresses no body of a `Running` Pod or one already held by another pod signal.
- After a restart longer than 5 minutes, an open occurrence whose signal is gone ends at the first reading, stamped with the last time a reading before the restart saw it; after a shorter one, an occurrence whose signal returns within 5 minutes of that time stays open. A pair with no open occurrence waits its full window again.
- A fold that only advances an open incident's `last_seen_at` sends no notice.
- A fold whose transaction fails leaves memory as it was, and the next reading writes the same changes.
- With monitoring off for a cluster, no reading runs and its open occurrences have ended, each at its last-seen time.
- An incident with 150 occurrences keeps its newest 100, its first-seen time and an `occurrence_count` of 150.
- A subject name of 10,000 bytes or one holding control characters is stored without failing the fold.

## When it lands

- `apps/desktop/sidecar/internal/agent/CLAUDE.md` gains the monitor package's invariants: a signal is judged only while each kind it reads is `Watching` and no answer was cut, the fold is pure and changes memory only after its commit, the notice follows the commit, and a fold that only advances `last_seen_at` sends none.
- `apps/desktop/docs/persisted-data.md` lists the two tables.
- `apps/desktop/docs/security/<date>-monitoring-on-by-default.md` records that `monitoring_enabled` defaults on and gates detection, and that no caller of `RunMonitor` exists until step 4. `security-model/cluster-proxy.md` gains no row, since no protection moves; KUBE-41 links the record.
- `apps/desktop/docs/TODO.md`'s *Settings* item under *Build the monitoring agent* says the switch gates detection.
