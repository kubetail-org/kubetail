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
	"path/filepath"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/realpath"
)

// The probe's policy is System over its shell, its folder writable, and the
// Never paths denied, with no Kubetail paths and no relay. It passes Check with
// a toolchain folder inside ~/.docker, and with no home at all.
func TestTheProbeKeepsTheCredentialsHidden(t *testing.T) {
	// It changes the package's lists, so this test stays serial.
	base := realpath.Resolved(t.TempDir())
	home := filepath.Join(base, "home")
	mkdirs(t, home, ".docker/bin")
	dir := mkdirs(t, base, "probe")[0]
	keepLists(t)
	sharedLists.Toolchain = append(slices.Clone(sharedLists.Toolchain), Location{Name: "docker", Read: []string{"~/.docker/bin"}})
	s := &Sandbox{self: "/usr/bin/true"}

	p := s.probePolicy("/bin/sh", dir, home)

	require.NoError(t, p.Check())
	assert.Equal(t, s.System(home, "/bin/sh").Files.Read, p.Files.Read)
	assert.Equal(t, []string{dir}, p.Files.Write)
	assert.Equal(t, AlwaysPolicy{Deny: s.Never(home)}, p.Always)
	assert.Empty(t, p.Network.Relays)

	p = s.probePolicy("/bin/sh", dir, "")

	require.NoError(t, p.Check())
	assert.Equal(t, s.Never(""), p.Always.Deny)
	for _, d := range p.Always.Deny {
		assert.True(t, filepath.IsAbs(d), d)
	}
}
