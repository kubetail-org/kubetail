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

// platformLists is what only macOS's sandbox reads and hides. /Applications
// holds app bundles, which are programs: Docker Desktop's kubectl is a link
// into one, and so is Xcode's developer directory. /etc's secret files are
// named under /private/etc alone, since rules compile resolved.
var platformLists = Lists{
	System: []string{
		"/usr", "/bin", "/sbin", "/System", "/Library", "/Applications", "/private/etc", "/opt",
		"/nix/store", "/nix/var/nix/profiles", "/run/current-system",
	},
	Never: []string{
		"/private/etc/ssh", "/private/etc/sudoers", "/private/etc/sudoers.d", "/private/etc/master.passwd",
		"/private/etc/krb5.conf", "/private/etc/krb5.keytab",
		"~/Library/Keychains", "~/Library/Cookies", "~/Library/Application Support/Google/Chrome",
		"~/Library/Application Support/Firefox", "~/Library/Safari",
	},
	NoWrite: []string{"~/Library/LaunchAgents"},
}

// brewVar is Homebrew's var, which the System folders take in and which holds
// its services' databases and logs.
var brewVar = []string{"/opt/homebrew/var", "/usr/local/var"}

// homesParent is the folder the users' homes sit in, and notHomes its entries
// that are no user's.
var (
	homesParent = "/Users"
	notHomes    = []string{"Shared"}
)

// runtimeNever is nothing on macOS, whose container sockets are under the home.
func runtimeNever() []string { return nil }
