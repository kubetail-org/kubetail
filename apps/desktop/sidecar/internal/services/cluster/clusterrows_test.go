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

package cluster

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/appdb"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/appdb/appdbtest"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/sqlstmt"
)

// openTestDB is an app.db of this test's own, with no janitor.
func openTestDB(t *testing.T) *appdb.DB {
	t.Helper()
	return appdbtest.Open(t)
}

// openTestStore prepares the cluster statements over a fresh app.db.
func openTestStore(t *testing.T) (*appdb.DB, *sqlstmt.Set[stmtID]) {
	t.Helper()
	db := openTestDB(t)
	st, err := sqlstmt.Prepare[stmtID](context.Background(), db.Write.DB, db.Read.DB, statements)
	require.NoError(t, err)
	t.Cleanup(func() { _ = st.Close() })
	return db, st
}

var t0 = time.UnixMilli(1_700_000_000_000).UTC()

// importRow inserts a kubeconfig row for contextName at t0 and returns it.
func importRow(t *testing.T, st *sqlstmt.Set[stmtID], contextName string) ClusterRow {
	t.Helper()
	var inserted bool
	id := appdb.NewID()
	err := st.InTx(context.Background(), func(s stmts) error {
		var err error
		inserted, err = insertClusterIfAbsent(context.Background(), s, id, contextName, t0)
		return err
	})
	require.NoError(t, err)
	require.True(t, inserted)
	row, ok, err := getCluster(context.Background(), st.Stmts(), ClusterID(id))
	require.NoError(t, err)
	require.True(t, ok)
	return row
}

// A fresh import takes SQL's defaults: enabled, syncing, not monitored, not the
// default, unnamed, unmarked, both stamps at the import time.
func TestInsertClusterIfAbsentTakesTheDefaults(t *testing.T) {
	t.Parallel()

	_, st := openTestStore(t)
	row := importRow(t, st, "dev")

	require.Equal(t, "kubeconfig", row.Source)
	require.NotNil(t, row.SourceKey)
	require.Equal(t, "dev", *row.SourceKey)
	require.Nil(t, row.Name)
	require.True(t, row.Enabled)
	require.True(t, row.SyncEnabled)
	require.False(t, row.IsDefault)
	require.Equal(t, t0, row.CreatedAt)
	require.Equal(t, t0, row.UpdatedAt)
	require.Nil(t, row.DeleteRequestedAt)
}

// A second import of the same context is a no-op — the claim is per (source,
// source_key), so the row, its id and its toggles survive a context that leaves and
// returns, and a marked row keeps the claim until it is removed.
func TestInsertClusterIfAbsentKeepsTheClaim(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	_, st := openTestStore(t)
	row := importRow(t, st, "dev")
	_, err := setClusterToggle(ctx, st.Stmts(), stmtSetClusterEnabled, row.ID, false, t0.Add(time.Second))
	require.NoError(t, err)

	reimport := func() bool {
		var inserted bool
		require.NoError(t, st.InTx(ctx, func(s stmts) error {
			var err error
			inserted, err = insertClusterIfAbsent(ctx, s, appdb.NewID(), "dev", t0.Add(time.Minute))
			return err
		}))
		return inserted
	}
	require.False(t, reimport())
	rows, err := listClusters(ctx, st.Stmts())
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, row.ID, rows[0].ID)
	require.False(t, rows[0].Enabled)

	_, err = markCluster(ctx, st.Stmts(), row.ID, t0.Add(2*time.Second))
	require.NoError(t, err)
	require.False(t, reimport())
}

// A toggle write moves the field and updated_at alone, and comes back as the row it
// wrote. A marked row refuses every toggle as not found: the user's choices are
// closed once its deletion is asked for.
func TestClusterTogglesAreGuardedByTheMark(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	_, st := openTestStore(t)
	row := importRow(t, st, "dev")
	at := t0.Add(time.Second)

	got, err := setClusterToggle(ctx, st.Stmts(), stmtSetClusterSyncEnabled, row.ID, false, at)
	require.NoError(t, err)
	require.False(t, got.SyncEnabled)
	require.True(t, got.Enabled)
	require.Equal(t, at, got.UpdatedAt)
	require.Equal(t, t0, got.CreatedAt)

	_, err = markCluster(ctx, st.Stmts(), row.ID, at)
	require.NoError(t, err)
	for name, set := range map[string]func() error{
		"enabled": func() error {
			_, err := setClusterToggle(ctx, st.Stmts(), stmtSetClusterEnabled, row.ID, false, at)
			return err
		},
		"sync": func() error {
			_, err := setClusterToggle(ctx, st.Stmts(), stmtSetClusterSyncEnabled, row.ID, true, at)
			return err
		},
	} {
		require.ErrorIs(t, set(), ErrNotFound, name)
	}
	_, err = setClusterToggle(ctx, st.Stmts(), stmtSetClusterEnabled, ClusterID(appdb.NewID()), false, at)
	require.ErrorIs(t, err, ErrNotFound)
}

// The mark is a user edit, so it stamps updated_at; a repeat leaves the first stamp
// and reports that nothing changed.
func TestMarkClusterIsIdempotent(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	_, st := openTestStore(t)
	row := importRow(t, st, "dev")
	at := t0.Add(time.Second)

	marked, err := markCluster(ctx, st.Stmts(), row.ID, at)
	require.NoError(t, err)
	require.True(t, marked)
	got, ok, err := getCluster(ctx, st.Stmts(), row.ID)
	require.NoError(t, err)
	require.True(t, ok)
	require.NotNil(t, got.DeleteRequestedAt)
	require.Equal(t, at, *got.DeleteRequestedAt)
	require.Equal(t, at, got.UpdatedAt)

	marked, err = markCluster(ctx, st.Stmts(), row.ID, at.Add(time.Second))
	require.NoError(t, err)
	require.False(t, marked)
	got, _, err = getCluster(ctx, st.Stmts(), row.ID)
	require.NoError(t, err)
	require.Equal(t, at, *got.DeleteRequestedAt)

	marked, err = markCluster(ctx, st.Stmts(), ClusterID(appdb.NewID()), at)
	require.NoError(t, err)
	require.False(t, marked)
}

// The final delete goes only through a marked row nothing references: unmarked, it
// deletes nothing; a chat filed under it holds it; once the chat is gone it goes.
func TestDeleteClusterIsGuarded(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, st := openTestStore(t)
	row := importRow(t, st, "dev")

	deleted, err := deleteMarkedCluster(ctx, st.Stmts(), row.ID)
	require.NoError(t, err)
	require.False(t, deleted)

	_, err = markCluster(ctx, st.Stmts(), row.ID, t0)
	require.NoError(t, err)
	_, err = db.Write.Exec(`INSERT INTO chats (id, cluster_id, mode, created_at, updated_at) VALUES ('c', ?, 'chat', 0, 0)`, string(row.ID))
	require.NoError(t, err)
	deleted, err = deleteMarkedCluster(ctx, st.Stmts(), row.ID)
	require.NoError(t, err)
	require.False(t, deleted)

	_, err = db.Write.Exec(`DELETE FROM chats`)
	require.NoError(t, err)
	deleted, err = deleteMarkedCluster(ctx, st.Stmts(), row.ID)
	require.NoError(t, err)
	require.True(t, deleted)
	_, ok, err := getCluster(ctx, st.Stmts(), row.ID)
	require.NoError(t, err)
	require.False(t, ok)
}

// The list is in id order — ids are v7, so that is import order.
func TestListClustersIsInIDOrder(t *testing.T) {
	t.Parallel()

	_, st := openTestStore(t)
	first := importRow(t, st, "b")
	second := importRow(t, st, "a")
	rows, err := listClusters(context.Background(), st.Stmts())
	require.NoError(t, err)
	require.Equal(t, []ClusterID{first.ID, second.ID}, []ClusterID{rows[0].ID, rows[1].ID})
}

// Every read and write over the table reports a store that will not answer, never an
// empty or a no-op answer.
func TestClusterRowsReportAStorageFault(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, st := openTestStore(t)
	row := importRow(t, st, "dev")
	require.NoError(t, db.Close())

	for name, op := range map[string]func() error{
		"get":  func() error { _, _, err := getCluster(ctx, st.Stmts(), row.ID); return err },
		"list": func() error { _, err := listClusters(ctx, st.Stmts()); return err },
		"toggle": func() error {
			_, err := setClusterToggle(ctx, st.Stmts(), stmtSetClusterEnabled, row.ID, false, t0)
			return err
		},
		"mark":   func() error { _, err := markCluster(ctx, st.Stmts(), row.ID, t0); return err },
		"delete": func() error { _, err := deleteMarkedCluster(ctx, st.Stmts(), row.ID); return err },
		"insert": func() error {
			return st.InTx(ctx, func(s stmts) error {
				_, err := insertClusterIfAbsent(ctx, s, appdb.NewID(), "x", t0)
				return err
			})
		},
	} {
		assert.Error(t, op(), name)
	}
}

// setDefault runs setClusterDefault in its own transaction, as the service does,
// letting any enabled row be the default.
func setDefault(t *testing.T, st *sqlstmt.Set[stmtID], id ClusterID, on bool, at time.Time) (ClusterRow, error) {
	t.Helper()
	var row ClusterRow
	err := st.InTx(context.Background(), func(s stmts) error {
		var err error
		row, _, err = setClusterDefault(context.Background(), s, id, on, at, func(r ClusterRow) bool { return r.Enabled })
		return err
	})
	return row, err
}

// makeDefault makes id the default at t0, a test's starting state.
func makeDefault(t *testing.T, st *sqlstmt.Set[stmtID], id ClusterID) {
	t.Helper()
	_, err := setDefault(t, st, id, true, t0)
	require.NoError(t, err)
}

// reread is the row as stored now.
func reread(t *testing.T, st *sqlstmt.Set[stmtID], id ClusterID) ClusterRow {
	t.Helper()
	row, ok, err := getCluster(context.Background(), st.Stmts(), id)
	require.NoError(t, err)
	require.True(t, ok)
	return row
}

// Setting the default clears the one before, and both stamps move, so the watch
// sends a change for each.
func TestSetClusterDefaultMovesTheDefault(t *testing.T) {
	t.Parallel()

	_, st := openTestStore(t)
	a, b := importRow(t, st, "a"), importRow(t, st, "b")
	at := t0.Add(time.Second)

	got, err := setDefault(t, st, a.ID, true, at)
	require.NoError(t, err)
	require.True(t, got.IsDefault)
	require.Equal(t, at, got.UpdatedAt)

	later := at.Add(time.Second)
	got, err = setDefault(t, st, b.ID, true, later)
	require.NoError(t, err)
	require.True(t, got.IsDefault)
	old := reread(t, st, a.ID)
	require.False(t, old.IsDefault)
	require.Equal(t, later, old.UpdatedAt)
}

// Clearing the default leaves no row the default.
func TestSetClusterDefaultClears(t *testing.T) {
	t.Parallel()

	_, st := openTestStore(t)
	a := importRow(t, st, "a")
	makeDefault(t, st, a.ID)

	got, err := setDefault(t, st, a.ID, false, t0.Add(time.Second))
	require.NoError(t, err)
	require.False(t, got.IsDefault)
	require.False(t, reread(t, st, a.ID).IsDefault)
}

// A call that changes nothing writes nothing: the stamp stays, so no change is sent.
func TestSetClusterDefaultThatChangesNothingWritesNothing(t *testing.T) {
	t.Parallel()

	_, st := openTestStore(t)
	a, b := importRow(t, st, "a"), importRow(t, st, "b")
	at := t0.Add(time.Second)
	_, err := setDefault(t, st, a.ID, true, at)
	require.NoError(t, err)

	got, err := setDefault(t, st, a.ID, true, at.Add(time.Second))
	require.NoError(t, err)
	require.True(t, got.IsDefault)
	require.Equal(t, at, got.UpdatedAt, "setting the default again")

	got, err = setDefault(t, st, b.ID, false, at.Add(time.Second))
	require.NoError(t, err)
	require.False(t, got.IsDefault)
	require.Equal(t, t0, got.UpdatedAt, "clearing a cluster that isn't the default")
}

// A marked row is closed to the default as to every toggle, and an absent one is
// not found; one the predicate refuses cannot become the default.
func TestSetClusterDefaultRefusals(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	_, st := openTestStore(t)
	marked, disabled := importRow(t, st, "marked"), importRow(t, st, "disabled")
	_, err := markCluster(ctx, st.Stmts(), marked.ID, t0)
	require.NoError(t, err)
	_, err = setClusterToggle(ctx, st.Stmts(), stmtSetClusterEnabled, disabled.ID, false, t0)
	require.NoError(t, err)

	_, err = setDefault(t, st, marked.ID, true, t0)
	require.ErrorIs(t, err, ErrNotFound)
	_, err = setDefault(t, st, marked.ID, false, t0)
	require.ErrorIs(t, err, ErrNotFound)
	_, err = setDefault(t, st, ClusterID(appdb.NewID()), true, t0)
	require.ErrorIs(t, err, ErrNotFound)

	_, err = setDefault(t, st, disabled.ID, true, t0.Add(time.Second))
	require.ErrorIs(t, err, ErrCannotBeDefault)
	require.False(t, reread(t, st, disabled.ID).IsDefault)
}

// Marking the default for deletion clears it in the same write, so another can be
// the default while it is torn down.
func TestMarkingTheDefaultClearsIt(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	_, st := openTestStore(t)
	a, b := importRow(t, st, "a"), importRow(t, st, "b")
	makeDefault(t, st, a.ID)

	_, err := markCluster(ctx, st.Stmts(), a.ID, t0.Add(time.Second))
	require.NoError(t, err)
	require.False(t, reread(t, st, a.ID).IsDefault)

	got, err := setDefault(t, st, b.ID, true, t0.Add(2*time.Second))
	require.NoError(t, err)
	require.True(t, got.IsDefault)
}

// Disabling the default clears it, and enabling the cluster again leaves it clear.
func TestDisablingTheDefaultClearsIt(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	_, st := openTestStore(t)
	a := importRow(t, st, "a")
	makeDefault(t, st, a.ID)

	got, err := setClusterToggle(ctx, st.Stmts(), stmtSetClusterEnabled, a.ID, false, t0.Add(time.Second))
	require.NoError(t, err)
	require.False(t, got.IsDefault)

	got, err = setClusterToggle(ctx, st.Stmts(), stmtSetClusterEnabled, a.ID, true, t0.Add(2*time.Second))
	require.NoError(t, err)
	require.False(t, got.IsDefault)
}

// Enabling a cluster that is already the default keeps it the default.
func TestEnablingTheDefaultKeepsIt(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	_, st := openTestStore(t)
	a := importRow(t, st, "a")
	makeDefault(t, st, a.ID)

	got, err := setClusterToggle(ctx, st.Stmts(), stmtSetClusterEnabled, a.ID, true, t0.Add(time.Second))
	require.NoError(t, err)
	require.True(t, got.IsDefault)
}

// The importer leaves a returning context's row alone, so an archived default is
// the default again when its context comes back.
func TestAReturningDefaultIsStillTheDefault(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	_, st := openTestStore(t)
	a := importRow(t, st, "a")
	makeDefault(t, st, a.ID)

	require.NoError(t, st.InTx(ctx, func(s stmts) error {
		_, err := insertClusterIfAbsent(ctx, s, appdb.NewID(), "a", t0.Add(time.Minute))
		return err
	}))
	require.True(t, reread(t, st, a.ID).IsDefault)
}

// A clear that fails rolls the whole write back: the old default stays.
func TestSetClusterDefaultRollsBackAFailedClear(t *testing.T) {
	t.Parallel()

	db, st := openTestStore(t)
	a, b := importRow(t, st, "a"), importRow(t, st, "b")
	makeDefault(t, st, a.ID)
	_, err := db.Write.Exec(`CREATE TRIGGER refuse_clear BEFORE UPDATE OF is_default ON clusters
		WHEN NEW.is_default = 0 BEGIN SELECT RAISE(ABORT, 'refused'); END`)
	require.NoError(t, err)

	_, err = setDefault(t, st, b.ID, true, t0.Add(time.Second))
	require.ErrorContains(t, err, "refused")
	require.True(t, reread(t, st, a.ID).IsDefault)
	require.False(t, reread(t, st, b.ID).IsDefault)
}
