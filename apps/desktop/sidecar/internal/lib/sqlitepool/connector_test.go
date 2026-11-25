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
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"modernc.org/sqlite"
	sqlitelib "modernc.org/sqlite/lib"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/testutil"
)

// The connector opens through its driver, and refuses a context already done.
func TestTheConnectorIsItsDrivers(t *testing.T) {
	d := &sqlite.Driver{}
	c := newConnector(d, "file:"+filepath.Join(t.TempDir(), "test.db"))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := c.Connect(ctx)

	require.ErrorIs(t, err, context.Canceled)
	require.Same(t, d, c.Driver())
	require.NoError(t, c.Close(), "nothing was opened")
}

// An open that fails leaves nothing counted, so a close with no budget does not wait
// on it.
func TestAFailedOpenIsNotCounted(t *testing.T) {
	db := newPool(newConnector(&sqlite.Driver{}, "file:"+filepath.Join(t.TempDir(), "missing.db")+"?mode=ro"))
	db.c.budget = 0

	_, err := db.c.Connect(context.Background())

	require.Error(t, err)
	require.NoError(t, db.Close())
}

// A connection in use when the pool closes, as one a cancelled transaction's rollback
// still holds on database/sql's goroutine, has closed by the time Close returns (#648).
func TestCloseWaitsForAConnectionInUse(t *testing.T) {
	db := newPool(newConnector(&sqlite.Driver{}, "file:"+filepath.Join(t.TempDir(), "test.db")))
	conn, err := db.Conn(context.Background())
	require.NoError(t, err)

	done := make(chan error, 1)
	go func() { done <- db.Close() }()
	// Once the connector is marked closed, the wait has the connection counted and can
	// only end when it is let go: so the wait is proven, not timed.
	waitClosing(t, db.c)
	select {
	case err := <-done:
		t.Fatalf("close returned with a connection in use: %v", err)
	default:
	}
	require.NoError(t, conn.Close())

	require.NoError(t, testutil.Recv(t, done, "the close"))
}

// A close under a context that ends gives up on the connection then, not after the
// budget, and says why.
func TestCloseContextGivesUpWhenTheContextEnds(t *testing.T) {
	p := newPool(newConnector(&sqlite.Driver{}, "file:"+filepath.Join(t.TempDir(), "test.db")))
	conn, err := p.Conn(context.Background())
	require.NoError(t, err)
	t.Cleanup(func() { conn.Close() })
	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() { done <- p.CloseContext(ctx) }()
	waitClosing(t, p.c)
	cancel()

	err = testutil.Recv(t, done, "the close")
	require.ErrorIs(t, err, context.Canceled)
	require.ErrorIs(t, err, ErrStillOpen)
}

// A file's pools closed one after another under one context share its deadline: the
// second pool's close ends with the same context, however long the first waited.
func TestPoolsClosedUnderOneContextShareItsEnd(t *testing.T) {
	path := "file:" + filepath.Join(t.TempDir(), "test.db")
	a, b := newPool(newConnector(&sqlite.Driver{}, path)), newPool(newConnector(&sqlite.Driver{}, path))
	ca, err := a.Conn(context.Background())
	require.NoError(t, err)
	t.Cleanup(func() { ca.Close() })
	cb, err := b.Conn(context.Background())
	require.NoError(t, err)
	t.Cleanup(func() { cb.Close() })
	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() { done <- errors.Join(a.CloseContext(ctx), b.CloseContext(ctx)) }()
	waitClosing(t, a.c)
	cancel()

	err = testutil.Recv(t, done, "the closes")
	require.Len(t, err.(interface{ Unwrap() []error }).Unwrap(), 2, "each pool reported")
	require.ErrorIs(t, err, context.Canceled)
}

// CloseAll closes the pools under one deadline and reports every pool's error.
func TestCloseAllReportsEveryPool(t *testing.T) {
	path := "file:" + filepath.Join(t.TempDir(), "test.db")
	a, b := newPool(newConnector(&sqlite.Driver{}, path)), newPool(newConnector(&sqlite.Driver{}, path))
	ca, err := a.Conn(context.Background())
	require.NoError(t, err)
	t.Cleanup(func() { ca.Close() })
	cb, err := b.Conn(context.Background())
	require.NoError(t, err)
	t.Cleanup(func() { cb.Close() })

	err = CloseAll(0, a, b)

	require.ErrorIs(t, err, ErrStillOpen)
	require.Len(t, err.(interface{ Unwrap() []error }).Unwrap(), 2)
}

// waitClosing returns once c's Close has marked it closed.
func waitClosing(t *testing.T, c *connector) {
	t.Helper()
	require.Eventually(t, func() bool {
		c.mu.Lock()
		defer c.mu.Unlock()
		return c.closed
	}, testutil.Timeout, time.Millisecond, "the connector to be marked closed")
}

// database/sql's opener checks the pool's closed flag before it calls Connect, so an
// opener paused between the two would otherwise open the file after Close returned,
// and the store delete it from under the connection.
func TestConnectIsRefusedOnceClosed(t *testing.T) {
	c := newConnector(&sqlite.Driver{}, "file:"+filepath.Join(t.TempDir(), "test.db"))
	require.NoError(t, c.Close())

	_, err := c.Connect(context.Background())

	require.ErrorIs(t, err, errClosed)
}

func TestCloseGivesUpOnAConnectionHeldPastTheBudget(t *testing.T) {
	db := newPool(newConnector(&sqlite.Driver{}, "file:"+filepath.Join(t.TempDir(), "test.db")))
	db.c.budget = 0
	conn, err := db.Conn(context.Background())
	require.NoError(t, err)
	t.Cleanup(func() { conn.Close() })

	require.ErrorIs(t, db.Close(), ErrStillOpen)
}

// sqlite.Limit reaches the driver's connection through the wrapper's Unwrap.
func TestLimitSeesThroughTheWrapper(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")
	w, err := OpenWriter(path)
	require.NoError(t, err)
	t.Cleanup(func() { w.Close() })
	q := OpenQuery(&sqlite.Driver{}, path, 1)
	t.Cleanup(func() { q.Close() })
	conn, err := q.Conn(context.Background())
	require.NoError(t, err)
	t.Cleanup(func() { conn.Close() })

	_, err = sqlite.Limit(conn, sqlitelib.SQLITE_LIMIT_ATTACHED, 0)
	require.NoError(t, err)
	prev, err := sqlite.Limit(conn, sqlitelib.SQLITE_LIMIT_ATTACHED, 0)
	require.NoError(t, err)
	require.Zero(t, prev)
}

// database/sql refuses a read-only transaction on a connection without ConnBeginTx, so
// this goes red the moment the wrapper hides it.
func TestAReadOnlyTransactionReachesTheDriver(t *testing.T) {
	db, err := OpenWriter(filepath.Join(t.TempDir(), "test.db"))
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })

	tx, err := db.BeginTx(context.Background(), &sql.TxOptions{ReadOnly: true})
	require.NoError(t, err)
	require.NoError(t, tx.Rollback())
}
