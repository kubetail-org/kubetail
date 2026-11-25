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

package layout

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"
)

// RootsFromEnv reads platform's roots from getenv as the host's path resolver
// does: on Linux each XDG variable when it is absolute, else a folder under
// HOME; on macOS folders under HOME; on Windows LOCALAPPDATA and APPDATA. A
// Unix root holding `..` is refused, named by the variable it came from. On
// Linux HOME is read only for a root that falls back to it.
func RootsFromEnv(platform Platform, getenv func(string) string) (Roots, error) {
	if platform == Windows {
		local := getenv("LOCALAPPDATA")
		if !isAbs(platform, local) {
			return Roots{}, fmt.Errorf("LOCALAPPDATA is %q, not an absolute path", local)
		}
		roots := Roots{LocalData: local, Cache: local, Home: getenv("USERPROFILE")}
		if roaming := getenv("APPDATA"); isAbs(platform, roaming) {
			roots.Config = roaming
		}
		return roots, nil
	}
	home := getenv("HOME")
	if platform == MacOS {
		if err := checkHome(home); err != nil {
			return Roots{}, err
		}
		support := filepath.Join(home, "Library", "Application Support")
		return Roots{Config: support, LocalData: support, Cache: filepath.Join(home, "Library", "Caches"), Home: home}, nil
	}
	roots := Roots{Home: home}
	for _, root := range []struct {
		dst      *string
		variable string
		fallback []string
	}{
		{&roots.Config, "XDG_CONFIG_HOME", []string{".config"}},
		{&roots.LocalData, "XDG_DATA_HOME", []string{".local", "share"}},
		{&roots.Cache, "XDG_CACHE_HOME", []string{".cache"}},
		{&roots.State, "XDG_STATE_HOME", []string{".local", "state"}},
	} {
		dir := getenv(root.variable)
		if !isAbs(platform, dir) {
			if err := checkHome(home); err != nil {
				return Roots{}, err
			}
			*root.dst = filepath.Join(append([]string{home}, root.fallback...)...)
			continue
		}
		if err := RefuseDotDot(root.variable, dir); err != nil {
			return Roots{}, err
		}
		*root.dst = dir
	}
	return roots, nil
}

// checkHome refuses a Unix HOME a root cannot be placed under: a relative one
// would place it under the launcher's working directory.
func checkHome(home string) error {
	if !strings.HasPrefix(home, "/") {
		return fmt.Errorf("HOME is %q, not an absolute path", home)
	}
	return RefuseDotDot("HOME", home)
}

// isAbs reads dir as platform does, whatever OS runs the test.
func isAbs(platform Platform, dir string) bool {
	if platform != Windows {
		return strings.HasPrefix(dir, "/")
	}
	return strings.HasPrefix(dir, `\\`) ||
		len(dir) >= 3 && dir[1] == ':' && (dir[2] == '\\' || dir[2] == '/')
}

// RefuseDotDot refuses a Unix folder holding `..`, named by where it came
// from. The filesystem reads one after a link as the link target's parent,
// while the fences and `filepath.Join` take it out by name, so the two could
// name different folders.
func RefuseDotDot(name, dir string) error {
	if slices.Contains(strings.Split(dir, "/"), "..") {
		return fmt.Errorf("%s is %q, which holds `..`; set it without one", name, dir)
	}
	return nil
}
