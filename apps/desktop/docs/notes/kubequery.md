# KubeQuery

A note, not a spec: what KubeQuery is. It is built: the views, the guard, the result and the
record are what the code does, and `sidecar/CLAUDE.md` states them.

## Goal

The agent reaches the cluster through `kubectl` in Bash. KubeQuery adds what kubectl cannot:
read-only SQL over Kubetail's cache of the chat's cluster. It takes no cluster or context, so it
reads only the chat's own. → [ADR: the cluster is kubectl in Bash, and KubeQuery over the
mirror](../adr/2026-09-27-the-cluster-is-kubectl-in-bash-and-kubequery.md).

## What the cache adds

Every object of every kind the cluster serves is already in one SQLite file per cache
(`kubestore`). Owner references and labels are rows. Events sit beside them with their text
indexed, and each object's status transitions are kept. So KubeQuery can do what `kubectl`
cannot:

- **Join across kinds.** Owner chains in both directions. Selectors resolved: a Service's Pods, the
  PodDisruptionBudgets and NetworkPolicies over a Pod. References in both directions: the Pods that
  mount a ConfigMap, and the Secret a Pod names that does not exist.
- **Aggregate.** Counts and sums by namespace, node, owner, image or status.
- **Look back.** Status transitions per object, what changed lately, and events long after the
  cluster's one-hour TTL expired them: the cache ages nothing out.
- **Answer from one snapshot.** A query is one statement, which SQLite runs in one read
  transaction, so its parts agree.
- **Cost the cluster nothing, and answer while it is down.** A cache read makes no request. When
  the cluster is unreachable it answers `last-known`, with when the app last saw it live.

## The tool

Its schema is [`kubequery/prompts/schema.json`](../../sidecar/internal/tools/kubequery/prompts/schema.json).
It takes `sql`, one SQLite query in which a `;` may only come last, and is dropped; `limit`, 200 unless given,
at most 2000; and `description`, what the query answers, which the transcript draws. It runs
without asking, under a 10-second bound.

A result is JSON: `cluster`, `freshness`, `columns`, `more`, then `rows`, one to a line, so a
preview of a saved result shows the verdict and the columns, and `Read` can page it by line. A
cell holding a JSON object or array (a `body`, a `json_extract` of an object) is nested rather
than escaped. Every text cell is redacted first, as a command's output is: a JSON cell by its
structure, each string in it read line by line, and any other through `safe.Redact`. A result
past 30,000 bytes is saved to the chat's results with a preview, as bash's is, and cut only
between rows. `Read` cuts a line at 2,000 characters, so a query selects fields rather than
whole bodies.

## Freshness

Every result carries `cluster`, the cluster's display name or else its kube-context, and
`freshness`, the card's freshness section. A query's text does not say which kinds it read, so
`freshness` names every kind that is not `watching`. Its `status` is `watching`, `syncing`,
`paused`, `last-known` with `since`, `unknown` with a `reason`, or `partial` with `notWatching`
naming the kinds behind. `since` is the cache-wide stamp the card carries: when every kind was last
seen live. Under `syncing` and `unknown` the rows are withheld and only the verdict goes: a count
over a store the app does not trust is a false answer with a number on it. A `Connecting` cache
answers from the kinds that listed in an earlier session, as `last-known` or `partial` with reason
`Connecting`; `syncing` names the kinds never listed whose list runs, `unknown` those whose cannot.
The verdict is read off the cluster's health, and the query runs inside the same `ReadActive`
reading, so the rows and the verdict describe one cache. Under `Connecting` the cookies are a third
read between them, which the ADR accepts.

## KubeQuery's views

The views are the contract. They are created as `TEMP` views on the query connection, so they
shadow the store's tables of the same name, and the tables below them can change without the prompt
changing. Times are Unix milliseconds; `datetime(at / 1000, 'unixepoch')` renders one. `api_group`
is `''` for the core group.

```
objects         uid, api_version, api_group, version, kind, resource, namespace, name,
                created_at, changed_at, generation, resource_version,
                status, ready, total, restarts, node, owner_uid, labels, body
                -- labels and body are JSON text; body is the whole object, as the cache keeps it
labels          uid, key, value
annotations     uid, key, value
owners          uid, owner_uid, controller           -- direct ownerReferences
ancestors       uid, ancestor_uid, depth             -- every owner above, to depth 8; 1 is direct
events          rowid, uid, involved_uid, involved_kind, involved_namespace, involved_name,
                type, reason, message, first_seen, last_seen, count, body
status_history  uid, at, status
kinds           api_version, api_group, version, kind, resource, scope, is_crd, count
containers      pod_uid, name, init, position, sidecar, image, image_id, ready, restarts, state,
                reason, exit_code, last_reason, last_exit_code,
                cpu_request, cpu_limit, memory_request, memory_limit   -- cores and bytes
selects         selector_uid, uid                    -- the Pods each selector matches in its namespace
refs            uid, path, to_group, to_kind, to_namespace, to_name, key, optional, to_uid, to_listed
                -- to_uid null with to_listed 1: a missing target; to_listed 0: the cache cannot tell
```

`changed_at` is when the cache last saw the object change: the store keeps it with the write
position, and a relist that finds the object unchanged leaves it. An object unchanged since the
cache first listed it reads that list's time. `events_fts`, the store's full-text index over
`reason` and `message`, is reached through `events.rowid`:
`SELECT * FROM events WHERE rowid IN (SELECT rowid FROM events_fts('ImagePullBackOff'))`.

One function: `quantity(text)`, a quantity as a number (`quantity('250m')` is 0.25,
`quantity('1Gi')` is 1073741824). What a view reads from bodies, the store reads when it writes
the object, as it writes `labels`: a Pod's containers, an object's references and its selector's
terms. A view is then plain SQL over rows, and no function a statement can
call parses a body.

```sql
-- CPU requested per node against what it can allocate. A Pod asks for the larger of its
-- containers and sidecars summed and, for each other init container, its request plus the
-- sidecars started before it, then its overhead. A Pod that sets its own asks that.
-- Filter on the phase, not status: a Pod in CrashLoopBackOff still holds its request.
WITH c AS (
  SELECT pod_uid, init, sidecar, coalesce(cpu_request, 0) AS cpu,
         coalesce(sum(cpu_request) FILTER (WHERE sidecar = 1)
                    OVER (PARTITION BY pod_uid, init ORDER BY position), 0) AS sidecars_before
  FROM containers
),
pod AS (
  SELECT pod_uid,
         max(sum(cpu) FILTER (WHERE init = 0 OR sidecar = 1),
             coalesce(max(cpu + sidecars_before) FILTER (WHERE init = 1 AND sidecar = 0), 0)) AS cpu
  FROM c
  GROUP BY pod_uid
)
SELECT p.node,
       round(sum(coalesce(quantity(p.body ->> '$.spec.resources.requests.cpu'), pod.cpu)
                 + coalesce(quantity(p.body ->> '$.spec.overhead.cpu'), 0)), 2) AS cpu_requested,
       quantity(n.body ->> '$.status.allocatable.cpu') AS cpu_allocatable
FROM objects p
JOIN pod ON pod.pod_uid = p.uid
JOIN objects n ON n.kind = 'Node' AND n.name = p.node
WHERE p.kind = 'Pod' AND p.node IS NOT NULL
  AND p.body ->> '$.status.phase' NOT IN ('Succeeded', 'Failed')
GROUP BY p.node
ORDER BY cpu_requested DESC;

-- Services whose selector matches no ready Pod
SELECT s.namespace, s.name
FROM objects s
WHERE s.kind = 'Service' AND json_extract(s.body, '$.spec.selector') IS NOT NULL
  AND NOT EXISTS (
    SELECT 1 FROM selects x JOIN objects p ON p.uid = x.uid
    WHERE x.selector_uid = s.uid AND p.total > 0 AND p.ready = p.total);

-- What changed in the last 30 minutes, leaving out what changes every few seconds
SELECT kind, namespace, name, datetime(changed_at / 1000, 'unixepoch') AS at
FROM objects
WHERE changed_at > (unixepoch() - 1800) * 1000
  AND kind NOT IN ('Pod', 'Lease', 'Node', 'EndpointSlice', 'Endpoints')
ORDER BY changed_at DESC;
```

Measured on a synthetic cache of 10,000 objects (SQLite 3.53): opening a query
connection with its views, 1 ms; a scan of `objects` that reads no body, under 6 ms, and no body is
decompressed; one that reads every Pod's body, 56 ms; the whole `ancestors` closure, 24 ms;
`annotations` over every object, 87 ms.

The tables written with the object cost on the write: a relist page of 500 Pods, each about 6 KB
with four containers (`BenchmarkWriteAPodPage`), takes 199 ms with its `containers` and `refs`
rows and 122 ms without (the Linux sandbox, arm64, 8 cores, SQLite 3.53). A kind with neither
references nor a selector writes no `refs`, `selectors` or `selector_terms` rows.

`selects` and `refs` were measured on a synthetic cache of 10,000 Pods and 2,000 selectors of two terms in 50
namespaces, each Pod making five references to 2,000 ConfigMaps and Secrets of which half exist
(`BenchmarkSelects`, `BenchmarkRefs`; the same machine): every row of `selects`, 1.0 s; one
selector's Pods, 2 ms; one Pod's selectors, 1.4 ms; every reference whose target is missing,
120 ms. The last drives its lookup from the catalog: in the planner's own order it walked every
object of each namespace and took 2.7 s.

## The guard

The connection and the wrap are the guard, not the text:

- **Opened `mode=ro`**, so no pragma the statement runs can make it write.
- **`SQLITE_LIMIT_ATTACHED` set to 0** through `sqlite.Limit`, so no statement opens another file.
  `ATTACH` and `VACUUM INTO` both need one.
- **`SQLITE_LIMIT_LENGTH` set to 16 MiB**, so no statement builds a value past it.
- **One statement.** The tool refuses a `;` anywhere but at the end. SQLite separates statements
  with `;` alone, and `modernc` would otherwise run a whole script and answer its last statement.
- **Only a query, run whole in one step.** The statement runs as
  `WITH q AS MATERIALIZED (SELECT * FROM (<sql>) LIMIT n) SELECT * FROM q`. A subquery can only
  be a query, so no `PRAGMA` (some change every connection in the process, and SQLite applies
  them while preparing), `ATTACH` or DDL compiles; the wrap is prepared before anything else.
  The statement is then prepared alone too, which keeps a stray `)` or a trailing `/*` from
  breaking out. And
  SQLite builds `q` before answering the first row, the one step modernc's interrupt reaches:
  its `rows.Next` never looks at the context.
- **A connection of its own per query.** Each cache file owns a query pool that keeps no idle
  connection, so nothing one query leaves on its connection (a dropped view, a temp table, a
  pragma) reaches the next. The file cancels its queries when it closes, so a clear interrupts
  one rather than waiting it out, and waits for their connections to close before the file is
  deleted.
- **A driver of its own.** `body` and the other functions, and the hook that creates the views,
  are registered on a `sqlite.Driver` value `kubestore` owns, so they reach query connections
  alone.
- **Bounded.** The 10-second bound on the call interrupts the statement (modernc's
  `sqlite3_interrupt`), and a row cap and a byte cap bound the result, cut only between rows.
  The deadline cannot interrupt a Go callback, so each function bounds its own work whatever a
  statement hands it: `body()` inflates at most 16 MiB; `quantity` reads at most 64 bytes and an
  exponent of at most ±1,000, since parsing `1e-N` builds a power of ten of about N digits.
- **Nothing the views do not already show.** The store's tables stay reachable as `main.<table>`,
  which is no leak: they hold the same redacted bodies the views read. They are simply not a
  contract.

The residual: a sort over a large join, or a value built to the length limit, allocates memory or
temp disk until the deadline stops it. It is local, and bounded per call and per turn.

## Security

**A new class of data leaves the machine.** Object bodies go to the model provider on the model's
request, with no call-by-call approval. That is the line the
[first tool's record](../security/2026-09-14-list-objects-tool.md) drew: "a body read … gets its
own record before it lands". [KubeQuery reads the mirror](../security/2026-09-27-kubequery-reads-the-mirror.md)
is that record. What bounds it:

- A Secret's values are redacted at write time, and `managedFields` and the last-applied annotation
  (which holds a Secret's data in the clear) are dropped. KubeQuery shows a Secret's type, keys and
  metadata.
- Every text cell is redacted as a command's output is, so KubeQuery is not a way around it. A
  JSON cell is redacted by its structure: `safe.Redact` reads each string in it decoded, line by
  line, and under a key named for a credential, in any case or spelling (`clientSecret`,
  `DB_PASSWORD`), every string and number goes. So does the `value` of an object whose `name`
  is a credential's, which is how a Pod's env var carries one. Over the compact text, the rules
  would cut the rest of a body after one match and miss a line inside an escaped string.
- The residual: env values under ordinary names (`DB_URL` with a password in it), ConfigMap
  data, annotations, and CRD fields the redaction table does not list go as they are, less what
  the rules' shapes catch. A body can carry the API server's URL, which the
  card keeps out.

**Injection steers only what is read.** A model that has read attacker-controlled cluster text can
pick which cached data reaches the provider, which already has the chat. KubeQuery reaches no other
host, file or cluster.

**Consent is the send**, as it is for the card. KubeQuery reads without asking, and the transcript
shows every query and its result. An off switch, a setting beside memory's, lands before a release
build ships.
