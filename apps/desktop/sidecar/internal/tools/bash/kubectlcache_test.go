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

package bash

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/apimeta"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/testutil"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/cluster"
)

// A new server identity gets a directory of its own under the cluster's, and
// a record with none yet is unknown.
func TestEachServerIdentityHasItsOwnCache(t *testing.T) {
	dir := t.TempDir()

	a, err := makeKubectlCache(dir, "7", "uid-a")
	require.NoError(t, err)
	b, err := makeKubectlCache(dir, "7", "uid-b")
	require.NoError(t, err)
	none, err := makeKubectlCache(dir, "7", "")
	require.NoError(t, err)

	assert.Equal(t, filepath.Join(dir, "7", serverKey("uid-a")), a)
	assert.NotEqual(t, a, b)
	assert.Equal(t, filepath.Join(dir, "7", unknownServer), none)
	for _, path := range []string{a, b, none} {
		assert.DirExists(t, path)
	}
}

// The OS or the user may clear the cache directory while Kubetail runs, and the
// next call makes the kubectl cache again.
func TestAClearedKubectlCacheIsMadeAgain(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "kubectl")
	_, err := makeKubectlCache(dir, "7", "uid")
	require.NoError(t, err)

	require.NoError(t, os.RemoveAll(dir))
	path, err := makeKubectlCache(dir, "7", "uid")

	require.NoError(t, err)
	assert.DirExists(t, path)
}

// An id that is not one plain name would reach another directory than the
// cluster's, so it is refused before anything is made.
func TestABadClusterIDHasNoKubectlCache(t *testing.T) {
	dir := t.TempDir()
	for _, id := range []apimeta.ClusterID{"", ".", "..", "a/b"} {
		_, err := makeKubectlCache(dir, id, "uid")
		assert.Error(t, err, "%q", id)
	}
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	assert.Empty(t, entries)
}

// A file where the cluster's directory or its server's should be is refused,
// not replaced.
func TestAKubectlCacheThatCannotBeMadeIsAnError(t *testing.T) {
	dir := t.TempDir()
	cluster := filepath.Join(dir, "7")

	require.NoError(t, os.WriteFile(cluster, nil, 0o600))
	_, err := makeKubectlCache(dir, "7", "uid")
	assert.Error(t, err, "a file for the cluster")

	require.NoError(t, os.Remove(cluster))
	require.NoError(t, os.Mkdir(cluster, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(cluster, serverKey("uid")), nil, 0o600))
	_, err = makeKubectlCache(dir, "7", "uid")
	assert.Error(t, err, "a file for the server")
}

// The UID is the cluster's text, so it names the directory only through its
// hash: 32 hex digits whatever it holds.
func TestTheServerKeyIsAHash(t *testing.T) {
	for _, uid := range []string{"../../etc", "a/b", "x"} {
		assert.Regexp(t, `^[0-9a-f]{32}$`, serverKey(uid), "%q", uid)
	}
}

// The sweep removes every entry that names no cluster the service holds, and
// keeps a marked cluster's, since its chats may still be running commands.
func TestTheSweepRemovesTheCacheOfAGoneCluster(t *testing.T) {
	dir := t.TempDir()
	marked := kubeCluster("staging", "uid")
	at := time.Now()
	marked.DeletionRequestedAt = &at
	svc := fakeService{clusters: map[apimeta.ClusterID]*cluster.Cluster{"7": kubeCluster("prod", "uid"), "8": marked}}
	for _, id := range []apimeta.ClusterID{"7", "8", "9"} {
		_, err := makeKubectlCache(dir, id, "uid")
		require.NoError(t, err)
	}
	require.NoError(t, os.WriteFile(filepath.Join(dir, "stray"), nil, 0o600))

	sweepKubectlCache(t.Context(), dir, svc)

	assert.ElementsMatch(t, []string{"7", "8"}, entryNames(t, dir))
}

// A sweep that cannot read the clusters removes nothing, and one with no
// cache to sweep does nothing.
func TestTheSweepRemovesNothingItCannotCheck(t *testing.T) {
	dir := t.TempDir()
	_, err := makeKubectlCache(dir, "7", "uid")
	require.NoError(t, err)
	logs := testutil.CaptureLogs(t)

	sweepKubectlCache(t.Context(), dir, fakeService{err: errors.New("disk")})
	sweepKubectlCache(t.Context(), filepath.Join(dir, "missing"), fakeService{})

	assert.Equal(t, []string{"7"}, entryNames(t, dir))
	assert.Contains(t, logs.String(), "could not read the clusters to sweep the kubectl cache")
}

// A cache that is not a folder is left as it is, and the clusters are never
// read.
func TestTheSweepLeavesACacheItCannotOpen(t *testing.T) {
	file := filepath.Join(t.TempDir(), "kubectl")
	require.NoError(t, os.WriteFile(file, nil, 0o600))
	logs := testutil.CaptureLogs(t)

	sweepKubectlCache(t.Context(), file, fakeService{err: errors.New("never read")})

	assert.FileExists(t, file)
	assert.Contains(t, logs.String(), "could not open the kubectl cache to sweep it")
	assert.NotContains(t, logs.String(), "could not read the clusters")
}

// New sweeps the kubectl cache as it starts, on a goroutine of its own.
func TestNewSweepsTheKubectlCache(t *testing.T) {
	t.Setenv("SHELL", "")
	dir := t.TempDir()
	_, err := makeKubectlCache(dir, "9", "uid")
	require.NoError(t, err)

	if _, ok := New(Paths{ShellDir: t.TempDir(), RunsDir: t.TempDir(), TmpDir: t.TempDir(), KubectlDir: dir}, 0, nil, fakeService{}, nil, nil, Cloud{}); !ok {
		t.Skip("no bash found on this machine")
	}

	require.Eventually(t, func() bool {
		_, err := os.Stat(filepath.Join(dir, "9"))
		return os.IsNotExist(err)
	}, testutil.Timeout, time.Millisecond, "the gone cluster's cache goes")
}

// entryNames is every entry of dir, by name.
func entryNames(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}
