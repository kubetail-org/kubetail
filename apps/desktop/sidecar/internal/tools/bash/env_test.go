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

package bash

import (
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// processEnv is a process environment carrying credentials a sandboxed run
// must not inherit.
var processEnv = []string{
	"PATH=/usr/local/bin:/usr/bin",
	"HOME=/Users/ana",
	"AWS_ACCESS_KEY_ID=AKIAEXAMPLE",
	"KUBECONFIG=/Users/ana/.kube/config",
	"SSH_AUTH_SOCK=/tmp/ssh.sock",
	"GITHUB_TOKEN=ghp_example",
	"LANG=en_US.UTF-8",
	"LC_ALL=en_US.UTF-8",
	"LC_CTYPE=UTF-8",
	"TMPDIR=/var/folders/x/T/",
	"TERM=xterm-256color",
}

var kubetailVars = []string{"KUBETAIL=1", "KUBETAIL_SIDECAR_PID=10", "KUBETAIL_DESKTOP_PID=9"}

// A run with no cluster has no kubeconfig and no kubectl cache to name.
func TestWithNoClusterTheEnvironmentNamesNoKubeconfig(t *testing.T) {
	got := sandboxedRunEnv(processEnv, kubetailVars, "/usr/bin", "/data/ws", "/data/ws", &runDir{path: "/tmp/r"}, nil, "/th", nil, nil, nil)

	for _, kv := range got {
		assert.NotRegexp(t, `^KUBE(CONFIG|CACHEDIR)=`, kv)
	}
}

// The run's PATH is the one it is handed, never the process's.
func TestThePathIsTheRunsOwn(t *testing.T) {
	rd := &runDir{path: "/run/r", tmp: "/cache/t"}
	got := sandboxedRunEnv([]string{"PATH=/sidecar/bin", "LANG=C"}, nil, "/opt/bin:/usr/bin", "/ws", "/ws", rd, nil, "/th", nil, nil, nil)

	assert.Equal(t, []string{"PATH=/opt/bin:/usr/bin", "HOME=/ws", "PWD=/ws", "TMPDIR=/cache/t", "ZDOTDIR=/run/r", "LANG=C", "TERM=dumb"}, got[:7])
	assert.NotContains(t, strings.Join(got, "\n"), "/sidecar/bin")
}

// A run outside the sandbox keeps the process's environment whole, then what
// Kubetail adds, then PWD.
func TestAnOutsideRunKeepsItsEnvironment(t *testing.T) {
	got := outsideEnv(processEnv, kubetailVars, "/data/ws")

	assert.Equal(t, append(append(append([]string{}, processEnv...), kubetailVars...), "PWD=/data/ws"), got)
}

// The sidecar's LANG passes; with none, the platform's default: on macOS
// en_US.UTF-8, on Linux C.UTF-8 where the system has that locale, else C.
func TestTheLocale(t *testing.T) {
	rd := &runDir{path: "/run/r", tmp: "/cache/t"}
	assert.Contains(t, sandboxedRunEnv([]string{"LANG=de_DE.UTF-8"}, nil, "/usr/bin", "/ws", "/ws", rd, nil, "/th", nil, nil, nil), "LANG=de_DE.UTF-8")
	assert.Contains(t, sandboxedRunEnv(nil, nil, "/usr/bin", "/ws", "/ws", rd, nil, "/th", nil, nil, nil), "LANG="+defaultLang(runtime.GOOS, fileExists))

	none := func(string) bool { return false }
	assert.Equal(t, "en_US.UTF-8", defaultLang("darwin", none))
	assert.Equal(t, "C", defaultLang("linux", none))
	for _, dir := range []string{"/usr/lib/locale/C.utf8", "/usr/lib/locale/C.UTF-8"} {
		assert.Equal(t, "C.UTF-8", defaultLang("linux", func(p string) bool { return p == dir }), dir)
	}
}

// The toolchain's variables follow the fixed ones, before Kubetail's own.
func TestTheToolchainsVariablesRideTheEnvironment(t *testing.T) {
	rd := &runDir{path: "/run/r", tmp: "/cache/t"}
	toolchain := []string{"NVM_DIR=/home/ana/.nvm", "ASDF_NODEJS_VERSION=20.1.0"}

	got := sandboxedRunEnv([]string{"LANG=C"}, kubetailVars, "/usr/bin", "/ws", "/ws", rd, nil, "/th", nil, toolchain, nil)

	home := toolHomeEnv("/th")
	i := slices.Index(got, home[len(home)-1])
	assert.Equal(t, toolchain, got[i+1:i+3])
	assert.Equal(t, kubetailVars, got[i+3:])
}

// The cloud platforms' variables follow the toolchain's, before Kubetail's own.
func TestTheCloudPlatformsVariablesRideTheEnvironment(t *testing.T) {
	rd := &runDir{path: "/run/r", tmp: "/cache/t"}
	toolchain, cloud := []string{"NVM_DIR=/home/ana/.nvm"}, []string{"GH_HOST=github.localhost"}

	got := sandboxedRunEnv(nil, kubetailVars, "/usr/bin", "/ws", "/ws", rd, nil, "/th", nil, toolchain, cloud)

	i := slices.Index(got, "NVM_DIR=/home/ana/.nvm")
	assert.Equal(t, cloud, got[i+1:i+2])
	assert.Equal(t, kubetailVars, got[i+2:])
}

// Each line of ~/.tool-versions whose tool and first version are plain
// becomes ASDF_<TOOL>_VERSION, spelled as asdf spells it; any other line is
// skipped.
func TestAsdfVersionsComeFromToolVersions(t *testing.T) {
	text := "nodejs 20.1.0 18.0.0\n" +
		"golang  1.22.3 # pinned\n" +
		"kube-linter v0.6.8\n" +
		"python 3.12.1+local\n" +
		"# a comment\n\n" +
		"Bad-Name 1.0\n" +
		"ruby $(evil)\n" +
		"terraform\n"

	assert.Equal(t, []string{
		"ASDF_NODEJS_VERSION=20.1.0",
		"ASDF_GOLANG_VERSION=1.22.3",
		"ASDF_KUBE_LINTER_VERSION=v0.6.8",
		"ASDF_PYTHON_VERSION=3.12.1+local",
	}, parseToolVersions(text))
}

// The versions are read from the user's home, a plain file alone; with none,
// or with something other than a plain file, there are none.
func TestAsdfVersionsAreReadFromTheHome(t *testing.T) {
	home := t.TempDir()
	assert.Empty(t, toolVersions(home))

	require.NoError(t, os.WriteFile(filepath.Join(home, ".tool-versions"), []byte("nodejs 20.1.0\n"), 0o600))
	assert.Equal(t, []string{"ASDF_NODEJS_VERSION=20.1.0"}, toolVersions(home))

	big := filepath.Join(t.TempDir(), "home")
	require.NoError(t, os.MkdirAll(filepath.Join(big, ".tool-versions"), 0o700))
	assert.Empty(t, toolVersions(big), "a folder")
}

func TestLastOfEachNameKeepsTheLaterEntryInOrder(t *testing.T) {
	name := func(e string) string { return strings.ToUpper(strings.SplitN(e, "=", 2)[0]) }
	got := lastOfEachName([]string{"Path=a", "HOME=/h", "EDITOR=vi", "PATH=b", "EDITOR=nvim"}, name)
	assert.Equal(t, []string{"HOME=/h", "PATH=b", "EDITOR=nvim"}, got)
}
