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
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/oskeyring/oskeyringtest"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/testutil"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/connectors"
)

// connected is a rig whose service restored storedToken.
func connected(t *testing.T) *rig {
	t.Helper()
	r := newRig(t)
	r.keyring.put(tokenAccount, storedEntry(storedToken, "octocat"))
	r.restart()
	return r
}

// No token is handed out signed out or during a sign-in.
func TestATokenOnlyWhileConnected(t *testing.T) {
	r := newRig(t)
	_, err := r.svc.Token(t.Context())
	require.ErrorIs(t, err, ErrSignedOut)

	holdGrant(t, r)
	require.NoError(t, r.svc.Connect(t.Context()))
	_, err = r.svc.Token(t.Context())
	require.ErrorIs(t, err, ErrSignedOut)
}

// A 401 on the token is confirmed through /user before it is dropped: only
// /user's own 401 deletes the entry, and Kubetail cannot revoke a token.
func TestARefusedTokenIsCheckedBeforeItIsDropped(t *testing.T) {
	for status, dropped := range map[int]bool{
		http.StatusUnauthorized:       true,
		http.StatusOK:                 false,
		http.StatusServiceUnavailable: false,
	} {
		r := connected(t)
		r.github.set(func(f *fakeGitHub) { f.userStatus = status })

		r.svc.Refused(storedToken)
		r.checkEnded.Await(t, "the check")

		assert.Equal(t, []string{storedToken}, r.github.userRequests(), status)
		_, ok := r.stored()
		assert.Equal(t, !dropped, ok, status)
		if dropped {
			assert.Equal(t, connectors.State{Phase: connectors.Failed, Failure: connectors.KeyRejected, Session: r.svc.State().Session}, r.svc.State())
			_, err := r.svc.Token(t.Context())
			require.ErrorIs(t, err, ErrSignedOut)
		} else {
			assert.Equal(t, connectors.Connected, r.svc.State().Phase, status)
		}
	}
}

// Another token's 401 starts no check, and one check runs at a time.
func TestOneCheckOfTheCurrentToken(t *testing.T) {
	r := connected(t)
	hold := make(chan struct{})
	r.github.set(func(f *fakeGitHub) { f.holdUser = hold })

	r.svc.Refused("test_someoneElsesToken000000001")
	r.svc.Refused(storedToken)
	r.github.arrived.Await(t, "the check")
	r.svc.Refused(storedToken)
	close(hold)
	r.checkEnded.Await(t, "the check")

	assert.Equal(t, []string{storedToken}, r.github.userRequests())
}

// fallback is gh's token in the tests: one token, and the 401s it is told of.
type fallback struct {
	token   string
	refused []string
}

func (f *fallback) Token(context.Context) (string, error) { return f.token, nil }
func (f *fallback) Refused(token string)                  { f.refused = append(f.refused, token) }

// The proxy's token is the connector's while connected and the fallback's
// otherwise; a 401 is told to both, each of which acts on its own token alone.
func TestTokensPreferTheConnector(t *testing.T) {
	gh := &fallback{token: "test_ghsOwnToken00000000000001"}
	r := newRig(t)
	tokens := r.svc.Tokens(gh)

	tok, err := tokens.Token(t.Context())
	require.NoError(t, err)
	assert.Equal(t, gh.token, tok)

	r.keyring.put(tokenAccount, storedEntry(storedToken, "octocat"))
	tokens = r.restart().Tokens(gh)
	tok, err = tokens.Token(t.Context())
	require.NoError(t, err)
	assert.Equal(t, storedToken, tok)

	tokens.Refused(storedToken)
	r.checkEnded.Await(t, "the check")
	assert.Equal(t, []string{storedToken}, gh.refused)
	assert.Equal(t, []string{storedToken}, r.github.userRequests())
}

// A check that cannot reach GitHub keeps the token.
func TestACheckThatCannotReachGitHubKeepsTheToken(t *testing.T) {
	r := connected(t)
	r.github.srv.Close()

	r.svc.Refused(storedToken)
	r.checkEnded.Await(t, "the check")

	assert.Equal(t, connectors.Connected, r.svc.State().Phase)
	_, ok := r.stored()
	assert.True(t, ok)
}

// A Token asked for while Restoring answers the restored token once the read
// answers, and ends with its context when the read never does. The proxy's
// tokens wait too, and reach the fallback only once the connector is signed
// out.
func TestTokenWaitsForTheRestore(t *testing.T) {
	gh := &fallback{token: "test_ghsOwnToken00000000000001"}
	r := newRig(t)
	r.keyring.put(tokenAccount, storedEntry(storedToken, "octocat"))
	s, release := r.relaunchHeld(oskeyringtest.Get)

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err := s.Tokens(gh).Token(ctx)
	assert.ErrorIs(t, err, context.Canceled, "no fallback before the restore")

	got := make(chan string, 1)
	go func() { tok, _ := s.Tokens(gh).Token(t.Context()); got <- tok }()
	release()
	assert.Equal(t, storedToken, testutil.Recv(t, got, "the token"))
}

// A wait for the restore ends signed out when the service closes with the
// read still held.
func TestAWaitEndsWithClose(t *testing.T) {
	r := newRig(t)
	s, _ := r.relaunchHeld(oskeyringtest.Get)
	ended := make(chan error, 1)
	go func() { _, err := s.Token(t.Context()); ended <- err }()

	s.Close()

	assert.ErrorIs(t, testutil.Recv(t, ended, "the wait"), ErrSignedOut)
}
