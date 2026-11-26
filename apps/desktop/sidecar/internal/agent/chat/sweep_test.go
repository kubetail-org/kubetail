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

package chat

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/appdb"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/testutil"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/llm"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/run/sandbox"
)

// Stopping ends a sweep's report nobody reads, so the hook cannot hold the stop.
func TestStopEndsAnUnreadSweepReport(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	box, lists := testBox()
	s, err := newService(openTestDB(t, dir), chatsDirIn(dir), monitorDirIn(dir), fakeLLM(), &stubClusterCards{}, nil, box, lists, sandbox.State{}, testSettings(t), nil)
	require.NoError(t, err)
	s.onSwept = make(chan struct{})
	stop, err := s.Start(t.Context())
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), testutil.Timeout)
	defer cancel()
	require.NoError(t, stop(ctx))
	require.NoError(t, s.Close())
}

// The sweeper subscribes before its startup sweep and runs on every clusters signal:
// a marked cluster's chats go, and another cluster's stay.
func TestTheSweeperDeletesAMarkedClustersChats(t *testing.T) {
	t.Parallel()

	s := newTestService(t)
	now := time.UnixMilli(1_000).UTC()
	mine := seedChat(t, s.db, aChat("7", now))
	seedTurn(t, s.db, mine.ID, now)
	also := seedChat(t, s.db, aChat("7", now))
	elsewhere := seedChat(t, s.db, aChat("8", now))

	markCluster(t, s.db, "7")
	s.db.Notify(appdb.KeyClusters)

	// The signal's sweep is the one that takes both chats.
	for {
		testutil.Wait(t, s.onSwept, "a sweep")
		_, ok, err := s.Get(t.Context(), also.ID)
		require.NoError(t, err)
		if !ok {
			break
		}
	}
	for _, id := range []ChatID{mine.ID, also.ID} {
		_, ok, err := s.Get(t.Context(), id)
		require.NoError(t, err)
		assert.False(t, ok, "chat %s was filed under the marked cluster", id)
	}
	_, ok, err := s.Get(t.Context(), elsewhere.ID)
	require.NoError(t, err)
	assert.True(t, ok, "another cluster's chat is not the sweeper's to take")
	assert.Zero(t, tableCount(t, s.db, "messages"), "the messages went with their chat")
}

// A sweep that finds nothing to do notifies nobody, or two subscribers of one key
// would wake each other forever.
func TestASweepThatCleansNothingNotifiesNobody(t *testing.T) {
	t.Parallel()

	s := newTestService(t)
	sub := s.db.ListenForChanges(appdb.KeyClusters)
	defer sub.Close()

	markCluster(t, s.db, "9")
	s.db.Notify(appdb.KeyClusters)
	testutil.Recv(t, sub.Chan(), "the mark's own signal")
	testutil.Wait(t, s.onSwept, "the sweep to finish")

	select {
	case <-sub.Chan():
		t.Fatal("a no-op sweep must not notify")
	case <-time.After(10 * s.sweepRetry):
		// The window is a multiple of the retry cadence, which is also what the
		// sweeper would re-arm itself on had the sweep failed.
	}
}

// A marked cluster the last run left behind is swept at startup, with no signal.
func TestTheSweeperRecoversAtStartup(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	db := openTestDB(t, dir)
	c := seedChat(t, db, aChat("7", time.UnixMilli(1_000).UTC()))
	markCluster(t, db, "7")
	require.NoError(t, db.Close())

	s := startService(t, dir)

	_, ok, err := s.Get(t.Context(), c.ID)
	require.NoError(t, err)
	assert.False(t, ok)
}

// A sweep that fails re-arms its own retry: no notification follows a failure, since
// the mirror cannot remove a row while chats remain and notifies only on removal.
func TestAFailedSweepRetriesOnItsOwn(t *testing.T) {
	t.Parallel()

	s := newTestService(t)
	c := seedChat(t, s.db, aChat("7", time.UnixMilli(1_000).UTC()))
	failures := 2
	s.deleteWrite = func(ctx context.Context, id ChatID) (bool, error) {
		if failures > 0 {
			failures--
			return false, assert.AnError
		}
		return s.deleteRow(ctx, id)
	}

	markCluster(t, s.db, "7")
	s.db.Notify(appdb.KeyClusters)

	// Sweeps until the retries have finished what the failures left: the
	// signal's, and one per failed attempt, come first.
	for {
		testutil.Wait(t, s.onSwept, "a sweep")
		_, ok, err := s.Get(t.Context(), c.ID)
		require.NoError(t, err)
		if !ok {
			break
		}
	}
	assert.Equal(t, 0, failures, "every failure was retried through")
}

// The read of the clusters table reports a store that will not answer.
func TestTheClusterReadsReportAStorageFault(t *testing.T) {
	t.Parallel()

	s := newTestService(t)
	require.NoError(t, s.store.Close())

	_, err := markedClusterIDs(t.Context(), s.store.Stmts())
	assert.Error(t, err)
}

// A service that is stopping refuses to start its sweeper, the way every other
// entrant is refused once stop has begun.
func TestStartAfterStopRefusesTheSweeper(t *testing.T) {
	t.Parallel()

	s, err := newService(openTestDB(t, t.TempDir()), chatsDirIn(t.TempDir()), monitorDirIn(t.TempDir()), fakeLLM(), noClusterCards, nil, testReaders, noLists, sandbox.State{}, testSettings(t), nil)
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, s.Close()) })
	require.NoError(t, s.stop(t.Context()))

	_, err = s.Start(t.Context())

	assert.ErrorIs(t, err, ErrStopping)
}

// The sweep a cluster delete runs: every chat filed under it goes, and nothing
// else is touched.
func TestDeleteByClusterDeletesEachChat(t *testing.T) {
	t.Parallel()

	s := newTestService(t)
	now := time.UnixMilli(1_000).UTC()
	first := seedChat(t, s.db, aChat("7", now))
	second := seedChat(t, s.db, aChat("7", now))
	elsewhere := seedChat(t, s.db, aChat("8", now))

	n, err := s.deleteByCluster(t.Context(), "7")
	require.NoError(t, err)
	assert.Equal(t, 2, n)

	for _, id := range []ChatID{first.ID, second.ID} {
		_, ok, err := s.Get(t.Context(), id)
		require.NoError(t, err)
		assert.False(t, ok, "chat %s was filed under the swept cluster", id)
	}
	_, ok, err := s.Get(t.Context(), elsewhere.ID)
	require.NoError(t, err)
	assert.True(t, ok, "another cluster's chat is not the sweep's to take")
}

func TestDeleteByClusterOfAClusterWithNoChatsIsANoOp(t *testing.T) {
	t.Parallel()

	s := newTestService(t)
	n, err := s.deleteByCluster(t.Context(), "9")
	assert.NoError(t, err)
	assert.Zero(t, n)
}

// The read that names the chats is the sweep's first statement, and its failure is
// the caller's to see: a sweep that could not list is not a sweep that found none.
func TestDeleteByClusterReportsAReadThatFailed(t *testing.T) {
	t.Parallel()

	s := newTestService(t)
	require.NoError(t, s.store.Close())

	_, err := s.deleteByCluster(t.Context(), "1")
	assert.ErrorContains(t, err, "chat ids by cluster")
}

// A chats' directory the sweep cannot list is left for the next start.
func TestTheStartSweepLeavesAChatsDirectoryItCannotList(t *testing.T) {
	// CaptureLogs swaps the process's default logger, so this test stays serial.

	dir := t.TempDir()
	s, err := newService(openTestDB(t, dir), chatsDirIn(dir), monitorDirIn(dir), fakeLLM(), noClusterCards, nil, testReaders, noLists, sandbox.State{}, testSettings(t), nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.monitorRoot.Close() })
	require.NoError(t, s.chatsRoot.Close())
	logs := testutil.CaptureLogs(t)

	s.sweepChatDirs()

	assert.Contains(t, logs.String(), "could not list the chats' directory")
}

// A monitor's directory the sweep cannot list is logged and left for the next
// pass, which still reads the live clusters.
func TestAMonitorSweepLeavesADirectoryItCannotList(t *testing.T) {
	// CaptureLogs swaps the process's default logger, so this test stays serial.

	dir := t.TempDir()
	s, err := newService(openTestDB(t, dir), chatsDirIn(dir), monitorDirIn(dir), fakeLLM(), noClusterCards, nil, testReaders, noLists, sandbox.State{}, testSettings(t), nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.chatsRoot.Close(); _ = s.store.Close() })
	require.NoError(t, s.monitorRoot.Close())
	logs := testutil.CaptureLogs(t)

	assert.NoError(t, s.sweepMonitors())

	assert.Contains(t, logs.String(), "could not list the monitor's directory")
}

// The start sweep removes every entry of the chats' directory that is not a
// chat's: what a failed removal or a crash left.
func TestTheStartSweepRemovesTheDirectoriesOfGoneChats(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	db := openTestDB(t, dir)
	kept := seedChat(t, db, aChat("1", time.Now()))
	results := chatsDirIn(dir)
	for _, name := range []string{string(kept.ID), "01a0ce44-0000-7000-8000-000000000000"} {
		require.NoError(t, os.MkdirAll(filepath.Join(results, name), 0o700))
		require.NoError(t, os.WriteFile(filepath.Join(results, name, "out.txt"), nil, 0o600))
	}
	require.NoError(t, os.WriteFile(filepath.Join(results, "stray"), nil, 0o600))

	s, err := newService(db, chatsDirIn(dir), monitorDirIn(dir), fakeLLM(), noClusterCards, nil, testReaders, noLists, sandbox.State{}, testSettings(t), nil)
	require.NoError(t, err)
	startPrepared(t, s)

	entries, err := os.ReadDir(results)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	assert.Equal(t, string(kept.ID), entries[0].Name())
	assert.FileExists(t, filepath.Join(results, string(kept.ID), "out.txt"))
}

// awaitSweepUntil waits out sweeps until cond holds.
func awaitSweepUntil(t *testing.T, s *service, cond func() bool) {
	t.Helper()
	for !cond() {
		testutil.Wait(t, s.onSwept, "a sweep")
	}
}

// Marking a cluster ends its monitor and removes its folder before its chats
// go, so a chat delete that fails leaves no monitor running; the row's delete
// then takes the run's rows.
func TestAMonitorRunEndsWithItsCluster(t *testing.T) {
	t.Parallel()

	s := startMonitorService(t, "", testTool{name: "echo"})
	c := seedChat(t, s.db, aChat("7", time.UnixMilli(1_000).UTC()))
	_, done := heldMonitor(t, s, "7")
	var atDelete []bool
	s.deleteWrite = func(ctx context.Context, id ChatID) (bool, error) {
		s.turnsMu.Lock()
		_, running := s.monitors["7"]
		s.turnsMu.Unlock()
		_, err := os.Stat(s.monitorDir("7").Path())
		atDelete = append(atDelete, running || err == nil)
		if len(atDelete) == 1 {
			return false, assert.AnError
		}
		return s.deleteRow(ctx, id)
	}

	markCluster(t, s.db, "7")
	s.db.Notify(appdb.KeyClusters)

	res := testutil.Recv(t, done, "the run to end")
	assert.Equal(t, RunCancelled, res.Status)
	awaitSweepUntil(t, s, func() bool { _, ok, _ := s.Get(t.Context(), c.ID); return !ok })
	assert.Equal(t, []bool{false, false}, atDelete, "no run and no folder by the time any chat delete runs")
	assert.NoDirExists(t, s.monitorDir("7").Path())

	_, err := s.db.Write.Exec(`DELETE FROM clusters WHERE id = '7'`)
	require.NoError(t, err)
	assert.Zero(t, tableCount(t, s.db, "agent_runs"))
	assert.Zero(t, tableCount(t, s.db, "llm_calls"))
}

// A cluster whose row is gone before any pass saw it marked still loses its
// monitor and its folder; a run on a live cluster is left alone.
func TestAMonitorEndsWhenItsRowIsGone(t *testing.T) {
	t.Parallel()

	s := startMonitorService(t, "", testTool{name: "echo"})
	_, gone := heldMonitor(t, s, "7")
	liveGate, live := heldMonitor(t, s, "8")

	_, err := s.db.Write.Exec(`DELETE FROM clusters WHERE id = '7'`)
	require.NoError(t, err)
	s.db.Notify(appdb.KeyClusters)

	assert.Equal(t, RunCancelled, testutil.Recv(t, gone, "the run to end").Status)
	awaitSweepUntil(t, s, func() bool { _, err := os.Stat(s.monitorDir("7").Path()); return os.IsNotExist(err) })
	assert.DirExists(t, s.monitorDir("8").Path())
	select {
	case <-live:
		t.Fatal("a live cluster's run is not the sweep's to end")
	default:
	}
	close(liveGate)
	assert.Equal(t, RunSucceeded, testutil.Recv(t, live, "the live run").Status)
}

// The first pass removes every monitor folder that names no live cluster, and
// keeps a live cluster's.
func TestTheMonitorsFolderIsSwept(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	for _, name := range []string{"7", "gone"} {
		require.NoError(t, os.MkdirAll(filepath.Join(monitorDirIn(dir), name, "workspace"), 0o700))
	}
	require.NoError(t, os.WriteFile(filepath.Join(monitorDirIn(dir), "stray"), nil, 0o600))

	startService(t, dir)

	entries, err := os.ReadDir(monitorDirIn(dir))
	require.NoError(t, err)
	require.Len(t, entries, 1)
	assert.Equal(t, "7", entries[0].Name())
}

// A run reserved for a cluster made after the sweep read the live rows is not
// the sweep's to end: it took its slots before that read.
func TestAMonitorSweepLeavesARunReservedAfterItsRead(t *testing.T) {
	t.Parallel()

	s := startMonitorService(t, "")
	type reservation struct {
		m   *monitor
		err error
	}
	reserved := make(chan reservation, 1)
	var once sync.Once
	s.onLiveRead = func() {
		once.Do(func() {
			_, err := s.db.Write.Exec(`INSERT INTO clusters (id, source, source_key, created_at, updated_at)
			VALUES ('new', 'kubeconfig', 'ctx-new', 0, 0)`)
			if err != nil {
				reserved <- reservation{err: err}
				return
			}
			m, err := s.reserveMonitor(context.Background(), "new", "", llm.Target{})
			reserved <- reservation{m, err}
		})
	}

	s.db.Notify(appdb.KeyClusters)
	r := testutil.Recv(t, reserved, "the reservation")
	require.NoError(t, r.err)
	t.Cleanup(func() { s.releaseMonitor("new", r.m) })
	testutil.Wait(t, s.onSwept, "the sweep")
	assert.NoError(t, r.m.ctx.Err(), "the run is not cancelled")
}

// A run that starts on a cluster with a folder after the sweep copied the
// slots, its cluster marked before the live read, keeps its folder through that
// pass; the mark's signal brings the pass that ends it and removes it.
func TestAMonitorSweepKeepsTheFolderOfARunItDidNotEnd(t *testing.T) {
	t.Parallel()

	s := startMonitorService(t, "", testTool{name: "echo"})
	require.NoError(t, os.MkdirAll(s.monitorDir("8").Path(), 0o700))
	entered, proceed := make(chan struct{}), make(chan struct{})
	var once sync.Once
	s.onSlotsCopied = func() {
		once.Do(func() {
			entered <- struct{}{}
			<-proceed
		})
	}

	s.db.Notify(appdb.KeyClusters)
	testutil.Wait(t, entered, "the slots copied")
	_, done := heldMonitor(t, s, "8")
	markCluster(t, s.db, "8")
	close(proceed)
	testutil.Wait(t, s.onSwept, "the sweep")
	assert.DirExists(t, s.monitorDir("8").Path())
	select {
	case <-done:
		t.Fatal("a run the pass did not copy is not the pass's to end")
	default:
	}

	s.db.Notify(appdb.KeyClusters)
	assert.Equal(t, RunCancelled, testutil.Recv(t, done, "the run to end").Status)
	awaitSweepUntil(t, s, func() bool { _, err := os.Stat(s.monitorDir("8").Path()); return os.IsNotExist(err) })
}
