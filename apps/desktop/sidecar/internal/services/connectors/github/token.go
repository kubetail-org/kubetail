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
	"errors"
	"log/slog"
	"net/http"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/connectors"
)

// ErrSignedOut is a token asked for while the connector is not connected.
var ErrSignedOut = errors.New("github: not connected")

// Token is the connected account's token. While Restoring it waits for the
// restore.
func (s *Service) Token(ctx context.Context) (string, error) {
	if err := s.awaitRestore(ctx); err != nil {
		return "", err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.state.Phase != connectors.Connected {
		return "", ErrSignedOut
	}
	return s.token, nil
}

// awaitRestore waits for the restore to publish. ctx ends the wait with its
// error, and Close with ErrSignedOut.
func (s *Service) awaitRestore(ctx context.Context) error {
	select {
	case <-s.restored:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-s.ctx.Done():
		return ErrSignedOut
	}
}

// Refused is told of a 401 on token: if it is the connected one, it checks
// whether GitHub still honours it before dropping it. Any other token is not
// the connector's, and is ignored.
func (s *Service) Refused(token string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.state.Phase != connectors.Connected || token != s.token || s.checking {
		return
	}
	s.checking = true
	gen := s.gen
	s.wg.Go(func() { s.check(gen, token) })
}

// check asks GitHub's /user whether it still honours token, reading the status
// alone. Only a 401 drops the token, and only while it is still current:
// Kubetail cannot revoke it, so dropping one GitHub honours would strand it.
func (s *Service) check(gen uint64, token string) {
	if s.checkEnded != nil {
		defer s.checkEnded()
	}
	status, _, err := s.user(token)
	if err != nil {
		slog.Warn("github: could not check the token", "err", err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.checking = false
	if status != http.StatusUnauthorized || gen != s.gen {
		return
	}
	slog.Warn("github: GitHub no longer honours the token")
	s.forgetTokenLocked(connectors.State{Phase: connectors.Failed, Failure: connectors.KeyRejected}) //nolint:errcheck // a refused delete is the state's NotDeleted
}

// TokenSource is a GitHub token as the sandbox's GitHub proxy reads one.
type TokenSource interface {
	Token(ctx context.Context) (string, error)
	// Refused says GitHub answered a request carrying token 401.
	Refused(token string)
}

// Tokens is the proxy's token: the connector's while connected, fallback's
// once it is signed out.
func (s *Service) Tokens(fallback TokenSource) TokenSource {
	return tokens{s: s, fallback: fallback}
}

type tokens struct {
	s        *Service
	fallback TokenSource
}

func (t tokens) Token(ctx context.Context) (string, error) {
	token, err := t.s.Token(ctx)
	if errors.Is(err, ErrSignedOut) {
		return t.fallback.Token(ctx)
	}
	return token, err
}

// Refused tells both sources, each of which acts on its own token alone.
func (t tokens) Refused(token string) {
	t.s.Refused(token)
	t.fallback.Refused(token)
}
