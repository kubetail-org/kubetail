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

package appdbtest

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/appdb"
)

// schemaOf is the database's migration version and its schema as SQLite records it.
func schemaOf(t *testing.T, db *appdb.DB) (int, []string) {
	t.Helper()
	var version int
	require.NoError(t, db.Read.QueryRow(`SELECT MAX(version) FROM schema_migrations`).Scan(&version))
	rows, err := db.Read.Query(`SELECT type || ' ' || name || ': ' || ifnull(sql, '') FROM sqlite_master ORDER BY type, name`)
	require.NoError(t, err)
	defer rows.Close()
	var schema []string
	for rows.Next() {
		var s string
		require.NoError(t, rows.Scan(&s))
		schema = append(schema, s)
	}
	require.NoError(t, rows.Err())
	return version, schema
}

// A copy is at the version a fresh migration reaches, with the same schema, so a
// test on a copy runs against what production opens.
func TestACopyIsAtTheCurrentSchema(t *testing.T) {
	fresh, err := appdb.Open(filepath.Join(t.TempDir(), "app.db"), 0)
	require.NoError(t, err)
	t.Cleanup(func() { _ = fresh.Close() })

	wantVersion, wantSchema := schemaOf(t, fresh)
	gotVersion, gotSchema := schemaOf(t, Open(t))
	assert.Positive(t, gotVersion)
	assert.Equal(t, wantVersion, gotVersion)
	assert.Equal(t, wantSchema, gotSchema)
}

// Each copy is the test's own: a row one writes is not in the next.
func TestEachCopyIsItsOwn(t *testing.T) {
	a := Open(t)
	_, err := a.Write.Exec(`INSERT INTO clusters (id, source, source_key, created_at, updated_at) VALUES ('1', 'kubeconfig', 'ctx', 0, 0)`)
	require.NoError(t, err)

	var n int
	require.NoError(t, Open(t).Read.QueryRow(`SELECT count(*) FROM clusters`).Scan(&n))
	assert.Zero(t, n)
}

// A reopen by name finds the rows the first open left.
func TestOpenInReopensWhatIsThere(t *testing.T) {
	dir := t.TempDir()
	first := OpenIn(t, dir)
	_, err := first.Write.Exec(`INSERT INTO clusters (id, source, source_key, created_at, updated_at) VALUES ('1', 'kubeconfig', 'ctx', 0, 0)`)
	require.NoError(t, err)
	require.NoError(t, first.Close())

	var n int
	require.NoError(t, OpenIn(t, dir).Read.QueryRow(`SELECT count(*) FROM clusters`).Scan(&n))
	assert.Equal(t, 1, n)
}
