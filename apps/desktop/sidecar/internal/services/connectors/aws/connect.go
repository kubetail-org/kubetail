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

package aws

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"strings"

	"golang.org/x/oauth2"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/oauth/browser"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/oskeyring"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/safe"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/connectors"
)

// callbackPath is the loopback's path, as the AWS CLI's.
const callbackPath = "/oauth/callback"

// attempt is one sign-in under way.
type attempt struct {
	gen uint64
	// ctx ends with the attempt, however it ends; cancel ends it.
	ctx             context.Context
	cancel          context.CancelFunc
	state, verifier string
	listener        *browser.Listener
}

// forget drops the attempt's secrets from the redactor.
func (a *attempt) forget() {
	safe.ForgetSecret(a.state)
	safe.ForgetSecret(a.verifier)
}

// Connect starts a sign-in and returns once the browser opens; the outcome
// arrives on the state. It is refused during an attempt, while connected, and
// while a session the keyring would not delete is still there; every other
// failure is the state's.
func (s *Service) Connect(context.Context) error {
	a, err := s.begin()
	if a == nil {
		return err
	}
	if err := s.openBrowser(a); err != nil {
		slog.Warn("aws: could not open the browser", "err", err)
		a.forget()
		s.endAttempt(a.gen, connectors.Refused)
		return nil
	}
	s.wg.Go(func() {
		if s.attemptEnded != nil {
			defer s.attemptEnded()
		}
		ctx, cancel := context.WithTimeout(a.ctx, s.attemptTimeout)
		defer cancel()
		s.finish(ctx, a)
	})
	return nil
}

// begin starts an attempt under a new generation, once the keyring is known to
// take a write. When it does not it answers no attempt and no error: the state
// says NotSaved, and no browser opens.
func (s *Service) begin() (*attempt, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	switch s.state.Phase {
	case connectors.Restoring:
		return nil, connectors.ErrRestoring
	case connectors.SigningIn:
		return nil, connectors.ErrSigningIn
	case connectors.Connected:
		return nil, connectors.ErrConnected
	case connectors.Failed:
		if s.state.Failure == connectors.NotDeleted {
			return nil, connectors.ErrNotDeleted
		}
	}
	s.gen++
	if err := oskeyring.Check(s.keyring, checkAccount); err != nil {
		slog.Error("aws: the keyring cannot hold a session", "err", err)
		s.publishLocked(connectors.State{Phase: connectors.Failed, Failure: connectors.NotSaved})
		return nil, nil
	}
	ctx, cancel := context.WithCancel(s.ctx)
	a := &attempt{gen: s.gen, ctx: ctx, cancel: cancel, state: oauth2.GenerateVerifier(), verifier: oauth2.GenerateVerifier()}
	safe.AddSecret(a.state)
	safe.AddSecret(a.verifier)
	s.attempt = a
	s.publishLocked(connectors.State{Phase: connectors.SigningIn})
	return a, nil
}

// openBrowser binds the loopback, then opens the browser on the authorize page
// that names it.
func (s *Service) openBrowser(a *attempt) error {
	var err error
	if a.listener, err = browser.Listen(a.state, callbackPath); err != nil { // coverage:ignore binding an ephemeral loopback port does not fail
		return err
	}
	if err := s.open(s.authorizeURL(a)); err != nil {
		a.listener.Close()
		return err
	}
	return nil
}

// authorizeURL is AWS's authorize page for a, under AWS's fixed client. AWS
// names S256 "SHA-256".
func (s *Service) authorizeURL(a *attempt) string {
	page := s.signin.JoinPath("v1", "authorize")
	page.RawQuery = url.Values{
		"response_type":         {"code"},
		"client_id":             {ClientID},
		"redirect_uri":          {a.listener.RedirectURL()},
		"state":                 {a.state},
		"scope":                 {"openid"},
		"code_challenge":        {oauth2.S256ChallengeFromVerifier(a.verifier)},
		"code_challenge_method": {"SHA-256"},
	}.Encode()
	return page.String()
}

// Cancel ends a sign-in under way. A code already being exchanged finishes
// into a stale generation, and its tokens are dropped.
func (s *Service) Cancel() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.state.Phase != connectors.SigningIn {
		return
	}
	s.stopAttemptLocked()
	s.gen++
	s.publishLocked(connectors.State{Phase: connectors.SignedOut})
}

// stopAttemptLocked ends the attempt's wait for the redirect (s.mu held).
func (s *Service) stopAttemptLocked() {
	if s.attempt != nil {
		s.attempt.cancel()
		s.attempt = nil
	}
}

// failLocked ends the attempt as failed (s.mu held).
func (s *Service) failLocked(f connectors.Failure) {
	s.stopAttemptLocked()
	s.publishLocked(connectors.State{Phase: connectors.Failed, Failure: f})
}

// endAttempt fails the attempt of gen, if it is still the one under way.
func (s *Service) endAttempt(gen uint64, f connectors.Failure) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if gen == s.gen && s.state.Phase == connectors.SigningIn {
		s.failLocked(f)
	}
}

// finish sees the attempt through to its outcome. The attempt's secrets are
// forgotten before the outcome is published, so no reader of the state finds
// them registered. Tokens that arrive after the attempt ended are dropped.
func (s *Service) finish(ctx context.Context, a *attempt) {
	sess, failure := s.authorize(ctx, a)
	a.listener.Close()
	a.forget()
	switch {
	case sess != nil:
		s.complete(a.gen, sess)
	case failure != "":
		s.endAttempt(a.gen, failure)
	}
}

// authorize waits for the redirect, then exchanges its code for a session. The
// wait answers Cancel, with neither a session nor a failure; the exchange does
// not, since the code is spent.
func (s *Service) authorize(ctx context.Context, a *attempt) (*session, connectors.Failure) {
	q, err := a.listener.Wait(ctx)
	if err != nil {
		authErr, declined := errors.AsType[*browser.AuthorizationError](err)
		switch {
		case declined && authErr.Code == "access_denied":
			return nil, connectors.Declined
		case errors.Is(err, context.DeadlineExceeded):
			return nil, connectors.TimedOut
		case errors.Is(err, context.Canceled):
			// Whoever cancelled the wait has ended the attempt.
			return nil, ""
		default:
			return nil, connectors.Refused
		}
	}
	if len(q["code"]) != 1 {
		return nil, connectors.Refused
	}
	return s.exchange(a, q.Get("code"))
}

// exchange trades code for a session under a new key. An answer of AWS's own
// that is an error, or whose ID token names no principal, is its refusal;
// anything else is Unavailable.
func (s *Service) exchange(a *attempt, code string) (*session, connectors.Failure) {
	ctx, cancel := context.WithTimeout(s.ctx, issuerTimeout)
	defer cancel()
	key := newDPoPKey()
	tok, err := s.tokens.exchange(ctx, key, code, a.verifier, a.listener.RedirectURL())
	if err != nil {
		slog.Warn("aws: the code exchange failed", "err", err)
		if refused(err) {
			return nil, connectors.Refused
		}
		return nil, connectors.Unavailable
	}
	principal, err := principalOf(tok.idToken)
	if err != nil {
		slog.Warn("aws: the ID token names no principal", "err", err)
		tok.forget()
		return nil, connectors.Refused
	}
	safe.ForgetSecret(tok.idToken)
	sess := &session{key: key, arn: principal}
	sess.take(tok)
	return sess, ""
}

// principalOf is the principal an ID token's sub names: the ARN of the IAM
// identity, role session or root that signed in. The token is the token
// endpoint's own answer over TLS, so its signature is not checked; the ARN is
// display only.
func principalOf(idToken string) (string, error) {
	parts := strings.Split(idToken, ".")
	if len(parts) != 3 {
		return "", errors.New("aws: the ID token is no JWT")
	}
	data, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return "", fmt.Errorf("aws: the ID token's claims: %w", err)
	}
	var claims struct {
		Sub string `json:"sub"`
	}
	if err := json.Unmarshal(data, &claims); err != nil {
		return "", fmt.Errorf("aws: the ID token's claims: %w", err)
	}
	if _, err := accountOf(claims.Sub); err != nil {
		return "", err
	}
	return claims.Sub, nil
}

// complete saves the session of the attempt of gen, then connects. A session
// whose attempt has ended, or that the keyring will not take, is dropped.
func (s *Service) complete(gen uint64, sess *session) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if gen != s.gen {
		sess.forget()
		return
	}
	if err := saveEntry(s.keyring, sess.entry()); err != nil {
		slog.Error("aws: could not save the session", "err", err)
		sess.forget()
		s.failLocked(connectors.NotSaved)
		return
	}
	s.stopAttemptLocked()
	s.session = sess
	s.publishLocked(connectors.State{Phase: connectors.Connected, Subject: sess.arn})
}
