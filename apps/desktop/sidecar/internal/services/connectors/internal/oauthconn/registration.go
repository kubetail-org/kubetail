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
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/appdb"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/sqlstmt"
)

// registration is the provider's row in app.db: what the provider issued this
// installation and who signed in. No token.
type registration struct {
	// Data is the row's provider_data: what the provider's sign-in alone keeps.
	Data     map[string]string
	ClientID string
	Subject  string
	Email    string
	// Name is the account's display name; empty when the ID token named none.
	Name string
	// SessionID names the sign-in whose refresh token the keyring holds; empty
	// is signed out. A session exists only when the keyring's entry names it too.
	SessionID string
}

// forgotten is r with no client, no account and no session, so the next sign-in
// registers anew. The provider's data stays.
func (r registration) forgotten() registration {
	return registration{Data: r.Data}
}

type stmtID int

const (
	stmtSelect stmtID = iota
	stmtUpsert
)

var statements = []sqlstmt.Statement{
	stmtSelect: sqlstmt.OnReader(`SELECT ifnull(client_id, ''), ifnull(subject, ''), ifnull(email, ''),
		ifnull(name, ''), ifnull(session_id, ''), provider_data FROM oauth_connections WHERE provider = ?`),
	stmtUpsert: sqlstmt.OnWriter(`INSERT INTO oauth_connections
		(provider, client_id, subject, email, name, session_id, provider_data, created_at, updated_at)
		VALUES (?1, nullif(?2, ''), nullif(?3, ''), nullif(?4, ''), nullif(?5, ''), nullif(?6, ''), ?7, ?8, ?8)
		ON CONFLICT (provider) DO UPDATE SET client_id = excluded.client_id, subject = excluded.subject,
			email = excluded.email, name = excluded.name, session_id = excluded.session_id,
			provider_data = excluded.provider_data, updated_at = excluded.updated_at`),
}

// registrationWriter writes the row; a field, so a test fails a write.
type registrationWriter func(registration) error

// registrationStore is the row and what memory holds of it. Not synchronized:
// the service calls it under its lock.
type registrationStore struct {
	// provider is the row's key: the catalog's provider id.
	provider string
	stmts    *sqlstmt.Set[stmtID]
	write    registrationWriter
	cur      registration
}

// openRegistration prepares the statements over db for provider's row. Memory
// holds the zero registration until load.
func openRegistration(db *appdb.DB, provider string) (*registrationStore, error) {
	stmts, err := sqlstmt.Prepare[stmtID](context.Background(), db.Write.DB, db.Read.DB, statements)
	if err != nil {
		return nil, fmt.Errorf("prepare oauth connection statements: %w", err)
	}
	s := &registrationStore{provider: provider, stmts: stmts}
	s.write = s.upsert
	return s, nil
}

// load reads the row into memory. No row is the zero registration, and a row it
// cannot read leaves memory as it was.
func (s *registrationStore) load() error {
	var reg registration
	var data sql.NullString
	err := s.stmts.Stmts().QueryRow(context.Background(), stmtSelect, s.provider).
		Scan(&reg.ClientID, &reg.Subject, &reg.Email, &reg.Name, &reg.SessionID, &data)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if data.Valid {
		if err := json.Unmarshal([]byte(data.String), &reg.Data); err != nil {
			return fmt.Errorf("provider_data: %w", err)
		}
	}
	s.cur = reg
	return nil
}

func (s *registrationStore) upsert(reg registration) error {
	var data sql.NullString
	if reg.Data != nil {
		encoded, _ := json.Marshal(reg.Data) // a map of strings encodes
		data = sql.NullString{String: string(encoded), Valid: true}
	}
	_, err := s.stmts.Stmts().Exec(context.Background(), stmtUpsert,
		s.provider, reg.ClientID, reg.Subject, reg.Email, reg.Name, reg.SessionID, data, time.Now().UnixMilli())
	return err
}

func (s *registrationStore) get() registration { return s.cur }

// save writes next on every call, one equal to what memory holds included: a
// failed write leaves memory as it was, and its retry must reach the row.
// Memory changes only after the write returns.
func (s *registrationStore) save(next registration) error {
	if err := s.write(next); err != nil {
		return err
	}
	s.cur = next
	return nil
}

func (s *registrationStore) close() error { return s.stmts.Close() }
