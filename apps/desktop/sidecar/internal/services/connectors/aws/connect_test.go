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
	"encoding/base64"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/oauth2"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/safe"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/testutil"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/version"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/connectors"
)

func TestConnectOpensAWSsAuthorizePageOnTheLoopback(t *testing.T) {
	browser := &parked{}
	r := newRig(t, withBrowser(browser.open))

	require.NoError(t, r.svc.Connect(t.Context()))
	require.Equal(t, connectors.SigningIn, r.svc.State().Phase)

	opened := browser.opened()
	require.Len(t, opened, 1)
	u, err := url.Parse(opened[0])
	require.NoError(t, err)
	require.Equal(t, r.signin.url()+"/v1/authorize", u.Scheme+"://"+u.Host+u.Path)
	q := u.Query()
	require.Equal(t, "code", q.Get("response_type"))
	require.Equal(t, ClientID, q.Get("client_id"))
	require.Equal(t, "openid", q.Get("scope"))
	require.Equal(t, "SHA-256", q.Get("code_challenge_method"))
	require.NotEmpty(t, q.Get("state"))
	require.NotEmpty(t, q.Get("code_challenge"))
	redirect, err := url.Parse(q.Get("redirect_uri"))
	require.NoError(t, err)
	require.Equal(t, "http", redirect.Scheme)
	require.Equal(t, "127.0.0.1", redirect.Hostname())
	require.Equal(t, "/oauth/callback", redirect.Path)
}

func TestEachAttemptHasItsOwnStateAndChallenge(t *testing.T) {
	browser := &parked{}
	r := newRig(t, withBrowser(browser.open))
	require.NoError(t, r.svc.Connect(t.Context()))
	r.svc.Cancel()
	require.NoError(t, r.svc.Connect(t.Context()))

	opened := browser.opened()
	require.Len(t, opened, 2)
	a, b := queryOf(t, opened[0]), queryOf(t, opened[1])
	require.NotEqual(t, a.Get("state"), b.Get("state"))
	require.NotEqual(t, a.Get("code_challenge"), b.Get("code_challenge"))
	require.NotEqual(t, oauth2.S256ChallengeFromVerifier(a.Get("state")), a.Get("code_challenge"))
}

func queryOf(t *testing.T, raw string) url.Values {
	t.Helper()
	u, err := url.Parse(raw)
	require.NoError(t, err)
	return u.Query()
}

func TestASignInConnectsAsThePrincipalAndKeepsTheSession(t *testing.T) {
	r := newRig(t)

	st := connect(t, r.svc)
	require.Equal(t, testARN, st.Subject)

	bodies := r.signin.tokenBodies()
	require.Len(t, bodies, 1)
	require.Equal(t, "authorization_code", bodies[0]["grantType"])
	require.Equal(t, r.signin.authorizeQueries()[0].Get("redirect_uri"), bodies[0]["redirectUri"])

	e, ok := r.stored()
	require.True(t, ok)
	require.Equal(t, entryVersion, e.V)
	require.Equal(t, testARN, e.ARN)
	require.NotEmpty(t, e.Refresh)
	_, err := parseKey(e.DPoPKey)
	require.NoError(t, err)

	require.Equal(t, connectors.Connected, r.restart().State().Phase)
}

// The signin SDK names itself; Kubetail's agent leads it, once.
func TestASignInNamesKubetailAheadOfTheSDK(t *testing.T) {
	r := newRig(t)
	connect(t, r.svc)

	agents := r.signin.userAgents()
	require.Len(t, agents, 1)
	sdk, ok := strings.CutPrefix(agents[0], version.UserAgent()+" ")
	require.True(t, ok, agents[0])
	assert.Contains(t, sdk, "aws-sdk-go-v2/")
	assert.NotContains(t, sdk, "kubetail-desktop/")
}

func TestASignInTheUserDeclinesIsDeclined(t *testing.T) {
	r := newRig(t, withBrowser(declining))
	require.NoError(t, r.svc.Connect(t.Context()))
	await(t, r.svc, "Declined", failedWith(connectors.Declined))
}

func TestASignInNobodyFinishesTimesOut(t *testing.T) {
	browser := &parked{}
	r := newRig(t, withBrowser(browser.open), withAttemptTimeout(time.Millisecond))
	require.NoError(t, r.svc.Connect(t.Context()))
	await(t, r.svc, "TimedOut", failedWith(connectors.TimedOut))
}

func TestABrowserThatWillNotOpenIsRefused(t *testing.T) {
	r := newRig(t, withBrowser(func(string) error { return errBoom }))
	require.NoError(t, r.svc.Connect(t.Context()))
	require.Equal(t, connectors.Failed, r.svc.State().Phase)
	require.Equal(t, connectors.Refused, r.svc.State().Failure)
}

func TestARedirectWithTwoCodesIsRefused(t *testing.T) {
	r := newRig(t, withBrowser(func(raw string) error {
		q := queryOf(t, raw)
		return following(q.Get("redirect_uri") + "?" + url.Values{"state": {q.Get("state")}, "code": {"a", "b"}}.Encode())
	}))
	require.NoError(t, r.svc.Connect(t.Context()))
	await(t, r.svc, "Refused", failedWith(connectors.Refused))
	require.Empty(t, r.signin.tokenBodies())
}

func TestAnExchangeAWSRefusesIsRefused(t *testing.T) {
	r := newRig(t)
	r.signin.set(func(f *fakeSignin) {
		f.tokenErr = &apiError{status: http.StatusBadRequest, typ: "InvalidGrantException", code: "invalid_grant"}
	})
	require.NoError(t, r.svc.Connect(t.Context()))
	await(t, r.svc, "Refused", failedWith(connectors.Refused))
	_, ok := r.stored()
	require.False(t, ok)
}

func TestAnExchangeThatNeverReachesAWSIsUnavailable(t *testing.T) {
	r := newRig(t)
	r.signin.set(func(f *fakeSignin) { f.tokenErr = &apiError{dropConn: true} })
	require.NoError(t, r.svc.Connect(t.Context()))
	await(t, r.svc, "Unavailable", failedWith(connectors.Unavailable))
}

func TestAnIDTokenNamingNoAccountIsRefused(t *testing.T) {
	r := newRig(t)
	r.signin.set(func(f *fakeSignin) { f.subject = "nobody" })
	require.NoError(t, r.svc.Connect(t.Context()))
	await(t, r.svc, "Refused", failedWith(connectors.Refused))
	_, ok := r.stored()
	require.False(t, ok)
}

func TestAKeyringThatCannotHoldASessionOpensNoBrowser(t *testing.T) {
	browser := &parked{}
	r := newRig(t, withBrowser(browser.open))
	r.keyring.failSet(checkAccount, errBoom)

	require.NoError(t, r.svc.Connect(t.Context()))
	require.Equal(t, connectors.NotSaved, r.svc.State().Failure)
	require.Empty(t, browser.opened())
}

func TestASessionTheKeyringWillNotTakeIsNotSaved(t *testing.T) {
	r := newRig(t)
	r.keyring.failSet(sessionAccount, errBoom)
	require.NoError(t, r.svc.Connect(t.Context()))
	await(t, r.svc, "NotSaved", failedWith(connectors.NotSaved))
}

func TestConnectIsRefusedDuringAnAttemptAndWhileConnected(t *testing.T) {
	browser := &parked{}
	r := newRig(t, withBrowser(browser.open))
	require.NoError(t, r.svc.Connect(t.Context()))
	require.ErrorIs(t, r.svc.Connect(t.Context()), connectors.ErrSigningIn)

	r.storeSession(entry{Refresh: "refresh-1", DPoPKey: encodeKey(newDPoPKey()), ARN: testARN})
	require.ErrorIs(t, r.restart().Connect(t.Context()), connectors.ErrConnected)
}

func TestCancelEndsTheWait(t *testing.T) {
	browser := &parked{}
	r := newRig(t, withBrowser(browser.open))
	require.NoError(t, r.svc.Connect(t.Context()))
	r.svc.Cancel()
	require.Equal(t, connectors.SignedOut, r.svc.State().Phase)
	r.svc.Close() // joins the attempt's goroutine, which the cancel ended
	require.Empty(t, r.signin.tokenBodies())
}

func TestCancelOutsideAnAttemptChangesNothing(t *testing.T) {
	r := newRig(t)
	before := r.svc.State()
	r.svc.Cancel()
	require.Equal(t, before, r.svc.State())
}

func TestTokensThatArriveAfterACancelAreDropped(t *testing.T) {
	ended := testutil.NewProbe[struct{}](1)
	r := newRig(t, withAttemptEnded(func() { ended.Fire(struct{}{}) }))
	hold := make(chan struct{})
	r.signin.set(func(f *fakeSignin) { f.hold = hold })
	require.NoError(t, r.svc.Connect(t.Context()))
	r.signin.arrived.Await(t, "the exchange")
	r.svc.Cancel()
	close(hold)
	ended.Await(t, "the attempt's end")

	require.Equal(t, connectors.SignedOut, r.svc.State().Phase)
	_, ok := r.stored()
	require.False(t, ok)
}

func TestARedirectErrorOtherThanADeclineIsRefused(t *testing.T) {
	r := newRig(t, withBrowser(func(raw string) error {
		q := queryOf(t, raw)
		return following(q.Get("redirect_uri") + "?" + url.Values{"error": {"server_error"}, "state": {q.Get("state")}}.Encode())
	}))
	require.NoError(t, r.svc.Connect(t.Context()))
	await(t, r.svc, "Refused", failedWith(connectors.Refused))
}

func TestAnIncompleteTokenAnswerIsUnavailable(t *testing.T) {
	for name, answer := range map[string]string{
		"no session token": `{"accessToken":{"accessKeyId":"A","secretAccessKey":"S","sessionToken":""},"expiresIn":900,"refreshToken":"R","tokenType":"aws_sigv4"}`,
		"no lifetime":      `{"accessToken":{"accessKeyId":"A","secretAccessKey":"S","sessionToken":"T"},"refreshToken":"R","tokenType":"aws_sigv4"}`,
	} {
		t.Run(name, func(t *testing.T) {
			r := newRig(t)
			r.signin.set(func(f *fakeSignin) {
				f.answer = func(w http.ResponseWriter, _ *http.Request) {
					w.Header().Set("Content-Type", "application/json")
					_, _ = w.Write([]byte(answer))
				}
			})
			require.NoError(t, r.svc.Connect(t.Context()))
			await(t, r.svc, "Unavailable", failedWith(connectors.Unavailable))
		})
	}
}

func TestPrincipalOfReadsAnARNNamingAnAccount(t *testing.T) {
	jwt := func(payload string) string {
		return "e30." + base64.RawURLEncoding.EncodeToString([]byte(payload)) + ".c2ln"
	}

	got, err := principalOf(jwt(`{"sub":"arn:aws:sts::123456789012:assumed-role/Admin/alice"}`))
	require.NoError(t, err)
	require.Equal(t, "arn:aws:sts::123456789012:assumed-role/Admin/alice", got)

	for name, token := range map[string]string{
		"no JWT":          "a.b",
		"no base64":       "e30.!!!.c2ln",
		"no JSON":         jwt(`{`),
		"no ARN":          jwt(`{"sub":"alice"}`),
		"no account":      jwt(`{"sub":"arn:aws:s3:::bucket"}`),
		"an empty claims": jwt(`{}`),
	} {
		_, err := principalOf(token)
		require.Error(t, err, name)
	}
}

// A sign-in forgets its state and verifier before it publishes Connected. The
// session's write is the last step before that.
func TestASignInForgetsItsSecretsBeforeItConnects(t *testing.T) {
	r := newRig(t)
	r.keyring.onSet(func(account string) {
		if account != sessionAccount {
			return
		}
		assert.False(t, safe.HasSecret(r.signin.authorizeQueries()[0].Get("state")), "the state")
		assert.False(t, safe.HasSecret(r.signin.tokenBodies()[0]["codeVerifier"]), "the verifier")
	})
	connect(t, r.svc)
}
