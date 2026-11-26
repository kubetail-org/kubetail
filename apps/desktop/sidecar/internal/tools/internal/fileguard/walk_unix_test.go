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

package fileguard

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/realpath"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/run/session"
)

// readWalked is the whole of the file the walk reaches, or the error on the way.
func readWalked(t *testing.T, f Fence, folder session.Folder, path string) (string, error) {
	t.Helper()
	file, err := f.Walk(folder, path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	r, err := file.Open()
	if err != nil {
		return "", err
	}
	defer r.Close()
	b, err := io.ReadAll(r)
	return string(b), err
}

// onWalk runs hook before each step of the walk for the rest of the test.
func onWalk(t *testing.T, hook func(dir, name string)) {
	t.Helper()
	walkHook = hook
	t.Cleanup(func() { walkHook = nil })
}

func TestTheWalkReadsInsideTheGrant(t *testing.T) {
	g := newGrantHome(t)
	home := session.Folder{Path: g.home}
	require.NoError(t, os.Symlink("code/README", g.in("readme-link")))
	require.NoError(t, os.Symlink(g.in("code/svc"), g.in("code/svc-link")))

	for path, want := range map[string]string{
		g.in("code/README"):                         "readme",
		g.in("readme-link"):                         "readme",
		g.in("code/svc-link/main.go"):               "package main",
		g.in("code/svc/../README"):                  "readme",
		g.in("Documents/project/../../code/README"): "readme",
	} {
		got, err := readWalked(t, g.fence, home, path)
		require.NoError(t, err, path)
		assert.Equal(t, want, got, path)
	}
	_, err := readWalked(t, g.fence, home, g.in("code/missing"))
	assert.ErrorIs(t, err, ErrMissing)
	_, err = readWalked(t, g.fence, home, g.in("code"))
	assert.ErrorIs(t, err, ErrDirectory)
}

func TestTheWalkRefusesWhatTheSandboxKeepsShut(t *testing.T) {
	g := newGrantHome(t)
	home := session.Folder{Path: g.home}
	require.NoError(t, os.Symlink(".ssh", g.in("alias")))
	require.NoError(t, os.Symlink("alias", g.in("deep")))
	require.NoError(t, os.Symlink(".netrc", g.in("netrc-link")))
	require.NoError(t, os.Symlink("Documents/notes.txt", g.in("notes")))
	outside := filepath.Join(filepath.Dir(g.home), "outside")
	require.NoError(t, os.WriteFile(outside, []byte("outside"), 0o644))
	require.NoError(t, os.Symlink(outside, g.in("out")))
	require.NoError(t, os.Symlink("../../outside", g.in("code/up")))

	for _, path := range []string{
		g.in(".ssh/id_ed25519"), g.in("alias/id_ed25519"), g.in("deep/id_ed25519"), g.in(".netrc"),
		g.in("netrc-link"), g.in("notes"), g.in("Documents/notes.txt"),
	} {
		_, err := readWalked(t, g.fence, home, path)
		assert.ErrorIs(t, err, ErrHidden, path)
	}
	for _, path := range []string{g.in("out"), g.in("code/up/x")} {
		_, err := readWalked(t, g.fence, home, path)
		assert.ErrorIs(t, err, ErrLeaves, path)
	}

	project := session.Folder{Path: g.in("Documents/project")}
	got, err := readWalked(t, g.fence, project, g.in("Documents/project/a.txt"))
	require.NoError(t, err, "a closed folder above the grant is the one it opens")
	assert.Equal(t, "a", got)
}

func TestTheWalkChecksTheGrantItself(t *testing.T) {
	g := newGrantHome(t)
	svc := session.Folder{Path: g.in("code/svc")}
	require.NoError(t, os.Rename(g.in("code/svc"), g.in("code/svc-old")))
	require.NoError(t, os.Symlink(g.in(".ssh"), g.in("code/svc")))
	_, err := readWalked(t, g.fence, svc, g.in("code/svc/id_ed25519"))
	assert.ErrorIs(t, err, ErrMoved, "the granted folder swapped for a link")

	require.NoError(t, os.Remove(g.in("code/svc")))
	require.NoError(t, os.Rename(g.in("code/svc-old"), g.in("code/svc")))
	require.NoError(t, os.Rename(g.in("code"), g.in("code-old")))
	require.NoError(t, os.Symlink(g.in(".ssh"), g.in("code")))
	require.NoError(t, os.Symlink(g.in(".ssh"), g.in(".ssh/svc")))
	_, err = readWalked(t, g.fence, svc, g.in("code/svc/id_ed25519"))
	assert.ErrorIs(t, err, ErrMoved, "a parent swapped for a link")
}

func TestTheWalkRefusesALinkSwappedIn(t *testing.T) {
	g := newGrantHome(t)
	home := session.Folder{Path: g.home}
	_, ok := g.fence.Granted(t.Context(), []session.Folder{home}, g.in("code/svc/main.go"))
	require.True(t, ok)
	require.NoError(t, os.WriteFile(g.in(".ssh/main.go"), []byte("key"), 0o600))
	before, err := os.ReadFile(g.in(".ssh/main.go"))
	require.NoError(t, err)

	onWalk(t, func(_, name string) {
		if name == "svc" {
			require.NoError(t, os.Rename(g.in("code/svc"), g.in("code/svc-old")))
			require.NoError(t, os.Symlink(g.in(".ssh"), g.in("code/svc")))
		}
	})
	_, err = readWalked(t, g.fence, home, g.in("code/svc/main.go"))
	assert.ErrorIs(t, err, ErrHidden)
	after, err := os.ReadFile(g.in(".ssh/main.go"))
	require.NoError(t, err)
	assert.Equal(t, before, after)
}

func TestTheWalkRenamesOnTheHandle(t *testing.T) {
	g := newGrantHome(t)
	svc := session.Folder{Path: g.in("code/svc"), Write: true}
	outside := filepath.Join(filepath.Dir(g.home), "outside")
	require.NoError(t, os.MkdirAll(outside, 0o755))
	require.NoError(t, os.MkdirAll(g.in("code/svc/sub"), 0o755))
	require.NoError(t, os.WriteFile(g.in("code/svc/sub/old.txt"), []byte("old"), 0o644))

	for name, write := range map[string]func(File) error{
		"old.txt": func(f File) error {
			info, err := f.Lstat()
			if err != nil {
				return err
			}
			return f.Replace(t.Context(), []byte("new"), info)
		},
		"new.txt": func(f File) error { return f.Create(t.Context(), []byte("new"), 0o022) },
	} {
		file, err := g.fence.Walk(svc, g.in("code/svc/sub/"+name))
		require.NoError(t, err, name)
		// The directory swapped for a link between the walk and the write.
		require.NoError(t, os.Rename(g.in("code/svc/sub"), g.in("code/svc/sub-moved")))
		require.NoError(t, os.Symlink(outside, g.in("code/svc/sub")))
		require.NoError(t, write(file), name)
		require.NoError(t, file.Close())

		entries, err := os.ReadDir(outside)
		require.NoError(t, err)
		assert.Empty(t, entries, "%s: nothing outside the folder changes", name)
		b, err := os.ReadFile(g.in("code/svc/sub-moved/" + name))
		require.NoError(t, err)
		assert.Equal(t, "new", string(b), "%s: written on the handle", name)
		require.NoError(t, os.Remove(g.in("code/svc/sub")))
		require.NoError(t, os.Rename(g.in("code/svc/sub-moved"), g.in("code/svc/sub")))
	}
}

func TestAWalkMakesTheMissingParents(t *testing.T) {
	g := newGrantHome(t)
	svc := session.Folder{Path: g.in("code/svc"), Write: true}
	file, err := g.fence.Walk(svc, g.in("code/svc/a/b/c.txt"))
	require.NoError(t, err)
	defer file.Close()
	_, err = file.Lstat()
	require.ErrorIs(t, err, ErrMissing)
	require.NoError(t, file.Creatable())
	require.NoError(t, file.Create(t.Context(), []byte("c"), 0o022))
	b, err := os.ReadFile(g.in("code/svc/a/b/c.txt"))
	require.NoError(t, err)
	assert.Equal(t, "c", string(b))
}

func TestTheFenceCreatesNoClosedPath(t *testing.T) {
	g := newGrantHome(t)
	require.NoError(t, os.RemoveAll(g.in(".ssh")))
	home := session.Folder{Path: g.home}
	_, ok := g.fence.Granted(t.Context(), []session.Folder{home}, g.in(".ssh/config"))
	assert.False(t, ok)
	_, err := readWalked(t, g.fence, home, g.in(".ssh/config"))
	assert.Error(t, err)
	assert.NoDirExists(t, g.in(".ssh"), "a hidden path is never made")
}

// A folder missing when the walk ends is left to Create; a read never opens
// through it, since a link made there after the walk would be followed by the
// root, past every check the walk made.
func TestAnAncestorMadeAfterTheWalkIsNotFollowed(t *testing.T) {
	g := newGrantHome(t)
	home := session.Folder{Path: g.home, Write: true}
	file, err := g.fence.Walk(home, g.in("new/id_ed25519"))
	require.NoError(t, err)
	defer file.Close()
	require.NoError(t, os.Symlink(".ssh", g.in("new")))

	_, err = file.Open()
	assert.ErrorIs(t, err, ErrMissing)
	_, err = file.Lstat()
	assert.ErrorIs(t, err, ErrMissing)
	assert.Error(t, file.Creatable(), "a link in a new folder's way is refused")
	assert.Error(t, file.Create(t.Context(), []byte("planted"), 0o022))
	b, err := os.ReadFile(g.in(".ssh/id_ed25519"))
	require.NoError(t, err)
	assert.Equal(t, "key", string(b))
}

// A link made after the path was resolved reaches the walk unresolved, so the
// walk follows it itself.
func TestTheWalkFollowsALinkMadeAfterTheResolve(t *testing.T) {
	g := newGrantHome(t)
	home := session.Folder{Path: g.home}
	resolvePaths = func(paths []string) []string { return paths }
	t.Cleanup(func() { resolvePaths = realpath.ResolvedAll })
	require.NoError(t, os.Symlink("code/README", g.in("readme-link")))
	require.NoError(t, os.Symlink("svc", g.in("code/svc-link")))
	require.NoError(t, os.Symlink(".ssh", g.in("alias")))
	require.NoError(t, os.Symlink(filepath.Dir(g.home), g.in("out")))
	require.NoError(t, os.Symlink("loop", g.in("loop")))

	for path, want := range map[string]string{
		g.in("readme-link"):           "readme",
		g.in("code/svc-link/main.go"): "package main",
	} {
		got, err := readWalked(t, g.fence, home, path)
		require.NoError(t, err, path)
		assert.Equal(t, want, got, path)
	}
	for path, want := range map[string]error{
		g.in("alias/id_ed25519"): ErrHidden,
		g.in("out/x"):            ErrLeaves,
		g.in("loop"):             ErrTooManyLinks,
	} {
		_, err := readWalked(t, g.fence, home, path)
		assert.ErrorIs(t, err, want, path)
	}
}

func TestTheWalkRefusesWhatIsNotAFolderOnTheWay(t *testing.T) {
	g := newGrantHome(t)
	_, err := g.fence.Walk(session.Folder{Path: g.home}, g.in("code/README/x"))
	assert.ErrorAs(t, err, &ErrNotDir{})
	_, err = g.fence.Walk(session.Folder{Path: g.in("gone")}, g.in("gone/x"))
	assert.ErrorIs(t, err, ErrMissing, "a granted folder that is gone")
}

func TestTheWalkReachesTheGrantItself(t *testing.T) {
	g := newGrantHome(t)
	_, err := readWalked(t, g.fence, session.Folder{Path: g.in("code")}, g.in("code"))
	assert.ErrorIs(t, err, ErrDirectory)
}

func TestAFenceThatKnowsNothingWalksNowhere(t *testing.T) {
	g := newGrantHome(t)
	fence, err := NewFence(filepath.Join(filepath.Dir(g.home), "kubetail"))
	require.NoError(t, err)
	_, err = fence.Walk(session.Folder{Path: g.home}, g.in("code/README"))
	assert.ErrorIs(t, err, ErrHidden)
}

func TestTheWalkAnswersAnErrorOnTheWay(t *testing.T) {
	g := newGrantHome(t)
	_, err := readWalked(t, g.fence, session.Folder{Path: g.home}, g.in("code/"+strings.Repeat("a", 300)))
	assert.ErrorIs(t, err, syscall.ENAMETOOLONG)
}

// A folder on the way that the user cannot open stops the walk with the
// system's own answer.
func TestTheWalkAnswersAFolderItCannotOpen(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root opens any folder")
	}
	g := newGrantHome(t)
	require.NoError(t, os.Mkdir(g.in("code/locked"), 0))
	t.Cleanup(func() { _ = os.Chmod(g.in("code/locked"), 0o700) })

	_, err := readWalked(t, g.fence, session.Folder{Path: g.home}, g.in("code/locked/x"))
	assert.ErrorIs(t, err, os.ErrPermission)
}
