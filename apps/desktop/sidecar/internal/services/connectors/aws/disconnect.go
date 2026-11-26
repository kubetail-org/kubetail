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
	"log/slog"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/connectors"
)

// Disconnect forgets the session on this machine. It revokes nothing: AWS's
// revocation is an IAM-signed call this connector does not make, so the
// refresh token stays valid at AWS until the session expires. A delete the
// keyring refuses is returned, and calling again retries it.
func (s *Service) Disconnect() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.state.Phase == connectors.Restoring {
		return connectors.ErrRestoring
	}
	s.stopAttemptLocked()
	// Only a held session, or one a refused delete left, is in the keyring; any
	// other entry is a newer Kubetail's.
	if s.session == nil && !(s.state.Phase == connectors.Failed && s.state.Failure == connectors.NotDeleted) {
		s.gen++
		s.publishLocked(connectors.State{Phase: connectors.SignedOut})
		return nil
	}
	if err := s.clearLocked(); err != nil {
		s.publishLocked(connectors.State{Phase: connectors.Failed, Failure: connectors.NotDeleted})
		return err
	}
	s.publishLocked(connectors.State{Phase: connectors.SignedOut})
	return nil
}

// clearLocked ends the session in memory, then deletes its entry, and answers
// the delete's error. Memory goes first: the generation moves on, so a refresh
// under way is dropped whatever the keyring answers (s.mu held).
func (s *Service) clearLocked() error {
	if s.session != nil {
		s.session.forget()
		s.session = nil
	}
	s.gen++
	if err := s.keyring.Delete(sessionAccount); err != nil {
		slog.Error("aws: could not delete the session", "err", err)
		return err
	}
	return nil
}
