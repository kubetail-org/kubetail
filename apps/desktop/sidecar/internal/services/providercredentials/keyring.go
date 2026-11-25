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
	"encoding/json"
	"errors"
	"log/slog"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/oskeyring"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/safe"
)

// openRouterAccount is OpenRouter's saved key's account: its connector owns
// "openrouter-api-key".
const openRouterAccount = "openrouter-saved-api-key"

// entryVersion is the entry's stamp.
const entryVersion = 1

// entry is an account's value.
type entry struct {
	V   int    `json:"v"`
	Key string `json:"key"`
}

// account is id's keyring account under the service's name.
func account(id string) string {
	if id == "openrouter" {
		return openRouterAccount
	}
	return id + "-api-key"
}

func encode(key string) string {
	data, _ := json.Marshal(entry{V: entryVersion, Key: key}) // a struct of a string and an int encodes
	return string(data)
}

// restore reads p's entry, deletes one this build cannot use, then lands p's
// read. A newer version's entry is left and marked, so Delete leaves it too;
// an unreadable keyring writes nothing. No keyring call runs under a lock,
// and each ends with s.ctx: a Close during either lands nothing.
func (s *Service) restore(p *provider) {
	v, err := oskeyring.Read(s.ctx, s.keyring, account(p.ID), oskeyring.SlowAfter)
	if s.ctx.Err() != nil {
		return
	}
	key, newer, unusable := readKey(p.ID, v, err)
	if unusable {
		err := oskeyring.Delete(s.ctx, s.keyring, account(p.ID))
		if s.ctx.Err() != nil {
			return
		}
		if err != nil {
			slog.Warn("providercredentials: could not delete an unreadable keyring entry", "provider", p.ID, "err", err)
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if key != "" {
		safe.AddSecret(key)
	}
	p.saved, p.newer, p.read = key, newer, true
	s.landLocked(p)
}

// readKey is the key a read of id's entry answered, "" for none. newer is an
// entry a newer Kubetail wrote; unusable is one that does not parse, or holds a
// key Save would refuse.
func readKey(id, v string, err error) (key string, newer, unusable bool) {
	if errors.Is(err, oskeyring.ErrNoEntry) {
		return "", false, false
	}
	if err != nil {
		slog.Warn("providercredentials: could not read the keyring", "provider", id, "err", err)
		return "", false, false
	}
	// The stamp is read alone, so a newer shape is recognised before it fails
	// to decode as this one.
	var stamp struct {
		V int `json:"v"`
	}
	if json.Unmarshal([]byte(v), &stamp) == nil && stamp.V > entryVersion {
		slog.Warn("providercredentials: the saved key's entry is a newer Kubetail's; leaving it", "provider", id, "v", stamp.V)
		return "", true, false
	}
	var e entry
	if json.Unmarshal([]byte(v), &e) != nil || e.V != entryVersion || !isKey(e.Key) {
		return "", false, true
	}
	return e.Key, false, false
}
