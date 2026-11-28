# Step 1 — Detect

No prerequisite. [The sequence's README](README.md) holds the decisions.

The sidecar lists each integration's targets, reads each monitored one on its integration's cadence, folds the signals it finds into incidents and occurrences, and stores them in `app.db`. This step builds the core, which knows no integration, and the Kubernetes integration. Nothing reads the incidents yet but tests.

## Design

**The package is `apps/desktop/sidecar/internal/agent/monitor`**, a `lifecycle.Part` built in `app/runtime.go` after `rt.Chat`. `New` takes the `*appdb.DB` and a `Registration` per integration; the unexported `newMonitor` also takes its clock, so tests drive time. `runtime.go` passes one, from `monitor/kubesignals`, built over the cluster service.

**The core is three layers, each a file with its test:**

- `fold.go` — a pure function from the last state, a reading and the time to the changes to write: occurrences to start, occurrences to end, incidents to open. It holds the windows: each signal's open-after and resolve-after, the 24-hour reopen and the 24-hour stale bound.
- `store.go` — the tables, a `sqlstmt.Set`, one transaction per fold, then `Notify(appdb.KeyIncidents)` after commit. A fold that only advances `last_seen_at` changes nothing step 2 serves, and sends no notice.
- `monitor.go` — the loop: the reconcile of each integration's targets, then for each monitored target a reading on its integration's cadence, the fold and the store.

**An integration is one registration**, the only thing another integration adds to the sidecar:

```go
// Registration is one integration and what the core needs to know of it.
type Registration struct {
	Name        string        // as monitor_targets.integration holds it
	Cadence     time.Duration // how often one of its targets is read
	Signals     []Signal      // each signal it can find
	Integration Integration
}

// Integration lists and reads the targets of one system, such as Kubernetes.
type Integration interface {
	// ListTargets lists every target the integration has now, readable or not.
	ListTargets(ctx context.Context) ([]Target, error)
	// ListenForTargetChanges receives when ListTargets may answer differently.
	// Notices coalesce. The channel is nil for a listing that never changes,
	// and stops receiving when ctx ends.
	ListenForTargetChanges(ctx context.Context) <-chan struct{}
	// Read takes one reading of a target. An error is a reading that judges nothing.
	Read(ctx context.Context, t Target) (Reading, error)
}

// Target is one thing an integration reads.
type Target struct {
	Key   string // the integration's own; the core compares it and never parses it
	Label string // one line naming the target
}

type Signal struct {
	Name         string        // carries its integration's prefix: kubernetes.crash_loop
	Label        string        // fixed text naming the signal
	OpenAfter    time.Duration // how long it holds before an occurrence starts
	ResolveAfter time.Duration // how long it is absent before the occurrence ends; zero ends it at the next judged reading
	Severity     Severity
}

type Reading struct {
	UID          string                   // what the target reached; "" while it cannot say
	Observations []Observation            // what is true now
	Unknown      map[string]ReadErrorCode // each signal this reading could not judge, by name, and why
}

type Observation struct {
	Signal  string            // its name
	Subject Subject
	Detail  map[string]string // what the integration saw this time
}

type Subject struct {
	Key        string            // the integration's own; the core compares it and never parses it
	Label      string            // one line naming the subject
	Attributes map[string]string // the integration's own facts about the subject
}
```

`Reading` and what it holds are plain data: the fold is a function over them and calls nothing.

- **An incident's common fields are the core's; the rest is the subject's attributes.** The core matches an incident by `Subject.Key`, shows `Subject.Label`, and stores `Attributes` and `Detail` as they come, reading neither. The attributes say what the subject is and are written once, when the incident opens. `Detail` says what was seen and is written with each occurrence.
- **A target is its key and its label, on both sides of the interface.** `ListTargets` returns them and `Read` takes one. The row's id, switch and UID are the core's, on an unexported type of the store: an integration finds what it reads from the key and never sees the row.
- **A subject and a signal are a pair**, the unit the fold tracks. A reading that observes one pair twice keeps the first.
- **A signal is named with its integration** (`kubernetes.crash_loop`), and two integrations never share one. `New` refuses a registration whose signal lacks its name's prefix, and a second registration of one name.
- **A signal a reading leaves out of `Unknown` was judged**: its pairs are the observations that name it, and none means it holds nowhere.
- **The core checks every reading before it folds it.** An observation whose label, attributes or detail is over its bound is dropped, and its signal is unknown for that reading, with `invalid`. The bounds are 256 bytes for a label, 4 KiB for the attributes as JSON and 16 KiB for the detail. An integration cuts what it sends to fit, and the check is the one place the bounds are enforced: no table tests a length, as no other table of `app.db` does, so a value the check let through cannot fail the fold's commit on every reading.
- **A name no registration holds is dropped and logged, and changes no state.** An observation, or an entry of `Unknown`, that names a signal its integration did not register is a bug in the integration. `invalid` is a registered signal's alone, so every signal a target's state names has a label.
- **The core cannot import an integration.** Each imports `monitor` for these types, so Go refuses the cycle. The store and the loop are tested over a fake integration the tests make up.

**The loop** reads each monitored target on its integration's cadence. A target is monitored while its row is `enabled` and this build has its integration. A cluster is read every 30 seconds.

- **Each target is read on a goroutine of its own**, so a slow one delays no other.
- **A reading is bounded by its cadence**: one still running at the next tick is cancelled and folds as one that judges nothing, with `timeout`.
- **A `KeyMonitorTargets` notice wakes the loop to compare generations, never to read.** A target whose generation moved loses its pairs from memory, and reads stay on the cadence. The monitor's own notices would otherwise start a reading each.

### Targets

**An integration lists its targets, and the core keeps their rows.** The reconcile takes what `ListTargets` answers and, in one transaction:

- inserts a row for each new key, enabled, its id from `appdb.NewID()`;
- writes a label that changed;
- deletes each of the integration's rows whose key is listed no more, which takes its incidents.

It notifies `KeyMonitorTargets` after the commit, when anything changed.

- **The loop reconciles at start, on each notice from `ListenForTargetChanges`, and every 5 minutes.** It listens before its first listing, so a change that lands between the two costs a repeated listing, never a missed one. A listing that fails changes nothing and is tried again after one cadence: only an answer removes a target.
- **`ListTargets` is a List and `ListenForTargetChanges` a change notification**, as [the read contracts](../../../apps/desktop/docs/read-contracts/README.md) define them. A notice carries nothing, since there is one thing to re-read. An integration never blocks to send one: a loop that falls behind gets one notice, not one per change.
- **`ListTargets` lists durable records, never what answers now.** A key that leaves the listing takes its incidents and the user's switch with it, so an integration lists what the user has added, reachable or not.
- **A target whose integration this build lacks is never listed**, so never read and never deleted. The store's reads leave it and its incidents out.
- **The Kubernetes integration lists every `clusters` row**: its key is the cluster's id and its label the cluster's name, bounded. Its `ListenForTargetChanges` is fed by `ListenForChanges(appdb.KeyClusters)`, closed when `ctx` ends. A cluster that is disabled or awaiting deletion stays listed, and its reading fails with `disabled`: a target goes only when its cluster's row does, so switching a cluster off keeps its incidents.

**`clusters.monitoring_enabled` moves to `monitor_targets.enabled`**, defaulting to 1. `Cluster.monitoringEnabled` and `clusterMonitoringEnabledSet` give way to `MonitorTarget.enabled` and `monitorTargetEnabledSet` in step 2; this step keeps the old field and mutation resolving through the cluster's target, so nothing in the webview changes yet. A cluster the reconcile has yet to give a target reads as enabled, and setting it is refused. That refusal is accepted: the old mutation lives only on `wip/desktop-monitoring`, until step 2 replaces it with one that takes a target's id.

### From an incident to its cluster

The core holds no cluster's id. An incident names its target, and a Kubernetes target's key is its cluster's id:

```sql
SELECT c.*
FROM incidents i
JOIN monitor_targets t ON t.id = i.target_id
JOIN clusters c        ON t.integration = 'kubernetes' AND c.id = t.key
```

- **The key is opaque to the core alone.** `monitor/kubesignals` reads `Target.Key` as the cluster's id in `Read`, and exports `ClusterID(integration, key string) (string, bool)`, the one function that says so. Step 2's resolver fills `MonitorTarget.clusterID` through it.
- **No foreign key ties a target to its cluster.** The reconcile keeps them in step: a cluster's delete is followed by its target's, in a transaction of its own.
- **An incident names the server it happened on.** `monitor_targets.current_uid` is what the target reaches now: for a cluster, the server's UID. `incidents.target_uid` is what it reached when the incident opened, stamped from `Reading.UID`, and it is part of the incident's match. A failure on another server is another incident, whatever its subject is called.

**A target's UID is what its last reading reached, and it can change.** Each reading names what it reached; a cluster's is the server UID of its active cache (`Spec.ServerUID`). The monitor writes it to `monitor_targets.current_uid` when it first sees it, and the target's first reading under a new one, while the app runs or after a restart:

- ends the target's open occurrences as `stopped`, each stamped with its last-seen time;
- drops the target's pairs from memory, so its windows start over;
- writes the new UID, in the same transaction.

A commit that writes a UID, the first or a new one, notifies `KeyMonitorTargets` as well as `KeyIncidents`: step 2 serves the UID on the target.

A reading that names no UID judges nothing. An integration whose targets have no identity apart from their key returns the key as the UID: every reading names one. The old UID's incidents stay listed under the target, and none of them gains an occurrence while the target reaches another server.

**A target that returns to an earlier UID takes up that UID's incidents.** A failure within 24 hours of one's last occurrence joins it; later, it opens a new one. The baseline went with the first change, so from step 3b the return takes a new one, and a failure found in its window joins as a `baseline` occurrence and raises no alert.

**The read state is the target's too.** `monitor_targets.unknown_signals` holds each signal the last reading could not judge, with its code, as a JSON object. An error from `Read` fills in every signal of the integration with the error's code. The state is derived, never stored: `NeverRead` while `last_read_at` is null, then `Ok` when the object is empty, `Partial` when it names some signals, `Failed` when it names them all. The codes are the core's (`ReadErrorCode`), and an integration maps its own failures onto them:

| Code | Means | For a cluster |
| --- | --- | --- |
| `not_ready` | the target is still loading | no cache yet, a cache or kind that is syncing, resuming or resyncing, or a kind that has not answered this session |
| `disabled` | the user switched the read off | a cluster disabled or awaiting deletion, cluster sync off, a paused kind |
| `unavailable` | the target did not answer | an unreachable server, a stale kind, a failed sync, a lost connection |
| `permission_denied` | the target refused the read | a kind the user may not list |
| `unauthenticated` | the target refused the credentials | a rejected or expired credential |
| `rate_limited` | the target asked the reader to wait | — |
| `unsupported` | the target cannot answer this signal at all | — |
| `truncated` | the answer was cut at its bound | a statement that reports `More` |
| `timeout` | the reading outlived its cadence | set by the loop |
| `invalid` | the reading broke the core's bounds | set by the core's check |

`Read`'s error carries its code, and one that carries none reads `unavailable`. A code outside the core's list, in an error or a reading, reads `invalid`, so every stored code has a value on the wire. The monitor writes `unknown_signals` only when it changes, and notifies `KeyMonitorTargets` after the commit. Each fold also writes `last_read_at`, with no notice. Step 2 serves the signals and the state, and step 3a's empty states read them.

### The Kubernetes integration

`monitor/kubesignals` reads a cluster's cache through `cluster.CachedData().ReadActive`, over the tables `main.objects`, `main.containers` and `main.owner_refs`.

| Signal | Label | True while | Opens after | Severity |
| --- | --- | --- | --- | --- |
| `kubernetes.crash_loop` | Crash loop | a container waits with reason `CrashLoopBackOff` | 2 min | warning |
| `kubernetes.image_pull` | Image pull failing | a container waits with reason `ImagePullBackOff` or `ErrImagePull` | 2 min | warning |
| `kubernetes.pod_pending` | Pod pending | a pod's phase is `Pending` and neither signal above holds | 5 min | warning |
| `kubernetes.node_not_ready` | Node not ready | a node's `Ready` condition is not `True` | 2 min | critical |
| `kubernetes.replicas_unavailable` | Replicas unavailable | a Deployment, StatefulSet or DaemonSet is short of ready replicas, is not rolling, and none of its pods holds a signal above | 5 min | warning |
| `kubernetes.rollout_stalled` | Rollout stalled | a Deployment, StatefulSet or DaemonSet is short of ready replicas, is rolling, and none of its pods holds a signal above | 15 min | warning |

Each resolves after 5 minutes.

**A subject is an API group, a kind, a namespace and a name**, the group empty for the core group and the namespace empty for a cluster-scoped kind. Its key is the four joined with `/` (`apps/Deployment/default/web`), which no Kubernetes name can hold. Its label is the kind and `<namespace>/<name>`, or the name alone: `Deployment default/web`, `Node worker-1`. Its attributes are the four: `apiGroup`, `kind`, `namespace` and `name`.

#### What an occurrence records

An observation's `Detail` says what the reading saw behind the subject. The fold stores the detail of the reading at which the occurrence starts.

| Signal | Detail |
| --- | --- |
| `kubernetes.crash_loop` | `pod`, `container`, `node`, `image`, `restarts`, `lastReason`, `lastExitCode`, `pods` |
| `kubernetes.image_pull` | `pod`, `container`, `node`, `image`, `reason`, `pods` |
| `kubernetes.pod_pending` | `pod`, `node`, `reason`, `message`, `pods` |
| `kubernetes.node_not_ready` | `status`, `reason`, `message` |
| `kubernetes.replicas_unavailable` | `ready`, `desired` |
| `kubernetes.rollout_stalled` | `ready`, `desired`, `updated` |

- **A workload's detail names one pod and counts the rest.** `pods` is how many of the subject's pods hold the signal, and `pod` is the first of them by name, with its container, so the same reading always names the same one.
- **The crash-loop and image-pull details come from `main.containers` and `objects.host` alone**: no body is decompressed for them, so they carry no message.
- **A pending pod's `reason` and `message` are its `PodScheduled` condition's** when that is not `True`, from the body the statement already decompressed; otherwise `reason` is its `status_summary`. An unscheduled pod has no `node`.
- **A node's `status`, `reason` and `message` are its `Ready` condition's**, read from the body of each node the statement found not ready.
- **A value the cluster does not give is left out**, never written empty. Each value is cut to 1 KiB.
- **None of this is in the subject's attributes.** The attributes name the subject and are the same for every occurrence; the detail is what differed this time. A screen can filter incidents by an attribute, and by a detail only within one incident's occurrences.

#### What a reading can judge

**The cache admits a reading as a whole, and the reading then checks each kind.** `ReadActive` refuses a cache that is not `Trusted()`. An admitted cache can still hold a stale kind's rows, or a paused one's, so admission alone judges no signal.

- **A refused cache judges nothing.** `ErrNoCache` and `ErrSyncing` read `not_ready`. A cache whose cluster sync is off reads `Paused` with no kind listed, and judges nothing either, with `disabled`.
- **One kind that has not answered refuses the whole cache.** A cache is `Connecting` until every unpaused kind has reported a state this session, and `Trusted()` refuses `Connecting`. Until then every signal reads `not_ready`, a Node's included, though Nodes may be `Watching`. Cache-wide admission wins: judging kinds one by one under `Connecting` would need a read path beside `ReadActive`, and the state lasts seconds at a start. A kind that never answers keeps the target `Failed` with `not_ready`, and its open occurrences end as `stale`.
- **A signal is judged only when every kind it requires is in `WatchingKindRefs`.** The reading takes the health `ReadActive` admitted it under, from `CacheReading.Health()`, read just before the rows' snapshot. A required kind that is not there leaves its signal unknown:

  | The kind is | Code |
  | --- | --- |
  | in `PausedKindRefs` | `disabled` |
  | in `UnhealthyKindRefs` (`Stale`, `Resuming`, `Resyncing`, `SyncFailed`, a connection lost) | the code the table above gives its state |
  | in `UnansweredKindRefs`, or in no list: discovery has yet to find it | `not_ready` |

- **`ClusterCacheHealth` gains `WatchingKindRefs` and `PausedKindRefs`**, each sorted: the kinds whose state is `Watching`, and the kinds the user has switched off. The health fold skips a paused kind before it reads its state, so no other field names it. Like `UnansweredKindRefs`, neither is on the wire.
- **No Kubernetes signal reads `unsupported`.** Each requires only kinds every cluster serves, so one missing from the cache is one discovery has not reached, never one the cluster lacks.

  | Signal | Kinds it requires |
  | --- | --- |
  | `kubernetes.crash_loop`, `kubernetes.image_pull`, `kubernetes.pod_pending` | Pod |
  | `kubernetes.node_not_ready` | Node |
  | `kubernetes.replicas_unavailable`, `kubernetes.rollout_stalled` | Deployment, StatefulSet, DaemonSet, ReplicaSet, Pod |

  The owner walk reads more kinds, but an owner kind that is not `Watching`, or that the cluster does not serve, only moves a pod signal's subject, as a custom controller does: a user who may not list CronJobs still hears of a crash loop.
- **A statement whose answer reports `More` makes its signals unknown**, with `truncated`: a cut answer would read as signals gone. The bounds are 50,000 rows and 16 MiB per statement.

#### The statements

- **Every statement names its tables `main.`** The own-query connection carries KubeQuery's TEMP views, and a bare `objects` or `containers` resolves to the view, which renames `raw_json`, `ready_count` and `total_count`, and takes no `INDEXED BY`.
- **The pod signals read `containers.state` and `containers.reason`**, init containers included. `kubernetes.pod_pending` reads the phase from `body(o.raw_json) ->> '$.status.phase'`: `status_summary` holds the worst container reason in place of the phase.
- **A body is decompressed only for a candidate**: a Pod, read through `objects_kind_ns_name`, whose `status_summary` is not `Running`, `Succeeded` or `Failed`, with no container waiting for one of the first two signals. Its age is no test: the window is the fold's, from when the subject was first seen Pending, so a workload whose Pending Pods are replaced keeps its window, and a young Pending Pod still keeps its workload out of `kubernetes.replicas_unavailable`. The candidates are a `MATERIALIZED` CTE, and `body()` runs only over it. An unscheduled Pod has no container statuses and reads `ready_count = total_count = 0`, so no test may lean on the counts.
- **The owner walk is a recursive CTE over `main.owner_refs` with `is_controller = 1`**, depth at most 8. The subject is the last owner the cache holds; a pod with no controller is its own.
- **A custom controller can move a subject.** A pod under a kind the walk does not judge, such as an Argo `Rollout`, takes the `ReplicaSet` as its subject until that kind has listed, then the `Rollout`, which opens a second incident. That gap is accepted: judging every kind an owner reference names would leave the pods of a kind Kubetail cannot list unknown for good.
- **A workload is short when `ready_count < total_count`** (`readyReplicas` against `spec.replicas`, `numberReady` against `desiredNumberScheduled`). Short and not rolling, it holds `kubernetes.replicas_unavailable`; short and rolling, `kubernetes.rollout_stalled`. The two never hold together.
- **A workload is rolling while pods of an earlier revision remain**, read from its body. Being short of pods is no evidence of a rollout:

  | Kind | Rolling when |
  | --- | --- |
  | any | `status.observedGeneration` is behind `metadata.generation`: its counts describe an earlier spec |
  | Deployment | `status.replicas` is above `updatedReplicas` |
  | StatefulSet | `updateRevision` differs from `currentRevision` while `updatedReplicas` is below `spec.replicas` less its `partition` |
  | DaemonSet | `updatedNumberScheduled` is below `currentNumberScheduled` |

  An `OnDelete` workload whose controller has seen its spec is never rolling.
- **A workload that cannot make its pods is not rolling.** A Deployment that wants three replicas and holds two, all of its current revision, holds `kubernetes.replicas_unavailable` after 5 minutes, and so does a DaemonSet short of scheduled pods on its current template. A new workload, or a Deployment that recreates its pods, has none of an earlier revision either, and gets the same 5 minutes while its pods start.
- **No statement reads a Deployment's `Progressing` condition.** Its deadline can be set out of reach and a paused Deployment never meets it, so a rolling Deployment that stays short is reported after 15 minutes like any other.
- **A workload whose pods are all updated and not ready is not rolling.** It holds `kubernetes.replicas_unavailable` after 5 minutes: its new pods exist and do not become ready, which is that failure.
- `kubernetes.node_not_ready` reads `ready_count = 0` on Nodes.

### The windows

The fold keeps, per pair, a first-seen time, a last-seen time and two running totals in memory: how long it has held and how long it has been absent. While an incident is open, each fold that sees its pair writes the last-seen time to `incidents.last_seen_at`, so a restart keeps it.

**Only an earned interval adds to a total.** The interval between two consecutive folds of a target is earned for a signal when the signal was judged at both of them and the interval is no longer than twice the cadence. Nothing else counts: not an interval with an unknown fold at either end, and not one the loop did not watch, whether the machine slept, the loop stalled or the app was down. A signal unknown for ten minutes and then judged has earned nothing for those ten minutes.

- **A pair's held total grows by each earned interval while the pair is live**: from the fold that first sees it until it is dropped. A fold that does not see it leaves it live, so a crash loop's `Running` spells between back-offs do not restart the window.
- **A pair's absent total grows by each earned interval that ends at a fold not seeing it**, and returns to zero at a fold that sees it.
- **An occurrence starts at a fold that sees its pair, once the held total has reached its signal's open-after**, stamped with the pair's first-seen time and carrying that reading's detail. A fold that does not see the pair starts nothing, whatever the total: a failure seen once and gone opens no incident, and one seen again inside the resolve-after window opens at that sighting.
- **A fold never both starts and resolves an occurrence.** One that does not see the pair can only resolve it or drop it.
- **An occurrence resolves at a judged fold where the absent total has reached its signal's resolve-after**, stamped with the pair's last-seen time. With a resolve-after of zero that is the first judged fold not seeing it. A pair with no occurrence is dropped at the same point.
- **The fold leaves an unknown signal's pairs as they are**: their occurrences neither start nor resolve.
- **The fold measures time on the wall clock.** A monotonic reading does not advance while the machine sleeps, so the clock the loop passes carries none. A stamp is never earlier than its occurrence's start: a clock set back ends an occurrence at its `started_at`.

**An occurrence ends in one of three ways**, each stamped with its last-seen time and recorded in `end_reason`:

| Reason | When |
| --- | --- |
| `resolved` | a judged signal's pair has been absent for the resolve-after window |
| `stopped` | the target's monitoring was turned off, its UID changed, or its signal is unknown with `disabled` |
| `stale` | its pair was last seen more than 24 hours ago |

- **`stopped` is at once.** The user switched the read off, on the target, the cluster or the kind, and nothing will say whether the failure went on.
- **`stale` is the bound on an unknown signal**, and it is applied before the reading. A fold ends each open occurrence last seen more than 24 hours ago and drops its pair. A reading that then sees the pair starts a new window: no occurrence spans a day nobody watched.
- **A fold applies a changed UID first, then stale, then its reading.** A restart after 30 hours onto another server ends the old occurrences as `stopped`, the more specific reason.
- **Reopening is measured from `ended_at`, whatever the reason.** A started occurrence within 24 hours of the incident's last occurrence's end, under the same UID, joins that incident; later, it opens a new one. A `stale` end is already 24 hours past its stamp, so a failure that returns after one always opens a new incident, and alerts.

**A restart loses the memory.** A target's first reading after a start that names its stored UID seeds a pair for each open occurrence: its first-seen is the occurrence's `started_at`, its last-seen the incident's `last_seen_at`, its absent total zero. The time the app was down is no earned interval, and nor is the time the target then takes to answer: an occurrence whose signal is gone resolves after its resolve-after window of judged absence, stamped with the last time a reading before the restart saw it, and one last seen more than 24 hours ago ends as `stale` at once. A reading that names another UID ends them as `stopped` instead. A pair with no open occurrence starts its window again.

**A fold that fails to commit changes nothing in memory**, so the next reading folds again from the same state.

**The switch stops a target in its own transaction, not the loop's.** `monitor_targets.generation` counts the target's switches. The write that turns monitoring off sets `enabled`, ends the target's open occurrences as `stopped`, each stamped with its last-seen time, and adds one to the generation; the write that turns it on adds one too.

- **A fold commits only under the generation its reading started under**, and only while the target is enabled. Its transaction tests both, and a fold that fails the test is discarded: a reading that began before a switch writes nothing after it, though the target is on again.
- **The loop drops a target's pairs when it finds a generation it does not hold**, so an off and an on that reach it as one notice still start the windows over.
- **A target deleted takes its incidents with it**, and a fold that finds its row gone is discarded.

### Schema

Added to `appdb/migrations/0001_init.sql`, with `clusters.monitoring_enabled` removed:

```sql
-- monitor_targets: each thing the monitor reads. Its integration lists it, and
-- the monitor's reconcile makes and deletes its row. integration is checked in
-- Go against the integrations the build has. key is the integration's own: for
-- kubernetes, the cluster's id. current_uid is what the target reached at its last
-- reading: for kubernetes, the server's UID.
CREATE TABLE monitor_targets (
  id              TEXT    PRIMARY KEY,
  integration     TEXT    NOT NULL,
  key             TEXT    NOT NULL,
  label           TEXT    NOT NULL,
  enabled         INTEGER NOT NULL DEFAULT 1 CHECK (enabled IN (0, 1)),
  generation      INTEGER NOT NULL DEFAULT 0,
  current_uid     TEXT,
  unknown_signals TEXT    NOT NULL DEFAULT '{}' CHECK (json_valid(unknown_signals) AND json_type(unknown_signals) = 'object'),
  last_read_at    INTEGER,
  created_at      INTEGER NOT NULL,
  updated_at      INTEGER NOT NULL,
  UNIQUE (integration, key)
) STRICT;

CREATE TABLE incidents (
  id                 TEXT    PRIMARY KEY,
  target_id          TEXT    NOT NULL REFERENCES monitor_targets(id) ON DELETE CASCADE,
  target_uid         TEXT    NOT NULL,
  subject_key        TEXT    NOT NULL,
  subject_label      TEXT    NOT NULL,
  subject_attributes TEXT    NOT NULL DEFAULT '{}' CHECK (json_valid(subject_attributes)),
  signal             TEXT    NOT NULL,
  signal_label       TEXT    NOT NULL,
  severity           TEXT    NOT NULL CHECK (severity IN ('critical', 'warning', 'info')),
  first_seen_at      INTEGER NOT NULL,
  last_seen_at       INTEGER NOT NULL,
  occurrence_count   INTEGER NOT NULL CHECK (occurrence_count >= 1)
) STRICT;

CREATE INDEX incidents_subject ON incidents (target_id, target_uid, signal, subject_key);
CREATE INDEX incidents_last_seen ON incidents (last_seen_at);

CREATE TABLE incident_occurrences (
  id          TEXT    PRIMARY KEY,
  incident_id TEXT    NOT NULL REFERENCES incidents(id) ON DELETE CASCADE,
  number      INTEGER NOT NULL CHECK (number >= 1),
  started_at  INTEGER NOT NULL,
  ended_at    INTEGER CHECK (ended_at >= started_at),
  end_reason  TEXT    CHECK (end_reason IN ('resolved', 'stopped', 'stale')),
  detail      TEXT    NOT NULL DEFAULT '{}' CHECK (json_valid(detail)),
  UNIQUE (incident_id, number),
  CHECK ((ended_at IS NULL) = (end_reason IS NULL))
) STRICT, WITHOUT ROWID;

CREATE UNIQUE INDEX incident_occurrences_one_open ON incident_occurrences (incident_id) WHERE ended_at IS NULL;
```

- **`incidents` holds what every incident has, and `subject_attributes` holds the rest**, a JSON object of strings, as an occurrence's `detail` is. No column is one integration's alone. `subject_key` is there for the core to match by; nothing draws it.
- **An incident describes itself.** `signal_label` and `severity` are copied from the signal when the incident opens, so a row whose signal a later build renames or drops still reads whole.
- **`monitor_targets` names no integration and no cluster.** No `CHECK` lists the integrations and no column references `clusters`, so a new integration has nothing to migrate.
- **A target's label is its integration's**, rewritten by the reconcile when it changes. A cluster's is its name.
- **`id` names the row, `key` is its fixed handle, and `current_uid` is observed state**, as `unknown_signals` and `last_read_at` are: null until the first reading and overwritten when a reading reaches another server. `incidents.target_uid` is fixed for the incident's life.
- **`generation` counts the target's switches**, and is the fold's guard against a reading from before one. It is not served.
- **`last_read_at` is null until the target's first reading**, which is how a target never read is told from one that reads `Ok`. The fold that first sets it notifies `KeyMonitorTargets`; later ones do not.
- Both incident ids are `appdb.NewID()`; an occurrence's is its identity on step 2's event feed. An incident is open while its newest occurrence has no `ended_at`. Its `last_seen_at` is the last time a reading saw its signal; once its newest occurrence ends, it equals that occurrence's `ended_at`.
- **An occurrence's `number` is its order.** It is the incident's `occurrence_count` in the transaction that starts it: 1 for the first, never reused. "Newest" means the highest number everywhere: whether the incident is open, what the trim keeps, and what steps 2 to 4 replay, sort, cap and brief from. `started_at` is the wall clock's and orders nothing: a clock set back can stamp a later occurrence earlier, or twice the same.
- `detail` is written when an occurrence starts.
- The loop deletes an incident that is not open and whose `last_seen_at` is more than 30 days ago, and trims an incident to its newest 100 occurrences in the fold that adds one. `occurrence_count` is every occurrence the incident has had: 1 when it opens, and one more in the transaction that starts each later occurrence. The trim leaves it alone.

`desktop/v0.1.0` has not shipped, so the tables go in `0001_init.sql`. An `app.db` written by `desktop/v0.1.0-rc.1` records version 1 and never gains them: `sqlitemigrate` skips it. A release candidate's data is not carried forward. The monitor prepares its statements when the app builds, so on an `rc.1` `app.db` the sidecar does not start and the start screen names the missing table; the release notes say to delete `app.db`.

### The switch

**`monitor_targets.enabled` defaults to 1 and gates detection.** Its comment in `0001_init.sql`, and the descriptions of `Cluster.monitoringEnabled` and `clusterMonitoringEnabledSet` in `schema.graphqls`, say so.

It does not gate model spending on its own. `chat.RunMonitor` keeps its check in the transaction that inserts the run, so a target with monitoring off takes no run, but from this step every target passes it by default. The check is the caller's to supply: `RunMonitor` takes an admit function and calls it in that transaction after the insert, with the id of the run it inserted, and refuses with `ErrMonitoringOff` when the function does. `chat` reads no table of the monitor's. What keeps a run from starting unasked is that `RunMonitor` has no caller: step 4's only caller is the user's press, and step 5 gates unattended runs on the triage setting. A security record says so in this step, and names [the monitoring session record](../../../apps/desktop/docs/security/2026-10-05-the-monitoring-session.md), which describes a run as gated by the switch.

## Tasks

| Task | Status | PR |
| --- | --- | --- |
| 1. Schema and switch | Planned | — |
| 2. Kubernetes integration | Planned | — |
| 3. Fold | Planned | — |
| 4. Store and loop | Planned | — |
| 5. The tick in the tray | Planned | — |

1. **Schema and switch.** The three tables and their indexes, the switch's move with `RunMonitor`'s admit function and the GraphQL field reading through the cluster's target, `WatchingKindRefs`, `PausedKindRefs` and `CacheReading.Health()`, `KeyIncidents` and `KeyMonitorTargets`, the new checksum in `appdb_test.go`'s `shippedMigrations`, the rows in `docs/persisted-data.md`, and the security record.
2. **Kubernetes integration.** Its registration, its listing of clusters and `ClusterID`, its subject's key, label and attributes, each signal's detail, the reading and its unknown signals with their codes, one statement per signal, tested against a real cache store from `cluster/clustertest` seeded with each case, kinds in each state. Each statement that reads `objects` by kind names its index with `INDEXED BY`, so a plan that cannot use it fails to prepare and every statement test pins it. `BenchmarkAReadingOf5000Pods` reads a cache seeded with 5,000 Pods, a few of them failing; the PR records its result and the machine it ran on. A reading over 20 ms opens a decision of its own, since a loop pushed from the cache would have to restate the cadence, the timeout and unknown time, and it changes nothing in this step. The statements' predicates for a rolling workload are also run against real controllers on a kind cluster, each case recorded in the PR.
3. **Fold.** Table-driven tests over earned intervals and both totals, the three endings with stale applied first, unknown signals, a gap between folds, a clock set back and a new UID, with time as an argument.
4. **Store and loop.** The reconcile and its backstop, the switch's transaction and the generation test in the fold's, the check of a reading, the transaction, the notice, the UID and the unknown signals, the retention and the trim, each integration's cadence, `New`'s checks of its registrations, and the wiring into `Runtime`.
5. **The tick in the tray.** By hand, on macOS: with every window closed for an hour, record the time between the loop's ticks. If they stretch past twice the cadence, every gap reads as unknown and nothing opens or resolves; the PR records the result and what holds the tick if they do.

## Acceptance

- A new cluster gains a target, enabled, at the reconcile its `KeyClusters` notice starts. Deleting the cluster deletes the target and its incidents at the next, and renaming it rewrites the target's label.
- A listing that fails deletes no target and is tried again; one that fails at start still gives a new cluster its target.
- A cluster added between the loop's listen and its first listing gains its target without waiting for the backstop.
- A fake integration that sends 100 notices while a listing runs causes one more listing, and none of its sends blocks.
- An incident's cluster is found through its target's key, and `ClusterID` answers nothing for a target of another integration.
- A Deployment whose pod crash-loops for 2 minutes opens one incident on the Deployment, however many pods it has.
- A crash loop whose container runs for 40 seconds between back-offs opens its incident 2 minutes after it was first seen.
- A container that alternates between `Running` and `CrashLoopBackOff` holds one occurrence open.
- A Deployment whose Pending Pods are replaced every 3 minutes opens a `kubernetes.pod_pending` incident, not `kubernetes.replicas_unavailable`.
- With CronJobs forbidden, a crash loop is still detected.
- The incident's occurrence ends 5 minutes after the pods recover, as `resolved`, stamped with the time the signal was last seen.
- The same signal 1 hour later adds a second occurrence; 25 hours later it opens a second incident.
- A syncing, unreachable or stale cache opens and resolves nothing, and every signal reads unknown with its code.
- A disabled cluster, or one whose cluster sync is off, keeps its target and its incidents; its open occurrences end at once as `stopped`.
- An open occurrence on a cluster that stays unreachable ends as `stale` 24 hours after it was last seen, stamped with that time. The same failure seen an hour later opens a new incident.
- After a restart 30 hours after an occurrence was last seen, the first fold ends it as `stale`, though its reading sees the pair, which waits its window again and opens a new incident.
- After a restart, 6 minutes of a cache still syncing count as no absence.
- A context whose server changes, while the app runs or while it is down, ends the target's open occurrences as `stopped` at their last-seen times and writes the new UID, which arrives as a `Modified` of the target. The new server's failure opens a new incident, though its subject matches an old one.
- A crash loop seen once, then unknown for minutes, then seen again opens nothing until it has held 2 judged minutes: the first judged fold after the unknown stretch earns nothing for it.
- A StatefulSet whose rollout stalls short of ready opens a `kubernetes.rollout_stalled` incident after 15 minutes, and no `kubernetes.replicas_unavailable`. So does a Deployment that rolls for 15 minutes with its progress deadline out of reach, and a paused one.
- A workload whose `observedGeneration` is behind its `generation` is rolling, whatever its counts say.
- A Deployment that wants three replicas and holds two, all of its current revision, holds `kubernetes.replicas_unavailable` after 5 minutes and never `kubernetes.rollout_stalled`; so does a DaemonSet short of scheduled pods on its current template.
- A crash loop seen at one fold and at no other for 2 minutes opens nothing; seen again 3 minutes after the first, it opens at that fold with that reading's detail. Unseen for 5 judged minutes, its pair is dropped and no incident exists.
- A restart after 30 hours onto another server ends the old occurrences as `stopped`, not `stale`.
- A context moved to another server and back within an hour adds the returning failure to the first server's incident.
- A StatefulSet whose pods are all updated and not ready holds `kubernetes.replicas_unavailable` after 5 minutes.
- A cache with one kind unanswered is refused whole: every signal reads `not_ready`, a Node's included, and nothing opens or resolves.
- A required kind that no list of the health names leaves its signals unknown with `not_ready`, never `unsupported`.
- An owner kind the cluster does not serve changes nothing but a pod signal's subject.
- Two folds 8 hours apart, the machine asleep between them, start and resolve nothing: a pair seen before and after has held no longer, and one gone after has been absent no time.
- A clock set back an hour while an occurrence is open ends it no earlier than its `started_at`, and the fold commits.
- An `OnDelete` DaemonSet and a partitioned StatefulSet that has updated as far as its partition count when short.
- With Pods paused, the open pod incidents end as `stopped`; with the Pods kind `Stale` or `SyncFailed`, no pod signal opens or resolves. Either way `kubernetes.node_not_ready` still does, the state reads `Partial`, and `unknown_signals` names the three pod signals and the two workload signals, each with its code.
- With the Pods kind unknown for 10 minutes, an open pod incident whose signal is then gone resolves 5 judged minutes later, not at once.
- A statement whose answer is cut at the row bound opens and resolves nothing for its signals, which read `truncated`.
- `kubernetes.replicas_unavailable` stays quiet for a Deployment whose pod holds `CrashLoopBackOff`, and for one that is rolling.
- A Pending Pod whose summary reads `ContainerCreating` is detected, and so is an unscheduled Pod with no container statuses.
- The pending statement reads Pods through `objects_kind_ns_name` and decompresses no body of a `Running` Pod or one already held by another pod signal.
- A fake integration's listed targets gain rows, each is read by the key and label it was listed with, and it opens and ends incidents as Kubernetes does, each on its own cadence, and its attributes and detail are stored as they came.
- A fake integration that returns each target's key as its UID opens and ends incidents as any other does.
- A fake signal whose resolve-after is zero ends its occurrence at the first judged reading without it.
- A fake integration's observation with attributes over 4 KiB is dropped: its signal reads `invalid`, the reading's other signals fold, and the commit succeeds.
- An observation, or an `Unknown` entry, naming a signal its integration did not register is dropped and logged, and the target's state names no such signal. `kubesignals` emits no name it did not register (`TestEveryEmittedSignalIsRegistered`).
- A target whose state changes reads again at its next tick, not at its own notice.
- A reading that takes a whole cadence on one target delays no other target's.
- `New` refuses a registration whose signal lacks its name's prefix, and two registrations of one name.
- A target whose integration this build lacks is never read and never deleted, and fails nothing.
- After a restart of any length under 24 hours, an open occurrence whose signal is gone resolves 5 judged minutes later, stamped with the last time a reading before the restart saw it; one whose signal still holds stays open. A pair with no open occurrence waits its full window again.
- A fold that only advances an open incident's `last_seen_at` sends no notice.
- A fold whose transaction fails leaves memory as it was, and the next reading writes the same changes.
- A reading that outlives its cadence is cancelled and judges nothing, with `timeout`.
- Turning a target's monitoring off ends its open occurrences as `stopped`, each at its last-seen time, in the switch's own transaction, and no reading runs after.
- A reading that started before a switch off commits nothing, though the target was switched on again before it returned (`TestAFoldCommitsOnlyUnderItsGeneration`).
- An off and an on that reach the loop as one notice start the target's windows over.
- A run on a target whose monitoring is turned off between the caller's read and the insert is refused with `ErrMonitoringOff` (`TestRunMonitorAdmitsInsideTheInsert`).
- An incident with 150 occurrences keeps those numbered 51 to 150, its first-seen time and an `occurrence_count` of 150.
- With the clock set back an hour between them, an incident's second occurrence starts with a `started_at` before the first's or equal to it: the fold commits, and the second is the newest.
- An incident on a Deployment stores its label, `Deployment default/web`, its signal's label, the server's UID and attributes naming its group, kind, namespace and name.
- A Deployment and a StatefulSet of one name in one namespace are two subjects.
- A Deployment with three crash-looping pods starts an occurrence whose detail names the first by name, its container, node, image and restart count, and a `pods` of 3.
- An unschedulable pod's occurrence carries its `PodScheduled` condition's reason and message and no `node`; a node's carries its `Ready` condition's.
- The crash-loop and image-pull statements decompress no body to fill their detail.
- A subject name of 10,000 bytes or one holding control characters is cut by the integration and stored without failing the fold.

## When it lands

- `apps/desktop/sidecar/internal/agent/CLAUDE.md` gains the monitor package's invariants: the core is tested over a fake integration, an integration lists durable targets, the loop listens for their changes before it lists them, and a failed listing deletes none, the core never parses a target's key or reads a subject's attributes or a `detail`, the core checks a reading before it folds it, a signal carries its integration's prefix and both its windows, a signal is judged only while every read it depends on answered in full, only an interval between two judged folds earns time and a gap between folds earns none, an occurrence ends as `resolved`, `stopped` or `stale` with stale applied first, the switch stops a target in its own transaction and a fold commits only under the generation it started under, the fold is pure, measures wall time and changes memory only after its commit, the notice follows the commit and names every key whose served rows the commit changed, a fold that only advances `last_seen_at` sends none, the loop never reads on a notice, and a new UID ends the target's open occurrences and opens new incidents.
- `apps/desktop/sidecar/internal/agent/chat`'s guide says `RunMonitor` takes its admit function and calls it in the run's insert.
- `apps/desktop/docs/persisted-data.md` lists the three tables, says how an incident reaches its cluster, names the Kubernetes subject's attribute keys and each signal's detail keys as stored formats, and drops `clusters.monitoring_enabled`.
- `apps/desktop/docs/security/<date>-monitoring-on-by-default.md` records that every cluster's target is enabled by default and gates detection, that `RunMonitor` still checks the switch in the run's insert through the function its caller supplies, and that no caller of `RunMonitor` exists until step 4. `security-model/cluster-proxy.md` gains no row, since no protection moves; KUBE-41 links the record.
- `apps/desktop/docs/TODO.md`'s *Settings* item under *Build the monitoring agent* says the switch gates detection.
