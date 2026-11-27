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

package app

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A link at any subfolder fails the start, naming it and the folder to link
// instead; the folder it leads to is left as it was.
func TestALinkedSubfolderFailsTheStart(t *testing.T) {
	t.Parallel()

	for _, sub := range []struct{ root, name string }{
		{"data", "chats"}, {"data", "monitor"},
		{"cache", "kube"}, {"cache", "logs"}, {"cache", "kubectl"}, {"cache", "tmp"},
		{"runtime", "shell"}, {"runtime", "runs"},
	} {
		t.Run(sub.name, func(t *testing.T) {
			f := testFolders(t)
			root := map[string]string{"data": f.Data, "cache": f.Cache, "runtime": f.Runtime}[sub.root]
			link := filepath.Join(root, sub.name)
			elsewhere := t.TempDir()
			kept := filepath.Join(elsewhere, "kept")
			require.NoError(t, os.WriteFile(kept, nil, 0o600))
			require.NoError(t, os.Symlink(elsewhere, link))

			err := makeSubfolders(pathsOf(Config{Folders: f}))

			require.EqualError(t, err, link+" is a link; link "+root+" instead")
			assert.FileExists(t, kept)
		})
	}
}

// A subfolder that cannot be made, in a folder this user cannot write, fails
// the start.
func TestASubfolderThatCannotBeMadeFailsTheStart(t *testing.T) {
	t.Parallel()

	f := testFolders(t)
	require.NoError(t, os.Chmod(f.Runtime, 0o500))
	t.Cleanup(func() { _ = os.Chmod(f.Runtime, 0o700) })

	require.Error(t, makeSubfolders(pathsOf(Config{Folders: f})))
}
