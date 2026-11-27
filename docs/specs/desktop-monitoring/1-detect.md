# Step 1 — Detect

No prerequisite. [The sequence's README](README.md) holds the decisions.

The sidecar reads each monitored cluster's cache on a fixed cadence, folds the signals it finds into incidents and occurrences, and stores them in `app.db`. Nothing reads them yet but tests.

## Design

**The package is `apps/desktop/sidecar/internal/agent/monitor`**, a `lifecycle.Part` built in `app/runtime.go` after `rt.Chat`. It takes the `*appdb.DB`, the cluster service and its cadence and windows as constructor arguments, so tests shrink them.

**Three layers, each a file with its test:**

- `signals.go` — one reading of one cluster: `cluster.CachedData().ReadActive` over the `objects`, `containers` and `owner_refs` tables, answering the set of `(subject, signal)` pairs true now. Pure but for the read. `ErrNoCache` and `ErrSyncing` answer *no reading*, never an empty set.
- `fold.go` — a pure function from the last state, a reading and the time to the changes to write: occurrences to start, occurrences to end, incidents to open. It holds the windows: counts-after, the 5-minute absence, the 24-hour reopen.
- `store.go` — the tables, a `sqlstmt.Set`, one transaction per fold, then `Notify(appdb.KeyIncidents)` after commit.

**The loop** runs one reading per monitored cluster every 30 seconds, and once more when `KeyClusters` pings. A cluster is monitored while its row is live, enabled and `monitoring_enabled`.

**What is held in memory** is each pair's first-seen and last-seen time, which the windows read. A restart loses them: the first trusted reading after a start ends an open occurrence whose signal is absent, at that reading's time, and keeps one whose signal is present.

**Schema**, added to `appdb/migrations/0001_init.sql`:

```sql
CREATE TABLE incidents (
  id            TEXT    PRIMARY KEY,
  cluster_id    TEXT    NOT NULL REFERENCES clusters(id) ON DELETE CASCADE,
  subject_group TEXT    NOT NULL,
  subject_kind  TEXT    NOT NULL,
  subject_namespace TEXT NOT NULL,
  subject_name  TEXT    NOT NULL,
  signal        TEXT    NOT NULL,
  baseline      INTEGER NOT NULL CHECK (baseline IN (0, 1)),
  first_seen_at INTEGER NOT NULL
) STRICT;

CREATE TABLE incident_occurrences (
  incident_id TEXT    NOT NULL REFERENCES incidents(id) ON DELETE CASCADE,
  started_at  INTEGER NOT NULL,
  ended_at    INTEGER,
  PRIMARY KEY (incident_id, started_at)
) STRICT, WITHOUT ROWID;

CREATE TABLE monitor_baselines (
  cluster_id TEXT    PRIMARY KEY REFERENCES clusters(id) ON DELETE CASCADE,
  taken_at   INTEGER NOT NULL
) STRICT, WITHOUT ROWID;
```

An incident is open while its newest occurrence has no `ended_at`. A partial unique index allows one open occurrence per incident. An incident whose newest occurrence ended more than 30 days ago is deleted by the loop.

**`clusters.monitoring_enabled` defaults to 1.** Its comment, and the descriptions of `Cluster.monitoringEnabled` and `clusterMonitoringEnabledSet` in `schema.graphqls`, say it gates detection. `chat.RunMonitor` keeps its own check: a cluster with monitoring off takes no run.

## Tasks

| Task | Status | PR |
| --- | --- | --- |
| 1. Schema | Planned | — |
| 2. Signals | Planned | — |
| 3. Fold | Planned | — |
| 4. Store and loop | Planned | — |

1. **Schema.** The three tables, the default, `KeyIncidents`, the new checksum in `appdb_test.go`'s `shippedMigrations`, and the rows in `docs/persisted-data.md`.
2. **Signals.** The reading, one statement per signal, tested against a real cache store from `cluster/clustertest` seeded with each case.
3. **Fold.** Table-driven tests over the windows, with time as an argument.
4. **Store and loop.** The transaction, the notice, the baseline, the retention and the wiring into `Runtime`.

## Acceptance

- A Deployment whose pod crash-loops for 2 minutes opens one incident on the Deployment, however many pods it has.
- The incident's occurrence ends 5 minutes after the pods recover, stamped with the time the signal was last seen.
- The same signal 1 hour later adds a second occurrence; 25 hours later it opens a second incident.
- A container that alternates between `Running` and `CrashLoopBackOff` holds one occurrence open.
- A signal already true at the first reading opens a `baseline` incident at once.
- A syncing or unreachable cache opens and ends nothing.
- `ReplicasUnavailable` stays quiet for a Deployment whose pod holds `CrashLoopBackOff`.
- With monitoring off for a cluster, no reading runs, its open occurrences have ended and its baseline is gone.
- A subject name of 10,000 bytes or one holding control characters is stored without failing the fold.

## When it lands

- `apps/desktop/sidecar/internal/agent/CLAUDE.md` gains the monitor package's invariants: only a trusted reading opens or ends, the fold is pure, the notice follows the commit.
- `apps/desktop/docs/persisted-data.md` lists the three tables.
