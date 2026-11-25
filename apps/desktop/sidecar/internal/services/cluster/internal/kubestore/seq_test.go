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

package kubestore

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The counter is the file's, so it needs no initialization at open and nothing between
// goroutines: it starts at 0 with the schema and every taker gets the next number.
func TestNextSeqHandsOutRisingPositions(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	st := openFileOf(t, newTestStore(t)).set.Stmts()

	var got []int64
	for range 3 {
		seq, err := nextSeq(ctx, st)
		require.NoError(t, err)
		got = append(got, seq)
	}

	assert.Equal(t, []int64{1, 2, 3}, got)
}

// The head is the counter itself, not the highest stamp in any table: a reader asks for
// positions above its cursor, and nothing is ever stamped above the number last handed out.
func TestHeadIsTheLastPositionHandedOut(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	st := openFileOf(t, newTestStore(t)).set.Stmts()

	at0, err := head(ctx, st)
	require.NoError(t, err)
	seq, err := nextSeq(ctx, st)
	require.NoError(t, err)
	at1, err := head(ctx, st)
	require.NoError(t, err)

	assert.Zero(t, at0, "a fresh file has handed out nothing")
	assert.Equal(t, seq, at1)
}

// The migration seeds the counter, so a file without one is not a file we wrote. Answering
// 0 would say nothing has ever been written, leaving a reader asking for nothing and
// serving a stale table for as long as it holds the cache open.
func TestAMissingCounterIsAnError(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	st := openFileOf(t, newTestStore(t)).set.Stmts()
	_, err := st.Exec(ctx, stmtDeleteMeta, seqKey)
	require.NoError(t, err)

	_, err = head(ctx, st)

	assert.Error(t, err)
}

// The counter is what every write stamps from, so a file that cannot hand out a position
// fails the write rather than stamping it with a zero every later cursor sits above.
func TestNextSeqReportsACounterItCannotTake(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	dropTable(t, s, "cluster_meta")

	_, err := nextSeq(context.Background(), openFileOf(t, s).set.Stmts())

	assert.Error(t, err)
}

func TestHeadReportsACounterItCannotRead(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	dropTable(t, s, "cluster_meta")

	_, err := head(context.Background(), openFileOf(t, s).set.Stmts())

	assert.Error(t, err)
}
