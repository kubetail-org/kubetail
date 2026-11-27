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

// Package cleanup finds and deletes what Kubetail keeps on this machine, for
// scripts/clean-dev-data.go and scripts/uninstall-desktop.go. The folders are
// the sidecar's own layout (internal/app/layout), so it deletes what the app
// uses.
package cleanup

import (
	"errors"
	"fmt"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"time"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/app/layout"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/filelock"
)

// lockFile is the data directory's lock (internal/app/paths.go's LockFile).
const lockFile = "sidecar.lock"

// Build is a dev build or a release, whose folders, runtime directory and
// keyring service differ.
type Build struct {
	Dev bool
}

// Layout is where b keeps each kind on this platform, read from getenv's
// roots.
func (b Build) Layout(getenv func(string) string) (layout.Layout, error) {
	roots, err := layout.RootsFromEnv(layout.Current(), layout.KnownFolders(getenv))
	if err != nil {
		return layout.Layout{}, err
	}
	return layout.Of(layout.Current(), b.Dev, roots), nil
}

// runtimeLeaf is paths.rs's RUNTIME_DIR_NAME.
func (b Build) runtimeLeaf() string {
	if b.Dev {
		return "kubetail-dev"
	}
	return "kubetail"
}

// TopDirs is each folder Kubetail makes on a platform root: the data, cache,
// config and state leaves on Linux, the app folder, cache and logs on macOS,
// the app folders on the local data and config roots on Windows. Each is
// deleted whole, so a linked one is unlinked rather than followed, and the
// folders inside it are never visited on their own.
func TopDirs(l layout.Layout) []string {
	holders := []string{l.App, l.AppOnConfigRoot, l.State}
	var dirs []string
	for _, dir := range l.Dirs() {
		inside := slices.ContainsFunc(holders, func(holder string) bool {
			return holder != "" && under(holder, dir)
		})
		if !inside {
			dirs = append(dirs, dir)
		}
	}
	return dirs
}

// under reports whether dir is strictly under parent, by name. A folder the
// OS takes as parent itself, in any spelling, is not.
func under(parent, dir string) bool {
	rel, err := filepath.Rel(parent, dir)
	return err == nil && rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}

// RefuseWhileRunning fails while b's sidecar runs: one holding the data
// directory's lock, or one the host started, whose KUBETAIL_DATA_DIR may have
// moved its lock but not its socket. It misses a standalone sidecar given its
// own -socket and data directory, and does not guard against one starting
// after it returns.
func (b Build) RefuseWhileRunning(l layout.Layout) error {
	if runtime.GOOS == "windows" {
		return refuseWhileHeld(l.App)
	}
	dirs, err := b.RuntimeDirs()
	if err != nil {
		return err
	}
	if err := refuseWhileListening(append(dirs, os.TempDir())); err != nil {
		return err
	}
	return refuseWhileLocked(filepath.Join(l.Data, lockFile))
}

// refuseWhileLocked fails while a process holds the lock at path.
func refuseWhileLocked(path string) error {
	// The sidecar only ever makes the lock as a file. Opening anything else
	// could make one, outside Kubetail's folders too, so it is left to the
	// user.
	info, err := os.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("%s is not a file; check it and remove it by hand", path)
	}
	lock, err := filelock.TryLock(path)
	if errors.Is(err, filelock.ErrLocked) {
		return fmt.Errorf("a sidecar is using %s; quit Kubetail first", filepath.Dir(path))
	}
	if err != nil {
		return err
	}
	return lock.Close()
}

// refuseWhileListening looks for a live sidecar socket in each of dirs: the
// runtime directories, and the temp dir, where a sidecar run by hand listens
// by default (ipc.DefaultSocketPath). A socket a crashed run left behind
// refuses the dial, so it does not count.
func refuseWhileListening(dirs []string) error {
	for _, dir := range dirs {
		socks, err := filepath.Glob(filepath.Join(dir, "kubetail-sidecar-*.sock"))
		if err != nil {
			return err
		}
		for _, sock := range socks {
			conn, err := net.DialTimeout("unix", sock, time.Second)
			if err != nil {
				continue
			}
			conn.Close()
			return fmt.Errorf("a sidecar is listening on %s; quit Kubetail first", sock)
		}
	}
	return nil
}

// RuntimeDirs is every directory b's host may have taken as its Unix runtime
// directory, whether or not it exists.
func (b Build) RuntimeDirs() ([]string, error) {
	return runtimeDirs(b.runtimeLeaf(), os.Getenv("XDG_RUNTIME_DIR"), os.TempDir(), os.Geteuid())
}

// runtimeDirs mirrors paths.rs's runtime_dir: <xdg>/<leaf>, else <leaf>-<uid>
// under temp, or a random sibling of it when that name was refused.
func runtimeDirs(leaf, xdg, temp string, uid int) ([]string, error) {
	if filepath.IsAbs(xdg) {
		// Before the stat: a link the host resolved through at launch may be
		// gone, and the temp dir would then be searched instead.
		if err := layout.RefuseDotDot("XDG_RUNTIME_DIR", xdg); err != nil {
			return nil, err
		}
		if info, err := os.Stat(xdg); err == nil && info.IsDir() {
			return []string{filepath.Join(xdg, leaf)}, nil
		}
	}
	if err := layout.RefuseDotDot("TMPDIR", temp); err != nil {
		return nil, err
	}
	base := filepath.Join(temp, fmt.Sprintf("%s-%d", leaf, uid))
	siblings, _ := filepath.Glob(base + "-??????")
	return append([]string{base}, siblings...), nil
}

// refuseWhileHeld renames the app folder and back. Windows refuses to rename
// a directory while a file anywhere under it is open, as the running
// sidecar's lock is.
func refuseWhileHeld(app string) error {
	aside := app + ".clean"
	if err := os.Rename(app, aside); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("%s is in use; quit Kubetail first: %w", app, err)
	}
	return os.Rename(aside, app)
}

// Remove deletes path, a folder or a file, saying so.
func Remove(path string) error {
	if _, err := os.Lstat(path); errors.Is(err, fs.ErrNotExist) {
		fmt.Printf("%s: already gone\n", path)
		return nil
	} else if err != nil {
		return err
	}
	fmt.Printf("removing %s\n", path)
	if os.RemoveAll(path) == nil {
		return nil
	}
	// A chat's workspace can hold a read-only directory, whose entries cannot
	// be removed until it is writable again.
	_ = filepath.WalkDir(path, func(p string, d fs.DirEntry, err error) error {
		if err == nil && d.IsDir() {
			_ = os.Chmod(p, 0o700)
		}
		return nil
	})
	return os.RemoveAll(path)
}
