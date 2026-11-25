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

package oauthconn

import (
	"bytes"
	"log/slog"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/safe"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/testutil"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/llm"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/connectors"
)

// Disconnect signs out on this machine, then revokes the refresh token with
// the saved client. The registration stays.
func TestDisconnectRevokesAndClears(t *testing.T) {
	r := newRig(t)
	connect(t, r.svc)
	refresh, _ := storedRefresh(t, r)

	require.NoError(t, r.svc.Disconnect(t.Context()))

	st := r.svc.State()
	assert.Equal(t, connectors.SignedOut, st.Phase)
	assert.True(t, st.Registered)
	_, ok := storedRefresh(t, r)
	assert.False(t, ok)
	reg := savedRegistration(t, r)
	assert.Empty(t, reg.SessionID)
	assert.Equal(t, "client-issued", reg.ClientID)
	r.issuer.awaitRevoked(t, refresh)
	revoked := r.issuer.revoked[0]
	assert.Equal(t, "refresh_token", revoked.Get("token_type_hint"))
	assert.Equal(t, "client-issued", revoked.Get("client_id"))
	_, err := signedToken(t, r.svc)
	assert.ErrorIs(t, err, llm.ErrSignedOut)
}

// A launch's session is revoked too, its endpoints discovered for it.
func TestDisconnectRevokesARestoredSession(t *testing.T) {
	r := newRig(t)
	connect(t, r.svc)
	refresh, _ := storedRefresh(t, r)
	s := r.restart()

	require.NoError(t, s.Disconnect(t.Context()))

	r.issuer.awaitRevoked(t, refresh)
}

// A revocation the provider never confirms is tried once more, then said on the
// state, until the next Connect.
func TestAnUnconfirmedRevocationSaysSo(t *testing.T) {
	r := newRig(t)
	connect(t, r.svc)
	r.issuer.set(func(f *fakeIssuer) { f.revokeStatus = http.StatusBadGateway })

	require.NoError(t, r.svc.Disconnect(t.Context()))

	st := await(t, r.svc, "an unconfirmed revocation", func(s connectors.State) bool { return s.RevocationUnconfirmed })
	assert.Equal(t, connectors.SignedOut, st.Phase)
	assert.Len(t, r.issuer.revokedTokens(), 2)

	r.issuer.set(func(f *fakeIssuer) { f.revokeStatus = 0 })
	require.NoError(t, r.svc.Connect(t.Context(), false))
	assert.False(t, r.svc.State().RevocationUnconfirmed)
}

// A sign-out whose row write fails still deletes the entry; only when both
// fail would the session return at the next launch, and Disconnect says so.
func TestADisconnectThatCannotWrite(t *testing.T) {
	t.Run("the row alone", func(t *testing.T) {
		fail, writes := failingWrites(0, errBoom)
		r := newRig(t)
		connect(t, r.svc)
		r.opts = append(r.opts, fail)
		s := r.restart()
		*writes = 0

		require.NoError(t, s.Disconnect(t.Context()))

		_, ok := storedRefresh(t, r)
		assert.False(t, ok)
		assert.Equal(t, connectors.SignedOut, r.restart().State().Phase)
	})
	t.Run("the row and the entry", func(t *testing.T) {
		fail, _ := failingWrites(0, errBoom)
		r := newRig(t)
		connect(t, r.svc)
		r.opts = append(r.opts, fail)
		s := r.restart()
		r.keyring.failDelete(refreshAccount, errBoom)

		assert.Error(t, s.Disconnect(t.Context()))
	})
}

// A refresh that finishes after a Disconnect belongs to no session: every
// caller waiting on it fails signed out, and its replacement tokens are revoked
// and never saved.
func TestDisconnectDuringARefresh(t *testing.T) {
	r := newRig(t)
	r.issuer.set(func(f *fakeIssuer) { f.accessTTL = 0 })
	connect(t, r.svc)
	release := make(chan struct{})
	r.issuer.set(func(f *fakeIssuer) { f.accessTTL, f.hold = time.Hour, release })
	done := make(chan error, 1)
	go func() {
		_, err := signedToken(t, r.svc)
		done <- err
	}()
	r.issuer.arrived.Await(t, "the refresh")

	require.NoError(t, r.svc.Disconnect(t.Context()))
	close(release)

	assert.ErrorIs(t, <-done, llm.ErrSignedOut)
	r.issuer.awaitRevoked(t, "refresh-token-0003")
	_, ok := storedRefresh(t, r)
	assert.False(t, ok)
}

// The next session does not join a refresh still under way for an ended one:
// it refreshes on its own, and the old refresh answers its own caller alone.
func TestANewSessionDoesNotJoinAnEndedSessionsRefresh(t *testing.T) {
	r := newRig(t)
	r.issuer.set(func(f *fakeIssuer) { f.accessTTL = 0 })
	connect(t, r.svc)
	release := make(chan struct{})
	r.issuer.set(func(f *fakeIssuer) { f.hold = release })
	stale := make(chan error, 1)
	go func() {
		_, err := signedToken(t, r.svc)
		stale <- err
	}()
	r.issuer.arrived.Await(t, "the ended session's refresh")
	require.NoError(t, r.svc.Disconnect(t.Context()))
	r.issuer.set(func(f *fakeIssuer) { f.hold = nil })
	connect(t, r.svc)

	fresh := make(chan error, 1)
	go func() {
		_, err := signedToken(t, r.svc)
		fresh <- err
	}()

	assert.NoError(t, testutil.Recv(t, fresh, "the new session's own refresh"))
	close(release)
	assert.ErrorIs(t, testutil.Recv(t, stale, "the ended session's answer"), llm.ErrSignedOut)
}

// A refresh that lands after Disconnect and cannot be revoked says so on the
// signed-out state.
func TestALateRefreshThatCannotBeRevokedSaysSo(t *testing.T) {
	r := newRig(t)
	r.issuer.set(func(f *fakeIssuer) { f.accessTTL = 0 })
	connect(t, r.svc)
	release := make(chan struct{})
	// The session's own token is revoked; the one the late refresh mints is not.
	r.issuer.set(func(f *fakeIssuer) { f.hold, f.refuseRevoke = release, "refresh-token-0003" })
	done := make(chan error, 1)
	go func() {
		_, err := signedToken(t, r.svc)
		done <- err
	}()
	r.issuer.arrived.Await(t, "the refresh")
	require.NoError(t, r.svc.Disconnect(t.Context()))
	r.issuer.awaitRevoked(t, "refresh-token-0002")

	close(release)

	assert.ErrorIs(t, testutil.Recv(t, done, "the refresh's answer"), llm.ErrSignedOut)
	st := await(t, r.svc, "an unconfirmed revocation", func(s connectors.State) bool { return s.RevocationUnconfirmed })
	assert.Equal(t, connectors.SignedOut, st.Phase)
}

// Disconnect during an attempt ends the attempt.
func TestDisconnectDuringAnAttempt(t *testing.T) {
	r := newRig(t, withBrowser((&parked{}).open))
	require.NoError(t, r.svc.Connect(t.Context(), false))

	require.NoError(t, r.svc.Disconnect(t.Context()))

	assert.Equal(t, connectors.SignedOut, r.svc.State().Phase)
	assert.NoError(t, r.svc.Connect(t.Context(), false), "a new attempt may start")
}

// redactedLog keeps what is logged as the production handler renders it,
// blanked as each line is written.
type redactedLog struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (l *redactedLog) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.buf.WriteString(safe.String(string(p)))
	return len(p), nil
}

func (l *redactedLog) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.buf.String()
}

// A refused revocation that echoes the token logs it blanked: the token stays
// registered until the revocation ends.
func TestAFailedRevocationLogsNoToken(t *testing.T) {
	var logs redactedLog
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })
	r := newRig(t)
	connect(t, r.svc)
	refresh, _ := storedRefresh(t, r)
	r.issuer.set(func(f *fakeIssuer) { f.revokeStatus, f.revokeEcho = http.StatusBadRequest, true })

	require.NoError(t, r.svc.Disconnect(t.Context()))

	await(t, r.svc, "an unconfirmed revocation", func(s connectors.State) bool { return s.RevocationUnconfirmed })
	assert.Contains(t, logs.String(), "cannot revoke")
	assert.NotContains(t, logs.String(), refresh)
}
