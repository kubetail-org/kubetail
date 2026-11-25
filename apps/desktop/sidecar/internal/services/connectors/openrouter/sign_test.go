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

package openrouter

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/oskeyring/oskeyringtest"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/testutil"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/llm"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/connectors"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/internal/signer"
)

// Sign signs only the API base: a request off it is refused unsigned.
// signer.Covers holds the rule.
func TestTheKeyGoesNowhereElse(t *testing.T) {
	keyring := newFakeKeyring()
	keyring.put(keyAccount, `{"v":1,"key":"stored-openrouter-key-0001"}`)
	s := newService(t, Config{APIBaseURL: "https://openrouter.test/api/v1", Keyring: keyring})
	require.Equal(t, connectors.Connected, s.State().Phase)
	req, err := http.NewRequest(http.MethodPost, "https://openrouter.test/auth", nil)
	require.NoError(t, err)

	assert.Error(t, s.Sign(req))
	assert.Empty(t, req.Header.Get("Authorization"))
}

// Outside Connected there is nothing to sign with.
func TestSignIsSignedOutWithoutAKey(t *testing.T) {
	r := newRig(t)

	_, err := r.signedKey(r.svc)

	assert.ErrorIs(t, err, llm.ErrSignedOut)
}

// A binding signs with the key it began under, and once that generation ends
// it fails signed out, whatever key replaced it.
func TestABindingEndsWithItsSession(t *testing.T) {
	r := newRig(t)
	connect(t, r.svc)
	bound := r.svc.Bind()
	key, err := r.signedKey(bound)
	require.NoError(t, err)
	stored, _ := r.storedKey()
	assert.Equal(t, stored, key)

	require.NoError(t, r.svc.Disconnect())
	connect(t, r.svc)

	_, err = r.signedKey(bound)
	assert.ErrorIs(t, err, llm.ErrSignedOut)
	_, err = r.signedKey(r.svc.Bind())
	assert.NoError(t, err)
}

// A binding made with no key has ended for good, even once a sign-in under
// way connects.
func TestABindingWithNoSessionHasEnded(t *testing.T) {
	browser := &parked{}
	r := newRig(t, withBrowser(browser.open))
	require.NoError(t, r.svc.Connect(t.Context()))
	bound := r.svc.Bind()

	_, err := r.signedKey(bound)

	assert.ErrorIs(t, err, llm.ErrSignedOut)
	bound.Refused(r.apiRequest("/chat/completions"))
}

// refusedWith tells signer of a 401 on a request it signed.
func (r *rig) refusedWith(signer llm.Signer) {
	r.t.Helper()
	req := r.apiRequest("/chat/completions")
	require.NoError(r.t, signer.Sign(req))
	signer.Refused(req)
}

// A 401 starts one GET /key under the key. Only that check's own 401 drops
// the key: the entry is deleted and the state is KeyRejected. A 200, or any
// other answer, or none, keeps it.
func TestARejectedKeyIsConfirmedFirst(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		drop   bool
	}{
		{name: "a 200", status: http.StatusOK},
		{name: "a 500", status: http.StatusInternalServerError},
		{name: "a 403", status: http.StatusForbidden},
		{name: "no answer", drop: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newRig(t)
			connect(t, r.svc)
			key, _ := r.storedKey()
			r.openRouter.set(func(f *fakeOpenRouter) { f.checkStatus, f.dropCheck = tc.status, tc.drop })

			r.refusedWith(r.svc)

			r.checkEnded.Await(t, "the check")
			// A dropped connection is retried once by the client, under the same key.
			checks := r.openRouter.keyChecks()
			require.NotEmpty(t, checks)
			for _, c := range checks {
				assert.Equal(t, key, c)
			}
			assert.Equal(t, connectors.Connected, r.svc.State().Phase)
			_, ok := r.keyring.entry(keyAccount)
			assert.True(t, ok)
		})
	}

	t.Run("a 401", func(t *testing.T) {
		r := newRig(t)
		connect(t, r.svc)
		bound := r.svc.Bind()
		r.openRouter.set(func(f *fakeOpenRouter) { f.checkStatus = http.StatusUnauthorized })

		r.refusedWith(bound)

		await(t, r.svc, "KeyRejected", failedWith(connectors.KeyRejected))
		_, ok := r.keyring.entry(keyAccount)
		assert.False(t, ok)
		_, err := r.signedKey(bound)
		assert.ErrorIs(t, err, llm.ErrSignedOut)
		connect(t, r.svc)
	})

	t.Run("a 401 whose entry cannot be deleted", func(t *testing.T) {
		r := newRig(t)
		connect(t, r.svc)
		r.keyring.failDelete(keyAccount, errBoom)
		r.openRouter.set(func(f *fakeOpenRouter) { f.checkStatus = http.StatusUnauthorized })

		r.refusedWith(r.svc)

		await(t, r.svc, "NotDeleted", failedWith(connectors.NotDeleted))
	})
}

// One check runs at a time: a 401 while one is under way joins it.
func TestOneKeyCheckAtATime(t *testing.T) {
	r := newRig(t)
	connect(t, r.svc)
	release := make(chan struct{})
	r.openRouter.set(func(f *fakeOpenRouter) { f.holdCheck = release })

	r.refusedWith(r.svc)
	r.openRouter.arrived.Await(t, "the check")
	r.refusedWith(r.svc)
	close(release)

	r.checkEnded.Await(t, "the check")
	assert.Len(t, r.openRouter.keyChecks(), 1)
}

// A 401 under a key that has ended, or on a request the key did not sign,
// starts no check; nor does a check's 401 drop a key that replaced its own.
func TestAStaleRefusalChecksNothing(t *testing.T) {
	r := newRig(t)
	connect(t, r.svc)
	bound := r.svc.Bind()
	req := r.apiRequest("/chat/completions")
	require.NoError(t, bound.Sign(req))
	require.NoError(t, r.svc.Disconnect())
	connect(t, r.svc)

	bound.Refused(req)
	r.svc.Refused(req)
	r.svc.Refused(r.apiRequest("/chat/completions"))

	assert.Empty(t, r.openRouter.keyChecks())

	release := make(chan struct{})
	r.openRouter.set(func(f *fakeOpenRouter) { f.holdCheck, f.checkStatus = release, http.StatusUnauthorized })
	r.refusedWith(r.svc)
	r.openRouter.arrived.Await(t, "the check")
	require.NoError(t, r.svc.Disconnect())
	connect(t, r.svc)
	close(release)

	r.checkEnded.Await(t, "the check")
	assert.Equal(t, connectors.Connected, r.svc.State().Phase)
}

// Bind answers signer.Ended{} with no session, a sign-in under way included:
// providercredentials reads that value as the cue to bind a key.
func TestBindIsDeadWithNoSession(t *testing.T) {
	r := newRig(t)
	assert.Equal(t, llm.Signer(signer.Ended{}), r.svc.Bind())
	connect(t, r.svc)
	assert.NotEqual(t, llm.Signer(signer.Ended{}), r.svc.Bind())
	require.NoError(t, r.svc.Disconnect())
	assert.Equal(t, llm.Signer(signer.Ended{}), r.svc.Bind())
}

// A Sign made while Restoring carries the restored key once the read answers,
// and ends with its context when the read never does. A request off the base
// is refused at once.
func TestSignWaitsForTheRestore(t *testing.T) {
	r := newRig(t)
	r.keyring.put(keyAccount, storedEntry)
	s, release := r.relaunchHeld(oskeyringtest.Get)

	off, err := http.NewRequest(http.MethodGet, "https://elsewhere.test/v1", nil)
	require.NoError(t, err)
	assert.ErrorIs(t, s.Sign(off), errOffBase)

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	assert.ErrorIs(t, s.Sign(r.apiRequest("/chat/completions").WithContext(ctx)), context.Canceled)

	signed := make(chan string, 1)
	go func() { key, _ := r.signedKey(s); signed <- key }()
	release()
	assert.Equal(t, "stored-openrouter-key-0001", testutil.Recv(t, signed, "the signature"))
}

// A wait for the restore ends signed out when the service closes with the
// read still held, bound or not.
func TestAWaitEndsWithClose(t *testing.T) {
	r := newRig(t)
	r.keyring.put(keyAccount, storedEntry)
	s, _ := r.relaunchHeld(oskeyringtest.Get)
	bound := s.Bind()
	ended := make(chan error, 2)
	go func() { _, err := r.signedKey(s); ended <- err }()
	go func() { _, err := r.signedKey(bound); ended <- err }()

	s.Close()

	assert.ErrorIs(t, testutil.Recv(t, ended, "the unbound wait"), llm.ErrSignedOut)
	assert.ErrorIs(t, testutil.Recv(t, ended, "the bound wait"), llm.ErrSignedOut)
}

// A binding made while Restoring signs with the key the restore published,
// and never comes alive under one connected later. RestoredSession stays the
// restore's.
func TestABindingMadeWhileRestoringIsTheRestoredSessions(t *testing.T) {
	t.Run("restored connected", func(t *testing.T) {
		r := newRig(t)
		r.keyring.put(keyAccount, storedEntry)
		s, release := r.relaunchHeld(oskeyringtest.Get)
		bound := s.Bind()
		release()
		awaitRestored(t, s)

		key, err := r.signedKey(bound)
		require.NoError(t, err)
		assert.Equal(t, "stored-openrouter-key-0001", key)
		restored := s.RestoredSession()
		assert.NotZero(t, restored)

		require.NoError(t, s.Disconnect())
		connect(t, s)
		_, err = r.signedKey(bound)
		assert.ErrorIs(t, err, llm.ErrSignedOut)
		assert.Equal(t, restored, s.RestoredSession())
	})
	t.Run("restored signed out", func(t *testing.T) {
		r := newRig(t)
		s, release := r.relaunchHeld(oskeyringtest.Get)
		bound := s.Bind()
		release()
		awaitRestored(t, s)
		connect(t, s)

		_, err := r.signedKey(bound)
		assert.ErrorIs(t, err, llm.ErrSignedOut)
		assert.Zero(t, s.RestoredSession())
	})
}
