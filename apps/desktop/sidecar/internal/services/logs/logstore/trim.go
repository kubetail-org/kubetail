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
	"time"
)

// trimFinished trims the stream id to its finished_at when it has one, and is whether
// it removed anything.
func trimFinished(ctx context.Context, st stmts, id int64) (bool, error) {
	var finished sql.NullInt64
	if err := st.QueryRow(ctx, stmtSelectFinishedAt, id).Scan(&finished); err != nil || !finished.Valid {
		return false, err
	}
	return trim(ctx, st, id, finished.Int64)
}

// trim cuts the stream id to the instance that finished at finishedAt, and is
// whether it removed anything. A container status's times are whole seconds and a
// line's are nanoseconds, so the cut is the end of finishedAt's second: the lines
// past it, which the next instance wrote under a lagging mirror's id, go, and the
// chunks are cut to the last line left. A trim that removed anything clears at_end,
// since the end that read reached was the next instance's.
func trim(ctx context.Context, st stmts, id, finishedAt int64) (bool, error) {
	cut := finishedAt - finishedAt%int64(time.Second) + int64(time.Second) - 1
	removed := int64(0)
	for _, stmt := range []stmtID{stmtTrimLines, stmtTrimChunks, stmtCutChunks} {
		res, err := st.Exec(ctx, stmt, id, cut)
		if err != nil {
			return false, err
		}
		n, err := res.RowsAffected()
		if err != nil { // coverage:ignore the sqlite driver's RowsAffected never fails
			return false, err
		}
		removed += n
	}
	if removed == 0 {
		return false, nil
	}
	_, err := st.Exec(ctx, stmtClearAtEnd, id)
	return true, err
}
