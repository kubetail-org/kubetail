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

package write

import (
	"context"
	"crypto/sha256"
	"errors"
	"io"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/testutil"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/run/session"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/tools"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/tools/internal/fileguard"
)

// Under the sidecar's owner-only umask, a new file is 0o666 and each directory
// made for it 0o777, less the umask the tool was given.
//
// syscall.Umask is process-global: no t.Parallel() here.
func TestWriteCreatesAFileAndItsParents(t *testing.T) {
	prev := syscall.Umask(0o077)
	defer syscall.Umask(prev)
	tl, rt, _, _ := chat(t)
	root := t.TempDir()
	path := filepath.Join(root, "a", "b", "x.txt")

	_, isError := tl.Run(t.Context(), rt, call(path, "x\n"))
	require.False(t, isError)
	for p, want := range map[string]os.FileMode{
		path:                          0o644,
		filepath.Join(root, "a"):      os.ModeDir | 0o755,
		filepath.Join(root, "a", "b"): os.ModeDir | 0o755,
	} {
		info, err := os.Lstat(p)
		require.NoError(t, err)
		assert.Equal(t, want, info.Mode(), p)
	}
}

// An existing file keeps its permission bits, never setuid, and its group.
func TestWriteKeepsAnExistingModeAndGroup(t *testing.T) {
	tl, rt, st, _ := chat(t)
	for _, mode := range []os.FileMode{0o600, 0o755} {
		path := file(t, st, "a\n")
		require.NoError(t, os.Chmod(path, mode|os.ModeSetuid))
		_, isError := tl.Run(t.Context(), rt, call(path, "b\n"))
		require.False(t, isError)
		info, err := os.Lstat(path)
		require.NoError(t, err)
		assert.Equal(t, mode, info.Mode())
	}

	path := file(t, st, "a\n")
	info, err := os.Lstat(path)
	require.NoError(t, err)
	gid := int(info.Sys().(*syscall.Stat_t).Gid)
	groups, err := os.Getgroups()
	require.NoError(t, err)
	for _, g := range groups {
		if g == gid {
			continue
		}
		require.NoError(t, os.Chown(path, -1, g))
		_, isError := tl.Run(t.Context(), rt, call(path, "b\n"))
		require.False(t, isError)
		info, err := os.Lstat(path)
		require.NoError(t, err)
		assert.Equal(t, g, int(info.Sys().(*syscall.Stat_t).Gid))
		return
	}
	// fileguard's TestReplaceableTakesAGroupTheNewFileGets decides the group
	// against a stubbed list, so the case runs where this part cannot.
	t.Log("the test user is in one group; the group is checked in fileguard")
}

// A link in the last component is asked about like any path, since the gate
// reads the name alone, then refused with its target; nothing is written
// through it.
func TestWriteRefusesALinkNamingItsTarget(t *testing.T) {
	tl, rt, st, _ := chat(t)
	target := file(t, st, "a\n")
	link := filepath.Join(t.TempDir(), "l")
	require.NoError(t, os.Symlink(target, link))
	st[link] = st[target]

	assert.Equal(t, tools.Approval{}, approval(t, tl, rt, call(link, "b\n")))
	text, isError := tl.Run(t.Context(), rt, call(link, "b\n"))
	assert.True(t, isError)
	assert.Equal(t, "This is a symbolic link. Write to its target instead: "+target, text)
	assert.Equal(t, "a\n", content(t, target))
}

// A file the user cannot write, one in a directory they cannot write, and a new
// file under a directory they cannot write are refused before anything
// changes.
func TestWriteRefusesWhatItCannotWrite(t *testing.T) {
	tl, rt, st, _ := chat(t)
	ro := file(t, st, "a\n")
	require.NoError(t, os.Chmod(ro, 0o444))

	inShut := file(t, st, "a\n")
	shut := filepath.Dir(inShut)
	require.NoError(t, os.Chmod(shut, 0o500))
	t.Cleanup(func() { _ = os.Chmod(shut, 0o700) })

	for path, want := range map[string]string{
		ro:                                  "Kubetail cannot write this file.",
		inShut:                              "Kubetail cannot replace files in this directory.",
		filepath.Join(shut, "new", "x.txt"): "Kubetail cannot write in " + shut + ".",
	} {
		text, isError := tl.Run(t.Context(), rt, call(path, "b\n"))
		assert.True(t, isError, path)
		assert.Equal(t, want, text, path)
	}
	assert.Equal(t, "a\n", content(t, ro))
	assert.Equal(t, []string{"x.txt"}, names(t, shut))
}

// A file the user can write but not read is refused, since its stamp cannot be
// checked; a path whose ancestor cannot be resolved is refused, since the
// fence cannot tell.
func TestWriteRefusesWhatItCannotRead(t *testing.T) {
	tl, rt, st, _ := chat(t)
	path := file(t, st, "a\n")
	require.NoError(t, os.Chmod(path, 0o200))
	text, isError := tl.Run(t.Context(), rt, call(path, "b\n"))
	assert.True(t, isError)
	assert.Equal(t, "Kubetail cannot read this file.", text)

	locked := filepath.Join(t.TempDir(), "locked")
	require.NoError(t, os.Mkdir(locked, 0))
	t.Cleanup(func() { _ = os.Chmod(locked, 0o700) })
	text, isError = tl.Run(t.Context(), rt, call(filepath.Join(locked, "x.txt"), "b\n"))
	assert.True(t, isError)
	assert.Equal(t, "Kubetail cannot write this file.", text)
}

// The data directory reached through a link in a parent is refused on disk,
// with no directory made under it.
func TestWriteRefusesTheDataDirThroughALink(t *testing.T) {
	tl, rt, _, data := chat(t)
	link := filepath.Join(t.TempDir(), "l")
	require.NoError(t, os.Symlink(data, link))

	text, isError := tl.Run(t.Context(), rt, call(filepath.Join(link, "new", "x.txt"), "x"))
	assert.True(t, isError)
	assert.Equal(t, "Write cannot change Kubetail's directories.", text)
	assert.Empty(t, names(t, data))
}

// A link in the workspace is refused rather than followed, at the file and at a
// directory on the way, and nothing outside changes.
func TestALinkOutOfTheWorkspaceIsRefused(t *testing.T) {
	tl, rt, _, _ := chat(t)
	ws := tools.WorkspacePath(rt.Dir)
	require.NoError(t, os.MkdirAll(ws, 0o700))
	outside := t.TempDir()
	target := filepath.Join(outside, "zshrc")
	require.NoError(t, os.WriteFile(target, []byte("keep\n"), 0o600))
	require.NoError(t, os.Symlink(target, filepath.Join(ws, "notes")))
	require.NoError(t, os.Symlink(outside, filepath.Join(ws, "dir")))

	text, isError := tl.Run(t.Context(), rt, call(filepath.Join(ws, "notes"), "x"))
	assert.True(t, isError)
	assert.Equal(t, "This is a symbolic link. Write to its target instead: "+target, text)

	for _, path := range []string{filepath.Join(ws, "dir", "x.txt"), filepath.Join(ws, "dir", "a", "x.txt")} {
		_, isError = tl.Run(t.Context(), rt, call(path, "x"))
		assert.True(t, isError, path)
	}
	assert.Equal(t, []string{"zshrc"}, names(t, outside))
	b, err := os.ReadFile(target)
	require.NoError(t, err)
	assert.Equal(t, "keep\n", string(b))
}

// Every path the gate lets through unasked is written through the workspace's
// root: a link in the workspace leading out is skipped by name and then
// refused, and a plain name is written under the root.
func TestAWriteThatSkipsGoesThroughTheRoot(t *testing.T) {
	tl, rt, _, _ := chat(t)
	ws := tools.WorkspacePath(rt.Dir)
	require.NoError(t, os.MkdirAll(ws, 0o700))
	outside := t.TempDir()
	require.NoError(t, os.Symlink(outside, filepath.Join(ws, "out")))

	escape := filepath.Join(ws, "out", "x.txt")
	assert.Equal(t, tools.Approval{Skip: true}, approval(t, tl, rt, call(escape, "x")))
	_, isError := tl.Run(t.Context(), rt, call(escape, "x"))
	assert.True(t, isError)
	assert.Empty(t, names(t, outside))

	plain := filepath.Join(ws, "x.txt")
	assert.Equal(t, tools.Approval{Skip: true}, approval(t, tl, rt, call(plain, "x")))
	text, isError := tl.Run(t.Context(), rt, call(plain, "x"))
	assert.False(t, isError, text)
	assert.Equal(t, "x", content(t, plain))
}

// A replace is a rename: a reader holding the old file reads the old bytes,
// and no temporary file is left.
func TestWriteIsARename(t *testing.T) {
	tl, rt, st, _ := chat(t)
	path := file(t, st, "old\n")
	held, err := os.Open(path)
	require.NoError(t, err)
	defer held.Close()

	_, isError := tl.Run(t.Context(), rt, call(path, "new\n"))
	require.False(t, isError)
	b, err := io.ReadAll(held)
	require.NoError(t, err)
	assert.Equal(t, "old\n", string(b))
	assert.Equal(t, []string{"x.txt"}, names(t, filepath.Dir(path)))
}

// granted is a home of the test's own, with a key under a never-readable
// .ssh, notes in a closed Documents, and a code folder holding a file the
// chat has read; Write knowing both closed paths; and a runtime whose session
// grants folders.
type granted struct {
	home string
	tl   *Tool
	rt   tools.Runtime
}

func newGranted(t *testing.T, folders func(home string) []session.Folder) granted {
	t.Helper()
	home := filepath.Join(testutil.GrantableDir(t), "home")
	for rel, body := range map[string]string{".ssh/id_ed25519": "key", "Documents/notes.txt": "notes", "code/x.txt": "old"} {
		p := filepath.Join(home, rel)
		require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
		require.NoError(t, os.WriteFile(p, []byte(body), 0o600))
	}
	data := filepath.Join(t.TempDir(), "data")
	require.NoError(t, os.Mkdir(data, 0o700))
	tl, err := New(0o022, func() (never, closed []string) {
		return []string{filepath.Join(home, ".ssh")}, []string{filepath.Join(home, "Documents")}
	}, data)
	require.NoError(t, err)
	st := stamps{filepath.Join(home, "code/x.txt"): {Sum: sha256.Sum256([]byte("old")), Whole: true}}
	rt := tools.Runtime{Files: st, Dir: chatDirIn(data)}
	given := folders(home)
	rt.Session.Folders = func(context.Context) ([]session.Folder, error) { return given, nil }
	return granted{home: home, tl: tl, rt: rt}
}

func (g granted) in(rel string) string { return filepath.Join(g.home, rel) }

// write is a call writing content at path, run with approval a.
func (g granted) write(t *testing.T, path, content string, a tools.Approval) (string, bool) {
	t.Helper()
	return g.tl.RunApproved(t.Context(), g.rt, call(path, content), a)
}

func readWritten(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	require.NoError(t, err)
	return string(b)
}

func TestAWriteInAGrantedFolderAsksNoOne(t *testing.T) {
	g := newGranted(t, func(home string) []session.Folder {
		return []session.Folder{{Path: filepath.Join(home, "code"), Write: true}}
	})
	code := session.Folder{Path: g.in("code"), Write: true}
	for _, rel := range []string{"code/x.txt", "code/new.txt"} {
		a := approval(t, g.tl, g.rt, call(g.in(rel), "new"))
		assert.Equal(t, tools.Approval{Skip: true, Folder: &code}, a, rel)
		text, isError := g.write(t, g.in(rel), "new", a)
		require.False(t, isError, text)
		assert.Equal(t, "new", readWritten(t, g.in(rel)))
	}
}

func TestAWriteInAReadGrantStillAsks(t *testing.T) {
	g := newGranted(t, func(home string) []session.Folder { return []session.Folder{{Path: home}} })
	assert.Equal(t, tools.Approval{}, approval(t, g.tl, g.rt, call(g.in("code/x.txt"), "new")))
}

func TestAWriteInAGrantedFolderMakesItsParents(t *testing.T) {
	g := newGranted(t, func(home string) []session.Folder {
		return []session.Folder{{Path: filepath.Join(home, "code"), Write: true}}
	})
	path := g.in("code/a/b/c.txt")
	a := approval(t, g.tl, g.rt, call(path, "c"))
	require.NotNil(t, a.Folder)
	text, isError := g.write(t, path, "c", a)
	require.False(t, isError, text)
	assert.Equal(t, "c", readWritten(t, path))

	// A file put at the name between the approval and the write is not
	// replaced: the new file is placed without clobbering.
	taken := g.in("code/taken.txt")
	a = approval(t, g.tl, g.rt, call(taken, "mine"))
	require.NoError(t, os.WriteFile(taken, []byte("theirs"), 0o644))
	g.tl.write = func(ctx context.Context, rt tools.Runtime, path string, folder *session.Folder, content []byte) (bool, error) {
		file, err := g.tl.fence.Walk(*folder, path)
		require.NoError(t, err)
		defer file.Close()
		return true, g.tl.create(ctx, file, content)
	}
	_, isError = g.write(t, taken, "mine", a)
	assert.True(t, isError)
	assert.Equal(t, "theirs", readWritten(t, taken))
}

func TestAGrantedWriteStaysInTheFolder(t *testing.T) {
	g := newGranted(t, func(home string) []session.Folder {
		return []session.Folder{{Path: filepath.Join(home, "code"), Write: true}}
	})
	code := session.Folder{Path: g.in("code"), Write: true}
	outside := filepath.Join(filepath.Dir(g.home), "outside")
	require.NoError(t, os.MkdirAll(outside, 0o755))
	require.NoError(t, os.Symlink(outside, g.in("code/out")))
	require.NoError(t, os.Symlink("../.ssh", g.in("code/ssh")))

	for _, rel := range []string{"code/out/x.txt", "code/ssh/id_ed25519", "code/ssh/new"} {
		text, isError := g.write(t, g.in(rel), "planted", tools.Approval{Skip: true, Folder: &code})
		assert.True(t, isError, rel)
		assert.Equal(t, refusal(fileguard.ErrLeaves), text, rel)
	}
	entries, err := os.ReadDir(outside)
	require.NoError(t, err)
	assert.Empty(t, entries, "nothing outside changes")
	assert.Equal(t, "key", readWritten(t, g.in(".ssh/id_ed25519")))
}

func TestARevokeBetweenApprovalAndRunChangesNothing(t *testing.T) {
	g := newGranted(t, func(home string) []session.Folder {
		return []session.Folder{{Path: filepath.Join(home, "code"), Write: true}}
	})
	a := approval(t, g.tl, g.rt, call(g.in("code/x.txt"), "new"))
	require.NotNil(t, a.Folder)
	g.rt.Session.Folders = func(context.Context) ([]session.Folder, error) { panic("the run read the folders again") }
	text, isError := g.write(t, g.in("code/x.txt"), "new", a)
	require.False(t, isError, text)
	assert.Equal(t, "new", readWritten(t, g.in("code/x.txt")))
}

func TestAGrantedCallAnswersAtItsEnd(t *testing.T) {
	g := newGranted(t, func(home string) []session.Folder {
		return []session.Folder{{Path: filepath.Join(home, "code"), Write: true}}
	})
	a := approval(t, g.tl, g.rt, call(g.in("code/y.txt"), "y"))
	real := g.tl.write
	release := make(chan struct{})
	returned := make(chan struct{})
	g.tl.write = func(ctx context.Context, rt tools.Runtime, path string, folder *session.Folder, content []byte) (bool, error) {
		defer close(returned)
		<-release
		return real(ctx, rt, path, folder, content)
	}
	ctx, cancel := context.WithCancel(t.Context())
	ran := make(chan struct{})
	var text string
	go func() {
		defer close(ran)
		text, _ = g.tl.RunApproved(ctx, g.rt, call(g.in("code/y.txt"), "y"), a)
	}()
	cancel()
	testutil.Wait(t, ran, "the cancel's answer")
	assert.Equal(t, cancelled, text)
	close(release)
	testutil.Wait(t, returned, "the walk")
	assert.NoFileExists(t, g.in("code/y.txt"))
}

// A read of the granted folders that fails fails the call.
func TestAFoldersReadThatFailsFailsTheCall(t *testing.T) {
	g := newGranted(t, func(string) []session.Folder { return nil })
	unread := errors.New("folders unread")
	g.rt.Session.Folders = func(context.Context) ([]session.Folder, error) { return nil, unread }
	_, err := g.tl.Approval(t.Context(), g.rt, call(g.in("code/x.txt"), "new"))
	var refused *tools.Refusal
	require.ErrorAs(t, err, &refused)
	assert.Equal(t, "Kubetail could not read the folders granted. Try again.", refused.Result)
}
