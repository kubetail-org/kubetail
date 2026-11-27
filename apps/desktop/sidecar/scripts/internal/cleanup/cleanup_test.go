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

package cleanup

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/app/layout"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/filelock"
)

// testRoots are every root, under one fake top.
func testRoots() layout.Roots {
	top := filepath.FromSlash("/r")
	return layout.Roots{
		Config:    filepath.Join(top, "config"),
		LocalData: filepath.Join(top, "data"),
		Cache:     filepath.Join(top, "cache"),
		Home:      filepath.Join(top, "home"),
		State:     filepath.Join(top, "state"),
	}
}

// Each platform's top folders hold every other folder of its layout, and
// none holds another.
func TestTopDirsAreEachRootsOwnFolder(t *testing.T) {
	r := testRoots()
	j := filepath.Join
	cases := []struct {
		platform layout.Platform
		dev      bool
		want     []string
	}{
		{layout.Linux, false, []string{j(r.LocalData, "kubetail"), j(r.Cache, "kubetail"), j(r.Config, "kubetail"), j(r.State, "kubetail")}},
		{layout.Linux, true, []string{j(r.LocalData, "kubetail-dev"), j(r.Cache, "kubetail-dev"), j(r.Config, "kubetail-dev"), j(r.State, "kubetail-dev")}},
		{layout.MacOS, false, []string{j(r.Cache, "Kubetail"), j(r.Home, "Library", "Logs", "Kubetail"), j(r.LocalData, "Kubetail")}},
		{layout.Windows, false, []string{j(r.LocalData, "Kubetail"), j(r.Config, "Kubetail")}},
	}
	for _, c := range cases {
		t.Run(string(c.platform), func(t *testing.T) {
			assert.Equal(t, c.want, TopDirs(layout.Of(c.platform, c.dev, r)))
		})
	}
}

func TestUnderIsStrictlyBelowByName(t *testing.T) {
	parent := filepath.FromSlash("/a/b")
	assert.True(t, under(parent, filepath.FromSlash("/a/b/c")))
	assert.False(t, under(parent, parent))
	assert.False(t, under(parent, filepath.FromSlash("/a/b/../b")))
	assert.False(t, under(parent, filepath.FromSlash("/a/bc")))
	assert.False(t, under(parent, filepath.FromSlash("/a")))
}

func TestTheRuntimeDirIsUnderXDGWhenItIsADirectory(t *testing.T) {
	xdg := t.TempDir()
	dirs, err := runtimeDirs("kubetail", xdg, t.TempDir(), 501)
	require.NoError(t, err)
	assert.Equal(t, []string{filepath.Join(xdg, "kubetail")}, dirs)
}

// Without XDG's, the host takes <leaf>-<uid> under the temp dir, or a
// sibling with mkdtemp's suffix when that name was refused.
func TestTheRuntimeDirsUnderTheTempDirIncludeEachFreshSibling(t *testing.T) {
	temp := t.TempDir()
	base := filepath.Join(temp, "kubetail-501")
	require.NoError(t, os.Mkdir(base+"-abc123", 0o700))
	require.NoError(t, os.Mkdir(base+"-toolong1", 0o700))
	require.NoError(t, os.Mkdir(filepath.Join(temp, "kubetail-dev-501-abc123"), 0o700))

	dirs, err := runtimeDirs("kubetail", filepath.Join(temp, "missing"), temp, 501)
	require.NoError(t, err)
	assert.Equal(t, []string{base, base + "-abc123"}, dirs)
}

func TestARuntimeRootHoldingDotDotIsRefused(t *testing.T) {
	_, err := runtimeDirs("kubetail", "", "/tmp/../tmp", 501)
	assert.ErrorContains(t, err, "TMPDIR")
}

func TestNoLockIsNotRunning(t *testing.T) {
	assert.NoError(t, refuseWhileLocked(filepath.Join(t.TempDir(), lockFile)))
}

func TestAFreeLockIsNotRunning(t *testing.T) {
	path := filepath.Join(t.TempDir(), lockFile)
	require.NoError(t, os.WriteFile(path, nil, 0o600))
	assert.NoError(t, refuseWhileLocked(path))
}

func TestAHeldLockIsRunning(t *testing.T) {
	path := filepath.Join(t.TempDir(), lockFile)
	lock, err := filelock.TryLock(path)
	require.NoError(t, err)
	defer lock.Close()

	assert.ErrorContains(t, refuseWhileLocked(path), "quit Kubetail first")
}

// Opening it could make a file wherever it points, so it is never opened.
func TestALockThatIsNotAFileIsRefused(t *testing.T) {
	path := filepath.Join(t.TempDir(), lockFile)
	require.NoError(t, os.Mkdir(path, 0o700))
	assert.ErrorContains(t, refuseWhileLocked(path), "not a file")
}

func TestRemoveDeletesAFolderWithAReadOnlyOneInside(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "Kubetail")
	locked := filepath.Join(dir, "chats", "workspace")
	require.NoError(t, os.MkdirAll(locked, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(locked, "f"), nil, 0o600))
	require.NoError(t, os.Chmod(locked, 0o500))

	require.NoError(t, Remove(dir))
	assert.NoDirExists(t, dir)
	assert.NoError(t, Remove(dir), "a missing folder is already gone")
}
