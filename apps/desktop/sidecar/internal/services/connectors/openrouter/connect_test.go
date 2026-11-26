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
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/oauth2"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/safe"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/testutil"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/llm"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/connectors"
)

// A keyring that cannot take a write fails the sign-in NotSaved before any
// browser opens, so OpenRouter issues no key this machine cannot keep.
func TestNoBrowserOpensWithoutAKeyring(t *testing.T) {
	browser := &parked{}
	r := newRig(t, withBrowser(browser.open))
	r.keyring.failSet(checkAccount, errBoom)

	require.NoError(t, r.svc.Connect(t.Context()))

	assert.Equal(t, connectors.State{Phase: connectors.Failed, Failure: connectors.NotSaved, Session: r.svc.State().Session}, r.svc.State())
	assert.Empty(t, browser.opened())
}

// A sign-in opens OpenRouter's auth page with an S256 challenge, the key's
// label and a loopback callback carrying our state; the code it redirects with
// is exchanged under the matching verifier, and the key lands in the keyring
// before Connected.
func TestASignInConnects(t *testing.T) {
	r := newRig(t)

	st := connect(t, r.svc)

	auths := r.openRouter.authQueries()
	require.Len(t, auths, 1)
	q := auths[0]
	assert.Equal(t, "S256", q.Get("code_challenge_method"))
	assert.Equal(t, KeyLabel, q.Get("key_label"))
	callback, err := url.Parse(q.Get("callback_url"))
	require.NoError(t, err)
	assert.Equal(t, "http", callback.Scheme)
	assert.Equal(t, "127.0.0.1", callback.Hostname())
	assert.Equal(t, callbackPath, callback.Path)
	assert.NotEmpty(t, callback.Query().Get("state"))

	exchanges := r.openRouter.exchangeBodies()
	require.Len(t, exchanges, 1)
	assert.Equal(t, "code-1", exchanges[0]["code"])
	assert.Equal(t, q.Get("code_challenge"), oauth2.S256ChallengeFromVerifier(exchanges[0]["code_verifier"]))
	assert.Equal(t, "S256", exchanges[0]["code_challenge_method"])

	key, ok := r.storedKey()
	require.True(t, ok)
	assert.Equal(t, r.openRouter.mintedKeys(), []string{key})
	assert.NotZero(t, st.Session)
	signed, err := r.signedKey(r.svc)
	require.NoError(t, err)
	assert.Equal(t, key, signed)
	_, checked := r.keyring.entry(checkAccount)
	assert.False(t, checked, "the check's entry is deleted")
}

// A stored key survives a restart.
func TestAKeySurvivesARestart(t *testing.T) {
	r := newRig(t)
	connect(t, r.svc)
	key, _ := r.storedKey()

	s := r.restart()

	assert.Equal(t, connectors.Connected, s.State().Phase)
	signed, err := r.signedKey(s)
	require.NoError(t, err)
	assert.Equal(t, key, signed)
}

// Connect is refused during an attempt and while connected; neither refusal
// opens a browser.
func TestConnectIsRefusedDuringAnAttemptAndWhileConnected(t *testing.T) {
	browser := &parked{}
	r := newRig(t, withBrowser(browser.open))
	require.NoError(t, r.svc.Connect(t.Context()))
	require.Equal(t, connectors.SigningIn, r.svc.State().Phase)

	assert.ErrorIs(t, r.svc.Connect(t.Context()), connectors.ErrSigningIn)
	assert.Len(t, browser.opened(), 1)

	r.keyring.put(keyAccount, `{"v":1,"key":"stored-openrouter-key-0001"}`)
	s := r.restart()
	assert.ErrorIs(t, s.Connect(t.Context()), connectors.ErrConnected)
	assert.Len(t, browser.opened(), 1)
}

// callbackOf is the loopback callback the auth page was opened with.
func callbackOf(t *testing.T, authPage string) *url.URL {
	t.Helper()
	u, err := url.Parse(authPage)
	require.NoError(t, err)
	callback, err := url.Parse(u.Query().Get("callback_url"))
	require.NoError(t, err)
	return callback
}

// hit sends the callback query q, answering the status, or 0 when nothing
// answered.
func hit(t *testing.T, callback *url.URL, q url.Values) int {
	t.Helper()
	u := *callback
	u.RawQuery = q.Encode()
	resp, err := http.Get(u.String())
	if err != nil {
		return 0
	}
	resp.Body.Close()
	return resp.StatusCode
}

// Only a request carrying our state can end the attempt: a wrong or missing
// state, or our state with no code, is a 400 that leaves it pending; our state
// with an error, or with two codes, fails it Refused with nothing exchanged;
// one code is exchanged; a request after the end finds no listener.
func TestTheCallbackTable(t *testing.T) {
	for _, tc := range []struct {
		name string
		q    func(state string) url.Values
		// ended is the state that follows, nil while the attempt stays pending.
		// An attempt that ends closes its listener, which may cut the answer.
		ended func(connectors.State) bool
	}{
		{"wrong state", func(string) url.Values { return url.Values{"state": {"nope"}, "code": {"c"}} }, nil},
		{"no state", func(string) url.Values { return url.Values{"code": {"c"}} }, nil},
		{"wrong state with an error", func(string) url.Values { return url.Values{"state": {"nope"}, "error": {"x"}} }, nil},
		{"no code", func(s string) url.Values { return url.Values{"state": {s}} }, nil},
		{"an empty code", func(s string) url.Values { return url.Values{"state": {s}, "code": {""}} }, nil},
		{"an error", func(s string) url.Values { return url.Values{"state": {s}, "error": {"access_denied"}} }, failedWith(connectors.Refused)},
		{"two codes", func(s string) url.Values { return url.Values{"state": {s}, "code": {"code-1", "code-2"}} }, failedWith(connectors.Refused)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			browser := &parked{}
			r := newRig(t, withBrowser(browser.open))
			require.NoError(t, r.svc.Connect(t.Context()))
			callback := callbackOf(t, browser.opened()[0])
			state := callback.Query().Get("state")

			status := hit(t, callback, tc.q(state))

			if tc.ended == nil {
				assert.Equal(t, http.StatusBadRequest, status)
				assert.Equal(t, connectors.SigningIn, r.svc.State().Phase)
				assert.Empty(t, r.openRouter.exchangeBodies())
				return
			}
			await(t, r.svc, tc.name, tc.ended)
			assert.Empty(t, r.openRouter.exchangeBodies())
		})
	}
}

// One code with our state is exchanged, and the callback answers once: a
// later request finds the attempt over.
func TestTheCallbackAnswersOnce(t *testing.T) {
	browser := &parked{}
	r := newRig(t, withBrowser(browser.open))
	require.NoError(t, r.svc.Connect(t.Context()))
	callback := callbackOf(t, browser.opened()[0])
	state := callback.Query().Get("state")
	// The auth page mints the code the exchange honours.
	resp, err := (&http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}).Get(browser.opened()[0])
	require.NoError(t, err)
	resp.Body.Close()

	hit(t, callback, url.Values{"state": {state}, "code": {"code-1"}})
	await(t, r.svc, "Connected", inPhase(connectors.Connected))

	assert.NotEqual(t, http.StatusOK, hit(t, callback, url.Values{"state": {state}, "code": {"code-1"}}))
	assert.Len(t, r.openRouter.exchangeBodies(), 1)
}

// Only a 200 whose JSON names a non-empty key connects. Every other answer
// saves no entry: a 4xx is Refused, and no answer, a transport error, a
// redirect, a 5xx or a body with no key is Unavailable.
func TestTheExchangeTable(t *testing.T) {
	for _, tc := range []struct {
		name   string
		answer http.HandlerFunc
		want   connectors.Failure
	}{
		{"a 400", status(http.StatusBadRequest, `{"error":"bad"}`), connectors.Refused},
		{"a 403", status(http.StatusForbidden, ``), connectors.Refused},
		{"a 500", status(http.StatusInternalServerError, ``), connectors.Unavailable},
		{"a 503", status(http.StatusServiceUnavailable, `{"key":"sk-or-v1-from-a-503"}`), connectors.Unavailable},
		{"a redirect", func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, "/elsewhere", http.StatusTemporaryRedirect)
		}, connectors.Unavailable},
		{"not JSON", status(http.StatusOK, `sk-or-v1-plain`), connectors.Unavailable},
		{"no key", status(http.StatusOK, `{"user_id":"u"}`), connectors.Unavailable},
		{"an empty key", status(http.StatusOK, `{"key":""}`), connectors.Unavailable},
		{"a key that is no string", status(http.StatusOK, `{"key":42}`), connectors.Unavailable},
		{"JSON with trailing bytes", status(http.StatusOK, `{"key":"sk-or-v1-trailing"} junk`), connectors.Unavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newRig(t)
			r.openRouter.set(func(f *fakeOpenRouter) { f.exchange = tc.answer })

			require.NoError(t, r.svc.Connect(t.Context()))

			await(t, r.svc, string(tc.want), failedWith(tc.want))
			_, ok := r.keyring.entry(keyAccount)
			assert.False(t, ok)
		})
	}
}

func status(code int, body string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(code)
		_, _ = w.Write([]byte(body))
	}
}

// An exchange that never answers within its bound is Unavailable.
func TestAStalledExchangeIsUnavailable(t *testing.T) {
	r := newRig(t, withIssuerTimeout(time.Millisecond))
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	r.openRouter.set(func(f *fakeOpenRouter) { f.hold = release })

	require.NoError(t, r.svc.Connect(t.Context()))

	await(t, r.svc, "Unavailable", failedWith(connectors.Unavailable))
}

// An exchange that cannot reach OpenRouter is Unavailable.
func TestAnUnreachableExchangeIsUnavailable(t *testing.T) {
	r := newRig(t)
	r.openRouter.set(func(f *fakeOpenRouter) {
		f.exchange = func(w http.ResponseWriter, _ *http.Request) {
			conn, _, err := w.(http.Hijacker).Hijack()
			require.NoError(t, err)
			conn.Close()
		}
	})

	require.NoError(t, r.svc.Connect(t.Context()))

	await(t, r.svc, "Unavailable", failedWith(connectors.Unavailable))
}

// Nobody finishing the sign-in in time is TimedOut.
func TestASignInTimesOut(t *testing.T) {
	browser := &parked{}
	r := newRig(t, withBrowser(browser.open), withAttemptTimeout(time.Millisecond))

	require.NoError(t, r.svc.Connect(t.Context()))

	await(t, r.svc, "TimedOut", failedWith(connectors.TimedOut))
}

// A browser that cannot open fails the attempt Refused.
func TestABrowserThatCannotOpenIsRefused(t *testing.T) {
	r := newRig(t, withBrowser(func(string) error { return errBoom }))

	require.NoError(t, r.svc.Connect(t.Context()))

	assert.Equal(t, connectors.Failed, r.svc.State().Phase)
	assert.Equal(t, connectors.Refused, r.svc.State().Failure)
}

// Cancel before the callback signs out, and Cancel outside a sign-in does
// nothing.
func TestCancel(t *testing.T) {
	browser := &parked{}
	r := newRig(t, withBrowser(browser.open))
	r.svc.Cancel()
	assert.Equal(t, connectors.SignedOut, r.svc.State().Phase)
	require.NoError(t, r.svc.Connect(t.Context()))
	before := r.svc.State().Session

	r.svc.Cancel()

	st := r.svc.State()
	assert.Equal(t, connectors.SignedOut, st.Phase)
	assert.Greater(t, st.Session, before)
	callback := callbackOf(t, browser.opened()[0])
	assert.NotEqual(t, http.StatusOK, hit(t, callback, url.Values{"state": {callback.Query().Get("state")}, "code": {"c"}}))
}

// A key that arrives after a Cancel is dropped, never saved, and the next
// attempt is undisturbed.
func TestAKeyThatArrivesLateIsDropped(t *testing.T) {
	r := newRig(t)
	release := make(chan struct{})
	r.openRouter.set(func(f *fakeOpenRouter) { f.hold = release })
	require.NoError(t, r.svc.Connect(t.Context()))
	r.openRouter.arrived.Await(t, "the exchange")

	r.svc.Cancel()
	r.openRouter.set(func(f *fakeOpenRouter) { f.hold = nil })
	close(release)
	require.Eventually(t, func() bool { return len(r.openRouter.mintedKeys()) == 1 }, testutil.Timeout, time.Millisecond, "the late key")
	connect(t, r.svc)

	minted := r.openRouter.mintedKeys()
	require.Len(t, minted, 2)
	key, ok := r.storedKey()
	require.True(t, ok)
	assert.Equal(t, minted[1], key)
}

// A key the keyring will not take fails NotSaved and is not held.
func TestAKeyThatCannotBeSavedIsNotKept(t *testing.T) {
	r := newRig(t)
	r.keyring.failSet(keyAccount, errBoom)

	require.NoError(t, r.svc.Connect(t.Context()))

	await(t, r.svc, "NotSaved", failedWith(connectors.NotSaved))
	_, err := r.signedKey(r.svc)
	assert.ErrorIs(t, err, llm.ErrSignedOut)
}

// A sign-in forgets its state, verifier and challenge before it publishes
// Connected. The key's write is the last step before that.
func TestASignInForgetsItsSecretsBeforeItConnects(t *testing.T) {
	r := newRig(t)
	r.keyring.onSet(func(account string) {
		if account != keyAccount {
			return
		}
		auth := r.openRouter.authQueries()[0]
		callback, err := url.Parse(auth.Get("callback_url"))
		assert.NoError(t, err)
		assert.False(t, safe.HasSecret(callback.Query().Get("state")), "the state")
		assert.False(t, safe.HasSecret(auth.Get("code_challenge")), "the challenge")
		assert.False(t, safe.HasSecret(r.openRouter.exchangeBodies()[0]["code_verifier"]), "the verifier")
	})
	connect(t, r.svc)
}
