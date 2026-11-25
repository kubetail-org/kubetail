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
	"cmp"
	"context"
	"database/sql"
	"fmt"
	"math"
	"slices"
)

// Pos is a line's place within its stream: its moment in unix nanoseconds, then its
// ordinal, the backend's number for the line within that moment.
type Pos struct {
	TS  int64
	Ord int
}

// Compare orders p against q.
func (p Pos) Compare(q Pos) int {
	return cmp.Or(cmp.Compare(p.TS, q.TS), cmp.Compare(p.Ord, q.Ord))
}

// Next and Prev are the first place past p and the last before it that a line can
// sit at: an ordinal is never negative, and one at its bound steps the moment, since
// a bound that reads none of its moment sits at math.MaxInt.
func (p Pos) Next() Pos {
	if p.Ord == math.MaxInt {
		return Pos{p.TS + 1, 0}
	}
	return Pos{p.TS, p.Ord + 1}
}

func (p Pos) Prev() Pos {
	if p.Ord <= 0 {
		return Pos{p.TS - 1, math.MaxInt}
	}
	return Pos{p.TS, p.Ord - 1}
}

// Chunk is one chunks row: every line its Backend served for the stream
// from From to To that matches Pattern is in the file. Prev is the position it
// continues from, the last line of the chunk before it with nothing between, nil
// when it opened on a gap. AtBeginning is the backend holding nothing older than From,
// AtEnd To being a finished instance's last line, and Open a tailer still writing
// into it. ReadAt is when a page last read it or a fill wrote it, in unix
// milliseconds, 0 when neither has.
type Chunk struct {
	ID          int64
	Pattern     string
	From        Pos
	To          Pos
	Prev        *Pos
	Backend     string
	AtBeginning bool
	AtEnd       bool
	Open        bool
	Bytes       int64
	ReadAt      int64
}

// Span is a run of chunks with nothing between them: each overlaps the run or
// continues from a line inside it.
type Span struct{ From, To Pos }

// Gap is a range of a stream no span covers, between After and Before, both
// exclusive: After nil for the range older than every span, Before nil for the
// range newer. Tier is the first of the cluster's backends that may hold it, by
// index, and the gap is Lost when none can: older than an at-beginning chunk of the
// last backend, past an at_end chunk, or in a stream no backend serves. Tailed is a gap
// past the start of the tailer running on the stream, to be crossed and never filled:
// the tailer reads on from its start, and writes its first chunk only on its first
// commit. Tail is that start on the gap that ends at it, whose Before is then the
// place just past the start, where the tailer's first chunk may begin.
type Gap struct {
	After  *Pos
	Before *Pos
	Tier   int
	Lost   bool
	Tailed bool
	Tail   *Pos
}

// Live is whether a fill may be offered the gap: it is neither lost nor its tailer's.
func (g Gap) Live() bool { return !g.Lost && !g.Tailed }

// Newest is whether the gap is the newest a fill may read: the range newer than
// every span, or the one up to the tailer's start. No chunk sits at its newer edge.
func (g Gap) Newest() bool { return g.Before == nil || g.Tail != nil }

// Coverage is a stream's chunks for one pattern read as spans, oldest first, and
// the gaps around them, oldest first: one before each span and one after the last,
// the one a tailer's start falls inside cut in two at the start.
type Coverage struct {
	Spans []Span
	Gaps  []Gap
}

// CoverageOf reads chunks, the stream's for one pattern, against backends, the
// cluster's in the order a gap is offered to them by name, none for a stream no
// backend serves, and tail, the start of the tailer running on the stream, nil with
// none.
func CoverageOf(chunks []Chunk, backends []string, tail *Pos) Coverage {
	chunks = slices.SortedFunc(slices.Values(chunks), func(a, b Chunk) int {
		return cmp.Or(a.From.Compare(b.From), a.To.Compare(b.To))
	})
	var cov Coverage
	atEnd := false
	for _, c := range chunks {
		n := len(cov.Spans)
		if n > 0 && joins(cov.Spans[n-1], c) {
			cov.Spans[n-1].To = MaxPos(cov.Spans[n-1].To, c.To)
			atEnd = atEnd || c.AtEnd
			continue
		}
		cov.Spans = append(cov.Spans, Span{c.From, c.To})
		atEnd = c.AtEnd
	}
	for i := range len(cov.Spans) + 1 {
		var g Gap
		if i > 0 {
			g.After = &cov.Spans[i-1].To
		}
		if i < len(cov.Spans) {
			g.Before = &cov.Spans[i].From
			g.Tier = tierOf(chunks, backends, *g.Before)
		} else if atEnd {
			g.Tier = len(backends)
		}
		g.Lost = g.Tier >= len(backends)
		cov.Gaps = append(cov.Gaps, cutAtTail(g, tail)...)
	}
	return cov
}

// cutAtTail is g read against tail, the tailer's start: as it is when it lies below
// the start, tailed when it lies past it, and cut in two when the start falls inside
// it, the range up to the start and the tailer's past it.
func cutAtTail(g Gap, tail *Pos) []Gap {
	switch {
	case tail == nil, g.Before != nil && g.Before.Compare(tail.Next()) <= 0:
		return []Gap{g}
	case g.After != nil && g.After.Compare(*tail) >= 0:
		g.Tailed = true
		return []Gap{g}
	}
	start, cut := *tail, tail.Next()
	below, past := g, g
	below.Before, below.Tail = &cut, &start
	past.After, past.Tailed = &start, true
	return []Gap{below, past}
}

// joins is whether c continues s: it overlaps it, or its link names a line in it.
func joins(s Span, c Chunk) bool {
	if c.From.Compare(s.To) <= 0 {
		return true
	}
	return c.Prev != nil && c.Prev.Compare(s.From) >= 0 && c.Prev.Compare(s.To) <= 0
}

// tierOf is the first backend that may hold lines older than before: a backend with
// an at-beginning chunk starting at or after it holds none.
func tierOf(chunks []Chunk, backends []string, before Pos) int {
	for i, name := range backends {
		if !slices.ContainsFunc(chunks, func(c Chunk) bool {
			return c.AtBeginning && c.Backend == name && c.From.Compare(before) >= 0
		}) {
			return i
		}
	}
	return len(backends)
}

// MaxPos and MinPos are the later and the earlier of a and b.
func MaxPos(a, b Pos) Pos {
	if a.Compare(b) >= 0 {
		return a
	}
	return b
}

func MinPos(a, b Pos) Pos {
	if a.Compare(b) <= 0 {
		return a
	}
	return b
}

// Chunks is the stream id's chunks that cover pattern: its own and the unlabelled
// ones, oldest first.
func (s *Store) Chunks(ctx context.Context, id int64, pattern string) ([]Chunk, error) {
	var out []Chunk
	err := s.with(ctx, func(ctx context.Context, st stmts) error {
		var err error
		out, err = queryChunks(ctx, st, stmtSelectChunks, id, pattern)
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("chunks: %w", err)
	}
	return out, nil
}

// queryChunks is the chunks stmt answers, each row chunkColumns.
func queryChunks(ctx context.Context, st stmts, stmt stmtID, args ...any) ([]Chunk, error) {
	rows, err := st.Query(ctx, stmt, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Chunk
	for rows.Next() {
		var c Chunk
		var prevNS, prevOrd sql.NullInt64
		if err := rows.Scan(&c.ID, &c.Pattern, &c.From.TS, &c.From.Ord, &c.To.TS, &c.To.Ord, &prevNS, &prevOrd, // coverage:ignore a STRICT row always scans into these fields
			&c.Backend, &c.AtBeginning, &c.AtEnd, &c.Open, &c.Bytes, &c.ReadAt); err != nil {
			return nil, err
		}
		if prevNS.Valid {
			c.Prev = &Pos{prevNS.Int64, int(prevOrd.Int64)}
		}
		out = append(out, c)
	}
	return out, rows.Err()
}
