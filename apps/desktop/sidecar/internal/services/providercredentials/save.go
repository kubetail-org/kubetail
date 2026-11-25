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

package providercredentials

import (
	"log/slog"
	"strings"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/safe"
)

// maxKeyLen bounds a saved key: no vendor's is near it.
const maxKeyLen = 512

// Save keeps key as id's saved key, in the keyring and in memory, replacing
// any saved one. The key is not checked against the provider. It is refused
// while the provider is Restoring.
func (s *Service) Save(id, key string) error {
	p, err := s.keyed(id)
	if err != nil {
		return err
	}
	key = strings.TrimSpace(key)
	if !isKey(key) {
		return ErrBadKey
	}
	s.writes.Lock()
	defer s.writes.Unlock()
	if s.restoring(p) {
		return ErrRestoring
	}
	safe.AddSecret(key)
	if err := s.keyring.Set(account(id), encode(key)); err != nil {
		// Logged while the key is registered, in case the error quotes it.
		slog.Warn("providercredentials: could not save the key", "provider", id, "err", err)
		safe.ForgetSecret(key)
		return ErrNotSaved
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	safe.ForgetSecret(p.saved)
	p.saved = key
	p.keyGen++
	p.undeleted, p.rejected, p.newer = false, false, false
	s.publishLocked(p)
	return nil
}

// Delete deletes id's saved key, from memory first, so every binding made
// under a key ends whatever the keyring answers. It deletes the entry even with no
// key held, since a keyring unread at launch may still hold one; only a newer
// Kubetail's entry is left. A delete the keyring refuses is reported on the
// provider, and calling again retries it. It is refused while the provider is
// Restoring.
func (s *Service) Delete(id string) error {
	p, err := s.keyed(id)
	if err != nil {
		return err
	}
	s.writes.Lock()
	defer s.writes.Unlock()

	s.mu.Lock()
	if p.restoringLocked() {
		s.mu.Unlock()
		return ErrRestoring
	}
	if p.newer {
		s.mu.Unlock()
		return nil
	}
	if p.saved != "" {
		safe.ForgetSecret(p.saved)
		p.saved = ""
		p.keyGen++
		// The mark was that key's. On a retry it is the environment key's, and stays.
		p.rejected = false
		s.publishLocked(p)
	}
	s.mu.Unlock()

	err = s.keyring.Delete(account(id))

	s.mu.Lock()
	defer s.mu.Unlock()
	p.undeleted = err != nil
	s.publishLocked(p)
	if err != nil {
		slog.Error("providercredentials: could not delete the saved key", "provider", id, "err", err)
		return ErrNotDeleted
	}
	return nil
}

// keyed is id's provider when it takes a key.
func (s *Service) keyed(id string) (*provider, error) {
	p, ok := s.byID[id]
	switch {
	case !ok:
		return nil, ErrUnknownProvider
	case p.KeyVar == "":
		return nil, ErrNoKey
	}
	return p, nil
}

// isKey is a key the redactor can register, of printable ASCII alone.
func isKey(key string) bool {
	if len(key) < safe.MinSecretLen || len(key) > maxKeyLen {
		return false
	}
	for i := range len(key) {
		if key[i] < 0x21 || key[i] > 0x7e {
			return false
		}
	}
	return true
}
