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

package read

import (
	"context"
	"errors"
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

// A FIFO in the directory is refused at once, with no writer: opening one for
// reading would otherwise block past any cancel.
func TestReadRefusesAFIFOAtOnce(t *testing.T) {
	dir, tl, rt := chat(t)
	fifo := filepath.Join(dir, "fifo")
	require.NoError(t, syscall.Mkfifo(fifo, 0o600))

	var text string
	var isError bool
	testutil.WaitReturn(t, func() { text, isError = tl.Run(t.Context(), rt, call(fifo)) }, "the FIFO's refusal")
	assert.True(t, isError)
	assert.Equal(t, notHere, text)
}

// The data directory is refused however it is reached: a link elsewhere into
// it, or a link to one of its files, is notHere, and the refusal names no
// target.
func TestReadRefusesTheDataDirThroughALink(t *testing.T) {
	dir, tl, rt := chat(t)
	data := filepath.Dir(filepath.Dir(dir))
	require.NoError(t, os.WriteFile(filepath.Join(data, "app.db"), []byte("rows"), 0o600))
	elsewhere := t.TempDir()
	require.NoError(t, os.Symlink(data, filepath.Join(elsewhere, "d")))
	require.NoError(t, os.Symlink(filepath.Join(data, "app.db"), filepath.Join(elsewhere, "db")))

	for _, path := range []string{
		filepath.Join(elsewhere, "d", "app.db"),
		filepath.Join(elsewhere, "db"),
		filepath.Join(elsewhere, "d", "chats", "c2", "x.txt"),
	} {
		assert.Equal(t, tools.Approval{}, approval(t, tl, rt, path), "by name it is elsewhere, so it is asked")
		text, isError := tl.Run(t.Context(), rt, call(path))
		assert.True(t, isError, path)
		assert.Equal(t, notHere, text, path)
	}
}

// The refusals only Unix can stage: a link names its target, a FIFO is not a
// regular file, and a file the sidecar cannot read, or whose place it cannot
// tell, is one sentence either way.
func TestReadRefusalsSayWhatIsWrongOnUnix(t *testing.T) {
	_, tl, rt := chat(t)
	dir, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "x.yaml"), []byte("x"), 0o600))
	require.NoError(t, os.Symlink("x.yaml", filepath.Join(dir, "link")))
	require.NoError(t, syscall.Mkfifo(filepath.Join(dir, "fifo"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "locked.txt"), []byte("x"), 0))
	require.NoError(t, os.Mkdir(filepath.Join(dir, "shut"), 0))
	t.Cleanup(func() { _ = os.Chmod(filepath.Join(dir, "shut"), 0o700) })

	for path, want := range map[string]string{
		filepath.Join(dir, "link"):              "This is a symbolic link. Read its target instead: " + filepath.Join(dir, "x.yaml"),
		filepath.Join(dir, "fifo"):              "This is not a regular file.",
		filepath.Join(dir, "locked.txt"):        "Kubetail cannot read this file.",
		filepath.Join(dir, "shut", "inner.txt"): "Kubetail cannot read this file.",
	} {
		text, isError := tl.Run(t.Context(), rt, call(path))
		assert.True(t, isError, path)
		assert.Equal(t, want, text, path)
	}
}

// A link in the directory that leads out of it is refused at the open.
func TestReadRefusesALinkOut(t *testing.T) {
	dir, tl, rt := chat(t)
	target := filepath.Join(t.TempDir(), "secret.txt")
	require.NoError(t, os.WriteFile(target, []byte("secret"), 0o600))
	require.NoError(t, os.Symlink(target, filepath.Join(dir, "link.txt")))

	text, isError := tl.Run(t.Context(), rt, call(filepath.Join(dir, "link.txt")))
	assert.True(t, isError)
	assert.Equal(t, notHere, text)
}

// granted is a home of the test's own, with a key under a never-readable
// .ssh, notes in a closed Documents and a project inside it, and code; Read
// knowing both closed paths; and a runtime whose session grants folders.
type granted struct {
	home string
	tl   *Tool
	rt   tools.Runtime
}

func newGranted(t *testing.T) granted {
	t.Helper()
	home := filepath.Join(testutil.GrantableDir(t), "home")
	for rel, body := range map[string]string{
		".ssh/id_ed25519": "key", ".ssh/config": "Host x", "Documents/notes.txt": "notes",
		"Documents/project/a.txt": "project", "code/x.txt": "code",
	} {
		p := filepath.Join(home, rel)
		require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
		require.NoError(t, os.WriteFile(p, []byte(body), 0o600))
	}
	data := filepath.Join(t.TempDir(), "data")
	dir := filepath.Join(data, "chats", "c1")
	require.NoError(t, os.MkdirAll(dir, 0o700))
	tl, err := New(func() (never, closed []string) {
		return []string{filepath.Join(home, ".ssh")}, []string{filepath.Join(home, "Documents")}
	}, data)
	require.NoError(t, err)
	return granted{home: home, tl: tl, rt: tools.Runtime{Dir: chatDir(dir), Files: stamps{}}}
}

func (g granted) in(rel string) string { return filepath.Join(g.home, rel) }

// read is a call of path run with approval a.
func (g granted) read(t *testing.T, path string, a tools.Approval) (string, bool) {
	t.Helper()
	return g.tl.RunApproved(t.Context(), g.rt, call(path), a)
}

func TestAReadInAGrantedFolderAsksNoOne(t *testing.T) {
	g := newGranted(t)
	code := session.Folder{Path: g.in("code")}
	g = newGrantedAt(t, g, code)

	a := approval(t, g.tl, g.rt, g.in("code/x.txt"))
	assert.Equal(t, tools.Approval{Skip: true, Folder: &code}, a)
	text, isError := g.read(t, g.in("code/x.txt"), a)
	require.False(t, isError, text)
	assert.Contains(t, text, "code")
	assert.Equal(t, tools.Approval{}, approval(t, g.tl, g.rt, g.in("Documents/project/a.txt")), "a path under no grant asks")
}

func TestAMissingFileInAGrantedFolderIsRefused(t *testing.T) {
	g := newGranted(t)
	code := session.Folder{Path: g.in("code")}
	g = newGrantedAt(t, g, code)
	text, isError := g.read(t, g.in("code/missing.txt"), tools.Approval{Skip: true, Folder: &code})
	assert.True(t, isError)
	assert.Equal(t, refusal(fileguard.ErrMissing), text)
}

// newGrantedAt is g with its session granting folders.
func newGrantedAt(t *testing.T, g granted, folders ...session.Folder) granted {
	t.Helper()
	g.rt.Session.Folders = func(context.Context) ([]session.Folder, error) { return folders, nil }
	return g
}

func TestARevokeBetweenApprovalAndRunChangesNothing(t *testing.T) {
	g := newGranted(t)
	home := session.Folder{Path: g.home}
	g = newGrantedAt(t, g, home)
	require.NoError(t, os.Symlink(g.in(".ssh/id_ed25519"), g.in("code/key")))
	a := approval(t, g.tl, g.rt, g.in("code/x.txt"))
	require.Equal(t, &home, a.Folder)

	for name, folders := range map[string]func(context.Context) ([]session.Folder, error){
		"emptied":         func(context.Context) ([]session.Folder, error) { return nil, nil },
		"failing to read": func(context.Context) ([]session.Folder, error) { panic("the run read the folders again") },
	} {
		g.rt.Session.Folders = folders
		text, isError := g.read(t, g.in("code/x.txt"), a)
		require.False(t, isError, "%s: %s", name, text)
		assert.Contains(t, text, "code", name)
		text, isError = g.read(t, g.in("code/key"), a)
		assert.True(t, isError, name)
		assert.Equal(t, refusal(fileguard.ErrHidden), text, "%s: through the walk of the folder the approval named, never by path", name)
	}
}

func TestAHiddenPathUnderAGrantedHomeStillAsks(t *testing.T) {
	g := newGranted(t)
	g = newGrantedAt(t, g, session.Folder{Path: g.home})
	for path, want := range map[string]string{g.in(".ssh/config"): "Host x", g.in("Documents/notes.txt"): "notes"} {
		a := approval(t, g.tl, g.rt, path)
		assert.Equal(t, tools.Approval{}, a, "%s is not skipped", path)
		text, isError := g.read(t, path, a)
		require.False(t, isError, "%s, approved, is read as today: %s", path, text)
		assert.Contains(t, text, want)
	}

	project := session.Folder{Path: g.in("Documents/project")}
	g = newGrantedAt(t, g, project)
	assert.Equal(t, tools.Approval{Skip: true, Folder: &project}, approval(t, g.tl, g.rt, g.in("Documents/project/a.txt")))
}

func TestALinkToAHiddenPathUnderAGrantedHomeIsRefused(t *testing.T) {
	g := newGranted(t)
	home := session.Folder{Path: g.home}
	g = newGrantedAt(t, g, home)
	require.NoError(t, os.MkdirAll(g.in(".ssh/keys"), 0o700))
	require.NoError(t, os.WriteFile(g.in(".ssh/keys/k"), []byte("key"), 0o600))
	require.NoError(t, os.Symlink(".ssh", g.in("alias")))
	require.NoError(t, os.Symlink("alias", g.in("deep")))
	require.NoError(t, os.Symlink(".ssh/keys", g.in("sub")))
	require.NoError(t, os.Symlink("code/x.txt", g.in("beside")))

	skipped := tools.Approval{Skip: true, Folder: &home}
	for _, path := range []string{g.in("alias/id_ed25519"), g.in("deep/id_ed25519"), g.in("sub/k")} {
		assert.Equal(t, tools.Approval{}, approval(t, g.tl, g.rt, path), "%s asks", path)
		text, isError := g.read(t, path, skipped)
		assert.True(t, isError, path)
		assert.Equal(t, refusal(fileguard.ErrHidden), text, path)
	}
	a := approval(t, g.tl, g.rt, g.in("beside"))
	assert.Equal(t, skipped, a)
	text, isError := g.read(t, g.in("beside"), a)
	require.False(t, isError, text)
	assert.Contains(t, text, "code", "a link to a file beside them is followed")
}

func TestAWalkThroughAClosedFolderIsRefused(t *testing.T) {
	g := newGranted(t)
	home := session.Folder{Path: g.home}
	g = newGrantedAt(t, g, home)
	require.NoError(t, os.Symlink("Documents/notes.txt", g.in("notes")))
	text, isError := g.read(t, g.in("notes"), tools.Approval{Skip: true, Folder: &home})
	assert.True(t, isError)
	assert.Equal(t, refusal(fileguard.ErrHidden), text)
}

func TestAGrantedCallAnswersAtItsEnd(t *testing.T) {
	g := newGranted(t)
	code := session.Folder{Path: g.in("code")}
	g = newGrantedAt(t, g, code)
	real := g.tl.fetchGranted
	release := make(chan struct{})
	returned := make(chan struct{})
	g.tl.fetchGranted = func(folder session.Folder, path string) ([]byte, error) {
		defer close(returned)
		<-release
		return real(folder, path)
	}
	ctx, cancel := context.WithCancel(t.Context())
	ran := make(chan struct{})
	var text string
	go func() {
		defer close(ran)
		text, _ = g.tl.RunApproved(ctx, g.rt, call(g.in("code/x.txt")), tools.Approval{Skip: true, Folder: &code})
	}()
	cancel()
	testutil.Wait(t, ran, "the cancel's answer")
	assert.Equal(t, cancelled, text)
	close(release)
	testutil.Wait(t, returned, "the walk")
}

// A read of the granted folders that fails fails the call: it never reads as
// no grant, which would ask about a path the user granted.
func TestAFoldersReadThatFailsFailsTheCall(t *testing.T) {
	g := newGranted(t)
	unread := errors.New("folders unread")
	g.rt.Session.Folders = func(context.Context) ([]session.Folder, error) { return nil, unread }
	_, err := g.tl.Approval(t.Context(), g.rt, call(g.in("code/x.txt")))
	var refused *tools.Refusal
	require.ErrorAs(t, err, &refused)
	assert.Equal(t, "Kubetail could not read the folders granted. Try again.", refused.Result)
}
