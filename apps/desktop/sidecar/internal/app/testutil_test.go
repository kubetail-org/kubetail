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

package app

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/app/layout"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/realpath"
)

// scratch is a folder for the test, as the kernel names it.
func scratch(t *testing.T) string {
	t.Helper()
	c, err := realpath.Canonical([]string{t.TempDir()})
	require.NoError(t, err)
	return c[0]
}

// namedOn is platform's layout over roots under root, root its profile, and
// a runtime folder of its own.
func namedOn(platform layout.Platform, root string, roots layout.Roots) Named {
	l := layout.Of(platform, false, roots)
	runtime := l.Runtime
	if runtime == "" {
		runtime = filepath.Join(root, "run")
	}
	return Named{Platform: platform, Layout: l, Data: l.Data, Cache: l.Cache, Runtime: runtime, Profile: root}
}

// linuxRoots is each XDG root its own folder under root.
func linuxRoots(root string) layout.Roots {
	return layout.Roots{
		Config: filepath.Join(root, "config"), LocalData: filepath.Join(root, "data"),
		Cache: filepath.Join(root, "cache"), State: filepath.Join(root, "state"), Home: root,
	}
}

// linuxNamed is Linux's layout over linuxRoots.
func linuxNamed(root string) Named {
	return namedOn(layout.Linux, root, linuxRoots(root))
}

// macOSRoots is macOS's roots under root: one for the config and the data,
// one for the cache.
func macOSRoots(root string) layout.Roots {
	support := filepath.Join(root, "Library", "Application Support")
	return layout.Roots{Config: support, LocalData: support, Cache: filepath.Join(root, "Library", "Caches"), Home: root}
}

// windowsRoots is Windows' roots under root, local and roaming apart.
func windowsRoots(root string) layout.Roots {
	local, roaming := filepath.Join(root, "AppData", "Local"), filepath.Join(root, "AppData", "Roaming")
	return layout.Roots{Config: roaming, LocalData: local, Cache: local, Home: root}
}

// sharedWindowsRoots is Windows' roots under root, every one the local
// folder, so the app folder and the one on the config root are one.
func sharedWindowsRoots(root string) layout.Roots {
	local := filepath.Join(root, "AppData", "Local")
	return layout.Roots{Config: local, LocalData: local, Cache: local, Home: root}
}

// testFolders is ResolveFolders over this platform's layout on roots under
// t.TempDir(), the runtime folder in a scratch folder of its own on Unix. It
// fails the test on an error.
func testFolders(t *testing.T) Folders {
	t.Helper()
	root := scratch(t)
	n := namedOn(layout.Current(), root, linuxRoots(root))
	if n.Layout.Runtime == "" {
		n.Runtime = filepath.Join(t.TempDir(), "run")
	}
	f, err := ResolveFolders(n)
	require.NoError(t, err)
	return f
}
