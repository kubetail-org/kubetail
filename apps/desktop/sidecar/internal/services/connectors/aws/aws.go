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

// Package aws is the AWS connector: AWS's own sign-in for developer tools,
// the flow `aws login` runs. Authorization code with PKCE on a loopback
// redirect, under AWS's fixed client; its token endpoint answers temporary
// SigV4 credentials and a refresh token, both bound to the session's DPoP key.
// The refresh token and the key are kept in the OS keyring; the credentials
// are memory's alone, refreshed when a caller asks for them.
//
// Files: this one (Service, New), keyring.go (the session's entry), dpop.go
// (the session's key and the proof each token request carries), token.go (the
// token endpoint), connect.go (the sign-in), credentials.go (Retrieve, the
// refresh), disconnect.go.
package aws

import (
	"context"
	"crypto/ecdsa"
	"fmt"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/amorey/gochan/watch"
	sdkaws "github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/aws/arn"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/oauth/browser"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/oskeyring"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/safe"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/llm"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/connectors"
)

const (
	// ID is the connector's id, and the prefix of its keyring accounts.
	ID = "aws"
	// Region is where a session is signed in to, and refreshed.
	Region = "us-east-1"
	// SigninURL is AWS's sign-in in Region: the authorize page is its
	// /v1/authorize, the token endpoint its /v1/token.
	SigninURL = "https://" + Region + ".signin.aws.amazon.com"
	// SigninURLVar moves SigninURL in a debug build.
	SigninURLVar = "KUBETAIL_AWS_SIGNIN_URL"
	// ClientID is AWS's client for a developer tool that signs in on the same
	// device as its browser.
	ClientID = "arn:aws:signin:::devtools/same-device"

	attemptTimeout = 5 * time.Minute
	// issuerTimeout bounds one answer of AWS's: an exchange, a refresh.
	issuerTimeout = 30 * time.Second
)

// Config is the service's production knobs.
type Config struct {
	// Keyring is the OS keyring the session is kept in.
	Keyring oskeyring.Keyring
	// SigninURL is SigninURL unless a debug build moved it.
	SigninURL string
}

// Service is the AWS connector. Every change happens under mu, against gen:
// Connect, Cancel, Disconnect and a session's end each bump it, and an exchange
// or a refresh that finishes applies only to the generation it began under.
type Service struct {
	signin         *url.URL
	tokens         *tokenClient
	keyring        oskeyring.Keyring
	open           func(url string) error
	httpClient     *http.Client
	now            func() time.Time
	attemptTimeout time.Duration
	// attemptEnded is told when an attempt's goroutine ends; nil in production.
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

	// refreshing serializes refreshes: a refresh token is spent once.
	refreshing sync.Mutex

	mu      sync.Mutex
	gen     uint64
	state   connectors.State
	session *session
	attempt *attempt
}

// session is a signed-in principal's tokens.
type session struct {
	key *ecdsa.PrivateKey
	// arn is the principal's, checked to name an account when it was taken.
	arn     string
	refresh string
	// creds are the current credentials; zero until a restored session's first
	// refresh.
	creds sdkaws.Credentials
	// unsaved is a rotated refresh token not yet in the keyring. Until it lands
	// no credentials are handed out: the old token is spent, and a crash would
	// lose the session without a sign.
	unsaved bool
}

// take makes tok the session's tokens, its credentials named as the account's.
func (sess *session) take(tok tokens) {
	sess.refresh, sess.creds = tok.refresh, tok.creds
	sess.creds.AccountID, _ = accountOf(sess.arn)
}

// entry is what the keyring keeps of the session.
func (sess *session) entry() entry {
	return entry{Refresh: sess.refresh, DPoPKey: encodeKey(sess.key), ARN: sess.arn}
}

// forget drops the session's tokens from the redactor.
func (sess *session) forget() {
	safe.ForgetSecret(sess.refresh)
	safe.ForgetSecret(sess.creds.SecretAccessKey)
	safe.ForgetSecret(sess.creds.SessionToken)
}

// accountOf is the account an ARN names.
func accountOf(principal string) (string, error) {
	a, err := arn.Parse(principal)
	if err != nil {
		return "", err
	}
	if a.AccountID == "" {
		return "", fmt.Errorf("aws: %q names no account", principal)
	}
	return a.AccountID, nil
}

// option is an unexported build seam, so only in-package tests pass one.
type option func(*Service)

func withBrowser(open func(string) error) option { return func(s *Service) { s.open = open } }

func withNow(now func() time.Time) option { return func(s *Service) { s.now = now } }

func withAttemptTimeout(d time.Duration) option { return func(s *Service) { s.attemptTimeout = d } }

func withAttemptEnded(fn func()) option { return func(s *Service) { s.attemptEnded = fn } }

// New builds the service, Restoring, and restores a stored session on a
// goroutine. It reaches no network and makes no keyring call.
func New(cfg Config) (*Service, error) {
	return newWithOptions(cfg)
}

func newWithOptions(cfg Config, opts ...option) (*Service, error) {
	signin, err := url.Parse(cfg.SigninURL)
	if err != nil {
		return nil, err
	}
	ctx, stop := context.WithCancel(context.Background())
	s := &Service{
		signin:         signin,
		keyring:        cfg.Keyring,
		open:           browser.Open,
		httpClient:     llm.NoRedirectClient,
		now:            time.Now,
		attemptTimeout: attemptTimeout,
		ctx:            ctx,
		stop:           stop,
		hub:            watch.New(connectors.State{}),
		restored:       make(chan struct{}),
	}
	s.tx = s.hub.Sender()
	for _, opt := range opts {
		opt(s)
	}
	s.tokens = newTokenClient(signin.String(), s.httpClient, s.now)
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

// Close ends every goroutine the service started, waits for them, and drops
// the session's tokens from the redactor.
func (s *Service) Close() {
	s.stop()
	s.wg.Wait()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.session != nil {
		s.session.forget()
		s.session = nil
	}
}

// publishLocked makes st the state, stamped with the generation (s.mu held).
func (s *Service) publishLocked(st connectors.State) {
	st.Session = s.gen
	s.state = st
	s.tx.Send(st) //nolint:errcheck // Send never blocks, and the hub is never closed
}
