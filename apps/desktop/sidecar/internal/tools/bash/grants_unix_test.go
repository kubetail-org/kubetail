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

package bash

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/realpath"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/testutil"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/run/sandbox"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/run/session"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/settings"
)

// grantedRun runs one command through a fake sandbox with folders granted,
// and answers the policy it was handed.
func grantedRun(t *testing.T, tl *Tool, folders []session.Folder) sandbox.Policy {
	t.Helper()
	fake := &fakeSandboxer{}
	tl.sandboxer = fake
	rt := testRuntime(t)
	if folders != nil {
		rt.Session.Folders = func(context.Context) ([]session.Folder, error) { return folders, nil }
	}
	text, isError := tl.Run(t.Context(), rt, command("true"))
	require.False(t, isError, text)
	require.Len(t, fake.seen(), 1)
	return fake.seen()[0].Policy
}

// folders makes each of rels under a fresh grantable directory, and answers
// the directory and their paths.
func folders(t *testing.T, rels ...string) (string, []string) {
	t.Helper()
	base := testutil.GrantableDir(t)
	var paths []string
	for _, rel := range rels {
		p := filepath.Join(base, rel)
		require.NoError(t, os.MkdirAll(p, 0o755))
		paths = append(paths, p)
	}
	return base, paths
}

func TestAGrantIsAFilesRule(t *testing.T) {
	t.Parallel()

	tl := tool(t)
	_, p := folders(t, "code", "svc")
	without := grantedRun(t, tool(t), nil)

	got := grantedRun(t, tl, []session.Folder{{Path: p[0]}, {Path: p[1], Write: true}})
	assert.Equal(t, append(without.Files.Read, p[0]), got.Files.Read, "a read grant is a Read rule")
	assert.Equal(t, append(without.Files.Write, p[1]), got.Files.Write, "a read-write grant a Write rule")
	assert.Equal(t, without.Files.Deny, got.Files.Deny)
	assert.Equal(t, without.Always.Deny, got.Always.Deny, "the Always part is unchanged")
	assert.Equal(t, without.Always.Kubetail, got.Always.Kubetail)
	assert.NoError(t, got.Check())
}

func TestASessionWithNoFoldersGetsNone(t *testing.T) {
	t.Parallel()

	got := grantedRun(t, tool(t), nil)
	assert.Empty(t, got.Files.Write)
	assert.Equal(t, grantedRun(t, tool(t), []session.Folder{}).Files, got.Files)
}

func TestNestedGrantsResolveToTheWider(t *testing.T) {
	t.Parallel()

	_, p := folders(t, "code", "code/svc", "code/svc/deep", "other")
	code, svc, deep, other := p[0], p[1], p[2], p[3]
	got := grantedRun(t, tool(t), []session.Folder{
		{Path: other}, {Path: other, Write: true}, // read for the chat, read-write always
		{Path: code, Write: true}, {Path: svc}, // a folder under a read-write one
		{Path: deep, Write: true},
	})
	assert.Equal(t, []string{other, code}, got.Files.Write, "the wider grant wins, and nothing beneath a Write rule")
	assert.NotContains(t, got.Files.Read, other)
	assert.NotContains(t, got.Files.Read, svc)
	assert.NoError(t, got.Check())

	got = grantedRun(t, tool(t), []session.Folder{{Path: code}, {Path: svc, Write: true}})
	assert.Contains(t, got.Files.Read, code)
	assert.Equal(t, []string{svc}, got.Files.Write, "a read-write folder under a read one stays: the deeper rule wins")
	assert.NoError(t, got.Check())
}

func TestAGrantThatFailsTheCheckIsLeftOut(t *testing.T) {
	// CaptureLogs swaps the process's default logger, so this test stays serial.
	tl := tool(t)
	base, p := folders(t, "ssh", "code/svc", "code/home")
	ssh, svc, homeLink := p[0], p[1], p[2]
	never := []string{ssh}
	logs := testutil.CaptureLogs(t)
	fake := &fakeSandboxer{never: never}
	tl.sandboxer = fake

	require.NoError(t, os.RemoveAll(svc))
	require.NoError(t, os.Symlink(ssh, svc))
	require.NoError(t, os.RemoveAll(homeLink))
	require.NoError(t, os.Symlink(base, homeLink))
	rt := testRuntime(t)
	rt.Session.Folders = func(context.Context) ([]session.Folder, error) {
		return []session.Folder{{Path: svc}, {Path: homeLink}}, nil
	}
	text, isError := tl.Run(t.Context(), rt, command("true"))
	require.False(t, isError, "the run starts: %s", text)
	require.Len(t, fake.seen(), 1)
	got := fake.seen()[0].Policy
	assert.NotContains(t, got.Files.Read, svc)
	assert.NotContains(t, got.Files.Read, homeLink)
	assert.NotContains(t, got.Files.Read, ssh)
	assert.NotContains(t, got.Files.Read, base)
	assert.Contains(t, logs.String(), "This folder has moved or become a link. Grant it again.")
}

// grantedHome is a home of the test's own, with a startup file, a key, a
// kubeconfig, a closed folder holding a project, and Kubetail's data
// directory inside it, and tl, a confiningTool, set to run under it.
func grantedHome(t *testing.T, tl *Tool) (home, data string) {
	t.Helper()
	home = testutil.GrantableDir(t)
	tl.home = home
	data = filepath.Join(home, ".local", "share", "kubetail")
	tl.denied = append(tl.denied, data)
	for rel, body := range map[string]string{
		".zshrc": "zsh", ".ssh/id_ed25519": "key", ".kube/config": "kubeconfig",
		"Documents/notes.txt": "notes", "Documents/project/a.txt": "project", ".local/share/kubetail/app.db": "db",
	} {
		p := filepath.Join(home, rel)
		require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
		require.NoError(t, os.WriteFile(p, []byte(body), 0o644))
	}
	return home, data
}

// runIn runs line in the real sandbox with folders granted.
func runIn(t *testing.T, tl *Tool, line string, folders ...session.Folder) (string, bool) {
	t.Helper()
	rt := testRuntime(t)
	rt.Session.Folders = func(context.Context) ([]session.Folder, error) { return folders, nil }
	return tl.Run(t.Context(), rt, command(line))
}

// The note's first invariant, over a grant: a granted home opens, and the
// denied-always list and Kubetail's directories stay shut inside it.
func TestADeniedAlwaysPathStaysHiddenUnderAGrantedHome(t *testing.T) {
	t.Parallel()

	tl := confiningTool(t)
	home, data := grantedHome(t, tl)
	granted := session.Folder{Path: home}

	text, isError := runIn(t, tl, "cat "+filepath.Join(home, ".zshrc"), granted)
	require.False(t, isError, text)
	assert.Equal(t, "zsh", text)
	for _, p := range []string{filepath.Join(home, ".ssh", "id_ed25519"), filepath.Join(data, "app.db"), filepath.Join(home, "Documents", "notes.txt")} {
		text, isError = runIn(t, tl, "cat "+p, granted)
		assert.True(t, isError, "%s: %s", p, text)
	}
	for _, dir := range []string{".kube", "Documents"} {
		text, _ = runIn(t, tl, "ls "+filepath.Join(home, dir), granted)
		assert.NotContains(t, text, "config", dir)
		assert.NotContains(t, text, "notes.txt", dir)
	}

	project := session.Folder{Path: filepath.Join(home, "Documents", "project")}
	text, isError = runIn(t, tl, "cat "+filepath.Join(project.Path, "a.txt"), project)
	require.False(t, isError, text)
	assert.Equal(t, "project", text, "a grant inside a closed folder opens that folder")
	text, isError = runIn(t, tl, "cat "+filepath.Join(home, "Documents", "notes.txt"), project)
	assert.True(t, isError, "and nothing else of it: %s", text)
}

func TestAReadWriteGrantIsWritten(t *testing.T) {
	t.Parallel()

	tl := confiningTool(t)
	home, _ := grantedHome(t, tl)
	rw, ro := filepath.Join(home, "rw"), filepath.Join(home, "ro")
	require.NoError(t, os.MkdirAll(rw, 0o755))
	require.NoError(t, os.MkdirAll(ro, 0o755))

	text, isError := runIn(t, tl, "echo hi > "+filepath.Join(rw, "x"), session.Folder{Path: rw, Write: true})
	require.False(t, isError, text)
	b, err := os.ReadFile(filepath.Join(rw, "x"))
	require.NoError(t, err)
	assert.Equal(t, "hi\n", string(b))

	text, isError = runIn(t, tl, "echo hi > "+filepath.Join(ro, "x"), session.Folder{Path: ro})
	assert.True(t, isError, text)
	assert.NoFileExists(t, filepath.Join(ro, "x"), "a read grant is not written")
}

// A run carries the folders holding the links Kubetail followed, and a
// read-write grant of one is left out of it: a command that can write one
// could repoint the link.
func TestAReadWriteGrantOfALinkParentIsLeftOut(t *testing.T) {
	t.Parallel()

	tl := tool(t)
	_, p := folders(t, "cache", "big/kubetail")
	cache, target := p[0], p[1]
	link := filepath.Join(cache, "kubetail")
	require.NoError(t, os.Symlink(target, link))
	tl.links = []realpath.Link{{Name: link, Parent: cache}}

	got := grantedRun(t, tl, []session.Folder{{Path: cache, Write: true}})

	assert.Equal(t, []string{cache}, got.Always.LinkParents)
	assert.NotContains(t, got.Files.Write, cache)
	assert.NoError(t, got.Check())
}

// A read of the granted folders that fails fails the run before anything
// starts: it never runs with no grant.
func TestAFoldersReadThatFailsFailsTheRun(t *testing.T) {
	t.Parallel()

	tl := tool(t)
	fake := &fakeSandboxer{}
	tl.sandboxer = fake
	rt := testRuntime(t)
	rt.Session.Folders = func(context.Context) ([]session.Folder, error) { return nil, errors.New("folders unread") }
	text, isError := tl.Run(t.Context(), rt, command("true"))
	assert.True(t, isError)
	assert.Contains(t, text, "Kubetail could not read the folders granted.")
	assert.Empty(t, fake.seen())
}

// A run reads its PATH once: a typed folder the user removes after the run's
// start is still one its grant check refuses a write grant on, as the run
// still searches it.
func TestARunKeepsItsNoWriteAfterTheSettingChanges(t *testing.T) {
	t.Parallel()

	tl := tool(t)
	_, p := folders(t, "tools")
	reads := 0
	tl.pathList = func(context.Context) (settings.RunPath, error) {
		reads++
		if reads > 1 {
			return settings.RunPath{}, nil
		}
		return settings.RunPath{Search: []settings.PathFolder{{Path: p[0], Origin: settings.OriginTyped}}, NoWrite: p}, nil
	}

	got := grantedRun(t, tl, []session.Folder{{Path: p[0], Write: true}})
	assert.Equal(t, 1, reads, "one read per run")
	assert.NotContains(t, got.Files.Write, p[0], "the run's own PATH refuses the write grant")
	assert.Contains(t, got.Files.Read, p[0], "and searches the folder")
}
