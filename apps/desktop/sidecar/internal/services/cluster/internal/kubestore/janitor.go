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

// The per-file janitor: what one cache's own tables hold, and how the file hands pages
// back to the OS.
package kubestore

import (
	"context"
	"fmt"
	"time"
)

// vacuumPagesPerSweep bounds the free pages one pass hands back (~8MiB at a 4KiB page).
const vacuumPagesPerSweep = 2048

// Retention is what the janitor's own tables hold. Events are absent on purpose: their
// retention is the server's, mirrored by the relist's prune, and the janitor sweeps only
// tables nothing upstream owns.
type Retention struct {
	// StatusHistoryTTL caps the per-object status timeline. A relist rewrites every row
	// and inserts nothing, so volume is small and a week is cheap. Zero keeps everything.
	StatusHistoryTTL time.Duration
	// DeletesTTL caps the deletes log. By age rather than by count, because what it
	// bounds is how stale a reader's cursor may be before it has to diff again. Zero
	// keeps everything.
	DeletesTTL time.Duration
	// Interval is the sweep cadence. Zero runs no janitor at all, which is what a test
	// about anything else opens its manager with.
	Interval time.Duration
	// SizeLimit caps the cache's whole footprint in bytes — the database plus its
	// -wal/-shm sidecars, the same sum Stats.Bytes reports. Zero is unbounded. The limit
	// is soft: a sweep is what notices, so a cache overshoots by whatever the cluster
	// sends between one sweep and the next.
	SizeLimit int64
}

// gib is one gibibyte, the unit SizeLimit is set in.
const gib = 1 << 30

// DefaultRetention is the production setting.
var DefaultRetention = Retention{
	StatusHistoryTTL: 7 * 24 * time.Hour,
	DeletesTTL:       time.Hour,
	Interval:         5 * time.Minute,
	SizeLimit:        2 * gib,
}

// runJanitor sweeps until ctx is cancelled; a sweep that fails is retried by the next one.
// Two things wake it: a write, so a cache filling fast is measured within seconds, and the
// ticker, for the checkpoints and vacuums that move a file's size with no write behind
// them. The first sweep runs here rather than in the open that spawned this goroutine:
// inline it would vacuum under m.mu, which Clear holds across close → unlink → reopen and
// Stats holds for its whole measurement.
func runJanitor(ctx context.Context, f *file, ret Retention) {
	sweep(ctx, f, ret)

	t := time.NewTicker(ret.Interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		case <-f.janitorWakeups:
		}
		sweep(ctx, f, ret)
	}
}

// sweep is one janitor pass: trim the tables past their retention, hand freed pages back
// to the OS, then check the file against its size limit. Sweeps are ordinary writes, so
// they serialize with the syncs behind the writer pool's single connection.
func sweep(ctx context.Context, f *file, ret Retention) {
	// A zero TTL is a table this manager was not given a retention for, and the fields are
	// independent — so a partial Retention must leave each unset one alone. Without the
	// guard the cutoff is now, which trims the whole table: for deletes that also raises
	// every kind's mark to the head of the log, invalidating every reader's cursor at once.
	if ret.StatusHistoryTTL > 0 {
		// Unbounded, unlike the vacuum below: `at` carries no index, so this is a full scan
		// — but the table is append-on-change and small by construction, and one statement
		// is cheaper than a page-by-page walk. If it ever stops being small the answer is an
		// index on `at`, not a chunked delete.
		if _, err := f.set.Stmts().Exec(ctx, stmtSweepStatusHistory,
			time.Now().Add(-ret.StatusHistoryTTL).UnixMilli(),
		); err != nil {
			warn(ctx, f, "kubestore: status_history sweep failed", err)
		}
	}

	if ret.DeletesTTL > 0 {
		if err := trimDeletes(ctx, f, time.Now().Add(-ret.DeletesTTL).UnixMilli()); err != nil {
			warn(ctx, f, "kubestore: deletes sweep failed", err)
		}
	}

	// Under auto_vacuum=INCREMENTAL this walks only the freelist. THE FREELIST decides,
	// never what this sweep itself deleted — the writers that actually free pages (a
	// relist's prune, ClearKind, a Remove) do not vacuum, so a rows-deleted gate would
	// strand the file at its high-water mark forever.
	var freePages int64
	if err := f.db.QueryRowContext(ctx, `PRAGMA freelist_count`).Scan(&freePages); err != nil {
		warn(ctx, f, "kubestore: freelist_count failed", err)
	} else if freePages > 0 {
		vacuum(ctx, f, freePages)
	}

	checkSizeLimit(ctx, f, ret.SizeLimit)
}

// warn logs a sweep's failed step. The file's close cancels ctx, often mid-sweep, and a
// step that fails after it is the close, not a failure.
func warn(ctx context.Context, f *file, msg string, err error) {
	if ctx.Err() != nil {
		return
	}
	f.log.Warn(msg, "cacheID", f.cacheID, "err", err)
}

// vacuum hands at most f.vacuumPages free pages back to the OS. Bounded because the
// argument-less form reclaims the whole freelist in one statement, holding the single
// writer for as long as that takes.
func vacuum(ctx context.Context, f *file, freePages int64) {
	pages := min(freePages, f.vacuumPages)
	if _, err := f.db.ExecContext(ctx, fmt.Sprintf(`PRAGMA incremental_vacuum(%d)`, pages)); err != nil {
		warn(ctx, f, "kubestore: incremental_vacuum failed", err)
	}
}

// checkpoint moves the write-ahead log into the database and truncates it. A reader still
// on the log leaves part of it behind, which SQLite reports as busy rather than as a
// failure: the sweep judges whatever size that left, and the next one takes the rest.
func checkpoint(ctx context.Context, f *file) {
	var busy, logPages, movedPages int64
	if err := f.db.QueryRowContext(ctx, `PRAGMA wal_checkpoint(TRUNCATE)`).
		Scan(&busy, &logPages, &movedPages); err != nil {
		warn(ctx, f, "kubestore: wal_checkpoint failed", err)
	}
}

// A sweep's verdict on the file's size, kept in file.sizeVerdict. sizeUnknown is where a
// file starts and where an unbounded manager leaves it. It is not "under": the first
// checked sweep of any file is a change and gets published — including the fresh file a
// Clear swaps in, whose release nothing else would report.
const (
	sizeUnknown int32 = iota
	sizeUnder
	sizeOver
)

// checkSizeLimit measures the file, records whether it is over its limit, and publishes
// the verdict when it changed. It runs after the vacuum: before it the size is the file's
// high-water mark, and free pages would trip a limit nothing is filling.
//
// A failed measurement leaves the verdict alone. A file gone from under a sweep is a clear
// or a teardown, and answering "under" for it would publish a release nothing released.
// A closing file is not measured at all.
func checkSizeLimit(ctx context.Context, f *file, limit int64) {
	if limit <= 0 || ctx.Err() != nil {
		return
	}
	usage, err := statDiskUsage(f.path)
	if err != nil {
		warn(ctx, f, "kubestore: size check failed", err)
		return
	}
	// A log larger than its database is a checkpoint owed, not a full cache: its pages
	// overwrite ones the database already counts, so the sum would count them twice and
	// could pause a cache whose contents fit.
	if usage.wal > usage.db {
		checkpoint(ctx, f)
		if usage, err = statDiskUsage(f.path); err != nil { // coverage:ignore only the file going during the checkpoint reaches it, and the stat before it found the file
			warn(ctx, f, "kubestore: size check failed", err)
			return
		}
	}
	verdict := sizeUnder
	if usage.total() > limit {
		verdict = sizeOver
	}
	if f.sizeVerdict.Swap(verdict) != verdict {
		// Fails only on a closed hub: the manager shutting down under a sweep in flight.
		_ = f.sizeLimitSender.Send(f.cacheID, struct{}{})
	}
}
