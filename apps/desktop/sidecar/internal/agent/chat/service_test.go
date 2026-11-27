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
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/agent/loop"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/appdb"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/apimeta"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/testutil"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/version"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/llm"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/run/sandbox"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/logs"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/tools"
)

// --- lifecycle ---

// A run a previous process left unfinished is failed at the next start, with its
// content kept: the answer reads as Failed with the reason, not as Streaming forever.
func TestStartFailsAStrandedRun(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	now := time.UnixMilli(1_000).UTC()
	db := openTestDB(t, dir)
	c := seedChat(t, db, aChat("1", now))
	stranded := seedTurn(t, db, c.ID, now)
	setRunStatus(t, db, stranded.Run, RunRunning)
	_, err := db.Write.Exec(`UPDATE messages SET content = ? WHERE id = ?`, `[{"type":"text","text":"half an answer"}]`, string(stranded.Assistant))
	require.NoError(t, err)
	require.NoError(t, db.Close())

	s := startService(t, dir)

	msgs, err := listMessages(t.Context(), s.store.Stmts(), c.ID, testReaders)
	require.NoError(t, err)
	require.Len(t, msgs, 2)
	assert.Equal(t, StatusFailed, msgs[1].Status)
	assert.Equal(t, strandedReason, msgs[1].Error)
	assert.Contains(t, string(msgs[1].Content), "half an answer")
	assert.Equal(t, RunFailed, runStatusOf(t, s.db, stranded.Run))
}

// A monitor run a previous process left running is failed at the next start, as
// any run is, its open calls closed: the reconcile survives its NULL chat.
func TestAStrandedMonitorRunIsFailed(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	db := openTestDB(t, dir)
	run := agentRun{ID: newRunID(), ClusterID: "7", ProviderID: "fake", ModelID: "fake", Dialect: "fake", Task: "look", AppVersion: "test"}
	require.NoError(t, prepareOn(t, db).InTx(t.Context(), func(st stmts) error { return insertMonitorRun(t.Context(), st, run) }))
	setRunStatus(t, db, run.ID, RunRunning)
	_, err := db.Write.Exec(`INSERT INTO llm_calls (id, run_id, seq, provider, model, started_at) VALUES ('l', ?, 0, 'fake', 'fake', 0)`, string(run.ID))
	require.NoError(t, err)
	_, err = db.Write.Exec(`INSERT INTO tool_calls (id, llm_call_id, seq, tool_name, contract_name, status, created_at, started_at) VALUES ('t', 'l', 0, 'Bash', 'bash/v1', 'running', 0, 0)`)
	require.NoError(t, err)
	require.NoError(t, db.Close())

	s := startService(t, dir)

	assert.Equal(t, RunFailed, runStatusOf(t, s.db, run.ID))
	assert.True(t, llmCallOf(t, s.db, run.ID).finished)
	calls := toolCallRows(t, s.db, run.ID)
	require.Len(t, calls, 1)
	assert.Equal(t, "failed", calls[0].status)
	assert.Equal(t, `{"error":"stranded"}`, calls[0].errText)
}

// A watch opened before Start read the stranded answer as it was; the reconcile
// pings its chat, so the watcher sees it fail rather than stream forever.
func TestStartTellsAPreStartWatchOfAStrandedRun(t *testing.T) {
	t.Parallel()

	db := openTestDB(t, t.TempDir())
	now := time.UnixMilli(1_000).UTC()
	c := seedChat(t, db, aChat("1", now))
	stranded := seedTurn(t, db, c.ID, now)
	setRunStatus(t, db, stranded.Run, RunRunning)
	s, err := newService(db, chatsDirIn(t.TempDir()), monitorDirIn(t.TempDir()), fakeLLM(), noClusterCards, nil, testReaders, noLists, sandbox.State{}, testSettings(t), nil)
	require.NoError(t, err)

	w, err := s.WatchMessageCollection(t.Context(), c.ID)
	require.NoError(t, err)
	snapshot := collectSnapshot(t, w.Frames)
	require.Equal(t, StatusStreaming, snapshot[1].Status)

	startPrepared(t, s)

	failed := awaitFrame(t, w.Frames, func(f ChatMessageCollectionWatchFrame) bool {
		return f.Message != nil && f.Message.ID == stranded.Assistant
	})
	assert.Equal(t, CollectionWatchFrameModified, failed.Type)
	assert.Equal(t, StatusFailed, failed.Message.Status)
}

// A start that fails is the end of the service, so it joins the watches admitted
// before it: nothing else will, since the lifecycle stops only what started.
func TestAFailedStartEndsThePreStartWatches(t *testing.T) {
	t.Parallel()

	db := openTestDB(t, t.TempDir())
	s, err := newService(db, chatsDirIn(t.TempDir()), monitorDirIn(t.TempDir()), fakeLLM(), noClusterCards, nil, testReaders, noLists, sandbox.State{}, testSettings(t), nil)
	require.NoError(t, err)
	w, err := s.WatchCollection(t.Context())
	require.NoError(t, err)
	collectChatSnapshot(t, w.Frames)
	require.NoError(t, db.Close())

	_, err = s.Start(t.Context())
	require.Error(t, err)

	testutil.RecvClosed(t, w.Frames, "the pre-start watch")
	assert.NoError(t, w.Err(), "the service ending is not a failure of the watch")
	require.NoError(t, s.Close())
}

// The stranded runs settle without moving their chats: the sends that
// stranded them already did, and moving them again would put every interrupted
// chat above ones the user touched since.
func TestTheStartupReconcileLeavesTheListOrderAlone(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	older, newer := time.UnixMilli(1_000).UTC(), time.UnixMilli(2_000).UTC()
	db := openTestDB(t, dir)
	interrupted := seedChat(t, db, aChat("1", older))
	seedTurn(t, db, interrupted.ID, older)
	recent := seedChat(t, db, aChat("1", newer))
	require.NoError(t, db.Close())

	s := startService(t, dir)

	chats, err := s.List(t.Context())
	require.NoError(t, err)
	require.Len(t, chats, 2)
	assert.Equal(t, recent.ID, chats[0].ID)
	assert.Equal(t, older, chats[1].UpdatedAt)
}

// A service whose statements will not prepare is not built: the app closes the
// file behind it rather than running over a store that answers nothing.
func TestNewFailsWhenTheStatementsWillNotPrepare(t *testing.T) {
	t.Parallel()

	db := openTestDB(t, t.TempDir())
	require.NoError(t, db.Close())

	_, err := New(db, chatsDirIn(t.TempDir()), monitorDirIn(t.TempDir()), fakeLLM(), noClusterCards, nil, tools.Box{}, noLists, sandbox.State{}, testSettings(t), nil)

	assert.ErrorContains(t, err, "prepare chat statements")
}

// A service whose monitor's directory cannot be made is not built.
func TestNewFailsWhenTheMonitorsDirectoryCannotBeMade(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	require.NoError(t, os.WriteFile(monitorDirIn(dir), nil, 0o600))

	_, err := New(openTestDB(t, dir), chatsDirIn(dir), monitorDirIn(dir), fakeLLM(), noClusterCards, nil, tools.Box{}, noLists, sandbox.State{}, testSettings(t), nil)

	assert.ErrorContains(t, err, "open the monitor's directory")
}

// A reconcile the service could not run is a failed start, not a service that
// quietly left stranded rows behind.
func TestStartReportsAReconcileItCouldNotRun(t *testing.T) {
	t.Parallel()

	db := openTestDB(t, t.TempDir())
	s, err := newService(db, chatsDirIn(t.TempDir()), monitorDirIn(t.TempDir()), fakeLLM(), noClusterCards, nil, testReaders, noLists, sandbox.State{}, testSettings(t), nil)
	require.NoError(t, err)
	// The statements fail to close on the closed file; the directory is what matters.
	t.Cleanup(func() { _ = s.Close() })
	require.NoError(t, db.Close())

	_, err = s.Start(t.Context())
	assert.Error(t, err)
}

// The service's context exists from construction, so a watch that arrives before
// Start is answered.
func TestAWatchBeforeStartIsAnswered(t *testing.T) {
	t.Parallel()

	s, err := newService(openTestDB(t, t.TempDir()), chatsDirIn(t.TempDir()), monitorDirIn(t.TempDir()), fakeLLM(), noClusterCards, nil, testReaders, noLists, sandbox.State{}, testSettings(t), nil)
	require.NoError(t, err)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), testutil.Timeout)
		defer cancel()
		require.NoError(t, s.stop(ctx))
		require.NoError(t, s.Close())
	})

	st, err := s.WatchCollection(t.Context())
	require.NoError(t, err)
	assert.Empty(t, collectChatSnapshot(t, st.Frames))
}

// Nothing joins the WaitGroup once stop has run: work admitted after its Wait is
// work Close pulls the connection out from under.
func TestWorkArrivingAfterStopIsRefused(t *testing.T) {
	t.Parallel()

	s, err := newService(openTestDB(t, t.TempDir()), chatsDirIn(t.TempDir()), monitorDirIn(t.TempDir()), fakeLLM(), noClusterCards, nil, testReaders, noLists, sandbox.State{}, testSettings(t), nil)
	require.NoError(t, err)
	stop, err := s.Start(t.Context())
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, s.Close()) })
	c := seedChat(t, s.db, aChat("1", time.UnixMilli(1_000).UTC()))
	require.NoError(t, stop(t.Context()))

	assert.ErrorIs(t, s.Delete(t.Context(), c.ID), ErrStopping)
	_, err = s.Send(t.Context(), nil, ModeChat, "1", SendSwitches{}, "fake", "fake", "high", reqID("1"), "hi", nil)
	assert.ErrorIs(t, err, ErrStopping)
	// The sweep is Delete per chat, so it is refused where a single delete is — and
	// reports the first refusal rather than walking the rest.
	_, err = s.deleteByCluster(t.Context(), "1")
	assert.ErrorIs(t, err, ErrStopping)

	st, err := s.WatchCollection(t.Context())
	require.NoError(t, err)
	testutil.RecvClosed(t, st.Frames, "the list watch")
	assert.NoError(t, st.Err())
}

// stop waits with drain.WithContext, so a deadline that expires with work still
// running is reported rather than passed off as a clean drain.
func TestStopReportsADrainItCouldNotFinish(t *testing.T) {
	t.Parallel()

	s := newTestService(t)
	c := seedChat(t, s.db, aChat("1", time.UnixMilli(1_000).UTC()))
	entered, release := make(chan struct{}), make(chan struct{})
	s.deleteWrite = func(ctx context.Context, id ChatID) (bool, error) {
		close(entered)
		<-release
		return s.deleteRow(ctx, id)
	}
	deleted := make(chan error, 1)
	go func() { deleted <- s.Delete(t.Context(), c.ID) }()
	testutil.Wait(t, entered, "the delete to reach its write")

	expired, cancel := context.WithCancel(context.Background())
	cancel()
	assert.Error(t, s.stop(expired))

	// Released, the write lands or reads the cancel, whichever comes first; the
	// drain report above is what this pins.
	close(release)
	testutil.Recv(t, deleted, "the delete")
}

// A delete's write outlives its caller but not the service: stop cancels one parked
// behind the writer, so the drain is not held by it.
func TestADeletesWriteEndsWithTheService(t *testing.T) {
	t.Parallel()

	s := newTestService(t)
	c := seedChat(t, s.db, aChat("1", time.UnixMilli(1_000).UTC()))
	entered := make(chan struct{})
	s.deleteWrite = func(ctx context.Context, id ChatID) (bool, error) {
		close(entered)
		<-ctx.Done()
		return false, ctx.Err()
	}
	deleted := make(chan error, 1)
	go func() { deleted <- s.Delete(t.Context(), c.ID) }()
	testutil.Wait(t, entered, "the delete to reach its write")

	require.NoError(t, s.stop(t.Context()))

	assert.ErrorIs(t, testutil.Recv(t, deleted, "the delete"), context.Canceled)
}

// A store that will not answer reaches every caller as an error, never as an empty
// list or a silent no-op. A closed pool is the one failure every entry point shares.
func TestEveryEntryPointReportsAFailedStore(t *testing.T) {
	t.Parallel()

	s := newTestService(t)
	id := ChatID(appdb.NewID())
	require.NoError(t, s.db.Close())

	_, err := s.Rename(t.Context(), id, "title")
	assert.Error(t, err)
	assert.Error(t, s.Delete(t.Context(), id))
	_, _, err = s.Get(t.Context(), id)
	assert.Error(t, err)
	_, err = s.List(t.Context())
	assert.Error(t, err)
	_, err = s.Send(t.Context(), nil, ModeChat, "1", SendSwitches{}, "fake", "fake", "high", reqID("1"), "hi", nil)
	assert.Error(t, err)
}

// --- rename ---

func TestRenameOfAnUnknownChatIsChatGone(t *testing.T) {
	t.Parallel()

	s := newTestService(t)

	_, err := s.Rename(t.Context(), ChatID(appdb.NewID()), "title")
	assert.ErrorIs(t, err, ErrChatGone)
}

// A rename is trimmed, and one left empty, longer than the cap or of more than one
// line is refused with nothing written: the title reaches every window over the list watch.
func TestRenameRefusesAnEmptyOverlongOrMultilineTitle(t *testing.T) {
	t.Parallel()

	s := newTestService(t)
	c := seedChat(t, s.db, aChat("1", time.UnixMilli(1_000).UTC()))

	refused := []string{"", "  \n ", strings.Repeat("x", maxTitleLen+1)}
	for _, lineBreak := range lineBreaks {
		refused = append(refused, "ops"+string(lineBreak)+"west")
	}
	for _, title := range refused {
		_, err := s.Rename(t.Context(), c.ID, title)
		assert.ErrorIs(t, err, ErrBadRequest, "%q", title)
	}
	stored, _, err := s.Get(t.Context(), c.ID)
	require.NoError(t, err)
	assert.Equal(t, c.Title, stored.Title)

	renamed, err := s.Rename(t.Context(), c.ID, "  pods ")
	require.NoError(t, err)
	assert.Equal(t, "pods", renamed.Title)
}

// The webview checks a title before it sends one, by the same rules; its
// `normalizeChatTitle` tests pin these cases (src/lib/chats.test.tsx).
func TestRenameTrimsAndCountsAsTheWebviewDoes(t *testing.T) {
	t.Parallel()

	s := newTestService(t)
	c := seedChat(t, s.db, aChat("1", time.UnixMilli(1_000).UTC()))

	renamed, err := s.Rename(t.Context(), c.ID, " \t\u0085\u00a0\u2028\u3000pods\n\u205f ")
	require.NoError(t, err)
	assert.Equal(t, "pods", renamed.Title)

	renamed, err = s.Rename(t.Context(), c.ID, "\u0085"+strings.Repeat("a", maxTitleLen))
	require.NoError(t, err)
	assert.Equal(t, strings.Repeat("a", maxTitleLen), renamed.Title)

	// TrimSpace keeps U+FEFF, which JavaScript's trim drops.
	_, err = s.Rename(t.Context(), c.ID, "\ufeff"+strings.Repeat("a", maxTitleLen))
	assert.ErrorIs(t, err, ErrBadRequest)
}

// The rename moves the chat's recency, and what it returns is the row it committed.
func TestRenameReturnsTheRecordItCommitted(t *testing.T) {
	t.Parallel()

	s := newTestService(t)
	s.now = func() time.Time { return time.UnixMilli(5_000) }
	c := seedChat(t, s.db, aChat("1", time.UnixMilli(1_000).UTC()))

	renamed, err := s.Rename(t.Context(), c.ID, "pods")
	require.NoError(t, err)

	stored, ok, err := s.Get(t.Context(), c.ID)
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, stored, renamed)
	assert.Equal(t, "pods", renamed.Title)
	assert.Equal(t, time.UnixMilli(5_000).UTC(), renamed.UpdatedAt)
}

// A rename racing a delete ends one way or the other: ErrChatGone, or the record
// whole. Never a nil error over an empty record. A guard, not a proof — a run whose
// delete lands outside the window passes with the bug present.
func TestRenameNeverReportsSuccessOverAnEmptyRecord(t *testing.T) {
	t.Parallel()

	s := newTestService(t)

	for range 50 {
		c := seedChat(t, s.db, aChat("1", time.UnixMilli(1_000).UTC()))
		var (
			wg        sync.WaitGroup
			renamed   Chat
			renameErr error
		)
		wg.Go(func() { renamed, renameErr = s.Rename(t.Context(), c.ID, "pods") })
		wg.Go(func() { require.NoError(t, s.Delete(t.Context(), c.ID)) })
		wg.Wait()

		if renameErr != nil {
			assert.ErrorIs(t, renameErr, ErrChatGone)
			continue
		}
		assert.Equal(t, c.ID, renamed.ID)
		assert.Equal(t, "pods", renamed.Title)
	}
}

// --- delete ---

func TestDeleteTakesTheChatAndItsMessages(t *testing.T) {
	t.Parallel()

	s := newTestService(t)
	now := time.UnixMilli(1_000).UTC()
	c := seedChat(t, s.db, aChat("1", now))
	seedTurn(t, s.db, c.ID, now)

	w, err := s.WatchMessageCollection(t.Context(), c.ID)
	require.NoError(t, err)
	require.Len(t, collectSnapshot(t, w.Frames), 2)

	require.NoError(t, s.Delete(t.Context(), c.ID))

	for range 2 {
		f := testutil.Recv(t, w.Frames, "a removal")
		assert.Equal(t, CollectionWatchFrameDeleted, f.Type)
	}
	_, ok, err := s.Get(t.Context(), c.ID)
	require.NoError(t, err)
	assert.False(t, ok)
	msgs, err := listMessages(t.Context(), s.store.Stmts(), c.ID, testReaders)
	require.NoError(t, err)
	assert.Empty(t, msgs)
	assert.Zero(t, tableCount(t, s.db, "agent_runs"))
}

func TestDeleteOfAnUnknownChatIsANoOp(t *testing.T) {
	t.Parallel()

	s := newTestService(t)
	assert.NoError(t, s.Delete(t.Context(), ChatID(appdb.NewID())))
}

// A chat's row going may be what a marked cluster's teardown was waiting on, so a
// delete that removed one pings the clusters key — and one that found nothing does
// not, since a no-op ping would wake the mirror for nothing.
func TestDeleteTellsTheClusterMirrorWhenARowWent(t *testing.T) {
	t.Parallel()

	s := newTestService(t)
	c := seedChat(t, s.db, aChat("1", time.UnixMilli(1_000).UTC()))
	sub := s.db.ListenForChanges(appdb.KeyClusters)
	defer sub.Close()

	require.NoError(t, s.Delete(t.Context(), c.ID))
	testutil.Recv(t, sub.Chan(), "the ping for the row that went")

	require.NoError(t, s.Delete(t.Context(), c.ID))
	// The window is a multiple of the sweeper's cadence, the one clock in play.
	testutil.NoRecv(t, sub.Chan(), 10*s.sweepRetry, "a ping for a delete that removed nothing")
}

// A delete whose caller gave up still lands: the rows go on a context of the
// service's own, so a chat is never left half-deleted.
func TestDeleteOutlivesItsCallersContext(t *testing.T) {
	t.Parallel()

	s := newTestService(t)
	c := seedChat(t, s.db, aChat("1", time.UnixMilli(1_000).UTC()))
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	require.NoError(t, s.Delete(ctx, c.ID))

	_, ok, err := s.Get(t.Context(), c.ID)
	require.NoError(t, err)
	assert.False(t, ok)
}

// --- the watches ---

// The transcript watch opens with the rows in seq order, then its Bookmark, and a
// change to a row arrives as Modified.
func TestMessageCollectionWatchOpensWithASnapshotAndItsBookmark(t *testing.T) {
	t.Parallel()

	s := newTestService(t)
	now := time.UnixMilli(1_000).UTC()
	c := seedChat(t, s.db, aChat("1", now))
	turn := seedTurn(t, s.db, c.ID, now)

	w, err := s.WatchMessageCollection(t.Context(), c.ID)
	require.NoError(t, err)

	first := testutil.Recv(t, w.Frames, "the question")
	assert.Equal(t, CollectionWatchFrameAdded, first.Type)
	assert.Equal(t, RoleUser, first.Message.Role)
	second := testutil.Recv(t, w.Frames, "the empty answer")
	assert.Equal(t, CollectionWatchFrameAdded, second.Type)
	assert.Equal(t, StatusStreaming, second.Message.Status)
	bookmark := testutil.Recv(t, w.Frames, "the bookmark")
	assert.Equal(t, CollectionWatchFrameBookmark, bookmark.Type)
	assert.Nil(t, bookmark.Message)

	settleSeededRun(t, s.db, turn.Run, RunSucceeded, time.UnixMilli(2_000).UTC())
	s.notify(messagesKey(c.ID))
	settled := awaitFrame(t, w.Frames, func(f ChatMessageCollectionWatchFrame) bool {
		return f.Message != nil && f.Message.ID == turn.Assistant && f.Message.Status == StatusComplete
	})
	assert.Equal(t, CollectionWatchFrameModified, settled.Type)
}

// A watcher of one chat sees nothing of another's rows, and a change to the other
// chat does not wake it: the keys are per chat.
func TestMessageCollectionWatchIsScopedToItsChat(t *testing.T) {
	t.Parallel()

	s := newTestService(t)
	now := time.UnixMilli(1_000).UTC()
	mine := seedChat(t, s.db, aChat("1", now))
	other := seedChat(t, s.db, aChat("1", now))
	seedTurn(t, s.db, mine.ID, now)
	seedTurn(t, s.db, other.ID, now)

	w, err := s.WatchMessageCollection(t.Context(), mine.ID)
	require.NoError(t, err)
	snapshot := collectSnapshot(t, w.Frames)
	require.Len(t, snapshot, 2)
	for _, m := range snapshot {
		assert.Equal(t, mine.ID, m.ChatID)
	}

	require.NoError(t, s.Delete(t.Context(), other.ID))
	seedTurn(t, s.db, mine.ID, now)
	s.notify(messagesKey(mine.ID))
	// The other chat's delete pinged its own key ahead of this one; the first frame
	// this watcher sees is its own chat's new row, never a removal.
	f := testutil.Recv(t, w.Frames, "the next frame")
	assert.Equal(t, CollectionWatchFrameAdded, f.Type)
	assert.Equal(t, mine.ID, f.Message.ChatID)
}

// A watcher that never reads holds nothing but its own pump: another watcher of the
// same chat, and every other operation, go on.
func TestAStalledWatcherStallsNothingElse(t *testing.T) {
	t.Parallel()

	s := newTestService(t)
	now := time.UnixMilli(1_000).UTC()
	c := seedChat(t, s.db, aChat("1", now))
	seedTurn(t, s.db, c.ID, now)

	stalled, err := s.WatchMessageCollection(t.Context(), c.ID)
	require.NoError(t, err)
	_ = stalled // never read

	w, err := s.WatchMessageCollection(t.Context(), c.ID)
	require.NoError(t, err)
	require.Len(t, collectSnapshot(t, w.Frames), 2)
	_, err = s.Rename(t.Context(), c.ID, "pods")
	require.NoError(t, err)
	seedTurn(t, s.db, c.ID, now)
	s.notify(messagesKey(c.ID))
	testutil.Recv(t, w.Frames, "the reading watcher's next frame")
}

func TestWatchCollectionReportsCreatesAndRenames(t *testing.T) {
	t.Parallel()

	s := newTestService(t)

	w, err := s.WatchCollection(t.Context())
	require.NoError(t, err)
	bookmark := testutil.Recv(t, w.Frames, "the bookmark over an empty list")
	require.Equal(t, CollectionWatchFrameBookmark, bookmark.Type)

	c := seedChat(t, s.db, aChat("1", time.UnixMilli(1_000).UTC()))
	s.notify(chatsKey)
	added := awaitFrame(t, w.Frames, func(f ChatCollectionWatchFrame) bool { return f.Type == CollectionWatchFrameAdded })
	assert.Equal(t, c.ID, added.Chat.ID)

	_, err = s.Rename(t.Context(), c.ID, "pods")
	require.NoError(t, err)
	renamed := awaitFrame(t, w.Frames, func(f ChatCollectionWatchFrame) bool {
		return f.Chat != nil && f.Chat.Title == "pods"
	})
	assert.Equal(t, CollectionWatchFrameModified, renamed.Type)
}

func TestWatchCollectionReportsADelete(t *testing.T) {
	t.Parallel()

	s := newTestService(t)
	c := seedChat(t, s.db, aChat("1", time.UnixMilli(1_000).UTC()))

	w, err := s.WatchCollection(t.Context())
	require.NoError(t, err)
	require.Len(t, collectChatSnapshot(t, w.Frames), 1)

	require.NoError(t, s.Delete(t.Context(), c.ID))

	gone := awaitFrame(t, w.Frames, func(f ChatCollectionWatchFrame) bool { return f.Type == CollectionWatchFrameDeleted })
	assert.Equal(t, c.ID, gone.Chat.ID)
}

// A watch whose re-read fails ends with the reason on Err — a consumer that ignored
// it would turn a broken watch into a silent one.
func TestAWatchWhoseReadFailsReportsWhy(t *testing.T) {
	t.Parallel()

	s := newTestService(t)
	seedChat(t, s.db, aChat("1", time.UnixMilli(1_000).UTC()))

	w, err := s.WatchCollection(t.Context())
	require.NoError(t, err)
	collectChatSnapshot(t, w.Frames)

	// A closed pool is how a failing read is produced.
	require.NoError(t, s.store.Close())
	s.notify(chatsKey)

	testutil.WaitClosed(t, w.Frames, "the failed watch")
	assert.ErrorContains(t, w.Err(), "list chats")
}

func TestAMessageCollectionWatchReportsAFailedReRead(t *testing.T) {
	t.Parallel()

	s := newTestService(t)
	now := time.UnixMilli(1_000).UTC()
	c := seedChat(t, s.db, aChat("1", now))
	seedTurn(t, s.db, c.ID, now)

	w, err := s.WatchMessageCollection(t.Context(), c.ID)
	require.NoError(t, err)
	require.Len(t, collectSnapshot(t, w.Frames), 2)

	require.NoError(t, s.store.Close())
	s.notify(messagesKey(c.ID))

	testutil.WaitClosed(t, w.Frames, "the failed watch")
	assert.ErrorContains(t, w.Err(), "list messages")
}

// A snapshot the watch cannot read fails it rather than sending an empty list.
func TestAWatchThatCannotReadItsSnapshotFails(t *testing.T) {
	t.Parallel()

	s := newTestService(t)
	require.NoError(t, s.store.Close())

	list, err := s.WatchCollection(t.Context())
	require.NoError(t, err)
	testutil.WaitClosed(t, list.Frames, "the failed list watch")
	assert.ErrorContains(t, list.Err(), "list chats")

	msgs, err := s.WatchMessageCollection(t.Context(), ChatID(appdb.NewID()))
	require.NoError(t, err)
	testutil.WaitClosed(t, msgs.Frames, "the failed messages watch")
	assert.ErrorContains(t, msgs.Err(), "list messages")
}

// A consumer that stops reading ends its own pump wherever it had got to: the
// snapshot, the bookmark, or a change above it. One cancel per frame the pump would
// send, so each send site is the one that finds the consumer gone.
func TestAWatchEndsWhereverItsConsumerLeft(t *testing.T) {
	t.Parallel()

	s := newTestService(t)
	now := time.UnixMilli(1_000).UTC()
	c := seedChat(t, s.db, aChat("1", now))
	seedTurn(t, s.db, c.ID, now)

	for reads := range 4 {
		ctx, leave := context.WithCancel(t.Context())
		w, err := s.WatchMessageCollection(ctx, c.ID)
		require.NoError(t, err)
		for range reads {
			testutil.Recv(t, w.Frames, "a frame")
		}
		leave()
		testutil.WaitClosed(t, w.Frames, "the abandoned watch")
		assert.NoError(t, w.Err(), "a consumer leaving is not a failure")
	}
}

// The same for the list watch. Two chats, so a consumer that reads nothing leaves
// the pump inside the snapshot rather than at the bookmark.
func TestWatchCollectionEndsWhereverItsConsumerLeft(t *testing.T) {
	t.Parallel()

	s := newTestService(t)
	now := time.UnixMilli(1_000).UTC()
	seedChat(t, s.db, aChat("1", now))
	seedChat(t, s.db, aChat("1", now))

	for reads := range 4 {
		ctx, leave := context.WithCancel(t.Context())
		w, err := s.WatchCollection(ctx)
		require.NoError(t, err)
		for range reads {
			testutil.Recv(t, w.Frames, "a frame")
		}
		leave()
		testutil.WaitClosed(t, w.Frames, "the abandoned watch")
		assert.NoError(t, w.Err())
	}
}

// A change the consumer never reads ends its pump rather than blocking it forever.
// Each change is three frames and the consumer reads one: reading it proves the pump
// is inside the fold, the stream's one-slot buffer takes the second, and the pump is
// parked on the third when the consumer leaves. The rows are written straight to the
// store and announced once, so one fold carries all three.
func TestAChangeTheConsumerNeverReadsEndsThePump(t *testing.T) {
	t.Parallel()

	s := newTestService(t)
	now := time.UnixMilli(1_000).UTC()
	c := seedChat(t, s.db, aChat("1", now))
	addRows := func(t *testing.T) {
		for range 3 {
			_, err := s.db.Write.Exec(`INSERT INTO messages (id, chat_id, seq, role, content, created_at)
			VALUES (?, ?, (SELECT COALESCE(MAX(seq), -1) + 1 FROM messages WHERE chat_id = ?), 'user', '[]', ?)`,
				appdb.NewID(), string(c.ID), string(c.ID), millis(now))
			require.NoError(t, err)
		}
		s.notify(messagesKey(c.ID))
	}

	for _, change := range []struct {
		what string
		make func(*testing.T)
	}{
		{"rows appearing", addRows},
		{"the chat going", func(t *testing.T) {
			_, err := deleteChat(t.Context(), s.store.Stmts(), c.ID)
			require.NoError(t, err)
			s.notify(messagesKey(c.ID))
		}},
	} {
		t.Run(change.what, func(t *testing.T) {
			ctx, leave := context.WithCancel(t.Context())
			w, err := s.WatchMessageCollection(ctx, c.ID)
			require.NoError(t, err)
			collectSnapshot(t, w.Frames)

			change.make(t)
			testutil.Recv(t, w.Frames, "the first frame of the change")
			leave()
			testutil.WaitClosed(t, w.Frames, "the abandoned watch")
			assert.NoError(t, w.Err())
		})
	}
}

// The list watch folds a burst the same way, whether the chats changed or went.
func TestAListChangeTheConsumerNeverReadsEndsThePump(t *testing.T) {
	t.Parallel()

	s := newTestService(t)

	var ids []ChatID
	for i := range int64(3) {
		c := seedChat(t, s.db, aChat("1", time.UnixMilli(i).UTC()))
		ids = append(ids, c.ID)
	}

	for _, change := range []struct {
		what string
		make func(*testing.T)
	}{
		{"the chats renamed", func(t *testing.T) {
			for i, id := range ids {
				_, _, err := renameChat(t.Context(), s.store.Stmts(), id, "renamed", time.UnixMilli(int64(i)))
				require.NoError(t, err)
			}
			s.notify(chatsKey)
		}},
		{"the chats going", func(t *testing.T) {
			for _, id := range ids {
				_, err := deleteChat(t.Context(), s.store.Stmts(), id)
				require.NoError(t, err)
			}
			s.notify(chatsKey)
		}},
	} {
		t.Run(change.what, func(t *testing.T) {
			ctx, leave := context.WithCancel(t.Context())
			w, err := s.WatchCollection(ctx)
			require.NoError(t, err)
			require.Len(t, collectChatSnapshot(t, w.Frames), 3)

			change.make(t)
			testutil.Recv(t, w.Frames, "the first frame of the change")
			leave()
			testutil.WaitClosed(t, w.Frames, "the abandoned watch")
			assert.NoError(t, w.Err())
		})
	}
}

// --- send ---

func TestSendWritesTheQuestionItsRunAndTheAnswer(t *testing.T) {
	t.Parallel()

	s := newTestService(t)

	msg := send(t, s, nil, "1", "what is a pod?\nsecond line")
	assert.Equal(t, StatusStreaming, msg.Status)
	assert.Equal(t, RoleAssistant, msg.Role)
	assert.Equal(t, int64(1), msg.Seq)
	assert.Equal(t, emptyContent, msg.Content)
	assert.Equal(t, []string{"fake", "fake", "high"}, []string{msg.ProviderID, msg.ModelID, msg.Effort})

	chats, err := s.List(t.Context())
	require.NoError(t, err)
	require.Len(t, chats, 1)
	assert.Equal(t, "what is a pod?", chats[0].Title, "the title is the message's first line")

	msgs, err := listMessages(t.Context(), s.store.Stmts(), msg.ChatID, testReaders)
	require.NoError(t, err)
	require.Len(t, msgs, 2)
	assert.Equal(t, RoleUser, msgs[0].Role)
	assert.Equal(t, StatusComplete, msgs[0].Status, "a user message is born complete")
	assert.Equal(t, marshalBlocks([]llm.Block{llm.ContextBlock(s.withWorkspace("", msg.ChatID)), llm.TextBlock("what is a pod?\nsecond line")}), msgs[0].Content)
	assert.Equal(t, msg.ID, msgs[1].ID)

	again, ok, err := answerByRequestKey(t.Context(), s.store.Stmts(), reqID("1"), testReaders)
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, msg.ID, again.ID)
	var appVersion string
	require.NoError(t, s.db.Read.QueryRow(`SELECT app_version FROM agent_runs WHERE id = ?`, string(msg.RunID)).Scan(&appVersion))
	assert.Equal(t, version.Version, appVersion)
	awaitSettled(t, s, msg.ChatID, msg.ID)
}

func TestSendRejectsAKeyThatIsNotAUUID(t *testing.T) {
	t.Parallel()

	s := newTestService(t)
	_, err := s.Send(t.Context(), nil, ModeChat, "1", SendSwitches{}, "fake", "fake", "high", "not-a-uuid", "hi", nil)
	assert.ErrorIs(t, err, ErrBadRequest)
	assert.Zero(t, tableCount(t, s.db, "messages"))
}

func TestSendRejectsAnEmptyRequestIDOrMessage(t *testing.T) {
	t.Parallel()

	s := newTestService(t)
	_, err := s.Send(t.Context(), nil, ModeChat, "1", SendSwitches{}, "fake", "fake", "high", "", "hi", nil)
	assert.ErrorIs(t, err, ErrBadRequest)
	_, err = s.Send(t.Context(), nil, ModeChat, "1", SendSwitches{}, "fake", "fake", "high", reqID("1"), "  \n ", nil)
	assert.ErrorIs(t, err, ErrBadRequest)
	assert.Zero(t, tableCount(t, s.db, "messages"))
}

func TestSendRejectsAModeThatIsNeitherConstant(t *testing.T) {
	t.Parallel()

	s := newTestService(t)
	_, err := s.Send(t.Context(), nil, Mode("panel"), "1", SendSwitches{}, "fake", "fake", "high", reqID("1"), "hi", nil)
	assert.ErrorIs(t, err, ErrBadRequest)
	assert.Zero(t, tableCount(t, s.db, "chats"))
}

func TestSendCapsTheTitleItDerives(t *testing.T) {
	t.Parallel()

	s := newTestService(t)
	msg := send(t, s, nil, "1", strings.Repeat("x", 200))
	c, ok, err := s.Get(t.Context(), msg.ChatID)
	require.NoError(t, err)
	require.True(t, ok)
	assert.Len(t, c.Title, maxTitleLen)
	awaitSettled(t, s, msg.ChatID, msg.ID)
}

// A rename holding a line break is refused, so a derived title holds none either.
func TestTitleFromEndsAtTheFirstLineBreak(t *testing.T) {
	t.Parallel()

	for _, lineBreak := range lineBreaks {
		assert.Equal(t, "ops", titleFrom(" ops"+string(lineBreak)+"west"), "%U", lineBreak)
	}
}

func TestSendCapsTheTitleAtACharacterBoundary(t *testing.T) {
	t.Parallel()

	s := newTestService(t)
	msg := send(t, s, nil, "1", strings.Repeat("集", 100))
	c, ok, err := s.Get(t.Context(), msg.ChatID)
	require.NoError(t, err)
	require.True(t, ok)
	assert.True(t, utf8.ValidString(c.Title))
	assert.Equal(t, maxTitleLen, utf8.RuneCountInString(c.Title))
	awaitSettled(t, s, msg.ChatID, msg.ID)
}

func TestSendSetsTheModeOnlyWhenItCreatesTheChat(t *testing.T) {
	t.Parallel()

	s := newTestService(t)
	first, err := s.Send(t.Context(), nil, ModeDashboard, "1", SendSwitches{}, "fake", "fake", "high", reqID("1"), "hi", nil)
	require.NoError(t, err)
	awaitSettled(t, s, first.ChatID, first.ID)

	_, err = s.Send(t.Context(), &first.ChatID, ModeChat, "1", SendSwitches{}, "fake", "fake", "high", reqID("2"), "and?", nil)
	require.NoError(t, err)

	c, ok, err := s.Get(t.Context(), first.ChatID)
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, ModeDashboard, c.Mode, "the chat keeps the mode it was made in")
}

func TestSendSetsTheClusterOnlyWhenItCreatesTheChat(t *testing.T) {
	t.Parallel()

	s := newTestService(t)
	first, err := s.Send(t.Context(), nil, ModeChat, "7", SendSwitches{}, "fake", "fake", "high", reqID("1"), "hi", nil)
	require.NoError(t, err)
	awaitSettled(t, s, first.ChatID, first.ID)

	_, err = s.Send(t.Context(), &first.ChatID, ModeChat, "8", SendSwitches{}, "fake", "fake", "high", reqID("2"), "and?", nil)
	require.NoError(t, err)

	c, ok, err := s.Get(t.Context(), first.ChatID)
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, apimeta.ClusterID("7"), c.ClusterID, "the chat keeps the cluster it was made under")
}

func TestSendRefusesAProviderTheServiceDoesNotHold(t *testing.T) {
	t.Parallel()

	s := newTestService(t)
	_, err := s.Send(t.Context(), nil, ModeChat, "1", SendSwitches{}, "nobody", "fake", "high", reqID("1"), "hi", nil)
	assert.ErrorIs(t, err, ErrBadRequest)
	assert.Zero(t, tableCount(t, s.db, "chats"))
}

func TestSendIntoAnUnknownChatIsChatGone(t *testing.T) {
	t.Parallel()

	s := newTestService(t)
	id := ChatID(appdb.NewID())
	_, err := s.Send(t.Context(), &id, ModeChat, "1", SendSwitches{}, "fake", "fake", "high", reqID("1"), "hi", nil)
	assert.ErrorIs(t, err, ErrChatGone)
	assert.Zero(t, tableCount(t, s.db, "messages"))
}

func TestSendIntoAMarkedClusterIsClusterGone(t *testing.T) {
	t.Parallel()

	s := newTestService(t)
	markCluster(t, s.db, "9")
	_, err := s.Send(t.Context(), nil, ModeChat, "9", SendSwitches{}, "fake", "fake", "high", reqID("1"), "hi", nil)
	assert.ErrorIs(t, err, ErrClusterGone)
	_, err = s.Send(t.Context(), nil, ModeChat, "no-such-cluster", SendSwitches{}, "fake", "fake", "high", reqID("2"), "hi", nil)
	assert.ErrorIs(t, err, ErrClusterGone)
	assert.Zero(t, tableCount(t, s.db, "chats"))
}

// runDialect is a run's stored dialect.
func runDialect(t *testing.T, db *appdb.DB, id RunID) string {
	t.Helper()
	var d string
	require.NoError(t, db.Read.QueryRow(`SELECT dialect FROM agent_runs WHERE id = ?`, string(id)).Scan(&d))
	return d
}

// Each run records the dialect of the provider it was sent to, which reads its
// stored content after that provider has left the catalog: a chat's run, and a
// subagent's under it.
func TestARunRecordsItsDialect(t *testing.T) {
	t.Parallel()

	s := startServiceWithAgent(t)
	subagentFake(s, "Count the pods.")
	fakeOf(s).SetToolCalls(agentCall("Count the pods."))

	msg := send(t, s, nil, "k", "how many pods?")
	awaitSettled(t, s, msg.ChatID, msg.ID)
	awaitTasks(t, s, msg.ChatID)

	runs := subagentRuns(t, s.db, msg.RunID)
	require.Len(t, runs, 1)
	assert.Equal(t, string(llm.DialectFake), runDialect(t, s.db, msg.RunID))
	assert.Equal(t, string(llm.DialectFake), runDialect(t, s.db, runs[0].id))
}

// A send names what it runs on, and the catalog is the authority: a model the
// provider does not hold, or an effort the model does not list, is a bad request
// with nothing written. The replay lookup comes first, so a retry of a send that
// already ran is answered whatever the catalog holds now.
func TestSendRefusesAnEffortTheModelDoesNotList(t *testing.T) {
	t.Parallel()

	s := newTestService(t)

	for _, ref := range [][3]string{{"nobody", "fake", "high"}, {"fake", "other", "high"}, {"fake", "fake", "max"}} {
		_, err := s.Send(t.Context(), nil, ModeChat, "1", SendSwitches{}, ref[0], ref[1], ref[2], reqID("1"), "hi", nil)
		assert.ErrorIs(t, err, ErrBadRequest, ref)
	}
	assert.Zero(t, tableCount(t, s.db, "messages"))

	first := send(t, s, nil, "1", "hi")
	_, err := s.Send(t.Context(), nil, ModeChat, "1", SendSwitches{}, "fake", "fake", "max", reqID("1"), "hi", nil)
	require.NoError(t, err)
	awaitSettled(t, s, first.ChatID, first.ID)
}

// A chat moves to a provider of another dialect: the send is accepted, the
// history reaches the target with each row still naming the provider and the
// dialect that wrote it, which is what makes a wire strip another's payloads, and
// the new run records the target's dialect.
func TestAChatMovesAcrossDialects(t *testing.T) {
	t.Parallel()

	s := newTestService(t)
	now := time.UnixMilli(1_000).UTC()
	c := seedChat(t, s.db, aChat("1", now))
	turn := seedTurn(t, s.db, c.ID, now)
	_, err := s.db.Write.Exec(`UPDATE agent_runs SET provider = 'messages', dialect = ? WHERE id = ?`, string(llm.DialectMessages), string(turn.Run))
	require.NoError(t, err)
	thinking := llm.ThinkingBlock("Count them.")
	thinking.Payload = json.RawMessage(`{"type":"thinking","thinking":"Count them.","signature":"c2ln"}`)
	answer := marshalBlocks([]llm.Block{thinking, llm.TextBlock("Two pods.")})
	_, err = s.db.Write.Exec(`UPDATE messages SET content = ? WHERE id = ?`, string(answer), string(turn.Assistant))
	require.NoError(t, err)
	settleSeededRun(t, s.db, turn.Run, RunSucceeded, now)

	sent, err := s.Send(t.Context(), &c.ID, ModeChat, "1", SendSwitches{}, "fake", "fake", "high", reqID("1"), "and nodes?", nil)
	require.NoError(t, err)
	got := awaitSettled(t, s, c.ID, sent.ID)

	assert.Equal(t, StatusComplete, got.Status)
	history := fakeOf(s).LastRequest().Messages
	require.GreaterOrEqual(t, len(history), 2)
	assert.Equal(t, "messages", history[1].ProviderID)
	assert.Equal(t, llm.DialectMessages, history[1].Dialect)
	assert.Equal(t, "Two pods.", llm.Prompt(history[1].Blocks))
	assert.Equal(t, "fake", got.ProviderID)
	assert.Equal(t, string(llm.DialectFake), runDialect(t, s.db, got.RunID))
}

// A send whose rows will not go in writes nothing and leaves the chat free: the
// turn was reserved before the insert, and a slot left held would refuse every
// later send on that chat as one already in flight. The clash here is the request
// key's unique index, reached by a question whose answer the replay lookup cannot
// find.
func TestASendWhoseRowsCannotBeWrittenReleasesItsTurn(t *testing.T) {
	t.Parallel()

	s := newTestService(t)
	now := time.UnixMilli(1_000).UTC()
	c := seedChat(t, s.db, aChat("1", now))
	_, err := s.db.Write.Exec(`INSERT INTO messages (id, chat_id, seq, role, content, request_key, created_at)
		VALUES (?, ?, 0, 'user', '[]', ?, 0)`, appdb.NewID(), string(c.ID), reqID("1"))
	require.NoError(t, err)

	_, err = s.Send(t.Context(), &c.ID, ModeChat, "1", SendSwitches{}, "fake", "fake", "high", reqID("1"), "hi", nil)

	assert.Error(t, err)
	assert.Nil(t, s.turnOf(c.ID))
	assert.Equal(t, 1, tableCount(t, s.db, "messages"))
	assert.Zero(t, tableCount(t, s.db, "agent_runs"))
}

func TestASecondSendIsRefusedWhileATurnIsInFlight(t *testing.T) {
	t.Parallel()

	s := newTestService(t)
	gate := make(chan struct{})
	fakeOf(s).SetGate(gate)
	first := send(t, s, nil, "1", "hi")

	_, err := s.Send(t.Context(), &first.ChatID, ModeChat, "1", SendSwitches{}, "fake", "fake", "high", reqID("2"), "and?", nil)

	assert.ErrorIs(t, err, ErrTurnInFlight)
	assert.Equal(t, 2, tableCount(t, s.db, "messages"))
	close(gate)
	awaitSettled(t, s, first.ChatID, first.ID)
}

// The answer grows on the watch, then settles: Complete, with the fake's sentence
// and its stop reason, and the stored row says the same. The list moved with it.
func TestASendStreamsItsAnswerToTheWatch(t *testing.T) {
	t.Parallel()

	s := newTestService(t)
	older := seedChat(t, s.db, aChat("1", time.UnixMilli(1_000).UTC()))
	gate := make(chan struct{})
	fakeOf(s).SetGate(gate)
	// The watch is on the list first: the chat does not exist until the send.
	list, err := s.WatchCollection(t.Context())
	require.NoError(t, err)
	collectChatSnapshot(t, list.Frames)
	msg := send(t, s, nil, "1", "hi")
	awaitFrame(t, list.Frames, func(f ChatCollectionWatchFrame) bool {
		return f.Type == CollectionWatchFrameAdded && f.Chat.ID == msg.ChatID
	})
	st, err := s.WatchMessageCollection(t.Context(), msg.ChatID)
	require.NoError(t, err)
	// The first chunk may already be in the snapshot, or arrive as a Modified.
	partial := awaitFrame(t, st.Frames, func(f ChatMessageCollectionWatchFrame) bool {
		return f.Message != nil && f.Message.ID == msg.ID && f.Message.Content != emptyContent
	})
	assert.Equal(t, StatusStreaming, partial.Message.Status)
	close(gate)

	done := awaitFrame(t, st.Frames, func(f ChatMessageCollectionWatchFrame) bool {
		return f.Message != nil && f.Message.ID == msg.ID && !f.Message.Status.inFlight()
	}).Message
	assert.Equal(t, StatusComplete, done.Status)
	assert.Equal(t, "end_turn", done.FinishReason)
	assert.True(t, done.FinishedAt.Valid)
	assert.Equal(t, marshalBlocks(llm.AnswerBlocks(fakeThought, fakeSentence)), done.Content)
	stored, err := listMessages(t.Context(), s.store.Stmts(), msg.ChatID, testReaders)
	require.NoError(t, err)
	assert.Equal(t, *done, stored[1])

	chats, err := s.List(t.Context())
	require.NoError(t, err)
	assert.Equal(t, []ChatID{msg.ChatID, older.ID}, []ChatID{chats[0].ID, chats[1].ID})
}

// The model is shown the system prompt and the chat so far: the earlier question
// and answer, then the new question — never the empty answer being written.
func TestTheFakeIsAskedTheChatsHistory(t *testing.T) {
	t.Parallel()

	s := newTestService(t)
	first := send(t, s, nil, "1", "hi")
	awaitSettled(t, s, first.ChatID, first.ID)
	second := send(t, s, &first.ChatID, "2", "and?")
	awaitSettled(t, s, first.ChatID, second.ID)

	req := fakeOf(s).LastRequest()
	assert.Equal(t, "fake", req.Model.ID)
	assert.True(t, strings.HasPrefix(req.SystemPrompt, promptSystem), "chat's own prompt opens what the model reads")
	assert.Equal(t, []llm.Message{
		{Role: "user", Blocks: []llm.Block{llm.ContextBlock(s.withWorkspace("", first.ChatID)), llm.TextBlock("hi")}},
		{Role: "assistant", Blocks: llm.AnswerBlocks(fakeThought, fakeSentence), ProviderID: "fake", Dialect: llm.DialectFake, Effort: "high"},
		{Role: "user", Blocks: []llm.Block{llm.TextBlock("and?")}},
	}, req.Messages)
}

// A vendor that routes by a key keeps a chat's turns on the machine holding its
// prefix, so every request of every turn names the chat.
func TestATurnCarriesTheChatIDAsItsAffinityKey(t *testing.T) {
	t.Parallel()

	s := newTestService(t)
	first := send(t, s, nil, "1", "hi")
	awaitSettled(t, s, first.ChatID, first.ID)
	second := send(t, s, &first.ChatID, "2", "and?")
	awaitSettled(t, s, first.ChatID, second.ID)

	reqs := fakeOf(s).Requests()
	require.Len(t, reqs, 2)
	for _, req := range reqs {
		assert.Equal(t, string(first.ChatID), req.AffinityKey)
	}
}

// Each answer row says what wrote it and whether it thought, off its run; a
// question has no run and says nothing.
func TestHistoryNamesEachAnswersProviderAndEffort(t *testing.T) {
	t.Parallel()

	s := newTestService(t)
	first := send(t, s, nil, "1", "hi")
	awaitSettled(t, s, first.ChatID, first.ID)
	second := send(t, s, &first.ChatID, "2", "and?")
	awaitSettled(t, s, first.ChatID, second.ID)

	msgs := fakeOf(s).LastRequest().Messages
	require.Len(t, msgs, 3)
	assert.Equal(t, []string{"", "fake", ""}, []string{msgs[0].ProviderID, msgs[1].ProviderID, msgs[2].ProviderID})
	assert.Equal(t, []string{"", "high", ""}, []string{msgs[0].Effort, msgs[1].Effort, msgs[2].Effort})
}

func TestARetriedSendStartsNothing(t *testing.T) {
	t.Parallel()

	s := newTestService(t)
	first := send(t, s, nil, "1", "hi")
	awaitSettled(t, s, first.ChatID, first.ID)

	again := send(t, s, nil, "1", "hi")

	assert.Equal(t, first.ID, again.ID)
	assert.Equal(t, StatusComplete, again.Status, "the answer as it stands, not a new turn")
	assert.Equal(t, 1, fakeOf(s).Asked())
	assert.Equal(t, 2, tableCount(t, s.db, "messages"))
}

func TestARetryIgnoresItsOtherArguments(t *testing.T) {
	t.Parallel()

	s := newTestService(t)
	first := send(t, s, nil, "1", "hi")
	awaitSettled(t, s, first.ChatID, first.ID)
	other := ChatID(appdb.NewID())

	again, err := s.Send(t.Context(), &other, ModeDashboard, "8", SendSwitches{}, "other", "other", "low", reqID("1"), "something else", nil)

	require.NoError(t, err)
	assert.Equal(t, first.ID, again.ID)
	assert.Equal(t, 1, tableCount(t, s.db, "chats"))
}

// A read mid-answer, the fake gated inside its thought, sees the thinking so far
// and no text: the overlay is what fills the disclosure while the model is silent.
func TestTheLiveOverlayCarriesTheThinkingAheadOfTheText(t *testing.T) {
	t.Parallel()

	s := newTestService(t)
	gate := make(chan struct{})
	fakeOf(s).SetGate(gate)
	t.Cleanup(func() { close(gate) })
	msg := send(t, s, nil, "1", "hi")

	live := awaitLiveContent(t, s, msg.ChatID)

	assert.Equal(t, fakeFirstWord, live.Thinking())
	assert.Equal(t, marshalBlocks([]llm.Block{llm.ThinkingBlock(fakeFirstWord)}), live.Content)
}

// The turn stores the thinking ahead of the text, and the stored row reads the
// thought back.
func TestACompletedTurnStoresTheThinkingAheadOfTheText(t *testing.T) {
	t.Parallel()

	s := newTestService(t)
	msg := send(t, s, nil, "1", "hi")

	got := awaitSettled(t, s, msg.ChatID, msg.ID)

	assert.Equal(t, StatusComplete, got.Status)
	assert.Equal(t, marshalBlocks([]llm.Block{llm.ThinkingBlock(fakeThought), llm.TextBlock(fakeSentence)}), got.Content)
	assert.Equal(t, fakeThought, got.Thinking())
}

// A retry landing mid-answer carries the content so far: the overlay, not the
// empty stored row.
func TestARetryMidAnswerCarriesTheLiveText(t *testing.T) {
	t.Parallel()

	s := newTestService(t)
	gate := make(chan struct{})
	fakeOf(s).SetGate(gate)
	first := send(t, s, nil, "1", "hi")
	awaitLiveContent(t, s, first.ChatID)

	again := send(t, s, nil, "1", "hi")

	assert.Equal(t, first.ID, again.ID)
	assert.NotEqual(t, emptyContent, again.Content)
	assert.Equal(t, StatusStreaming, again.Status)
	close(gate)
	awaitSettled(t, s, first.ChatID, first.ID)
}

// The key went with the chat: a retry naming the deleted chat is refused, and a
// create-shaped one is a fresh send.
func TestARetryAfterADeleteIsAFreshSend(t *testing.T) {
	t.Parallel()

	s := newTestService(t)
	first := send(t, s, nil, "1", "hi")
	awaitSettled(t, s, first.ChatID, first.ID)
	require.NoError(t, s.Delete(t.Context(), first.ChatID))

	_, err := s.Send(t.Context(), &first.ChatID, ModeChat, "1", SendSwitches{}, "fake", "fake", "high", reqID("1"), "hi", nil)
	assert.ErrorIs(t, err, ErrChatGone)

	again := send(t, s, nil, "1", "hi")
	assert.NotEqual(t, first.ChatID, again.ChatID)
	awaitSettled(t, s, again.ChatID, again.ID)
	assert.Equal(t, 1, tableCount(t, s.db, "chats"))
}

func TestCancelKeepsThePartialAnswer(t *testing.T) {
	t.Parallel()

	s := newTestService(t)
	fakeOf(s).SetGate(make(chan struct{}))
	msg := send(t, s, nil, "1", "hi")
	awaitLiveContent(t, s, msg.ChatID)

	require.NoError(t, s.Cancel(t.Context(), msg.ChatID))

	got := awaitSettled(t, s, msg.ChatID, msg.ID)
	assert.Equal(t, StatusCancelled, got.Status)
	assert.Equal(t, marshalBlocks([]llm.Block{llm.ThinkingBlock(fakeFirstWord)}), got.Content)
	assert.Empty(t, got.FinishReason)
	assert.Equal(t, RunCancelled, runStatusOf(t, s.db, msg.RunID))
	assert.Equal(t, llmCallRow{err: callCancelled, finished: true}, llmCallOf(t, s.db, msg.RunID))
}

func TestCancelOfAnIdleChatIsANoOp(t *testing.T) {
	t.Parallel()

	s := newTestService(t)
	assert.NoError(t, s.Cancel(t.Context(), ChatID(appdb.NewID())))
}

// A cancel that lands before the goroutine claims its run: the run settles
// cancelled from queued, and the fake was never asked.
func TestACancelBeforeTheClaimRunsNothing(t *testing.T) {
	t.Parallel()

	s := newTestService(t)
	// The turn is reserved and cancelled by hand, ahead of its goroutine.
	c := seedChat(t, s.db, aChat("1", time.UnixMilli(1_000).UTC()))
	turn := seedTurn(t, s.db, c.ID, time.UnixMilli(1_000).UTC())
	tr, err := s.reserveTurn(c.ID, turn.Run, fakeTarget(s))
	require.NoError(t, err)
	tr.cancel()
	msgs, err := listMessages(t.Context(), s.store.Stmts(), c.ID, testReaders)
	require.NoError(t, err)

	s.startTurn(tr, msgs[1])
	testutil.Wait(t, tr.done, "the turn to end")

	assert.Equal(t, RunCancelled, runStatusOf(t, s.db, turn.Run))
	assert.Zero(t, fakeOf(s).Asked())
	assert.Zero(t, tableCount(t, s.db, "llm_calls"), "no call was opened")
}

// A stream that panics fails its turn alone: the answer settles failed with the
// panic as its error, the call row closes, and the chat's slot is released.
func TestAPanickingStreamFailsItsTurnAlone(t *testing.T) {
	t.Parallel()

	s := newTestService(t)
	fakeOf(s).PanicNext("the sdk blew up")
	msg := send(t, s, nil, "1", "hi")

	got := awaitSettled(t, s, msg.ChatID, msg.ID)

	assert.Equal(t, StatusFailed, got.Status)
	assert.Equal(t, "panic: the sdk blew up", got.Error)
	assert.Equal(t, llmCallRow{err: "panic: the sdk blew up", finished: true}, llmCallOf(t, s.db, msg.RunID))
	assert.Nil(t, s.turnOf(msg.ChatID))
}

func TestAStreamFailureKeepsThePartialAnswer(t *testing.T) {
	t.Parallel()

	s := newTestService(t)
	fakeOf(s).FailAfter(1, errors.New("provider went away"))
	msg := send(t, s, nil, "1", "hi")

	got := awaitSettled(t, s, msg.ChatID, msg.ID)

	assert.Equal(t, StatusFailed, got.Status)
	assert.Equal(t, "provider went away", got.Error)
	assert.Equal(t, marshalBlocks([]llm.Block{llm.ThinkingBlock(fakeFirstWord)}), got.Content)
	assert.Equal(t, llmCallRow{err: "provider went away", finished: true}, llmCallOf(t, s.db, msg.RunID))
}

// A run that is not queued when the goroutine claims it — here settled by hand
// under the send — asks no model and settles failed.
func TestARefusedClaimAsksNoModel(t *testing.T) {
	t.Parallel()

	s := newTestService(t)
	c := seedChat(t, s.db, aChat("1", time.UnixMilli(1_000).UTC()))
	turn := seedTurn(t, s.db, c.ID, time.UnixMilli(1_000).UTC())
	setRunStatus(t, s.db, turn.Run, RunRunning)
	tr, err := s.reserveTurn(c.ID, turn.Run, fakeTarget(s))
	require.NoError(t, err)
	msgs, err := listMessages(t.Context(), s.store.Stmts(), c.ID, testReaders)
	require.NoError(t, err)

	s.startTurn(tr, msgs[1])
	testutil.Wait(t, tr.done, "the turn to end")

	assert.Equal(t, RunFailed, runStatusOf(t, s.db, turn.Run))
	assert.Zero(t, fakeOf(s).Asked())
	msgs, err = listMessages(t.Context(), s.store.Stmts(), c.ID, testReaders)
	require.NoError(t, err)
	assert.ErrorContains(t, errors.New(msgs[1].Error), "not queued")
}

// The completion ping finds the settled row: the overlay is gone by the time the
// watcher re-reads, so the frame it folds is Complete, and no ping follows.
func TestATurnEndsWriteThenReleaseThenNotify(t *testing.T) {
	t.Parallel()

	s := newTestService(t)
	msg := send(t, s, nil, "1", "hi")
	awaitTurnReleased(t, s, msg.ChatID)

	assert.Nil(t, s.turnOf(msg.ChatID), "the slot is released once the turn ends")
	got := awaitSettled(t, s, msg.ChatID, msg.ID)
	assert.Equal(t, StatusComplete, got.Status)
}

// A delete mid-turn cancels the turn and joins it, so the settle lands before the
// rows go and nothing is written to a deleted chat.
func TestDeleteMidTurnCancelsAndJoinsIt(t *testing.T) {
	t.Parallel()

	s := newTestService(t)
	fakeOf(s).SetGate(make(chan struct{}))
	msg := send(t, s, nil, "1", "hi")
	awaitLiveContent(t, s, msg.ChatID)
	tr := s.turnOf(msg.ChatID)

	require.NoError(t, s.Delete(t.Context(), msg.ChatID))

	testutil.Wait(t, tr.done, "the turn to have ended")
	_, ok, err := s.Get(t.Context(), msg.ChatID)
	require.NoError(t, err)
	assert.False(t, ok)
	assert.Zero(t, tableCount(t, s.db, "messages"))
	assert.Zero(t, tableCount(t, s.db, "llm_calls"))
}

// A send between a delete's join and its write is refused: the chat is going, and a
// turn reserved in that gap would run on rows that are about to vanish.
func TestASendDuringADeleteIsRefused(t *testing.T) {
	t.Parallel()

	s := newTestService(t)
	msg := send(t, s, nil, "1", "hi")
	awaitSettled(t, s, msg.ChatID, msg.ID)
	paused, resume := make(chan struct{}), make(chan struct{})
	s.deleteWrite = func(ctx context.Context, id ChatID) (bool, error) {
		close(paused)
		<-resume
		return s.deleteRow(ctx, id)
	}
	deleted := make(chan error, 1)
	go func() { deleted <- s.Delete(t.Context(), msg.ChatID) }()
	testutil.Wait(t, paused, "the delete to reach its write")

	_, err := s.Send(t.Context(), &msg.ChatID, ModeChat, "1", SendSwitches{}, "fake", "fake", "high", reqID("2"), "and?", nil)

	assert.ErrorIs(t, err, ErrChatGone)
	close(resume)
	require.NoError(t, testutil.Recv(t, deleted, "the delete to finish"))
	assert.Nil(t, s.turnOf(msg.ChatID))
	assert.Zero(t, tableCount(t, s.db, "messages"))
	assert.Equal(t, 1, fakeOf(s).Asked(), "no turn ran in the gap")
}

// Stop cancels every turn and waits for its settle.
func TestStopCancelsTheTurnsAndSettlesThem(t *testing.T) {
	t.Parallel()

	s, err := newService(openTestDB(t, t.TempDir()), chatsDirIn(t.TempDir()), monitorDirIn(t.TempDir()), fakeLLM(), noClusterCards, nil, testReaders, noLists, sandbox.State{}, testSettings(t), nil)
	require.NoError(t, err)
	stop, err := s.Start(t.Context())
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, s.Close()) })
	fakeOf(s).SetGate(make(chan struct{}))
	msg := send(t, s, nil, "1", "hi")
	awaitLiveContent(t, s, msg.ChatID)

	require.NoError(t, stop(t.Context()))

	msgs, err := listMessages(t.Context(), s.store.Stmts(), msg.ChatID, testReaders)
	require.NoError(t, err)
	assert.Equal(t, StatusCancelled, msgs[1].Status)
	assert.Equal(t, marshalBlocks([]llm.Block{llm.ThinkingBlock(fakeFirstWord)}), msgs[1].Content)
	assert.Equal(t, llmCallRow{err: callCancelled, finished: true}, llmCallOf(t, s.db, msg.RunID))
}

func TestASendAfterARestartContinuesTheSeq(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	first, err := newService(openTestDB(t, dir), chatsDirIn(dir), monitorDirIn(dir), fakeLLM(), noClusterCards, nil, testReaders, noLists, sandbox.State{}, testSettings(t), nil)
	require.NoError(t, err)
	stop, err := first.Start(t.Context())
	require.NoError(t, err)
	msg := send(t, first, nil, "1", "hi")
	awaitSettled(t, first, msg.ChatID, msg.ID)
	require.NoError(t, stop(t.Context()))
	require.NoError(t, first.Close())

	s := startService(t, dir)
	next := send(t, s, &msg.ChatID, "2", "and?")

	assert.Equal(t, int64(3), next.Seq)
	awaitSettled(t, s, msg.ChatID, next.ID)
}

// A call a previous process left open is closed at the next start, as stranded.
func TestStartClosesTheCallsAPreviousRunLeftOpen(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	db := openTestDB(t, dir)
	now := time.UnixMilli(1_000).UTC()
	c := seedChat(t, db, aChat("1", now))
	turn := seedTurn(t, db, c.ID, now)
	setRunStatus(t, db, turn.Run, RunRunning)
	_, err := db.Write.Exec(`INSERT INTO llm_calls (id, run_id, seq, provider, model, started_at) VALUES (?, ?, 0, 'fake', 'fake', 0)`, appdb.NewID(), string(turn.Run))
	require.NoError(t, err)
	require.NoError(t, db.Close())

	s := startService(t, dir)

	assert.Equal(t, llmCallRow{err: llmCallStranded, finished: true}, llmCallOf(t, s.db, turn.Run))
}

// A retry that gets past the replay lookup while the first attempt is still writing is
// caught by the second lookup, the one inside the transaction: it answers with the row
// that attempt wrote rather than asking the question twice.
func TestARetryRacingTheFirstAttemptIsAnsweredInsideTheTransaction(t *testing.T) {
	t.Parallel()

	s := newTestService(t)
	now := time.UnixMilli(1_000).UTC()
	c := seedChat(t, s.db, aChat("1", now))

	// The clock is read between the send's own lookup, which finds nothing, and its
	// transaction, so parking there is parking in the window the race needs.
	var once sync.Once
	parked, release := make(chan struct{}), make(chan struct{})
	s.now = func() time.Time {
		once.Do(func() { close(parked); <-release })
		return now
	}
	sent := make(chan ChatMessage, 1)
	go func() {
		msg, err := s.Send(t.Context(), &c.ID, ModeChat, "1", SendSwitches{}, "fake", "fake", "high", reqID("1"), "hi", nil)
		assert.NoError(t, err)
		sent <- msg
	}()
	testutil.Wait(t, parked, "the send to reach its transaction")

	turn := seedTurn(t, s.db, c.ID, now)
	_, err := s.db.Write.Exec(`UPDATE messages SET request_key = ? WHERE id = ?`, reqID("1"), string(turn.User))
	require.NoError(t, err)
	close(release)

	assert.Equal(t, turn.Assistant, testutil.Recv(t, sent, "the send").ID)
	assert.Equal(t, 2, tableCount(t, s.db, "messages"), "the retry wrote nothing")
	assert.Zero(t, fakeOf(s).Asked())
}

// A send whose store fails at any step of its transaction writes nothing and leaves the
// chat free: the turn is reserved before the rows go in, and a slot left held would
// refuse every later send on that chat as one already in flight. Each case fails one
// step — a read by giving it a shape no scanner takes, a write with a trigger.
func TestASendWhoseStoreFailsWritesNothingAndLeavesTheChatFree(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name    string
		id      stmtID
		swap    string
		trigger string
		snap    *Snapshot
	}{
		{name: "the chat read", id: stmtSelectChat, swap: `SELECT 1, 2`},
		{name: "the cluster check", id: stmtSelectClusterAccepts, swap: `SELECT 1, 2`},
		{name: "the view's call", id: stmtSelectViewCall, swap: `SELECT 1, 2`, snap: &Snapshot{CallID: "call"}},
		{name: "the newest context", id: stmtSelectNewestContext, swap: `SELECT 1, 2`},
		{name: "the next seq", id: stmtNextSeq, swap: `SELECT 1, 2`},
		{name: "the chat's touch", trigger: `CREATE TRIGGER refuse BEFORE UPDATE ON chats BEGIN SELECT RAISE(ABORT, 'refused'); END`},
		{name: "the run insert", trigger: `CREATE TRIGGER refuse BEFORE INSERT ON agent_runs BEGIN SELECT RAISE(ABORT, 'refused'); END`},
		{name: "the answer insert", trigger: `CREATE TRIGGER refuse BEFORE INSERT ON messages WHEN NEW.role = 'assistant' BEGIN SELECT RAISE(ABORT, 'refused'); END`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			table := statements
			if tc.swap != "" {
				table = swapped(map[stmtID]string{tc.id: tc.swap})
			}
			s := startServiceOn(t, table)
			c := seedChat(t, s.db, aChat("1", time.UnixMilli(1_000).UTC()))
			if tc.trigger != "" {
				_, err := s.db.Write.Exec(tc.trigger)
				require.NoError(t, err)
			}

			_, err := s.Send(t.Context(), &c.ID, ModeChat, "1", SendSwitches{}, "fake", "fake", "high", reqID("1"), "hi", tc.snap)

			assert.Error(t, err)
			assert.NotErrorIs(t, err, ErrBadRequest)
			assert.Nil(t, s.turnOf(c.ID))
			assert.Zero(t, tableCount(t, s.db, "messages"))
			assert.Zero(t, tableCount(t, s.db, "agent_runs"))
		})
	}
}

// A rename the store refuses is the store's error, not a missing chat: the row is
// still there and the caller is not told it is gone.
func TestARenameTheStoreRefusesIsNotAMissingChat(t *testing.T) {
	t.Parallel()

	s := newTestService(t)
	c := seedChat(t, s.db, aChat("1", time.UnixMilli(1_000).UTC()))
	_, err := s.db.Write.Exec(`CREATE TRIGGER refuse BEFORE UPDATE ON chats BEGIN SELECT RAISE(ABORT, 'refused'); END`)
	require.NoError(t, err)

	_, err = s.Rename(t.Context(), c.ID, "renamed")

	assert.Error(t, err)
	assert.NotErrorIs(t, err, ErrChatGone)
}

// A switch the store refuses is the store's error, not a missing chat, and
// nothing is written.
func TestASwitchTheStoreRefusesIsNotAMissingChat(t *testing.T) {
	t.Parallel()

	for name, flip := range map[string]func(s *service, id ChatID) error{
		"the sandbox": func(s *service, id ChatID) error {
			_, err := s.SetSandboxDisabled(t.Context(), id, true)
			return err
		},
		"the network": func(s *service, id ChatID) error {
			_, err := s.SetNetworkEnabled(t.Context(), id, true)
			return err
		},
		"the network off": func(s *service, id ChatID) error {
			_, err := s.SetNetworkEnabled(t.Context(), id, false)
			return err
		},
	} {
		t.Run(name, func(t *testing.T) {
			s := newTestService(t)
			s.sandboxState = sandbox.State{Available: true, NetworkAvailable: true}
			c := seedChat(t, s.db, aChat("1", time.UnixMilli(1_000).UTC()))
			refuse(t, s, "UPDATE", "chats")

			err := flip(s, c.ID)

			assert.ErrorContains(t, err, "refused")
			assert.NotErrorIs(t, err, ErrChatGone)
			stored, _, err := s.Get(t.Context(), c.ID)
			require.NoError(t, err)
			assert.False(t, stored.SandboxDisabled)
			assert.False(t, stored.NetworkEnabled)
		})
	}
}

// A network switch that cannot read the chat it checks writes nothing.
func TestANetworkSwitchThatCannotReadTheChatWritesNothing(t *testing.T) {
	t.Parallel()

	s := startServiceOn(t, swapped(map[stmtID]string{stmtSelectChat: `SELECT 1, 2`}))
	s.sandboxState = sandbox.State{Available: true, NetworkAvailable: true}
	c := seedChat(t, s.db, aChat("1", time.UnixMilli(1_000).UTC()))

	_, err := s.SetNetworkEnabled(t.Context(), c.ID, true)

	assert.Error(t, err)
	assert.NotErrorIs(t, err, ErrChatGone)
	var enabled bool
	require.NoError(t, s.db.Read.QueryRow(`SELECT network_enabled FROM chats WHERE id = ?`, string(c.ID)).Scan(&enabled))
	assert.False(t, enabled)
}

// A call that ran lands its row under the model call that asked, with what was
// asked, what came back, and both timestamps.
func TestACallThatRanLandsItsRow(t *testing.T) {
	t.Parallel()

	s := startServiceWithTool(t, testTool{name: "echo"})
	fakeOf(s).SetToolCalls(llm.StagedCall("echo", `{"say":"hi"}`))

	msg := send(t, s, nil, "1", "hi")
	awaitSettled(t, s, msg.ChatID, msg.ID)

	assert.Equal(t, []storedToolCall{{
		seq: 0, name: "echo", contract: "echo/v1", useID: "call-1", args: `{"say":"hi"}`, result: `{"say":"hi"}`,
		status: toolSucceeded, hasStarted: true, hasFinished: true, llmCallSeq: 0,
	}}, toolCallRows(t, s.db, msg.RunID))
}

// The running row is committed before the tool runs: the tool reads it from inside
// its own Run.
func TestARunningRowIsCommittedBeforeTheToolRuns(t *testing.T) {
	t.Parallel()

	var seen string
	var s *service
	s = startServiceWithTool(t, testTool{name: "echo", run: func(context.Context, json.RawMessage) (string, bool) {
		require.NoError(t, s.db.Read.QueryRow(`SELECT status FROM tool_calls`).Scan(&seen))
		return "ok", false
	}})
	fakeOf(s).SetToolCalls(llm.StagedCall("echo", `{}`))

	msg := send(t, s, nil, "1", "hi")
	awaitSettled(t, s, msg.ChatID, msg.ID)

	assert.Equal(t, toolRunning, seen)
}

// A refusal the loop wrote is a row of its own: failed, the code in error, and no
// started_at — the record of a call that never ran.
func TestARefusalTheLoopWroteLandsItsRow(t *testing.T) {
	t.Parallel()

	s := startServiceWithTool(t, testTool{name: "echo"})
	fakeOf(s).SetToolCalls(llm.StagedCall("nosuch", `{}`))

	msg := send(t, s, nil, "1", "hi")
	awaitSettled(t, s, msg.ChatID, msg.ID)

	assert.Equal(t, []storedToolCall{{
		seq: 0, name: "nosuch", useID: "call-1", args: `{}`, result: `{"error":"unknown-tool"}`, errText: `{"error":"unknown-tool"}`,
		status: toolFailed, hasFinished: true, llmCallSeq: 0,
	}}, toolCallRows(t, s.db, msg.RunID))
}

// A tool's own error is prose the model reads: the row keeps it in result, what the
// model read, and marks error with the tool's own marker rather than the text again.
func TestAToolsOwnErrorLandsAsAJSONObject(t *testing.T) {
	t.Parallel()

	s := startServiceWithTool(t, testTool{name: "echo", run: func(context.Context, json.RawMessage) (string, bool) {
		return "no such namespace", true
	}})
	fakeOf(s).SetToolCalls(llm.StagedCall("echo", `{}`))

	msg := send(t, s, nil, "1", "hi")
	awaitSettled(t, s, msg.ChatID, msg.ID)

	rows := toolCallRows(t, s.db, msg.RunID)
	require.Len(t, rows, 1)
	assert.Equal(t, toolFailed, rows[0].status)
	assert.Equal(t, "no such namespace", rows[0].result)
	assert.JSONEq(t, `{"error":"tool"}`, rows[0].errText)
}

// Each round is a call row of its own, numbered within the run, and the stop
// reason the wire serves is the last round's.
func TestATurnsModelCallsLandWithTheirSeq(t *testing.T) {
	t.Parallel()

	s := startServiceWithTool(t, testTool{name: "echo"})
	fakeOf(s).SetToolCalls(llm.StagedCall("echo", `{}`))

	msg := send(t, s, nil, "1", "hi")
	done := awaitSettled(t, s, msg.ChatID, msg.ID)

	assert.Equal(t, []llmCallRow{
		{stopReason: llm.StopToolUse, finished: true},
		{stopReason: "end_turn", finished: true},
	}, llmCallRows(t, s.db, msg.RunID))
	assert.Equal(t, "end_turn", done.FinishReason)
}

// A completed turn stores its rounds: the first reply, the call, its answer, then
// the reply that followed. The thinking reads across both replies.
func TestACompletedTurnStoresItsRounds(t *testing.T) {
	t.Parallel()

	s := startServiceWithTool(t, testTool{name: "echo"})
	fakeOf(s).SetToolCalls(llm.StagedCall("echo", `{"say":"hi"}`))

	msg := send(t, s, nil, "1", "hi")
	done := awaitSettled(t, s, msg.ChatID, msg.ID)

	assert.Equal(t, StatusComplete, done.Status)
	assert.Equal(t, marshalBlocks(append(firstRound(`{"say":"hi"}`), llm.AnswerBlocks(fakeSecondThought, fakeSecondSentence)...)), done.Content)
	assert.Equal(t, fakeThought+llm.ThinkingSeparator+fakeSecondThought, done.Thinking())
}

// firstRound is the fake's first reply with one echoed call answered after it.
func firstRound(input string) []llm.Block {
	return append(llm.AnswerBlocks(fakeThought, fakeSentence),
		llm.ToolUseBlock("call-1", "echo", []byte(input)),
		llm.ToolResultBlock("call-1", input, false))
}

// A payload is the provider's to read and nobody else's: neither the overlay a
// stream publishes, nor the row a watcher reads, nor the answer a retried send
// returns carries one, and the history the next send replays does.
func TestAPayloadStaysOutOfEveryRowAReaderGets(t *testing.T) {
	t.Parallel()

	gate := make(chan struct{})
	release := sync.OnceFunc(func() { close(gate) })
	t.Cleanup(release)
	var s *service
	s = startServiceWithTool(t, testTool{name: "echo", run: func(_ context.Context, input json.RawMessage) (string, bool) {
		fakeOf(s).SetGate(gate)
		return string(input), false
	}})
	call := llm.StagedCall("echo", `{}`)
	call.Payload = json.RawMessage(`{"type":"function_call","call_id":"call-1","status":"completed"}`)
	fakeOf(s).SetToolCalls(call)

	msg := send(t, s, nil, "1", "hi")
	mid := awaitContent(t, s, msg.ChatID, msg.ID,
		marshalBlocks(append(firstRound(`{}`), llm.ThinkingBlock(fakeSecondFirstWord))))
	assert.NotContains(t, string(mid.Content), `"payload"`, "the overlay is what a reader is shown")

	release()
	settled := awaitSettled(t, s, msg.ChatID, msg.ID)
	assert.Equal(t, StatusComplete, settled.Status)
	assert.NotContains(t, string(settled.Content), `"payload"`)
	again := send(t, s, nil, "1", "hi")
	assert.Equal(t, msg.ID, again.ID)
	assert.NotContains(t, string(again.Content), `"payload"`, "a retry answers with the row as a reader sees it")

	next := send(t, s, &msg.ChatID, "2", "and?")
	awaitSettled(t, s, msg.ChatID, next.ID)
	replayed := fakeOf(s).LastRequest().Messages[1].Blocks
	assert.Equal(t, call.Payload, replayed[2].Payload, "the replay keeps what the provider signed")
}

// A read mid-round sees the round whole: the tool arms the gate, so the second
// reply parks inside its thought with the call and its answer already in the
// overlay.
func TestAReadMidRoundSeesTheRound(t *testing.T) {
	t.Parallel()

	gate := make(chan struct{})
	var s *service
	s = startServiceWithTool(t, testTool{name: "echo", run: func(_ context.Context, input json.RawMessage) (string, bool) {
		fakeOf(s).SetGate(gate)
		return string(input), false
	}})
	t.Cleanup(func() { close(gate) })
	fakeOf(s).SetToolCalls(llm.StagedCall("echo", `{}`))
	msg := send(t, s, nil, "1", "hi")

	want := marshalBlocks(append(firstRound(`{}`), llm.ThinkingBlock(fakeSecondFirstWord)))
	live := awaitContent(t, s, msg.ChatID, msg.ID, want)

	assert.Equal(t, StatusStreaming, live.Status)
}

// A cancelled turn keeps its rounds in the row, and the next send shows the model
// that row's text alone: a row that did not settle Complete has no result for
// every call it asked.
// A row whose rounds are dropped loses its payloads with them: the Responses API
// refuses a reasoning item whose following item is gone, and a cancelled turn's
// row keeps a payload that reasoned toward a call the history no longer sends.
// Its text stays, since that is what the model reads.
func TestAnUnsettledRowsPayloadsGoWithItsRounds(t *testing.T) {
	t.Parallel()

	s := newTestService(t)
	chat := seedChat(t, s.db, aChat("1", s.now()))
	turn := seedTurn(t, s.db, chat.ID, s.now())
	thinking := llm.ThinkingBlock("I count them.")
	thinking.Payload = json.RawMessage(`{"id":"rs_1","type":"reasoning","encrypted_content":"gAAAA"}`)
	row := []llm.Block{
		thinking,
		llm.ToolUseBlock("call-1", "echo", json.RawMessage(`{}`)),
		llm.ToolResultBlock("call-1", "hi", false),
	}
	_, err := s.db.Write.Exec(`UPDATE messages SET content = ? WHERE id = ?`, string(marshalBlocks(row)), string(turn.Assistant))
	require.NoError(t, err)
	setRunStatus(t, s.db, turn.Run, RunCancelled)

	next := send(t, s, &chat.ID, "2", "and?")
	awaitSettled(t, s, chat.ID, next.ID)

	msgs := fakeOf(s).LastRequest().Messages
	require.Len(t, msgs, 3)
	assert.Equal(t, []llm.Block{llm.ThinkingBlock("I count them.")}, msgs[1].Blocks)
}

func TestACancelledTurnKeepsItsRoundsAndSendsItsTextAlone(t *testing.T) {
	t.Parallel()

	gate := make(chan struct{})
	var s *service
	s = startServiceWithTool(t, testTool{name: "echo", run: func(_ context.Context, input json.RawMessage) (string, bool) {
		fakeOf(s).SetGate(gate)
		return string(input), false
	}})
	t.Cleanup(func() { close(gate) })
	fakeOf(s).SetToolCalls(llm.StagedCall("echo", `{}`))
	msg := send(t, s, nil, "1", "hi")
	want := marshalBlocks(append(firstRound(`{}`), llm.ThinkingBlock(fakeSecondFirstWord)))
	awaitContent(t, s, msg.ChatID, msg.ID, want)

	require.NoError(t, s.Cancel(t.Context(), msg.ChatID))
	got := awaitSettled(t, s, msg.ChatID, msg.ID)

	assert.Equal(t, StatusCancelled, got.Status)
	assert.Equal(t, want, got.Content)

	next := send(t, s, &msg.ChatID, "2", "and?")
	awaitSettled(t, s, msg.ChatID, next.ID)
	assert.Equal(t, []llm.Message{
		{Role: "user", Blocks: []llm.Block{llm.ContextBlock(s.withWorkspace("", msg.ChatID)), llm.TextBlock("hi")}},
		{Role: "assistant", Blocks: []llm.Block{llm.ThinkingBlock(fakeThought), llm.TextBlock(fakeSentence), llm.ThinkingBlock(fakeSecondFirstWord)},
			ProviderID: "fake", Dialect: llm.DialectFake, Effort: "high"},
		{Role: "user", Blocks: []llm.Block{llm.TextBlock("and?")}},
	}, fakeOf(s).LastRequest().Messages)
}

// A finish write the store refused while the run was live is healed at settlement:
// the settle writes the run's status ahead of the rows, so the retry lands. The
// model call's row keeps its own stop reason, and the run carries the error.
func TestATransientFinishWriteIsHealedAtSettlement(t *testing.T) {
	t.Parallel()

	s := startServiceWithTool(t, testTool{name: "echo"})
	_, err := s.db.Write.Exec(`CREATE TRIGGER refuse BEFORE UPDATE ON tool_calls
		WHEN (SELECT r.status FROM agent_runs r JOIN llm_calls c ON c.run_id = r.id WHERE c.id = NEW.llm_call_id) = 'running'
		BEGIN SELECT RAISE(ABORT, 'refused'); END`)
	require.NoError(t, err)
	fakeOf(s).SetToolCalls(llm.StagedCall("echo", `{"say":"hi"}`))

	msg := send(t, s, nil, "1", "hi")
	got := awaitSettled(t, s, msg.ChatID, msg.ID)

	assert.Equal(t, StatusFailed, got.Status)
	assert.Contains(t, got.Error, "refused")
	assert.Equal(t, []storedToolCall{{
		seq: 0, name: "echo", contract: "echo/v1", useID: "call-1", args: `{"say":"hi"}`, result: `{"say":"hi"}`,
		status: toolSucceeded, hasStarted: true, hasFinished: true, llmCallSeq: 0,
	}}, toolCallRows(t, s.db, msg.RunID))
	assert.Equal(t, []llmCallRow{{stopReason: llm.StopToolUse, finished: true}}, llmCallRows(t, s.db, msg.RunID))
}

// A tool call a previous process left open is closed at the next start.
func TestStrandedToolCallsAreClosedOnStart(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	db := openTestDB(t, dir)
	now := time.UnixMilli(1_000).UTC()
	c := seedChat(t, db, aChat("1", now))
	turn := seedTurn(t, db, c.ID, now)
	setRunStatus(t, db, turn.Run, RunRunning)
	call := appdb.NewID()
	_, err := db.Write.Exec(`INSERT INTO llm_calls (id, run_id, seq, provider, model, started_at) VALUES (?, ?, 0, 'fake', 'fake', 0)`, call, string(turn.Run))
	require.NoError(t, err)
	_, err = db.Write.Exec(`INSERT INTO tool_calls (id, llm_call_id, seq, tool_name, contract_name, status, created_at, started_at) VALUES (?, ?, 0, 'echo', 'echo/v1', 'running', 0, 0)`, appdb.NewID(), call)
	require.NoError(t, err)
	require.NoError(t, db.Close())

	s := startService(t, dir)

	assert.Equal(t, []storedToolCall{{
		seq: 0, name: "echo", contract: "echo/v1", errText: toolCallStranded, status: toolFailed,
		hasStarted: true, hasFinished: true, llmCallSeq: 0,
	}}, toolCallRows(t, s.db, turn.Run))
}

// What the model is told ends with the agent's two sections: what it can do, then
// the standing rule. An empty box says the turn has no tools; a box with one
// carries that tool's own section.
// The catalog's word decides: a model that takes no tools is offered none and
// told so, whatever the box holds.
func TestAModelThatTakesNoToolsIsOfferedNone(t *testing.T) {
	t.Parallel()

	s := startServiceWithTool(t, testTool{name: "echo"})

	msg, err := s.Send(t.Context(), nil, ModeChat, "1", SendSwitches{}, "fake", "fake-no-tools", "", reqID("1"), "hi", nil)
	require.NoError(t, err)
	awaitSettled(t, s, msg.ChatID, msg.ID)

	req := fakeOf(s).LastRequest()
	assert.Empty(t, req.Tools)
	assert.Contains(t, req.SystemPrompt, "\n# What you can do\n\nYou have no tools on this turn:")
}

func TestTheSystemPromptSaysWhatTheModelCanDo(t *testing.T) {
	t.Parallel()

	t.Run("no tools", func(t *testing.T) {
		s := newTestService(t)
		msg := send(t, s, nil, "1", "hi")
		awaitSettled(t, s, msg.ChatID, msg.ID)

		prompt := fakeOf(s).LastRequest().SystemPrompt
		assert.Contains(t, prompt, "\n# What you can do\n\nYou have no tools on this turn:")
		assert.True(t, strings.HasSuffix(prompt, "a host or a remote the user did not name."))
	})

	t.Run("one tool", func(t *testing.T) {
		s := startServiceWithTool(t, testTool{name: "echo"})
		msg := send(t, s, nil, "1", "hi")
		awaitSettled(t, s, msg.ChatID, msg.ID)

		prompt := fakeOf(s).LastRequest().SystemPrompt
		assert.Contains(t, prompt, "\n## echo\n\nSays back what it is given.\n")
		assert.True(t, strings.HasSuffix(prompt, "a host or a remote the user did not name."))
	})
}

// A tool_calls write the store refuses runs nothing: the call is answered not-run,
// its own row cannot land either, and the settle that would heal it fails too — so
// the run is left for the next start to fail as stranded.
func TestAToolRowTheStoreRefusesRunsNothing(t *testing.T) {
	t.Parallel()

	ran := false
	s := startServiceWithTool(t, testTool{name: "echo", run: func(context.Context, json.RawMessage) (string, bool) {
		ran = true
		return "ok", false
	}})
	_, err := s.db.Write.Exec(`CREATE TRIGGER refuse BEFORE INSERT ON tool_calls BEGIN SELECT RAISE(ABORT, 'refused'); END`)
	require.NoError(t, err)
	fakeOf(s).SetToolCalls(llm.StagedCall("echo", `{}`))

	msg := send(t, s, nil, "1", "hi")
	testutil.Wait(t, s.turnOf(msg.ChatID).retrying, "the settle to retry")
	require.NoError(t, s.stop(t.Context()))

	assert.False(t, ran, "no tool runs behind a write that did not land")
	assert.Zero(t, tableCount(t, s.db, "tool_calls"))
	assert.True(t, messageStatusOf(runStatusOf(t, s.db, msg.RunID)).inFlight(),
		"the run is left for the next start to fail as stranded")
}

// A running row the store refused once is written again by the refusal that
// follows, under the same seq: one row for the call, and a settle that lands.
func TestAFailedStartWriteLeavesOneRowForTheCall(t *testing.T) {
	t.Parallel()

	ran := false
	s := startServiceWithTool(t, testTool{name: "echo", run: func(context.Context, json.RawMessage) (string, bool) {
		ran = true
		return "ok", false
	}})
	_, err := s.db.Write.Exec(`CREATE TRIGGER refuse BEFORE INSERT ON tool_calls WHEN NEW.status = 'running'
		BEGIN SELECT RAISE(ABORT, 'refused'); END`)
	require.NoError(t, err)
	fakeOf(s).SetToolCalls(llm.StagedCall("echo", `{}`))

	msg := send(t, s, nil, "1", "hi")
	got := awaitSettled(t, s, msg.ChatID, msg.ID)

	assert.False(t, ran)
	assert.Equal(t, StatusFailed, got.Status)
	assert.Equal(t, []storedToolCall{{
		seq: 0, name: "echo", contract: "echo/v1", useID: "call-1", args: `{}`, result: `{"error":"not-run"}`, errText: `{"error":"not-run"}`,
		status: toolFailed, hasFinished: true, llmCallSeq: 0,
	}}, toolCallRows(t, s.db, msg.RunID))
}

// A tool that panics fails the turn. The row keeps the reply that asked, and the
// settle closes the tool row the recorder never did: failed, interrupted, with its
// finish time, so nothing waits on a restart to close it.
func TestAToolThatPanicsClosesItsRow(t *testing.T) {
	t.Parallel()

	s := startServiceWithTool(t, testTool{name: "echo", run: func(context.Context, json.RawMessage) (string, bool) {
		panic("boom")
	}})
	fakeOf(s).SetToolCalls(llm.StagedCall("echo", `{}`))

	msg := send(t, s, nil, "1", "hi")
	got := awaitSettled(t, s, msg.ChatID, msg.ID)

	assert.Equal(t, StatusFailed, got.Status)
	assert.Contains(t, got.Error, "panic: boom")
	assert.Equal(t, marshalBlocks(append(llm.AnswerBlocks(fakeThought, fakeSentence), llm.ToolUseBlock("call-1", "echo", []byte(`{}`)))), got.Content)
	assert.Equal(t, []storedToolCall{{
		seq: 0, name: "echo", contract: "echo/v1", useID: "call-1", args: `{}`, errText: toolCallInterrupted,
		status: toolFailed, hasStarted: true, hasFinished: true, llmCallSeq: 0,
	}}, toolCallRows(t, s.db, msg.RunID))
}

// A start that cannot close a call or a task a previous process left open is a
// failed start: the sweep is part of it, whichever table refuses.
func TestAStartThatCannotCloseAStrandedCallFails(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct{ name, trigger string }{
		{"the model call", `CREATE TRIGGER refuse BEFORE UPDATE ON llm_calls BEGIN SELECT RAISE(ABORT, 'refused'); END`},
		{"the tool call", `CREATE TRIGGER refuse BEFORE UPDATE ON tool_calls BEGIN SELECT RAISE(ABORT, 'refused'); END`},
		{"the task", `CREATE TRIGGER refuse BEFORE UPDATE ON background_tasks BEGIN SELECT RAISE(ABORT, 'refused'); END`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := openTestDB(t, t.TempDir())
			now := time.UnixMilli(1_000).UTC()
			c := seedChat(t, db, aChat("1", now))
			turn := seedTurn(t, db, c.ID, now)
			call, toolCall := appdb.NewID(), appdb.NewID()
			_, err := db.Write.Exec(`INSERT INTO llm_calls (id, run_id, seq, provider, model, started_at) VALUES (?, ?, 0, 'fake', 'fake', 0)`, call, string(turn.Run))
			require.NoError(t, err)
			_, err = db.Write.Exec(`INSERT INTO tool_calls (id, llm_call_id, seq, tool_name, contract_name, status, created_at) VALUES (?, ?, 0, 'echo', 'echo/v1', 'running', 0)`, toolCall, call)
			require.NoError(t, err)
			_, err = db.Write.Exec(`INSERT INTO background_tasks (id, chat_id, tool_call_id, output_path, status, started_at)
				VALUES ('task-1', ?, ?, '/r/task-1.output', 'running', 0)`, string(c.ID), toolCall)
			require.NoError(t, err)
			_, err = db.Write.Exec(tc.trigger)
			require.NoError(t, err)
			s, err := newService(db, chatsDirIn(t.TempDir()), monitorDirIn(t.TempDir()), fakeLLM(), noClusterCards, nil, testReaders, noLists, sandbox.State{}, testSettings(t), nil)
			require.NoError(t, err)

			_, err = s.Start(t.Context())

			assert.ErrorContains(t, err, "refused")
			require.NoError(t, s.Close())
		})
	}
}

// A follow-up on another provider of the same dialect is accepted, and its run
// names that provider.
func TestAChatMovesWithinItsDialect(t *testing.T) {
	t.Parallel()

	p := llm.FakeProvider(llm.NewFake(0))
	other := p
	other.ID, other.Label = "fake-2", "Fake two"
	box, lists := testBox()
	s := startServiceWith(t, t.TempDir(), llm.New(p, other), &stubClusterCards{}, box, lists)
	first := send(t, s, nil, "1", "what is a pod?")
	awaitSettled(t, s, first.ChatID, first.ID)

	second, err := s.Send(t.Context(), &first.ChatID, ModeChat, "1", SendSwitches{}, "fake-2", "fake", "high", reqID("2"), "and a node?", nil)
	require.NoError(t, err)
	got := awaitSettled(t, s, second.ChatID, second.ID)

	assert.Equal(t, StatusComplete, got.Status)
	assert.Equal(t, "fake-2", got.ProviderID)
}

// A retry with no content is still the same send, not a malformed one.
func TestARetryWithNoContentIsStillTheSameSend(t *testing.T) {
	t.Parallel()

	s := newTestService(t)
	gate := make(chan struct{})
	fakeOf(s).SetGate(gate)
	first := send(t, s, nil, "1", "what is a pod?")

	again, err := s.Send(t.Context(), nil, ModeChat, "1", SendSwitches{}, "fake", "fake", "high", reqID("1"), "  ", nil)

	require.NoError(t, err)
	assert.Equal(t, first.ID, again.ID)
	close(gate)
	awaitSettled(t, s, first.ChatID, first.ID)
	assert.Equal(t, 2, tableCount(t, s.db, "messages"))
}

// A model the provider does not hold is a bad request, with nothing written.
func TestSendRefusesAModelTheRegistryDoesNotHold(t *testing.T) {
	t.Parallel()

	s := newTestService(t)

	_, err := s.Send(t.Context(), nil, ModeChat, "1", SendSwitches{}, "fake", "nonesuch", "high", reqID("1"), "hello", nil)

	assert.ErrorIs(t, err, ErrBadRequest)
	assert.Zero(t, tableCount(t, s.db, "chats"))
}

// A follow-up is checked against its chat's cluster, never the one its argument
// names: a chat whose cluster is marked takes no more sends.
func TestASendIntoAMarkedClusterIsRefused(t *testing.T) {
	t.Parallel()

	s := newTestService(t)
	existing, err := s.Send(t.Context(), nil, ModeChat, "7", SendSwitches{}, "fake", "fake", "high", reqID("1"), "hello", nil)
	require.NoError(t, err)
	awaitSettled(t, s, existing.ChatID, existing.ID)
	markCluster(t, s.db, "7")

	_, err = s.Send(t.Context(), &existing.ChatID, ModeChat, "1", SendSwitches{}, "fake", "fake", "high", reqID("2"), "a follow-up", nil)

	assert.ErrorIs(t, err, ErrClusterGone)
	assert.Equal(t, 2, tableCount(t, s.db, "messages"))
}

// A retry of a send accepted before its cluster was marked is answered by its
// key: the send happened.
func TestAReplayIsAnsweredAheadOfTheClusterCheck(t *testing.T) {
	t.Parallel()

	s := newTestService(t)
	first, err := s.Send(t.Context(), nil, ModeChat, "7", SendSwitches{}, "fake", "fake", "high", reqID("1"), "hello", nil)
	require.NoError(t, err)
	awaitSettled(t, s, first.ChatID, first.ID)
	markCluster(t, s.db, "7")

	again, err := s.Send(t.Context(), nil, ModeChat, "7", SendSwitches{}, "fake", "fake", "high", reqID("1"), "hello", nil)

	require.NoError(t, err)
	assert.Equal(t, first.ID, again.ID)
}

// Two sends released together start one turn, and seq never collides. A guard,
// not a proof: two goroutines that happen to serialize pass it with the bug
// present.
func TestTwoSendsOnOneChatStartOneTurn(t *testing.T) {
	t.Parallel()

	s := newTestService(t)
	first := send(t, s, nil, "0", "opening")
	awaitSettled(t, s, first.ChatID, first.ID)
	gate := make(chan struct{})
	fakeOf(s).SetGate(gate)

	var (
		wg       sync.WaitGroup
		mu       sync.Mutex
		accepted []ChatMessage
		refused  int
		start    = make(chan struct{})
	)
	for _, key := range []string{"1", "2"} {
		wg.Go(func() {
			<-start
			msg, err := s.Send(t.Context(), &first.ChatID, ModeChat, "1", SendSwitches{}, "fake", "fake", "high", reqID(key), "question", nil)
			mu.Lock()
			defer mu.Unlock()
			switch {
			case errors.Is(err, ErrTurnInFlight):
				refused++
			case assert.NoError(t, err):
				accepted = append(accepted, msg)
			}
		})
	}
	close(start)
	wg.Wait()

	assert.Equal(t, 1, refused)
	require.Len(t, accepted, 1)
	close(gate)
	awaitSettled(t, s, first.ChatID, accepted[0].ID)
	msgs, err := s.readMessages(t.Context(), first.ChatID)
	require.NoError(t, err)
	for i, m := range msgs {
		assert.Equal(t, int64(i), m.Seq, "seq is contiguous")
	}
}

// A turn that fails still says what it was asked to run.
func TestAFailedTurnKeepsWhatItWasAskedToRun(t *testing.T) {
	t.Parallel()

	s := newTestService(t)
	fakeOf(s).FailAfter(1, errors.New("boom"))
	msg := send(t, s, nil, "1", "what is a pod?")

	got := awaitSettled(t, s, msg.ChatID, msg.ID)

	assert.Equal(t, StatusFailed, got.Status)
	assert.Equal(t, [3]string{"fake", "fake", "high"}, [3]string{got.ProviderID, got.ModelID, got.Effort})
}

// A call refused before its first chunk is a failed turn like any other.
func TestATurnWhoseCallNeverStartedFails(t *testing.T) {
	t.Parallel()

	s := newTestService(t)
	fakeOf(s).FailAfter(0, errors.New("no model behind this"))
	msg := send(t, s, nil, "1", "what is a pod?")

	got := awaitSettled(t, s, msg.ChatID, msg.ID)

	assert.Equal(t, StatusFailed, got.Status)
	assert.Equal(t, "no model behind this", got.Error)
	assert.True(t, got.FinishedAt.Valid)
}

// A model that stops sending is a failure, not a cancel: the turn's own context
// was never cancelled.
func TestATurnThatGoesQuietFails(t *testing.T) {
	t.Parallel()

	s := newTestService(t)
	fakeOf(s).FailAfter(1, llm.ErrStreamIdle)
	msg := send(t, s, nil, "1", "what is a pod?")

	got := awaitSettled(t, s, msg.ChatID, msg.ID)

	assert.Equal(t, StatusFailed, got.Status)
	assert.Equal(t, llm.ErrStreamIdle.Error(), got.Error)
}

// A settled answer moves its chat up the list, past the send that asked it.
func TestACompletedTurnMovesTheChatsRecency(t *testing.T) {
	t.Parallel()

	s := newTestService(t)
	stepClock(s)
	msg := send(t, s, nil, "1", "what is a pod?")
	awaitSettled(t, s, msg.ChatID, msg.ID)

	c, ok, err := s.Get(t.Context(), msg.ChatID)

	require.NoError(t, err)
	require.True(t, ok)
	assert.True(t, c.UpdatedAt.After(c.CreatedAt))
}

// A failed delete of a chat with no turn writes nothing: no settle runs, no slot
// is held, and the rows are as they were.
func TestAFailedDeleteOfASettledChatWritesNothing(t *testing.T) {
	t.Parallel()

	s := newTestService(t)
	msg := send(t, s, nil, "1", "what is a pod?")
	before := awaitSettled(t, s, msg.ChatID, msg.ID)
	s.deleteWrite = func(context.Context, ChatID) (bool, error) { return false, errRefused }
	settles := 0
	s.settleWrite = func(context.Context, *turn) error { settles++; return nil }

	assert.ErrorIs(t, s.Delete(t.Context(), msg.ChatID), errRefused)

	assert.Zero(t, settles)
	assert.Nil(t, s.turnOf(msg.ChatID))
	msgs, err := s.readMessages(t.Context(), msg.ChatID)
	require.NoError(t, err)
	require.Len(t, msgs, 2)
	assert.Equal(t, before.Content, msgs[1].Content)
	assert.Equal(t, before.Status, msgs[1].Status)
}

// A delete of an idle chat holds no turn, and a read while its write is held
// sees the stored rows.
func TestAReadDuringADeleteOfAnIdleChatSeesTheStoredRows(t *testing.T) {
	t.Parallel()

	s := newTestService(t)
	msg := send(t, s, nil, "1", "what is a pod?")
	settled := awaitSettled(t, s, msg.ChatID, msg.ID)
	blocked, release := make(chan struct{}), make(chan struct{})
	s.deleteWrite = func(context.Context, ChatID) (bool, error) {
		close(blocked)
		<-release
		return false, errRefused
	}
	deleted := make(chan error, 1)
	go func() { deleted <- s.Delete(context.Background(), msg.ChatID) }()
	testutil.Wait(t, blocked, "the delete's own write")

	msgs, err := s.readMessages(t.Context(), msg.ChatID)

	require.NoError(t, err)
	require.Len(t, msgs, 2)
	assert.Equal(t, settled.Content, msgs[1].Content)
	assert.Equal(t, settled.Status, msgs[1].Status)
	close(release)
	assert.ErrorIs(t, testutil.Recv(t, deleted, "the delete"), errRefused)
}

// The watcher that saw the turn start is gone, and the answer is still there
// when the next one looks.
func TestATurnOutlivesTheWatcherThatSawItStart(t *testing.T) {
	t.Parallel()

	s := newTestService(t)
	gate := make(chan struct{})
	fakeOf(s).SetGate(gate)
	msg := send(t, s, nil, "1", "what is a pod?")
	watchCtx, closeWatch := context.WithCancel(t.Context())
	w, err := s.WatchMessageCollection(watchCtx, msg.ChatID)
	require.NoError(t, err)
	testutil.Recv(t, w.Frames, "the question")

	closeWatch()
	testutil.WaitClosed(t, w.Frames, "the abandoned watch")
	close(gate)
	awaitSettled(t, s, msg.ChatID, msg.ID)

	fresh, err := s.WatchMessageCollection(t.Context(), msg.ChatID)
	require.NoError(t, err)
	snapshot := collectSnapshot(t, fresh.Frames)
	require.Len(t, snapshot, 2)
	assert.Equal(t, StatusComplete, snapshot[1].Status)
	assert.Equal(t, marshalBlocks(llm.AnswerBlocks(fakeThought, fakeSentence)), snapshot[1].Content)
}

// Both subscribers reach the same settled message, and neither is the caller.
// Not every frame: conflation lets two watchers see different ones in between.
func TestTwoSubscribersBothSeeTheTurnSettle(t *testing.T) {
	t.Parallel()

	s := newTestService(t)
	gate := make(chan struct{})
	fakeOf(s).SetGate(gate)
	msg := send(t, s, nil, "1", "what is a pod?")
	first, err := s.WatchMessageCollection(t.Context(), msg.ChatID)
	require.NoError(t, err)
	second, err := s.WatchMessageCollection(t.Context(), msg.ChatID)
	require.NoError(t, err)

	close(gate)

	for _, w := range []*Stream[ChatMessageCollectionWatchFrame]{first, second} {
		settled := awaitFrame(t, w.Frames, func(f ChatMessageCollectionWatchFrame) bool {
			return f.Message != nil && f.Message.ID == msg.ID && f.Message.Status == StatusComplete
		})
		assert.Equal(t, marshalBlocks(llm.AnswerBlocks(fakeThought, fakeSentence)), settled.Message.Content)
	}
}

// A watch opened mid-answer snapshots the live text, not the empty stored row.
func TestALateSubscriberSeesTheLiveText(t *testing.T) {
	t.Parallel()

	s := newTestService(t)
	s.checkpointEvery = time.Hour
	gate := make(chan struct{})
	fakeOf(s).SetGate(gate)
	msg := send(t, s, nil, "1", "what is a pod?")
	awaitLiveContent(t, s, msg.ChatID)

	w, err := s.WatchMessageCollection(t.Context(), msg.ChatID)
	require.NoError(t, err)
	snapshot := collectSnapshot(t, w.Frames)

	require.Len(t, snapshot, 2)
	assert.Equal(t, StatusStreaming, snapshot[1].Status)
	assert.Equal(t, marshalBlocks([]llm.Block{llm.ThinkingBlock(fakeFirstWord)}), snapshot[1].Content)
	close(gate)
	awaitSettled(t, s, msg.ChatID, msg.ID)
}

// A finished answer does not spin: once the Complete frame is out, the watch
// sends nothing more.
func TestAFinishedAnswerSendsNoStreamingFrameAfterIt(t *testing.T) {
	t.Parallel()

	s := newTestService(t)
	gate := make(chan struct{})
	fakeOf(s).SetGate(gate)
	msg := send(t, s, nil, "1", "what is a pod?")
	w, err := s.WatchMessageCollection(t.Context(), msg.ChatID)
	require.NoError(t, err)
	// The snapshot lands while the answer is held, so the settled row arrives as
	// a delta and the Bookmark cannot trail it.
	collectSnapshot(t, w.Frames)

	close(gate)
	awaitFrame(t, w.Frames, func(f ChatMessageCollectionWatchFrame) bool {
		return f.Message != nil && f.Message.ID == msg.ID && f.Message.Status == StatusComplete
	})

	// A negative assertion, bounded by a window: nothing marks a frame that must
	// not come, and everything upstream has already happened, so a stale one
	// would arrive at once.
	testutil.NoRecv(t, w.Frames, 100*time.Millisecond, "a frame after the answer settled")
}

// A call the turn's cancel cut is closed by what the tool did: one that answered
// the cancel with an error is failed cancelled, having started; one that returned
// a real result is succeeded with it.
func TestACallTheCancelCutIsClosedByWhatItDid(t *testing.T) {
	t.Parallel()

	run := func(t *testing.T, answer string, isError bool) storedToolCall {
		t.Helper()
		reached := make(chan struct{})
		s := startServiceWithTool(t, testTool{name: "echo", run: func(ctx context.Context, _ json.RawMessage) (string, bool) {
			close(reached)
			<-ctx.Done()
			return answer, isError
		}})
		fakeOf(s).SetToolCalls(llm.StagedCall("echo", `{}`))
		msg := send(t, s, nil, "1", "?")
		testutil.Wait(t, reached, "the cut call")

		require.NoError(t, s.Cancel(t.Context(), msg.ChatID))
		got := awaitSettled(t, s, msg.ChatID, msg.ID)

		require.Equal(t, StatusCancelled, got.Status)
		rows := toolCallRows(t, s.db, msg.RunID)
		require.Len(t, rows, 1)
		return rows[0]
	}

	t.Run("answered the cancel", func(t *testing.T) {
		row := run(t, "context canceled", true)
		assert.Equal(t, toolFailed, row.status)
		assert.Equal(t, loop.CodeCancelled.Text(), row.errText)
		assert.True(t, row.hasStarted)
		assert.True(t, row.hasFinished)
	})

	t.Run("returned a result", func(t *testing.T) {
		row := run(t, `{"count":2}`, false)
		assert.Equal(t, toolSucceeded, row.status)
		assert.Equal(t, `{"count":2}`, row.result)
	})
}

// A run's call order is its rows' seq, never their ids: each model call takes
// the next place in the run, and each tool call the place of its tool_use block
// in the reply, a refused call holding its place between the ones that ran.
func TestACallsSeqIsItsPlaceInTheRun(t *testing.T) {
	t.Parallel()

	s := startServiceWithTool(t, testTool{name: "echo"})
	fakeOf(s).SetToolCalls(llm.StagedCall("nope", `{}`), llm.StagedCall("echo", `{}`), llm.StagedCall("nope", `{}`))
	msg := send(t, s, nil, "1", "?")
	awaitSettled(t, s, msg.ChatID, msg.ID)

	var seqs []int
	rows, err := s.db.Read.Query(`SELECT seq FROM llm_calls WHERE run_id = ? ORDER BY started_at, seq`, string(msg.RunID))
	require.NoError(t, err)
	for rows.Next() {
		var seq int
		require.NoError(t, rows.Scan(&seq))
		seqs = append(seqs, seq)
	}
	require.NoError(t, rows.Close())
	assert.Equal(t, []int{0, 1}, seqs, "the reply's round and the one after it")
	calls := toolCallRows(t, s.db, msg.RunID)
	require.Len(t, calls, 3)
	for i, want := range []string{toolFailed, toolSucceeded, toolFailed} {
		assert.Equal(t, i, calls[i].seq)
		assert.Zero(t, calls[i].llmCallSeq, "under the first model call")
		assert.Equal(t, want, calls[i].status)
	}
}

// A turn of rounds that ran their calls and rounds the budget refused lands a
// row per call, each under the model call whose reply asked for it; only a call
// that ran has started.
func TestATurnsToolCallsLandInResultOrder(t *testing.T) {
	t.Parallel()

	s := startServiceWithTool(t, testTool{name: "echo"})
	// Two calls a reply on a budget of eight: four rounds run both, the fifth is
	// refused and the sixth, the synthesis, is refused too.
	fakeOf(s).RepeatToolCalls(llm.StagedCall("echo", `{"n":1}`), llm.StagedCall("echo", `{"n":2}`))
	msg := send(t, s, nil, "1", "?")

	got := awaitSettled(t, s, msg.ChatID, msg.ID)

	require.Equal(t, StatusComplete, got.Status)
	rows := toolCallRows(t, s.db, msg.RunID)
	require.Len(t, rows, 12)
	for i, r := range rows {
		assert.Equal(t, i/2, r.llmCallSeq, "row %d is under round %d's call", i, i/2)
		assert.Equal(t, fmt.Sprintf(`{"n":%d}`, i%2+1), r.args)
		assert.Equal(t, i < MaxToolCalls, r.hasStarted, "only a call that ran started")
	}
}

// A repeated tool_use id is two rows, each with its own result: the row's id is
// the app's, so the provider's never keys anything, within a reply and across
// rounds alike, and the settle lands. At the production budget.
func TestARepeatedToolUseIDLandsARowPerResult(t *testing.T) {
	t.Parallel()

	echo := testTool{name: "echo", run: func(_ context.Context, input json.RawMessage) (string, bool) { return string(input), false }}

	t.Run("within a reply", func(t *testing.T) {
		s := startServiceWithTool(t, echo)
		fakeOf(s).SetToolCalls(llm.StagedCallWithID("dup", "echo", `{"n":1}`), llm.StagedCallWithID("dup", "echo", `{"n":2}`))
		msg := send(t, s, nil, "1", "?")

		got := awaitSettled(t, s, msg.ChatID, msg.ID)

		require.Equal(t, StatusComplete, got.Status)
		rows := toolCallRows(t, s.db, msg.RunID)
		require.Len(t, rows, 2)
		for i, r := range rows {
			assert.Equal(t, "dup", r.useID)
			assert.Equal(t, fmt.Sprintf(`{"n":%d}`, i+1), r.result)
		}
	})

	t.Run("across rounds", func(t *testing.T) {
		s := startServiceWithTool(t, echo)
		fakeOf(s).RepeatToolCalls(llm.StagedCallWithID("dup", "echo", `{"n":1}`))
		msg := send(t, s, nil, "1", "?")

		got := awaitSettled(t, s, msg.ChatID, msg.ID)

		require.Equal(t, StatusComplete, got.Status)
		rows := toolCallRows(t, s.db, msg.RunID)
		require.Len(t, rows, MaxToolCalls+2)
		for i, r := range rows {
			if i < MaxToolCalls {
				assert.Equal(t, toolSucceeded, r.status)
				assert.Equal(t, `{"n":1}`, r.result)
			} else {
				assert.Equal(t, toolFailed, r.status)
				assert.Equal(t, loop.CodeBudget.Text(), r.errText)
			}
		}
	})
}

// A new chat starts with its cluster's default, else every cluster's, else in
// the sandbox, and with no network either way.
func TestANewChatStartsWithItsDefault(t *testing.T) {
	t.Parallel()

	s := newTestService(t)
	s.sandboxState = sandbox.State{Available: true}
	seedClusters(t, s.db, "2", "3")
	sandboxed := sendAndSettle(t, s, nil, "1", "1", "hi")
	setSandboxDefault(t, s, "", true)
	setSandboxDefault(t, s, "3", false)
	everyClusters := sendAndSettle(t, s, nil, "2", "2", "hi")
	clustersOwn := sendAndSettle(t, s, nil, "3", "3", "hi")

	for msg, want := range map[ChatMessage]bool{sandboxed: false, everyClusters: true, clustersOwn: false} {
		c, ok, err := s.Get(t.Context(), msg.ChatID)
		require.NoError(t, err)
		require.True(t, ok)
		assert.Equal(t, want, c.SandboxDisabled, c.ClusterID)
		assert.False(t, c.NetworkEnabled, c.ClusterID)
	}
}

// A default is read only when a chat is created: changing it leaves every chat
// that exists as it was.
func TestADefaultChangeLeavesExistingChatsAlone(t *testing.T) {
	t.Parallel()

	s := newTestService(t)
	s.sandboxState = sandbox.State{Available: true}
	sandboxed := sendAndSettle(t, s, nil, "1", "1", "hi")
	setSandboxDefault(t, s, "", true)
	outside := sendAndSettle(t, s, nil, "1", "2", "hi")
	setSandboxDefault(t, s, "", false)
	setSandboxDefault(t, s, "1", true)
	setSandboxDefault(t, s, "1", false)

	for msg, want := range map[ChatMessage]bool{sandboxed: false, outside: true} {
		c, _, err := s.Get(t.Context(), msg.ChatID)
		require.NoError(t, err)
		assert.Equal(t, want, c.SandboxDisabled)
	}
	again := sendAndSettle(t, s, &outside.ChatID, "1", "3", "and?")
	assert.Equal(t, outside.ChatID, again.ChatID, "a send names the chat's own switch")
}

// A default that cannot be read starts no chat: a guess could start one
// outside the sandbox the user chose.
func TestACreateWhoseDefaultCannotBeReadIsRefused(t *testing.T) {
	t.Parallel()

	s := newTestService(t)
	s.sandboxState = sandbox.State{Available: true}
	_, err := s.db.Write.Exec(`DROP TABLE settings`)
	require.NoError(t, err)

	_, err = s.Send(t.Context(), nil, ModeChat, "1", SendSwitches{}, "fake", "fake", "high", reqID("1"), "hi", nil)
	assert.Error(t, err)
	list, err := s.List(t.Context())
	require.NoError(t, err)
	assert.Empty(t, list, "nothing written")
}

// A machine with no sandbox starts every chat as it can, whatever a stored
// default says.
func TestANewChatIgnoresItsDefaultWithoutASandbox(t *testing.T) {
	t.Parallel()

	s := newTestService(t)
	setSandboxDefault(t, s, "", true)

	msg := sendAndSettle(t, s, nil, "1", "1", "hi")
	c, _, err := s.Get(t.Context(), msg.ChatID)
	require.NoError(t, err)
	assert.False(t, c.SandboxDisabled)
}

// The switch is the chat's row: it reaches every window through the list watch,
// and it is not activity, so the list's order stays.
func TestTheSwitchIsWrittenAndWatched(t *testing.T) {
	t.Parallel()

	s := newTestService(t)
	s.sandboxState = sandbox.State{Available: true}
	s.now = func() time.Time { return time.UnixMilli(5_000) }
	c := seedChat(t, s.db, aChat("1", time.UnixMilli(1_000).UTC()))
	w, err := s.WatchCollection(t.Context())
	require.NoError(t, err)
	require.Len(t, collectChatSnapshot(t, w.Frames), 1)

	switched, err := s.SetSandboxDisabled(t.Context(), c.ID, true)
	require.NoError(t, err)
	assert.True(t, switched.SandboxDisabled)
	assert.Equal(t, c.UpdatedAt, switched.UpdatedAt)
	stored, _, err := s.Get(t.Context(), c.ID)
	require.NoError(t, err)
	assert.Equal(t, stored, switched)

	f := testutil.Recv(t, w.Frames, "the switch")
	assert.Equal(t, CollectionWatchFrameModified, f.Type)
	assert.True(t, f.Chat.SandboxDisabled)

	back, err := s.SetSandboxDisabled(t.Context(), c.ID, false)
	require.NoError(t, err)
	assert.False(t, back.SandboxDisabled)

	_, err = s.SetSandboxDisabled(t.Context(), ChatID(appdb.NewID()), true)
	assert.ErrorIs(t, err, ErrChatGone)
}

// A send carries the switch its sender saw, and the turn runs only where they saw
// it would: another window can switch the chat in between.
func TestASendThatSawTheOtherSwitchIsRefused(t *testing.T) {
	t.Parallel()

	s := newTestService(t)
	s.sandboxState = sandbox.State{Available: true}
	first := sendAndSettle(t, s, nil, "1", "1", "hi")
	_, err := s.SetSandboxDisabled(t.Context(), first.ChatID, true)
	require.NoError(t, err)

	_, err = s.Send(t.Context(), &first.ChatID, ModeChat, "1", SendSwitches{}, "fake", "fake", "high", reqID("2"), "and?", nil)
	assert.ErrorIs(t, err, ErrChatSandboxChanged)
	assert.Len(t, questions(t, s, first.ChatID), 1, "nothing written")

	sent, err := s.Send(t.Context(), &first.ChatID, ModeChat, "1", SendSwitches{SandboxDisabled: true}, "fake", "fake", "high", reqID("3"), "and?", nil)
	require.NoError(t, err)
	awaitSettled(t, s, sent.ChatID, sent.ID)
}

// A create carries the default its composer showed, and one that differs
// from the default it resolves saw another window's change: refused, with
// nothing written.
func TestACreateThatSawAnotherDefaultIsRefused(t *testing.T) {
	t.Parallel()

	s := newTestService(t)
	s.sandboxState = sandbox.State{Available: true}

	_, err := s.Send(t.Context(), nil, ModeChat, "1", SendSwitches{SandboxDisabled: true}, "fake", "fake", "high", reqID("1"), "hi", nil)
	assert.ErrorIs(t, err, ErrChatSandboxChanged)

	setSandboxDefault(t, s, "1", true)
	_, err = s.Send(t.Context(), nil, ModeChat, "1", SendSwitches{}, "fake", "fake", "high", reqID("2"), "hi", nil)
	assert.ErrorIs(t, err, ErrChatSandboxChanged)
	list, err := s.List(t.Context())
	require.NoError(t, err)
	assert.Empty(t, list, "nothing written")

	sent, err := s.Send(t.Context(), nil, ModeChat, "1", SendSwitches{SandboxDisabled: true}, "fake", "fake", "high", reqID("3"), "hi", nil)
	require.NoError(t, err)
	awaitSettled(t, s, sent.ChatID, sent.ID)
}

// A replay is answered by its key alone, so a switch after the first attempt
// does not refuse it.
func TestAReplayIgnoresTheSwitch(t *testing.T) {
	t.Parallel()

	s := newTestService(t)
	s.sandboxState = sandbox.State{Available: true}
	first := sendAndSettle(t, s, nil, "1", "1", "hi")
	_, err := s.SetSandboxDisabled(t.Context(), first.ChatID, true)
	require.NoError(t, err)

	again, err := s.Send(t.Context(), nil, ModeChat, "1", SendSwitches{}, "fake", "fake", "high", reqID("1"), "hi", nil)
	require.NoError(t, err)
	assert.Equal(t, first.ID, again.ID)
}

// A machine with no sandbox has nothing to switch, so the switch is refused and
// the row stays as it was.
func TestTheSwitchIsRefusedWithoutASandbox(t *testing.T) {
	t.Parallel()

	s := newTestService(t)
	c := seedChat(t, s.db, aChat("1", time.UnixMilli(1_000).UTC()))

	_, err := s.SetSandboxDisabled(t.Context(), c.ID, true)
	assert.ErrorIs(t, err, ErrBadRequest)
	stored, _, err := s.Get(t.Context(), c.ID)
	require.NoError(t, err)
	assert.False(t, stored.SandboxDisabled)
}

// The network switch is the chat's row, false for a new chat: it reaches every
// window through the list watch, and it is not activity, so the list's order
// stays.
func TestTheNetworkSwitchIsTheChats(t *testing.T) {
	t.Parallel()

	s := newTestService(t)
	s.sandboxState = sandbox.State{Available: true, NetworkAvailable: true}
	s.now = func() time.Time { return time.UnixMilli(5_000) }
	msg := sendAndSettle(t, s, nil, "1", "1", "hi")
	created, _, err := s.Get(t.Context(), msg.ChatID)
	require.NoError(t, err)
	assert.False(t, created.NetworkEnabled)
	w, err := s.WatchCollection(t.Context())
	require.NoError(t, err)
	require.Len(t, collectChatSnapshot(t, w.Frames), 1)

	switched, err := s.SetNetworkEnabled(t.Context(), msg.ChatID, true)
	require.NoError(t, err)
	assert.True(t, switched.NetworkEnabled)
	assert.Equal(t, created.UpdatedAt, switched.UpdatedAt)
	stored, _, err := s.Get(t.Context(), msg.ChatID)
	require.NoError(t, err)
	assert.Equal(t, stored, switched)

	f := testutil.Recv(t, w.Frames, "the switch")
	assert.Equal(t, CollectionWatchFrameModified, f.Type)
	assert.True(t, f.Chat.NetworkEnabled)

	back, err := s.SetNetworkEnabled(t.Context(), msg.ChatID, false)
	require.NoError(t, err)
	assert.False(t, back.NetworkEnabled)

	_, err = s.SetNetworkEnabled(t.Context(), ChatID(appdb.NewID()), true)
	assert.ErrorIs(t, err, ErrChatGone)
	_, err = s.SetNetworkEnabled(t.Context(), ChatID(appdb.NewID()), false)
	assert.ErrorIs(t, err, ErrChatGone)
}

// A send carries the network switch its sender saw: another window can turn it
// on or off in between, and a turn must not run with network nobody saw. A
// replay is answered by its key alone, and a create says off.
func TestASendWithAStaleNetworkSwitchIsRefused(t *testing.T) {
	t.Parallel()

	s := newTestService(t)
	s.sandboxState = sandbox.State{Available: true, NetworkAvailable: true}
	first := sendAndSettle(t, s, nil, "1", "1", "hi")
	_, err := s.SetNetworkEnabled(t.Context(), first.ChatID, true)
	require.NoError(t, err)

	_, err = s.Send(t.Context(), &first.ChatID, ModeChat, "1", SendSwitches{}, "fake", "fake", "high", reqID("2"), "and?", nil)
	assert.ErrorIs(t, err, ErrChatNetworkChanged)
	assert.Len(t, questions(t, s, first.ChatID), 1, "nothing written")

	again, err := s.Send(t.Context(), nil, ModeChat, "1", SendSwitches{}, "fake", "fake", "high", reqID("1"), "hi", nil)
	require.NoError(t, err)
	assert.Equal(t, first.ID, again.ID, "a replay ignores the switch")

	sent, err := s.Send(t.Context(), &first.ChatID, ModeChat, "1", SendSwitches{NetworkEnabled: true}, "fake", "fake", "high", reqID("3"), "and?", nil)
	require.NoError(t, err)
	awaitSettled(t, s, sent.ChatID, sent.ID)

	_, err = s.Send(t.Context(), nil, ModeChat, "1", SendSwitches{NetworkEnabled: true}, "fake", "fake", "high", reqID("4"), "hi", nil)
	assert.ErrorIs(t, err, ErrChatNetworkChanged, "a chat starts with no network")
}

// The network switch is on only in the sandbox: leaving it turns the switch
// off in the same write, and turning it on outside is refused, so a chat back
// in the sandbox starts without network and the confirm is seen again.
func TestLeavingTheSandboxTurnsNetworkOff(t *testing.T) {
	t.Parallel()

	s := newTestService(t)
	s.sandboxState = sandbox.State{Available: true, NetworkAvailable: true}
	first := sendAndSettle(t, s, nil, "1", "1", "hi")
	_, err := s.SetNetworkEnabled(t.Context(), first.ChatID, true)
	require.NoError(t, err)
	w, err := s.WatchCollection(t.Context())
	require.NoError(t, err)
	require.Len(t, collectChatSnapshot(t, w.Frames), 1)

	outside, err := s.SetSandboxDisabled(t.Context(), first.ChatID, true)
	require.NoError(t, err)
	assert.True(t, outside.SandboxDisabled)
	assert.False(t, outside.NetworkEnabled)
	f := testutil.Recv(t, w.Frames, "the switch")
	assert.Equal(t, CollectionWatchFrameModified, f.Type)
	assert.False(t, f.Chat.NetworkEnabled, "one frame carries both")

	_, err = s.SetNetworkEnabled(t.Context(), first.ChatID, true)
	assert.ErrorIs(t, err, ErrBadRequest)
	stored, _, err := s.Get(t.Context(), first.ChatID)
	require.NoError(t, err)
	assert.False(t, stored.NetworkEnabled, "nothing written")
	_, err = s.SetNetworkEnabled(t.Context(), first.ChatID, false)
	assert.NoError(t, err, "off is always accepted")

	back, err := s.SetSandboxDisabled(t.Context(), first.ChatID, false)
	require.NoError(t, err)
	assert.False(t, back.NetworkEnabled, "a chat back in the sandbox starts without network")
	on, err := s.SetNetworkEnabled(t.Context(), first.ChatID, true)
	require.NoError(t, err)
	assert.True(t, on.NetworkEnabled)
}

// Where network is unavailable nothing turns it on: the switch and a send
// with the toggle are refused, nothing written; turning the switch off is
// always accepted.
func TestNothingTurnsNetworkOnWhereItIsUnavailable(t *testing.T) {
	t.Parallel()

	s := newTestService(t)
	s.sandboxState = sandbox.State{Available: true, NetworkReason: "pasta not found"}
	first := sendAndSettle(t, s, nil, "1", "1", "hi")

	_, err := s.SetNetworkEnabled(t.Context(), first.ChatID, true)
	assert.ErrorIs(t, err, ErrBadRequest)
	stored, _, err := s.Get(t.Context(), first.ChatID)
	require.NoError(t, err)
	assert.False(t, stored.NetworkEnabled)
	_, err = s.SetNetworkEnabled(t.Context(), first.ChatID, false)
	assert.NoError(t, err)

	_, err = s.Send(t.Context(), &first.ChatID, ModeChat, "1", SendSwitches{NetworkThisTurn: true}, "fake", "fake", "high", reqID("2"), "and?", nil)
	assert.ErrorIs(t, err, ErrBadRequest)
	assert.Len(t, questions(t, s, first.ChatID), 1, "nothing written")
}

// logToolOf is the log tool whose run shows an action of kind.
var logToolOf = map[tools.ActionKind]string{tools.ActionLogsView: "LogsView", tools.ActionLogsRead: "LogsRead", tools.ActionLogsWatch: "LogsWatch"}

// seedViewCall files a settled turn in chatID whose one call showed shown, as a
// log tool's run does, and returns the call's id. With no action shown, the call
// is a LogsView call's.
func seedViewCall(t *testing.T, db *appdb.DB, chatID ChatID, toolUseID string, shown *tools.Action) ToolCallID {
	t.Helper()
	at := time.Now()
	turn := seedTurn(t, db, chatID, at)
	settleSeededRun(t, db, turn.Run, RunSucceeded, at)
	llmCall, id := appdb.NewID(), ToolCallID(appdb.NewID())
	_, err := db.Write.Exec(`INSERT INTO llm_calls (id, run_id, seq, provider, model, started_at) VALUES (?, ?, 0, 'fake', 'fake', ?)`,
		llmCall, string(turn.Run), millis(at))
	require.NoError(t, err)
	name, action := "LogsView", any(nil)
	if shown != nil {
		raw, err := json.Marshal(shown)
		require.NoError(t, err)
		name, action = logToolOf[shown.Kind()], string(raw)
	}
	_, err = db.Write.Exec(`INSERT INTO tool_calls (id, llm_call_id, seq, tool_name, contract_name, tool_use_id, arguments, status, shown_action, created_at)
		VALUES (?, ?, 0, ?, ?, ?, '{}', 'succeeded', ?, ?)`, string(id), llmCall, name, string(testReaders.ContractOf(name)), toolUseID, action, millis(at))
	require.NoError(t, err)
	return id
}

// sendWithView is a send into chatID carrying snap, on the fake's names.
func sendWithView(t *testing.T, s *service, chatID *ChatID, key string, snap *Snapshot) (ChatMessage, error) {
	t.Helper()
	return s.Send(t.Context(), chatID, ModeChat, "1", SendSwitches{}, "fake", "fake", "high", reqID(key), "what's this?", snap)
}

// A snapshot of this chat's view puts a View section in the question's context,
// after the workspace and before the sandbox, restating the call's stored action.
func TestASendCarriesTheViewBeforeTheSandbox(t *testing.T) {
	t.Parallel()

	s := serviceWithClusterCards(t, &stubClusterCards{card: "## Cluster\n\n```json\n{}\n```"})
	s.sandboxState = sandbox.State{Available: true, NetworkAvailable: true}
	first := sendAndSettle(t, s, nil, "1", "1", "tail webapp logs")
	action := tailWebapp()
	call := seedViewCall(t, s.db, first.ChatID, "toolu_view", &tools.Action{LogsView: &action})

	msg, err := sendWithView(t, s, &first.ChatID, "2", &Snapshot{CallID: call, Lines: 3, SelectedCount: 1, Selected: []SelectedLine{aLine(1, "boom")}})
	require.NoError(t, err)
	awaitSettled(t, s, msg.ChatID, msg.ID)

	context := newestContextOf(t, s, first.ChatID)
	view := strings.Index(context, viewHeading+`{"call":"toolu_view"`)
	require.Positive(t, view, context)
	assert.Less(t, strings.Index(context, "## Workspace"), view)
	assert.Less(t, view, strings.Index(context, "## Sandbox"))
	assert.Contains(t, context, `"grep":"error"`)
	assert.Contains(t, context, `"message":"boom"`)
}

// A snapshot the sidecar cannot vouch for refuses the send, writing nothing.
func TestASendRefusesASnapshotItCannotVouchFor(t *testing.T) {
	t.Parallel()

	s := serviceWithClusterCards(t, &stubClusterCards{card: "card"})
	first := sendAndSettle(t, s, nil, "1", "1", "one")
	other := sendAndSettle(t, s, nil, "1", "2", "two")
	action := tailWebapp()
	elsewhere := seedViewCall(t, s.db, other.ChatID, "toolu_other", &tools.Action{LogsView: &action})
	query := seedViewCall(t, s.db, first.ChatID, "toolu_query", &tools.Action{KubeQuery: &tools.KubeQueryAction{SQL: "SELECT 1"}})
	bare := seedViewCall(t, s.db, first.ChatID, "toolu_bare", nil)
	mine := seedViewCall(t, s.db, first.ChatID, "toolu_mine", &tools.Action{LogsView: &action})
	before := tableCount(t, s.db, "messages")

	for name, snap := range map[string]Snapshot{
		"another chat's call":   {CallID: elsewhere},
		"not a LogsView call":   {CallID: query},
		"a call with no action": {CallID: bare},
		"no such call":          {CallID: "nope"},
		"a bound":               {CallID: mine, Lines: -1},
	} {
		_, err := sendWithView(t, s, &first.ChatID, name, &snap)
		assert.ErrorIs(t, err, ErrBadRequest, name)
	}
	_, err := sendWithView(t, s, nil, "create", &Snapshot{CallID: mine})
	assert.ErrorIs(t, err, ErrBadRequest, "a send that creates a chat")
	assert.Equal(t, before, tableCount(t, s.db, "messages"), "nothing written")
}

// A replay is answered by its key, whatever snapshot it carries.
func TestAReplayIgnoresItsSnapshot(t *testing.T) {
	t.Parallel()

	s := serviceWithClusterCards(t, &stubClusterCards{card: "card"})
	first := sendAndSettle(t, s, nil, "1", "1", "one")
	again, err := sendWithView(t, s, nil, "1", &Snapshot{CallID: "nope", Lines: -1})
	require.NoError(t, err)
	assert.Equal(t, first.ID, again.ID)
}

// The block is sent when the View changes: not again for the same screen, and
// again without the section once no view rides.
func TestTheViewSectionReSendsTheBlockWhenItChanges(t *testing.T) {
	t.Parallel()

	s := serviceWithClusterCards(t, &stubClusterCards{card: "card"})
	first := sendAndSettle(t, s, nil, "1", "1", "one")
	action := tailWebapp()
	call := seedViewCall(t, s.db, first.ChatID, "toolu_view", &tools.Action{LogsView: &action})
	snap := &Snapshot{CallID: call, Lines: 3, Paused: true}

	for _, key := range []string{"2", "3"} {
		msg, err := sendWithView(t, s, &first.ChatID, key, snap)
		require.NoError(t, err)
		awaitSettled(t, s, msg.ChatID, msg.ID)
	}
	asked := questions(t, s, first.ChatID)
	assert.Equal(t, []llm.BlockType{llm.BlockContext, llm.BlockText}, blockTypes(t, asked[len(asked)-2]))
	assert.Equal(t, []llm.BlockType{llm.BlockText}, blockTypes(t, asked[len(asked)-1]), "the same screen")

	sendAndSettle(t, s, &first.ChatID, "1", "4", "and now?")
	asked = questions(t, s, first.ChatID)
	assert.Equal(t, []llm.BlockType{llm.BlockContext, llm.BlockText}, blockTypes(t, asked[len(asked)-1]))
	assert.NotContains(t, newestContextOf(t, s, first.ChatID), viewHeading)
}

// readWebapp is a LogsRead action whose View around this is tailWebapp's view at a
// moment, unpinned.
func readWebapp() tools.LogsReadAction {
	at := time.Date(2026, 10, 17, 9, 14, 0, 0, time.UTC)
	around := tailWebapp()
	around.Anchor, around.PinToEnd = tools.LogsViewAnchor{Kind: tools.LogsAnchorAt, At: &at}, false
	first := logs.PageFromFirst
	return tools.LogsReadAction{LogsScopeInput: around.LogsScopeInput, PageFrom: &first, Lines: []tools.LogsReadLine{}, ViewAround: &around}
}

// seedSubagentCall files a LogsRead call showing shown in a subagent of a settled
// turn of chatID.
func seedSubagentCall(t *testing.T, db *appdb.DB, chatID ChatID, toolUseID string, shown *tools.Action) ToolCallID {
	t.Helper()
	at := time.Now()
	turn := seedTurn(t, db, chatID, at)
	settleSeededRun(t, db, turn.Run, RunSucceeded, at)
	sub, llmCall, id := appdb.NewID(), appdb.NewID(), ToolCallID(appdb.NewID())
	_, err := db.Write.Exec(`INSERT INTO agent_runs (id, parent_run_id, agent_type, app_version, trigger, chat_id, provider, model, dialect, task, status, created_at)
		VALUES (?, ?, 'general-purpose', 'test', 'agent', ?, 'fake', 'fake', 'fake', 'p', 'succeeded', ?)`, sub, string(turn.Run), string(chatID), millis(at))
	require.NoError(t, err)
	_, err = db.Write.Exec(`INSERT INTO llm_calls (id, run_id, seq, provider, model, started_at) VALUES (?, ?, 0, 'fake', 'fake', ?)`, llmCall, sub, millis(at))
	require.NoError(t, err)
	raw, err := json.Marshal(shown)
	require.NoError(t, err)
	_, err = db.Write.Exec(`INSERT INTO tool_calls (id, llm_call_id, seq, tool_name, contract_name, tool_use_id, arguments, status, shown_action, created_at)
		VALUES (?, ?, 0, 'LogsRead', 'logsread/v1', ?, '{}', 'succeeded', ?, ?)`, string(id), llmCall, toolUseID, string(raw), millis(at))
	require.NoError(t, err)
	return id
}

// A snapshot of a view opened from a LogsRead call restates the read's View around
// this, a subagent's read too; a read with none is refused.
func TestASendCarriesAReadsViewAround(t *testing.T) {
	t.Parallel()

	s := serviceWithClusterCards(t, &stubClusterCards{card: "card"})
	first := sendAndSettle(t, s, nil, "1", "1", "why did it fail?")
	read := readWebapp()
	calls := map[string]ToolCallID{
		"the chat's":   seedViewCall(t, s.db, first.ChatID, "toolu_read", &tools.Action{LogsRead: &read}),
		"a subagent's": seedSubagentCall(t, s.db, first.ChatID, "toolu_sub", &tools.Action{LogsRead: &read}),
	}
	for name, call := range calls {
		msg, err := sendWithView(t, s, &first.ChatID, name, &Snapshot{CallID: call, Lines: 2})
		require.NoError(t, err, name)
		awaitSettled(t, s, msg.ChatID, msg.ID)
		context := newestContextOf(t, s, first.ChatID)
		assert.Contains(t, context, `"anchor":"2026-10-17T09:14:00Z"`, name)
		assert.Contains(t, context, `"pinToEnd":false`, name)
	}

	none := readWebapp()
	none.ViewAround = nil
	bare := seedViewCall(t, s.db, first.ChatID, "toolu_none", &tools.Action{LogsRead: &none})
	_, err := sendWithView(t, s, &first.ChatID, "none", &Snapshot{CallID: bare})
	assert.ErrorIs(t, err, ErrBadRequest)
}

// seedWatchTask files the task of a watch call: running with no result, or
// finished with result.
func seedWatchTask(t *testing.T, db *appdb.DB, chatID ChatID, call ToolCallID, status string, result *tools.WatchResult) {
	t.Helper()
	var raw sql.NullString
	if result != nil {
		b, err := json.Marshal(result)
		require.NoError(t, err)
		raw = sql.NullString{String: string(b), Valid: true}
	}
	_, err := db.Write.Exec(`INSERT INTO background_tasks (id, chat_id, tool_call_id, output_path, status, started_at, result)
		VALUES (?, ?, ?, '/r/t.output', ?, 0, ?)`, appdb.NewID(), string(chatID), string(call), status, raw)
	require.NoError(t, err)
}

// A snapshot of a view opened from a LogsWatch call restates the watch's View
// around this, off its task's result; a watch still running has none, and is
// refused.
func TestASendCarriesAWatchsViewAround(t *testing.T) {
	t.Parallel()

	s := serviceWithClusterCards(t, &stubClusterCards{card: "card"})
	first := sendAndSettle(t, s, nil, "1", "1", "why did it panic?")
	around := readWebapp().ViewAround
	watch := &tools.Action{LogsWatch: &tools.LogsWatchAction{LogsScopeInput: around.LogsScopeInput, Limit: 1}}
	matched := seedViewCall(t, s.db, first.ChatID, "toolu_watch", watch)
	seedWatchTask(t, s.db, first.ChatID, matched, taskMatched, &tools.WatchResult{Lines: []tools.LogsReadLine{}, ViewAround: around})

	msg, err := sendWithView(t, s, &first.ChatID, "2", &Snapshot{CallID: matched, Lines: 2})
	require.NoError(t, err)
	awaitSettled(t, s, msg.ChatID, msg.ID)
	context := newestContextOf(t, s, first.ChatID)
	assert.Contains(t, context, `{"call":"toolu_watch"`)
	assert.Contains(t, context, `"anchor":"2026-10-17T09:14:00Z"`)

	running := seedViewCall(t, s.db, first.ChatID, "toolu_running", watch)
	seedWatchTask(t, s.db, first.ChatID, running, taskRunning, nil)
	expired := seedViewCall(t, s.db, first.ChatID, "toolu_expired", watch)
	seedWatchTask(t, s.db, first.ChatID, expired, taskExpired, &tools.WatchResult{Lines: []tools.LogsReadLine{}})
	for name, call := range map[string]ToolCallID{"running": running, "with no line": expired} {
		_, err := sendWithView(t, s, &first.ChatID, name, &Snapshot{CallID: call})
		assert.ErrorIs(t, err, ErrBadRequest, name)
	}
}
