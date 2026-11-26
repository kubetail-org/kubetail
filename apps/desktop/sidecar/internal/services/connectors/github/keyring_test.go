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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/oskeyring/oskeyringtest"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/safe"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/testutil"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/connectors"
)

const storedToken = "test_storedGitHubToken000000001"

// A first launch holds no token: signed out, and the keyring untouched.
func TestAFirstLaunchIsSignedOut(t *testing.T) {
	r := newRig(t)

	assert.Equal(t, connectors.SignedOut, r.svc.State().Phase)
	assert.Zero(t, r.keyring.sets)
}

// A launch reads the entry and reaches no network: a token is Connected as its
// login and registered with the redactor; one that does not parse is signed
// out and deleted.
func TestNewRestoresWhatTheKeyringHolds(t *testing.T) {
	r := newRig(t)
	r.keyring.put(tokenAccount, storedEntry(storedToken, "octocat"))

	s := r.restart()

	st := s.State()
	assert.Equal(t, connectors.Connected, st.Phase)
	assert.Equal(t, "octocat", st.Name)
	assert.NotZero(t, st.Session)
	assert.True(t, safe.HasSecret(storedToken))
	tok, err := s.Token(t.Context())
	require.NoError(t, err)
	assert.Equal(t, storedToken, tok)
	assert.Empty(t, r.github.userRequests())

	for _, bad := range []string{
		`not json`, `{"v":1}`, `{"v":1,"token":"","login":"octocat"}`, `{"v":1,"token":"` + storedToken + `"}`,
		`{"v":0,"token":"` + storedToken + `","login":"octocat"}`, `{"v":1,"token":42,"login":"octocat"}`,
	} {
		r.keyring.put(tokenAccount, bad)

		s = r.restart()

		assert.Equal(t, connectors.SignedOut, s.State().Phase, bad)
		_, ok := r.keyring.entry(tokenAccount)
		assert.False(t, ok, "%s is deleted", bad)
	}
}

// An entry a newer Kubetail wrote is left in place, whatever its shape: that
// Kubetail reads it again.
func TestANewerEntryIsLeft(t *testing.T) {
	for _, newer := range []string{
		`{"v":2,"token":"gho_newer","login":"octocat","host":"x"}`,
		`{"v":2,"token":{"value":"gho_newer"}}`,
	} {
		r := newRig(t)
		r.keyring.put(tokenAccount, newer)

		s := r.restart()

		assert.Equal(t, connectors.SignedOut, s.State().Phase)
		v, ok := r.keyring.entry(tokenAccount)
		assert.True(t, ok)
		assert.Equal(t, newer, v)
	}
}

// A keyring that cannot be read is no proof of a sign-out: signed out, and
// nothing written or deleted.
func TestAnUnreadableKeyringWritesNothing(t *testing.T) {
	r := newRig(t)
	r.keyring.put(tokenAccount, storedEntry(storedToken, "octocat"))
	r.keyring.failGet(errBoom)

	s := r.restart()

	assert.Equal(t, connectors.SignedOut, s.State().Phase)
	_, ok := r.keyring.entry(tokenAccount)
	assert.True(t, ok)
}

// An unreadable entry the keyring will not delete is still signed out.
func TestAnUnreadableEntryThatStaysIsSignedOut(t *testing.T) {
	r := newRig(t)
	r.keyring.put(tokenAccount, `not json`)
	r.keyring.failDelete(tokenAccount, errBoom)

	s := r.restart()

	assert.Equal(t, connectors.SignedOut, s.State().Phase)
}

// assertUnpublished fails if s's restore has published.
func assertUnpublished(t *testing.T, s *Service) {
	t.Helper()
	select {
	case <-s.Restored():
		t.Fatal("the restore published")
	default:
	}
}

// New returns before the keyring answers, Restoring, and Close leaves a read
// that never answers behind.
func TestCloseLeavesAReadThatNeverAnswers(t *testing.T) {
	r := newRig(t)
	r.keyring.put(tokenAccount, storedEntry(storedToken, "octocat"))
	s, _ := r.relaunchHeld(oskeyringtest.Get)

	assert.Equal(t, connectors.Restoring, s.State().Phase)
	testutil.WaitReturn(t, s.Close, "Close")
	assert.Equal(t, connectors.Restoring, s.State().Phase)
	assertUnpublished(t, s)
}

// Released, the restore publishes what the keyring held, with no relaunch.
func TestTheRestorePublishesWhatItRead(t *testing.T) {
	for name, c := range map[string]struct {
		stored  bool
		failGet bool
		want    connectors.Phase
	}{
		"connected":  {stored: true, want: connectors.Connected},
		"signed out": {want: connectors.SignedOut},
		"unreadable": {stored: true, failGet: true, want: connectors.SignedOut},
	} {
		t.Run(name, func(t *testing.T) {
			r := newRig(t)
			if c.stored {
				r.keyring.put(tokenAccount, storedEntry(storedToken, "octocat"))
			}
			if c.failGet {
				r.calls.FailGet(tokenAccount, errBoom)
			}
			s, release := r.relaunchHeld(oskeyringtest.Get)
			require.Equal(t, connectors.Restoring, s.State().Phase)

			release()
			awaitRestored(t, s)

			assert.Equal(t, c.want, s.State().Phase)
			_, kept := r.keyring.entry(tokenAccount)
			assert.Equal(t, c.stored, kept)
		})
	}
}

// The restore publishes only once its delete of an unparsable entry has
// answered; until then the service is Restoring and refuses every change.
func TestTheRestoreWaitsForItsCleanup(t *testing.T) {
	r := newRig(t)
	r.keyring.put(tokenAccount, `not json`)
	s, release := r.relaunchHeld(oskeyringtest.Delete)
	r.awaitCall(oskeyringtest.Call{Kind: oskeyringtest.Delete, Account: tokenAccount})

	assert.Equal(t, connectors.Restoring, s.State().Phase)
	assert.ErrorIs(t, s.Connect(t.Context()), connectors.ErrRestoring)

	release()
	awaitRestored(t, s)
	assert.Equal(t, connectors.SignedOut, s.State().Phase)
	_, kept := r.keyring.entry(tokenAccount)
	assert.False(t, kept)
}

// A Close during the restore's delete returns and publishes nothing.
func TestCloseLeavesACleanupThatNeverAnswers(t *testing.T) {
	r := newRig(t)
	r.keyring.put(tokenAccount, `not json`)
	s, _ := r.relaunchHeld(oskeyringtest.Delete)
	r.awaitCall(oskeyringtest.Call{Kind: oskeyringtest.Delete, Account: tokenAccount})

	testutil.WaitReturn(t, s.Close, "Close")

	assert.Equal(t, connectors.Restoring, s.State().Phase)
	assertUnpublished(t, s)
}

// While Restoring every change is refused, and none reaches the keyring.
func TestAChangeIsRefusedWhileRestoring(t *testing.T) {
	r := newRig(t)
	s, _ := r.relaunchHeld(oskeyringtest.Get)

	assert.ErrorIs(t, s.Connect(t.Context()), connectors.ErrRestoring)
	assert.ErrorIs(t, s.Disconnect(), connectors.ErrRestoring)
	s.Cancel()
	s.Refused(storedToken)

	st := s.State()
	assert.Equal(t, connectors.Restoring, st.Phase)
	assert.Zero(t, st.Session, "nothing moved the generation")
	for _, c := range r.calls.Calls() {
		assert.Equal(t, oskeyringtest.Get, c.Kind, "no write before the read answered")
	}
}
