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

// Package sqlitemigratetest is what a store's tests use to pin its shipped formats: it
// makes a database look written by a newer build and checksums a migration set.
// Test-only, imported by no production code.
package sqlitemigratetest

import (
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"path"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/sqlitepool"
)

// NewerVersion is the schema version StampNewer records, above any this build carries.
const NewerVersion = 999

// StampNewer records NewerVersion in the schema_migrations table of the closed database at
// path, as a newer build's migration would. The database must have been migrated once.
func StampNewer(t testing.TB, path string) {
	t.Helper()
	db, err := sqlitepool.OpenWriter(path)
	require.NoError(t, err)
	defer func() { require.NoError(t, db.Close()) }()
	_, err = db.Exec(
		`INSERT INTO schema_migrations(version, name, applied_at) VALUES (?, '0999_test.sql', 0)`,
		NewerVersion,
	)
	require.NoError(t, err)
}

// Checksums answers each *.sql file in fsys's dir by its SHA-256, in hex, keyed by file
// name. A store's test compares it with the migrations its releases shipped.
func Checksums(t testing.TB, fsys fs.FS, dir string) map[string]string {
	t.Helper()
	names, err := fs.Glob(fsys, dir+"/*.sql")
	require.NoError(t, err)
	sums := make(map[string]string, len(names))
	for _, name := range names {
		b, err := fs.ReadFile(fsys, name)
		require.NoError(t, err)
		sum := sha256.Sum256(b)
		sums[path.Base(name)] = hex.EncodeToString(sum[:])
	}
	return sums
}
