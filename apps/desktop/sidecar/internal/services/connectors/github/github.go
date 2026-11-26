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

// Package github is the GitHub connector: GitHub's device flow under Kubetail's
// OAuth App, whose token is kept in the OS keyring. While connected, the token
// is what a sandboxed command's GitHub proxy sends.
//
// Files: this one (Service, New), keyring.go (the token's entry), connect.go
// (the sign-in), device.go (GitHub's endpoints), token.go (the proxy's token
// and its check), disconnect.go.
package github

import (
	"context"
	"net/url"
	"sync"
	"time"

	"github.com/amorey/gochan/watch"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/oauth/browser"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/oskeyring"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/connectors"
)

const (
	// ID is the connector's id.
	ID = "github"
	// AuthURL is GitHub's origin: the device flow's endpoints are under it, and
	// the browser opens on no other.
	AuthURL = "https://github.com"
	// AuthURLVar moves AuthURL in a debug build.
	AuthURLVar = "KUBETAIL_GITHUB_AUTH_URL"
	// ClientIDVar sets the OAuth App's client id in a debug build.
	ClientIDVar = "KUBETAIL_GITHUB_CLIENT_ID"
	// APIURL is GitHub's API, which names the account and confirms a refused token.
	APIURL = "https://api.github.com"
	// SettingsURL is GitHub's list of authorized OAuth Apps, where the user
	// revokes the token.
	SettingsURL = "https://github.com/settings/applications"

	// attemptTimeout bounds a sign-in, as GitHub's device codes last 15 minutes.
	attemptTimeout = 15 * time.Minute
	// issuerTimeout bounds one answer of GitHub's.
	issuerTimeout = 30 * time.Second
)

// scopes is what the token is for: git and gh on the user's repositories,
// their organizations, and their workflow files.
var scopes = []string{"repo", "read:org", "workflow"}

// Config is the service's production knobs.
type Config struct {
	// Keyring is the OS keyring the token is kept in.
	Keyring oskeyring.Keyring
	// ClientID is Kubetail's OAuth App. Empty refuses every Connect
	// (connectors.ErrNoClient).
	ClientID string
	// AuthURL and APIURL are the constants, AuthURL moved only by a debug build.
	AuthURL string
	APIURL  string
}

// Service is the GitHub connector. Every change happens under mu, against gen:
// Connect, Cancel, Disconnect and a rejected token each bump it, and a sign-in
// or a token check that finishes applies only to the generation it began
// under.
type Service struct {
	clientID       string
	auth, api      *url.URL
	keyring        oskeyring.Keyring
	open           func(url string) error
	attemptTimeout time.Duration
	issuerTimeout  time.Duration
	// pace is how long GitHub's interval of seconds is.
	pace func(seconds int) time.Duration
	// checkEnded is told when a token check ends, attemptEnded when a sign-in's
	// polling and its outcome do; nil in production.
	checkEnded   func()
	attemptEnded func()

	// ctx bounds every goroutine the service starts; Close cancels it and joins
	// them.
	ctx  context.Context
	stop context.CancelFunc
	wg   sync.WaitGroup

	hub *watch.Hub[connectors.State]
	tx  *watch.Sender[connectors.State]
	// restored closes once the restore has published.
	restored chan struct{}

	mu      sync.Mutex
	gen     uint64
	state   connectors.State
	token   string
	attempt *attempt
	// checking is a token check under way.
	checking bool
}

// option is an unexported build seam, so only in-package tests pass one.
type option func(*Service)

func withBrowser(open func(string) error) option { return func(s *Service) { s.open = open } }

func withAttemptTimeout(d time.Duration) option { return func(s *Service) { s.attemptTimeout = d } }

func withPace(pace func(int) time.Duration) option { return func(s *Service) { s.pace = pace } }

func withCheckEnded(fn func()) option { return func(s *Service) { s.checkEnded = fn } }

func withAttemptEnded(fn func()) option { return func(s *Service) { s.attemptEnded = fn } }

// New builds the service, Restoring, and restores a stored token on a
// goroutine. It reaches no network and makes no keyring call.
func New(cfg Config) (*Service, error) {
	return newWithOptions(cfg)
}

func newWithOptions(cfg Config, opts ...option) (*Service, error) {
	auth, err := url.Parse(cfg.AuthURL)
	if err != nil {
		return nil, err
	}
	api, err := url.Parse(cfg.APIURL)
	if err != nil {
		return nil, err
	}
	ctx, stop := context.WithCancel(context.Background())
	s := &Service{
		clientID:       cfg.ClientID,
		auth:           auth,
		api:            api,
		keyring:        cfg.Keyring,
		open:           browser.Open,
		attemptTimeout: attemptTimeout,
		issuerTimeout:  issuerTimeout,
		pace:           func(seconds int) time.Duration { return time.Duration(seconds) * time.Second },
		ctx:            ctx,
		stop:           stop,
		hub:            watch.New(connectors.State{}),
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
}
