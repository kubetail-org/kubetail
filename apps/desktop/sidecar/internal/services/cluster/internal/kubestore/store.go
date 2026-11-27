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

package kubestore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/amorey/gobus/conflate"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/watch"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/sqlitepool"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/sqlstmt"
)

// The one kind whose rows live in events rather than objects: core v1 events. Any
// group may serve a Kind called "Event" — a CRD's rows are ordinary objects — so the
// events table is identified by api version and plural, never by the Kind name. Every
// cached event rolls into the schema's hardcoded ('v1','Event') count.
const (
	coreEventsAPIVersion = "v1"
	coreEventsKind       = "Event"
	coreEventsResource   = "events"
)

// Kind identifies one synced collection: the objects table is keyed by Kind while a
// watch is opened on the plural, so a writer carries both.
type Kind struct {
	APIVersion string
	Kind       string
	Resource   string
}

// isCoreEvents reports the one kind whose rows live in events rather than objects. Any
// group may serve a Kind called "Event" — a CRD's rows are ordinary objects — so this
// asks by api version and plural, never by the Kind name.
func (k Kind) isCoreEvents() bool {
	return k.APIVersion == coreEventsAPIVersion && k.Resource == coreEventsResource
}

// ChangeNotifications carries the store's change pings. The value is empty — the key is the
// whole news, and a reader answers it by re-reading and diffing, so an early or late
// ping costs one idempotent read rather than a wrong frame.
//
// **Close it when done.** A receiver ends with an error when the store closes, which is
// what reaches a live watch whose store was cleared or shut down.
type ChangeNotifications = *conflate.Receiver[string, struct{}]

// EventsKey is the events bus key: its own, so an event-storm cluster does not wake
// every object watch in the cache.
const EventsKey = "events"

// KindsKey is the catalog bus key — what the sweep's write pings. Its own, because what
// moves it is a kind appearing or leaving rather than any row count.
const KindsKey = "kinds"

// ObjectsKey is one kind's bus key, by the plural a reader opened its watch on.
func ObjectsKey(apiVersion, resource string) string {
	return "objects/" + apiVersion + "/" + resource
}

// Counts is the whole-cache tally behind the stats state watch, read off the
// trigger-maintained per-kind counts — O(kinds), never a scan of objects. Events are
// excluded: they are not a catalog kind.
type Counts struct {
	ObjectCount int
	KindCount   int
}

// file is one cache's open SQLite database and the change bus over it — what Clear
// swaps under the claims on it, and what the last Release closes. The writer pool is
// capped at one connection.
type file struct {
	// path is the main database file; the -wal/-shm sidecars sit beside it. Held so the
	// janitor can measure the file without going through the manager, whose lock it must
	// never take.
	path string
	// cacheID is the key the size verdict is published under, and what the janitor's log
	// lines name.
	cacheID int64
	// log is where the janitor warns: slog's default, or a capture in a test.
	log *slog.Logger
	db  *sqlitepool.Pool
	// readDB is the reader pool beside the writer: the watches re-read on every ping, and
	// a read must not queue behind the one write connection. Distinct from the manager's
	// openReadOnly, which opens a CLOSED cache's file per call to measure it; this serves
	// an open one for the file's life.
	readDB *sqlitepool.Pool
	// queryDB is KubeQuery's pool: read-only connections through the manager's query
	// driver, none kept idle, so nothing one statement leaves on its connection reaches
	// the next.
	queryDB *sqlitepool.Pool
	// ownQueryDB is the same kind of connection for the sidecar's own statements, kept
	// between them: a statement the sidecar wrote leaves nothing on its connection.
	ownQueryDB *sqlitepool.Pool
	// own is the statements the sidecar's readings run, compiled on ownQueryDB.
	own *ownStmts
	// onPrepare, onQuery and afterQuery are seams for a test: onPrepare runs as a
	// statement is compiled, onQuery as a query starts its statement, afterQuery as it is
	// about to let go of its connection. Nil in production.
	onPrepare, onQuery, afterQuery func()
	// queryCtx ends the file's queries when it closes. inUse counts the operations
	// running on the file, queries included, which close waits for: sql.DB.Close does not
	// wait for a connection in use, and Windows refuses to delete a file one holds open.
	// An operation registers under the manager's lock, the one every close runs under.
	queryCtx      context.Context
	cancelQueries context.CancelFunc
	inUse         sync.WaitGroup
	// set is statements prepared on both pools; a call routes by the table, never by
	// which helper it came through.
	set *sqlstmt.Set[stmtID]
	hub *conflate.Hub[string, struct{}]
	// stopJanitor retires this file's sweeper and waits for it to exit. The sweep runs on
	// the janitor's own context, so the cancel aborts it mid-statement and the wait is
	// short; without the wait, a statement still unwinding holds a connection the pools'
	// Close does not wait for, and Windows refuses to delete a file one holds open.
	stopJanitor func()
	// vacuumPages bounds what one sweep hands back: vacuumPagesPerSweep, or less in a test.
	vacuumPages int64
	// closeBudget is how long closePools waits for a connection still in use.
	closeBudget time.Duration
	// now is the wall clock in millis; a seam so a test can freeze it. Reads go through
	// stamp, never here.
	now func() int64
	// clockMu guards lastStamp, which forces the write stamps strictly upward.
	clockMu   sync.Mutex
	lastStamp int64
	// sizeVerdict is the last sweep's answer on this file's size (sizeUnknown, sizeUnder or
	// sizeOver), so the next sweep can tell a change from a repeat. Atomic because the
	// janitor writes it on its own goroutine while Stats reads it under the manager's
	// lock, which the janitor never holds.
	sizeVerdict atomic.Int32
	// janitorWakeups asks the janitor for a sweep after a commit, so a cache filling fast is
	// measured within seconds rather than at the next tick, and one that a clear shrank is
	// released as soon. Capacity one, sent without blocking: a burst of writes owes one
	// sweep, not one each.
	janitorWakeups chan struct{}
	// sizeLimitSender publishes a changed size verdict on the manager's sizeLimitHub, so a
	// reader watches every cache through one subscription rather than binding to files as
	// they open.
	sizeLimitSender *conflate.Sender[int64, struct{}]
}

// Store is one holder's claim on one cache, and everything done through it — every Store
// is a claim, and owes a Release. The file under it is resolved per call, so a Clear's swap
// reaches every holder and a Remove leaves them answering ErrClosed rather than writing
// into an unlinked inode.
type Store struct {
	m       *Manager
	cacheID int64
	e       *entry
	// bound is the file this store was handed, when it must not follow a swap. Clear
	// installs a fresh empty file on the same entry, and a reader that followed it would
	// answer "no rows" for a cache that was full — which a collection watch reports as a
	// Deleted for every row it holds. Nil means "whatever the entry holds", which is what
	// a writer wants: its next write belongs in the current file.
	bound *file
}

// wakeJanitor asks for a sweep. Dropped when one is already owed, so a relist's every
// write after the first costs nothing.
func (f *file) wakeJanitor() {
	select {
	case f.janitorWakeups <- struct{}{}:
	default:
	}
}

// stamp is the updated_at every write records: the wall clock, forced strictly
// increasing. A relist prunes by comparing against its own boundary, and the clock has
// millisecond resolution — so two writes inside one tick would be indistinguishable,
// and a re-list that ran in the same millisecond as the rows it supersedes would keep
// every one of them.
func (f *file) stamp() int64 {
	f.clockMu.Lock()
	defer f.clockMu.Unlock()

	next := f.now()
	if next <= f.lastStamp {
		next = f.lastStamp + 1
	}
	f.lastStamp = next
	return next
}

// writeStamp is what one write transaction marks its rows with, taken once before
// anything it writes so rows committed together carry the same pair. The two rise together
// within one file's life and not across a reopen, since only the counter is on disk —
// nothing may assume an order in one from the other (see trimDeletes).
type writeStamp struct {
	// at is the wall clock in millis — what a relist prunes by.
	at int64
	// seq is the position a reader resumes from.
	seq int64
}

// writeStamp takes this transaction's pair. The counter read is a write, so it must run
// on the transaction that will use the number, not beside it.
func (f *file) writeStamp(ctx context.Context, st stmts) (writeStamp, error) {
	seq, err := nextSeq(ctx, st)
	if err != nil {
		return writeStamp{}, err
	}
	return writeStamp{at: f.stamp(), seq: seq}, nil
}

// notify announces a commit: readers of key re-read, and the janitor sweeps. Every write
// path that commits calls it, so no commit grows or shrinks the file unmeasured. A failed
// send is a closed hub — a store shutting down, which the subscriber learns from its own
// receiver.
func (f *file) notify(key string) {
	_ = f.hub.Sender().Send(key, struct{}{})
	f.wakeJanitor()
}

// close closes the pools and ends every subscriber, which is how a clear or a
// shutdown reaches a live watch.
//
// It interrupts the file's queries and waits for every operation on it to end first, so
// the file is not deleted under a connection one still holds.
func (f *file) close() error {
	f.cancelQueries()
	f.inUse.Wait()
	if f.stopJanitor != nil {
		f.stopJanitor()
	}
	f.hub.Close()
	return errors.Join(f.set.Close(), f.own.close(), f.closePools())
}

// closePools closes the pools and waits for a connection database/sql still holds, all
// of them under one budget. Called again on a file whose wait ran out, it waits again.
// The mode=ro pools go first; the reader's query_only is a rule for its statements,
// not the file's mode, so it may close last.
func (f *file) closePools() error {
	return sqlitepool.CloseAll(f.closeBudget, f.queryDB, f.ownQueryDB, f.db, f.readDB)
}

// startJanitor spawns this file's sweeper, or nothing when no interval is set.
func (f *file) startJanitor(ret Retention) {
	if ret.Interval <= 0 {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	f.stopJanitor = func() {
		cancel()
		<-done
	}
	go func() {
		defer close(done)
		runJanitor(ctx, f, ret)
	}()
}

// newFile wraps the open pools and their prepared set. Nothing else builds one —
// openFile is the only caller.
func newFile(
	path string, cacheID int64, db, readDB, queryDB, ownQueryDB *sqlitepool.Pool,
	set *sqlstmt.Set[stmtID], sizeLimitSender *conflate.Sender[int64, struct{}],
	closeBudget time.Duration,
) *file {
	queryCtx, cancelQueries := context.WithCancel(context.Background())
	return &file{
		queryCtx:        queryCtx,
		cancelQueries:   cancelQueries,
		path:            path,
		cacheID:         cacheID,
		log:             slog.Default(),
		db:              db,
		readDB:          readDB,
		queryDB:         queryDB,
		ownQueryDB:      ownQueryDB,
		own:             newOwnStmts(),
		set:             set,
		hub:             conflate.New[string, struct{}](),
		janitorWakeups:  make(chan struct{}, 1),
		sizeLimitSender: sizeLimitSender,
		vacuumPages:     vacuumPagesPerSweep,
		closeBudget:     closeBudget,
		now:             func() int64 { return time.Now().UnixMilli() },
	}
}

// Release gives the claim back; the last release on an entry closes its file. It counts
// down the entry this store claimed, never whatever the id maps to now — a retired
// entry's stragglers must not close a fresh claim's file.
//
// The close is under the manager's lock, as a clear's is: Windows refuses to unlink an
// open file, so a Clear or Remove must not find the entry gone while its file is still
// closing.
func (s *Store) Release() {
	s.m.mu.Lock()
	defer s.m.mu.Unlock()
	s.e.refs--
	if s.e.refs > 0 {
		return
	}
	f := s.e.file
	s.e.file = nil
	if s.m.entries[s.cacheID] == s.e {
		delete(s.m.entries, s.cacheID)
	}
	if f != nil {
		if err := s.m.closeFile(f); err != nil {
			s.m.closedLocked(s.cacheID, f, err)
		}
	}
}

// file resolves the open file behind this store, or ErrClosed once it is gone — a
// Remove, or a Clear that could not reopen. A method that reaches the database goes
// through use instead, which adds the count a close waits on.
func (s *Store) file() (*file, error) {
	s.m.mu.Lock()
	defer s.m.mu.Unlock()
	return s.fileLocked()
}

// use resolves the file and counts an operation on it in one critical section, so the
// operation either registers before a close and is waited for, or answers ErrClosed.
// done ends it, once its connection is released.
func (s *Store) use() (f *file, done func(), err error) {
	s.m.mu.Lock()
	defer s.m.mu.Unlock()
	if f, err = s.fileLocked(); err != nil {
		return nil, nil, err
	}
	f.inUse.Add(1)
	return f, f.inUse.Done, nil
}

// fileLocked is file, for a caller holding the manager's lock.
func (s *Store) fileLocked() (*file, error) {
	if s.e.file == nil {
		return nil, fmt.Errorf("cache %d: %w", s.cacheID, ErrClosed)
	}
	// A bound store answers for its own file only: a Clear's swap ends it the way a Remove
	// does, rather than silently redirecting the read to the fresh empty one.
	if s.bound != nil && s.e.file != s.bound {
		return nil, fmt.Errorf("cache %d: %w", s.cacheID, ErrClosed)
	}
	return s.e.file, nil
}

// ListenForChanges returns the change notifications for this cache, narrowed to keys — or every
// key when none are given, which is what a reader spanning both buses needs. It ends when the file
// closes, which is what tells a live watch that the cache was cleared or shut down.
//
// The filter runs at ENQUEUE, so a pods watch does not even hold a slot for an events
// write. Filtering in the reader's own loop would wake every open watch on every write in
// the cache, which is what the per-kind bus keys exist to prevent.
func (s *Store) ListenForChanges(keys ...string) (ChangeNotifications, error) {
	f, err := s.file()
	if err != nil {
		return nil, err
	}
	return f.subscribe(keys...), nil
}

// subscribe is the receiver both doors hand out: Store.ListenForChanges for a holder, and
// Manager.ListenForChanges for a caller that only borrows the feed.
func (f *file) subscribe(keys ...string) ChangeNotifications {
	if len(keys) == 0 {
		return f.hub.Receiver()
	}
	want := make(map[string]struct{}, len(keys))
	for _, k := range keys {
		want[k] = struct{}{}
	}
	return f.hub.Receiver(f.hub.WithKeyFilter(func(k string) bool {
		_, ok := want[k]
		return ok
	}))
}

// Cookie returns the watch resourceVersion recorded for one kind, and whether one is
// recorded. Keys into cluster_meta, per the schema's bookkeeping bag.
func (s *Store) Cookie(ctx context.Context, apiVersion, resource string) (string, bool, error) {
	f, done, err := s.use()
	if err != nil {
		return "", false, err
	}
	defer done()
	var v string
	err = f.set.Stmts().QueryRow(ctx, stmtSelectMeta, cookieKey(apiVersion, resource)).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("cookie: %w", err)
	}
	return v, true, nil
}

// SetCookie records the watch resourceVersion for one kind.
func (s *Store) SetCookie(ctx context.Context, apiVersion, resource, resourceVersion string) error {
	f, done, err := s.use()
	if err != nil {
		return err
	}
	defer done()
	if err := setCookie(ctx, f.set.Stmts(), apiVersion, resource, resourceVersion); err != nil {
		return fmt.Errorf("set cookie: %w", err)
	}
	// No notify: a cookie is a position, not content, so no reader waits on it.
	f.wakeJanitor()
	return nil
}

// ApplyChange lands one watch delta: Added/Modified upsert the row, Deleted removes it.
// The row and the position that would replay it go in one transaction, so no restart
// resumes from a position the rows do not back.
func (s *Store) ApplyChange(ctx context.Context, k Kind, t watch.EventType, u *unstructured.Unstructured) error {
	switch t {
	case watch.Added, watch.Modified, watch.Deleted:
	default:
		return nil
	}
	if u == nil || u.Object == nil {
		return fmt.Errorf("apply %s %s: empty object", k.Kind, t)
	}

	f, done, err := s.use()
	if err != nil {
		return err
	}
	defer done()
	err = f.set.InTx(ctx, func(st stmts) error {
		stamp, err := f.writeStamp(ctx, st)
		if err != nil {
			return err
		}

		if t == watch.Deleted {
			// An unkeyable delete errors rather than no-opping: booking progress for a delta
			// whose row never went would resume the next watch past it.
			uid := string(u.GetUID())
			if uid == "" {
				return errors.New("delete: empty UID")
			}
			if k.isCoreEvents() {
				if err = logDeletes(ctx, st, stmtLogDeleteEvent, stamp, uid); err == nil {
					_, err = st.Exec(ctx, stmtDeleteEvent, uid)
				}
			} else {
				err = deleteObjectRow(ctx, st, uid, stamp)
			}
		} else if k.isCoreEvents() {
			err = f.writeEvent(ctx, st, u, stamp)
		} else {
			err = f.writeObject(ctx, st, k, u, stamp)
		}
		// A body the projection rejects is skipped, exactly as a relist page skips it. The
		// cookie still advances over it: the server replays from that position, so a run that
		// failed here would be handed the same body every time it resumed.
		if err != nil && !errors.Is(err, errUnprojectable) {
			return fmt.Errorf("%s: %w", t, err)
		}

		if rv := u.GetResourceVersion(); rv != "" {
			if err := setCookie(ctx, st, k.APIVersion, k.Resource, rv); err != nil {
				return fmt.Errorf("advance cookie: %w", err)
			}
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("apply %s: %w", k.Kind, err)
	}
	f.notify(busKey(k))
	return nil
}

// writeObject projects and upserts one object body.
func (f *file) writeObject(ctx context.Context, st stmts, k Kind, u *unstructured.Unstructured, stamp writeStamp) error {
	row, err := projectObject(u)
	if err != nil {
		return err
	}
	return insertObjectRow(ctx, st, k, row, stamp)
}

// writeEvent projects and upserts one event body.
func (f *file) writeEvent(ctx context.Context, st stmts, u *unstructured.Unstructured, stamp writeStamp) error {
	row, err := extractEvent(u)
	if err != nil {
		return err
	}
	return insertEventRow(ctx, st, row, stamp)
}

// CountKind returns one kind's cached rows, off the trigger-maintained kind_counts
// rather than a scan of the shared objects table. A kind nothing has written reads 0.
func (s *Store) CountKind(ctx context.Context, k Kind) (int, error) {
	f, done, err := s.use()
	if err != nil {
		return 0, err
	}
	defer done()
	// Every cached event rolls into the schema's hardcoded ('v1','Event') tally,
	// maintained by the events triggers.
	var n int
	err = f.set.Stmts().QueryRow(ctx, stmtCountKind, k.APIVersion, k.Kind).Scan(&n)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("count kind %s: %w", k.Kind, err)
	}
	return n, nil
}

// Counts is the whole-cache tally: total cached objects, and how many kinds hold any.
func (s *Store) Counts(ctx context.Context) (Counts, error) {
	f, done, err := s.use()
	if err != nil {
		return Counts{}, err
	}
	defer done()
	return countKinds(ctx, f.db.DB)
}

// countKinds reads the tally off the trigger-maintained per-kind counts — O(kinds),
// never a scan of objects. A kind emptied by deletes keeps a zero row (an advertised
// but empty kind must read 0 rather than vanish), so the kind count is of kinds with
// rows. Shared with the manager's read-only path, which has a database but no Store.
func countKinds(ctx context.Context, db *sql.DB) (Counts, error) {
	var out Counts
	err := db.QueryRowContext(ctx,
		`SELECT COALESCE(SUM(count), 0), COUNT(*) FROM kind_counts
		 WHERE count > 0 AND NOT (api_version = ? AND kind = ?)`,
		coreEventsAPIVersion, coreEventsKind).Scan(&out.ObjectCount, &out.KindCount)
	if err != nil {
		return Counts{}, fmt.Errorf("counts: %w", err)
	}
	return out, nil
}

// ClearKind deletes one kind's rows, everything hanging off them, and its cookie, in one
// transaction — for a kind that has stopped being synced. The catalog row is not one of
// them: that table says what the cluster serves, and its one writer is the sweep.
//
// It takes the whole Kind because the rows are keyed by the singular while a watch is
// opened on the plural, and **the caller is what knows both**: the record carries the
// Kind, and resolving it here through kind_catalog would tie a teardown to a table the
// sweep owns, leaving every row behind for a kind no sweep has reached yet.
func (s *Store) ClearKind(ctx context.Context, k Kind) error {
	f, done, err := s.use()
	if err != nil {
		return err
	}
	defer done()
	err = f.set.InTx(ctx, func(st stmts) error {
		stamp, err := f.writeStamp(ctx, st)
		if err != nil {
			return err
		}

		// Core events are not in objects: they have their own table, which this collection
		// owns outright.
		if k.isCoreEvents() {
			if err := logDeletes(ctx, st, stmtLogDeleteAllEvents, stamp); err != nil {
				return fmt.Errorf("log events: %w", err)
			}
			if _, err := st.Exec(ctx, stmtDeleteAllEvents); err != nil {
				return fmt.Errorf("delete events: %w", err)
			}
		} else {
			// owner_refs by child_uid only: an edge is extracted from the CHILD's
			// ownerReferences, so a retained child's edge into a cleared owner is still what
			// that child says, and only rewriting the child could put it back. Traversals
			// join against objects, where a missing owner reads the same as one whose kind
			// is not mirrored at all.
			if err := logDeletes(ctx, st, stmtLogClearObjectsOfKind, stamp, k.APIVersion, k.Kind); err != nil {
				return fmt.Errorf("log rows: %w", err)
			}
			for _, id := range []stmtID{
				stmtClearOwnerRefsOfKind, stmtClearLabelsOfKind, stmtClearContainersOfKind,
				stmtClearRefsOfKind, stmtClearSelectorsOfKind, stmtClearSelectorTermsOfKind,
				stmtClearStatusHistoryOfKind, stmtClearObjectsOfKind,
			} {
				if _, err := st.Exec(ctx, id, k.APIVersion, k.Kind); err != nil {
					return fmt.Errorf("delete rows: %w", err)
				}
			}
		}

		// The sweep above leaves the tally at 0, which is what an advertised but empty kind
		// must read. A forgotten kind is different: nothing will name it again.
		if _, err := st.Exec(ctx, stmtDeleteKindCount, k.APIVersion, k.Kind); err != nil {
			return fmt.Errorf("delete counts: %w", err)
		}
		// The catalog row stays: it says the CLUSTER serves this kind, which clearing the
		// cache does not change. SyncKinds' prune is what takes one out.
		if _, err := st.Exec(ctx, stmtDeleteMeta, cookieKey(k.APIVersion, k.Resource)); err != nil {
			return fmt.Errorf("delete cookie: %w", err)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("clear kind: %w", err)
	}
	f.notify(busKey(k))
	return nil
}

// BeginReplace opens a streaming full-LIST reconcile of one kind's rows; Commit prunes
// what the LIST did not carry.
//
// Clearing the resume cookie is DEFERRED to the first written page, so a pass failing
// before any write keeps the intact snapshot's cookie, while one failing after writing
// leaves none and the next start cold-lists and prunes the leftovers.
func (s *Store) BeginReplace(k Kind) (*ReplaceSession, error) {
	f, err := s.file()
	if err != nil {
		return nil, err
	}
	// The session holds the file rather than re-resolving per page: a clear stops the
	// cache's workers before it swaps, so no session is live across one.
	return &ReplaceSession{s: s, f: f, kind: k, mark: f.stamp()}, nil
}

// ReplaceSession streams a paginated relist into the shared tables, reconciling one
// kind's rows only.
//
// It reconciles by MARK AND SWEEP: every page stamps updated_at and Commit deletes this
// kind's rows still older. That keeps the pass O(one page) in memory — a keep-set of
// every uid would defeat pagination — and prunes in a few statements rather than a
// read-back plus three per stale row. (The objects table's `generation` column is the
// object's own metadata.generation, not a sweep counter, and must not be pruned on.)
//
// Per-page commits trade whole-pass atomicity for that memory bound: a pass failing
// mid-pagination leaves committed pages visible until the next one prunes them.
type ReplaceSession struct {
	s    *Store
	f    *file
	kind Kind
	// mark is the sweep boundary: every stamp taken before this session is strictly
	// below it, and every page this session writes strictly above.
	mark int64
	// cookieCleared makes the first page's cookie clear happen once per session.
	cookieCleared bool
}

// WritePage lands one page in its own transaction, clearing the resume cookie alongside
// the first page. A body that will not project is skipped, not fatal: one malformed
// object must not stop a collection from syncing.
//
// The first page clears the cookie **even when it carries nothing**: a cookie means a
// completed LIST landed on disk, so a relist that has begun must leave none standing —
// a pass that then fails would otherwise let the next start resume from it and skip the
// reconcile its rows still need. Later empty pages have nothing to do.
func (r *ReplaceSession) WritePage(ctx context.Context, items []*unstructured.Unstructured) error {
	if len(items) == 0 && r.cookieCleared {
		return nil
	}
	done, err := r.use()
	if err != nil {
		return err
	}
	defer done()
	err = r.f.set.InTx(ctx, func(st stmts) error {
		if !r.cookieCleared {
			if err := deleteCookie(ctx, st, r.kind.APIVersion, r.kind.Resource); err != nil {
				return fmt.Errorf("clear cookie: %w", err)
			}
		}
		stamp, err := r.f.writeStamp(ctx, st)
		if err != nil {
			return err
		}
		for _, u := range items {
			var err error
			if r.kind.isCoreEvents() {
				err = r.writeEvent(ctx, st, u, stamp)
			} else {
				err = r.writeObject(ctx, st, u, stamp)
			}
			if err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("write page: %w", err)
	}
	r.cookieCleared = true
	if len(items) == 0 {
		// Nothing landed, so nothing to announce.
		return nil
	}
	// Per committed page, not only at Commit: a relist that commits pages and then fails
	// would otherwise leave durable rows unannounced, and unmeasured, until some later
	// write — and a large relist grows the file page by page long before it commits.
	r.f.notify(busKey(r.kind))
	return nil
}

// use counts one call on the session's file, as Store.use does, or answers ErrClosed
// once a clear or close has taken that file away.
func (r *ReplaceSession) use() (func(), error) {
	r.s.m.mu.Lock()
	defer r.s.m.mu.Unlock()
	if r.s.e.file != r.f {
		return nil, fmt.Errorf("cache %d: %w", r.s.cacheID, ErrClosed)
	}
	r.f.inUse.Add(1)
	return r.f.inUse.Done, nil
}

// writeObject lands one page item, skipping a body that will not project.
func (r *ReplaceSession) writeObject(ctx context.Context, st stmts, u *unstructured.Unstructured, stamp writeStamp) error {
	row, err := projectObject(u)
	if errors.Is(err, errUnprojectable) {
		return nil
	} else if err != nil { // coverage:ignore projectObject wraps every error it returns in errUnprojectable
		return err
	}
	return insertObjectRow(ctx, st, r.kind, row, stamp)
}

// writeEvent is writeObject for the events table.
func (r *ReplaceSession) writeEvent(ctx context.Context, st stmts, u *unstructured.Unstructured, stamp writeStamp) error {
	row, err := extractEvent(u)
	if errors.Is(err, errUnprojectable) {
		return nil
	} else if err != nil { // coverage:ignore extractEvent wraps every error it returns in errUnprojectable
		return err
	}
	return insertEventRow(ctx, st, row, stamp)
}

// Commit sweeps the rows no page rewrote, then persists the cookie in the same
// transaction — a failed persist must not leave the cookie durably advanced, which
// would resume the next watch past the objects before it.
func (r *ReplaceSession) Commit(ctx context.Context, resourceVersion string) (int, error) {
	done, err := r.use()
	if err != nil {
		return 0, err
	}
	defer done()
	var pruned int
	err = r.f.set.InTx(ctx, func(st stmts) error {
		stamp, err := r.f.writeStamp(ctx, st)
		if err != nil {
			return err
		}

		if r.kind.isCoreEvents() {
			// The events table is this collection's outright, so the prune is unscoped.
			if err := logDeletes(ctx, st, stmtLogPruneEvents, stamp, r.mark); err != nil {
				return fmt.Errorf("log pruned events: %w", err)
			}
			res, err := st.Exec(ctx, stmtPruneEvents, r.mark)
			if err != nil {
				return fmt.Errorf("prune events: %w", err)
			}
			n, err := res.RowsAffected()
			if err != nil { // coverage:ignore SQLite's driver always reports the rows affected
				return fmt.Errorf("prune events: %w", err)
			}
			pruned = int(n)
		} else {
			if pruned, err = sweepObjects(ctx, st, r.kind, r.mark, stamp); err != nil {
				return fmt.Errorf("prune: %w", err)
			}
		}

		if resourceVersion != "" {
			if err := setCookie(ctx, st, r.kind.APIVersion, r.kind.Resource, resourceVersion); err != nil {
				return fmt.Errorf("persist cookie: %w", err)
			}
		}
		return nil
	})
	if err != nil {
		return 0, fmt.Errorf("commit relist: %w", err)
	}
	r.f.notify(busKey(r.kind))
	return pruned, nil
}

// busKey is the ping key one kind's writes fan out on.
func busKey(k Kind) string {
	if k.isCoreEvents() {
		return EventsKey
	}
	return ObjectsKey(k.APIVersion, k.Resource)
}

// setMeta writes one bookkeeping value, so a caller can put it in the transaction whose
// rows it describes.
func setMeta(ctx context.Context, st stmts, key, value string) error {
	_, err := st.Exec(ctx, stmtUpsertMeta, key, value)
	return err
}

// getMeta reads one bookkeeping value, and whether it is recorded at all.
func getMeta(ctx context.Context, st stmts, key string) (string, bool, error) {
	var v string
	err := st.QueryRow(ctx, stmtSelectMeta, key).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return v, true, nil
}

// setCookie writes one kind's resume position, so a delta and a relist commit share the
// statement.
func setCookie(ctx context.Context, st stmts, apiVersion, resource, resourceVersion string) error {
	return setMeta(ctx, st, cookieKey(apiVersion, resource), resourceVersion)
}

// ListedKinds is every catalog kind with a cookie: the kinds whose list has committed,
// in this session or an earlier one.
func (s *Store) ListedKinds(ctx context.Context) ([]Kind, error) {
	f, done, err := s.use()
	if err != nil {
		return nil, err
	}
	defer done()
	var listed []Kind
	err = inReadTx(ctx, f, "read listed kinds", func(st stmts) error {
		rows, err := st.Query(ctx, stmtSelectListedKinds)
		if err != nil { // coverage:ignore a cancel fails the transaction before its first statement
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var k Kind
			if err := rows.Scan(&k.APIVersion, &k.Kind, &k.Resource); err != nil { // coverage:ignore three text columns scan into three strings
				return err
			}
			listed = append(listed, k)
		}
		return rows.Err()
	})
	return listed, err
}

// deleteCookie durably removes one kind's resume position, so the next start cold-lists.
func deleteCookie(ctx context.Context, st stmts, apiVersion, resource string) error {
	_, err := st.Exec(ctx, stmtDeleteMeta, cookieKey(apiVersion, resource))
	return err
}

// cookieKey is the cluster_meta key one kind's watch resourceVersion is stored under.
// Never parsed back — apiVersion and resource are read from the caller's own arguments,
// not recovered from the key. The refs view (query_views.sql) and stmtSelectListedKinds
// build the same key to ask whether a kind is listed, so the three change together.
func cookieKey(apiVersion, resource string) string {
	return "cookie/" + apiVersion + "/" + resource
}
