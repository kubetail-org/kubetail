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

package google

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

const testBase = "https://aiplatform.googleapis.com/v1/projects/my-project/locations/global/endpoints/openapi"

// newService is a signed-out connector over a fresh app.db and an empty keyring.
func newService(t *testing.T, cfg Config) *Service {
	t.Helper()
	cfg.DB = appdbtest.Open(t)
	cfg.Keyring = oskeyringtest.New()
	s, err := New(cfg)
	require.NoError(t, err)
	t.Cleanup(s.Close)
	testutil.Wait(t, s.Restored(), "the restore")
	return s
}

// The provider is Google's installed-app sign-in: written endpoints, the
// build's client and secret, Vertex AI's scope required, and a refresh token
// asked for on every sign-in.
func TestTheProviderIsGooglesInstalledAppSignIn(t *testing.T) {
	p := provider(Config{ClientID: "client", ClientSecret: "secret", APIBaseURL: testBase})

	assert.Equal(t, "google", p.ID)
	assert.Equal(t, &endpoints, p.Endpoints)
	assert.Equal(t, "https://accounts.google.com", p.Endpoints.Issuer)
	assert.Equal(t, "client", p.ClientID)
	assert.Equal(t, "secret", p.ClientSecret)
	assert.Equal(t, testBase, p.APIBaseURL)
	assert.True(t, p.NoResource)
	assert.Equal(t, []string{"openid", "email", "profile", cloudPlatformScope}, p.Scopes)
	assert.Equal(t, cloudPlatformScope, p.RequiredScope)
	assert.Equal(t, url.Values{"access_type": {"offline"}, "prompt": {"consent"}}, p.Authorize(nil, false))
	assert.Equal(t, []string{"invalid_grant"}, p.SessionEnders)
}

// New is the engine at the configured base: signed out, it answers a request
// there signed out, and with no base it signs nothing.
func TestNewSignsTheConfiguredBase(t *testing.T) {
	s := newService(t, Config{ClientID: "client", APIBaseURL: testBase, Project: "my-project"})
	require.Equal(t, connectors.SignedOut, s.State().Phase)
	assert.Equal(t, "my-project", s.Project())

	req, err := http.NewRequest(http.MethodPost, testBase+"/chat/completions", nil)
	require.NoError(t, err)
	assert.ErrorIs(t, s.Sign(req), llm.ErrSignedOut)

	none := newService(t, Config{ClientID: "client"})
	err = none.Sign(req)
	require.Error(t, err)
	assert.NotErrorIs(t, err, llm.ErrSignedOut)
}

// A build with no client refuses Connect before anything starts.
func TestConnectWithNoClientIsRefused(t *testing.T) {
	s := newService(t, Config{APIBaseURL: testBase})

	assert.ErrorIs(t, s.Connect(t.Context(), false), connectors.ErrNoClient)
	assert.Equal(t, connectors.SignedOut, s.State().Phase)
}

// With a client, Connect is the engine's: one whose stores refuse the sign-in
// fails NotSaved before any browser opens.
func TestConnectWithAClientRunsTheEngine(t *testing.T) {
	db, err := appdb.Open(filepath.Join(t.TempDir(), "app.db"), 0)
	require.NoError(t, err)
	s, err := New(Config{DB: db, Keyring: oskeyringtest.New(), ClientID: "client", APIBaseURL: testBase})
	require.NoError(t, err)
	t.Cleanup(s.Close)
	testutil.Wait(t, s.Restored(), "the restore")
	require.NoError(t, db.Close())

	require.NoError(t, s.Connect(t.Context(), false))

	assert.Equal(t, connectors.State{Phase: connectors.Failed, Failure: connectors.NotSaved, Session: s.State().Session}, s.State())
}

// The engine's failure fails New.
func TestNewFailsOnAClosedDatabase(t *testing.T) {
	db, err := appdb.Open(filepath.Join(t.TempDir(), "app.db"), 0)
	require.NoError(t, err)
	require.NoError(t, db.Close())

	_, err = New(Config{DB: db})

	assert.Error(t, err)
}
