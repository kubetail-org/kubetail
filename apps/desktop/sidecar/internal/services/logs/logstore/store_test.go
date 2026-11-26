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

package logstore

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/sqlitepool"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/testutil"
)

// openTestFile opens a fresh file under a temp directory, closed when the test ends.
func openTestFile(t *testing.T) *file {
	t.Helper()
	f, err := openFile(filepath.Join(t.TempDir(), "1.db"), sqlitepool.CloseBudget)
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, f.close()) })
	return f
}

// newTestStore is a claim on a fresh file, under a manager closed when the test ends.
func newTestStore(t *testing.T) *Store {
	t.Helper()
	m := NewManager(t.TempDir(), testRetention)
	t.Cleanup(func() { assert.NoError(t, m.Close()) })
	s, err := m.OpenOrCreate(1)
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, s.Close()) })
	return s
}

// clock is a time a test moves by hand, in unix milliseconds, safe to read from the
// writer's goroutine.
type clock struct{ ms atomic.Int64 }

func newClock(ms int64) *clock {
	c := &clock{}
	c.ms.Store(ms)
	return c
}

func (c *clock) now() time.Time { return time.UnixMilli(c.ms.Load()) }
func (c *clock) set(ms int64)   { c.ms.Store(ms) }

// newClockedStore is a claim on a fresh file under a manager kept to ret on c.
func newClockedStore(t *testing.T, ret Retention, c *clock) *Store {
	t.Helper()
	m := NewManager(t.TempDir(), ret)
	m.now = c.now
	t.Cleanup(func() { assert.NoError(t, m.Close()) })
	s, err := m.OpenOrCreate(1)
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, s.Close()) })
	return s
}

// stamps is each chunk of the stream id's read_at_ms, oldest chunk first, nil for one
// no page has read.
func stamps(t *testing.T, s *Store, id int64) []*int64 {
	t.Helper()
	var out []*int64
	using(t, s, func(f *file) {
		rows, err := f.readDB.Query(`SELECT read_at_ms FROM chunks WHERE stream_id = ? ORDER BY from_ns, from_ord`, id)
		require.NoError(t, err)
		defer rows.Close()
		for rows.Next() {
			var ms sql.NullInt64
			require.NoError(t, rows.Scan(&ms))
			if ms.Valid {
				out = append(out, &ms.Int64)
			} else {
				out = append(out, nil)
			}
		}
		require.NoError(t, rows.Err())
	})
	return out
}

// insertStream adds one stream and answers its id.
func insertStream(t *testing.T, f *file) int64 {
	t.Helper()
	res, err := f.db.Exec(`INSERT INTO streams (namespace, pod, pod_uid, container, is_default, container_id, node, region, zone, os, arch, labels)
		VALUES ('web', 'api-7f9c', 'uid-api', 'app', 1, 'containerd://1', 'node-a', '', '', 'linux', 'amd64', '{}')`)
	require.NoError(t, err)
	id, err := res.LastInsertId()
	require.NoError(t, err)
	return id
}

// insertLine adds one line at a position and answers its id.
func insertLine(t *testing.T, f *file, stream, tsNS, ord int64) int64 {
	t.Helper()
	res, err := f.db.Exec(`INSERT INTO lines (stream_id, ts_ns, ord, message) VALUES (?, ?, ?, 'hello')`, stream, tsNS, ord)
	require.NoError(t, err)
	id, err := res.LastInsertId()
	require.NoError(t, err)
	return id
}

// refuse makes the file behind s refuse every op, INSERT, UPDATE or DELETE, of a row
// of table, so a test can fail a statement inside a write.
func refuse(t *testing.T, s *Store, op, table string) {
	t.Helper()
	using(t, s, func(f *file) {
		_, err := f.db.Exec(fmt.Sprintf(`CREATE TRIGGER refuse_%[1]s_%[2]s BEFORE %[1]s ON %[2]s BEGIN SELECT RAISE(ABORT, 'refused'); END`, op, table))
		require.NoError(t, err)
	})
}

// alter runs ddl on the file behind s: a dropped table fails every statement that
// reads it, and a trigger that raises the writes it names.
func alter(t *testing.T, s *Store, ddl string) {
	t.Helper()
	using(t, s, func(f *file) {
		_, err := f.db.Exec(ddl)
		require.NoError(t, err)
	})
}

// refuseClose makes the file behind s refuse every close of a chunk, and nothing
// else a write does to chunks.
func refuseClose(t *testing.T, s *Store) {
	t.Helper()
	alter(t, s, `CREATE TRIGGER refuse_close BEFORE UPDATE OF open ON chunks BEGIN SELECT RAISE(ABORT, 'refused'); END`)
}

// countStreams reads how many streams the file holds, through the reader pool.
func countStreams(t *testing.T, f *file) int {
	t.Helper()
	var n int
	require.NoError(t, f.readDB.QueryRow(`SELECT count(*) FROM streams`).Scan(&n))
	return n
}

// The directory need not exist, and what a file held is there when it is opened again.
func TestAFileReopensWithWhatItHeld(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "logs", "1.db")
	f, err := openFile(path, sqlitepool.CloseBudget)
	require.NoError(t, err)
	insertStream(t, f)
	require.NoError(t, f.close())

	f, err = openFile(path, sqlitepool.CloseBudget)
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, f.close()) })

	assert.Equal(t, 1, countStreams(t, f))
}

// A file that cannot be opened is refused at the step that failed.
func TestAFileThatCannotOpenIsRefused(t *testing.T) {
	t.Parallel()
	// prepared opens a file and runs stmt on it, leaving it closed for openFile to try.
	prepared := func(stmt string) func(t *testing.T, path string) {
		return func(t *testing.T, path string) {
			f, err := openFile(path, sqlitepool.CloseBudget)
			require.NoError(t, err)
			id := insertStream(t, f)
			_, err = f.db.Exec(`INSERT INTO chunks (stream_id, pattern, from_ns, from_ord, to_ns, to_ord, backend, at_beginning, at_end, open, bytes, read_at_ms)
				VALUES (?, '', 1, 0, 1, 0, 'kubelet', 0, 0, 1, 0, 0)`, id)
			require.NoError(t, err)
			_, err = f.db.Exec(stmt)
			require.NoError(t, err)
			require.NoError(t, f.close())
		}
	}
	for _, tc := range []struct {
		name  string
		setup func(t *testing.T, path string)
		want  string
	}{
		{"its directory is a file", func(t *testing.T, path string) {
			require.NoError(t, os.WriteFile(filepath.Dir(path), nil, 0o600))
		}, "mkdir"},
		{"it is not a database", func(t *testing.T, path string) {
			require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
			require.NoError(t, os.WriteFile(path, []byte("not a database, not a database, not a database"), 0o600))
		}, "open writer"},
		{"its schema is another's", func(t *testing.T, path string) {
			require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
			db, err := sql.Open("sqlite", path)
			require.NoError(t, err)
			_, err = db.Exec(`CREATE TABLE streams (x)`)
			require.NoError(t, err)
			require.NoError(t, db.Close())
		}, "migrate"},
		{"a table it reads is gone", prepared(`DROP TABLE chunks`), "prepare"},
		{"its open chunks cannot close", prepared(`CREATE TRIGGER refuse BEFORE UPDATE ON chunks BEGIN SELECT RAISE(ABORT, 'refused'); END`), "close open chunks"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			path := filepath.Join(t.TempDir(), "logs", "1.db")
			tc.setup(t, path)

			_, err := openFile(path, sqlitepool.CloseBudget)
			assert.ErrorContains(t, err, tc.want)
		})
	}
}

// STRICT is what keeps a moment a number: a loose column would store the text and sort it
// after every integer, putting the line out of position.
func TestAMomentMustBeAnInteger(t *testing.T) {
	t.Parallel()
	f := openTestFile(t)
	stream := insertStream(t, f)

	_, err := f.db.Exec(`INSERT INTO chunks (stream_id, pattern, from_ns, from_ord, to_ns, to_ord, backend, at_beginning, at_end, open, bytes, read_at_ms)
		VALUES (?, '', 'yesterday', 0, 2, 0, 'kubelet', 0, 0, 0, 0, 0)`, stream)

	assert.ErrorContains(t, err, "cannot store TEXT value in INTEGER column")
}

// A line's key is its position, so two fills that found the same line store it once: the
// second insert is refused, and the fill's OR IGNORE skips it.
func TestAPositionHoldsOneLine(t *testing.T) {
	t.Parallel()
	f := openTestFile(t)
	stream := insertStream(t, f)
	insertLine(t, f, stream, 100, 0)

	_, err := f.db.Exec(`INSERT INTO lines (stream_id, ts_ns, ord, message) VALUES (?, 100, 0, 'again')`, stream)
	assert.ErrorContains(t, err, "UNIQUE constraint failed")

	res, err := f.db.Exec(`INSERT OR IGNORE INTO lines (stream_id, ts_ns, ord, message) VALUES (?, 100, 0, 'again')`, stream)
	require.NoError(t, err)
	n, err := res.RowsAffected()
	require.NoError(t, err)
	assert.Zero(t, n)
}

// A flag column takes 0 or 1 and nothing else.
func TestAFlagIsZeroOrOne(t *testing.T) {
	t.Parallel()
	f := openTestFile(t)
	_, err := f.db.Exec(`INSERT INTO streams (namespace, pod, pod_uid, container, is_default, container_id, node, region, zone, os, arch, labels)
		VALUES ('web', 'api-7f9c', 'uid-api', 'app', 2, 'containerd://1', 'node-a', '', '', 'linux', 'amd64', '{}')`)
	assert.ErrorContains(t, err, "CHECK constraint failed")

	id := insertStream(t, f)
	for _, flag := range []string{"at_beginning", "at_end", "open"} {
		values := map[string]int{"at_beginning": 0, "at_end": 0, "open": 0}
		values[flag] = 2
		_, err := f.db.Exec(`INSERT INTO chunks (stream_id, pattern, from_ns, from_ord, to_ns, to_ord, backend, at_beginning, at_end, open, bytes, read_at_ms)
			VALUES (?, '', 1, 0, 1, 0, 'kubelet', ?, ?, ?, 0, 0)`, id, values["at_beginning"], values["at_end"], values["open"])
		assert.ErrorContains(t, err, "CHECK constraint failed", flag)
	}
}

// A deleted row's id is never handed out again: a tailer, a watermark or a sweep that
// names a row by id across transactions must never find another stream's in its place.
func TestADeletedIDIsNeverReused(t *testing.T) {
	t.Parallel()
	f := openTestFile(t)
	stream := insertStream(t, f)
	_, err := f.db.Exec(`DELETE FROM streams WHERE id = ?`, stream)
	require.NoError(t, err)
	again := insertStream(t, f)
	assert.NotEqual(t, stream, again, "a stream's")

	insertChunkRow := func() int64 {
		res, err := f.db.Exec(`INSERT INTO chunks (stream_id, pattern, from_ns, from_ord, to_ns, to_ord, backend, at_beginning, at_end, open, bytes, read_at_ms)
			VALUES (?, '', 1, 0, 1, 0, 'kubelet', 0, 0, 0, 0, 0)`, again)
		require.NoError(t, err)
		id, err := res.LastInsertId()
		require.NoError(t, err)
		return id
	}
	chunk := insertChunkRow()
	_, err = f.db.Exec(`DELETE FROM chunks WHERE id = ?`, chunk)
	require.NoError(t, err)
	assert.NotEqual(t, chunk, insertChunkRow(), "a chunk's")
}

// Every operation on a closed claim answers ErrClosed, under what it was doing.
func TestEveryOperationOnAClosedClaimIsRefused(t *testing.T) {
	t.Parallel()
	m := NewManager(t.TempDir(), testRetention)
	t.Cleanup(func() { assert.NoError(t, m.Close()) })
	s, err := m.OpenOrCreate(1)
	require.NoError(t, err)
	require.NoError(t, s.Close())
	ctx := t.Context()

	for name, op := range map[string]func() error{
		"put streams": func() error {
			_, err := s.PutStreams(ctx, []Stream{app()})
			return err
		},
		"finish streams": func() error { return s.FinishStreams(ctx, []Stream{app()}) },
		"pod streams": func() error {
			_, err := s.PodStreams(ctx, "web", "api-7f9c", "")
			return err
		},
		"chunks": func() error {
			_, err := s.Chunks(ctx, 1, "")
			return err
		},
		"write": func() error {
			_, err := s.Write(ctx, nil, true)
			return err
		},
		"read":  func() error { return s.Read(ctx, func(Reader) error { return nil }) },
		"evict": func() error { return s.Evict(ctx, 1, anyStamp) },
		"stamp": func() error { return s.Stamp(ctx, []Chunk{{ID: 1}}) },
		"subscribe": func() error {
			_, err := s.ListenForStreamChanges(1)
			return err
		},
		"context": func() error {
			_, err := s.Context()
			return err
		},
	} {
		err := op()
		assert.ErrorIs(t, err, ErrClosed, name)
		assert.ErrorContains(t, err, name+": ", name)
	}
}

// An operation the file's close interrupts answers ErrClosed, not the cancel it met.
func TestAnOperationTheCloseInterruptsIsClosed(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)

	err := s.operate(t.Context(), func(ctx context.Context, f *file) error {
		f.cancel() // as the file's close does, while the operation runs
		<-ctx.Done()
		return ctx.Err()
	})

	assert.ErrorIs(t, err, ErrClosed)
}

// twoStreams files app and a second container of its pod in s and answers their ids.
func twoStreams(t *testing.T, s *Store) (int64, int64) {
	t.Helper()
	sidecar := app()
	sidecar.Container, sidecar.ContainerID, sidecar.Default = "proxy", "containerd://2", false
	ids, err := s.PutStreams(t.Context(), []Stream{app(), sidecar})
	require.NoError(t, err)
	return ids[0], ids[1]
}

// A commit of lines pings each stream it wrote, after the commit, and none it wrote
// no line to; the file closing ends the receiver.
func TestAWriteOfLinesPingsItsStreams(t *testing.T) {
	t.Parallel()
	m, _ := newTestManager(t)
	s := open(t, m, 1)
	t.Cleanup(func() { assert.NoError(t, s.Close()) })
	a, b := twoStreams(t, s)
	pings := subscribe(t, s, a, b)

	write(t, s, Fill{Stream: b, Backend: "kubelet", Next: new(at(1, 0))})
	write(t, s, Fill{Stream: a, Backend: "kubelet", Lines: lines(at(1, 0))})

	// First touch orders the pings, so b's would come first.
	assert.Equal(t, a, testutil.Recv(t, pings, "a's ping").Key)
	assert.Len(t, positions(t, s, a), 1, "the ping follows the commit")

	require.NoError(t, m.Clear(1))
	testutil.RecvClosed(t, pings, "the receiver to end")
}
