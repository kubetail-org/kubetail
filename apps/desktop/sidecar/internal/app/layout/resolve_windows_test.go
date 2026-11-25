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
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A junctioned layout folder is named by its target.
func TestResolveFollowsAJunctionedLayoutFolder(t *testing.T) {
	root := canonical(t, t.TempDir())
	target := filepath.Join(root, "big", "Kubetail")
	require.NoError(t, os.MkdirAll(target, 0o700))
	local := filepath.Join(root, "local")
	require.NoError(t, os.Mkdir(local, 0o700))
	out, err := exec.Command("cmd", "/c", "mklink", "/J", filepath.Join(local, "Kubetail"), target).CombinedOutput()
	require.NoError(t, err, string(out))
	l := Of(Windows, false, Roots{Config: local, LocalData: local, Cache: local, Home: root})

	got, _, err := Resolve(l)

	require.NoError(t, err)
	assert.Equal(t, target, got.App)
	assert.Equal(t, filepath.Join(target, "data"), got.Data)
}
