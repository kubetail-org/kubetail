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

package realpath

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAJunctionIsALink(t *testing.T) {
	name := filepath.Join(t.TempDir(), "link")
	link(t, t.TempDir(), name)
	info, err := os.Lstat(name)
	require.NoError(t, err)
	assert.True(t, IsLink(info))
}

// A directory symlink is a link too.
func TestADirectorySymlinkIsALink(t *testing.T) {
	link := filepath.Join(t.TempDir(), "link")
	out, err := exec.Command("cmd", "/c", "mklink", "/D", link, t.TempDir()).CombinedOutput()
	require.NoError(t, err, string(out))
	info, err := os.Lstat(link)
	require.NoError(t, err)
	assert.True(t, IsLink(info))
}
