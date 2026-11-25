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

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/app/layout"
)

// Each folder is required and absolute: a relative one would name another
// file from each working directory, and the fence and a chat's paths compare
// absolute paths.
func TestResolveFoldersRefusesAnEmptyOrRelativeFolder(t *testing.T) {
	for _, name := range []string{"the data directory", "the cache directory", "the runtime directory"} {
		for _, dir := range []string{"", "relative"} {
			t.Run(name+"="+dir, func(t *testing.T) {
				t.Chdir(t.TempDir())
				n := linuxNamed(scratch(t))
				switch name {
				case "the data directory":
					n.Data = dir
				case "the cache directory":
					n.Cache = dir
				case "the runtime directory":
					n.Runtime = dir
				}
				_, err := ResolveFolders(n)
				require.ErrorContains(t, err, name)
				assert.NoDirExists(t, "relative")
			})
		}
	}
}

// Every folder is made, and named as the kernel names it.
func TestResolveFoldersMakesEachFolder(t *testing.T) {
	n := linuxNamed(scratch(t))

	f, err := ResolveFolders(n)

	require.NoError(t, err)
	assert.Equal(t, n.Layout, f.Layout)
	assert.Equal(t, []string{n.Data, n.Cache, n.Runtime}, []string{f.Data, f.Cache, f.Runtime})
	for _, dir := range []string{f.Data, f.Cache, f.Runtime} {
		assert.DirExists(t, dir)
	}
	assert.Empty(t, f.Links)
}

// A debug build's moved folder is made where it is named.
func TestResolveFoldersMakesAMissingMovedFolder(t *testing.T) {
	root := scratch(t)
	n := linuxNamed(root)
	n.Data = filepath.Join(root, "moved", "data")

	f, err := ResolveFolders(n)

	require.NoError(t, err)
	assert.Equal(t, n.Data, f.Data)
	assert.DirExists(t, n.Data)
}

// Every platform's layout passes the check, on Windows with its two roots
// apart and as one.
func TestResolveFoldersAcceptsEachPlatformsLayout(t *testing.T) {
	for _, c := range []struct {
		name     string
		platform layout.Platform
		roots    func(root string) layout.Roots
	}{
		{"linux", layout.Linux, linuxRoots},
		{"macos", layout.MacOS, macOSRoots},
		{"windows", layout.Windows, windowsRoots},
		{"windows with one root", layout.Windows, sharedWindowsRoots},
	} {
		t.Run(c.name, func(t *testing.T) {
			root := scratch(t)
			_, err := ResolveFolders(namedOn(c.platform, root, c.roots(root)))
			require.NoError(t, err)
		})
	}
}

// Two kinds the environment puts on one folder fail the start.
func TestResolveFoldersRefusesTwoKindsOnOneFolder(t *testing.T) {
	root := scratch(t)
	n := linuxNamed(root)
	n.Cache = n.Data

	_, err := ResolveFolders(n)

	require.EqualError(t, err, "the data directory and the cache directory are both "+n.Data+"; give each its own folder")
	assert.NoDirExists(t, n.Data, "a refused layout is left as it was")
}

// A kind the layout does not put inside another fails the start there.
func TestResolveFoldersRefusesAKindInsideAnother(t *testing.T) {
	n := linuxNamed(scratch(t))
	n.Cache = filepath.Join(n.Data, "cache")

	_, err := ResolveFolders(n)

	require.EqualError(t, err, "the cache directory "+n.Cache+" is inside the data directory "+n.Data)
}

// On Windows the profile's ACL keeps Kubetail's files private, so a layout
// On Windows a layout folder Windows names outside the profile, as Folder
// Redirection does, is the administrator's; a link leading one out is refused
// (folders_unix_test.go).
func TestResolveFoldersTakesWindowsFoldersWindowsPlaces(t *testing.T) {
	windowsNamed := func(root, profile string) Named {
		n := namedOn(layout.Windows, root, sharedWindowsRoots(root))
		n.Profile = profile
		return n
	}
	t.Run("inside", func(t *testing.T) {
		root := scratch(t)
		_, err := ResolveFolders(windowsNamed(root, root))
		require.NoError(t, err)
	})
	t.Run("redirected outside", func(t *testing.T) {
		_, err := ResolveFolders(windowsNamed(scratch(t), scratch(t)))
		require.NoError(t, err)
	})
	t.Run("a moved folder", func(t *testing.T) {
		root := scratch(t)
		n := windowsNamed(root, root)
		n.Data = filepath.Join(scratch(t), "data")
		_, err := ResolveFolders(n)
		require.NoError(t, err)
	})
}

// A file where a folder goes fails the start.
func TestResolveFoldersFailsOnAFileWhereAFolderGoes(t *testing.T) {
	n := linuxNamed(scratch(t))
	require.NoError(t, os.MkdirAll(filepath.Dir(n.Data), 0o700))
	require.NoError(t, os.WriteFile(n.Data, nil, 0o600))

	_, err := ResolveFolders(n)

	require.ErrorContains(t, err, "the data directory")
}

// On Windows a profile Kubetail cannot name fails the start.
func TestResolveFoldersRefusesAnUnknownWindowsProfile(t *testing.T) {
	root := scratch(t)
	n := namedOn(layout.Windows, root, sharedWindowsRoots(root))
	n.Profile = ""

	_, err := ResolveFolders(n)

	require.ErrorContains(t, err, "the user profile is unknown")
}
