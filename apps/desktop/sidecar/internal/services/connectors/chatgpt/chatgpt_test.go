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

package chatgpt

import (
	"net/http"
	"net/url"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/appdb"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/appdb/appdbtest"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/oskeyring/oskeyringtest"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/testutil"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/llm"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/connectors"
)

// newService is a signed-out connector over a fresh app.db and an empty keyring.
func newService(t *testing.T, apiBaseURL string) *Service {
	t.Helper()
	s, err := New(Config{DB: appdbtest.Open(t), Keyring: oskeyringtest.New(), Issuer: Issuer, APIBaseURL: apiBaseURL})
	require.NoError(t, err)
	t.Cleanup(s.Close)
	testutil.Wait(t, s.Restored(), "the restore")
	return s
}

// The provider asks for the plan's scope and keeps no sign-in without it, and
// the code for a registration OpenAI no longer knows ends a session.
func TestTheProviderIsSignInWithChatGPT(t *testing.T) {
	p := provider(Config{Issuer: Issuer, APIBaseURL: APIBaseURL})

	assert.Equal(t, "chatgpt", p.ID)
	assert.Equal(t, "https://auth.openai.com", p.Issuer)
	assert.Equal(t, "https://api.openai.com/v1", p.APIBaseURL)
	assert.Contains(t, p.Scopes, p.RequiredScope)
	assert.Equal(t, "chatgpt.tokens.use.direct", p.RequiredScope)
	assert.Contains(t, p.SessionEnders, p.ClientGone)
}

// A debug build's issuer and API base reach the provider.
func TestTheProviderTakesTheConfiguredEndpoints(t *testing.T) {
	p := provider(Config{Issuer: "http://issuer.test", APIBaseURL: "http://api.test/v1"})

	assert.Equal(t, "http://issuer.test", p.Issuer)
	assert.Equal(t, "http://api.test/v1", p.APIBaseURL)
}

// The installation's host id is minted once and sent on every authorize; the
// app's name goes only with the sign-in that registers a client.
func TestAuthorizeNamesTheInstallation(t *testing.T) {
	p := provider(Config{})
	data := p.NewData()
	assert.Regexp(t, `^urn:uuid:[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`, data["hostID"])
	assert.NotEqual(t, data, p.NewData())

	assert.Equal(t, url.Values{"ext_agent_host_id": {data["hostID"]}, "agent_name_hint": {"Kubetail"}}, p.Authorize(data, true))
	assert.Equal(t, url.Values{"ext_agent_host_id": {data["hostID"]}}, p.Authorize(data, false))
}

// New is the engine at the configured base: signed out, it answers a request
// there signed out, and refuses one at the default base as off its API.
func TestNewSignsTheConfiguredBase(t *testing.T) {
	s := newService(t, "https://api.test/v1")
	require.Equal(t, connectors.SignedOut, s.State().Phase)

	moved, err := http.NewRequest(http.MethodGet, "https://api.test/v1/responses", nil)
	require.NoError(t, err)
	assert.ErrorIs(t, s.Sign(moved), llm.ErrSignedOut)

	off, err := http.NewRequest(http.MethodGet, APIBaseURL+"/responses", nil)
	require.NoError(t, err)
	err = s.Sign(off)
	require.Error(t, err)
	assert.NotErrorIs(t, err, llm.ErrSignedOut)
}

// The engine's failure fails New.
func TestNewFailsOnAClosedDatabase(t *testing.T) {
	db, err := appdb.Open(filepath.Join(t.TempDir(), "app.db"), 0)
	require.NoError(t, err)
	require.NoError(t, db.Close())

	_, err = New(Config{DB: db, Issuer: Issuer, APIBaseURL: APIBaseURL})

	assert.Error(t, err)
}
