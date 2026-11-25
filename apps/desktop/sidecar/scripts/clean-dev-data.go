//go:build ignore

// Deletes the dev app's data, cache, config and state directories, so the
// next `pnpm tauri dev` starts from an empty database and default settings,
// and resyncs its caches.
// With -window-state it deletes only the saved window frame, so the next dev
// launch opens at the default size. Run from the sidecar module:
//
//	go -C sidecar run ./scripts/clean-dev-data.go                 # `make clean-data`
//	go -C sidecar run ./scripts/clean-dev-data.go -window-state   # part of `make clean`
//
// The folders are the sidecar's own layout for a dev build, read from the same
// roots (internal/app/layout), so this deletes what the app uses. The leaf
// always ends in `-dev`, so an installed release's folders are never touched.
// It is Go rather than shell so it runs on Windows too.
//
// It is for a dev app that has quit. It refuses while a sidecar holds the data
// directory's lock, listens in the host's runtime directory or listens on the
// default socket in the temp dir, and while the lock is not a file. Its
// limits: it misses a standalone sidecar given its own -socket and data
// directory; it does not guard against a sidecar starting while it runs; on
// Linux, -window-state needs an absolute HOME even when XDG_STATE_HOME is set;
// and it deletes each root's own folder, so a root configured inside another
// Kubetail folder through a link is not followed.
package main

import (
	"errors"
	"flag"
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

// windowStateFile is src-tauri/src/window_state.rs's FILE_NAME.
const windowStateFile = "window-state.json"

// lockFile is the data directory's lock (internal/app/paths.go's LockFile).
const lockFile = "sidecar.lock"

func main() {
	windowState := flag.Bool("window-state", false, "delete only the saved window frame")
	flag.Parse()
	run := clean
	if *windowState {
		run = cleanWindowState
	}
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "clean-dev-data:", err)
		os.Exit(1)
	}
}

// devLayout is where a dev build keeps each kind, as the sidecar places it,
// read from getenv's roots.
func devLayout(getenv func(string) string) (layout.Layout, error) {
	roots, err := layout.RootsFromEnv(layout.Current(), layout.KnownFolders(getenv))
	if err != nil {
		return layout.Layout{}, err
	}
	return layout.Of(layout.Current(), true, roots), nil
}

// stateOnly is the environment with every root but the state's unset, so
// they fall back under the home and a bad one does not stop a cleanup that
// reads the state alone.
func stateOnly(name string) string {
	switch name {
	case "XDG_DATA_HOME", "XDG_CACHE_HOME", "XDG_CONFIG_HOME":
		return ""
	}
	return os.Getenv(name)
}

func clean() error {
	l, err := devLayout(os.Getenv)
	if err != nil {
		return err
	}
	if err := refuseWhileRunning(l); err != nil {
		return err
	}
	for _, dir := range topDirs(l) {
		if err := remove(dir); err != nil {
			return err
		}
	}
	return nil
}

// cleanWindowState deletes the dev app's saved window frame.
func cleanWindowState() error {
	l, err := devLayout(stateOnly)
	if err != nil {
		return err
	}
	err = os.Remove(filepath.Join(l.State, windowStateFile))
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	return err
}

// topDirs is each folder Kubetail makes on a platform root: the data, cache,
// config and state leaves on Linux, the app folder, cache and logs on macOS,
// the app folders on the local data and config roots on Windows. Each is deleted whole, so a
// linked one is unlinked rather than followed, and the folders inside it are
// never visited on their own.
func topDirs(l layout.Layout) []string {
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

// refuseWhileRunning fails while a dev sidecar runs: one holding the data
// directory's lock, or one the host started, whose KUBETAIL_DATA_DIR may have
// moved its lock but not its socket.
func refuseWhileRunning(l layout.Layout) error {
	if runtime.GOOS == "windows" {
		return refuseWhileHeld(l.App)
	}
	if err := refuseWhileListening(); err != nil {
		return err
	}
	// The sidecar only ever makes the lock as a file. Opening anything else
	// could make one, outside the dev folders too, so it is left to the user.
	path := filepath.Join(l.Data, lockFile)
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
		return fmt.Errorf("a dev sidecar is using %s; quit it first", l.Data)
	}
	if err != nil {
		return err
	}
	return lock.Close()
}

// refuseWhileListening looks for a live sidecar socket in the dev runtime
// directory, and in the temp dir, where a sidecar run by hand listens by
// default (ipc.DefaultSocketPath). A socket a crashed run left behind refuses
// the dial, so it does not count.
func refuseWhileListening() error {
	dirs, err := runtimeDirs()
	if err != nil {
		return err
	}
	dirs = append(dirs, os.TempDir())
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
			return fmt.Errorf("a sidecar is listening on %s; quit it first", sock)
		}
	}
	return nil
}

// runtimeDirs mirrors paths.rs's runtime_dir: $XDG_RUNTIME_DIR/kubetail-dev,
// else kubetail-dev-<uid> under the temp dir, or a random sibling of it when
// that name was refused.
func runtimeDirs() ([]string, error) {
	if dir := os.Getenv("XDG_RUNTIME_DIR"); filepath.IsAbs(dir) {
		// Before the stat: a link the host resolved through at launch may be
		// gone, and the temp dir would then be searched instead.
		if err := layout.RefuseDotDot("XDG_RUNTIME_DIR", dir); err != nil {
			return nil, err
		}
		if info, err := os.Stat(dir); err == nil && info.IsDir() {
			return []string{filepath.Join(dir, "kubetail-dev")}, nil
		}
	}
	temp := os.TempDir()
	if err := layout.RefuseDotDot("TMPDIR", temp); err != nil {
		return nil, err
	}
	base := filepath.Join(temp, fmt.Sprintf("kubetail-dev-%d", os.Geteuid()))
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
		return fmt.Errorf("%s is in use; quit the dev sidecar first: %w", app, err)
	}
	return os.Rename(aside, app)
}

func remove(dir string) error {
	if _, err := os.Lstat(dir); errors.Is(err, fs.ErrNotExist) {
		fmt.Printf("%s: already gone\n", dir)
		return nil
	} else if err != nil {
		return err
	}
	fmt.Printf("removing %s\n", dir)
	if os.RemoveAll(dir) == nil {
		return nil
	}
	// A chat's workspace can hold a read-only directory, whose entries cannot
	// be removed until it is writable again.
	_ = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err == nil && d.IsDir() {
			_ = os.Chmod(path, 0o700)
		}
		return nil
	})
	return os.RemoveAll(dir)
}
