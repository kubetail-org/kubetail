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
	"testing"

	"github.com/stretchr/testify/assert"
)

// System reads the system's folders, /etc whole, with its secret files on
// Never.
func TestLinuxListsReadTheSystemAndHideItsSecrets(t *testing.T) {
	for _, p := range []string{"/usr", "/etc", "/opt", "/nix/store", "/home/linuxbrew/.linuxbrew", "/snap", "/nix/var/nix/profiles", "/run/current-system"} {
		assert.Contains(t, platformLists.System, p)
	}
	for _, p := range []string{
		"/etc/ssh", "/etc/sudoers", "/etc/sudoers.d", "/etc/shadow", "/etc/gshadow", "/etc/krb5.conf", "/etc/krb5.keytab",
		"~/.local/share/keyrings", "~/.config/google-chrome", "~/.config/chromium", "~/.mozilla",
	} {
		assert.Contains(t, platformLists.Never, p)
	}
	assert.Equal(t, []string{"/home/linuxbrew/.linuxbrew/var"}, brewVar)
}

func TestLinuxNoWriteHoldsTheUserServices(t *testing.T) {
	got := NoWrite("/home/me")
	for _, p := range []string{
		"/home/me/.config/systemd", "/home/me/.local/share/systemd/user", "/home/me/.config/environment.d", "/home/me/.config/autostart",
	} {
		assert.Contains(t, got, p)
	}
}
