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

package settings

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// checked is the folders as a run checks them, by z's zones, less the file
// info each carries.
func checked(z *zoneFixture, folders ...PathFolder) []CheckedFolder {
	out := NewRunCheck(z.zones.Open, z.zones.Never, z.zones.Home).Folders(folders)
	for i := range out {
		out[i].info = nil
	}
	return out
}

func shellFolder(path string) PathFolder { return PathFolder{Path: path, Origin: OriginShell} }
func typedFolder(path string) PathFolder { return PathFolder{Path: path, Origin: OriginTyped} }

// A shell folder is searched only when every run reads it already and no
// group but an administrators' can add programs to it; typing it is how the
// user includes one that is not.
func TestAShellFolderIsSearchedOnlyWhenOpen(t *testing.T) {
	fakeGroups(t, "staff")
	z := newZones(t)
	open, closed, shared := z.dir(t, "open/bin"), z.dir(t, "home/bin"), dirWithMode(t, z.open, "shared", 0o775)

	got := checked(z, shellFolder(open), shellFolder(closed), shellFolder(shared))
	assert.Equal(t, CheckedFolder{PathFolder: shellFolder(open), Target: open, Searched: true, Open: true}, got[0])
	assert.False(t, got[1].Searched)
	assert.Equal(t, closed, got[1].Target)
	assert.Contains(t, got[1].Reason, "type it")
	assert.True(t, got[1].Addable)
	assert.False(t, got[2].Searched)
	assert.True(t, got[2].Shared)
	assert.Contains(t, got[2].Reason, "group")
	assert.True(t, got[2].Addable)
}

// A typed folder is the user's read grant: searched where no run reads it,
// with a Read rule, and searched though its group can add programs to it.
func TestATypedFolderIsARead(t *testing.T) {
	fakeGroups(t, "staff")
	z := newZones(t)
	closed, shared := z.dir(t, "home/bin"), dirWithMode(t, z.home, "shared", 0o775)

	got := checked(z, typedFolder(closed), typedFolder(shared))
	assert.Equal(t, CheckedFolder{PathFolder: typedFolder(closed), Target: closed, Searched: true}, got[0])
	assert.Equal(t, CheckedFolder{PathFolder: typedFolder(shared), Target: shared, Searched: true, Shared: true}, got[1])
}

// A typed folder that is a link, or has become one since it was typed, is
// left out naming where it leads: the user typed the link, not its target.
// A shell folder that leads into an open folder is searched there.
func TestATypedLinkIsLeftOut(t *testing.T) {
	z := newZones(t)
	target := z.dir(t, "home/v2")
	link := filepath.Join(z.home, "v1")
	require.NoError(t, os.Symlink(target, link))
	openTarget := z.dir(t, "open/real")
	shellLink := filepath.Join(z.open, "shims")
	require.NoError(t, os.Symlink(openTarget, shellLink))

	got := checked(z, typedFolder(link), shellFolder(shellLink))
	assert.False(t, got[0].Searched)
	assert.Contains(t, got[0].Reason, link+" is a link to "+target)
	assert.True(t, got[1].Searched)
	assert.Equal(t, openTarget, got[1].Target)
}

// What no run may search is left out whatever its origin, a project's
// folder among them.
func TestAFilteredFolderIsLeftOut(t *testing.T) {
	z := newZones(t)
	project := z.dir(t, "open/app/node_modules/.bin")
	world := dirWithMode(t, z.home, "world", 0o777)
	for _, f := range checked(z, shellFolder(project), typedFolder(project), typedFolder(world), shellFolder("bin"), typedFolder(z.never)) {
		assert.False(t, f.Searched, f.Path)
		assert.NotEmpty(t, f.Reason, f.Path)
		assert.False(t, f.Addable, f.Path)
	}
}

// A folder listed twice is judged at each place: a closed shell folder the
// user also typed is searched at its typed place, and a folder already
// searched is not searched again.
func TestAFolderListedTwiceIsJudgedAtEachPlace(t *testing.T) {
	z := newZones(t)
	tools, open := z.dir(t, "home/tools"), z.dir(t, "open/bin")

	got := checked(z, shellFolder(tools), shellFolder(open), typedFolder(tools), typedFolder(open))
	assert.False(t, got[0].Searched)
	assert.Equal(t, "Searched where you typed it.", got[0].Reason)
	assert.False(t, got[0].Addable, "typing it again would not help")
	assert.True(t, got[1].Searched)
	assert.True(t, got[2].Searched)
	assert.False(t, got[3].Searched)
	assert.Equal(t, "Searched earlier in the list.", got[3].Reason)
}

// Add is offered for a shell folder left out only because no run reads it or
// its group can write it, and whose folder could be typed.
func TestAddableFolders(t *testing.T) {
	fakeGroups(t, "staff")
	z := newZones(t)
	closed, shared := z.dir(t, "home/bin"), dirWithMode(t, z.home, "shared", 0o775)
	world := dirWithMode(t, z.home, "world", 0o777)
	dollar := z.dir(t, "home/a$b")
	inNever := z.dir(t, "never/bin")

	got := checked(z, shellFolder(closed), shellFolder(shared), shellFolder(world), shellFolder(dollar), shellFolder(inNever), shellFolder(closed))
	addable := make([]bool, len(got))
	for i, f := range got {
		addable[i] = f.Addable
	}
	assert.Equal(t, []bool{true, true, false, false, false, false}, addable)
}
