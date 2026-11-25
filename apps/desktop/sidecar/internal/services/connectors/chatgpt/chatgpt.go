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

// Package chatgpt is the ChatGPT connector: Sign in with ChatGPT, run by the
// OAuth engine under OpenAI's endpoints, scopes and codes. Its tokens spend the
// user's ChatGPT plan.
package chatgpt

import (
	"net/url"

	"github.com/google/uuid"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/appdb"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/oskeyring"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/connectors/internal/oauthconn"
)

const (
	// ID is the ChatGPT plan's provider id.
	ID = "chatgpt"
	// Issuer is OpenAI's auth server.
	Issuer = "https://auth.openai.com"
	// IssuerURLVar moves Issuer in a debug build.
	IssuerURLVar = "KUBETAIL_CHATGPT_ISSUER_URL"
	// APIBaseURL is the one resource the tokens are for.
	APIBaseURL = "https://api.openai.com/v1"
	// UsageURL is ChatGPT's page that manages each app's access and usage.
	UsageURL = "https://chatgpt.com/settings/usage"

	agentName = "Kubetail"
	// planScope is what lets the tokens spend the plan; signing in alone does not.
	planScope = "chatgpt.tokens.use.direct"
	// hostIDKey is the installation's host id in the row's provider_data.
	hostIDKey = "hostID"
	// clientGone is the token endpoint's code for a registration it no longer
	// knows.
	clientGone = "invalid_client"
)

// Config is the connector's production knobs.
type Config struct {
	// DB is app.db, which holds the registration.
	DB *appdb.DB
	// Keyring is the OS keyring the refresh token is kept in.
	Keyring oskeyring.Keyring
	// Issuer and APIBaseURL are the constants, each moved only by a debug build.
	Issuer     string
	APIBaseURL string
}

// Service is the ChatGPT connector: the OAuth engine under ChatGPT's provider.
type Service struct {
	*oauthconn.Service
}

// New is the connector, Restoring until the engine has read the keyring, then
// signed in if app.db and the keyring hold a session. It reaches no network
// and makes no keyring call.
func New(cfg Config) (*Service, error) {
	engine, err := oauthconn.New(oauthconn.Config{
		Provider: provider(cfg),
		DB:       cfg.DB,
		Keyring:  cfg.Keyring,
	})
	if err != nil {
		return nil, err
	}
	return &Service{engine}, nil
}

// provider is Sign in with ChatGPT at cfg's endpoints.
func provider(cfg Config) oauthconn.Provider {
	return oauthconn.Provider{
		ID:            ID,
		Issuer:        cfg.Issuer,
		APIBaseURL:    cfg.APIBaseURL,
		Scopes:        []string{"openid", "profile", "email", "offline_access", "resource.invoke", planScope},
		RequiredScope: planScope,
		CallbackPath:  "/auth/callback",
		FirstClientID: "dynamic_agent_client",
		NewData:       newData,
		Authorize:     authorize,
		SessionEnders: []string{
			"invalid_grant", "invalid_refresh_token", "token_expired", "refresh_token_expired",
			"refresh_token_invalidated", "refresh_token_reused", clientGone,
		},
		ClientGone: clientGone,
	}
}

// newData mints the installation's host id, which every sign-in sends.
func newData() map[string]string {
	return map[string]string{hostIDKey: "urn:uuid:" + uuid.NewString()}
}

// authorize names the installation, and on the sign-in that registers a
// client, the app the client is for.
func authorize(data map[string]string, registering bool) url.Values {
	q := url.Values{"ext_agent_host_id": {data[hostIDKey]}}
	if registering {
		q.Set("agent_name_hint", agentName)
	}
	return q
}
