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

package settings

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/appdb/appdbtest"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/realpath"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/testutil"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/run/sandbox"
)

// grantFixture is a home in miniature: a never-readable .ssh, a toolchain
// folder, a closed Documents, and a startup file, beside a system folder
// every run reads.
type grantFixture struct {
	base, home, system string
	zones              Zones
}

func newGrantFixture(t *testing.T) *grantFixture {
	t.Helper()
	base := testutil.GrantableDir(t)
	f := &grantFixture{base: base, home: filepath.Join(base, "home"), system: filepath.Join(base, "usr")}
	for _, d := range []string{"home/.ssh", "home/.local/bin", "home/Documents/project", "home/code/svc", "home/Library/Keychains", "usr/local/bin"} {
		require.NoError(t, os.MkdirAll(filepath.Join(base, d), 0o755))
	}
	require.NoError(t, os.WriteFile(filepath.Join(f.home, ".zshrc"), nil, 0o644))
	f.zones = Zones{
		Never:   []string{f.in(".ssh"), f.in("Library/Keychains")},
		Open:    sandbox.FilePolicy{Read: []string{f.system, f.in(".local/bin")}, Deny: []string{f.in("Documents")}},
		Home:    f.home,
		NoWrite: []string{f.in(".zshrc"), f.in(".config/fish")},
	}
	return f
}

// in is rel under the fixture's home.
func (f *grantFixture) in(rel string) string { return filepath.Join(f.home, rel) }

// ruleOf is the check that refused a grant, "" when none did.
func ruleOf(t *testing.T, err error) string {
	t.Helper()
	if err == nil {
		return ""
	}
	var r FolderRefusal
	require.ErrorAs(t, err, &r)
	assert.NotEmpty(t, r.Reason)
	return r.Rule
}

func TestCheckFolderRefusesEachRule(t *testing.T) {
	f := newGrantFixture(t)
	require.NoError(t, os.Symlink(f.in(".ssh"), f.in("alias")))
	for _, c := range []struct {
		name, path string
		write      bool
		rule       string
	}{
		{"relative", "home/code", false, "absolute"},
		{"a tilde", "~/code", false, "absolute"},
		{"not clean", f.home + "/code/../code", false, "absolute"},
		{"missing", f.in("nowhere"), false, "exists"},
		{"a file", f.in(".zshrc"), false, "exists"},
		{"the root", "/", false, "root"},
		{"a link into .ssh", f.in("alias"), false, "link"},
		{"never readable", f.in(".ssh"), false, "never"},
		{"inside a never path", f.in("Library/Keychains"), false, "never"},
		{"a closed folder", f.in("Documents"), false, "closed"},
		{"a toolchain folder read-write", f.in(".local/bin"), true, "tool"},
		{"a folder over a toolchain folder read-write", f.in(".local"), true, "tool"},
		{"under a system read path read-write", filepath.Join(f.system, "local/bin"), true, "tool"},
		{"over a never path read-write", f.in("Library"), true, "over-never"},
		{"the home read-write", f.home, true, "home"},
	} {
		assert.Equal(t, c.rule, ruleOf(t, CheckFolder(c.path, c.write, f.zones, nil)), c.name)
	}
	for _, c := range []struct {
		name, path string
		write      bool
	}{
		{"the home read-only", f.home, false},
		{"a folder", f.in("code/svc"), true},
		{"a toolchain folder read-only", f.in(".local/bin"), false},
		{"a folder over a never path read-only", f.in("Library"), false},
		{"inside a closed folder read-write", f.in("Documents/project"), true},
	} {
		assert.NoError(t, CheckFolder(c.path, c.write, f.zones, nil), c.name)
	}
}

func TestANeverPathThatIsALinkIsCaughtAtItsTarget(t *testing.T) {
	f := newGrantFixture(t)
	dotfiles := f.in("dotfiles")
	require.NoError(t, os.MkdirAll(filepath.Join(dotfiles, "ssh"), 0o755))
	require.NoError(t, os.RemoveAll(f.in(".ssh")))
	require.NoError(t, os.Symlink(filepath.Join(dotfiles, "ssh"), f.in(".ssh")))

	assert.Equal(t, "never", ruleOf(t, CheckFolder(filepath.Join(dotfiles, "ssh"), false, f.zones, nil)))
	assert.Equal(t, "over-never", ruleOf(t, CheckFolder(dotfiles, true, f.zones, nil)))
	assert.NoError(t, CheckFolder(dotfiles, false, f.zones, nil))
}

func TestAReadWriteGrantOverAClosedFolderIsRefused(t *testing.T) {
	f := newGrantFixture(t)
	// So neither the home's own rule nor its toolchain folder is what answers.
	f.zones.Home = f.in("elsewhere")
	f.zones.Open.Read = []string{f.system}
	assert.Equal(t, "over-closed", ruleOf(t, CheckFolder(f.home, true, f.zones, nil)))
}

func TestALinkIsRefusedWithItsTarget(t *testing.T) {
	f := newGrantFixture(t)
	require.NoError(t, os.Symlink(f.home, f.in("code/home")))
	require.NoError(t, os.Symlink(f.in("code"), f.in("src")))
	for path, target := range map[string]string{
		f.in("code/home"): f.home,
		f.in("src/svc"):   f.in("code/svc"),
	} {
		err := CheckFolder(path, false, f.zones, nil)
		var r FolderRefusal
		require.ErrorAs(t, err, &r, path)
		assert.Equal(t, "link", r.Rule)
		assert.Equal(t, target, r.Target)
		assert.Equal(t, path+" is a link to "+target+"; grant "+target+" instead.", r.Reason)
		assert.NoError(t, CheckFolder(target, false, f.zones, nil), "the resolved path itself is accepted")
	}
}

func TestAMovedGrantIsRefused(t *testing.T) {
	f := newGrantFixture(t)
	svc := f.in("code/svc")
	require.NoError(t, CheckStoredFolder(svc, false, f.zones, nil))

	require.NoError(t, os.RemoveAll(svc))
	require.NoError(t, os.Symlink(f.home, svc))
	err := CheckStoredFolder(svc, false, f.zones, nil)
	assert.Equal(t, "moved", ruleOf(t, err))
	assert.Equal(t, "This folder has moved or become a link. Grant it again.", err.Error())
	assert.NoError(t, CheckStoredFolder(f.home, false, f.zones, nil), "though the home itself would pass as a read grant")

	code := f.in("code")
	require.NoError(t, os.Rename(code, f.in("code-old")))
	require.NoError(t, os.MkdirAll(f.in("elsewhere/svc"), 0o755))
	require.NoError(t, os.Symlink(f.in("elsewhere"), code))
	assert.Equal(t, "moved", ruleOf(t, CheckStoredFolder(svc, false, f.zones, nil)), "a parent replaced by a link")
}

func TestAFixedMountCannotBeGranted(t *testing.T) {
	f := newGrantFixture(t)
	for _, p := range []string{"/proc", "/proc/1/root", "/dev", "/dev/pts"} {
		for _, write := range []bool{false, true} {
			err := CheckFolder(p, write, f.zones, nil)
			require.Error(t, err, p)
			if sandbox.FixedMount(p) {
				assert.Equal(t, "fixed", ruleOf(t, err), p)
			}
		}
	}
}

// A PATH entry is the folder the shell searches. The kernel follows a link
// before a .., so the entry names the parent of the link's target: a write
// grant there is refused, and one on the folder beside the link is not.
func TestAPathEntryThroughALinkAndDotDotIsTheFolderSearched(t *testing.T) {
	f := newGrantFixture(t)
	for _, d := range []string{"opt/tool/v2", "opt/tool/bin", "work/bin"} {
		require.NoError(t, os.MkdirAll(f.in(d), 0o755))
	}
	require.NoError(t, os.Symlink(f.in("opt/tool/v2"), f.in("work/current")))
	entries := []string{f.in("work/current") + "/../bin"}

	assert.Equal(t, "tool", ruleOf(t, CheckFolder(f.in("opt/tool/bin"), true, f.zones, entries)))
	assert.NoError(t, CheckFolder(f.in("work/bin"), true, f.zones, entries))
}

// A zone or PATH folder that cannot be followed holds nothing, so it shuts no
// other folder.
func TestAPathThatCannotBeFollowedShutsNoOtherFolder(t *testing.T) {
	f := newGrantFixture(t)
	require.NoError(t, os.MkdirAll(f.in("work"), 0o755))
	loop := f.in("loop")
	require.NoError(t, os.Symlink(loop, loop))
	bin := filepath.Join(loop, "bin")
	z := f.zones
	z.Never = append(slices.Clone(z.Never), bin)
	z.NoWrite = append(slices.Clone(z.NoWrite), bin)

	assert.NoError(t, CheckFolder(f.in("work"), true, z, []string{bin}))
	assert.NoError(t, CheckStoredFolder(f.in("work"), true, z, []string{bin, "relative"}))
}

func TestAWriteGrantOnCodeThatRunsIsRefused(t *testing.T) {
	f := newGrantFixture(t)
	pathNoWrite := []string{f.in("tools/bin")}
	for _, d := range append(pathNoWrite, f.in(".config/fish"), f.in("dotfiles")) {
		require.NoError(t, os.MkdirAll(d, 0o755))
	}
	require.NoError(t, os.Remove(f.in(".zshrc")))
	require.NoError(t, os.WriteFile(f.in("dotfiles/zshrc"), nil, 0o644))
	require.NoError(t, os.Symlink(f.in("dotfiles/zshrc"), f.in(".zshrc")))

	for name, c := range map[string]struct {
		path, rule string
	}{
		"a PATH folder":                {pathNoWrite[0], "tool"},
		"over a PATH folder":           {f.in("tools"), "tool"},
		"under a system read path":     {filepath.Join(f.system, "local/bin"), "tool"},
		"a NoWrite folder":             {f.in(".config/fish"), "code"},
		"over a NoWrite folder":        {f.in(".config"), "code"},
		"a startup file's target repo": {f.in("dotfiles"), "code"},
	} {
		assert.Equal(t, c.rule, ruleOf(t, CheckFolder(c.path, true, f.zones, pathNoWrite)), name)
		assert.NoError(t, CheckFolder(c.path, false, f.zones, pathNoWrite), "%s, read-only", name)
	}
}

func TestNoSnapshotRefusesEveryFolder(t *testing.T) {
	f := newGrantFixture(t)
	store := newStore(t)
	s := NewService(store, nil, nil, nil, "")
	for _, write := range []bool{false, true} {
		assert.Equal(t, "no-sandbox", ruleOf(t, s.CheckFolder(t.Context(), f.in("code/svc"), write)))
		assert.Equal(t, "no-sandbox", ruleOf(t, s.CheckStoredFolder(t.Context(), f.in("code/svc"), write)))
	}
	never, closed := s.Hidden()
	assert.Empty(t, never)
	assert.Empty(t, closed)
	assert.Empty(t, s.NeverReadable(t.Context()))
	assert.Empty(t, s.ClosedFolders(t.Context()))
	assert.Empty(t, s.WideFolders(t.Context()))
}

func TestTheZonesAreASnapshot(t *testing.T) {
	f := newGrantFixture(t)
	filtered := f.in("proj/node_modules/.bin")
	require.NoError(t, os.MkdirAll(filtered, 0o755))
	store := newStore(t)
	var calls atomic.Int32
	s := NewService(store, func() Zones {
		calls.Add(1)
		return f.zones
	}, func(context.Context) ([]string, error) { return []string{filtered}, nil }, nil, "")

	_, err := s.SetShellPath(t.Context(), []string{filtered})
	require.NoError(t, err)
	assert.Equal(t, int32(1), calls.Load())
	for range 3 {
		assert.NoError(t, s.CheckFolder(t.Context(), f.in("code/svc"), true))
	}
	assert.Equal(t, "tool", ruleOf(t, s.CheckFolder(t.Context(), filtered, true)),
		"a login PATH entry no run searches is still one something outside the sandbox searches")
	assert.Equal(t, int32(1), calls.Load(), "the checks read the snapshot, never the zones")
	never, closed := s.Hidden()
	assert.Equal(t, []string{f.in(".ssh"), f.in("Library/Keychains")}, never)
	assert.Equal(t, []string{f.in("Documents")}, closed)

	require.NoError(t, s.RefreshPath(t.Context()))
	assert.Equal(t, int32(2), calls.Load(), "a refresh takes the snapshot again")

	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	s.checkFolder = func(string, bool, zones, []string, bool) error {
		<-release
		return nil
	}
	s.diskTimeout = time.Millisecond
	err = s.CheckStoredFolder(t.Context(), f.in("code/svc"), false)
	assert.Equal(t, "timeout", ruleOf(t, err))
	assert.EqualError(t, err, "Kubetail could not check this folder in time.")
}

func TestAFolderIsRefusedWhenTheSnapshotRunsPastTheBound(t *testing.T) {
	store := newStore(t)
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	s := NewService(store, func() Zones {
		<-release
		return Zones{}
	}, nil, nil, "")
	s.diskTimeout = time.Millisecond

	err := s.CheckFolder(t.Context(), "/srv/code", false)
	assert.Equal(t, "timeout", ruleOf(t, err))
}

func TestTheSnapshotIsTakenOnFirstUse(t *testing.T) {
	f := newGrantFixture(t)
	store := newStore(t)
	var calls atomic.Int32
	s := NewService(store, func() Zones {
		calls.Add(1)
		return f.zones
	}, nil, nil, "")
	never, _ := s.Hidden()
	assert.Empty(t, never, "Hidden reads the snapshot and never takes one")

	assert.Equal(t, "home", ruleOf(t, s.CheckFolder(t.Context(), f.home, true)))
	assert.NoError(t, s.CheckFolder(t.Context(), f.home, false))
	assert.Equal(t, int32(1), calls.Load())
	assert.Equal(t, []string{f.in(".ssh"), f.in("Library/Keychains")}, s.NeverReadable(t.Context()))
}

func TestAClosedFolderOpensOnlyAFolderInsideIt(t *testing.T) {
	f := newGrantFixture(t)
	store := newStore(t)
	s := NewService(store, func() Zones { return f.zones }, nil, nil, "")
	assert.Equal(t, []string{f.in("Documents")}, s.ClosedFolders(t.Context()))
	assert.Equal(t, "closed", ruleOf(t, s.CheckFolder(t.Context(), f.in("Documents"), false)))
	assert.NoError(t, s.CheckFolder(t.Context(), f.in("Documents/project"), false))
}

func TestWideIsTheHomeAndEachFolderOverIt(t *testing.T) {
	f := newGrantFixture(t)
	store := newStore(t)
	s := NewService(store, func() Zones { return f.zones }, nil, nil, "")
	wide := s.WideFolders(t.Context())
	assert.Equal(t, f.home, wide[0])
	assert.Contains(t, wide, filepath.Dir(f.home))
	assert.NotContains(t, wide, "/", "the root is refused, never warned of")
	assert.NotContains(t, wide, f.in("code"))
	for _, w := range wide[1:] {
		if w != "/Volumes" {
			assert.True(t, realpath.Under(f.home, w), w)
		}
	}
}

// A never path that is a link is caught at the target it has now, whatever
// the snapshot saw: the check resolves the zones again each time.
func TestACheckResolvesTheZonesNow(t *testing.T) {
	f := newGrantFixture(t)
	for _, dir := range []string{"dotfiles/ssh-a", "dotfiles/ssh-b"} {
		require.NoError(t, os.MkdirAll(f.in(dir), 0o700))
	}
	require.NoError(t, os.RemoveAll(f.in(".ssh")))
	require.NoError(t, os.Symlink(f.in("dotfiles/ssh-a"), f.in(".ssh")))
	store := newStore(t)
	s := NewService(store, func() Zones { return f.zones }, nil, nil, "")
	assert.Equal(t, "never", ruleOf(t, s.CheckFolder(t.Context(), f.in("dotfiles/ssh-a"), false)))
	assert.NoError(t, s.CheckFolder(t.Context(), f.in("dotfiles/ssh-b"), false))

	require.NoError(t, os.Remove(f.in(".ssh")))
	require.NoError(t, os.Symlink(f.in("dotfiles/ssh-b"), f.in(".ssh")))
	assert.Equal(t, "never", ruleOf(t, s.CheckFolder(t.Context(), f.in("dotfiles/ssh-b"), false)), "the new target")
	assert.NoError(t, s.CheckFolder(t.Context(), f.in("dotfiles/ssh-a"), false), "the old one is a folder like any other")
	never, _ := s.Hidden()
	assert.Equal(t, f.zones.Never, never, "Hidden is the list as the zones name it")
}

// Naming a folder lists nothing, so one under a folder that can be searched
// and not listed is granted.
func TestAFolderUnderOneThatCannotBeListedIsGranted(t *testing.T) {
	f := newGrantFixture(t)
	svc := f.in("locked/svc")
	require.NoError(t, os.MkdirAll(svc, 0o755))
	require.NoError(t, os.Chmod(f.in("locked"), 0o311))
	t.Cleanup(func() { os.Chmod(f.in("locked"), 0o755) })

	assert.NoError(t, CheckFolder(svc, false, f.zones, nil))
	assert.NoError(t, CheckStoredFolder(svc, true, f.zones, nil))
}

// A command that can write the folder holding a link Kubetail followed could
// repoint it, so that folder, and any above it, is granted read-only alone.
func TestAWriteGrantOnOrAboveALinkParentIsRefused(t *testing.T) {
	f := newGrantFixture(t)
	parent := f.in("big/cache")
	require.NoError(t, os.MkdirAll(parent, 0o755))
	link := filepath.Join(parent, "kubetail")
	require.NoError(t, os.Symlink(f.in("code/svc"), link))
	f.zones.Links = []realpath.Link{{Name: link, Parent: parent}}

	err := CheckFolder(parent, true, f.zones, nil)
	assert.Equal(t, "link-parent", ruleOf(t, err))
	assert.Equal(t, "Kubetail follows the link "+link+", and a command that can write "+parent+" could repoint it. Grant it read-only.", err.Error())
	assert.Equal(t, "link-parent", ruleOf(t, CheckFolder(f.in("big"), true, f.zones, nil)), "a folder above one")
	assert.Equal(t, "link-parent", ruleOf(t, CheckStoredFolder(parent, true, f.zones, nil)), "a stored grant")
	assert.NoError(t, CheckFolder(parent, false, f.zones, nil), "read-only")
	assert.NoError(t, CheckFolder(f.in("code"), true, f.zones, nil), "a folder beside it")
}

// No read-write grant reaches a folder of the PATH: every folder of the
// shell's, whatever the setting says, and each typed one.
func TestAWriteGrantOnAShellFolderIsRefusedWithoutPath(t *testing.T) {
	f := newGrantFixture(t)
	shell := f.in("tools/shell")
	require.NoError(t, os.MkdirAll(shell, 0o755))
	store := newStore(t)
	s := NewService(store, func() Zones { return f.zones }, nil, nil, "")
	_, err := s.SetShellPath(t.Context(), []string{shell})
	require.NoError(t, err)
	require.NoError(t, s.SetPath(t.Context(), "~/code"))

	assert.Equal(t, "tool", ruleOf(t, s.CheckFolder(t.Context(), shell, true)), "the user's terminal still runs it")
	assert.NoError(t, s.CheckFolder(t.Context(), shell, false))
}

func TestAWriteGrantOnATypedPathFolderIsRefused(t *testing.T) {
	f := newGrantFixture(t)
	store := newStore(t)
	s := NewService(store, func() Zones { return f.zones }, nil, nil, "")
	require.NoError(t, s.SetPath(t.Context(), "$PATH:~/code"))

	assert.Equal(t, "tool", ruleOf(t, s.CheckFolder(t.Context(), f.in("code/svc"), true)))
	assert.Equal(t, "tool", ruleOf(t, s.CheckStoredFolder(t.Context(), f.in("code"), true)))
}

// A read of the PATH that fails is an error, never a refusal on the folder:
// whoever asked decides what it costs.
func TestAFolderCheckReadThatFailsIsAnError(t *testing.T) {
	f := newGrantFixture(t)
	db := appdbtest.Open(t)
	store := openStore(t, db)
	s := NewService(store, func() Zones { return f.zones }, nil, nil, "")
	exec(t, db, `DROP TABLE shell_path`)

	err := s.CheckStoredFolder(t.Context(), f.in("code/svc"), true)
	require.Error(t, err)
	assert.False(t, errors.As(err, new(FolderRefusal)), "not a refusal")
}
