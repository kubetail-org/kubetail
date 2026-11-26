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
	"bytes"
	"fmt"
	"log/slog"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/safe"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/connectors"
)

const testARN = "arn:aws:iam::123456789012:user/alice"

func TestWithNoEntryItIsSignedOut(t *testing.T) {
	r := newRig(t)
	require.Equal(t, connectors.SignedOut, r.svc.State().Phase)
}

func TestAStoredSessionIsConnectedAsItsPrincipal(t *testing.T) {
	r := newRig(t)
	r.storeSession(entry{Refresh: "refresh-1", DPoPKey: encodeKey(newDPoPKey()), ARN: testARN})

	st := r.restart().State()
	require.Equal(t, connectors.Connected, st.Phase)
	require.Equal(t, testARN, st.Subject)
	require.NotZero(t, st.Session)
}

func TestAnEntryThisBuildCannotUseIsDeleted(t *testing.T) {
	for name, value := range map[string]string{
		"no JSON":    "{",
		"no refresh": jsonEntry(t, entry{V: entryVersion, DPoPKey: encodeKey(newDPoPKey()), ARN: testARN}),
		"no key":     jsonEntry(t, entry{V: entryVersion, Refresh: "r", DPoPKey: "junk", ARN: testARN}),
		"no account": jsonEntry(t, entry{V: entryVersion, Refresh: "r", DPoPKey: encodeKey(newDPoPKey()), ARN: "nobody"}),
		"an old v":   jsonEntry(t, entry{V: 0, Refresh: "r", DPoPKey: encodeKey(newDPoPKey()), ARN: testARN}),
	} {
		t.Run(name, func(t *testing.T) {
			r := newRig(t)
			r.keyring.put(sessionAccount, value)
			require.Equal(t, connectors.SignedOut, r.restart().State().Phase)
			_, ok := r.keyring.entry(sessionAccount)
			require.False(t, ok)
		})
	}
}

func TestAnUnusableEntryTheKeyringWillNotDeleteIsStillSignedOut(t *testing.T) {
	r := newRig(t)
	r.keyring.put(sessionAccount, "{")
	r.keyring.failDelete(sessionAccount, errBoom)

	require.Equal(t, connectors.SignedOut, r.restart().State().Phase)
}

func TestANewerKubetailsEntryIsLeft(t *testing.T) {
	r := newRig(t)
	value := jsonEntry(t, entry{V: entryVersion + 1, Refresh: "r", DPoPKey: encodeKey(newDPoPKey()), ARN: testARN})
	r.keyring.put(sessionAccount, value)

	require.Equal(t, connectors.SignedOut, r.restart().State().Phase)
	got, ok := r.keyring.entry(sessionAccount)
	require.True(t, ok)
	require.Equal(t, value, got)
}

func TestANewerKubetailsEntryOfAnotherShapeIsLeft(t *testing.T) {
	r := newRig(t)
	value := fmt.Sprintf(`{"v":%d,"refresh":{"token":"r"}}`, entryVersion+1)
	r.keyring.put(sessionAccount, value)

	require.Equal(t, connectors.SignedOut, r.restart().State().Phase)
	got, ok := r.keyring.entry(sessionAccount)
	require.True(t, ok)
	require.Equal(t, value, got)
}

func TestAKeyringThatCannotBeReadWritesNothing(t *testing.T) {
	r := newRig(t)
	r.storeSession(entry{Refresh: "refresh-1", DPoPKey: encodeKey(newDPoPKey()), ARN: testARN})
	r.keyring.failGet(errBoom)

	require.Equal(t, connectors.SignedOut, r.restart().State().Phase)
	_, ok := r.keyring.entry(sessionAccount)
	require.True(t, ok)
}

// No token, secret key, code or verifier reaches the log, through a sign-in, a
// refresh, a session AWS ended and a disconnect, and each is forgotten by the
// redactor once cleared. Serial: it swaps the process's logger.
func TestNoCredentialReachesTheLog(t *testing.T) {
	var logs bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(prev) })

	clock := newClock()
	r := newRig(t, withNow(clock.now))
	connect(t, r.svc)
	held, _ := r.stored()
	assert.True(t, safe.HasSecret(held.Refresh), "a held refresh token is registered with the redactor")
	r.restart()
	_, err := r.svc.Retrieve(t.Context())
	require.NoError(t, err)
	r.signin.set(func(f *fakeSignin) {
		f.tokenErr = &apiError{status: http.StatusUnauthorized, typ: "AccessDeniedException", code: "TOKEN_EXPIRED"}
	})
	clock.advance(time.Hour)
	_, err = r.svc.Retrieve(t.Context())
	require.ErrorIs(t, err, ErrSignedOut)
	r.signin.set(func(f *fakeSignin) { f.tokenErr = nil })
	connect(t, r.svc)
	require.NoError(t, r.svc.Disconnect())

	secrets := r.signin.mintedSecrets()
	require.Len(t, secrets, 9)
	for _, secret := range secrets {
		assert.NotContains(t, logs.String(), secret)
		assert.False(t, safe.HasSecret(secret), "%s is forgotten once cleared", secret)
	}
	for _, body := range r.signin.tokenBodies() {
		if body["grantType"] == "authorization_code" {
			assert.NotContains(t, logs.String(), body["codeVerifier"])
			assert.False(t, safe.HasSecret(body["codeVerifier"]))
		}
	}
}
