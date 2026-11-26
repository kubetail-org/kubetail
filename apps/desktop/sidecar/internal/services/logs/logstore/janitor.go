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
	"fmt"
	"log/slog"
	"maps"
	"os"
	"slices"
	"time"
)

// sweepPods is the most pods one sweep asks the service about.
const sweepPods = 500

// PodsGone answers which of the pod uids the mirror behind cache cacheID no longer
// holds. It answers none when it cannot tell, since a stream removed is gone for good.
type PodsGone func(ctx context.Context, cacheID int64, uids []string) ([]string, error)

// SetPodsGone is how a sweep learns which pods are gone. Until it is set, no sweep
// removes a stream. Files may be open and sweeping already, so it is read under mu.
func (m *Manager) SetPodsGone(fn PodsGone) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.podsGone = fn
}

// sweepReport is what one sweep did: the chunks' bytes before and after it, how many
// chunks it evicted and streams it removed, whether it stopped over budget with
// nothing left to evict, and whether it handed free pages back.
type sweepReport struct {
	Before, After int64
	Evicted       int
	Removed       int
	Short         bool
	Vacuumed      bool
}

// candidate is a chunk a sweep may evict, and what evicting it frees.
type candidate struct {
	id, stream, bytes int64
}

// startJanitor runs f's janitor until f closes: a sweep every Interval, and one each
// time the commits of lines since the last wake pass SweepBytes. A sweep's own writes
// count nothing, so a sweep never owes the next.
func (m *Manager) startJanitor(cacheID int64, f *file) {
	f.sweepBytes = m.ret.SweepBytes
	f.janitor.Go(func() {
		t := time.NewTicker(m.ret.Interval)
		defer t.Stop()
		for {
			select {
			case <-f.ctx.Done():
				return
			case <-t.C:
			case <-f.wake:
			}
			if _, err := m.sweep(f.ctx, cacheID, f); err != nil && f.ctx.Err() == nil {
				slog.Warn("log store: a sweep failed", "cacheID", cacheID, "err", err)
			}
		}
	})
}

// Sweep runs one sweep of the claim's file, as its janitor does.
func (s *Store) Sweep(ctx context.Context) error {
	err := s.operate(ctx, func(ctx context.Context, f *file) error {
		_, err := s.m.sweep(ctx, s.cacheID, f)
		return err
	})
	if err != nil {
		return fmt.Errorf("sweep: %w", err)
	}
	return nil
}

// sweep closes the chunks whose close failed, evicts f's unprotected chunks until every
// stream is under its share and the file under its budget, removes the empty streams
// of gone pods, and vacuums. Each eviction is its own transaction, so the tailers'
// batches land between them.
func (m *Manager) sweep(ctx context.Context, cacheID int64, f *file) (sweepReport, error) {
	var r sweepReport
	// A close that fails again leaves only its chunk out of eviction, so the sweep goes on.
	if err := f.closeUnclosed(ctx); err != nil && ctx.Err() == nil {
		slog.Warn("log store: a tail's chunk could not be closed", "cacheID", cacheID, "err", err)
	}
	sizes, err := streamBytes(ctx, f)
	if err != nil {
		return r, err
	}
	for _, n := range sizes {
		r.Before += n
	}
	r.After = r.Before
	cutoff := m.now().Add(-m.ret.ReadTTL).UnixMilli()
	evict := func(c candidate) error {
		var evicted bool
		err := f.heldChunks.unlessHeld(c.id, func() (err error) {
			evicted, err = f.evict(ctx, c.id, cutoff)
			return err
		})
		if evicted {
			r.After -= c.bytes
			sizes[c.stream] -= c.bytes
			r.Evicted++
		}
		return err
	}
	var cands []candidate
	if m.over(sizes, r.After) {
		if cands, err = candidates(ctx, f, cutoff); err != nil { // coverage:ignore streamBytes just read chunks; only the file failing between reaches it
			return r, err
		}
	}
	// An over-share stream's chunks go first, then any stream's while the file is over
	// its budget; the candidates are in eviction order within each.
	var rest []candidate
	for _, c := range cands {
		if sizes[c.stream] <= m.ret.StreamShare {
			rest = append(rest, c)
			continue
		}
		if err := evict(c); err != nil {
			return r, err
		}
	}
	for _, c := range rest {
		if r.After <= m.ret.Bytes {
			break
		}
		if err := evict(c); err != nil {
			return r, err
		}
	}
	r.Short = m.over(sizes, r.After)
	if f.short.Swap(r.Short) != r.Short {
		if r.Short {
			slog.Warn("log store: the file is over its budget with nothing to evict", "cacheID", cacheID, "bytes", r.After)
		} else {
			slog.Info("log store: the file is back within its budget", "cacheID", cacheID, "bytes", r.After)
		}
	}
	m.mu.Lock()
	podsGone := m.podsGone
	m.mu.Unlock()
	if podsGone != nil {
		if r.Removed, err = removeStreams(ctx, cacheID, f, podsGone, m.podsPerSweep); err != nil {
			return r, err
		}
	}
	// A chain outlives its pod's last stream until here, which is harmless: only a
	// stream is read.
	if _, err := f.set.Stmts().Exec(ctx, stmtRemoveOrphanOwners); err != nil {
		return r, err
	}
	if r.Vacuumed, err = m.vacuum(ctx, f); err != nil { // coverage:ignore only the file failing between the sweep's last statement and its vacuum reaches it
		return r, err
	}
	slog.Debug("log store: swept", "cacheID", cacheID, "before", r.Before, "after", r.After,
		"evicted", r.Evicted, "removed", r.Removed, "short", r.Short, "vacuumed", r.Vacuumed)
	if m.sweeps != nil {
		m.sweeps(cacheID, r)
	}
	return r, nil
}

// over is whether a file of size bytes, its streams' as sizes, is over its budget or
// has a stream over its share.
func (m *Manager) over(sizes map[int64]int64, size int64) bool {
	if size > m.ret.Bytes {
		return true
	}
	for _, n := range sizes {
		if n > m.ret.StreamShare {
			return true
		}
	}
	return false
}

// removeStreams deletes every stream with no chunks that nothing holds whose pod
// podsGone says is gone, asking about at most limit pods, and answers how many it
// deleted.
func removeStreams(ctx context.Context, cacheID int64, f *file, podsGone PodsGone, limit int) (int, error) {
	empty, err := emptyStreams(ctx, f)
	if err != nil || len(empty) == 0 {
		return 0, err
	}
	uids := f.nextPods(slices.Compact(slices.Sorted(maps.Values(empty))), limit)
	answer, err := podsGone(ctx, cacheID, uids)
	if err != nil {
		return 0, err
	}
	// The delete runs under the holds' lock, so a row is removed before its hold or never.
	h := &f.heldRows
	h.mu.Lock()
	defer h.mu.Unlock()
	var ids []int64
	for id, uid := range empty {
		if slices.Contains(answer, uid) && h.n[id] == 0 {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return 0, nil
	}
	res, err := f.set.Stmts().Exec(ctx, stmtRemoveStreams, idList(ids))
	if err != nil {
		return 0, err
	}
	n, err := res.RowsAffected()
	return int(n), err
}

// nextPods is at most limit of the sorted uids, taking up after the last one the
// previous sweep asked about and wrapping round, so pods that stay never keep the
// rest from being asked about.
func (f *file) nextPods(uids []string, limit int) []string {
	f.podsMu.Lock()
	defer f.podsMu.Unlock()
	i, found := slices.BinarySearch(uids, f.podsAfter)
	if found {
		i++
	}
	next := append(slices.Clone(uids[i:]), uids[:i]...)
	next = next[:min(len(next), limit)]
	f.podsAfter = next[len(next)-1]
	return next
}

// vacuum hands at most VacuumPages free pages back to the filesystem, when the file
// has any, then truncates the write-ahead log when it outweighs the file, as appdb's
// janitor does. It answers whether it vacuumed.
func (m *Manager) vacuum(ctx context.Context, f *file) (bool, error) {
	var free int64
	if err := f.db.QueryRowContext(ctx, `PRAGMA freelist_count`).Scan(&free); err != nil {
		return false, err
	}
	if free > 0 {
		if _, err := f.db.ExecContext(ctx, fmt.Sprintf(`PRAGMA incremental_vacuum(%d)`, min(free, m.ret.VacuumPages))); err != nil { // coverage:ignore freelist_count just ran on the same connection
			return false, err
		}
	}
	if fileSize(f.path+"-wal") > fileSize(f.path) {
		var busy, logPages, moved int64
		if err := f.db.QueryRowContext(ctx, `PRAGMA wal_checkpoint(TRUNCATE)`).Scan(&busy, &logPages, &moved); err != nil { // coverage:ignore a checkpoint other connections hold up answers busy, not an error
			return free > 0, err
		}
	}
	return free > 0, nil
}

// fileSize is the size of the file at path, 0 for one that cannot be read.
func fileSize(path string) int64 {
	info, err := os.Stat(path)
	if err != nil {
		return 0
	}
	return info.Size()
}

// streamBytes is each stream's chunks' bytes summed.
func streamBytes(ctx context.Context, f *file) (map[int64]int64, error) {
	rows, err := f.set.Stmts().Query(ctx, stmtStreamBytes)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]int64{}
	for rows.Next() {
		var id, n int64
		if err := rows.Scan(&id, &n); err != nil { // coverage:ignore a STRICT row always scans into these fields
			return nil, err
		}
		out[id] = n
	}
	return out, rows.Err()
}

// candidates is every chunk neither open nor read since cutoff, in the order a sweep
// evicts them: never-read before read, oldest first within each.
func candidates(ctx context.Context, f *file, cutoff int64) ([]candidate, error) {
	rows, err := f.set.Stmts().Query(ctx, stmtCandidates, cutoff)
	if err != nil { // coverage:ignore streamBytes just read chunks; only the file failing between reaches it
		return nil, err
	}
	defer rows.Close()
	var out []candidate
	for rows.Next() {
		var c candidate
		if err := rows.Scan(&c.id, &c.stream, &c.bytes); err != nil { // coverage:ignore a STRICT row always scans into these fields
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// emptyStreams is each stream with no chunks and a pod uid, by id.
func emptyStreams(ctx context.Context, f *file) (map[int64]string, error) {
	rows, err := f.set.Stmts().Query(ctx, stmtEmptyStreams)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]string{}
	for rows.Next() {
		var id int64
		var uid string
		if err := rows.Scan(&id, &uid); err != nil { // coverage:ignore a STRICT row always scans into these fields
			return nil, err
		}
		out[id] = uid
	}
	return out, rows.Err()
}
