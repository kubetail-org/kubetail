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

package kubeconn

import (
	"context"
	"testing"

	"github.com/amorey/gobus/watch"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stateAt is a State told apart by the URL its connection answered at.
func stateAt(url string) State {
	return State{Connection: Observation[string]{Value: url}}
}

// A state watch's first value is the state it attached to, with nothing published since.
func TestAStateWatchSendsTheStateItAttachedTo(t *testing.T) {
	t.Parallel()

	hub := watch.New[string, State]()
	w := NewStateWatch(stateAt("https://a"), hub.Watch("prod"))
	defer w.Close()

	got, err := w.RecvContext(t.Context())

	require.NoError(t, err)
	assert.Equal(t, stateAt("https://a"), got)
}

func TestAStateWatchSendsEachPublishedStateAfterTheFirst(t *testing.T) {
	t.Parallel()

	hub := watch.New[string, State]()
	w := NewStateWatch(stateAt("https://a"), hub.Watch("prod"))
	defer w.Close()
	_, err := w.RecvContext(t.Context())
	require.NoError(t, err)

	require.NoError(t, hub.Sender().Send("prod", stateAt("https://b")))
	got, err := w.RecvContext(t.Context())

	require.NoError(t, err)
	assert.Equal(t, stateAt("https://b"), got)
}

func TestAStateWatchStopsWithItsContext(t *testing.T) {
	t.Parallel()

	hub := watch.New[string, State]()
	w := NewStateWatch(State{}, hub.Watch("prod"))
	defer w.Close()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	_, err := w.RecvContext(ctx)

	assert.ErrorIs(t, err, context.Canceled)
}

// A claim's watch opens on the claim's state now, so a holder needs no read of its own.
func TestAClaimsWatchOpensOnItsState(t *testing.T) {
	t.Parallel()

	srv := serveAPI(t)
	s := New(serving(srv, "prod", "key-1"))
	startService(t, s)
	lease := s.Acquire("prod")
	defer lease.Release()
	probed := lease.WatchState()
	defer probed.Close()
	awaitState(t, probed, func(st State) bool { return st.Phase() == PhaseProbed })

	watched := lease.WatchState()
	defer watched.Close()
	got, err := watched.RecvContext(t.Context())

	require.NoError(t, err)
	assert.Equal(t, PhaseProbed, got.Phase())
}
