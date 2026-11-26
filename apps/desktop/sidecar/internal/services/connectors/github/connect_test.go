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
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/safe"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/testutil"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/connectors"
)

// A build that names no OAuth App refuses Connect and asks GitHub nothing.
func TestConnectWithNoClientIsRefused(t *testing.T) {
	r := newRig(t)
	r.clientID = ""
	s := r.restart()

	require.ErrorIs(t, s.Connect(t.Context()), connectors.ErrNoClient)

	assert.Equal(t, connectors.SignedOut, s.State().Phase)
	assert.Empty(t, r.github.codeRequests())
}

// A keyring that cannot take a write fails the sign-in NotSaved before GitHub

// is asked for a code, so GitHub grants no token this machine cannot keep.
func TestNoCodeIsAskedForWithoutAKeyring(t *testing.T) {
	r := newRig(t)
	r.keyring.failSet(checkAccount, errBoom)

	require.NoError(t, r.svc.Connect(t.Context()))

	assert.Equal(t, connectors.State{Phase: connectors.Failed, Failure: connectors.NotSaved, Session: r.svc.State().Session}, r.svc.State())
	assert.Empty(t, r.github.codeRequests())
	assert.Empty(t, r.browser.opened())
}

// Connect is refused during a sign-in and while connected.
func TestConnectIsRefusedDuringASignInAndWhileConnected(t *testing.T) {
	r := newRig(t)
	holdGrant(t, r)

	require.NoError(t, r.svc.Connect(t.Context()))
	require.ErrorIs(t, r.svc.Connect(t.Context()), connectors.ErrSigningIn)

	r.keyring.put(tokenAccount, storedEntry(storedToken, "octocat"))
	s := r.restart()
	require.ErrorIs(t, s.Connect(t.Context()), connectors.ErrConnected)
}

// holdGrant parks GitHub's grant until the test ends, so the sign-in stays

// under way.
func holdGrant(t *testing.T, r *rig) {
	t.Helper()
	hold := make(chan struct{})
	r.github.set(func(f *fakeGitHub) { f.holdPoll = hold })
	t.Cleanup(func() { close(hold) })
}

// A sign-in asks GitHub for a code under the OAuth App and its scopes, shows

// the code, then opens the browser on GitHub's page.
func TestASignInShowsTheCodeAndOpensGitHubsPage(t *testing.T) {
	r := newRig(t)
	holdGrant(t, r)

	require.NoError(t, r.svc.Connect(t.Context()))

	codes := r.github.codeRequests()
	require.Len(t, codes, 1)
	assert.Equal(t, testClientID, codes[0].Get("client_id"))
	assert.Equal(t, "repo read:org workflow", codes[0].Get("scope"))
	assert.Equal(t, []string{r.github.authURL() + "/login/device"}, r.browser.opened())
	st := r.svc.State()
	assert.Equal(t, connectors.SigningIn, st.Phase)
	assert.Equal(t, "WDJB-MJHT", st.UserCode)
}

// A code answer that is not GitHub's whole, well-formed answer is refused, and

// A browser that cannot open fails the sign-in Refused.
func TestABrowserThatCannotOpenIsRefused(t *testing.T) {
	r := newRig(t)
	r.browser.err = errBoom

	require.NoError(t, r.svc.Connect(t.Context()))

	assert.Equal(t, connectors.State{Phase: connectors.Failed, Failure: connectors.Refused, Session: r.svc.State().Session}, r.svc.State())
}

// The token endpoint is polled under the device code, once an interval: a

// pending answer waits as long again, slow_down five seconds longer, or as long

// An expired code times out, any other error is a refusal, and a token

// A sign-in nobody finishes times out.
func TestASignInNobodyFinishesTimesOut(t *testing.T) {
	r := newRig(t, withAttemptTimeout(time.Millisecond))
	r.github.set(func(f *fakeGitHub) {
		f.pollAnswer = func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(w, map[string]any{"error": "authorization_pending"})
		}
	})

	require.NoError(t, r.svc.Connect(t.Context()))

	await(t, r.svc, "TimedOut", failedWith(connectors.TimedOut))
}

// A granted token names its account through /user, lands in the keyring with

// it, and only then is Connected; the code is no longer shown.
func TestASignInConnects(t *testing.T) {
	r := newRig(t)
	r.github.set(func(f *fakeGitHub) { f.polls = []string{"authorization_pending"} })

	st := connect(t, r.svc)

	minted := r.github.mintedTokens()
	require.Len(t, minted, 1)
	assert.Equal(t, minted, r.github.userRequests())
	assert.Equal(t, "octocat", st.Name)
	assert.Empty(t, st.UserCode)
	e, ok := r.stored()
	require.True(t, ok)
	assert.Equal(t, entry{V: entryVersion, Token: minted[0], Login: "octocat"}, e)
	assert.True(t, safe.HasSecret(minted[0]))
	tok, err := r.svc.Token(t.Context())
	require.NoError(t, err)
	assert.Equal(t, minted[0], tok)
	_, checked := r.keyring.entry(checkAccount)
	assert.False(t, checked, "the check's entry is deleted")
}

// A token whose account cannot be named is not kept: /user's 401, or a login

// that is not one, is a refusal, and any other answer Unavailable.
func TestATokenWithNoAccountIsNotKept(t *testing.T) {
	for name, steer := range map[string]struct {
		set  func(f *fakeGitHub)
		want connectors.Failure
	}{
		"401":         {func(f *fakeGitHub) { f.userStatus = http.StatusUnauthorized }, connectors.Refused},
		"503":         {func(f *fakeGitHub) { f.userStatus = http.StatusServiceUnavailable }, connectors.Unavailable},
		"a bad login": {func(f *fakeGitHub) { f.login = "\x1b]8;;evil" }, connectors.Refused},
	} {
		t.Run(name, func(t *testing.T) {
			r := newRig(t)
			r.github.set(steer.set)

			require.NoError(t, r.svc.Connect(t.Context()))
			await(t, r.svc, string(steer.want), failedWith(steer.want))

			_, ok := r.stored()
			assert.False(t, ok)
			assert.False(t, safe.HasSecret(r.github.mintedTokens()[0]))
		})
	}
}

// A token the keyring will not take fails NotSaved and is forgotten.
func TestATokenTheKeyringRefusesIsNotSaved(t *testing.T) {
	r := newRig(t)
	r.keyring.failSet(tokenAccount, errBoom)

	require.NoError(t, r.svc.Connect(t.Context()))
	await(t, r.svc, "NotSaved", failedWith(connectors.NotSaved))

	assert.False(t, safe.HasSecret(r.github.mintedTokens()[0]))
}

// A token GitHub grants after a Cancel is dropped, never saved.
func TestATokenAfterACancelIsDropped(t *testing.T) {
	r := newRig(t)
	hold := make(chan struct{})
	r.github.set(func(f *fakeGitHub) { f.holdUser = hold })

	require.NoError(t, r.svc.Connect(t.Context()))
	r.github.arrived.Await(t, "the account's lookup")
	r.svc.Cancel()
	assert.Equal(t, connectors.SignedOut, r.svc.State().Phase)
	close(hold)
	r.attemptEnded.Await(t, "the attempt")

	_, ok := r.stored()
	assert.False(t, ok)
	assert.Equal(t, connectors.SignedOut, r.svc.State().Phase)
	assert.False(t, safe.HasSecret(r.github.mintedTokens()[0]))
}

// A Cancel while GitHub is issuing the code ends the request: no code is

// shown, no browser opens, and the request's failure is not the state's.
func TestACancelBeforeTheCodeOpensNothing(t *testing.T) {
	r := newRig(t)
	hold := make(chan struct{})
	r.github.set(func(f *fakeGitHub) {
		code := f.deviceCode
		f.code = func(w http.ResponseWriter, req *http.Request) {
			f.arrived.Fire("code")
			<-hold
			f.mu.Lock()
			f.code = nil
			f.mu.Unlock()
			code(w, req)
		}
	})

	done := make(chan error, 1)
	go func() { done <- r.svc.Connect(t.Context()) }()
	r.github.arrived.Await(t, "the code request")
	r.svc.Cancel()
	close(hold)

	require.NoError(t, testutil.Recv(t, done, "Connect"))
	assert.Equal(t, connectors.SignedOut, r.svc.State().Phase)
	assert.Empty(t, r.svc.State().UserCode)
	assert.Empty(t, r.browser.opened())
}

// A Cancel while a poll is in flight ends the polling with no failure.
func TestACancelDuringAPollEndsIt(t *testing.T) {
	r := newRig(t)
	holdGrant(t, r)
	require.NoError(t, r.svc.Connect(t.Context()))
	r.github.arrived.Await(t, "the poll")

	r.svc.Cancel()
	r.attemptEnded.Await(t, "the attempt")

	assert.Equal(t, connectors.SignedOut, r.svc.State().Phase)
	_, ok := r.stored()
	assert.False(t, ok)
}

// Cancel with no sign-in under way changes nothing.
func TestCancelWithNoSignInChangesNothing(t *testing.T) {
	r := newRig(t)
	before := r.svc.State()

	r.svc.Cancel()

	assert.Equal(t, before, r.svc.State())
}

// A sign-in forgets its device code before it publishes Connected. The
// token's write is the last step before that.
func TestASignInForgetsItsDeviceCodeBeforeItConnects(t *testing.T) {
	r := newRig(t)
	r.keyring.onSet(func(account string) {
		if account != tokenAccount {
			return
		}
		assert.False(t, safe.HasSecret(r.github.pollRequests()[0].Get("device_code")))
	})
	connect(t, r.svc)
}
