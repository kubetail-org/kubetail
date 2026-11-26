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

package layout

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/realpath"
)

// canonical is dir as the kernel names it.
func canonical(t *testing.T, dir string) string {
	t.Helper()
	c, err := realpath.Canonical([]string{dir})
	require.NoError(t, err)
	return c[0]
}

func TestResolveKeepsALayoutWithNoLink(t *testing.T) {
	root := canonical(t, t.TempDir())
	l := Of(Linux, false, Roots{Config: root, LocalData: root, Cache: filepath.Join(root, "cache"), State: root, Home: root})

	got, links, err := Resolve(l)

	require.NoError(t, err)
	assert.Equal(t, l, got)
	assert.Empty(t, links)
}

func TestResolveRuntimeTakesAMissingFolderUnderItsResolvedParent(t *testing.T) {
	dir := filepath.Join(canonical(t, t.TempDir()), "kubetail-501")

	got, links, err := ResolveRuntime(dir)

	require.NoError(t, err)
	assert.Equal(t, dir, got)
	assert.Empty(t, links)
}

func TestResolveRuntimeRefusesAFile(t *testing.T) {
	file := filepath.Join(t.TempDir(), "kubetail-501")
	require.NoError(t, os.WriteFile(file, nil, 0o600))

	_, _, err := ResolveRuntime(file)

	require.Error(t, err)
	assert.Contains(t, err.Error(), file)
}
