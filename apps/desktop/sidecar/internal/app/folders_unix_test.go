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

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/app/layout"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/realpath"
)

// symlink makes a link at name to target, with the folders above name.
func symlink(t *testing.T, target, name string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(name), 0o700))
	require.NoError(t, os.Symlink(target, name))
}

// leaveOpen makes each of dirs 0755, as an earlier build left it.
func leaveOpen(t *testing.T, dirs ...string) {
	t.Helper()
	for _, dir := range dirs {
		require.NoError(t, os.MkdirAll(dir, 0o755))
		require.NoError(t, os.Chmod(dir, 0o755))
	}
}

func assertPerm(t *testing.T, want os.FileMode, dirs ...string) {
	t.Helper()
	for _, dir := range dirs {
		info, err := os.Stat(dir)
		require.NoError(t, err)
		assert.Equal(t, want, info.Mode().Perm(), dir)
	}
}

// A layout folder that is a link starts, and is named by its target, which
// is what the fence holds.
func TestALinkedLayoutFolderIsResolved(t *testing.T) {
	root := scratch(t)
	target := filepath.Join(root, "big", "kubetail")
	require.NoError(t, os.MkdirAll(target, 0o700))
	n := linuxNamed(root)
	symlink(t, target, n.Cache)

	f, err := ResolveFolders(n)

	require.NoError(t, err)
	assert.Equal(t, target, f.Cache)
	assert.Equal(t, target, f.Layout.Cache)
	assert.Equal(t, []realpath.Link{{Name: n.Cache, Parent: filepath.Dir(n.Cache)}}, f.Links)
	assert.Contains(t, pathsOf(Config{Folders: f}).Bash.DeniedDirs, target)
	assert.NotContains(t, pathsOf(Config{Folders: f}).Bash.DeniedDirs, n.Cache)
	assert.Equal(t, f.Links, pathsOf(Config{Folders: f}).Bash.Links, "a run and a grant read them")
}

// A root reached through a linked parent starts, and is named by where the
// link leads.
func TestARootUnderALinkedParentIsResolved(t *testing.T) {
	root := scratch(t)
	target := filepath.Join(root, "var", "home")
	require.NoError(t, os.MkdirAll(target, 0o700))
	home := filepath.Join(root, "home")
	symlink(t, target, home)
	n := namedOn(layout.Linux, root, linuxRoots(home))

	f, err := ResolveFolders(n)

	require.NoError(t, err)
	assert.Equal(t, filepath.Join(target, "cache", "kubetail"), f.Cache)
	assert.Equal(t, []string{root}, realpath.Parents(f.Links))
	assert.Contains(t, pathsOf(Config{Folders: f}).Bash.DeniedDirs, f.Cache)
}

// A layout folder that is a link to nothing fails the start, naming both.
func TestALinkedLayoutFolderWhoseTargetIsMissingFailsTheStart(t *testing.T) {
	root := scratch(t)
	n := linuxNamed(root)
	target := filepath.Join(root, "gone")
	symlink(t, target, n.Cache)

	_, err := ResolveFolders(n)

	require.ErrorContains(t, err, n.Cache)
	require.ErrorContains(t, err, target)
	assert.NoDirExists(t, target)
}

// A cycle of links at a layout folder stops the start, naming the folder.
func TestACycleOfLinksAtALayoutFolderFailsTheStart(t *testing.T) {
	root := scratch(t)
	n := linuxNamed(root)
	symlink(t, n.Cache, filepath.Join(root, "loop"))
	symlink(t, filepath.Join(root, "loop"), n.Cache)

	_, err := ResolveFolders(n)

	require.ErrorContains(t, err, n.Cache)
}

// The runtime folder's parent can be shared, so a link at the folder itself
// is refused; one above it is followed.
func TestALinkedRuntimeFolderFailsTheStart(t *testing.T) {
	root := scratch(t)
	n := linuxNamed(root)
	symlink(t, t.TempDir(), n.Runtime)

	_, err := ResolveFolders(n)

	require.ErrorContains(t, err, n.Runtime)
}

func TestARuntimeFolderUnderALinkedParentIsResolved(t *testing.T) {
	root := scratch(t)
	target := filepath.Join(root, "private", "var")
	require.NoError(t, os.MkdirAll(target, 0o700))
	n := linuxNamed(root)
	symlink(t, target, filepath.Join(root, "var"))
	n.Runtime = filepath.Join(root, "var", "kubetail-501")

	f, err := ResolveFolders(n)

	require.NoError(t, err)
	assert.Equal(t, filepath.Join(target, "kubetail-501"), f.Runtime)
	assert.DirExists(t, f.Runtime)
}

// Two kinds linked to one folder fail the start, and so does a kind linked
// inside another.
func TestKindsLinkedOntoOneAnotherFailTheStart(t *testing.T) {
	t.Run("one folder", func(t *testing.T) {
		root := scratch(t)
		n := linuxNamed(root)
		target := filepath.Join(root, "shared")
		require.NoError(t, os.Mkdir(target, 0o700))
		symlink(t, target, n.Data)
		symlink(t, target, n.Cache)

		_, err := ResolveFolders(n)

		require.EqualError(t, err, "the data directory and the cache directory are both "+target+"; give each its own folder")
	})
	t.Run("inside", func(t *testing.T) {
		root := scratch(t)
		n := linuxNamed(root)
		target := filepath.Join(n.Data, "inner")
		require.NoError(t, os.MkdirAll(target, 0o700))
		symlink(t, target, n.Cache)

		_, err := ResolveFolders(n)

		require.EqualError(t, err, "the cache directory "+target+" is inside the data directory "+n.Data)
	})
}

// Logs linked out of the state are fine: they resolve outside it.
func TestLogsLinkedOutOfTheStateStart(t *testing.T) {
	root := scratch(t)
	n := linuxNamed(root)
	target := filepath.Join(root, "mnt", "logs")
	require.NoError(t, os.MkdirAll(target, 0o700))
	symlink(t, target, n.Layout.Logs)

	f, err := ResolveFolders(n)

	require.NoError(t, err)
	assert.Equal(t, target, f.Layout.Logs)
}

// A layout the check refuses is left as it was: no folder made, no mode
// changed.
func TestARefusedLayoutChangesNoMode(t *testing.T) {
	root := scratch(t)
	n := linuxNamed(root)
	leaveOpen(t, n.Data)
	n.Cache = n.Data

	_, err := ResolveFolders(n)

	require.Error(t, err)
	assertPerm(t, 0o755, n.Data)
	assert.NoDirExists(t, n.Runtime)
}

// A folder the host did not make, as in a run without it, is made
// owner-only.
func TestResolveFoldersMakesAMissingFolderOwnerOnly(t *testing.T) {
	f, err := ResolveFolders(linuxNamed(scratch(t)))

	require.NoError(t, err)
	assertPerm(t, 0o700, f.Data, f.Cache, f.Runtime)
}

// An earlier build left the data and cache directories, and the app folder
// above them, open: each is tightened before the sidecar writes in it.
func TestResolveFoldersTightensItsFoldersAndTheAppFolderAbove(t *testing.T) {
	root := scratch(t)
	n := namedOn(layout.MacOS, root, macOSRoots(root))
	leaveOpen(t, n.Layout.App, n.Data, n.Cache)

	_, err := ResolveFolders(n)

	require.NoError(t, err)
	assertPerm(t, 0o700, n.Layout.App, n.Data, n.Cache)
}

// A debug build's moved folder may be one the developer shares: one that
// exists keeps its mode.
func TestResolveFoldersLeavesAMovedFolderAsItIs(t *testing.T) {
	root := scratch(t)
	n := linuxNamed(root)
	n.Data, n.Cache = filepath.Join(root, "shared-data"), filepath.Join(root, "shared-cache")
	leaveOpen(t, n.Data, n.Cache)

	_, err := ResolveFolders(n)

	require.NoError(t, err)
	assertPerm(t, 0o755, n.Data, n.Cache)
}

// A moved folder spelling the layout's own folder another way is that
// folder, and is tightened.
func TestResolveFoldersTightensTheLayoutsFolderSpelledAnotherWay(t *testing.T) {
	n := linuxNamed(scratch(t))
	leaveOpen(t, n.Data, n.Cache)
	data, cache := n.Data, n.Cache
	n.Data, n.Cache = data+"/", cache+"/."

	_, err := ResolveFolders(n)

	require.NoError(t, err)
	assertPerm(t, 0o700, data, cache)
}

// A moved folder reaching the layout's own through a link is that folder: it
// is tightened, and the app folder holding it too.
func TestResolveFoldersTightensTheLayoutsFolderReachedThroughALink(t *testing.T) {
	root := scratch(t)
	n := namedOn(layout.MacOS, root, macOSRoots(root))
	leaveOpen(t, n.Layout.App, n.Data)
	n.Data = filepath.Join(root, "data-link")
	symlink(t, n.Layout.Data, n.Data)

	_, err := ResolveFolders(n)

	require.NoError(t, err)
	assertPerm(t, 0o700, n.Layout.App, n.Layout.Data)
}

// A profile that is a link to nothing fails the start, naming it.
func TestResolveFoldersFailsOnAWindowsProfileLinkedToNothing(t *testing.T) {
	root := scratch(t)
	n := namedOn(layout.Windows, root, sharedWindowsRoots(root))
	n.Profile = filepath.Join(root, "profile")
	symlink(t, filepath.Join(root, "gone"), n.Profile)

	_, err := ResolveFolders(n)

	require.ErrorContains(t, err, n.Profile)
}

// On Windows a link that leads a layout folder out of the profile, or onto
// the profile itself, fails the start: the profile's ACL is what keeps the
// folder private. Nothing is made.
func TestResolveFoldersRefusesALinkOutOfTheWindowsProfile(t *testing.T) {
	for _, c := range []struct {
		name   string
		target func(root string) string
	}{
		{"outside", func(string) string { return scratch(t) }},
		{"the profile itself", func(root string) string { return root }},
	} {
		t.Run(c.name, func(t *testing.T) {
			root := scratch(t)
			n := namedOn(layout.Windows, root, sharedWindowsRoots(root))
			target := c.target(root)
			symlink(t, target, n.Layout.App)

			_, err := ResolveFolders(n)

			require.EqualError(t, err, n.Layout.App+" leads to "+target+", outside your profile; keep it inside "+root)
			assert.NoDirExists(t, filepath.Join(target, "data"))
		})
	}
}
