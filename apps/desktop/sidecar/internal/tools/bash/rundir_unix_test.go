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

package bash

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/testutil"
)

// longestRuns is the longest runs directory a run's socket fits under here.
var longestRuns = maxSocketPath - len("/") - maxRunDirName - len("/"+socketName)

// A run's directory is named for the sidecar's pid, under the runs directory,
// owner-only; the kubeconfig and the socket are named in it.
func TestARunsDirectoryIsInTheRunsDirectory(t *testing.T) {
	old := syscall.Umask(0)
	defer syscall.Umask(old)
	runs := filepath.Join(shortTemp(t), "runs")

	d, err := newRunDir(runs, t.TempDir(), 1234)
	require.NoError(t, err)

	assert.Equal(t, runs, filepath.Dir(d.path))
	assert.Regexp(t, `^1234-\d+$`, filepath.Base(d.path))
	assert.Equal(t, filepath.Join(d.path, "kubeconfig"), d.kubeconfig())
	for _, dir := range []string{runs, d.path} {
		info, err := os.Stat(dir)
		require.NoError(t, err)
		assert.Equal(t, os.FileMode(0o700), info.Mode().Perm(), dir)
	}
}

// A run's TMPDIR is its own too, named the same way, under the tmp directory
// the cache directory holds.
func TestARunsTMPDIRIsInTheCacheDirectory(t *testing.T) {
	old := syscall.Umask(0)
	defer syscall.Umask(old)
	tmpDir := filepath.Join(t.TempDir(), "tmp")

	d, err := newRunDir(runsIn(t), tmpDir, 1234)
	require.NoError(t, err)

	assert.Equal(t, tmpDir, filepath.Dir(d.tmp))
	assert.Regexp(t, `^1234-\d+$`, filepath.Base(d.tmp))
	for _, dir := range []string{tmpDir, d.tmp} {
		info, err := os.Stat(dir)
		require.NoError(t, err)
		assert.Equal(t, os.FileMode(0o700), info.Mode().Perm(), dir)
	}
}

// A tmp directory that cannot be made makes no run: nothing is left in the
// runs directory.
func TestARunsTMPDIRNeedsItsDirectory(t *testing.T) {
	runs := runsIn(t)
	file := filepath.Join(t.TempDir(), "file")
	require.NoError(t, os.WriteFile(file, nil, 0o600))

	_, err := newRunDir(runs, filepath.Join(file, "tmp"), 1)

	require.Error(t, err)
	entries, err := os.ReadDir(runs)
	require.NoError(t, err)
	assert.Empty(t, entries)
}

// A runs or tmp directory the run cannot make its own in makes no run, and
// leaves no run's directory in the other; the sidecar's lock stays, being the
// sidecar's rather than the run's.
func TestAnUnwritableRunsOrTmpDirMakesNoRun(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root writes a read-only directory")
	}
	for name, c := range map[string]struct {
		locked int
		held   bool
	}{"runs": {0, false}, "tmp": {1, false}, "runs, lock held": {0, true}, "tmp, lock held": {1, true}} {
		locked := c.locked
		t.Run(name, func(t *testing.T) {
			dirs := []string{runsIn(t), t.TempDir()}
			if c.held {
				for _, dir := range dirs {
					require.NoError(t, holdRunLock(dir, 1))
				}
			}
			require.NoError(t, os.Chmod(dirs[locked], 0o500))
			t.Cleanup(func() { _ = os.Chmod(dirs[locked], 0o700) })

			_, err := newRunDir(dirs[0], dirs[1], 1)

			require.Error(t, err)
			for _, dir := range dirs {
				entries, err := os.ReadDir(dir)
				require.NoError(t, err)
				for _, e := range entries {
					assert.Equal(t, lockName(1), e.Name(), dir)
				}
			}
		})
	}
}

// A runs directory in a directory another user could write, or reached
// through a link, makes no run: whoever owns the parent could swap what is in
// it.
func TestARunsDirectoryNotThisUsersAloneMakesNoRun(t *testing.T) {
	open := shortTemp(t)
	require.NoError(t, os.Chmod(open, 0o777))
	link := filepath.Join(shortTemp(t), "link")
	require.NoError(t, os.Symlink(shortTemp(t), link))

	for _, parent := range []string{open, link} {
		_, err := newRunDir(filepath.Join(parent, "runs"), t.TempDir(), 1)
		assert.ErrorContains(t, err, "is not a directory of this user's alone", parent)
	}
}

// A snapshot in a directory another user could write is never sourced: the
// command, foreground or background, could not start.
func TestACommandRefusesASnapshotAnotherCouldSwap(t *testing.T) {
	open := shortTemp(t)
	require.NoError(t, os.Chmod(open, 0o777))
	for name, input := range map[string]json.RawMessage{"call": command("echo ran"), "task": background("echo ran")} {
		t.Run(name, func(t *testing.T) {
			tl := tool(t)
			tl.scripts = filepath.Join(open, "shell")
			tl.snapshot = filepath.Join(tl.scripts, "snapshot.sh")

			text, isError := tl.Run(t.Context(), testRuntime(t), input)

			assert.True(t, isError)
			assert.True(t, strings.HasPrefix(text, "could not start: "), text)
			assert.Contains(t, text, "is not a directory of this user's alone")
		})
	}
}

// A snapshot whose directory has gone is never sourced either.
func TestACommandRefusesASnapshotWhoseDirectoryWent(t *testing.T) {
	tl := tool(t)
	tl.scripts = filepath.Join(shortTemp(t), "gone", "shell")
	tl.snapshot = filepath.Join(tl.scripts, "snapshot.sh")

	text, isError := tl.Run(t.Context(), testRuntime(t), command("echo ran"))

	assert.True(t, isError)
	assert.True(t, strings.HasPrefix(text, "could not start: "), text)
}

// Two runs get two of each, and each goes with its own run.
func TestTheTmpDirIsTheRunsOwn(t *testing.T) {
	runs, tmpDir := runsIn(t), t.TempDir()
	a, err := newRunDir(runs, tmpDir, 1)
	require.NoError(t, err)
	b, err := newRunDir(runs, tmpDir, 1)
	require.NoError(t, err)

	assert.NotEqual(t, a.tmp, b.tmp)
	require.NoError(t, a.remove())
	assert.NoDirExists(t, a.path)
	assert.NoDirExists(t, a.tmp)
	assert.DirExists(t, b.path)
	assert.DirExists(t, b.tmp)
}

// A command can leave a directory in its TMPDIR it cannot write; the TMPDIR
// still goes, and a link there goes without its target.
func TestAnUnwritableTmpDirIsRemoved(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root removes a read-only directory's entries")
	}
	d, err := newRunDir(runsIn(t), t.TempDir(), 1)
	require.NoError(t, err)
	locked := filepath.Join(d.tmp, "locked")
	require.NoError(t, os.Mkdir(locked, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(locked, "f"), nil, 0o400))
	keep := filepath.Join(t.TempDir(), "keep")
	require.NoError(t, os.WriteFile(keep, nil, 0o600))
	require.NoError(t, os.Symlink(keep, filepath.Join(locked, "link")))
	require.NoError(t, os.Chmod(locked, 0o500))

	require.NoError(t, d.remove())

	assert.NoDirExists(t, d.tmp)
	assert.FileExists(t, keep)
}

// The run directories of a sidecar that no longer holds its lock are swept,
// with the lock, and nothing else: a live sidecar's, a link, and an entry no
// run is named as.
func TestAGoneSidecarsRunsAreSwept(t *testing.T) {
	runs := shortTemp(t)
	gone := filepath.Join(runs, "111-1")
	lockless := filepath.Join(runs, "333-1")
	live := filepath.Join(runs, "222-2")
	for _, dir := range []string{gone, lockless, live} {
		require.NoError(t, os.MkdirAll(filepath.Join(dir, "tmp"), 0o700))
	}
	require.NoError(t, os.WriteFile(filepath.Join(runs, lockName(111)), nil, 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(runs, lockName(444)), nil, 0o600))
	require.NoError(t, holdRunLock(runs, 222))
	unreadable := filepath.Join(runs, "777-1")
	require.NoError(t, os.Mkdir(unreadable, 0o700))
	require.NoError(t, os.Mkdir(filepath.Join(runs, lockName(777)), 0o700))
	other := filepath.Join(runs, "notes")
	require.NoError(t, os.WriteFile(other, nil, 0o600))
	link := filepath.Join(runs, "111-3")
	require.NoError(t, os.Symlink(live, link))

	sweepRunDirs(runs)

	assert.NoDirExists(t, gone)
	assert.NoDirExists(t, lockless, "a sidecar takes its lock before it makes a run")
	assert.NoFileExists(t, filepath.Join(runs, lockName(111)))
	assert.NoFileExists(t, filepath.Join(runs, lockName(444)), "a lock with no run goes too")
	assert.DirExists(t, filepath.Join(live, "tmp"))
	assert.FileExists(t, filepath.Join(runs, lockName(222)))
	assert.DirExists(t, unreadable, "a lock the sweep cannot open says nothing")
	assert.FileExists(t, other)
	_, err := os.Lstat(link)
	assert.NoError(t, err, "a link is not a run's directory")
}

// A pid a later process took, this one included, keeps nothing: the lock
// died with the sidecar that made the run.
func TestAReusedPidsRunsAreSwept(t *testing.T) {
	runs := shortTemp(t)
	left := filepath.Join(runs, strconv.Itoa(os.Getpid())+"-1")
	require.NoError(t, os.Mkdir(left, 0o700))

	sweepRunDirs(runs)

	assert.NoDirExists(t, left)
}

// A sidecar taking its lock while a sweep holds it and removes the file ends
// holding the file named at the path, whichever went first: a lock on a
// removed file would guard nothing.
func TestALockASweepRemovesIsTakenAgain(t *testing.T) {
	runs := shortTemp(t)
	path := filepath.Join(runs, lockName(666))
	sweep, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600)
	require.NoError(t, err)
	require.NoError(t, syscall.Flock(int(sweep.Fd()), syscall.LOCK_EX))
	taken := make(chan error, 1)
	go func() { taken <- holdRunLock(runs, 666) }()

	require.NoError(t, os.Remove(path))
	require.NoError(t, sweep.Close())
	require.NoError(t, testutil.Recv(t, taken, "the lock"))

	root, err := os.OpenRoot(runs)
	require.NoError(t, err)
	defer root.Close()
	_, free := freeLock(root, 666)
	assert.False(t, free, "the file at the path is the one held")
}

// A sidecar takes its lock once, so its next run finds the lock held rather
// than waiting on itself.
func TestTheLockIsTakenOnce(t *testing.T) {
	runs := shortTemp(t)
	require.NoError(t, holdRunLock(runs, 555))
	require.NoError(t, holdRunLock(runs, 555))

	sweepRunDirs(runs)

	assert.FileExists(t, filepath.Join(runs, lockName(555)))
}

// The login shell's TMPDIR needs its folder, its lock and room in the folder;
// lacking any, no TMPDIR is made.
func TestALoginShellTmpDirThatCannotBeMadeIsAnError(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root writes any folder")
	}
	file := filepath.Join(t.TempDir(), "file")
	require.NoError(t, os.WriteFile(file, nil, 0o600))
	_, _, err := TempDir(filepath.Join(file, "tmp"))()
	assert.ErrorIs(t, err, syscall.ENOTDIR, "a folder under a file")

	taken := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(taken, lockName(os.Getpid())), 0o700))
	_, _, err = TempDir(taken)()
	assert.ErrorIs(t, err, syscall.EISDIR, "a folder where the lock goes")

	shut := t.TempDir()
	_, cleanup, err := TempDir(shut)()
	require.NoError(t, err)
	cleanup()
	require.NoError(t, os.Chmod(shut, 0o500))
	t.Cleanup(func() { _ = os.Chmod(shut, 0o700) })
	_, _, err = TempDir(shut)()
	assert.ErrorIs(t, err, os.ErrPermission, "a folder it cannot write, its lock already held")
}

// A login shell's TMPDIR that cannot be removed is left for the next start's
// sweep, and says so.
func TestALoginShellTmpDirThatCannotBeRemovedIsLogged(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root writes any folder")
	}
	tmp := t.TempDir()
	dir, cleanup, err := TempDir(tmp)()
	require.NoError(t, err)
	require.NoError(t, os.Chmod(tmp, 0o500))
	t.Cleanup(func() { _ = os.Chmod(tmp, 0o700) })
	logs := testutil.CaptureLogs(t)

	cleanup()

	assert.DirExists(t, dir)
	assert.Contains(t, logs.String(), "could not remove the login shell's TMPDIR")
}

// New sweeps the runs and tmp directories as it starts, on a goroutine of its
// own.
func TestNewSweepsADeadSidecarsRunDirectories(t *testing.T) {
	runs, tmpDir := shortTemp(t), t.TempDir()
	t.Setenv("SHELL", "")
	name := "111-1"
	for _, dir := range []string{runs, tmpDir} {
		require.NoError(t, os.Mkdir(filepath.Join(dir, name), 0o700))
	}

	if _, ok := New(Paths{ShellDir: t.TempDir(), RunsDir: runs, TmpDir: tmpDir}, 0, nil, nil, nil, nil, Cloud{}); !ok {
		t.Skip("no bash found on this machine")
	}

	require.Eventually(t, func() bool {
		_, runErr := os.Stat(filepath.Join(runs, name))
		_, tmpErr := os.Stat(filepath.Join(tmpDir, name))
		return os.IsNotExist(runErr) && os.IsNotExist(tmpErr)
	}, testutil.Timeout, time.Millisecond, "both of the dead sidecar's directories go")
}

// Under the longest runs directory allowed, the longest name a run's directory
// can have — a 7-digit pid and MkdirTemp's 10-digit suffix — still leaves its
// socket's path within what Go binds.
func TestTheSocketPathFits(t *testing.T) {
	runs := "/" + strings.Repeat("t", longestRuns-1)
	name := strings.Repeat("9", 7) + "-" + strings.Repeat("9", 10)
	require.Len(t, name, maxRunDirName)

	require.NoError(t, checkSocketPath(runs))
	assert.Len(t, filepath.Join(runs, name, socketName), maxSocketPath)
}

// One byte past it, the run answers before anything is made, naming the runs
// directory and the limit.
func TestATooLongRunsDirCouldNotStart(t *testing.T) {
	assert.Error(t, checkSocketPath("/"+strings.Repeat("t", longestRuns)))

	runs := shortTemp(t)
	for len(runs) <= longestRuns {
		runs = filepath.Join(runs, "d")
		require.NoError(t, os.Mkdir(runs, 0o700))
	}

	_, err := newRunDir(runs, t.TempDir(), 1)

	require.Error(t, err)
	assert.Contains(t, err.Error(), runs)
	assert.Contains(t, err.Error(), strconv.Itoa(maxSocketPath))
	entries, err := os.ReadDir(runs)
	require.NoError(t, err)
	assert.Empty(t, entries)
}

// A runs directory that cannot be made makes no run's directory.
func TestARunsDirectoryNeedsItsRunsDirectory(t *testing.T) {
	file := filepath.Join(shortTemp(t), "file")
	require.NoError(t, os.WriteFile(file, nil, 0o600))
	_, err := newRunDir(filepath.Join(file, "runs"), t.TempDir(), 1)
	assert.Error(t, err)
}

// A run's directory whose runs directory went is an error to remove, which the
// end of a run logs and goes on from.
func TestARunsDirectoryUnderAGoneRunsIsLeft(t *testing.T) {
	runs := filepath.Join(shortTemp(t), "runs")
	d, err := newRunDir(runs, t.TempDir(), 1)
	require.NoError(t, err)
	require.NoError(t, os.RemoveAll(runs))

	assert.Error(t, d.remove())
	d.removeLogged()
}

// A runs directory that is not there yet has nothing to sweep, and one the
// sweep cannot remove from is left as it is for the next start.
func TestTheSweepLeavesWhatItCannotRemove(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root removes a read-only directory's entries")
	}
	logs := testutil.CaptureLogs(t)
	sweepRunDirs(filepath.Join(shortTemp(t), "gone"))
	assert.Empty(t, logs.String())

	file := filepath.Join(shortTemp(t), "file")
	require.NoError(t, os.WriteFile(file, nil, 0o600))
	sweepRunDirs(file)
	assert.Contains(t, logs.String(), "could not open a directory to sweep run directories")

	runs := shortTemp(t)
	dead := filepath.Join(runs, "111-1")
	require.NoError(t, os.Mkdir(dead, 0o700))
	require.NoError(t, os.Chmod(runs, 0o500))
	t.Cleanup(func() { _ = os.Chmod(runs, 0o700) })

	sweepRunDirs(runs)
	assert.DirExists(t, dead)
}

// A login shell's TMPDIR is a run's: under the tmp directory, behind the
// sidecar's lock, and gone with the cleanup.
func TestTheLoginShellsTempDirIsARuns(t *testing.T) {
	tmp := filepath.Join(t.TempDir(), "tmp")

	dir, cleanup, err := TempDir(tmp)()

	require.NoError(t, err)
	assert.Equal(t, tmp, filepath.Dir(dir))
	assert.True(t, strings.HasPrefix(filepath.Base(dir), strconv.Itoa(os.Getpid())+"-"))
	assert.FileExists(t, filepath.Join(tmp, lockName(os.Getpid())))
	sweepRunDirs(tmp)
	assert.DirExists(t, dir, "the lock keeps a sweep off it")
	cleanup()
	assert.NoDirExists(t, dir)
}

// A run's file is owner-only from birth.
func TestANewFileIsOwnerOnly(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "file")
	require.NoError(t, writeNew(path, []byte("x")))
	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
}
