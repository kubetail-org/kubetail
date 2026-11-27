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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// System reads the system's folders, /private/etc whole, with its secret
// files on Never under the path System reads them by.
func TestDarwinListsReadTheSystemAndHideItsSecrets(t *testing.T) {
	t.Parallel()

	for _, p := range []string{"/usr", "/private/etc", "/opt", "/Applications", "/nix/store", "/nix/var/nix/profiles", "/run/current-system"} {
		assert.Contains(t, platformLists.System, p)
	}
	for _, p := range []string{
		"/private/etc/ssh", "/private/etc/sudoers", "/private/etc/sudoers.d", "/private/etc/master.passwd",
		"/private/etc/krb5.conf", "/private/etc/krb5.keytab",
		"~/Library/Keychains", "~/Library/Cookies", "~/Library/Application Support/Google/Chrome",
		"~/Library/Application Support/Firefox", "~/Library/Safari",
	} {
		assert.Contains(t, platformLists.Never, p)
	}
	assert.Equal(t, []string{"/opt/homebrew/var", "/usr/local/var"}, brewVar)
}

func TestDarwinNoWriteHoldsLaunchAgents(t *testing.T) {
	t.Parallel()

	assert.Contains(t, NoWrite("/Users/me"), "/Users/me/Library/LaunchAgents")
}

// A HOME in another case than the disk's is still the user's own home: it is
// not denied as another user's, so the probe's policy passes its check.
func TestAHomeInAnotherCaseIsNotAnotherUsersHome(t *testing.T) {
	// otherHomesIn changes the package's lists, so this test stays serial.
	parent := canonicalOf(t, t.TempDir())[0]
	d := mkdirs(t, parent, "Me", "other")
	home := filepath.Join(parent, "me")
	skipIfCaseSensitive(t, home)
	otherHomesIn(t, parent)
	s := &Sandbox{self: "/usr/bin/true"}

	assert.Equal(t, []string{d[1]}, otherHomes(home))
	assert.NotContains(t, s.Never(home), d[0])
	require.NoError(t, s.probePolicy("/bin/sh", canonicalOf(t, t.TempDir())[0], home).Check())
}
