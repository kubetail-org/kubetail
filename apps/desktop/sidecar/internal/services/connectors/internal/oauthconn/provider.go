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
	"net/url"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/oauth"
)

// Provider is what one provider's sign-in has of its own: authorization code
// with PKCE on a loopback redirect, under a client fixed in code or one the
// provider registers for this installation during its first sign-in.
type Provider struct {
	// ID is the row's key in oauth_connections and the prefix of the keyring's
	// accounts. For a model provider it is also the catalog's provider id.
	ID string
	// Issuer is the provider's auth server, whose discovery document names the
	// endpoints.
	Issuer string
	// Endpoints, set, are the provider's endpoints written out: Issuer is unused
	// and nothing is discovered.
	Endpoints *oauth.Endpoints
	// ClientID is a client fixed in code. Set, the provider registers none:
	// FirstClientID and ClientGone are unused and the row's client_id stays NULL.
	ClientID string
	// ClientSecret is the fixed client's secret, which an installed app's
	// provider may require though the app cannot keep it confidential.
	ClientSecret string
	// APIBaseURL is the one resource the tokens are for, and all Sign signs.
	// Empty signs nothing.
	APIBaseURL string
	// NoResource leaves the RFC 8707 resource, APIBaseURL, out of every request
	// to the issuer, for one that takes none.
	NoResource bool
	// Scopes is what a sign-in asks for.
	Scopes []string
	// RequiredScope is the scope a sign-in is kept for: one granted without it
	// fails ScopeNotGranted, and the next asks for consent again. Empty keeps
	// every sign-in.
	RequiredScope string
	// CallbackPath is the loopback redirect's path.
	CallbackPath string
	// FirstClientID is the client a first sign-in names: the provider registers
	// one during it and returns its id beside the code.
	FirstClientID string
	// NewData mints what the provider keeps for this installation, the row's
	// provider_data, at the first sign-in. It outlives a forgotten registration.
	// Nil keeps nothing.
	NewData func() map[string]string
	// Authorize is the provider's own parameters of an authorize request, over
	// the row's data. registering is a sign-in under FirstClientID. Nil adds none.
	Authorize func(data map[string]string, registering bool) url.Values
	// SessionEnders is the token endpoint's codes that end a session.
	SessionEnders []string
	// ClientGone is the one of them that says the provider no longer knows the
	// registration, so the next sign-in registers anew.
	ClientGone string
}
