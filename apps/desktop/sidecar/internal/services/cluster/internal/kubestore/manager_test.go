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
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/watch"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/sqlitemigrate"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/sqlitemigrate/sqlitemigratetest"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/sqlitepool"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/testutil"
)

func TestOpenCreatesFreshStoreWithNoCookie(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	m := NewManager(dir, Retention{})
	t.Cleanup(func() { require.NoError(t, m.Close()) })

	store, err := m.OpenOrCreate(1)
	require.NoError(t, err)
	defer store.Release()

	require.FileExists(t, filepath.Join(dir, "1.db"))

	_, ok, err := store.Cookie(context.Background(), "v1", "pods")
	require.NoError(t, err)
	require.False(t, ok)
}

func TestTwoOpensShareOneStore(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	m := NewManager(dir, Retention{})
	t.Cleanup(func() { require.NoError(t, m.Close()) })

	ha, err := m.OpenOrCreate(1)
	require.NoError(t, err)
	defer ha.Release()

	hb, err := m.OpenOrCreate(1)
	require.NoError(t, err)
	defer hb.Release()

	ctx := context.Background()
	require.NoError(t, ha.SetCookie(ctx, "v1", "pods", "42"))

	v, ok, err := hb.Cookie(ctx, "v1", "pods")
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, "42", v)
}

func TestCookiePersistsAcrossReleaseAndReacquire(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	m := NewManager(dir, Retention{})
	t.Cleanup(func() { require.NoError(t, m.Close()) })

	ctx := context.Background()

	store, err := m.OpenOrCreate(1)
	require.NoError(t, err)
	require.NoError(t, store.SetCookie(ctx, "v1", "pods", "7"))
	store.Release()

	h2, err := m.OpenOrCreate(1)
	require.NoError(t, err)
	defer h2.Release()

	v, ok, err := h2.Cookie(ctx, "v1", "pods")
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, "7", v)
}

func TestClearWithNoHandlesDeletesFiles(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	m := NewManager(dir, Retention{})
	t.Cleanup(func() { require.NoError(t, m.Close()) })

	store, err := m.OpenOrCreate(1)
	require.NoError(t, err)
	require.NoError(t, store.SetCookie(context.Background(), "v1", "pods", "1"))
	store.Release()

	require.NoError(t, m.Clear(1))

	stats, err := m.Stats(context.Background(), 1)
	require.NoError(t, err)
	require.False(t, stats.Exists)
}

func TestClearOnNeverExistingCacheIsNotAnError(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	m := NewManager(dir, Retention{})
	t.Cleanup(func() { require.NoError(t, m.Close()) })

	require.NoError(t, m.Clear(999))
}

func TestClearUnderLiveHandleReopensAndKeepsHandleWorking(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	m := NewManager(dir, Retention{})
	t.Cleanup(func() { require.NoError(t, m.Close()) })

	ctx := context.Background()

	store, err := m.OpenOrCreate(1)
	require.NoError(t, err)
	defer store.Release()

	require.NoError(t, store.SetCookie(ctx, "v1", "pods", "1"))

	require.NoError(t, m.Clear(1))

	_, ok, err := store.Cookie(ctx, "v1", "pods")
	require.NoError(t, err)
	require.False(t, ok, "cookie must be gone after Clear")

	// The same handle keeps working against the fresh store.
	require.NoError(t, store.SetCookie(ctx, "v1", "pods", "99"))
	v, ok, err := store.Cookie(ctx, "v1", "pods")
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, "99", v)
}

func TestOpenExistingClaimsACacheThatHasAFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	m := NewManager(dir, Retention{})
	t.Cleanup(func() { require.NoError(t, m.Close()) })

	ctx := context.Background()
	store, err := m.OpenOrCreate(1)
	require.NoError(t, err)
	require.NoError(t, store.SetCookie(ctx, "v1", "pods", "1"))
	store.Release()

	reopened, ok, err := m.OpenExisting(1)
	require.NoError(t, err)
	require.True(t, ok)
	require.NoError(t, reopened.ClearKind(ctx, podKind))
	reopened.Release()

	h2, err := m.OpenOrCreate(1)
	require.NoError(t, err)
	defer h2.Release()

	_, ok, err = h2.Cookie(ctx, "v1", "pods")
	require.NoError(t, err)
	require.False(t, ok)
}

// The three files are measured apart so a reader can tell a cache that holds a lot from one whose
// WAL is not being checkpointed. Bytes remains the sum, since that is what the cache costs on disk.
func TestStatsReportsEachFileSeparately(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	m := NewManager(dir, Retention{})
	t.Cleanup(func() { require.NoError(t, m.Close()) })

	store, err := m.OpenOrCreate(1)
	require.NoError(t, err)
	require.NoError(t, store.SetCookie(context.Background(), "v1", "pods", "1"))

	stats, err := m.Stats(context.Background(), 1)
	require.NoError(t, err)
	main, statErr := os.Stat(filepath.Join(dir, "1.db"))
	require.NoError(t, statErr)
	store.Release()

	require.True(t, stats.Exists)
	assert.Equal(t, main.Size(), stats.DBBytes)
	// Measured while the store is open, so the shared-memory index is there — which is what a
	// bare stat of the main file misses.
	assert.Positive(t, stats.SHMBytes)
	assert.Equal(t, stats.DBBytes+stats.WALBytes+stats.SHMBytes, stats.Bytes())
}

func TestManagerCloseClosesEveryOpenStoreWithoutPanicking(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	m := NewManager(dir, Retention{})

	h1, err := m.OpenOrCreate(1)
	require.NoError(t, err)
	h2, err := m.OpenOrCreate(2)
	require.NoError(t, err)
	_ = h1
	_ = h2

	require.NoError(t, m.Close())
}

// Remove deletes the files and, unlike Clear, leaves nothing open behind it: no
// empty store is reopened for a cache that is going away.
func TestRemoveDeletesTheFilesAndDropsTheOpenStore(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	m := NewManager(dir, Retention{})
	t.Cleanup(func() { require.NoError(t, m.Close()) })

	store, err := m.OpenOrCreate(1)
	require.NoError(t, err)
	require.NoError(t, store.SetCookie(context.Background(), "v1", "pods", "1"))

	require.NoError(t, m.Remove(1))

	stats, err := m.Stats(context.Background(), 1)
	require.NoError(t, err)
	assert.False(t, stats.Exists, "the cache's files survived Remove")
	assert.ErrorIs(t, store.SetCookie(context.Background(), "v1", "pods", "2"), ErrClosed,
		"a claim still out reached a file that is gone")

	// Releasing a claim whose file is gone is not an error.
	store.Release()
}

func TestRemoveOnNeverExistingCacheIsNotAnError(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	m := NewManager(dir, Retention{})
	t.Cleanup(func() { require.NoError(t, m.Close()) })

	require.NoError(t, m.Remove(999))
}

// A deleted cache stays deleted: a straggler holding a view of it from before the
// teardown must not open a fresh file nothing will ever name again.
func TestOpenAfterRemoveIsRefused(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	m := NewManager(dir, Retention{})
	t.Cleanup(func() { require.NoError(t, m.Close()) })

	store, err := m.OpenOrCreate(1)
	require.NoError(t, err)
	store.Release()

	require.NoError(t, m.Remove(1))

	_, err = m.OpenOrCreate(1)
	assert.ErrorIs(t, err, ErrRemoved)
	// The file went with the cache, so there is nothing to claim — and finding that out
	// must not put one back.
	_, ok, err := m.OpenExisting(1)
	require.NoError(t, err)
	assert.False(t, ok)

	stats, err := m.Stats(context.Background(), 1)
	require.NoError(t, err)
	assert.False(t, stats.Exists, "the refused open recreated the file")

	// Another cache is unaffected.
	h2, err := m.OpenOrCreate(2)
	require.NoError(t, err)
	h2.Release()
}

// A cache file a newer build migrated is refused with the runner's typed error, which
// kubesync reports as the cache's StoreFailed message.
func TestOpenRefusesANewerFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	first := NewManager(dir, Retention{})
	store, err := first.OpenOrCreate(1)
	require.NoError(t, err)
	store.Release()
	require.NoError(t, first.Close())
	sqlitemigratetest.StampNewer(t, filepath.Join(dir, "1.db"))

	m := NewManager(dir, Retention{})
	t.Cleanup(func() { require.NoError(t, m.Close()) })
	_, err = m.OpenOrCreate(1)

	assert.ErrorAs(t, err, new(*sqlitemigrate.NewerSchemaError))
}

// A Delete whose cleanup failed still retires the id: the caller retries, and an
// Acquire in the meantime would recreate the file the retry is there to remove.
func TestRemoveRetiresTheCacheEvenWhenCleanupFails(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	m := NewManager(dir, Retention{})
	t.Cleanup(func() { require.NoError(t, m.Close()) })

	store, err := m.OpenOrCreate(1)
	require.NoError(t, err)
	store.Release()

	// An unremovable sidecar: os.Remove refuses a non-empty directory.
	wal := filepath.Join(dir, "1.db-wal")
	require.NoError(t, os.MkdirAll(filepath.Join(wal, "blocker"), 0o700))

	require.Error(t, m.Remove(1))

	_, err = m.OpenOrCreate(1)
	assert.ErrorIs(t, err, ErrRemoved)

	// The retry, once the obstruction is gone, still finishes the job.
	require.NoError(t, os.RemoveAll(wal))
	require.NoError(t, m.Remove(1))
	stats, err := m.Stats(context.Background(), 1)
	require.NoError(t, err)
	assert.False(t, stats.Exists)
}

// A Clear whose files will not go still leaves the cache usable: the caller retries,
// and until then a handle must not resolve to the store Clear closed.
func TestClearKeepsTheCacheUsableWhenTheFilesWillNotGo(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	failDelete := errors.New("boom")
	blocked := true
	m := newManagerWithOptions(dir, withDeleteFiles(func(path string) error {
		if blocked {
			return failDelete
		}
		return deleteStoreFiles(path)
	}))
	t.Cleanup(func() { require.NoError(t, m.Close()) })

	ctx := context.Background()
	store, err := m.OpenOrCreate(1)
	require.NoError(t, err)
	defer store.Release()
	require.NoError(t, store.SetCookie(ctx, "v1", "pods", "1"))

	require.ErrorIs(t, m.Clear(1), failDelete)

	// The rows are still there — nothing was deleted — and the handle still writes.
	v, ok, err := store.Cookie(ctx, "v1", "pods")
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, "1", v)
	require.NoError(t, store.SetCookie(ctx, "v1", "pods", "2"))

	// The retry, once the files can go, wipes the store as usual.
	blocked = false
	require.NoError(t, m.Clear(1))
	_, ok, err = store.Cookie(ctx, "v1", "pods")
	require.NoError(t, err)
	assert.False(t, ok)
}

// A retired cache's kind clear is an error, not a panic. Acquire refuses the id here;
// the nil-store guard behind it covers the interleaving Acquire cannot see, a Delete
// landing between the claim and the resolve.
func TestManagerClearKindOnARetiredCacheIsAnError(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	// Delete leaves the file, so ClearKind gets past the existence check and reaches
	// the store the delete retired.
	m := newManagerWithOptions(dir, withDeleteFiles(func(string) error { return nil }))
	t.Cleanup(func() { require.NoError(t, m.Close()) })

	ctx := context.Background()
	store, err := m.OpenOrCreate(1)
	require.NoError(t, err)
	defer store.Release()

	require.NoError(t, m.Remove(1))

	assert.ErrorIs(t, store.ClearKind(ctx, podKind), ErrClosed, "a claim on a retired entry")
	// The file is still on disk (this clear's unlink is stubbed out), so the refusal has
	// to come from the tombstone rather than from the file being absent.
	_, _, err = m.OpenExisting(1)
	assert.ErrorIs(t, err, ErrRemoved)
}

// A Clear that cannot reopen retires its entry, and the handles left on it are that
// entry's alone: they read no store, and releasing them must not count down — or
// close — the store a fresh claim opened at the same id.
func TestReleaseAfterAFailedClearLeavesAFreshClaimAlone(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	// The unlink also blocks the reopen: a store cannot be created where a directory
	// already holds the name.
	m := newManagerWithOptions(dir, withDeleteFiles(func(path string) error {
		if err := deleteStoreFiles(path); err != nil {
			return err
		}
		// A directory where the fresh file must go. Chmodding the parent would say the
		// same thing on Unix and nothing at all on Windows, where a mode is not a
		// directory's write permission.
		return os.Mkdir(path, 0o700)
	}))
	t.Cleanup(func() { require.NoError(t, m.Close()) })

	ctx := context.Background()
	first, err := m.OpenOrCreate(1)
	require.NoError(t, err)
	second, err := m.OpenOrCreate(1)
	require.NoError(t, err)

	require.Error(t, m.Clear(1), "the reopen must have failed for this test to mean anything")
	assert.ErrorIs(t, first.SetCookie(ctx, "v1", "pods", "0"), ErrClosed, "a claim on the retired entry")

	require.NoError(t, os.Remove(m.path(1)), "lift the block, so a fresh claim can open")
	fresh, err := m.OpenOrCreate(1)
	require.NoError(t, err)
	defer fresh.Release()

	first.Release()
	second.Release()

	require.NotNil(t, fresh, "the fresh claim's store was dropped by a stale release")
	require.NoError(t, fresh.SetCookie(ctx, "v1", "pods", "1"))

	// The fresh entry is still the one the id resolves to, so a later claim joins that
	// store rather than opening a second one over the same file.
	other, err := m.OpenOrCreate(1)
	require.NoError(t, err)
	defer other.Release()
	assert.Same(t, fresh.e, other.e, "a second file was opened beside the live one")
}

// A cache that was never opened has no file to claim, and asking must not create one —
// schema, sidecars and all.
func TestOpenExistingCreatesNothingForACacheWithNoFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	m := NewManager(dir, Retention{})
	t.Cleanup(func() { require.NoError(t, m.Close()) })

	store, ok, err := m.OpenExisting(1)
	require.NoError(t, err)
	require.False(t, ok)
	require.Nil(t, store)

	stats, err := m.Stats(context.Background(), 1)
	require.NoError(t, err)
	assert.False(t, stats.Exists, "the open created the cache it found nothing of")

	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	assert.Empty(t, entries, "the clear left files behind")
}

// The state watch borrows the feed of whatever is open, and borrowing must never create a file:
// that is what would resurrect a cache whose teardown deleted it.
func TestManagerSubscribeAnswersNothingForAnUnopenedCache(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	m := NewManager(dir, Retention{})
	t.Cleanup(func() { require.NoError(t, m.Close()) })

	_, ok := m.ListenForChanges(1)
	assert.False(t, ok)
	assert.NoFileExists(t, filepath.Join(dir, "1.db"))
}

// The feed comes with no claim, so a borrower cannot keep the cache alive: the writer's
// release still closes the file.
func TestManagerSubscribeTakesNoClaim(t *testing.T) {
	t.Parallel()

	m := NewManager(t.TempDir(), Retention{})
	t.Cleanup(func() { require.NoError(t, m.Close()) })
	store, err := m.OpenOrCreate(1)
	require.NoError(t, err)

	sub, ok := m.ListenForChanges(1)
	require.True(t, ok)
	defer sub.Close()

	store.Release()

	assert.False(t, cacheIsOpen(m, 1), "the borrowed feed held the file open")
}

// cacheIsOpen asks whether anything holds cacheID's file open, which is what
// ListenForChanges' ok reports. The receiver is closed straight away: this is a question, not a watch.
// unstattableDir returns a manager directory the OS refuses to stat under, for a reason
// that is not "missing". An embedded NUL fails in the syscall's own string conversion on
// every platform; a path under a regular file would not, being ENOTDIR on Unix and
// ERROR_PATH_NOT_FOUND — which reads as absent — on Windows.
func unstattableDir(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "a\x00dir")
}

// closesAndFails is a close that reports err having closed the file anyway: Windows will
// not unlink a file something still holds open, so a fake that only reports leaves
// t.TempDir()'s own cleanup to fail.
func closesAndFails(err error) func(*file) error {
	return func(f *file) error {
		_ = f.close()
		return err
	}
}

func cacheIsOpen(m *Manager, cacheID int64) bool {
	sub, ok := m.ListenForChanges(cacheID)
	if ok {
		sub.Close()
	}
	return ok
}

// The counts answer for a cache nobody is syncing too: the workers are stopped while a
// cluster is paused, and a state watch reporting no objects beside a megabyte-sized file
// would read as data loss. Reading it opens the file read-only, so a cache that does
// not exist stays not existing.
func TestCountsReadsAClosedCacheWithoutOpeningItForWrites(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	dir := t.TempDir()
	m := NewManager(dir, Retention{})
	t.Cleanup(func() { require.NoError(t, m.Close()) })

	store, err := m.OpenOrCreate(1)
	require.NoError(t, err)
	require.NoError(t, store.ApplyChange(ctx, Kind{APIVersion: "v1", Kind: "Pod", Resource: "pods"},
		watch.Added, obj(map[string]any{
			"apiVersion": "v1", "kind": "Pod",
			"metadata": map[string]any{"uid": "uid-1", "name": "api-0", "resourceVersion": "1"},
		})))
	store.Release()
	require.False(t, cacheIsOpen(m, 1), "the store stayed open")

	got, err := m.Stats(ctx, 1)

	require.NoError(t, err)
	assert.Equal(t, Counts{ObjectCount: 1, KindCount: 1}, got.Counts)
}

func TestCountsOfANeverExistingCacheIsEmpty(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	m := NewManager(dir, Retention{})
	t.Cleanup(func() { require.NoError(t, m.Close()) })

	got, err := m.Stats(context.Background(), 1)

	require.NoError(t, err)
	assert.Equal(t, Stats{}, got)
	assert.NoFileExists(t, filepath.Join(dir, "1.db"))
}

// A count on a cache whose file is stale reads the disk at once: a read-only open
// creates nothing, so the state watch over it keeps answering while the file waits.
func TestACountOnAStaleCacheReadsTheDisk(t *testing.T) {
	t.Parallel()

	m := newManagerWithOptions(t.TempDir(), withCloseBudget(0))
	t.Cleanup(func() { _ = m.Close() })
	store, err := m.OpenOrCreate(1)
	require.NoError(t, err)
	conn, err := openFileOf(t, store).db.Conn(context.Background())
	require.NoError(t, err)
	store.Release()

	stats, err := m.Stats(context.Background(), 1)

	require.NoError(t, err)
	assert.True(t, stats.Exists)
	require.ErrorIs(t, m.Clear(1), sqlitepool.ErrStillOpen, "the stale file was forgotten")
	require.NoError(t, conn.Close())
	require.NoError(t, m.Clear(1))
	assert.NoFileExists(t, m.path(1))
}

// A close that fails leaves the file unusable whatever it reports, so the entry has to
// be retired: a claim on it must answer ErrClosed rather than reach a dead database, and
// the next open starts the cache fresh. A failure that left no connection open does not
// skip the delete, or the clear would have cleared nothing.
func TestClearRetiresTheEntryWhenTheFileWillNotClose(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	boom := errors.New("close failed")
	m := newManagerWithOptions(t.TempDir(), withCloseFile(closesAndFails(boom)))
	store, err := m.OpenOrCreate(1)
	require.NoError(t, err)
	defer store.Release()

	require.ErrorIs(t, m.Clear(1), boom)

	assert.ErrorIs(t, store.SetCookie(ctx, "v1", "pods", "1"), ErrClosed,
		"a claim reached the file the clear could not close")
	_, err = store.ListenForChanges()
	assert.ErrorIs(t, err, ErrClosed, "a subscription was handed back on a dead bus")
	assert.False(t, cacheIsOpen(m, 1), "the retired entry is still bound")
	assert.NoFileExists(t, m.path(1), "the failed close skipped the delete")
}

// A clear closes, unlinks and re-creates the file, and the fresh one has no schema until
// its migrations run — all of it under the manager's lock. A measurement must wait that
// out rather than read what is there mid-way: the state watch above it is subscribed by the
// very view holding the Clear button, and a failed read ends that subscription.
func TestStatsWaitsOutAClear(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	clearing, release := make(chan struct{}), make(chan struct{})
	m := newManagerWithOptions(t.TempDir(), withDeleteFiles(func(path string) error {
		close(clearing)
		<-release
		return deleteStoreFiles(path)
	}))
	t.Cleanup(func() { require.NoError(t, m.Close()) })

	// A file on disk with nothing open over it, which is the path that reads from disk.
	store, err := m.OpenOrCreate(1)
	require.NoError(t, err)
	store.Release()

	cleared := make(chan struct{})
	go func() { defer close(cleared); assert.NoError(t, m.Clear(1)) }()
	testutil.Wait(t, clearing, "the clear to take the lock")
	// Released whatever the assertions do, or a failure would leave the clear holding
	// the lock and deadlock the manager's own close.
	letClearFinish := sync.OnceFunc(func() { close(release) })
	defer func() { letClearFinish(); <-cleared }()

	measured := make(chan error, 1)
	go func() {
		_, err := m.Stats(ctx, 1)
		measured <- err
	}()

	// A negative assertion needs a bounded window: the measurement must be waiting on the
	// clear, not reading through it.
	testutil.NoRecv(t, measured, 50*time.Millisecond, "a measurement taken mid-clear")
	letClearFinish()

	assert.NoError(t, testutil.Recv(t, measured, "the measurement once the clear landed"))
}

// A Clear closes the file and installs a fresh empty one on the same entry. A claim taken
// before that must not follow the swap: a re-read of the new file would answer "no rows" for
// a cache that was full, and the watch would emit a Deleted for every row it held.
func TestAClaimTakenBeforeAClearDoesNotFollowTheSwap(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	m := NewManager(t.TempDir(), Retention{})
	t.Cleanup(func() { require.NoError(t, m.Close()) })

	writer, err := m.OpenOrCreate(1)
	require.NoError(t, err)
	defer writer.Release()
	require.NoError(t, writer.SyncKinds(ctx, []KindRow{podRow}, true, 7))

	bound, ok, err := m.OpenExisting(1)
	require.NoError(t, err)
	require.True(t, ok)
	defer bound.Release()
	kinds, err := bound.Kinds(ctx)
	require.NoError(t, err)
	require.Len(t, kinds, 1)

	require.NoError(t, m.Clear(1))

	_, err = bound.Kinds(ctx)
	assert.ErrorIs(t, err, ErrClosed, "the bound store read the file the clear swapped in")
}

// A fresh claim after the clear is how a reconnecting watch reaches the new file.
func TestAFreshClaimAfterAClearReadsTheNewFile(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	m := NewManager(t.TempDir(), Retention{})
	t.Cleanup(func() { require.NoError(t, m.Close()) })

	writer, err := m.OpenOrCreate(1)
	require.NoError(t, err)
	defer writer.Release()
	require.NoError(t, writer.SyncKinds(ctx, []KindRow{podRow}, true, 7))
	require.NoError(t, m.Clear(1))

	bound, ok, err := m.OpenExisting(1)
	require.NoError(t, err)
	require.True(t, ok)
	defer bound.Release()
	kinds, err := bound.Kinds(ctx)
	require.NoError(t, err)
	assert.Empty(t, kinds, "the clear emptied the file")
}

// An idle cache — paused, or freshly restarted — has no claim on it and so no open file,
// but its rows are still on disk. A read that only bound to an already-open file would
// report it empty.
func TestOpenExistingReadsACacheNobodyHoldsOpen(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	m := NewManager(t.TempDir(), Retention{})
	t.Cleanup(func() { require.NoError(t, m.Close()) })

	writer, err := m.OpenOrCreate(1)
	require.NoError(t, err)
	require.NoError(t, writer.SyncKinds(ctx, []KindRow{podRow}, true, 7))
	writer.Release()
	require.False(t, cacheIsOpen(m, 1), "the last release left the file open")

	store, ok, err := m.OpenExisting(1)
	require.NoError(t, err)
	require.True(t, ok)
	defer store.Release()

	kinds, err := store.Kinds(ctx)
	require.NoError(t, err)
	assert.Len(t, kinds, 1)
}

// A watch can open before anything has created the cache's file, and must go live the moment
// one does rather than waiting out a poll.
func TestListenForCacheOpenChangesFiresWhenTheStoreOpens(t *testing.T) {
	t.Parallel()

	m := NewManager(t.TempDir(), Retention{})
	t.Cleanup(func() { require.NoError(t, m.Close()) })

	opened := m.ListenForCacheOpenChanges(1)
	defer opened.Close()

	store, err := m.OpenOrCreate(1)
	require.NoError(t, err)
	defer store.Release()

	testutil.Recv(t, opened.Chan(), "the open signal")
}

// The signal is for a store that was not there yet; one already open needs no wait, and
// OpenExisting is what the caller tries first.
func TestListenForCacheOpenChangesDoesNotFireForAnAlreadyOpenStore(t *testing.T) {
	t.Parallel()

	m := NewManager(t.TempDir(), Retention{})
	t.Cleanup(func() { require.NoError(t, m.Close()) })

	store, err := m.OpenOrCreate(1)
	require.NoError(t, err)
	defer store.Release()

	opened := m.ListenForCacheOpenChanges(1)
	defer opened.Close()

	// A negative assertion needs a bounded window; the signal would already be pending.
	if _, err := opened.TryRecv(); err == nil {
		t.Fatal("the open signal fired for a store that was already open")
	}
}

// The reader pool exists so a watch's re-read never queues behind the one write connection.
// Opened with the writer's DSN it carries a write lock instead, which shows up as contention
// rather than as an error — so the pool is opened by the opener that refuses writes.
func TestTheReaderPoolRefusesWrites(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	f, err := store.file()
	require.NoError(t, err)

	_, err = f.readDB.ExecContext(context.Background(),
		`INSERT INTO cluster_meta (key, value) VALUES ('k', 'v')`)

	require.Error(t, err)
}

// A file that will not open is reported rather than papered over: a cache whose file is
// unreadable is a fault to show, and answering with a fresh empty one would silently
// discard whatever is on disk.
func TestOpenReportsAFileThatIsNotADatabase(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "1.db"), []byte("not a database"), 0o600))
	m := NewManager(dir, Retention{})

	_, err := m.OpenOrCreate(1)

	assert.ErrorContains(t, err, "not a database")
}

// The caches directory is created on demand, so a path that cannot become one is where
// the open fails — before any of the file's own machinery is reached.
func TestOpenReportsADirectoryItCannotCreate(t *testing.T) {
	t.Parallel()

	blocked := filepath.Join(t.TempDir(), "a-file")
	require.NoError(t, os.WriteFile(blocked, []byte("x"), 0o600))
	m := NewManager(blocked, Retention{})

	_, err := m.OpenOrCreate(1)

	assert.ErrorContains(t, err, "mkdir")
}

// A migration that will not run leaves the file as it found it and says so — opening over
// a schema nobody can read would put every later statement's failure in its place.
func TestOpenReportsAMigrationItCannotRun(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	seedRawDB(t, filepath.Join(dir, "1.db"), `CREATE TABLE schema_migrations(wrong TEXT)`)
	m := NewManager(dir, Retention{})

	_, err := m.OpenOrCreate(1)

	assert.ErrorContains(t, err, "migrate")
}

// The statements are prepared at open, so a file whose migrations claim to have run over
// tables that are not there fails HERE — once, naming the statement — rather than on
// whichever write happens to be first.
func TestOpenReportsAStatementItCannotPrepare(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	seedRawDB(t, filepath.Join(dir, "1.db"),
		`CREATE TABLE schema_migrations(version INTEGER NOT NULL);
		 INSERT INTO schema_migrations(version) VALUES (`+currentSchemaVersion(t)+`)`)
	m := NewManager(dir, Retention{})

	_, err := m.OpenOrCreate(1)

	assert.ErrorContains(t, err, "prepare")
}

// A stat that fails for a reason other than absence is a fault, not an absent cache:
// answering "no such cache" would have the caller create one where it cannot.
func TestOpenExistingReportsAPathItCannotStat(t *testing.T) {
	t.Parallel()

	m := NewManager(unstattableDir(t), Retention{})

	_, _, err := m.OpenExisting(1)

	assert.ErrorContains(t, err, "open cache 1")
}

// Stats answers a missing cache as absent, but anything else is a measurement that
// failed — and the state watch above it must say so rather than render a zero.
func TestStatsReportsAPathItCannotStat(t *testing.T) {
	t.Parallel()

	m := NewManager(unstattableDir(t), Retention{})

	_, err := m.Stats(context.Background(), 1)

	assert.ErrorContains(t, err, "stats")
}

// A file that exists and will not read is a fault of its own: the size is measurable and
// the counts are not, and half a measurement is not one.
func TestStatsReportsAFileItCannotRead(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "1.db"), []byte("not a database"), 0o600))
	m := NewManager(dir, Retention{})

	_, err := m.Stats(context.Background(), 1)

	assert.ErrorContains(t, err, "counts")
}

// A cache removed between the stat and the read is gone, not broken. SQLite reports a
// missing file as an ordinary error, so the file itself is what tells the two apart.
func TestCountsFromDiskAnswersAVanishedFileAsEmpty(t *testing.T) {
	t.Parallel()

	m := NewManager(t.TempDir(), Retention{})

	counts, err := m.countsFromDiskLocked(context.Background(), filepath.Join(t.TempDir(), "gone.db"), 1)

	require.NoError(t, err)
	assert.Zero(t, counts.ObjectCount)
}

// Remove retires the entry and deletes the file whatever happens, but a close that fails
// is still reported, and until a pass succeeds nothing may reopen.
func TestRemoveReportsAFileThatWillNotClose(t *testing.T) {
	t.Parallel()

	boom := errors.New("close failed")
	m := newManagerWithOptions(t.TempDir(), withCloseFile(closesAndFails(boom)))
	store, err := m.OpenOrCreate(1)
	require.NoError(t, err)
	defer store.Release()

	assert.ErrorIs(t, m.Remove(1), boom)
	assert.NoFileExists(t, m.path(1), "the failed close skipped the delete")
}

// A file whose close ran out of budget may still have a connection open, and SQLite's
// close of a file's last connection deletes the WAL by path: deleting the file and
// creating a fresh one at that path would hand the fresh one's WAL to that close. So
// nothing deletes or opens the path until a retry has waited the file's pools out.
func TestNothingTouchesThePathOfAFileStillClosing(t *testing.T) {
	t.Parallel()

	m := newManagerWithOptions(t.TempDir(), withCloseBudget(0))
	t.Cleanup(func() { _ = m.Close() })
	store, err := m.OpenOrCreate(1)
	require.NoError(t, err)
	defer store.Release()
	// Held straight from the pool, as a cancelled transaction's rollback holds one on
	// database/sql's goroutine, so the close finds it in use.
	conn, err := openFileOf(t, store).db.Conn(context.Background())
	require.NoError(t, err)

	require.ErrorIs(t, m.Clear(1), sqlitepool.ErrStillOpen)
	assert.FileExists(t, m.path(1), "deleted under an open connection")
	_, err = m.OpenOrCreate(1)
	assert.ErrorIs(t, err, sqlitepool.ErrStillOpen, "opened over an open connection")
	require.ErrorIs(t, m.Clear(1), sqlitepool.ErrStillOpen, "the retry found the connection closed")
	assert.FileExists(t, m.path(1))

	require.NoError(t, conn.Close())

	require.NoError(t, m.Clear(1))
	assert.NoFileExists(t, m.path(1))
	fresh, err := m.OpenOrCreate(1)
	require.NoError(t, err)
	fresh.Release()
}

// Remove follows the same rule, and the retry deletes once the pools have closed.
func TestRemoveWaitsOutAFileStillClosing(t *testing.T) {
	t.Parallel()

	m := newManagerWithOptions(t.TempDir(), withCloseBudget(0))
	t.Cleanup(func() { _ = m.Close() })
	store, err := m.OpenOrCreate(1)
	require.NoError(t, err)
	defer store.Release()
	conn, err := openFileOf(t, store).db.Conn(context.Background())
	require.NoError(t, err)

	require.ErrorIs(t, m.Remove(1), sqlitepool.ErrStillOpen)
	assert.FileExists(t, m.path(1), "deleted under an open connection")
	require.NoError(t, conn.Close())

	require.NoError(t, m.Remove(1))
	assert.NoFileExists(t, m.path(1))
}

// The manager's Close keeps a file a connection still holds as stale too, since the
// cache pass may still remove the cache after the shutdown began.
func TestCloseKeepsAFileStillClosingForTheRemoveAfterIt(t *testing.T) {
	t.Parallel()

	m := newManagerWithOptions(t.TempDir(), withCloseBudget(0))
	store, err := m.OpenOrCreate(1)
	require.NoError(t, err)
	defer store.Release()
	conn, err := openFileOf(t, store).db.Conn(context.Background())
	require.NoError(t, err)

	require.ErrorIs(t, m.Close(), sqlitepool.ErrStillOpen)
	require.ErrorIs(t, m.Remove(1), sqlitepool.ErrStillOpen)
	assert.FileExists(t, m.path(1), "deleted under an open connection")
	require.NoError(t, conn.Close())

	require.NoError(t, m.Remove(1))
	assert.NoFileExists(t, m.path(1))
}

// The manager has nothing to do at startup — the files open on demand — so it satisfies
// the lifecycle contract without holding one open.
func TestStartHasNothingToDo(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	m := NewManager(t.TempDir(), Retention{})
	t.Cleanup(func() { require.NoError(t, m.Close()) })

	stop, err := m.Start(ctx)

	require.NoError(t, err)
	assert.NoError(t, stop(ctx))
}

// seedRawDB writes a cache file directly, standing in for one an older build left behind
// or a corruption the manager has to meet on open.
func seedRawDB(t *testing.T, path, schema string) {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+path)
	require.NoError(t, err)
	defer db.Close() //nolint:errcheck // seeded and done with
	_, err = db.ExecContext(context.Background(), schema)
	require.NoError(t, err)
}

// currentSchemaVersion is the version the shipped migrations end at. A seeded file claims
// it, so Apply skips the file and the open reaches what comes after.
func currentSchemaVersion(t *testing.T) string {
	t.Helper()
	entries, err := fs.ReadDir(migrationsFS, "migrations")
	require.NoError(t, err)
	return strconv.Itoa(len(entries))
}

// The reader pool prepares its own half of the statements, so a schema that satisfies the
// writers and not the readers fails at open rather than on the first watch — where an
// empty table would be all the user saw.
func TestOpenReportsAReadStatementItCannotPrepare(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	// Migrated to the current version so Apply skips, over tables the writers can use and
	// the readers cannot: kind_counts without the column the nav's counts come from.
	seedRawDB(t, filepath.Join(dir, "1.db"), `
		CREATE TABLE schema_migrations(version INTEGER NOT NULL);
		INSERT INTO schema_migrations(version) VALUES (`+currentSchemaVersion(t)+`);
		CREATE TABLE cluster_meta(key TEXT PRIMARY KEY, value TEXT NOT NULL);
		CREATE TABLE objects(uid TEXT PRIMARY KEY, api_version TEXT, kind TEXT, namespace TEXT,
			name TEXT, resource_version TEXT, generation INT, created_at INT, updated_at INT,
			status_summary TEXT, ready_count INT, total_count INT, restart_count INT, host TEXT,
			write_seq INT, changed_at INT, raw_json BLOB);
		CREATE TABLE owner_refs(child_uid TEXT, owner_uid TEXT, is_controller INT,
			PRIMARY KEY(child_uid, owner_uid));
		CREATE TABLE labels(uid TEXT, key TEXT, value TEXT, PRIMARY KEY(uid, key));
		CREATE TABLE containers(pod_uid TEXT, init INT, position INT, name TEXT, sidecar INT,
			image TEXT, image_id TEXT, container_id TEXT, ready INT, restarts INT, state TEXT,
			reason TEXT, exit_code INT, last_reason TEXT, last_exit_code INT,
			last_container_id TEXT, cpu_request REAL,
			cpu_limit REAL, memory_request REAL, memory_limit REAL);
		CREATE TABLE refs(uid TEXT, path TEXT, to_group TEXT, to_kind TEXT, to_namespace TEXT,
			to_name TEXT, key TEXT, optional INT, PRIMARY KEY(uid, path));
		CREATE TABLE selectors(uid TEXT PRIMARY KEY, namespace TEXT);
		CREATE TABLE selector_terms(uid TEXT, term INT, key TEXT, op TEXT, vals TEXT,
			PRIMARY KEY(uid, term));
		CREATE TABLE status_history(uid TEXT, at INT, summary TEXT);
		CREATE TABLE deletes(seq INT, api_version TEXT, kind TEXT, uid TEXT, at INT);
		CREATE TABLE events(uid TEXT PRIMARY KEY, involved_uid TEXT, involved_kind TEXT,
			involved_ns TEXT, involved_name TEXT, type TEXT, reason TEXT, message TEXT,
			first_seen INT, last_seen INT, count INT, raw_json BLOB, updated_at INT,
			resource_version TEXT, write_seq INT);
		CREATE TABLE kind_catalog(api_version TEXT, kind TEXT, resource TEXT, scope TEXT,
			is_crd INT, schema_json TEXT, printer_columns TEXT, PRIMARY KEY(api_version, kind));
		CREATE TABLE kind_counts(api_version TEXT, kind TEXT, PRIMARY KEY(api_version, kind));`)
	m := NewManager(dir, Retention{})

	_, err := m.OpenOrCreate(1)

	assert.ErrorContains(t, err, "kind_counts")
}

// statDiskUsage is the one measurement both callers share: the state watch through Stats, and
// the janitor's size check. Its error return is what keeps "not cached" and "cannot read
// this file" apart — collapsing them would report a full cache as absent.
func TestStatDiskUsageMeasuresTheThreeFiles(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	m := NewManager(dir, Retention{})
	t.Cleanup(func() { require.NoError(t, m.Close()) })
	store, err := m.OpenOrCreate(1)
	require.NoError(t, err)
	t.Cleanup(store.Release)
	require.NoError(t, store.SetCookie(context.Background(), "v1", "pods", "1"))

	usage, err := statDiskUsage(filepath.Join(dir, "1.db"))

	require.NoError(t, err)
	main, statErr := os.Stat(filepath.Join(dir, "1.db"))
	require.NoError(t, statErr)
	assert.Equal(t, main.Size(), usage.db)
	assert.Positive(t, usage.shm, "the sidecars are there while the file is open")
	assert.GreaterOrEqual(t, usage.wal, int64(0))
	assert.Equal(t, usage.db+usage.wal+usage.shm, usage.total())
}

// A missing main file is the caller's to interpret, so it comes back as a plain
// os.IsNotExist rather than as zeroes: Stats reads it as "not cached", the janitor as a
// file that went while it swept.
func TestStatDiskUsageReportsAMissingFile(t *testing.T) {
	t.Parallel()

	_, err := statDiskUsage(filepath.Join(t.TempDir(), "nope.db"))

	assert.True(t, os.IsNotExist(err), "got %v", err)
}

// The state watch reports the janitor's verdict rather than recomputing one. A sweep checkpoints
// a WAL-heavy file before deciding and a bare measurement does not, so two verdicts would
// disagree mid-checkpoint — and the pause hangs off this one.
func TestStatsReportsTheJanitorsVerdict(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	m := NewManager(t.TempDir(), Retention{SizeLimit: gib})
	t.Cleanup(func() { require.NoError(t, m.Close()) })
	store, err := m.OpenOrCreate(1)
	require.NoError(t, err)
	t.Cleanup(store.Release)

	sweep(ctx, openFileOf(t, store), Retention{SizeLimit: 1})

	got, err := m.Stats(ctx, 1)
	require.NoError(t, err)
	assert.True(t, got.OverSizeLimit)
	assert.Equal(t, int64(gib), got.SizeLimitBytes, "the ceiling a client renders against")
}

// A cache nobody has open has no janitor and so no verdict. It is the right answer — a
// closed cache cannot grow — but it sits beside a Bytes of whatever the file holds, so it
// has to be stated rather than discovered.
func TestStatsReportsNoVerdictForAClosedCache(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	m := NewManager(t.TempDir(), Retention{SizeLimit: 1})
	t.Cleanup(func() { require.NoError(t, m.Close()) })
	store, err := m.OpenOrCreate(1)
	require.NoError(t, err)
	store.Release()
	require.False(t, cacheIsOpen(m, 1))

	got, err := m.Stats(ctx, 1)

	require.NoError(t, err)
	assert.True(t, got.Exists)
	assert.False(t, got.OverSizeLimit, "no open file, no verdict")
}

// A manager with no interval runs no janitor, so nothing ever forms a verdict. Every test
// that opens a manager for some other reason is in this state.
func TestStatsReportsNoVerdictWithoutAJanitor(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	m := NewManager(t.TempDir(), Retention{SizeLimit: 1})
	t.Cleanup(func() { require.NoError(t, m.Close()) })
	store, err := m.OpenOrCreate(1)
	require.NoError(t, err)
	t.Cleanup(store.Release)

	got, err := m.Stats(ctx, 1)

	require.NoError(t, err)
	assert.False(t, got.OverSizeLimit)
}

// The last release keeps a file a connection still holds as stale too: it is the
// commonest close, and a Clear after it would otherwise delete under the connection.
func TestTheLastReleaseKeepsAFileStillClosing(t *testing.T) {
	t.Parallel()

	m := newManagerWithOptions(t.TempDir(), withCloseBudget(0))
	t.Cleanup(func() { _ = m.Close() })
	store, err := m.OpenOrCreate(1)
	require.NoError(t, err)
	conn, err := openFileOf(t, store).db.Conn(context.Background())
	require.NoError(t, err)

	store.Release()

	require.ErrorIs(t, m.Clear(1), sqlitepool.ErrStillOpen)
	assert.FileExists(t, m.path(1), "deleted under an open connection")
	require.NoError(t, conn.Close())

	require.NoError(t, m.Clear(1))
	assert.NoFileExists(t, m.path(1))
}
