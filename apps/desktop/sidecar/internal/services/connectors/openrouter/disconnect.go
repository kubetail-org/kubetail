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
	"log/slog"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/safe"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/connectors"
)

// Disconnect forgets the key on this machine. The key stays live on OpenRouter
// until the user deletes it there. A delete the keyring refuses is returned,
// and calling again retries it.
func (s *Service) Disconnect() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.state.Phase == connectors.Restoring {
		return connectors.ErrRestoring
	}
	s.stopAttemptLocked()
	// Only a held key, or one a refused delete left, is in the keyring; any
	// other entry is a newer Kubetail's.
	if s.key == "" && !(s.state.Phase == connectors.Failed && s.state.Failure == connectors.NotDeleted) {
		s.gen++
		s.publishLocked(connectors.State{Phase: connectors.SignedOut})
		return nil
	}
	return s.forgetKeyLocked(connectors.State{Phase: connectors.SignedOut})
}

// forgetKeyLocked ends the key in memory, then in the keyring, and publishes
// next. Memory goes first: the generation moves on, so every binding ends
// whatever the keyring answers. A refused delete publishes Failed with
// NotDeleted and is returned (s.mu held).
func (s *Service) forgetKeyLocked(next connectors.State) error {
	safe.ForgetSecret(s.key)
	s.key = ""
	s.gen++
	if err := s.keyring.Delete(keyAccount); err != nil {
		slog.Error("openrouter: could not delete the key", "err", err)
		s.publishLocked(connectors.State{Phase: connectors.Failed, Failure: connectors.NotDeleted})
		return err
	}
	s.publishLocked(next)
	return nil
}
