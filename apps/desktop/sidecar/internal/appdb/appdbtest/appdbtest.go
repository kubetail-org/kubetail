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

// Package appdbtest opens an app.db for a test without migrating it: the schema is
// migrated once per test binary into a template, and each test opens a copy, which
// appdb.Open finds already at the current version. Test-only, imported by no
// production code.
package appdbtest

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/appdb"
)

var template = sync.OnceValues(migrateTemplate)

// migrateTemplate migrates a fresh app.db and returns its bytes. The log is
// checkpointed into the file first, so the one file is the whole database.
func migrateTemplate() ([]byte, error) {
	dir, err := os.MkdirTemp("", "appdbtest-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	path := filepath.Join(dir, "app.db")
	db, err := appdb.Open(path, 0)
	if err != nil { // coverage:ignore a fresh file in a directory just made opens and migrates
		return nil, fmt.Errorf("migrate template: %w", err)
	}
	_, err = db.Write.ExecContext(context.Background(), `PRAGMA wal_checkpoint(TRUNCATE)`)
	if cerr := db.Close(); err == nil {
		err = cerr
	}
	if err != nil { // coverage:ignore nothing else holds the fresh file, so its checkpoint and close succeed
		return nil, fmt.Errorf("checkpoint template: %w", err)
	}
	return os.ReadFile(path)
}

// Open is an app.db of the test's own in a temporary directory, with no janitor,
// closed when the test ends.
func Open(t testing.TB) *appdb.DB {
	t.Helper()
	return OpenIn(t, t.TempDir())
}

// OpenIn is Open with the file at dir/app.db, for a test that reopens it by name.
// A file already there is opened as it is, so a reopen finds the rows it left.
func OpenIn(t testing.TB, dir string) *appdb.DB {
	t.Helper()
	path := filepath.Join(dir, "app.db")
	if _, err := os.Stat(path); os.IsNotExist(err) {
		b, err := template()
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(path, b, 0o600))
	}
	db, err := appdb.Open(path, 0)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	return db
}
