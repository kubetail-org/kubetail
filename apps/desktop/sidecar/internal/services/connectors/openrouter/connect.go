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

package openrouter

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/url"

	"golang.org/x/oauth2"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/oauth/browser"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/oskeyring"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/safe"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/llm"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/connectors"
)

const (
	// callbackPath is the loopback's path. Our state rides its query, which
	// OpenRouter keeps when it appends the code.
	callbackPath = "/callback"
	// maxExchangeBody caps the exchange's answer.
	maxExchangeBody = 64 << 10
)

// attempt is one sign-in under way.
type attempt struct {
	gen uint64
	// ctx ends with the attempt, however it ends; cancel ends it.
	ctx                        context.Context
	cancel                     context.CancelFunc
	state, verifier, challenge string
	listener                   *browser.Listener
}

// forget drops the attempt's secrets from the redactor.
func (a *attempt) forget() {
	safe.ForgetSecret(a.state)
	safe.ForgetSecret(a.verifier)
	safe.ForgetSecret(a.challenge)
}

// Connect starts a sign-in and returns once the browser opens; the outcome
// arrives on the state. It is refused during an attempt, while connected, and
// while a key the keyring would not delete is still there; every other failure
// is the state's.
func (s *Service) Connect(context.Context) error {
	a, err := s.begin()
	if a == nil {
		return err
	}
	if err := s.open(s.authPageURL(a)); err != nil {
		slog.Warn("openrouter: could not open the browser", "err", err)
		a.forget()
		s.endAttempt(a.gen, connectors.Refused)
		return nil
	}
	s.wg.Go(func() {
		ctx, cancel := context.WithTimeout(a.ctx, s.attemptTimeout)
		defer cancel()
		s.finish(ctx, a)
	})
	return nil
}

// begin starts an attempt under a new generation and binds its loopback, once
// the keyring is known to take a write. When it does not it answers no attempt
// and no error: the state says NotSaved, and no browser opens.
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
		slog.Error("openrouter: the keyring cannot hold a key", "err", err)
		s.publishLocked(connectors.State{Phase: connectors.Failed, Failure: connectors.NotSaved})
		return nil, nil
	}
	state := oauth2.GenerateVerifier()
	listener, err := browser.Listen(state, callbackPath)
	if err != nil { // coverage:ignore binding an ephemeral loopback port does not fail
		slog.Error("openrouter: could not bind the loopback", "err", err)
		s.publishLocked(connectors.State{Phase: connectors.Failed, Failure: connectors.Refused})
		return nil, nil
	}
	ctx, cancel := context.WithCancel(s.ctx)
	verifier := oauth2.GenerateVerifier()
	a := &attempt{
		gen: s.gen, ctx: ctx, cancel: cancel, listener: listener,
		state: state, verifier: verifier, challenge: oauth2.S256ChallengeFromVerifier(verifier),
	}
	safe.AddSecret(a.state)
	safe.AddSecret(a.verifier)
	safe.AddSecret(a.challenge)
	s.attempt = a
	s.publishLocked(connectors.State{Phase: connectors.SigningIn})
	return a, nil
}

// authPageURL is OpenRouter's auth page for a: the challenge, the key's label,
// and the loopback with our state in its query.
func (s *Service) authPageURL(a *attempt) string {
	callback := a.listener.RedirectURL() + "?" + url.Values{"state": {a.state}}.Encode()
	page := s.auth.JoinPath("auth")
	page.RawQuery = url.Values{
		"callback_url":          {callback},
		"code_challenge":        {a.challenge},
		"code_challenge_method": {"S256"},
		"key_label":             {KeyLabel},
	}.Encode()
	return page.String()
}

// Cancel ends a sign-in under way. A code already being exchanged finishes
// into a stale generation, and its key is dropped.
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

// stopAttemptLocked ends the attempt's wait for the callback and closes its
// loopback, so no callback is answered once the attempt has ended (s.mu held).
func (s *Service) stopAttemptLocked() {
	if s.attempt != nil {
		s.attempt.cancel()
		s.attempt.listener.Close()
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
// them registered. A key that arrives after the attempt ended is dropped.
func (s *Service) finish(ctx context.Context, a *attempt) {
	key, failure := s.authorize(ctx, a)
	a.listener.Close()
	a.forget()
	switch {
	case key != "":
		s.complete(a.gen, key)
	case failure != "":
		s.endAttempt(a.gen, failure)
	}
}

// authorize waits for the callback, then exchanges its code for the key. The
// wait answers Cancel, with neither a key nor a failure; the exchange does
// not, since the code is spent.
func (s *Service) authorize(ctx context.Context, a *attempt) (string, connectors.Failure) {
	q, err := a.listener.Wait(ctx)
	if err != nil {
		switch {
		case errors.Is(err, context.DeadlineExceeded):
			return "", connectors.TimedOut
		case errors.Is(err, context.Canceled):
			// Whoever cancelled the wait has ended the attempt.
			return "", ""
		default:
			return "", connectors.Refused
		}
	}
	if len(q["code"]) != 1 {
		return "", connectors.Refused
	}
	return s.exchange(q.Get("code"), a.verifier)
}

// exchange trades code for the key. Only a 200 whose JSON names a non-empty
// key is one; a 4xx is OpenRouter's refusal, and anything else Unavailable.
func (s *Service) exchange(code, verifier string) (string, connectors.Failure) {
	ctx, cancel := context.WithTimeout(s.ctx, s.issuerTimeout)
	defer cancel()
	body, _ := json.Marshal(map[string]string{"code": code, "code_verifier": verifier, "code_challenge_method": "S256"}) // a map of strings encodes
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.auth.JoinPath("api/v1/auth/keys").String(), bytes.NewReader(body))
	if err != nil { // coverage:ignore the origin parsed in New, and the method is a constant
		return "", connectors.Unavailable
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := llm.NoRedirectClient.Do(req)
	if err != nil {
		slog.Warn("openrouter: the exchange failed", "err", err)
		return "", connectors.Unavailable
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode >= 400 && resp.StatusCode < 500:
		slog.Warn("openrouter: the exchange was refused", "status", resp.StatusCode)
		return "", connectors.Refused
	case resp.StatusCode != http.StatusOK:
		slog.Warn("openrouter: the exchange answered", "status", resp.StatusCode)
		return "", connectors.Unavailable
	}
	var answer struct {
		Key string `json:"key"`
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxExchangeBody))
	if err != nil || json.Unmarshal(data, &answer) != nil || answer.Key == "" {
		slog.Warn("openrouter: the exchange answered no key")
		return "", connectors.Unavailable
	}
	safe.AddSecret(answer.Key)
	return answer.Key, ""
}

// complete saves the key of the attempt of gen, then connects. A key whose
// attempt has ended, or that the keyring will not take, is dropped.
func (s *Service) complete(gen uint64, key string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if gen != s.gen {
		safe.ForgetSecret(key)
		return
	}
	if err := saveKey(s.keyring, key); err != nil {
		slog.Error("openrouter: could not save the key", "err", err)
		safe.ForgetSecret(key)
		s.failLocked(connectors.NotSaved)
		return
	}
	s.stopAttemptLocked()
	s.key = key
	s.publishLocked(connectors.State{Phase: connectors.Connected})
}
