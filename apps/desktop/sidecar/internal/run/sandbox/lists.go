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

// Lists is what the sandbox reads and hides on a platform, before a run adds
// its own paths. A path starting with ~/ is under the user's home; with no
// home, Toolchain, Closed and the ~/ paths of Never add nothing.
type Lists struct {
	System    []string   // readable by default
	Toolchain []Location // readable under the home by default, when present
	Never     []string   // denied always
	Closed    []string   // denied, but a grant inside one opens what it names
	NoWrite   []string   // run by something outside the sandbox: a Read rule may name one, a Write rule may not
}

// Location is a folder the user's tools live in. Env is set only when the
// first Read folder exists.
type Location struct {
	Name string            // shown in Settings, like "asdf"
	Read []string          // folders every run can read
	Env  map[string]string // variables that point the tool at them, since HOME is the workspace
}

// System is what every sandboxed run starts from.
type System struct {
	Files FilePolicy // what every run reads, and the folders it may not
	Env   []string   // each found location's Env, as NAME=value with ~ expanded
	Asdf  bool       // whether the asdf location was found
}

// sharedLists is what every platform's sandbox reads and hides. A credential
// a tool keeps inside its location's tree is on Never, since the location
// makes that tree readable.
var sharedLists = Lists{
	Toolchain: []Location{
		{Name: "user binaries", Read: []string{"~/.local/bin", "~/bin", "~/go/bin", "~/.cargo/bin"}},
		// ~/.asdf is asdf's data; its scripts may live under a package prefix,
		// which asdf finds itself, so ASDF_DIR is left unset.
		{Name: "asdf", Read: []string{"~/.asdf"}, Env: map[string]string{"ASDF_DATA_DIR": "~/.asdf"}},
		{Name: "mise", Read: []string{"~/.local/share/mise", "~/.config/mise"}, Env: map[string]string{
			"MISE_DATA_DIR": "~/.local/share/mise", "MISE_CONFIG_DIR": "~/.config/mise",
		}},
		// ~/.cargo/bin's cargo and rustc are rustup's proxies, which run the
		// toolchains under ~/.rustup. CARGO_HOME is the tool home's, since cargo
		// writes its registry there.
		{Name: "rustup", Read: []string{"~/.rustup"}, Env: map[string]string{"RUSTUP_HOME": "~/.rustup"}},
		{Name: "nvm", Read: []string{"~/.nvm"}, Env: map[string]string{"NVM_DIR": "~/.nvm"}},
		{Name: "pyenv", Read: []string{"~/.pyenv"}, Env: map[string]string{"PYENV_ROOT": "~/.pyenv"}},
		{Name: "rbenv", Read: []string{"~/.rbenv"}, Env: map[string]string{"RBENV_ROOT": "~/.rbenv"}},
		{Name: "volta", Read: []string{"~/.volta"}, Env: map[string]string{"VOLTA_HOME": "~/.volta"}},
		{Name: "SDKMAN", Read: []string{"~/.sdkman/candidates"}},
		{Name: "tfenv", Read: []string{"~/.tfenv"}},
		{Name: "aqua", Read: []string{"~/.local/share/aquaproj-aqua", "~/.config/aquaproj-aqua"}, Env: map[string]string{
			"AQUA_ROOT_DIR": "~/.local/share/aquaproj-aqua", "AQUA_GLOBAL_CONFIG": "~/.config/aquaproj-aqua/aqua.yaml",
		}},
		{Name: "bun", Read: []string{"~/.bun/bin"}},
		{Name: "deno", Read: []string{"~/.deno/bin"}},
		{Name: "pipx and uv", Read: []string{"~/.local/share/pipx", "~/.local/share/uv"}},
		{Name: "krew", Read: []string{"~/.krew"}, Env: map[string]string{"KREW_ROOT": "~/.krew"}},
		{Name: "helm plugins", Read: []string{"~/.local/share/helm/plugins"}, Env: map[string]string{"HELM_PLUGINS": "~/.local/share/helm/plugins"}},
		{Name: "Google Cloud SDK", Read: []string{"~/google-cloud-sdk"}},
		{Name: "Nix", Read: []string{"~/.nix-profile"}},
		// The folders above their bin hold the Docker socket.
		{Name: "Rancher Desktop", Read: []string{"~/.rd/bin"}},
		{Name: "OrbStack", Read: []string{"~/.orbstack/bin"}},
		{Name: "Linkerd", Read: []string{"~/.linkerd2/bin"}},
		{Name: "istioctl", Read: []string{"~/.istioctl/bin"}},
	},
	Never: []string{
		"~/.kube", "~/.aws", "~/.azure", "~/.config/gcloud", "~/.ssh", "~/.gnupg", "~/.config/gh", "~/.docker",
		"~/.netrc", "~/.git-credentials",
		"~/.cargo/credentials", "~/.cargo/credentials.toml", "~/.pulumi/credentials.json", "~/.fly/config.yml",
		"~/.helm", "~/.terraform.d", "~/.npmrc", "~/.pypirc", "~/.gem/credentials", "~/.config/git/credentials",
		"~/.local/share/uv/credentials",
		// A glob compiles to no rule, so each history is named.
		"~/.bash_history", "~/.zsh_history", "~/.python_history", "~/.node_repl_history", "~/.psql_history",
		"~/.mysql_history", "~/.lesshst", "~/.local/share/fish/fish_history",
		// The container sockets. Docker Desktop's ~/.docker/run is inside ~/.docker.
		"/run/containerd", "/var/run/docker.sock", "/run/docker.sock", "/run/podman",
		"~/.rd/docker.sock", "~/.orbstack/run", "~/.colima", "~/.lima",
	},
	Closed: []string{"~/Documents", "~/Desktop", "~/Downloads"},
	// The shells' startup folders and files, which the login shell runs and
	// whose snapshot shapes every command outside the sandbox.
	NoWrite: []string{
		"~/.oh-my-zsh", "~/.zsh", "~/.config/zsh", "~/.config/fish", "~/.config/nushell", "~/.bashrc.d", "~/.config/direnv",
		"~/.zshenv", "~/.zprofile", "~/.zshrc", "~/.zlogin", "~/.bash_profile", "~/.bash_login", "~/.profile", "~/.bashrc",
	},
}

// appDataDirs are the folders under the home that hold every app's data. A
// Toolchain or shell folder that resolves to one of them, to the home, or
// above either, is not read, since it would read all of it.
var appDataDirs = []string{"~/.config", "~/.local", "~/.local/share", "~/Library"}
