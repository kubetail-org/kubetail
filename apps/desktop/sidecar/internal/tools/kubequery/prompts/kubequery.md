## KubeQuery

KubeQuery reads Kubetail's cache of this chat's cluster, kept current by watches. It costs the cluster nothing and answers while the cluster is unreachable. Use it before `kubectl` for anything the cache holds; use LogsRead for logs, and `kubectl` for exec, metrics and changes.

`sql` is one SQLite `SELECT` (or `WITH`) over these views; a `;` may only end it. Times are Unix milliseconds: `datetime(at / 1000, 'unixepoch')` renders one.

```
objects: uid, api_version, api_group, version, kind, resource, namespace, name, created_at, changed_at,
  generation, resource_version, status, ready, total, restarts, node, owner_uid, labels (JSON), body (JSON)
labels, annotations: uid, key, value
owners: uid, owner_uid, controller (direct ownerReferences)
ancestors: uid, ancestor_uid, depth (every owner above, to depth 8; 1 is direct)
events: rowid, uid, involved_uid, involved_kind, involved_namespace, involved_name,
  type, reason, message, first_seen, last_seen, count, body (JSON)
status_history: uid, at, status
kinds: api_version, api_group, version, kind, resource, scope, is_crd, count
containers: pod_uid, name, init, position, sidecar, image, image_id, container_id, ready, restarts, state, reason,
  exit_code, last_reason, last_exit_code, last_container_id, cpu_request, cpu_limit, memory_request, memory_limit
  (cores and bytes)
selects: selector_uid, uid (the Pods each selector matches in its namespace)
refs: uid, path, to_group, to_kind, to_namespace, to_name, key, optional, to_uid, to_listed
```

- `objects` holds every kind but Events, which are in `events`. `api_group` is `''` for core. `body` is the object less `managedFields`, with a Secret's values `[redacted]`. Read fields with `body ->> '$.spec.nodeName'`; quote a dotted key: `labels ->> '$."app.kubernetes.io/name"'`. Select fields, not whole bodies: a saved result reads back 2,000 characters a line.
- `status`, `ready`, `total`, `restarts`, `node`: Kubetail's readings where a kind has one (a Pod's phase or waiting reason, ready and total containers, restarts, node; a workload's ready and desired replicas; a Node's condition), else null. `owner_uid` is the controller owner.
- `changed_at` is when the cache last saw the object change, or first listed it. Leases, Nodes and EndpointSlices change every few seconds. `status_history` is every change of `status`.
- `events` keeps an event after the cluster expires it. `events_fts` searches `reason` and `message`: `SELECT * FROM events WHERE rowid IN (SELECT rowid FROM events_fts('ImagePullBackOff'))`.
- `containers` is one row per container of each Pod, init containers included (`init` 1), from its spec and status. `position` is its index in its list; `sidecar` 1 is an init container that runs beside the others. `last_reason` and `last_exit_code` are the run before, where an OOMKilled crash shows.
- `quantity(text)` is a Kubernetes quantity as a number: `quantity('250m')` is 0.25, `quantity('1Gi')` is 1073741824, and text that is not a quantity is null.
- `selects` covers a Service's selector, a workload's, a PodDisruptionBudget's and a NetworkPolicy's `podSelector`.
- `refs` holds the references by name in Pod specs and templates, StatefulSets, ServiceAccounts, Ingresses, PVCs, PVs, role bindings and HPAs. It is not every reference: others, custom resources' included, are only in `body`, so an object no `refs` row points at may still be in use. A null `to_uid` is a missing target only when `to_listed` is 1. `optional` 1 is a reference the Pod starts without.

A result is JSON: `cluster`, `freshness`, `columns`, `more`, then `rows`, one to a line. `more` is true when rows were left out, past `limit` or the size cap. A `freshness` of `last-known` or `partial` may be out of date; under `syncing` or `unknown` no rows come back, and kinds it lists as `syncing` or `unknown` are incomplete, so an object missing from the result may exist. `{"error":"sql"}` carries SQLite's complaint in `message`: fix the query. `too-large` is a row no result can hold: select fewer or narrower columns.
