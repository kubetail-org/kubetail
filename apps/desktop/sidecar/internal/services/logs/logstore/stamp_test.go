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
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A page stamps the chunks it read with the manager's clock. A chunk stamped within a
// tenth of ReadTTL is left as it is, so a viewer sitting on a chunk costs one write
// every tenth of ReadTTL, not one a page; a chunk the page did not name is left too.
func TestAPageStampsWhatItRead(t *testing.T) {
	t.Parallel()
	c := newClock(1_000)
	ret := testRetention
	ret.ReadTTL = 10 * time.Second
	s := newClockedStore(t, ret, c)
	id := streamOf(t, s)
	tail := openTail(t, s, id, nil, false)
	big := Line{Pos: at(1, 0), Message: string(make([]byte, ret.ChunkBytes))}
	require.NoError(t, tail.Close(t.Context(), []Line{big, {Pos: at(2, 0), Message: "m"}}))
	read := func() []Chunk {
		chunks, err := s.Chunks(t.Context(), id, "")
		require.NoError(t, err)
		require.Len(t, chunks, 2)
		return chunks[:1]
	}

	c.set(5_000)
	require.NoError(t, s.Stamp(t.Context(), read()))
	assert.Equal(t, []*int64{new(int64(5_000)), nil}, stamps(t, s, id), "a chunk no page had read, and only the one named")

	c.set(5_999)
	require.NoError(t, s.Stamp(t.Context(), read()))
	assert.Equal(t, []*int64{new(int64(5_000)), nil}, stamps(t, s, id), "within a tenth of ReadTTL")

	c.set(6_000)
	require.NoError(t, s.Stamp(t.Context(), read()))
	assert.Equal(t, []*int64{new(int64(6_000)), nil}, stamps(t, s, id), "a tenth of ReadTTL on")
}

// A stamp the file refuses fails, saying so; naming no chunk writes nothing.
func TestAStampTheFileRefusesFails(t *testing.T) {
	t.Parallel()
	c := newClock(1_000)
	s := newClockedStore(t, testRetention, c)
	id := streamOf(t, s)
	write(t, s, Fill{Stream: id, Backend: "kubelet", Lines: lines(at(1, 0))})
	c.set(1_000 + testRetention.ReadTTL.Milliseconds())
	chunks, err := s.Chunks(t.Context(), id, "")
	require.NoError(t, err)
	refuse(t, s, "UPDATE", "chunks")

	require.NoError(t, s.Stamp(t.Context(), nil))
	err = s.Stamp(t.Context(), chunks)
	assert.ErrorContains(t, err, "stamp: ")
	assert.ErrorContains(t, err, "refused")
}
