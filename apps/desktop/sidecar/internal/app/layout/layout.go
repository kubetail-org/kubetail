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

// Package layout places Kubetail's folders: the sidecar's data and cache and
// the host's config, state and logs, each on its platform's root. It mirrors
// `layout` in src-tauri/src/paths.rs, and sidecar/internal/app/layout/testdata/layouts.json pins
// the two to one answer for every platform.
package layout

import (
	"path/filepath"
	"runtime"
	"slices"
)

// Platform is the platform a layout is for. Of is a pure function of it, so
// every platform's layout is tested on every OS.
type Platform string

const (
	Linux   Platform = "linux"
	MacOS   Platform = "macos"
	Windows Platform = "windows"
)

// Current is the platform this build runs on.
func Current() Platform {
	return platformOf(runtime.GOOS)
}

// platformOf is the platform of goos, a pure function so every OS tests every
// arm.
func platformOf(goos string) Platform {
	switch goos {
	case "linux":
		return Linux
	case "darwin":
		return MacOS
	}
	return Windows
}

// Roots are the platform folders Kubetail's own hang off, read by RootsFromEnv.
type Roots struct {
	// Config is empty when the platform names no config root.
	Config    string `json:"config"`
	LocalData string `json:"local_data"`
	Cache     string `json:"cache"`
	Home      string `json:"home"`
	// State is XDG's state root, which only Linux reads.
	State string `json:"state"`
}

// Layout is where each kind of folder sits on one platform, whether or not it
// exists. An empty field is a kind the platform does not have. Its JSON is
// what the sidecar reports to the host before READY.
type Layout struct {
	// App is the folder macOS and Windows keep several kinds in.
	App string `json:"app,omitempty"`
	// AppOnConfigRoot is Windows' app folder on the config root, which holds
	// the config alone. It is App when the config root is the local data root.
	AppOnConfigRoot string `json:"app_on_config_root,omitempty"`
	Data            string `json:"data"`
	Config          string `json:"config,omitempty"`
	State           string `json:"state"`
	Cache           string `json:"cache"`
	Logs            string `json:"logs"`
	// Runtime is Windows' alone: on Unix the host places it under the
	// session's runtime root, which a squatted name can move.
	Runtime string `json:"runtime,omitempty"`
}

// fields is every folder of l, the sidecar's own first.
func (l *Layout) fields() []*string {
	return []*string{&l.Data, &l.Cache, &l.Logs, &l.Config, &l.State, &l.Runtime, &l.App, &l.AppOnConfigRoot}
}

// Dirs is every folder l names, the sidecar's data and cache first, each once.
func (l Layout) Dirs() []string {
	var dirs []string
	for _, dir := range l.fields() {
		if *dir != "" && !slices.Contains(dirs, *dir) {
			dirs = append(dirs, *dir)
		}
	}
	return dirs
}

// appDirName is the leaf each folder takes under its root. A dev build takes
// a `-dev` sibling, so a dev run never shares an installed release's folders.
// Linux names folders in lowercase; macOS and Windows show the product name.
func appDirName(platform Platform, dev bool) string {
	name := "Kubetail"
	if platform == Linux {
		name = "kubetail"
	}
	if dev {
		name += "-dev"
	}
	return name
}

// Of places each kind on platform, in a folder of its own:
//
//   - Linux gives every kind a root (XDG's), so each is a leaf on its root.
//   - macOS keeps the cache and the logs in the platform's folders for them;
//     the data, config and state are subfolders of one app folder.
//   - Windows keeps every local kind in one app folder under the local data
//     root, the logs in the state, and the config in an app folder on the
//     config root.
func Of(platform Platform, dev bool, roots Roots) Layout {
	leaf := appDirName(platform, dev)
	app := filepath.Join(roots.LocalData, leaf)
	switch platform {
	case Linux:
		state := filepath.Join(roots.State, leaf)
		return Layout{
			Data:   app,
			Config: joinIf(roots.Config, leaf),
			State:  state,
			Cache:  filepath.Join(roots.Cache, leaf),
			Logs:   filepath.Join(state, "logs"),
		}
	case MacOS:
		l := Layout{
			App:   app,
			Data:  filepath.Join(app, "data"),
			State: filepath.Join(app, "state"),
			Cache: filepath.Join(roots.Cache, leaf),
			Logs:  filepath.Join(roots.Home, "Library", "Logs", leaf),
		}
		// In the app folder whatever the config root is, which decides only
		// whether there is a config directory.
		if roots.Config != "" {
			l.Config = filepath.Join(app, "config")
		}
		return l
	}
	appOnConfigRoot := joinIf(roots.Config, leaf)
	return Layout{
		App:             app,
		AppOnConfigRoot: appOnConfigRoot,
		Data:            filepath.Join(app, "data"),
		Config:          joinIf(appOnConfigRoot, "config"),
		State:           filepath.Join(app, "state"),
		Cache:           filepath.Join(app, "cache"),
		Logs:            filepath.Join(app, "state", "logs"),
		Runtime:         filepath.Join(app, "run"),
	}
}

// joinIf is name on root, or empty when there is no root.
func joinIf(root, name string) string {
	if root == "" {
		return ""
	}
	return filepath.Join(root, name)
}
