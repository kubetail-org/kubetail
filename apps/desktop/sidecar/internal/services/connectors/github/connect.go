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

package github

import (
	"context"
	"log/slog"

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
}

// Connect starts a sign-in: it asks GitHub for a code, shows it on the state,
// and opens the browser on GitHub's page, where the user enters it. It returns
// once the browser opens; the outcome arrives on the state. It is refused with
// no OAuth App, during an attempt, while connected, and while a token the
// keyring would not delete is still there; every other failure is the state's.
func (s *Service) Connect(context.Context) error {
	a, err := s.begin()
	if a == nil {
		return err
	}
	code, failure := s.requestCode(a.ctx)
	if failure != "" {
		s.endAttempt(a.gen, failure)
		return nil
	}
	// With the public client id, the device code claims the token.
	safe.AddSecret(code.DeviceCode)
	if !s.showCode(a.gen, code.UserCode) || !s.openPage(a.gen, code.VerificationURI) {
		safe.ForgetSecret(code.DeviceCode)
		return nil
	}
	s.wg.Go(func() { s.finish(a, code) })
	return nil
}

// openPage opens the browser on GitHub's page for the code. A browser that
// does not open ends the attempt of gen.
func (s *Service) openPage(gen uint64, page string) bool {
	if err := s.open(page); err != nil {
		slog.Warn("github: could not open the browser", "err", err)
		s.endAttempt(gen, connectors.Refused)
		return false
	}
	return true
}

// finish polls for the token until the code expires or the attempt does.
func (s *Service) finish(a *attempt, code deviceCode) {
	if s.attemptEnded != nil {
		defer s.attemptEnded()
	}
	lifetime := s.attemptTimeout
	if code.ExpiresIn > 0 {
		lifetime = min(lifetime, s.pace(code.ExpiresIn))
	}
	ctx, cancel := context.WithTimeout(a.ctx, lifetime)
	defer cancel()
	token, failure := s.poll(ctx, code)
	// Forgotten before the outcome is published, so no reader of the state
	// finds it registered.
	safe.ForgetSecret(code.DeviceCode)
	if token == "" {
		if failure != "" {
			s.endAttempt(a.gen, failure)
		}
		return
	}
	login, failure := s.fetchLogin(token)
	if failure != "" {
		safe.ForgetSecret(token)
		s.endAttempt(a.gen, failure)
		return
	}
	s.complete(a.gen, token, login)
}

// complete saves the token of the attempt of gen, then connects. A token whose
// attempt has ended, or that the keyring will not take, is dropped.
func (s *Service) complete(gen uint64, token, login string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if gen != s.gen {
		safe.ForgetSecret(token)
		return
	}
	if err := saveEntry(s.keyring, token, login); err != nil {
		slog.Error("github: could not save the token", "err", err)
		safe.ForgetSecret(token)
		s.failLocked(connectors.NotSaved)
		return
	}
	s.stopAttemptLocked()
	s.token = token
	s.publishLocked(connectors.State{Phase: connectors.Connected, Name: login})
}

// begin starts an attempt under a new generation, once the keyring is known to
// take a write. When it does not it answers no attempt and no error: the state
// says NotSaved, and GitHub is asked for nothing.
func (s *Service) begin() (*attempt, error) {
	if s.clientID == "" {
		return nil, connectors.ErrNoClient
	}
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
		slog.Error("github: the keyring cannot hold a token", "err", err)
		s.publishLocked(connectors.State{Phase: connectors.Failed, Failure: connectors.NotSaved})
		return nil, nil
	}
	ctx, cancel := context.WithCancel(s.ctx)
	s.attempt = &attempt{gen: s.gen, ctx: ctx, cancel: cancel}
	s.publishLocked(connectors.State{Phase: connectors.SigningIn})
	return s.attempt, nil
}

// showCode publishes the user's code for the attempt of gen, if it is still
// the one under way, and reports whether it is.
func (s *Service) showCode(gen uint64, userCode string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if gen != s.gen || s.state.Phase != connectors.SigningIn { // coverage:ignore a Cancel landing between the code's answer and here; no test can time it
		return false
	}
	s.publishLocked(connectors.State{Phase: connectors.SigningIn, UserCode: userCode})
	return true
}

// Cancel ends a sign-in under way. A token GitHub grants after it is dropped.
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

// stopAttemptLocked ends the attempt's requests and its polling (s.mu held).
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
