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
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// anyStamp is a cutoff past every stamp, for an eviction that ignores protection.
const anyStamp = math.MaxInt64

// An eviction checks protection again in its own transaction: a chunk a page stamped at
// or past the cutoff since the sweep picked it is left where it is, with its lines.
func TestEvictLeavesAChunkReadSinceItWasPicked(t *testing.T) {
	t.Parallel()
	c := newClock(5_000)
	s := newClockedStore(t, testRetention, c)
	id := streamOf(t, s)
	write(t, s, Fill{Stream: id, Backend: "kubelet", Lines: lines(at(1, 0))})
	chunks, err := s.Chunks(t.Context(), id, "")
	require.NoError(t, err)

	require.NoError(t, s.Evict(t.Context(), chunks[0].ID, 5_000))
	assert.Equal(t, []Pos{at(1, 0)}, positions(t, s, id), "stamped at the cutoff")

	require.NoError(t, s.Evict(t.Context(), chunks[0].ID, 5_001))
	assert.Empty(t, positions(t, s, id), "stamped before it")
}

// Evicting a chunk takes its lines and its row in one transaction and leaves the
// stream's other chunks and lines alone; a chunk linked to it then starts a span.
func TestEvictTakesAChunkAndItsLines(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)
	id := streamOf(t, s)
	write(t, s, Fill{Stream: id, Backend: "kubelet", Lines: lines(at(1, 0), at(2, 0))},
		Fill{Stream: id, Backend: "kubelet", Lines: lines(at(3, 0)), Prev: new(at(2, 0))})
	chunks, err := s.Chunks(t.Context(), id, "")
	require.NoError(t, err)

	require.NoError(t, s.Evict(t.Context(), chunks[0].ID, anyStamp))

	assert.Equal(t, []Pos{at(3, 0)}, positions(t, s, id))
	assert.Equal(t, []Chunk{linked(chunk(at(3, 0), at(3, 0)), at(2, 0))}, chunksOfStream(t, s, id))
	assert.Equal(t, []Span{{at(3, 0), at(3, 0)}}, CoverageOf(chunksOfStream(t, s, id), kubelet, nil).Spans)
}

// Evicting a chunk keeps the lines another chunk still claims: two fills of one
// range can overlap, and the survivor's lines must still be in the file.
func TestEvictKeepsTheLinesAnotherChunkClaims(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)
	id := streamOf(t, s)
	write(t, s, Fill{Stream: id, Backend: "kubelet", Lines: lines(at(1, 0), at(2, 0))},
		Fill{Stream: id, Backend: "kubelet", Lines: lines(at(2, 0), at(3, 0))})
	chunks, err := s.Chunks(t.Context(), id, "")
	require.NoError(t, err)

	require.NoError(t, s.Evict(t.Context(), chunks[0].ID, anyStamp))

	assert.Equal(t, []Pos{at(2, 0), at(3, 0)}, positions(t, s, id))
}

// Evicting a chunk overlapped on both sides by others takes only the lines between
// them, and no line either still claims.
func TestEvictTakesOnlyTheLinesBetweenItsNeighbours(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)
	id := streamOf(t, s)
	write(t, s,
		Fill{Stream: id, Backend: "kubelet", Lines: lines(at(1, 0), at(2, 0), at(3, 0), at(4, 0), at(5, 0))},
		Fill{Stream: id, Backend: "kubelet", Lines: lines(at(0, 0), at(1, 0), at(2, 0))},
		Fill{Stream: id, Backend: "kubelet", Lines: lines(at(4, 0), at(5, 0), at(6, 0))})
	chunks, err := s.Chunks(t.Context(), id, "")
	require.NoError(t, err)
	require.Len(t, chunks, 3)

	require.NoError(t, s.Evict(t.Context(), chunks[1].ID, anyStamp))

	assert.Equal(t, []Pos{at(0, 0), at(1, 0), at(2, 0), at(4, 0), at(5, 0), at(6, 0)}, positions(t, s, id))
}

// An eviction reads the chunk's bounds in its own transaction: a fill that extended
// the chunk since the caller read it is evicted with it, leaving no line unclaimed.
func TestEvictTakesTheChunkAsItIsNow(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)
	id := streamOf(t, s)
	write(t, s, Fill{Stream: id, Backend: "kubelet", Lines: lines(at(1, 0), at(2, 0))})
	chunks, err := s.Chunks(t.Context(), id, "")
	require.NoError(t, err)
	write(t, s, Fill{Stream: id, Backend: "kubelet", Lines: lines(at(3, 0)), Prev: new(at(2, 0)), Merge: true})

	require.NoError(t, s.Evict(t.Context(), chunks[0].ID, anyStamp))

	assert.Empty(t, positions(t, s, id))
	assert.Empty(t, chunksOfStream(t, s, id))
}

// Evicting a chunk the file no longer holds does nothing.
func TestEvictingAGoneChunkDoesNothing(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)
	id := streamOf(t, s)
	write(t, s, Fill{Stream: id, Backend: "kubelet", Lines: lines(at(1, 0))})

	require.NoError(t, s.Evict(t.Context(), 99, anyStamp))

	assert.Equal(t, []Pos{at(1, 0)}, positions(t, s, id))
}

// An eviction whose delete of the lines or of the chunk the file refuses fails,
// leaving the chunk and its lines.
func TestAnEvictionTheFileRefusesFails(t *testing.T) {
	t.Parallel()
	for _, table := range []string{"lines", "chunks"} {
		t.Run(table, func(t *testing.T) {
			t.Parallel()
			s := newTestStore(t)
			id := streamOf(t, s)
			write(t, s, Fill{Stream: id, Backend: "kubelet", Lines: lines(at(1, 0))})
			chunks, err := s.Chunks(t.Context(), id, "")
			require.NoError(t, err)
			refuse(t, s, "DELETE", table)

			err = s.Evict(t.Context(), chunks[0].ID, anyStamp)

			assert.ErrorContains(t, err, "evict: ")
			assert.ErrorContains(t, err, "refused")
			assert.Equal(t, []Pos{at(1, 0)}, positions(t, s, id))
			assert.Len(t, chunksOfStream(t, s, id), 1)
		})
	}
}

// An eviction that cannot read the chunk's bounds fails.
func TestAnEvictionThatCannotReadTheChunkFails(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)
	alter(t, s, `DROP TABLE chunks`)

	err := s.Evict(t.Context(), 1, anyStamp)

	assert.ErrorContains(t, err, "evict: ")
	assert.ErrorContains(t, err, "no such table")
}
