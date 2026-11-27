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

// The store's query side: statements the model wrote, and the sidecar's own readings,
// over views of the cache on read-only connections of their own.
package kubestore

import (
	"context"
	"database/sql"
	"database/sql/driver"
	_ "embed"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	"modernc.org/sqlite"
	sqlitelib "modernc.org/sqlite/lib"
)

// queryPoolSize caps a file's concurrent connections in each query pool.
const queryPoolSize = 2

//go:embed query_views.sql
var queryViews string

// newQueryDriver is the driver of a manager's query connections alone, so body() and the
// views reach no other connection in the process. Each call of body() adds one to bodyCalls.
func newQueryDriver(bodyCalls *atomic.Int64) *sqlite.Driver {
	d := &sqlite.Driver{}
	d.MustRegisterDeterministicScalarFunction("body", 1, func(fc *sqlite.FunctionContext, args []driver.Value) (driver.Value, error) {
		bodyCalls.Add(1)
		return bodyFunc(fc, args)
	})
	d.MustRegisterDeterministicScalarFunction("quantity", 1, quantityFunc)
	d.RegisterConnectionHook(func(conn sqlite.ExecQuerierContext, _ string) error {
		_, err := conn.ExecContext(context.Background(), queryViews, nil)
		return err
	})
	return d
}

// bodyFunc is SQL's body(): a stored raw_json as JSON text. NULL for NULL, and for a blob
// that will not decompress, as a watch serves a body that will not load. A statement can
// hand it any blob, so it inflates at most queryMaxLength: SQLite checks the length only
// of a value already built, and a Go callback is not interrupted.
func bodyFunc(_ *sqlite.FunctionContext, args []driver.Value) (driver.Value, error) {
	b, ok := args[0].([]byte)
	if !ok {
		return nil, nil
	}
	out, err := decompressRawUpTo(b, queryMaxLength)
	if errors.Is(err, errRawTooLarge) {
		return nil, fmt.Errorf("body past the %d MiB limit", queryMaxLength>>20)
	}
	if err != nil {
		return nil, nil
	}
	return string(out), nil
}

// quantityFunc is SQL's quantity(): a text through parseQuantity, a number as a real, and
// NULL for anything else and for text the parse refuses, so a sum skips it rather than
// failing the query.
func quantityFunc(_ *sqlite.FunctionContext, args []driver.Value) (driver.Value, error) {
	switch v := args[0].(type) {
	case string:
		if f, ok := parseQuantity(v); ok {
			return f, nil
		}
	case int64:
		return float64(v), nil
	case float64:
		return v, nil
	}
	return nil, nil
}

// QueryResult is one statement's answer. Cells are what the driver scans: int64, float64,
// string, []byte or nil. More says rows were left out, past maxRows or maxBytes.
type QueryResult struct {
	Columns []string
	Rows    [][]any
	More    bool
}

// QueryError is SQLite refusing the statement: its message, and nothing of the store's.
type QueryError struct {
	Message string
}

func (e *QueryError) Error() string { return e.Message }

// Query runs one statement over the views on a query connection, with args bound to its
// placeholders, and answers at most maxRows rows and maxBytes of text and blob cells.
//
// The statement runs wrapped: modernc interrupts a cancelled query only while it takes the
// first row, and rows.Next never looks at the context, so a statement slow past its first
// row would outlive its deadline. Materializing it makes all of it run in that first
// step. The wrap also makes it a subquery, which only a query can be.
func (s *Store) Query(ctx context.Context, text string, maxRows, maxBytes int, args ...any) (QueryResult, error) {
	var res QueryResult
	err := s.withFile(ctx, func(ctx context.Context, f *file) error {
		conn, err := queryConn(ctx, f.queryDB.DB)
		if err != nil {
			return err
		}
		defer conn.Close()
		res, err = f.query(ctx, conn, text, maxRows, maxBytes, args)
		return err
	})
	if err != nil {
		return QueryResult{}, err
	}
	return res, nil
}

// Reading is statements the sidecar wrote, run by ReadOwn on one snapshot of the cache.
type Reading struct {
	ctx context.Context
	f   *file
	tx  *sql.Tx
	// stmts is each statement the reading has run, on tx. Shared by every copy of the
	// value; one goroutine runs the reading.
	stmts map[ownKey]*sql.Stmt
}

// Query is Store.Query inside the reading. A statement run again in the reading, with
// the same row limit, is compiled once.
func (r Reading) Query(text string, maxRows, maxBytes int, args ...any) (QueryResult, error) {
	text, err := trim(text)
	if err != nil {
		return QueryResult{}, err
	}
	key := ownKey{text, maxRows}
	stmt, ok := r.stmts[key]
	if !ok {
		// The file's copy joins the transaction; a statement the file lacks compiles for
		// this reading alone. Either closes with the transaction.
		if kept := r.f.own.get(key); kept != nil {
			stmt = r.tx.StmtContext(r.ctx, kept)
		} else if stmt, err = r.f.prepare(r.ctx, r.tx, text, maxRows); err != nil {
			return QueryResult{}, err
		}
		r.stmts[key] = stmt
	}
	return r.f.run(r.ctx, r.tx, stmt, maxRows, maxBytes, args)
}

// ReadOwn runs read's statements, which the sidecar wrote, inside one read transaction
// on the pool that keeps its connections between readings, so every statement sees the
// cache as the first one did: a reader that joins what several statements answer never
// joins two states of the cache.
func (s *Store) ReadOwn(ctx context.Context, read func(Reading) error) error {
	return s.withFile(ctx, func(ctx context.Context, f *file) error {
		stmts := map[ownKey]*sql.Stmt{}
		err := f.snapshot(ctx, func(tx *sql.Tx) error {
			return read(Reading{ctx, f, tx, stmts})
		})
		// Only once the connection is back in the pool: compiling there takes one, and a
		// reading still holding its own could wait on readings that hold the rest.
		f.keepOwn(ctx, stmts)
		return err
	})
}

// snapshot runs use inside a read transaction on a connection of the own-query pool,
// and lets the connection go before it returns.
func (f *file) snapshot(ctx context.Context, use func(*sql.Tx) error) error {
	conn, err := queryConn(ctx, f.ownQueryDB.DB)
	if err != nil {
		return err
	}
	defer conn.Close()
	tx, err := conn.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil { // coverage:ignore only a cancel landing since the connection was taken fails it
		return err
	}
	// A read-only transaction has nothing to undo; ending it frees the snapshot.
	defer tx.Rollback() //nolint:errcheck
	return use(tx)
}

// keepOwn compiles on the own-query pool each statement of stmts the file does not keep
// yet. Called with no connection of the pool held, since compiling there takes one. A
// statement that fails to compile, as when ctx has ended, compiles again for the next
// reading that runs it.
func (f *file) keepOwn(ctx context.Context, stmts map[ownKey]*sql.Stmt) {
	for key := range stmts {
		if !f.own.wants(key) {
			continue
		}
		stmt, err := f.prepare(ctx, f.ownQueryDB, key.text, key.maxRows)
		if err != nil {
			continue
		}
		f.own.put(key, stmt)
	}
}

// ownKey is one statement a reading runs: its trimmed text, and the row limit written
// into its wrap.
type ownKey struct {
	text    string
	maxRows int
}

// ownStmts is the statements the sidecar's readings run, each compiled once on the
// own-query pool. database/sql compiles a pool statement again on the first connection
// it runs on, and keeps that copy with the connection.
type ownStmts struct {
	mu    sync.Mutex
	stmts map[ownKey]*sql.Stmt
	// max caps the statements kept: ownStmtsMax, or less in a test.
	max int
}

// ownStmtsMax caps a file's own statements. A text can carry a placeholder per value,
// so a reading over a hundred nodes has a text of its own.
const ownStmtsMax = 256

func newOwnStmts() *ownStmts {
	return &ownStmts{stmts: map[ownKey]*sql.Stmt{}, max: ownStmtsMax}
}

// get is key's statement, nil until the file keeps one.
func (o *ownStmts) get(key ownKey) *sql.Stmt {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.stmts[key]
}

// wants says whether key's statement would be kept: the table holds none and has room.
func (o *ownStmts) wants(key ownKey) bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.wantsLocked(key)
}

func (o *ownStmts) wantsLocked(key ownKey) bool {
	_, ok := o.stmts[key]
	return !ok && len(o.stmts) < o.max
}

// put keeps stmt as key's, or closes it: two readings can compile one statement.
func (o *ownStmts) put(key ownKey, stmt *sql.Stmt) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if !o.wantsLocked(key) {
		stmt.Close()
		return
	}
	o.stmts[key] = stmt
}

// close closes the statements. The pool is the file's to close.
func (o *ownStmts) close() error {
	o.mu.Lock()
	defer o.mu.Unlock()
	var errs []error
	for _, stmt := range o.stmts {
		errs = append(errs, stmt.Close())
	}
	return errors.Join(errs...)
}

// withFile runs use on the store's file, under a context the file's close cancels too.
func (s *Store) withFile(ctx context.Context, use func(context.Context, *file) error) error {
	f, done, err := s.use()
	if err != nil {
		return err
	}
	defer done()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	defer context.AfterFunc(f.queryCtx, cancel)()
	err = use(ctx, f)
	if err != nil && f.queryCtx.Err() != nil {
		return fmt.Errorf("cache %d: %w", s.cacheID, ErrClosed)
	}
	return err
}

// query runs one statement on conn, a connection of KubeQuery's pool.
func (f *file) query(ctx context.Context, conn *sql.Conn, text string, maxRows, maxBytes int, args []any) (QueryResult, error) {
	text, err := trim(text)
	if err != nil {
		return QueryResult{}, err
	}
	if f.afterQuery != nil {
		defer f.afterQuery()
	}
	wrapped, err := f.prepare(ctx, conn, text, maxRows)
	if err != nil {
		return QueryResult{}, err
	}
	defer wrapped.Close()
	return f.run(ctx, conn, wrapped, maxRows, maxBytes, args)
}

// querier is a connection or a transaction on one.
type querier interface {
	PrepareContext(ctx context.Context, query string) (*sql.Stmt, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

// trim is text as one statement, with no trailing semicolon, or a *QueryError for more.
func trim(text string) (string, error) {
	text = strings.TrimSpace(text)
	text = strings.TrimSpace(strings.TrimSuffix(text, ";"))
	if strings.Contains(text, ";") {
		return "", &QueryError{Message: onlyOneQuery}
	}
	return text, nil
}

// wrap is text as run: materialized, so all of it runs in the step that takes the first
// row, and cut one row past maxRows. The cap is written in, not bound: SQLite reads a bound
// LIMIT while planning, so it would compile the statement again every time it is bound.
func wrap(text string, maxRows int) string {
	return "WITH q AS MATERIALIZED (SELECT * FROM (" + text + "\n) LIMIT " + strconv.Itoa(maxRows+1) + ") SELECT * FROM q"
}

// prepare compiles text, a trimmed statement, wrapped for maxRows on q.
func (f *file) prepare(ctx context.Context, q querier, text string, maxRows int) (*sql.Stmt, error) {
	if f.onPrepare != nil {
		f.onPrepare()
	}
	// The wrap is prepared first: SQLite applies some pragmas while preparing them, and a
	// statement that prepares wrapped is a query and holds none.
	wrapped, err := q.PrepareContext(ctx, wrap(text, maxRows))
	if err != nil {
		return nil, prepareError(err)
	}
	// Alone, so a trailing unclosed /* that hid the wrap's tail is refused.
	alone, err := q.PrepareContext(ctx, text)
	if err != nil {
		wrapped.Close()
		return nil, prepareError(err)
	}
	alone.Close()
	return wrapped, nil
}

// run runs wrapped, a statement prepare compiled, on q, with args bound to its
// placeholders, and answers at most maxRows rows and maxBytes of text and blob cells.
func (f *file) run(ctx context.Context, q querier, wrapped *sql.Stmt, maxRows, maxBytes int, args []any) (QueryResult, error) {
	// SQLite clears a pending interrupt when a statement starts on a connection running
	// none, so a deadline landing between modernc's check of ctx and its first step would
	// be lost and the statement would run whole. One left mid-step keeps it pending.
	hold, err := q.QueryContext(context.Background(), `SELECT 1 UNION ALL SELECT 2`)
	if err != nil {
		return QueryResult{}, err
	}
	defer hold.Close()
	if !hold.Next() { // coverage:ignore a constant SELECT under context.Background always yields its first row
		return QueryResult{}, hold.Err()
	}

	if f.onQuery != nil {
		f.onQuery()
	}
	rows, err := wrapped.QueryContext(ctx, args...)
	if err != nil {
		return QueryResult{}, statementError(err)
	}
	defer rows.Close()
	var res QueryResult
	if res.Columns, err = rows.Columns(); err != nil { // coverage:ignore Columns fails only on closed rows, and only a cancel landing since the query closes them
		return QueryResult{}, err
	}
	size := 0
	for rows.Next() {
		row := make([]any, len(res.Columns))
		ptrs := make([]any, len(row))
		for i := range row {
			ptrs[i] = &row[i]
		}
		if err := rows.Scan(ptrs...); err != nil { // coverage:ignore Scan into *any takes every SQLite value; only a cancel landing between Next and Scan fails it
			return QueryResult{}, err
		}
		size += cellBytes(row)
		if len(res.Rows) == maxRows || size > maxBytes {
			res.More = true
			break
		}
		res.Rows = append(res.Rows, row)
	}
	if err := rows.Err(); err != nil { // coverage:ignore the wrap runs the whole statement in the first step, which QueryContext takes, and modernc interrupts no later one
		return QueryResult{}, statementError(err)
	}
	return res, nil
}

// queryMaxLength bounds any value a statement builds.
const queryMaxLength = 16 << 20

// queryConn takes a connection of pool with its limits set: no attached database, since
// ATTACH and VACUUM INTO both open another file, and no value past queryMaxLength.
func queryConn(ctx context.Context, pool *sql.DB) (*sql.Conn, error) {
	conn, err := pool.Conn(ctx)
	if err != nil {
		return nil, err
	}
	for id, v := range map[int]int{
		sqlitelib.SQLITE_LIMIT_ATTACHED: 0,
		sqlitelib.SQLITE_LIMIT_LENGTH:   queryMaxLength,
	} {
		if _, err := sqlite.Limit(conn, id, v); err != nil { // coverage:ignore Limit fails only for a connection of another driver
			conn.Close()
			return nil, err
		}
	}
	return conn, nil
}

// cellBytes is what a row's text and blob cells hold.
func cellBytes(row []any) int {
	n := 0
	for _, v := range row {
		switch v := v.(type) {
		case string:
			n += len(v)
		case []byte:
			n += len(v)
		}
	}
	return n
}

// prepareError is statementError, with what runs added to a syntax error: SQLite's own
// message names the token it stopped at, which is all it can say about a statement that
// is not a query.
func prepareError(err error) error {
	err = statementError(err)
	var qe *QueryError
	if errors.As(err, &qe) && (strings.Contains(qe.Message, "syntax error") ||
		strings.Contains(qe.Message, "incomplete input")) {
		qe.Message += " " + onlyOneQuery
	}
	return err
}

// statementError is a *QueryError for SQLite's refusal, and err itself for anything else.
func statementError(err error) error {
	if se, ok := errors.AsType[*sqlite.Error](err); ok {
		return &QueryError{Message: se.Error()}
	}
	return err
}

// onlyOneQuery is what a statement that is not one query hears.
const onlyOneQuery = "Only one query runs: SELECT, WITH or VALUES, not ending inside a /* comment."
