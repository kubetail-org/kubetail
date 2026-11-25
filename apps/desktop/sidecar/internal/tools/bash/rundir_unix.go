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
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/rootdir"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/run/loginshell"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/run/sandbox"
)

// TempDir makes a login shell's TMPDIR as a sandboxed run's is made: a <pid>-*
// folder under tmpDir, taken under the sidecar's lock so a sweep never removes
// it mid-run, seeded, and removed by the cleanup it answers.
func TempDir(tmpDir string) loginshell.TempDir {
	return func() (string, func(), error) {
		pid := os.Getpid()
		if err := os.MkdirAll(tmpDir, 0o700); err != nil {
			return "", nil, err
		}
		if err := holdRunLock(tmpDir, pid); err != nil {
			return "", nil, err
		}
		dir, err := os.MkdirTemp(tmpDir, strconv.Itoa(pid)+"-*")
		if err != nil {
			return "", nil, err
		}
		sandbox.SeedTmpDir(dir)
		return dir, func() {
			if err := removeUnder(dir); err != nil {
				slog.Warn("could not remove the login shell's TMPDIR; the next start sweeps it", "err", err)
			}
		}, nil
	}
}

// A sidecar holds a lock, <pid>.lock in each directory it makes its runs'
// directories in, for as long as it lives. A lock dies with its process, while
// a pid passes to later ones, across a reboot too, so the lock is what says
// whether the sidecar that made a directory still runs.

// heldLocks is every lock this process holds, by path. Each is taken once and
// kept, since a second flock of the file from this process would wait on the
// first.
var heldLocks = struct {
	sync.Mutex
	files map[string]*os.File
}{files: map[string]*os.File{}}

func lockName(pid int) string { return strconv.Itoa(pid) + ".lock" }

// holdRunLock takes the lock of the sidecar pid names in dir, once, and keeps
// it for the life of the process. It waits out a sweep holding the lock for a
// moment, and takes it again when that sweep removed the file, since a lock on
// a removed file guards nothing.
func holdRunLock(dir string, pid int) error {
	path := filepath.Join(dir, lockName(pid))
	heldLocks.Lock()
	defer heldLocks.Unlock()
	if heldLocks.files[path] != nil {
		return nil
	}
	for {
		f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|syscall.O_NOFOLLOW, 0o600)
		if err != nil {
			return err
		}
		var held os.FileInfo
		err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX)
		if err == nil {
			held, err = f.Stat()
		}
		if err != nil { // coverage:ignore a blocking flock and an fstat of a file just opened fail only on an I/O error
			f.Close()
			return err
		}
		if named, err := os.Lstat(path); err == nil && os.SameFile(held, named) {
			heldLocks.files[path] = f
			return nil
		}
		f.Close() // coverage:ignore only a sweep removing the lock after this open reaches it, an order no test can fix
	}
}

// sweepRunDirs removes the run directories under dir, the runs or the tmp
// directory, of every sidecar that no longer holds its lock, and the locks it
// can take: what a crash left. It matches a directory named <pid>-* and a file
// named <pid>.lock and nothing else. A directory not made yet holds none. A
// failure is logged, and the next start tries again.
func sweepRunDirs(dir string) {
	root, err := os.OpenRoot(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return
	}
	if err != nil {
		slog.Warn("could not open a directory to sweep run directories", "dir", dir, "err", err)
		return
	}
	defer root.Close()
	entries, err := fs.ReadDir(root.FS(), ".")
	if err != nil { // coverage:ignore a folder just opened for reading fails to list only on an I/O error
		slog.Warn("could not list a directory to sweep run directories", "dir", dir, "err", err)
		return
	}
	// gone is each pid whose sidecar is gone, and its lock, held until its
	// directories have gone; nil for a pid with no lock to hold.
	gone := map[int]*os.File{}
	defer func() {
		for pid, lock := range gone {
			if lock != nil {
				// Removed while held: a sidecar of that pid starting now waits on
				// it, then finds the file gone and makes its own.
				_ = root.Remove(lockName(pid))
				lock.Close()
			}
		}
	}()
	isGone := func(pid int) bool {
		if _, ok := gone[pid]; ok {
			return true
		}
		lock, free := freeLock(root, pid)
		if free {
			gone[pid] = lock
		}
		return free
	}
	for _, e := range entries {
		if pid, ok := lockPID(e.Name()); ok {
			isGone(pid)
		}
	}
	rootdir.Sweep(root, entries, func(e fs.DirEntry) bool {
		pid, ok := runDirPID(e.Name())
		// A link is not a directory here: DirEntry reports what the entry is.
		return !ok || !e.IsDir() || !isGone(pid)
	})
}

// freeLock takes the lock of the sidecar pid names in root when no one holds
// it, and reports that sidecar gone. A lock that is not there is gone too, with
// no file to hold: a sidecar takes its lock before it makes a directory.
func freeLock(root *os.Root, pid int) (*os.File, bool) {
	f, err := root.OpenFile(lockName(pid), os.O_RDWR, 0)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, true
	}
	if err != nil {
		return nil, false
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		return nil, false
	}
	return f, true
}

// runDirPID is the pid a run directory's name carries, and whether name is one.
func runDirPID(name string) (int, bool) {
	digits, _, ok := strings.Cut(name, "-")
	if !ok {
		return 0, false
	}
	return parsePID(digits)
}

// lockPID is the pid a lock's name carries, and whether name is one.
func lockPID(name string) (int, bool) {
	digits, ok := strings.CutSuffix(name, ".lock")
	if !ok {
		return 0, false
	}
	return parsePID(digits)
}

func parsePID(digits string) (int, bool) {
	pid, err := strconv.Atoi(digits)
	return pid, err == nil && pid > 0
}

// checkPrivate refuses dir unless it is a directory, not a link, that this user
// owns and no one else can write. Without XDG_RUNTIME_DIR the runtime directory
// is /tmp/kubetail-<uid>, whose name another user can take once it is removed,
// and whoever owns it could swap the snapshot every command sources or a run's
// kubeconfig.
func checkPrivate(dir string) error {
	info, err := os.Lstat(dir)
	if err != nil {
		return err
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !info.IsDir() || !ok || int(st.Uid) != os.Getuid() || info.Mode().Perm()&0o022 != 0 {
		return fmt.Errorf("%s is not a directory of this user's alone", dir)
	}
	return nil
}
