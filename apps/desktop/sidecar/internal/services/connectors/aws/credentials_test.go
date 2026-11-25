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
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/oskeyring/oskeyringtest"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/testutil"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/connectors"
)

func TestASignInsCredentialsAreHandedOutAsTheyCame(t *testing.T) {
	r := newRig(t)
	connect(t, r.svc)

	creds, err := r.svc.Retrieve(t.Context())
	require.NoError(t, err)
	require.Equal(t, r.signin.accessKeyIDs()[0], creds.AccessKeyID)
	require.NotEmpty(t, creds.SecretAccessKey)
	require.NotEmpty(t, creds.SessionToken)
	require.Equal(t, "123456789012", creds.AccountID)
	require.True(t, creds.CanExpire)
	require.Len(t, r.signin.tokenBodies(), 1, "no refresh")
}

func TestARestoredSessionRefreshesUnderItsKeyAndKeepsTheRotatedToken(t *testing.T) {
	r := newRig(t)
	connect(t, r.svc)
	before, _ := r.stored()
	r.restart()

	creds, err := r.svc.Retrieve(t.Context())
	require.NoError(t, err)
	bodies := r.signin.tokenBodies()
	require.Len(t, bodies, 2)
	require.Equal(t, "refresh_token", bodies[1]["grantType"])
	require.Equal(t, before.Refresh, bodies[1]["refreshToken"])
	require.Equal(t, r.signin.accessKeyIDs()[1], creds.AccessKeyID)

	after, _ := r.stored()
	require.NotEqual(t, before.Refresh, after.Refresh)
	require.Equal(t, before.DPoPKey, after.DPoPKey)
	require.Equal(t, before.ARN, after.ARN)
}

func TestCredentialsAreReusedUntilTheyNearExpiry(t *testing.T) {
	clock := newClock()
	r := newRig(t, withNow(clock.now))
	connect(t, r.svc)

	_, err := r.svc.Retrieve(t.Context())
	require.NoError(t, err)
	clock.advance(900*time.Second - expiryWindow - time.Second)
	_, err = r.svc.Retrieve(t.Context())
	require.NoError(t, err)
	require.Len(t, r.signin.tokenBodies(), 1)

	clock.advance(2 * time.Second)
	creds, err := r.svc.Retrieve(t.Context())
	require.NoError(t, err)
	require.Len(t, r.signin.tokenBodies(), 2)
	require.Equal(t, r.signin.accessKeyIDs()[1], creds.AccessKeyID)
}

func TestSignedOutThereAreNoCredentials(t *testing.T) {
	r := newRig(t)
	_, err := r.svc.Retrieve(t.Context())
	require.ErrorIs(t, err, ErrSignedOut)
}

func TestARefreshAWSRefusesEndsTheSession(t *testing.T) {
	for name, e := range map[string]apiError{
		"invalid grant": {status: http.StatusBadRequest, typ: "InvalidGrantException", code: "invalid_grant"},
		"expired":       {status: http.StatusUnauthorized, typ: "AccessDeniedException", code: "TOKEN_EXPIRED"},
		"password":      {status: http.StatusUnauthorized, typ: "AccessDeniedException", code: "USER_CREDENTIALS_CHANGED"},
	} {
		t.Run(name, func(t *testing.T) {
			r := newRig(t)
			r.storeSession(entry{Refresh: "refresh-unknown", DPoPKey: encodeKey(newDPoPKey()), ARN: testARN})
			r.restart()
			r.signin.set(func(f *fakeSignin) { f.tokenErr = &e })

			_, err := r.svc.Retrieve(t.Context())
			require.ErrorIs(t, err, ErrSignedOut)
			require.Equal(t, connectors.Failed, r.svc.State().Phase)
			require.Equal(t, connectors.SessionEnded, r.svc.State().Failure)
			_, ok := r.stored()
			require.False(t, ok)
		})
	}
}

func TestARefreshThatNeverReachesAWSKeepsTheSession(t *testing.T) {
	for name, e := range map[string]apiError{
		"dropped":     {dropConn: true},
		"unavailable": {status: http.StatusServiceUnavailable, typ: "ServiceUnavailableException", code: "service_unavailable"},
	} {
		t.Run(name, func(t *testing.T) {
			r := newRig(t)
			connect(t, r.svc)
			r.restart()
			r.signin.set(func(f *fakeSignin) { f.tokenErr = &e })

			_, err := r.svc.Retrieve(t.Context())
			require.Error(t, err)
			require.NotErrorIs(t, err, ErrSignedOut)
			require.Equal(t, connectors.Connected, r.svc.State().Phase)

			r.signin.set(func(f *fakeSignin) { f.tokenErr = nil })
			_, err = r.svc.Retrieve(t.Context())
			require.NoError(t, err)
		})
	}
}

func TestARotatedTokenIsSavedBeforeCredentialsAreHandedOut(t *testing.T) {
	r := newRig(t)
	connect(t, r.svc)
	r.restart()
	r.keyring.failSet(sessionAccount, errBoom)

	_, err := r.svc.Retrieve(t.Context())
	require.ErrorIs(t, err, errBoom)
	_, err = r.svc.Retrieve(t.Context())
	require.ErrorIs(t, err, errBoom, "no credentials while the rotated token is unsaved")

	r.keyring.failSet(sessionAccount, nil)
	creds, err := r.svc.Retrieve(t.Context())
	require.NoError(t, err)
	require.Len(t, r.signin.tokenBodies(), 2, "the save is retried, never the refresh")
	require.Equal(t, r.signin.accessKeyIDs()[1], creds.AccessKeyID)
	stored, _ := r.stored()
	var live bool
	r.signin.set(func(f *fakeSignin) { _, live = f.refreshes[stored.Refresh] })
	require.True(t, live, "the keyring holds the live refresh token")
}

// A Retrieve made while Restoring answers the restored session's credentials
// once the read answers, and ends with its context when the read never does.
func TestRetrieveWaitsForTheRestore(t *testing.T) {
	r := newRig(t)
	connect(t, r.svc)
	s, release := r.relaunchHeld(oskeyringtest.Get)

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err := s.Retrieve(ctx)
	require.ErrorIs(t, err, context.Canceled)

	got := make(chan error, 1)
	go func() { _, err := s.Retrieve(t.Context()); got <- err }()
	// A bounded negative wait: a Retrieve that did not wait would answer
	// ErrSignedOut at once, with no session restored yet.
	testutil.NoRecv(t, got, 50*time.Millisecond, "a Retrieve before the read answers")
	release()
	require.NoError(t, testutil.Recv(t, got, "the credentials"))
}

// A wait for the restore ends signed out when the service closes with the
// read still held.
func TestAWaitEndsWithClose(t *testing.T) {
	r := newRig(t)
	s, _ := r.relaunchHeld(oskeyringtest.Get)
	ended := make(chan error, 1)
	go func() { _, err := s.Retrieve(t.Context()); ended <- err }()
	// A bounded negative wait: a Retrieve that did not wait would answer
	// ErrSignedOut at once, as Close's end of the wait does.
	testutil.NoRecv(t, ended, 50*time.Millisecond, "a Retrieve before the read answers")

	s.Close()

	require.ErrorIs(t, testutil.Recv(t, ended, "the wait"), ErrSignedOut)
}
