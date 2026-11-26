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
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/llm"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/connectors"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/internal/signer"
)

// anySession is the generation the service itself signs under: whichever key
// is current. A key's own generation is never 0.
const anySession = 0

// errOffBase is a request outside the API base the key is for.
var errOffBase = errors.New("openrouter: the request is not for OpenRouter's API")

// Sign sets r's bearer to the key. It signs only the API base and what is
// under it, and only while connected. While Restoring it waits for the
// restore, r's context or Close.
func (s *Service) Sign(r *http.Request) error { return s.sign(r, anySession) }

// Refused is told of a 401: it checks whether OpenRouter still honours the key
// before dropping it.
func (s *Service) Refused(r *http.Request) { s.refused(r, anySession) }

// Bind is a binding for the current key alone. With no key, during a sign-in
// included, it has ended for good: an attempt's generation becomes its key's,
// so a binding made during one would come alive later. While Restoring it is
// the key the restore publishes, never one connected after.
func (s *Service) Bind() llm.Signer {
	s.mu.Lock()
	defer s.mu.Unlock()
	switch s.state.Phase {
	case connectors.Restoring:
		return signer.Pending(s.restored, s.ctx.Done(), s.restoredBinding)
	case connectors.Connected:
		return sessionBinding{s: s, gen: s.gen}
	}
	return signer.Ended{}
}

// restoredBinding is the binding of the key the restore published.
func (s *Service) restoredBinding() llm.Signer {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.restoredGen == 0 {
		return signer.Ended{}
	}
	return sessionBinding{s: s, gen: s.restoredGen}
}

// sessionBinding signs under one generation, and ends once it has moved.
type sessionBinding struct {
	s   *Service
	gen uint64
}

func (b sessionBinding) Sign(r *http.Request) error { return b.s.sign(r, b.gen) }
func (b sessionBinding) Refused(r *http.Request)    { b.s.refused(r, b.gen) }

// keyLocked is the key gen names, "" when it has ended (s.mu held).
func (s *Service) keyLocked(gen uint64) string {
	if (gen != anySession && gen != s.gen) || s.state.Phase != connectors.Connected {
		return ""
	}
	return s.key
}

func (s *Service) sign(r *http.Request, gen uint64) error {
	if !signer.Covers(s.base, r.URL) {
		return errOffBase
	}
	if err := s.awaitRestore(r.Context()); err != nil {
		return err
	}
	s.mu.Lock()
	key := s.keyLocked(gen)
	s.mu.Unlock()
	if key == "" {
		return llm.ErrSignedOut
	}
	r.Header.Set("Authorization", "Bearer "+key)
	return nil
}

// awaitRestore waits for the restore to publish. ctx ends the wait with its
// error, and Close with llm.ErrSignedOut.
func (s *Service) awaitRestore(ctx context.Context) error {
	select {
	case <-s.restored:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-s.ctx.Done():
		return llm.ErrSignedOut
	}
}

// refused starts a check of the key r carried, if it is still the key gen
// names and no check is under way.
func (s *Service) refused(r *http.Request, gen uint64) {
	sent := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	s.mu.Lock()
	defer s.mu.Unlock()
	key := s.keyLocked(gen)
	if key == "" || sent != key || s.checking {
		return
	}
	s.checking = true
	// The key's own generation: gen may be anySession.
	current := s.gen
	s.wg.Go(func() { s.check(current, key) })
}

// check asks OpenRouter whether it still honours key, reading the status
// alone. Only a 401 drops the key, and only while it is still current: Kubetail
// cannot revoke a key, so dropping one OpenRouter honours would strand it.
func (s *Service) check(gen uint64, key string) {
	if s.checkEnded != nil {
		defer s.checkEnded()
	}
	status, err := s.keyStatus(key)
	if err != nil {
		slog.Warn("openrouter: could not check the key", "err", err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.checking = false
	if status != http.StatusUnauthorized || gen != s.gen {
		return
	}
	slog.Warn("openrouter: OpenRouter no longer honours the key")
	s.forgetKeyLocked(connectors.State{Phase: connectors.Failed, Failure: connectors.KeyRejected}) //nolint:errcheck // a refused delete is the state's NotDeleted
}

// keyStatus is the status GET <base>/key answers under key.
func (s *Service) keyStatus(key string) (int, error) {
	ctx, cancel := context.WithTimeout(s.ctx, s.issuerTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.base.JoinPath("key").String(), nil)
	if err != nil { // coverage:ignore the base parsed in New, and the method is a constant
		return 0, err
	}
	req.Header.Set("Authorization", "Bearer "+key)
	resp, err := llm.NoRedirectClient.Do(req)
	if err != nil {
		return 0, err
	}
	resp.Body.Close()
	return resp.StatusCode, nil
}
