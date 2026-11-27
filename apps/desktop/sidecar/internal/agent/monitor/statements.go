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

package monitor

import (
	"strconv"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/sqlstmt"
)

// The statements a reading runs over the cache's own-query connection. That
// connection carries KubeQuery's TEMP views, and a bare objects or containers names
// the view, so every table here is main.'s.

// ownerWalk follows each seeded pod up its controllers, through owners the cache
// holds, at most 8 deep so an owner cycle ends. The ancestors view follows every
// owner reference, not only controllers. Its rows are one per pod, signal and step:
// pod uid, signal, depth, then the uid, apiVersion, kind, namespace and name at it.
const ownerWalk = `,
walk(pod_uid, uid, depth) AS (
  SELECT DISTINCT uid, uid, 0 FROM seed
  UNION ALL
  SELECT w.pod_uid, r.owner_uid, w.depth + 1
  FROM walk w
  JOIN main.owner_refs r ON r.child_uid = w.uid AND r.is_controller = 1
  JOIN main.objects o ON o.uid = r.owner_uid
  WHERE w.depth < 8
)
SELECT s.uid, s.signal, w.depth, o.uid, o.api_version, o.kind, o.namespace, o.name
FROM seed s
JOIN walk w ON w.pod_uid = s.uid
JOIN main.objects o ON o.uid = w.uid`

// stmtWaitingPods is each pod with a container, init containers included, waiting
// in a crash loop or on its image.
const stmtWaitingPods = `WITH RECURSIVE
seed(uid, signal) AS (
  SELECT DISTINCT pod_uid, CASE reason WHEN 'CrashLoopBackOff' THEN 'CrashLoopBackOff' ELSE 'ImagePullBackOff' END
  FROM main.containers
  WHERE state = 'waiting' AND reason IN ('CrashLoopBackOff', 'ImagePullBackOff', 'ErrImagePull')
)` + ownerWalk

// stmtPendingPods is each Pod whose phase is Pending, of any age, that no container
// holds in a crash loop or on its image. Its window is the fold's, from when the
// subject was first seen Pending, so a workload whose Pending Pods are replaced keeps
// its window, and a young Pending Pod still names its owners. The phase is in the body alone:
// status_summary holds the worst container reason in its place. So the body opens
// only for a candidate, which a Running Pod's summary keeps it from being; an
// unscheduled Pod has no container statuses, so no test may lean on the counts.
// seed is materialized because the walk reads it twice.
const stmtPendingPods = `WITH RECURSIVE
candidate AS MATERIALIZED (
  SELECT o.uid, o.raw_json
  FROM main.objects o INDEXED BY objects_kind_ns_name
  WHERE o.api_version = 'v1' AND o.kind = 'Pod'
    AND ifnull(o.status_summary, '') NOT IN ('Running', 'Succeeded', 'Failed')
    AND NOT EXISTS (
      SELECT 1 FROM main.containers c
      WHERE c.pod_uid = o.uid AND c.state = 'waiting' AND c.reason IN ('CrashLoopBackOff', 'ImagePullBackOff', 'ErrImagePull'))
),
seed(uid, signal) AS MATERIALIZED (
  SELECT uid, 'PodPending' FROM candidate WHERE body(raw_json) ->> '$.status.phase' = 'Pending'
)` + ownerWalk

// stmtNotReadyNodes is each Node whose Ready condition is not True.
const stmtNotReadyNodes = `SELECT name FROM main.objects INDEXED BY objects_kind_ready
WHERE api_version = 'v1' AND kind = 'Node' AND ready_count = 0`

// stmtShortWorkloads is each Deployment, StatefulSet and DaemonSet with fewer ready
// replicas than desired, and its status and spec, which tell a rollout in progress.
const stmtShortWorkloads = `WITH candidate AS MATERIALIZED (
  SELECT uid, kind, namespace, name, body(raw_json) AS body
  FROM main.objects INDEXED BY objects_kind_ready
  WHERE api_version = 'apps/v1' AND kind IN ('Deployment', 'StatefulSet', 'DaemonSet') AND ready_count < total_count
)
SELECT uid, kind, namespace, name, body -> '$.status', body -> '$.spec' FROM candidate`

// The store's statements over app.db, prepared once (sqlstmt.Set). The text lives
// here and never at a call site.

type stmtID int

const (
	stmtSelectLatestIncident stmtID = iota
	stmtInsertIncident
	stmtReopenIncident
	stmtInsertOccurrence
	stmtTrimOccurrences
	stmtEndOccurrence
	stmtSeeIncident
	stmtDeleteRecoveredBefore
	stmtEndClusterOccurrences
	stmtEndOtherServersOccurrences
	stmtSelectOpenOccurrences

	numStmts int = iota
)

// stmts issues the set's statements, on the pools or inside a transaction.
type stmts = sqlstmt.Stmts[stmtID]

// pairWhere names one pair's incidents in a scope.
const pairWhere = `cluster_id = ? AND server_uid = ? AND subject_group = ? AND subject_kind = ? AND subject_namespace = ? AND subject_name = ? AND signal = ?`

// openOccurrenceOf is true of an incident with an open occurrence.
const openOccurrenceOf = `EXISTS (SELECT 1 FROM incident_occurrences o WHERE o.incident_id = incidents.id AND o.ended_at IS NULL)`

// keptOccurrences is how many occurrences an incident keeps.
const keptOccurrences = 100

var statements = []sqlstmt.Statement{
	stmtSelectLatestIncident: sqlstmt.OnBoth(`SELECT id, last_seen_at, ` + openOccurrenceOf + ` FROM incidents WHERE ` + pairWhere + ` ORDER BY last_seen_at DESC LIMIT 1`),
	stmtInsertIncident: sqlstmt.OnWriter(`INSERT INTO incidents (id, cluster_id, server_uid, subject_group, subject_kind, subject_namespace, subject_name, signal,
  first_seen_at, last_seen_at, occurrence_count) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 1)`),
	stmtReopenIncident:   sqlstmt.OnWriter(`UPDATE incidents SET last_seen_at = ?, occurrence_count = occurrence_count + 1 WHERE id = ?`),
	stmtInsertOccurrence: sqlstmt.OnWriter(`INSERT INTO incident_occurrences (id, incident_id, started_at) VALUES (?, ?, ?)`),
	stmtTrimOccurrences: sqlstmt.OnWriter(`DELETE FROM incident_occurrences WHERE incident_id = ?1 AND started_at <
  (SELECT started_at FROM incident_occurrences WHERE incident_id = ?1 ORDER BY started_at DESC LIMIT 1 OFFSET ` + strconv.Itoa(keptOccurrences-1) + `)`),
	stmtEndOccurrence: sqlstmt.OnWriter(`UPDATE incident_occurrences SET ended_at = ? WHERE ended_at IS NULL
  AND incident_id IN (SELECT id FROM incidents WHERE ` + pairWhere + `)`),
	stmtSeeIncident:           sqlstmt.OnWriter(`UPDATE incidents SET last_seen_at = ? WHERE ` + pairWhere + ` AND ` + openOccurrenceOf),
	stmtDeleteRecoveredBefore: sqlstmt.OnWriter(`DELETE FROM incidents WHERE last_seen_at < ? AND NOT ` + openOccurrenceOf),
	// Each ends when its incident was last seen, which every fold that sees it moves.
	stmtEndClusterOccurrences: sqlstmt.OnWriter(`UPDATE incident_occurrences
  SET ended_at = (SELECT last_seen_at FROM incidents WHERE incidents.id = incident_occurrences.incident_id)
  WHERE ended_at IS NULL AND incident_id IN (SELECT id FROM incidents WHERE cluster_id = ?)`),
	stmtEndOtherServersOccurrences: sqlstmt.OnWriter(`UPDATE incident_occurrences
  SET ended_at = (SELECT last_seen_at FROM incidents WHERE incidents.id = incident_occurrences.incident_id)
  WHERE ended_at IS NULL AND incident_id IN (SELECT id FROM incidents WHERE cluster_id = ? AND server_uid <> ?)`),
	stmtSelectOpenOccurrences: sqlstmt.OnReader(`SELECT i.subject_group, i.subject_kind, i.subject_namespace, i.subject_name, i.signal, o.started_at, i.last_seen_at
  FROM incidents i JOIN incident_occurrences o ON o.incident_id = i.id AND o.ended_at IS NULL
  WHERE i.cluster_id = ? AND i.server_uid = ?`),
}
