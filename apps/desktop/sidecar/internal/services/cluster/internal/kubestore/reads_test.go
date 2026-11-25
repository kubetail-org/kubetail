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
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/watch"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The watches re-read on every ping, so a read must not queue behind the single-connection
// writer: a transaction held open would otherwise stall every open watch on the cache.
func TestAReadDoesNotQueueBehindAHeldWriteTransaction(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	s := newTestStore(t)
	require.NoError(t, s.SyncKinds(ctx, []KindRow{podRow}, true, 7))

	f, err := s.file()
	require.NoError(t, err)
	tx, err := f.db.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer tx.Rollback() //nolint:errcheck // the writer is held on purpose

	// The reader pool is separate, so this answers rather than blocking until the
	// transaction ends.
	kinds, err := s.Kinds(ctx)
	require.NoError(t, err)
	assert.Len(t, kinds, 1)
}

// The catalog is what the cluster serves and the counts are what is cached, so the join is
// outer: an advertised kind with nothing synced yet reads with Count 0 rather than vanishing
// from the nav.
func TestKindsJoinsCountsAndKeepsAnEmptyKind(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	s := newTestStore(t)
	require.NoError(t, s.SyncKinds(ctx, []KindRow{podRow, deploymentRow}, true, 7))
	require.NoError(t, s.ApplyChange(ctx, podKind, watch.Added, pod("uid-1", "api-0", "42")))

	got, err := s.Kinds(ctx)
	require.NoError(t, err)

	assert.Equal(t, []KindRow{
		{APIVersion: "apps/v1", Kind: "Deployment", Resource: "deployments", Scope: ScopeNamespaced},
		{APIVersion: "v1", Kind: "Pod", Resource: "pods", Scope: ScopeNamespaced, Count: 1},
	}, got, "ordered by (api_version, kind) for stable display")
}

// Under a namespace a namespaced kind counts its objects there alone, and the Events kind
// the events about objects there, so an event about a Node counts in none. A
// cluster-scoped kind is in no namespace, so it keeps its whole count.
func TestKindsInCountsTheNamespacedKindsInOneNamespace(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	s := newTestStore(t)
	nodeRow := KindRow{APIVersion: "v1", Kind: "Node", Resource: "nodes", Scope: ScopeCluster}
	eventRow := KindRow{APIVersion: "v1", Kind: "Event", Resource: "events", Scope: ScopeNamespaced}
	require.NoError(t, s.SyncKinds(ctx, []KindRow{podRow, deploymentRow, nodeRow, eventRow}, true, 7))

	staging := pod("uid-3", "api-2", "44")
	staging.SetNamespace("staging")
	for _, p := range []*unstructured.Unstructured{pod("uid-1", "api-0", "42"), pod("uid-2", "api-1", "43"), staging} {
		require.NoError(t, s.ApplyChange(ctx, podKind, watch.Added, p))
	}
	node := obj(map[string]any{
		"apiVersion": "v1", "kind": "Node",
		"metadata": map[string]any{"uid": "node-1", "name": "node-1", "resourceVersion": "45"},
	})
	require.NoError(t, s.ApplyChange(ctx, Kind{APIVersion: "v1", Kind: "Node", Resource: "nodes"}, watch.Added, node))
	for uid, ns := range map[string]string{"ev-1": "prod", "ev-2": "staging"} {
		e := event(uid, "2026-08-26T10:00:00Z")
		require.NoError(t, unstructured.SetNestedField(e.Object, ns, "involvedObject", "namespace"))
		require.NoError(t, s.ApplyChange(ctx, eventsKind, watch.Added, e))
	}
	nodeEvent := event("ev-3", "2026-08-26T10:00:00Z")
	nodeEvent.SetNamespace("prod")
	nodeEvent.Object["involvedObject"] = map[string]any{"uid": "node-1", "kind": "Node", "name": "node-1"}
	require.NoError(t, s.ApplyChange(ctx, eventsKind, watch.Added, nodeEvent))

	got, err := s.KindsIn(ctx, "prod")
	require.NoError(t, err)

	assert.Equal(t, []KindRow{
		{APIVersion: "apps/v1", Kind: "Deployment", Resource: "deployments", Scope: ScopeNamespaced},
		{APIVersion: "v1", Kind: "Event", Resource: "events", Scope: ScopeNamespaced, Count: 1},
		{APIVersion: "v1", Kind: "Node", Resource: "nodes", Scope: ScopeCluster, Count: 1},
		{APIVersion: "v1", Kind: "Pod", Resource: "pods", Scope: ScopeNamespaced, Count: 2},
	}, got)
}

// A cache gone under the read answers ErrClosed, never an empty catalog.
func TestKindsInReportsAClosedStore(t *testing.T) {
	t.Parallel()

	_, err := closedStore(t).KindsIn(context.Background(), "prod")

	assert.ErrorIs(t, err, ErrClosed)
}

func TestKindsInReportsATableItCannotRead(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	dropTable(t, s, "kind_catalog")

	_, err := s.KindsIn(context.Background(), "prod")

	assert.ErrorContains(t, err, "read kinds")
}

// The pair the fold forks on: the rows and the fingerprint of the sweep that wrote them,
// out of one read.
func TestKindsWithFingerprintReadsThePairTheSweepWrote(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	s := newTestStore(t)
	require.NoError(t, s.SyncKinds(ctx, []KindRow{podRow}, true, 42))

	rows, fingerprint, ok, err := s.KindsWithFingerprint(ctx)

	require.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, uint64(42), fingerprint)
	require.Len(t, rows, 1)
	assert.Equal(t, "Pod", rows[0].Kind)
}

// A file no sweep has written is the state a wipe leaves behind, and the fold must read it
// as one — never as a cluster that serves nothing.
func TestKindsWithFingerprintReportsAnUnwrittenTable(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)

	rows, _, ok, err := s.KindsWithFingerprint(context.Background())

	require.NoError(t, err)
	assert.False(t, ok, "no sweep wrote this table")
	assert.Empty(t, rows)
}

// A value that will not parse says as little about the sweep as an absent one, and reading
// it as a fingerprint would compare a number nothing wrote.
func TestKindsWithFingerprintReadsGarbageAsUnwritten(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	s := newTestStore(t)
	require.NoError(t, s.SyncKinds(ctx, []KindRow{podRow}, true, 42))
	require.NoError(t, setMeta(ctx, openFileOf(t, s).set.Stmts(), kindsFingerprintKey, "not-a-number"))

	_, _, ok, err := s.KindsWithFingerprint(ctx)

	require.NoError(t, err)
	assert.False(t, ok)
}

// last_seen has one-second resolution, and a relist re-inserts every row with fresh rowids,
// so rows tied on a second would reshuffle between two reads if the order were left to rowid.
// The uid tiebreak is what makes the order total.
func TestEventsBreaksLastSeenTiesByUID(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	s := newTestStore(t)
	for _, uid := range []string{"uid-a", "uid-c", "uid-b"} {
		require.NoError(t, s.ApplyChange(ctx, eventsKind, watch.Added, event(uid, "2026-08-26T10:00:00Z")))
	}

	got, _, err := s.EventsWithCursor(ctx)
	require.NoError(t, err)

	require.Len(t, got, 3)
	assert.Equal(t, []string{"uid-c", "uid-b", "uid-a"},
		[]string{got[0].UID, got[1].UID, got[2].UID})
}

// Objects are keyed by Kind while a watch names the plural, so the read translates through
// kind_catalog. A kind the sweep has not registered resolves to nothing — which is why the
// catalog's rows are load-bearing for this family rather than decoration.
func TestObjectsResolveThePluralThroughTheCatalog(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	s := newTestStore(t)
	require.NoError(t, s.ApplyChange(ctx, podKind, watch.Added, pod("uid-1", "api-0", "42")))

	got, _, err := s.ObjectsWithCursor(ctx, "v1", "pods")
	require.NoError(t, err)
	assert.Empty(t, got, "no catalog row, so the plural names no Kind")

	require.NoError(t, s.SyncKinds(ctx, []KindRow{podRow}, true, 7))

	got, _, err = s.ObjectsWithCursor(ctx, "v1", "pods")
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "uid-1", got[0].UID)
	assert.Equal(t, "42", got[0].ResourceVersion)
}

// The body is fetched by uid, decompressed, for the rows that become frames — the diff
// keys on (uid, resource_version) and never loads one.
func TestObjectBodyReturnsTheDecompressedBody(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	s := newTestStore(t)
	require.NoError(t, s.SyncKinds(ctx, []KindRow{podRow}, true, 7))
	require.NoError(t, s.ApplyChange(ctx, podKind, watch.Added, pod("uid-1", "api-0", "42")))

	body, ok, err := s.ObjectBody(ctx, "uid-1")

	require.NoError(t, err)
	require.True(t, ok)
	assert.Contains(t, string(body), `"uid":"uid-1"`)
}

// The row can be deleted between the diff read that named it and the fetch, and the next
// resync's Deleted frame is the real answer — so a body that is gone is reported, never
// raised as a read failure.
func TestObjectBodyReportsARowThatIsGone(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)

	body, ok, err := s.ObjectBody(context.Background(), "uid-gone")

	require.NoError(t, err)
	assert.False(t, ok)
	assert.Nil(t, body)
}

// Ordered by (namespace, name), which is what the table renders and what the index serves.
func TestObjectsAreOrderedByNamespaceAndName(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	s := newTestStore(t)
	require.NoError(t, s.SyncKinds(ctx, []KindRow{podRow}, true, 7))
	for _, name := range []string{"api-2", "api-0", "api-1"} {
		require.NoError(t, s.ApplyChange(ctx, podKind, watch.Added, pod("uid-"+name, name, "42")))
	}

	got, _, err := s.ObjectsWithCursor(ctx, "v1", "pods")
	require.NoError(t, err)

	require.Len(t, got, 3)
	assert.Equal(t, []string{"api-0", "api-1", "api-2"}, []string{got[0].Name, got[1].Name, got[2].Name})
}

// dropTable removes one table from under the prepared statements, so a read that names it
// fails the way an unreadable file does. A prepared statement is re-prepared against the
// live schema, which is what makes this reach the reader pool too.
func dropTable(t *testing.T, s *Store, table string) {
	t.Helper()
	_, err := db(t, s).ExecContext(context.Background(), `DROP TABLE `+table)
	require.NoError(t, err)
}

// A read that cannot reach its table reports the fault. Answering empty would say the
// cluster serves nothing, and the nav would empty over a storage problem.
func TestKindsReportsATableItCannotRead(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	dropTable(t, s, "kind_catalog")

	_, err := s.Kinds(context.Background())

	assert.ErrorContains(t, err, "read kinds")
}

// The rows and the fingerprint come out of one transaction, so a fingerprint that cannot
// be read fails the pair rather than handing back rows under a missing one — which a
// caller would take for a wiped table.
func TestKindsWithFingerprintReportsAnUnreadableFingerprint(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	dropTable(t, s, "cluster_meta")

	_, _, _, err := s.KindsWithFingerprint(context.Background())

	assert.ErrorContains(t, err, "fingerprint")
}

// A row the reader cannot make sense of is a fault, not a kind to skip: a catalog read
// short by one kind is a nav missing an entry with nothing to say why.
func TestKindsReportsARowItCannotScan(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	s := newTestStore(t)
	_, err := db(t, s).ExecContext(ctx,
		`INSERT INTO kind_catalog(api_version, kind, resource, scope, is_crd)
		 VALUES ('v1', 'Pod', 'pods', 'Namespaced', 2)`)
	require.NoError(t, err)

	_, err = s.Kinds(ctx)

	assert.ErrorContains(t, err, "read kinds")
}

// A body that will not decompress is reported rather than served: handing a caller half a
// document, or the compressed bytes, would put nonsense in front of the user.
func TestObjectBodyReportsAStoredBodyItCannotDecompress(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	s := newTestStore(t)
	require.NoError(t, s.ApplyChange(ctx, podKind, watch.Added, pod("uid-1", "api-0", "1")))
	// A zlib stream's own leading byte, so the codec commits to inflating it and fails.
	_, err := db(t, s).ExecContext(ctx,
		`UPDATE objects SET raw_json = x'7801ffff' WHERE uid = 'uid-1'`)
	require.NoError(t, err)

	_, _, err = s.ObjectBody(ctx, "uid-1")

	assert.ErrorContains(t, err, "read object body")
}

// A snapshot is read at a position: the rows and the head come out of one transaction, so
// the cursor the reader keeps covers exactly the rows it was handed. Two reads instead
// would let a write land between them, and the cursor would claim rows nobody sent. The
// cursor also carries the Kind the rows were read under, which is the other half of "what
// is prev current with": a plural can be remapped onto a renamed Kind.
func TestASnapshotIsReadAtACursor(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	s := newTestStore(t)
	require.NoError(t, s.SyncKinds(ctx, []KindRow{podRow}, true, 7))
	require.NoError(t, s.ApplyChange(ctx, podKind, watch.Added, pod("uid-1", "api-0", "42")))
	require.NoError(t, s.ApplyChange(ctx, eventsKind, watch.Added, firing("ev-1", "10", 1)))

	objects, objectsAt, err := s.ObjectsWithCursor(ctx, "v1", "pods")
	require.NoError(t, err)
	events, eventsAt, err := s.EventsWithCursor(ctx)
	require.NoError(t, err)

	require.Len(t, objects, 1)
	require.Len(t, events, 1)
	at := storeHead(t, s)
	assert.Equal(t, Cursor{Seq: at, Kind: "Pod"}, objectsAt)
	assert.Equal(t, Cursor{Seq: at, Kind: "Event"}, eventsAt)
	assert.Equal(t, at, eventSeq(t, s, "ev-1"), "the head is the last write's position")
}

// Every read serves a watch that holds the client's table, so each one has to report a
// fault rather than answer empty: empty is a collection the cache no longer holds, which
// the watch turns into a Deleted per row on screen.
func TestObjectsWithCursorReportsAReadItCannotMake(t *testing.T) {
	t.Parallel()

	breaks := map[string]func(*testing.T, *Store){
		"the kind": func(t *testing.T, s *Store) { dropTable(t, s, "kind_catalog") },
		"the rows": func(t *testing.T, s *Store) { dropTable(t, s, "objects") },
		// A NULL uid is a row with nothing for the watch to key on.
		"a row it cannot scan": func(t *testing.T, s *Store) {
			swapTable(t, s, "objects", `SELECT NULL AS uid, 'v1' AS api_version, 'Pod' AS kind,
				'prod' AS namespace, 'api-0' AS name, '42' AS resource_version, 0 AS created_at`)
		},
	}
	for name, brk := range breaks {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			s := newTestStore(t)
			require.NoError(t, s.SyncKinds(ctx, []KindRow{podRow}, true, 7))
			brk(t, s)

			_, _, err := s.ObjectsWithCursor(ctx, "v1", "pods")

			assert.Error(t, err)
		})
	}
}

func TestEventsWithCursorReportsAReadItCannotMake(t *testing.T) {
	t.Parallel()

	breaks := map[string]func(*testing.T, *Store){
		"the rows": func(t *testing.T, s *Store) { dropTable(t, s, "events") },
		"a row it cannot scan": func(t *testing.T, s *Store) {
			swapTable(t, s, "events", `SELECT NULL AS uid, 'Normal' AS type, 'Started' AS reason,
				'' AS message, 1 AS count, 0 AS first_seen, 0 AS last_seen,
				'Pod' AS involved_kind, 'prod' AS involved_ns, 'api-0' AS involved_name`)
		},
	}
	for name, brk := range breaks {
		t.Run(name, func(t *testing.T) {
			s := newTestStore(t)
			brk(t, s)

			_, _, err := s.EventsWithCursor(context.Background())

			assert.Error(t, err)
		})
	}
}

// A read that fails partway through its rows reports it. rows.Err is the only place that
// failure surfaces — the loop simply stops, so a missing check would serve however many
// rows arrived before the fault as if they were the whole catalog.
func TestKindsReportsAReadThatFailsMidway(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	// The first row reads; the second raises as it is computed, which is a fault the loop
	// itself cannot see.
	swapTable(t, s, "kind_catalog", `SELECT 'v1' AS api_version, 'Pod' AS kind,
		'pods' AS resource, 'Namespaced' AS scope, 0 AS is_crd,
		NULL AS schema_json, NULL AS printer_columns
		UNION ALL SELECT 'v1', json_extract('{', '$.a'), 'x', 'Namespaced', 0, NULL, NULL`)

	_, _, _, err := s.KindsWithFingerprint(context.Background())

	assert.Error(t, err)
}
