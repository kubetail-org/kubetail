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
	"encoding/json"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/appdb/appdbtest"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/testutil"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/connectors"
)

// A first sign-in registers a client with the provider and connects: the authorize
// URL carries the flow's parameters, the issued client id is saved, and the
// refresh token goes to the keyring.
func TestAFirstSignInRegistersAndConnects(t *testing.T) {
	r := newRig(t)

	st := connect(t, r.svc)

	assert.Equal(t, "ada@example.com", st.Email)
	assert.True(t, st.Registered)
	queries := r.issuer.authorizeQueries()
	require.Len(t, queries, 1)
	q := queries[0]
	assert.Equal(t, "code", q.Get("response_type"))
	assert.Equal(t, firstClientID, q.Get("client_id"))
	assert.Equal(t, "yes", q.Get("registering"))
	assert.Equal(t, apiBase, q.Get("resource"))
	assert.Equal(t, "openid email offline_access plan.use", q.Get("scope"))
	assert.Equal(t, "S256", q.Get("code_challenge_method"))
	assert.NotEmpty(t, q.Get("code_challenge"))
	assert.NotEmpty(t, q.Get("state"))
	assert.NotEmpty(t, q.Get("nonce"))
	assert.Empty(t, q.Get("login_hint"))
	assert.Regexp(t, `^host-`, q.Get("host"), "the provider's own parameter, over the data it minted")
	assert.Regexp(t, regexp.MustCompile(`^http://127\.0\.0\.1:\d+/auth/callback$`), q.Get("redirect_uri"))

	exchanges := r.issuer.forms("authorization_code")
	require.Len(t, exchanges, 1)
	assert.Equal(t, "client-issued", exchanges[0].Get("client_id"))
	assert.Equal(t, apiBase, exchanges[0].Get("resource"))
	assert.Equal(t, q.Get("redirect_uri"), exchanges[0].Get("redirect_uri"))

	reg := savedRegistration(t, r)
	require.NotEmpty(t, reg.SessionID)
	assert.Equal(t, registration{
		Data: hostData(q.Get("host")), ClientID: "client-issued", Subject: "user-1", Email: "ada@example.com", Name: "Ada Lovelace",
		SessionID: reg.SessionID,
	}, reg)
	e, found, err := parseEntry(r.keyring.Get(refreshAccount))
	require.NoError(t, err)
	require.True(t, found)
	assert.True(t, strings.HasPrefix(e.Token, "refresh-token-"))
	assert.Equal(t, reg.SessionID, e.Session, "the entry names the row's sign-in")
}

// The issued client id is saved before the code is exchanged, so a failed
// exchange does not register a second client.
func TestTheClientIDIsSavedBeforeTheExchange(t *testing.T) {
	r := newRig(t)
	release := make(chan struct{})
	r.issuer.set(func(f *fakeIssuer) { f.hold = release })

	require.NoError(t, r.svc.Connect(t.Context(), false))
	r.issuer.arrived.Await(t, "the code exchange")

	assert.Equal(t, "client-issued", savedRegistration(t, r).ClientID)
	close(release)
	await(t, r.svc, "Connected", inPhase(connectors.Connected))
}

// A later sign-in runs under the saved client, hints the saved account, and
// sends the same host id.
func TestALaterSignInReusesTheRegistration(t *testing.T) {
	r := newRig(t)
	connect(t, r.svc)
	require.NoError(t, r.svc.Disconnect(t.Context()))

	connect(t, r.svc)

	queries := r.issuer.authorizeQueries()
	require.Len(t, queries, 2)
	later := queries[1]
	assert.Equal(t, "client-issued", later.Get("client_id"))
	assert.Equal(t, "ada@example.com", later.Get("login_hint"))
	assert.Empty(t, later.Get("registering"))
	assert.Equal(t, queries[0].Get("host"), later.Get("host"))
	assert.Equal(t, "client-issued", r.issuer.forms("authorization_code")[1].Get("client_id"))
}

// A later sign-in's redirect may leave the client id out; naming another one
// fails the attempt.
func TestALaterSignInRefusesAnotherClient(t *testing.T) {
	r := newRig(t)
	connect(t, r.svc)
	require.NoError(t, r.svc.Disconnect(t.Context()))

	none := ""
	r.issuer.set(func(f *fakeIssuer) { f.callbackClientID = &none })
	connect(t, r.svc)
	require.NoError(t, r.svc.Disconnect(t.Context()))

	other := "client-other"
	r.issuer.set(func(f *fakeIssuer) { f.callbackClientID = &other })
	require.NoError(t, r.svc.Connect(t.Context(), false))
	await(t, r.svc, "Refused", failedWith(connectors.Refused))
	assert.Equal(t, "client-issued", savedRegistration(t, r).ClientID)
}

// A first sign-in must come back with a client of its own.
func TestAFirstSignInWithoutAnIssuedClientIsRefused(t *testing.T) {
	for name, clientID := range map[string]string{"none": "", "the registration client": firstClientID} {
		t.Run(name, func(t *testing.T) {
			r := newRig(t)
			r.issuer.set(func(f *fakeIssuer) { f.callbackClientID = &clientID })

			require.NoError(t, r.svc.Connect(t.Context(), false))

			await(t, r.svc, "Refused", failedWith(connectors.Refused))
			assert.Empty(t, savedRegistration(t, r).ClientID)
			assert.Empty(t, r.issuer.forms("authorization_code"))
		})
	}
}

// A user who declines on the provider's page has declined.
func TestADeclinedSignIn(t *testing.T) {
	r := newRig(t)
	r.issuer.set(func(f *fakeIssuer) { f.deny = "access_denied" })

	require.NoError(t, r.svc.Connect(t.Context(), false))

	await(t, r.svc, "Declined", failedWith(connectors.Declined))
}

// Signing in proves identity alone: without the required scope the
// tokens are revoked and kept nowhere, and the next attempt asks for consent.
func TestASignInWithoutTheRequiredScopeKeepsNoTokens(t *testing.T) {
	r := newRig(t)
	r.issuer.set(func(f *fakeIssuer) { f.scope = "openid email offline_access" })

	require.NoError(t, r.svc.Connect(t.Context(), false))

	await(t, r.svc, "ScopeNotGranted", failedWith(connectors.ScopeNotGranted))
	r.issuer.awaitRevoked(t, "refresh-token-0002")
	_, ok := storedRefresh(t, r)
	assert.False(t, ok)
	assert.Empty(t, savedRegistration(t, r).SessionID)
	assert.Equal(t, "client-issued", savedRegistration(t, r).ClientID)

	r.issuer.set(func(f *fakeIssuer) { f.scope = strings.Join(scopes, " ") })
	connect(t, r.svc)
	assert.Equal(t, "consent", r.issuer.authorizeQueries()[1].Get("prompt"))
	require.NoError(t, r.svc.Disconnect(t.Context()))
	connect(t, r.svc)
	assert.Empty(t, r.issuer.authorizeQueries()[2].Get("prompt"), "consent is asked for once")
}

// A token response with no scope granted what was asked, the required scope included.
func TestASignInWhoseResponseNamesNoScopeConnects(t *testing.T) {
	r := newRig(t)
	r.issuer.set(func(f *fakeIssuer) { f.scope = "" })

	connect(t, r.svc)

	_, ok := storedRefresh(t, r)
	assert.True(t, ok)
}

// The registration belongs to one account: a sign-in as another is refused,
// its tokens revoked, and the registration kept.
func TestAnotherAccountIsAMismatch(t *testing.T) {
	r := newRig(t)
	connect(t, r.svc)
	require.NoError(t, r.svc.Disconnect(t.Context()))
	r.issuer.set(func(f *fakeIssuer) { f.subject, f.email = "user-2", "bob@example.com" })

	require.NoError(t, r.svc.Connect(t.Context(), false))

	await(t, r.svc, "AccountMismatch", failedWith(connectors.AccountMismatch))
	minted := r.issuer.mintedTokens()
	r.issuer.awaitRevoked(t, minted[len(minted)-3])
	reg := savedRegistration(t, r)
	assert.Equal(t, "user-1", reg.Subject)
	assert.Equal(t, "ada@example.com", reg.Email)
	assert.Empty(t, reg.SessionID)
}

// An attempt nobody finishes times out on its own bound.
func TestASignInTimesOut(t *testing.T) {
	r := newRig(t, withBrowser((&parked{}).open), withAttemptTimeout(0))

	require.NoError(t, r.svc.Connect(t.Context(), false))

	await(t, r.svc, "TimedOut", failedWith(connectors.TimedOut))
}

// An issuer that cannot be reached fails the attempt before any browser opens.
func TestAnUnreachableIssuerIsUnavailable(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	addr := ln.Addr().String()
	require.NoError(t, ln.Close())
	browser := &parked{}
	s := newService(t, Config{Provider: testProvider("http://" + addr), DB: appdbtest.Open(t), Keyring: newFakeKeyring()},
		withBrowser(browser.open))

	require.NoError(t, s.Connect(t.Context(), false))

	await(t, s, "Unavailable", failedWith(connectors.Unavailable))
	assert.Zero(t, browser.opened())
}

// A discovery document the flow cannot trust fails like an unreachable one.
func TestAnUntrustedDiscoveryIsUnavailable(t *testing.T) {
	srv := http.NewServeMux()
	srv.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{"issuer": "https://evil.test"})
	})
	r := newRig(t)
	ts := newServer(t, srv)
	s := newService(t, Config{Provider: testProvider(ts), DB: r.db, Keyring: r.keyring}, withBrowser(following))

	require.NoError(t, s.Connect(t.Context(), false))

	await(t, s, "Unavailable", failedWith(connectors.Unavailable))
}

// Connect is refused while an attempt runs, and while connected.
func TestConnectIsRefusedDuringAnAttemptAndWhileConnected(t *testing.T) {
	r := newRig(t, withBrowser((&parked{}).open))
	require.NoError(t, r.svc.Connect(t.Context(), false))

	assert.ErrorIs(t, r.svc.Connect(t.Context(), false), connectors.ErrSigningIn)
	assert.ErrorIs(t, r.svc.Connect(t.Context(), true), connectors.ErrSigningIn)

	r2 := newRig(t)
	connect(t, r2.svc)
	assert.ErrorIs(t, r2.svc.Connect(t.Context(), false), connectors.ErrConnected)
	assert.ErrorIs(t, r2.svc.Connect(t.Context(), true), connectors.ErrConnected)
}

// failingWrites makes every registration write after the first n fail.
func failingWrites(n int, err error) (option, *int) {
	var writes int
	return withRegistrationWriter(func(write registrationWriter) registrationWriter {
		return func(reg registration) error {
			writes++
			if writes > n {
				return err
			}
			return write(reg)
		}
	}), &writes
}

// A sign-in that cannot be recorded never starts: no browser opens.
func TestASignInThatCannotBeRecordedOpensNoBrowser(t *testing.T) {
	t.Run("the first write", func(t *testing.T) {
		browser := &parked{}
		fail, _ := failingWrites(0, errBoom)
		r := newRig(t, withBrowser(browser.open), fail)

		require.NoError(t, r.svc.Connect(t.Context(), false))

		await(t, r.svc, "NotSaved", failedWith(connectors.NotSaved))
		assert.Zero(t, browser.opened())
	})
	t.Run("the keyring check", func(t *testing.T) {
		browser := &parked{}
		r := newRig(t, withBrowser(browser.open))
		r.keyring.failSet(checkAccount, errBoom)

		require.NoError(t, r.svc.Connect(t.Context(), false))

		await(t, r.svc, "NotSaved", failedWith(connectors.NotSaved))
		assert.Zero(t, browser.opened())
	})
}

// A sign-in whose save fails is revoked and keeps nothing but its client id,
// and Connected is never published.
func TestASignInThatCannotBeSavedKeepsNothing(t *testing.T) {
	t.Run("the refresh entry", func(t *testing.T) {
		r := newRig(t)
		r.keyring.failSet(refreshAccount, errBoom)
		states, cancel := r.svc.WatchState()
		defer cancel()

		require.NoError(t, r.svc.Connect(t.Context(), false))

		await(t, r.svc, "NotSaved", failedWith(connectors.NotSaved))
		r.issuer.awaitRevoked(t, "refresh-token-0002")
		assert.Equal(t, registration{Data: savedRegistration(t, r).Data, ClientID: "client-issued"}, savedRegistration(t, r))
		assertNeverConnected(t, states)
	})
	t.Run("the row", func(t *testing.T) {
		// The writes are the sign-in's first, the client id, then the session.
		fail, _ := failingWrites(2, errBoom)
		r := newRig(t, fail)
		states, cancel := r.svc.WatchState()
		defer cancel()

		require.NoError(t, r.svc.Connect(t.Context(), false))

		await(t, r.svc, "NotSaved", failedWith(connectors.NotSaved))
		r.issuer.awaitRevoked(t, "refresh-token-0002")
		_, ok := storedRefresh(t, r)
		assert.False(t, ok, "the entry is deleted")
		assertNeverConnected(t, states)
	})
}

// assertNeverConnected reads every state published so far and finds no
// Connected among them.
func assertNeverConnected(t *testing.T, states <-chan connectors.State) {
	t.Helper()
	for {
		select {
		case st := <-states:
			assert.NotEqual(t, connectors.Connected, st.Phase)
		default:
			return
		}
	}
}

// A crash between the refresh entry and the row leaves an entry naming a
// sign-in the row does not, which the next launch deletes and never restores.
func TestACrashBeforeTheRowLandsRestoresNoSession(t *testing.T) {
	fail, _ := failingWrites(2, errBoom)
	r := newRig(t, fail)
	r.keyring.failDelete(refreshAccount, errBoom)
	require.NoError(t, r.svc.Connect(t.Context(), false))
	await(t, r.svc, "NotSaved", failedWith(connectors.NotSaved))
	_, ok := storedRefresh(t, r)
	require.True(t, ok, "the failed delete left the entry")
	r.keyring.failDelete(refreshAccount, nil)
	r.opts = nil

	s := r.restart()

	assert.Equal(t, connectors.SignedOut, s.State().Phase)
	_, ok = storedRefresh(t, r)
	assert.False(t, ok)
}

// A revocation of an unsaved sign-in that the provider does not confirm says so.
func TestAnUnsavedSignInThatCannotBeRevokedSaysSo(t *testing.T) {
	r := newRig(t)
	r.keyring.failSet(refreshAccount, errBoom)
	r.issuer.set(func(f *fakeIssuer) { f.revokeStatus = http.StatusInternalServerError })

	require.NoError(t, r.svc.Connect(t.Context(), false))

	st := await(t, r.svc, "an unconfirmed revocation", func(s connectors.State) bool { return s.RevocationUnconfirmed })
	assert.Equal(t, connectors.NotSaved, st.Failure)
}

// Cancel before the redirect closes the listener and exchanges nothing.
func TestCancelBeforeTheCallback(t *testing.T) {
	browser := &parked{}
	r := newRig(t, withBrowser(browser.open))
	require.NoError(t, r.svc.Connect(t.Context(), false))
	await(t, r.svc, "SigningIn", inPhase(connectors.SigningIn))

	r.svc.Cancel()

	assert.Equal(t, connectors.SignedOut, r.svc.State().Phase)
	u, err := url.Parse(browser.urls[0])
	require.NoError(t, err)
	// The attempt's goroutine closes the listener once its wait sees the cancel,
	// after Cancel returns.
	redirect := u.Query().Get("redirect_uri")
	require.Eventually(t, func() bool {
		resp, err := http.Get(redirect)
		if err != nil {
			return true
		}
		resp.Body.Close()
		return false
	}, testutil.Timeout, time.Millisecond, "the listener closes")
	assert.Empty(t, r.issuer.forms("authorization_code"))
}

// An exchange that finishes after a Cancel or a Disconnect belongs to no
// session: its tokens are revoked and never saved, and the issued client id
// stays saved.
func TestAnExchangeThatFinishesLateIsRevoked(t *testing.T) {
	for name, end := range map[string]func(*Service){
		"cancel":     func(s *Service) { s.Cancel() },
		"disconnect": func(s *Service) { _ = s.Disconnect(t.Context()) },
	} {
		t.Run(name, func(t *testing.T) {
			r := newRig(t)
			release := make(chan struct{})
			r.issuer.set(func(f *fakeIssuer) { f.hold = release })
			require.NoError(t, r.svc.Connect(t.Context(), false))
			r.issuer.arrived.Await(t, "the code exchange")

			end(r.svc)
			close(release)

			r.issuer.awaitRevoked(t, "refresh-token-0002")
			assert.Equal(t, connectors.SignedOut, r.svc.State().Phase)
			_, ok := storedRefresh(t, r)
			assert.False(t, ok)
			assert.Equal(t, "client-issued", savedRegistration(t, r).ClientID)
			assert.Empty(t, savedRegistration(t, r).SessionID)
		})
		t.Run(name+" unconfirmed", func(t *testing.T) {
			r := newRig(t)
			release := make(chan struct{})
			r.issuer.set(func(f *fakeIssuer) { f.hold, f.revokeStatus = release, http.StatusInternalServerError })
			require.NoError(t, r.svc.Connect(t.Context(), false))
			r.issuer.arrived.Await(t, "the code exchange")

			end(r.svc)
			close(release)

			st := await(t, r.svc, "an unconfirmed revocation", func(s connectors.State) bool { return s.RevocationUnconfirmed })
			assert.Equal(t, connectors.SignedOut, st.Phase)
		})
	}
}

// A late exchange that finishes during the next attempt is revoked under the
// client it was issued to, and the new attempt goes on.
func TestALateExchangeDoesNotDisturbTheNextAttempt(t *testing.T) {
	r := newRig(t)
	release := make(chan struct{})
	r.issuer.set(func(f *fakeIssuer) { f.hold = release })
	require.NoError(t, r.svc.Connect(t.Context(), false))
	r.issuer.arrived.Await(t, "the code exchange")
	r.svc.Cancel()
	browser := &parked{}
	r.svc.open = browser.open
	require.NoError(t, r.svc.Connect(t.Context(), true))

	close(release)

	r.issuer.awaitRevoked(t, "refresh-token-0002")
	assert.Equal(t, connectors.SigningIn, r.svc.State().Phase)
	revoked := r.issuer.revoked[0]
	assert.Equal(t, "client-issued", revoked.Get("client_id"))
}

// Connect(fresh) forgets the registration and the account, and keeps the host
// id: it is the way to change accounts.
func TestAFreshConnectRegistersAnew(t *testing.T) {
	r := newRig(t)
	connect(t, r.svc)
	require.NoError(t, r.svc.Disconnect(t.Context()))
	r.issuer.set(func(f *fakeIssuer) { f.issued, f.subject, f.email = "client-second", "user-2", "bob@example.com" })

	require.NoError(t, r.svc.Connect(t.Context(), true))
	st := await(t, r.svc, "Connected", inPhase(connectors.Connected))

	assert.Equal(t, "bob@example.com", st.Email)
	queries := r.issuer.authorizeQueries()
	assert.Equal(t, firstClientID, queries[1].Get("client_id"))
	assert.Equal(t, "yes", queries[1].Get("registering"))
	assert.Empty(t, queries[1].Get("login_hint"))
	assert.Equal(t, queries[0].Get("host"), queries[1].Get("host"))
	assert.Equal(t, "client-second", savedRegistration(t, r).ClientID)
}

// newServer runs mux until the test ends and answers its URL.
func newServer(t *testing.T, mux *http.ServeMux) string {
	t.Helper()
	srv := &http.Server{Handler: mux}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { _ = srv.Close() })
	return "http://" + ln.Addr().String()
}

// Any other error on the redirect is a refusal.
func TestAnotherAuthorizationErrorIsRefused(t *testing.T) {
	r := newRig(t)
	r.issuer.set(func(f *fakeIssuer) { f.deny = "server_error" })

	require.NoError(t, r.svc.Connect(t.Context(), false))

	await(t, r.svc, "Refused", failedWith(connectors.Refused))
}

// A browser that cannot open fails the attempt at once.
func TestABrowserThatCannotOpenIsRefused(t *testing.T) {
	r := newRig(t, withBrowser(func(string) error { return errBoom }))

	require.NoError(t, r.svc.Connect(t.Context(), false))

	assert.Equal(t, connectors.Refused, r.svc.State().Failure)
}

// A code the issuer refuses, or an exchange that never answers, fails the
// attempt as the issuer's refusal or as unreachable.
func TestAFailedExchange(t *testing.T) {
	t.Run("refused", func(t *testing.T) {
		r := newRig(t)
		r.issuer.set(func(f *fakeIssuer) { f.exchangeError = "invalid_grant" })

		require.NoError(t, r.svc.Connect(t.Context(), false))

		await(t, r.svc, "Refused", failedWith(connectors.Refused))
	})
	t.Run("unreachable", func(t *testing.T) {
		r := newRig(t)
		release := make(chan struct{})
		r.issuer.set(func(f *fakeIssuer) { f.hold = release })
		require.NoError(t, r.svc.Connect(t.Context(), false))
		r.issuer.arrived.Await(t, "the code exchange")

		r.issuer.srv.CloseClientConnections()

		await(t, r.svc, "Unavailable", failedWith(connectors.Unavailable))
		close(release)
	})
	t.Run("after a cancel", func(t *testing.T) {
		r := newRig(t)
		release := make(chan struct{})
		r.issuer.set(func(f *fakeIssuer) { f.hold = release })
		require.NoError(t, r.svc.Connect(t.Context(), false))
		r.issuer.arrived.Await(t, "the code exchange")
		r.svc.Cancel()
		r.issuer.set(func(f *fakeIssuer) { f.exchangeError = "invalid_grant" })

		close(release)
		r.svc.Close() // joins the attempt's goroutine

		assert.Equal(t, connectors.SignedOut, r.svc.State().Phase, "a stale failure changes nothing")
	})
}

// An ID token that does not carry the nonce sent is refused, and its tokens
// revoked.
func TestAWrongNonceIsRefused(t *testing.T) {
	r := newRig(t)
	r.issuer.set(func(f *fakeIssuer) { f.badNonce = true })

	require.NoError(t, r.svc.Connect(t.Context(), false))

	await(t, r.svc, "Refused", failedWith(connectors.Refused))
	r.issuer.awaitRevoked(t, "refresh-token-0002")
}

// A refused ID token whose revocation the provider does not confirm says so on the
// failed state.
func TestAWrongNonceThatCannotBeRevokedSaysSo(t *testing.T) {
	r := newRig(t)
	r.issuer.set(func(f *fakeIssuer) { f.badNonce, f.revokeStatus = true, http.StatusInternalServerError })

	require.NoError(t, r.svc.Connect(t.Context(), false))

	st := await(t, r.svc, "an unconfirmed revocation", func(s connectors.State) bool { return s.RevocationUnconfirmed })
	assert.Equal(t, connectors.Failed, st.Phase)
	assert.Equal(t, connectors.Refused, st.Failure)
}

// A first sign-in whose client id cannot be recorded stops before the exchange.
func TestAClientIDThatCannotBeSavedStops(t *testing.T) {
	fail, _ := failingWrites(1, errBoom)
	r := newRig(t, fail)

	require.NoError(t, r.svc.Connect(t.Context(), false))

	await(t, r.svc, "NotSaved", failedWith(connectors.NotSaved))
	assert.Empty(t, r.issuer.forms("authorization_code"))
}

// An ended attempt's redirect records no client: a later attempt's stands.
func TestAnEndedAttemptRecordsNoClient(t *testing.T) {
	r := newRig(t, withBrowser((&parked{}).open))
	require.NoError(t, r.svc.Connect(t.Context(), false))
	r.svc.mu.Lock()
	stale := r.svc.gen
	r.svc.mu.Unlock()
	r.svc.Cancel()
	require.NoError(t, r.svc.Connect(t.Context(), false))

	assert.False(t, r.svc.saveClientID(stale, "client-stale"))

	assert.Empty(t, savedRegistration(t, r).ClientID)
	assert.Equal(t, connectors.SigningIn, r.svc.State().Phase)
}

// A sign-in that grants no refresh token is refused, and keeps nothing.
func TestASignInWithoutARefreshTokenKeepsNothing(t *testing.T) {
	r := newRig(t)
	r.issuer.set(func(f *fakeIssuer) { f.noRefresh = true })

	require.NoError(t, r.svc.Connect(t.Context(), false))

	await(t, r.svc, "Refused", failedWith(connectors.Refused))
	_, ok := storedRefresh(t, r)
	assert.False(t, ok)
	assert.Empty(t, savedRegistration(t, r).SessionID)
}

// Cancel does nothing outside a sign-in.
func TestCancelOutsideASignIn(t *testing.T) {
	r := newRig(t)
	before := r.svc.State()

	r.svc.Cancel()

	assert.Equal(t, before, r.svc.State())
}

// A fixed client registers nothing: the authorize names it, the row's client_id
// stays empty, and a redirect may name it or no client. One naming another
// client is refused.
func TestAFixedClientRegistersNothing(t *testing.T) {
	t.Run("a sign-in", func(t *testing.T) {
		r := newFixedRig(t)

		st := connect(t, r.svc)

		q := r.issuer.authorizeQueries()
		require.Len(t, q, 1)
		assert.Equal(t, fixedClientID, q[0].Get("client_id"))
		assert.False(t, q[0].Has("resource"), "no API base, no resource")
		assert.Empty(t, savedRegistration(t, r).ClientID)
		assert.False(t, st.Registered)
		exchanges := r.issuer.forms("authorization_code")
		require.Len(t, exchanges, 1)
		assert.Equal(t, fixedClientID, exchanges[0].Get("client_id"))
	})
	t.Run("a redirect naming no client", func(t *testing.T) {
		r := newFixedRig(t)
		none := ""
		r.issuer.set(func(f *fakeIssuer) { f.callbackClientID = &none })

		connect(t, r.svc)
	})
	t.Run("a redirect naming another client", func(t *testing.T) {
		r := newFixedRig(t)
		other := "other-client"
		r.issuer.set(func(f *fakeIssuer) { f.callbackClientID = &other })

		require.NoError(t, r.svc.Connect(t.Context(), false))

		await(t, r.svc, "Refused", failedWith(connectors.Refused))
		assert.Empty(t, savedRegistration(t, r).ClientID)
	})
}

// A provider with written endpoints discovers nothing; its client's secret goes
// on the exchange and the refresh, and no resource goes anywhere.
func TestAnInstalledAppSignsInUnderItsWrittenEndpoints(t *testing.T) {
	r := newRigWith(t, installedProvider)

	connect(t, r.svc)
	req := apiRequest(t, "/chat/completions")
	require.NoError(t, r.svc.Sign(req))
	r.svc.Refused(req)
	_, err := signedToken(t, r.svc)
	require.NoError(t, err)

	r.issuer.set(func(f *fakeIssuer) { assert.Zero(t, f.discoveries) })
	q := r.issuer.authorizeQueries()
	require.Len(t, q, 1)
	assert.False(t, q[0].Has("resource"))
	for _, grant := range []string{"authorization_code", "refresh_token"} {
		forms := r.issuer.forms(grant)
		require.Len(t, forms, 1, grant)
		assert.Equal(t, installedSecret, forms[0].Get("client_secret"), grant)
		assert.False(t, forms[0].Has("resource"), grant)
	}
}

// Start answers why a sign-in could not start, leaving the state Failed with
// it and no browser open. Connect leaves the same state and answers nil.
func TestStartAnswersWhyItCouldNotStart(t *testing.T) {
	for name, c := range map[string]struct {
		fail func(r *rig)
		want connectors.Failure
	}{
		"discovery fails":            {func(r *rig) { r.issuer.srv.Close() }, connectors.Unavailable},
		"the keyring refuses writes": {func(r *rig) { r.keyring.failSet(checkAccount, errBoom) }, connectors.NotSaved},
		"the browser will not open":  {func(r *rig) { r.svc.open = func(string) error { return errBoom } }, connectors.Refused},
	} {
		t.Run(name, func(t *testing.T) {
			browser := &parked{}
			r := newFixedRig(t, withBrowser(browser.open))
			c.fail(r)

			failure, err := r.svc.Start(t.Context(), true)

			require.NoError(t, err)
			assert.Equal(t, c.want, failure)
			assert.True(t, failedWith(c.want)(r.svc.State()))

			require.NoError(t, r.svc.Connect(t.Context(), true))

			assert.True(t, failedWith(c.want)(r.svc.State()))
			assert.Zero(t, browser.opened())
		})
	}
}

// startHeld runs Start against an issuer that holds discovery, and answers
// once the request has arrived. The channel carries Start's failure.
func startHeld(t *testing.T) (*rig, <-chan connectors.Failure) {
	t.Helper()
	r := newFixedRig(t)
	hold := make(chan struct{})
	t.Cleanup(func() { close(hold) })
	r.issuer.set(func(f *fakeIssuer) { f.holdDiscovery = hold })
	done := make(chan connectors.Failure, 1)
	go func() {
		failure, err := r.svc.Start(t.Context(), true)
		assert.NoError(t, err)
		done <- failure
	}()
	r.issuer.discovering.Await(t, "discovery")
	return r, done
}

// A Disconnect during Start's discovery ends the attempt first: the state is
// SignedOut, and Start answers no failure, since the state says what happened.
func TestASignOutDuringAStartIsNotAFailure(t *testing.T) {
	r, done := startHeld(t)

	require.NoError(t, r.svc.Disconnect(t.Context()))

	assert.Empty(t, testutil.Recv(t, done, "Start's answer"))
	assert.Equal(t, connectors.SignedOut, r.svc.State().Phase)
	assert.Empty(t, r.issuer.authorizeQueries(), "no browser opened")
}

// A Close during Start's discovery is no failed sign-in either.
func TestACloseDuringAStartIsNotAFailure(t *testing.T) {
	r, done := startHeld(t)

	r.svc.Close()

	assert.Empty(t, testutil.Recv(t, done, "Start's answer"))
	assert.NotEqual(t, connectors.Failed, r.svc.State().Phase)
}

// A fresh sign-in may be another account: it connects, with that account's
// subject, email and name in the row and on the state, and a restart restores
// them.
func TestAFreshSignInTakesAnotherAccount(t *testing.T) {
	r := newFixedRig(t)
	first := connect(t, r.svc)
	assert.Equal(t, []string{"user-1", "ada@example.com", "Ada Lovelace"}, []string{first.Subject, first.Email, first.Name})
	require.NoError(t, r.svc.Disconnect(t.Context()))
	r.issuer.set(func(f *fakeIssuer) { f.subject, f.email, f.name = "user-2", "grace@example.com", "Grace Hopper" })

	require.NoError(t, r.svc.Connect(t.Context(), true))

	st := await(t, r.svc, "Connected", inPhase(connectors.Connected))
	want := []string{"user-2", "grace@example.com", "Grace Hopper"}
	assert.Equal(t, want, []string{st.Subject, st.Email, st.Name})
	reg := savedRegistration(t, r)
	assert.Equal(t, want, []string{reg.Subject, reg.Email, reg.Name})
	restored := r.restart().State()
	assert.Equal(t, want, []string{restored.Subject, restored.Email, restored.Name})
}
