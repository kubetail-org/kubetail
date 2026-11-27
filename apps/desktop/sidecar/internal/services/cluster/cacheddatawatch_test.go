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

package cluster

import (
	"context"
	"errors"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/testutil"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/cluster/internal/kubestore"
)

// row is the stand-in the loop is tested over, the way collectionWatch is tested once over its
// own: what the loop does is the same for all three watches, and testing it per watch would
// pin the projection three times and the protocol none.
type row struct {
	UID  string
	Data string
}

// frame is row's delta frame.
type frame struct {
	Type CollectionWatchFrameType
	Row  *row
}

// watchFixture drives one loop: a real store manager, a hand-driven read, and the frames.
type watchFixture struct {
	t     *testing.T
	m     *kubestore.Manager
	store *kubestore.Store

	mu   sync.Mutex
	rows []row
	err  error
	// reads counts the FULL read calls, so a test can pin a debounce collapsing a burst —
	// and that a changes-driven loop did not fall back to one.
	reads int
	// at is the cursor the full read answers at, and what the changes hook counts from.
	at kubestore.Cursor
	// next is what the changes hook answers, and changesErr fails it. Hand-driven, so a
	// loop test says what moved without going through a store.
	next       kubestore.Changes[row]
	changesErr error
	changes    int
	// built fires as each frame is built, which is the moment before it is sent: a test
	// that wants the loop parked in a send waits for one more build than the buffer holds.
	built *testutil.Probe[CollectionWatchFrameType]
}

func newWatchFixture(t *testing.T) *watchFixture {
	t.Helper()
	m := kubestore.NewManager(t.TempDir(), kubestore.Retention{})
	t.Cleanup(func() { require.NoError(t, m.Close()) })
	return &watchFixture{t: t, m: m, built: testutil.NewProbe[CollectionWatchFrameType](16)}
}

// open creates the cache's file, the way a worker or the sweep does.
func (f *watchFixture) open() {
	f.t.Helper()
	store, err := f.m.OpenOrCreate(1)
	require.NoError(f.t, err)
	f.store = store
}

// set replaces what the next read answers, and clears any failure standing over it.
func (f *watchFixture) set(rows ...row) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rows, f.err = rows, nil
}

// readAs is the identity the full read answers under — the Kind a plural resolves to.
func (f *watchFixture) readAs(kind string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.at.Kind = kind
}

// fail makes the next reads fail until set is called again.
func (f *watchFixture) fail(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.err = err
}

func (f *watchFixture) read(context.Context, *kubestore.Store) ([]row, kubestore.Cursor, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reads++
	if f.err != nil {
		return nil, kubestore.Cursor{}, f.err
	}
	return append([]row(nil), f.rows...), f.at, nil
}

// push is what the next changes read answers: the rows written and the uids deleted above
// the cursor, at a position one past it.
func (f *watchFixture) push(c kubestore.Changes[row]) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.at.Seq++
	c.At = f.at
	f.next, f.changesErr = c, nil
}

// pushRaw sets an answer verbatim, for the two a reader must not take at face value: a
// cursor below the trim mark and an answer under a different Kind.
func (f *watchFixture) pushRaw(c kubestore.Changes[row]) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.next, f.changesErr = c, nil
}

// failChanges makes the next changes reads fail until push is called again.
func (f *watchFixture) failChanges(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.changesErr = err
}

func (f *watchFixture) readChanges(_ context.Context, _ *kubestore.Store, since int64) (kubestore.Changes[row], error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.changes++
	if f.changesErr != nil {
		return kubestore.Changes[row]{}, f.changesErr
	}
	if since >= f.next.At.Seq {
		return kubestore.Changes[row]{At: f.next.At}, nil
	}
	return f.next, nil
}

func (f *watchFixture) changesCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.changes
}

func (f *watchFixture) readCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.reads
}

// ping is a write landing on the bus.
func (f *watchFixture) ping() {
	f.t.Helper()
	require.NoError(f.t, f.store.SyncKinds(context.Background(), nil, true, 7))
}

// start runs the loop with the test's own cadences, diffing on every re-read the way the
// kinds watch does.
func (f *watchFixture) start(ctx context.Context, debounce, retry time.Duration) *Stream[frame] {
	return f.run(ctx, debounce, retry, nil)
}

// startWithChanges runs the loop over the changes hook, the way the objects and events
// watches do.
func (f *watchFixture) startWithChanges(ctx context.Context, debounce, retry time.Duration) *Stream[frame] {
	return f.run(ctx, debounce, retry, f.readChanges)
}

func (f *watchFixture) run(ctx context.Context, debounce, retry time.Duration, changes changesRead[row]) *Stream[frame] {
	return runCachedDataWatch(ctx, cachedDataWatchSpec[row, frame]{
		changes:  changes,
		stores:   f.m,
		cacheID:  1,
		debounce: debounce,
		retry:    retry,
		key:      func(r row) string { return r.UID },
		read:     f.read,
		frame: func(_ context.Context, _ *kubestore.Store, t CollectionWatchFrameType, r row) frame {
			f.built.Fire(t)
			return frame{Type: t, Row: &r}
		},
		bookmark: frame{Type: CollectionWatchFrameBookmark},
	})
}

// recvFrame takes the next frame, bounded by the failsafe.
func recvFrame(t *testing.T, s *Stream[frame]) frame {
	t.Helper()
	return testutil.Recv(t, s.Frames, "a watch frame")
}

// The snapshot is every row as Added, then the Bookmark that closes it — never an empty
// state before the Bookmark, which a client renders as "nothing here".
func TestCacheWatchEmitsTheSnapshotThenTheBookmark(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	f := newWatchFixture(t)
	f.open()
	f.set(row{UID: "a", Data: "1"}, row{UID: "b", Data: "1"})

	s := f.start(ctx, time.Millisecond, time.Millisecond)

	assert.Equal(t, frame{Type: CollectionWatchFrameAdded, Row: &row{UID: "a", Data: "1"}}, recvFrame(t, s))
	assert.Equal(t, frame{Type: CollectionWatchFrameAdded, Row: &row{UID: "b", Data: "1"}}, recvFrame(t, s))
	assert.Equal(t, CollectionWatchFrameBookmark, recvFrame(t, s).Type)
}

// The three ways a re-read differs from the snapshot before it. A departure carries its
// last-known row, since the client keys the removal by UID and the row is gone from disk.
func TestCacheWatchDiffsAgainstThePreviousSnapshot(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	f := newWatchFixture(t)
	f.open()
	f.set(row{UID: "a", Data: "1"}, row{UID: "gone", Data: "1"})

	s := f.start(ctx, time.Millisecond, time.Millisecond)
	recvFrame(t, s)
	recvFrame(t, s)
	require.Equal(t, CollectionWatchFrameBookmark, recvFrame(t, s).Type)

	f.set(row{UID: "a", Data: "2"}, row{UID: "new", Data: "1"})
	f.ping()

	got := map[CollectionWatchFrameType]row{}
	for range 3 {
		fr := recvFrame(t, s)
		got[fr.Type] = *fr.Row
	}
	assert.Equal(t, row{UID: "a", Data: "2"}, got[CollectionWatchFrameModified])
	assert.Equal(t, row{UID: "new", Data: "1"}, got[CollectionWatchFrameAdded])
	assert.Equal(t, row{UID: "gone", Data: "1"}, got[CollectionWatchFrameDeleted], "the departure lost its body")
}

// A burst is one re-read. The bus coalesces only what is undelivered, so a loop that drains
// promptly would otherwise re-read the whole collection once per write.
func TestCacheWatchCollapsesABurstIntoOneReRead(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		f := newWatchFixture(t)
		f.open()
		f.set(row{UID: "a", Data: "1"})

		const debounce = 40 * time.Millisecond
		s := f.start(ctx, debounce, time.Millisecond)
		recvFrame(t, s)
		require.Equal(t, CollectionWatchFrameBookmark, recvFrame(t, s).Type)
		synctest.Wait()
		require.Equal(t, 1, f.readCount())

		f.set(row{UID: "a", Data: "2"})
		// Virtual time keeps even slow SQLite writes inside one debounce window.
		// Drain each ping separately so the loop, rather than just the bus, must
		// collapse them. Later pings must not extend the first ping's deadline.
		for range 5 {
			f.ping()
			synctest.Wait()
			time.Sleep(5 * time.Millisecond)
		}
		time.Sleep(debounce - 25*time.Millisecond - time.Nanosecond)
		synctest.Wait()
		require.Equal(t, 1, f.readCount(), "the watch read before the debounce elapsed")

		time.Sleep(time.Nanosecond)
		synctest.Wait()
		require.Equal(t, 2, f.readCount(), "the burst was not collapsed at the first ping's deadline")
		assert.Equal(t, frame{Type: CollectionWatchFrameModified, Row: &row{UID: "a", Data: "2"}}, recvFrame(t, s))

		time.Sleep(debounce)
		synctest.Wait()
		assert.Equal(t, 2, f.readCount(), "the burst left an extra re-read pending")
	})
}

// A failed read is retried in place rather than ending the stream: the bus is keyed, so a
// kind nobody writes to may not ping for hours, and one transient error would otherwise
// leave the client's table empty until it did.
func TestCacheWatchRetriesAFailedReRead(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	f := newWatchFixture(t)
	f.open()
	f.set(row{UID: "a", Data: "1"})

	s := f.start(ctx, time.Millisecond, 5*time.Millisecond)
	recvFrame(t, s)
	require.Equal(t, CollectionWatchFrameBookmark, recvFrame(t, s).Type)

	before := f.readCount()
	f.fail(errors.New("disk I/O error"))
	f.ping()
	// Waited for, so the recovery below cannot land ahead of the read it is recovering
	// from — which would leave the retry untested and the test still green.
	require.Eventually(t, func() bool { return f.readCount() > before },
		5*time.Second, time.Millisecond, "the re-read to fail")

	// The retry timer is what drives the recovery; nothing else pings.
	f.set(row{UID: "a", Data: "2"})

	assert.Equal(t, CollectionWatchFrameModified, recvFrame(t, s).Type)
	assert.NoError(t, s.Err(), "a recovered read ended the stream")
}

// brokenStores is a registry whose cache has a file that will not open.
type brokenStores struct {
	m   *kubestore.Manager
	err error
}

func (b brokenStores) OpenExisting(int64) (*kubestore.Store, bool, error) { return nil, false, b.err }

func (b brokenStores) ListenForCacheOpenChanges(cacheID int64) kubestore.CacheOpenChangeNotifications {
	return b.m.ListenForCacheOpenChanges(cacheID)
}

// A store that will not open is a broken source, and the watch has to say so. Reading it as
// "no file yet" would leave the stream parked on ListenForCacheOpenChanges — which only fires when a file is
// CREATED, so a cache whose file is already there would wait for a signal that never comes.
func TestCacheWatchReportsAStoreThatWillNotOpen(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	f := newWatchFixture(t)
	boom := errors.New("database disk image is malformed")

	s := runCachedDataWatch(ctx, cachedDataWatchSpec[row, frame]{
		stores:   brokenStores{m: f.m, err: boom},
		cacheID:  1,
		debounce: time.Millisecond,
		retry:    time.Millisecond,
		key:      func(r row) string { return r.UID },
		read:     f.read,
		frame: func(_ context.Context, _ *kubestore.Store, t CollectionWatchFrameType, r row) frame {
			return frame{Type: t, Row: &r}
		},
		bookmark: frame{Type: CollectionWatchFrameBookmark},
	})

	testutil.WaitClosed(t, s.Frames, "the stream to end")
	assert.ErrorIs(t, s.Err(), boom, "a broken store read as an empty one")
}

// The loop's claim is its own to give back: it holds the cache's file open while it reads,
// so a watch that ended without releasing would keep every cache anyone ever looked at open
// for the process's life.
func TestCacheWatchReleasesItsClaimWhenItEnds(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	f := newWatchFixture(t)
	f.open()
	f.store.Release() // the workers stop; the loop becomes the only holder
	f.set(row{UID: "a", Data: "1"})

	s := f.start(ctx, time.Millisecond, time.Millisecond)
	recvFrame(t, s)
	require.Equal(t, CollectionWatchFrameBookmark, recvFrame(t, s).Type)
	require.True(t, cacheIsOpen(f.m, 1), "the loop is not holding the file open")

	cancel()
	for range s.Frames { //nolint:revive // drain until the loop has exited
	}

	assert.False(t, cacheIsOpen(f.m, 1), "the loop kept its claim after ending")
}

// A cleared cache ends the watch CLEANLY. A non-nil Err is filed as a watch failure and
// reaches the client as an error toast — for a user pressing "clear cache", once per open
// watch. The reconnect re-snapshots against the fresh file, so nothing is lost by silence.
func TestCacheWatchEndsCleanlyWhenTheStoreIsCleared(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	f := newWatchFixture(t)
	f.open()
	f.set(row{UID: "a", Data: "1"})

	s := f.start(ctx, time.Millisecond, time.Millisecond)
	recvFrame(t, s)
	require.Equal(t, CollectionWatchFrameBookmark, recvFrame(t, s).Type)

	require.NoError(t, f.m.Clear(1))

	for fr := range s.Frames {
		assert.NotEqual(t, CollectionWatchFrameDeleted, fr.Type, "the clear blanked the client's table")
	}
	assert.NoError(t, s.Err(), "a clear reported itself as a watch failure")
}

// A watch can open before anything has created the file. It must not stall: the Bookmark
// goes out on the empty snapshot, and the rows arrive as ordinary Added frames once a
// writer opens the cache.
func TestCacheWatchBookmarksBeforeTheStoreExists(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	f := newWatchFixture(t)
	f.set(row{UID: "a", Data: "1"})

	s := f.start(ctx, time.Millisecond, time.Millisecond)
	assert.Equal(t, CollectionWatchFrameBookmark, recvFrame(t, s).Type, "the watch stalled on a cache with no file")

	f.open()

	fr := recvFrame(t, s)
	assert.Equal(t, CollectionWatchFrameAdded, fr.Type)
	assert.Equal(t, row{UID: "a", Data: "1"}, *fr.Row)
}

// A consumer that has gone ends the loop wherever it is: every send is checked, so a
// cancelled watch stops at the frame it was on rather than blocking on a channel nobody
// is reading. The context is cancelled with a send already parked — Frames buffers one,
// and the second row has nowhere to go — so the check is what frees the loop rather than
// a race with the buffer.
func TestCacheWatchStopsAtTheFrameTheConsumerLeftOn(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	f := newWatchFixture(t)
	f.open()
	f.set(row{UID: "a", Data: "1"}, row{UID: "b", Data: "1"})

	stream := f.start(ctx, time.Millisecond, time.Millisecond)
	cancel()

	testutil.WaitClosed(t, stream.Frames, "the loop to stop for a consumer that is gone")
	assert.NoError(t, stream.Err(), "a consumer leaving is not a watch failure")
}

// The Bookmark is a send like any other: one row fills the buffer, and the bookmark behind
// it is where a departed consumer ends the loop. Cancelled once that row is built and
// nobody is reading, so the loop is at the bookmark and not at the row before it.
func TestCacheWatchStopsOnTheBookmarkWhenTheConsumerLeft(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	f := newWatchFixture(t)
	f.open()
	f.set(row{UID: "a", Data: "1"})

	stream := f.start(ctx, time.Millisecond, time.Millisecond)
	f.built.Await(t, "the snapshot's one row")
	cancel()

	testutil.WaitClosed(t, stream.Frames, "the loop to stop on the bookmark")
	assert.NoError(t, stream.Err())
}

// A snapshot that cannot be read is a watch failure — unlike a re-read, which retries.
// There is no last-known data to hold here, so the client is told rather than left on an
// empty table with nothing to say why.
func TestCacheWatchEndsWhenTheSnapshotWillNotRead(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	boom := errors.New("read failed")
	f := newWatchFixture(t)
	f.open()
	f.fail(boom)

	stream := f.start(ctx, time.Millisecond, time.Millisecond)

	testutil.WaitClosed(t, stream.Frames, "the watch to end")
	assert.ErrorIs(t, stream.Err(), boom)
}

// Every kind of difference is a send that can find the consumer gone. Driven directly
// because the loop's own select would race the cancellation, and what is under test is
// that each of the three sends is checked rather than assumed.
func TestSendDiffStopsAtAnyFrameWhenTheConsumerIsGone(t *testing.T) {
	t.Parallel()

	diffs := map[string]struct {
		prev map[string]row
		rows []row
	}{
		"added":    {prev: map[string]row{}, rows: []row{{UID: "a", Data: "1"}}},
		"modified": {prev: map[string]row{"a": {UID: "a", Data: "1"}}, rows: []row{{UID: "a", Data: "2"}}},
		"deleted":  {prev: map[string]row{"a": {UID: "a", Data: "1"}}},
	}
	w := cachedDataWatchSpec[row, frame]{
		key: func(r row) string { return r.UID },
		frame: func(_ context.Context, _ *kubestore.Store, t CollectionWatchFrameType, r row) frame {
			return frame{Type: t, Row: &r}
		},
	}

	for name, diff := range diffs {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			// Buffered like Frames and already full, so the one send each case makes has
			// nowhere to go — otherwise the buffer takes it and the check never runs.
			out := make(chan frame, 1)
			out <- frame{}

			_, ok := w.sendDiff(ctx, out, nil, diff.prev, diff.rows)

			assert.False(t, ok)
		})
	}
}

// A watch opened before the cache has a file waits for one, and a context that ends first
// ends the wait — cleanly, since nothing broke.
func TestCacheWatchStopsWaitingForAFileWhenTheContextEnds(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	f := newWatchFixture(t)
	stream := f.start(ctx, time.Millisecond, time.Millisecond)
	require.Equal(t, CollectionWatchFrameBookmark, recvFrame(t, stream).Type)

	cancel()

	testutil.WaitClosed(t, stream.Frames, "the wait to end with the context")
	assert.NoError(t, stream.Err())
}

// The manager shutting down under a waiting watch ends it the same way: the open feed
// closes, and there is no file coming.
func TestCacheWatchStopsWaitingWhenTheManagerShutsDown(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	f := newWatchFixture(t)
	stream := f.start(ctx, time.Millisecond, time.Millisecond)
	require.Equal(t, CollectionWatchFrameBookmark, recvFrame(t, stream).Type)

	require.NoError(t, f.m.Close())

	testutil.WaitClosed(t, stream.Frames, "the wait to end with the manager")
	assert.NoError(t, stream.Err())
}

// The point of the whole change: a ping re-reads what moved past the cursor, not the
// collection. A write to one held row is one Modified, and the full read stays at the one
// the snapshot made.
func TestCacheWatchReadsWhatMovedPastItsCursor(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	f := newWatchFixture(t)
	f.open()
	f.set(row{UID: "a", Data: "1"})

	s := f.startWithChanges(ctx, time.Millisecond, time.Millisecond)
	recvFrame(t, s)
	require.Equal(t, CollectionWatchFrameBookmark, recvFrame(t, s).Type)
	require.Equal(t, 1, f.readCount())

	f.push(kubestore.Changes[row]{Written: []row{{UID: "a", Data: "2"}}})
	f.ping()

	fr := recvFrame(t, s)
	assert.Equal(t, CollectionWatchFrameModified, fr.Type)
	assert.Equal(t, row{UID: "a", Data: "2"}, *fr.Row)
	assert.Equal(t, 1, f.readCount(), "the loop fell back to a full read")
}

// A delete reaches the client as the row it last held: the store logged only the uid,
// because the row it describes is gone from disk.
func TestCacheWatchSendsADeleteFromWhatItHeld(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	f := newWatchFixture(t)
	f.open()
	f.set(row{UID: "a", Data: "1"})

	s := f.startWithChanges(ctx, time.Millisecond, time.Millisecond)
	recvFrame(t, s)
	require.Equal(t, CollectionWatchFrameBookmark, recvFrame(t, s).Type)

	f.push(kubestore.Changes[row]{Deleted: []string{"a"}})
	f.ping()

	fr := recvFrame(t, s)
	assert.Equal(t, CollectionWatchFrameDeleted, fr.Type)
	assert.Equal(t, row{UID: "a", Data: "1"}, *fr.Row, "the departure lost its body")
}

// A uid that came and went between two reads is in the log and in no table, and the client
// never held it — so it is nothing, not a Deleted for a row it would have to invent.
func TestCacheWatchDropsADeleteForARowItNeverHeld(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	f := newWatchFixture(t)
	f.open()
	f.set(row{UID: "a", Data: "1"})

	s := f.startWithChanges(ctx, time.Millisecond, time.Millisecond)
	recvFrame(t, s)
	require.Equal(t, CollectionWatchFrameBookmark, recvFrame(t, s).Type)

	// The ghost rides along with a real change, so the next frame is the assertion: had the
	// ghost produced one, it would be this frame.
	f.push(kubestore.Changes[row]{
		Deleted: []string{"ghost"},
		Written: []row{{UID: "b", Data: "1"}},
	})
	f.ping()

	fr := recvFrame(t, s)
	assert.Equal(t, CollectionWatchFrameAdded, fr.Type)
	assert.Equal(t, row{UID: "b", Data: "1"}, *fr.Row)
}

// Deletes are applied BEFORE writes, so a uid the log says went and the tables say is back
// — ClearKind logs a delete per row and the restarted sync lists the same objects above it
// — ends as the live row rather than as a row the client dropped.
func TestCacheWatchAppliesDeletesBeforeWrites(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	f := newWatchFixture(t)
	f.open()
	f.set(row{UID: "a", Data: "1"})

	s := f.startWithChanges(ctx, time.Millisecond, time.Millisecond)
	recvFrame(t, s)
	require.Equal(t, CollectionWatchFrameBookmark, recvFrame(t, s).Type)

	f.push(kubestore.Changes[row]{
		Deleted: []string{"a"},
		Written: []row{{UID: "a", Data: "2"}},
	})
	f.ping()

	assert.Equal(t, CollectionWatchFrameDeleted, recvFrame(t, s).Type)
	fr := recvFrame(t, s)
	assert.Equal(t, CollectionWatchFrameAdded, fr.Type, "the re-added row was folded as a modification")
	assert.Equal(t, row{UID: "a", Data: "2"}, *fr.Row)
}

// A cursor at or below the kind's trim mark has lost deletes it never saw, so what moved
// past it can no longer be trusted: the loop re-reads the collection and diffs it, which is
// what every burst cost before the cursor existed.
func TestCacheWatchFallsBackWhenItsCursorWasTrimmed(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	f := newWatchFixture(t)
	f.open()
	f.set(row{UID: "a", Data: "1"}, row{UID: "gone", Data: "1"})

	s := f.startWithChanges(ctx, time.Millisecond, time.Millisecond)
	recvFrame(t, s)
	recvFrame(t, s)
	require.Equal(t, CollectionWatchFrameBookmark, recvFrame(t, s).Type)
	require.Equal(t, 1, f.readCount())

	// The mark is above the cursor, and the changes answer is deliberately empty: taking it
	// at face value would send nothing at all.
	f.pushRaw(kubestore.Changes[row]{At: kubestore.Cursor{Seq: 99}, Trimmed: 99})
	f.set(row{UID: "a", Data: "2"})
	f.ping()

	got := map[CollectionWatchFrameType]row{}
	for range 2 {
		fr := recvFrame(t, s)
		got[fr.Type] = *fr.Row
	}
	assert.Equal(t, row{UID: "a", Data: "2"}, got[CollectionWatchFrameModified])
	assert.Equal(t, row{UID: "gone", Data: "1"}, got[CollectionWatchFrameDeleted])
	assert.Equal(t, 2, f.readCount(), "the trimmed cursor was taken at face value")
}

// A kind whose catalog row is gone is served by nothing, and its rows are the client's to
// drop. The changes read answers under the empty Kind — an identity change — and the full
// read behind the fallback reads empty, which is the Deleted per held row.
func TestCacheWatchFallsBackWhenTheKindIsGone(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	f := newWatchFixture(t)
	f.open()
	f.readAs("Widget")
	f.set(row{UID: "a", Data: "1"})

	s := f.startWithChanges(ctx, time.Millisecond, time.Millisecond)
	recvFrame(t, s)
	require.Equal(t, CollectionWatchFrameBookmark, recvFrame(t, s).Type)

	// The plural resolves to nothing now, so the answer's Kind is empty.
	f.pushRaw(kubestore.Changes[row]{At: kubestore.Cursor{Seq: 99}})
	f.readAs("")
	f.set()
	f.ping()

	fr := recvFrame(t, s)
	assert.Equal(t, CollectionWatchFrameDeleted, fr.Type)
	assert.Equal(t, row{UID: "a", Data: "1"}, *fr.Row)
}

// A failed changes read is retried in place, like a failed re-read: nothing is sent, the
// cursor stays where it was, and the retry covers the same changes — so the frames the
// failed read would have produced arrive on it rather than being lost.
func TestCacheWatchRetriesAFailedChangesRead(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	f := newWatchFixture(t)
	f.open()
	f.set(row{UID: "a", Data: "1"})

	s := f.startWithChanges(ctx, time.Millisecond, 5*time.Millisecond)
	recvFrame(t, s)
	require.Equal(t, CollectionWatchFrameBookmark, recvFrame(t, s).Type)

	before := f.changesCount()
	f.failChanges(errors.New("disk I/O error"))
	f.ping()
	// Waited for, so the recovery below cannot land ahead of the read it recovers from —
	// which would leave the retry untested and the test still green.
	require.Eventually(t, func() bool { return f.changesCount() > before },
		5*time.Second, time.Millisecond, "the changes read to fail")

	// The retry timer is what drives the recovery; nothing else pings.
	f.push(kubestore.Changes[row]{Written: []row{{UID: "a", Data: "2"}}})

	fr := recvFrame(t, s)
	assert.Equal(t, CollectionWatchFrameModified, fr.Type)
	assert.Equal(t, row{UID: "a", Data: "2"}, *fr.Row)
	assert.Equal(t, 1, f.readCount(), "a failed changes read fell back to a full read")
	assert.NoError(t, s.Err(), "a recovered read ended the stream")
}

// A watch that bound after the empty snapshot has read nothing, so its cursor is 0 and the
// first changes read covers the whole kind: every row arrives as Added, exactly what the
// armed debounce reads in.
func TestCacheWatchBoundLateReadsTheKindFromZero(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	f := newWatchFixture(t)
	f.push(kubestore.Changes[row]{Written: []row{{UID: "a", Data: "1"}}})

	s := f.startWithChanges(ctx, time.Millisecond, time.Millisecond)
	require.Equal(t, CollectionWatchFrameBookmark, recvFrame(t, s).Type, "the watch stalled on a cache with no file")

	f.open()

	fr := recvFrame(t, s)
	assert.Equal(t, CollectionWatchFrameAdded, fr.Type)
	assert.Equal(t, row{UID: "a", Data: "1"}, *fr.Row)
	assert.Zero(t, f.readCount(), "the empty snapshot left a cursor the changes read could not use")
}

// A read answering under a different identity is a plural remapped onto a renamed Kind. Both
// ranges are keyed by it, so the rows this watch holds and the deletes the old Kind's worker
// logged are in neither — taking the empty answer would leave those rows on screen for as
// long as the watch stayed connected.
func TestCacheWatchFallsBackWhenTheKindBehindThePluralChanges(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	f := newWatchFixture(t)
	f.open()
	f.readAs("Widget")
	f.set(row{UID: "a", Data: "1"})

	s := f.startWithChanges(ctx, time.Millisecond, time.Millisecond)
	recvFrame(t, s)
	require.Equal(t, CollectionWatchFrameBookmark, recvFrame(t, s).Type)
	require.Equal(t, 1, f.readCount())

	// The renamed Kind's ranges carry neither the old row nor the delete its worker logged.
	f.pushRaw(kubestore.Changes[row]{At: kubestore.Cursor{Seq: 99, Kind: "Gadget"}})
	f.readAs("Gadget")
	f.set()
	f.ping()

	fr := recvFrame(t, s)
	assert.Equal(t, CollectionWatchFrameDeleted, fr.Type, "the rows of the Kind the plural left were kept")
	assert.Equal(t, row{UID: "a", Data: "1"}, *fr.Row)
	assert.Equal(t, 2, f.readCount())
}

// Every frame a changes read produces is a send that can find the consumer gone. Driven
// directly because the loop's own select would race the cancellation, and what is under
// test is that both sends are checked rather than assumed.
func TestApplyStopsAtAnyFrameWhenTheConsumerIsGone(t *testing.T) {
	t.Parallel()

	changes := map[string]kubestore.Changes[row]{
		"written": {Written: []row{{UID: "a", Data: "1"}}},
		"deleted": {Deleted: []string{"a"}},
	}
	w := cachedDataWatchSpec[row, frame]{
		key: func(r row) string { return r.UID },
		frame: func(_ context.Context, _ *kubestore.Store, t CollectionWatchFrameType, r row) frame {
			return frame{Type: t, Row: &r}
		},
	}

	for name, c := range changes {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			// Buffered like Frames and already full, so the one send each case makes has
			// nowhere to go — otherwise the buffer takes it and the check never runs.
			out := make(chan frame, 1)
			out <- frame{}

			ok := w.apply(ctx, out, nil, map[string]row{"a": {UID: "a", Data: "1"}}, c)

			assert.False(t, ok)
		})
	}
}

// goneStores hands out a claim whose cache has already gone, which is the file going
// between the open and the subscribe — the one gap bind cannot close by ordering.
type goneStores struct {
	m     *kubestore.Manager
	store *kubestore.Store
}

func (g goneStores) OpenExisting(int64) (*kubestore.Store, bool, error) { return g.store, true, nil }

func (g goneStores) ListenForCacheOpenChanges(cacheID int64) kubestore.CacheOpenChangeNotifications {
	return g.m.ListenForCacheOpenChanges(cacheID)
}

// A cache that went between the claim and the subscription ends the watch CLEANLY: it is a
// clear or a teardown, and a non-nil Err reaches the client as an error toast per open
// watch for a button the user pressed.
func TestCacheWatchEndsCleanlyWhenTheCacheGoesBeforeItSubscribes(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	f := newWatchFixture(t)
	f.open()
	require.NoError(t, f.m.Remove(1))

	s := runCachedDataWatch(ctx, cachedDataWatchSpec[row, frame]{
		stores:   goneStores{m: f.m, store: f.store},
		cacheID:  1,
		debounce: time.Millisecond,
		retry:    time.Millisecond,
		key:      func(r row) string { return r.UID },
		read:     f.read,
		frame: func(_ context.Context, _ *kubestore.Store, t CollectionWatchFrameType, r row) frame {
			return frame{Type: t, Row: &r}
		},
		bookmark: frame{Type: CollectionWatchFrameBookmark},
	})

	testutil.WaitClosed(t, s.Frames, "the stream to end")
	assert.NoError(t, s.Err(), "a cache that went away was reported as a watch failure")
}

// lateBreakStores has no file to bind at first and is broken by the time the watch waits
// for one — the failure awaitOpen answers with rather than parking forever.
type lateBreakStores struct {
	m    *kubestore.Manager
	err  error
	mu   sync.Mutex
	seen int
}

func (l *lateBreakStores) OpenExisting(int64) (*kubestore.Store, bool, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.seen++
	if l.seen == 1 {
		return nil, false, nil
	}
	return nil, false, l.err
}

func (l *lateBreakStores) ListenForCacheOpenChanges(cacheID int64) kubestore.CacheOpenChangeNotifications {
	return l.m.ListenForCacheOpenChanges(cacheID)
}

// A watch waiting for a file that then will not open reports it. Parking on ListenForCacheOpenChanges would
// leave the client on an empty table with no reason for it, since that signal fires when a
// file is CREATED and this one already exists.
func TestCacheWatchReportsAStoreThatBreaksWhileItWaits(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	f := newWatchFixture(t)
	boom := errors.New("database disk image is malformed")

	s := runCachedDataWatch(ctx, cachedDataWatchSpec[row, frame]{
		stores:   &lateBreakStores{m: f.m, err: boom},
		cacheID:  1,
		debounce: time.Millisecond,
		retry:    time.Millisecond,
		key:      func(r row) string { return r.UID },
		read:     f.read,
		frame: func(_ context.Context, _ *kubestore.Store, t CollectionWatchFrameType, r row) frame {
			return frame{Type: t, Row: &r}
		},
		bookmark: frame{Type: CollectionWatchFrameBookmark},
	})

	assert.Equal(t, CollectionWatchFrameBookmark, recvFrame(t, s).Type)
	testutil.WaitClosed(t, s.Frames, "the stream to end")
	assert.ErrorIs(t, s.Err(), boom)
}

// A consumer that leaves mid-burst ends the loop at the frame it was on, whichever read
// produced it. The buffer holds one, so the second frame of a burst is parked in its send
// with nowhere to go, and the cancel is what frees it — no race with the loop's own select,
// which is why this cannot be written as "cancel and see".
func TestCacheWatchStopsMidBurstWhenTheConsumerLeaves(t *testing.T) {
	t.Parallel()

	bursts := map[string]func(*watchFixture){
		"reading what moved": func(f *watchFixture) {
			f.push(kubestore.Changes[row]{
				Deleted: []string{"a"},
				Written: []row{{UID: "b", Data: "2"}, {UID: "c", Data: "1"}},
			})
		},
		// A different Kind sends the loop back to the full read, whose own diff is then
		// what the departed consumer is parked in.
		"falling back to the full read": func(f *watchFixture) {
			f.pushRaw(kubestore.Changes[row]{At: kubestore.Cursor{Seq: 9, Kind: "Gadget"}})
			f.readAs("Gadget")
			f.set(row{UID: "b", Data: "2"}, row{UID: "c", Data: "1"})
		},
	}
	for name, burst := range bursts {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			f := newWatchFixture(t)
			f.open()
			f.readAs("Widget")
			f.set(row{UID: "a", Data: "1"})

			s := f.startWithChanges(ctx, time.Millisecond, time.Millisecond)
			recvFrame(t, s)
			require.Equal(t, CollectionWatchFrameBookmark, recvFrame(t, s).Type)
			f.built.Drain()

			burst(f)
			f.ping()
			// One frame is in the buffer, the second is parked in its send.
			f.built.Await(t, "the burst's first frame")
			f.built.Await(t, "the burst's second frame")
			cancel()

			testutil.WaitClosed(t, s.Frames, "the loop to stop where the consumer left")
			assert.NoError(t, s.Err(), "a consumer leaving is not a watch failure")
		})
	}
}

// The retry path is the other way into a re-read, and it ends the same way. A failed
// changes read arms the retry with nothing sent, and the retry's own frames are where the
// departed consumer stops the loop.
func TestCacheWatchStopsOnTheRetryWhenTheConsumerLeaves(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	f := newWatchFixture(t)
	f.open()
	f.set(row{UID: "a", Data: "1"})

	s := f.startWithChanges(ctx, time.Millisecond, 5*time.Millisecond)
	recvFrame(t, s)
	require.Equal(t, CollectionWatchFrameBookmark, recvFrame(t, s).Type)
	f.built.Drain()

	before := f.changesCount()
	f.failChanges(errors.New("disk I/O error"))
	f.ping()
	require.Eventually(t, func() bool { return f.changesCount() > before },
		5*time.Second, time.Millisecond, "the changes read to fail")

	// Only the retry timer can fire now: the burst below pings nothing.
	f.push(kubestore.Changes[row]{Written: []row{{UID: "b", Data: "1"}, {UID: "c", Data: "1"}}})
	f.built.Await(t, "the retry's first frame")
	f.built.Await(t, "the retry's second frame")
	cancel()

	testutil.WaitClosed(t, s.Frames, "the loop to stop on the retry's frames")
	assert.NoError(t, s.Err())
}
