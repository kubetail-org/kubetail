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

// Package logstore is the log cache's file: one SQLite database per cluster cache, holding
// the lines read from the cluster and, per stream, which ranges of them are whole. The
// schema is migrations/0001_init.sql.
//
// A Store is a claim on an open file, and the Manager is the only way to one. A clear or a
// removal has to close every claim's file before deleting it: on POSIX a delete under an
// open handle does not fail, it forks the file, the old handle writing into the unlinked
// inode while a fresh open starts empty. Sequencing that is only possible where the claims
// are held.
package logstore

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/amorey/gobus/conflate"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/sqlitemigrate"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/sqlitepool"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/sqlstmt"
)

// readerPoolSize is how many reads run beside the one writer.
const readerPoolSize = 4

// file is one open log file: the writer, the reader pool beside it, the statements
// prepared on both, and the bus every commit of lines pings by stream id.
type file struct {
	path   string
	db     *sqlitepool.Pool
	readDB *sqlitepool.Pool
	// closeBudget is how long closePools waits for a connection still in use.
	closeBudget time.Duration
	set         *sqlstmt.Set[stmtID]
	bus         *conflate.Hub[int64, struct{}]
	// w is the file's writer, nil for a file opened outside a manager.
	w *writer
	// heldRows and heldChunks are the stream rows and chunks in use, which a sweep
	// leaves alone.
	heldRows   holds
	heldChunks holds
	// written is the message bytes committed since the janitor last woke, and wake,
	// of capacity one, wakes it once they pass sweepBytes, 0 for a file with no
	// janitor; janitor joins it on close.
	written    atomic.Int64
	sweepBytes int64
	wake       chan struct{}
	janitor    sync.WaitGroup
	// short is whether the last sweep stopped over budget with nothing to evict, so
	// the log says so as the file enters that state and as it leaves, not every sweep.
	short atomic.Bool
	// unclosed is the chunks of ended tails whose close failed, which the janitor
	// closes before it picks what to evict.
	unclosedMu sync.Mutex
	unclosed   []int64
	// podsAfter is the last pod uid a sweep asked about, where the next one takes up.
	podsMu    sync.Mutex
	podsAfter string
	// ctx is what an operation runs its statements under; close cancels it, so the wait
	// for inUse is a wait for interrupted statements to unwind. inUse counts the
	// operations running on the file, which close waits for, since sql.DB.Close does not
	// wait for a connection in use and Windows refuses to delete a file one holds open.
	// An operation registers under the manager's lock, where the file leaves its entry.
	ctx    context.Context
	cancel context.CancelFunc
	inUse  sync.WaitGroup
}

// openFile opens the file at path, creating its directory, the file and the schema on
// first touch. The directory is 0700 and the file 0600, since a log holds whatever the
// cluster wrote. closeBudget is how long its close waits for a connection still in use.
func openFile(path string, closeBudget time.Duration) (*file, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("mkdir: %w", err)
	}
	db, err := sqlitepool.OpenWriter(path)
	if err != nil {
		return nil, fmt.Errorf("open writer: %w", err)
	}
	// A failure closes the pools under the budget and reports what that answered.
	// Nothing else holds them and nothing cancels, so no connection is in use.
	if _, err := sqlitemigrate.Apply(context.Background(), db.DB, migrationsFS, "migrations"); err != nil {
		return nil, errors.Join(fmt.Errorf("migrate: %w", err), sqlitepool.CloseAll(closeBudget, db))
	}
	// After the migrations, so a reader never races the schema onto a fresh file.
	readDB := sqlitepool.OpenReader(path, readerPoolSize)
	set, err := sqlstmt.Prepare[stmtID](context.Background(), db.DB, readDB.DB, statements)
	if err != nil {
		return nil, errors.Join(fmt.Errorf("prepare: %w", err), sqlitepool.CloseAll(closeBudget, db, readDB))
	}
	// Closing a chunk is its tailer's last write, so a crash or a failed write leaves it
	// open; no tailer runs before the file is open.
	if _, err := set.Stmts().Exec(context.Background(), stmtCloseOpenChunks); err != nil {
		return nil, errors.Join(fmt.Errorf("close open chunks: %w", err), set.Close(),
			sqlitepool.CloseAll(closeBudget, db, readDB))
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &file{
		path: path, db: db, readDB: readDB, closeBudget: closeBudget, set: set, bus: conflate.New[int64, struct{}](),
		heldRows: holds{n: map[int64]int{}}, heldChunks: holds{n: map[int64]int{}},
		wake: make(chan struct{}, 1), ctx: ctx, cancel: cancel,
	}, nil
}

// close ends the bus's receivers, interrupts the file's operations, its janitor and
// its writer, waits for them to end, and closes both pools.
func (f *file) close() error {
	f.bus.Close()
	f.cancel()
	f.janitor.Wait()
	if f.w != nil {
		f.w.stop()
	}
	f.inUse.Wait()
	return errors.Join(f.set.Close(), f.closePools())
}

// closePools closes both pools and waits for a connection database/sql still holds,
// under one budget. Called again on a file whose wait ran out, it waits again.
func (f *file) closePools() error {
	return sqlitepool.CloseAll(f.closeBudget, f.db, f.readDB)
}

// bind is ctx ended also when the file closes, so a close interrupts the statements
// an operation runs under it.
func (f *file) bind(ctx context.Context) (context.Context, func()) {
	ctx, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(f.ctx, cancel)
	return ctx, func() {
		stop()
		cancel()
	}
}

// committed follows every commit of lines, a tailer's batch or a fill: each stream in
// streams is pinged on the bus, and the message bytes committed count toward waking
// the janitor, which wakes once they pass sweepBytes.
func (f *file) committed(streams []int64, bytes int64) {
	for _, id := range streams {
		_ = f.bus.Sender().Send(id, struct{}{}) // an error is the bus closed, whose receivers have ended
	}
	if f.sweepBytes <= 0 || f.written.Add(bytes) < f.sweepBytes {
		return
	}
	f.written.Store(0)
	select {
	case f.wake <- struct{}{}:
	default:
	}
}

// Store is one holder's claim on one cache's file, and owes a Close. The file is resolved
// per operation from the entry the claim took, so a clear or a removal reaches every
// holder: once the entry is retired the claim answers ErrClosed, never a closed database
// and never the fresh file a later open created under the same id.
type Store struct {
	m       *Manager
	cacheID int64
	e       *entry
	// closed is set by the claim's own Close, so a second Close gives nothing back.
	closed bool
}

// use resolves the file behind the claim and counts an operation on it in one critical
// section, so the operation either registers before a close and is waited for, or
// answers ErrClosed. done ends it once its statements are finished; call it before
// anything else on the manager, the claim's Close included, since a close waits for it.
func (s *Store) use() (f *file, done func(), err error) {
	s.m.mu.Lock()
	defer s.m.mu.Unlock()

	if s.closed || s.e.file == nil {
		return nil, nil, fmt.Errorf("log store %d: %w", s.cacheID, ErrClosed)
	}
	f = s.e.file
	f.inUse.Add(1)
	return f, f.inUse.Done, nil
}

// operate runs fn as one operation on the claim's file, its context ended also when
// the file closes.
func (s *Store) operate(ctx context.Context, fn func(context.Context, *file) error) error {
	f, done, err := s.use()
	if err != nil {
		return err
	}
	defer done()
	bound, stop := f.bind(ctx)
	defer stop()
	err = fn(bound, f)
	if err != nil && ctx.Err() == nil && f.ctx.Err() != nil {
		// The file's close interrupted the operation, which reads as a cancel.
		return fmt.Errorf("log store %d: %w", s.cacheID, ErrClosed)
	}
	return err
}

// with runs fn as one operation on the claim's file, its statements on the pools.
func (s *Store) with(ctx context.Context, fn func(context.Context, stmts) error) error {
	return s.operate(ctx, func(ctx context.Context, f *file) error { return fn(ctx, f.set.Stmts()) })
}

// inTx runs fn as one operation on the claim's file, inside one write transaction.
func (s *Store) inTx(ctx context.Context, fn func(context.Context, stmts) error) error {
	return s.operate(ctx, func(ctx context.Context, f *file) error {
		return f.set.InTx(ctx, func(st stmts) error { return fn(ctx, st) })
	})
}

// inReadTx runs fn as one operation on the claim's file, inside one read transaction,
// so every statement it runs sees one snapshot.
func (s *Store) inReadTx(ctx context.Context, fn func(context.Context, stmts) error) error {
	return s.operate(ctx, func(ctx context.Context, f *file) error {
		return f.set.InReadTx(ctx, func(st stmts) error { return fn(ctx, st) })
	})
}

// Close gives the claim back. The last close on an entry closes its file and drops the
// entry; on a retired entry there is nothing left to close.
func (s *Store) Close() error {
	m := s.m
	m.mu.Lock()
	defer m.mu.Unlock()

	if s.closed {
		return nil
	}
	s.closed = true
	s.e.refs--
	if s.e.refs > 0 || s.e.file == nil {
		return nil
	}
	f := m.detachLocked(s.cacheID)
	m.acquireLocked(s.cacheID)
	m.mu.Unlock()
	err := f.close()
	m.fileClosed(s.cacheID, f, err)
	m.mu.Lock()
	m.releaseLocked(s.cacheID)
	return err
}

// ListenForStreamChanges is a receiver of a ping per stream in ids each time a commit
// writes it lines, coalesced per stream. Listen before the first read, or a commit between
// the two is missed; close the receiver when done. The file closing ends it.
func (s *Store) ListenForStreamChanges(ids ...int64) (*conflate.Receiver[int64, struct{}], error) {
	f, done, err := s.use()
	if err != nil {
		return nil, fmt.Errorf("subscribe: %w", err)
	}
	defer done()
	return f.bus.Receiver(f.bus.WithKeyFilter(func(id int64) bool { return slices.Contains(ids, id) })), nil
}

// Context ends once the file behind the claim closes, by a Clear, a Remove or the
// manager's Close.
func (s *Store) Context() (context.Context, error) {
	f, done, err := s.use()
	if err != nil {
		return nil, fmt.Errorf("context: %w", err)
	}
	defer done()
	return f.ctx, nil
}
