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

package chat

import (
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/run/sandbox"

	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The service keeps every chat's directory under one owner-only directory.
func TestTheServiceMakesTheChatsDirectory(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	startService(t, dir)

	info, err := os.Stat(chatsDirIn(dir))
	require.NoError(t, err)
	assert.True(t, info.IsDir())
	assert.Equal(t, os.FileMode(0o700), info.Mode().Perm())
}

// A chat's directory is made owner-only.
func TestAChatsDirectoryIsOwnerOnly(t *testing.T) {
	t.Parallel()

	s := startService(t, t.TempDir())
	d := s.chatDir("c1")

	root, err := d.Root(true)
	require.NoError(t, err)
	require.NoError(t, root.Close())
	info, err := os.Stat(d.Path())
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o700), info.Mode().Perm())
}

// A chats' directory the service cannot open fails its construction.
func TestNewRefusesAChatsDirectoryItCannotOpen(t *testing.T) {
	t.Parallel()

	if os.Getuid() == 0 {
		t.Skip("root opens an unreadable directory")
	}
	dir := t.TempDir()
	chats := chatsDirIn(dir)
	require.NoError(t, os.Mkdir(chats, 0o000))
	t.Cleanup(func() { _ = os.Chmod(chats, 0o700) })

	_, err := newService(openTestDB(t, dir), chats, monitorDirIn(dir), fakeLLM(), noClusterCards, nil, testReaders, noLists, sandbox.State{}, testSettings(t), nil)

	assert.ErrorContains(t, err, "open the chats' directory")
}

// A chat's entry swapped for a link to a directory outside the chats' directory
// is refused, so nothing a command links in is written through.
func TestOpenResultsRefusesALinkedDirectory(t *testing.T) {
	t.Parallel()

	s := newTestService(t)
	d := s.chatDir("c1")
	require.NoError(t, os.MkdirAll(filepath.Dir(d.Path()), 0o700))
	outside := t.TempDir()
	require.NoError(t, os.Symlink(outside, d.Path()))

	for _, create := range []bool{false, true} {
		_, err := d.Root(create)
		assert.Error(t, err, "create %v", create)
	}
	entries, err := os.ReadDir(outside)
	require.NoError(t, err)
	assert.Empty(t, entries)
}

// Deleting a chat removes a link in its directory and never the link's target.
func TestDeletingAChatRemovesALinkNotItsTarget(t *testing.T) {
	t.Parallel()

	s := newTestService(t)
	c := seedChat(t, s.db, aChat("1", time.Now()))
	path := makeChatDir(t, s, c.ID)
	target := filepath.Join(t.TempDir(), "keep.txt")
	require.NoError(t, os.WriteFile(target, []byte("keep"), 0o600))
	require.NoError(t, os.Symlink(target, filepath.Join(path, "link")))
	require.NoError(t, os.Symlink(filepath.Dir(target), filepath.Join(path, "dirlink")))

	require.NoError(t, s.Delete(t.Context(), c.ID))

	assert.NoDirExists(t, path)
	assert.FileExists(t, target)
}

// A removal that fails leaves the directory for the next start's sweep. A
// read-only chats' directory fails it in a way no chmod of the chat's own
// entry undoes.
func TestAFailedRemovalIsLeftForTheStartSweep(t *testing.T) {
	t.Parallel()

	if os.Getuid() == 0 {
		t.Skip("root removes a read-only directory's entries")
	}
	dir := t.TempDir()
	s := startService(t, dir)
	c := seedChat(t, s.db, aChat("1", time.Now()))
	path := makeChatDir(t, s, c.ID)
	results := filepath.Dir(path)
	require.NoError(t, os.Chmod(results, 0o500))
	t.Cleanup(func() { _ = os.Chmod(results, 0o700) })

	require.NoError(t, s.Delete(t.Context(), c.ID), "the rows go whatever the files do")
	assert.DirExists(t, path)

	require.NoError(t, os.Chmod(results, 0o700))
	startService(t, dir)
	assert.NoDirExists(t, path)
}

// A command can leave a directory its owner cannot write, such as a Go module
// cache. The chat's directory still goes with it, and a link in it pointing out
// goes without its target.
func TestAReadOnlyWorkspaceGoesWithTheChat(t *testing.T) {
	t.Parallel()

	if os.Getuid() == 0 {
		t.Skip("root removes a read-only directory's entries")
	}
	s := newTestService(t)
	c := seedChat(t, s.db, aChat("1", time.Now()))
	path := makeChatDir(t, s, c.ID)
	cache := filepath.Join(path, "work", "cache")
	require.NoError(t, os.MkdirAll(cache, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(cache, "mod"), nil, 0o400))
	outside := t.TempDir()
	require.NoError(t, os.Chmod(outside, 0o500))
	t.Cleanup(func() { _ = os.Chmod(outside, 0o700) })
	require.NoError(t, os.Symlink(outside, filepath.Join(cache, "out")))
	require.NoError(t, os.Chmod(cache, 0o500))
	require.NoError(t, os.Chmod(filepath.Join(path, "work"), 0o500))

	require.NoError(t, s.Delete(t.Context(), c.ID))

	assert.NoDirExists(t, path)
	info, err := os.Stat(outside)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o500), info.Mode().Perm(), "a link's target is never chmodded")
}

// A chat's entry that is a link is removed, never walked: the walk that makes
// a read-only tree removable would otherwise chmod what the link leads to.
func TestAChatEntryThatIsALinkIsNeverWalked(t *testing.T) {
	t.Parallel()

	if os.Getuid() == 0 {
		t.Skip("root removes a read-only directory's entries")
	}
	s := newTestService(t)
	other := makeChatDir(t, s, "c2")
	locked := filepath.Join(other, "locked")
	require.NoError(t, os.Mkdir(locked, 0o500))
	t.Cleanup(func() { _ = os.Chmod(locked, 0o700) })
	require.NoError(t, os.Symlink("c2", s.chatDir("c1").Path()))
	results := filepath.Dir(other)
	// Read-only, so the first removal fails and the retry runs.
	require.NoError(t, os.Chmod(results, 0o500))
	t.Cleanup(func() { _ = os.Chmod(results, 0o700) })

	s.chatDir("c1").remove()

	info, err := os.Stat(locked)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o500), info.Mode().Perm())
}

// The monitor's directory is made owner-only, and so is each cluster's entry.
func TestANewMonitorDirIsOwnerOnly(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	s := startService(t, dir)
	d := s.monitorDir("7")

	root, err := d.Root(true)
	require.NoError(t, err)
	require.NoError(t, root.Close())
	assert.Equal(t, filepath.Join(monitorDirIn(dir), "7"), d.Path())
	for _, path := range []string{monitorDirIn(dir), d.Path()} {
		info, err := os.Stat(path)
		require.NoError(t, err)
		assert.Equal(t, os.FileMode(0o700), info.Mode().Perm(), path)
	}
}
