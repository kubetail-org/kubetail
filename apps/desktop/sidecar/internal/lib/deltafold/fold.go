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

// Package deltafold is one watch's memory: the records it has sent, by key, and how
// a re-read becomes delta frames. A snapshot is every record as Added and one
// Bookmark; a re-read diffs against what was sent. It knows no record type: the
// caller supplies the key, the equality and the frame.
package deltafold

import (
	"context"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/apimeta"
)

// Fold holds what a watch has sent under each key. A record handed to it, and
// anything it references, must not be mutated afterwards: Diff compares the retained
// record against the new one, and a slice edited behind a retained record would make
// the two equal before the compare. Hand it freshly built values on every read.
type Fold[K comparable, T any, F any] struct {
	key   func(T) K
	equal func(T, T) bool
	frame func(apimeta.CollectionWatchFrameType, T) F
	known map[K]T
}

// New builds a fold over key, equal and frame.
func New[K comparable, T any, F any](
	key func(T) K, equal func(T, T) bool, frame func(apimeta.CollectionWatchFrameType, T) F,
) *Fold[K, T, F] {
	return &Fold[K, T, F]{key: key, equal: equal, frame: frame, known: map[K]T{}}
}

// Equal is == for a comparable record.
func Equal[T comparable](a, b T) bool { return a == b }

// Send delivers one frame, reporting false when the consumer is gone. A bare send
// would block forever once a window stops reading.
func Send[F any](ctx context.Context, out chan<- F, frame F) bool {
	select {
	case out <- frame:
		return true
	case <-ctx.Done():
		return false
	}
}

// Snapshot sends every record as Added, then the Bookmark closing them, built from
// the zero record. False means the consumer is gone.
func (f *Fold[K, T, F]) Snapshot(ctx context.Context, out chan<- F, records []T) bool {
	for _, r := range records {
		f.known[f.key(r)] = r
		if !Send(ctx, out, f.frame(apimeta.CollectionWatchFrameAdded, r)) {
			return false
		}
	}
	var none T
	return Send(ctx, out, f.frame(apimeta.CollectionWatchFrameBookmark, none))
}

// Diff sends what a re-read changed: a record that appeared is Added, one that
// changed is Modified, one that went is Deleted carrying the last-known record.
// Nothing orders one watch's Deleted frames against another's.
func (f *Fold[K, T, F]) Diff(ctx context.Context, out chan<- F, records []T) bool {
	seen := make(map[K]struct{}, len(records))
	for _, r := range records {
		seen[f.key(r)] = struct{}{}
		if !f.Upsert(ctx, out, r) {
			return false
		}
	}
	for k, r := range f.known {
		if _, still := seen[k]; still {
			continue
		}
		delete(f.known, k)
		if !Send(ctx, out, f.frame(apimeta.CollectionWatchFrameDeleted, r)) {
			return false
		}
	}
	return true
}

// Upsert sends one record as Added or Modified, or nothing when it is unchanged.
func (f *Fold[K, T, F]) Upsert(ctx context.Context, out chan<- F, record T) bool {
	k := f.key(record)
	prev, had := f.known[k]
	if had && f.equal(prev, record) {
		return true
	}
	f.known[k] = record
	t := apimeta.CollectionWatchFrameAdded
	if had {
		t = apimeta.CollectionWatchFrameModified
	}
	return Send(ctx, out, f.frame(t, record))
}

// Has reports whether the fold has sent a record under key.
func (f *Fold[K, T, F]) Has(key K) bool {
	_, ok := f.known[key]
	return ok
}
