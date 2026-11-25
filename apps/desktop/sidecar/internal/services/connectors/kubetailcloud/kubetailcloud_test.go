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

package kubetailcloud

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/appdb/appdbtest"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/oskeyring/oskeyringtest"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/testutil"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/connectors"
)

// The provider is the Kubetail Cloud account under the configured fixed client, and
// signs nothing.
func TestTheProviderIsTheKubetailCloudSignIn(t *testing.T) {
	t.Parallel()
	p := provider(Config{Issuer: "https://issuer.test", ClientID: "kubetail-desktop"})

	assert.Equal(t, "kubetail-cloud", p.ID)
	assert.Equal(t, "https://issuer.test", p.Issuer)
	assert.Equal(t, "kubetail-desktop", p.ClientID)
	assert.Equal(t, []string{"openid", "offline_access", "email", "profile"}, p.Scopes)
	assert.Equal(t, "/oauth/callback", p.CallbackPath)
	assert.Equal(t, []string{"invalid_grant"}, p.SessionEnders)
	assert.Empty(t, p.APIBaseURL)
	assert.Empty(t, p.RequiredScope)
	assert.Empty(t, p.FirstClientID)
	assert.Empty(t, p.ClientGone)
	assert.Nil(t, p.NewData)
	assert.Nil(t, p.Authorize)
}

// An engine that cannot be built is New's error.
func TestNewAnswersTheEnginesError(t *testing.T) {
	t.Parallel()
	want := errors.New("no statements")

	_, err := newService(func() (engine, error) { return nil, want })

	require.ErrorIs(t, err, want)
}

// New builds the engine over app.db, signed out with no session stored, and
// reaches no network.
func TestNewIsSignedOutWithNoSession(t *testing.T) {
	t.Parallel()
	s, err := New(Config{DB: appdbtest.Open(t), Keyring: oskeyringtest.New(), Issuer: "https://issuer.test", ClientID: "kubetail-desktop"})
	require.NoError(t, err)
	t.Cleanup(s.Close)
	testutil.Wait(t, s.Restored(), "the restore")

	assert.Equal(t, connectors.SignedOut, s.State().Phase)
}

// State, Disconnect and Close are the engine's.
func TestStateDisconnectAndCloseAreTheEngines(t *testing.T) {
	t.Parallel()
	e := newFakeEngine()
	e.state = connectors.State{Phase: connectors.Connected, Subject: "u1"}
	s := newTestService(t, e)

	assert.Equal(t, e.state, s.State())
	require.NoError(t, s.Disconnect(context.Background()))
	s.Close()

	assert.Equal(t, []string{"Disconnect", "Close"}, e.recorded())
}
