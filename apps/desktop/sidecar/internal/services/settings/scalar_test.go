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

package settings

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/appdb"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/appdb/appdbtest"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/apimeta"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/testutil"
)

// The test's own entries, one of each kind, each with a check.
type testList struct {
	Names []string `json:"names"`
}

var (
	flagEntry = Entry[bool]{Key: "test.flag", Default: true, Strictest: new(false)}
	// countEntry has no strictest value: a value it cannot use reads as its default.
	countEntry = Entry[int64]{Key: "test.count", Default: 3, Check: func(n int64) error {
		if n < 0 {
			return errors.New("a count is never negative")
		}
		return nil
	}}
	nameEntry = Entry[string]{Key: "test.name", Default: "ada", Strictest: new("nobody"), PerCluster: true, Check: func(s string) error {
		if s == "" {
			return errors.New("a name is never empty")
		}
		return nil
	}}
	// namesEntry's zero value, a nil slice, encodes as the JSON null.
	namesEntry = Entry[[]string]{Key: "test.names", Default: []string{}}
	listEntry  = Entry[testList]{Key: "test.list", Default: testList{Names: []string{}}, Check: func(l testList) error {
		if len(l.Names) > 2 {
			return errors.New("a list holds two names at most")
		}
		return nil
	}}
)

// readOf is e's value for cluster and the scope it came from.
func readOf[T any](t *testing.T, s *Store, e Entry[T], cluster apimeta.ClusterID) (T, settingScope) {
	t.Helper()
	v, scope, err := readSetting(t.Context(), s.stmts.Stmts(), e, cluster)
	require.NoError(t, err)
	return v, scope
}

// reading is a read of e's value for every cluster, as any kind.
func reading[T any](t *testing.T, s *Store, e Entry[T]) func() any {
	return func() any {
		t.Helper()
		v, _ := readOf(t, s, e, "")
		return v
	}
}

func write[T any](t *testing.T, s *Store, e Entry[T], cluster apimeta.ClusterID, v T) error {
	t.Helper()
	return writeSetting(t.Context(), s.stmts.Stmts(), e, cluster, v, s.now().UnixMilli())
}

// storedRow is a key's row in a scope as stored: its value, the value's SQL
// type and its times.
type storedRow struct {
	Value            any
	Type             string
	Created, Updated int64
}

func storedRowOf(t *testing.T, db *appdb.DB, key string, cluster apimeta.ClusterID) storedRow {
	t.Helper()
	var r storedRow
	require.NoError(t, db.Read.QueryRow(`SELECT value, typeof(value), created_at, updated_at FROM settings WHERE key = ? AND ifnull(cluster_id, '') = ?`, key, cluster).
		Scan(&r.Value, &r.Type, &r.Created, &r.Updated))
	return r
}

func settingsCount(t *testing.T, db *appdb.DB) int {
	t.Helper()
	var n int
	require.NoError(t, db.Read.QueryRow(`SELECT count(*) FROM settings`).Scan(&n))
	return n
}

func TestASettingWithNoRowReadsItsDefault(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	flag, scope := readOf(t, s, flagEntry, "")
	assert.True(t, flag)
	assert.Equal(t, scopeDefault, scope)
	count, _ := readOf(t, s, countEntry, "")
	assert.Equal(t, int64(3), count)
	name, _ := readOf(t, s, nameEntry, "")
	assert.Equal(t, "ada", name)
	list, _ := readOf(t, s, listEntry, "")
	assert.Equal(t, testList{Names: []string{}}, list)
}

// Each kind round-trips, and is stored as its SQL type: a boolean and an
// integer as an integer, a string and a JSON value as text.
func TestASettingWriteRoundTrips(t *testing.T) {
	t.Parallel()
	db := appdbtest.Open(t)
	s := openStore(t, db)

	require.NoError(t, write(t, s, flagEntry, "", false))
	require.NoError(t, write(t, s, countEntry, "", 7))
	require.NoError(t, write(t, s, nameEntry, "", "grace"))
	require.NoError(t, write(t, s, listEntry, "", testList{Names: []string{"a", "b"}}))

	flag, scope := readOf(t, s, flagEntry, "")
	assert.False(t, flag)
	assert.Equal(t, scopeEveryCluster, scope)
	count, _ := readOf(t, s, countEntry, "")
	assert.Equal(t, int64(7), count)
	name, _ := readOf(t, s, nameEntry, "")
	assert.Equal(t, "grace", name)
	list, _ := readOf(t, s, listEntry, "")
	assert.Equal(t, testList{Names: []string{"a", "b"}}, list)

	assert.Equal(t, storedRow{Value: int64(0), Type: "integer"}, onlyValue(storedRowOf(t, db, "test.flag", "")))
	assert.Equal(t, storedRow{Value: int64(7), Type: "integer"}, onlyValue(storedRowOf(t, db, "test.count", "")))
	assert.Equal(t, storedRow{Value: "grace", Type: "text"}, onlyValue(storedRowOf(t, db, "test.name", "")))
	assert.Equal(t, storedRow{Value: `{"names":["a","b"]}`, Type: "text"}, onlyValue(storedRowOf(t, db, "test.list", "")))
}

// onlyValue is r without its times.
func onlyValue(r storedRow) storedRow { return storedRow{Value: r.Value, Type: r.Type} }

func TestASecondSettingWriteKeepsCreatedAtAndMovesUpdatedAt(t *testing.T) {
	t.Parallel()
	db := appdbtest.Open(t)
	s := openStore(t, db)
	clock := time.UnixMilli(1000)
	s.now = func() time.Time { return clock }

	require.NoError(t, write(t, s, countEntry, "", 7))
	clock = time.UnixMilli(2000)
	require.NoError(t, write(t, s, countEntry, "", 7), "a write of the same value")
	assert.Equal(t, storedRow{Value: int64(7), Type: "integer", Created: 1000, Updated: 2000}, storedRowOf(t, db, "test.count", ""))
}

// A reset keeps the row, with NULL, and reads the default.
func TestASettingResetWritesNull(t *testing.T) {
	t.Parallel()
	db := appdbtest.Open(t)
	s := openStore(t, db)
	require.NoError(t, write(t, s, countEntry, "", 7))
	require.NoError(t, resetSetting(t.Context(), s.stmts.Stmts(), countEntry, "", s.now().UnixMilli()))

	count, scope := readOf(t, s, countEntry, "")
	assert.Equal(t, int64(3), count)
	assert.Equal(t, scopeDefault, scope)
	assert.Equal(t, storedRow{Value: nil, Type: "null"}, onlyValue(storedRowOf(t, db, "test.count", "")))
}

// A value equal to the default is a value: it stays when the default changes.
func TestASettingEqualToTheDefaultIsStored(t *testing.T) {
	t.Parallel()
	db := appdbtest.Open(t)
	s := openStore(t, db)
	require.NoError(t, write(t, s, countEntry, "", 3))

	later := countEntry
	later.Default = 5
	count, scope := readOf(t, s, later, "")
	assert.Equal(t, int64(3), count)
	assert.Equal(t, scopeEveryCluster, scope)
}

// A cluster's own row wins; a NULL there, or no row, falls through to the
// row for every cluster, then the default. An entry no cluster has its own of
// reads the row for every cluster alone.
func TestAClustersSettingFallsThrough(t *testing.T) {
	t.Parallel()
	db := appdbtest.Open(t)
	seedCluster(t, db, "dev", "dev-eks")
	s := openStore(t, db)
	ctx := t.Context()

	name, scope := readOf(t, s, nameEntry, "dev")
	assert.Equal(t, []any{"ada", scopeDefault}, []any{name, scope})
	require.NoError(t, write(t, s, nameEntry, "", "grace"))
	name, scope = readOf(t, s, nameEntry, "dev")
	assert.Equal(t, []any{"grace", scopeEveryCluster}, []any{name, scope})
	require.NoError(t, write(t, s, nameEntry, "dev", "linus"))
	name, scope = readOf(t, s, nameEntry, "dev")
	assert.Equal(t, []any{"linus", scopeOwn}, []any{name, scope})
	name, _ = readOf(t, s, nameEntry, "")
	assert.Equal(t, "grace", name, "every cluster keeps its own")
	require.NoError(t, resetSetting(ctx, s.stmts.Stmts(), nameEntry, "dev", 0))
	name, scope = readOf(t, s, nameEntry, "dev")
	assert.Equal(t, []any{"grace", scopeEveryCluster}, []any{name, scope}, "a NULL falls through")

	exec(t, db, `INSERT INTO settings (key, cluster_id, value, created_at, updated_at) VALUES ('test.count', 'dev', 9, 0, 0)`)
	count, scope := readOf(t, s, countEntry, "dev")
	assert.Equal(t, []any{int64(3), scopeDefault}, []any{count, scope}, "an entry no cluster has its own of")
}

// A value the check refuses is a SettingRefusal naming the key, wrapping the
// check's own error, and writes nothing.
func TestARefusedSettingWritesNothing(t *testing.T) {
	t.Parallel()
	db := appdbtest.Open(t)
	s := openStore(t, db)

	err := write(t, s, countEntry, "", -1)
	refusal, ok := errors.AsType[SettingRefusal](err)
	require.True(t, ok, "%v", err)
	assert.Equal(t, "test.count", refusal.Key)
	assert.EqualError(t, err, "test.count: a count is never negative")
	assert.Error(t, write(t, s, listEntry, "", testList{Names: []string{"a", "b", "c"}}))
	assert.Zero(t, settingsCount(t, db))
}

// A value Kubetail cannot use reads as the entry's strictest value, or its
// default when it has none, and is logged by key and reason, never by value.
// The row stays. Serial: CaptureLogs swaps the process's logger.
func TestASettingKubetailCannotUseReadsItsStrictestOrDefault(t *testing.T) {
	db := appdbtest.Open(t)
	s := openStore(t, db)
	logs := testutil.CaptureLogs(t)
	for _, tc := range []struct {
		name  string
		key   string
		value any
		read  func() any
		want  any
	}{
		{"a flag of the wrong kind", "test.flag", "secret-yes", reading(t, s, flagEntry), false},
		{"a flag neither 0 nor 1", "test.flag", int64(2), reading(t, s, flagEntry), false},
		{"a count of the wrong kind", "test.count", "secret-three", reading(t, s, countEntry), int64(3)},
		{"a count its check refuses", "test.count", int64(-5), reading(t, s, countEntry), int64(3)},
		{"a name of the wrong kind", "test.name", int64(4), reading(t, s, nameEntry), "nobody"},
		{"a list of the wrong kind", "test.list", int64(1), reading(t, s, listEntry), testList{Names: []string{}}},
		{"a list that does not decode", "test.list", "secret-[", reading(t, s, listEntry), testList{Names: []string{}}},
		{"a list followed by more", "test.list", `{"names":["a"]}secret`, reading(t, s, listEntry), testList{Names: []string{}}},
		{"a list that is JSON null", "test.list", "null", reading(t, s, listEntry), testList{Names: []string{}}},
		{"a list with a field it does not know", "test.list", `{"names":[],"secret":1}`, reading(t, s, listEntry), testList{Names: []string{}}},
		{"a list its check refuses", "test.list", `{"names":["secret-a","b","c"]}`, reading(t, s, listEntry), testList{Names: []string{}}},
	} {
		exec(t, db, `DELETE FROM settings`)
		exec(t, db, `INSERT INTO settings (key, value, created_at, updated_at) VALUES (?, ?, 0, 0)`, tc.key, tc.value)
		assert.Equal(t, tc.want, tc.read(), tc.name)
		assert.Equal(t, 1, settingsCount(t, db), "%s: the row stays", tc.name)
	}
	logged := logs.String()
	assert.Contains(t, logged, "test.flag")
	assert.Contains(t, logged, "test.list")
	assert.NotContains(t, logged, "secret")
}

// A row whose key no entry has is kept and changes nothing.
func TestASettingRowWithNoEntryIsKept(t *testing.T) {
	t.Parallel()
	db := appdbtest.Open(t)
	exec(t, db, `INSERT INTO settings (key, value, created_at, updated_at) VALUES ('test.gone', 'x', 0, 0)`)
	s := openStore(t, db)
	require.NoError(t, write(t, s, countEntry, "", 7))
	count, _ := readOf(t, s, countEntry, "")
	assert.Equal(t, int64(7), count)
	assert.Equal(t, storedRow{Value: "x", Type: "text"}, onlyValue(storedRowOf(t, db, "test.gone", "")))
}

func TestASettingReadThatFailsIsAnError(t *testing.T) {
	t.Parallel()
	db := appdbtest.Open(t)
	s := openStore(t, db)
	exec(t, db, `DROP TABLE settings`)
	ctx, st := t.Context(), s.stmts.Stmts()

	_, _, err := readSetting(ctx, st, countEntry, "")
	assert.Error(t, err)
	_, err = selectSettingScopes(ctx, st, countEntry.Key)
	assert.Error(t, err)
	assert.Error(t, write(t, s, countEntry, "", 1))
	assert.Error(t, resetSetting(ctx, st, countEntry, "", 0))
}

// A JSON value is never the JSON null: a NULL is the reset, and a read
// refuses the text null. The row keeps its value.
func TestASettingWriteOfJSONNullIsRefused(t *testing.T) {
	t.Parallel()
	db := appdbtest.Open(t)
	s := openStore(t, db)
	require.NoError(t, write(t, s, namesEntry, "", []string{"a"}))

	_, ok := errors.AsType[SettingRefusal](write(t, s, namesEntry, "", nil))
	assert.True(t, ok)
	names, _ := readOf(t, s, namesEntry, "")
	assert.Equal(t, []string{"a"}, names)
}

// A setting no cluster has its own of refuses a cluster's write or reset,
// which its read would never see, and writes no row.
func TestASettingWithNoClusterScopeRefusesAClustersWrite(t *testing.T) {
	t.Parallel()
	db := appdbtest.Open(t)
	seedCluster(t, db, "dev", "dev-eks")
	s := openStore(t, db)

	assert.ErrorIs(t, write(t, s, countEntry, "dev", 1), errNoClusterScope)
	assert.ErrorIs(t, resetSetting(t.Context(), s.stmts.Stmts(), countEntry, "dev", 0), errNoClusterScope)
	assert.Zero(t, settingsCount(t, db))
}

// The read of one key for one cluster takes the index, as every read a
// decision makes does.
func TestTheReadOfOneClustersSettingUsesTheIndex(t *testing.T) {
	t.Parallel()
	plan := queryPlan(t, appdbtest.Open(t), statements[stmtSelectScopedSetting].Text, "dev", "test.name")
	assert.Contains(t, plan, "USING INDEX settings_scope_key")
}

// queryPlan is SQLite's plan for q, its steps' details joined by lines.
func queryPlan(t *testing.T, db *appdb.DB, q string, args ...any) string {
	t.Helper()
	rows, err := db.Read.Query(`EXPLAIN QUERY PLAN `+q, args...)
	require.NoError(t, err)
	defer rows.Close()
	var plan []string
	for rows.Next() {
		var id, parent, unused int
		var detail string
		require.NoError(t, rows.Scan(&id, &parent, &unused, &detail))
		plan = append(plan, detail)
	}
	require.NoError(t, rows.Err())
	return strings.Join(plan, "\n")
}
