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

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/oauth"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/safe"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/connectors"
)

// Disconnect signs out on this machine, then asks the provider to revoke the refresh
// token, on a goroutine of the service's. A revocation the provider never confirms
// arrives on the state. The registration stays. It errs only when
// the sign-out could not be recorded and the refresh token's entry is still
// there: that session would return at the next launch. It is refused while
// Restoring.
func (s *Service) Disconnect(context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.state.Phase == connectors.Restoring {
		return connectors.ErrRestoring
	}
	var clientID, refresh string
	if s.session != nil {
		clientID, refresh = s.session.clientID, s.session.refresh
	}
	s.stopAttemptLocked()
	if refresh != "" {
		// clearLocked forgets the session's registration; this one is revoke's to
		// forget, so the token stays blanked in any log line its failure writes.
		safe.AddSecret(refresh)
	}
	rowErr, keyErr := s.clearLocked(false)
	s.publishLocked(connectors.State{Phase: connectors.SignedOut})
	if refresh != "" {
		s.revokeLocked(s.gen, clientID, oauth.Token{RefreshToken: refresh})
	}
	if rowErr != nil && keyErr != nil {
		return errors.Join(rowErr, keyErr)
	}
	return nil
}

// revokeLocked revokes tok's refresh token under clientID on a goroutine, once
// more on a failure. One the provider never confirms sets RevocationUnconfirmed, so
// every window can say so, while the state it belongs to stands: gen's, or for
// tokens that arrived after gen ended, the state of the one change that ended
// it. A later Connect is never flagged (s.mu held).
func (s *Service) revokeLocked(gen uint64, clientID string, tok oauth.Token) {
	if gen != s.gen {
		gen++
	}
	s.wg.Go(func() {
		defer safe.ForgetSecret(tok.RefreshToken)
		defer safe.ForgetSecret(tok.AccessToken)
		for range 2 {
			ctx, cancel := context.WithTimeout(s.ctx, revokeTimeout)
			client, err := s.clientFor(ctx, clientID)
			if err == nil {
				err = client.Revoke(ctx, tok.RefreshToken)
			}
			cancel()
			if err == nil || s.ctx.Err() != nil {
				return
			}
			slog.Warn("oauth: revocation failed", "provider", s.p.ID, "err", err)
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		if gen == s.gen {
			st := s.state
			st.RevocationUnconfirmed = true
			s.publishLocked(st)
		}
	})
}
