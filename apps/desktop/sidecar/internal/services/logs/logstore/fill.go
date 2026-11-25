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
	"database/sql"
	"errors"
	"fmt"
	"slices"
)

// Line is one line of a stream: its position and its message, cleaned.
type Line struct {
	Pos
	Message string
}

// Fill is one backend read of one gap of a stream, buffered for Write: the lines it
// returned, oldest first, all inside the gap, and what the read proved about the range
// around them.
type Fill struct {
	Stream int64
	// Pattern is the grep the backend read under, "" for every line, and Backend the
	// backend, by name.
	Pattern string
	Backend string
	Lines   []Line
	// Prev is the last line of the chunk the gap starts at, when the read began there:
	// the backend holds nothing between it and the first line, so the first chunk
	// continues from it. Merge is the read returning Prev's moment whole, so the first
	// chunk extends the one ending at Prev.
	Prev  *Pos
	Merge bool
	// Next is the first line of the chunk the gap ends at, when the read served every
	// line up to it: that chunk then continues from the last line, or from Prev when the
	// read returned none.
	Next *Pos
	// AtBeginning is the read reaching the backend's oldest line, which marks the oldest chunk
	// it wrote, or Next's with none. AtEnd is it reaching a finished instance's last
	// line, which marks the newest chunk it wrote, or Prev's with none.
	AtBeginning bool
	AtEnd       bool
}

// Written is what a Write did: the chunks it wrote or extended, and the streams
// whose fills it skipped because their row was gone.
type Written struct {
	Chunks  []int64
	Skipped []int64
}

// Write files every fill in one transaction, so a reader sees a fill's lines and the
// chunks claiming them together or not at all. A line two fills found is stored once.
// A stream that has finished is trimmed after its fills. A fill whose stream row is
// gone is skipped, not failed: the janitor can remove a row between a lookup and its
// hold, and that must not take every other stream's fill down with it. With stamp,
// a page's write, every chunk written or extended is stamped read now in the same
// transaction, since a sweep before the page's walk would otherwise take what it
// fetched. Each stream a fill wrote lines to is pinged once the transaction commits.
func (s *Store) Write(ctx context.Context, fills []Fill, stamp bool) (Written, error) {
	w, _, err := s.write(ctx, fills, stamp, false)
	return w, err
}

// WriteHeld is an unstamped Write, a follow's, whose chunks are held from their commit
// until release, which the caller owes once it has read them; on an error nothing is
// held.
func (s *Store) WriteHeld(ctx context.Context, fills []Fill) (Written, func(), error) {
	return s.write(ctx, fills, false, true)
}

// write is Write, holding what it wrote when hold is set.
func (s *Store) write(ctx context.Context, fills []Fill, stamp, hold bool) (Written, func(), error) {
	var w Written
	var kept []Fill
	var release func() // nil unless hold, which sets it on success
	err := s.operate(ctx, func(ctx context.Context, f *file) error {
		if hold {
			// From before the transaction until the hold is taken: a sweep evicts under
			// the same lock, so it lands wholly before the write or after the hold.
			f.heldChunks.mu.Lock()
			defer f.heldChunks.mu.Unlock()
		}
		err := f.set.InTx(ctx, func(st stmts) error {
			var err error
			if kept, w.Skipped, err = fillsWithRows(ctx, st, fills); err != nil {
				return err
			}
			if w.Chunks, err = writeFills(ctx, st, s.m.ret, kept); err != nil {
				return err
			}
			if !stamp || len(w.Chunks) == 0 {
				return nil
			}
			_, err = st.Exec(ctx, stmtStampChunks, s.m.now().UnixMilli(), idList(w.Chunks), 0)
			return err
		})
		if err != nil {
			return err
		}
		f.committed(streamsWithLines(kept, func(f Fill) (int64, int) { return f.Stream, len(f.Lines) }),
			linesBytes(kept, func(f Fill) []Line { return f.Lines }))
		if hold {
			release = f.heldChunks.holdLocked(w.Chunks)
		}
		return nil
	})
	if err != nil {
		return Written{}, func() {}, fmt.Errorf("write: %w", err)
	}
	return w, release, nil
}

// fillsWithRows is the fills whose stream row the file holds, and the streams of
// the rest.
func fillsWithRows(ctx context.Context, st stmts, fills []Fill) (kept []Fill, gone []int64, err error) {
	for _, id := range streamsOf(fills) {
		var one int
		err := st.QueryRow(ctx, stmtStreamExists, id).Scan(&one)
		if errors.Is(err, sql.ErrNoRows) {
			gone = append(gone, id)
		} else if err != nil {
			return nil, nil, err
		}
	}
	kept = slices.DeleteFunc(slices.Clone(fills), func(f Fill) bool { return slices.Contains(gone, f.Stream) })
	return kept, gone, nil
}

// writeFills files every fill, then trims each stream they wrote that has finished,
// and answers the chunks it wrote or extended.
func writeFills(ctx context.Context, st stmts, ret Retention, fills []Fill) ([]int64, error) {
	var chunks []int64
	for _, f := range fills {
		ids, err := writeFill(ctx, st, ret, f)
		if err != nil {
			return nil, err
		}
		chunks = append(chunks, ids...)
	}
	for _, id := range streamsOf(fills) {
		if _, err := trimFinished(ctx, st, id); err != nil {
			return nil, err
		}
	}
	return distinct(chunks), nil
}

// writeFill files f's lines, the chunks claiming them, and the links and marks its
// read proved, and answers the chunks it wrote.
func writeFill(ctx context.Context, st stmts, ret Retention, f Fill) ([]int64, error) {
	if err := insertLines(ctx, st, f.Stream, f.Lines); err != nil {
		return nil, err
	}
	var ids []int64
	last := f.Prev
	if len(f.Lines) > 0 {
		chunks, err := chunksFor(ctx, st, ret, f)
		if err != nil {
			return nil, err
		}
		for _, c := range chunks {
			c, err := putChunk(ctx, st, f.Stream, c)
			if err != nil {
				return nil, err
			}
			ids = append(ids, c.ID)
		}
		last = &f.Lines[len(f.Lines)-1].Pos
	} else {
		if f.AtBeginning && f.Next != nil {
			if _, err := st.Exec(ctx, stmtMarkAtBeginning, f.Stream, f.Pattern, f.Backend, f.Next.TS, f.Next.Ord); err != nil {
				return nil, err
			}
		}
		if f.AtEnd && f.Prev != nil {
			if _, err := st.Exec(ctx, stmtMarkAtEnd, f.Stream, f.Pattern, f.Backend, f.Prev.TS, f.Prev.Ord); err != nil {
				return nil, err
			}
		}
	}
	if f.Next != nil && last != nil {
		_, err := st.Exec(ctx, stmtLinkChunk, f.Stream, f.Pattern, f.Next.TS, f.Next.Ord, last.TS, last.Ord)
		return ids, err
	}
	return ids, nil
}

// insertLines stores lines under the stream id; a line already stored is kept once.
func insertLines(ctx context.Context, st stmts, stream int64, lines []Line) error {
	for _, l := range lines {
		if _, err := st.Exec(ctx, stmtInsertLine, stream, l.TS, l.Ord, l.Message); err != nil {
			return err
		}
	}
	return nil
}

// chunksFor is f's lines cut into chunks at ret's ChunkBytes and ChunkSpan, each linked
// to the one before it: the first continuing from Prev, and extending the chunk that ends
// there when f merges and that chunk has room.
func chunksFor(ctx context.Context, st stmts, ret Retention, f Fill) ([]Chunk, error) {
	cur, fresh, err := startChunk(ctx, st, ret, f.Stream, f.Pattern, f.Backend, f.Prev, f.Merge, f.Lines[0])
	if err != nil {
		return nil, err
	}
	if fresh {
		cur.AtBeginning = f.AtBeginning
	}
	out := cutChunks(ret, cur, fresh, f.Lines)
	out[len(out)-1].AtEnd = f.AtEnd
	return out, nil
}

// startChunk is the chunk a run of lines from first goes in: the closed chunk of
// pattern and backend ending at prev when the run merges and that chunk has room, else
// a fresh one linked to prev, which fresh says.
func startChunk(ctx context.Context, st stmts, ret Retention, stream int64, pattern, backend string, prev *Pos, merge bool, first Line) (Chunk, bool, error) {
	if merge && prev != nil {
		c, ok, err := chunkEndingAt(ctx, st, stream, pattern, backend, *prev)
		if err != nil || ok && !full(ret, c, first.TS) {
			return c, false, err
		}
	}
	return Chunk{Pattern: pattern, Backend: backend, From: first.Pos, Prev: prev}, true, nil
}

// cutChunks is lines added to cur, fresh when it holds none yet, cut at ret's
// ChunkBytes and ChunkSpan: each next chunk opens at the line after, linked to the one
// before. The last chunk is the one the next line would go in.
func cutChunks(ret Retention, cur Chunk, fresh bool, lines []Line) []Chunk {
	var out []Chunk
	for _, l := range lines {
		if !fresh && full(ret, cur, l.TS) {
			out = append(out, cur)
			prev := cur.To
			cur = Chunk{Pattern: cur.Pattern, Backend: cur.Backend, From: l.Pos, Prev: &prev}
		}
		// A line at or before the end is one the row already holds, a fill's that
		// merged into it; one past the end moves it off the instance's last line.
		if fresh || l.Pos.Compare(cur.To) > 0 {
			cur.To, cur.AtEnd = l.Pos, false
			cur.Bytes += int64(len(l.Message))
		}
		fresh = false
	}
	return append(out, cur)
}

// full is whether c is closed to a line at ts: it holds ChunkBytes, or ts is ChunkSpan
// past its first line.
func full(ret Retention, c Chunk, ts int64) bool {
	return c.Bytes >= ret.ChunkBytes || ts-c.From.TS >= int64(ret.ChunkSpan)
}

// chunkEndingAt is the stream's closed chunk of pattern and backend whose last line is
// at, false with none.
func chunkEndingAt(ctx context.Context, st stmts, stream int64, pattern, backend string, at Pos) (Chunk, bool, error) {
	chunks, err := queryChunks(ctx, st, stmtSelectChunkEndingAt, stream, pattern, backend, at.TS, at.Ord)
	if err != nil || len(chunks) == 0 {
		return Chunk{}, false, err
	}
	return chunks[0], true, nil
}

// putChunk writes c and answers its row as it now stands: a new row, or the row of
// its pattern that already starts at its first line, which the write merges into. It
// never touches read_at_ms: a stamp is a page's, and Write sets it apart.
func putChunk(ctx context.Context, st stmts, stream int64, c Chunk) (Chunk, error) {
	var prevNS, prevOrd any
	if c.Prev != nil {
		prevNS, prevOrd = c.Prev.TS, c.Prev.Ord
	}
	err := st.QueryRow(ctx, stmtInsertChunk, stream, c.Pattern, c.From.TS, c.From.Ord, c.To.TS, c.To.Ord,
		prevNS, prevOrd, c.Backend, c.AtBeginning, c.AtEnd, c.Open, c.Bytes).
		Scan(&c.ID, &c.To.TS, &c.To.Ord, &c.AtEnd, &c.Open, &c.Bytes)
	return c, err
}

// messageBytes is the text of lines, the measure a chunk's bytes and SweepBytes count.
func messageBytes(lines []Line) int64 {
	var n int64
	for _, l := range lines {
		n += int64(len(l.Message))
	}
	return n
}

// linesBytes is the text of every item's lines.
func linesBytes[T any](items []T, lines func(T) []Line) int64 {
	var n int64
	for _, item := range items {
		n += messageBytes(lines(item))
	}
	return n
}

// streamsWithLines is each stream the items write a line to, once, in order: of
// answers an item's stream and how many lines it writes.
func streamsWithLines[T any](items []T, of func(T) (stream int64, lines int)) []int64 {
	var ids []int64
	for _, item := range items {
		if stream, lines := of(item); lines > 0 {
			ids = append(ids, stream)
		}
	}
	return distinct(ids)
}

// streamsOf is each stream fills write to, once.
func streamsOf(fills []Fill) []int64 {
	ids := make([]int64, len(fills))
	for i, f := range fills {
		ids[i] = f.Stream
	}
	return distinct(ids)
}

// distinct is ids with each repeat dropped, in order.
func distinct(ids []int64) []int64 {
	var out []int64
	for _, id := range ids {
		if !slices.Contains(out, id) {
			out = append(out, id)
		}
	}
	return out
}
