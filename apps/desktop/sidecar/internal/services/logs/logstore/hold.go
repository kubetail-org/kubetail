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

import "sync"

// holds counts, by id, what is using a row or a chunk, so a sweep leaves it alone.
type holds struct {
	mu sync.Mutex
	n  map[int64]int
}

// hold counts ids held until release is called; a second release does nothing.
func (h *holds) hold(ids []int64) (release func()) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.holdLocked(ids)
}

// holdLocked is hold with mu held.
func (h *holds) holdLocked(ids []int64) (release func()) {
	for _, id := range ids {
		h.n[id]++
	}
	var once sync.Once
	return func() {
		once.Do(func() {
			h.mu.Lock()
			defer h.mu.Unlock()
			for _, id := range ids {
				if h.n[id]--; h.n[id] == 0 {
					delete(h.n, id)
				}
			}
		})
	}
}

// unlessHeld runs fn under the holds' lock unless anything holds id, so a hold taken
// meanwhile waits for fn: what fn removes goes before its hold or not at all.
func (h *holds) unlessHeld(id int64, fn func() error) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.n[id] > 0 {
		return nil
	}
	return fn()
}

// Hold keeps the stream rows from the janitor until release is called. A row the
// janitor removed before the hold stays removed: it was a gone pod's with nothing in
// the file, which a walk reads nothing of. On a closed claim nothing is held.
func (s *Store) Hold(rows ...int64) (release func()) {
	f, done, err := s.use()
	if err != nil {
		return func() {}
	}
	defer done()
	return f.heldRows.hold(rows)
}

// HoldChunks keeps the chunks from eviction until release is called. On a closed
// claim nothing is held.
func (s *Store) HoldChunks(chunks ...int64) (release func()) {
	f, done, err := s.use()
	if err != nil {
		return func() {}
	}
	defer done()
	return f.heldChunks.hold(chunks)
}
