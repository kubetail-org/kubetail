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

package logstore

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/sqlitepool"
)

var (
	// ErrRemoved is what OpenOrCreate answers for a cache Remove has retired.
	ErrRemoved = errors.New("log store removed")
	// ErrClosed is what a Store answers once the file under it is gone, through a Clear, a
	// Remove or the manager's Close, and what OpenOrCreate answers after Close. The claim
	// is still the holder's to Close.
	ErrClosed = errors.New("log store is closed")
)

// Manager owns the logs directory and hands out refcounted Stores, one file per cluster
// cache, named by the cache's id. Each open file runs its own writer and, with a
// non-zero Interval, its own janitor, both ended by the file's close. The logs service
// builds and closes it; the cluster service's cache pass clears and removes files
// through that service, after the close too.
type Manager struct {
	// dir holds each cache's file as "<cacheID>.db".
	dir string
	// ret bounds what each file holds: a fill closes its chunks at its ChunkBytes and
	// ChunkSpan, and a file's writer runs under its write knobs.
	ret Retention
	// now is the clock a chunk's read_at_ms is stamped from: time.Now, or a test's.
	now func() time.Time
	// after is how a writer waits out its WriteTick: time.After, or a test's.
	after func(time.Duration) <-chan time.Time
	// sweeps is told what each sweep did: nil, or a test's.
	sweeps func(cacheID int64, r sweepReport)
	// podsPerSweep is the most pods one sweep asks about: sweepPods, or a test's.
	podsPerSweep int
	// closeBudget is how long a file's close waits for a connection still in use:
	// sqlitepool.CloseBudget, or less in a test.
	closeBudget time.Duration

	// mu guards everything below. It is never held across a file's open, close or delete,
	// so a slow disk or a long query on one cache stalls no other.
	mu sync.Mutex
	// entries is the open files by cache id. An entry leaves the map the moment its file
	// is taken to close, so a listed entry always has a file and a retired one never does.
	entries map[int64]*entry
	// busy is the caches with an open, a close or a delete running outside mu, each
	// closed when it ends. Every such step waits out the one before it (acquireLocked),
	// so a delete never runs under a close still in flight, which Windows refuses, and two
	// opens never race to create one file. A cache with an entry is never busy: an open
	// installs its entry as it ends, and a close takes the entry away as it starts.
	busy map[int64]chan struct{}
	// removed is the caches Remove has retired. A cache's id is never reused, so refusing
	// it for the manager's life is the whole rule.
	removed map[int64]bool
	// stale is each cache's file whose close ran out of budget, so a connection of it may
	// still be open. SQLite's close of a file's last connection deletes the WAL by path,
	// so nothing deletes or creates the file at that path until a clear, a removal or an
	// open has waited that file's pools out. A cache has an entry or a stale file, never
	// both: an open waits the stale file out first, and every path that records one has
	// detached its entry.
	stale map[int64]*file
	// closed is set by Close, after which nothing opens.
	closed bool
	// podsGone answers which pods the mirror no longer holds, nil until SetPodsGone.
	podsGone PodsGone
}

// entry is one cache's open file and the claims on it. A Store keeps the entry it
// claimed, so a straggler from before a clear counts down its own entry and never the
// fresh one the id names now.
type entry struct {
	file *file
	refs int
}

// NewManager returns a Manager rooted at dir, its files kept to ret. Nothing is opened
// until OpenOrCreate.
func NewManager(dir string, ret Retention) *Manager {
	return &Manager{
		dir:          dir,
		ret:          ret,
		now:          time.Now,
		after:        time.After,
		podsPerSweep: sweepPods,
		closeBudget:  sqlitepool.CloseBudget,
		entries:      map[int64]*entry{},
		busy:         map[int64]chan struct{}{},
		removed:      map[int64]bool{},
		stale:        map[int64]*file{},
	}
}

// OpenOrCreate claims cacheID's file, creating the directory, the file and its schema on
// first touch, or joins the open one. Close the claim.
func (m *Manager) OpenOrCreate(cacheID int64) (*Store, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if e, ok := m.entries[cacheID]; ok {
		e.refs++
		return &Store{m: m, cacheID: cacheID, e: e}, nil
	}
	// The wait lets go of mu, so everything is read after it.
	m.acquireLocked(cacheID)
	defer m.releaseLocked(cacheID)
	if m.closed {
		return nil, fmt.Errorf("open log store %d: %w", cacheID, ErrClosed)
	}
	if m.removed[cacheID] {
		return nil, fmt.Errorf("open log store %d: %w", cacheID, ErrRemoved)
	}
	if e, ok := m.entries[cacheID]; ok {
		e.refs++
		return &Store{m: m, cacheID: cacheID, e: e}, nil
	}

	stale := m.stale[cacheID]
	m.mu.Unlock()
	if stale != nil {
		if err := stale.closePools(); err != nil {
			m.mu.Lock()
			return nil, fmt.Errorf("open log store %d: %w", cacheID, err)
		}
	}
	f, err := openFile(m.path(cacheID), m.closeBudget)
	m.mu.Lock()
	delete(m.stale, cacheID)
	if err != nil {
		return nil, fmt.Errorf("open log store %d: %w", cacheID, err)
	}
	if m.closed {
		// Close ran while the file opened, and had no entry of this one to close.
		m.mu.Unlock()
		closeErr := f.close()
		m.fileClosed(cacheID, f, closeErr)
		m.mu.Lock()
		return nil, errors.Join(fmt.Errorf("open log store %d: %w", cacheID, ErrClosed), closeErr)
	}
	f.startWriter(m.ret, m.after)
	if m.ret.Interval > 0 {
		m.startJanitor(cacheID, f)
	}
	e := &entry{file: f, refs: 1}
	m.entries[cacheID] = e
	return &Store{m: m, cacheID: cacheID, e: e}, nil
}

// Clear empties cacheID's file: the open entry is retired, so a claim still held answers
// ErrClosed, and the file is deleted, so the next open creates a fresh one under a new
// entry. The cache goes on, so the id is not tombstoned.
func (m *Manager) Clear(cacheID int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if err := m.retireLocked(cacheID); err != nil {
		return fmt.Errorf("clear log store %d: %w", cacheID, err)
	}
	return nil
}

// Remove is Clear for a cache going away with its record: the same retire and delete,
// and the id refused from then on. It is idempotent, and a removal that failed stays
// refused, since the caller retries and an open in between would recreate what is going.
func (m *Manager) Remove(cacheID int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.removed[cacheID] = true
	if err := m.retireLocked(cacheID); err != nil {
		return fmt.Errorf("remove log store %d: %w", cacheID, err)
	}
	return nil
}

// retireLocked closes cacheID's file if one is open, drops its entry, and deletes the
// file. A file a connection still holds is kept as stale and not deleted: the caller's
// retry waits its pools out first. Any other close failure leaves the delete to run,
// since the file is unusable either way. It is called with mu held and lets go of it for
// the close and the delete.
func (m *Manager) retireLocked(cacheID int64) error {
	// First, so an open in flight installs its entry and a close in flight finishes
	// before anything here looks.
	m.acquireLocked(cacheID)
	f := m.detachLocked(cacheID)
	stale := m.stale[cacheID]
	m.mu.Unlock()

	var err error
	if f != nil {
		err = f.close()
	}
	// A stale file waited out is forgotten below; one still held stays.
	if stale != nil {
		if waitErr := stale.closePools(); waitErr != nil {
			err = errors.Join(err, waitErr)
		} else {
			stale = nil
		}
	}
	keep := stale != nil || (f != nil && m.fileClosed(cacheID, f, err))
	if !keep {
		err = errors.Join(err, deleteFiles(m.path(cacheID)))
	}

	m.mu.Lock()
	if !keep {
		delete(m.stale, cacheID)
	}
	m.releaseLocked(cacheID)
	return err
}

// fileClosed records what closing cacheID's file f answered, and reports whether the file
// is kept as stale: it is when err says a connection still holds it. Every path that
// closes a file reports through here, so none can forget the rule. It takes mu.
func (m *Manager) fileClosed(cacheID int64, f *file, err error) (stale bool) {
	if !errors.Is(err, sqlitepool.ErrStillOpen) {
		return false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.stale[cacheID] = f
	return true
}

// detachLocked takes cacheID's file out of its entry and drops the entry, so every claim
// answers ErrClosed from here on. It answers nil when nothing is open.
func (m *Manager) detachLocked(cacheID int64) *file {
	e, ok := m.entries[cacheID]
	if !ok {
		return nil
	}
	f := e.file
	e.file = nil
	delete(m.entries, cacheID)
	return f
}

// acquireLocked waits out any open, close or delete running on cacheID, then marks one
// of the caller's own, which releaseLocked ends. It is called and returns with mu held,
// letting go of it while it waits.
func (m *Manager) acquireLocked(cacheID int64) {
	for {
		done, ok := m.busy[cacheID]
		if !ok {
			break
		}
		m.mu.Unlock()
		<-done
		m.mu.Lock()
	}
	m.busy[cacheID] = make(chan struct{})
}

// releaseLocked ends the step acquireLocked marked, waking whoever waits on it.
func (m *Manager) releaseLocked(cacheID int64) {
	close(m.busy[cacheID])
	delete(m.busy, cacheID)
}

// Start is the lifecycle shape; each file's work starts with its open.
func (m *Manager) Start(context.Context) (func(context.Context) error, error) {
	return func(context.Context) error { return nil }, nil
}

// Close closes every open file and refuses every open after it. Claims still out answer
// ErrClosed. Clear and Remove still delete a file after it: the cache pass that calls
// Remove clears the cache's finalizer on its answer, and nothing else would ever delete
// the file.
func (m *Manager) Close() error {
	m.mu.Lock()
	m.closed = true
	var ids []int64
	var files []*file
	for id := range m.entries {
		ids = append(ids, id)
		files = append(files, m.detachLocked(id))
	}
	for _, id := range ids {
		m.acquireLocked(id)
	}
	m.mu.Unlock()

	// At once, so the files' waits for a connection still in use overlap: a shutdown
	// spends one close budget, however many caches are open.
	errs := make([]error, len(files))
	var wg sync.WaitGroup
	for i, f := range files {
		wg.Go(func() { errs[i] = f.close() })
	}
	wg.Wait()
	for i, f := range files {
		m.fileClosed(ids[i], f, errs[i])
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	for _, id := range ids {
		m.releaseLocked(id)
	}
	return errors.Join(errs...)
}

// path is cacheID's file within the manager's directory.
func (m *Manager) path(cacheID int64) string {
	return filepath.Join(m.dir, strconv.FormatInt(cacheID, 10)+".db")
}

// deleteFiles removes the database and its -wal/-shm siblings; one that never existed is
// not an error. The siblings go first: a -wal left beside a fresh database would be
// replayed into it, while a database left without its -wal is only behind.
func deleteFiles(path string) error {
	for _, p := range []string{path + "-wal", path + "-shm", path} {
		if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}

// Retention is what the manager keeps its files to.
func (m *Manager) Retention() Retention { return m.ret }
