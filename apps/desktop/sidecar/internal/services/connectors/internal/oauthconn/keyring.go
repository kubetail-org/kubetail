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
	"encoding/json"
	"errors"
	"log/slog"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/oskeyring"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/safe"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/connectors"
)

// The keyring's accounts under the service's name, each prefixed with the
// provider's id.

// refreshAccount holds the refresh token, in an entry.
func (s *Service) refreshAccount() string { return s.p.ID + "-refresh-token" }

// checkAccount is written and deleted before a sign-in opens the browser, so a
// machine with no keyring learns so before the user signs in.
func (s *Service) checkAccount() string { return s.p.ID + "-keyring-check" }

// entryVersion is the entry's stamp.
const entryVersion = 1

// entry is the refresh account's value: the refresh token and the sign-in it
// belongs to, which the registration's row names too.
type entry struct {
	V       int    `json:"v"`
	Token   string `json:"token"`
	Session string `json:"session"`
}

// saveEntry writes account's entry: the refresh token of the sign-in sessionID
// names.
func saveEntry(k oskeyring.Keyring, account, sessionID, token string) error {
	data, _ := json.Marshal(entry{V: entryVersion, Token: token, Session: sessionID}) // a struct of strings and an int encodes
	return k.Set(account, string(data))
}

// parseEntry is the entry a read answered. found says the account holds
// something; a value this build cannot read is found and names no session.
func parseEntry(v string, err error) (e entry, found bool, _ error) {
	if errors.Is(err, oskeyring.ErrNoEntry) {
		return entry{}, false, nil
	}
	if err != nil {
		return entry{}, false, err
	}
	if json.Unmarshal([]byte(v), &e) != nil || e.V != entryVersion {
		return entry{}, true, nil
	}
	return e, true, nil
}

// restoration is what the restore makes of the stores: the state to publish,
// the session it carries, and what is left to clean up.
type restoration struct {
	state    connectors.State
	session  *session
	clearRow bool
	// deleteEntry is an entry no session can use.
	deleteEntry bool
}

// restore reads the refresh token's entry, deletes one no session can use,
// then publishes what it read. No keyring call runs under s.mu, and each ends
// with s.ctx: a Close during either publishes nothing.
func (s *Service) restore(rowErr error) {
	v, err := oskeyring.Read(s.ctx, s.keyring, s.refreshAccount(), oskeyring.SlowAfter)
	if s.ctx.Err() != nil {
		return
	}
	r := s.judge(rowErr, v, err)
	if r.deleteEntry {
		err := oskeyring.Delete(s.ctx, s.keyring, s.refreshAccount())
		if s.ctx.Err() != nil {
			return
		}
		if err != nil {
			slog.Warn("oauth: could not delete a stale keyring entry", "provider", s.p.ID, "err", err)
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.applyRestoreLocked(r)
	close(s.restored)
}

// judge is what the read makes of the stores. A session exists only when the
// row and the keyring's entry name the same sign-in. Anything else is a
// sign-in or a sign-out that a crash, a lost commit or a failed delete left
// half done, or a store restored from a backup: the row is signed out and the
// entry deleted. A row or a keyring that cannot be read is no proof of a
// sign-out: nothing is written.
func (s *Service) judge(rowErr error, v string, readErr error) restoration {
	signedOut := restoration{state: connectors.State{Phase: connectors.SignedOut}}
	if rowErr != nil {
		// The entry is left for a launch that can read the row, unless a Connect
		// rewrites the row first, which ends that session.
		slog.Warn("oauth: could not read the registration; treating as signed out", "provider", s.p.ID, "err", rowErr)
		return signedOut
	}
	e, found, err := parseEntry(v, readErr)
	if err != nil {
		slog.Warn("oauth: could not read the keyring; treating as signed out", "provider", s.p.ID, "err", err)
		return signedOut
	}
	reg := s.reg.get()
	clientID := s.clientID(reg)
	if reg.SessionID != "" && e.Session == reg.SessionID && clientID != "" && e.Token != "" {
		return restoration{
			state:   connectors.State{Phase: connectors.Connected, Subject: reg.Subject, Email: reg.Email, Name: reg.Name},
			session: &session{id: reg.SessionID, clientID: clientID, refresh: e.Token},
		}
	}
	signedOut.clearRow = reg.SessionID != ""
	signedOut.deleteEntry = found
	return signedOut
}

// applyRestoreLocked writes the row r clears and publishes r (s.mu held).
func (s *Service) applyRestoreLocked(r restoration) {
	if r.clearRow {
		reg := s.reg.get()
		reg.SessionID = ""
		if err := s.reg.save(reg); err != nil {
			slog.Warn("oauth: could not record the sign-out", "provider", s.p.ID, "err", err)
		}
	}
	if r.session != nil {
		safe.AddSecret(r.session.refresh)
		s.gen++
		s.session = r.session
		s.restoredGen = s.gen
	}
	s.publishLocked(r.state)
}
