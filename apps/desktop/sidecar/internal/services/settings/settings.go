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

// Package settings is the sidecar's user settings, rows in app.db, and the
// sandbox state derived from them.
package settings

import (
	"context"
	"fmt"
	"time"

	"github.com/amorey/gobus/conflate"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/appdb"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/sqlstmt"
)

// ChangeNotifications names a setting that changed, one notice per appdb key. Notices for
// one key coalesce, and none carries a value: the reader re-reads the setting.
type ChangeNotifications = *conflate.Receiver[string, struct{}]

// Store keeps the approval modes, the always rules, the always folder grants,
// the sandbox settings, the PATH setting, the login shell's last PATH and the
// registered executables in app.db. It notifies each write of the
// permissions, the executables and the PATH.
// Safe for concurrent use.
type Store struct {
	db    *appdb.DB
	stmts *sqlstmt.Set[stmtID]
	now   func() time.Time
}

// Open prepares the store's statements over db.
func Open(db *appdb.DB) (*Store, error) {
	st, err := sqlstmt.Prepare[stmtID](context.Background(), db.Write.DB, db.Read.DB, statements)
	if err != nil {
		return nil, fmt.Errorf("prepare settings statements: %w", err)
	}
	return &Store{db: db, stmts: st, now: time.Now}, nil
}

// Close closes the store's statements.
func (s *Store) Close() error {
	return s.stmts.Close()
}
