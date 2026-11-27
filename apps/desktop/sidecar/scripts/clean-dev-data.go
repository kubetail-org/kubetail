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

//go:build ignore

// Deletes the dev app's data, cache, config and state directories, so the
// next `pnpm tauri dev` starts from an empty database and default settings,
// and resyncs its caches.
// With -window-state it deletes only the saved window frame, so the next dev
// launch opens at the default size. Run from the sidecar module:
//
//	go -C sidecar run ./scripts/clean-dev-data.go                 # `make clean-desktop-data`
//	go -C sidecar run ./scripts/clean-dev-data.go -window-state   # `make clean-desktop-window`
//
// The folders are the sidecar's own layout for a dev build, read from the same
// roots (internal/app/layout) by scripts/internal/cleanup, so this deletes
// what the app uses. The leaf always ends in `-dev`, so an installed release's
// folders are never touched. It is Go rather than shell so it runs on Windows
// too. `make uninstall-desktop` is the release's counterpart.
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
	"os"
	"path/filepath"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/scripts/internal/cleanup"
)

// windowStateFile is src-tauri/src/window_state.rs's FILE_NAME.
const windowStateFile = "window-state.json"

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

// dev is the dev build, whose leaf always ends in `-dev`.
var dev = cleanup.Build{Dev: true}

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
	l, err := dev.Layout(os.Getenv)
	if err != nil {
		return err
	}
	if err := dev.RefuseWhileRunning(l); err != nil {
		return err
	}
	for _, dir := range cleanup.TopDirs(l) {
		if err := cleanup.Remove(dir); err != nil {
			return err
		}
	}
	return nil
}

// cleanWindowState deletes the dev app's saved window frame.
func cleanWindowState() error {
	l, err := dev.Layout(stateOnly)
	if err != nil {
		return err
	}
	err = os.Remove(filepath.Join(l.State, windowStateFile))
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	return err
}
