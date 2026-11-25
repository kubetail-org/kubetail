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

//go:build !windows

package sqlitepool

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// A cache file holds every object the sidecar mirrored out of the cluster, so it must not be
// readable by other users on the box — whatever umask the process was started with. Windows
// has no POSIX permission bits; the data directory's per-user ACL carries it there.
func TestOpenWriterFilesAreOwnerOnly(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")
	db, err := OpenWriter(path)
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })
	// -wal and -shm exist only once something has written through the WAL.
	_, err = db.Exec(`CREATE TABLE t (id INTEGER PRIMARY KEY); INSERT INTO t VALUES (1);`)
	require.NoError(t, err)

	for _, p := range []string{path, path + "-wal", path + "-shm"} {
		fi, err := os.Stat(p)
		require.NoError(t, err, p)
		require.Equal(t, os.FileMode(0o600), fi.Mode().Perm(), p)
	}
}
