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

package kubestore

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A cluster cache mirrors every object the credentials can read, so its file must be
// owner-only even when the sidecar was started with a permissive umask. The test binary
// never runs main, so this pins the chmod in sqlitepool.OpenWriter on the real path.
//
// syscall.Umask is process-global, so this test must not be t.Parallel(): Go runs every
// serial test before it resumes the parallel ones, which keeps the umask from reaching them.
func TestCacheFileIsOwnerOnly(t *testing.T) {
	prev := syscall.Umask(0o022)
	defer syscall.Umask(prev)

	dir := t.TempDir()
	m := NewManager(dir, Retention{})
	t.Cleanup(func() { require.NoError(t, m.Close()) })

	store, err := m.OpenOrCreate(1)
	require.NoError(t, err)
	defer store.Release()

	fi, err := os.Stat(filepath.Join(dir, "1.db"))
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), fi.Mode().Perm())
}

// A sidecar that will not stat is not an absent one. Only absence is zero; any other
// failure has to surface, or a transient error would undercount an over-limit cache and
// the sweep would publish a release. A symlink to itself is a stat that fails with ELOOP
// rather than ENOENT.
func TestStatDiskUsageReportsASidecarItCannotStat(t *testing.T) {
	t.Parallel()

	for _, sidecar := range []string{"-wal", "-shm"} {
		t.Run(sidecar, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			path := filepath.Join(dir, "1.db")
			require.NoError(t, os.WriteFile(path, []byte("db"), 0o600))
			require.NoError(t, os.Symlink(path+sidecar, path+sidecar))

			_, err := statDiskUsage(path)

			require.Error(t, err)
			assert.False(t, os.IsNotExist(err), "a failure, not an absence: %v", err)
		})
	}
}
