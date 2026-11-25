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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/oskeyring/oskeyringtest"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/testutil"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/connectors"
)

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
	r.seed(registration{Data: hostData("h"), ClientID: "c", SessionID: "s1"}, "refresh-stored", "s1")
	s, _ := r.relaunchHeld(oskeyringtest.Get)

	assert.Equal(t, connectors.Restoring, s.State().Phase)
	testutil.WaitReturn(t, s.Close, "Close")
	assert.Equal(t, connectors.Restoring, s.State().Phase)
	assertUnpublished(t, s)
}

// Released, the restore publishes what the keyring held, with no relaunch.
func TestTheRestorePublishesWhatItRead(t *testing.T) {
	signedIn := registration{Data: hostData("h"), ClientID: "c", SessionID: "s1"}
	for name, c := range map[string]struct {
		refresh string
		failGet bool
		want    connectors.Phase
	}{
		"connected":  {refresh: "refresh-stored", want: connectors.Connected},
		"signed out": {want: connectors.SignedOut},
		"unreadable": {refresh: "refresh-stored", failGet: true, want: connectors.SignedOut},
	} {
		t.Run(name, func(t *testing.T) {
			r := newRig(t)
			r.seed(signedIn, c.refresh, "s1")
			if c.failGet {
				r.calls.FailGet(refreshAccount, errBoom)
			}
			s, release := r.relaunchHeld(oskeyringtest.Get)
			require.Equal(t, connectors.Restoring, s.State().Phase)

			release()
			awaitRestored(t, s)

			assert.Equal(t, c.want, s.State().Phase)
		})
	}
}

// The restore publishes only once its delete of an unusable entry has
// answered; until then the service is Restoring and refuses every change.
func TestTheRestoreWaitsForItsCleanup(t *testing.T) {
	r := newRig(t)
	r.seed(registration{Data: hostData("h"), ClientID: "c", SessionID: "s1"}, "refresh-stored", "s2")
	s, release := r.relaunchHeld(oskeyringtest.Delete)
	r.awaitCall(oskeyringtest.Call{Kind: oskeyringtest.Delete, Account: refreshAccount})

	assert.Equal(t, connectors.Restoring, s.State().Phase)
	assert.ErrorIs(t, s.Connect(t.Context(), false), connectors.ErrRestoring)
	assert.Equal(t, "s1", savedRegistration(t, r).SessionID, "the row is written after the delete")

	release()
	awaitRestored(t, s)
	assert.Equal(t, connectors.SignedOut, s.State().Phase)
	assert.Empty(t, savedRegistration(t, r).SessionID)
	_, found := storedRefresh(t, r)
	assert.False(t, found)
}

// A Close during the restore's delete returns, publishes nothing and writes
// no row: the next launch finds the same mismatch.
func TestCloseLeavesACleanupThatNeverAnswers(t *testing.T) {
	r := newRig(t)
	r.seed(registration{Data: hostData("h"), ClientID: "c", SessionID: "s1"}, "refresh-stored", "s2")
	s, _ := r.relaunchHeld(oskeyringtest.Delete)
	r.awaitCall(oskeyringtest.Call{Kind: oskeyringtest.Delete, Account: refreshAccount})

	testutil.WaitReturn(t, s.Close, "Close")

	assert.Equal(t, connectors.Restoring, s.State().Phase)
	assert.Equal(t, "s1", savedRegistration(t, r).SessionID)
	assertUnpublished(t, s)
}

// A row that cannot be read restores too: Restoring until the keyring has
// answered, then signed out, with both stores as they were.
func TestAnUnreadableRowWaitsForTheKeyring(t *testing.T) {
	r := newRig(t)
	_, err := r.db.Write.ExecContext(t.Context(), `INSERT INTO oauth_connections
		(provider, client_id, session_id, provider_data, created_at, updated_at) VALUES ('acme', 'c', 's1', '{', 0, 0)`)
	require.NoError(t, err)
	require.NoError(t, saveEntry(r.keyring, refreshAccount, "s2", "refresh-stored"))

	s, release := r.relaunchHeld(oskeyringtest.Get)
	assert.Equal(t, connectors.Restoring, s.State().Phase)

	release()
	awaitRestored(t, s)
	assert.Equal(t, connectors.SignedOut, s.State().Phase)
	_, found := storedRefresh(t, r)
	assert.True(t, found, "the entry is left")
	assert.NotContains(t, r.calls.Calls(), oskeyringtest.Call{Kind: oskeyringtest.Delete, Account: refreshAccount})
}

// While Restoring every change is refused, and none reaches the keyring.
func TestAChangeIsRefusedWhileRestoring(t *testing.T) {
	r := newRig(t)
	s, _ := r.relaunchHeld(oskeyringtest.Get)

	assert.ErrorIs(t, s.Connect(t.Context(), false), connectors.ErrRestoring)
	_, err := s.Start(t.Context(), true)
	assert.ErrorIs(t, err, connectors.ErrRestoring)
	assert.ErrorIs(t, s.Disconnect(t.Context()), connectors.ErrRestoring)
	s.Cancel()

	st := s.State()
	assert.Equal(t, connectors.Restoring, st.Phase)
	assert.Zero(t, st.Session, "nothing moved the generation")
	for _, c := range r.calls.Calls() {
		assert.Equal(t, oskeyringtest.Get, c.Kind, "no write before the read answered")
	}
}
