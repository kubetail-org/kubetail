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

package tools

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/rootdir"
)

// The workspace is owner-only whatever the umask, as the results are.
func TestTheWorkspaceIsOwnerOnly(t *testing.T) {
	old := syscall.Umask(0)
	defer syscall.Umask(old)
	results := testChatDir(filepath.Join(t.TempDir(), "c1"))

	root, err := OpenWorkspace(results, true)
	require.NoError(t, err)
	root.Close()

	info, err := os.Stat(WorkspacePath(results))
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o700), info.Mode().Perm())
}

// A link at work is refused whatever it points at, inside the chat's
// directory or out, so nothing a command links in is written through.
func TestTheWorkspaceRefusesALink(t *testing.T) {
	for name, target := range map[string]string{"outside": t.TempDir(), "inside": "other"} {
		t.Run(name, func(t *testing.T) {
			results := testChatDir(t.TempDir())
			require.NoError(t, os.Mkdir(filepath.Join(string(results), "other"), 0o700))
			require.NoError(t, os.Symlink(target, WorkspacePath(results)))

			for _, create := range []bool{false, true} {
				_, err := OpenWorkspace(results, create)
				assert.ErrorIs(t, err, rootdir.ErrNotADirectory, "create %v", create)
			}
		})
	}
}
