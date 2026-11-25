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
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/connectors"
)

// no browser opens: the page must be on GitHub's origin, and the code is shown.
func TestABadCodeAnswerIsRefused(t *testing.T) {
	for name, answer := range map[string]map[string]any{
		"another origin": {"device_code": "d", "user_code": "WDJB-MJHT", "verification_uri": "https://github.example/login/device"},
		"plain http":     {"device_code": "d", "user_code": "WDJB-MJHT", "verification_uri": "http://github.com/login/device"},
		"a bad code":     {"device_code": "d", "user_code": "\x1b[2Jpwned", "verification_uri": "%s/login/device"},
		"no device code": {"user_code": "WDJB-MJHT", "verification_uri": "%s/login/device"},
		"an error":       {"error": "device_flow_disabled"},
	} {
		t.Run(name, func(t *testing.T) {
			r := newRig(t)
			if page, ok := answer["verification_uri"].(string); ok {
				answer["verification_uri"] = strings.Replace(page, "%s", r.github.authURL(), 1)
			}
			r.github.set(func(f *fakeGitHub) { f.code = func(w http.ResponseWriter, _ *http.Request) { writeJSON(w, answer) } })

			require.NoError(t, r.svc.Connect(t.Context()))

			assert.Equal(t, connectors.Refused, r.svc.State().Failure)
			assert.Empty(t, r.browser.opened())
		})
	}
}

// GitHub's 4xx is a refusal; a 5xx, or no answer, is Unavailable.
func TestACodeRequestGitHubDidNotAnswerFails(t *testing.T) {
	for status, want := range map[int]connectors.Failure{
		http.StatusNotFound:           connectors.Refused,
		http.StatusServiceUnavailable: connectors.Unavailable,
	} {
		r := newRig(t)
		r.github.set(func(f *fakeGitHub) { f.code = func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(status) } })

		require.NoError(t, r.svc.Connect(t.Context()))

		assert.Equal(t, want, r.svc.State().Failure, status)
	}

	r := newRig(t)
	r.github.srv.Close()
	require.NoError(t, r.svc.Connect(t.Context()))
	assert.Equal(t, connectors.Unavailable, r.svc.State().Failure)
}

// as GitHub names. GitHub's answers end the sign-in as theirs.
func TestPollingWaitsAsGitHubAsks(t *testing.T) {
	r := newRig(t)
	r.github.set(func(f *fakeGitHub) {
		f.polls = []string{"authorization_pending", "slow_down", "authorization_pending", "slow_down", "access_denied"}
		f.slowDownInterval = 0
	})

	require.NoError(t, r.svc.Connect(t.Context()))
	await(t, r.svc, "Declined", failedWith(connectors.Declined))

	assert.Equal(t, []int{900, 5, 5, 10, 10, 15}, r.paced(), "the code's lifetime, then each wait")
	polls := r.github.pollRequests()
	require.Len(t, polls, 5)
	assert.Equal(t, testClientID, polls[0].Get("client_id"))
	assert.Equal(t, "device-"+r.github.prefix, polls[0].Get("device_code"))
	assert.Equal(t, "urn:ietf:params:oauth:grant-type:device_code", polls[0].Get("grant_type"))
}

// A slow_down that names an interval is waited out at that interval.
func TestSlowDownTakesGitHubsInterval(t *testing.T) {
	r := newRig(t)
	r.github.set(func(f *fakeGitHub) {
		f.polls = []string{"slow_down", "expired_token"}
		f.slowDownInterval = 30
	})

	require.NoError(t, r.svc.Connect(t.Context()))
	await(t, r.svc, "TimedOut", failedWith(connectors.TimedOut))

	assert.Equal(t, []int{900, 5, 30}, r.paced())
}

// endpoint that cannot be reached is Unavailable.
func TestPollingEndsOnGitHubsAnswer(t *testing.T) {
	for poll, want := range map[string]connectors.Failure{
		"expired_token":         connectors.TimedOut,
		"incorrect_device_code": connectors.Refused,
		"access_denied":         connectors.Declined,
	} {
		r := newRig(t)
		r.github.set(func(f *fakeGitHub) { f.polls = []string{poll} })

		require.NoError(t, r.svc.Connect(t.Context()))

		await(t, r.svc, poll, failedWith(want))
	}

	r := newRig(t)
	r.github.set(func(f *fakeGitHub) {
		f.pollAnswer = func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusBadGateway) }
	})
	require.NoError(t, r.svc.Connect(t.Context()))
	await(t, r.svc, "Unavailable", failedWith(connectors.Unavailable))
}

// A code answer that names no interval polls at RFC 8628's five seconds.
func TestNoIntervalPollsEveryFiveSeconds(t *testing.T) {
	r := newRig(t)
	r.github.set(func(f *fakeGitHub) {
		f.code = func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(w, map[string]any{"device_code": "device-x", "user_code": "WDJB-MJHT", "verification_uri": f.srv.URL + "/login/device"})
		}
		f.polls = []string{"access_denied"}
	})

	require.NoError(t, r.svc.Connect(t.Context()))
	await(t, r.svc, "Declined", failedWith(connectors.Declined))

	assert.Equal(t, []int{defaultInterval}, r.paced(), "no lifetime is timed, then one wait")
}

// refused.
func TestAnAnswerThatIsNotOneFails(t *testing.T) {
	for name, c := range map[string]struct {
		set  func(f *fakeGitHub)
		want connectors.Failure
	}{
		"a code answer": {func(f *fakeGitHub) {
			f.code = func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("not json")) }
		}, connectors.Unavailable},
		"a token answer": {func(f *fakeGitHub) {
			f.pollAnswer = func(w http.ResponseWriter, _ *http.Request) { writeJSON(w, map[string]any{"access_token": "short"}) }
		}, connectors.Refused},
		"an account answer": {func(f *fakeGitHub) { f.userBody = "not json" }, connectors.Unavailable},
		"no account answer": {func(f *fakeGitHub) { f.dropUser = true }, connectors.Unavailable},
	} {
		t.Run(name, func(t *testing.T) {
			r := newRig(t)
			r.github.set(c.set)

			require.NoError(t, r.svc.Connect(t.Context()))

			await(t, r.svc, string(c.want), failedWith(c.want))
		})
	}
}
