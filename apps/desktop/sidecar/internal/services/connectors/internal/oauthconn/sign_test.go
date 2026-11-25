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
	"context"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/oskeyring/oskeyringtest"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/testutil"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/llm"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/connectors"
)

// Sign answers the access token the sign-in minted while it is valid.
func TestSignAnswersTheAccessTokenInMemory(t *testing.T) {
	r := newRig(t)
	connect(t, r.svc)

	tok, err := signedToken(t, r.svc)

	require.NoError(t, err)
	assert.Equal(t, "access-token-0002", tok)
	assert.Empty(t, r.issuer.forms("refresh_token"))
}

// An expired access token is refreshed first, with the saved client, the
// resource and no scope; the rotated refresh token is in the keyring before Sign
// answers.
func TestSignRefreshesAnExpiredToken(t *testing.T) {
	r := newRig(t)
	r.issuer.set(func(f *fakeIssuer) { f.accessTTL = 0 })
	connect(t, r.svc)
	r.issuer.set(func(f *fakeIssuer) { f.accessTTL = time.Hour })

	tok, err := signedToken(t, r.svc)

	require.NoError(t, err)
	assert.Equal(t, "access-token-0003", tok)
	forms := r.issuer.forms("refresh_token")
	require.Len(t, forms, 1)
	assert.Equal(t, "refresh-token-0002", forms[0].Get("refresh_token"))
	assert.Equal(t, "client-issued", forms[0].Get("client_id"))
	assert.Equal(t, apiBase, forms[0].Get("resource"))
	assert.Empty(t, forms[0].Get("scope"))
	stored, _ := storedRefresh(t, r)
	assert.Equal(t, "refresh-token-0003", stored)
}

// A rotated refresh token survives a crash between the refresh and the next
// request: the next launch refreshes with it, which a rotating issuer honours.
func TestARotatedTokenSurvivesACrash(t *testing.T) {
	r := newRig(t)
	r.issuer.set(func(f *fakeIssuer) { f.accessTTL = 0 })
	connect(t, r.svc)
	_, err := signedToken(t, r.svc)
	require.NoError(t, err)

	s := r.restart()
	tok, err := signedToken(t, s)

	require.NoError(t, err)
	assert.Equal(t, "access-token-0004", tok)
	assert.Equal(t, "refresh-token-0003", r.issuer.forms("refresh_token")[1].Get("refresh_token"))
}

// The access token lives in memory alone, so a launch refreshes at its first
// Sign, once.
func TestALaunchRefreshesAtItsFirstSign(t *testing.T) {
	r := newRig(t)
	connect(t, r.svc)
	s := r.restart()

	for range 2 {
		_, err := signedToken(t, s)
		require.NoError(t, err)
	}

	assert.Len(t, r.issuer.forms("refresh_token"), 1)
}

// Sign signs only its resource: a request off the API base is refused
// unsigned. signer.Covers holds the rule.
func TestTheBearerGoesNowhereElse(t *testing.T) {
	r := newRig(t)
	connect(t, r.svc)
	req, err := http.NewRequest(http.MethodGet, "https://api.test/v2/responses", nil)
	require.NoError(t, err)

	assert.Error(t, r.svc.Sign(req))
	assert.Empty(t, req.Header.Get("Authorization"))
}

// Callers that arrive during a refresh wait for its result: one refresh.
func TestConcurrentSignsShareOneRefresh(t *testing.T) {
	r := newRig(t)
	r.issuer.set(func(f *fakeIssuer) { f.accessTTL = 0 })
	connect(t, r.svc)
	release := make(chan struct{})
	r.issuer.set(func(f *fakeIssuer) { f.accessTTL, f.hold = time.Hour, release })

	var wg sync.WaitGroup
	tokens := make([]string, 4)
	for i := range tokens {
		wg.Go(func() {
			tok, err := signedToken(t, r.svc)
			assert.NoError(t, err)
			tokens[i] = tok
		})
	}
	r.issuer.arrived.Await(t, "the refresh")
	close(release)
	wg.Wait()

	assert.Len(t, r.issuer.forms("refresh_token"), 1)
	for _, tok := range tokens {
		assert.Equal(t, "access-token-0003", tok)
	}
}

// A caller that gives up stops waiting; the refresh goes on, since the issuer
// has already rotated the token, and the next caller uses its result.
func TestACancelledCallerDoesNotCancelTheRefresh(t *testing.T) {
	r := newRig(t)
	r.issuer.set(func(f *fakeIssuer) { f.accessTTL = 0 })
	connect(t, r.svc)
	release := make(chan struct{})
	r.issuer.set(func(f *fakeIssuer) { f.accessTTL, f.hold = time.Hour, release })
	ctx, cancel := context.WithCancel(t.Context())
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, apiBase+"/responses", nil)
	require.NoError(t, err)
	done := make(chan error, 1)
	go func() { done <- r.svc.Sign(req) }()
	r.issuer.arrived.Await(t, "the refresh")

	cancel()
	assert.ErrorIs(t, <-done, context.Canceled)
	r.issuer.set(func(f *fakeIssuer) { f.hold = nil })
	close(release)

	tok, err := signedToken(t, r.svc)
	require.NoError(t, err)
	assert.Equal(t, "access-token-0003", tok)
	assert.Len(t, r.issuer.forms("refresh_token"), 1)
}

// Until a rotated refresh token is in the keyring, Sign hands out no access
// token; a later Sign retries the write, never the refresh, and the spent token
// is never sent again.
func TestAnUnsavedRotationHoldsTheAccessTokenBack(t *testing.T) {
	r := newRig(t)
	r.issuer.set(func(f *fakeIssuer) { f.accessTTL = 0 })
	connect(t, r.svc)
	r.issuer.set(func(f *fakeIssuer) { f.accessTTL = time.Hour })
	r.keyring.failSet(refreshAccount, errBoom)

	_, err := signedToken(t, r.svc)
	require.Error(t, err)
	assert.NotErrorIs(t, err, llm.ErrSignedOut)
	_, err = signedToken(t, r.svc)
	require.Error(t, err)

	r.keyring.failSet(refreshAccount, nil)
	tok, err := signedToken(t, r.svc)
	require.NoError(t, err)
	assert.Equal(t, "access-token-0003", tok)
	assert.Len(t, r.issuer.forms("refresh_token"), 1)
	stored, _ := storedRefresh(t, r)
	assert.Equal(t, "refresh-token-0003", stored)
	assert.Equal(t, connectors.Connected, r.svc.State().Phase)
}

// A refresh the issuer refuses ends the session; invalid_client also forgets
// the registration, so the next Connect registers anew.
func TestARefusedRefreshEndsTheSession(t *testing.T) {
	for _, code := range testProvider("").SessionEnders {
		t.Run(code, func(t *testing.T) {
			r := newRig(t)
			r.issuer.set(func(f *fakeIssuer) { f.accessTTL = 0 })
			connect(t, r.svc)
			r.issuer.set(func(f *fakeIssuer) { f.refreshCode = code })

			_, err := signedToken(t, r.svc)

			assert.ErrorIs(t, err, llm.ErrSignedOut)
			st := r.svc.State()
			assert.Equal(t, connectors.Failed, st.Phase)
			assert.Equal(t, connectors.SessionEnded, st.Failure)
			_, ok := storedRefresh(t, r)
			assert.False(t, ok)
			reg := savedRegistration(t, r)
			assert.Empty(t, reg.SessionID)
			assert.Equal(t, code != clientGone, reg.ClientID != "")
			assert.Equal(t, code != clientGone, st.Registered)
		})
	}
}

// A refresh that cannot reach the issuer clears nothing: the next Sign tries
// again.
func TestAnUnreachableIssuerClearsNothing(t *testing.T) {
	r := newRig(t)
	r.issuer.set(func(f *fakeIssuer) { f.accessTTL = 0 })
	connect(t, r.svc)
	r.issuer.srv.Close()

	_, err := signedToken(t, r.svc)

	require.Error(t, err)
	assert.NotErrorIs(t, err, llm.ErrSignedOut)
	assert.Equal(t, connectors.Connected, r.svc.State().Phase)
	_, ok := storedRefresh(t, r)
	assert.True(t, ok)
}

// Refused drops the access token it reads off the request when it is still the
// current one; the next Sign refreshes. A stale one changes nothing.
func TestRefusedDropsTheCurrentAccessToken(t *testing.T) {
	r := newRig(t)
	connect(t, r.svc)
	stale := apiRequest(t, "/responses")
	stale.Header.Set("Authorization", "Bearer access-token-old")
	r.svc.Refused(stale)
	_, err := signedToken(t, r.svc)
	require.NoError(t, err)
	assert.Empty(t, r.issuer.forms("refresh_token"))

	req := apiRequest(t, "/responses")
	require.NoError(t, r.svc.Sign(req))
	r.svc.Refused(req)
	tok, err := signedToken(t, r.svc)

	require.NoError(t, err)
	assert.Equal(t, "access-token-0003", tok)
	assert.Len(t, r.issuer.forms("refresh_token"), 1)
}

// A binding made with no session has ended for good, even when the sign-in
// under way goes on to succeed.
func TestABindingWithNoSessionHasEnded(t *testing.T) {
	r := newRig(t)
	signedOut := r.svc.Bind()
	release := make(chan struct{})
	r.issuer.set(func(f *fakeIssuer) { f.hold = release })
	require.NoError(t, r.svc.Connect(t.Context(), false))
	r.issuer.arrived.Await(t, "the code exchange")
	signingIn := r.svc.Bind()
	close(release)
	await(t, r.svc, "Connected", inPhase(connectors.Connected))

	for _, b := range []llm.Signer{signedOut, signingIn} {
		_, err := signedToken(t, b)
		assert.ErrorIs(t, err, llm.ErrSignedOut)
	}
}

// A binding ends with its session: under the next account it fails signed out
// while the service signs on, and its Refused touches nothing.
func TestABindingEndsWithItsSession(t *testing.T) {
	r := newRig(t)
	connect(t, r.svc)
	bound := r.svc.Bind()
	tok, err := signedToken(t, bound)
	require.NoError(t, err)
	assert.Equal(t, "access-token-0002", tok)

	require.NoError(t, r.svc.Disconnect(t.Context()))
	connect(t, r.svc)

	_, err = signedToken(t, bound)
	assert.ErrorIs(t, err, llm.ErrSignedOut)
	req := apiRequest(t, "/responses")
	req.Header.Set("Authorization", "Bearer access-token-0004")
	bound.Refused(req)
	tok, err = signedToken(t, r.svc)
	require.NoError(t, err)
	assert.Equal(t, "access-token-0004", tok)
	assert.Empty(t, r.issuer.forms("refresh_token"))
}

// A bound signer's Refused drops its own session's token.
func TestABoundRefusalDropsTheToken(t *testing.T) {
	r := newRig(t)
	connect(t, r.svc)
	bound := r.svc.Bind()
	req := apiRequest(t, "/responses")
	require.NoError(t, bound.Sign(req))

	bound.Refused(req)
	_, err := signedToken(t, bound)

	require.NoError(t, err)
	assert.Len(t, r.issuer.forms("refresh_token"), 1)
}

// A binding with no session fails every Sign, and its Refused is a no-op.
func TestAnEndedBindingRefusesNothing(t *testing.T) {
	r := newRig(t)
	ended := r.svc.Bind()
	req := apiRequest(t, "/responses")

	ended.Refused(req)

	assert.ErrorIs(t, ended.Sign(req), llm.ErrSignedOut)
}

// An engine with no API base signs no request, absolute or relative.
func TestNoAPIBaseSignsNothing(t *testing.T) {
	r := newFixedRig(t)
	connect(t, r.svc)

	for _, raw := range []string{"/anything", apiBase + "/responses", r.issuer.url() + "/userinfo"} {
		req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, raw, nil)
		require.NoError(t, err)
		assert.ErrorIs(t, r.svc.Sign(req), errOffResource, raw)
		assert.Empty(t, req.Header.Get("Authorization"), raw)
	}
}

// A Sign made while Restoring carries the restored session's token once the
// read answers, and ends with its context when the read never does. A
// request off the resource is refused at once.
func TestSignWaitsForTheRestore(t *testing.T) {
	r := newRig(t)
	connect(t, r.svc)
	s, release := r.relaunchHeld(oskeyringtest.Get)

	off, err := http.NewRequest(http.MethodGet, "https://elsewhere.test/v1", nil)
	require.NoError(t, err)
	assert.ErrorIs(t, s.Sign(off), errOffResource)

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	assert.ErrorIs(t, s.Sign(apiRequest(t, "/responses").WithContext(ctx)), context.Canceled)

	signed := make(chan error, 1)
	go func() { _, err := signedToken(t, s); signed <- err }()
	release()
	assert.NoError(t, testutil.Recv(t, signed, "the signature"))
}

// A wait for the restore ends signed out when the service closes with the
// read still held, bound or not.
func TestAWaitEndsWithClose(t *testing.T) {
	r := newRig(t)
	connect(t, r.svc)
	s, _ := r.relaunchHeld(oskeyringtest.Get)
	bound := s.Bind()
	ended := make(chan error, 2)
	go func() { _, err := signedToken(t, s); ended <- err }()
	go func() { _, err := signedToken(t, bound); ended <- err }()

	s.Close()

	assert.ErrorIs(t, testutil.Recv(t, ended, "the unbound wait"), llm.ErrSignedOut)
	assert.ErrorIs(t, testutil.Recv(t, ended, "the bound wait"), llm.ErrSignedOut)
}

// A binding made while Restoring signs for the session the restore published,
// and never comes alive under one connected later. RestoredSession stays the
// restore's.
func TestABindingMadeWhileRestoringIsTheRestoredSessions(t *testing.T) {
	t.Run("restored connected", func(t *testing.T) {
		r := newRig(t)
		connect(t, r.svc)
		s, release := r.relaunchHeld(oskeyringtest.Get)
		bound := s.Bind()
		release()
		awaitRestored(t, s)

		_, err := signedToken(t, bound)
		require.NoError(t, err)
		restored := s.RestoredSession()
		assert.NotZero(t, restored)

		require.NoError(t, s.Disconnect(t.Context()))
		connect(t, s)
		_, err = signedToken(t, bound)
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

		_, err := signedToken(t, bound)
		assert.ErrorIs(t, err, llm.ErrSignedOut)
		assert.Zero(t, s.RestoredSession())
	})
}
