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

package rootdir

import (
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/testutil"
)

// A directory Open makes is owner-only whatever the umask.
func TestOpenMakesItOwnerOnly(t *testing.T) {
	old := syscall.Umask(0)
	defer syscall.Umask(old)
	parent, dir := openTestRoot(t)

	root, err := Open(parent, "d", true)
	require.NoError(t, err)
	root.Close()

	info, err := os.Stat(filepath.Join(dir, "d"))
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o700), info.Mode().Perm())
}

// A link is refused whatever it points at, inside the parent or out.
func TestOpenRefusesALink(t *testing.T) {
	for name, target := range map[string]string{"outside": t.TempDir(), "inside": "other"} {
		t.Run(name, func(t *testing.T) {
			parent, dir := openTestRoot(t)
			require.NoError(t, os.Mkdir(filepath.Join(dir, "other"), 0o700))
			require.NoError(t, os.Symlink(target, filepath.Join(dir, "d")))

			for _, create := range []bool{false, true} {
				_, err := Open(parent, "d", create)
				assert.ErrorIs(t, err, ErrNotADirectory, "create %v", create)
			}
		})
	}
}

// A link swapped in for a directory after it was checked is refused at the
// open, even one that stays inside the parent, which OpenRoot would follow.
func TestOpenRefusesALinkSwappedInAfterTheCheck(t *testing.T) {
	parent, _ := openTestRoot(t)
	require.NoError(t, parent.Mkdir("c1", 0o700))
	require.NoError(t, parent.Mkdir("c2", 0o700))
	info, err := parent.Lstat("c1")
	require.NoError(t, err)

	require.NoError(t, parent.Remove("c1"))
	require.NoError(t, parent.Symlink("c2", "c1"))

	_, err = openSameDir(parent, "c1", info)
	assert.ErrorIs(t, err, ErrNotADirectory)
}

// A directory its owner cannot read is found but cannot be opened, and Open says so.
func TestOpenRefusesADirectoryItCannotRead(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root reads a directory whatever its mode")
	}
	parent, dir := openTestRoot(t)
	d := filepath.Join(dir, "d")
	require.NoError(t, os.Mkdir(d, 0))
	t.Cleanup(func() { _ = os.Chmod(d, 0o700) })

	_, err := Open(parent, "d", false)
	assert.ErrorIs(t, err, fs.ErrPermission)
}

// A command can leave a directory its owner cannot write, such as a Go module
// cache. RemoveAll still takes the tree.
func TestRemoveAllTakesAnUnwritableTree(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root removes a read-only directory's entries")
	}
	parent, dir := openTestRoot(t)
	cache := filepath.Join(dir, "d", "cache")
	require.NoError(t, os.MkdirAll(cache, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(cache, "mod"), nil, 0o400))
	require.NoError(t, os.Chmod(cache, 0o500))
	require.NoError(t, os.Chmod(filepath.Join(dir, "d"), 0o500))

	require.NoError(t, RemoveAll(parent, "d"))
	assert.NoDirExists(t, filepath.Join(dir, "d"))
}

// MakeRoot makes the directory and each parent it lacks owner-only, whatever
// the umask.
func TestMakeRootMakesItOwnerOnly(t *testing.T) {
	old := syscall.Umask(0)
	defer syscall.Umask(old)
	parent := filepath.Join(t.TempDir(), "a")
	dir := filepath.Join(parent, "b")

	root, err := MakeRoot(dir)
	require.NoError(t, err)
	require.NoError(t, root.Close())

	for _, d := range []string{parent, dir} {
		info, err := os.Stat(d)
		require.NoError(t, err)
		assert.Equal(t, os.FileMode(0o700), info.Mode().Perm(), d)
	}
}

// An entry that will not go is logged and left, and the sweep goes on.
func TestSweepLeavesWhatWillNotGo(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root removes a read-only directory's entries")
	}
	root, dir := openTestRoot(t)
	require.NoError(t, os.Mkdir(filepath.Join(dir, "stuck"), 0o700))
	entries, err := fs.ReadDir(root.FS(), ".")
	require.NoError(t, err)
	require.NoError(t, os.Chmod(dir, 0o500))
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })
	logs := testutil.CaptureLogs(t)

	Sweep(root, entries, func(fs.DirEntry) bool { return false })

	assert.DirExists(t, filepath.Join(dir, "stuck"))
	assert.Contains(t, logs.String(), "could not remove a swept entry")
}

// A link in the tree is removed and its target left alone, mode included: the
// walk that makes a tree removable never follows one.
func TestRemoveAllRemovesALinkNotItsTarget(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root removes a read-only directory's entries")
	}
	parent, dir := openTestRoot(t)
	outside := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(outside, "keep"), nil, 0o600))
	require.NoError(t, os.Chmod(outside, 0o500))
	t.Cleanup(func() { _ = os.Chmod(outside, 0o700) })
	tree := filepath.Join(dir, "d")
	require.NoError(t, os.Mkdir(tree, 0o700))
	require.NoError(t, os.Symlink(outside, filepath.Join(tree, "out")))
	// Read-only, so the first removal fails and the walk runs.
	require.NoError(t, os.Chmod(tree, 0o500))

	require.NoError(t, RemoveAll(parent, "d"))

	assert.NoDirExists(t, tree)
	assert.FileExists(t, filepath.Join(outside, "keep"))
	info, err := os.Stat(outside)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o500), info.Mode().Perm())
}

// A name that is itself a link is removed, never walked into.
func TestRemoveAllOfALinkLeavesItsTarget(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root removes a read-only directory's entries")
	}
	parent, dir := openTestRoot(t)
	require.NoError(t, os.Mkdir(filepath.Join(dir, "other"), 0o700))
	locked := filepath.Join(dir, "other", "locked")
	require.NoError(t, os.Mkdir(locked, 0o500))
	t.Cleanup(func() { _ = os.Chmod(locked, 0o700) })
	require.NoError(t, os.Symlink("other", filepath.Join(dir, "d")))

	require.NoError(t, RemoveAll(parent, "d"))

	assert.NoFileExists(t, filepath.Join(dir, "d"))
	info, err := os.Stat(locked)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o500), info.Mode().Perm())
}
