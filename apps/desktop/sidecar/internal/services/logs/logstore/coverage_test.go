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

// at is the position of the ord-th line at s seconds past 10:00.
func at(s int64, ord int) Pos { return Pos{TS: second(s), Ord: ord} }

// chunk is a kubelet chunk from from to to.
func chunk(from, to Pos) Chunk { return Chunk{From: from, To: to, Backend: "kubelet"} }

// linked is c continuing from prev.
func linked(c Chunk, prev Pos) Chunk {
	c.Prev = &prev
	return c
}

// kubelet is the one backend of a cluster read through its API server.
var kubelet = []string{"kubelet"}

// Chunks that overlap are one span, and so are two the later one's link joins; a
// chunk with no link opens on a gap.
func TestCoverageJoinsOverlappingAndLinkedChunks(t *testing.T) {
	t.Parallel()
	cov := CoverageOf([]Chunk{
		chunk(at(1, 0), at(3, 0)),
		chunk(at(2, 0), at(5, 0)),
		linked(chunk(at(7, 0), at(8, 1)), at(5, 0)),
		chunk(at(10, 0), at(11, 0)),
	}, kubelet, nil)

	assert.Equal(t, []Span{{at(1, 0), at(8, 1)}, {at(10, 0), at(11, 0)}}, cov.Spans)
	assert.Equal(t, []Gap{
		{Before: new(at(1, 0))},
		{After: new(at(8, 1)), Before: new(at(10, 0))},
		{After: new(at(11, 0))},
	}, cov.Gaps)
}

// A link that names no chunk the file holds starts a span: the chunk it named is
// gone, and its lines with it.
func TestAChunkWhoseLinkNamesAGoneChunkStartsASpan(t *testing.T) {
	t.Parallel()
	cov := CoverageOf([]Chunk{
		chunk(at(1, 0), at(3, 0)),
		linked(chunk(at(7, 0), at(8, 0)), at(5, 0)),
	}, kubelet, nil)

	assert.Equal(t, []Span{{at(1, 0), at(3, 0)}, {at(7, 0), at(8, 0)}}, cov.Spans)
}

// A stream with no chunk is one gap, the whole log.
func TestAStreamWithNoChunkIsOneGap(t *testing.T) {
	t.Parallel()
	cov := CoverageOf(nil, kubelet, nil)

	assert.Empty(t, cov.Spans)
	assert.Equal(t, []Gap{{}}, cov.Gaps)
}

// atBeginning is c marked at the beginning.
func atBeginning(c Chunk) Chunk {
	c.AtBeginning = true
	return c
}

// A gap older than an at-beginning chunk of the last backend is lost, the range before the
// oldest chunk included, and the gaps newer than it are live: a Tail page that
// reached the kubelet's oldest line at 10:30 over an older chunk from 9:55 crosses
// to it.
func TestAGapOlderThanTheLastBackendsBeginningIsLost(t *testing.T) {
	t.Parallel()
	cov := CoverageOf([]Chunk{
		chunk(at(-300, 0), at(300, 0)),
		atBeginning(chunk(at(1800, 0), at(1900, 0))),
		chunk(at(2000, 0), at(2100, 0)),
	}, kubelet, nil)

	var lost []bool
	for _, g := range cov.Gaps {
		lost = append(lost, g.Lost)
	}
	assert.Equal(t, []bool{true, true, false, false}, lost)
}

// A gap older than the beginning of an earlier backend is offered to the next, and lost
// only past the last.
func TestAGapOlderThanAnEarlierBackendsBeginningGoesToTheNext(t *testing.T) {
	t.Parallel()
	archive := chunk(at(0, 0), at(10, 0))
	archive.Backend = "archive"
	chunks := []Chunk{archive, atBeginning(chunk(at(20, 0), at(30, 0)))}

	cov := CoverageOf(chunks, []string{"kubelet", "archive"}, nil)
	assert.Equal(t, []int{1, 1, 0}, tiers(cov), "the kubelet holds nothing before 20")
	assert.False(t, cov.Gaps[0].Lost)

	chunks[0] = atBeginning(archive)
	cov = CoverageOf(chunks, []string{"kubelet", "archive"}, nil)
	assert.Equal(t, []int{2, 1, 0}, tiers(cov))
	assert.True(t, cov.Gaps[0].Lost)
}

// tiers is each gap's tier.
func tiers(cov Coverage) []int {
	var out []int
	for _, g := range cov.Gaps {
		out = append(out, g.Tier)
	}
	return out
}

// The range newer than an at_end chunk is lost, since a finished instance writes no
// more, and every gap is lost in a stream no backend serves.
func TestTheNewestGapIsLostPastAtEnd(t *testing.T) {
	t.Parallel()
	end := chunk(at(0, 0), at(10, 0))
	end.AtEnd = true

	assert.True(t, CoverageOf([]Chunk{end}, kubelet, nil).Gaps[1].Lost)
	assert.False(t, CoverageOf([]Chunk{chunk(at(0, 0), at(10, 0))}, kubelet, nil).Gaps[1].Lost)
	for _, g := range CoverageOf([]Chunk{chunk(at(0, 0), at(10, 0)), chunk(at(20, 0), at(30, 0))}, nil, nil).Gaps {
		assert.True(t, g.Lost)
	}
}

// insertChunk adds c to the stream id in the file behind s, open as c says and stamped
// at its ReadAt (0 for none), and answers it with its row's id.
func insertChunk(t *testing.T, s *Store, id int64, c Chunk) Chunk {
	t.Helper()
	using(t, s, func(f *file) {
		var prevNS, prevOrd any
		if c.Prev != nil {
			prevNS, prevOrd = c.Prev.TS, c.Prev.Ord
		}
		var stamp any
		if c.ReadAt != 0 {
			stamp = c.ReadAt
		}
		res, err := f.db.Exec(`INSERT INTO chunks (stream_id, pattern, from_ns, from_ord, to_ns, to_ord, prev_ns, prev_ord, backend, at_beginning, at_end, open, bytes, read_at_ms)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, id, c.Pattern, c.From.TS, c.From.Ord, c.To.TS, c.To.Ord, prevNS, prevOrd, c.Backend, c.AtBeginning, c.AtEnd, c.Open, c.Bytes, stamp)
		require.NoError(t, err)
		c.ID, err = res.LastInsertId()
		require.NoError(t, err)
	})
	return c
}

// labelled is c filled under pattern.
func labelled(c Chunk, pattern string) Chunk {
	c.Pattern = pattern
	return c
}

// A page for pattern P is covered by the chunks labelled P and the unlabelled ones,
// and treats a chunk labelled Q as a gap; an unfiltered page reads the unlabelled
// ones alone.
func TestAPatternReadsItsOwnAndUnlabelledChunks(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)
	ids, err := s.PutStreams(t.Context(), []Stream{app()})
	require.NoError(t, err)
	plain := insertChunk(t, s, ids[0], chunk(at(0, 0), at(1, 0)))
	p := insertChunk(t, s, ids[0], labelled(chunk(at(2, 0), at(3, 0)), "P"))
	insertChunk(t, s, ids[0], labelled(chunk(at(4, 0), at(5, 0)), "Q"))

	got, err := s.Chunks(t.Context(), ids[0], "P")
	require.NoError(t, err)
	assert.Equal(t, []Chunk{plain, p}, got)
	got, err = s.Chunks(t.Context(), ids[0], "")
	require.NoError(t, err)
	assert.Equal(t, []Chunk{plain}, got)
}

// A running tailer reads on from its start, so every gap past it is the tailer's,
// crossed and never filled, and a gap the start falls inside is cut there: the range
// up to the start is still live.
func TestAGapPastATailersStartIsTheTailers(t *testing.T) {
	t.Parallel()
	start := at(10, 0)
	chunks := []Chunk{chunk(at(0, 0), at(5, 0)), chunk(at(12, 0), at(15, 0)), chunk(at(18, 0), at(20, 0))}

	gaps := CoverageOf(chunks, kubelet, &start).Gaps
	var tailed, newest, atTail []bool
	var edges [][2]*Pos
	for _, g := range gaps {
		tailed, newest, atTail = append(tailed, g.Tailed), append(newest, g.Newest()), append(atTail, g.Tail != nil)
		edges = append(edges, [2]*Pos{g.After, g.Before})
	}
	assert.Equal(t, []bool{false, false, true, true, true}, tailed)
	assert.Equal(t, []bool{false, true, false, false, false}, atTail, "the gap up to the start ends at it")
	assert.Equal(t, []bool{false, true, false, false, true}, newest, "the newest a fill may read, and the newest")
	cut := at(10, 1)
	assert.Equal(t, [2]*Pos{new(at(5, 0)), &cut}, edges[1], "the range up to the start")
	assert.Equal(t, [2]*Pos{&start, new(at(12, 0))}, edges[2], "the range past it")

	for _, g := range CoverageOf(chunks, kubelet, nil).Gaps {
		assert.False(t, g.Tailed, "with no tailer")
		assert.Nil(t, g.Tail)
	}
}

// A tailer that has not committed yet leaves the range up to its start live, past the
// newest chunk or in a stream with none.
func TestTheNewestGapIsTheTailersPastItsStartAlone(t *testing.T) {
	t.Parallel()
	start := at(10, 0)
	for _, chunks := range [][]Chunk{{chunk(at(0, 0), at(5, 0))}, nil} {
		gaps := CoverageOf(chunks, kubelet, &start).Gaps
		below, past := gaps[len(gaps)-2], gaps[len(gaps)-1]
		assert.True(t, below.Live(), "up to the start")
		assert.Equal(t, new(at(10, 1)), below.Before)
		assert.True(t, past.Tailed, "past the start")
		assert.Nil(t, past.Before)
	}

	newest := at(5, 0)
	gaps := CoverageOf([]Chunk{chunk(at(0, 0), newest)}, kubelet, &newest).Gaps
	assert.True(t, gaps[1].Tailed, "a tailer starting at the newest line holds the rest")
}

// A Head bound that reads none of its moment sits at the moment's last ordinal, so
// a tailer starting there leaves every line of the moment below its start, and the
// range up to the start takes them in.
func TestATailerStartingAtAMomentsEndLeavesTheMomentBelow(t *testing.T) {
	t.Parallel()
	start := at(10, math.MaxInt)
	gaps := CoverageOf([]Chunk{chunk(at(0, 0), at(5, 0))}, kubelet, &start).Gaps
	require.Len(t, gaps, 3)
	assert.Positive(t, gaps[1].Before.Compare(at(10, 7)), "a line of the start's moment is below the range's end")
	assert.Equal(t, Pos{second(10) + 1, 0}, *gaps[1].Before)
	assert.Equal(t, start, gaps[1].Before.Prev(), "the step back from the moment after lands at the start")
}
