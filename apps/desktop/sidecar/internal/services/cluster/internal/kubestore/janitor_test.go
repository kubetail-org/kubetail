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
	"log/slog"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/watch"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/testutil"
)

// addStatusRow appends one status transition at a given age, standing in for the write an
// object's status change makes.
func addStatusRow(t *testing.T, s *Store, uid string, age time.Duration) {
	t.Helper()
	_, err := db(t, s).ExecContext(context.Background(),
		`INSERT INTO status_history(uid, at, summary) VALUES (?, ?, 'Running')`,
		uid, time.Now().Add(-age).UnixMilli())
	require.NoError(t, err)
}

// status_history is the one table the store owns outright rather than mirroring from the
// server, so nothing upstream ages it out and the janitor is the only bound it has.
func TestSweepTrimsStatusHistoryPastItsTTL(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	addStatusRow(t, store, "uid-old", 48*time.Hour)
	addStatusRow(t, store, "uid-fresh", time.Minute)

	sweep(context.Background(), openFileOf(t, store), Retention{StatusHistoryTTL: time.Hour})

	assert.Equal(t, 0, countRows(t, store, `SELECT COUNT(*) FROM status_history WHERE uid = 'uid-old'`))
	assert.Equal(t, 1, countRows(t, store, `SELECT COUNT(*) FROM status_history WHERE uid = 'uid-fresh'`))
}

// freelist is how many pages the file holds free — the number the vacuum hands back, and
// the only honest gate on whether there is anything to do.
func freelist(t *testing.T, s *Store) int64 {
	t.Helper()
	var pages int64
	require.NoError(t, db(t, s).QueryRowContext(context.Background(), `PRAGMA freelist_count`).Scan(&pages))
	return pages
}

// fillStatusHistory writes enough expired rows to span many pages, so freeing them leaves a
// freelist worth measuring.
func fillStatusHistory(t *testing.T, s *Store, rows int) {
	t.Helper()
	tx, err := db(t, s).BeginTx(context.Background(), nil)
	require.NoError(t, err)
	at := time.Now().Add(-48 * time.Hour).UnixMilli()
	for i := range rows {
		_, err := tx.ExecContext(context.Background(),
			`INSERT INTO status_history(uid, at, summary) VALUES (?, ?, ?)`,
			"uid", at, strings.Repeat("x", 200)+strconv.Itoa(i))
		require.NoError(t, err)
	}
	require.NoError(t, tx.Commit())
}

// auto_vacuum=INCREMENTAL on its own reclaims nothing: freed pages join the freelist and the
// file sits at its high-water mark until something runs the vacuum. Nothing else does, so the
// size state watch would report the worst the cache has ever been rather than what it holds.
func TestSweepHandsFreePagesBack(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	fillStatusHistory(t, store, 5000)
	require.Zero(t, freelist(t, store), "a file that has freed nothing yet")

	sweep(context.Background(), openFileOf(t, store), Retention{StatusHistoryTTL: time.Hour})

	assert.Zero(t, freelist(t, store), "the pages the delete freed went back to the OS")
}

// A cache has one writer, so an unbounded vacuum blocks every kind's sync — and the freelist
// is biggest exactly when that hurts most, right after a relist. A backlog drains over the
// following sweeps instead.
func TestSweepBoundsWhatOneVacuumHandsBack(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	fillStatusHistory(t, store, 5000)
	f := openFileOf(t, store)
	f.vacuumPages = 8 // so no assertion here encodes the production page count

	sweep(context.Background(), f, Retention{StatusHistoryTTL: time.Hour})
	before := freelist(t, store)
	require.NotZero(t, before, "a bounded sweep leaves a backlog")

	sweep(context.Background(), f, Retention{StatusHistoryTTL: time.Hour})

	assert.Equal(t, before-8, freelist(t, store), "the backlog drains at the same bound")
}

// Nothing else runs a sweep, so a file that opens with a retention gets its own janitor —
// and it sweeps first thing inside its goroutine rather than inline, since a cache should
// not wait a full interval for its first pass and a vacuum must never run under m.mu.
func TestAJanitorSweepsTheFileItWasOpenedFor(t *testing.T) {
	t.Parallel()

	m := NewManager(t.TempDir(), Retention{StatusHistoryTTL: time.Hour, Interval: time.Millisecond})
	t.Cleanup(func() { require.NoError(t, m.Close()) })
	store, err := m.OpenOrCreate(1)
	require.NoError(t, err)
	t.Cleanup(store.Release)

	fillStatusHistory(t, store, 100)

	require.Eventually(t, func() bool { return sweptClean(store) },
		testutil.Timeout, time.Millisecond, "the janitor swept without anybody asking it")
}

// openFile has two call sites and the second is the one that gets missed: Clear reopens a
// fresh file mid-clear, for the claims still held. Starting the janitor at the open rather
// than in openFile silently leaves a cleared cache without one — the cache most likely to
// need it, since a clear is what frees the pages.
func TestAClearedCacheKeepsItsJanitor(t *testing.T) {
	t.Parallel()

	m := NewManager(t.TempDir(), Retention{StatusHistoryTTL: time.Hour, Interval: time.Millisecond})
	t.Cleanup(func() { require.NoError(t, m.Close()) })
	store, err := m.OpenOrCreate(1)
	require.NoError(t, err)
	t.Cleanup(store.Release)

	require.NoError(t, m.Clear(1))
	fillStatusHistory(t, store, 100)

	require.Eventually(t, func() bool { return sweptClean(store) },
		testutil.Timeout, time.Millisecond, "the file a clear swapped in sweeps like any other")
}

// sweptClean reports that a sweep has run over this file: the expired rows are the evidence,
// since the freelist of a file that has freed nothing is legitimately zero.
func sweptClean(s *Store) bool {
	return noRows(s, `SELECT COUNT(*) FROM status_history`)
}

// noRows is countRows for an Eventually check. Its goroutine can outlive the test, so a
// failed query answers false rather than failing t, which would panic the binary.
func noRows(s *Store, q string) bool {
	f, err := s.file()
	if err != nil {
		return false
	}
	var n int
	return f.db.QueryRowContext(context.Background(), q).Scan(&n) == nil && n == 0
}

// The janitor keeps sweeping: the first pass runs at open, and the interval is what makes
// the second one happen. Waiting for the first to finish before dirtying the file is what
// separates them — a fill racing the opening sweep proves only that one ran.
func TestAJanitorKeepsSweepingOnItsInterval(t *testing.T) {
	t.Parallel()

	m := NewManager(t.TempDir(), Retention{StatusHistoryTTL: time.Hour, Interval: time.Millisecond})
	t.Cleanup(func() { require.NoError(t, m.Close()) })
	store, err := m.OpenOrCreate(1)
	require.NoError(t, err)
	t.Cleanup(store.Release)
	fillStatusHistory(t, store, 100)
	require.Eventually(t, func() bool { return sweptClean(store) },
		testutil.Timeout, time.Millisecond, "the opening sweep")

	fillStatusHistory(t, store, 100)

	require.Eventually(t, func() bool { return sweptClean(store) },
		testutil.Timeout, time.Millisecond, "a second sweep, which only the ticker starts")
}

// A vacuum that will not run is logged and the sweep returns — the janitor's next
// interval tries again. Failing louder would take down the only thing that hands pages
// back, over a condition that is usually momentary.
func TestASweepSurvivesAVacuumItCannotRun(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	dir := t.TempDir()
	m := NewManager(dir, Retention{})
	t.Cleanup(func() { require.NoError(t, m.Close()) })
	store, err := m.OpenOrCreate(1)
	require.NoError(t, err)
	t.Cleanup(store.Release)
	// Rows written and deleted, so the freelist the vacuum walks is not empty.
	fillStatusHistory(t, store, 2000)
	_, err = db(t, store).ExecContext(ctx, `DELETE FROM status_history`)
	require.NoError(t, err)

	// A read-only pool over the same file: the freelist still reads, and the vacuum that
	// would hand its pages back cannot write.
	f := openFileOf(t, store)
	readOnly := openReadOnly(filepath.Join(dir, "1.db"))
	t.Cleanup(func() { require.NoError(t, readOnly.Close()) })
	// The handle being displaced is the only reference left to it, and Windows will not
	// unlink a file something still holds open.
	displaced := f.db
	t.Cleanup(func() { require.NoError(t, displaced.Close()) })
	f.db = readOnly

	assert.NotPanics(t, func() { sweep(ctx, f, Retention{StatusHistoryTTL: time.Hour}) })
}

// addDeleteEntry logs one delete at a given age, standing in for the entry a delete left.
func addDeleteEntry(t *testing.T, s *Store, kind, uid string, seq int64, age time.Duration) {
	t.Helper()
	_, err := db(t, s).ExecContext(context.Background(),
		`INSERT INTO deletes(seq, api_version, kind, uid, at) VALUES (?, 'v1', ?, ?, ?)`,
		seq, kind, uid, time.Now().Add(-age).UnixMilli())
	require.NoError(t, err)
}

// trimmedMark is how far a kind's log has been trimmed, as a reader reads it.
func trimmedMark(t *testing.T, s *Store, kind string) int64 {
	t.Helper()
	mark, err := trimmed(context.Background(), openFileOf(t, s).set.Stmts(), "v1", kind)
	require.NoError(t, err)
	return mark
}

// A reader's cursor goes stale by time, not by count, so the log is bounded by age. What
// it trims it must also announce: a cursor at or below the mark can no longer be trusted
// to have seen every delete above it.
func TestSweepTrimsTheDeletesLogPastItsTTL(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	addDeleteEntry(t, store, "Pod", "uid-old", 7, 48*time.Hour)
	addDeleteEntry(t, store, "Pod", "uid-older", 4, 49*time.Hour)
	addDeleteEntry(t, store, "Pod", "uid-fresh", 9, time.Minute)

	sweep(context.Background(), openFileOf(t, store), Retention{DeletesTTL: time.Hour})

	assert.Equal(t, 1, countRows(t, store, `SELECT COUNT(*) FROM deletes`), "only the fresh entry stays")
	assert.Equal(t, int64(7), trimmedMark(t, store, "Pod"), "the highest position removed")
}

// Cursors are per kind, so the marks are too: a global one would have a busy kind's
// deletes push every quiet kind's cursor into the diff within minutes.
func TestSweepMarksEachKindSeparately(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	addDeleteEntry(t, store, "Pod", "uid-1", 3, 48*time.Hour)
	addDeleteEntry(t, store, "ConfigMap", "uid-2", 8, 48*time.Hour)
	addDeleteEntry(t, store, "Secret", "uid-3", 9, time.Minute)

	sweep(context.Background(), openFileOf(t, store), Retention{DeletesTTL: time.Hour})

	assert.Equal(t, int64(3), trimmedMark(t, store, "Pod"))
	assert.Equal(t, int64(8), trimmedMark(t, store, "ConfigMap"))
	assert.Zero(t, trimmedMark(t, store, "Secret"), "a kind with nothing removed has no mark")
}

// Retention's fields are independent, so a manager given one TTL and not the other must
// leave the unset table alone. A cutoff of now trims the whole log and raises every kind's
// mark to the head of it, invalidating every reader's cursor in one sweep.
func TestAZeroTTLTrimsNothing(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	addDeleteEntry(t, store, "Pod", "uid-1", 5, 48*time.Hour)
	addStatusRow(t, store, "uid-1", 48*time.Hour)

	sweep(context.Background(), openFileOf(t, store), Retention{})

	assert.Equal(t, 1, countRows(t, store, `SELECT COUNT(*) FROM deletes`))
	assert.Zero(t, trimmedMark(t, store, "Pod"), "nothing went, so nothing is marked")
	assert.Equal(t, 1, countRows(t, store, `SELECT COUNT(*) FROM status_history`))
}

// A mark only ever goes up: a later sweep that takes nothing from a kind must not lower
// what an earlier one recorded, or a cursor below it would read as valid again.
func TestASweepThatRemovesNothingLeavesTheMarkAlone(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	addDeleteEntry(t, store, "Pod", "uid-1", 5, 48*time.Hour)
	sweep(context.Background(), openFileOf(t, store), Retention{DeletesTTL: time.Hour})

	sweep(context.Background(), openFileOf(t, store), Retention{DeletesTTL: time.Hour})

	assert.Equal(t, int64(5), trimmedMark(t, store, "Pod"))
}

// `at` and `seq` rise together only within one file lifetime: the clock stamp is forced
// upward in memory and starts again from the wall clock on reopen, while the counter is on
// disk and does not. So a burst that ran the stamps ahead of the clock leaves entries whose
// age and position disagree, and a later sweep can take a LOWER position than an earlier
// one. The mark must not follow it down — a cursor between the two marks would read as
// valid over deletes that are already gone.
func TestASweepNeverLowersAKindsMark(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	addDeleteEntry(t, store, "Pod", "uid-high", 200, 90*time.Minute)
	addDeleteEntry(t, store, "Pod", "uid-low", 100, 30*time.Minute)

	sweep(context.Background(), openFileOf(t, store), Retention{DeletesTTL: time.Hour})
	require.Equal(t, int64(200), trimmedMark(t, store, "Pod"))
	sweep(context.Background(), openFileOf(t, store), Retention{DeletesTTL: 10 * time.Minute})

	assert.Equal(t, int64(200), trimmedMark(t, store, "Pod"), "the older entry's lower position")
	assert.Zero(t, countRows(t, store, `SELECT COUNT(*) FROM deletes`), "both entries went")
}

// A failed trim is logged and the sweep carries on to the vacuum: the next sweep retries,
// and a cache whose log will not trim must still hand its free pages back.
func TestSweepSurvivesATrimItCannotMake(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	swapTable(t, store, "deletes",
		`SELECT 1 AS seq, 'v1' AS api_version, 'Pod' AS kind, 'uid-1' AS uid, 0 AS at`)

	f := openFileOf(t, store)
	logs := captureWarnings(f)
	assert.NotPanics(t, func() { sweep(context.Background(), f, Retention{DeletesTTL: time.Hour}) })
	assert.Contains(t, logs.String(), "kubestore: deletes sweep failed")
}

// A failed status_history trim is a real failure, so it is still logged.
func TestSweepWarnsOfAStatusHistoryTrimItCannotMake(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	swapTable(t, store, "status_history", `SELECT 'uid' AS uid, 0 AS at, 'Running' AS summary`)
	f := openFileOf(t, store)
	logs := captureWarnings(f)

	sweep(context.Background(), f, Retention{StatusHistoryTTL: time.Hour})

	assert.Contains(t, logs.String(), "kubestore: status_history sweep failed")
}

// captureWarnings points f's janitor log at a capture. Safe only on a file with no janitor
// running, which a zero Interval guarantees.
func captureWarnings(f *file) *testutil.LogCapture {
	logs := &testutil.LogCapture{}
	f.log = slog.New(slog.NewTextHandler(logs, nil))
	return logs
}

// Closing a cache cancels its janitor's context, often mid-sweep. That is no failure, so
// the sweep warns of nothing, trims nothing and forms no verdict.
func TestASweepOnAClosingFileLogsNothing(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	fillStatusHistory(t, store, 10)
	f := openFileOf(t, store)
	logs := captureWarnings(f)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	sweep(ctx, f, Retention{StatusHistoryTTL: time.Hour, DeletesTTL: time.Hour, SizeLimit: 1})

	assert.Empty(t, logs.String())
	assert.Equal(t, 10, countRows(t, store, `SELECT COUNT(*) FROM status_history`), "a sweep on a closing file trims nothing")
	assert.Equal(t, sizeUnknown, f.sizeVerdict.Load(), "a sweep on a closing file forms no verdict")
}

// The vacuum and the checkpoint are as quiet on a closing file.
func TestAStepTheCloseInterruptsLogsNothing(t *testing.T) {
	t.Parallel()

	f := openFileOf(t, newTestStore(t))
	logs := captureWarnings(f)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	vacuum(ctx, f, 1)
	checkpoint(ctx, f)

	assert.Empty(t, logs.String())
}

// The ceiling is the whole footprint, so a sweep's verdict is the sum of the three files
// against SizeLimit — measured after the vacuum, or the freelist would trip a limit
// nothing is filling.
func TestSweepMarksAFileOverItsLimit(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	f := openFileOf(t, store)

	sweep(context.Background(), f, Retention{SizeLimit: 1})

	assert.Equal(t, sizeOver, f.sizeVerdict.Load())
}

func TestSweepMarksAFileUnderItsLimit(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	f := openFileOf(t, store)

	sweep(context.Background(), f, Retention{SizeLimit: gib})

	assert.Equal(t, sizeUnder, f.sizeVerdict.Load())
}

// Zero is unbounded, and it forms no verdict at all rather than answering "under". That is
// what keeps the first sweep of a file always publishing while an unbounded manager never
// does — and what leaves a limit that later becomes non-zero its own first edge.
func TestSweepFormsNoVerdictWithoutALimit(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	f := openFileOf(t, store)

	sweep(context.Background(), f, Retention{})

	assert.Equal(t, sizeUnknown, f.sizeVerdict.Load())
}

// A file gone from under a sweep is a clear or a teardown, and a verdict of "under" for it
// would publish a release nothing released.
func TestSweepLeavesTheVerdictOfAFileItCannotMeasure(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	f := openFileOf(t, store)
	f.sizeVerdict.Store(sizeOver)
	f.path = filepath.Join(t.TempDir(), "gone.db")

	sweep(context.Background(), f, Retention{SizeLimit: gib})

	assert.Equal(t, sizeOver, f.sizeVerdict.Load())
}

// A sync's writes sit in the WAL until a checkpoint moves them, so a file can cross its
// ceiling on a log that is about to be reclaimed. Pausing a cache whose contents fit is the
// wrong answer, so the sweep checkpoints first and judges the size that remains.
func TestSweepCheckpointsBeforeJudgingAWALHeavyFile(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := newTestStore(t)
	f := openFileOf(t, store)
	// Nothing reclaims the log on its own, so the writes below stay in it.
	_, err := f.db.ExecContext(ctx, `PRAGMA wal_autocheckpoint = 0`)
	require.NoError(t, err)
	fillStatusHistory(t, store, 5000)
	usage, err := statDiskUsage(f.path)
	require.NoError(t, err)
	require.Greater(t, usage.wal, usage.db, "the writes are in the log, not the file")

	// A limit the pair crosses and the checkpointed file does not.
	sweep(ctx, f, Retention{SizeLimit: usage.db + usage.wal - 1})

	assert.Equal(t, sizeUnder, f.sizeVerdict.Load())
}

// The verdict is published as an edge: a reader is woken when the answer changes and left
// alone while it stands. A ping per sweep would wake a controller pass every interval per
// cache to say what it already knew.
func TestSweepPublishesOnlyWhenTheVerdictChanges(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	m := NewManager(t.TempDir(), Retention{})
	t.Cleanup(func() { require.NoError(t, m.Close()) })
	store, err := m.OpenOrCreate(1)
	require.NoError(t, err)
	t.Cleanup(store.Release)
	f := openFileOf(t, store)
	news := m.ListenForSizeLimitChanges()
	t.Cleanup(news.Close)

	sweep(ctx, f, Retention{SizeLimit: 1})
	assert.Equal(t, int64(1), testutil.Recv(t, news.Chan(), "the first verdict").Key)

	sweep(ctx, f, Retention{SizeLimit: 1})
	testutil.NoRecv(t, news.Chan(), 50*time.Millisecond, "a verdict that did not change")

	sweep(ctx, f, Retention{SizeLimit: gib})
	assert.Equal(t, int64(1), testutil.Recv(t, news.Chan(), "the release").Key)
}

// Unbounded publishes nothing at all. The memo stays unknown, so a limit that later becomes
// non-zero still gets an edge off its own first sweep.
func TestSweepPublishesNothingWithoutALimit(t *testing.T) {
	t.Parallel()

	m := NewManager(t.TempDir(), Retention{})
	t.Cleanup(func() { require.NoError(t, m.Close()) })
	store, err := m.OpenOrCreate(1)
	require.NoError(t, err)
	t.Cleanup(store.Release)
	news := m.ListenForSizeLimitChanges()
	t.Cleanup(news.Close)

	sweep(context.Background(), openFileOf(t, store), Retention{})

	testutil.NoRecv(t, news.Chan(), 50*time.Millisecond, "an unbounded manager")
}

// A clear swaps in a fresh file, and its verdict memo starts unknown rather than under —
// so the first sweep of the new file reports the release. A two-state memo would read the
// swap as "under, unchanged" and the cache would stay paused with nothing left to pause it.
func TestAClearedFilePublishesItsFirstVerdict(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	m := NewManager(t.TempDir(), Retention{})
	t.Cleanup(func() { require.NoError(t, m.Close()) })
	store, err := m.OpenOrCreate(1)
	require.NoError(t, err)
	t.Cleanup(store.Release)
	sweep(ctx, openFileOf(t, store), Retention{SizeLimit: 1})
	news := m.ListenForSizeLimitChanges()
	t.Cleanup(news.Close)

	require.NoError(t, m.Clear(1))
	sweep(ctx, openFileOf(t, store), Retention{SizeLimit: gib})

	assert.Equal(t, int64(1), testutil.Recv(t, news.Chan(), "the fresh file's first verdict").Key)
}

// The sweep interval is five minutes in production, which is a lot of cluster on a cache
// filling fast — and a long wait for the release after a clear shrinks one. So every write
// path that commits wakes the janitor, whether it grew the file or shrank it. The manager
// runs no janitor, so the wake waits on the channel for the test to find, and no sweep's
// speed enters the test.
func TestEveryCommitWakesTheJanitor(t *testing.T) {
	t.Parallel()

	pods := Kind{APIVersion: "v1", Kind: "Pod", Resource: "pods"}
	writes := map[string]func(t *testing.T, ctx context.Context, store *Store){
		"apply change": func(t *testing.T, ctx context.Context, store *Store) {
			require.NoError(t, store.ApplyChange(ctx, pods, watch.Added, pod("uid-1", "api-0", "1")))
		},
		"relist page": func(t *testing.T, ctx context.Context, store *Store) {
			session, err := store.BeginReplace(pods)
			require.NoError(t, err)
			require.NoError(t, session.WritePage(ctx, []*unstructured.Unstructured{pod("uid-1", "api-0", "1")}))
		},
		"relist commit": func(t *testing.T, ctx context.Context, store *Store) {
			session, err := store.BeginReplace(pods)
			require.NoError(t, err)
			_, err = session.Commit(ctx, "1")
			require.NoError(t, err)
		},
		"sync kinds": func(t *testing.T, ctx context.Context, store *Store) {
			require.NoError(t, store.SyncKinds(ctx, []KindRow{{APIVersion: "v1", Kind: "Pod", Resource: "pods"}}, true, 1))
		},
		"clear kind": func(t *testing.T, ctx context.Context, store *Store) {
			require.NoError(t, store.ClearKind(ctx, pods))
		},
		"set cookie": func(t *testing.T, ctx context.Context, store *Store) {
			require.NoError(t, store.SetCookie(ctx, "v1", "pods", "1"))
		},
	}
	for name, write := range writes {
		t.Run(name, func(t *testing.T) {
			m := NewManager(t.TempDir(), Retention{})
			t.Cleanup(func() { require.NoError(t, m.Close()) })
			store, err := m.OpenOrCreate(1)
			require.NoError(t, err)
			t.Cleanup(store.Release)
			f := openFileOf(t, store)
			require.Empty(t, f.janitorWakeups, "a wake owed before the write")

			write(t, context.Background(), store)

			testutil.Recv(t, f.janitorWakeups, "the write's wake")
		})
	}
}

// A wake is what makes the janitor sweep between ticks. The interval is an hour, so nothing
// but the wake can explain the second sweep, and the expired status row is what says it ran.
func TestAWakeStartsASweep(t *testing.T) {
	t.Parallel()

	m := NewManager(t.TempDir(), Retention{StatusHistoryTTL: time.Hour, Interval: time.Hour, SizeLimit: gib})
	t.Cleanup(func() { require.NoError(t, m.Close()) })
	news := m.ListenForSizeLimitChanges()
	t.Cleanup(news.Close)
	store, err := m.OpenOrCreate(1)
	require.NoError(t, err)
	t.Cleanup(store.Release)
	// The opening sweep's verdict, which is what says that sweep is over: the row added
	// after it is the woken sweep's to trim and nobody else's.
	testutil.Recv(t, news.Chan(), "the opening sweep")
	addStatusRow(t, store, "uid-old", 48*time.Hour)

	openFileOf(t, store).wakeJanitor()

	require.Eventually(t, func() bool {
		return noRows(store, `SELECT COUNT(*) FROM status_history WHERE uid = 'uid-old'`)
	}, testutil.Timeout, time.Millisecond, "the wake started a sweep before the ticker could")
}
