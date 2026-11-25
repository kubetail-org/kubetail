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
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/realpath"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/testutil"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/run/session"
)

// grantHome is a home of the test's own with a never-readable .ssh holding a
// key, a closed Documents holding a project, and a code folder; and a fence
// whose hidden sets name the two, resolved.
type grantHome struct {
	home  string
	fence Fence
}

func newGrantHome(t *testing.T) grantHome {
	t.Helper()
	base := testutil.GrantableDir(t)
	home := filepath.Join(base, "home")
	for rel, body := range map[string]string{
		".ssh/id_ed25519": "key", ".netrc": "netrc", "Documents/notes.txt": "notes",
		"Documents/project/a.txt": "a", "code/svc/main.go": "package main", "code/README": "readme",
	} {
		p := filepath.Join(home, rel)
		require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
		require.NoError(t, os.WriteFile(p, []byte(body), 0o644))
	}
	fence, err := NewFence(filepath.Join(base, "kubetail"))
	require.NoError(t, err)
	fence = fence.WithHidden(func() (never, closed []string) {
		return []string{filepath.Join(home, ".ssh"), filepath.Join(home, ".netrc")}, []string{filepath.Join(home, "Documents")}
	})
	return grantHome{home: home, fence: fence}
}

func (g grantHome) in(rel string) string { return filepath.Join(g.home, rel) }

func TestGrantedComparesResolvedPaths(t *testing.T) {
	g := newGrantHome(t)
	require.NoError(t, os.Symlink(g.home, g.in("../home-link")))
	require.NoError(t, os.Symlink(".ssh", g.in("alias")))
	home := session.Folder{Path: g.home}

	got, ok := g.fence.Granted(t.Context(), []session.Folder{home}, g.in("../home-link/code/README"))
	assert.True(t, ok, "a path under a link to a grant matches it")
	assert.Equal(t, home, got)
	for _, p := range []string{g.in(".ssh/id_ed25519"), g.in("alias/id_ed25519"), g.in(".netrc"), g.in("Documents/notes.txt"), g.in("Documents/project/a.txt")} {
		_, ok = g.fence.Granted(t.Context(), []session.Folder{home}, p)
		assert.False(t, ok, "%s is hidden under a granted home", p)
	}
	_, ok = g.fence.Granted(t.Context(), []session.Folder{home}, g.in("code/new/file"))
	assert.True(t, ok, "a path not yet made is judged through its deepest folder")

	project := session.Folder{Path: g.in("Documents/project")}
	got, ok = g.fence.Granted(t.Context(), []session.Folder{home, project}, g.in("Documents/project/a.txt"))
	assert.True(t, ok, "a closed folder above the grant is the grant it opens")
	assert.Equal(t, project, got)

	svc := session.Folder{Path: g.in("code/svc"), Write: true}
	got, ok = g.fence.Granted(t.Context(), []session.Folder{home, svc}, g.in("code/svc/main.go"))
	assert.True(t, ok)
	assert.Equal(t, svc, got, "the read-write folder, where one covers the path")
	_, ok = g.fence.Granted(t.Context(), []session.Folder{svc}, g.in("code/README"))
	assert.False(t, ok, "a path under no grant")

	none, err := NewFence(t.TempDir())
	require.NoError(t, err)
	_, ok = none.Granted(t.Context(), []session.Folder{home}, g.in("code/README"))
	assert.False(t, ok, "a fence that knows nothing hidden skips nothing")
}

// The hidden paths are compared as files, so a never file under a second name
// inside a grant is still hidden: neither the gate nor the walk opens it.
func TestAHiddenFileUnderAnotherNameStaysHidden(t *testing.T) {
	g := newGrantHome(t)
	home := session.Folder{Path: g.home}
	require.NoError(t, os.Link(g.in(".netrc"), g.in("code/notes")))

	_, ok := g.fence.Granted(t.Context(), []session.Folder{home}, g.in("code/notes"))
	assert.False(t, ok)
	_, err := readWalked(t, g.fence, home, g.in("code/notes"))
	assert.ErrorIs(t, err, ErrHidden)
}

// A never path that is a link is caught at the target it has now, not the one
// it had when the sandbox's lists were read: with ~/.ssh retargeted under a
// granted home, neither the gate nor the walk opens the new target.
func TestARetargetedNeverPathStaysHidden(t *testing.T) {
	g := newGrantHome(t)
	home := session.Folder{Path: g.home}
	for _, dir := range []string{"dotfiles/ssh-a", "dotfiles/ssh-b"} {
		require.NoError(t, os.MkdirAll(g.in(dir), 0o700))
		require.NoError(t, os.WriteFile(g.in(dir+"/id_ed25519"), []byte("key"), 0o600))
	}
	require.NoError(t, os.RemoveAll(g.in(".ssh")))
	require.NoError(t, os.Symlink(g.in("dotfiles/ssh-a"), g.in(".ssh")))
	_, ok := g.fence.Granted(t.Context(), []session.Folder{home}, g.in(".ssh/id_ed25519"))
	require.False(t, ok)

	require.NoError(t, os.Remove(g.in(".ssh")))
	require.NoError(t, os.Symlink(g.in("dotfiles/ssh-b"), g.in(".ssh")))
	_, ok = g.fence.Granted(t.Context(), []session.Folder{home}, g.in(".ssh/id_ed25519"))
	assert.False(t, ok, "the gate asks for the new target")
	_, err := readWalked(t, g.fence, home, g.in(".ssh/id_ed25519"))
	assert.ErrorIs(t, err, ErrHidden, "the walk refuses the new target")
	_, err = readWalked(t, g.fence, home, g.in("dotfiles/ssh-b/id_ed25519"))
	assert.ErrorIs(t, err, ErrHidden, "by its own path too")
}

// Resolving a path can block in a syscall on a dead network mount, so Granted
// answers the context without it; with no folder granted nothing is resolved.
func TestGrantedAnswersTheCancelWhileResolvingBlocks(t *testing.T) {
	g := newGrantHome(t)
	release := make(chan struct{})
	returned := make(chan struct{})
	resolved := 0
	resolvePaths = func(paths []string) []string {
		// Granted resolves both hidden sets, then the path. The first blocks;
		// the last is the abandoned goroutine's final read of resolvePaths,
		// which the cleanup below must not race.
		resolved++
		if resolved == 1 {
			<-release
		}
		if resolved == 3 {
			close(returned)
		}
		return paths
	}
	t.Cleanup(func() { resolvePaths = realpath.ResolvedAll })

	_, ok := g.fence.Granted(t.Context(), nil, g.in("code/README"))
	assert.False(t, ok)
	assert.Zero(t, resolved, "no grant, nothing resolved")

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, ok = g.fence.Granted(ctx, []session.Folder{{Path: g.home}}, g.in("code/README"))
	assert.False(t, ok, "a cancelled gate asks")
	close(release)
	testutil.Wait(t, returned, "the abandoned resolve")
}
