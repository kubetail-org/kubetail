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

// Package openrouter is the OpenRouter connector: a sign-in with PKCE whose
// exchange grants an ordinary API key, kept in the OS keyring. The key signs
// the openrouter provider's requests (llm.SessionSigner) while it is
// connected, through providercredentials, which ranks it over the provider's keys.
//
// Files: this one (Service, New), keyring.go (the key's entry),
// connect.go (the sign-in), sign.go (Sign, Bind, the key check),
// disconnect.go.
package openrouter

import (
	"context"
	"net/url"
	"sync"
	"time"

	"github.com/amorey/gochan/watch"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/oauth/browser"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/oskeyring"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/llm"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/connectors"
)

const (
	// ID is the openrouter row's provider id.
	ID = "openrouter"
	// AuthURL is OpenRouter's origin: the auth page is its /auth, the exchange
	// its /api/v1/auth/keys.
	AuthURL = "https://openrouter.ai"
	// AuthURLVar moves AuthURL in a debug build.
	AuthURLVar = "KUBETAIL_OPENROUTER_AUTH_URL"
	// APIBaseURL is where the key is sent, and nowhere else.
	APIBaseURL = "https://openrouter.ai/api/v1"
	// KeysURL is OpenRouter's key settings, where the user deletes the key.
	KeysURL = "https://openrouter.ai/settings/keys"
	// KeyLabel names the key on OpenRouter's key list.
	KeyLabel = "Kubetail"

	attemptTimeout = 5 * time.Minute
	// issuerTimeout bounds one answer of OpenRouter's: the exchange, a key check.
	issuerTimeout = 30 * time.Second
)

// Config is the service's production knobs.
type Config struct {
	// Keyring is the OS keyring the key is kept in.
	Keyring oskeyring.Keyring
	// AuthURL is OpenRouter's origin, AuthURL unless a debug build moved it.
	AuthURL string
	// APIBaseURL is APIBaseURL unless a debug build moved it.
	APIBaseURL string
}

// Service is the OpenRouter connector. Every change happens under mu, against
// gen: Connect, Cancel, Disconnect and a rejected key each bump it, and an
// exchange or a key check that finishes applies only to the generation it
// began under.
type Service struct {
	auth, base     *url.URL
	keyring        oskeyring.Keyring
	open           func(url string) error
	attemptTimeout time.Duration
	issuerTimeout  time.Duration
	// checkEnded is told when a key check ends; nil in production.
	checkEnded func()

	// ctx bounds every goroutine the service starts; Close cancels it and joins
	// them.
	ctx  context.Context
	stop context.CancelFunc
	wg   sync.WaitGroup

	hub *watch.Hub[connectors.State]
	tx  *watch.Sender[connectors.State]
	// sessions holds the generation while Connected, 0 otherwise.
	sessions *watch.Hub[uint64]
	// restored closes once the restore has published.
	restored chan struct{}

	mu    sync.Mutex
	gen   uint64
	state connectors.State
	key   string
	// restoredGen is the generation the restore published, 0 for none.
	restoredGen uint64
	attempt     *attempt
	// checking is a key check under way.
	checking bool
}

// option is an unexported build seam, so only in-package tests pass one.
type option func(*Service)

func withBrowser(open func(string) error) option { return func(s *Service) { s.open = open } }

func withAttemptTimeout(d time.Duration) option { return func(s *Service) { s.attemptTimeout = d } }

func withIssuerTimeout(d time.Duration) option { return func(s *Service) { s.issuerTimeout = d } }

func withCheckEnded(fn func()) option { return func(s *Service) { s.checkEnded = fn } }

// New builds the service, Restoring, and restores a stored key on a goroutine.
// It reaches no network and makes no keyring call.
func New(cfg Config) (*Service, error) {
	return newWithOptions(cfg)
}

func newWithOptions(cfg Config, opts ...option) (*Service, error) {
	auth, err := url.Parse(cfg.AuthURL)
	if err != nil {
		return nil, err
	}
	base, err := url.Parse(cfg.APIBaseURL)
	if err != nil {
		return nil, err
	}
	ctx, stop := context.WithCancel(context.Background())
	s := &Service{
		auth:           auth,
		base:           base,
		keyring:        cfg.Keyring,
		open:           browser.Open,
		attemptTimeout: attemptTimeout,
		issuerTimeout:  issuerTimeout,
		ctx:            ctx,
		stop:           stop,
		hub:            watch.New(connectors.State{}),
		sessions:       watch.New[uint64](0),
		restored:       make(chan struct{}),
	}
	s.tx = s.hub.Sender()
	for _, opt := range opts {
		opt(s)
	}
	s.publishLocked(connectors.State{Phase: connectors.Restoring})
	s.wg.Go(s.restore)
	return s, nil
}

// Restored closes once the restore has published what the keyring held.
func (s *Service) Restored() <-chan struct{} { return s.restored }

// RestoredSession is the generation the restore published, 0 for none.
func (s *Service) RestoredSession() uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.restoredGen
}

// State is the connector's current state.
func (s *Service) State() connectors.State {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.state
}

// WatchState streams the state: current on subscribe, then each change.
func (s *Service) WatchState() (<-chan connectors.State, func()) {
	rx := s.hub.Receiver()
	return rx.Chan(), rx.Close
}

// WatchSessionGenerationState streams the connected key's generation while Connected, 0
// otherwise. Current on subscribe, then each change.
func (s *Service) WatchSessionGenerationState() (<-chan uint64, func()) {
	rx := s.sessions.Receiver()
	return rx.Chan(), rx.Close
}

// Close ends every goroutine the service started and waits for them, but
// for a keyring call that never answers, which it leaves behind.
func (s *Service) Close() {
	s.stop()
	s.wg.Wait()
}

// publishLocked makes st the state, stamped with the generation (s.mu held).
func (s *Service) publishLocked(st connectors.State) {
	st.Session = s.gen
	s.state = st
	s.tx.Send(st) //nolint:errcheck // Send never blocks, and the hub is never closed
	var session uint64
	if st.Phase == connectors.Connected {
		session = s.gen
	}
	s.sessions.Sender().Send(session) //nolint:errcheck // as above
}

var _ llm.SessionSigner = (*Service)(nil)
