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
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/realpath"
)

// Every rule a run's policy holds a PATH folder to, each its own reason.
func TestSearchFolderRefusesWhatARunCannotSearch(t *testing.T) {
	base := realpath.Resolved(t.TempDir())
	ok := mkdirs(t, base, "bin")[0]
	denied := mkdirs(t, base, "denied")[0]
	inDenied := mkdirs(t, denied, "bin")[0]
	world := mkdirs(t, base, "world")[0]
	require.NoError(t, os.Chmod(world, 0o777))
	sticky := mkdirs(t, base, "sticky")[0]
	require.NoError(t, os.Chmod(sticky, 0o777|os.ModeSticky))
	named := mkdirs(t, base, "a:b")[0]
	file := filepath.Join(base, "file")
	require.NoError(t, os.WriteFile(file, nil, 0o600))
	link := filepath.Join(base, "link")
	require.NoError(t, os.Symlink(ok, link))
	intoDenied := filepath.Join(base, "into-denied")
	require.NoError(t, os.Symlink(inDenied, intoDenied))
	home := mkdirs(t, base, "home")[0]
	appData := mkdirs(t, home, ".config")[0]
	inHome := mkdirs(t, home, "bin")[0]
	intoHome := filepath.Join(base, "into-home")
	require.NoError(t, os.Symlink(home, intoHome))
	never, broad := []string{realpath.Resolved(denied)}, Broad(home)

	for dir, want := range map[string]string{
		"":                             "empty",
		"bin":                          "relative",
		"~/bin":                        "relative",
		filepath.Join(base, "missing"): "missing",
		file:                           "missing",
		world:                          "world",
		sticky:                         "world",
		denied:                         "never",
		inDenied:                       "never",
		intoDenied:                     "never",
		named:                          "separator",
		"/":                            "broad",
		base:                           "broad",
		home:                           "broad",
		appData:                        "broad",
		intoHome:                       "broad",
	} {
		folder, _, reason := SearchFolder(dir, never, broad)
		assert.Equal(t, want, reason, dir)
		assert.Empty(t, folder, dir)
	}

	folder, info, reason := SearchFolder(link, never, broad)
	assert.Empty(t, reason)
	assert.Equal(t, ok, folder, "the folder the link resolves to")
	assert.True(t, info.IsDir())

	folder, _, reason = SearchFolder(inHome, never, broad)
	assert.Empty(t, reason, "a folder inside the home holds none of it")
	assert.Equal(t, inHome, folder)
}

// The Read rules a run's PATH needs: a Deny on the very folder gives way, so an
// included closed folder opens; a Deny above it stays, and loses to the deeper
// Read by the policy's own rule.
//
// The folders are real, since a Deny is resolved before it is compared: a
// fixed path such as /home/... resolves elsewhere on macOS.
func TestWithSearchOpensTheFolderItself(t *testing.T) {
	base := realpath.Resolved(t.TempDir())
	downloads := filepath.Join(base, "Downloads")
	documents := filepath.Join(base, "Documents")
	inDocuments := filepath.Join(documents, "bin")
	files := FilePolicy{Read: []string{"/usr"}, Deny: []string{downloads, documents}}
	got := files.WithSearch([]string{downloads, inDocuments})
	assert.Equal(t, FilePolicy{
		Read: []string{"/usr", downloads, inDocuments},
		Deny: []string{documents},
	}, got)
	assert.Equal(t, []string{downloads, documents}, files.Deny, "the policy it was called on is unchanged")
}

// The kernel follows a link before the .. after it, so the entry names the
// folder beside the link's target, not the one beside the link.
func TestSearchFolderFollowsALinkBeforeDotDot(t *testing.T) {
	base := realpath.Resolved(t.TempDir())
	dirs := mkdirs(t, base, "bin", "versions/bin", "versions/v1")
	beside, target := dirs[1], dirs[2]
	require.NoError(t, os.Symlink(target, filepath.Join(base, "current")))

	folder, _, reason := SearchFolder(base+"/current/../bin", nil, nil)
	assert.Empty(t, reason)
	assert.Equal(t, beside, folder)
}
