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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/oskeyring/oskeyringtest"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/testutil"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/connectors"
)

const storedEntry = `{"v":1,"key":"stored-openrouter-key-0001"}`

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
	r.keyring.put(keyAccount, storedEntry)
	s, _ := r.relaunchHeld(oskeyringtest.Get)

	assert.Equal(t, connectors.Restoring, s.State().Phase)
	testutil.WaitReturn(t, s.Close, "Close")
	assert.Equal(t, connectors.Restoring, s.State().Phase)
	assertUnpublished(t, s)
}

// Released, the restore publishes what the keyring held, with no relaunch.
func TestTheRestorePublishesWhatItRead(t *testing.T) {
	for name, c := range map[string]struct {
		entry   string
		failGet bool
		want    connectors.Phase
	}{
		"connected":  {entry: storedEntry, want: connectors.Connected},
		"signed out": {want: connectors.SignedOut},
		"unreadable": {entry: storedEntry, failGet: true, want: connectors.SignedOut},
	} {
		t.Run(name, func(t *testing.T) {
			r := newRig(t)
			if c.entry != "" {
				r.keyring.put(keyAccount, c.entry)
			}
			if c.failGet {
				r.calls.FailGet(keyAccount, errBoom)
			}
			s, release := r.relaunchHeld(oskeyringtest.Get)
			require.Equal(t, connectors.Restoring, s.State().Phase)

			release()
			awaitRestored(t, s)

			assert.Equal(t, c.want, s.State().Phase)
			_, kept := r.keyring.entry(keyAccount)
			assert.Equal(t, c.entry != "", kept)
		})
	}
}

// The restore publishes only once its delete of an unparsable entry has
// answered; until then the service is Restoring and refuses every change.
func TestTheRestoreWaitsForItsCleanup(t *testing.T) {
	r := newRig(t)
	r.keyring.put(keyAccount, `not json`)
	s, release := r.relaunchHeld(oskeyringtest.Delete)
	r.awaitCall(oskeyringtest.Call{Kind: oskeyringtest.Delete, Account: keyAccount})

	assert.Equal(t, connectors.Restoring, s.State().Phase)
	assert.ErrorIs(t, s.Connect(t.Context()), connectors.ErrRestoring)

	release()
	awaitRestored(t, s)
	assert.Equal(t, connectors.SignedOut, s.State().Phase)
	_, kept := r.keyring.entry(keyAccount)
	assert.False(t, kept)
}

// A Close during the restore's delete returns and publishes nothing.
func TestCloseLeavesACleanupThatNeverAnswers(t *testing.T) {
	r := newRig(t)
	r.keyring.put(keyAccount, `not json`)
	s, _ := r.relaunchHeld(oskeyringtest.Delete)
	r.awaitCall(oskeyringtest.Call{Kind: oskeyringtest.Delete, Account: keyAccount})

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

	st := s.State()
	assert.Equal(t, connectors.Restoring, st.Phase)
	assert.Zero(t, st.Session, "nothing moved the generation")
	for _, c := range r.calls.Calls() {
		assert.Equal(t, oskeyringtest.Get, c.Kind, "no write before the read answered")
	}
}
