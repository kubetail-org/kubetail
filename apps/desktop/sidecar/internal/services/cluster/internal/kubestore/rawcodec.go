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
	"compress/zlib"
	"errors"
	"io"
	"sync"
)

// raw_json blobs are the cache's largest column and compress ~4-5x. zlib not gzip: its
// output is deterministic (gzip embeds an mtime) and its frame is ~6 bytes vs ~18. No
// version prefix: a zlib stream starts 0x78 and plain JSON '{' (0x7B), so the format is
// self-identifying and a version scheme can be retrofitted without a migration.

// Both codecs are per-object hot paths, and a flate writer carries a ~325KB window
// (reader ~40KB), so they're pooled. Both are Reset onto the call's buffers, so a
// pooled instance carries no state between calls.
var (
	zlibWriters = sync.Pool{New: func() any { return zlib.NewWriter(io.Discard) }}
	zlibReaders sync.Pool
)

// compressRaw zlib-compresses b; the result begins with the 0x78 header byte.
func compressRaw(b []byte) ([]byte, error) {
	var buf bytes.Buffer
	w := zlibWriters.Get().(*zlib.Writer)
	defer zlibWriters.Put(w)
	w.Reset(&buf)
	if _, err := w.Write(b); err != nil { // coverage:ignore writing into a bytes.Buffer cannot fail
		return nil, err
	}
	// Close flushes; the writer stays reusable through Reset.
	if err := w.Close(); err != nil { // coverage:ignore writing into a bytes.Buffer cannot fail
		return nil, err
	}
	return buf.Bytes(), nil
}

// errRawTooLarge is a stream that inflates past the limit it was read under.
var errRawTooLarge = errors.New("decompressed body past the limit")

// decompressRaw reverses compressRaw, erroring if b is not a valid zlib stream.
func decompressRaw(b []byte) ([]byte, error) {
	return decompressRawUpTo(b, -1)
}

// decompressRawUpTo is decompressRaw reading at most limit bytes, errRawTooLarge past
// them; a negative limit reads it all. A small stream can inflate to gigabytes, so a
// caller handed a blob it did not write reads it under a limit.
func decompressRawUpTo(b []byte, limit int) ([]byte, error) {
	src := bytes.NewReader(b)
	r, _ := zlibReaders.Get().(io.ReadCloser)
	if r == nil {
		var err error
		if r, err = zlib.NewReader(src); err != nil {
			return nil, err
		}
	} else if err := r.(zlib.Resetter).Reset(src, nil); err != nil {
		return nil, err
	}
	var in io.Reader = r
	if limit >= 0 {
		in = io.LimitReader(r, int64(limit)+1)
	}
	// A malformed or oversized stream's reader is dropped rather than pooled.
	out, err := io.ReadAll(in)
	if err != nil {
		return nil, err
	}
	if limit >= 0 && len(out) > limit {
		return nil, errRawTooLarge
	}
	if err := r.Close(); err != nil { // coverage:ignore ReadAll read to the stream's end without error, which leaves Close nothing to report
		return nil, err
	}
	zlibReaders.Put(r)
	return out, nil
}
