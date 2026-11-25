package appdb

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/testutil"
)

// openTestDB is an app.db of this test's own with no janitor running, so a sweep runs
// only when the test calls it.
func openTestDB(t *testing.T) (*DB, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "app.db")
	db, err := Open(path, 0)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	return db, path
}

// fill writes n rows of a kilobyte each into a table of the test's own, so nothing here
// depends on a consumer's schema.
func fill(t *testing.T, db *DB, n int) {
	t.Helper()
	ctx := context.Background()
	_, err := db.Write.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS junk (id INTEGER PRIMARY KEY, body BLOB)`)
	require.NoError(t, err)
	_, err = db.Write.ExecContext(ctx, `
		WITH RECURSIVE seq(i) AS (SELECT 1 UNION ALL SELECT i + 1 FROM seq WHERE i < ?)
		INSERT INTO junk (body) SELECT zeroblob(1024) FROM seq`, n)
	require.NoError(t, err)
}

// clear deletes every row, which frees pages without vacuuming — the writer's ordinary
// shape, and the freelist a sweep exists to drain.
func clear(t *testing.T, db *DB) {
	t.Helper()
	_, err := db.Write.ExecContext(context.Background(), `DELETE FROM junk`)
	require.NoError(t, err)
}

// truncateLog moves the log into the file and truncates it, so the database file holds
// the rows: measured straight after a fill it is a few pages, and can be larger after a
// sweep moves the truncated state in.
func truncateLog(t *testing.T, db *DB) {
	t.Helper()
	_, err := db.Write.ExecContext(context.Background(), `PRAGMA wal_checkpoint(TRUNCATE)`)
	require.NoError(t, err)
}

func freelist(t *testing.T, db *DB) int64 {
	t.Helper()
	var n int64
	require.NoError(t, db.Write.QueryRowContext(context.Background(), `PRAGMA freelist_count`).Scan(&n))
	return n
}

// sizeOf is a file's size, a missing sidecar counting as zero.
func sizeOf(t *testing.T, path string) int64 {
	t.Helper()
	fi, err := os.Stat(path)
	if os.IsNotExist(err) {
		return 0
	}
	require.NoError(t, err)
	return fi.Size()
}

// A delete frees pages onto the freelist and nothing else hands them back: the sweep is
// the one thing that does.
func TestSweepEmptiesTheFreelist(t *testing.T) {
	db, _ := openTestDB(t)
	fill(t, db, 500)
	truncateLog(t, db)
	clear(t, db)
	require.NotZero(t, freelist(t, db), "a delete leaves a freelist")

	sweep(context.Background(), db)

	assert.Zero(t, freelist(t, db))
}

// Under WAL the vacuum's shrink is a frame in the log; the database file gives its bytes
// up only when a checkpoint moves it. The fixture checkpoints after the fill so the file
// is at a size the vacuum can shrink.
func TestSweepShrinksTheDatabaseFile(t *testing.T) {
	db, path := openTestDB(t)
	fill(t, db, 500)
	truncateLog(t, db)
	clear(t, db)
	before := sizeOf(t, path)

	sweep(context.Background(), db)

	assert.Less(t, sizeOf(t, path), before)
}

// The argument-less vacuum reclaims the whole freelist in one statement, holding the
// single writer for as long as that takes; a bounded one leaves a backlog that drains
// over the sweeps that follow.
func TestSweepVacuumsAtMostItsBound(t *testing.T) {
	db, _ := openTestDB(t)
	fill(t, db, 500)
	truncateLog(t, db)
	clear(t, db)
	db.vacuumPages = 8 // so no assertion here encodes the production page count
	before := freelist(t, db)

	sweep(context.Background(), db)
	require.Equal(t, before-8, freelist(t, db), "one sweep hands back exactly the bound")

	sweep(context.Background(), db)
	assert.Equal(t, before-16, freelist(t, db), "the backlog drains at the same bound")
}

// SQLite's automatic checkpoint moves frames into the file but never truncates the log,
// so a long streamed answer leaves a log many times the database behind it. A log larger
// than the file it feeds is a truncate owed, vacuum or no vacuum.
func TestSweepTruncatesALogThatOutweighsTheFile(t *testing.T) {
	db, path := openTestDB(t)
	fill(t, db, 500)
	require.Zero(t, freelist(t, db), "nothing to vacuum")
	require.Greater(t, sizeOf(t, path+"-wal"), sizeOf(t, path), "the rows are still in the log")

	sweep(context.Background(), db)

	assert.Zero(t, sizeOf(t, path+"-wal"))
}

// A log under the file's size is left where it is: truncating on every sweep would
// stall the writer behind the busy handler for nothing. On a tiny database the log
// outweighs it after almost any write, so this is a state the fixture constructs — a
// file of enough pages, a truncated log, then one small write.
func TestSweepLeavesASmallLogAlone(t *testing.T) {
	db, path := openTestDB(t)
	fill(t, db, 500)
	truncateLog(t, db)
	_, err := db.Write.ExecContext(context.Background(), `INSERT INTO junk (body) VALUES (zeroblob(16))`)
	require.NoError(t, err)
	before := sizeOf(t, path+"-wal")
	require.NotZero(t, before)
	require.Less(t, before, sizeOf(t, path))

	sweep(context.Background(), db)

	assert.Equal(t, before, sizeOf(t, path+"-wal"))
}

// A zero interval is what a test about anything else opens with, and it must run nothing:
// a sweep behind such a test would move the file under it.
func TestAZeroIntervalRunsNoJanitor(t *testing.T) {
	db, _ := openTestDB(t)

	assert.Nil(t, db.janitorDone)
}

// Nothing else runs a sweep, so a file that opens with an interval gets its own janitor —
// and it sweeps first thing rather than an interval in, so a freelist the last run left
// is swept at startup. The empty freelist is the evidence. Close returns only once the
// janitor has stopped, so the pools close under no one.
func TestAJanitorSweepsTheFileItWasOpenedFor(t *testing.T) {
	path := filepath.Join(t.TempDir(), "app.db")
	db, err := Open(path, 0)
	require.NoError(t, err)
	fill(t, db, 500)
	truncateLog(t, db)
	clear(t, db)
	require.NotZero(t, freelist(t, db))
	require.NoError(t, db.Close())

	db, err = Open(path, time.Millisecond)
	require.NoError(t, err)
	require.Eventually(t, func() bool { return freelist(t, db) == 0 },
		5*time.Second, time.Millisecond, "the janitor swept without anybody asking it")

	require.NoError(t, db.Close())
	select {
	case <-db.janitorDone:
	default:
		t.Fatal("Close returned before the janitor stopped")
	}
}

// The janitor sweeps on its interval, not only at startup. The first sweep ran at Open
// and is over once the first freelist is gone, so the second one is the ticker's.
func TestAJanitorKeepsSweepingOnItsInterval(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "app.db"), time.Millisecond)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	for _, round := range []string{"first", "second"} {
		fill(t, db, 500)
		truncateLog(t, db)
		clear(t, db)
		require.Eventually(t, func() bool { return freelist(t, db) == 0 },
			testutil.Timeout, time.Millisecond, "the %s freelist to be swept", round)
	}
}

// A vacuum that fails is logged and leaves the freelist for the next sweep. query_only
// refuses the vacuum's write and nothing else the sweep runs.
func TestASweepWhoseVacuumFailsLeavesItForTheNext(t *testing.T) {
	logs := testutil.CaptureLogs(t)
	db, _ := openTestDB(t)
	fill(t, db, 500)
	truncateLog(t, db)
	clear(t, db)
	before := freelist(t, db)
	_, err := db.Write.Exec(`PRAGMA query_only=1`)
	require.NoError(t, err)

	sweep(context.Background(), db)
	assert.Contains(t, logs.String(), "appdb: incremental_vacuum failed")
	require.Equal(t, before, freelist(t, db))

	_, err = db.Write.Exec(`PRAGMA query_only=0`)
	require.NoError(t, err)
	sweep(context.Background(), db)
	assert.Zero(t, freelist(t, db))
}

// A file that is not there, like a log not yet created, weighs nothing.
func TestFileSizeOfAMissingFileIsZero(t *testing.T) {
	assert.Zero(t, fileSize(filepath.Join(t.TempDir(), "app.db-wal")))
}
