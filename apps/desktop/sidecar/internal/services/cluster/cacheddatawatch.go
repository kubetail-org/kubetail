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

// The shared loop behind every cached-data watch (kinds, objects, events). One loop for
// all three, because the bookmark discipline and the diff are protocol rules rather than
// per-watch choices.
//
// The mechanism, end to end — there is no polling anywhere in it:
//
//  1. Claim the cache's store (never creating its file) and subscribe to the store's
//     change bus BEFORE reading, so a write landing between the two costs one redundant
//     re-read instead of a lost frame.
//  2. Send the snapshot: every row as Added, then one Bookmark. A cache with no file yet
//     is empty, not pending — the Bookmark still goes out, and the watch then waits for
//     the manager to announce the file's creation.
//  3. Wait for change notifications. Every committed write pings the bus (a bare
//     coalesced signal, no payload); the loop collapses a burst of pings into one read of
//     what moved past its cursor — the rows written above it, the uids deleted above it —
//     and sends one frame per change. A Deleted frame carries the row's last-known value,
//     since it is gone from the store and the client keys the removal by id. The kinds
//     watch re-reads the whole catalog and diffs it instead, and that full read is also
//     every watch's fallback for a cursor that can no longer be used.
//  4. The store closing (a clear, a teardown, shutdown) ends the watch cleanly; the
//     client reconnects and re-snapshots against whatever replaced it.
package cluster

import (
	"context"
	"time"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/cluster/internal/kubestore"
)

// cachedDataWatchStores is the registry as a watch reaches it: claim a cache whose file exists,
// or learn when one is created. **Never a create** — a read that created a file would
// resurrect a cache being torn down as an orphan nothing will ever name again.
type cachedDataWatchStores interface {
	OpenExisting(cacheID int64) (*kubestore.Store, bool, error)
	ListenForCacheOpenChanges(cacheID int64) kubestore.CacheOpenChangeNotifications
}

// changesRead is what one collection answers about a cursor. The store's own type, since
// the loop only forwards it.
type changesRead[T any] func(ctx context.Context, s *kubestore.Store, since int64) (kubestore.Changes[T], error)

// cachedDataWatchSpec is what one watch contributes to the shared loop: which cache, how to
// read it, and how a row becomes a frame.
//
// Every row type is comparable — the reads serve identity, never a body — so the diff is
// ==. For objects that is (uid, resourceVersion) in effect, since the server moves the
// resourceVersion on every write and the rest of the row is immutable per uid.
type cachedDataWatchSpec[T comparable, F any] struct {
	stores  cachedDataWatchStores
	cacheID int64
	// busKeys narrows the change subscription; empty means every key.
	busKeys []string
	// debounce is how long after a burst's first ping the re-read runs, and retry paces
	// a re-read that failed. Parameters, so a test picks its own timescale.
	debounce time.Duration
	retry    time.Duration

	key func(T) string
	// read is the whole collection at the cursor it is current at — the snapshot, and the
	// fallback when a cursor can no longer be used.
	read func(ctx context.Context, s *kubestore.Store) ([]T, kubestore.Cursor, error)
	// changes reads what moved past a cursor. Nil for the kinds watch, which diffs on every
	// re-read: ~150 rows carrying counts that move under them, so there is nothing to gain.
	changes changesRead[T]

	// frame builds one frame, and is handed the store so a watch can fetch what the diff
	// deliberately did not read — the objects watch's body. The other two ignore both.
	frame    func(ctx context.Context, s *kubestore.Store, t CollectionWatchFrameType, row T) F
	bookmark F
}

// runCachedDataWatch streams the cache's rows: the snapshot as Added frames, one Bookmark
// closing it, then a diff per debounced burst of writes.
//
// **It never ends with an error for a cache that went away.** A Clear or a teardown is
// expected — a user pressing a button — and a non-nil Stream.Err() reaches the client as
// a watch failure, which is an error toast per open watch and a suppressed backoff reset.
// The reconnect re-snapshots against the fresh file, so the silence costs nothing. Err is
// for a read that is actually broken.
func runCachedDataWatch[T comparable, F any](ctx context.Context, spec cachedDataWatchSpec[T, F]) *Stream[F] {
	return NewStream(ctx, spec.pump)
}

func (w cachedDataWatchSpec[T, F]) pump(ctx context.Context, out chan<- F) error {
	store, sub, err := w.bind()
	if err != nil {
		return watchEnd(err)
	}
	// The claim keeps the cache's file open while this watch reads it, and the receiver is
	// registered on that file's bus — both are ours to give back, whichever way the loop
	// ends. Closed over the variables, since awaitOpen replaces them.
	defer func() {
		if store != nil {
			store.Release()
			sub.Close()
		}
	}()

	// The snapshot, whether or not a store answered: a cache with no file yet is empty,
	// not pending, and holding the Bookmark back would have the client render a spinner
	// over a question already answered. Diffing against nothing sends every row as Added.
	prev := map[string]T{}
	// What everything in prev is current with. A watch that bound late has read nothing and
	// starts at the zero cursor: position 0, which no row is ever stamped with, under no
	// Kind — so its first resync covers the whole collection, which is what it needs.
	var cursor kubestore.Cursor
	if store != nil {
		rows, at, err := w.read(ctx, store)
		if err != nil {
			return err
		}
		var ok bool
		if prev, ok = w.sendDiff(ctx, out, store, prev, rows); !ok {
			return nil
		}
		cursor = at
	}
	if !sendFrame(ctx, out, w.bookmark) {
		return nil
	}

	// A store bound after the empty snapshot may hold rows written before we subscribed;
	// arming the debounce (below) reads them in, and anything later pings the bus we now
	// hold.
	armed := false
	if store == nil {
		if store, sub, err = w.awaitOpen(ctx); err != nil {
			return watchEnd(err)
		}
		if store == nil {
			return nil
		}
		armed = true
	}

	// Both timers start disarmed; Reset arms them. (Go's timers deliver no stale tick
	// after Stop, so neither needs a drain.)
	debounce := time.NewTimer(w.debounce)
	debounce.Stop()
	retry := time.NewTimer(w.retry)
	retry.Stop()

	// full re-reads the collection and diffs it — the snapshot's own path, and what a cursor
	// that can no longer be used falls back to. It takes the read's own cursor, so whatever
	// the plural resolves to now is what prev is current with.
	full := func() bool {
		rows, at, err := w.read(ctx, store)
		if err != nil {
			retry.Reset(w.retry)
			return true
		}
		var ok bool
		if prev, ok = w.sendDiff(ctx, out, store, prev, rows); !ok {
			return false
		}
		cursor = at
		return true
	}

	// resync reads what moved, sends the frames, and says whether to keep going. A failed
	// read arms its own retry rather than ending the stream: the bus is keyed by what was
	// written, so a kind nobody writes to may not ping for hours, and one transient error
	// would leave the client's table empty until it did.
	resync := func() bool {
		if w.changes == nil {
			return full()
		}
		c, err := w.changes(ctx, store, cursor.Seq)
		if err != nil {
			retry.Reset(w.retry)
			return true
		}
		// Two answers the loop must not take at face value, both of them empty and neither
		// a fault. A cursor below the trim mark has lost deletes the log no longer holds —
		// the mark is the highest position removed, so a cursor AT it has seen everything
		// still above. And a read answering under a different Kind — the catalog remapped
		// the plural onto a renamed Kind, or dropped its row — holds the rows this watch
		// has, and the deletes the old Kind's worker logged, in neither range. One full
		// read is what either costs.
		if cursor.Seq < c.Trimmed || c.At.Kind != cursor.Kind {
			return full()
		}
		if !w.apply(ctx, out, store, prev, c) {
			return false
		}
		// The cursor moves only once every frame is out: a consumer that left mid-burst
		// leaves it where it was, so the reconnect's read covers the same changes again —
		// advancing over half-sent frames would lose them for good.
		cursor = c.At
		return true
	}

	if armed {
		debounce.Reset(w.debounce)
	}

	for {
		select {
		case <-ctx.Done():
			return nil
		case _, ok := <-sub.Chan():
			if !ok {
				// The file under this watch closed — a Clear, a teardown, a shutdown.
				// A clean end, so the client reconnects silently into the fresh snapshot.
				return nil
			}
			// Armed once per burst, never re-armed per ping: extending the timer on
			// every ping would starve the re-read under a sustained write storm.
			if !armed {
				debounce.Reset(w.debounce)
				armed = true
			}
		case <-debounce.C:
			armed = false
			if !resync() {
				return nil
			}
		case <-retry.C:
			if !resync() {
				return nil
			}
		}
	}
}

// apply sends one frame per change the store reported, with false when the consumer is
// gone. prev is updated in place.
func (w cachedDataWatchSpec[T, F]) apply(ctx context.Context, out chan<- F, store *kubestore.Store, prev map[string]T, c kubestore.Changes[T]) bool {
	// Deletes first: a uid can be in both ranges — a clear logs a delete per row and the
	// relist writes the same objects back above it — and the writes range only ever carries
	// rows that still exist, so this order lands on the live row.
	for _, uid := range c.Deleted {
		old, held := prev[uid]
		if !held {
			// It came and went between two reads: the client never held it, so a frame
			// here would be a departure it would have to invent a row for.
			continue
		}
		if !sendFrame(ctx, out, w.frame(ctx, store, CollectionWatchFrameDeleted, old)) {
			return false
		}
		delete(prev, uid)
	}
	for _, r := range c.Written {
		k := w.key(r)
		t := CollectionWatchFrameAdded
		if _, held := prev[k]; held {
			t = CollectionWatchFrameModified
		}
		if !sendFrame(ctx, out, w.frame(ctx, store, t, r)) {
			return false
		}
		prev[k] = r
	}
	return true
}

// sendDiff sends one frame per difference between prev and rows — Added, Modified, or
// Deleted (carrying the row's last-known value) — and returns the new by-key snapshot,
// with ok false when the consumer is gone.
func (w cachedDataWatchSpec[T, F]) sendDiff(ctx context.Context, out chan<- F, store *kubestore.Store, prev map[string]T, rows []T) (map[string]T, bool) {
	next := make(map[string]T, len(rows))
	for _, r := range rows {
		k := w.key(r)
		next[k] = r
		old, held := prev[k]
		switch {
		case !held:
			if !sendFrame(ctx, out, w.frame(ctx, store, CollectionWatchFrameAdded, r)) {
				return next, false
			}
		case old != r:
			if !sendFrame(ctx, out, w.frame(ctx, store, CollectionWatchFrameModified, r)) {
				return next, false
			}
		}
	}
	for k, old := range prev {
		if _, held := next[k]; !held {
			if !sendFrame(ctx, out, w.frame(ctx, store, CollectionWatchFrameDeleted, old)) {
				return next, false
			}
		}
	}
	return next, true
}

// bind claims the cache's store and subscribes to its change notifications, or answers nil when the
// cache has no file yet. It never waits and never creates: waiting is awaitOpen's.
//
// **It claims rather than borrowing an already-open file**, because nothing holds a cache
// open while it is idle: the workers release on pause and on shutdown, so a borrowing read
// would report a paused cache as empty over rows that are still on disk.
//
// Subscribing before reading is what closes the ordering gap — a write landing between the
// two costs one idempotent re-read rather than a lost frame.
//
// A nil store with a nil error is a cache with no file yet, which awaitOpen waits for. Any
// error is terminal — cacheGone tells the expected endings from the broken ones — because
// no wait resolves them: ListenForCacheOpenChanges fires when a file is CREATED, so a cache whose file is
// already there and will not open would park on a signal that never comes.
func (w cachedDataWatchSpec[T, F]) bind() (*kubestore.Store, kubestore.ChangeNotifications, error) {
	store, ok, err := w.stores.OpenExisting(w.cacheID)
	if err != nil {
		return nil, nil, err
	}
	if !ok {
		return nil, nil, nil
	}
	sub, err := store.ListenForChanges(w.busKeys...)
	if err != nil {
		// The file went between the two calls — a clear, a teardown. Terminal, and
		// cacheGone reads it as the clean ending it is.
		store.Release()
		return nil, nil, err
	}
	return store, sub, nil
}

// awaitOpen blocks until the cache's file exists and binds to it. A nil store with a nil
// error means ctx ended or the manager shut down first. Binding is re-tried after
// registering and after every open signal, so a file that opens (or opens and closes
// again) at any point in between is never missed.
func (w cachedDataWatchSpec[T, F]) awaitOpen(ctx context.Context) (*kubestore.Store, kubestore.ChangeNotifications, error) {
	opens := w.stores.ListenForCacheOpenChanges(w.cacheID)
	defer opens.Close()
	for {
		store, sub, err := w.bind()
		if err != nil {
			return nil, nil, err
		}
		if store != nil {
			return store, sub, nil
		}
		select {
		case <-ctx.Done():
			return nil, nil, nil
		case _, ok := <-opens.Chan():
			if !ok {
				return nil, nil, nil
			}
		}
	}
}

// watchEnd turns a terminal bind failure into the stream's ending. A cache that went away
// ends clean — the client reconnects into whatever replaced it — and everything else is
// reported, because a stream that stops silently leaves a stalled table and no reason for
// it.
func watchEnd(err error) error {
	if cacheGone(err) {
		return nil
	}
	return err
}
