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
	"bytes"
	"log/slog"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/safe"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/connectors"
)

// No token or device code reaches the log. The device code, which with the
// public client id claims the token, is registered with the redactor while
// its sign-in polls, and each is forgotten once cleared.
func TestNoTokenLeavesTheKeyring(t *testing.T) {
	var logs bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(prev) })

	r := newRig(t)
	hold := make(chan struct{})
	r.github.set(func(f *fakeGitHub) { f.holdPoll = hold })
	require.NoError(t, r.svc.Connect(t.Context()))
	r.github.arrived.Await(t, "the granting poll")
	deviceCode := r.github.pollRequests()[0].Get("device_code")
	assert.True(t, safe.HasSecret(deviceCode), "a polling device code is registered")
	r.github.set(func(f *fakeGitHub) { f.holdPoll = nil })
	close(hold)
	await(t, r.svc, "Connected", inPhase(connectors.Connected))

	r.github.set(func(f *fakeGitHub) { f.userStatus = http.StatusUnauthorized })
	r.svc.Refused(r.github.mintedTokens()[0])
	await(t, r.svc, "KeyRejected", failedWith(connectors.KeyRejected))
	r.github.set(func(f *fakeGitHub) { f.userStatus = 0 })
	connect(t, r.svc)
	require.NoError(t, r.svc.Disconnect())
	r.svc.Close()

	tokens := r.github.mintedTokens()
	require.Len(t, tokens, 2)
	for _, secret := range append(tokens, deviceCode) {
		assert.NotContains(t, logs.String(), secret)
		assert.False(t, safe.HasSecret(secret), "%s is forgotten once cleared", secret)
	}
}

// A URL that does not parse fails New.
func TestNewRefusesABadURL(t *testing.T) {
	for _, cfg := range []Config{
		{AuthURL: "http://[::1", APIURL: APIURL},
		{AuthURL: AuthURL, APIURL: "http://[::1"},
	} {
		_, err := New(cfg)
		require.ErrorContains(t, err, "http://[::1")
	}
}

// GitHub's intervals are seconds.
func TestGitHubsIntervalsAreSeconds(t *testing.T) {
	s, err := newWithOptions(Config{AuthURL: AuthURL, APIURL: APIURL, Keyring: newFakeKeyring()})
	require.NoError(t, err)
	t.Cleanup(s.Close)

	assert.Equal(t, 5*time.Second, s.pace(5))
}
