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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/oskeyring/oskeyringtest"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/testutil"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/connectors"
)

func TestAnEntryReadsBackAsItWasSaved(t *testing.T) {
	k := newFakeKeyring()
	want := entry{V: entryVersion, Refresh: "refresh-1", DPoPKey: "pem", ARN: "arn:aws:iam::123456789012:user/alice"}
	require.NoError(t, saveEntry(k, want))

	got, found, err := parseEntry(k.Get(sessionAccount))
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, want, got)
}

func TestNoEntryIsNotFound(t *testing.T) {
	_, found, err := parseEntry(newFakeKeyring().Get(sessionAccount))
	require.NoError(t, err)
	require.False(t, found)
}

func TestAKeyringThatCannotBeReadIsAnError(t *testing.T) {
	k := newFakeKeyring()
	k.failGet(errBoom)
	_, _, err := parseEntry(k.Get(sessionAccount))
	require.ErrorIs(t, err, errBoom)
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
	connect(t, r.svc)
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
				connect(t, r.svc)
			}
			if c.failGet {
				r.calls.FailGet(sessionAccount, errBoom)
			}
			s, release := r.relaunchHeld(oskeyringtest.Get)
			require.Equal(t, connectors.Restoring, s.State().Phase)

			release()
			awaitRestored(t, s)

			assert.Equal(t, c.want, s.State().Phase)
			_, kept := r.stored()
			assert.Equal(t, c.stored, kept)
		})
	}
}

// The restore publishes only once its delete of an unusable entry has
// answered; until then the service is Restoring and refuses every change.
func TestTheRestoreWaitsForItsCleanup(t *testing.T) {
	r := newRig(t)
	r.keyring.put(sessionAccount, `not json`)
	s, release := r.relaunchHeld(oskeyringtest.Delete)
	r.awaitCall(oskeyringtest.Call{Kind: oskeyringtest.Delete, Account: sessionAccount})

	assert.Equal(t, connectors.Restoring, s.State().Phase)
	assert.ErrorIs(t, s.Connect(t.Context()), connectors.ErrRestoring)

	release()
	awaitRestored(t, s)
	assert.Equal(t, connectors.SignedOut, s.State().Phase)
	_, kept := r.stored()
	assert.False(t, kept)
}

// A Close during the restore's delete returns and publishes nothing.
func TestCloseLeavesACleanupThatNeverAnswers(t *testing.T) {
	r := newRig(t)
	r.keyring.put(sessionAccount, `not json`)
	s, _ := r.relaunchHeld(oskeyringtest.Delete)
	r.awaitCall(oskeyringtest.Call{Kind: oskeyringtest.Delete, Account: sessionAccount})

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
