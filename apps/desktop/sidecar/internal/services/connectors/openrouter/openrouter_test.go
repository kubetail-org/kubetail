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
	"bytes"
	"log/slog"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/safe"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/testutil"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/connectors"
)

// A first launch holds no key: signed out, and the keyring untouched.
func TestAFirstLaunchIsSignedOut(t *testing.T) {
	r := newRig(t)

	assert.Equal(t, connectors.SignedOut, r.svc.State().Phase)
	assert.Zero(t, r.keyring.sets)
}

// A launch reads the entry and reaches no network: a key is Connected and
// registered with the redactor; no entry, or one that does not parse, is
// signed out, and the unparseable one is deleted.
func TestNewRestoresWhatTheKeyringHolds(t *testing.T) {
	r := newRig(t)
	r.keyring.put(keyAccount, `{"v":1,"key":"stored-openrouter-key-0001"}`)

	s := r.restart()

	assert.Equal(t, connectors.Connected, s.State().Phase)
	assert.NotZero(t, s.State().Session)
	assert.True(t, safe.HasSecret("stored-openrouter-key-0001"))
	tok, err := r.signedKey(s)
	require.NoError(t, err)
	assert.Equal(t, "stored-openrouter-key-0001", tok)
	assert.Empty(t, r.openRouter.keyChecks())

	for _, bad := range []string{`not json`, `{"v":1}`, `{"v":1,"key":""}`, `{"v":0,"key":"sk-or-v1-old"}`, `{"v":1,"key":42}`} {
		r.keyring.put(keyAccount, bad)

		s = r.restart()

		assert.Equal(t, connectors.SignedOut, s.State().Phase, bad)
		_, ok := r.keyring.entry(keyAccount)
		assert.False(t, ok, "%s is deleted", bad)
	}
}

// An entry a newer Kubetail wrote is left in place: that Kubetail reads it again,
// and deleting it would strand a live key on OpenRouter.
// Its version is read first, so a shape this build cannot decode is left too.
func TestANewerEntryIsLeft(t *testing.T) {
	for _, newer := range []string{
		`{"v":2,"key":"sk-or-v1-newer","label":"x"}`,
		`{"v":2,"key":{"id":"k1","secret":"sk-or-v1-newer"}}`,
	} {
		r := newRig(t)
		r.keyring.put(keyAccount, newer)

		s := r.restart()

		assert.Equal(t, connectors.SignedOut, s.State().Phase, newer)
		v, ok := r.keyring.entry(keyAccount)
		assert.True(t, ok, newer)
		assert.Equal(t, newer, v)
	}
}

// A keyring that cannot be read is no proof of a sign-out: signed out, and
// nothing written or deleted.
func TestAnUnreadableKeyringWritesNothing(t *testing.T) {
	r := newRig(t)
	r.keyring.put(keyAccount, `{"v":1,"key":"stored-openrouter-key-0001"}`)
	r.keyring.failGet(errBoom)

	s := r.restart()

	assert.Equal(t, connectors.SignedOut, s.State().Phase)
	_, ok := r.keyring.entry(keyAccount)
	assert.True(t, ok)
}

// An entry that does not parse and cannot be deleted is left for the next
// launch, signed out.
func TestAnUnreadableEntryNewCannotDelete(t *testing.T) {
	r := newRig(t)
	r.keyring.put(keyAccount, `not json`)
	r.keyring.failDelete(keyAccount, errBoom)

	s := r.restart()

	assert.Equal(t, connectors.SignedOut, s.State().Phase)
	_, ok := r.keyring.entry(keyAccount)
	assert.True(t, ok)
}

// New refuses an auth origin or an API base that does not parse.
func TestNewRefusesABadURL(t *testing.T) {
	_, err := New(Config{AuthURL: "http://[::1", APIBaseURL: APIBaseURL})
	assert.Error(t, err)

	_, err = New(Config{AuthURL: AuthURL, APIBaseURL: "http://[::1"})
	assert.Error(t, err)
}

// WatchSessionGenerationState streams the key's generation while Connected and 0 in every
// other phase, current on subscribe.
func TestWatchSessionGenerationStateStreamsTheConnectedSession(t *testing.T) {
	r := newRig(t)
	sessions, cancel := r.svc.WatchSessionGenerationState()
	defer cancel()
	assert.Zero(t, testutil.Recv(t, sessions, "the signed-out launch"))

	st := connect(t, r.svc)
	recvSession(t, sessions, st.Session)

	require.NoError(t, r.svc.Disconnect())
	recvSession(t, sessions, 0)
}

// recvSession waits for sessions to carry want. A latest-value watch may skip
// the frames before it.
func recvSession(t *testing.T, sessions <-chan uint64, want uint64) {
	t.Helper()
	for testutil.Recv(t, sessions, "a session") != want {
	}
}

// No key, code or verifier leaves the keyring and memory: after a sign-in,
// a signed request, a refusal OpenRouter confirms and a disconnect, none is in
// a log line. The log is read raw, before redaction: the service logs none.
// The service writes no file.
func TestNoKeyLeavesTheKeyring(t *testing.T) {
	var logs bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(prev) })

	r := newRig(t)
	connect(t, r.svc)
	held, _ := r.storedKey()
	assert.True(t, safe.HasSecret(held), "a held key is registered with the redactor")
	_, err := r.signedKey(r.svc)
	require.NoError(t, err)
	r.openRouter.set(func(f *fakeOpenRouter) { f.checkStatus = http.StatusUnauthorized })
	r.refusedWith(r.svc)
	await(t, r.svc, "KeyRejected", failedWith(connectors.KeyRejected))
	connect(t, r.svc)
	require.NoError(t, r.svc.Disconnect())

	keys := r.openRouter.mintedKeys()
	require.Len(t, keys, 2)
	for _, key := range keys {
		assert.NotContains(t, logs.String(), key)
		assert.False(t, safe.HasSecret(key), "%s is forgotten once cleared", key)
	}
	for _, body := range r.openRouter.exchangeBodies() {
		assert.NotContains(t, logs.String(), body["code_verifier"])
		assert.NotContains(t, logs.String(), `"`+body["code"]+`"`)
	}
}
