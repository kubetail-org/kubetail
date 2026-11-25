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

package sandbox

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/realpath"
)

var update = flag.Bool("update", false, "rewrite the golden files in testdata")

// fixture is a whole machine under one folder, for the goldens: system roots,
// one of them a link, a home with toolchain folders, one of them inside a
// credential path, every credential path and Kubetail's three directories, the
// runtime one reached through a link. Its roots and toolchain stand in for
// the lists' for the test's life.
type fixture struct {
	base, home, shell, self                 string
	env                                     []string
	roots, brewVar                          []string
	data, cache, runtime                    string
	workspace, tmp, kubectl, runDir, socket string
}

// goldenFiles are the fixture's files, beside the folders mkdirs makes.
var goldenFiles = []string{
	"sys/usr/bin/bash", "sys/etc/hosts", "sys/opt/homebrew/bin/brew", "sys/opt/homebrew/var/db/x",
	"home/apps/bin/tool", "home/tools/cargo/bin/cargo", "home/.docker/bin/docker", "home/.docker/config.json",
	"home/.netrc", "home/.git-credentials", "home/.cargo/credentials", "home/.cargo/credentials.toml",
	"home/.pulumi/credentials.json", "home/.fly/config.yml",
	"app/kubetail-sidecar", "private/run/kubetail/runs/1-a/resolv.conf",
}

func newFixture(t *testing.T) fixture {
	t.Helper()
	base := realpath.Resolved(t.TempDir())
	mkdirs(t, base,
		"home/.kube", "home/.aws", "home/.azure", "home/.config/gcloud", "home/.ssh", "home/.gnupg", "home/.config/gh",
		"home/.local/share/keyrings", "home/Library/Keychains", "home/.cargo/bin",
		"home/apps/kubetail/chats/c/workspace", "cache/kubetail/tmp/1-a", "cache/kubetail/kubectl/c/s",
		"private/run/kubetail/runs/1-a")
	for _, f := range goldenFiles {
		path := filepath.Join(base, filepath.FromSlash(f))
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
		require.NoError(t, os.WriteFile(path, nil, 0o700))
	}
	require.NoError(t, os.Symlink("usr/bin", filepath.Join(base, "sys", "bin")))
	require.NoError(t, os.Symlink("../../tools/cargo/bin/cargo", filepath.Join(base, "home", ".cargo", "bin", "cargo")))
	require.NoError(t, os.Symlink("private/run", filepath.Join(base, "run")))

	f := fixture{
		base: base, home: filepath.Join(base, "home"),
		shell: filepath.Join(base, "sys", "usr", "bin", "bash"),
		self:  filepath.Join(base, "app", "kubetail-sidecar"),
		roots: []string{
			filepath.Join(base, "sys", "usr"), filepath.Join(base, "sys", "bin"),
			filepath.Join(base, "sys", "etc"), filepath.Join(base, "sys", "opt"),
		},
		brewVar: []string{filepath.Join(base, "sys", "opt", "homebrew", "var")},
		data:    filepath.Join(base, "home", "apps", "kubetail"),
		cache:   filepath.Join(base, "cache", "kubetail"),
		runtime: filepath.Join(base, "run", "kubetail"),
	}
	f.env = []string{"PATH=" + strings.Join([]string{
		filepath.Join(base, "sys", "bin"), filepath.Join(base, "sys", "usr", "bin"),
	}, string(filepath.ListSeparator))}
	f.workspace = filepath.Join(f.data, "chats", "c", "workspace")
	f.tmp = filepath.Join(f.cache, "tmp", "1-a")
	f.kubectl = filepath.Join(f.cache, "kubectl", "c", "s")
	f.runDir = filepath.Join(f.runtime, "runs", "1-a")
	f.socket = filepath.Join(f.runDir, "proxy.sock")

	keepLists(t)
	sharedLists.Toolchain = []Location{
		{Name: "apps", Read: []string{"~/apps"}},
		{Name: "cargo", Read: []string{"~/.cargo/bin"}},
		{Name: "docker", Read: []string{"~/.docker/bin"}},
	}
	platformLists.System = f.roots
	return f
}

// run is the run Bash builds over the fixture on s, with a cluster or
// without: its policy is the Workspace policy, with its limits on an 8-CPU
// machine whose kernel counts the run's namespace alone.
func (f fixture) run(s *Sandbox, cluster bool) Run {
	kubetail := []string{f.data, f.cache, f.runtime}
	r := Run{
		Shell: f.shell, Args: []string{"-c", "true"}, Dir: f.workspace, Env: f.env,
		Policy: Policy{
			Files: s.System(f.home, f.shell).Files.Outside(kubetail...),
			Always: AlwaysPolicy{
				Deny: s.Never(f.home), Kubetail: kubetail,
				Read: []string{f.runDir}, Write: []string{f.workspace, f.tmp},
			},
			Limits: Limits{CPUSeconds: 4840, MemoryBytes: 16 << 30, OpenFiles: 4096, Processes: 1024},
		},
	}
	if cluster {
		r.Policy.Always.Write = append(r.Policy.Always.Write, f.kubectl)
		r.Policy.Network.Relays = []Relay{{Port: 6443, Socket: f.socket}}
	}
	return r
}

// withInternet is r with the internet, and the resolver s needs for it in
// the run's directory.
func (f fixture) withInternet(s *Sandbox, r Run) Run {
	r.Policy.Network.Internet = true
	if s.NeedsResolver() {
		r.Policy.Network.Resolver = filepath.Join(f.runDir, "resolv.conf")
	}
	return r
}

// golden compares got, with the fixture's folder as $FIXTURE, to the named
// file in testdata, or rewrites the file under -update.
func (f fixture) golden(t *testing.T, name string, lines []string) {
	t.Helper()
	got := strings.ReplaceAll(strings.Join(lines, "\n")+"\n", f.base, "$FIXTURE")
	path := filepath.Join("testdata", name)
	if *update {
		require.NoError(t, os.MkdirAll("testdata", 0o755))
		require.NoError(t, os.WriteFile(path, []byte(got), 0o644))
		return
	}
	want, err := os.ReadFile(path)
	require.NoError(t, err, "run with -update to write it")
	assert.Equal(t, string(want), got)
}

// aboveFixture reports whether line is the ancestor rule of a folder above
// the fixture's: what lies above it differs from one machine to the next.
func (f fixture) aboveFixture(line string) bool {
	for d := filepath.Dir(f.base); ; d = filepath.Dir(d) {
		if line == `(allow file-read-metadata (literal "`+d+`"))` {
			return true
		}
		if d == filepath.Dir(d) {
			return false
		}
	}
}
