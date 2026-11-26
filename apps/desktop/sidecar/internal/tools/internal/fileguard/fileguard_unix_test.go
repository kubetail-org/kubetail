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

package fileguard

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

// A link elsewhere that leads into the data directory is held, and so is the
// directory itself reached through it.
func TestFenceHoldsALinkIntoTheDataDir(t *testing.T) {
	dir, f := dataDir(t)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "app.db"), []byte("x"), 0o600))
	elsewhere := t.TempDir()
	link := filepath.Join(elsewhere, "l")
	require.NoError(t, os.Symlink(dir, link))
	require.NoError(t, os.Symlink(filepath.Join(dir, "app.db"), filepath.Join(elsewhere, "db")))

	for _, p := range []string{link, filepath.Join(link, "app.db"), filepath.Join(elsewhere, "db")} {
		held, err := f.Holds(p)
		require.NoError(t, err, p)
		assert.True(t, held, p)
		assert.False(t, f.Named(p), p)
	}
}

// A file that does not exist yet is judged by the directory it would land in.
func TestFenceHoldsAMissingFileUnderALinkIntoIt(t *testing.T) {
	dir, f := dataDir(t)
	link := filepath.Join(t.TempDir(), "l")
	require.NoError(t, os.Symlink(dir, link))

	held, err := f.Holds(filepath.Join(link, "new", "x.txt"))
	require.NoError(t, err)
	assert.True(t, held)
}

// A link in the last component is refused with where it leads, absolute and
// plain: a relative target is joined to the link's directory with that
// directory's own links resolved, so it names the file the link reaches.
func TestOpenRefusesALinkNamingItsTarget(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(filepath.Join(root, "a", "conf"), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(root, "a", "conf", "x.yaml"), []byte("x"), 0o600))
	require.NoError(t, os.Mkdir(filepath.Join(root, "a", "b"), 0o700))
	require.NoError(t, os.Symlink("../conf/x.yaml", filepath.Join(root, "a", "b", "rel")))
	require.NoError(t, os.Symlink(filepath.Join(root, "a", "conf", "x.yaml"), filepath.Join(root, "abs")))

	// p/q reached through the link lq: ../x from there is p/x, not the lexical
	// join's root/x.
	require.NoError(t, os.MkdirAll(filepath.Join(root, "p", "q"), 0o700))
	require.NoError(t, os.Symlink(filepath.Join(root, "p", "q"), filepath.Join(root, "lq")))
	require.NoError(t, os.Symlink("../x", filepath.Join(root, "p", "q", "l")))

	for path, target := range map[string]string{
		filepath.Join(root, "a", "b", "rel"): filepath.Join(root, "a", "conf", "x.yaml"),
		filepath.Join(root, "abs"):           filepath.Join(root, "a", "conf", "x.yaml"),
		filepath.Join(root, "lq", "l"):       filepath.Join(root, "p", "x"),
	} {
		_, err := Open(path)
		var link ErrLink
		require.ErrorAs(t, err, &link, path)
		assert.Equal(t, target, link.Target, path)
		assert.ErrorContains(t, link, target, path)
		got, err := Abs(link.Target)
		require.NoError(t, err, "the target is a path Abs takes")
		assert.Equal(t, link.Target, got)
	}
}

// A FIFO is refused by its Lstat, before any open: opening one for reading
// would wait for a writer.
func TestOpenRefusesAFIFOBeforeOpeningIt(t *testing.T) {
	fifo := filepath.Join(t.TempDir(), "fifo")
	require.NoError(t, syscall.Mkfifo(fifo, 0o600))

	var err error
	testutil.WaitReturn(t, func() { _, err = Open(fifo) }, "the FIFO's refusal")
	assert.ErrorIs(t, err, ErrNotRegular)
}

// Lstat refuses a link in the last component with its target, and a FIFO as
// not a regular file.
func TestLstatRefusesALinkAndAFIFO(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(root, "x.txt"), []byte("x"), 0o600))
	require.NoError(t, os.Symlink("x.txt", filepath.Join(root, "l")))
	require.NoError(t, syscall.Mkfifo(filepath.Join(root, "fifo"), 0o600))

	_, err = Lstat(filepath.Join(root, "l"))
	var link ErrLink
	require.ErrorAs(t, err, &link)
	assert.Equal(t, filepath.Join(root, "x.txt"), link.Target)
	_, err = Lstat(filepath.Join(root, "fifo"))
	assert.ErrorIs(t, err, ErrNotRegular)
}

// What Open cannot read it hands back as the system said it, so the caller
// can tell a permission from a missing file: a file it may not open, and one
// under a directory it may not search.
func TestOpenPassesOnWhatItCannotRead(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "locked.txt"), []byte("x"), 0))
	require.NoError(t, os.Mkdir(filepath.Join(dir, "shut"), 0))
	t.Cleanup(func() { _ = os.Chmod(filepath.Join(dir, "shut"), 0o700) })

	for _, p := range []string{filepath.Join(dir, "locked.txt"), filepath.Join(dir, "shut", "x.txt")} {
		_, err := Open(p)
		assert.ErrorIs(t, err, fs.ErrPermission, p)
	}
}

// A path whose parent cannot be read cannot be judged, so the caller refuses.
func TestFenceCannotTellPastAnUnreadableAncestor(t *testing.T) {
	_, f := dataDir(t)
	parent := filepath.Join(t.TempDir(), "locked")
	require.NoError(t, os.Mkdir(parent, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(parent, "x.txt"), []byte("x"), 0o600))
	require.NoError(t, os.Chmod(parent, 0))
	t.Cleanup(func() { _ = os.Chmod(parent, 0o700) })

	_, err := f.Holds(filepath.Join(parent, "x.txt"))
	assert.ErrorIs(t, err, ErrUnresolved)
}

// A path with an empty, "." or ".." component is refused with its plain form,
// since the request draws the path as the model wrote it.
func TestAbsRefusesAPathThatIsNotPlain(t *testing.T) {
	for p, plain := range map[string]string{
		"/a/../b": "/b",
		"/a/./b":  "/a/b",
		"/a//b":   "/a/b",
		"/a/":     "/a",
		"//a":     "/a",
	} {
		_, err := Abs(p)
		var np ErrNotPlain
		require.ErrorAs(t, err, &np, p)
		assert.Equal(t, plain, np.Plain, p)
		assert.ErrorContains(t, err, plain, p)
	}

	for _, p := range []string{"/", "/a", "/a/b.txt", "/a/.b", "/a/b..c"} {
		got, err := Abs(p)
		require.NoError(t, err, p)
		assert.Equal(t, p, got)
	}
}

// A link out of the root is refused, at the name and at a directory on the
// way, and a link at the name names where it leads, absolute and plain.
func TestTheRootFormsRefuseALinkOut(t *testing.T) {
	root := testRoot(t)
	outside, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(outside, "x.txt"), []byte("outside"), 0o600))
	require.NoError(t, root.Symlink(filepath.Join(outside, "x.txt"), "notes"))
	require.NoError(t, root.Symlink(outside, "dir"))

	for _, name := range []string{"notes", filepath.Join("dir", "x.txt")} {
		_, err := LstatIn(root, name)
		assert.Error(t, err, name)
		_, err = OpenIn(root, name)
		assert.Error(t, err, name)
	}

	_, err = LstatIn(root, "notes")
	var link ErrLink
	require.ErrorAs(t, err, &link)
	assert.Equal(t, filepath.Join(outside, "x.txt"), link.Target)
}

// A link that stays inside the root is still a link at the name: the file
// tools never write through one.
func TestTheRootFormsRefuseALinkInside(t *testing.T) {
	root := testRoot(t)
	require.NoError(t, root.WriteFile("x.txt", []byte("x"), 0o600))
	require.NoError(t, root.Symlink("x.txt", "l"))

	_, err := LstatIn(root, "l")
	var link ErrLink
	require.ErrorAs(t, err, &link)
	assert.Equal(t, filepath.Join(root.Name(), "x.txt"), link.Target)
	_, err = OpenIn(root, "l")
	assert.ErrorAs(t, err, &link)
}

// A FIFO is refused before it is opened.
func TestOpenInRefusesAFIFO(t *testing.T) {
	root := testRoot(t)
	require.NoError(t, syscall.Mkfifo(filepath.Join(root.Name(), "fifo"), 0o600))

	_, err := OpenIn(root, "fifo")
	assert.ErrorIs(t, err, ErrNotRegular)
}

// What OpenIn cannot open it hands back as the system said it, so the caller
// can tell a permission from a missing file.
func TestOpenInPassesOnWhatItCannotRead(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root opens any file")
	}
	root := testRoot(t)
	require.NoError(t, root.WriteFile("locked.txt", []byte("x"), 0))

	_, err := OpenIn(root, "locked.txt")
	assert.ErrorIs(t, err, os.ErrPermission)
}

// Named compares by name, and a Unix name is case-sensitive: the data directory
// in another case is caught by Holds alone.
func TestNamedMissesTheDataDirInAnotherCase(t *testing.T) {
	f, upper := otherCase(t)

	assert.False(t, f.Named(upper))
}
