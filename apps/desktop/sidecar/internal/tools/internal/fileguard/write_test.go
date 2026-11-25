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

package fileguard

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// names is what dir holds, by name.
func names(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	var out []string
	for _, e := range entries {
		out = append(out, e.Name())
	}
	return out
}

// A replace puts the bytes as given at the path, and no temporary file is left.
func TestReplacePutsTheBytesAsGiven(t *testing.T) {
	path := filepath.Join(t.TempDir(), "x.txt")
	require.NoError(t, os.WriteFile(path, []byte("old\n"), 0o600))
	info, err := os.Lstat(path)
	require.NoError(t, err)

	require.NoError(t, Replace(t.Context(), path, []byte("new\r\n"), info))

	b, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, "new\r\n", string(b), "the bytes as given")
	assert.Equal(t, []string{"x.txt"}, names(t, filepath.Dir(path)))
}

// A new file is made with every directory missing on its way, holding the
// bytes as given.
func TestCreateMakesAFileAndItsParents(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "a", "b", "x.txt")

	require.NoError(t, Create(t.Context(), path, []byte("x\r\n"), 0o022))
	b, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, "x\r\n", string(b))
	assert.Equal(t, []string{"x.txt"}, names(t, filepath.Join(root, "a", "b")))

	require.NoError(t, Create(t.Context(), filepath.Join(root, "y.txt"), nil, 0o022))
	b, err = os.ReadFile(filepath.Join(root, "y.txt"))
	require.NoError(t, err)
	assert.Empty(t, b)
}

// endsAfter is a context whose Err reports it ended from its nth call on, so a
// test cancels between two of a write's checks.
type endsAfter struct {
	context.Context
	n int
}

func (c *endsAfter) Err() error {
	c.n--
	if c.n < 0 {
		return context.Canceled
	}
	return nil
}

// A write whose context ends leaves nothing behind: no directory Create made,
// no temporary file, and no file at the path.
func TestReplaceAndCreateLeaveNothingBehindOnACancel(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "a", "b", "x.txt")
	ended, cancel := context.WithCancel(t.Context())
	cancel()

	assert.ErrorIs(t, Create(ended, path, []byte("x"), 0o022), ErrCancelled, "before the first directory")
	assert.Empty(t, names(t, root))
	assert.ErrorIs(t, Create(&endsAfter{Context: t.Context(), n: 1}, path, []byte("x"), 0o022), ErrCancelled, "before the rename")
	assert.Empty(t, names(t, root))

	old := filepath.Join(root, "old.txt")
	require.NoError(t, os.WriteFile(old, []byte("old\n"), 0o600))
	info, err := os.Lstat(old)
	require.NoError(t, err)
	assert.ErrorIs(t, Replace(ended, old, []byte("new\n"), info), ErrCancelled)
	assert.Equal(t, []string{"old.txt"}, names(t, root))
	b, err := os.ReadFile(old)
	require.NoError(t, err)
	assert.Equal(t, "old\n", string(b))
}

// A name at the filesystem's limit still gets a temporary file beside it: its
// cut, on a rune boundary, leaves room for the dot and the suffix.
func TestCreateCutsALongName(t *testing.T) {
	for _, base := range []string{strings.Repeat("a", 251) + ".txt", strings.Repeat("é", 126) + "abc"} {
		require.Len(t, base, 255)
		dir := t.TempDir()
		require.NoError(t, Create(t.Context(), filepath.Join(dir, base), []byte("x"), 0o022))
		assert.Equal(t, []string{base}, names(t, dir))
	}
	assert.Equal(t, strings.Repeat("é", 100), cutName(strings.Repeat("é", 126)+"abc"))
	assert.Equal(t, strings.Repeat("é", 99), cutName("a" + strings.Repeat("é", 126))[1:])
}

// A rename that fails is ErrReplace, and takes its temporary file with it.
func TestAFailedReplaceLeavesNothingBehind(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "x.txt")
	require.NoError(t, os.WriteFile(path, []byte("old\n"), 0o600))
	info, err := os.Lstat(path)
	require.NoError(t, err)
	require.NoError(t, os.Remove(path))
	require.NoError(t, os.MkdirAll(filepath.Join(path, "full"), 0o700))

	assert.ErrorIs(t, Replace(t.Context(), path, []byte("new\n"), info), ErrReplace)
	assert.Equal(t, []string{"x.txt"}, names(t, dir))
}

// existingIn is a file of the test's own under a root, 0600, and its info.
func existingIn(t *testing.T) (*os.Root, string, os.FileInfo) {
	t.Helper()
	root := testRoot(t)
	require.NoError(t, root.Mkdir("a", 0o700))
	name := filepath.Join("a", "x.txt")
	require.NoError(t, root.WriteFile(name, []byte("old\n"), 0o600))
	info, err := root.Lstat(name)
	require.NoError(t, err)
	return root, name, info
}

// ReplaceIn puts the bytes as given at the name by a rename, and leaves no
// temporary file.
func TestReplaceInReplacesWhole(t *testing.T) {
	root, name, info := existingIn(t)

	require.NoError(t, ReplaceIn(t.Context(), root, name, []byte("new\r\n"), info))

	b, err := root.ReadFile(name)
	require.NoError(t, err)
	assert.Equal(t, "new\r\n", string(b))
	assert.Equal(t, []string{"x.txt"}, names(t, filepath.Join(root.Name(), "a")))
}

// A replace whose context ended changes nothing and leaves nothing behind.
func TestReplaceInLeavesNothingBehindOnACancel(t *testing.T) {
	root, name, info := existingIn(t)
	ended, cancel := context.WithCancel(t.Context())
	cancel()

	assert.ErrorIs(t, ReplaceIn(ended, root, name, []byte("new\n"), info), ErrCancelled)
	b, err := root.ReadFile(name)
	require.NoError(t, err)
	assert.Equal(t, "old\n", string(b))
	assert.Equal(t, []string{"x.txt"}, names(t, filepath.Join(root.Name(), "a")))
}

// A rename that fails is ErrReplace, and takes its temporary file with it.
func TestAFailedReplaceInLeavesNothingBehind(t *testing.T) {
	root, name, info := existingIn(t)
	require.NoError(t, root.Remove(name))
	require.NoError(t, root.MkdirAll(filepath.Join(name, "full"), 0o700))

	assert.ErrorIs(t, ReplaceIn(t.Context(), root, name, []byte("new\n"), info), ErrReplace)
	assert.Equal(t, []string{"x.txt"}, names(t, filepath.Join(root.Name(), "a")))
}

// CreateIn makes the file and every directory missing on its way, holding the
// bytes as given.
func TestCreateInMakesAFileAndItsDirectories(t *testing.T) {
	root := testRoot(t)
	name := filepath.Join("a", "b", "x.txt")

	require.NoError(t, CreateIn(t.Context(), root, name, []byte("x\r\n"), 0o022))
	b, err := root.ReadFile(name)
	require.NoError(t, err)
	assert.Equal(t, "x\r\n", string(b))
	assert.Equal(t, []string{"x.txt"}, names(t, filepath.Join(root.Name(), "a", "b")))

	require.NoError(t, CreateIn(t.Context(), root, "y.txt", nil, 0o022))
	b, err = root.ReadFile("y.txt")
	require.NoError(t, err)
	assert.Empty(t, b)
}

// A create whose context ends leaves nothing behind: no directory, no
// temporary file, no file.
func TestCreateInLeavesNothingBehindOnACancel(t *testing.T) {
	root := testRoot(t)
	name := filepath.Join("a", "b", "x.txt")
	ended, cancel := context.WithCancel(t.Context())
	cancel()

	assert.ErrorIs(t, CreateIn(ended, root, name, []byte("x"), 0o022), ErrCancelled, "before the first directory")
	assert.Empty(t, names(t, root.Name()))
	assert.ErrorIs(t, CreateIn(&endsAfter{Context: t.Context(), n: 1}, root, name, []byte("x"), 0o022), ErrCancelled, "before the link")
	assert.Empty(t, names(t, root.Name()))
}
