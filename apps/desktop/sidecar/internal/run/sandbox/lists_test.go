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
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/realpath"
)

// Each credential a tool keeps inside a Toolchain location's tree is on Never,
// since the location makes the tree readable. The credentials outside every
// location stay on the list as credentials.
func TestTheCredentialsBesideAToolsProgramsAreListed(t *testing.T) {
	t.Parallel()

	assert.Contains(t, sharedLists.Never, "~/.local/share/uv/credentials")
	assert.True(t, slices.ContainsFunc(sharedLists.Toolchain, func(l Location) bool {
		return slices.ContainsFunc(l.Read, func(r string) bool { return realpath.Within("~/.local/share/uv/credentials", r) })
	}))
	for _, c := range []string{"~/.cargo/credentials", "~/.cargo/credentials.toml", "~/.pulumi/credentials.json", "~/.fly/config.yml"} {
		assert.Contains(t, sharedLists.Never, c)
	}
}

// Never holds the note's list: the tools' credentials, the shells' and
// REPLs' histories, and the container sockets.
func TestNeverHoldsTheNotesList(t *testing.T) {
	t.Parallel()

	for _, p := range []string{
		"~/.helm", "~/.terraform.d", "~/.npmrc", "~/.pypirc", "~/.gem/credentials", "~/.config/git/credentials",
		"~/.bash_history", "~/.zsh_history", "~/.python_history", "~/.node_repl_history", "~/.psql_history",
		"~/.mysql_history", "~/.lesshst", "~/.local/share/fish/fish_history",
		"/run/containerd", "/var/run/docker.sock", "/run/docker.sock", "/run/podman",
		"~/.rd/docker.sock", "~/.orbstack/run", "~/.colima", "~/.lima",
	} {
		assert.Contains(t, sharedLists.Never, p)
	}
}

// The folders that hold the user's files stay shut under a grant above them.
func TestTheUsersFoldersAreClosed(t *testing.T) {
	t.Parallel()

	assert.Equal(t, []string{"~/Documents", "~/Desktop", "~/Downloads"}, sharedLists.Closed)
}

// Every location reads under the home, and only the asdf location sets
// asdf's own variables.
func TestTheToolchainIsUnderTheHome(t *testing.T) {
	t.Parallel()

	names := map[string]bool{}
	for _, l := range sharedLists.Toolchain {
		assert.False(t, names[l.Name], "%s is listed twice", l.Name)
		names[l.Name] = true
		assert.NotEmpty(t, l.Read, l.Name)
		for _, r := range l.Read {
			assert.Regexp(t, `^~/`, r, l.Name)
		}
		for _, v := range l.Env {
			assert.Regexp(t, `^~/`, v, l.Name)
		}
	}
	for name, env := range map[string][]string{
		"asdf": {"ASDF_DATA_DIR"}, "mise": {"MISE_DATA_DIR", "MISE_CONFIG_DIR"}, "nvm": {"NVM_DIR"},
		"pyenv": {"PYENV_ROOT"}, "rbenv": {"RBENV_ROOT"}, "volta": {"VOLTA_HOME"}, "rustup": {"RUSTUP_HOME"},
		"aqua": {"AQUA_ROOT_DIR", "AQUA_GLOBAL_CONFIG"}, "krew": {"KREW_ROOT"}, "helm plugins": {"HELM_PLUGINS"},
	} {
		i := slices.IndexFunc(sharedLists.Toolchain, func(l Location) bool { return l.Name == name })
		if assert.NotEqual(t, -1, i, name) {
			for _, k := range env {
				assert.Contains(t, sharedLists.Toolchain[i].Env, k, name)
			}
		}
	}
	// ~/.asdf is asdf's data. Its implementation may live under a package
	// prefix, so ASDF_DIR would point it at the wrong scripts.
	i := slices.IndexFunc(sharedLists.Toolchain, func(l Location) bool { return l.Name == "asdf" })
	assert.NotContains(t, sharedLists.Toolchain[i].Env, "ASDF_DIR")
	// cargo writes its registry under CARGO_HOME, which the tool home sets.
	i = slices.IndexFunc(sharedLists.Toolchain, func(l Location) bool { return l.Name == "rustup" })
	assert.NotContains(t, sharedLists.Toolchain[i].Env, "CARGO_HOME")
	// Their folders above the bin hold the Docker socket.
	for _, name := range []string{"Rancher Desktop", "OrbStack"} {
		i := slices.IndexFunc(sharedLists.Toolchain, func(l Location) bool { return l.Name == name })
		if assert.NotEqual(t, -1, i, name) {
			assert.Regexp(t, `/bin$`, sharedLists.Toolchain[i].Read[0])
		}
	}
}
