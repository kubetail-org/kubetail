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

package aws

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/testutil"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/connectors"
)

func TestDisconnectForgetsTheSession(t *testing.T) {
	r := newRig(t)
	connect(t, r.svc)

	require.NoError(t, r.svc.Disconnect())
	require.Equal(t, connectors.SignedOut, r.svc.State().Phase)
	_, ok := r.stored()
	require.False(t, ok)
	_, err := r.svc.Retrieve(t.Context())
	require.ErrorIs(t, err, ErrSignedOut)
	require.Equal(t, connectors.SignedOut, r.restart().State().Phase)
}

func TestADeleteTheKeyringRefusesIsNotDeletedUntilARetry(t *testing.T) {
	r := newRig(t)
	connect(t, r.svc)
	r.keyring.failDelete(sessionAccount, errBoom)

	require.ErrorIs(t, r.svc.Disconnect(), errBoom)
	require.Equal(t, connectors.NotDeleted, r.svc.State().Failure)
	_, err := r.svc.Retrieve(t.Context())
	require.ErrorIs(t, err, ErrSignedOut, "memory forgets the session first")
	require.ErrorIs(t, r.svc.Connect(t.Context()), connectors.ErrNotDeleted)

	r.keyring.failDelete(sessionAccount, nil)
	require.NoError(t, r.svc.Disconnect())
	require.Equal(t, connectors.SignedOut, r.svc.State().Phase)
	_, ok := r.stored()
	require.False(t, ok)
}

func TestDisconnectDuringASignInEndsIt(t *testing.T) {
	browser := &parked{}
	r := newRig(t, withBrowser(browser.open))
	require.NoError(t, r.svc.Connect(t.Context()))

	require.NoError(t, r.svc.Disconnect())
	require.Equal(t, connectors.SignedOut, r.svc.State().Phase)
}

func TestDisconnectSignedOutDeletesNothing(t *testing.T) {
	r := newRig(t)
	newer := jsonEntry(t, entry{V: entryVersion + 1, Refresh: "r", ARN: testARN})
	r.keyring.put(sessionAccount, newer)
	r.restart()

	require.NoError(t, r.svc.Disconnect())
	got, ok := r.keyring.entry(sessionAccount)
	require.True(t, ok)
	require.Equal(t, newer, got)
}

func TestARefreshThatEndsAfterADisconnectIsDropped(t *testing.T) {
	r := newRig(t)
	connect(t, r.svc)
	r.restart()
	hold := make(chan struct{})
	r.signin.set(func(f *fakeSignin) { f.hold = hold })

	done := make(chan error, 1)
	go func() {
		_, err := r.svc.Retrieve(t.Context())
		done <- err
	}()
	r.signin.arrived.Await(t, "the refresh")
	require.NoError(t, r.svc.Disconnect())
	close(hold)

	require.ErrorIs(t, testutil.Recv(t, done, "the refresh's answer"), ErrSignedOut)
	_, ok := r.stored()
	require.False(t, ok)
	require.Equal(t, connectors.SignedOut, r.svc.State().Phase)
}
