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
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/tools"
)

// dataDir is a data directory of the test's own, and the fence around it.
func dataDir(t *testing.T) (string, Fence) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "data")
	require.NoError(t, os.Mkdir(dir, 0o700))
	f, err := NewFence(dir)
	require.NoError(t, err)
	return dir, f
}

// A fence around several directories holds each of them, by name and on
// disk, and nothing beside them.
func TestAFenceHoldsEachOfItsDirectories(t *testing.T) {
	base := t.TempDir()
	var dirs []string
	for _, name := range []string{"data", "cache", "run"} {
		dir := filepath.Join(base, name)
		require.NoError(t, os.Mkdir(dir, 0o700))
		dirs = append(dirs, dir)
	}
	f, err := NewFence(dirs...)
	require.NoError(t, err)

	for _, dir := range dirs {
		path := filepath.Join(dir, "x", "y.txt")
		assert.True(t, f.Named(path), path)
		held, err := f.Holds(path)
		require.NoError(t, err)
		assert.True(t, held, path)
	}
	outside := filepath.Join(base, "other", "y.txt")
	assert.False(t, f.Named(outside))
	held, err := f.Holds(outside)
	require.NoError(t, err)
	assert.False(t, held)
}

// Named is a comparison of names alone: the directory and what is under it,
// never a sibling that shares its prefix.
func TestFenceNamedIsByName(t *testing.T) {
	dir, f := dataDir(t)

	assert.True(t, f.Named(dir))
	assert.True(t, f.Named(filepath.Join(dir, "app.db")))
	assert.True(t, f.Named(filepath.Join(dir, "chats", "c1", "missing.txt")))
	assert.False(t, f.Named(dir+"-other"))
	assert.False(t, f.Named(filepath.Dir(dir)))
}

// otherCase is a fence over a data directory and a file under it spelled in
// another case. The volume decides whether that is the same directory, not the
// platform, so a case-sensitive one skips.
func otherCase(t *testing.T) (Fence, string) {
	t.Helper()
	dir, f := dataDir(t)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "probe"), nil, 0o600))
	if _, err := os.Stat(filepath.Join(dir, "PROBE")); err != nil {
		t.Skip("the temp volume is case-sensitive")
	}
	return f, filepath.Join(filepath.Dir(dir), strings.ToUpper(filepath.Base(dir)), "app.db")
}

// On a case-insensitive volume the data directory spelled in another case is
// the same directory, and Holds catches it by identity.
func TestFenceHoldsTheDataDirInAnotherCase(t *testing.T) {
	f, upper := otherCase(t)

	held, err := f.Holds(upper)
	require.NoError(t, err)
	assert.True(t, held)
}

// A sibling of the data directory, and a file missing under it, are not held.
func TestFenceDoesNotHoldASibling(t *testing.T) {
	dir, f := dataDir(t)
	sibling := dir + "-other"
	require.NoError(t, os.Mkdir(sibling, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(sibling, "x.txt"), []byte("x"), 0o600))

	for _, p := range []string{sibling, filepath.Join(sibling, "x.txt"), filepath.Join(sibling, "a", "b.txt")} {
		held, err := f.Holds(p)
		require.NoError(t, err, p)
		assert.False(t, held, p)
	}

	held, err := f.Holds(filepath.Join(dir, "missing", "x.txt"))
	require.NoError(t, err)
	assert.True(t, held, "the data directory itself")
}

// A regular file within the limit opens, and ReadAll is the whole of it.
func TestOpenReadsARegularFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "x.txt")
	require.NoError(t, os.WriteFile(path, []byte("a\nb\n"), 0o600))

	f, err := Open(path)
	require.NoError(t, err)
	defer f.Close()
	b, err := ReadAll(f)
	require.NoError(t, err)
	assert.Equal(t, "a\nb\n", string(b))
}

func TestOpenRefusesAMissingFile(t *testing.T) {
	_, err := Open(filepath.Join(t.TempDir(), "missing.txt"))
	assert.ErrorIs(t, err, ErrMissing)
	_, err = Open(filepath.Join(t.TempDir(), "missing", "x.txt"))
	assert.ErrorIs(t, err, ErrMissing)
}

func TestOpenRefusesADirectory(t *testing.T) {
	_, err := Open(t.TempDir())
	assert.ErrorIs(t, err, ErrDirectory)
}

func TestOpenRefusesAFileOverTheLimit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "big.txt")
	big, err := os.Create(path)
	require.NoError(t, err)
	require.NoError(t, big.Truncate(tools.FileLimit+1))
	require.NoError(t, big.Close())

	_, err = Open(path)
	assert.ErrorIs(t, err, ErrTooLarge)
}

// Lstat classifies the path itself by its type alone: a file past the limit is
// still a file, for a caller that replaces it to refuse in its own words.
func TestLstatSaysWhatThePathIs(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "big.txt")
	big, err := os.Create(path)
	require.NoError(t, err)
	require.NoError(t, big.Truncate(tools.FileLimit+1))
	require.NoError(t, big.Close())

	info, err := Lstat(path)
	require.NoError(t, err)
	assert.Equal(t, int64(tools.FileLimit+1), info.Size())

	_, err = Lstat(dir)
	assert.ErrorIs(t, err, ErrDirectory)
	_, err = Lstat(filepath.Join(dir, "missing.txt"))
	assert.ErrorIs(t, err, ErrMissing)
	_, err = Lstat(filepath.Join(path, "x.txt"))
	assert.ErrorIs(t, err, ErrMissing, "a file in the way is absence")
}

// A writer can keep appending after the Stat, so ReadAll stops past the limit.
func TestReadAllRefusesAFileThatGrewPastTheLimit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "x.txt")
	require.NoError(t, os.WriteFile(path, []byte("a"), 0o600))
	f, err := Open(path)
	require.NoError(t, err)
	defer f.Close()
	grown, err := os.OpenFile(path, os.O_WRONLY, 0)
	require.NoError(t, err)
	require.NoError(t, grown.Truncate(tools.FileLimit+1))
	require.NoError(t, grown.Close())

	_, err = ReadAll(f)
	assert.ErrorIs(t, err, ErrTooLarge)
}

// A read that fails is the failure, never a partial file.
func TestReadAllPassesOnAFailedRead(t *testing.T) {
	path := filepath.Join(t.TempDir(), "x.txt")
	require.NoError(t, os.WriteFile(path, []byte("a"), 0o600))
	f, err := Open(path)
	require.NoError(t, err)
	require.NoError(t, f.Close())

	b, err := ReadAll(f)
	assert.ErrorIs(t, err, os.ErrClosed)
	assert.Nil(t, b)
}

// A NUL in the first 8 KiB is binary; one past it is not looked for.
func TestBinarySeesANulInTheFirst8KiB(t *testing.T) {
	assert.False(t, Binary(nil))
	assert.False(t, Binary([]byte("plain text\n")))
	assert.True(t, Binary([]byte("a\x00b")))
	assert.True(t, Binary([]byte(strings.Repeat("a", 8<<10-1)+"\x00")))
	assert.False(t, Binary([]byte(strings.Repeat("a", 8<<10)+"\x00")))
}

// A fence around no directory would hold nothing, so it is refused.
func TestAFenceNeedsADirectory(t *testing.T) {
	_, err := NewFence()
	assert.ErrorIs(t, err, ErrNoFence)
}

// A fenced directory the OS or the user removed and made again is the one
// Holds compares with, so a path reaching it another way is still held.
func TestHoldsSeesAFencedDirectoryMadeAgain(t *testing.T) {
	dir, f := dataDir(t)
	require.NoError(t, os.Remove(dir))
	require.NoError(t, os.Mkdir(dir, 0o700))
	link := filepath.Join(t.TempDir(), "link")
	require.NoError(t, os.Symlink(dir, link))

	held, err := f.Holds(filepath.Join(link, "x.txt"))

	require.NoError(t, err)
	assert.True(t, held)
}

// A fenced directory that is missing is made again, owner-only, so a path
// spelled another way cannot make it through a write while the fence
// compares with nothing.
func TestHoldsMakesAMissingFencedDirectory(t *testing.T) {
	dir, f := dataDir(t)
	require.NoError(t, os.Remove(dir))
	parent := filepath.Join(t.TempDir(), "parent")
	require.NoError(t, os.Symlink(filepath.Dir(dir), parent))

	held, err := f.Holds(filepath.Join(parent, filepath.Base(dir), "x.txt"))

	require.NoError(t, err)
	assert.True(t, held)
	info, err := os.Stat(dir)
	require.NoError(t, err)
	assert.True(t, info.IsDir())
}

// A fenced directory that can be neither read nor made is left out, and the
// fence goes on holding the others: a path elsewhere is not refused over it.
func TestHoldsLeavesOutAFencedDirectoryItCannotMake(t *testing.T) {
	file := filepath.Join(t.TempDir(), "file")
	require.NoError(t, os.WriteFile(file, nil, 0o600))
	data := t.TempDir()
	f, err := NewFence(filepath.Join(file, "run"), data)
	require.NoError(t, err)

	held, err := f.Holds(filepath.Join(t.TempDir(), "x.txt"))
	require.NoError(t, err)
	assert.False(t, held, "a path elsewhere")

	held, err = f.Holds(filepath.Join(data, "x.txt"))
	require.NoError(t, err)
	assert.True(t, held, "a path in a directory still fenced")
}

// A path the request cannot draw whole on one line is refused before anything
// else is judged: a newline, any other control character, or more than MaxPath
// bytes.
func TestAbsRefusesAPathThatIsNotOneLine(t *testing.T) {
	root := string(filepath.Separator)
	long := root + strings.Repeat("a", MaxPath)
	for _, p := range []string{root + "a\n" + root + "b", root + "a\tb", root + "a\rb", root + "a\x00b", root + "a\u0085b", "a\nb", long} {
		_, err := Abs(p)
		assert.ErrorIs(t, err, ErrNotOneLine, p)
	}

	_, err := Abs(long[:MaxPath])
	assert.NotErrorIs(t, err, ErrNotOneLine, "MaxPath bytes is one line")
}

// A path that is not absolute is refused, "~" included: Kubetail has no working
// directory to read it against.
func TestAbsRefusesARelativePath(t *testing.T) {
	for _, p := range []string{"a.txt", "./a.txt", "~/a.txt", "~", ""} {
		_, err := Abs(p)
		assert.ErrorIs(t, err, ErrNotAbs, p)
	}
}

// testRoot is a directory of the test's own, opened as a root.
func testRoot(t *testing.T) *os.Root {
	t.Helper()
	root, err := os.OpenRoot(t.TempDir())
	require.NoError(t, err)
	t.Cleanup(func() { root.Close() })
	return root
}

// LstatIn classifies the name as Lstat classifies a path.
func TestLstatInSaysWhatTheNameIs(t *testing.T) {
	root := testRoot(t)
	require.NoError(t, root.Mkdir("dir", 0o700))
	big, err := root.Create("big.txt")
	require.NoError(t, err)
	require.NoError(t, big.Truncate(tools.FileLimit+1))
	require.NoError(t, big.Close())

	info, err := LstatIn(root, "big.txt")
	require.NoError(t, err)
	assert.Equal(t, int64(tools.FileLimit+1), info.Size())

	_, err = LstatIn(root, "dir")
	assert.ErrorIs(t, err, ErrDirectory)
	_, err = LstatIn(root, "missing.txt")
	assert.ErrorIs(t, err, ErrMissing)
	_, err = LstatIn(root, filepath.Join("big.txt", "x.txt"))
	assert.ErrorIs(t, err, ErrMissing, "a file in the way is absence")
}

// OpenIn opens what Open opens, and refuses what Open refuses.
func TestOpenInReadsARegularFile(t *testing.T) {
	root := testRoot(t)
	require.NoError(t, root.MkdirAll("a", 0o700))
	require.NoError(t, root.WriteFile(filepath.Join("a", "x.txt"), []byte("a\nb\n"), 0o600))
	big, err := root.Create("big.txt")
	require.NoError(t, err)
	require.NoError(t, big.Truncate(tools.FileLimit+1))
	require.NoError(t, big.Close())

	f, err := OpenIn(root, filepath.Join("a", "x.txt"))
	require.NoError(t, err)
	b, err := ReadAll(f)
	require.NoError(t, err)
	require.NoError(t, f.Close())
	assert.Equal(t, "a\nb\n", string(b))

	_, err = OpenIn(root, "missing.txt")
	assert.ErrorIs(t, err, ErrMissing)
	_, err = OpenIn(root, "a")
	assert.ErrorIs(t, err, ErrDirectory)
	_, err = OpenIn(root, "big.txt")
	assert.ErrorIs(t, err, ErrTooLarge)
}

// A link's target cannot be read off a name that is no link, as when the link
// is swapped for a file after its Lstat: the error passes on.
func TestALinkTargetOfAFileIsAnError(t *testing.T) {
	root := testRoot(t)
	require.NoError(t, root.WriteFile("x.txt", nil, 0o600))

	_, err := linkTarget(filepath.Join(root.Name(), "x.txt"))
	assert.Error(t, err)
	_, err = linkTargetIn(root, "x.txt")
	assert.Error(t, err)
}
