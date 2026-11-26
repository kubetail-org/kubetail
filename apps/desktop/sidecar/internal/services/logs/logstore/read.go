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
	"math"
)

// Oldest and Newest stand before and after every line of a stream.
var (
	Oldest = Pos{TS: math.MinInt64, Ord: math.MinInt}
	Newest = Pos{TS: math.MaxInt64, Ord: math.MaxInt}
)

// Read runs fn over one snapshot of the file, so the chunks it reads and the lines
// they claim agree.
func (s *Store) Read(ctx context.Context, fn func(Reader) error) error {
	err := s.inReadTx(ctx, func(ctx context.Context, st stmts) error {
		return fn(Reader{ctx, st})
	})
	if err != nil {
		return fmt.Errorf("read: %w", err)
	}
	return nil
}

// Reader reads one snapshot of the file.
type Reader struct {
	ctx context.Context
	st  stmts
}

// Chunks is the stream id's chunks that cover pattern, as Store.Chunks reads them.
func (r Reader) Chunks(id int64, pattern string) ([]Chunk, error) {
	return queryChunks(r.ctx, r.st, stmtSelectChunks, id, pattern)
}

// Lines hands each of the stream id's lines past from and up to to, from excluded
// and to kept, to each in the order a walk from one to the other meets them: oldest
// first when from is the older, newest first when it is the newer. It is one walk
// of the position index, and ends once each answers false.
func (r Reader) Lines(id int64, from, to Pos, each func(Line) bool) error {
	stmt := stmtWalkForward
	if from.Compare(to) > 0 {
		stmt = stmtWalkBackward
	}
	rows, err := r.st.Query(r.ctx, stmt, id, from.TS, from.Ord, to.TS, to.Ord)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var l Line
		if err := rows.Scan(&l.TS, &l.Ord, &l.Message); err != nil { // coverage:ignore a STRICT row always scans into these fields
			return err
		}
		if !each(l) {
			return nil
		}
	}
	return rows.Err()
}
