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

package edit

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

// An edited file keeps its permission bits, never setuid, and its group.
func TestEditKeepsTheModeAndGroup(t *testing.T) {
	tl, rt, st, _ := chat(t)
	for _, mode := range []os.FileMode{0o600, 0o755} {
		path := file(t, st, "a\n")
		require.NoError(t, os.Chmod(path, mode|os.ModeSetuid))
		_, isError := tl.Run(t.Context(), rt, call(path, "a", "b"))
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
		_, isError := tl.Run(t.Context(), rt, call(path, "a", "b"))
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
// reads the name alone, then refused with its target; nothing is changed
// through it.
func TestEditRefusesALinkNamingItsTarget(t *testing.T) {
	tl, rt, st, _ := chat(t)
	target := file(t, st, "a\n")
	link := filepath.Join(t.TempDir(), "l")
	require.NoError(t, os.Symlink(target, link))
	st[link] = st[target]

	assert.Equal(t, tools.Approval{}, approval(t, tl, rt, call(link, "a", "b")))
	text, isError := tl.Run(t.Context(), rt, call(link, "a", "b"))
	assert.True(t, isError)
	assert.Equal(t, "This is a symbolic link. Edit its target instead: "+target, text)
	assert.Equal(t, "a\n", content(t, target))
}

// A file the user cannot write, and one in a directory they cannot write, are
// refused before anything changes.
func TestEditRefusesWhatItCannotWrite(t *testing.T) {
	tl, rt, st, _ := chat(t)
	ro := file(t, st, "a\n")
	require.NoError(t, os.Chmod(ro, 0o444))

	inShut := file(t, st, "a\n")
	shut := filepath.Dir(inShut)
	require.NoError(t, os.Chmod(shut, 0o500))
	t.Cleanup(func() { _ = os.Chmod(shut, 0o700) })

	for path, want := range map[string]string{
		ro:     "Kubetail cannot write this file.",
		inShut: "Kubetail cannot replace files in this directory.",
	} {
		text, isError := tl.Run(t.Context(), rt, call(path, "a", "b"))
		assert.True(t, isError, path)
		assert.Equal(t, want, text, path)
		assert.Equal(t, "a\n", content(t, path), path)
	}
	assert.Equal(t, []string{"x.txt"}, names(t, shut))
}

// A file the user can write but not read is refused, since its stamp cannot be
// checked; a path whose ancestor cannot be resolved is refused, since the
// fence cannot tell.
func TestEditRefusesWhatItCannotRead(t *testing.T) {
	tl, rt, st, _ := chat(t)
	path := file(t, st, "a\n")
	require.NoError(t, os.Chmod(path, 0o200))
	text, isError := tl.Run(t.Context(), rt, call(path, "a", "b"))
	assert.True(t, isError)
	assert.Equal(t, "Kubetail cannot read this file.", text)

	locked := filepath.Join(t.TempDir(), "locked")
	require.NoError(t, os.Mkdir(locked, 0))
	t.Cleanup(func() { _ = os.Chmod(locked, 0o700) })
	text, isError = tl.Run(t.Context(), rt, call(filepath.Join(locked, "x.txt"), "a", "b"))
	assert.True(t, isError)
	assert.Equal(t, "Kubetail cannot write this file.", text)
}

// The data directory reached through a link in a parent is refused on disk.
func TestEditRefusesTheDataDirThroughALink(t *testing.T) {
	tl, rt, st, data := chat(t)
	inData := filepath.Join(data, "host.json")
	require.NoError(t, os.WriteFile(inData, []byte("a\n"), 0o600))
	link := filepath.Join(t.TempDir(), "l")
	require.NoError(t, os.Symlink(data, link))
	through := filepath.Join(link, "host.json")
	st[through] = st[file(t, st, "a\n")]

	text, isError := tl.Run(t.Context(), rt, call(through, "a", "b"))
	assert.True(t, isError)
	assert.Equal(t, "Edit cannot change Kubetail's directories.", text)
	assert.Equal(t, "a\n", content(t, inData))
}

// A link in the workspace is refused rather than followed, at the file and at a
// directory on the way, and nothing outside changes.
func TestALinkOutOfTheWorkspaceIsRefused(t *testing.T) {
	tl, rt, st, _ := chat(t)
	ws := tools.WorkspacePath(rt.Dir)
	require.NoError(t, os.MkdirAll(ws, 0o700))
	outside := t.TempDir()
	target := filepath.Join(outside, "zshrc")
	require.NoError(t, os.WriteFile(target, []byte("keep\n"), 0o600))
	require.NoError(t, os.Symlink(target, filepath.Join(ws, "notes")))
	require.NoError(t, os.Symlink(outside, filepath.Join(ws, "dir")))
	for _, path := range []string{filepath.Join(ws, "notes"), filepath.Join(ws, "dir", "zshrc")} {
		st[path] = tools.Stamp{Sum: sha256.Sum256([]byte("keep\n")), Whole: true}
	}

	text, isError := tl.Run(t.Context(), rt, call(filepath.Join(ws, "notes"), "keep", "x"))
	assert.True(t, isError)
	assert.Equal(t, "This is a symbolic link. Edit its target instead: "+target, text)
	_, isError = tl.Run(t.Context(), rt, call(filepath.Join(ws, "dir", "zshrc"), "keep", "x"))
	assert.True(t, isError)

	b, err := os.ReadFile(target)
	require.NoError(t, err)
	assert.Equal(t, "keep\n", string(b))
}

// Every path the gate lets through unasked is changed through the workspace's
// root: a link in the workspace leading out is skipped by name and then
// refused, and a plain name is changed under the root.
func TestAWriteThatSkipsGoesThroughTheRoot(t *testing.T) {
	tl, rt, st, _ := chat(t)
	ws := tools.WorkspacePath(rt.Dir)
	require.NoError(t, os.MkdirAll(ws, 0o700))
	outside := t.TempDir()
	target := filepath.Join(outside, "x.txt")
	require.NoError(t, os.WriteFile(target, []byte("keep\n"), 0o600))
	require.NoError(t, os.Symlink(outside, filepath.Join(ws, "out")))
	escape := filepath.Join(ws, "out", "x.txt")
	plain := filepath.Join(ws, "x.txt")
	require.NoError(t, os.WriteFile(plain, []byte("keep\n"), 0o600))
	for _, path := range []string{escape, plain} {
		st[path] = tools.Stamp{Sum: sha256.Sum256([]byte("keep\n")), Whole: true}
	}

	assert.Equal(t, tools.Approval{Skip: true}, approval(t, tl, rt, call(escape, "keep", "x")))
	_, isError := tl.Run(t.Context(), rt, call(escape, "keep", "x"))
	assert.True(t, isError)
	assert.Equal(t, "keep\n", content(t, target))

	assert.Equal(t, tools.Approval{Skip: true}, approval(t, tl, rt, call(plain, "keep", "x")))
	text, isError := tl.Run(t.Context(), rt, call(plain, "keep", "x"))
	assert.False(t, isError, text)
	assert.Equal(t, "x\n", content(t, plain))
}

// An edit is a rename: a reader holding the old file reads the old bytes, and
// no temporary file is left.
func TestEditIsARename(t *testing.T) {
	tl, rt, st, _ := chat(t)
	path := file(t, st, "old\n")
	held, err := os.Open(path)
	require.NoError(t, err)
	defer held.Close()

	_, isError := tl.Run(t.Context(), rt, call(path, "old", "new"))
	require.False(t, isError)
	b, err := io.ReadAll(held)
	require.NoError(t, err)
	assert.Equal(t, "old\n", string(b))
	assert.Equal(t, []string{"x.txt"}, names(t, filepath.Dir(path)))
}

// granted is a home of the test's own, with a key under a never-readable
// .ssh, notes in a closed Documents, and a code folder holding a file the
// chat has read whole; Edit knowing both closed paths; and a runtime whose
// session grants folders.
type granted struct {
	home string
	tl   *Tool
	rt   tools.Runtime
}

func newGranted(t *testing.T, folders func(home string) []session.Folder) granted {
	t.Helper()
	home := filepath.Join(testutil.GrantableDir(t), "home")
	for rel, body := range map[string]string{".ssh/config": "Host a", "Documents/notes.txt": "a note", "code/x.txt": "old a"} {
		p := filepath.Join(home, rel)
		require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
		require.NoError(t, os.WriteFile(p, []byte(body), 0o600))
	}
	data := filepath.Join(t.TempDir(), "data")
	require.NoError(t, os.Mkdir(data, 0o700))
	tl, err := New(func() (never, closed []string) {
		return []string{filepath.Join(home, ".ssh")}, []string{filepath.Join(home, "Documents")}
	}, data)
	require.NoError(t, err)
	st := stamps{}
	for rel, body := range map[string]string{".ssh/config": "Host a", "Documents/notes.txt": "a note", "code/x.txt": "old a"} {
		st[filepath.Join(home, rel)] = tools.Stamp{Sum: sha256.Sum256([]byte(body)), Whole: true}
	}
	rt := tools.Runtime{Files: st, Dir: chatDirIn(data)}
	given := folders(home)
	rt.Session.Folders = func(context.Context) ([]session.Folder, error) { return given, nil }
	return granted{home: home, tl: tl, rt: rt}
}

func (g granted) in(rel string) string { return filepath.Join(g.home, rel) }

func (g granted) edit(t *testing.T, path string, a tools.Approval) (string, bool) {
	t.Helper()
	return g.tl.RunApproved(t.Context(), g.rt, call(path, "a", "b"), a)
}

func readEdited(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	require.NoError(t, err)
	return string(b)
}

func TestAnEditInAGrantedFolderAsksNoOne(t *testing.T) {
	g := newGranted(t, func(home string) []session.Folder {
		return []session.Folder{{Path: filepath.Join(home, "code"), Write: true}}
	})
	code := session.Folder{Path: g.in("code"), Write: true}
	a := approval(t, g.tl, g.rt, call(g.in("code/x.txt"), "a", "b"))
	assert.Equal(t, tools.Approval{Skip: true, Folder: &code}, a)
	text, isError := g.edit(t, g.in("code/x.txt"), a)
	require.False(t, isError, text)
	assert.Equal(t, "old b", readEdited(t, g.in("code/x.txt")))
}

func TestAnEditInAReadGrantStillAsks(t *testing.T) {
	g := newGranted(t, func(home string) []session.Folder { return []session.Folder{{Path: home}} })
	assert.Equal(t, tools.Approval{}, approval(t, g.tl, g.rt, call(g.in("code/x.txt"), "a", "b")))
}

func TestAHiddenPathUnderAGrantedFolderStillAsks(t *testing.T) {
	g := newGranted(t, func(home string) []session.Folder { return []session.Folder{{Path: home, Write: true}} })
	for _, rel := range []string{".ssh/config", "Documents/notes.txt"} {
		a := approval(t, g.tl, g.rt, call(g.in(rel), "a", "b"))
		assert.Equal(t, tools.Approval{}, a, rel)
		text, isError := g.edit(t, g.in(rel), a)
		require.False(t, isError, "%s, approved, is edited as today: %s", rel, text)
	}
}

func TestAnEditThroughALinkToAHiddenPathIsRefused(t *testing.T) {
	g := newGranted(t, func(home string) []session.Folder {
		return []session.Folder{{Path: filepath.Join(home, "code"), Write: true}}
	})
	code := session.Folder{Path: g.in("code"), Write: true}
	require.NoError(t, os.Symlink("../.ssh", g.in("code/ssh")))
	text, isError := g.edit(t, g.in("code/ssh/config"), tools.Approval{Skip: true, Folder: &code})
	assert.True(t, isError)
	assert.Equal(t, refusal(fileguard.ErrLeaves), text)
	assert.Equal(t, "Host a", readEdited(t, g.in(".ssh/config")))
}

func TestARevokeBetweenApprovalAndRunChangesNothing(t *testing.T) {
	g := newGranted(t, func(home string) []session.Folder {
		return []session.Folder{{Path: filepath.Join(home, "code"), Write: true}}
	})
	a := approval(t, g.tl, g.rt, call(g.in("code/x.txt"), "a", "b"))
	require.NotNil(t, a.Folder)
	g.rt.Session.Folders = func(context.Context) ([]session.Folder, error) { panic("the run read the folders again") }
	text, isError := g.edit(t, g.in("code/x.txt"), a)
	require.False(t, isError, text)
	assert.Equal(t, "old b", readEdited(t, g.in("code/x.txt")))
}

func TestAGrantedCallAnswersAtItsEnd(t *testing.T) {
	g := newGranted(t, func(home string) []session.Folder {
		return []session.Folder{{Path: filepath.Join(home, "code"), Write: true}}
	})
	a := approval(t, g.tl, g.rt, call(g.in("code/x.txt"), "a", "b"))
	real := g.tl.edit
	release := make(chan struct{})
	returned := make(chan struct{})
	g.tl.edit = func(ctx context.Context, rt tools.Runtime, path string, folder *session.Folder, in input) (edited, error) {
		defer close(returned)
		<-release
		return real(ctx, rt, path, folder, in)
	}
	ctx, cancel := context.WithCancel(t.Context())
	ran := make(chan struct{})
	var text string
	go func() {
		defer close(ran)
		text, _ = g.tl.RunApproved(ctx, g.rt, call(g.in("code/x.txt"), "a", "b"), a)
	}()
	cancel()
	testutil.Wait(t, ran, "the cancel's answer")
	assert.Equal(t, cancelled, text)
	close(release)
	testutil.Wait(t, returned, "the walk")
	assert.Equal(t, "old a", readEdited(t, g.in("code/x.txt")))
}

// A read of the granted folders that fails fails the call.
func TestAFoldersReadThatFailsFailsTheCall(t *testing.T) {
	g := newGranted(t, func(string) []session.Folder { return nil })
	unread := errors.New("folders unread")
	g.rt.Session.Folders = func(context.Context) ([]session.Folder, error) { return nil, unread }
	_, err := g.tl.Approval(t.Context(), g.rt, call(g.in("code/x.txt"), "old", "new"))
	var refused *tools.Refusal
	require.ErrorAs(t, err, &refused)
	assert.Equal(t, "Kubetail could not read the folders granted. Try again.", refused.Result)
}
