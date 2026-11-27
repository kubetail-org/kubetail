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

// Package appdb owns <data-dir>/app.db: the only place that opens the file, holding its
// single forward-only migration sequence and handing its pools to consumers.
//
// A SQLite file has ONE schema_migrations sequence, so its schema can't be co-owned by
// packages each embedding their own migrations — a consumer's tables go in the numbered
// files here. A change is the next numbered file; a shipped one is never edited or
// removed (docs/adr/2026-11-08-a-shipped-schema-changes-only-by-a-new-migration.md).
// app.db lives outside clusters/, which is reserved for the per-cluster cache files (one
// each, owned by internal/services/cluster), so a cache scan never mistakes it for one.
package appdb

import (
	"context"
	"embed"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/amorey/gobus/conflate"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/sqlitemigrate"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/sqlitepool"
)

// DefaultSweepInterval is the janitor's production cadence.
const DefaultSweepInterval = 5 * time.Minute

// readerPoolSize caps the reader's connections. The readers are the watch pumps, a
// handful per window, and each open connection is memory and a file descriptor.
const readerPoolSize = 4

// DB is app.db open for one process: the single-connection writer every mutation runs
// on, the query_only reader pool the watches read from, so a read never queues
// behind a write, and the change bus the two meet on.
type DB struct {
	Write *sqlitepool.Pool
	Read  *sqlitepool.Pool

	hub *conflate.Hub[string, struct{}]

	// path is the database file, which the janitor measures beside its -wal.
	path string
	// vacuumPages bounds what one sweep hands back: vacuumPagesPerSweep, or less in a test.
	vacuumPages int64
	// stopJanitor cancels the janitor and janitorDone closes once it has returned; both
	// nil when the DB opened with no interval.
	stopJanitor context.CancelFunc
	janitorDone chan struct{}
}

//go:embed migrations/*.sql
var migrationsFS embed.FS

// Open opens app.db, creating the parent dir and migrating as needed. The migration
// runs on the writer before the reader opens. sweepEvery is the janitor's cadence; zero
// runs none, which is what a test about anything else opens with.
func Open(path string, sweepEvery time.Duration) (*DB, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("mkdir %s: %w", filepath.Dir(path), err)
	}
	// One writer connection: the app-level tables are tiny and single-writer, so this
	// sidesteps SQLITE_BUSY entirely.
	w, err := sqlitepool.OpenWriter(path)
	if err != nil {
		return nil, err
	}
	if _, err := sqlitemigrate.Apply(context.Background(), w.DB, migrationsFS, "migrations"); err != nil {
		w.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	r := sqlitepool.OpenReader(path, readerPoolSize)
	db := &DB{
		Write:       w,
		Read:        r,
		hub:         conflate.New[string, struct{}](),
		path:        path,
		vacuumPages: vacuumPagesPerSweep,
	}
	if sweepEvery > 0 {
		ctx, cancel := context.WithCancel(context.Background())
		db.stopJanitor, db.janitorDone = cancel, make(chan struct{})
		go func() {
			defer close(db.janitorDone)
			runJanitor(ctx, db, sweepEvery)
		}()
	}
	return db, nil
}

// Close stops the janitor, ends every subscription, then releases both pools. The join
// is safe because Close holds no lock a sweep could wait on, and bounded because
// modernc interrupts a running statement when its context is cancelled — even a
// wal_checkpoint waiting on its busy handler returns as soon as the cancel lands. A
// subscriber must be joined first: one still running would read closed pools.
func (db *DB) Close() error {
	if db.stopJanitor != nil {
		db.stopJanitor()
		<-db.janitorDone
		// A sweep the cancel interrupted leaves SQLite's interrupt flag set on the
		// writer until its next statement starts, and the checkpoint that closing it
		// runs would fail on the flag and leave the -wal behind. Any statement clears it.
		_, _ = db.Write.Exec(`SELECT 1`)
	}
	db.hub.Close()
	return sqlitepool.CloseAll(sqlitepool.CloseBudget, db.Read, db.Write)
}

// The change bus over the file: a writer notifies a key after its commit, and every
// watcher subscribed to it re-reads. A ping carries nothing and a burst on one key
// coalesces to one, so a watcher re-reads once however many commits it slept
// through. → docs/adr/2026-08-26-store-change-ping-bus.md

// The keys, named here so a writer in one service and a watcher in another cannot
// spell one apart. Every service over the file notifies and subscribes through these.
const (
	// KeyChats: the chat list changed — a row added, removed, or moved in recency.
	KeyChats = "chats"
	// KeyClusters: a clusters row was inserted, edited, marked, or removed. Re-read
	// and diff; the mirror and the chat sweeper re-read too.
	KeyClusters = "clusters"
	// KeyIncidents: the monitor opened an incident or started or ended an
	// occurrence, or deleted incidents past their retention. A fold that only moves an
	// open incident's last_seen_at notifies nothing.
	KeyIncidents = "incidents"
	// KeyMemories: a memories row was inserted, rewritten or removed by a write of
	// the memory service. A cascade from a clusters delete notifies KeyClusters alone.
	KeyMemories = "memories"
	// KeyPermissions: a permission_modes row, or a permission_rules or
	// folder_grants row whose chat_id is NULL, was written by the settings store.
	KeyPermissions = "permissions"
	// KeyExecutables: an executables row was registered or removed by the
	// settings store.
	KeyExecutables = "executables"
	// KeySandboxPath: the PATH setting was saved, or a read of the login shell
	// retook the zones, whether or not shell_path changed. Folder checks read
	// both, so a folder grant's answer may have changed.
	KeySandboxPath = "sandbox-path"
)

// MessagesKey: the chat's message rows changed. Re-read and diff.
func MessagesKey(chatID string) string { return "messages/" + chatID }

// GrantsKey: the chat's permission_rules or folder_grants rows changed: a grant
// written or revoked. A cascade from a chat's delete notifies nothing here.
func GrantsKey(chatID string) string { return "grants/" + chatID }

// Notify wakes every subscriber of key. Call it after a commit, never inside the
// transaction, or a subscriber re-reads the old rows. It never blocks.
func (db *DB) Notify(key string) {
	_ = db.hub.Sender().Send(key, struct{}{})
}

// ListenForChanges returns a receiver of pings on any of keys, coalesced per key. Listen
// before the first read, or a commit between the two is missed. Close the receiver
// when done; DB.Close ends every receiver.
func (db *DB) ListenForChanges(keys ...string) *conflate.Receiver[string, struct{}] {
	if len(keys) == 0 {
		panic("appdb: ListenForChanges with no keys")
	}
	return db.hub.Receiver(db.hub.WithKeyFilter(func(k string) bool { return slices.Contains(keys, k) }))
}
