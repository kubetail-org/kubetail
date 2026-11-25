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

package workqueue

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/testutil"
)

// result is one return from Next, carried back from the goroutine that made the call.
type result[K comparable] struct {
	key K
	ok  bool
}

// next calls Next off the test's goroutine so a call that should block does not hang the test,
// and hands back a channel the assertion reads.
func next[K comparable](ctx context.Context, q *Queue[K]) <-chan result[K] {
	out := make(chan result[K], 1)
	go func() {
		k, ok := q.Next(ctx)
		out <- result[K]{key: k, ok: ok}
	}()
	return out
}

// mustNext takes the next key, failing the test if none arrives.
func mustNext[K comparable](t *testing.T, ctx context.Context, q *Queue[K]) K {
	t.Helper()
	got := testutil.Recv(t, next(ctx, q), "next key")
	require.True(t, got.ok, "queue reported closed")
	return got.key
}

func TestAddIsDelivered(t *testing.T) {
	q := New[string]()
	q.Add("prod")

	assert.Equal(t, "prod", mustNext(t, t.Context(), q))
}

func TestWorkAddedBeforeAnyWorkerExistsIsStillDelivered(t *testing.T) {
	// The rule that rules out a fan-out bus: a bus drops a send nobody is receiving, so a
	// producer that runs before the worker loop starts would lose its work.
	q := New[string]()
	q.Add("prod")

	assert.Equal(t, "prod", mustNext(t, t.Context(), q))
}

func TestAddIfAbsentDoesNotRequestRedelivery(t *testing.T) {
	q := New[string]()
	q.AddIfAbsent("prod")
	q.AddIfAbsent("prod")
	require.Equal(t, "prod", mustNext(t, t.Context(), q))
	q.AddIfAbsent("prod")
	q.Done("prod")

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, ok := q.Next(ctx)
	require.False(t, ok, "a scheduled key was delivered twice")

	q.AddIfAbsent("prod")
	require.Equal(t, "prod", mustNext(t, t.Context(), q))
	q.Add("prod")
	q.AddIfAbsent("prod")
	q.Done("prod")
	require.Equal(t, "prod", mustNext(t, t.Context(), q), "an explicit add must still redeliver")
}

func TestNextWaitsForWork(t *testing.T) {
	q := New[string]()

	got := next(t.Context(), q)
	testutil.NoRecv(t, got, 50*time.Millisecond, "key before any add")

	q.Add("prod")
	assert.Equal(t, "prod", testutil.Recv(t, got, "next key").key)
}

func TestAKeyQueuedTwiceIsDeliveredOnce(t *testing.T) {
	q := New[string]()
	q.Add("prod")
	q.Add("prod")
	q.Add("staging")

	assert.Equal(t, "prod", mustNext(t, t.Context(), q))
	assert.Equal(t, "staging", mustNext(t, t.Context(), q))
}

func TestKeysAreDeliveredInTheOrderAdded(t *testing.T) {
	q := New[string]()
	for _, k := range []string{"a", "b", "c"} {
		q.Add(k)
	}

	for _, want := range []string{"a", "b", "c"} {
		assert.Equal(t, want, mustNext(t, t.Context(), q))
	}
}

func TestAKeyAddedWhileHeldIsQueuedAgainOnDone(t *testing.T) {
	// The reason a queue is more than a channel: the pass in flight cannot have seen this add,
	// so folding it into that pass would lose it.
	q := New[string]()

	q.Add("prod")
	require.Equal(t, "prod", mustNext(t, t.Context(), q))

	q.Add("prod")
	pending := next(t.Context(), q)
	testutil.NoRecv(t, pending, 50*time.Millisecond, "key handed out while still held")

	q.Done("prod")
	assert.Equal(t, "prod", testutil.Recv(t, pending, "requeued key").key)
}

func TestRepeatedAddsWhileHeldQueueOneMorePass(t *testing.T) {
	q := New[string]()

	q.Add("prod")
	require.Equal(t, "prod", mustNext(t, t.Context(), q))
	q.Add("prod")
	q.Add("prod")
	q.Done("prod")

	require.Equal(t, "prod", mustNext(t, t.Context(), q))
	q.Done("prod")
	testutil.NoRecv(t, next(t.Context(), q), 50*time.Millisecond, "a third pass")
}

func TestDoneForAKeyNeverTakenChangesNothing(t *testing.T) {
	q := New[string]()
	q.Done("prod")

	q.Add("prod")
	assert.Equal(t, "prod", mustNext(t, t.Context(), q))
}

func TestEachKeyReachesOneWorker(t *testing.T) {
	// What a bus cannot do, and the ceiling this queue exists to lift.
	const keys = 200

	q := New[int]()
	for i := range keys {
		q.Add(i)
	}
	q.Close()

	var (
		mu   sync.Mutex
		seen []int
		wg   sync.WaitGroup
	)
	for range 8 {
		wg.Go(func() {
			for {
				k, ok := q.Next(t.Context())
				if !ok {
					return
				}
				mu.Lock()
				seen = append(seen, k)
				mu.Unlock()
				q.Done(k)
			}
		})
	}
	testutil.WaitReturn(t, wg.Wait, "workers to drain the queue")

	assert.Len(t, seen, keys, "every key delivered exactly once")
	assert.ElementsMatch(t, seen, rangeTo(keys))
}

func TestNextEndsWhenTheContextDoes(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	got := next(ctx, New[string]())
	cancel()

	assert.False(t, testutil.Recv(t, got, "next after cancel").ok)
}

func TestCloseLetsAWorkerDrainWhatIsQueued(t *testing.T) {
	q := New[string]()
	q.Add("prod")
	q.Close()

	assert.Equal(t, "prod", mustNext(t, t.Context(), q))
	assert.False(t, testutil.Recv(t, next(t.Context(), q), "next after drain").ok)
}

func TestCloseWakesAParkedWorker(t *testing.T) {
	q := New[string]()
	got := next(t.Context(), q)
	testutil.NoRecv(t, got, 50*time.Millisecond, "key before close")

	q.Close()
	assert.False(t, testutil.Recv(t, got, "next after close").ok)
}

func TestAddAfterCloseIsDropped(t *testing.T) {
	q := New[string]()
	q.Close()
	q.Add("prod")
	q.AddIfAbsent("staging")

	assert.False(t, testutil.Recv(t, next(t.Context(), q), "next after close").ok)
}

func TestDoneAfterCloseQueuesNothing(t *testing.T) {
	q := New[string]()

	q.Add("prod")
	require.Equal(t, "prod", mustNext(t, t.Context(), q))
	q.Add("prod")
	q.Close()
	q.Done("prod")

	assert.False(t, testutil.Recv(t, next(t.Context(), q), "next after close").ok)
}

func TestCloseIsIdempotent(t *testing.T) {
	q := New[string]()
	q.Close()
	assert.NotPanics(t, q.Close)
}

func rangeTo(n int) []int {
	out := make([]int, n)
	for i := range out {
		out[i] = i
	}
	return out
}
