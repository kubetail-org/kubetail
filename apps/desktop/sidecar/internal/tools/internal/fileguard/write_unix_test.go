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
	"io"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// existing is a file of the test's own, 0600, and its info.
func existing(t *testing.T) (string, os.FileInfo) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "x.txt")
	require.NoError(t, os.WriteFile(path, []byte("a\n"), 0o600))
	info, err := os.Lstat(path)
	require.NoError(t, err)
	return path, info
}

// gidOf is the group a file is in.
func gidOf(t *testing.T, info os.FileInfo) int {
	t.Helper()
	return int(info.Sys().(*syscall.Stat_t).Gid)
}

// A file the user does not own is refused: the rename would hand it to them.
func TestReplaceableRefusesAnotherUsersFile(t *testing.T) {
	path, info := existing(t)
	other := owner{uid: os.Getuid() + 1, groups: []int{gidOf(t, info)}}
	assert.ErrorIs(t, replaceable(path, info, other), ErrOtherUser)
}

// A file in a group the user is not in, and that a new file in its directory
// would not take, is refused: the new file could not be given it back.
func TestReplaceableRefusesAGroupItCannotGive(t *testing.T) {
	path, info := existing(t)
	stranger := owner{uid: os.Getuid(), groups: []int{gidOf(t, info) + 1}}
	assert.ErrorIs(t, replaceable(path, info, stranger), ErrOtherGroup)
}

// A file the user cannot write is refused, though the rename needs only the
// directory; and so is one in a directory the user cannot write, so the user is
// not asked for a rename that fails.
func TestReplaceableRefusesWhatItCannotWrite(t *testing.T) {
	path, _ := existing(t)
	require.NoError(t, os.Chmod(path, 0o444))
	info, err := os.Lstat(path)
	require.NoError(t, err)
	assert.ErrorIs(t, Replaceable(path, info), ErrReadOnly)

	path, info = existing(t)
	dir := filepath.Dir(path)
	require.NoError(t, os.Chmod(dir, 0o500))
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })
	var ro ErrDirReadOnly
	require.ErrorAs(t, Replaceable(path, info), &ro)
	assert.Equal(t, dir, ro.Prefix)
}

// A file whose directory is gone since its Lstat cannot be judged: the error
// passes on, under a path and under a root alike.
func TestReplaceableAnswersADirectoryGone(t *testing.T) {
	path, info := existing(t)
	gone := filepath.Join(filepath.Dir(path), "gone", "x.txt")
	assert.ErrorIs(t, Replaceable(gone, info), os.ErrNotExist)

	root := testRoot(t)
	assert.ErrorIs(t, ReplaceableIn(root, filepath.Join("gone", "x.txt"), info), os.ErrNotExist)
}

// A group that cannot be read off the new file fails the replace's finish.
func TestKeepGroupAnswersAFileItCannotStat(t *testing.T) {
	path, info := existing(t)
	f, err := os.Open(path)
	require.NoError(t, err)
	require.NoError(t, f.Close())
	assert.ErrorIs(t, keepGroup(f, info), os.ErrClosed)
}

// A directory sync that cannot open the directory changes nothing and fails
// nothing: the file is already in place.
func TestSyncDirIgnoresADirectoryGone(t *testing.T) {
	path, _ := existing(t)
	syncDir(filepath.Join(filepath.Dir(path), "gone"))
	root := testRoot(t)
	syncDirIn(root, "gone")
}

// A new file needs the deepest part of its path that exists to be a directory
// the user can write and search, and a refusal names that part.
func TestCreatableRefuses(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "f")
	require.NoError(t, os.WriteFile(file, nil, 0o600))
	var notDir ErrNotDir
	require.ErrorAs(t, Creatable(filepath.Join(file, "a", "x.txt")), &notDir)
	assert.Equal(t, file, notDir.Prefix)
	assert.ErrorContains(t, notDir, file)

	shut := filepath.Join(dir, "shut")
	require.NoError(t, os.Mkdir(shut, 0o500))
	t.Cleanup(func() { _ = os.Chmod(shut, 0o700) })
	var ro ErrDirReadOnly
	require.ErrorAs(t, Creatable(filepath.Join(shut, "a", "b", "x.txt")), &ro)
	assert.Equal(t, shut, ro.Prefix)

	assert.ErrorContains(t, ro, shut)

	assert.NoError(t, Creatable(filepath.Join(dir, "x.txt")))
	assert.NoError(t, Creatable(filepath.Join(dir, "a", "b", "x.txt")))

	locked := filepath.Join(dir, "locked")
	require.NoError(t, os.Mkdir(locked, 0))
	t.Cleanup(func() { _ = os.Chmod(locked, 0o700) })
	assert.ErrorIs(t, Creatable(filepath.Join(locked, "a", "x.txt")), os.ErrPermission, "a directory it cannot search")
}

// inGroup is info with its group swapped for gid.
type inGroup struct {
	os.FileInfo
	gid uint32
}

func (i inGroup) Sys() any {
	st := *i.FileInfo.Sys().(*syscall.Stat_t)
	st.Gid = i.gid
	return &st
}

// A group the temporary file cannot be given fails the replace before the
// rename, and takes the temporary file with it: the old file stays as it was.
// Replaceable refuses such a file first; this is the backstop.
func TestAReplaceThatCannotKeepTheGroupChangesNothing(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root can give a file any group")
	}
	path, info := existing(t)

	assert.ErrorIs(t, Replace(t.Context(), path, []byte("new\n"), inGroup{info, 0}), ErrWrite)
	b, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, "a\n", string(b))
	assert.Equal(t, []string{"x.txt"}, names(t, filepath.Dir(path)))
}

// A write whose temporary file cannot be made is ErrWrite, and changes nothing.
func TestAWriteThatCannotMakeItsTemporaryFileIsErrWrite(t *testing.T) {
	path, info := existing(t)
	dir := filepath.Dir(path)
	require.NoError(t, os.Chmod(dir, 0o500))
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })

	assert.ErrorIs(t, Replace(t.Context(), path, []byte("new\n"), info), ErrWrite)
	assert.ErrorIs(t, Create(t.Context(), filepath.Join(dir, "y.txt"), nil, 0o022), ErrWrite)
	assert.Equal(t, []string{"x.txt"}, names(t, dir))
}

// A replace keeps the old file's permission bits, never setuid, setgid or
// sticky.
func TestReplaceKeepsTheMode(t *testing.T) {
	for _, mode := range []os.FileMode{0o600, 0o755, 0o640} {
		path, _ := existing(t)
		require.NoError(t, os.Chmod(path, mode|os.ModeSetuid))
		info, err := os.Lstat(path)
		require.NoError(t, err)

		require.NoError(t, Replace(t.Context(), path, []byte("new\n"), info))
		got, err := os.Lstat(path)
		require.NoError(t, err)
		assert.Equal(t, mode, got.Mode(), "%v", mode)
	}
}

// A replace keeps a group of the user's other than the one a new file gets.
// Each group Chown can give is the same case; this runs where the test user
// has a second one, and TestReplaceableTakesAGroupTheNewFileGets decides it
// everywhere.
func TestReplaceKeepsTheGroup(t *testing.T) {
	path, info := existing(t)
	groups, err := os.Getgroups()
	require.NoError(t, err)
	second := -1
	for _, g := range groups {
		if g != gidOf(t, info) {
			second = g
		}
	}
	if second < 0 {
		t.Skip("the test user is in one group")
	}
	require.NoError(t, os.Chown(path, -1, second))
	info, err = os.Lstat(path)
	require.NoError(t, err)

	require.NoError(t, Replace(t.Context(), path, []byte("new\n"), info))
	got, err := os.Lstat(path)
	require.NoError(t, err)
	assert.Equal(t, second, gidOf(t, got))
}

// Under the sidecar's owner-only umask, a new file and each directory made for
// it take the umask it was given: 0o666 and 0o777 less it.
//
// syscall.Umask is process-global: no t.Parallel() here.
func TestCreateAppliesTheUmask(t *testing.T) {
	prev := syscall.Umask(0o077)
	defer syscall.Umask(prev)

	for _, umask := range []os.FileMode{0o022, 0o002, 0o077} {
		root := t.TempDir()
		path := filepath.Join(root, "a", "b", "x.txt")
		require.NoError(t, Create(t.Context(), path, []byte("x\n"), umask))

		info, err := os.Lstat(path)
		require.NoError(t, err)
		assert.Equal(t, 0o666&^umask, info.Mode(), "%v", umask)
		for _, dir := range []string{filepath.Join(root, "a"), filepath.Join(root, "a", "b")} {
			info, err := os.Lstat(dir)
			require.NoError(t, err)
			assert.Equal(t, os.ModeDir|0o777&^umask, info.Mode(), "%v %s", umask, dir)
		}
	}
}

// A link where Create would make a directory is never followed: a dangling one
// reads as missing to the walk, so the Mkdir meets it, and nothing is made
// where it leads.
func TestCreateDoesNotFollowALinkInItsWay(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(t.TempDir(), "elsewhere")
	require.NoError(t, os.Symlink(target, filepath.Join(root, "a")))

	err := Create(t.Context(), filepath.Join(root, "a", "b", "x.txt"), []byte("x"), 0o022)
	assert.ErrorIs(t, err, ErrWrite)
	assert.NoDirExists(t, target)
	assert.Equal(t, []string{"a"}, names(t, root))
}

// stubPlacing swaps the no-replace rename and the link for the test's own.
func stubPlacing(t *testing.T, rename, ln func(from, to string) error) {
	t.Helper()
	realRename, realLink := renameNoReplace, link
	t.Cleanup(func() { renameNoReplace, link = realRename, realLink })
	renameNoReplace, link = rename, ln
}

// planting puts a file at to, then does what next does: a file created at the
// path after the temporary one was made.
func planting(next func(from, to string) error) func(from, to string) error {
	return func(from, to string) error {
		if err := os.WriteFile(to, []byte("theirs\n"), 0o600); err != nil {
			return err
		}
		return next(from, to)
	}
}

// unsupported is a filesystem that does not have the call.
func unsupported(err error) func(from, to string) error {
	return func(string, string) error { return &os.LinkError{Op: "stub", Err: err} }
}

// A new file never replaces one put at its path since the check: by the
// no-replace rename, by the link a filesystem without it falls back to, and by
// the O_EXCL write one without hard links falls back to. Each answers
// ErrExists, leaves the file there as it was, and leaves no temporary file.
func TestCreateRefusesAnExistingFile(t *testing.T) {
	realRename := renameNoReplace
	for name, c := range map[string]struct{ rename, ln func(from, to string) error }{
		"no-replace rename": {planting(realRename), os.Link},
		"link":              {unsupported(syscall.ENOTSUP), planting(os.Link)},
		"O_EXCL":            {unsupported(syscall.EINVAL), planting(unsupported(syscall.EPERM))},
	} {
		stubPlacing(t, c.rename, c.ln)
		dir := t.TempDir()
		path := filepath.Join(dir, "x.txt")

		assert.ErrorIs(t, Create(t.Context(), path, []byte("mine\n"), 0o022), ErrExists, name)
		b, err := os.ReadFile(path)
		require.NoError(t, err)
		assert.Equal(t, "theirs\n", string(b), name)
		assert.Equal(t, []string{"x.txt"}, names(t, dir), name)
	}
}

// Each fallback puts the file in place, with its mode, and no temporary file.
func TestCreateFallsBackWhereTheFilesystemLacksACall(t *testing.T) {
	for name, c := range map[string]struct{ rename, ln func(from, to string) error }{
		"link":   {unsupported(syscall.ENOSYS), os.Link},
		"O_EXCL": {unsupported(syscall.ENOTSUP), unsupported(syscall.EPERM)},
	} {
		stubPlacing(t, c.rename, c.ln)
		dir := t.TempDir()
		path := filepath.Join(dir, "x.txt")

		require.NoError(t, Create(t.Context(), path, []byte("mine\n"), 0o022), name)
		b, err := os.ReadFile(path)
		require.NoError(t, err)
		assert.Equal(t, "mine\n", string(b), name)
		info, err := os.Lstat(path)
		require.NoError(t, err)
		assert.Equal(t, os.FileMode(0o644), info.Mode(), name)
		assert.Equal(t, []string{"x.txt"}, names(t, dir), name)
	}
}

// A failure that is not a missing call is not a reason to fall back, and a
// failed rename takes the directories Create made with it.
func TestCreateDoesNotFallBackOnAnyOtherFailure(t *testing.T) {
	stubPlacing(t, unsupported(syscall.EIO), os.Link)
	dir := t.TempDir()

	assert.ErrorIs(t, Create(t.Context(), filepath.Join(dir, "a", "b", "x.txt"), nil, 0o022), ErrWrite)
	assert.Empty(t, names(t, dir))
}

// A group the user is in is one Chown can give, and a group the directory
// hands every new file is one the new file takes anyway: always on macOS, and
// under a setgid directory on Linux. So a file Write made in macOS's /tmp,
// group wheel, is replaced.
func TestReplaceableTakesAGroupTheNewFileGets(t *testing.T) {
	path, info := existing(t)
	me := owner{uid: os.Getuid(), groups: []int{gidOf(t, info)}}
	assert.NoError(t, replaceable(path, info, me), "a group of the user's")

	bsd := owner{uid: os.Getuid(), bsdGroups: true}
	assert.NoError(t, replaceable(path, info, bsd), "the directory's group, on macOS")

	linux := owner{uid: os.Getuid()}
	require.NoError(t, os.Chmod(filepath.Dir(path), 0o700|os.ModeSetgid))
	assert.NoError(t, replaceable(path, info, linux), "the directory's group, under setgid")
}

// ReplaceableIn refuses what Replaceable refuses: another user's file, a group
// the new file could not be given, a file the user cannot write, and one in a
// directory the user cannot write.
func TestReplaceableIn(t *testing.T) {
	root, name, info := existingIn(t)
	assert.NoError(t, ReplaceableIn(root, name, info))

	other := owner{uid: os.Getuid() + 1, groups: []int{gidOf(t, info)}}
	assert.ErrorIs(t, replaceableIn(root, name, info, other), ErrOtherUser)
	stranger := owner{uid: os.Getuid(), groups: []int{gidOf(t, info) + 1}}
	assert.ErrorIs(t, replaceableIn(root, name, info, stranger), ErrOtherGroup)

	require.NoError(t, root.Chmod(name, 0o444))
	info, err := root.Lstat(name)
	require.NoError(t, err)
	assert.ErrorIs(t, ReplaceableIn(root, name, info), ErrReadOnly)

	root, name, info = existingIn(t)
	require.NoError(t, root.Chmod("a", 0o500))
	t.Cleanup(func() { _ = root.Chmod("a", 0o700) })
	var ro ErrDirReadOnly
	require.ErrorAs(t, ReplaceableIn(root, name, info), &ro)
	assert.Equal(t, filepath.Join(root.Name(), "a"), ro.Prefix)
}

// A replace keeps the old file's permission bits, never setuid, setgid or
// sticky.
func TestReplaceInKeepsTheMode(t *testing.T) {
	for _, mode := range []os.FileMode{0o600, 0o755, 0o640} {
		root, name, _ := existingIn(t)
		require.NoError(t, root.Chmod(name, mode|os.ModeSetuid))
		info, err := root.Lstat(name)
		require.NoError(t, err)

		require.NoError(t, ReplaceIn(t.Context(), root, name, []byte("new\n"), info))
		got, err := root.Lstat(name)
		require.NoError(t, err)
		assert.Equal(t, mode, got.Mode(), "%v", mode)
	}
}

// A replace through a directory that leads out of the root is refused, and
// nothing outside changes.
func TestReplaceInRefusesADirectoryLinkOut(t *testing.T) {
	root, _, info := existingIn(t)
	outside := t.TempDir()
	require.NoError(t, root.Symlink(outside, "out"))

	assert.ErrorIs(t, ReplaceIn(t.Context(), root, filepath.Join("out", "x.txt"), []byte("new\n"), info), ErrWrite)
	assert.Empty(t, names(t, outside))
}

// stubLinkIn swaps the link CreateIn places a new file by for the test's own.
func stubLinkIn(t *testing.T, ln func(root *os.Root, from, to string) error) {
	t.Helper()
	real := linkIn
	t.Cleanup(func() { linkIn = real })
	linkIn = ln
}

// plantingIn puts a file at to, then does what next does.
func plantingIn(next func(root *os.Root, from, to string) error) func(root *os.Root, from, to string) error {
	return func(root *os.Root, from, to string) error {
		if err := root.WriteFile(to, []byte("theirs\n"), 0o600); err != nil {
			return err
		}
		return next(root, from, to)
	}
}

// A new file never replaces one put at its name since the check: by the link,
// and by the O_EXCL write a filesystem without hard links falls back to. Each
// answers ErrExists, leaves the file as it was, and leaves no temporary file.
func TestCreateInNeverReplaces(t *testing.T) {
	notSupported := func(*os.Root, string, string) error { return &os.LinkError{Op: "stub", Err: syscall.EPERM} }
	for name, ln := range map[string]func(*os.Root, string, string) error{
		"link":   plantingIn((*os.Root).Link),
		"O_EXCL": plantingIn(notSupported),
	} {
		stubLinkIn(t, ln)
		root := testRoot(t)

		assert.ErrorIs(t, CreateIn(t.Context(), root, "x.txt", []byte("mine\n"), 0o022), ErrExists, name)
		b, err := root.ReadFile("x.txt")
		require.NoError(t, err)
		assert.Equal(t, "theirs\n", string(b), name)
		assert.Equal(t, []string{"x.txt"}, names(t, root.Name()), name)
	}
}

// A filesystem without hard links gets the O_EXCL write, which puts the file in
// place with its mode and leaves no temporary file.
func TestCreateInFallsBackWithoutHardLinks(t *testing.T) {
	stubLinkIn(t, func(*os.Root, string, string) error { return &os.LinkError{Op: "stub", Err: syscall.EPERM} })
	root := testRoot(t)

	require.NoError(t, CreateIn(t.Context(), root, "x.txt", []byte("mine\n"), 0o022))
	b, err := root.ReadFile("x.txt")
	require.NoError(t, err)
	assert.Equal(t, "mine\n", string(b))
	info, err := root.Lstat("x.txt")
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o644), info.Mode())
	assert.Equal(t, []string{"x.txt"}, names(t, root.Name()))
}

// A new file and each directory made for it take the umask given.
//
// syscall.Umask is process-global: no t.Parallel() here.
func TestCreateInAppliesTheUmask(t *testing.T) {
	prev := syscall.Umask(0o077)
	defer syscall.Umask(prev)
	root := testRoot(t)

	require.NoError(t, CreateIn(t.Context(), root, filepath.Join("a", "x.txt"), nil, 0o022))
	dir, err := root.Lstat("a")
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o755), dir.Mode().Perm())
	file, err := root.Lstat(filepath.Join("a", "x.txt"))
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o644), file.Mode().Perm())
}

// CreatableIn refuses what Creatable refuses, naming the part in the way, and a
// link on the way whether it leads out or not.
func TestCreatableIn(t *testing.T) {
	root := testRoot(t)
	require.NoError(t, root.WriteFile("f", nil, 0o600))
	var notDir ErrNotDir
	require.ErrorAs(t, CreatableIn(root, filepath.Join("f", "a", "x.txt")), &notDir)
	assert.Equal(t, filepath.Join(root.Name(), "f"), notDir.Prefix)

	require.NoError(t, root.Mkdir("shut", 0o500))
	t.Cleanup(func() { _ = root.Chmod("shut", 0o700) })
	var ro ErrDirReadOnly
	require.ErrorAs(t, CreatableIn(root, filepath.Join("shut", "a", "x.txt")), &ro)
	assert.Equal(t, filepath.Join(root.Name(), "shut"), ro.Prefix)

	assert.NoError(t, CreatableIn(root, "x.txt"))
	assert.NoError(t, CreatableIn(root, filepath.Join("a", "b", "x.txt")))

	require.NoError(t, root.Mkdir("d", 0o700))
	require.NoError(t, root.Symlink("d", "in"))
	require.NoError(t, root.Symlink(t.TempDir(), "out"))
	for _, l := range []string{"in", "out"} {
		var link ErrLink
		assert.ErrorAs(t, CreatableIn(root, filepath.Join(l, "x.txt")), &link, l)
	}
}

// A create through a directory that leads out of the root is refused, and
// nothing outside changes.
func TestCreateInRefusesADirectoryLinkOut(t *testing.T) {
	root := testRoot(t)
	outside := t.TempDir()
	require.NoError(t, root.Symlink(outside, "out"))

	for _, name := range []string{filepath.Join("out", "x.txt"), filepath.Join("out", "a", "x.txt")} {
		assert.ErrorIs(t, CreateIn(t.Context(), root, name, []byte("x"), 0o022), ErrWrite, name)
	}
	assert.Empty(t, names(t, outside))
}

// A group the temporary file cannot be given fails the replace before the
// rename, and takes the temporary file with it. ReplaceableIn refuses such a
// file first; this is the backstop.
func TestAReplaceInThatCannotKeepTheGroupChangesNothing(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root can give a file any group")
	}
	root, name, info := existingIn(t)

	assert.ErrorIs(t, ReplaceIn(t.Context(), root, name, []byte("new\n"), inGroup{info, 0}), ErrWrite)
	b, err := root.ReadFile(name)
	require.NoError(t, err)
	assert.Equal(t, "old\n", string(b))
	assert.Equal(t, []string{"x.txt"}, names(t, filepath.Join(root.Name(), "a")))
}

// A write whose temporary file cannot be made is ErrWrite, and changes nothing.
func TestAWriteInThatCannotMakeItsTemporaryFileIsErrWrite(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root writes into a read-only directory")
	}
	root, name, info := existingIn(t)
	require.NoError(t, root.Chmod("a", 0o500))
	t.Cleanup(func() { _ = root.Chmod("a", 0o700) })

	assert.ErrorIs(t, ReplaceIn(t.Context(), root, name, []byte("new\n"), info), ErrWrite)
	assert.ErrorIs(t, CreateIn(t.Context(), root, filepath.Join("a", "y.txt"), nil, 0o022), ErrWrite)
	assert.Equal(t, []string{"x.txt"}, names(t, filepath.Join(root.Name(), "a")))
}

// A link that fails for any reason but a missing call is not a reason to fall
// back, and the failed create takes the directories it made with it.
func TestCreateInDoesNotFallBackOnAnyOtherFailure(t *testing.T) {
	stubLinkIn(t, func(*os.Root, string, string) error { return &os.LinkError{Op: "stub", Err: syscall.EIO} })
	root := testRoot(t)

	assert.ErrorIs(t, CreateIn(t.Context(), root, filepath.Join("a", "b", "x.txt"), nil, 0o022), ErrWrite)
	assert.Empty(t, names(t, root.Name()))
}

// A file where a new file needs a directory fails the create.
func TestCreateInRefusesAFileInTheWay(t *testing.T) {
	root := testRoot(t)
	require.NoError(t, root.WriteFile("f", nil, 0o600))

	assert.ErrorIs(t, CreateIn(t.Context(), root, filepath.Join("f", "a", "x.txt"), nil, 0o022), ErrWrite)
	assert.Equal(t, []string{"f"}, names(t, root.Name()))
}

// A directory CreatableIn cannot search is passed on as the system said it.
func TestCreatableInPassesOnWhatItCannotSearch(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root searches any directory")
	}
	root := testRoot(t)
	require.NoError(t, root.Mkdir("locked", 0))
	t.Cleanup(func() { _ = root.Chmod("locked", 0o700) })

	assert.ErrorIs(t, CreatableIn(root, filepath.Join("locked", "a", "x.txt")), os.ErrPermission)
}

// A replace is a rename: a reader holding the old file keeps reading the old
// bytes, the path holds the new ones, and no temporary file is left.
func TestReplaceIsARename(t *testing.T) {
	path := filepath.Join(t.TempDir(), "x.txt")
	require.NoError(t, os.WriteFile(path, []byte("old\n"), 0o600))
	info, err := os.Lstat(path)
	require.NoError(t, err)
	held, err := os.Open(path)
	require.NoError(t, err)
	defer held.Close()

	require.NoError(t, Replace(t.Context(), path, []byte("new\r\n"), info))

	b, err := io.ReadAll(held)
	require.NoError(t, err)
	assert.Equal(t, "old\n", string(b))
	b, err = os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, "new\r\n", string(b), "the bytes as given")
	assert.Equal(t, []string{"x.txt"}, names(t, filepath.Dir(path)))
}
