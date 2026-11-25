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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/llm"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/connectors"
)

// Disconnect deletes the entry and signs out; the key is used no more.
func TestDisconnectDeletesTheKey(t *testing.T) {
	r := newRig(t)
	connect(t, r.svc)

	require.NoError(t, r.svc.Disconnect())

	assert.Equal(t, connectors.SignedOut, r.svc.State().Phase)
	_, ok := r.keyring.entry(keyAccount)
	assert.False(t, ok)
	_, err := r.signedKey(r.svc)
	assert.ErrorIs(t, err, llm.ErrSignedOut)
	assert.Equal(t, connectors.SignedOut, r.restart().State().Phase)
}

// Disconnect during a sign-in ends it, and its callback finds no listener.
func TestDisconnectDuringAnAttempt(t *testing.T) {
	browser := &parked{}
	r := newRig(t, withBrowser(browser.open))
	require.NoError(t, r.svc.Connect(t.Context()))

	require.NoError(t, r.svc.Disconnect())

	assert.Equal(t, connectors.SignedOut, r.svc.State().Phase)
	require.NoError(t, r.svc.Connect(t.Context()))
}

// A delete the keyring refuses is never reported as a sign-out: Disconnect
// errors, the state is NotDeleted, every binding ends at once, Connect is
// refused, a restart finds the key again, and a retry that deletes signs out.
func TestAFailedDeleteIsNotASignOut(t *testing.T) {
	r := newRig(t)
	connect(t, r.svc)
	bound := r.svc.Bind()
	r.keyring.failDelete(keyAccount, errBoom)

	err := r.svc.Disconnect()

	assert.ErrorIs(t, err, errBoom)
	assert.Equal(t, connectors.Failed, r.svc.State().Phase)
	assert.Equal(t, connectors.NotDeleted, r.svc.State().Failure)
	_, err = r.signedKey(bound)
	assert.ErrorIs(t, err, llm.ErrSignedOut)
	_, err = r.signedKey(r.svc)
	assert.ErrorIs(t, err, llm.ErrSignedOut)
	assert.ErrorIs(t, r.svc.Connect(t.Context()), connectors.ErrNotDeleted)

	assert.ErrorIs(t, r.svc.Disconnect(), errBoom, "a retry tries the delete again")

	r.restart()
	assert.Equal(t, connectors.Connected, r.svc.State().Phase, "the key returns on restart, as the row says")
	r.keyring.failDelete(keyAccount, nil)
	require.NoError(t, r.svc.Disconnect())
	assert.Equal(t, connectors.SignedOut, r.svc.State().Phase)
	_, ok := r.keyring.entry(keyAccount)
	assert.False(t, ok)
}

// With no key held there is nothing to delete: Disconnect during a sign-in, or
// signed out, ends the attempt and signs out without touching the keyring, so a
// failing keyring is no NotDeleted and a newer Kubetail's entry stays.
func TestDisconnectWithNoKeyDeletesNothing(t *testing.T) {
	browser := &parked{}
	r := newRig(t, withBrowser(browser.open))
	r.keyring.failDelete(keyAccount, errBoom)
	require.NoError(t, r.svc.Connect(t.Context()))

	require.NoError(t, r.svc.Disconnect())

	assert.Equal(t, connectors.SignedOut, r.svc.State().Phase)
	require.NoError(t, r.svc.Connect(t.Context()))

	r.keyring.failDelete(keyAccount, nil)
	r.keyring.put(keyAccount, `{"v":2,"key":"sk-or-v1-newer"}`)
	s := r.restart()
	require.NoError(t, s.Disconnect())
	_, ok := r.keyring.entry(keyAccount)
	assert.True(t, ok, "a newer Kubetail's entry stays")
}
