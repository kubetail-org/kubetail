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
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/testutil"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/connectors"
)

// Connect ends an attempt under way, then signs in fresh.
func TestConnectSupersedesAnAttempt(t *testing.T) {
	t.Parallel()
	e := newFakeEngine()
	s := newTestService(t, e)

	require.NoError(t, s.Connect(context.Background()))

	assert.Equal(t, []string{"Cancel", "Start(fresh=true)"}, e.recorded())
}

// A failure Start answers is Connect's error, naming it; a refusal passes
// through; a sign-in that started is nil.
func TestConnectErrsWhenTheSignInCannotStart(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		failure connectors.Failure
		err     error
		want    string
	}{
		{name: "started"},
		{name: "failed", failure: connectors.Unavailable, want: "kubetail: the sign-in could not start: Unavailable"},
		{name: "refused", err: connectors.ErrConnected, want: connectors.ErrConnected.Error()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			e := newFakeEngine()
			e.start = func(bool) (connectors.Failure, error) { return tc.failure, tc.err }
			s := newTestService(t, e)

			err := s.Connect(context.Background())

			if tc.want == "" {
				require.NoError(t, err)
				return
			}
			require.EqualError(t, err, tc.want)
			if tc.err != nil {
				require.ErrorIs(t, err, tc.err)
			}
		})
	}
}

// A Connect that overlaps one under way waits for it, then supersedes it:
// neither answers ErrSigningIn.
func TestOverlappingConnectsRunInTurn(t *testing.T) {
	t.Parallel()
	entered := make(chan struct{}, 2)
	release := make(chan struct{})
	e := newFakeEngine()
	first := true
	e.start = func(bool) (connectors.Failure, error) {
		entered <- struct{}{}
		if first {
			first = false
			<-release
		}
		return "", nil
	}
	s := newTestService(t, e)

	errs := make(chan error, 2)
	go func() { errs <- s.Connect(context.Background()) }()
	testutil.Wait(t, entered, "the first Connect to reach Start")
	go func() { errs <- s.Connect(context.Background()) }()

	// A negative assertion: the second Connect must not reach Start while the
	// first holds it, so its absence is checked for a bounded window.
	select {
	case <-entered:
		t.Fatal("the second Connect reached Start while the first was under way")
	case <-time.After(50 * time.Millisecond):
	}
	assert.Equal(t, []string{"Cancel", "Start(fresh=true)"}, e.recorded())

	close(release)
	require.NoError(t, testutil.Recv(t, errs, "the first Connect to answer"))
	require.NoError(t, testutil.Recv(t, errs, "the second Connect to answer"))
	assert.Equal(t, []string{"Cancel", "Start(fresh=true)", "Cancel", "Start(fresh=true)"}, e.recorded())
}
