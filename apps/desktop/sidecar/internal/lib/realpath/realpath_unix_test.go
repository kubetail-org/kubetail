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

package realpath

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A path that does not exist is resolved through its deepest folder that
// does, so a missing path under a link names where the link leads.
func TestAMissingPathIsResolvedThroughItsDeepestFolder(t *testing.T) {
	base := Resolved(t.TempDir())
	target := mkdirs(t, base, "private/var")[0]
	require.NoError(t, os.Symlink(target, filepath.Join(base, "var")))

	assert.Equal(t, filepath.Join(target, "missing", "file"), Resolved(filepath.Join(base, "var", "missing", "file")))
}

// Under follows the links of both paths itself, so no caller can forget to.
func TestUnderFollowsLinks(t *testing.T) {
	base := Resolved(t.TempDir())
	inside := mkdirs(t, base, "denied/inside")[0]
	link := filepath.Join(base, "link")
	require.NoError(t, os.Symlink(inside, link))
	denied := []string{filepath.Join(base, "denied")}

	assert.True(t, UnderAny(link, denied))
	assert.True(t, UnderAny(filepath.Join(link, "missing", "file"), denied), "a missing path, through its deepest folder")
	assert.True(t, Under(inside, filepath.Join(link, "..")), "a folder named through a link")
	assert.True(t, UnderAny(denied[0], denied))
	assert.False(t, UnderAny(base, denied))
	assert.True(t, Same(link, inside))
	assert.False(t, Same(link, denied[0]))
}

// The kernel follows a link before a .., so the path names the parent of the
// link's target, not the folder beside the link.
func TestALinkBeforeDotDotIsFollowedFirst(t *testing.T) {
	base := Resolved(t.TempDir())
	d := mkdirs(t, base, "opt/tool/v2", "opt/tool/bin", "bin")
	current := filepath.Join(base, "current")
	require.NoError(t, os.Symlink(d[0], current))
	asWritten := current + "/../bin"

	assert.Equal(t, d[1], Resolved(asWritten))
	assert.Equal(t, filepath.Join(d[1], "missing"), Resolved(asWritten+"/missing/"))
	assert.Equal(t, []string{d[1], filepath.Join(d[1], "missing")}, canonicalOf(t, asWritten, asWritten+"/missing"))
	assert.True(t, Same(asWritten, d[1]))
	assert.False(t, Same(asWritten, d[2]))
}

// Two paths are compared as files. A hard link is the one way to give a file
// two names on a filesystem that keeps case, and stands in here for a folder
// one that folds names opens under two spellings.
func TestUnderComparesFilesNotNames(t *testing.T) {
	base := Resolved(t.TempDir())
	d := mkdirs(t, base, "keys", "work")
	key := filepath.Join(d[0], "id")
	require.NoError(t, os.WriteFile(key, nil, 0o600))
	other := filepath.Join(d[1], "notes")
	require.NoError(t, os.Link(key, other))

	assert.True(t, Under(other, key))
	assert.True(t, Same(other, key))
	assert.False(t, Under(other, filepath.Join(d[0], "id.pub")), "a path that is not there is compared by name")
	assert.False(t, Under(other, d[0]), "a hard link lies in its own folder alone")
	assert.False(t, Under(filepath.Join(d[1], "missing"), filepath.Join(d[0], "missing")))
}

// unnameable is a path the kernel cannot name: one through a link to itself.
func unnameable(t *testing.T) string {
	t.Helper()
	loop := filepath.Join(Resolved(t.TempDir()), "loop")
	require.NoError(t, os.Symlink(loop, loop))
	return filepath.Join(loop, "svc")
}

func TestCanonicalFailsOnAPathThroughALinkToItself(t *testing.T) {
	svc := unnameable(t)

	got, err := Canonical([]string{svc})
	require.ErrorContains(t, err, svc)
	assert.Equal(t, []string{svc}, got, "kept as given")
}

// The kernel names a path by searching its folders, so none need be listed.
func TestCanonicalNamesAPathUnderAFolderItCannotList(t *testing.T) {
	svc := mkdirs(t, Resolved(t.TempDir()), "locked/svc")[0]
	locked := filepath.Dir(svc)
	require.NoError(t, os.Chmod(locked, 0o311))
	t.Cleanup(func() { os.Chmod(locked, 0o755) })
	missing := filepath.Join(locked, "missing", "x")

	assert.Equal(t, []string{svc, missing}, canonicalOf(t, svc, missing))
}

// A folder this user may not search hides what it holds from a run of theirs
// too, so a path through it is kept as given.
func TestCanonicalKeepsAPathItMayNotReach(t *testing.T) {
	svc := mkdirs(t, Resolved(t.TempDir()), "locked/svc")[0]
	locked := filepath.Dir(svc)
	require.NoError(t, os.Chmod(locked, 0o600))
	t.Cleanup(func() { os.Chmod(locked, 0o755) })
	if _, err := os.Stat(svc); err == nil {
		t.Skip("this user searches a folder whatever its mode")
	}

	assert.Equal(t, []string{svc}, canonicalOf(t, svc))
}

// Nothing lies beneath a file, so a path through one is missing. Any other
// failure leaves the path's name unknown.
func TestCanonicalFailsOnAPathTheKernelCannotName(t *testing.T) {
	base := Resolved(t.TempDir())
	file := filepath.Join(base, "file")
	require.NoError(t, os.WriteFile(file, nil, 0o600))

	through := filepath.Join(file, "x")
	got, err := Canonical([]string{through})
	require.NoError(t, err)
	assert.Equal(t, []string{through}, got)

	_, err = Canonical([]string{filepath.Join(base, strings.Repeat("n", 300))})
	require.ErrorIs(t, err, syscall.ENAMETOOLONG)
	_, err = Canonical([]string{filepath.Join(base, "a\x00b")})
	require.ErrorIs(t, err, syscall.EINVAL, "no file is named with a NUL")
}
