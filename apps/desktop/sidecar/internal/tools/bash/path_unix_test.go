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
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/appdb/appdbtest"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/testutil"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/run/sandbox"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/settings"
)

// pathFixture is a tool over a fake sandbox whose System reads open, and
// whose Never denies never; home is read by no rule.
type pathFixture struct {
	tl                      *Tool
	fake                    *fakeSandboxer
	base, open, home, never string
}

func newPathFixture(t *testing.T) *pathFixture {
	t.Helper()
	// Not under /tmp, which a typed folder's check refuses on Linux.
	base := testutil.GrantableDir(t)
	f := &pathFixture{tl: tool(t), base: base}
	f.open, f.home, f.never = f.dir(t, "open"), f.dir(t, "home"), f.dir(t, "never")
	f.fake = &fakeSandboxer{
		system: sandbox.System{Files: sandbox.FilePolicy{Read: []string{"/usr", "/bin", f.open}}},
		never:  []string{f.never},
	}
	f.tl.sandboxer = f.fake
	return f
}

// dir makes a folder under the fixture's base.
func (f *pathFixture) dir(t *testing.T, rel string) string {
	t.Helper()
	dir := filepath.Join(f.base, rel)
	require.NoError(t, os.MkdirAll(dir, 0o755))
	return dir
}

func shell(path string) settings.PathFolder {
	return settings.PathFolder{Path: path, Origin: settings.OriginShell}
}

func typed(path string) settings.PathFolder {
	return settings.PathFolder{Path: path, Origin: settings.OriginTyped}
}

// searching is a PATH list of folders, each also a NoWrite folder.
func searching(folders ...settings.PathFolder) func(context.Context) (settings.RunPath, error) {
	list := settings.RunPath{Search: folders}
	for _, f := range folders {
		list.NoWrite = append(list.NoWrite, f.Path)
	}
	return func(context.Context) (settings.RunPath, error) { return list, nil }
}

// runPathOf runs echo "$PATH" sandboxed and answers it and the run.
func (f *pathFixture) runPathOf(t *testing.T) (string, sandbox.Run) {
	t.Helper()
	text, isError := f.tl.Run(t.Context(), testRuntime(t), command(`echo "$PATH"`))
	require.False(t, isError, text)
	runs := f.fake.seen()
	require.NotEmpty(t, runs)
	return strings.TrimSuffix(text, "\n"), runs[len(runs)-1]
}

func TestTheRunFreezesThePath(t *testing.T) {
	t.Parallel()

	f := newPathFixture(t)
	inOpen, inHome := f.dir(t, "open/bin"), f.dir(t, "home/bin")
	folders := []settings.PathFolder{shell(inOpen), typed(inHome)}
	f.tl.pathList = func(context.Context) (settings.RunPath, error) {
		return settings.RunPath{Search: folders}, nil
	}
	// A setting changed while the run is built changes nothing it finds.
	release := make(chan struct{})
	close(release)
	f.fake.onSystem, f.fake.holdSys = func() { folders = nil }, release

	path, r := f.runPathOf(t)
	assert.Equal(t, inOpen+":"+inHome, path)
	assert.Equal(t, []string{"/usr", "/bin", f.open, inHome}, r.Policy.Files.Read, "a Read rule for each folder System does not open")
	assert.Contains(t, r.Env, "PATH="+inOpen+":"+inHome)

	// A setting with no folder finds nothing: no default stands in for it.
	f.fake.onSystem = nil
	path, r = f.runPathOf(t)
	assert.Equal(t, emptyPath, path)
	assert.Equal(t, []string{"/usr", "/bin", f.open}, r.Policy.Files.Read)
}

// A setting that cannot be read, or that Kubetail cannot use, starts no run.
func TestARunWhosePathCannotBeReadDoesNotStart(t *testing.T) {
	t.Parallel()

	f := newPathFixture(t)
	for err, says := range map[error]string{
		errors.New("disk I/O error"):                              "could not read your PATH setting",
		settings.PathRefusal("Your PATH setting cannot be used."): "Your PATH setting cannot be used.",
	} {
		f.tl.pathList = func(context.Context) (settings.RunPath, error) { return settings.RunPath{}, err }
		text, isError := f.tl.Run(t.Context(), testRuntime(t), command(`echo hi`))
		assert.True(t, isError)
		assert.Contains(t, text, says)
		assert.NotContains(t, text, "disk I/O", "the reason is logged, never shown")
	}
	assert.Empty(t, f.fake.seen(), "nothing ran")
}

func TestTheRunLeavesOutAFolderNoRuleOpens(t *testing.T) {
	// CaptureLogs swaps the process's default logger, so this test stays serial.
	f := newPathFixture(t)
	inNever := f.dir(t, "never/bin")
	world := f.dir(t, "open/world")
	require.NoError(t, os.Chmod(world, 0o777))
	byShell, byUser := f.dir(t, "home/shell"), f.dir(t, "home/user")
	kept := f.dir(t, "open/kept")
	f.tl.pathList = searching(typed(inNever), typed(world), shell(byShell), typed(byUser), shell(kept))
	log := testutil.CaptureLogs(t)

	path, r := f.runPathOf(t)
	assert.Equal(t, byUser+":"+kept, path)
	require.NoError(t, r.Policy.Check(), "no Read rule lies inside an Always path")
	for _, dir := range []string{inNever, world, byShell} {
		assert.Contains(t, log.String(), dir)
	}
}

// A typed folder that is the home, or holds it, is left out of a run: its
// Read rule would open all of the home.
func TestTheRunLeavesOutABroadFolder(t *testing.T) {
	t.Parallel()

	f := newPathFixture(t)
	f.tl.home = f.home
	inHome := f.dir(t, "home/bin")
	f.tl.pathList = searching(typed(f.home), typed(f.base), typed(inHome))

	path, r := f.runPathOf(t)
	assert.Equal(t, inHome, path)
	assert.Equal(t, []string{"/usr", "/bin", f.open, inHome}, r.Policy.Files.Read)
}

// A shell folder that leads into an open folder is searched there; once it
// leads to a closed one, it is left out.
func TestTheRunFollowsALinkIntoAnOpenFolder(t *testing.T) {
	t.Parallel()

	f := newPathFixture(t)
	second := f.dir(t, "open/v2")
	link := filepath.Join(f.base, "current")
	require.NoError(t, os.Symlink(second, link))
	f.tl.pathList = searching(shell(link))

	path, _ := f.runPathOf(t)
	assert.Equal(t, second, path)

	require.NoError(t, os.Remove(link))
	require.NoError(t, os.Symlink(f.dir(t, "home/v3"), link))
	path, _ = f.runPathOf(t)
	assert.Equal(t, emptyPath, path)
}

// Over the real service: a closed folder of the shell's PATH is not searched
// until the user types it into the setting.
func TestAClosedShellFolderIsFoundOnceTyped(t *testing.T) {
	t.Parallel()

	f := newPathFixture(t)
	scripts := f.dir(t, "home/scripts")
	require.NoError(t, os.WriteFile(filepath.Join(scripts, "hello"), []byte("#!/bin/sh\necho hi\n"), 0o755))
	store, err := settings.Open(appdbtest.Open(t))
	require.NoError(t, err)
	svc := settings.NewService(store, func() settings.Zones {
		return settings.Zones{Never: f.fake.never, Open: f.fake.system.Files}
	}, nil, nil, "")
	_, err = svc.SetShellPath(t.Context(), []string{scripts})
	require.NoError(t, err)
	f.tl.pathList = svc.RunPath

	text, isError := f.tl.Run(t.Context(), testRuntime(t), command("command -v hello"))
	assert.True(t, isError, text)

	require.NoError(t, svc.SetPath(t.Context(), "$PATH:"+scripts))
	text, isError = f.tl.Run(t.Context(), testRuntime(t), command("command -v hello"))
	require.False(t, isError, text)
	assert.Equal(t, filepath.Join(scripts, "hello")+"\n", text)
}

// A typed folder inside a closed folder is searched, its Read rule opening it
// alone; the closed folder itself, as a grant of it, is left out.
func TestATypedFolderInsideAClosedOneIsOpened(t *testing.T) {
	t.Parallel()

	f := newPathFixture(t)
	closed := f.dir(t, "open/Downloads")
	inside := f.dir(t, "open/Downloads/bin")
	f.fake.system.Files.Deny = []string{closed}
	f.tl.pathList = searching(typed(closed), typed(inside))

	path, r := f.runPathOf(t)
	assert.Equal(t, inside, path)
	assert.Contains(t, r.Policy.Files.Read, inside)
	assert.Contains(t, r.Policy.Files.Deny, closed)
}
