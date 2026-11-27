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
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// A junction at a subfolder fails the start, as a symlink does on Unix.
func TestAJunctionedSubfolderFailsTheStart(t *testing.T) {
	t.Parallel()

	f := testFolders(t)
	link := filepath.Join(f.Cache, "kube")
	out, err := exec.Command("cmd", "/c", "mklink", "/J", link, t.TempDir()).CombinedOutput()
	require.NoError(t, err, string(out))

	err = makeSubfolders(pathsOf(Config{Folders: f}))

	require.EqualError(t, err, link+" is a link; link "+f.Cache+" instead")
}
