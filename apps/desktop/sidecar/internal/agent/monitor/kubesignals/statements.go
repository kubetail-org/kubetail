// Copyright 2024 The Kubetail Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package kubesignals

// The statements of one reading. Every table is named main.: the own-query
// connection carries KubeQuery's TEMP views, and a bare objects or containers
// would read the view. Each read of objects by kind names its index, so a plan
// that cannot use it fails to prepare.

// The bounds of one statement's answer. An answer cut at either reports More, and
// its signals read truncated.
const (
	maxRows  = 50_000
	maxBytes = 16 << 20
)

// The container reasons of the two container signals.
const (
	crashLoopReasons = `'CrashLoopBackOff'`
	imagePullReasons = `'ImagePullBackOff', 'ErrImagePull'`
	heldReasons      = crashLoopReasons + `, ` + imagePullReasons
)

// pendingCandidates is the Pods that may be Pending: their summary is no phase a
// Pending Pod lacks, and no container waits for a container signal. No other
// test: an unscheduled Pod has no container statuses, so its counts read 0/0.
const pendingCandidates = `SELECT p.uid, p.name, p.host, p.status_summary, p.raw_json
  FROM main.objects p INDEXED BY objects_kind_ns_name
  WHERE p.api_version = 'v1' AND p.kind = 'Pod'
    AND coalesce(p.status_summary, '') NOT IN ('Running', 'Succeeded', 'Failed')
    AND NOT EXISTS (SELECT 1 FROM main.containers c
      WHERE c.pod_uid = p.uid AND c.state = 'waiting' AND c.reason IN (` + heldReasons + `))`

// candidatesCTE names the pending candidates candidates, materialized so a
// statement that reads them twice finds them once.
const candidatesCTE = `candidates AS MATERIALIZED (` + pendingCandidates + `),
`

// ownerWalk climbs from each row of hits(pod_uid) to its last controller the
// cache holds, at most 8 owners up; a Pod with no controller is its own. It
// ends in tops(pod_uid, uid).
const ownerWalk = `,
chain(pod_uid, uid, depth) AS (
  SELECT DISTINCT pod_uid, pod_uid, 0 FROM hits
  UNION ALL
  SELECT c.pod_uid, r.owner_uid, c.depth + 1
  FROM chain c
  JOIN main.owner_refs r ON r.child_uid = c.uid AND r.is_controller = 1
  JOIN main.objects o ON o.uid = r.owner_uid
  WHERE c.depth < 8
),
tops AS MATERIALIZED (
  SELECT pod_uid, uid, max(depth) FROM chain GROUP BY pod_uid
)`

// podSignal is a Pod signal's statement over hits, after the CTEs in ctes: one
// row per hit, its subject first, ordered so a subject's first row is its first
// Pod by name.
func podSignal(ctes, hits, columns string) string {
	return `WITH RECURSIVE ` + ctes + `hits AS MATERIALIZED (` + hits + `)` + ownerWalk + `
SELECT s.api_version, s.kind, s.namespace, s.name, h.pod_uid, ` + columns + `
FROM hits h JOIN tops t ON t.pod_uid = h.pod_uid JOIN main.objects s ON s.uid = t.uid
ORDER BY s.api_version, s.kind, s.namespace, s.name, h.pod_name, h.init DESC, h.position`
}

// containerHits is the containers waiting for one of reasons, read from
// main.containers and objects.host alone: no body is decompressed for them.
func containerHits(reasons string) string {
	return `SELECT p.uid AS pod_uid, p.name AS pod_name, p.host, c.init, c.position, c.name AS container,
    c.image, c.restarts, c.reason, c.last_reason, c.last_exit_code
  FROM main.objects p INDEXED BY objects_kind_ns_name
  JOIN main.containers c ON c.pod_uid = p.uid
  WHERE p.api_version = 'v1' AND p.kind = 'Pod' AND c.state = 'waiting' AND c.reason IN (` + reasons + `)`
}

var (
	// stmtCrashLoop answers subject, pod, pod name, node, container, image,
	// restarts, last reason and last exit code.
	stmtCrashLoop = podSignal("", containerHits(crashLoopReasons),
		`h.pod_name, h.host, h.container, h.image, h.restarts, h.last_reason, h.last_exit_code`)
	// stmtImagePull answers subject, pod, pod name, node, container, image and reason.
	stmtImagePull = podSignal("", containerHits(imagePullReasons),
		`h.pod_name, h.host, h.container, h.image, h.reason`)
	// stmtPodPending answers subject, pod, pod name, node, reason and message.
	// A body is decompressed for a candidate alone, once: SQLite would otherwise
	// flatten the bodies into each use of b.
	stmtPodPending = podSignal(candidatesCTE+`bodies AS MATERIALIZED (
  SELECT uid, name, host, status_summary, body(raw_json) AS b FROM candidates
),
`, `SELECT uid AS pod_uid, name AS pod_name, host, status_summary, 0 AS init, 0 AS position,
    c.value ->> '$.reason' AS scheduled_reason, c.value ->> '$.message' AS scheduled_message
  FROM bodies LEFT JOIN json_each(bodies.b, '$.status.conditions') c
    ON c.value ->> '$.type' = 'PodScheduled' AND c.value ->> '$.status' <> 'True'
  WHERE b ->> '$.status.phase' = 'Pending'`,
		`h.pod_name, h.host, coalesce(h.scheduled_reason, h.status_summary), h.scheduled_message`)
	// stmtNodeNotReady answers name, and the Ready condition's status, reason and message.
	stmtNodeNotReady = `WITH nodes AS MATERIALIZED (
  SELECT o.name, body(o.raw_json) AS b
  FROM main.objects o INDEXED BY objects_kind_ready
  WHERE o.api_version = 'v1' AND o.kind = 'Node' AND o.ready_count = 0
)
SELECT name, c.value ->> '$.status', c.value ->> '$.reason', c.value ->> '$.message'
FROM nodes LEFT JOIN json_each(nodes.b, '$.status.conditions') c ON c.value ->> '$.type' = 'Ready'
ORDER BY name`
	// stmtWorkloads answers each Deployment, StatefulSet and DaemonSet short of
	// ready replicas none of whose Pods, its hits, holds a Pod signal: kind,
	// namespace, name, ready, desired, updated, and whether it is rolling.
	stmtWorkloads = `WITH RECURSIVE ` + candidatesCTE + `hits AS MATERIALIZED (
  SELECT c.pod_uid FROM main.objects p INDEXED BY objects_kind_ns_name
  JOIN main.containers c ON c.pod_uid = p.uid
  WHERE p.api_version = 'v1' AND p.kind = 'Pod' AND c.state = 'waiting' AND c.reason IN (` + heldReasons + `)
  UNION
  SELECT uid FROM (SELECT uid, body(raw_json) AS b FROM candidates) WHERE b ->> '$.status.phase' = 'Pending'
)` + ownerWalk + `,
short AS MATERIALIZED (
  SELECT o.uid, o.kind, o.namespace, o.name, o.generation, o.ready_count, o.total_count, body(o.raw_json) AS b
  FROM main.objects o INDEXED BY objects_kind_ready
  WHERE o.api_version = 'apps/v1' AND o.kind IN ('Deployment', 'StatefulSet', 'DaemonSet')
    AND o.ready_count < o.total_count
    AND o.uid NOT IN (SELECT uid FROM tops)
)
SELECT kind, namespace, name, ready_count, total_count,
  CASE kind WHEN 'DaemonSet' THEN b ->> '$.status.updatedNumberScheduled' ELSE b ->> '$.status.updatedReplicas' END,
  coalesce(b ->> '$.status.observedGeneration', 0) < generation
  OR (coalesce(b ->> '$.spec.updateStrategy.type', '') <> 'OnDelete' AND CASE kind
    WHEN 'Deployment' THEN coalesce(b ->> '$.status.replicas', 0) > coalesce(b ->> '$.status.updatedReplicas', 0)
    WHEN 'StatefulSet' THEN coalesce(b ->> '$.status.updateRevision', '') <> coalesce(b ->> '$.status.currentRevision', '')
      AND coalesce(b ->> '$.status.updatedReplicas', 0)
        < coalesce(b ->> '$.spec.replicas', 1) - coalesce(b ->> '$.spec.updateStrategy.rollingUpdate.partition', 0)
    ELSE coalesce(b ->> '$.status.updatedNumberScheduled', 0) < coalesce(b ->> '$.status.currentNumberScheduled', 0)
  END)
FROM short
ORDER BY kind, namespace, name`
)
