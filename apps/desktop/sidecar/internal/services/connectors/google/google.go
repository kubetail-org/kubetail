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

// Package google is the Google connector: Google's sign-in for installed apps,
// run by the OAuth engine under Kubetail's client, whose tokens sign Vertex AI's
// OpenAI-compatible endpoint in the user's own Google Cloud project.
package google

import (
	"context"
	"net/url"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/appdb"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/oauth"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/oskeyring"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/connectors"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/connectors/internal/oauthconn"
)

const (
	// ID is the connector's id and its model provider's.
	ID = "google"
	// ClientIDVar and ClientSecretVar move the client in a debug build.
	ClientIDVar     = "KUBETAIL_GOOGLE_CLIENT_ID"
	ClientSecretVar = "KUBETAIL_GOOGLE_CLIENT_SECRET"

	// cloudPlatformScope is the one Vertex AI takes.
	cloudPlatformScope = "https://www.googleapis.com/auth/cloud-platform"
)

// endpoints is Google's OAuth endpoints. They span three origins, which
// discovery would refuse, so they are written out.
var endpoints = oauth.Endpoints{
	Issuer:        "https://accounts.google.com",
	AuthURL:       "https://accounts.google.com/o/oauth2/v2/auth",
	TokenURL:      "https://oauth2.googleapis.com/token",
	JWKSURL:       "https://www.googleapis.com/oauth2/v3/certs",
	RevocationURL: "https://oauth2.googleapis.com/revoke",
}

// Config is the connector's production knobs.
type Config struct {
	// DB is app.db, which holds the account's row.
	DB *appdb.DB
	// Keyring is the OS keyring the refresh token is kept in.
	Keyring oskeyring.Keyring
	// ClientID and ClientSecret are Kubetail's installed-app client. An empty
	// ClientID refuses Connect.
	ClientID     string
	ClientSecret string
	// APIBaseURL is the project's Vertex AI endpoint, BaseURL's. Empty signs
	// nothing.
	APIBaseURL string
	// Project is the Google Cloud project APIBaseURL names, for display.
	Project string
}

// Service is the Google connector: the OAuth engine under Google's provider.
type Service struct {
	*oauthconn.Service
	clientID string
	project  string
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
	return &Service{Service: engine, clientID: cfg.ClientID, project: cfg.Project}, nil
}

// Connect is the engine's, refused with no client.
func (s *Service) Connect(ctx context.Context, fresh bool) error {
	if s.clientID == "" {
		return connectors.ErrNoClient
	}
	return s.Service.Connect(ctx, fresh)
}

// Project is the Google Cloud project the models run in, empty when none was
// found.
func (s *Service) Project() string { return s.project }

// provider is Google's installed-app sign-in under cfg's client.
func provider(cfg Config) oauthconn.Provider {
	return oauthconn.Provider{
		ID:            ID,
		Endpoints:     &endpoints,
		ClientID:      cfg.ClientID,
		ClientSecret:  cfg.ClientSecret,
		APIBaseURL:    cfg.APIBaseURL,
		NoResource:    true,
		Scopes:        []string{"openid", "email", "profile", cloudPlatformScope},
		RequiredScope: cloudPlatformScope,
		CallbackPath:  "/oauth/callback",
		Authorize:     authorize,
		SessionEnders: []string{"invalid_grant"},
	}
}

// authorize asks for a refresh token, which Google grants only offline, and
// only with consent once the account has granted one.
func authorize(map[string]string, bool) url.Values {
	return url.Values{"access_type": {"offline"}, "prompt": {"consent"}}
}
