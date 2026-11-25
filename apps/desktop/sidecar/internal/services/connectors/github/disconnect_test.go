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

package github

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/safe"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/connectors"
)

// Disconnect forgets the token on this machine: memory, the redactor's
// registration and the entry.
func TestDisconnectForgetsTheToken(t *testing.T) {
	r := newRig(t)
	connect(t, r.svc)
	token := r.github.mintedTokens()[0]

	require.NoError(t, r.svc.Disconnect())

	assert.Equal(t, connectors.SignedOut, r.svc.State().Phase)
	_, ok := r.stored()
	assert.False(t, ok)
	assert.False(t, safe.HasSecret(token))
	_, err := r.svc.Token(t.Context())
	require.ErrorIs(t, err, ErrSignedOut)
}

// A delete the keyring refuses is Failed with NotDeleted and returned. Connect
// is refused until a Disconnect deletes it.
func TestARefusedDeleteIsRetried(t *testing.T) {
	r := connected(t)
	r.keyring.failDelete(tokenAccount, errBoom)

	require.ErrorIs(t, r.svc.Disconnect(), errBoom)

	assert.Equal(t, connectors.NotDeleted, r.svc.State().Failure)
	require.ErrorIs(t, r.svc.Connect(t.Context()), connectors.ErrNotDeleted)

	r.keyring.failDelete(tokenAccount, nil)
	require.NoError(t, r.svc.Disconnect())
	assert.Equal(t, connectors.SignedOut, r.svc.State().Phase)
	_, ok := r.stored()
	assert.False(t, ok)
}

// Disconnect during a sign-in ends it, and with no token held deletes
// nothing: any entry is a newer Kubetail's.
func TestDisconnectWithNoTokenDeletesNothing(t *testing.T) {
	r := newRig(t)
	newer := `{"v":2,"token":"x"}`
	r.keyring.put(tokenAccount, newer)
	r.restart()
	holdGrant(t, r)
	require.NoError(t, r.svc.Connect(t.Context()))

	require.NoError(t, r.svc.Disconnect())

	assert.Equal(t, connectors.SignedOut, r.svc.State().Phase)
	v, _ := r.keyring.entry(tokenAccount)
	assert.Equal(t, newer, v)
}

// A check that ends after a Disconnect drops nothing it does not hold.
func TestACheckAfterADisconnectIsDropped(t *testing.T) {
	r := connected(t)
	hold := make(chan struct{})
	r.github.set(func(f *fakeGitHub) { f.holdUser = hold; f.userStatus = http.StatusUnauthorized })

	r.svc.Refused(storedToken)
	r.github.arrived.Await(t, "the check")
	require.NoError(t, r.svc.Disconnect())
	close(hold)
	r.checkEnded.Await(t, "the check")

	assert.Equal(t, connectors.SignedOut, r.svc.State().Phase)
}
