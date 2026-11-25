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

package layout

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/realpath"
)

// A linked layout folder is named by its target, and the link is recorded.
func TestResolveFollowsALinkedLayoutFolder(t *testing.T) {
	root := canonical(t, t.TempDir())
	target := filepath.Join(root, "big", "kubetail")
	require.NoError(t, os.MkdirAll(target, 0o700))
	cache := filepath.Join(root, "cache")
	require.NoError(t, os.Mkdir(cache, 0o700))
	require.NoError(t, os.Symlink(target, filepath.Join(cache, "kubetail")))
	l := Of(Linux, false, Roots{Config: root, LocalData: root, Cache: cache, State: root, Home: root})

	got, links, err := Resolve(l)

	require.NoError(t, err)
	assert.Equal(t, target, got.Cache)
	assert.Equal(t, l.Data, got.Data)
	assert.Equal(t, []realpath.Link{{Name: l.Cache, Parent: cache}}, links)
}

// The runtime folder's parent can be shared between users, so a link at the
// folder itself is refused.
func TestResolveRuntimeRefusesALink(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "kubetail-501")
	require.NoError(t, os.Symlink(t.TempDir(), dir))

	_, _, err := ResolveRuntime(dir)

	require.Error(t, err)
	assert.Contains(t, err.Error(), dir)
}

// A link above the runtime folder is followed, as macOS's /var is.
func TestResolveRuntimeFollowsALinkAboveIt(t *testing.T) {
	root := canonical(t, t.TempDir())
	target := filepath.Join(root, "private", "var")
	require.NoError(t, os.MkdirAll(target, 0o700))
	parent := filepath.Join(root, "var")
	require.NoError(t, os.Symlink(target, parent))

	got, links, err := ResolveRuntime(filepath.Join(parent, "kubetail-501"))

	require.NoError(t, err)
	assert.Equal(t, filepath.Join(target, "kubetail-501"), got)
	assert.Equal(t, []realpath.Link{{Name: parent, Parent: root}}, links)
}

// A runtime folder that cannot be looked up, here one under a file, is an
// error.
func TestResolveRuntimeFailsOnAFolderItCannotLookUp(t *testing.T) {
	file := filepath.Join(t.TempDir(), "file")
	require.NoError(t, os.WriteFile(file, nil, 0o600))

	_, _, err := ResolveRuntime(filepath.Join(file, "kubetail-501"))

	require.Error(t, err)
}

// A link to nothing above the runtime folder fails it, naming the link.
func TestResolveRuntimeFailsUnderALinkToNothing(t *testing.T) {
	root := t.TempDir()
	parent := filepath.Join(root, "var")
	require.NoError(t, os.Symlink(filepath.Join(root, "gone"), parent))

	_, _, err := ResolveRuntime(filepath.Join(parent, "kubetail-501"))

	require.ErrorContains(t, err, parent)
}
