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

package kubetailcloud

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/testutil"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/connectors"
)

// fakeEngine records the calls made to it. start answers Start; nil starts.
type fakeEngine struct {
	start func(fresh bool) (connectors.Failure, error)

	mu    sync.Mutex
	calls []string
	state connectors.State
	// restored is Restored's channel, closed unless a test holds it.
	restored chan struct{}

	// feed is the engine's Subscribe channel. Unbuffered, so a publish returns once the
	// subscriber has taken the state.
	feed chan connectors.State
}

func newFakeEngine() *fakeEngine {
	e := newRestoringEngine()
	close(e.restored)
	return e
}

// newRestoringEngine is an engine whose restore has not published.
func newRestoringEngine() *fakeEngine {
	return &fakeEngine{state: connectors.State{Phase: connectors.SignedOut}, restored: make(chan struct{}), feed: make(chan connectors.State)}
}

func (f *fakeEngine) Restored() <-chan struct{} { return f.restored }

func (f *fakeEngine) record(call string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, call)
}

func (f *fakeEngine) recorded() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.calls...)
}

// publish hands st to the subscriber and returns once it has taken it.
func (f *fakeEngine) publish(t *testing.T, st connectors.State) {
	t.Helper()
	select {
	case f.feed <- st:
	case <-time.After(testutil.Timeout):
		t.Fatalf("the subscriber never took %+v", st)
	}
}

func (f *fakeEngine) State() connectors.State {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.state
}

// WatchState's cancel closes the feed, as the engine's ends its channel.
func (f *fakeEngine) WatchState() (<-chan connectors.State, func()) {
	f.record("Subscribe")
	return f.feed, sync.OnceFunc(func() { f.record("Unsubscribe"); close(f.feed) })
}

func (f *fakeEngine) Start(_ context.Context, fresh bool) (connectors.Failure, error) {
	f.record(fmt.Sprintf("Start(fresh=%t)", fresh))
	if f.start == nil {
		return "", nil
	}
	return f.start(fresh)
}

func (f *fakeEngine) Cancel() { f.record("Cancel") }

func (f *fakeEngine) Disconnect(context.Context) error {
	f.record("Disconnect")
	return nil
}

func (f *fakeEngine) Close() { f.record("Close") }

// newTestService is the connector over e, closed when the test ends.
func newTestService(t *testing.T, e *fakeEngine) *Service {
	t.Helper()
	s, err := newService(func() (engine, error) { return e, nil })
	require.NoError(t, err)
	t.Cleanup(s.Close)
	return s
}
