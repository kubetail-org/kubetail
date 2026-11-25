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
)

// halfPast is the ord-th line half a second past s seconds after 10:00.
func halfPast(s int64, ord int) Pos { return Pos{TS: second(s) + 500_000_000, Ord: ord} }

// When a resolve first sets finished_at, the stream loses its lines past the end of
// that whole second, which another instance wrote, keeps its own inside it, has its
// chunks cut there and is no longer at_end, since the end that read reached was the
// next instance's.
func TestATrimCutsAtTheEndOfFinishedAtsSecond(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)
	id := streamOf(t, s)
	write(t, s, Fill{Stream: id, Backend: "kubelet", Lines: lines(at(1, 0), halfPast(5, 0), at(6, 0)), AtEnd: true},
		Fill{Stream: id, Backend: "kubelet", Lines: lines(at(8, 0), at(9, 0))})

	done := app()
	done.FinishedAt = second(5)
	_, err := s.PutStreams(t.Context(), []Stream{done})
	require.NoError(t, err)

	assert.Equal(t, []Pos{at(1, 0), halfPast(5, 0)}, positions(t, s, id))
	assert.Equal(t, []Chunk{chunk(at(1, 0), halfPast(5, 0))}, chunksOfStream(t, s, id))
}

// A trim that removes nothing leaves at_end alone.
func TestATrimThatRemovesNothingKeepsAtEnd(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)
	id := streamOf(t, s)
	write(t, s, Fill{Stream: id, Backend: "kubelet", Lines: lines(at(1, 0), halfPast(5, 0)), AtEnd: true})

	done := app()
	done.FinishedAt = second(5)
	_, err := s.PutStreams(t.Context(), []Stream{done})
	require.NoError(t, err)

	end := chunk(at(1, 0), halfPast(5, 0))
	end.AtEnd = true
	assert.Equal(t, []Chunk{end}, chunksOfStream(t, s, id))
}

// A write to a finished stream is trimmed after it, since a read the instance check
// passed on a lagging mirror can still have been served the next instance's log.
func TestAWriteToAFinishedStreamIsTrimmed(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)
	done := app()
	done.FinishedAt = second(5)
	ids, err := s.PutStreams(t.Context(), []Stream{done})
	require.NoError(t, err)

	write(t, s, Fill{Stream: ids[0], Backend: "kubelet", Lines: lines(at(4, 0), at(7, 0)), AtEnd: true})

	assert.Equal(t, []Pos{at(4, 0)}, positions(t, s, ids[0]))
	assert.Equal(t, []Chunk{chunk(at(4, 0), at(4, 0))}, chunksOfStream(t, s, ids[0]))
}

// Finishing a stream the file holds sets its finished_at and trims it, as a resolve
// that files it does; one the file lacks stays absent, and one already finished keeps
// its time.
func TestFinishStreamsTrimsWhatTheFileHolds(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)
	id := streamOf(t, s)
	write(t, s, Fill{Stream: id, Backend: "kubelet", Lines: lines(at(1, 0), at(6, 0))})
	done, absent := app(), app()
	done.FinishedAt = second(5)
	absent.ContainerID, absent.FinishedAt = "containerd://9", second(5)

	require.NoError(t, s.FinishStreams(t.Context(), []Stream{done, absent}))

	assert.Equal(t, []Pos{at(1, 0)}, positions(t, s, id))
	rows, err := s.PodStreams(t.Context(), "web", "api-7f9c", "")
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, second(5), rows[0].FinishedAt)

	later := app()
	later.FinishedAt = second(9)
	require.NoError(t, s.FinishStreams(t.Context(), []Stream{later}))
	rows, err = s.PodStreams(t.Context(), "web", "api-7f9c", "")
	require.NoError(t, err)
	assert.Equal(t, second(5), rows[0].FinishedAt)
}
