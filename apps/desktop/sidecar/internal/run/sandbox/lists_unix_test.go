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
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/realpath"
)

// listPaths is every path the lists name, each once per list it is on.
func listPaths() [][]string {
	var toolchain []string
	for _, l := range slices.Concat(sharedLists.Toolchain, platformLists.Toolchain) {
		toolchain = append(toolchain, l.Read...)
	}
	return [][]string{
		slices.Concat(sharedLists.System, platformLists.System),
		toolchain,
		slices.Concat(sharedLists.Never, platformLists.Never),
		slices.Concat(sharedLists.Closed, platformLists.Closed),
		slices.Concat(sharedLists.NoWrite, platformLists.NoWrite),
	}
}

func TestNoPathIsInTwoLists(t *testing.T) {
	lists := listPaths()
	seen := map[string]bool{}
	for _, list := range lists {
		for _, p := range list {
			assert.False(t, seen[p], "%s is on two lists, or twice on one", p)
			seen[p] = true
		}
	}
}

func TestEveryListPathIsAbsoluteOrInTheHome(t *testing.T) {
	for _, p := range slices.Concat(listPaths()...) {
		assert.True(t, strings.HasPrefix(p, "/") || strings.HasPrefix(p, "~/"), p)
	}
}

// otherHomesIn makes parent the folder the users' homes sit in for the rest
// of the test.
func otherHomesIn(t *testing.T, parent string) {
	t.Helper()
	keepLists(t)
	homesParent = parent
}

// keepLists restores the lists, homesParent and brewVar when the test ends,
// so the test may change them. A probe's policy build can outlive the test
// that started it, still reading them, so each swap first waits it out.
func keepLists(t *testing.T) {
	t.Helper()
	boundedCalls.Wait()
	shared, platform, parent, brew := sharedLists, platformLists, homesParent, brewVar
	t.Cleanup(func() {
		boundedCalls.Wait()
		sharedLists, platformLists, homesParent, brewVar = shared, platform, parent, brew
	})
}

// Never is the Never paths with ~/ under the home, and with no home the
// absolute ones alone, since a ~/ path would be relative.
func TestNeverWithNoHomeIsTheAbsolutePaths(t *testing.T) {
	otherHomesIn(t, t.TempDir())
	keepLists(t)
	platformLists.Never = append(slices.Clone(platformLists.Never), "/etc/kubetail-secret")
	var s *Sandbox

	for _, p := range s.Never("") {
		assert.True(t, filepath.IsAbs(p), p)
	}
	assert.Contains(t, s.Never(""), "/etc/kubetail-secret")
	assert.Contains(t, s.Never(""), "/root")
	assert.Contains(t, s.Never("/home/ana"), "/home/ana/.kube")
	assert.Contains(t, s.Never("/home/ana"), "/etc/kubetail-secret")
}

// Every other folder where the users' homes sit is Never, read at call time,
// but for the user's own and the one that is no user's. System reads none.
func TestTheOtherHomesAreNever(t *testing.T) {
	parent := realpath.Resolved(t.TempDir())
	d := mkdirs(t, parent, "a", "b", notHomes[0], "ana")
	otherHomesIn(t, parent)
	s := &Sandbox{self: "/usr/bin/true"}

	never := s.Never(d[3])

	assert.Contains(t, never, d[0])
	assert.Contains(t, never, d[1])
	assert.NotContains(t, never, d[2])
	assert.NotContains(t, never, d[3])
	read := s.System(d[3], "/bin/sh").Files.Read
	assert.NotContains(t, read, d[0])
	assert.NotContains(t, read, d[1])
}

// A home nested in a folder of the homes parent keeps its own reads: that
// folder holds it, so it is not another user's home.
func TestANestedHomeIsNotAnotherUsersHome(t *testing.T) {
	parent := realpath.Resolved(t.TempDir())
	d := mkdirs(t, parent, "company/alice/.nvm", "bo")
	home := filepath.Dir(d[0])
	otherHomesIn(t, parent)
	withLists(t, Lists{Toolchain: []Location{{Name: "nvm", Read: []string{"~/.nvm"}}}})
	s := &Sandbox{self: "/usr/bin/true"}

	read := s.System(home, filepath.Join(home, "bin", "bash")).Files.Read

	assert.Contains(t, read, d[0])
	assert.Contains(t, read, filepath.Join(home, "bin"))
	assert.Equal(t, []string{d[1]}, otherHomes(home))
	assert.NotContains(t, s.Never(home), filepath.Join(parent, "company"))
}

// A Never path on or above the home is dropped, so a home of /root or one
// inside a denied folder does not deny itself. The home need not exist.
func TestANeverPathAboveTheHomeIsDropped(t *testing.T) {
	otherHomesIn(t, t.TempDir())
	base := realpath.Resolved(t.TempDir())
	keepLists(t)
	platformLists.Never = append(slices.Clone(platformLists.Never), filepath.Join(base, "never"))
	var s *Sandbox

	assert.NotContains(t, s.Never("/root"), "/root")
	assert.Contains(t, s.Never("/root"), "/root/.ssh")
	assert.NotContains(t, s.Never(filepath.Join(base, "never", "ana")), filepath.Join(base, "never"))
}

// addRoot makes dir one of the system roots for the rest of the test.
func addRoot(t *testing.T, dir string) {
	t.Helper()
	keepLists(t)
	platformLists.System = append(slices.Clone(platformLists.System), dir)
}

// addToolchain makes read a Toolchain location for the rest of the test.
func addToolchain(t *testing.T, read ...string) {
	t.Helper()
	keepLists(t)
	sharedLists.Toolchain = append(slices.Clone(sharedLists.Toolchain), Location{Name: "test", Read: read})
}

// withLists makes l the shared lists, with no platform lists and no
// Homebrew var, for the rest of the test.
func withLists(t *testing.T, l Lists) {
	t.Helper()
	keepLists(t)
	sharedLists, platformLists, brewVar = l, Lists{}, nil
}

// System reads the System folders, each Toolchain folder that exists, the
// shell's folder and this executable at its resolved path, and denies
// Homebrew's var and the Closed folders. Its Env is each found location's,
// and Asdf says whether asdf's was found. A missing location adds nothing.
func TestSystemReadsTheZones(t *testing.T) {
	base := realpath.Resolved(t.TempDir())
	home := filepath.Join(base, "home")
	d := mkdirs(t, home, "bins", ".asdf", "shells", "late")
	self := filepath.Join(mkdirs(t, base, "app")[0], "kubetail-sidecar")
	require.NoError(t, os.WriteFile(self, nil, 0o700))
	require.NoError(t, os.Symlink(self, filepath.Join(base, "self-link")))
	withLists(t, Lists{
		System: []string{filepath.Join(base, "usr")},
		Toolchain: []Location{
			{Name: "bins", Read: []string{"~/bins", "~/missing"}},
			{Name: "asdf", Read: []string{"~/.asdf"}, Env: map[string]string{"ASDF_DATA_DIR": "~/.asdf", "ASDF_HOME_TEST": "~/.asdf"}},
			{Name: "gone", Read: []string{"~/gone", "~/late"}, Env: map[string]string{"GONE": "~/gone"}},
		},
		Closed: []string{"~/Documents"},
	})
	brewVar = []string{filepath.Join(base, "usr", "var")}
	s := &Sandbox{self: filepath.Join(base, "self-link")}

	got := s.System(home, filepath.Join(d[2], "zsh"))

	assert.Equal(t, System{
		Files: FilePolicy{
			Read: []string{filepath.Join(base, "usr"), d[0], d[1], d[3], d[2], self},
			Deny: []string{filepath.Join(base, "usr", "var"), filepath.Join(home, "Documents")},
		},
		Env:  []string{"ASDF_DATA_DIR=" + d[1], "ASDF_HOME_TEST=" + d[1]},
		Asdf: true,
	}, got)

	withLists(t, Lists{Toolchain: []Location{{Name: "asdf", Read: []string{"~/none"}, Env: map[string]string{"ASDF_DIR": "~/none"}}}})
	got = s.System(home, "/bin/sh")
	assert.Empty(t, got.Env)
	assert.False(t, got.Asdf)
}

// A toolchain folder that is, or links to, the home, a folder above it, or a
// folder of every app's data under it, is not read: it would read all of it.
// A link to another folder under the home is read.
func TestAToolchainLinkToABroadFolderIsNotRead(t *testing.T) {
	base := realpath.Resolved(t.TempDir())
	home := filepath.Join(base, "home")
	d := mkdirs(t, home, ".config/x", ".local/share/x", "Library", "dotfiles/bin")
	links := map[string]string{
		"bin": home, ".nvm": base, ".krew": filepath.Join(home, ".config"), ".pyenv": filepath.Join(home, ".local"),
		".rbenv": filepath.Join(home, ".local", "share"), ".volta": filepath.Join(home, "Library"), "go": d[3],
	}
	// The first folder decides whether Env is set, so it is one not read.
	read := []string{"~/bin"}
	for name, target := range links {
		require.NoError(t, os.Symlink(target, filepath.Join(home, name)))
		if name != "bin" {
			read = append(read, "~/"+name)
		}
	}
	withLists(t, Lists{Toolchain: []Location{{Name: "links", Read: read, Env: map[string]string{"X": "~/bin"}}}})
	s := &Sandbox{self: "/usr/bin/true"}

	got := s.System(home, "/bin/sh")

	for name, target := range links {
		if target == d[3] {
			assert.Contains(t, got.Files.Read, filepath.Join(home, name))
			continue
		}
		assert.NotContains(t, got.Files.Read, filepath.Join(home, name), name)
	}
	assert.Empty(t, got.Env, "a location whose first folder is not read sets no variable")
}

// With no home, System holds no Toolchain or Closed rule and no Env, and a
// probe's policy over it passes Check.
func TestWithNoHomeSystemHoldsNoHomeRule(t *testing.T) {
	s := &Sandbox{self: "/usr/bin/true"}

	got := s.System("", "/bin/sh")

	assert.Equal(t, append(systemFolders(), realpath.Resolved("/usr/bin/true")), got.Files.Read)
	assert.Equal(t, brewVar, got.Files.Deny)
	assert.Empty(t, got.Env)
	assert.False(t, got.Asdf)
	assert.NoError(t, s.probePolicy("/bin/sh", realpath.Resolved(t.TempDir()), "").Check())
}

// A shell installed under a prefix outside the home reads the prefix, so it
// reaches its own share. Under the home, or where the prefix holds the home
// or is /, its folder is read alone. One in another user's home adds nothing.
func TestTheShellsPrefixIsReadButNeverTheHome(t *testing.T) {
	base := realpath.Resolved(t.TempDir())
	homes := mkdirs(t, base, "users")[0]
	home := mkdirs(t, homes, "ana")[0]
	other := mkdirs(t, homes, "bo/bin")[0]
	otherHomesIn(t, homes)
	withLists(t, Lists{})
	s := &Sandbox{self: "/usr/bin/true"}
	read := func(shell string) []string {
		return slices.DeleteFunc(s.System(home, shell).Files.Read, func(p string) bool { return p == realpath.Resolved(s.self) })
	}

	assert.Equal(t, []string{filepath.Join(base, "opt", "zsh")}, read(filepath.Join(base, "opt", "zsh", "bin", "zsh")))
	for _, bin := range []string{".local/bin", "opt/zsh/bin", "bin"} {
		assert.Equal(t, []string{filepath.Join(home, bin)}, read(filepath.Join(home, bin, "zsh")), bin)
	}
	assert.Equal(t, []string{filepath.Join(homes, "bin")}, read(filepath.Join(homes, "bin", "zsh")))
	assert.Equal(t, []string{"/bin"}, read("/bin/zsh"))
	assert.Equal(t, []string{"/bin"}, read("/bin/bash"))
	assert.Empty(t, read(filepath.Join(other, "zsh")))
}

// A shell whose folder is broad — the home, a folder of every app's data
// under it, or / — reads the program alone, never the folder.
func TestAShellInABroadFolderReadsOnlyItself(t *testing.T) {
	base := realpath.Resolved(t.TempDir())
	home := mkdirs(t, base, "home/.config")[0]
	home = filepath.Dir(home)
	otherHomesIn(t, t.TempDir())
	withLists(t, Lists{})
	s := &Sandbox{self: "/usr/bin/true"}

	for _, shell := range []string{filepath.Join(home, "bash"), filepath.Join(home, ".config", "bash"), "/sh"} {
		read := s.System(home, shell).Files.Read
		assert.Contains(t, read, shell)
		assert.NotContains(t, read, filepath.Dir(shell))
	}
}

// A shell that is a link reads where each link on its way leads, so the
// program it names runs, and nothing wider than shellFolder would read.
func TestAShellLinkReadsWhereItLeads(t *testing.T) {
	base := realpath.Resolved(t.TempDir())
	home := filepath.Join(base, "home")
	d := mkdirs(t, home, ".local/bin", "links", "shell-install/bin")
	require.NoError(t, os.WriteFile(filepath.Join(d[2], "bash"), nil, 0o700))
	require.NoError(t, os.Symlink("../shell-install/bin/bash", filepath.Join(d[1], "bash")))
	require.NoError(t, os.Symlink(filepath.Join(d[1], "bash"), filepath.Join(d[0], "bash")))
	otherHomesIn(t, t.TempDir())
	withLists(t, Lists{})
	s := &Sandbox{self: "/usr/bin/true"}

	read := s.System(home, filepath.Join(d[0], "bash")).Files.Read

	assert.Equal(t, []string{d[0], d[1], d[2], realpath.Resolved(s.self)}, read)
}

// A Toolchain folder inside a Never path makes no rule, so a policy of
// System and Never passes Check.
func TestSystemLeavesOutTheNeverPaths(t *testing.T) {
	base := realpath.Resolved(t.TempDir())
	home := filepath.Join(base, "home")
	d := mkdirs(t, home, ".docker/bin", "apps")
	withLists(t, Lists{
		Toolchain: []Location{{Name: "docker", Read: []string{"~/.docker/bin"}}, {Name: "apps", Read: []string{"~/apps"}}},
		Never:     []string{"~/.docker"},
	})
	s := &Sandbox{self: "/usr/bin/true"}

	got := s.System(home, "/bin/sh").Files

	assert.NotContains(t, got.Read, d[0])
	assert.Contains(t, got.Read, d[1])
	assert.NoError(t, Policy{Files: got, Always: AlwaysPolicy{Deny: s.Never(home)}}.Check())
}

func TestNoWriteIsUnderTheHome(t *testing.T) {
	got := NoWrite("/home/me")
	for _, p := range []string{
		"/home/me/.zshrc", "/home/me/.bashrc", "/home/me/.profile", "/home/me/.zshenv", "/home/me/.oh-my-zsh",
		"/home/me/.config/fish", "/home/me/.config/direnv",
	} {
		assert.Contains(t, got, p)
	}
	for _, p := range got {
		assert.True(t, strings.HasPrefix(p, "/home/me/"), p)
	}
	assert.Empty(t, NoWrite(""), "with no home there is nothing under it")
}
