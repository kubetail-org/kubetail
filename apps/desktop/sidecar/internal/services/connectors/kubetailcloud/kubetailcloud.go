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

// Package kubetailcloud is the Kubetail Cloud connector: the Kubetail Cloud account, signed in to by
// the OAuth engine under a fixed client. It signs no request.
package kubetailcloud

import (
	"context"
	"sync"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/appdb"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/oskeyring"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/connectors"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/connectors/internal/oauthconn"
)

// ID is the row's key and the keyring accounts' prefix.
const ID = "kubetail-cloud"

// Config is the connector's production knobs.
type Config struct {
	// DB is app.db, which holds the account's row.
	DB *appdb.DB
	// Keyring is the OS keyring the refresh token is kept in.
	Keyring oskeyring.Keyring
	// Issuer and ClientID are Kubetail Cloud's auth server and the app's client there.
	Issuer   string
	ClientID string
}

// engine is what the connector uses of the OAuth engine.
type engine interface {
	State() connectors.State
	Restored() <-chan struct{}
	WatchState() (<-chan connectors.State, func())
	Start(ctx context.Context, fresh bool) (connectors.Failure, error)
	Cancel()
	Disconnect(ctx context.Context) error
	Close()
}

// Service is the Kubetail Cloud connector. The engine stays unexported, so nothing
// outside this package can sign with the account.
type Service struct {
	engine engine
	// connecting runs one Connect at a time.
	connecting sync.Mutex
}

// New is the connector, Restoring until the engine has read the keyring, then
// signed in if app.db and the keyring hold a session. It reaches no network
// and makes no keyring call.
func New(cfg Config) (*Service, error) {
	return newService(func() (engine, error) {
		return oauthconn.New(oauthconn.Config{
			Provider: provider(cfg),
			DB:       cfg.DB,
			Keyring:  cfg.Keyring,
		})
	})
}

// newService is the connector over the engine newEngine builds.
func newService(newEngine func() (engine, error)) (*Service, error) {
	e, err := newEngine()
	if err != nil {
		return nil, err
	}
	return &Service{engine: e}, nil
}

// Restored closes once the engine has published what the stores held.
func (s *Service) Restored() <-chan struct{} { return s.engine.Restored() }

// State is the connector's current state.
func (s *Service) State() connectors.State { return s.engine.State() }

// Disconnect signs out on this machine, then revokes the refresh token on a
// goroutine. It errs only when the sign-out could not be recorded and the
// entry is still there.
func (s *Service) Disconnect(ctx context.Context) error { return s.engine.Disconnect(ctx) }

// Close ends the engine's goroutines, a revocation under way included, and
// waits for them.
func (s *Service) Close() { s.engine.Close() }

// provider is the Kubetail Cloud sign-in at cfg's issuer.
func provider(cfg Config) oauthconn.Provider {
	return oauthconn.Provider{
		ID:            ID,
		Issuer:        cfg.Issuer,
		ClientID:      cfg.ClientID,
		Scopes:        []string{"openid", "offline_access", "email", "profile"},
		CallbackPath:  "/oauth/callback",
		SessionEnders: []string{"invalid_grant"},
	}
}
