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

// Package sqlstmt prepares a table of SQL statements once on a SQLite file's writer
// and reader pools, and routes each call to the right copy. modernc has no
// compiled-statement cache — it prepares, runs and finalizes on every call — so a text
// handed to a pool at a call site is compiled every time.
package sqlstmt

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync/atomic"
)

// ErrClosed is a transaction asked of a closed set.
var ErrClosed = errors.New("sqlstmt: set is closed")

// Pool says where a statement is prepared. A read that runs inside a write
// transaction needs the writer's copy: Tx.StmtContext refuses a statement prepared on
// another pool, and the reader would miss the transaction's own writes.
type Pool uint8

const (
	Writer Pool = iota // a write: prepared on the writer alone
	Reader             // a read that never runs inside a write transaction
	Both               // a read some caller runs inside a write transaction
)

// Statement is one entry of a table: the text and where it is prepared.
type Statement struct {
	Text string
	On   Pool
}

func OnWriter(text string) Statement { return Statement{Text: text, On: Writer} }
func OnReader(text string) Statement { return Statement{Text: text, On: Reader} }
func OnBoth(text string) Statement   { return Statement{Text: text, On: Both} }

// Set is a table prepared once on the two pools and indexed by the caller's own id
// type. The pools stay the caller's: the set closes its statements alone.
type Set[ID ~int] struct {
	writeDB, readDB *sql.DB
	write, read     []*sql.Stmt
	// closed refuses a transaction once Close ran: Tx.StmtContext re-prepares a
	// closed statement, so a transaction would otherwise read on past the close.
	closed atomic.Bool
}

// Prepare compiles table on both pools, so a statement that will not compile fails
// the open rather than the call that first reaches it. Each compiles on the connection
// the call lands on and is cached there; a pool's other connections compile it the
// first time they serve it.
func Prepare[ID ~int](ctx context.Context, write, read *sql.DB, table []Statement) (*Set[ID], error) {
	for i, st := range table {
		if st.Text == "" {
			return nil, fmt.Errorf("statement %d has no text", i)
		}
	}
	w, err := prepare(ctx, write, table, Writer)
	if err != nil {
		return nil, err
	}
	r, err := prepare(ctx, read, table, Reader)
	if err != nil {
		closeAll(w)
		return nil, err
	}
	return &Set[ID]{writeDB: write, readDB: read, write: w, read: r}, nil
}

// prepare compiles the entries pool serves. A nil slot is an id this pool does not.
func prepare(ctx context.Context, db *sql.DB, table []Statement, pool Pool) ([]*sql.Stmt, error) {
	out := make([]*sql.Stmt, len(table))
	for i, st := range table {
		if st.On != pool && st.On != Both {
			continue
		}
		prepared, err := db.PrepareContext(ctx, st.Text)
		if err != nil {
			closeAll(out)
			return nil, fmt.Errorf("prepare %q: %w", st.Text, err)
		}
		out[i] = prepared
	}
	return out, nil
}

func closeAll(set []*sql.Stmt) error {
	var errs []error
	for _, st := range set {
		if st != nil {
			errs = append(errs, st.Close())
		}
	}
	return errors.Join(errs...)
}

// Close finalizes the statements. The pools are the caller's to close.
func (s *Set[ID]) Close() error {
	s.closed.Store(true)
	return errors.Join(closeAll(s.read), closeAll(s.write))
}

// Stmts issues on the pools.
func (s *Set[ID]) Stmts() Stmts[ID] { return Stmts[ID]{set: s} }

// InTx runs fn in one write transaction, issuing on it.
func (s *Set[ID]) InTx(ctx context.Context, fn func(Stmts[ID]) error) error {
	if s.closed.Load() {
		return ErrClosed
	}
	tx, err := s.writeDB.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // no-op after Commit
	if err := fn(s.tx(tx, s.write)); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	return nil
}

// InReadTx runs fn in one read-only transaction on the reader, issuing on it, and
// rolls it back: a read that pairs rows with a position needs both from one snapshot.
func (s *Set[ID]) InReadTx(ctx context.Context, fn func(Stmts[ID]) error) error {
	if s.closed.Load() {
		return ErrClosed
	}
	tx, err := s.readDB.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // a read transaction, never committed
	return fn(s.tx(tx, s.read))
}

// tx issues inside tx, rebinding from prepared: the copies on the pool tx is on.
func (s *Set[ID]) tx(tx *sql.Tx, prepared []*sql.Stmt) Stmts[ID] {
	return Stmts[ID]{set: s, tx: tx, prepared: prepared, bound: make(map[ID]*sql.Stmt)}
}

// Stmts issues a set's statements, on tx when there is one and on the pools when
// there is not, so one helper serves a call made either way.
type Stmts[ID ~int] struct {
	set *Set[ID]
	tx  *sql.Tx // nil: run on the pools
	// prepared is the pool tx is on, whose copies it rebinds. Tx.StmtContext refuses
	// a statement prepared on another pool.
	prepared []*sql.Stmt
	// bound holds the transaction's rebinding of each id. Tx.StmtContext allocates a
	// fresh statement per call and registers it db-wide, released only at commit, so
	// a transaction would otherwise pile up one per call. The map is shared by every
	// copy of this value and needs no lock: one goroutine owns the transaction.
	bound map[ID]*sql.Stmt
}

// stmt resolves an id: on the pools the reader's copy when there is one, else the
// writer's; inside a transaction its pool's copy, rebound onto it once. The pool is
// the table's call, never the helper's: a write that returns rows still runs on the
// writer. An id the transaction's pool does not hold is a table error — a read filed
// Reader reached inside a write transaction — so it panics rather than failing the
// call with an error the caller would wrap as the row's.
func (s Stmts[ID]) stmt(ctx context.Context, id ID) *sql.Stmt {
	if s.tx == nil {
		if prepared := s.set.read[id]; prepared != nil {
			return prepared
		}
		return s.set.write[id]
	}
	if bound, ok := s.bound[id]; ok {
		return bound
	}
	prepared := s.prepared[id]
	if prepared == nil {
		panic(fmt.Sprintf("sqlstmt: statement %d is not prepared on the transaction's pool", id))
	}
	bound := s.tx.StmtContext(ctx, prepared)
	s.bound[id] = bound
	return bound
}

func (s Stmts[ID]) Exec(ctx context.Context, id ID, args ...any) (sql.Result, error) {
	return s.stmt(ctx, id).ExecContext(ctx, args...)
}

func (s Stmts[ID]) Query(ctx context.Context, id ID, args ...any) (*sql.Rows, error) {
	return s.stmt(ctx, id).QueryContext(ctx, args...)
}

func (s Stmts[ID]) QueryRow(ctx context.Context, id ID, args ...any) *sql.Row {
	return s.stmt(ctx, id).QueryRowContext(ctx, args...)
}
