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

package tools

import (
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/rootdir"
)

// The workspace is under the chat's directory, named without touching disk.
func TestWorkspacePathIsUnderTheChatsDirectory(t *testing.T) {
	dir := testChatDir(filepath.Join(t.TempDir(), "c1"))

	assert.Equal(t, filepath.Join(string(dir), "workspace"), WorkspacePath(dir))
	assert.NoDirExists(t, string(dir))
}

// The tool home sits beside the workspace, named without touching disk.
func TestToolHomePathIsBesideTheWorkspace(t *testing.T) {
	dir := testChatDir(filepath.Join(t.TempDir(), "c1"))

	assert.Equal(t, filepath.Join(string(dir), "toolhome"), ToolHomePath(dir))
	assert.NoDirExists(t, string(dir))
}

// Opening with create makes the workspace, and the chat's directory above it.
func TestOpenWorkspaceMakesIt(t *testing.T) {
	results := testChatDir(filepath.Join(t.TempDir(), "c1"))

	root, err := OpenWorkspace(results, true)
	require.NoError(t, err)
	defer root.Close()

	assert.DirExists(t, WorkspacePath(results))
	require.NoError(t, root.WriteFile("x", []byte("x"), 0o600))
	assert.FileExists(t, filepath.Join(WorkspacePath(results), "x"))
}

// Without create, a missing workspace is not made.
func TestOpenWorkspaceWithoutCreateFindsNone(t *testing.T) {
	results := testChatDir(t.TempDir())

	_, err := OpenWorkspace(results, false)
	assert.ErrorIs(t, err, fs.ErrNotExist)
	assert.NoDirExists(t, WorkspacePath(results))
}

// A file where the workspace should be is refused.
func TestOpenWorkspaceRefusesAFile(t *testing.T) {
	results := testChatDir(t.TempDir())
	require.NoError(t, os.WriteFile(WorkspacePath(results), nil, 0o600))

	for _, create := range []bool{false, true} {
		_, err := OpenWorkspace(results, create)
		assert.ErrorIs(t, err, rootdir.ErrNotADirectory, "create %v", create)
	}
}

// A chat's directory that cannot be opened is passed on, and nothing is made.
func TestOpenWorkspacePassesOnResultsItCannotOpen(t *testing.T) {
	file := filepath.Join(t.TempDir(), "file")
	require.NoError(t, os.WriteFile(file, nil, 0o600))

	_, err := OpenWorkspace(testChatDir(filepath.Join(file, "c1")), true)
	assert.Error(t, err)
}
