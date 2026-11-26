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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/testutil"
)

// lines is one line at each position, saying "m".
func lines(at ...Pos) []Line {
	out := make([]Line, len(at))
	for i, p := range at {
		out[i] = Line{Pos: p, Message: "m"}
	}
	return out
}

// streamOf files app in s and answers its id.
func streamOf(t *testing.T, s *Store) int64 {
	t.Helper()
	ids, err := s.PutStreams(t.Context(), []Stream{app()})
	require.NoError(t, err)
	return ids[0]
}

// write writes fills stamped, as a page does, failing the test on an error.
func write(t *testing.T, s *Store, fills ...Fill) {
	t.Helper()
	_, err := s.Write(t.Context(), fills, true)
	require.NoError(t, err)
}

// positions is every line the stream id holds, oldest first.
func positions(t *testing.T, s *Store, id int64) []Pos {
	t.Helper()
	var out []Pos
	using(t, s, func(f *file) {
		rows, err := f.readDB.Query(`SELECT ts_ns, ord FROM lines WHERE stream_id = ? ORDER BY ts_ns, ord`, id)
		require.NoError(t, err)
		defer rows.Close()
		for rows.Next() {
			var p Pos
			require.NoError(t, rows.Scan(&p.TS, &p.Ord))
			out = append(out, p)
		}
		require.NoError(t, rows.Err())
	})
	return out
}

// chunksOfStream is the stream id's unlabelled chunks.
func chunksOfStream(t *testing.T, s *Store, id int64) []Chunk {
	t.Helper()
	chunks, err := s.Chunks(t.Context(), id, "")
	require.NoError(t, err)
	for i := range chunks {
		chunks[i].ID, chunks[i].Bytes, chunks[i].ReadAt = 0, 0, 0
	}
	return chunks
}

// A fill claims from the first line it returned, whatever it was asked for.
func TestAFillClaimsFromItsFirstLine(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)
	id := streamOf(t, s)

	write(t, s, Fill{Stream: id, Backend: "kubelet", Lines: lines(at(3, 0), at(4, 0), at(5, 1))})

	assert.Equal(t, []Chunk{chunk(at(3, 0), at(5, 1))}, chunksOfStream(t, s, id))
	assert.Equal(t, []Pos{at(3, 0), at(4, 0), at(5, 1)}, positions(t, s, id))
}

// A line two fills found is stored once: its position is its key.
func TestALineFoundTwiceIsStoredOnce(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)
	id := streamOf(t, s)

	write(t, s, Fill{Stream: id, Backend: "kubelet", Lines: lines(at(3, 0), at(4, 0))},
		Fill{Stream: id, Backend: "kubelet", Lines: lines(at(4, 0), at(5, 0))})

	assert.Equal(t, []Pos{at(3, 0), at(4, 0), at(5, 0)}, positions(t, s, id))
}

// A read that began in a chunk's last moment and returned it whole extends that
// chunk; one whose first moment came back cut is a chunk of its own, linked across
// the hole the backend has rotated away, so the two are still one span.
func TestAWholeFillMergesIntoTheChunkItContinues(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)
	id := streamOf(t, s)
	write(t, s, Fill{Stream: id, Backend: "kubelet", Lines: lines(at(1, 0), at(2, 0))})

	write(t, s, Fill{Stream: id, Backend: "kubelet", Lines: lines(at(2, 1), at(3, 0)), Prev: new(at(2, 0)), Merge: true})
	assert.Equal(t, []Chunk{chunk(at(1, 0), at(3, 0))}, chunksOfStream(t, s, id))

	write(t, s, Fill{Stream: id, Backend: "kubelet", Lines: lines(at(9, 0)), Prev: new(at(3, 0))})
	chunks := chunksOfStream(t, s, id)
	assert.Equal(t, []Chunk{chunk(at(1, 0), at(3, 0)), linked(chunk(at(9, 0), at(9, 0)), at(3, 0))}, chunks)
	assert.Len(t, CoverageOf(chunks, kubelet, nil).Spans, 1)
}

// A page's fill was made for a reader that has not read it yet, so a stamped Write
// stamps every chunk it writes or extends with the manager's clock: a sweep before the
// walk would otherwise take exactly what the page just fetched. An unstamped Write, a
// follow's, leaves the stamps as they were. Either answers the chunks it wrote.
func TestAStampedFillStampsWhatItWrites(t *testing.T) {
	t.Parallel()
	c := newClock(5_000)
	s := newClockedStore(t, testRetention, c)
	id := streamOf(t, s)

	w, err := s.Write(t.Context(), []Fill{{Stream: id, Backend: "kubelet", Lines: lines(at(1, 0), at(2, 0))}}, false)
	require.NoError(t, err)
	assert.Equal(t, []*int64{nil}, stamps(t, s, id), "unstamped")
	chunks, err := s.Chunks(t.Context(), id, "")
	require.NoError(t, err)
	assert.Equal(t, []int64{chunks[0].ID}, w.Chunks)

	c.set(9_000)
	write(t, s, Fill{Stream: id, Backend: "kubelet", Lines: lines(at(2, 1)), Prev: new(at(2, 0)), Merge: true})
	assert.Equal(t, []*int64{new(int64(9_000))}, stamps(t, s, id), "the chunk a stamped fill extended")

	c.set(12_000)
	_, err = s.Write(t.Context(), []Fill{{Stream: id, Backend: "kubelet", Lines: lines(at(2, 2)), Prev: new(at(2, 1)), Merge: true}}, false)
	require.NoError(t, err)
	assert.Equal(t, []*int64{new(int64(9_000))}, stamps(t, s, id), "an unstamped fill keeps the row's stamp")
}

// A fill whose stream row is gone is skipped, not failed: the janitor can remove a row
// between a page's lookup and its hold, and that must not take the other streams'
// fills down with it.
func TestAWriteSkipsAFillWhoseRowIsGone(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)
	kept, gone := twoStreams(t, s)
	using(t, s, func(f *file) {
		_, err := f.db.Exec(`DELETE FROM streams WHERE id = ?`, gone)
		require.NoError(t, err)
	})

	w, err := s.Write(t.Context(), []Fill{
		{Stream: gone, Backend: "kubelet", Lines: lines(at(1, 0))},
		{Stream: kept, Backend: "kubelet", Lines: lines(at(2, 0))},
	}, true)

	require.NoError(t, err)
	assert.Equal(t, []int64{gone}, w.Skipped)
	assert.Equal(t, []Pos{at(2, 0)}, positions(t, s, kept))
	assert.Empty(t, positions(t, s, gone))
}

// A held write holds its chunks from their commit: a sweep its commit's ping starts
// leaves them, so a follow's walk reads what its fill fetched, and they go once
// released.
func TestAHeldWriteHoldsFromItsCommit(t *testing.T) {
	t.Parallel()
	ret := sweepRetention()
	ret.Bytes, ret.ReadTTL = 0, 0
	s := newClockedStore(t, ret, newClock(100_000))
	id := streamOf(t, s)
	pings := subscribe(t, s, id)
	swept := make(chan error, 1)
	go func() {
		<-pings
		swept <- s.Sweep(t.Context())
	}()

	w, release, err := s.WriteHeld(t.Context(), []Fill{{Stream: id, Backend: "kubelet", Lines: lines(at(1, 0))}})
	require.NoError(t, err)
	require.NoError(t, testutil.Recv(t, swept, "the sweep the commit started"))
	assert.Equal(t, w.Chunks, idsIn(t, s, "chunks"))

	release()
	sweep(t, s)
	assert.Empty(t, idsIn(t, s, "chunks"))
}

// A held write that fails holds nothing, and its release is still safe to call.
func TestAFailedHeldWriteHoldsNothing(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)
	id := streamOf(t, s)
	refuse(t, s, "INSERT", "lines")

	_, release, err := s.WriteHeld(t.Context(), []Fill{{Stream: id, Backend: "kubelet", Lines: lines(at(1, 0))}})

	assert.ErrorContains(t, err, "refused")
	release()
	using(t, s, func(f *file) { assert.Empty(t, f.heldChunks.n) })
}

// A tailer is the one writer of the chunk it holds open, so a fill that continues it
// is a chunk of its own, linked to it: the two are still one span.
func TestNoFillExtendsAnOpenChunk(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)
	id := streamOf(t, s)
	openChunk(t, s, insertChunk(t, s, id, chunk(at(1, 0), at(2, 0))))

	write(t, s, Fill{Stream: id, Backend: "kubelet", Lines: lines(at(2, 1)), Prev: new(at(2, 0)), Merge: true})

	tail := chunk(at(1, 0), at(2, 0))
	tail.Open = true
	assert.Equal(t, []Chunk{tail, linked(chunk(at(2, 1), at(2, 1)), at(2, 0))}, chunksOfStream(t, s, id))
}

// A fill closes its chunk at ChunkBytes or ChunkSpan and opens the next at the line
// after, linked to it, so the seam is one span and a page over it reads nothing.
func TestAFillClosesItsChunkAtChunkBytesOrChunkSpan(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)
	id := streamOf(t, s)
	big := lines(at(1, 0), at(1, 1), at(2, 0))
	for i := range big {
		big[i].Message = string(make([]byte, testRetention.ChunkBytes/2))
	}

	write(t, s, Fill{Stream: id, Backend: "kubelet", Lines: big})
	write(t, s, Fill{Stream: id, Backend: "kubelet", Lines: lines(at(1000, 0), at(1000+300, 0), at(1000+301, 0))})

	chunks := chunksOfStream(t, s, id)
	assert.Equal(t, []Chunk{
		chunk(at(1, 0), at(1, 1)),
		linked(chunk(at(2, 0), at(2, 0)), at(1, 1)),
		chunk(at(1000, 0), at(1000, 0)),
		linked(chunk(at(1300, 0), at(1301, 0)), at(1000, 0)),
	}, chunks)
	assert.Len(t, CoverageOf(chunks, kubelet, nil).Spans, 2)
}

// A fill that read its whole range up to the next chunk links that chunk to its last
// line, and one that read nothing in its range links the next chunk to the one before:
// either way the seam is not read again.
func TestAFillLinksTheChunkItRanInto(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)
	id := streamOf(t, s)
	write(t, s, Fill{Stream: id, Backend: "kubelet", Lines: lines(at(1, 0))},
		Fill{Stream: id, Backend: "kubelet", Lines: lines(at(5, 0))},
		Fill{Stream: id, Backend: "kubelet", Lines: lines(at(9, 0))})

	write(t, s, Fill{Stream: id, Backend: "kubelet", Lines: lines(at(3, 0)), Next: new(at(5, 0))},
		Fill{Stream: id, Backend: "kubelet", Prev: new(at(5, 0)), Next: new(at(9, 0))})

	chunks := chunksOfStream(t, s, id)
	assert.Equal(t, []Chunk{
		chunk(at(1, 0), at(1, 0)),
		chunk(at(3, 0), at(3, 0)),
		linked(chunk(at(5, 0), at(5, 0)), at(3, 0)),
		linked(chunk(at(9, 0), at(9, 0)), at(5, 0)),
	}, chunks)
	assert.Equal(t, []Span{{at(1, 0), at(1, 0)}, {at(3, 0), at(9, 0)}}, CoverageOf(chunks, kubelet, nil).Spans)
}

// A read that reached the backend's oldest line marks the oldest chunk it wrote
// at_beginning, or the chunk it ran into when it found nothing older; one that reached a
// finished instance's last line marks the newest it wrote at_end, or the chunk it
// started from when it found nothing newer.
func TestAtBeginningAndAtEndAreWritten(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)
	id := streamOf(t, s)
	write(t, s, Fill{Stream: id, Backend: "kubelet", Lines: lines(at(1, 0), at(2, 0)), AtBeginning: true, AtEnd: true})
	end := chunk(at(1, 0), at(2, 0))
	end.AtBeginning, end.AtEnd = true, true
	assert.Equal(t, []Chunk{end}, chunksOfStream(t, s, id))

	other, err := s.PutStreams(t.Context(), []Stream{{Namespace: "web", Pod: "api-7f9c", Container: "app", ContainerID: "containerd://2"}})
	require.NoError(t, err)
	write(t, s, Fill{Stream: other[0], Backend: "kubelet", Lines: lines(at(5, 0))})
	write(t, s, Fill{Stream: other[0], Backend: "kubelet", Next: new(at(5, 0)), AtBeginning: true},
		Fill{Stream: other[0], Backend: "kubelet", Prev: new(at(5, 0)), AtEnd: true})
	end = chunk(at(5, 0), at(5, 0))
	end.AtBeginning, end.AtEnd = true, true
	assert.Equal(t, []Chunk{end}, chunksOfStream(t, s, other[0]))
}

// Two fills of one gap that start at the same line, as two pages with different
// limits write them, leave one chunk claiming the longer extent, in either order.
func TestFillsFromOneLineKeepTheLongerExtent(t *testing.T) {
	t.Parallel()
	for _, longerFirst := range []bool{false, true} {
		s := newTestStore(t)
		id := streamOf(t, s)
		short := Fill{Stream: id, Backend: "kubelet", Lines: lines(at(1, 0), at(2, 0)), AtBeginning: true}
		long := Fill{Stream: id, Backend: "kubelet", Lines: lines(at(1, 0), at(2, 0), at(3, 0)), AtBeginning: true, AtEnd: true}
		if longerFirst {
			write(t, s, long)
			write(t, s, short)
		} else {
			write(t, s, short)
			write(t, s, long)
		}

		want := chunk(at(1, 0), at(3, 0))
		want.AtBeginning, want.AtEnd = true, true
		assert.Equal(t, []Chunk{want}, chunksOfStream(t, s, id), "longer first: %v", longerFirst)
	}
}

// A fill's proof holds for its own pattern alone: a labelled fill links and marks
// its pattern's chunks, never an unlabelled one, which claims lines it did not read.
func TestALabelledFillProvesNothingForOtherPatterns(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)
	id := streamOf(t, s)
	write(t, s, Fill{Stream: id, Backend: "kubelet", Lines: lines(at(1, 0))},
		Fill{Stream: id, Backend: "kubelet", Lines: lines(at(3, 0))})

	write(t, s, Fill{Stream: id, Backend: "kubelet", Pattern: "P", Prev: new(at(1, 0)), Next: new(at(3, 0)), AtBeginning: true, AtEnd: true})
	write(t, s, Fill{Stream: id, Backend: "kubelet", Pattern: "P", Prev: new(at(3, 0)), AtEnd: true})

	assert.Equal(t, []Chunk{chunk(at(1, 0), at(1, 0)), chunk(at(3, 0), at(3, 0))}, chunksOfStream(t, s, id))
}

// A write the file refuses any part of fails whole, leaving no line behind: a line, a
// chunk, a beginning or an end mark, or the trim of a finished stream.
func TestAWriteTheFileRefusesFails(t *testing.T) {
	t.Parallel()
	for name, c := range map[string]struct {
		op, table string
		fill      func(id int64) Fill
	}{
		"a line": {"INSERT", "lines", func(id int64) Fill {
			return Fill{Stream: id, Backend: "kubelet", Lines: lines(at(3, 0))}
		}},
		"a chunk": {"INSERT", "chunks", func(id int64) Fill {
			return Fill{Stream: id, Backend: "kubelet", Lines: lines(at(3, 0))}
		}},
		"a beginning mark": {"UPDATE", "chunks", func(id int64) Fill {
			return Fill{Stream: id, Backend: "kubelet", Next: new(at(1, 0)), AtBeginning: true}
		}},
		"an end mark": {"UPDATE", "chunks", func(id int64) Fill {
			return Fill{Stream: id, Backend: "kubelet", Prev: new(at(2, 0)), AtEnd: true}
		}},
		"the trim": {"DELETE", "lines", func(id int64) Fill {
			return Fill{Stream: id, Backend: "kubelet", Lines: lines(at(7, 0))}
		}},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			s := newTestStore(t)
			done := app()
			done.FinishedAt = second(5)
			ids, err := s.PutStreams(t.Context(), []Stream{done})
			require.NoError(t, err)
			id := ids[0]
			write(t, s, Fill{Stream: id, Backend: "kubelet", Lines: lines(at(1, 0), at(2, 0))})
			refuse(t, s, c.op, c.table)

			_, err = s.Write(t.Context(), []Fill{c.fill(id)}, true)
			assert.ErrorContains(t, err, "write: ")
			assert.ErrorContains(t, err, "refused")
			assert.Equal(t, []Pos{at(1, 0), at(2, 0)}, positions(t, s, id))
		})
	}
}

// A write that cannot read the file fails whole, leaving no line behind: the check
// that a fill's stream row is there, or the chunk a merging fill extends.
func TestAWriteThatCannotReadTheFileFails(t *testing.T) {
	t.Parallel()
	for _, table := range []string{"streams", "chunks"} {
		t.Run(table, func(t *testing.T) {
			t.Parallel()
			s := newTestStore(t)
			id := streamOf(t, s)
			alter(t, s, `DROP TABLE `+table)

			_, err := s.Write(t.Context(), []Fill{{Stream: id, Backend: "kubelet", Lines: lines(at(3, 0)), Prev: new(at(2, 0)), Merge: true}}, true)

			assert.ErrorContains(t, err, "write: ")
			assert.ErrorContains(t, err, "no such table")
			assert.Empty(t, positions(t, s, id))
		})
	}
}
