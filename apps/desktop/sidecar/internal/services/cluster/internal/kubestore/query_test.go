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

package kubestore

import (
	"context"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/watch"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/testutil"
)

var (
	deploymentKind = Kind{APIVersion: "apps/v1", Kind: "Deployment", Resource: "deployments"}
	replicaSetKind = Kind{APIVersion: "apps/v1", Kind: "ReplicaSet", Resource: "replicasets"}
)

// owned builds a body of kind k owned by owner (controller), with one annotation.
func owned(k Kind, uid, name, owner string) *unstructured.Unstructured {
	meta := map[string]any{
		"uid": uid, "name": name, "namespace": "prod", "resourceVersion": "1",
		"labels":      map[string]any{"app": "api", "app.kubernetes.io/name": "api"},
		"annotations": map[string]any{"note": "hello " + name},
	}
	if owner != "" {
		meta["ownerReferences"] = []any{map[string]any{
			"apiVersion": "apps/v1", "kind": "Owner", "name": "o", "uid": owner, "controller": true,
		}}
	}
	return obj(map[string]any{
		"apiVersion": k.APIVersion, "kind": k.Kind, "metadata": meta,
		"spec": map[string]any{"nodeName": "node-1",
			"containers": []any{map[string]any{"name": "api", "image": "api:1"}}},
		"status": map[string]any{"phase": "Running"},
	})
}

// populated is a store holding a Deployment that owns a ReplicaSet that owns a Pod the
// Deployment selects, running as a ServiceAccount the store does not hold, the three kinds in
// the catalog, and one event about the Pod.
func populated(t *testing.T) *Store {
	t.Helper()
	ctx := context.Background()
	s := newTestStore(t)
	require.NoError(t, s.SyncKinds(ctx, []KindRow{
		podRow, deploymentRow,
		{APIVersion: "apps/v1", Kind: "ReplicaSet", Resource: "replicasets", Scope: ScopeNamespaced},
		{APIVersion: "v1", Kind: "Event", Resource: "events", Scope: ScopeNamespaced},
	}, true, 1))
	pod := owned(podKind, "pod", "api-7d9f-0", "rs")
	pod.Object["spec"].(map[string]any)["serviceAccountName"] = "api"
	dep := owned(deploymentKind, "dep", "api", "")
	dep.Object["spec"].(map[string]any)["selector"] = map[string]any{"matchLabels": map[string]any{"app": "api"}}
	require.NoError(t, s.ApplyChange(ctx, deploymentKind, watch.Added, dep))
	require.NoError(t, s.ApplyChange(ctx, replicaSetKind, watch.Added, owned(replicaSetKind, "rs", "api-7d9f", "dep")))
	require.NoError(t, s.ApplyChange(ctx, podKind, watch.Added, pod))
	require.NoError(t, s.ApplyChange(ctx, eventsKind, watch.Added, obj(map[string]any{
		"apiVersion": "v1", "kind": "Event",
		"metadata":       map[string]any{"uid": "ev", "namespace": "prod"},
		"involvedObject": map[string]any{"uid": "pod", "kind": "Pod", "name": "api-7d9f-0", "namespace": "prod"},
		"reason":         "BackOff", "message": "Back-off pulling image ImagePullBackOff",
		"type": "Warning", "lastTimestamp": "2026-09-27T10:00:00Z",
	})))
	return s
}

// query runs sql with room for every row the fixtures hold.
func query(t *testing.T, s *Store, sql string) QueryResult {
	t.Helper()
	res, err := s.Query(context.Background(), sql, 100, 1<<20)
	require.NoError(t, err)
	return res
}

// The views are the contract the prompt states, column for column.
func TestEveryViewReads(t *testing.T) {
	t.Parallel()

	s := populated(t)
	for view, columns := range map[string][]string{
		"objects": {"uid", "api_version", "api_group", "version", "kind", "resource", "namespace", "name",
			"created_at", "changed_at", "generation", "resource_version", "status", "ready", "total",
			"restarts", "node", "owner_uid", "labels", "body"},
		"labels":      {"uid", "key", "value"},
		"annotations": {"uid", "key", "value"},
		"owners":      {"uid", "owner_uid", "controller"},
		"ancestors":   {"uid", "ancestor_uid", "depth"},
		"events": {"rowid", "uid", "involved_uid", "involved_kind", "involved_namespace", "involved_name",
			"type", "reason", "message", "first_seen", "last_seen", "count", "body"},
		"status_history": {"uid", "at", "status"},
		"kinds":          {"api_version", "api_group", "version", "kind", "resource", "scope", "is_crd", "count"},
		"containers": {"pod_uid", "name", "init", "position", "sidecar", "image", "image_id", "container_id", "ready", "restarts",
			"state", "reason", "exit_code", "last_reason", "last_exit_code", "last_container_id",
			"cpu_request", "cpu_limit", "memory_request", "memory_limit"},
		"selects": {"selector_uid", "uid"},
		"refs":    {"uid", "path", "to_group", "to_kind", "to_namespace", "to_name", "key", "optional", "to_uid", "to_listed"},
	} {
		res := query(t, s, `SELECT * FROM `+view)
		assert.Equal(t, columns, res.Columns, view)
		assert.NotEmpty(t, res.Rows, view)
	}
}

func TestTheViewsReadTheTables(t *testing.T) {
	t.Parallel()

	s := populated(t)

	pod := query(t, s, `SELECT api_group, version, resource, owner_uid, labels ->> '$."app.kubernetes.io/name"',
		body ->> '$.spec.nodeName' FROM objects WHERE uid = 'pod'`)
	assert.Equal(t, [][]any{{"", "v1", "pods", "rs", "api", "node-1"}}, pod.Rows)

	dep := query(t, s, `SELECT api_group, version, resource, owner_uid FROM objects WHERE uid = 'dep'`)
	assert.Equal(t, [][]any{{"apps", "v1", "deployments", nil}}, dep.Rows)

	up := query(t, s, `SELECT ancestor_uid, depth FROM ancestors WHERE uid = 'pod' ORDER BY depth`)
	assert.Equal(t, [][]any{{"rs", int64(1)}, {"dep", int64(2)}}, up.Rows)

	notes := query(t, s, `SELECT key, value FROM annotations WHERE uid = 'rs'`)
	assert.Equal(t, [][]any{{"note", "hello api-7d9f"}}, notes.Rows)

	found := query(t, s, `SELECT uid, involved_namespace FROM events
		WHERE rowid IN (SELECT rowid FROM events_fts('ImagePullBackOff'))`)
	assert.Equal(t, [][]any{{"ev", "prod"}}, found.Rows)

	kinds := query(t, s, `SELECT count FROM kinds WHERE kind = 'Pod'`)
	assert.Equal(t, [][]any{{int64(1)}}, kinds.Rows)
}

// quantity() is a statement's own call, so its bounds are what keep it cheap: 16 MB of
// digits built in the statement, or an exponent past the bound, answers NULL at once.
func TestQuantityOnAQueryConnection(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)

	res := query(t, s, `SELECT quantity('250m'), quantity(3), quantity(1.5), quantity(NULL), quantity('junk'),
		quantity(x'3130'), quantity('1e-999999999'), quantity(replace(hex(zeroblob(8000000)), '0', '9'))`)

	assert.Equal(t, [][]any{{0.25, 3.0, 1.5, nil, nil, nil, nil, nil}}, res.Rows)
}

// requestsPerNode is the note's example (docs/notes/kubequery.md), as the model would send it.
const requestsPerNode = `-- CPU requested per node against what it can allocate. A Pod asks for the larger of its
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
ORDER BY cpu_requested DESC;`

// scheduledPod is a Pod on node in phase, with init containers, containers and the rest of
// its spec as given; each container is {name, cpu request}, and an init container named
// "sidecar-…" restarts always.
func scheduledPod(uid, node, phase string, inits, containers [][2]string, spec map[string]any) *unstructured.Unstructured {
	list := func(cs [][2]string) []any {
		out := []any{}
		for _, c := range cs {
			m := map[string]any{"name": c[0], "image": c[0] + ":1",
				"resources": map[string]any{"requests": map[string]any{"cpu": c[1]}}}
			if strings.HasPrefix(c[0], "sidecar") {
				m["restartPolicy"] = "Always"
			}
			out = append(out, m)
		}
		return out
	}
	spec["nodeName"] = node
	spec["initContainers"] = list(inits)
	spec["containers"] = list(containers)
	return obj(map[string]any{
		"apiVersion": "v1", "kind": "Pod",
		"metadata": map[string]any{"uid": uid, "name": uid, "namespace": "prod", "resourceVersion": "1"},
		"spec":     spec,
		"status":   map[string]any{"phase": phase},
	})
}

func TestTheContainersViewAnswersRequestsPerNode(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	s := newTestStore(t)
	nodeKind := Kind{APIVersion: "v1", Kind: "Node", Resource: "nodes"}
	for _, name := range []string{"n1", "n2", "n3"} {
		require.NoError(t, s.ApplyChange(ctx, nodeKind, watch.Added, obj(map[string]any{
			"apiVersion": "v1", "kind": "Node",
			"metadata": map[string]any{"uid": name, "name": name, "resourceVersion": "1"},
			"status":   map[string]any{"allocatable": map[string]any{"cpu": "4"}},
		})))
	}
	for _, p := range []*unstructured.Unstructured{
		// A sidecar started before the largest init container counts toward its peak:
		// max(0.2 + 0.5, 1 + 0.5).
		scheduledPod("before", "n1", "Running", [][2]string{{"sidecar-a", "500m"}, {"migrate", "1"}},
			[][2]string{{"api", "200m"}}, map[string]any{}),
		// One started after it does not: max(0.2 + 0.5, 1).
		scheduledPod("after", "n2", "Running", [][2]string{{"migrate", "1"}, {"sidecar-a", "500m"}},
			[][2]string{{"api", "200m"}}, map[string]any{}),
		// Overhead is added: 0.3 + 0.1.
		scheduledPod("overhead", "n3", "Running", nil, [][2]string{{"api", "300m"}},
			map[string]any{"overhead": map[string]any{"cpu": "100m"}}),
		// A crashing Pod still holds its request: 0.25.
		scheduledPod("crashing", "n3", "Running", nil, [][2]string{{"api", "250m"}}, map[string]any{}),
		// A finished one holds nothing.
		scheduledPod("done", "n3", "Succeeded", nil, [][2]string{{"job", "5"}}, map[string]any{}),
		// A Pod's own request stands for its containers': 0.75.
		scheduledPod("own", "n3", "Running", nil, [][2]string{{"api", "100m"}},
			map[string]any{"resources": map[string]any{"requests": map[string]any{"cpu": "750m"}}}),
	} {
		require.NoError(t, s.ApplyChange(ctx, podKind, watch.Added, p))
	}
	crashing := scheduledPod("crashing", "n3", "Running", nil, [][2]string{{"api", "250m"}}, map[string]any{})
	crashing.SetResourceVersion("2")
	crashing.Object["status"] = map[string]any{"phase": "Running", "containerStatuses": []any{map[string]any{
		"name": "api", "state": map[string]any{"waiting": map[string]any{"reason": "CrashLoopBackOff"}}}}}
	require.NoError(t, s.ApplyChange(ctx, podKind, watch.Modified, crashing))

	res := query(t, s, requestsPerNode)

	assert.Equal(t, []string{"node", "cpu_requested", "cpu_allocatable"}, res.Columns)
	assert.Equal(t, [][]any{{"n1", 1.5, 4.0}, {"n3", 1.4, 4.0}, {"n2", 1.0, 4.0}}, res.Rows)
}

// ten counts 1 to 10, one row each, each cell ten bytes of text.
const ten = `WITH RECURSIVE n(i) AS (SELECT 1 UNION ALL SELECT i + 1 FROM n WHERE i < 10)
	SELECT i, printf('%010d', i) AS text FROM n`

func TestAQueryStopsAtItsRowLimit(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)

	cut, err := s.Query(context.Background(), ten, 3, 1<<20)
	require.NoError(t, err)
	whole, err := s.Query(context.Background(), ten, 10, 1<<20)
	require.NoError(t, err)

	assert.Len(t, cut.Rows, 3)
	assert.True(t, cut.More)
	assert.Len(t, whole.Rows, 10)
	assert.False(t, whole.More)
}

// The byte limit counts text and blob cells; a row that would pass it is left out whole.
func TestAQueryStopsAtItsByteLimit(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)

	res, err := s.Query(context.Background(), ten, 100, 25)
	require.NoError(t, err)

	assert.Equal(t, [][]any{{int64(1), "0000000001"}, {int64(2), "0000000002"}}, res.Rows)
	assert.True(t, res.More)
}

func TestAStatementErrorIsAQueryError(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)

	_, err := s.Query(context.Background(), `SELECT nope FROM objects`, 10, 1<<20)

	var qe *QueryError
	require.ErrorAs(t, err, &qe)
	assert.Contains(t, qe.Message, "no such column: nope")
	assert.Equal(t, qe.Message, err.Error())
}

// slowPastTheFirstRow answers its first row at once and computes its second for minutes.
const slowPastTheFirstRow = `WITH RECURSIVE n(i) AS (SELECT 1 UNION ALL SELECT i + 1 FROM n WHERE i < 2)
	SELECT i, CASE WHEN i = 1 THEN 0 ELSE (
		WITH RECURSIVE c(x) AS (SELECT 1 UNION ALL SELECT x + 1 FROM c WHERE x < 1000000000)
		SELECT count(*) FROM c) END AS v
	FROM n`

// queryError runs sql and answers the *QueryError it must fail with.
func queryError(t *testing.T, s *Store, sql string) *QueryError {
	t.Helper()
	_, err := s.Query(context.Background(), sql, 10, 1<<20)
	var qe *QueryError
	require.ErrorAs(t, err, &qe, sql)
	return qe
}

// A statement runs as a subquery, so only a query runs. Some pragmas change every
// connection in the process, and SQLite applies them while preparing, so the process's
// settings must be unchanged after each is refused.
func TestOnlyAQueryRuns(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	dir := t.TempDir()
	writer := db(t, s)
	t.Cleanup(func() {
		_, _ = writer.Exec(`PRAGMA temp_store_directory = ''`)
		_, _ = writer.Exec(`PRAGMA soft_heap_limit = 0`)
	})

	for _, sql := range []string{
		`PRAGMA temp_store_directory = '` + dir + `'`,
		`PRAGMA soft_heap_limit = 12345`,
		`ATTACH '` + dir + `/other.db' AS other`,
		`VACUUM INTO '` + dir + `/copy.db'`,
		`DROP TABLE main.objects`,
		`EXPLAIN SELECT 1`,
		`-- a comment alone`,
		`SELECT 1; SELECT 2`,
	} {
		assert.Contains(t, queryError(t, s, sql).Message, onlyOneQuery, sql)
	}

	var soft int64
	require.NoError(t, writer.QueryRow(`PRAGMA soft_heap_limit`).Scan(&soft))
	assert.Zero(t, soft)
	var tempDir string
	rows, err := writer.Query(`PRAGMA temp_store_directory`)
	require.NoError(t, err)
	for rows.Next() {
		require.NoError(t, rows.Scan(&tempDir))
	}
	require.NoError(t, rows.Close())
	assert.Empty(t, tempDir)
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	assert.Empty(t, entries)
}

// A value is bound to its placeholder, never pasted into the statement, so a quote
// or a semicolon in it is matched as written.
func TestAValueIsBoundToItsPlaceholder(t *testing.T) {
	ctx := context.Background()
	s := populated(t)
	const odd = `it's; here`
	require.NoError(t, s.ApplyChange(ctx, podKind, watch.Added, owned(podKind, "odd", odd, "")))

	res, err := s.Query(ctx, `SELECT uid, name FROM objects WHERE kind = ? AND name = ?`, 10, 1<<20, "Pod", odd)

	require.NoError(t, err)
	assert.Equal(t, [][]any{{"odd", odd}}, res.Rows)
}

func TestATrailingSemicolonRuns(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	for _, sql := range []string{`SELECT 1;`, "SELECT 1 ;  \n"} {
		assert.Equal(t, [][]any{{int64(1)}}, query(t, s, sql).Rows, sql)
	}
}

// A trailing unclosed /* would hide the wrap's tail and let the rest of the text run
// outside it; the statement prepared alone refuses the stray ")".
func TestACommentCannotEscapeTheWrap(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)

	qe := queryError(t, s, `SELECT 1) LIMIT 1), r AS (`+slowPastTheFirstRow+`) SELECT * FROM r /*`)

	assert.Contains(t, qe.Message, onlyOneQuery)
}

func TestATrailingLineCommentRuns(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	assert.Equal(t, [][]any{{int64(1)}}, query(t, s, `SELECT 1 -- done`).Rows)
}

func TestAQueryKeepsItsOrder(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	res := query(t, s, `SELECT i FROM (`+ten+`) ORDER BY i DESC`)
	assert.Equal(t, []any{int64(10)}, res.Rows[0])
	assert.Equal(t, []any{int64(1)}, res.Rows[9])
}

func TestARepeatedColumnNameIsNumbered(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	assert.Equal(t, []string{"a", "a:1"}, query(t, s, `SELECT 1 AS a, 2 AS a`).Columns)
}

// An error while the statement runs is SQLite's own: the statement was a query.
func TestARunTimeErrorIsSQLitesOwn(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)

	qe := queryError(t, s, `SELECT json('{')`)

	assert.Contains(t, qe.Message, "malformed JSON")
	assert.NotContains(t, qe.Message, onlyOneQuery)
}

// modernc interrupts only the step that fetches the first row, so a statement slow past it
// ends at its deadline only because the wrap runs all of it in that step.
func TestADeadlinePastTheFirstRowInterruptsTheQuery(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, err := s.Query(ctx, slowPastTheFirstRow, 10, 1<<20)

	require.ErrorIs(t, err, context.DeadlineExceeded)
	assert.Less(t, time.Since(start), testutil.Timeout)
}

// A query waits for one of the pool's connections only as long as its context allows.
func TestAQueryEndsWithItsContextBeforeItHasAConnection(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := s.Query(ctx, `SELECT 1`, 10, 1<<20)

	require.ErrorIs(t, err, context.Canceled)
}

// queryConnOf is a query connection with no wrap in front of it, as Query takes one.
func queryConnOf(t *testing.T, s *Store) *sql.Conn {
	t.Helper()
	f, err := s.file()
	require.NoError(t, err)
	conn, err := queryConn(context.Background(), f.queryDB.DB)
	require.NoError(t, err)
	t.Cleanup(func() { conn.Close() })
	return conn
}

func TestAQueryConnectionCannotWrite(t *testing.T) {
	t.Parallel()

	s := populated(t)
	conn := queryConnOf(t, s)

	_, err := conn.ExecContext(context.Background(), `DELETE FROM main.objects`)

	require.ErrorContains(t, err, "readonly")
	assert.Equal(t, 3, countRows(t, s, `SELECT count(*) FROM objects`))
}

// ATTACH and VACUUM INTO both open another file, which the attached-database limit
// refuses: a read-only connection can still attach a file that exists and write a copy.
func TestAQueryConnectionCannotAttach(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	dir := t.TempDir()
	other, err := sql.Open("sqlite", "file:"+filepath.Join(dir, "other.db"))
	require.NoError(t, err)
	_, err = other.Exec(`CREATE TABLE t (x)`)
	require.NoError(t, err)
	require.NoError(t, other.Close())
	conn := queryConnOf(t, s)

	_, err = conn.ExecContext(context.Background(), `ATTACH '`+filepath.Join(dir, "other.db")+`' AS other`)
	require.Error(t, err)
	_, err = conn.ExecContext(context.Background(), `VACUUM INTO '`+filepath.Join(dir, "copy.db")+`'`)
	require.Error(t, err)

	assert.NoFileExists(t, filepath.Join(dir, "copy.db"))
}

// A view dropped and a temp table left on one connection do not reach the next query:
// objects.resource is a column of the view alone, so a reused connection, on which
// objects then names the table, would fail it.
func TestAQueryConnectionLeavesNothingForTheNext(t *testing.T) {
	t.Parallel()

	s := populated(t)
	conn := queryConnOf(t, s)
	_, err := conn.ExecContext(context.Background(), `DROP VIEW objects`)
	require.NoError(t, err)
	_, err = conn.ExecContext(context.Background(), `CREATE TEMP TABLE left_behind (x)`)
	require.NoError(t, err)
	require.NoError(t, conn.Close())

	res := query(t, s, `SELECT resource FROM objects WHERE uid = 'pod'`)
	left := query(t, s, `SELECT count(*) FROM temp.sqlite_master WHERE name = 'left_behind'`)

	assert.Equal(t, [][]any{{"pods"}}, res.Rows)
	assert.Equal(t, [][]any{{int64(0)}}, left.Rows)
}

// A reading's statements read the views and bind their values as Query does, on a
// connection the next reading takes again.
func TestReadOwnReadsTheViewsOnAKeptConnection(t *testing.T) {
	s := populated(t)
	for range 2 {
		require.NoError(t, s.ReadOwn(context.Background(), func(r Reading) error {
			res, err := r.Query(`SELECT resource FROM objects WHERE uid = ?`, 100, 1<<20, "pod")
			require.NoError(t, err)
			assert.Equal(t, [][]any{{"pods"}}, res.Rows)
			return nil
		}))
	}

	f, err := s.file()
	require.NoError(t, err)
	assert.Equal(t, 1, f.ownQueryDB.Stats().OpenConnections)
	assert.Zero(t, f.queryDB.Stats().OpenConnections)
}

// A reading's statements see the cache as its first did, whatever commits between
// them, and the next reading sees the commit.
func TestAReadingIsOneSnapshot(t *testing.T) {
	ctx := context.Background()
	s := populated(t)
	pods := func(r Reading) [][]any {
		res, err := r.Query(`SELECT name FROM objects WHERE kind = 'Pod' ORDER BY name`, 100, 1<<20)
		require.NoError(t, err)
		return res.Rows
	}

	require.NoError(t, s.ReadOwn(ctx, func(r Reading) error {
		before := pods(r)
		require.NoError(t, s.ApplyChange(ctx, podKind, watch.Added, owned(podKind, "pod-2", "api-7d9f-1", "rs")))
		assert.Equal(t, before, pods(r))
		return nil
	}))
	require.NoError(t, s.ReadOwn(ctx, func(r Reading) error {
		assert.Equal(t, [][]any{{"api-7d9f-0"}, {"api-7d9f-1"}}, pods(r))
		return nil
	}))
}

// prepares counts the statements s compiles from now on.
func prepares(t *testing.T, s *Store) *atomic.Int32 {
	t.Helper()
	f, err := s.file()
	require.NoError(t, err)
	n := new(atomic.Int32)
	f.onPrepare = func() { n.Add(1) }
	return n
}

// A statement a reading runs again is compiled once. Its row limit is part of it.
func TestAReadingCompilesARepeatOnce(t *testing.T) {
	s := populated(t)
	n := prepares(t, s)

	require.NoError(t, s.ReadOwn(context.Background(), func(r Reading) error {
		_, err := r.Query(`SELECT uid FROM objects ORDER BY uid`, 1, 1<<20)
		require.NoError(t, err)
		one, err := r.Query(`SELECT uid FROM objects ORDER BY uid`, 1, 1<<20)
		require.NoError(t, err)
		all, err := r.Query(`SELECT uid FROM objects ORDER BY uid`, 100, 1<<20)
		require.NoError(t, err)

		assert.Equal(t, [][]any{{"dep"}}, one.Rows)
		assert.True(t, one.More)
		assert.Equal(t, [][]any{{"dep"}, {"pod"}, {"rs"}}, all.Rows)
		assert.False(t, all.More)
		assert.Equal(t, int32(2), n.Load())
		return nil
	}))
}

// A statement compiles twice in its first reading, once for the reading and once for
// the file's pool, then never again.
func TestAFileCompilesAStatementOnce(t *testing.T) {
	s := populated(t)
	n := prepares(t, s)
	read := func() {
		t.Helper()
		require.NoError(t, s.ReadOwn(context.Background(), func(r Reading) error {
			res, err := r.Query(`SELECT uid FROM objects WHERE uid <> ? ORDER BY uid`, 2, 1<<20, "dep")
			require.NoError(t, err)
			assert.Equal(t, [][]any{{"pod"}, {"rs"}}, res.Rows)
			return nil
		}))
	}

	read()
	assert.Equal(t, int32(2), n.Load(), "the reading, then the pool's copy")
	read()
	assert.Equal(t, int32(2), n.Load(), "a later reading")
}

// Past the table's cap a statement compiles for each reading and is never kept, so
// texts built from data cannot grow the table.
func TestATextPastTheCapIsNotKept(t *testing.T) {
	s := populated(t)
	f, err := s.file()
	require.NoError(t, err)
	f.own.max = 1
	for _, text := range []string{`SELECT 1`, `SELECT 2`, `SELECT 1`} {
		require.NoError(t, s.ReadOwn(context.Background(), func(r Reading) error {
			_, err := r.Query(text, 10, 1<<20)
			return err
		}))
	}

	assert.NotNil(t, f.own.get(ownKey{`SELECT 1`, 10}))
	assert.Nil(t, f.own.get(ownKey{`SELECT 2`, 10}))
}

// Readings holding every connection of the pool can each meet a text the file has not
// compiled; all of them finish, and each text is kept.
func TestReadingsThatMissTogetherFinish(t *testing.T) {
	s := populated(t)
	f, err := s.file()
	require.NoError(t, err)
	var inside sync.WaitGroup
	inside.Add(queryPoolSize)
	done := make(chan error, queryPoolSize)
	for i := range queryPoolSize {
		go func() {
			done <- s.ReadOwn(context.Background(), func(r Reading) error {
				inside.Done()
				inside.Wait()
				_, err := r.Query(fmt.Sprintf(`SELECT %d FROM objects`, i), 10, 1<<20)
				return err
			})
		}()
	}
	for range queryPoolSize {
		require.NoError(t, testutil.Recv(t, done, "a reading"))
	}
	for i := range queryPoolSize {
		assert.NotNil(t, f.own.get(ownKey{fmt.Sprintf(`SELECT %d FROM objects`, i), 10}))
	}
}

// A reading that fails leaves its connection with no transaction open, so the next
// reading on it sees the cache as it is.
func TestAFailedReadingEndsItsTransaction(t *testing.T) {
	ctx := context.Background()
	s := populated(t)
	failed := errors.New("failed")

	require.ErrorIs(t, s.ReadOwn(ctx, func(r Reading) error {
		_, err := r.Query(`SELECT 1`, 10, 1<<20)
		require.NoError(t, err)
		return failed
	}), failed)
	require.NoError(t, s.ApplyChange(ctx, podKind, watch.Added, owned(podKind, "pod-2", "api-7d9f-1", "rs")))
	require.NoError(t, s.ReadOwn(ctx, func(r Reading) error {
		res, err := r.Query(`SELECT count(*) FROM objects WHERE kind = 'Pod'`, 10, 1<<20)
		require.NoError(t, err)
		assert.Equal(t, [][]any{{int64(2)}}, res.Rows)
		return nil
	}))
}

func TestAValuePastTheLengthLimitFails(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)

	qe := queryError(t, s, `SELECT length(zeroblob(17 * 1024 * 1024))`)

	assert.Contains(t, qe.Message, "too big")
	assert.NotContains(t, qe.Message, onlyOneQuery)
}

// queryResult is one Query's answer, for a test that runs it on a goroutine.
type queryResult struct {
	res QueryResult
	err error
}

// runQuery runs sql on its own goroutine and delivers what it answers.
func runQuery(s *Store, sql string) <-chan queryResult {
	out := make(chan queryResult, 1)
	go func() {
		res, err := s.Query(context.Background(), sql, 10, 1<<20)
		out <- queryResult{res, err}
	}()
	return out
}

// A clear cancels the file's queries rather than waiting them out: this one runs for
// minutes and has no deadline of its own.
func TestAClearInterruptsARunningQuery(t *testing.T) {
	t.Parallel()

	m := NewManager(t.TempDir(), Retention{})
	t.Cleanup(func() { require.NoError(t, m.Close()) })
	s, err := m.OpenOrCreate(1)
	require.NoError(t, err)
	t.Cleanup(s.Release)
	f, err := s.file()
	require.NoError(t, err)
	running := testutil.NewSignal()
	f.onQuery = func() { running.Fire() }

	done := runQuery(s, slowPastTheFirstRow)
	running.Wait(t, "the query to start")
	testutil.WaitReturn(t, func() { require.NoError(t, m.Clear(1)) }, "the clear")

	require.ErrorIs(t, testutil.Recv(t, done, "the query").err, ErrClosed)
}

// A close waits for its queries' connections before the file goes, which Windows needs to
// delete it.
func TestACloseWaitsForItsQueries(t *testing.T) {
	t.Parallel()

	m := NewManager(t.TempDir(), Retention{})
	t.Cleanup(func() { require.NoError(t, m.Close()) })
	s, err := m.OpenOrCreate(1)
	require.NoError(t, err)
	t.Cleanup(s.Release)
	f, err := s.file()
	require.NoError(t, err)
	held, release := testutil.NewSignal(), make(chan struct{})
	f.afterQuery = func() {
		held.Fire()
		<-release
	}

	done := runQuery(s, `SELECT 1`)
	held.Wait(t, "the query to hold its connection")
	cleared := make(chan error, 1)
	go func() { cleared <- m.Clear(1) }()

	// A negative assertion: nothing marks the moment a close would wrongly return, so it is
	// given a window, and fails the instant the clear returns inside it.
	testutil.NoRecv(t, cleared, 100*time.Millisecond, "the clear, while a query holds its connection")
	close(release)

	require.NoError(t, testutil.Recv(t, cleared, "the clear"))
	require.NoError(t, testutil.Recv(t, done, "the query").err)
}

// A clear closes the statements its file compiled for readings, with its pools.
func TestAClearClosesTheReadingsStatements(t *testing.T) {
	t.Parallel()

	m := NewManager(t.TempDir(), Retention{})
	t.Cleanup(func() { require.NoError(t, m.Close()) })
	s, err := m.OpenOrCreate(1)
	require.NoError(t, err)
	t.Cleanup(s.Release)
	f, err := s.file()
	require.NoError(t, err)
	for range 2 {
		require.NoError(t, s.ReadOwn(context.Background(), func(r Reading) error {
			_, err := r.Query(`SELECT 1`, 10, 1<<20)
			return err
		}))
	}
	stmt := f.own.get(ownKey{`SELECT 1`, 10})
	require.NotNil(t, stmt)

	require.NoError(t, m.Clear(1))

	_, err = stmt.QueryContext(context.Background())
	require.ErrorContains(t, err, "statement is closed")
}

func TestAReadingAfterACloseIsClosed(t *testing.T) {
	t.Parallel()

	err := closedStore(t).ReadOwn(context.Background(), func(Reading) error { return nil })
	require.ErrorIs(t, err, ErrClosed)
}

// A reading refuses a second statement as Query does, before it compiles anything.
func TestAReadingRunsOneStatement(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	require.NoError(t, s.ReadOwn(context.Background(), func(r Reading) error {
		_, err := r.Query(`SELECT 1; SELECT 2`, 10, 1<<20)
		var qe *QueryError
		require.ErrorAs(t, err, &qe)
		assert.Equal(t, onlyOneQuery, qe.Message)
		return nil
	}))
}

func TestAQueryAfterACloseIsClosed(t *testing.T) {
	t.Parallel()

	_, err := closedStore(t).Query(context.Background(), `SELECT 1`, 10, 1<<20)
	require.ErrorIs(t, err, ErrClosed)
}

// body() is JSON text for a stored body and NULL for anything else, as a watch serves a body
// that will not load.
func TestBodyIsNullForWhatIsNotABody(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	res := query(t, s, `SELECT body(NULL), body(x'00'), body('text')`)
	assert.Equal(t, [][]any{{nil, nil, nil}}, res.Rows)
}

func TestTheByteLimitCountsBlobs(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)

	res, err := s.Query(context.Background(), `SELECT zeroblob(10) UNION ALL SELECT zeroblob(10)`, 10, 15)

	require.NoError(t, err)
	assert.Len(t, res.Rows, 1)
	assert.True(t, res.More)
}

func TestACancelledQueryAnswersItsContext(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := s.Query(ctx, `SELECT 1`, 10, 1<<20)

	require.ErrorIs(t, err, context.Canceled)
}

// A small blob can inflate to far more than a query may build, so body() stops at the
// length limit rather than decompressing it all first.
func TestBodyStopsAtTheLengthLimit(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	bomb, err := compressRaw(make([]byte, 2*queryMaxLength))
	require.NoError(t, err)

	qe := queryError(t, s, `SELECT length(body(x'`+hex.EncodeToString(bomb)+`'))`)

	assert.Contains(t, qe.Message, "past the 16 MiB limit")
}

// labeledPod is a Pod of the given uid in namespace ns carrying the given labels.
func labeledPod(uid, ns string, labels map[string]any) *unstructured.Unstructured {
	return obj(map[string]any{
		"apiVersion": "v1", "kind": "Pod",
		"metadata": map[string]any{"uid": uid, "name": uid, "namespace": ns, "resourceVersion": "1", "labels": labels},
	})
}

// selecting is an object of kind k, of the given uid in namespace ns, with the given spec.
func selecting(k Kind, uid, ns string, spec map[string]any) *unstructured.Unstructured {
	return obj(map[string]any{
		"apiVersion": k.APIVersion, "kind": k.Kind,
		"metadata": map[string]any{"uid": uid, "name": uid, "namespace": ns, "resourceVersion": "1"},
		"spec":     spec,
	})
}

func TestTheSelectsViewMatchesPods(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	s := newTestStore(t)
	networkPolicyKind := Kind{APIVersion: "networking.k8s.io/v1", Kind: "NetworkPolicy", Resource: "networkpolicies"}
	pdbKind := Kind{APIVersion: "policy/v1", Kind: "PodDisruptionBudget", Resource: "poddisruptionbudgets"}
	expressions := func(exprs ...map[string]any) map[string]any {
		list := []any{}
		for _, e := range exprs {
			list = append(list, e)
		}
		return map[string]any{"selector": map[string]any{"matchExpressions": list}}
	}
	for _, p := range []*unstructured.Unstructured{
		labeledPod("web-1", "prod", map[string]any{"app": "web", "tier": "front"}),
		labeledPod("web-2", "prod", map[string]any{"app": "web"}),
		labeledPod("db-1", "prod", map[string]any{"app": "db"}),
		labeledPod("bare", "prod", nil),
		labeledPod("web-3", "other", map[string]any{"app": "web"}),
	} {
		require.NoError(t, s.ApplyChange(ctx, podKind, watch.Added, p))
	}
	// Not a Pod, so selected by nothing, whatever its labels.
	rs := selecting(replicaSetKind, "rs", "prod", map[string]any{})
	rs.SetLabels(map[string]string{"app": "web"})
	require.NoError(t, s.ApplyChange(ctx, replicaSetKind, watch.Added, rs))

	for _, sel := range []struct {
		k    Kind
		uid  string
		ns   string
		spec map[string]any
	}{
		{serviceKind, "svc", "prod", map[string]any{"selector": map[string]any{"app": "web"}}},
		{serviceKind, "svc-other", "other", map[string]any{"selector": map[string]any{"app": "web"}}},
		{networkPolicyKind, "np-all", "prod", map[string]any{"podSelector": map[string]any{}}},
		{deploymentKind, "dep-db", "prod", map[string]any{"selector": map[string]any{"matchLabels": map[string]any{"app": "db"}}}},
		{pdbKind, "pdb-none", "prod", map[string]any{"minAvailable": int64(1)}},
		{deploymentKind, "in", "prod", expressions(map[string]any{"key": "app", "operator": "In", "values": []any{"web", "db"}})},
		{deploymentKind, "notin", "prod", expressions(map[string]any{"key": "tier", "operator": "NotIn", "values": []any{"front"}})},
		{deploymentKind, "exists", "prod", expressions(map[string]any{"key": "tier", "operator": "Exists"})},
		{deploymentKind, "absent", "prod", expressions(map[string]any{"key": "tier", "operator": "DoesNotExist"})},
		{deploymentKind, "both", "prod", map[string]any{"selector": map[string]any{
			"matchLabels":      map[string]any{"app": "web"},
			"matchExpressions": []any{map[string]any{"key": "tier", "operator": "Exists"}},
		}}},
	} {
		require.NoError(t, s.ApplyChange(ctx, sel.k, watch.Added, selecting(sel.k, sel.uid, sel.ns, sel.spec)))
	}

	res := query(t, s, `SELECT selector_uid, uid FROM selects ORDER BY selector_uid, uid`)
	assert.Equal(t, [][]any{
		{"absent", "bare"}, {"absent", "db-1"}, {"absent", "web-2"},
		{"both", "web-1"},
		{"dep-db", "db-1"},
		{"exists", "web-1"},
		{"in", "db-1"}, {"in", "web-1"}, {"in", "web-2"},
		{"notin", "bare"}, {"notin", "db-1"}, {"notin", "web-2"},
		{"np-all", "bare"}, {"np-all", "db-1"}, {"np-all", "web-1"}, {"np-all", "web-2"},
		{"svc", "web-1"}, {"svc", "web-2"},
		{"svc-other", "web-3"},
	}, res.Rows)

	one := query(t, s, `SELECT selector_uid FROM selects WHERE uid = 'web-3'`)
	assert.Equal(t, [][]any{{"svc-other"}}, one.Rows)
}

func TestTheRefsViewResolvesTargets(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	s := newTestStore(t)
	configMapKind := Kind{APIVersion: "v1", Kind: "ConfigMap", Resource: "configmaps"}
	clusterRoleKind := Kind{APIVersion: rbacGroup + "/v1", Kind: "ClusterRole", Resource: "clusterroles"}
	bindingKind := Kind{APIVersion: rbacGroup + "/v1", Kind: "RoleBinding", Resource: "rolebindings"}
	widgetV1 := Kind{APIVersion: "example.com/v1", Kind: "Widget", Resource: "widgets"}
	hpaKind := Kind{APIVersion: "autoscaling/v2", Kind: "HorizontalPodAutoscaler", Resource: "horizontalpodautoscalers"}
	var rows []KindRow
	for _, k := range []Kind{podKind, configMapKind, clusterRoleKind, bindingKind, widgetV1, hpaKind,
		{APIVersion: "v1", Kind: "Secret", Resource: "secrets"},
		// The same kind at a second version: the lookup must not double a row.
		{APIVersion: "example.com/v2", Kind: "Widget", Resource: "widgets"},
	} {
		rows = append(rows, KindRow{APIVersion: k.APIVersion, Kind: k.Kind, Resource: k.Resource, Scope: ScopeNamespaced})
	}
	require.NoError(t, s.SyncKinds(ctx, rows, true, 1))
	for _, r := range rows {
		require.NoError(t, s.SetCookie(ctx, r.APIVersion, r.Resource, "1"))
	}

	for _, o := range []struct {
		k Kind
		u *unstructured.Unstructured
	}{
		{configMapKind, selecting(configMapKind, "cm", "prod", nil)},
		{clusterRoleKind, obj(map[string]any{"apiVersion": clusterRoleKind.APIVersion, "kind": "ClusterRole",
			"metadata": map[string]any{"uid": "view", "name": "view", "resourceVersion": "1"}})},
		{widgetV1, selecting(widgetV1, "w", "prod", nil)},
		{podKind, obj(map[string]any{"apiVersion": "v1", "kind": "Pod",
			"metadata": map[string]any{"uid": "pod", "name": "pod", "namespace": "prod", "resourceVersion": "1"},
			"spec": map[string]any{"volumes": []any{
				map[string]any{"name": "a", "configMap": map[string]any{"name": "cm"}},
				map[string]any{"name": "b", "secret": map[string]any{"secretName": "missing"}},
			}}})},
		{bindingKind, obj(map[string]any{"apiVersion": bindingKind.APIVersion, "kind": "RoleBinding",
			"metadata": map[string]any{"uid": "rb", "name": "rb", "namespace": "prod", "resourceVersion": "1"},
			"roleRef":  map[string]any{"apiGroup": rbacGroup, "kind": "ClusterRole", "name": "view"}})},
		{hpaKind, selecting(hpaKind, "hpa", "prod", map[string]any{
			"scaleTargetRef": map[string]any{"apiVersion": "example.com/v1", "kind": "Widget", "name": "w"}})},
	} {
		require.NoError(t, s.ApplyChange(ctx, o.k, watch.Added, o.u))
	}

	res := query(t, s, `SELECT uid, path, to_namespace, to_uid, to_listed FROM refs ORDER BY uid, path`)
	assert.Equal(t, [][]any{
		{"hpa", "$.spec.scaleTargetRef", "prod", "w", int64(1)},
		{"pod", "$.spec.volumes[0].configMap.name", "prod", "cm", int64(1)},
		{"pod", "$.spec.volumes[1].secret.secretName", "prod", nil, int64(1)},
		{"rb", "$.roleRef", "", "view", int64(1)},
	}, res.Rows)
}

// to_listed is 1 only while the target's kind has a completed list on disk.
func TestTheRefsViewSaysWhenItCannotTell(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	s := newTestStore(t)
	secretKind := Kind{APIVersion: "v1", Kind: "Secret", Resource: "secrets"}
	require.NoError(t, s.SyncKinds(ctx, []KindRow{podRow,
		{APIVersion: "v1", Kind: "Secret", Resource: "secrets", Scope: ScopeNamespaced}}, true, 1))
	require.NoError(t, s.ApplyChange(ctx, podKind, watch.Added, obj(map[string]any{"apiVersion": "v1", "kind": "Pod",
		"metadata": map[string]any{"uid": "pod", "name": "pod", "namespace": "prod", "resourceVersion": "1"},
		"spec": map[string]any{
			"volumes":            []any{map[string]any{"name": "a", "secret": map[string]any{"secretName": "creds"}}},
			"serviceAccountName": "api",
		}})))
	listed := func() [][]any {
		return query(t, s, `SELECT to_kind, to_listed FROM refs ORDER BY to_kind`).Rows
	}

	// A catalog row and no list; and a kind the catalog does not hold.
	assert.Equal(t, [][]any{{"Secret", int64(0)}, {"ServiceAccount", int64(0)}}, listed())

	session := beginReplace(t, s, secretKind)
	require.NoError(t, session.WritePage(ctx, nil))
	_, err := session.Commit(ctx, "10")
	require.NoError(t, err)
	assert.Equal(t, [][]any{{"Secret", int64(1)}, {"ServiceAccount", int64(0)}}, listed())

	// A relist under way has cleared its kind's list.
	session = beginReplace(t, s, secretKind)
	require.NoError(t, session.WritePage(ctx, nil))
	assert.Equal(t, [][]any{{"Secret", int64(0)}, {"ServiceAccount", int64(0)}}, listed())
	_, err = session.Commit(ctx, "11")
	require.NoError(t, err)

	require.NoError(t, s.ClearKind(ctx, secretKind))
	assert.Equal(t, [][]any{{"Secret", int64(0)}, {"ServiceAccount", int64(0)}}, listed())
}

// syntheticCache is the spec's measure of the step-4 views: 10,000 Pods and 2,000 selectors
// in 50 namespaces, each selector of two terms, and each Pod making five references to 2,000
// ConfigMaps and Secrets of which half exist. Every kind is listed, as a relist leaves it.
func syntheticCache(b *testing.B) *Store {
	b.Helper()
	ctx := context.Background()
	m := NewManager(b.TempDir(), Retention{})
	b.Cleanup(func() { require.NoError(b, m.Close()) })
	s, err := m.OpenOrCreate(1)
	require.NoError(b, err)
	b.Cleanup(s.Release)

	configMapKind := Kind{APIVersion: "v1", Kind: "ConfigMap", Resource: "configmaps"}
	secretKind := Kind{APIVersion: "v1", Kind: "Secret", Resource: "secrets"}
	kinds := []Kind{podKind, configMapKind, secretKind, serviceKind, deploymentKind}
	var rows []KindRow
	for _, k := range kinds {
		rows = append(rows, KindRow{APIVersion: k.APIVersion, Kind: k.Kind, Resource: k.Resource, Scope: ScopeNamespaced})
	}
	require.NoError(b, s.SyncKinds(ctx, rows, true, 1))

	const namespaces, podsPer, selectorsPer, targetsPer = 50, 200, 40, 20
	bodies := map[Kind][]*unstructured.Unstructured{}
	add := func(k Kind, u *unstructured.Unstructured) { bodies[k] = append(bodies[k], u) }
	for n := range namespaces {
		ns := fmt.Sprintf("ns-%02d", n)
		// Half of each namespace's targets exist.
		for i := range targetsPer / 2 {
			cm := selecting(configMapKind, fmt.Sprintf("%s-cm-%d", ns, i), ns, nil)
			cm.SetName(fmt.Sprintf("cm-%d", i))
			add(configMapKind, cm)
			secret := selecting(secretKind, fmt.Sprintf("%s-sec-%d", ns, i), ns, nil)
			secret.SetName(fmt.Sprintf("sec-%d", i))
			add(secretKind, secret)
		}
		for i := range podsPer {
			ref := func(k int) string { return fmt.Sprint((i + k) % targetsPer) }
			p := labeledPod(fmt.Sprintf("%s-pod-%d", ns, i), ns, map[string]any{
				"app": fmt.Sprintf("a%d", i%10), "tier": fmt.Sprintf("t%d", i%3)})
			p.Object["spec"] = map[string]any{
				"volumes": []any{
					map[string]any{"name": "a", "configMap": map[string]any{"name": "cm-" + ref(0)}},
					map[string]any{"name": "b", "secret": map[string]any{"secretName": "sec-" + ref(0)}},
					map[string]any{"name": "c", "projected": map[string]any{"sources": []any{
						map[string]any{"configMap": map[string]any{"name": "cm-" + ref(2)}}}}},
				},
				"containers": []any{map[string]any{"name": "api",
					"env": []any{map[string]any{"name": "A", "valueFrom": map[string]any{
						"configMapKeyRef": map[string]any{"name": "cm-" + ref(1), "key": "k"}}}},
					"envFrom": []any{map[string]any{"secretRef": map[string]any{"name": "sec-" + ref(1)}}},
				}},
			}
			add(podKind, p)
		}
		for i := range selectorsPer {
			uid := fmt.Sprintf("%s-sel-%d", ns, i)
			if i%2 == 0 {
				add(serviceKind, selecting(serviceKind, uid, ns, map[string]any{"selector": map[string]any{
					"app": fmt.Sprintf("a%d", i%10), "tier": fmt.Sprintf("t%d", i%3)}}))
				continue
			}
			add(deploymentKind, selecting(deploymentKind, uid, ns, map[string]any{"selector": map[string]any{
				"matchExpressions": []any{
					map[string]any{"key": "app", "operator": "In", "values": []any{fmt.Sprintf("a%d", i%10)}},
					map[string]any{"key": "tier", "operator": "NotIn", "values": []any{fmt.Sprintf("t%d", i%3)}},
				}}}))
		}
	}
	for _, k := range kinds {
		session, err := s.BeginReplace(k)
		require.NoError(b, err)
		for page := range slices.Chunk(bodies[k], 500) {
			require.NoError(b, session.WritePage(ctx, page))
		}
		_, err = session.Commit(ctx, "1")
		require.NoError(b, err)
	}
	return s
}

// benchQuery times one statement over s, each run returning at least one row.
func benchQuery(b *testing.B, s *Store, sql string) {
	b.Helper()
	ctx := context.Background()
	for b.Loop() {
		res, err := s.Query(ctx, sql, 2000, 1<<20)
		require.NoError(b, err)
		require.NotEmpty(b, res.Rows)
	}
}

func BenchmarkSelects(b *testing.B) {
	s := syntheticCache(b)
	b.Run("every row", func(b *testing.B) { benchQuery(b, s, `SELECT count(*) FROM selects`) })
	b.Run("one selector", func(b *testing.B) {
		benchQuery(b, s, `SELECT uid FROM selects WHERE selector_uid = 'ns-25-sel-7'`)
	})
	b.Run("one pod", func(b *testing.B) {
		benchQuery(b, s, `SELECT selector_uid FROM selects WHERE uid = 'ns-25-pod-17'`)
	})
}

func BenchmarkRefs(b *testing.B) {
	s := syntheticCache(b)
	b.Run("missing targets", func(b *testing.B) {
		benchQuery(b, s, `SELECT count(*) FROM refs WHERE to_uid IS NULL`)
	})
}

// A reference finds its target through the catalog and objects_kind_ns_name, every column
// known. With no statistics the planner prefers objects_ns_kind, which walks every object of
// the namespace for each reference: twenty times slower over the benchmark's cache.
func TestTheRefsViewFindsATargetByItsIndex(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	rows, err := queryConnOf(t, s).QueryContext(context.Background(),
		`EXPLAIN QUERY PLAN SELECT count(*) FROM refs WHERE to_uid IS NULL`)
	require.NoError(t, err)
	defer rows.Close()
	var plan []string
	for rows.Next() {
		var id, parent, notUsed int
		var detail string
		require.NoError(t, rows.Scan(&id, &parent, &notUsed, &detail))
		plan = append(plan, detail)
	}
	require.NoError(t, rows.Err())

	assert.Contains(t, plan, "SEARCH k USING INDEX kind_catalog_kind (kind=?)")
	assert.Contains(t, plan, "SEARCH o USING INDEX objects_kind_ns_name (api_version=? AND kind=? AND namespace=? AND name=?)")
}
