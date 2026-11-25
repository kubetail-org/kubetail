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

package rootdir

import (
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func openTestRoot(t *testing.T) (*os.Root, string) {
	t.Helper()
	dir := t.TempDir()
	root, err := os.OpenRoot(dir)
	require.NoError(t, err)
	t.Cleanup(func() { _ = root.Close() })
	return root, dir
}

// Open with create makes the directory, owner-only, and opens it.
func TestOpenMakesIt(t *testing.T) {
	parent, dir := openTestRoot(t)

	root, err := Open(parent, "d", true)
	require.NoError(t, err)
	defer root.Close()

	require.NoError(t, root.WriteFile("x", []byte("x"), 0o600))
	assert.FileExists(t, filepath.Join(dir, "d", "x"))
}

// Without create, a missing directory is fs.ErrNotExist, and nothing is made.
func TestOpenWithoutCreateFindsNone(t *testing.T) {
	parent, dir := openTestRoot(t)

	_, err := Open(parent, "d", false)
	assert.ErrorIs(t, err, fs.ErrNotExist)
	assert.NoDirExists(t, filepath.Join(dir, "d"))
}

// A file where the directory should be is refused, not replaced.
func TestOpenRefusesAFile(t *testing.T) {
	parent, dir := openTestRoot(t)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "d"), nil, 0o600))

	for _, create := range []bool{false, true} {
		_, err := Open(parent, "d", create)
		assert.ErrorIs(t, err, ErrNotADirectory, "create %v", create)
	}
}

// A name that is not there is already removed.
func TestRemoveAllOfAMissingNameIsNil(t *testing.T) {
	parent, _ := openTestRoot(t)

	assert.NoError(t, RemoveAll(parent, "gone"))
}

// MakeRoot makes the directory and every parent it lacks, then opens it.
func TestMakeRootMakesItsParents(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "a", "b")

	root, err := MakeRoot(dir)
	require.NoError(t, err)
	defer root.Close()

	require.NoError(t, root.WriteFile("x", []byte("x"), 0o600))
	assert.FileExists(t, filepath.Join(dir, "x"))
}

// A file where the directory should be cannot be made into one.
func TestMakeRootRefusesAFile(t *testing.T) {
	file := filepath.Join(t.TempDir(), "f")
	require.NoError(t, os.WriteFile(file, nil, 0o600))

	_, err := MakeRoot(file)
	assert.Error(t, err)
}

// Sweep removes what keep refuses, file or directory, and nothing else.
func TestSweepRemovesWhatKeepRefuses(t *testing.T) {
	root, dir := openTestRoot(t)
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "gone", "sub"), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "stray"), nil, 0o600))
	require.NoError(t, os.Mkdir(filepath.Join(dir, "kept"), 0o700))
	entries, err := fs.ReadDir(root.FS(), ".")
	require.NoError(t, err)

	Sweep(root, entries, func(e fs.DirEntry) bool { return e.Name() == "kept" })

	names, err := os.ReadDir(dir)
	require.NoError(t, err)
	require.Len(t, names, 1)
	assert.Equal(t, "kept", names[0].Name())
}
