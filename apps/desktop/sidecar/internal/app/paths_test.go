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
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/app/layout"
)

// Each path is where the tree on paths puts it.
func TestEveryPathIsUnderItsKind(t *testing.T) {
	data, cache, runtime := filepath.FromSlash("/d"), filepath.FromSlash("/c"), filepath.FromSlash("/r")
	p := pathsOf(Config{Folders: Folders{Data: data, Cache: cache, Runtime: runtime}})

	for want, got := range map[string]string{
		filepath.Join(data, "sidecar.lock"): p.LockFile,
		filepath.Join(data, "app.db"):       p.AppDBFile,
		filepath.Join(data, "chats"):        p.ChatsDir,
		filepath.Join(data, "monitor"):      p.MonitorDir,
		filepath.Join(data, "beehive.db"):   p.Cluster.BeehiveDBFile,
		filepath.Join(cache, "kube"):        p.Cluster.KubestoreDir,
		filepath.Join(cache, "logs"):        p.LogsDir,
		filepath.Join(cache, "kubectl"):     p.Bash.KubectlDir,
		filepath.Join(runtime, "shell"):     p.Bash.ShellDir,
		filepath.Join(runtime, "runs"):      p.Bash.RunsDir,
		filepath.Join(cache, "tmp"):         p.Bash.TmpDir,
	} {
		assert.Equal(t, want, got)
	}
	assert.Equal(t, []string{data, cache, runtime}, p.Bash.DeniedDirs, "a sandboxed run reads none of the three")
}

func TestTheLogDirectoryIsKubetails(t *testing.T) {
	data, cache, runtime := filepath.FromSlash("/d"), filepath.FromSlash("/c"), filepath.FromSlash("/r")
	logs := filepath.FromSlash("/u/Library/Logs/Kubetail")
	p := pathsOf(Config{Folders: Folders{Data: data, Cache: cache, Runtime: runtime, Layout: layout.Layout{Logs: logs}}})
	assert.Equal(t, []string{data, cache, runtime, logs}, p.Bash.DeniedDirs,
		"a log directory outside the three is one of Kubetail's directories, which the file tools are fenced out of too")

	nested := filepath.Join(data, "logs")
	p = pathsOf(Config{Folders: Folders{Data: data, Cache: cache, Runtime: runtime, Layout: layout.Layout{Logs: nested}}})
	assert.Equal(t, []string{data, cache, runtime, nested}, p.Bash.DeniedDirs,
		"one under the data directory keeps its entry, since the file tools compare folders as files")

	p = pathsOf(Config{Folders: Folders{Data: data, Cache: cache, Runtime: runtime, Layout: layout.Layout{Logs: data}}})
	assert.Equal(t, []string{data, cache, runtime}, p.Bash.DeniedDirs, "one that is the data directory adds nothing")
}

func TestTheStateDirectoryIsKubetails(t *testing.T) {
	data, cache, runtime := filepath.FromSlash("/d"), filepath.FromSlash("/c"), filepath.FromSlash("/r")
	state := filepath.FromSlash("/u/.local/state/kubetail")
	logs := filepath.Join(state, "logs")
	p := pathsOf(Config{Folders: Folders{Data: data, Cache: cache, Runtime: runtime, Layout: layout.Layout{Logs: logs, State: state}}})
	assert.Equal(t, []string{data, cache, runtime, logs, state}, p.Bash.DeniedDirs,
		"the state directory holds the window frame and the webview's profile beside the logs")

	p = pathsOf(Config{Folders: Folders{Data: data, Cache: cache, Runtime: runtime, Layout: layout.Layout{State: data}}})
	assert.Equal(t, []string{data, cache, runtime}, p.Bash.DeniedDirs, "one that is the data directory adds nothing")
}

func TestTheAppFolderIsKubetails(t *testing.T) {
	app := filepath.FromSlash("/u/Library/Application Support/Kubetail")
	data, state := filepath.Join(app, "data"), filepath.Join(app, "state")
	cache, runtime := filepath.FromSlash("/c"), filepath.FromSlash("/r")
	p := pathsOf(Config{Folders: Folders{Data: data, Cache: cache, Runtime: runtime, Layout: layout.Layout{State: state, App: app}}})
	assert.Equal(t, []string{data, cache, runtime, state, app}, p.Bash.DeniedDirs,
		"the folder holding the kinds is fenced beside them, so nothing is written among them")
}

// Windows keeps the config in a folder of its own on the roaming root, apart
// from the app folder.
func TestTheConfigsAppFolderIsKubetails(t *testing.T) {
	app := filepath.FromSlash("/u/AppData/Local/Kubetail")
	appOnConfigRoot := filepath.FromSlash("/u/AppData/Roaming/Kubetail")
	data, config := filepath.Join(app, "data"), filepath.Join(appOnConfigRoot, "config")
	cache, runtime := filepath.Join(app, "cache"), filepath.Join(app, "run")
	cfg := Config{Folders: Folders{Data: data, Cache: cache, Runtime: runtime, Layout: layout.Layout{Config: config, App: app, AppOnConfigRoot: appOnConfigRoot}}}
	assert.Equal(t, []string{data, cache, runtime, config, app, appOnConfigRoot}, pathsOf(cfg).Bash.DeniedDirs,
		"the config's folder is fenced beside the config, so nothing is written next to it")

	cfg.Folders.Layout.AppOnConfigRoot = app
	assert.Equal(t, []string{data, cache, runtime, config, app}, pathsOf(cfg).Bash.DeniedDirs,
		"one that is the app folder adds nothing")
}

func TestTheConfigDirectoryIsKubetails(t *testing.T) {
	data, cache, runtime := filepath.FromSlash("/d"), filepath.FromSlash("/c"), filepath.FromSlash("/r")
	config := filepath.FromSlash("/u/.config/Kubetail")
	p := pathsOf(Config{Folders: Folders{Data: data, Cache: cache, Runtime: runtime, Layout: layout.Layout{Config: config}}})
	assert.Equal(t, []string{data, cache, runtime, config}, p.Bash.DeniedDirs,
		"the config directory holds host.json, which the file tools are fenced out of")

	p = pathsOf(Config{Folders: Folders{Data: data, Cache: cache, Runtime: runtime, Layout: layout.Layout{Config: data}}})
	assert.Equal(t, []string{data, cache, runtime}, p.Bash.DeniedDirs, "one that is the data directory adds nothing")

	nested := filepath.Join(data, "config")
	p = pathsOf(Config{Folders: Folders{Data: data, Cache: cache, Runtime: runtime, Layout: layout.Layout{Config: nested}}})
	assert.Equal(t, []string{data, cache, runtime, nested}, p.Bash.DeniedDirs,
		"one under another keeps its entry, since the file tools compare folders as files")
}

// A debug build can move the data and cache directories off the layout; the
// layout's own are still Kubetail's.
func TestTheLayoutsDataAndCacheAreKubetails(t *testing.T) {
	data, cache, runtime := filepath.FromSlash("/d"), filepath.FromSlash("/c"), filepath.FromSlash("/r")
	l := layout.Layout{Data: filepath.FromSlash("/l/data"), Cache: filepath.FromSlash("/l/cache")}
	p := pathsOf(Config{Folders: Folders{Data: data, Cache: cache, Runtime: runtime, Layout: l}})
	assert.Equal(t, []string{data, cache, runtime, l.Data, l.Cache}, p.Bash.DeniedDirs)
}

// Every platform's layout, resolved on real folders, is fenced whole: a
// folder inside another keeps its own entry, since the file tools and the
// grant zones compare folders as files, and a second name of a nested folder,
// such as a bind mount's source, matches only the folder itself.
func TestEveryResolvedFolderIsFenced(t *testing.T) {
	for _, c := range []struct {
		name     string
		platform layout.Platform
		roots    func(root string) layout.Roots
	}{
		{"linux", layout.Linux, linuxRoots},
		{"macos", layout.MacOS, macOSRoots},
		{"windows", layout.Windows, windowsRoots},
		{"windows with one root", layout.Windows, sharedWindowsRoots},
	} {
		t.Run(c.name, func(t *testing.T) {
			root := scratch(t)
			n := namedOn(c.platform, root, c.roots(root))
			for _, dir := range n.Layout.Dirs() {
				require.NoError(t, os.MkdirAll(dir, 0o700))
			}
			f, err := ResolveFolders(n)
			require.NoError(t, err)

			fence := pathsOf(Config{Folders: f}).Bash.DeniedDirs

			for _, dir := range f.Layout.Dirs() {
				assert.Contains(t, fence, dir)
			}
		})
	}
}
