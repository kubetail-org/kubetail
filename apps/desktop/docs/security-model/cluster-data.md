# Cluster data and the caches

[← Security model](../security-model.md)

The sidecar mirrors each cluster into a SQLite cache on disk, and this page covers what goes into those files and what bounds them. Secret values are redacted at write time, a credential field the redactor cannot parse is dropped, and server noise is stripped before storage. The caches and `app.db` are left unencrypted, protected by file mode or ACL; disk encryption is an operator responsibility. A cache has a size ceiling, and every non-watch Kubernetes request carries an idle-read bound. A retention policy, a gesture before a kubeconfig `exec` plugin runs, and budgets on the work an operation causes are not built.

## Protections

| ID | Protection | Status | Residual |
| --- | --- | --- | --- |
| DATA-1 | Secret values are redacted at write time, keyed off the body's own group and kind. | **Enforced** | — |
| DATA-2 | `managedFields` and the last-applied annotation are stripped before storage. | **Enforced** | — |
| DATA-3 | A credential field the redactor cannot parse is dropped, not stored. | **Enforced** | — |
| DATA-4 | The redaction table is chosen from the body, not from how the object was addressed. | **Enforced** | — |
| DATA-5 | Every non-watch Kubernetes request carries an idle-read bound. | **Enforced** | — |
| DATA-6 | Cache contents are left unencrypted, protected by file mode/ACL. Disk encryption is an operator responsibility. | **By decision** | — |
| DATA-7 | `app.db` holds chat text in the clear, protected by the same file mode. Sign-out does not clear it. | **By decision** | A user can paste anything from a cluster into a chat, and nothing deletes old chats. |
| DATA-8 | `app.db` holds the name of every kube-context the importer has seen, in the `clusters` table. A context that left the file keeps its row. | **By decision** | — |
| DATA-9 | A cache has a size ceiling: the janitor judges each sweep and publishes the edge. | **Built** | Soft by one sweep interval. |
| DATA-10 | The cache pass stops the sync of a cache over its size ceiling and says why. | **Built** | Soft by one sweep interval, and a stopped cache stays stopped until the user clears it. |
| DATA-11 | Cached Kubernetes events have no retention beyond the relist's prune. The size ceiling (DATA-9, DATA-10) is what bounds them. | **By decision** | — |
| DATA-12 | A retention policy, so a cache stops outliving the user's interest in its cluster. | **Not built** | — |
| DATA-13 | A gesture before a kubeconfig `exec` plugin runs. | **Not built** | Every imported context is dialled, so a kubeconfig write runs the plugin it names. |
| DATA-14 | Query cost, concurrency, fan-out and total-disk budgets. | **Not built** | The outer request-size and timeout bounds are not budgets on the work an operation causes. |

## Evidence

### DATA-1

- **Where:** `sidecar/internal/services/cluster/internal/kubestore/objects.go`
- `TestProjectRedactsSecretValues`, `TestProjectRedactsOnlyCoreSecrets` — Secret values are redacted at write time, keyed off the body's own group and kind.

### DATA-2

- **Where:** `sidecar/internal/services/cluster/internal/kubestore/objects.go`
- `TestProjectStripsServerNoise` — `managedFields` and the last-applied annotation are stripped.

### DATA-3

- **Where:** `sidecar/internal/services/cluster/internal/kubestore/objects.go`
- `TestSanitizeDropsASecretWhoseDataIsNotAMap`, `TestSanitizeDropsAPasswordThatIsNotAString` — a credential field the redactor cannot parse is dropped.
- `TestNestedReadsDistinguishAnAbsentPathFromAnUnreadableOne` — pins the err branch the discrimination rests on.

### DATA-4

- **Where:** `sidecar/internal/services/cluster/internal/kubestore/objects.go`
- `TestSanitizeRedactsByTheBodysOwnGroupAndKind` — pins that the table is chosen from the body, not from how the object was addressed.

### DATA-5

- **Where:** `sidecar/internal/services/cluster/internal/kubeconn/idletimeout.go`
- `TestNewConnectionBoundsItsNonWatchReads` — pins the wrapper onto every connection.
- `TestIdleTimeoutCancelsAStalledBody`, `TestIdleTimeoutCancelsAStallBeforeHeaders`, `TestIdleTimeoutLetsASlowButStreamingBodyFinish`, `TestIdleTimeoutExemptsAWatch`, `TestIdleTimeoutLeavesACallersCancelAlone` — the behaviour either side of the wrapper.

### DATA-6

- **Where:** `sidecar/internal/services/cluster/internal/kubestore/manager.go`, `sidecar/internal/lib/sqlitepool`
- **Records:** [the cache is ordinary application data](../adr/2026-09-02-the-cache-is-ordinary-application-data.md)

### DATA-7

- **Where:** `sidecar/internal/agent/chat`, `sidecar/internal/lib/sqlitepool`
- **Records:** [chats live in `app.db`](../adr/2026-09-10-chats-live-in-app-db.md)
- `TestOpenWriterFilesAreOwnerOnly` — the file is owner-only like every other SQLite file we open.
- Note: a user can paste anything from a cluster into a chat, and the fake provider is enough to make that worth doing.
- Note: nothing deletes old chats.
- Note: the egress question is the real provider's, not this row's.

### DATA-8

- **Where:** `sidecar/internal/services/cluster/clusterrows.go`, `sidecar/internal/appdb/migrations/0001_init.sql`
- **Records:** [a cluster is a row in `app.db`](../adr/2026-09-16-clusters-are-rows-mirrored-into-beehive.md)
- Note: a context name is not a credential. The kubeconfig resolves those.
- Note: the file is owner-only like every other SQLite file we open.

### DATA-9

- **Where:** `sidecar/internal/services/cluster/internal/kubestore/janitor.go`
- `TestSweepMarksAFileOverItsLimit`, `TestSweepPublishesOnlyWhenTheVerdictChanges`, `TestStatsReportsTheJanitorsVerdict` — the janitor judges each sweep and publishes the edge.

### DATA-10

- **Where:** `sidecar/internal/services/cluster/caches.go`
- **Records:** [a stopped cache is held by its record](../adr/2026-09-03-a-stopped-cache-is-held-by-its-record.md)
- `TestCachePassStopsACacheOverItsSizeLimit`, `TestCachePassKeepsACacheStoppedOnceItsFileIsClosed`, `TestCachePassDoesNotStopACacheOnBytesAlone`, `TestCachesWatchHealthKeyedStateReportsACacheStoppedByItsCeiling`, `TestClearingACacheRequeuesItsRecord`, `TestCachePassRestartsACacheBackUnderItsLimit` — the cache pass stops the sync and says why.

### DATA-11

- **Records:** [bound the cache by total size](../adr/2026-09-03-bound-the-cache-by-total-size.md)

### DATA-12

- **Records:** [TODO](../TODO.md#security)
- Note: no spec yet.

### DATA-13

- **Records:** [TODO](../TODO.md#security)
- Note: no spec yet.

### DATA-14

- **Records:** [TODO](../TODO.md#security)
- Note: no spec yet.
