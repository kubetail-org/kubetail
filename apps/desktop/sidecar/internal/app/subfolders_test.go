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

package app

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Each subfolder pathsOf names is made at start, under its folder.
func TestMakeSubfoldersMakesEachSubfolder(t *testing.T) {
	t.Parallel()

	p := pathsOf(Config{Folders: testFolders(t)})

	require.NoError(t, makeSubfolders(p))

	for _, dir := range []string{p.ChatsDir, p.MonitorDir, p.Cluster.KubestoreDir, p.LogsDir, p.Bash.KubectlDir, p.Bash.TmpDir, p.Bash.ShellDir, p.Bash.RunsDir} {
		assert.DirExists(t, dir)
	}
}

// A file where a subfolder goes fails the start, naming it.
func TestAFileWhereASubfolderGoesFailsTheStart(t *testing.T) {
	t.Parallel()

	f := testFolders(t)
	file := filepath.Join(f.Data, "chats")
	require.NoError(t, os.WriteFile(file, nil, 0o600))

	err := makeSubfolders(pathsOf(Config{Folders: f}))

	require.EqualError(t, err, file+" is not a folder")
}

// A missing folder to make subfolders in fails the start.
func TestMakeSubfoldersFailsOnAMissingFolder(t *testing.T) {
	t.Parallel()

	f := testFolders(t)
	f.Cache = filepath.Join(t.TempDir(), "missing")

	require.Error(t, makeSubfolders(pathsOf(Config{Folders: f})))
}
