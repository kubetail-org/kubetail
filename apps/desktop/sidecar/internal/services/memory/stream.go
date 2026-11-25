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

package memory

import (
	"context"
	"sync/atomic"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/appdb"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/apimeta"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/deltafold"
)

// MemoryCollectionWatchFrame is one frame on a cluster's memory watch. Memory is nil on the
// Bookmark.
type MemoryCollectionWatchFrame struct {
	Type   apimeta.CollectionWatchFrameType
	Memory *Memory
}

// Stream is a watch: its frames, and why they stopped. Frames closes on every exit,
// so Err is what tells a failure from an ordinary teardown. The same shape as
// chat.Stream.
type Stream[T any] struct {
	Frames <-chan T
	err    atomic.Pointer[error]
}

// Err returns why the stream ended, or nil if it ended cleanly. Read it once Frames
// has closed.
func (s *Stream[T]) Err() error {
	if p := s.err.Load(); p != nil {
		return *p
	}
	return nil
}

// WatchCollectionByCluster streams what cluster sees, then every change to it. It re-reads on its own
// key and on the clusters key, whose delete cascades to the cluster's memories.
func (s *service) WatchCollectionByCluster(ctx context.Context, clusterID apimeta.ClusterID) (*Stream[MemoryCollectionWatchFrame], error) {
	if err := s.enter(); err != nil {
		return nil, err
	}
	ch := make(chan MemoryCollectionWatchFrame, 1)
	st := &Stream[MemoryCollectionWatchFrame]{Frames: ch}
	ctx, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(s.ctx, cancel)
	go func() {
		defer s.wg.Done()
		defer close(ch)
		defer stop()
		defer cancel()
		if err := s.pump(ctx, clusterID, ch); err != nil && ctx.Err() == nil {
			st.err.Store(&err)
		}
	}()
	return st, nil
}

func (s *service) pump(ctx context.Context, cluster apimeta.ClusterID, out chan<- MemoryCollectionWatchFrame) error {
	// Listen, then read: a snapshot taken first would miss a commit in between.
	rx := s.db.ListenForChanges(appdb.KeyMemories, appdb.KeyClusters)
	defer rx.Close()

	f := deltafold.New(memoryKey, sameMemory, memoryFrame)
	ms, err := visible(ctx, s.store.Stmts(), cluster)
	if err != nil {
		return err
	}
	if !f.Snapshot(ctx, out, ms) {
		return nil
	}
	for {
		if _, err := rx.RecvContext(ctx); err != nil {
			return nil
		}
		ms, err := visible(ctx, s.store.Stmts(), cluster)
		if err != nil {
			return err
		}
		if !f.Diff(ctx, out, ms) {
			return nil
		}
	}
}

func memoryKey(m Memory) MemoryID { return m.ID }

// sameMemory compares what the wire carries, so a change to anything else sends
// no frame. The cluster is compared by value: two reads of one row hold different
// pointers.
func sameMemory(a, b Memory) bool {
	return same(a.ClusterID, b.ClusterID) && a.Name == b.Name && a.Body == b.Body &&
		a.WrittenBy == b.WrittenBy && a.UpdatedAt.Equal(b.UpdatedAt)
}

func same[T comparable](a, b *T) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

func memoryFrame(t apimeta.CollectionWatchFrameType, m Memory) MemoryCollectionWatchFrame {
	if t == apimeta.CollectionWatchFrameBookmark {
		return MemoryCollectionWatchFrame{Type: t}
	}
	return MemoryCollectionWatchFrame{Type: t, Memory: &m}
}
