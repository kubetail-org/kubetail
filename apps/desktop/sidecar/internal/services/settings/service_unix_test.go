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
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/appdb/appdbtest"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/testutil"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/run/sandbox"
)

// zoneFixture is a machine in miniature: open is what every run reads, with
// a closed folder inside it, home is outside it, and never is denied always.
type zoneFixture struct {
	base, open, closed, home, never string
	zones                           Zones
}

func newZones(t *testing.T) *zoneFixture {
	t.Helper()
	// Not under /tmp, which a typed folder's check refuses on Linux as a
	// mount every run has its own of.
	base := testutil.GrantableDir(t)
	z := &zoneFixture{
		base:   base,
		open:   dirWithMode(t, base, "open", 0o755),
		closed: dirWithMode(t, base, "open/closed", 0o755),
		home:   dirWithMode(t, base, "home", 0o755),
		never:  dirWithMode(t, base, "never", 0o755),
	}
	z.zones = Zones{Never: []string{z.never}, Open: sandbox.FilePolicy{Read: []string{z.open}, Deny: []string{z.closed}}}
	return z
}

// dir makes a folder under the fixture's base and answers its path.
func (z *zoneFixture) dir(t *testing.T, rel string) string {
	t.Helper()
	return dirWithMode(t, z.base, rel, 0o755)
}

// newTestService is a Service over a fresh app.db, judging by z.
func newTestService(t *testing.T, z *zoneFixture, resolve func(context.Context) ([]string, error)) *Service {
	t.Helper()
	store := newStore(t)
	return NewService(store, func() Zones { return z.zones }, resolve, nil, "")
}

// shellDirs is the shell_path row, split; nil with none.
func shellDirs(t *testing.T, s *Service) []string {
	t.Helper()
	var dirs []string
	require.NoError(t, s.stmts.InTx(t.Context(), func(st stmts) error {
		var err error
		dirs, _, err = selectShellPath(t.Context(), st)
		return err
	}))
	return dirs
}

// A read of the shell is stored as it answered, and says whether it changed
// the row.
func TestSetShellPathAnswersWhetherItChanged(t *testing.T) {
	z := newZones(t)
	s := newTestService(t, z, nil)
	a, b := z.dir(t, "open/a"), z.dir(t, "home/b")

	changed, err := s.SetShellPath(t.Context(), []string{a, b, "bin"})
	require.NoError(t, err)
	assert.True(t, changed)
	assert.Equal(t, []string{a, b, "bin"}, shellDirs(t, s), "stored unfiltered, as the shell answered")

	changed, err = s.SetShellPath(t.Context(), []string{a, b, "bin"})
	require.NoError(t, err)
	assert.False(t, changed)

	_, err = s.SetShellPath(t.Context(), nil)
	assert.ErrorIs(t, err, ErrEmptyShellPath)
	assert.Equal(t, []string{a, b, "bin"}, shellDirs(t, s))
}

// Each read of the shell retakes the zones, so a folder closed since reaches
// the folder checks, and pings the watchers, though the PATH is the same.
func TestARefreshRetakesTheZones(t *testing.T) {
	z := newZones(t)
	s := newTestService(t, z, nil)
	dir := z.dir(t, "open/bin")
	rx := s.ListenForSandboxPathChanges()
	t.Cleanup(rx.Close)

	_, err := s.SetShellPath(t.Context(), []string{dir})
	require.NoError(t, err)
	assert.Equal(t, []string{z.closed}, s.ClosedFolders(t.Context()))
	_, err = rx.TryRecv()
	require.NoError(t, err)

	later := z.dir(t, "open/later")
	z.zones.Open.Deny = append(z.zones.Open.Deny, later)
	changed, err := s.SetShellPath(t.Context(), []string{dir})
	require.NoError(t, err)
	assert.False(t, changed)
	assert.Equal(t, []string{z.closed, later}, s.ClosedFolders(t.Context()))
	_, err = rx.TryRecv()
	assert.NoError(t, err, "an unchanged PATH still pings")
}

// Reads of the shell run one at a time, so an older one never lands after a
// newer one.
func TestShellPathWritesRunOneAtATime(t *testing.T) {
	z := newZones(t)
	s := newTestService(t, z, nil)
	first, second := z.dir(t, "open/first"), z.dir(t, "open/second")
	// Each write retakes the zones before it writes; the first holds there
	// until released.
	reading := testutil.NewProbe[struct{}](2)
	gate := make(chan struct{})
	release := sync.OnceFunc(func() { close(gate) })
	t.Cleanup(release)
	var once sync.Once
	zones := s.zones
	s.zones = func() Zones {
		reading.Fire(struct{}{})
		once.Do(func() { <-gate })
		return zones()
	}

	firstDone, secondDone := make(chan error, 1), make(chan error, 1)
	go func() { _, err := s.SetShellPath(t.Context(), []string{first}); firstDone <- err }()
	reading.Await(t, "the first write's read")
	go func() { _, err := s.SetShellPath(t.Context(), []string{second}); secondDone <- err }()
	// A negative assertion: the second reads nothing while the first holds.
	testutil.NoRecv(t, reading.Chan(), 20*time.Millisecond, "the second write's read before the first wrote")
	release()

	require.NoError(t, testutil.Recv(t, firstDone, "the first write"))
	require.NoError(t, testutil.Recv(t, secondDone, "the second write"))
	assert.Equal(t, []string{second}, shellDirs(t, s))
}

// A write waiting on the one before it gives up at its bound, and one whose
// zones run past it writes nothing.
func TestAShellPathWriteIsBounded(t *testing.T) {
	z := newZones(t)
	s := newTestService(t, z, nil)
	dir := z.dir(t, "open/bin")
	s.diskTimeout = time.Millisecond

	s.writing <- struct{}{}
	testutil.WaitReturn(t, func() {
		_, err := s.SetShellPath(t.Context(), []string{dir})
		assert.ErrorIs(t, err, context.DeadlineExceeded)
	}, "the waiting write")
	<-s.writing

	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	s.zones = func() Zones {
		<-release
		return z.zones
	}
	_, err := s.SetShellPath(t.Context(), []string{dir})
	assert.ErrorIs(t, err, context.DeadlineExceeded)
	assert.Nil(t, shellDirs(t, s), "nothing written")
}

// A tracked tool whose first open folder moves is logged once, after the
// write, and a probe that hangs never holds it.
func TestAMovedToolIsLogged(t *testing.T) {
	z := newZones(t)
	s := newTestService(t, z, nil)
	kept, fresh, closed := z.dir(t, "open/kept"), z.dir(t, "open/fresh"), z.dir(t, "home/bin")
	for _, dir := range []string{kept, fresh, closed} {
		require.NoError(t, os.WriteFile(filepath.Join(dir, "kubectl"), nil, 0o755))
	}
	_, err := s.SetShellPath(t.Context(), []string{closed, fresh, kept})
	require.NoError(t, err)

	log := testutil.CaptureLogs(t)
	_, err = s.SetShellPath(t.Context(), []string{closed, kept, fresh})
	require.NoError(t, err)
	// The probes run after the write, on a goroutine of their own, and an
	// earlier write's may log as well, so the test waits for this move.
	move := `"tool":"kubectl","from":"` + fresh + `","to":"` + kept + `"`
	require.Eventually(t, func() bool { return strings.Contains(log.String(), move) }, testutil.Timeout, time.Millisecond)
	assert.Equal(t, 1, strings.Count(log.String(), move), "said once")
	assert.NotContains(t, log.String(), `"from":"`+closed, "a folder no run reads never runs a tool")
}

// A probe of a tool that hangs never holds the write.
func TestAHungToolProbeDoesNotHoldTheWrite(t *testing.T) {
	z := newZones(t)
	s := newTestService(t, z, nil)
	dir := z.dir(t, "open/bin")
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	probed := make(chan string, 16)
	defer func(stat func(string) (os.FileInfo, error)) { statTool = stat }(statTool)
	statTool = func(name string) (os.FileInfo, error) {
		probed <- filepath.Dir(name)
		<-release
		return nil, os.ErrNotExist
	}

	_, err := s.SetShellPath(t.Context(), []string{dir})
	require.NoError(t, err)
	assert.Equal(t, []string{dir}, shellDirs(t, s), "the write landed while the probe hangs")
	assert.Equal(t, dir, testutil.Recv(t, probed, "a probe"))
}

// A read of the shell that fails, or answers no PATH, keeps the last PATH
// and says why; one that answers clears the fault.
func TestAFailedShellReadKeepsTheLastPath(t *testing.T) {
	z := newZones(t)
	dir := z.dir(t, "open/bin")
	var answer []string
	var fail error
	s := newTestService(t, z, func(context.Context) ([]string, error) { return answer, fail })
	s.fault = "timeout"
	assert.Equal(t, "timeout", s.PathFault(), "the launch's fault until a refresh answers")

	answer = []string{dir}
	require.NoError(t, s.RefreshPath(t.Context()))
	assert.Empty(t, s.PathFault())
	assert.Equal(t, []string{dir}, shellDirs(t, s))

	answer, fail = nil, errors.New("exit")
	err := s.RefreshPath(t.Context())
	assert.EqualError(t, err, "Your shell did not answer: exit.")
	assert.IsType(t, PathRefusal(""), err)
	assert.Equal(t, "exit", s.PathFault())
	assert.Equal(t, []string{dir}, shellDirs(t, s), "a failed read keeps the last PATH")

	fail = nil
	assert.EqualError(t, s.RefreshPath(t.Context()), "Your shell did not answer: its PATH is empty.")
	assert.Equal(t, []string{dir}, shellDirs(t, s), "an empty PATH keeps the last PATH")

	// A PATH the shell answered but that could not be stored leaves the
	// fault standing.
	answer = []string{z.dir(t, "open/other")}
	s.writing <- struct{}{}
	s.diskTimeout = time.Millisecond
	assert.ErrorIs(t, s.RefreshPath(t.Context()), context.DeadlineExceeded)
	<-s.writing
	assert.Equal(t, "its PATH is empty", s.PathFault())
	assert.Equal(t, []string{dir}, shellDirs(t, s))
}

// The last PATH read lives in app.db, so a launch whose own read fails finds
// it.
func TestTheShellsPathOutlivesARestart(t *testing.T) {
	z := newZones(t)
	dir := z.dir(t, "open/bin")
	db := appdbtest.Open(t)
	open := func() *Service {
		store := openStore(t, db)
		return NewService(store, func() Zones { return z.zones }, nil, nil, "")
	}
	_, err := open().SetShellPath(t.Context(), []string{dir})
	require.NoError(t, err)
	assert.Equal(t, []string{dir}, shellDirs(t, open()))
}

// A run's PATH is the setting expanded over the shell's last PATH, or over the
// platform's default folders before a read ever answered. Its NoWrite folders
// are the shell's and the typed ones, whatever the setting says.
func TestARunPathExpandsTheSetting(t *testing.T) {
	z := newZones(t)
	z.zones.Home = z.home
	fallback, shell, typed := z.dir(t, "open/default"), z.dir(t, "open/shell"), z.dir(t, "home/bin")
	store := newStore(t)
	s := NewService(store, func() Zones { return z.zones }, nil, []string{fallback}, "")
	ctx := t.Context()

	got, err := s.RunPath(ctx)
	require.NoError(t, err)
	assert.Equal(t, RunPath{
		Search:  []PathFolder{{Path: fallback, Origin: OriginShell}},
		NoWrite: []string{fallback},
	}, got, "no read has answered: the default folders")

	_, err = s.SetShellPath(ctx, []string{shell})
	require.NoError(t, err)
	require.NoError(t, s.SetPath(ctx, "~/bin:$PATH"))
	got, err = s.RunPath(ctx)
	require.NoError(t, err)
	assert.Equal(t, RunPath{
		Search:  []PathFolder{{Path: typed, Origin: OriginTyped}, {Path: shell, Origin: OriginShell}},
		NoWrite: []string{shell, typed},
	}, got)

	require.NoError(t, s.SetPath(ctx, "~/bin"))
	got, err = s.RunPath(ctx)
	require.NoError(t, err)
	assert.Equal(t, []PathFolder{{Path: typed, Origin: OriginTyped}}, got.Search)
	assert.Equal(t, []string{shell, typed}, got.NoWrite, "the user's terminal still runs what the shell's folders hold")
	noWrite, err := s.PathNoWrite(ctx)
	require.NoError(t, err)
	assert.Equal(t, got.NoWrite, noWrite)
}

// A refused setting starts no run, and its NoWrite folders are the shell's
// alone, since no typed folder can be read.
func TestARefusedPathStartsNoRun(t *testing.T) {
	z := newZones(t)
	shell := z.dir(t, "open/shell")
	db := appdbtest.Open(t)
	store := openStore(t, db)
	s := NewService(store, func() Zones { return z.zones }, nil, nil, "")
	_, err := s.SetShellPath(t.Context(), []string{shell})
	require.NoError(t, err)
	exec(t, db, `INSERT INTO settings (key, value, created_at, updated_at) VALUES ('sandbox.path', 'bin:$PATH', 0, 0)`)

	_, err = s.RunPath(t.Context())
	assert.IsType(t, PathRefusal(""), err)
	assert.ErrorContains(t, err, `"bin" is not a full path`)
	assert.ErrorContains(t, err, "Settings")
	noWrite, err := s.PathNoWrite(t.Context())
	require.NoError(t, err)
	assert.Equal(t, []string{shell}, noWrite)
}

// A read that fails, of either row, is an error: never the setting over the
// default folders, never $PATH.
func TestARunPathReadThatFailsIsAnError(t *testing.T) {
	for _, table := range []string{"settings", "shell_path"} {
		z := newZones(t)
		db := appdbtest.Open(t)
		store := openStore(t, db)
		s := NewService(store, func() Zones { return z.zones }, nil, []string{z.open}, "")
		exec(t, db, `DROP TABLE `+table)

		_, err := s.RunPath(t.Context())
		assert.Error(t, err, table)
		_, err = s.PathNoWrite(t.Context())
		assert.Error(t, err, table)
	}
}

// Settings lists the setting and each of its folders as a run would judge
// them; a refused setting lists none, and with no sandbox only the setting.
func TestSettingsListsThePathsFolders(t *testing.T) {
	z := newZones(t)
	z.zones.Home = z.home
	open, closed, typed := z.dir(t, "open/bin"), z.dir(t, "home/closed"), z.dir(t, "home/bin")
	db := appdbtest.Open(t)
	store := openStore(t, db)
	s := NewService(store, func() Zones { return z.zones }, nil, nil, "")
	ctx := t.Context()
	_, err := s.SetShellPath(ctx, []string{open, closed})
	require.NoError(t, err)
	require.NoError(t, s.SetPath(ctx, "$PATH:~/bin"))

	setting, folders, err := s.PathFolders(ctx)
	require.NoError(t, err)
	assert.Equal(t, PathSetting{Value: "$PATH:~/bin"}, setting)
	require.Len(t, folders, 3)
	assert.Equal(t, []string{open, closed, typed}, []string{folders[0].Path, folders[1].Path, folders[2].Path})
	assert.Equal(t, []bool{true, false, true}, []bool{folders[0].Searched, folders[1].Searched, folders[2].Searched})
	assert.True(t, folders[1].Addable)

	exec(t, db, `UPDATE settings SET value = 'bin' WHERE key = 'sandbox.path'`)
	setting, folders, err = s.PathFolders(ctx)
	require.NoError(t, err)
	assert.NotEmpty(t, setting.Refusal)
	assert.Empty(t, folders)

	none := NewService(store, nil, nil, nil, "")
	setting, folders, err = none.PathFolders(ctx)
	require.NoError(t, err)
	assert.Equal(t, "bin", setting.Value)
	assert.Empty(t, folders)

	exec(t, db, `DROP TABLE shell_path`)
	_, _, err = s.PathFolders(ctx)
	assert.Error(t, err, "a read that fails is an error")
}

// Settings' disk reads are bounded, as a read of the shell's are.
func TestSettingsListIsBounded(t *testing.T) {
	z := newZones(t)
	s := newTestService(t, z, nil)
	_, err := s.SetShellPath(t.Context(), []string{z.dir(t, "open/bin")})
	require.NoError(t, err)
	require.NoError(t, s.SetPath(t.Context(), z.dir(t, "home/bin")+":$PATH"))
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	s.checkFolder = func(string, bool, zones, []string, bool) error {
		<-release
		return nil
	}
	s.diskTimeout = time.Millisecond

	setting, folders, err := s.PathFolders(t.Context())
	require.NoError(t, err, "the setting is still there to show and edit")
	assert.Equal(t, z.dir(t, "home/bin")+":$PATH", setting.Value)
	require.Len(t, folders, 2)
	for _, f := range folders {
		assert.False(t, f.Searched)
		assert.Equal(t, timeoutRefusal.Reason, f.Reason)
	}
}

// A run's PATH and the NoWrite folders need the zones' home: zones that run
// past the bound refuse both, never expand ~ against nothing. Settings shows
// the setting with no folder.
func TestAPathWhoseZonesRunPastTheBoundIsRefused(t *testing.T) {
	z := newZones(t)
	s := newTestService(t, z, nil)
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	s.zones = func() Zones {
		<-release
		return z.zones
	}
	s.diskTimeout = time.Millisecond

	_, err := s.RunPath(t.Context())
	assert.Equal(t, timeoutRefusal, err)
	setting, folders, err := s.PathFolders(t.Context())
	require.NoError(t, err, "Settings still shows the setting")
	assert.Equal(t, "$PATH", setting.Value)
	assert.Empty(t, folders)
	_, err = s.PathNoWrite(t.Context())
	assert.Equal(t, timeoutRefusal, err)
}
