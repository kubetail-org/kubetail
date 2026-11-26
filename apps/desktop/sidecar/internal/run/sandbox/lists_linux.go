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
	"os"
	"path/filepath"
	"strconv"
)

// platformLists is what only Linux's sandbox reads and hides. /etc is read
// whole, since the loader and libc read files no short list names, and its
// secret files are on Never.
var platformLists = Lists{
	System: []string{
		"/usr", "/bin", "/sbin", "/lib", "/lib32", "/lib64", "/libx32", "/etc", "/opt",
		"/nix/store", "/nix/var/nix/profiles", "/run/current-system", "/snap", "/home/linuxbrew/.linuxbrew",
	},
	Never: []string{
		"/etc/ssh", "/etc/sudoers", "/etc/sudoers.d", "/etc/shadow", "/etc/gshadow", "/etc/krb5.conf", "/etc/krb5.keytab",
		"~/.local/share/keyrings", "~/.config/google-chrome", "~/.config/chromium", "~/.mozilla",
	},
	// The user's services, their environment, and the desktop's autostart.
	NoWrite: []string{"~/.config/systemd", "~/.local/share/systemd/user", "~/.config/environment.d", "~/.config/autostart"},
}

// brewVar is Linuxbrew's var, which the System folders take in and which
// holds its services' databases and logs.
var brewVar = []string{"/home/linuxbrew/.linuxbrew/var"}

// homesParent is the folder the users' homes sit in, and notHomes its entries
// that are no user's.
var (
	homesParent = "/home"
	notHomes    = []string{"linuxbrew"}
)

// runtimeNever is the rootless container sockets in the user's runtime
// folder. Kubetail's own runtime folder sits beside them.
func runtimeNever() []string {
	dir := filepath.Join("/run/user", strconv.Itoa(os.Getuid()))
	return []string{filepath.Join(dir, "docker.sock"), filepath.Join(dir, "podman")}
}
