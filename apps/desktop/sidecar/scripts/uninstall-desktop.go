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

// Uninstalls Kubetail Desktop and deletes everything a release keeps: the
// app, its data, cache, config, state, logs, webview profile and runtime
// directories, and its keyring entries. It prints what it will delete and
// asks first, unless YES=1 is set. Run from the sidecar module:
//
//	go -C sidecar run ./scripts/uninstall-desktop.go   # `make uninstall-desktop`
//
// It refuses while a release runs. A dev build's folders and keyring entries
// are left alone; `make clean-desktop-data` clears its folders. On macOS
// WKWebView's folders are shared with a dev build, so its localStorage goes
// too. It is Go rather than shell so it runs on Windows too.
package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/zalando/go-keyring"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/app/layout"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/scripts/internal/cleanup"
)

func main() {
	if err := uninstall(); err != nil {
		fmt.Fprintln(os.Stderr, "uninstall-desktop:", err)
		os.Exit(1)
	}
}

func uninstall() error {
	l, err := cleanup.Release.Layout(os.Getenv)
	if err != nil {
		return err
	}
	if err := cleanup.Release.RefuseWhileRunning(l); err != nil {
		return err
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	apps, notes, err := cleanup.FindApps(home)
	if err != nil {
		return err
	}
	var runtimeDirs []string
	if layout.Current() != layout.Windows {
		dirs, err := cleanup.Release.RuntimeDirs()
		if err != nil {
			return err
		}
		runtimeDirs = cleanup.OwnedDirs(dirs)
	}
	paths, err := cleanup.Existing(cleanup.UninstallPaths(layout.Current(), l, home, runtimeDirs))
	if err != nil {
		return err
	}

	fmt.Println("This removes Kubetail Desktop and every file it keeps. No migration brings them back.")
	for _, app := range apps {
		fmt.Println("  uninstall", app.Name)
	}
	for _, path := range paths {
		fmt.Println("  delete   ", path)
	}
	fmt.Printf("  delete    every keyring entry under the service %q\n", cleanup.KeyringService)
	for _, note := range notes {
		fmt.Println("Note:", note)
	}
	if os.Getenv("YES") != "1" && !cleanup.Confirm(os.Stdin, os.Stdout, "Continue?") {
		return errors.New("nothing deleted")
	}

	// Each step runs whatever an earlier one hit, so one failure leaves no
	// more behind than it must.
	var errs []error
	for _, app := range apps {
		if err := app.Remove(); err != nil {
			errs = append(errs, fmt.Errorf("uninstalling %s: %w", app.Name, err))
		}
	}
	for _, path := range paths {
		if err := cleanup.Remove(path); err != nil {
			errs = append(errs, err)
		}
	}
	fmt.Printf("deleting the keyring entries under %q\n", cleanup.KeyringService)
	if err := keyring.DeleteAll(cleanup.KeyringService); err != nil {
		errs = append(errs, fmt.Errorf("deleting the keyring entries: %w", err))
	}
	return errors.Join(errs...)
}
