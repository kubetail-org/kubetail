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

package sqlitepool

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"sync"
	"time"

	"modernc.org/sqlite"
)

// CloseBudget is how long a pool's close waits for a connection still in use when
// its context has no deadline. A file's close puts one over all of its pools.
const CloseBudget = 5 * time.Second

var errClosed = errors.New("sqlitepool: pool closed")

// ErrStillOpen is what a pool's close wraps when a connection was still open as its
// wait ended. The file is then still held: nothing may delete or recreate it.
var ErrStillOpen = errors.New("sqlitepool: connections still open")

// connector opens a pool's connections through its own driver (modernc's NewConnector
// always uses the one it registers as "sqlite") and counts them, so a Pool's close can
// wait for every one to close.
type connector struct {
	d      *sqlite.Driver
	dsn    string
	budget time.Duration

	mu     sync.Mutex
	closed bool
	open   int
	idle   chan struct{} // closed when open falls to zero, made anew when it leaves zero
}

func newConnector(d *sqlite.Driver, dsn string) *connector {
	return &connector{d: d, dsn: dsn, budget: CloseBudget}
}

// Pool is one of a store's pools: database/sql's, over the connector that counts its
// connections. Pass DB to what takes a *sql.DB, and never close DB itself: that skips
// the wait.
type Pool struct {
	*sql.DB
	c *connector
}

func newPool(c *connector) *Pool {
	return &Pool{DB: sql.OpenDB(c), c: c}
}

// Close is CloseContext with no deadline: the wait is the default budget.
func (p *Pool) Close() error {
	return p.CloseContext(context.Background())
}

// CloseAll closes pools one after another under one deadline of budget, so a file's
// pools wait one budget between them, not one each. The order is the caller's: SQLite
// checkpoints and deletes the WAL only when the last connection to close can write the
// file, so a mode=ro pool goes before a read-write one.
func CloseAll(budget time.Duration, pools ...*Pool) error {
	ctx, cancel := context.WithTimeout(context.Background(), budget)
	defer cancel()
	var errs []error
	for _, p := range pools {
		errs = append(errs, p.CloseContext(ctx))
	}
	return errors.Join(errs...)
}

// CloseContext closes the pool and returns once every connection it opened has closed,
// or errors when ctx ends first; ctx with no deadline waits CloseBudget. DB.Close closes
// only idle connections; one in use, as a cancelled transaction's rollback on
// database/sql's own goroutine, closes when it is let go, and Windows refuses to delete
// a file a connection still holds.
func (p *Pool) CloseContext(ctx context.Context) error {
	return errors.Join(p.DB.Close(), p.c.wait(ctx))
}

func (c *connector) Connect(ctx context.Context) (driver.Conn, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	// Counted before the open, so a Close racing an open in flight waits for it too.
	if err := c.acquire(); err != nil {
		return nil, err
	}
	dc, err := c.d.Open(c.dsn)
	if err != nil {
		c.release()
		return nil, err
	}
	sc, ok := dc.(sqliteConn)
	if !ok { // coverage:ignore every connection modernc opens implements sqliteConn
		dc.Close()
		c.release()
		return nil, fmt.Errorf("sqlitepool: %T lacks an interface database/sql looks for", dc)
	}
	return &countedConn{sqliteConn: sc, c: c}, nil
}

func (c *connector) Driver() driver.Driver { return c.d }

// Close marks the connector closed, so an open is refused from here on: database/sql's
// opener may be between its own closed check and Connect, and a file it opened after
// DB.Close returned would be deleted from under it. Pool.CloseContext does the wait.
func (c *connector) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closed = true
	return nil
}

// wait returns once every connection the connector opened has closed, or errors when
// ctx ends first.
func (c *connector) wait(ctx context.Context) error {
	c.mu.Lock()
	open, idle := c.open, c.idle
	c.mu.Unlock()
	if open == 0 {
		return nil
	}
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.budget)
		defer cancel()
	}

	select {
	case <-idle:
		return nil
	case <-ctx.Done():
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.open == 0 { // coverage:ignore both arms ready at once, and select picks one at random
		// The last connection closed as ctx ended, and select picked ctx.
		return nil
	}
	return fmt.Errorf("%w: %d after %w", ErrStillOpen, c.open, ctx.Err())
}

func (c *connector) acquire() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return errClosed
	}
	if c.open == 0 {
		c.idle = make(chan struct{})
	}
	c.open++
	return nil
}

func (c *connector) release() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.open--
	if c.open == 0 {
		close(c.idle)
	}
}

// sqliteConn is every interface database/sql looks for that a modernc connection
// implements. database/sql finds them by type assertion, so one the wrapper hid would
// be lost without an error: BeginTx with options refused, a lock wait ignoring its
// context, a cancelled transaction discarding its connection, validation skipped.
type sqliteConn interface {
	driver.Conn
	driver.ConnBeginTx
	driver.ConnPrepareContext
	driver.ExecerContext
	driver.QueryerContext
	driver.Pinger
	driver.SessionResetter
	driver.Validator
}

// countedConn is one connection of a connector's pool: the driver's own, with a Close
// that releases its count. Only sqliteConn's methods show through it; the driver's
// others (Serialize, NewBackup, the hooks) sit behind Unwrap, which is how
// sqlite.Limit reaches them, and code reaching one through Conn.Raw asserts it on
// Unwrap's result, never on the value Raw hands it.
type countedConn struct {
	sqliteConn
	c *connector
	// released guards the count: database/sql closes a connection once, and a second
	// close, as of the driver's connection inside a Conn.Raw callback, must not count
	// it down again.
	released sync.Once
}

func (cc *countedConn) Unwrap() driver.Conn { return cc.sqliteConn }

func (cc *countedConn) Close() error {
	err := cc.sqliteConn.Close()
	cc.released.Do(cc.c.release)
	return err
}
