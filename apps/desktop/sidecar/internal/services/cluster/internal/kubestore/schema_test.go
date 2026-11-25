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
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/watch"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/sqlitemigrate/sqlitemigratetest"
)

// shippedMigrations is every file in migrations/ by SHA-256. A new migration adds its
// line in the PR that writes it. Once a desktop release carries a migration, every
// installed cache has run it, so its line never changes again.
var shippedMigrations = map[string]string{
	"0001_init.sql": "16e8432e545d463ba37f873b1a8c0d4325cbda89267f4c52593331483db09c04",
}

func TestAShippedMigrationIsNeverEdited(t *testing.T) {
	t.Parallel()
	assert.Equal(t, shippedMigrations, sqlitemigratetest.Checksums(t, migrationsFS, "migrations"))
}

// A namespace's events are found through events_involved_ns.
func TestTheEventsAreIndexedByInvolvedNamespace(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	m := NewManager(t.TempDir(), Retention{})
	t.Cleanup(func() { require.NoError(t, m.Close()) })
	s, err := m.OpenOrCreate(1)
	require.NoError(t, err)
	t.Cleanup(s.Release)
	_, err = db(t, s).ExecContext(ctx, `
		INSERT INTO events (uid, resource_version, write_seq, involved_kind, involved_ns, raw_json, updated_at)
		VALUES ('ev-pod', '1', 1, 'Pod', 'prod', x'', 0), ('ev-node', '2', 2, 'Node', NULL, x'', 0)`)
	require.NoError(t, err)

	const inProd = `SELECT uid FROM events WHERE involved_ns = 'prod'`
	var uid string
	require.NoError(t, db(t, s).QueryRowContext(ctx, inProd).Scan(&uid))
	assert.Equal(t, "ev-pod", uid)
	assert.Equal(t, 2, countRows(t, s, `SELECT count(*) FROM events`))
	assert.Contains(t, queryPlan(t, s, inProd), "events_involved_ns")
}

// storesRowid reports whether the table keeps SQLite's implicit rowid. pragma_table_list's
// wr column is the declared form itself, so this reads the schema rather than inferring it.
func storesRowid(t *testing.T, s *Store, table string) bool {
	t.Helper()
	var withoutRowid bool
	require.NoError(t, db(t, s).QueryRowContext(context.Background(),
		`SELECT wr FROM pragma_table_list WHERE schema = 'main' AND name = ?`, table,
	).Scan(&withoutRowid))
	return !withoutRowid
}

// The four all-key tables store their key once, in the table's own b-tree, rather than
// under a rowid with a second copy in an autoindex.
func TestTheAllKeyTablesHaveNoRowid(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)

	for _, table := range []string{"cluster_meta", "owner_refs", "labels", "kind_counts", "refs",
		"selectors", "selector_terms"} {
		assert.False(t, storesRowid(t, s, table), "%s should be WITHOUT ROWID", table)
	}
}

// The exclusions, each for its own reason — a wide row, an FTS index keyed by rowid, or a
// table with no key at all. The schema states which beside each; this is what would notice
// one of them being swept into the change.
func TestTheWideAndKeylessTablesKeepTheirRowid(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)

	for _, table := range []string{"objects", "events", "status_history", "kind_catalog", "containers"} {
		assert.True(t, storesRowid(t, s, table), "%s should keep its rowid", table)
	}
}

// kind_counts is reached only through triggers, so no other test's failure would name it:
// a form that broke them leaves the nav's per-kind badge frozen while every write succeeds.
func TestTheKindCounterMovesInBothDirections(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	s := newTestStore(t)
	counted := func() int {
		return countRows(t, s,
			`SELECT count FROM kind_counts WHERE api_version = ? AND kind = ?`,
			podKind.APIVersion, podKind.Kind)
	}

	require.NoError(t, s.ApplyChange(ctx, podKind, watch.Added, pod("uid-1", "api-0", "1")))
	assert.Equal(t, 1, counted())

	require.NoError(t, s.ApplyChange(ctx, podKind, watch.Deleted, pod("uid-1", "api-0", "2")))
	assert.Equal(t, 0, counted())
}

// queryPlan is what SQLite would do to answer a statement with args bound.
func queryPlan(t *testing.T, s *Store, query string, args ...any) string {
	t.Helper()
	rows, err := db(t, s).QueryContext(context.Background(), `EXPLAIN QUERY PLAN `+query, args...)
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
	return strings.Join(plan, "\n")
}

// The events snapshot is the largest read in the file — nothing ages events out and nothing
// bounds the read — and its order has to be total, so it sorts by the uid tiebreak as well.
// The index carries that tiebreak IN THE SAME DIRECTION: an ascending one leaves the last
// term to a temp b-tree, which is a full sort of the table on every snapshot.
func TestTheEventsOrderIsServedByItsIndex(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)

	plan := queryPlan(t, s, statements[stmtSelectEvents].Text)

	assert.Contains(t, plan, "events_last_seen")
	assert.NotContains(t, plan, "TEMP B-TREE")
}

// The namespace scope recounts on every write ping, so each count is an index range,
// never a scan of objects or events.
func TestTheNamespacedKindCountsAreServedByIndexes(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)

	plan := queryPlan(t, s, statements[stmtSelectKinds].Text, "prod")

	assert.Contains(t, plan, "events_involved_ns")
	assert.Contains(t, plan, "objects_ns_kind")
	assert.NotContains(t, plan, "SCAN e")
	assert.NotContains(t, plan, "SCAN o")
}
