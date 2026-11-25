-- KubeQuery's views: the contract the tool's prompt states, created as TEMP views on each
-- query connection so an unqualified name reads the view and main.<table> the table. The
-- tables may change under them; the views may not without the prompt changing too.
-- Each is a plain SELECT SQLite flattens into the query, so a column the query does not
-- read is never computed: a query that never names body decompresses nothing.

CREATE TEMP VIEW objects AS
SELECT o.uid,
       o.api_version,
       CASE WHEN instr(o.api_version, '/') = 0 THEN ''
            ELSE substr(o.api_version, 1, instr(o.api_version, '/') - 1) END AS api_group,
       substr(o.api_version, instr(o.api_version, '/') + 1) AS version,
       o.kind,
       k.resource,
       o.namespace,
       o.name,
       nullif(o.created_at, 0) AS created_at,
       o.changed_at,
       o.generation,
       o.resource_version,
       o.status_summary AS status,
       o.ready_count AS ready,
       o.total_count AS total,
       o.restart_count AS restarts,
       o.host AS node,
       (SELECT r.owner_uid FROM main.owner_refs r
         WHERE r.child_uid = o.uid AND r.is_controller = 1) AS owner_uid,
       (SELECT json_group_object(l.key, l.value) FROM main.labels l WHERE l.uid = o.uid) AS labels,
       body(o.raw_json) AS body
FROM main.objects o
LEFT JOIN main.kind_catalog k ON k.api_version = o.api_version AND k.kind = o.kind;

CREATE TEMP VIEW labels AS
SELECT uid, key, value FROM main.labels;

CREATE TEMP VIEW containers AS
SELECT pod_uid, name, init, position, sidecar, image, image_id, container_id, ready, restarts, state,
       reason, exit_code, last_reason, last_exit_code, last_container_id,
       cpu_request, cpu_limit, memory_request, memory_limit
FROM main.containers;

-- Each selector beside each Pod of its namespace for which no term fails. A NotIn or
-- DoesNotExist term passes a Pod without the key, as apimachinery's does.
CREATE TEMP VIEW selects AS
SELECT s.uid AS selector_uid, p.uid
FROM main.selectors s
JOIN main.objects p
  ON p.namespace = s.namespace AND p.api_version = 'v1' AND p.kind = 'Pod'
WHERE NOT EXISTS (
  SELECT 1 FROM main.selector_terms t
  WHERE t.uid = s.uid
    AND CASE t.op
      WHEN 'In'           THEN NOT EXISTS (SELECT 1 FROM main.labels l WHERE l.uid = p.uid AND l.key = t.key
                                             AND l.value IN (SELECT value FROM json_each(t.vals)))
      WHEN 'NotIn'        THEN EXISTS (SELECT 1 FROM main.labels l WHERE l.uid = p.uid AND l.key = t.key
                                         AND l.value IN (SELECT value FROM json_each(t.vals)))
      WHEN 'Exists'       THEN NOT EXISTS (SELECT 1 FROM main.labels l WHERE l.uid = p.uid AND l.key = t.key)
      WHEN 'DoesNotExist' THEN EXISTS (SELECT 1 FROM main.labels l WHERE l.uid = p.uid AND l.key = t.key)
    END);

-- Each reference beside the object it names, when the cache holds it, and whether the cache
-- has a completed list of the target's kind: a kind's cookie is on disk exactly then, cleared
-- by a relist's first page and written by its commit. Scalar subqueries, so a row stays single
-- when the catalog holds the target's kind at two versions. CROSS JOIN keeps the catalog the
-- outer loop, so objects_kind_ns_name answers with every column known; left to itself the
-- planner walks every object of the namespace instead.
CREATE TEMP VIEW refs AS
SELECT r.uid, r.path, r.to_group, r.to_kind, r.to_namespace, r.to_name, r.key, r.optional,
       (SELECT o.uid
        FROM main.kind_catalog k
        CROSS JOIN main.objects o
          ON o.api_version = k.api_version AND o.kind = k.kind
         AND o.namespace = r.to_namespace AND o.name = r.to_name
        WHERE k.kind = r.to_kind
          AND CASE WHEN instr(k.api_version, '/') = 0 THEN ''
                   ELSE substr(k.api_version, 1, instr(k.api_version, '/') - 1) END = r.to_group
        LIMIT 1) AS to_uid,
       EXISTS (
        SELECT 1
        FROM main.kind_catalog k
        JOIN main.cluster_meta m ON m.key = 'cookie/' || k.api_version || '/' || k.resource
        WHERE k.kind = r.to_kind
          AND CASE WHEN instr(k.api_version, '/') = 0 THEN ''
                   ELSE substr(k.api_version, 1, instr(k.api_version, '/') - 1) END = r.to_group
       ) AS to_listed
FROM main.refs r;

CREATE TEMP VIEW annotations AS
SELECT o.uid, a.key, a.value
FROM main.objects o, json_each(body(o.raw_json), '$.metadata.annotations') a;

CREATE TEMP VIEW owners AS
SELECT child_uid AS uid, owner_uid, is_controller AS controller FROM main.owner_refs;

-- Depth 8 bounds the walk, so an owner cycle ends.
CREATE TEMP VIEW ancestors AS
WITH RECURSIVE up(uid, ancestor_uid, depth) AS (
    SELECT child_uid, owner_uid, 1 FROM main.owner_refs
    UNION ALL
    SELECT up.uid, r.owner_uid, up.depth + 1
    FROM up JOIN main.owner_refs r ON r.child_uid = up.ancestor_uid
    WHERE up.depth < 8
)
SELECT uid, ancestor_uid, depth FROM up;

-- rowid is the table's, which events_fts is keyed by.
CREATE TEMP VIEW events AS
SELECT e.rowid AS rowid,
       e.uid,
       e.involved_uid,
       e.involved_kind,
       e.involved_ns AS involved_namespace,
       e.involved_name,
       e.type,
       e.reason,
       e.message,
       e.first_seen,
       e.last_seen,
       e.count,
       body(e.raw_json) AS body
FROM main.events e;

CREATE TEMP VIEW status_history AS
SELECT uid, at, summary AS status FROM main.status_history;

CREATE TEMP VIEW kinds AS
SELECT k.api_version,
       CASE WHEN instr(k.api_version, '/') = 0 THEN ''
            ELSE substr(k.api_version, 1, instr(k.api_version, '/') - 1) END AS api_group,
       substr(k.api_version, instr(k.api_version, '/') + 1) AS version,
       k.kind,
       k.resource,
       k.scope,
       k.is_crd,
       coalesce(c.count, 0) AS count
FROM main.kind_catalog k
LEFT JOIN main.kind_counts c ON c.api_version = k.api_version AND c.kind = k.kind;
