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

package realpath

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// linkInAnotherCase makes base/Disk/kube and a link, base/link, whose text
// spells it base/DISK/KUBE. It answers the folder and the link. On a
// case-insensitive volume the link leads to the folder; on another it skips
// the test.
func linkInAnotherCase(t *testing.T) (folder, link string) {
	t.Helper()
	base := canonicalOf(t, t.TempDir())[0]
	folder = mkdirs(t, base, "Disk/kube")[0]
	upper := filepath.Join(base, "DISK", "KUBE")
	skipIfCaseSensitive(t, upper)
	link = filepath.Join(base, "link")
	require.NoError(t, os.Symlink(upper, link))
	return folder, link
}

// skipIfCaseSensitive skips the test where other, another spelling of a file
// the test made, names nothing.
func skipIfCaseSensitive(t *testing.T, other string) {
	t.Helper()
	if _, err := os.Stat(other); errors.Is(err, fs.ErrNotExist) {
		t.Skip("the filesystem is case-sensitive")
	}
}

// A folder in another case is the same folder, with no link and no canonical
// name read: the two are compared as files.
func TestUnderHoldsAFolderInAnotherCase(t *testing.T) {
	base := canonicalOf(t, t.TempDir())[0]
	folder := mkdirs(t, base, "Disk/kube")[0]
	upper := filepath.Join(base, "DISK", "KUBE")
	skipIfCaseSensitive(t, upper)

	assert.True(t, Same(upper, folder))
	assert.True(t, Under(filepath.Join(upper, "config"), folder))
	assert.True(t, Under(filepath.Join(upper, "new", "file"), filepath.Join(folder, "new")), "names past the deepest folder are compared as given")
	assert.False(t, Under(filepath.Join(upper, "other"), filepath.Join(folder, "new")))
	assert.False(t, Under(filepath.Join(base, "DISK"), folder))
}

// A folder that decides one dir and not another leaves the other to a folder
// further up: Holds answers as UnderAny of each dir apart.
func TestDirsHoldsWhatAnyOfItsDirsHolds(t *testing.T) {
	base := canonicalOf(t, t.TempDir())[0]
	folder := mkdirs(t, base, "Disk/kube")[0]
	upper := filepath.Join(base, "DISK")
	skipIfCaseSensitive(t, upper)
	p := filepath.Join(folder, "config")
	dirs := ResolveDirs([]string{filepath.Join(upper, "KUBE", "new"), upper})

	assert.False(t, Under(p, filepath.Join(upper, "KUBE", "new")), "kube decides new: config is not in it")
	assert.True(t, dirs.Holds(p), "Disk is DISK, and holds config")
	assert.False(t, ResolveDirs([]string{filepath.Join(upper, "KUBE", "new")}).Holds(p))
	assert.False(t, ResolveDirs(nil).Holds(p))
}

// Why Canonical asks the kernel: following a link keeps the link's own
// capitalization, so a resolved path need not match the disk's spelling.
func TestEvalSymlinksKeepsALinksCapitalization(t *testing.T) {
	folder, link := linkInAnotherCase(t)

	r, err := filepath.EvalSymlinks(link)
	require.NoError(t, err)

	assert.NotEqual(t, folder, r)
	assert.True(t, sameFile(t, folder, r), "the volume is case-insensitive")
}

func TestCanonicalSpellsALinksTargetAsTheDiskDoes(t *testing.T) {
	folder, link := linkInAnotherCase(t)

	assert.Equal(t, []string{folder, filepath.Join(folder, "missing")},
		canonicalOf(t, link, filepath.Join(link, "missing")))
}

// The directory lists both names for the one file, the other first.
func TestCanonicalSpellsAHardLinkedFileByItsOwnEntry(t *testing.T) {
	base := canonicalOf(t, t.TempDir())[0]
	file := filepath.Join(base, "Cred")
	require.NoError(t, os.WriteFile(file, nil, 0o600))
	require.NoError(t, os.Link(file, filepath.Join(base, "Alias")))
	upper := filepath.Join(base, "CRED")
	skipIfCaseSensitive(t, upper)

	assert.Equal(t, []string{file}, canonicalOf(t, upper))
}

func sameFile(t *testing.T, a, b string) bool {
	t.Helper()
	ia, err := os.Stat(a)
	require.NoError(t, err)
	ib, err := os.Stat(b)
	require.NoError(t, err)
	return os.SameFile(ia, ib)
}

// A volume that folds an accent's two compositions together stores one, and
// the kernel names the folder by it.
func TestCanonicalSpellsAnAccentAsStored(t *testing.T) {
	composed, decomposed := "caf\u00e9", "cafe\u0301"
	for _, tc := range []struct{ name, stored, other string }{
		{"stored composed", composed, decomposed},
		{"stored decomposed", decomposed, composed},
	} {
		t.Run(tc.name, func(t *testing.T) {
			base := canonicalOf(t, t.TempDir())[0]
			stored := mkdirs(t, base, tc.stored)[0]
			other := filepath.Join(base, tc.other)
			if _, err := os.Stat(other); errors.Is(err, fs.ErrNotExist) {
				t.Skip("the filesystem keeps the two compositions apart")
			}
			link := filepath.Join(base, "link")
			require.NoError(t, os.Symlink(other, link))

			assert.Equal(t, []string{stored, stored, stored}, canonicalOf(t, stored, other, link))
		})
	}
}

// Naming a path asks for no access to it: a folder that can be searched and
// not read, and a file that cannot be read, are still spelled as stored.
func TestCanonicalSpellsWhatItMayNotRead(t *testing.T) {
	base := canonicalOf(t, t.TempDir())[0]
	inner := mkdirs(t, base, "Searchable/Inner")[0]
	dir := filepath.Dir(inner)
	file := filepath.Join(inner, "File")
	require.NoError(t, os.WriteFile(file, nil, 0))
	skipIfCaseSensitive(t, filepath.Join(base, "SEARCHABLE"))
	require.NoError(t, os.Chmod(dir, 0o100))
	t.Cleanup(func() { os.Chmod(dir, 0o700) })

	assert.Equal(t, []string{dir, inner, file}, canonicalOf(t,
		filepath.Join(base, "SEARCHABLE"), filepath.Join(base, "SEARCHABLE", "INNER"), filepath.Join(base, "SEARCHABLE", "INNER", "FILE")))
}

// The data volume's own path and the path through its firmlink name one
// folder, and the kernel answers the firmlink's.
func TestCanonicalNamesTheHomeThroughItsFirmlink(t *testing.T) {
	home, err := os.UserHomeDir()
	require.NoError(t, err)
	onData := filepath.Join("/System/Volumes/Data", home)
	if _, err := os.Stat(onData); err != nil {
		t.Skip("no data volume at /System/Volumes/Data")
	}

	got := canonicalOf(t, home, onData)
	assert.Equal(t, got[0], got[1])
}

// macOS's /var is a link to /private/var, which the root holds.
func TestAPathUnderVarIsFollowedToPrivateVar(t *testing.T) {
	got, links := followed(t, "/var/kubetail-missing")

	assert.Equal(t, "/private/var/kubetail-missing", got)
	assert.Equal(t, []Link{{Name: "/var", Parent: "/"}}, links)
}
