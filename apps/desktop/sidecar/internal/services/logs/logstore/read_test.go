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

// A walk reads a stream's lines past one position and up to another, oldest first
// when it walks forward and newest first when it walks back, and stops when told.
func TestAReadWalksLinesInPositionOrder(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)
	id := streamOf(t, s)
	write(t, s, Fill{Stream: id, Backend: "kubelet", Lines: lines(at(1, 0), at(2, 0), at(2, 1), at(3, 0), at(4, 0))})
	walk := func(from, to Pos, n int) []Pos {
		var out []Pos
		require.NoError(t, s.Read(t.Context(), func(r Reader) error {
			return r.Lines(id, from, to, func(l Line) bool {
				out = append(out, l.Pos)
				return len(out) < n
			})
		}))
		return out
	}

	assert.Equal(t, []Pos{at(2, 1), at(3, 0)}, walk(at(2, 0), at(3, 0), 10))
	assert.Equal(t, []Pos{at(3, 0), at(2, 1), at(2, 0)}, walk(at(4, 0), at(2, 0), 10))
	assert.Equal(t, []Pos{at(1, 0), at(2, 0)}, walk(Oldest, Newest, 2))
	assert.Equal(t, []Pos{at(4, 0)}, walk(Newest, Oldest, 1))

	var chunks []Chunk
	require.NoError(t, s.Read(t.Context(), func(r Reader) error {
		var err error
		chunks, err = r.Chunks(id, "")
		return err
	}))
	assert.Len(t, chunks, 1)
}
