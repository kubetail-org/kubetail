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

// Package sqlitepool opens the sidecar's SQLite pools: a store's writer, its readers, and
// read-only connections for statements the caller did not write. It is the one home of the
// open contract, so every store opens its files the same way.
package sqlitepool

import (
	"fmt"
	"os"
	"time"

	"modernc.org/sqlite"
)

// OpenWriter opens a store's writer at path: one connection, so writes serialize at the
// pool rather than fighting at the SQLite layer, with the standard PRAGMAs in the DSN
// (WAL, 5s busy_timeout, synchronous=NORMAL, foreign_keys, auto_vacuum=INCREMENTAL,
// immediate txlock).
func OpenWriter(path string) (*Pool, error) {
	// modernc applies these _pragma values on each new connection.
	//
	// auto_vacuum must reach a file before anything writes to it, and journal_mode=WAL
	// writes the header — so the order these run in is load-bearing, and it is not the
	// order written here. modernc's applyQueryParams issues busy_timeout first and then
	// sorts the rest lexicographically, which puts auto_vacuum ahead of journal_mode.
	dsn := "file:" + path +
		"?_pragma=journal_mode(WAL)" +
		"&_pragma=busy_timeout(5000)" +
		"&_pragma=synchronous(NORMAL)" +
		"&_pragma=foreign_keys(on)" +
		"&_pragma=auto_vacuum(incremental)" +
		"&_txlock=immediate"
	db := newPool(newConnector(&sqlite.Driver{}, dsn))
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	db.SetConnMaxIdleTime(5 * time.Minute)

	// The pool is lazy — the file does not exist until a connection runs. Ping first,
	// then fix the mode: a file written by an older build kept the umask it was born with.
	// The siblings are absent on a fresh open; SQLite creates them from the database's
	// mode once that is fixed.
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, err
	}
	for _, p := range []string{path, path + "-wal", path + "-shm"} {
		if err := os.Chmod(p, 0o600); err != nil && !os.IsNotExist(err) { // coverage:ignore only a file another user owns refuses the chmod, and a test cannot make one
			db.Close()
			return nil, err
		}
	}

	// A file this build creates is already INCREMENTAL — the DSN sets it. This is for one
	// written by a build that predates that: SQLite ignores the pragma once a table exists,
	// so without the rewrite a janitor's incremental_vacuum is a no-op on it forever.
	const autoVacuumIncremental = 2
	var mode int
	if err := db.QueryRow(`PRAGMA auto_vacuum`).Scan(&mode); err != nil { // coverage:ignore the Ping's pragmas already read the header this reads
		db.Close()
		return nil, fmt.Errorf("read auto_vacuum: %w", err)
	}
	if mode != autoVacuumIncremental {
		if _, err := db.Exec(`PRAGMA auto_vacuum=INCREMENTAL; VACUUM;`); err != nil {
			db.Close()
			return nil, fmt.Errorf("set auto_vacuum: %w", err)
		}
	}
	return db, nil
}

// OpenReader opens a WAL reader pool at path. Same shape as OpenWriter, minus everything
// the writer owns: no journal_mode, no synchronous, no foreign_keys, and above all no
// _txlock — BEGIN IMMEDIATE takes a write lock, which query_only refuses. busy_timeout
// stays, because a reader still waits on a lock.
//
// query_only is the enforcement, not the caller's sql.TxOptions: a read transaction that
// forgets ReadOnly would otherwise take the WAL write lock and contend with the writer.
//
// The pool keeps every connection it opens until the idle timeout takes it: database/sql's
// default of 2 idle would close exactly the connections the pool was sized to keep, and
// reopening one re-runs the pragmas.
func OpenReader(path string, maxConns int) *Pool {
	dsn := "file:" + path +
		"?_pragma=busy_timeout(5000)" +
		"&_pragma=query_only(true)"
	db := newPool(newConnector(&sqlite.Driver{}, dsn))
	db.SetMaxOpenConns(maxConns)
	db.SetMaxIdleConns(maxConns)
	db.SetConnMaxIdleTime(5 * time.Minute)
	return db
}

// OpenQuery opens a pool of read-only connections at path for statements the caller
// did not write, through d, so d's functions and hooks reach these connections and no
// others. mode=ro is set at open, where no pragma a statement runs can undo it. The pool
// keeps no idle connection: every checkout is a fresh one, so nothing left on a
// connection reaches the next checkout. The file must exist; nothing is opened until the
// first checkout.
func OpenQuery(d *sqlite.Driver, path string, maxConns int) *Pool {
	db := newPool(queryConnector(d, path))
	db.SetMaxOpenConns(maxConns)
	db.SetMaxIdleConns(0)
	return db
}

// OpenOwnQuery is OpenQuery for statements the caller wrote. Those leave nothing on a
// connection, so the pool keeps every connection it opens until the idle timeout, as
// OpenReader does, and d's hooks run once per connection rather than once per statement.
func OpenOwnQuery(d *sqlite.Driver, path string, maxConns int) *Pool {
	db := newPool(queryConnector(d, path))
	db.SetMaxOpenConns(maxConns)
	db.SetMaxIdleConns(maxConns)
	db.SetConnMaxIdleTime(5 * time.Minute)
	return db
}

func queryConnector(d *sqlite.Driver, path string) *connector {
	return newConnector(d, "file:"+path+"?mode=ro&_pragma=busy_timeout(5000)")
}
