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
	"encoding/json"
	"errors"
	"log/slog"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/oskeyring"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/safe"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/connectors"
)

// The keyring's accounts under the service's name.
const (
	// tokenAccount holds the token and its account, in an entry.
	tokenAccount = "github-token"
	// checkAccount is written and deleted before a sign-in asks GitHub for a
	// code, so a machine with no keyring learns so before GitHub grants a token.
	checkAccount = "github-keyring-check"
)

// entryVersion is the entry's stamp.
const entryVersion = 1

// entry is the token account's value. Login is the account's, display only.
type entry struct {
	V     int    `json:"v"`
	Token string `json:"token"`
	Login string `json:"login"`
}

// saveEntry writes the token's entry.
func saveEntry(k oskeyring.Keyring, token, login string) error {
	data, _ := json.Marshal(entry{V: entryVersion, Token: token, Login: login}) // a struct of strings and an int encodes
	return k.Set(tokenAccount, string(data))
}

// restore reads the entry, deletes one this build cannot parse, then
// publishes what it read: the token, connected, or signed out. No keyring call
// runs under s.mu, and each ends with s.ctx: a Close during either publishes
// nothing.
func (s *Service) restore() {
	v, err := oskeyring.Read(s.ctx, s.keyring, tokenAccount, oskeyring.SlowAfter)
	if s.ctx.Err() != nil {
		return
	}
	e, unparsable := readEntry(v, err)
	if unparsable {
		err := oskeyring.Delete(s.ctx, s.keyring, tokenAccount)
		if s.ctx.Err() != nil {
			return
		}
		if err != nil {
			slog.Warn("github: could not delete an unreadable keyring entry", "err", err)
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	st := connectors.State{Phase: connectors.SignedOut}
	if e.Token != "" {
		safe.AddSecret(e.Token)
		s.gen++
		s.token = e.Token
		st = connectors.State{Phase: connectors.Connected, Name: e.Login}
	}
	s.publishLocked(st)
	close(s.restored)
}

// readEntry is the entry a read answered, empty for none. A newer version's
// entry is left for the Kubetail that wrote it; unparsable is one that does not
// parse. A keyring that cannot be read is no proof of a sign-out, so it is
// neither.
func readEntry(v string, err error) (e entry, unparsable bool) {
	if errors.Is(err, oskeyring.ErrNoEntry) {
		return entry{}, false
	}
	if err != nil {
		slog.Warn("github: could not read the keyring; treating as signed out", "err", err)
		return entry{}, false
	}
	// The stamp is read alone, so a newer shape is recognised before it fails
	// to decode as this one.
	var stamp struct {
		V int `json:"v"`
	}
	if json.Unmarshal([]byte(v), &stamp) == nil && stamp.V > entryVersion {
		slog.Warn("github: the token's entry is a newer Kubetail's; leaving it", "v", stamp.V)
		return entry{}, false
	}
	if json.Unmarshal([]byte(v), &e) != nil || e.V != entryVersion || e.Token == "" || e.Login == "" {
		return entry{}, true
	}
	return e, false
}
