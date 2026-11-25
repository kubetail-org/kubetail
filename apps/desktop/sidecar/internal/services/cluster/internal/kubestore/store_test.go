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
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/watch"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/testutil"
)

var (
	podKind    = Kind{APIVersion: "v1", Kind: "Pod", Resource: "pods"}
	eventsKind = Kind{APIVersion: "v1", Kind: "Event", Resource: "events"}
)

// newTestStore opens one cache's store for the test's life.
func newTestStore(t *testing.T) *Store {
	t.Helper()
	m := NewManager(t.TempDir(), Retention{})
	t.Cleanup(func() { require.NoError(t, m.Close()) })
	store, err := m.OpenOrCreate(1)
	require.NoError(t, err)
	t.Cleanup(store.Release)
	return store
}

// pod builds a Pod body with the given uid, name and resourceVersion.
func pod(uid, name, rv string) *unstructured.Unstructured {
	return obj(map[string]any{
		"apiVersion": "v1", "kind": "Pod",
		"metadata": map[string]any{
			"uid": uid, "name": name, "namespace": "prod", "resourceVersion": rv,
			"labels": map[string]any{"app": "api"},
		},
		"status": map[string]any{"phase": "Running"},
	})
}

// event builds an Event body with the given uid and last-seen time.
func event(uid, lastSeen string) *unstructured.Unstructured {
	return obj(map[string]any{
		"apiVersion": "v1", "kind": "Event",
		"metadata":       map[string]any{"uid": uid},
		"involvedObject": map[string]any{"uid": "pod-1", "kind": "Pod", "name": "api-0"},
		"reason":         "BackOff", "message": "restarting",
		"lastTimestamp": lastSeen,
	})
}

// beginReplace opens a relist session for the test.
func beginReplace(t *testing.T, s *Store, k Kind) *ReplaceSession {
	t.Helper()
	session, err := s.BeginReplace(k)
	require.NoError(t, err)
	return session
}

// subscribe opens the store's change notifications for the test.
func subscribe(t *testing.T, s *Store) ChangeNotifications {
	t.Helper()
	sub, err := s.ListenForChanges()
	require.NoError(t, err)
	return sub
}

// recvKey takes the next ping's key, bounded by the failsafe so a bus that never
// fires fails the test instead of hanging it.
func recvKey(t *testing.T, sub ChangeNotifications) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), testutil.Timeout)
	defer cancel()
	ev, err := sub.RecvContext(ctx)
	require.NoError(t, err)
	return ev.Key
}

// db is the open database behind a claim — the white-box reach these tests assert
// through.
func db(t *testing.T, s *Store) *sql.DB {
	t.Helper()
	f, err := s.file()
	require.NoError(t, err)
	return f.db.DB
}

// closedStore is a claim whose cache went away while it was held, so every read and write
// on it answers ErrClosed — the one way a method's own file() branch is reached.
func closedStore(t *testing.T) *Store {
	t.Helper()
	m := NewManager(t.TempDir(), Retention{})
	t.Cleanup(func() { require.NoError(t, m.Close()) })
	store, err := m.OpenOrCreate(1)
	require.NoError(t, err)
	t.Cleanup(store.Release)
	require.NoError(t, m.Remove(1))
	return store
}

// swapTable puts a view of the same name in a table's place, so a read still prepares and
// then scans a value the row type cannot hold. dropTable fails the query; this fails the
// scan, which is the other half of what a read has to report.
func swapTable(t *testing.T, s *Store, table, query string) {
	t.Helper()
	dropTable(t, s, table)
	_, err := db(t, s).ExecContext(context.Background(), `CREATE VIEW `+table+` AS `+query)
	require.NoError(t, err)
}

// failCommit arms a DEFERRED foreign-key violation on the next write of the given shape, so
// the statement succeeds and the COMMIT fails. Nothing a caller can pass makes SQLite fail
// that late, and a commit that fails after its statements landed is exactly what the write
// paths' own commit branches are for.
func failCommit(t *testing.T, s *Store, event, table string) {
	t.Helper()
	for _, ddl := range []string{
		`CREATE TABLE commit_parent(id INTEGER PRIMARY KEY)`,
		`CREATE TABLE commit_child(id INTEGER REFERENCES commit_parent(id) DEFERRABLE INITIALLY DEFERRED)`,
		`CREATE TRIGGER commit_boom AFTER ` + event + ` ON ` + table + ` BEGIN
			INSERT INTO commit_child VALUES (1);
		END`,
	} {
		_, err := db(t, s).ExecContext(context.Background(), ddl)
		require.NoError(t, err)
	}
}

// countRows is one scalar SQL read.
func countRows(t *testing.T, s *Store, q string, args ...any) int {
	t.Helper()
	var n int
	require.NoError(t, db(t, s).QueryRowContext(context.Background(), q, args...).Scan(&n))
	return n
}

// A delta lands the row, its edges, and the position that would replay it — in one
// transaction, so no restart resumes from a position the rows do not back.
// The last release closes the file under the manager's lock, so a Clear or Remove never
// finds the entry gone while its file is still open — Windows refuses to unlink one.
func TestTheLastReleaseClosesTheFileUnderTheLock(t *testing.T) {
	t.Parallel()

	var m *Manager
	underLock := make(chan bool, 1)
	m = newManagerWithOptions(t.TempDir(), withCloseFile(func(f *file) error {
		held := !m.mu.TryLock()
		if !held {
			m.mu.Unlock()
		}
		underLock <- held
		return f.close()
	}))
	t.Cleanup(func() { require.NoError(t, m.Close()) })

	store, err := m.OpenOrCreate(1)
	require.NoError(t, err)
	store.Release()

	assert.True(t, testutil.Recv(t, underLock, "the release to close the file"))
}

// A remove waits for an operation on the file to end before it deletes it: sql.DB.Close
// leaves a connection in use open, and Windows refuses to delete a file one holds.
func TestARemoveWaitsForAnOperationInFlight(t *testing.T) {
	t.Parallel()

	m := NewManager(t.TempDir(), Retention{})
	t.Cleanup(func() { require.NoError(t, m.Close()) })
	s, err := m.OpenOrCreate(1)
	require.NoError(t, err)
	t.Cleanup(s.Release)
	_, done, err := s.use()
	require.NoError(t, err)

	removed := make(chan error, 1)
	go func() { removed <- m.Remove(1) }()

	// A negative assertion: nothing marks the moment a remove would wrongly return, so it
	// is given a window, and fails the instant the remove returns inside it.
	testutil.NoRecv(t, removed, 100*time.Millisecond, "the remove, while an operation is in flight")
	done()

	require.NoError(t, testutil.Recv(t, removed, "the remove"))
	assert.NoFileExists(t, m.path(1))
}

// A replace session holds its file across pages, so one outliving a clear answers ErrClosed
// rather than writing into a file the clear closed.
func TestAReplaceSessionAfterAClearIsClosed(t *testing.T) {
	t.Parallel()

	m := NewManager(t.TempDir(), Retention{})
	t.Cleanup(func() { require.NoError(t, m.Close()) })
	s, err := m.OpenOrCreate(1)
	require.NoError(t, err)
	t.Cleanup(s.Release)
	r, err := s.BeginReplace(podKind)
	require.NoError(t, err)

	require.NoError(t, m.Clear(1))

	ctx := context.Background()
	require.ErrorIs(t, r.WritePage(ctx, []*unstructured.Unstructured{pod("uid-1", "one", "1")}), ErrClosed)
	_, err = r.Commit(ctx, "1")
	require.ErrorIs(t, err, ErrClosed)
}

func TestApplyChangeWritesTheObjectAndAdvancesTheCookie(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	s := newTestStore(t)

	require.NoError(t, s.ApplyChange(ctx, podKind, watch.Added, pod("uid-1", "api-0", "42")))

	assert.Equal(t, 1, countRows(t, s, `SELECT COUNT(*) FROM objects WHERE uid='uid-1'`))
	assert.Equal(t, 1, countRows(t, s, `SELECT COUNT(*) FROM labels WHERE uid='uid-1'`))
	assert.Equal(t, 1, countRows(t, s, `SELECT count FROM kind_counts WHERE api_version='v1' AND kind='Pod'`))

	rv, ok, err := s.Cookie(ctx, "v1", "pods")
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, "42", rv)
}

// A body the projection cannot key is skipped rather than failing the delta, and the cookie
// advances over it all the same: the server replays from that position, so a run that failed
// here would be handed the same body every time it resumed.
func TestApplyChangeSkipsAnUnprojectableBodyAndMovesPast(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	s := newTestStore(t)

	require.NoError(t, s.ApplyChange(ctx, podKind, watch.Added, pod("", "api-0", "42")))

	assert.Zero(t, countRows(t, s, `SELECT COUNT(*) FROM objects`), "nothing keyable to write")

	rv, ok, err := s.Cookie(ctx, "v1", "pods")
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, "42", rv, "the position moves, or the next resume replays the same body")
}

// The stored body is the sanitized one, and it round-trips through the codec — which is
// what lets a read serve raw_json verbatim.
func TestApplyChangeStoresTheBodyCompressed(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	s := newTestStore(t)

	require.NoError(t, s.ApplyChange(ctx, podKind, watch.Added, pod("uid-1", "api-0", "42")))

	var blob []byte
	require.NoError(t, db(t, s).QueryRowContext(ctx, `SELECT raw_json FROM objects WHERE uid='uid-1'`).Scan(&blob))
	assert.Equal(t, byte(0x78), blob[0])
	body, err := decompressRaw(blob)
	require.NoError(t, err)
	assert.Contains(t, string(body), `"api-0"`)
}

func TestApplyChangeDeletedRemovesTheRowAndItsEdges(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	s := newTestStore(t)
	require.NoError(t, s.ApplyChange(ctx, podKind, watch.Added, pod("uid-1", "api-0", "42")))

	require.NoError(t, s.ApplyChange(ctx, podKind, watch.Deleted, pod("uid-1", "api-0", "43")))

	assert.Zero(t, countRows(t, s, `SELECT COUNT(*) FROM objects WHERE uid='uid-1'`))
	assert.Zero(t, countRows(t, s, `SELECT COUNT(*) FROM labels WHERE uid='uid-1'`))
	// The kind's tally rests at 0 rather than vanishing: an advertised but empty kind
	// must read 0, and only a clear retires the row.
	assert.Zero(t, countRows(t, s, `SELECT count FROM kind_counts WHERE api_version='v1' AND kind='Pod'`))
}

// The Event kind is written to its own table: routing it to objects would leave the
// events watch, its FTS index, and the event count with nothing.
func TestApplyChangeRoutesCoreEventsToTheEventsTable(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	s := newTestStore(t)

	require.NoError(t, s.ApplyChange(ctx, eventsKind, watch.Added, event("ev-1", "2026-08-01T00:00:00Z")))

	assert.Equal(t, 1, countRows(t, s, `SELECT COUNT(*) FROM events WHERE uid='ev-1'`))
	assert.Zero(t, countRows(t, s, `SELECT COUNT(*) FROM objects`))
	assert.Equal(t, 1, countRows(t, s, `SELECT count FROM kind_counts WHERE api_version='v1' AND kind='Event'`))
}

// A relist reconciles by mark and sweep: what the pass rewrote survives, what it did not
// is gone — which is where an object deleted while we were disconnected finally leaves.
func TestReplacePrunesWhatTheListDidNotCarry(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	s := newTestStore(t)
	require.NoError(t, s.ApplyChange(ctx, podKind, watch.Added, pod("gone", "old", "1")))
	require.NoError(t, s.ApplyChange(ctx, podKind, watch.Added, pod("kept", "api-0", "1")))

	session := beginReplace(t, s, podKind)
	require.NoError(t, session.WritePage(ctx, []*unstructured.Unstructured{pod("kept", "api-0", "9")}))
	pruned, err := session.Commit(ctx, "100")
	require.NoError(t, err)

	assert.Equal(t, 1, pruned)
	assert.Zero(t, countRows(t, s, `SELECT COUNT(*) FROM objects WHERE uid='gone'`))
	assert.Equal(t, 1, countRows(t, s, `SELECT COUNT(*) FROM objects WHERE uid='kept'`))
	rv, _, err := s.Cookie(ctx, "v1", "pods")
	require.NoError(t, err)
	assert.Equal(t, "100", rv)
}

// A relist prunes only its own kind: every other kind's rows belong to another worker.
func TestReplacePrunesOnlyItsOwnKind(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	s := newTestStore(t)
	deployments := Kind{APIVersion: "apps/v1", Kind: "Deployment", Resource: "deployments"}
	dep := obj(map[string]any{
		"apiVersion": "apps/v1", "kind": "Deployment",
		"metadata": map[string]any{"uid": "dep-1", "name": "api", "resourceVersion": "1"},
	})
	require.NoError(t, s.ApplyChange(ctx, deployments, watch.Added, dep))

	session := beginReplace(t, s, podKind)
	require.NoError(t, session.WritePage(ctx, []*unstructured.Unstructured{pod("kept", "api-0", "9")}))
	_, err := session.Commit(ctx, "100")
	require.NoError(t, err)

	assert.Equal(t, 1, countRows(t, s, `SELECT COUNT(*) FROM objects WHERE uid='dep-1'`))
}

// A pass that wrote rows and then failed must leave no cookie: the rows on disk are
// half a collection, and resuming a watch from that position would never reconcile
// them. Only Commit writes one back.
func TestReplaceClearsTheCookieOnItsFirstPage(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	s := newTestStore(t)
	require.NoError(t, s.SetCookie(ctx, "v1", "pods", "old"))

	session := beginReplace(t, s, podKind)
	require.NoError(t, session.WritePage(ctx, []*unstructured.Unstructured{pod("uid-1", "api-0", "9")}))

	_, ok, err := s.Cookie(ctx, "v1", "pods")
	require.NoError(t, err)
	assert.False(t, ok)
}

// A body that will not project is skipped rather than failing the page: one malformed
// object must not stop a collection from syncing.
func TestReplaceSkipsAnUnprojectableBody(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	s := newTestStore(t)

	session := beginReplace(t, s, podKind)
	err := session.WritePage(ctx, []*unstructured.Unstructured{
		obj(map[string]any{"apiVersion": "v1", "kind": "Pod"}), // no uid
		pod("uid-1", "api-0", "9"),
	})

	require.NoError(t, err)
	assert.Equal(t, 1, countRows(t, s, `SELECT COUNT(*) FROM objects`))
}

func TestReplaceLandsEventsInTheEventsTable(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	s := newTestStore(t)
	require.NoError(t, s.ApplyChange(ctx, eventsKind, watch.Added, event("gone", "2026-08-01T00:00:00Z")))

	session := beginReplace(t, s, eventsKind)
	require.NoError(t, session.WritePage(ctx, []*unstructured.Unstructured{event("ev-1", "2026-08-01T00:01:00Z")}))
	pruned, err := session.Commit(ctx, "100")

	require.NoError(t, err)
	assert.Equal(t, 1, pruned)
	assert.Equal(t, 1, countRows(t, s, `SELECT COUNT(*) FROM events WHERE uid='ev-1'`))
	assert.Zero(t, countRows(t, s, `SELECT COUNT(*) FROM events WHERE uid='gone'`))
}

// The ping is what a read re-reads on. It is keyed per kind so an unrelated kind's
// write costs a kind watch nothing.
func TestAnObjectWriteNotifiesItsOwnKey(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	s := newTestStore(t)
	sub := subscribe(t, s)
	defer sub.Close()

	require.NoError(t, s.ApplyChange(ctx, podKind, watch.Added, pod("uid-1", "api-0", "42")))

	assert.Equal(t, ObjectsKey("v1", "pods"), recvKey(t, sub))
}

// Events have their own bus key, so an event-storm cluster does not wake every object
// watch in the cache.
func TestAnEventWriteNotifiesTheEventsKey(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	s := newTestStore(t)
	sub := subscribe(t, s)
	defer sub.Close()

	require.NoError(t, s.ApplyChange(ctx, eventsKind, watch.Added, event("ev-1", "2026-08-01T00:00:00Z")))

	assert.Equal(t, EventsKey, recvKey(t, sub))
}

// Aging out is not a write, so nothing would emit the Deleted a client needs for free.
// The pruner is what keeps the window a window — and it pings, so the read sees it.
func TestRollupCountsObjectsAndKindsExcludingEvents(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	s := newTestStore(t)
	require.NoError(t, s.ApplyChange(ctx, podKind, watch.Added, pod("uid-1", "api-0", "1")))
	require.NoError(t, s.ApplyChange(ctx, podKind, watch.Added, pod("uid-2", "api-1", "1")))
	require.NoError(t, s.ApplyChange(ctx, eventsKind, watch.Added, event("ev-1", "2026-08-01T00:00:00Z")))

	rollup, err := s.Counts(ctx)

	require.NoError(t, err)
	assert.Equal(t, 2, rollup.ObjectCount)
	assert.Equal(t, 1, rollup.KindCount)
}

// A kind emptied by deletes reads 0 rather than counting as a kind the cache holds.
func TestRollupIgnoresAKindWithNoRowsLeft(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	s := newTestStore(t)
	require.NoError(t, s.ApplyChange(ctx, podKind, watch.Added, pod("uid-1", "api-0", "1")))
	require.NoError(t, s.ApplyChange(ctx, podKind, watch.Deleted, pod("uid-1", "api-0", "2")))

	rollup, err := s.Counts(ctx)

	require.NoError(t, err)
	assert.Zero(t, rollup.ObjectCount)
	assert.Zero(t, rollup.KindCount)
}

// CountKind is the worker's own tally — the number its observation carries.
func TestCountKindReadsOneKindsTally(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	s := newTestStore(t)
	require.NoError(t, s.ApplyChange(ctx, podKind, watch.Added, pod("uid-1", "api-0", "1")))
	require.NoError(t, s.ApplyChange(ctx, eventsKind, watch.Added, event("ev-1", "2026-08-01T00:00:00Z")))

	pods, err := s.CountKind(ctx, podKind)
	require.NoError(t, err)
	events, err := s.CountKind(ctx, eventsKind)
	require.NoError(t, err)

	assert.Equal(t, 1, pods)
	assert.Equal(t, 1, events)
}

func TestCountKindOfAnUnwrittenKindIsZero(t *testing.T) {
	t.Parallel()

	n, err := newTestStore(t).CountKind(context.Background(), podKind)

	require.NoError(t, err)
	assert.Zero(t, n)
}

// Closing the store ends every subscriber — which is how a Clear or a shutdown reaches
// a live watch instead of leaving it waiting on a store that is gone.
func TestClosingTheStoreEndsSubscribers(t *testing.T) {
	t.Parallel()

	m := NewManager(t.TempDir(), Retention{})
	store, err := m.OpenOrCreate(1)
	require.NoError(t, err)
	sub, err := store.ListenForChanges()
	require.NoError(t, err)
	defer sub.Close()

	store.Release()

	ctx, cancel := context.WithTimeout(context.Background(), testutil.Timeout)
	defer cancel()
	_, err = sub.RecvContext(ctx)
	assert.Error(t, err)
}

func TestCookieRoundtripIsPerKind(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	m := NewManager(dir, Retention{})
	t.Cleanup(func() { require.NoError(t, m.Close()) })

	store, err := m.OpenOrCreate(1)
	require.NoError(t, err)
	defer store.Release()

	ctx := context.Background()
	require.NoError(t, store.SetCookie(ctx, "apps/v1", "deployments", "100"))
	require.NoError(t, store.SetCookie(ctx, "v1", "pods", "200"))

	v, ok, err := store.Cookie(ctx, "apps/v1", "deployments")
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, "100", v)

	v, ok, err = store.Cookie(ctx, "v1", "pods")
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, "200", v)
}

// ListedKinds is the catalog kinds with a cookie: not a catalog kind without one, nor
// a cookie the catalog lacks, and a cleared cookie leaves the list.
func TestListedKindsAreTheCatalogKindsWithACookie(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := newTestStore(t)
	require.NoError(t, store.SyncKinds(ctx, []KindRow{
		{APIVersion: "v1", Kind: "Pod", Resource: "pods", Scope: ScopeNamespaced},
		{APIVersion: "apps/v1", Kind: "Deployment", Resource: "deployments", Scope: ScopeNamespaced},
	}, true, 7))
	require.NoError(t, store.SetCookie(ctx, "v1", "pods", "20"))
	require.NoError(t, store.SetCookie(ctx, "apps/v1", "deployments", "30"))
	require.NoError(t, store.SetCookie(ctx, "batch/v1", "jobs", "40"))

	listed, err := store.ListedKinds(ctx)
	require.NoError(t, err)
	assert.Equal(t, []Kind{{"apps/v1", "Deployment", "deployments"}, {"v1", "Pod", "pods"}}, listed)

	require.NoError(t, store.ClearKind(ctx, Kind{APIVersion: "v1", Kind: "Pod", Resource: "pods"}))
	listed, err = store.ListedKinds(ctx)
	require.NoError(t, err)
	assert.Equal(t, []Kind{{"apps/v1", "Deployment", "deployments"}}, listed)
}

func TestListedKindsAfterACloseIsClosed(t *testing.T) {
	t.Parallel()

	_, err := closedStore(t).ListedKinds(context.Background())
	require.ErrorIs(t, err, ErrClosed)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = newTestStore(t).ListedKinds(ctx)
	require.ErrorIs(t, err, context.Canceled)
}

func TestStoreClearKindRemovesOnlyThatKindsCookie(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	m := NewManager(dir, Retention{})
	t.Cleanup(func() { require.NoError(t, m.Close()) })

	ctx := context.Background()
	store, err := m.OpenOrCreate(1)
	require.NoError(t, err)
	defer store.Release()

	require.NoError(t, store.SetCookie(ctx, "v1", "pods", "1"))
	require.NoError(t, store.SetCookie(ctx, "apps/v1", "deployments", "2"))

	require.NoError(t, store.ClearKind(ctx, podKind))

	_, ok, err := store.Cookie(ctx, "v1", "pods")
	require.NoError(t, err)
	require.False(t, ok)

	v, ok, err := store.Cookie(ctx, "apps/v1", "deployments")
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, "2", v)
}

func TestStoreClearKindOnEmptyStoreSucceeds(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	m := NewManager(dir, Retention{})
	t.Cleanup(func() { require.NoError(t, m.Close()) })

	store, err := m.OpenOrCreate(1)
	require.NoError(t, err)
	defer store.Release()

	require.NoError(t, store.ClearKind(context.Background(), podKind))
}

// ClearKind deletes the kind's objects and every row hanging off them — the schema
// has no cascading foreign keys — and touches no other kind.
func TestStoreClearKindDeletesObjectsAndTheirDependentRows(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	m := NewManager(dir, Retention{})
	t.Cleanup(func() { require.NoError(t, m.Close()) })

	ctx := context.Background()
	store, err := m.OpenOrCreate(1)
	require.NoError(t, err)
	defer store.Release()

	db := db(t, store)
	for _, kind := range []struct{ apiVersion, kind, resource, uid string }{
		{"v1", "Pod", "pods", "uid-pod"},
		{"apps/v1", "Deployment", "deployments", "uid-dep"},
	} {
		_, err := db.ExecContext(ctx,
			`INSERT INTO kind_catalog (api_version, kind, resource, scope, is_crd) VALUES (?, ?, ?, 'Namespaced', 0)`,
			kind.apiVersion, kind.kind, kind.resource)
		require.NoError(t, err)
		_, err = db.ExecContext(ctx,
			`INSERT INTO objects (uid, api_version, kind, name, resource_version, created_at, updated_at, write_seq, changed_at, raw_json)
			 VALUES (?, ?, ?, 'x', '1', 0, 0, 1, 0, x'7b7d')`,
			kind.uid, kind.apiVersion, kind.kind)
		require.NoError(t, err)
		_, err = db.ExecContext(ctx, `INSERT INTO owner_refs (child_uid, owner_uid) VALUES (?, 'owner')`, kind.uid)
		require.NoError(t, err)
		_, err = db.ExecContext(ctx, `INSERT INTO labels (uid, key, value) VALUES (?, 'app', 'x')`, kind.uid)
		require.NoError(t, err)
		_, err = db.ExecContext(ctx, `INSERT INTO status_history (uid, at, summary) VALUES (?, 0, 'Running')`, kind.uid)
		require.NoError(t, err)
	}

	require.NoError(t, store.ClearKind(ctx, podKind))

	for _, q := range []string{
		`SELECT COUNT(*) FROM objects WHERE uid = 'uid-pod'`,
		`SELECT COUNT(*) FROM owner_refs WHERE child_uid = 'uid-pod'`,
		`SELECT COUNT(*) FROM labels WHERE uid = 'uid-pod'`,
		`SELECT COUNT(*) FROM status_history WHERE uid = 'uid-pod'`,
	} {
		var n int
		require.NoError(t, db.QueryRowContext(ctx, q).Scan(&n))
		assert.Zerof(t, n, "rows left behind by: %s", q)
	}

	// The other kind is untouched, dependent rows included.
	for _, q := range []string{
		`SELECT COUNT(*) FROM objects WHERE uid = 'uid-dep'`,
		`SELECT COUNT(*) FROM owner_refs WHERE child_uid = 'uid-dep'`,
		`SELECT COUNT(*) FROM labels WHERE uid = 'uid-dep'`,
		`SELECT COUNT(*) FROM status_history WHERE uid = 'uid-dep'`,
	} {
		var n int
		require.NoError(t, db.QueryRowContext(ctx, q).Scan(&n))
		assert.Equalf(t, 1, n, "row removed by another kind's clear: %s", q)
	}
}

// Event rows live in their own table, so clearing the Events kind has to empty it —
// deleting from objects would leave every cached event behind.
func TestStoreClearKindClearsTheEventsTable(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	m := NewManager(dir, Retention{})
	t.Cleanup(func() { require.NoError(t, m.Close()) })

	ctx := context.Background()
	store, err := m.OpenOrCreate(1)
	require.NoError(t, err)
	defer store.Release()

	db := db(t, store)
	_, err = db.ExecContext(ctx,
		`INSERT INTO kind_catalog (api_version, kind, resource, scope, is_crd) VALUES ('v1', 'Event', 'events', 'Namespaced', 0)`)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx,
		`INSERT INTO events (uid, reason, message, raw_json, updated_at, resource_version, write_seq)
		 VALUES ('uid-ev', 'Pulled', 'ok', x'7b7d', 0, '1', 1)`)
	require.NoError(t, err)

	require.NoError(t, store.ClearKind(ctx, eventsKind))

	var n int
	require.NoError(t, db.QueryRowContext(ctx, `SELECT COUNT(*) FROM events`).Scan(&n))
	assert.Zero(t, n, "cached events survived their kind's clear")
}

// A retained child keeps its edge into a cleared owner: the edge is the child's own
// ownerReference, and nothing would put it back — a re-synced owner kind writes owner
// rows, not its children's edges.
func TestStoreClearKindKeepsARetainedChildsEdgeIntoTheClearedKind(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	m := NewManager(dir, Retention{})
	t.Cleanup(func() { require.NoError(t, m.Close()) })

	ctx := context.Background()
	store, err := m.OpenOrCreate(1)
	require.NoError(t, err)
	defer store.Release()

	db := db(t, store)
	_, err = db.ExecContext(ctx,
		`INSERT INTO kind_catalog (api_version, kind, resource, scope, is_crd) VALUES ('apps/v1', 'Deployment', 'deployments', 'Namespaced', 0)`)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx,
		`INSERT INTO objects (uid, api_version, kind, name, resource_version, created_at, updated_at, write_seq, changed_at, raw_json)
		 VALUES ('uid-dep', 'apps/v1', 'Deployment', 'web', '1', 0, 0, 1, 0, x'7b7d')`)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx,
		`INSERT INTO objects (uid, api_version, kind, name, resource_version, created_at, updated_at, write_seq, changed_at, raw_json)
		 VALUES ('uid-pod', 'v1', 'Pod', 'web-1', '1', 0, 0, 1, 0, x'7b7d')`)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `INSERT INTO owner_refs (child_uid, owner_uid) VALUES ('uid-pod', 'uid-dep')`)
	require.NoError(t, err)

	require.NoError(t, store.ClearKind(ctx, Kind{APIVersion: "apps/v1", Kind: "Deployment", Resource: "deployments"}))

	var n int
	require.NoError(t, db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM owner_refs WHERE child_uid = 'uid-pod'`).Scan(&n))
	assert.Equal(t, 1, n, "the retained Pod's own ownerReference was deleted with its owner")

	// The edge resolves to no owner, which is what a traversal's join already answers
	// for an owner kind that is not mirrored.
	require.NoError(t, db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM owner_refs r JOIN objects o ON o.uid = r.owner_uid WHERE r.child_uid = 'uid-pod'`).Scan(&n))
	assert.Zero(t, n)
}

// Another group may serve a Kind called "Event"; its rows are ordinary objects, so
// clearing it must leave the cached core events alone.
func TestStoreClearKindLeavesEventsAloneForANonCoreEventKind(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	m := NewManager(dir, Retention{})
	t.Cleanup(func() { require.NoError(t, m.Close()) })

	ctx := context.Background()
	store, err := m.OpenOrCreate(1)
	require.NoError(t, err)
	defer store.Release()

	db := db(t, store)
	_, err = db.ExecContext(ctx,
		`INSERT INTO kind_catalog (api_version, kind, resource, scope, is_crd) VALUES ('example.com/v1', 'Event', 'events', 'Namespaced', 1)`)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx,
		`INSERT INTO objects (uid, api_version, kind, name, resource_version, created_at, updated_at, write_seq, changed_at, raw_json)
		 VALUES ('uid-crd', 'example.com/v1', 'Event', 'x', '1', 0, 0, 1, 0, x'7b7d')`)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx,
		`INSERT INTO events (uid, reason, message, raw_json, updated_at, resource_version, write_seq)
		 VALUES ('uid-ev', 'Pulled', 'ok', x'7b7d', 0, '1', 1)`)
	require.NoError(t, err)

	require.NoError(t, store.ClearKind(ctx, Kind{APIVersion: "example.com/v1", Kind: "Event", Resource: "events"}))

	var n int
	require.NoError(t, db.QueryRowContext(ctx, `SELECT COUNT(*) FROM events`).Scan(&n))
	assert.Equal(t, 1, n, "the CRD's clear wiped the cached core events")

	require.NoError(t, db.QueryRowContext(ctx, `SELECT COUNT(*) FROM objects WHERE uid = 'uid-crd'`).Scan(&n))
	assert.Zero(t, n, "the CRD's own rows survived its clear")
}

// The events table is not keyed by kind, so its clear does not hang off the catalog
// row: a retry after a partial failure has already dropped that row.
func TestStoreClearKindClearsEventsWithNoCatalogRow(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	m := NewManager(dir, Retention{})
	t.Cleanup(func() { require.NoError(t, m.Close()) })

	ctx := context.Background()
	store, err := m.OpenOrCreate(1)
	require.NoError(t, err)
	defer store.Release()

	db := db(t, store)
	_, err = db.ExecContext(ctx,
		`INSERT INTO events (uid, reason, message, raw_json, updated_at, resource_version, write_seq)
		 VALUES ('uid-ev', 'Pulled', 'ok', x'7b7d', 0, '1', 1)`)
	require.NoError(t, err)

	require.NoError(t, store.ClearKind(ctx, eventsKind))

	var n int
	require.NoError(t, db.QueryRowContext(ctx, `SELECT COUNT(*) FROM events`).Scan(&n))
	assert.Zero(t, n, "cached events survived a clear that found no catalog row")
}

// A claim outlives the file under it: the cache's teardown closes and deletes it while
// a worker is still holding one. Every method answers ErrClosed rather than reaching a
// closed database, which is what turns a torn-down cache into one failed attempt.
func TestAStoreWhoseFileIsGoneAnswersErrClosed(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	m := NewManager(t.TempDir(), Retention{})
	t.Cleanup(func() { require.NoError(t, m.Close()) })
	store, err := m.OpenOrCreate(1)
	require.NoError(t, err)
	defer store.Release()

	require.NoError(t, m.Remove(1))

	assert.ErrorIs(t, store.ApplyChange(ctx, podKind, watch.Added, pod("uid-1", "api-0", "1")), ErrClosed)
	assert.ErrorIs(t, store.SetCookie(ctx, "v1", "pods", "1"), ErrClosed)
	assert.ErrorIs(t, store.ClearKind(ctx, podKind), ErrClosed)
	_, err = store.CountKind(ctx, podKind)
	assert.ErrorIs(t, err, ErrClosed)
	_, err = store.Counts(ctx)
	assert.ErrorIs(t, err, ErrClosed)
	_, err = store.BeginReplace(podKind)
	assert.ErrorIs(t, err, ErrClosed)
	_, _, err = store.Cookie(ctx, "v1", "pods")
	assert.ErrorIs(t, err, ErrClosed)
	_, err = store.ListenForChanges()
	assert.ErrorIs(t, err, ErrClosed)
	assert.ErrorIs(t, store.SyncKinds(ctx, nil, true, 1), ErrClosed)
	_, err = store.Kinds(ctx)
	assert.ErrorIs(t, err, ErrClosed)
	_, _, err = store.EventsWithCursor(ctx)
	assert.ErrorIs(t, err, ErrClosed)
	_, _, err = store.ObjectsWithCursor(ctx, "v1", "pods")
	assert.ErrorIs(t, err, ErrClosed)
	_, _, err = store.ObjectBody(ctx, "uid-1")
	assert.ErrorIs(t, err, ErrClosed)
}

// The first page invalidates the position even when it carries nothing. A cookie means
// a completed LIST landed on disk, so a relist that has begun must not leave one
// standing: a pass that then fails would let the next start resume from it, skipping
// the reconcile the rows still need.
func TestReplaceClearsTheCookieOnAnEmptyFirstPage(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	s := newTestStore(t)
	require.NoError(t, s.SetCookie(ctx, "v1", "pods", "old"))

	session := beginReplace(t, s, podKind)
	require.NoError(t, session.WritePage(ctx, nil))

	_, ok, err := s.Cookie(ctx, "v1", "pods")
	require.NoError(t, err)
	assert.False(t, ok, "an in-flight relist left a position claiming the cache is caught up")
}

// A page carrying nothing is not a write: it must not ping the bus, or an empty relist
// would wake every reader of that kind for no change.
func TestAnEmptyPageDoesNotNotify(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	s := newTestStore(t)
	sub := subscribe(t, s)
	defer sub.Close()

	session := beginReplace(t, s, podKind)
	require.NoError(t, session.WritePage(ctx, nil))

	// A negative assertion needs a bounded window; a ping would already be pending.
	_, err := sub.TryRecv()
	assert.Error(t, err, "an empty page pinged the bus")
}

// A kind's rows are keyed by Kind, and the caller knows which one — a teardown must not
// depend on the catalog to find them. Nothing writes catalog rows on the sync path (the
// sweep owns that table), so a clear that resolved through it would leave every row, edge
// and count behind for a kind that stopped being synced.
func TestClearKindDeletesRowsWithNoCatalogRow(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	s := newTestStore(t)
	require.NoError(t, s.ApplyChange(ctx, podKind, watch.Added, pod("uid-1", "api-0", "42")))
	require.Zero(t, countRows(t, s, `SELECT COUNT(*) FROM kind_catalog`), "nothing on the sync path writes catalog rows")

	require.NoError(t, s.ClearKind(ctx, podKind))

	assert.Zero(t, countRows(t, s, `SELECT COUNT(*) FROM objects`))
	assert.Zero(t, countRows(t, s, `SELECT COUNT(*) FROM labels`))
	// The tally goes with the kind rather than resting at 0: nothing will name it again.
	assert.Zero(t, countRows(t, s, `SELECT COUNT(*) FROM kind_counts WHERE api_version='v1' AND kind='Pod'`))
	_, ok, err := s.Cookie(ctx, "v1", "pods")
	require.NoError(t, err)
	assert.False(t, ok)
}

// The catalog is what the CLUSTER serves, and clearing a kind's cache does not stop it
// being served — a user emptying one kind must not have it vanish from the nav until the
// next sweep. The row leaves through the sweep's prune alone.
func TestClearKindKeepsTheCatalogRow(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	s := newTestStore(t)
	require.NoError(t, s.SyncKinds(ctx, []KindRow{podRow}, true, 7))
	require.NoError(t, s.ApplyChange(ctx, podKind, watch.Added, pod("uid-1", "api-0", "42")))

	require.NoError(t, s.ClearKind(ctx, podKind))

	assert.Zero(t, countRows(t, s, `SELECT COUNT(*) FROM objects`))
	assert.Equal(t, []KindRow{podRow}, catalogRows(t, s))
}

// A subscription named for one kind must not wake on another's writes, or every object
// write in the cache re-reads every open watch — which is what the per-kind bus keys exist
// to prevent. conflate filters at enqueue, so the key belongs on the subscription.
func TestListenForChangesWithAKeyIgnoresOtherKeys(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	s := newTestStore(t)
	sub, err := s.ListenForChanges(ObjectsKey("v1", "pods"))
	require.NoError(t, err)
	defer sub.Close()

	require.NoError(t, s.ApplyChange(ctx, eventsKind, watch.Added, event("uid-ev", "2026-08-26T10:00:00Z")))
	_, err = sub.TryRecv()
	assert.Error(t, err, "an events write woke a pods subscription")

	require.NoError(t, s.ApplyChange(ctx, podKind, watch.Added, pod("uid-1", "api-0", "42")))
	assert.Equal(t, ObjectsKey("v1", "pods"), recvKey(t, sub))
}

// No keys is the whole feed, which is what the kinds watch needs: object writes move counts
// and event writes move the hardcoded ('v1','Event') count, and there is one hub.
func TestListenForChangesWithNoKeysTakesEveryKey(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	s := newTestStore(t)
	sub, err := s.ListenForChanges()
	require.NoError(t, err)
	defer sub.Close()

	require.NoError(t, s.ApplyChange(ctx, eventsKind, watch.Added, event("uid-ev", "2026-08-26T10:00:00Z")))
	assert.Equal(t, EventsKey, recvKey(t, sub))
}

// podOwnedBy is a Pod carrying one ownerReference, so the sweep has an edge to cascade.
func podOwnedBy(uid, name, rv, ownerUID string) *unstructured.Unstructured {
	return obj(map[string]any{
		"apiVersion": "v1", "kind": "Pod",
		"metadata": map[string]any{
			"uid": uid, "name": name, "namespace": "prod", "resourceVersion": rv,
			"labels":          map[string]any{"app": "api"},
			"ownerReferences": []any{map[string]any{"uid": ownerUID, "kind": "ReplicaSet", "name": "rs"}},
		},
		"status": map[string]any{"phase": "Running"},
	})
}

// The prune is the only path that removes an object without knowing its uid, so it is the
// only one that can leave a side table behind. owner_refs is checked in both directions: a
// swept object is a child of something and an owner of something, and its children outlive
// it, so an inbound edge left behind points at a uid that is gone.
func TestReplacePruneTakesEverySideTableRowWithIt(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	s := newTestStore(t)
	for _, uid := range []string{"gone-1", "gone-2", "gone-3"} {
		require.NoError(t, s.ApplyChange(ctx, podKind, watch.Added, podOwnedBy(uid, uid, "1", "rs-1")))
	}
	require.NoError(t, s.ApplyChange(ctx, podKind, watch.Added, podOwnedBy("kept", "kept", "1", "rs-1")))
	// Another kind, so the Pod relist keeps it — and it owns gone-1, which is the inbound
	// edge nothing else would clear.
	deployments := Kind{APIVersion: "apps/v1", Kind: "Deployment", Resource: "deployments"}
	require.NoError(t, s.ApplyChange(ctx, deployments, watch.Added, obj(map[string]any{
		"apiVersion": "apps/v1", "kind": "Deployment",
		"metadata": map[string]any{
			"uid": "dep-1", "name": "api", "resourceVersion": "1",
			"ownerReferences": []any{map[string]any{"uid": "gone-1", "kind": "Pod", "name": "gone-1"}},
		},
	})))
	require.Equal(t, 3, countRows(t, s,
		`SELECT COUNT(*) FROM status_history WHERE uid IN ('gone-1', 'gone-2', 'gone-3')`),
		"the fixture writes one per pod")

	session := beginReplace(t, s, podKind)
	require.NoError(t, session.WritePage(ctx, []*unstructured.Unstructured{podOwnedBy("kept", "kept", "9", "rs-1")}))
	pruned, err := session.Commit(ctx, "100")
	require.NoError(t, err)

	require.Equal(t, 3, pruned)
	const gone = `IN ('gone-1', 'gone-2', 'gone-3')`
	assert.Zero(t, countRows(t, s, `SELECT COUNT(*) FROM labels WHERE uid `+gone))
	assert.Zero(t, countRows(t, s, `SELECT COUNT(*) FROM owner_refs WHERE child_uid `+gone))
	assert.Zero(t, countRows(t, s, `SELECT COUNT(*) FROM owner_refs WHERE owner_uid `+gone),
		"the Deployment's edge into gone-1")
	assert.Zero(t, countRows(t, s, `SELECT COUNT(*) FROM status_history WHERE uid `+gone))
	assert.Equal(t, 1, countRows(t, s, `SELECT COUNT(*) FROM labels WHERE uid='kept'`))
	assert.Equal(t, 1, countRows(t, s, `SELECT COUNT(*) FROM owner_refs WHERE child_uid='kept'`))
}

// openFileOf is the open file behind a claim — the white-box reach a test that drives a
// file-level helper needs.
func openFileOf(t *testing.T, s *Store) *file {
	t.Helper()
	f, err := s.file()
	require.NoError(t, err)
	return f
}

// breakStorage closes the pools under an open claim, so every statement the store issues
// fails while the claim itself stays valid. It is what stands in for the storage faults —
// a file that goes unreadable under a running cache — that no test can produce on demand.
func breakStorage(t *testing.T, s *Store) {
	t.Helper()
	f := openFileOf(t, s)
	require.NoError(t, f.db.Close())
	require.NoError(t, f.readDB.Close())
}

// Every operation reports a storage fault rather than answering as though the cache were
// empty. The distinction is load-bearing above: an empty answer is a cluster that serves
// nothing, and a watch that took one for a broken file would blank a populated table.
func TestEveryOperationReportsAStorageFault(t *testing.T) {
	t.Parallel()

	ops := map[string]func(context.Context, *Store) error{
		"Cookie": func(ctx context.Context, s *Store) error {
			_, _, err := s.Cookie(ctx, "v1", "pods")
			return err
		},
		"SetCookie": func(ctx context.Context, s *Store) error {
			return s.SetCookie(ctx, "v1", "pods", "9")
		},
		"ApplyChange": func(ctx context.Context, s *Store) error {
			return s.ApplyChange(ctx, podKind, watch.Added, pod("uid-1", "api-0", "1"))
		},
		"ApplyChangeDeleted": func(ctx context.Context, s *Store) error {
			return s.ApplyChange(ctx, podKind, watch.Deleted, pod("uid-1", "api-0", "1"))
		},
		"CountKind": func(ctx context.Context, s *Store) error {
			_, err := s.CountKind(ctx, podKind)
			return err
		},
		"Counts": func(ctx context.Context, s *Store) error {
			_, err := s.Counts(ctx)
			return err
		},
		"ClearKind": func(ctx context.Context, s *Store) error {
			return s.ClearKind(ctx, podKind)
		},
		"ClearKindEvents": func(ctx context.Context, s *Store) error {
			return s.ClearKind(ctx, eventsKind)
		},
		"Kinds": func(ctx context.Context, s *Store) error {
			_, err := s.Kinds(ctx)
			return err
		},
		"Events": func(ctx context.Context, s *Store) error {
			_, _, err := s.EventsWithCursor(ctx)
			return err
		},
		"Objects": func(ctx context.Context, s *Store) error {
			_, _, err := s.ObjectsWithCursor(ctx, "v1", "pods")
			return err
		},
		"ObjectBody": func(ctx context.Context, s *Store) error {
			_, _, err := s.ObjectBody(ctx, "uid-1")
			return err
		},
		"SyncKinds": func(ctx context.Context, s *Store) error {
			return s.SyncKinds(ctx, []KindRow{{APIVersion: "v1", Kind: "Pod", Resource: "pods", Scope: "Namespaced"}}, true, 1)
		},
		"WritePage": func(ctx context.Context, s *Store) error {
			r, err := s.BeginReplace(podKind)
			require.NoError(t, err)
			return r.WritePage(ctx, []*unstructured.Unstructured{pod("uid-1", "api-0", "1")})
		},
		"WritePageEvents": func(ctx context.Context, s *Store) error {
			r, err := s.BeginReplace(eventsKind)
			require.NoError(t, err)
			return r.WritePage(ctx, []*unstructured.Unstructured{event("ev-1", "2026-08-01T00:00:00Z")})
		},
		"Commit": func(ctx context.Context, s *Store) error {
			r, err := s.BeginReplace(podKind)
			require.NoError(t, err)
			_, err = r.Commit(ctx, "9")
			return err
		},
		"CommitEvents": func(ctx context.Context, s *Store) error {
			r, err := s.BeginReplace(eventsKind)
			require.NoError(t, err)
			_, err = r.Commit(ctx, "9")
			return err
		},
	}

	for name, op := range ops {
		t.Run(name, func(t *testing.T) {
			s := newTestStore(t)
			breakStorage(t, s)

			assert.Error(t, op(context.Background(), s))
		})
	}
}

// failWrites makes one table reject one kind of statement, so a fault can be placed at a
// chosen step of a multi-statement transaction. A prepared statement is re-prepared
// against the live schema, so this reaches the statements already compiled at open.
func failWrites(t *testing.T, s *Store, table, op string) {
	t.Helper()
	_, err := db(t, s).ExecContext(context.Background(), fmt.Sprintf(
		`CREATE TRIGGER fail_%[1]s_%[2]s BEFORE %[2]s ON %[1]s
		 BEGIN SELECT RAISE(ABORT, 'injected'); END`, table, op))
	require.NoError(t, err)
}

// An object write touches five tables and a cookie. A failure at any one of them aborts
// the whole write — the row, its edges, its timeline and the position that would replay
// it go together or not at all, which is what stops a restart resuming from a position
// the rows do not back.
//
// The object is seeded first because the edge tables are cleared before they are
// rewritten, and a BEFORE DELETE trigger fires per row: an object with no edges yet would
// walk straight past the fault.
func TestAnObjectWriteIsAllOrNothing(t *testing.T) {
	t.Parallel()

	steps := []struct{ table, op string }{
		{"status_history", "INSERT"},
		{"objects", "INSERT"},
		{"owner_refs", "DELETE"},
		{"owner_refs", "INSERT"},
		{"labels", "DELETE"},
		{"labels", "INSERT"},
		{"cluster_meta", "INSERT"},
	}

	for _, step := range steps {
		t.Run(step.table+" "+step.op, func(t *testing.T) {
			ctx := context.Background()
			s := newTestStore(t)
			seeded := podOwnedBy("uid-1", "api-0", "1", "owner-1")
			require.NoError(t, s.ApplyChange(ctx, podKind, watch.Added, seeded))
			require.NoError(t, s.SetCookie(ctx, podKind.APIVersion, podKind.Resource, "1"))

			// A changed summary, so the second write reaches the timeline too.
			next := podOwnedBy("uid-1", "api-0", "2", "owner-2")
			require.NoError(t, unstructured.SetNestedField(next.Object, "Pending", "status", "phase"))
			next.SetLabels(map[string]string{"app": "web"})
			failWrites(t, s, step.table, step.op)

			require.Error(t, s.ApplyChange(ctx, podKind, watch.Modified, next))

			assert.Equal(t, 1, countRows(t, s,
				`SELECT COUNT(*) FROM objects WHERE resource_version = '1'`))
			assert.Equal(t, 1, countRows(t, s,
				`SELECT COUNT(*) FROM labels WHERE uid = 'uid-1' AND value = 'api'`))
			assert.Equal(t, 1, countRows(t, s,
				`SELECT COUNT(*) FROM owner_refs WHERE owner_uid = 'owner-1'`))
			rv, _, err := s.Cookie(ctx, podKind.APIVersion, podKind.Resource)
			require.NoError(t, err)
			assert.Equal(t, "1", rv, "the position must not advance over a write that failed")
		})
	}
}

// An event write is the same bargain on its own table.
func TestAnEventWriteIsAllOrNothing(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	s := newTestStore(t)
	failWrites(t, s, "events", "INSERT")

	require.Error(t, s.ApplyChange(ctx, eventsKind, watch.Added, event("ev-1", "2026-08-01T00:00:00Z")))

	assert.Zero(t, countRows(t, s, `SELECT COUNT(*) FROM events`))
}

// A delete walks the side tables before the row itself; a failure part-way must not leave
// the object half-removed, with edges pointing at a uid that is gone.
func TestADeleteIsAllOrNothing(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	s := newTestStore(t)
	u := podOwnedBy("uid-1", "api-0", "1", "owner-1")
	u.SetLabels(map[string]string{"app": "api"})
	require.NoError(t, s.ApplyChange(ctx, podKind, watch.Added, u))
	failWrites(t, s, "labels", "DELETE")

	require.Error(t, s.ApplyChange(ctx, podKind, watch.Deleted, u))

	assert.Equal(t, 1, countRows(t, s, `SELECT COUNT(*) FROM objects`))
	assert.Equal(t, 1, countRows(t, s, `SELECT COUNT(*) FROM labels`))
	assert.Equal(t, 1, countRows(t, s, `SELECT COUNT(*) FROM owner_refs`))
}

// A core Event is deleted from its own table, not from objects — the two are separate
// tables, and a delta that removed the wrong one would leave the event cached forever.
func TestApplyChangeDeletedRemovesACoreEventFromTheEventsTable(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	s := newTestStore(t)
	ev := event("ev-1", "2026-08-01T00:00:00Z")
	require.NoError(t, s.ApplyChange(ctx, eventsKind, watch.Added, ev))

	require.NoError(t, s.ApplyChange(ctx, eventsKind, watch.Deleted, ev))

	assert.Zero(t, countRows(t, s, `SELECT COUNT(*) FROM events`))
}

// A delete names its object by uid alone, so a body carrying none says nothing about what
// to remove. Reported rather than skipped: the row it meant to take stays behind.
func TestApplyChangeDeletedRefusesABodyWithNoUID(t *testing.T) {
	t.Parallel()

	u := pod("", "api-0", "1")

	err := newTestStore(t).ApplyChange(context.Background(), podKind, watch.Deleted, u)

	assert.ErrorContains(t, err, "empty UID")
}

// A nil body would panic on the projection, taking the worker's goroutine with it.
func TestApplyChangeRefusesAnEmptyBody(t *testing.T) {
	t.Parallel()

	err := newTestStore(t).ApplyChange(context.Background(), podKind, watch.Added, nil)

	assert.ErrorContains(t, err, "empty object")
}

// Bookmark and Error carry no row. Neither is a failure — the watch loop hands every
// event through, and only the three that name an object mean a write.
func TestApplyChangeIgnoresAnEventThatCarriesNoRow(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	s := newTestStore(t)

	require.NoError(t, s.ApplyChange(ctx, podKind, watch.Bookmark, pod("uid-1", "api-0", "1")))

	assert.Zero(t, countRows(t, s, `SELECT COUNT(*) FROM objects`))
}

// A relist page is one transaction: a failure part-way leaves the collection as the
// previous pass left it, so a half-written page is never what a reader sees.
func TestARelistPageIsAllOrNothing(t *testing.T) {
	t.Parallel()

	steps := []struct {
		table, op string
		kind      Kind
		item      *unstructured.Unstructured
	}{
		{"cluster_meta", "DELETE", podKind, pod("uid-2", "api-1", "1")},
		{"objects", "INSERT", podKind, pod("uid-2", "api-1", "1")},
		{"events", "INSERT", eventsKind, event("ev-2", "2026-08-01T00:00:00Z")},
	}

	for _, step := range steps {
		t.Run(step.table+" "+step.op, func(t *testing.T) {
			ctx := context.Background()
			s := newTestStore(t)
			// A cookie to clear, so the first page's DELETE has a row to fire on.
			require.NoError(t, s.SetCookie(ctx, step.kind.APIVersion, step.kind.Resource, "1"))
			failWrites(t, s, step.table, step.op)
			r := beginReplace(t, s, step.kind)

			require.Error(t, r.WritePage(ctx, []*unstructured.Unstructured{step.item}))

			assert.Zero(t, countRows(t, s, `SELECT COUNT(*) FROM objects`))
			assert.Zero(t, countRows(t, s, `SELECT COUNT(*) FROM events`))
			rv, _, err := s.Cookie(ctx, step.kind.APIVersion, step.kind.Resource)
			require.NoError(t, err)
			assert.Equal(t, "1", rv, "a failed first page must leave the cookie standing")
		})
	}
}

// The first page clears the cookie even carrying nothing, so a pass that then fails
// cannot be resumed from a position its rows no longer back. Later empty pages have
// nothing left to do.
func TestAnEmptyFirstPageClearsTheCookieAndLaterOnesDoNothing(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	s := newTestStore(t)
	require.NoError(t, s.SetCookie(ctx, podKind.APIVersion, podKind.Resource, "1"))
	r := beginReplace(t, s, podKind)

	require.NoError(t, r.WritePage(ctx, nil))
	_, ok, err := s.Cookie(ctx, podKind.APIVersion, podKind.Resource)
	require.NoError(t, err)
	assert.False(t, ok)

	// The second empty page returns before opening a transaction at all — which is what
	// this fault would otherwise abort.
	failWrites(t, s, "cluster_meta", "DELETE")
	assert.NoError(t, r.WritePage(ctx, nil))
}

// A relist's prune and the position it hands on are one transaction: a prune that fails
// must not leave the cookie claiming the collection is settled.
func TestARelistCommitIsAllOrNothing(t *testing.T) {
	t.Parallel()

	steps := []struct {
		table, op string
		kind      Kind
		item      *unstructured.Unstructured
		// rows is where the collection's own rows live, and must still hold them.
		rows string
	}{
		{"objects", "DELETE", podKind, pod("uid-1", "api-0", "1"), "objects"},
		{"labels", "DELETE", podKind, pod("uid-1", "api-0", "1"), "objects"},
		{"events", "DELETE", eventsKind, event("ev-1", "2026-08-01T00:00:00Z"), "events"},
		{"cluster_meta", "INSERT", podKind, pod("uid-1", "api-0", "1"), "objects"},
	}

	for _, step := range steps {
		t.Run(step.table+" "+step.op, func(t *testing.T) {
			ctx := context.Background()
			s := newTestStore(t)
			// Seeded by an earlier pass, so the sweep has a row to take.
			require.NoError(t, s.ApplyChange(ctx, step.kind, watch.Added, step.item))
			before, _, err := s.Cookie(ctx, step.kind.APIVersion, step.kind.Resource)
			require.NoError(t, err)
			failWrites(t, s, step.table, step.op)
			r := beginReplace(t, s, step.kind)

			_, err = r.Commit(ctx, "9")

			require.Error(t, err)
			after, _, err := s.Cookie(ctx, step.kind.APIVersion, step.kind.Resource)
			require.NoError(t, err)
			assert.Equal(t, before, after, "a failed prune must not hand on a position")
			assert.Equal(t, 1, countRows(t, s, `SELECT COUNT(*) FROM `+step.rows))
		})
	}
}

// Clearing a kind takes its rows, its tally and its cookie together — a partial clear
// would leave a tally over rows that are gone, or a cookie resuming a kind with none.
func TestClearKindIsAllOrNothing(t *testing.T) {
	t.Parallel()

	steps := []struct {
		table, op string
		kind      Kind
		item      *unstructured.Unstructured
	}{
		{"labels", "DELETE", podKind, pod("uid-1", "api-0", "1")},
		{"kind_counts", "DELETE", podKind, pod("uid-1", "api-0", "1")},
		{"cluster_meta", "DELETE", podKind, pod("uid-1", "api-0", "1")},
		{"events", "DELETE", eventsKind, event("ev-1", "2026-08-01T00:00:00Z")},
	}

	for _, step := range steps {
		t.Run(step.table+" "+step.op, func(t *testing.T) {
			ctx := context.Background()
			s := newTestStore(t)
			require.NoError(t, s.ApplyChange(ctx, step.kind, watch.Added, step.item))
			require.NoError(t, s.SetCookie(ctx, step.kind.APIVersion, step.kind.Resource, "1"))
			failWrites(t, s, step.table, step.op)

			require.Error(t, s.ClearKind(ctx, step.kind))

			n, err := s.CountKind(ctx, step.kind)
			require.NoError(t, err)
			assert.Equal(t, 1, n)
			rv, _, err := s.Cookie(ctx, step.kind.APIVersion, step.kind.Resource)
			require.NoError(t, err)
			assert.Equal(t, "1", rv)
		})
	}
}

// The catalog is written in one transaction with the fingerprint that names the sweep:
// rows a reader can see under a fingerprint the sweep did not write would pass the
// freshness check while carrying someone else's answer.
func TestSyncKindsIsAllOrNothing(t *testing.T) {
	t.Parallel()

	rows := []KindRow{{APIVersion: "v1", Kind: "Pod", Resource: "pods", Scope: "Namespaced"}}
	steps := []struct{ table, op string }{
		{"kind_catalog", "DELETE"},
		{"kind_catalog", "INSERT"},
		{"cluster_meta", "INSERT"},
	}

	for _, step := range steps {
		t.Run(step.table+" "+step.op, func(t *testing.T) {
			ctx := context.Background()
			s := newTestStore(t)
			// A row for the rename-resolving DELETE to fire on.
			require.NoError(t, s.SyncKinds(ctx, []KindRow{
				{APIVersion: "v1", Kind: "Stale", Resource: "pods", Scope: "Namespaced"},
			}, true, 1))
			failWrites(t, s, step.table, step.op)

			require.Error(t, s.SyncKinds(ctx, rows, true, 2))

			got, fingerprint, ok, err := s.KindsWithFingerprint(ctx)
			require.NoError(t, err)
			require.True(t, ok)
			assert.Equal(t, uint64(1), fingerprint)
			require.Len(t, got, 1)
			assert.Equal(t, "Stale", got[0].Kind)
		})
	}
}

// A sweep that carried nothing empties the catalog: the cluster serves no kinds this
// cache can reach, which is different from a partial answer that prunes nothing.
func TestSyncKindsWithNoRowsEmptiesTheCatalog(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	s := newTestStore(t)
	require.NoError(t, s.SyncKinds(ctx, []KindRow{
		{APIVersion: "v1", Kind: "Pod", Resource: "pods", Scope: "Namespaced"},
	}, true, 1))

	require.NoError(t, s.SyncKinds(ctx, nil, true, 2))

	got, err := s.Kinds(ctx)
	require.NoError(t, err)
	assert.Empty(t, got)
}

// A relist page skips a body it cannot project rather than failing the page: one
// malformed object must not stop a collection from syncing, and the next pass gets
// another chance at it.
func TestARelistPageSkipsABodyItCannotProject(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	s := newTestStore(t)
	bad := obj(map[string]any{"apiVersion": "v1", "kind": "Event", "metadata": map[string]any{}})
	r := beginReplace(t, s, eventsKind)

	require.NoError(t, r.WritePage(ctx, []*unstructured.Unstructured{
		bad, event("ev-1", "2026-08-01T00:00:00Z"),
	}))

	assert.Equal(t, 1, countRows(t, s, `SELECT COUNT(*) FROM events`))
}

// A watch delta whose event will not project is skipped the same way, and the position
// still advances: a body the server will replay from that position forever is worse than
// one missing event.
func TestApplyChangeSkipsAnEventItCannotProject(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	s := newTestStore(t)
	bad := obj(map[string]any{
		"apiVersion": "v1", "kind": "Event",
		"metadata": map[string]any{"resourceVersion": "7"},
	})

	require.NoError(t, s.ApplyChange(ctx, eventsKind, watch.Added, bad))

	assert.Zero(t, countRows(t, s, `SELECT COUNT(*) FROM events`))
	rv, ok, err := s.Cookie(ctx, eventsKind.APIVersion, eventsKind.Resource)
	require.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, "7", rv)
}

// A write that cannot take a position must fail rather than land: a row stamped with
// nothing sits below every cursor, which is the silent miss the stamp exists to prevent.
// Its commit is the same promise at the other end — the notify below it says the write
// landed, and a client that took that word for it would never be told otherwise.
func TestApplyChangeReportsWhatItCouldNotDo(t *testing.T) {
	t.Parallel()

	breaks := map[string]func(*testing.T, *Store){
		"the position it could not take": func(t *testing.T, s *Store) { dropTable(t, s, "cluster_meta") },
		"the commit that failed":         func(t *testing.T, s *Store) { failCommit(t, s, "INSERT", "objects") },
	}
	for name, brk := range breaks {
		t.Run(name, func(t *testing.T) {
			s := newTestStore(t)
			brk(t, s)

			err := s.ApplyChange(context.Background(), podKind, watch.Added, pod("uid-1", "api-0", "42"))

			assert.Error(t, err)
		})
	}
}

// A clear that fails must take nothing with it: the rows it evicts are only recoverable
// through the log entries it writes first, so a half-done clear is the one outcome the
// transaction exists to rule out.
func TestClearKindReportsWhatItCouldNotDo(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		kind   Kind
		break_ func(*testing.T, *Store)
	}{
		"the position it could not take": {podKind, func(t *testing.T, s *Store) { dropTable(t, s, "cluster_meta") }},
		"the events it could not log":    {eventsKind, func(t *testing.T, s *Store) { dropTable(t, s, "deletes") }},
		"the rows it could not log":      {podKind, func(t *testing.T, s *Store) { dropTable(t, s, "deletes") }},
		"the commit that failed":         {podKind, func(t *testing.T, s *Store) { failCommit(t, s, "DELETE", "objects") }},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			s := newTestStore(t)
			require.NoError(t, s.ApplyChange(ctx, podKind, watch.Added, pod("uid-1", "api-0", "42")))
			require.NoError(t, s.ApplyChange(ctx, eventsKind, watch.Added, firing("ev-1", "10", 1)))
			c.break_(t, s)

			err := s.ClearKind(ctx, c.kind)

			assert.Error(t, err)
		})
	}
}

// A page that fails is retried by the relist that owns it, so it has to say so: silence
// would leave the sweep's mark covering rows no page rewrote.
func TestWritePageReportsWhatItCouldNotDo(t *testing.T) {
	t.Parallel()

	breaks := map[string]func(*testing.T, *Store){
		"the cookie it could not clear": func(t *testing.T, s *Store) { dropTable(t, s, "cluster_meta") },
		"the commit that failed":        func(t *testing.T, s *Store) { failCommit(t, s, "INSERT", "objects") },
	}
	for name, brk := range breaks {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			s := newTestStore(t)
			session := beginReplace(t, s, podKind)
			brk(t, s)

			err := session.WritePage(ctx, []*unstructured.Unstructured{pod("uid-1", "api-0", "42")})

			assert.Error(t, err)
		})
	}

	// The stamp is taken after the cookie is cleared, so only a page past the first reaches
	// it — the first fails on the cookie.
	t.Run("the position it could not take", func(t *testing.T) {
		ctx := context.Background()
		s := newTestStore(t)
		session := beginReplace(t, s, podKind)
		require.NoError(t, session.WritePage(ctx, []*unstructured.Unstructured{pod("uid-1", "api-0", "42")}))
		dropTable(t, s, "cluster_meta")

		err := session.WritePage(ctx, []*unstructured.Unstructured{pod("uid-2", "api-1", "42")})

		assert.Error(t, err)
	})
}

// The relist's commit is where the prune and the cookie land together. Any half of it
// failing has to reach the caller: a cookie persisted over a prune that did not run would
// resume the next watch past rows that are still there.
func TestCommitRelistReportsWhatItCouldNotDo(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		kind   Kind
		break_ func(*testing.T, *Store)
	}{
		"the position it could not take": {podKind, func(t *testing.T, s *Store) { dropTable(t, s, "cluster_meta") }},
		"the pruned events it could not log": {eventsKind, func(t *testing.T, s *Store) {
			dropTable(t, s, "deletes")
		}},
		"the events it could not prune": {eventsKind, func(t *testing.T, s *Store) {
			// A view logs like a table and refuses the delete behind it.
			swapTable(t, s, "events", `SELECT 'ev-1' AS uid, 0 AS updated_at, 0 AS write_seq`)
		}},
		"the commit that failed": {podKind, func(t *testing.T, s *Store) { failCommit(t, s, "UPDATE", "cluster_meta") }},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			s := newTestStore(t)
			session := beginReplace(t, s, c.kind)
			c.break_(t, s)

			_, err := session.Commit(ctx, "100")

			assert.Error(t, err)
		})
	}
}
