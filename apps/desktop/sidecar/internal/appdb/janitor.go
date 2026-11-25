// The file's janitor: how app.db hands its free pages back to the OS.
package appdb

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"
)

// vacuumPagesPerSweep bounds the free pages one pass hands back (~8MiB at a 4KiB page),
// so the writer is never held for the whole freelist.
const vacuumPagesPerSweep = 2048

// runJanitor sweeps until ctx is cancelled; a sweep that fails is retried by the next one.
// The first sweep runs at once, so a freelist the last run left is swept at startup rather
// than an interval in. After that only the ticker wakes it — there is no wake-on-commit,
// since nothing here judges a limit that has to be current within seconds, and a wake
// after each of a turn's progress writes would run freelist_count for nobody.
func runJanitor(ctx context.Context, db *DB, every time.Duration) {
	sweep(ctx, db)

	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		sweep(ctx, db)
	}
}

// sweep is one janitor pass: hand freed pages back to the OS, then truncate the log when
// it pays. Sweeps are ordinary writes, so they serialize with a turn's progress writes
// behind the single writer connection.
func sweep(ctx context.Context, db *DB) {
	// Under auto_vacuum=INCREMENTAL this walks only the freelist. The freelist decides,
	// never what any write freed: the delete that frees pages does not vacuum, and a
	// rows-deleted gate would strand the file at its high-water mark.
	var freePages int64
	if err := db.Write.QueryRowContext(ctx, `PRAGMA freelist_count`).Scan(&freePages); err != nil {
		slog.Warn("appdb: freelist_count failed", "err", err)
		return
	}
	if freePages > 0 {
		pages := min(freePages, db.vacuumPages)
		if _, err := db.Write.ExecContext(ctx, fmt.Sprintf(`PRAGMA incremental_vacuum(%d)`, pages)); err != nil {
			slog.Warn("appdb: incremental_vacuum failed", "err", err)
		}
		// Under WAL the shrink is a frame in the log; the database file gives its bytes
		// up only when a checkpoint moves it.
		walCheckpoint(ctx, db)
		return
	}
	// SQLite's automatic checkpoint moves frames into the file but never truncates the
	// log, so a log larger than the file it feeds is a truncate owed.
	if fileSize(db.path+"-wal") > fileSize(db.path) {
		walCheckpoint(ctx, db)
	}
}

// fileSize is a file's size in bytes, a missing sidecar counting as zero.
func fileSize(path string) int64 {
	fi, err := os.Stat(path)
	if err != nil {
		return 0
	}
	return fi.Size()
}

// walCheckpoint moves the write-ahead log into the database and truncates it. Not
// "checkpoint": chat's checkpoint is an answer's progress write, nothing to do with
// the log. TRUNCATE calls the busy handler until every reader is off the log, so a write
// can queue behind it for up to the writer's busy_timeout. A reader still on it leaves
// part behind, which SQLite reports as busy rather than as a failure; the next sweep
// takes the rest.
func walCheckpoint(ctx context.Context, db *DB) {
	var busy, logPages, movedPages int64
	if err := db.Write.QueryRowContext(ctx, `PRAGMA wal_checkpoint(TRUNCATE)`).
		Scan(&busy, &logPages, &movedPages); err != nil {
		slog.Warn("appdb: wal_checkpoint failed", "err", err)
	}
}
