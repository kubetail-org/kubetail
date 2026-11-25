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
	"encoding/json"
	"errors"
	"log/slog"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/oskeyring"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/safe"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/connectors"
)

// The keyring's accounts under the service's name.
const (
	// keyAccount holds the key, in an entry.
	keyAccount = "openrouter-api-key"
	// checkAccount is written and deleted before a sign-in opens the browser,
	// so a machine with no keyring learns so before OpenRouter issues a key.
	checkAccount = "openrouter-keyring-check"
)

// entryVersion is the entry's stamp.
const entryVersion = 1

// entry is the key account's value.
type entry struct {
	V   int    `json:"v"`
	Key string `json:"key"`
}

// saveKey writes the key's entry.
func saveKey(k oskeyring.Keyring, key string) error {
	data, _ := json.Marshal(entry{V: entryVersion, Key: key}) // a struct of a string and an int encodes
	return k.Set(keyAccount, string(data))
}

// restore reads the entry, deletes one this build cannot parse, then
// publishes what it read: the key, connected, or signed out. No keyring call
// runs under s.mu, and each ends with s.ctx: a Close during either publishes
// nothing.
func (s *Service) restore() {
	v, err := oskeyring.Read(s.ctx, s.keyring, keyAccount, oskeyring.SlowAfter)
	if s.ctx.Err() != nil {
		return
	}
	key, unparsable := readKey(v, err)
	if unparsable {
		err := oskeyring.Delete(s.ctx, s.keyring, keyAccount)
		if s.ctx.Err() != nil {
			return
		}
		if err != nil {
			slog.Warn("openrouter: could not delete an unreadable keyring entry", "err", err)
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	st := connectors.State{Phase: connectors.SignedOut}
	if key != "" {
		safe.AddSecret(key)
		s.gen++
		s.key, s.restoredGen = key, s.gen
		st.Phase = connectors.Connected
	}
	s.publishLocked(st)
	close(s.restored)
}

// readKey is the key a read of the entry answered, "" for none. A newer
// version's entry is left for the Kubetail that wrote it; unparsable is one that
// does not parse. A keyring that cannot be read is no proof of a sign-out, so
// it is neither.
func readKey(v string, err error) (key string, unparsable bool) {
	if errors.Is(err, oskeyring.ErrNoEntry) {
		return "", false
	}
	if err != nil {
		slog.Warn("openrouter: could not read the keyring; treating as signed out", "err", err)
		return "", false
	}
	// The stamp is read alone, so a newer shape is recognised before it fails
	// to decode as this one.
	var stamp struct {
		V int `json:"v"`
	}
	if json.Unmarshal([]byte(v), &stamp) == nil && stamp.V > entryVersion {
		slog.Warn("openrouter: the key's entry is a newer Kubetail's; leaving it", "v", stamp.V)
		return "", false
	}
	var e entry
	if json.Unmarshal([]byte(v), &e) != nil || e.V != entryVersion || e.Key == "" {
		return "", true
	}
	return e.Key, false
}
