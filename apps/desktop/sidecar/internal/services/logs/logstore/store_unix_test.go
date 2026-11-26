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

package logstore

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/sqlitepool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A log holds whatever the cluster wrote, so the file and its directory are owner-only
// whatever umask the sidecar started with. Not parallel: the umask is process-wide.
func TestALogFileIsOwnerOnly(t *testing.T) {
	prev := syscall.Umask(0o022)
	defer syscall.Umask(prev)
	dir := filepath.Join(t.TempDir(), "logs")

	f, err := openFile(filepath.Join(dir, "1.db"), sqlitepool.CloseBudget)
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, f.close()) })

	fi, err := os.Stat(filepath.Join(dir, "1.db"))
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), fi.Mode().Perm())
	di, err := os.Stat(dir)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o700), di.Mode().Perm())
}
