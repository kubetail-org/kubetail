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

package sandbox

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/testutil"
)

// Past maxBoundedCalls left running, bounded starts nothing until one ends: a
// waiting caller whose context ends is answered its error, and one still
// waiting enters once a call ends.
func TestBoundedRunsAtMostItsCapAtOnce(t *testing.T) {
	var releases []chan struct{}
	var held sync.WaitGroup
	// Registered first, so a failure below still releases every held call.
	t.Cleanup(func() {
		for _, r := range releases {
			close(r)
		}
		held.Wait()
		boundedCalls.Wait()
	})
	for range maxBoundedCalls {
		release, started := make(chan struct{}), make(chan struct{})
		releases = append(releases, release)
		held.Go(func() {
			_, _ = bounded(t.Context(), func() (struct{}, error) {
				close(started)
				<-release
				return struct{}{}, nil
			})
		})
		testutil.Wait(t, started, "a held call to start")
	}

	ctx, cancel := context.WithCancel(t.Context())
	cancelled := make(chan error, 1)
	go func() {
		_, err := bounded(ctx, func() (struct{}, error) { return struct{}{}, nil })
		cancelled <- err
	}()
	cancel()
	assert.ErrorIs(t, testutil.Recv(t, cancelled, "a waiting caller to give up"), context.Canceled)

	entered := make(chan struct{}, 1)
	go func() {
		_, _ = bounded(t.Context(), func() (struct{}, error) { entered <- struct{}{}; return struct{}{}, nil })
	}()
	// A negative assertion: with a slot free the caller enters as soon as it
	// is scheduled, so a window far past that shows it waits.
	testutil.NoRecv(t, entered, 50*time.Millisecond, "a call past the cap entered")
	close(releases[0])
	releases = releases[1:]
	testutil.Recv(t, entered, "the waiting call to enter once a slot frees")
}

// A call whose context has ended starts nothing, even with a slot free, so
// calls nobody waits for cannot fill the slots.
func TestBoundedStartsNothingForAnEndedContext(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	entered := make(chan struct{}, 100)

	// select picks among ready cases at random, so one try could pass by luck.
	for range 100 {
		_, err := bounded(ctx, func() (struct{}, error) { entered <- struct{}{}; return struct{}{}, nil })
		assert.ErrorIs(t, err, context.Canceled)
	}
	boundedCalls.Wait()

	assert.Empty(t, entered, "a call for an ended context started")
	assert.Empty(t, boundedSlots, "a call for an ended context kept its slot")
}
