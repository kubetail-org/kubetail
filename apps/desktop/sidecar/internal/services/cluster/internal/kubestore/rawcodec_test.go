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
	"bytes"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCompressRoundTrips(t *testing.T) {
	t.Parallel()

	body := []byte(`{"kind":"Pod","metadata":{"name":"api-0"}}`)

	packed, err := compressRaw(body)
	require.NoError(t, err)
	out, err := decompressRaw(packed)
	require.NoError(t, err)

	assert.Equal(t, body, out)
}

// The stored blob is self-identifying: a zlib stream opens 0x78, plain JSON '{'.
// That is what lets a version prefix be retrofitted without a migration.
func TestCompressWritesAZlibStream(t *testing.T) {
	t.Parallel()

	packed, err := compressRaw([]byte(`{"a":1}`))
	require.NoError(t, err)

	assert.Equal(t, byte(0x78), packed[0])
}

// Both codecs pool their flate machinery, so a reused instance must carry no state
// from the call before it.
func TestCodecsAreReusable(t *testing.T) {
	t.Parallel()

	for _, body := range [][]byte{[]byte(`{"a":1}`), []byte(strings.Repeat("x", 4096)), {}} {
		packed, err := compressRaw(body)
		require.NoError(t, err)
		out, err := decompressRaw(packed)
		require.NoError(t, err)
		assert.True(t, bytes.Equal(body, out))
	}
}

func TestDecompressRejectsGarbage(t *testing.T) {
	t.Parallel()

	_, err := decompressRaw([]byte("not zlib"))

	assert.Error(t, err)
}

// A stored body that is not a zlib stream after all is reported, not returned as itself:
// the format is self-identifying by its first byte, so anything that gets here claiming
// to be compressed and is not is a corrupt row.
func TestDecompressRejectsAStreamThatIsNotZlib(t *testing.T) {
	t.Parallel()

	// A zlib header the inflater accepts, then rubbish — so the failure lands in the read
	// rather than at the header check.
	_, err := decompressRaw([]byte{0x78, 0x01, 0xff, 0xff, 0xff})

	assert.Error(t, err)
}

// The pooled reader is per-goroutine luck: a fresh one takes the NewReader path and a
// reused one takes Reset, and both must reject the same bad stream. Running the good case
// first is what puts a reader in the pool for the second.
func TestDecompressRejectsABadStreamOnAPooledReaderToo(t *testing.T) {
	t.Parallel()

	good, err := compressRaw([]byte(`{"a":1}`))
	require.NoError(t, err)
	round, err := decompressRaw(good)
	require.NoError(t, err)
	require.JSONEq(t, `{"a":1}`, string(round))

	_, err = decompressRaw([]byte("not a stream at all"))

	assert.Error(t, err)
}

// The very first decompress in a process has no pooled reader and builds one, which reads
// the stream's header as it goes. A file whose stored body is not a zlib stream at all
// must be rejected there too, not only on the pooled path.
func TestDecompressRejectsABadStreamWithNoPooledReader(t *testing.T) {
	t.Parallel()

	zlibReaders = sync.Pool{}

	_, err := decompressRaw([]byte("not a stream at all"))

	assert.Error(t, err)
}

// A limited read answers the body at the limit and refuses one a byte past it, without
// inflating the rest.
func TestDecompressRawUpToStopsAtTheLimit(t *testing.T) {
	t.Parallel()

	blob, err := compressRaw([]byte("0123456789"))
	require.NoError(t, err)

	whole, err := decompressRawUpTo(blob, 10)
	require.NoError(t, err)
	assert.Equal(t, "0123456789", string(whole))

	_, err = decompressRawUpTo(blob, 9)
	assert.ErrorIs(t, err, errRawTooLarge)

	again, err := decompressRaw(blob)
	require.NoError(t, err, "a refused read leaves the pool usable")
	assert.Equal(t, "0123456789", string(again))
}
