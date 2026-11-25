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
	"encoding/json"
	"errors"
	"log/slog"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/oskeyring"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/safe"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/connectors"
)

// The keyring's accounts under the service's name.
const (
	// sessionAccount holds the session, in an entry.
	sessionAccount = "aws-session"
	// checkAccount is written and deleted before a sign-in opens the browser,
	// so a machine with no keyring learns so before the user signs in.
	checkAccount = "aws-keyring-check"
)

// entryVersion is the entry's stamp.
const entryVersion = 1

// entry is the session account's value: what a later launch needs to refresh
// the session. The credentials themselves are memory's alone.
type entry struct {
	V int `json:"v"`
	// Refresh is the refresh token, bound to DPoPKey.
	Refresh string `json:"refresh"`
	// DPoPKey is the session's P-256 private key, PEM-encoded.
	DPoPKey string `json:"dpopKey"`
	// ARN is the signed-in principal's, display only.
	ARN string `json:"arn"`
}

// saveEntry writes the session's entry.
func saveEntry(k oskeyring.Keyring, e entry) error {
	e.V = entryVersion
	data, _ := json.Marshal(e) // a struct of strings and an int encodes
	return k.Set(sessionAccount, string(data))
}

// parseEntry is the entry a read answered. found says the account holds
// something. A value that does not decode whole is answered as far as it did,
// so a newer shape still shows its stamp, and the caller judges the rest.
func parseEntry(v string, err error) (e entry, found bool, _ error) {
	if errors.Is(err, oskeyring.ErrNoEntry) {
		return entry{}, false, nil
	}
	if err != nil {
		return entry{}, false, err
	}
	_ = json.Unmarshal([]byte(v), &e)
	return e, true, nil
}

// restore reads the entry, deletes one this build cannot use, then publishes
// what it read: the session, connected, or signed out. No keyring call runs
// under s.mu, and each ends with s.ctx: a Close during either publishes
// nothing.
func (s *Service) restore() {
	v, err := oskeyring.Read(s.ctx, s.keyring, sessionAccount, oskeyring.SlowAfter)
	if s.ctx.Err() != nil {
		return
	}
	sess, unusable := readSession(v, err)
	if unusable {
		err := oskeyring.Delete(s.ctx, s.keyring, sessionAccount)
		if s.ctx.Err() != nil {
			return
		}
		if err != nil {
			slog.Warn("aws: could not delete an unusable keyring entry", "err", err)
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	st := connectors.State{Phase: connectors.SignedOut}
	if sess != nil {
		safe.AddSecret(sess.refresh)
		s.gen++
		s.session = sess
		st = connectors.State{Phase: connectors.Connected, Subject: sess.arn}
	}
	s.publishLocked(st)
	close(s.restored)
}

// readSession is the session a read of the entry answered, nil for none. A
// newer version's entry is left for the Kubetail that wrote it; unusable is one
// this build cannot use. A keyring that cannot be read is no proof of a
// sign-out, so it is neither.
func readSession(v string, err error) (sess *session, unusable bool) {
	e, found, err := parseEntry(v, err)
	if err != nil {
		slog.Warn("aws: could not read the keyring; treating as signed out", "err", err)
		return nil, false
	}
	if !found {
		return nil, false
	}
	if e.V > entryVersion {
		slog.Warn("aws: the session's entry is a newer Kubetail's; leaving it", "v", e.V)
		return nil, false
	}
	key, keyErr := parseKey(e.DPoPKey)
	_, arnErr := accountOf(e.ARN)
	if e.V != entryVersion || e.Refresh == "" || keyErr != nil || arnErr != nil {
		return nil, true
	}
	return &session{refresh: e.Refresh, key: key, arn: e.ARN}, false
}
