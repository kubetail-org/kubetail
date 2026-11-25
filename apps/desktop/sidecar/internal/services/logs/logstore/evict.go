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
)

// Evict deletes the chunk id and the lines it claims that no other chunk does, in
// one transaction, so a reader never sees a chunk without its lines. The chunk's
// bounds are read in the same transaction, since a fill may have extended it since
// the caller looked. A chunk linked to it starts a span from then on, since the lines
// before it are gone. A chunk a tailer holds open is left as it is: the tail writes
// it back from what it holds of it. So is one stamped at or past cutoff, in unix
// milliseconds: a page may have read it since the caller picked it.
func (s *Store) Evict(ctx context.Context, id, cutoff int64) error {
	err := s.operate(ctx, func(ctx context.Context, f *file) error {
		_, err := f.evict(ctx, id, cutoff)
		return err
	})
	if err != nil {
		return fmt.Errorf("evict: %w", err)
	}
	return nil
}

// evict is Evict on f, answering whether it took the chunk.
func (f *file) evict(ctx context.Context, id, cutoff int64) (bool, error) {
	evicted := false
	err := f.set.InTx(ctx, func(st stmts) error {
		var stream int64
		var c Chunk
		err := st.QueryRow(ctx, stmtSelectChunkBounds, id, cutoff).Scan(&stream, &c.From.TS, &c.From.Ord, &c.To.TS, &c.To.Ord)
		switch {
		case errors.Is(err, sql.ErrNoRows):
			return nil
		case err != nil:
			return err
		}
		ranges, err := unclaimed(ctx, st, stream, id, c)
		if err != nil { // coverage:ignore unclaimed reads the chunks the bounds' select just read in this transaction
			return err
		}
		for _, r := range ranges {
			if _, err := st.Exec(ctx, stmtEvictLines, stream, r.From.TS, r.From.Ord, r.To.TS, r.To.Ord); err != nil {
				return err
			}
		}
		if _, err = st.Exec(ctx, stmtEvictChunk, id); err != nil {
			return err
		}
		evicted = true
		return nil
	})
	return evicted, err
}

// unclaimed is the ranges of c, the chunk id of stream, that no other chunk claims,
// oldest first. Each is one range delete on the position index; asking per line
// whether another chunk claims it costs half a second on a chunk of short lines.
func unclaimed(ctx context.Context, st stmts, stream, id int64, c Chunk) ([]Span, error) {
	rows, err := st.Query(ctx, stmtSelectOverlapping, stream, c.From.TS, c.From.Ord, c.To.TS, c.To.Ord, id)
	if err != nil { // coverage:ignore the bounds' select just read chunks in this transaction
		return nil, err
	}
	defer rows.Close()
	var out []Span
	from := c.From
	for rows.Next() {
		var o Span
		if err := rows.Scan(&o.From.TS, &o.From.Ord, &o.To.TS, &o.To.Ord); err != nil { // coverage:ignore a STRICT row always scans into these fields
			return nil, err
		}
		if o.From.Compare(from) > 0 {
			out = append(out, Span{from, o.From.Prev()})
		}
		from = MaxPos(from, o.To.Next())
	}
	if err := rows.Err(); err != nil { // coverage:ignore only an I/O error mid-walk reaches it
		return nil, err
	}
	if from.Compare(c.To) <= 0 {
		out = append(out, Span{from, c.To})
	}
	return out, nil
}
