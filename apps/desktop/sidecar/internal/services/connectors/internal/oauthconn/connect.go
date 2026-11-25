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
	"context"
	"errors"
	"log/slog"
	"net/url"
	"slices"
	"strings"

	"github.com/google/uuid"
	"golang.org/x/oauth2"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/oauth"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/oauth/browser"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/oskeyring"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/safe"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/connectors"
)

// attempt is one sign-in under way.
type attempt struct {
	gen uint64
	// ctx ends with the attempt, however it ends; cancel ends it.
	ctx    context.Context
	cancel context.CancelFunc
	// reg is the registration as the attempt began under it.
	reg registration
	// consent asks the authorize page for consent again.
	consent                bool
	state, verifier, nonce string
	endpoints              oauth.Endpoints
	listener               *browser.Listener
}

// Connect starts a sign-in and returns once the browser opens; the outcome
// arrives on the state. fresh forgets the saved client and account first,
// keeping the host id: it is the one way to change accounts. It is refused
// while Restoring, during an attempt and while connected; every other failure
// is the state's.
func (s *Service) Connect(ctx context.Context, fresh bool) error {
	_, err := s.Start(ctx, fresh)
	return err
}

// Start is Connect, also answering the failure of a sign-in that could not
// start: the stores' check, discovery, the loopback or the browser. It answers
// a failure only when it made the state Failed with it. Its steps run under the
// service's context, not ctx.
func (s *Service) Start(_ context.Context, fresh bool) (connectors.Failure, error) {
	a, failure, err := s.begin(fresh)
	if a == nil {
		return failure, err
	}
	if failure = s.openBrowser(a); failure != "" {
		// A step that a Close or a Disconnect cancelled is no failed sign-in.
		if s.ctx.Err() != nil || !s.endAttempt(a.gen, failure) {
			failure = ""
		}
		return failure, nil
	}
	s.wg.Go(func() {
		defer a.listener.Close()
		ctx, cancel := context.WithTimeout(a.ctx, s.attemptTimeout)
		defer cancel()
		s.finish(ctx, a)
	})
	return "", nil
}

// begin starts an attempt under a new generation, once both stores are known
// to take a write. When one does not it answers no attempt and NotSaved, which
// the state says too, and no browser opens.
func (s *Service) begin(fresh bool) (*attempt, connectors.Failure, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	switch s.state.Phase {
	case connectors.Restoring:
		return nil, "", connectors.ErrRestoring
	case connectors.SigningIn:
		return nil, "", connectors.ErrSigningIn
	case connectors.Connected:
		return nil, "", connectors.ErrConnected
	}
	s.gen++
	reg := s.reg.get()
	if fresh {
		reg = reg.forgotten()
	}
	if reg.Data == nil && s.p.NewData != nil {
		reg.Data = s.p.NewData()
	}
	// Written whatever the row is believed to hold: a sign-out whose own write
	// failed can leave its session there.
	reg.SessionID = ""
	err := s.reg.save(reg)
	if err == nil {
		err = oskeyring.Check(s.keyring, s.checkAccount())
	}
	if err != nil {
		slog.Error("oauth: the stores cannot hold a sign-in", "provider", s.p.ID, "err", err)
		s.publishLocked(connectors.State{Phase: connectors.Failed, Failure: connectors.NotSaved})
		return nil, connectors.NotSaved, nil
	}
	ctx, cancel := context.WithCancel(s.ctx)
	s.attempt = &attempt{
		gen: s.gen, ctx: ctx, cancel: cancel, reg: reg, consent: s.consent,
		state: oauth2.GenerateVerifier(), verifier: oauth2.GenerateVerifier(), nonce: oauth2.GenerateVerifier(),
	}
	s.publishLocked(connectors.State{Phase: connectors.SigningIn})
	return s.attempt, "", nil
}

// openBrowser binds the loopback, then opens the browser on the authorize
// URL that names it. It answers why it could not.
func (s *Service) openBrowser(a *attempt) connectors.Failure {
	ctx, cancel := context.WithTimeout(a.ctx, issuerTimeout)
	defer cancel()
	var err error
	if a.endpoints, err = s.discover(ctx); err != nil {
		slog.Warn("oauth: discovery failed", "provider", s.p.ID, "err", err)
		return connectors.Unavailable
	}
	if a.listener, err = browser.Listen(a.state, s.p.CallbackPath); err != nil { // coverage:ignore binding an ephemeral loopback port does not fail
		return connectors.Refused
	}
	if a.ctx.Err() != nil { // coverage:ignore a Cancel landing between discovery and here; no test can time it
		a.listener.Close()
		return connectors.Refused
	}
	if err := s.open(s.authorizeURL(a)); err != nil {
		slog.Warn("oauth: could not open the browser", "provider", s.p.ID, "err", err)
		a.listener.Close()
		return connectors.Refused
	}
	return ""
}

// authorizeURL is the authorize request: a sign-in with no client registers
// one, any other runs under its client and hints the saved account. The
// provider adds its own parameters.
func (s *Service) authorizeURL(a *attempt) string {
	clientID := s.clientID(a.reg)
	registering := clientID == ""
	extra := url.Values{}
	if s.p.Authorize != nil {
		extra = s.p.Authorize(a.reg.Data, registering)
	}
	extra.Set("nonce", a.nonce)
	if registering {
		clientID = s.p.FirstClientID
	} else if a.reg.Email != "" {
		extra.Set("login_hint", a.reg.Email)
	}
	if a.consent {
		extra.Set("prompt", "consent")
	}
	return s.newClient(a.endpoints, clientID).AuthCodeURL(a.state, a.verifier, a.listener.RedirectURL(), extra)
}

// Cancel ends a sign-in under way. A code already being exchanged finishes
// into a stale generation, and its tokens are revoked.
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

// endAttempt fails the attempt of gen, if it is still the one under way, and
// reports whether it did.
func (s *Service) endAttempt(gen uint64, f connectors.Failure) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.endAttemptLocked(gen, f)
}

func (s *Service) endAttemptLocked(gen uint64, f connectors.Failure) bool {
	if gen != s.gen || s.state.Phase != connectors.SigningIn {
		return false
	}
	s.failLocked(f)
	return true
}

// finish waits for the redirect, then exchanges the code, verifies the
// account and saves the session. The wait answers Cancel; the exchange does
// not, since a code being exchanged has been spent, and its tokens are owed a
// revocation if the attempt ended meanwhile.
func (s *Service) finish(ctx context.Context, a *attempt) {
	q, err := a.listener.Wait(ctx)
	if err != nil {
		authErr, declined := errors.AsType[*browser.AuthorizationError](err)
		switch {
		case declined && authErr.Code == "access_denied":
			s.endAttempt(a.gen, connectors.Declined)
		case errors.Is(err, context.DeadlineExceeded):
			s.endAttempt(a.gen, connectors.TimedOut)
		case errors.Is(err, context.Canceled):
			// Whoever cancelled the wait has ended the attempt.
		default:
			s.endAttempt(a.gen, connectors.Refused)
		}
		return
	}
	known := s.clientID(a.reg)
	clientID, ok := s.issuedClient(q.Get("client_id"), known)
	if !ok || len(q["code"]) != 1 {
		s.endAttempt(a.gen, connectors.Refused)
		return
	}
	// Saved before the exchange, so a failed exchange does not register a second
	// client, and kept whatever the attempt comes to.
	if known == "" && !s.saveClientID(a.gen, clientID) {
		return
	}

	ctx, cancel := context.WithTimeout(s.ctx, issuerTimeout)
	defer cancel()
	client := s.newClient(a.endpoints, clientID)
	tok, err := client.Exchange(ctx, q.Get("code"), a.verifier, a.listener.RedirectURL())
	if err != nil {
		slog.Warn("oauth: the code exchange failed", "provider", s.p.ID, "err", err)
		s.endAttempt(a.gen, issuerFailure(err))
		return
	}
	// A session with no refresh token could not outlive its access token, nor be
	// revoked on Disconnect.
	if tok.RefreshToken == "" {
		slog.Warn("oauth: the code exchange answered no refresh token", "provider", s.p.ID)
		s.endAttempt(a.gen, connectors.Refused)
		return
	}
	for _, t := range []string{tok.AccessToken, tok.RefreshToken, tok.IDToken} {
		safe.AddSecret(t)
	}
	id, err := client.VerifyNonce(ctx, tok.IDToken, a.nonce)
	safe.ForgetSecret(tok.IDToken)
	if err != nil {
		slog.Warn("oauth: the ID token did not verify", "provider", s.p.ID, "err", err)
		s.mu.Lock()
		defer s.mu.Unlock()
		// Failed first: a revocation that fails flags the state it finds, and
		// publishing the failure after it would drop the flag.
		s.endAttemptLocked(a.gen, issuerFailure(err))
		s.revokeLocked(a.gen, clientID, tok)
		return
	}
	s.complete(a.gen, clientID, tok, id)
}

// issuerFailure is what a failed exchange or verification says: one that
// never reached the issuer is Unavailable, anything else Refused.
func issuerFailure(err error) connectors.Failure {
	if _, ok := errors.AsType[*url.Error](err); ok {
		return connectors.Unavailable
	}
	return connectors.Refused
}

// issuedClient is the client a redirect names: a sign-in that registers must
// name a client of its own; one under a known client may name none, or that one.
func (s *Service) issuedClient(issued, known string) (string, bool) {
	if known == "" {
		return issued, issued != "" && issued != s.p.FirstClientID
	}
	return known, issued == "" || issued == known
}

// saveClientID records a newly issued client for the attempt of gen, failing
// the attempt when it cannot. An ended attempt records nothing, so its redirect
// cannot replace the client a later attempt registered.
func (s *Service) saveClientID(gen uint64, clientID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if gen != s.gen || s.state.Phase != connectors.SigningIn {
		return false
	}
	reg := s.reg.get()
	reg.ClientID = clientID
	if err := s.reg.save(reg); err != nil {
		slog.Error("oauth: could not record the registration", "provider", s.p.ID, "err", err)
		s.failLocked(connectors.NotSaved)
		return false
	}
	return true
}

// complete saves a verified sign-in, in one critical section under its
// generation: the refresh token's entry, then the row, then memory. Connected
// is published only once the row lands, and a sign-in that cannot be saved is
// revoked rather than held to retry.
func (s *Service) complete(gen uint64, clientID string, tok oauth.Token, id oauth.Identity) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if gen != s.gen {
		s.revokeLocked(gen, clientID, tok)
		return
	}
	sessionID, failure := s.saveSessionLocked(tok, id)
	if failure != "" {
		s.revokeLocked(gen, clientID, tok)
		s.failLocked(failure)
		return
	}
	s.stopAttemptLocked()
	s.session = &session{id: sessionID, clientID: clientID, refresh: tok.RefreshToken, access: tok}
	s.publishLocked(connectors.State{Phase: connectors.Connected, Subject: id.UserID, Email: id.Email, Name: id.Name})
}

// saveSessionLocked checks what the sign-in granted and to whom, then writes
// the refresh token's entry and the row, both under a session id minted here.
// It answers the id, or why the sign-in is not kept, and leaves no entry behind
// then (s.mu held).
func (s *Service) saveSessionLocked(tok oauth.Token, id oauth.Identity) (string, connectors.Failure) {
	// A sign-in without the required scope is not kept, and the next authorize
	// asks again. A response with no scope granted what was asked (RFC 6749 §5.1).
	s.consent = s.p.RequiredScope != "" && tok.Scope != "" &&
		!slices.Contains(strings.Fields(tok.Scope), s.p.RequiredScope)
	if s.consent {
		return "", connectors.ScopeNotGranted
	}
	reg := s.reg.get()
	if reg.Subject != "" && reg.Subject != id.UserID {
		return "", connectors.AccountMismatch
	}
	sessionID := uuid.NewString()
	if err := saveEntry(s.keyring, s.refreshAccount(), sessionID, tok.RefreshToken); err != nil {
		slog.Error("oauth: could not save the refresh token", "provider", s.p.ID, "err", err)
		return "", connectors.NotSaved
	}
	reg.Subject, reg.Email, reg.Name, reg.SessionID = id.UserID, id.Email, id.Name, sessionID
	if err := s.reg.save(reg); err != nil {
		slog.Error("oauth: could not record the session", "provider", s.p.ID, "err", err)
		if err := s.keyring.Delete(s.refreshAccount()); err != nil {
			slog.Error("oauth: could not delete the refresh token", "provider", s.p.ID, "err", err)
		}
		return "", connectors.NotSaved
	}
	return sessionID, ""
}
