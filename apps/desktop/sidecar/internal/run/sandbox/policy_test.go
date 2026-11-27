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

package sandbox

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/realpath"
)

// foldingCase is a fileOf for a disk that ignores case, which no test can
// mount: each path is the file its lower-case spelling names.
func foldingCase() func(string) (fileID, bool) {
	files := map[string]uint64{}
	return func(path string) (fileID, bool) {
		name := strings.ToLower(path)
		if _, ok := files[name]; !ok {
			files[name] = uint64(len(files))
		}
		return fileID{ino: files[name]}, true
	}
}

// A policy's rules are compared by text, so one naming a folder two ways is
// refused: a Deny in one spelling would be dropped under a Read in the other.
func TestAPolicyNamingOneFolderTwoWaysIsRefused(t *testing.T) {
	t.Parallel()

	native := func(paths ...string) []string {
		for i, p := range paths {
			paths[i] = filepath.FromSlash(p)
		}
		return paths
	}
	require.NoError(t, aliased(native("/home/me/code", "/home/me/code/svc", "/home/me/.ssh"), foldingCase()))

	for name, paths := range map[string][]string{
		"the rules themselves":   {"/home/me/code", "/home/me/Code"},
		"a folder above them":    {"/home/me/.ssh", "/home/ME/code"},
		"a rule and one beneath": {"/home/me/code", "/home/me/CODE/.env"},
	} {
		require.ErrorContains(t, aliased(native(paths...), foldingCase()), "name one file", name)
	}

	// The same folder mounted in two places has two names, each real.
	twice := func(path string) (fileID, bool) {
		return fileID{ino: uint64(len(filepath.Base(path)))}, filepath.Base(path) == "code"
	}
	require.NoError(t, aliased(native("/mnt/a/code", "/srv/b/code"), twice))
}

// A policy fails Check for each thing it refuses, and passes when every
// rule is where a run's rules go.
func TestAPolicyChecks(t *testing.T) {
	t.Parallel()

	base := realpath.Resolved(t.TempDir())
	p := func(rel string) string { return filepath.Join(base, filepath.FromSlash(rel)) }
	workspace := func() Policy {
		return Policy{
			Files: FilePolicy{Read: []string{p("usr"), p("home/.config")}, Write: []string{p("cover")}, Deny: []string{p("usr/var")}},
			Always: AlwaysPolicy{
				Deny:     []string{p("home/.ssh")},
				Kubetail: []string{p("data"), p("cache"), p("runtime")},
				Read:     []string{p("runtime/runs/1"), p("runtime/shell/snapshot.sh")},
				Write:    []string{p("data/chats/c/workspace"), p("cache/tmp/1")},
				// home holds a link Kubetail followed, as ~ holds a linked ~/.cache.
				LinkParents: []string{p("home")},
			},
			Network: NetworkPolicy{Relays: []Relay{{Port: 6443, Socket: p("runtime/runs/1/proxy.sock")}}},
		}
	}
	assert.NoError(t, workspace().Check(), "the workspace policy")
	tie := workspace()
	tie.Files.Deny = append(tie.Files.Deny, p("cover"))
	assert.NoError(t, tie.Check(), "a rule on a Write rule's own path is a tie")

	for name, change := range map[string]func(*Policy){
		"a relative path":                    func(q *Policy) { q.Files.Read = append(q.Files.Read, "usr/lib") },
		"a Files rule beneath a Files Write": func(q *Policy) { q.Files.Read = append(q.Files.Read, p("cover/x")) },
		"a Deny beneath a Files Write": func(q *Policy) {
			q.Files.Write = append(q.Files.Write, p("home/.config"))
			q.Files.Deny = append(q.Files.Deny, p("home/.config/gcloud"))
		},
		"an Always path beneath a Files Write": func(q *Policy) { q.Always.Deny = append(q.Always.Deny, p("cover/.ssh")) },
		"a rule beneath an Always Write":       func(q *Policy) { q.Always.Read = append(q.Always.Read, p("cache/tmp/1/x")) },
		"a Files rule on a Deny path":          func(q *Policy) { q.Files.Deny = append(q.Files.Deny, p("home/.ssh")) },
		"a Files rule inside a Deny path":      func(q *Policy) { q.Files.Read = append(q.Files.Read, p("home/.ssh/keys")) },
		"a Files rule inside a Kubetail path":  func(q *Policy) { q.Files.Read = append(q.Files.Read, p("data/bin")) },
		"a run's own path outside Kubetail's":  func(q *Policy) { q.Always.Read = append(q.Always.Read, p("home/snapshot.sh")) },
		"a run's own path inside a Deny path": func(q *Policy) {
			q.Always.Deny = append(q.Always.Deny, p("data/keys"))
			q.Always.Write = append(q.Always.Write, p("data/keys/w"))
		},
		"a run's own path holding a Deny path": func(q *Policy) {
			q.Always.Deny = append(q.Always.Deny, p("runtime/runs/1/secret"))
		},
		"a Files Write on a link parent": func(q *Policy) {
			q.Files.Write = append(q.Files.Write, p("big"))
			q.Always.LinkParents = append(q.Always.LinkParents, p("big"))
		},
		"a Files Write above a link parent": func(q *Policy) {
			q.Files.Write = append(q.Files.Write, p("big"))
			q.Always.LinkParents = append(q.Always.LinkParents, p("big/cache"))
		},
		"an Always Write above a link parent": func(q *Policy) { q.Always.LinkParents = append(q.Always.LinkParents, p("cache/tmp/1/x")) },
		"a second relay":                      func(q *Policy) { q.Network.Relays = append(q.Network.Relays, Relay{Port: 1, Socket: p("s")}) },
	} {
		t.Run(name, func(t *testing.T) {
			q := workspace()
			change(&q)
			assert.Error(t, q.Check())
		})
	}
}

// Outside drops a Read and a Write rule on or inside the paths it is given,
// keeps one beside them and one above them, and keeps every Deny.
func TestOutsideDropsWhatIsOnOrInsideItsPaths(t *testing.T) {
	t.Parallel()

	f := FilePolicy{
		Read:  []string{"/k", "/k/bin", "/kx", "/"},
		Write: []string{"/k/w", "/w"},
		Deny:  []string{"/k/d"},
	}

	got := f.Outside("/k")

	assert.Equal(t, FilePolicy{Read: []string{"/kx", "/"}, Write: []string{"/w"}, Deny: []string{"/k/d"}}, got)
}

// A Deny is compiled only where a Files Read or Write reaches it: inside one,
// on one, or holding one. Anywhere else its path is off limits already.
func TestADenyIsCompiledOnlyWhereAReadOrWriteReachesIt(t *testing.T) {
	t.Parallel()

	base := realpath.Resolved(t.TempDir())
	d := mkdirs(t, base, "home/.local/share/keyrings", "home/.kube", "srv/tools", "home/.ssh")
	keyrings, kube, tools, ssh := d[0], d[1], d[2], d[3]
	p := Policy{
		Files:  FilePolicy{Read: []string{filepath.Join(base, "home", ".local"), kube, tools}},
		Always: AlwaysPolicy{Deny: []string{ssh, keyrings, kube, filepath.Join(base, "srv")}},
	}

	var denied []string
	for _, r := range compiledRules(t, p) {
		if r.kind == ruleDeny {
			denied = append(denied, r.at)
		}
	}

	assert.Equal(t, []string{keyrings, kube, filepath.Join(base, "srv")}, denied)
}

// A Kubetail folder inside another is denied once, with the one around it.
func TestAKubetailFolderInsideAnotherIsDeniedOnce(t *testing.T) {
	t.Parallel()

	base := realpath.Resolved(t.TempDir())
	d := mkdirs(t, base, "state/logs", "state", "data")
	logs, state, data := d[0], d[1], d[2]
	p := Policy{Files: FilePolicy{Read: []string{base}}, Always: AlwaysPolicy{Kubetail: []string{logs, state, data}}}

	var denied []string
	for _, r := range compiledRules(t, p) {
		if r.kind == ruleDeny {
			denied = append(denied, r.at)
		}
	}

	assert.Equal(t, []string{state, data}, denied)
}

func TestLimitsAreChecked(t *testing.T) {
	t.Parallel()

	assert.NoError(t, Policy{}.Check(), "no limits")
	assert.NoError(t, Policy{Limits: Limits{CPUSeconds: 1, MemoryBytes: 1, OpenFiles: 1, Processes: 1}}.Check())
	for name, l := range map[string]Limits{
		"a negative CPU time":          {CPUSeconds: -1},
		"a negative memory":            {MemoryBytes: -1, OpenFiles: 1},
		"a negative file count":        {OpenFiles: -1},
		"a negative process count":     {Processes: -1, OpenFiles: 1},
		"memory with no file limit":    {MemoryBytes: 1},
		"processes with no file limit": {Processes: 1},
	} {
		assert.Error(t, Policy{Limits: l}.Check(), name)
	}
}
