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
	"cmp"
	"context"
	"testing"
	"time"

	"github.com/amorey/gobus"
	"github.com/amorey/gochan/mpsc"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/testutil"
)

// pingEvent is one ping off the file's bus.
type pingEvent = gobus.Event[int64, struct{}]

// newWriterStore is a claim on a fresh file kept to ret, whose writer's tick is the
// channel answered: the writer writes what it holds once the test sends on it.
func newWriterStore(t *testing.T, ret Retention) (*Store, chan time.Time) {
	t.Helper()
	m := NewManager(t.TempDir(), ret)
	tick := make(chan time.Time)
	m.after = func(time.Duration) <-chan time.Time { return tick }
	t.Cleanup(func() { assert.NoError(t, m.Close()) })
	s := open(t, m, 1)
	t.Cleanup(func() { assert.NoError(t, s.Close()) })
	return s, tick
}

// fire sends the writer's tick, failing the test once the writer has waited past the
// failsafe without taking it.
func fire(t *testing.T, tick chan<- time.Time) {
	t.Helper()
	select {
	case tick <- time.Now():
	case <-time.After(testutil.Timeout):
		t.Fatal("the writer never took its tick")
	}
}

// subscribe is a receiver of the pings for ids, closed when the test ends.
func subscribe(t *testing.T, s *Store, ids ...int64) <-chan pingEvent {
	t.Helper()
	rx, err := s.ListenForStreamChanges(ids...)
	require.NoError(t, err)
	t.Cleanup(rx.Close)
	return rx.Chan()
}

// openTail opens a tail of the stream id continuing prev, failing the test on an error.
func openTail(t *testing.T, s *Store, id int64, prev *Pos, atBeginning bool) *Tail {
	t.Helper()
	tail, err := s.OpenTail(id, "kubelet", prev, atBeginning)
	require.NoError(t, err)
	return tail
}

// opened is c as a tailer holds it open.
func opened(c Chunk) Chunk {
	c.Open = true
	return c
}

// A tail's write protects nothing: the chunks it opens carry no stamp, and a chunk it
// extends keeps the stamp a page or a fill gave it, so a tailed stream nobody pages is
// evictable from the moment it is written.
func TestATailStampsNothing(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)
	id := streamOf(t, s)
	write(t, s, Fill{Stream: id, Backend: "kubelet", Lines: lines(at(1, 0))})
	before := stamps(t, s, id)[0]
	require.NotNil(t, before)

	tail := openTail(t, s, id, new(at(1, 0)), false)
	require.NoError(t, tail.Write(t.Context(), lines(at(1, 1))))
	big := Line{Pos: at(9, 0), Message: string(make([]byte, testRetention.ChunkBytes))}
	require.NoError(t, tail.Close(t.Context(), []Line{big, {Pos: at(9, 1), Message: "m"}}))

	assert.Equal(t, []*int64{before, nil}, stamps(t, s, id), "the chunk it extended keeps its stamp; the one it opened has none")
}

// A tail's batch waits for the writer's tick, and lands then in one open chunk, with
// a ping after the commit.
func TestABatchCommitsOnTheTick(t *testing.T) {
	t.Parallel()
	s, tick := newWriterStore(t, testRetention)
	id := streamOf(t, s)
	pings := subscribe(t, s, id)
	tail := openTail(t, s, id, nil, false)

	require.NoError(t, tail.Write(t.Context(), lines(at(1, 0), at(2, 0))))
	assert.Empty(t, positions(t, s, id), "before the tick")

	fire(t, tick)
	testutil.Recv(t, pings, "the batch's ping")
	assert.Equal(t, []Pos{at(1, 0), at(2, 0)}, positions(t, s, id))
	assert.Equal(t, []Chunk{opened(chunk(at(1, 0), at(2, 0)))}, chunksOfStream(t, s, id))
}

// Every batch that has arrived by the tick lands in one transaction, and the commit
// pings each stream it wrote lines to and none it did not.
func TestTwoTailsLandInOneWrite(t *testing.T) {
	t.Parallel()
	s, tick := newWriterStore(t, testRetention)
	a, b := twoStreams(t, s)
	quiet := app()
	quiet.Container, quiet.ContainerID, quiet.Default = "quiet", "containerd://3", false
	ids, err := s.PutStreams(t.Context(), []Stream{quiet})
	require.NoError(t, err)
	pings := subscribe(t, s, a, b, ids[0])

	require.NoError(t, openTail(t, s, ids[0], nil, false).Write(t.Context(), nil))
	require.NoError(t, openTail(t, s, a, nil, false).Write(t.Context(), lines(at(1, 0))))
	require.NoError(t, openTail(t, s, b, nil, false).Write(t.Context(), lines(at(2, 0))))
	fire(t, tick)

	// First touch orders the pings, so the quiet stream's would come first.
	assert.Equal(t, a, testutil.Recv(t, pings, "a's ping").Key)
	assert.Equal(t, b, testutil.Recv(t, pings, "b's ping").Key)
	assert.Len(t, positions(t, s, a), 1)
	assert.Len(t, positions(t, s, b), 1)
}

// A batch that closed full lands without the tick. A tail's chunk closes at
// ChunkBytes or ChunkSpan and the next opens at the line after, linked to it.
func TestATailsChunkClosesAtChunkBytesOrChunkSpan(t *testing.T) {
	t.Parallel()
	ret := testRetention
	ret.WriteBatch, ret.ChunkBytes, ret.ChunkSpan = 2, 3, 10*time.Second
	s, _ := newWriterStore(t, ret)
	id := streamOf(t, s)
	pings := subscribe(t, s, id)
	tail := openTail(t, s, id, nil, false)
	write := func(at ...Pos) {
		require.NoError(t, tail.Write(t.Context(), lines(at...)))
		testutil.Recv(t, pings, "a full batch's ping")
	}

	write(at(1, 0), at(2, 0))
	write(at(3, 0), at(20, 0))
	write(at(21, 0), at(31, 0))

	assert.Equal(t, []Chunk{
		chunk(at(1, 0), at(3, 0)),
		linked(chunk(at(20, 0), at(21, 0)), at(3, 0)),
		opened(linked(chunk(at(31, 0), at(31, 0)), at(21, 0))),
	}, chunksOfStream(t, s, id))
}

// Closing a tail lands its last lines and closes its chunk in one write, and returns
// once that write has landed. The tail's sender closing leaves the writer to the
// others.
func TestClosingATailClosesItsChunk(t *testing.T) {
	t.Parallel()
	s, tick := newWriterStore(t, testRetention)
	a, b := twoStreams(t, s)
	pings := subscribe(t, s, b)
	first := openTail(t, s, a, nil, false)
	require.NoError(t, first.Write(t.Context(), lines(at(1, 0))))

	require.NoError(t, first.Close(t.Context(), lines(at(2, 0))))
	assert.Equal(t, []Chunk{chunk(at(1, 0), at(2, 0))}, chunksOfStream(t, s, a))

	require.NoError(t, openTail(t, s, b, nil, false).Write(t.Context(), lines(at(3, 0))))
	fire(t, tick)
	testutil.Recv(t, pings, "the other tail's ping")
	assert.Equal(t, []Chunk{opened(chunk(at(3, 0), at(3, 0)))}, chunksOfStream(t, s, b))
}

// A tail that read nothing closes with nothing to write.
func TestATailWithNoLinesClosesClean(t *testing.T) {
	t.Parallel()
	s, _ := newWriterStore(t, testRetention)
	id := streamOf(t, s)

	require.NoError(t, openTail(t, s, id, nil, false).Close(t.Context(), nil))
	assert.Empty(t, chunksOfStream(t, s, id))
}

// A tail whose file closes is refused, and so is one opened after.
func TestATailOfAClosedFileIsRefused(t *testing.T) {
	t.Parallel()
	m, _ := newTestManager(t)
	s := open(t, m, 1)
	id := streamOf(t, s)
	tail := openTail(t, s, id, nil, false)

	require.NoError(t, m.Clear(1))
	assert.ErrorIs(t, tail.Close(t.Context(), lines(at(1, 0))), ErrClosed)
	_, err := s.OpenTail(id, "kubelet", nil, false)
	assert.ErrorIs(t, err, ErrClosed)
}

// A tail resumed at a chunk's last line extends that chunk. A tail whose read found
// that line rotated away opens an at-beginning chunk linked to nothing.
func TestATailResumesTheChunkItContinues(t *testing.T) {
	t.Parallel()
	s, _ := newWriterStore(t, testRetention)
	a, b := twoStreams(t, s)
	for _, id := range []int64{a, b} {
		write(t, s, Fill{Stream: id, Backend: "kubelet", Lines: lines(at(1, 0), at(2, 0))})
	}

	require.NoError(t, openTail(t, s, a, new(at(2, 0)), false).Close(t.Context(), lines(at(2, 1))))
	require.NoError(t, openTail(t, s, b, nil, true).Close(t.Context(), lines(at(3, 0))))

	assert.Equal(t, []Chunk{chunk(at(1, 0), at(2, 1))}, chunksOfStream(t, s, a))
	assert.Equal(t, []Chunk{chunk(at(1, 0), at(2, 0)), atBeginning(chunk(at(3, 0), at(3, 0)))}, chunksOfStream(t, s, b))
}

// A batch written to a stream past its finished_at, the next instance's lines under a
// lagging mirror's id, is trimmed to it.
func TestABatchPastAFinishedStreamIsTrimmed(t *testing.T) {
	t.Parallel()
	s, _ := newWriterStore(t, testRetention)
	row := app()
	row.FinishedAt = second(2)
	ids, err := s.PutStreams(t.Context(), []Stream{row})
	require.NoError(t, err)

	require.NoError(t, openTail(t, s, ids[0], nil, false).Close(t.Context(), lines(at(1, 0), at(9, 0))))

	assert.Equal(t, []Pos{at(1, 0)}, positions(t, s, ids[0]))
	assert.Equal(t, []Chunk{chunk(at(1, 0), at(1, 0))}, chunksOfStream(t, s, ids[0]))
}

// A write that fails loses its lines, so the tail fails: it takes no more lines, which
// would cover the missing ones as its own, and its close closes its chunk and answers
// the failure.
func TestAFailedWriteFailsTheTail(t *testing.T) {
	t.Parallel()
	s, tick := newWriterStore(t, testRetention)
	id := streamOf(t, s)
	pings := subscribe(t, s, id)
	tail := openTail(t, s, id, nil, false)
	require.NoError(t, tail.Write(t.Context(), lines(at(1, 0))))
	fire(t, tick)
	testutil.Recv(t, pings, "the first batch's ping")

	refuse(t, s, "INSERT", "lines")
	require.NoError(t, tail.Write(t.Context(), lines(at(2, 0))))
	fire(t, tick)
	testutil.Wait(t, tail.Failed(), "the tail's failure")
	using(t, s, func(f *file) {
		_, err := f.db.Exec(`DROP TRIGGER refuse_INSERT_lines`)
		require.NoError(t, err)
	})
	assert.Error(t, tail.Write(t.Context(), lines(at(3, 0))), "a failed tail takes no more lines")
	assert.Error(t, tail.Close(t.Context(), lines(at(4, 0))))

	assert.Equal(t, []Pos{at(1, 0)}, positions(t, s, id))
	assert.Equal(t, []Chunk{chunk(at(1, 0), at(1, 0))}, chunksOfStream(t, s, id))
}

// A fill can start a chunk at the line a tail's first batch starts at before that batch
// lands: the tail takes the row over with its later end, holds it open, and extends it
// from that end.
func TestATailTakesOverAFillsChunkAtItsFirstLine(t *testing.T) {
	t.Parallel()
	s, tick := newWriterStore(t, testRetention)
	id := streamOf(t, s)
	write(t, s, Fill{Stream: id, Backend: "kubelet", Lines: lines(at(1, 0), at(2, 0), at(3, 0))})
	pings := subscribe(t, s, id)

	tail := openTail(t, s, id, nil, false)
	require.NoError(t, tail.Write(t.Context(), lines(at(1, 0))))
	fire(t, tick)
	testutil.Recv(t, pings, "the first batch's ping")
	assert.Equal(t, []Chunk{opened(chunk(at(1, 0), at(3, 0)))}, chunksOfStream(t, s, id))

	require.NoError(t, tail.Write(t.Context(), lines(at(2, 0))))
	fire(t, tick)
	testutil.Recv(t, pings, "the second batch's ping")
	assert.Equal(t, []Chunk{opened(chunk(at(1, 0), at(3, 0)))}, chunksOfStream(t, s, id), "a line the row holds keeps its end")

	require.NoError(t, tail.Close(t.Context(), lines(at(4, 0))))
	assert.Equal(t, []Chunk{chunk(at(1, 0), at(4, 0))}, chunksOfStream(t, s, id))
}

// A fill starting at a tail's first line can extend the tail's open row after the tail
// has written it: the tail's next write keeps the fill's later end.
func TestATailKeepsAFillsLaterEndOnItsRow(t *testing.T) {
	t.Parallel()
	s, tick := newWriterStore(t, testRetention)
	id := streamOf(t, s)
	pings := subscribe(t, s, id)
	tail := openTail(t, s, id, nil, false)
	require.NoError(t, tail.Write(t.Context(), lines(at(1, 0))))
	fire(t, tick)
	testutil.Recv(t, pings, "the first batch's ping")

	write(t, s, Fill{Stream: id, Backend: "kubelet", Lines: lines(at(1, 0), at(2, 0), at(3, 0))})
	testutil.Recv(t, pings, "the fill's ping")
	require.NoError(t, tail.Write(t.Context(), lines(at(2, 0))))
	fire(t, tick)
	testutil.Recv(t, pings, "the second batch's ping")
	assert.Equal(t, []Chunk{opened(chunk(at(1, 0), at(3, 0)))}, chunksOfStream(t, s, id))

	require.NoError(t, tail.Close(t.Context(), lines(at(4, 0))))
	assert.Equal(t, []Chunk{chunk(at(1, 0), at(4, 0))}, chunksOfStream(t, s, id))
}

// A tail answers the last line that has landed, from before the commit's ping and
// past its close.
func TestATailAnswersWhatItCommitted(t *testing.T) {
	t.Parallel()
	s, tick := newWriterStore(t, testRetention)
	id := streamOf(t, s)
	pings := subscribe(t, s, id)
	tail := openTail(t, s, id, nil, false)
	assert.Nil(t, tail.Committed())

	require.NoError(t, tail.Write(t.Context(), lines(at(1, 0), at(2, 0))))
	fire(t, tick)
	testutil.Recv(t, pings, "the batch's ping")
	assert.Equal(t, new(at(2, 0)), tail.Committed())

	require.NoError(t, tail.Close(t.Context(), lines(at(3, 0))))
	assert.Equal(t, new(at(3, 0)), tail.Committed())
}

// A tail that closes with no lines left still closes the chunk it wrote.
func TestATailClosedWithNoLinesLeftClosesItsChunk(t *testing.T) {
	t.Parallel()
	s, tick := newWriterStore(t, testRetention)
	id := streamOf(t, s)
	pings := subscribe(t, s, id)
	tail := openTail(t, s, id, nil, false)
	require.NoError(t, tail.Write(t.Context(), lines(at(1, 0))))
	fire(t, tick)
	testutil.Recv(t, pings, "the batch's ping")

	require.NoError(t, tail.Close(t.Context(), nil))
	assert.Equal(t, []Chunk{chunk(at(1, 0), at(1, 0))}, chunksOfStream(t, s, id))
}

// A chunk a tailer holds open is not evicted, since the tail would write it back over
// the evicted lines; closed, it goes like any other.
func TestAnOpenChunkIsNotEvicted(t *testing.T) {
	t.Parallel()
	s, tick := newWriterStore(t, testRetention)
	id := streamOf(t, s)
	pings := subscribe(t, s, id)
	tail := openTail(t, s, id, nil, false)
	require.NoError(t, tail.Write(t.Context(), lines(at(1, 0), at(2, 0))))
	fire(t, tick)
	testutil.Recv(t, pings, "the batch's ping")
	chunks, err := s.Chunks(t.Context(), id, "")
	require.NoError(t, err)
	require.Len(t, chunks, 1)

	require.NoError(t, s.Evict(t.Context(), chunks[0].ID, anyStamp))
	assert.Equal(t, []Chunk{opened(chunk(at(1, 0), at(2, 0)))}, chunksOfStream(t, s, id), "held by its tailer")
	assert.Len(t, positions(t, s, id), 2)

	require.NoError(t, tail.Close(t.Context(), nil))
	require.NoError(t, s.Evict(t.Context(), chunks[0].ID, anyStamp))
	assert.Empty(t, chunksOfStream(t, s, id))
	assert.Empty(t, positions(t, s, id))
}

// A flush takes at most WriteQueue batches, so tails that keep the queue full cannot
// grow what the writer holds past the queue's bound; the rest wait for the next.
func TestAFlushTakesAtMostWriteQueueBatches(t *testing.T) {
	t.Parallel()
	ret := testRetention
	ret.WriteQueue = 2
	s, _ := newWriterStore(t, ret)
	id := streamOf(t, s)
	tail := openTail(t, s, id, nil, false)
	// A writer of its own, so the file's never takes the batches queued here.
	w := &writer{ret: ret, queue: mpsc.New[batch](ret.WriteQueue, mpsc.WithKeepOpen()), tails: map[*Tail]tailState{}}
	using(t, s, func(f *file) { w.f = f })
	tx := w.queue.Sender()
	defer tx.Close()
	w.pending = []batch{{tail: tail, lines: lines(at(1, 0))}}
	require.NoError(t, tx.Send(batch{tail: tail, lines: lines(at(2, 0))}))
	require.NoError(t, tx.Send(batch{tail: tail, lines: lines(at(3, 0))}))

	w.flush()

	assert.Equal(t, []Pos{at(1, 0), at(2, 0)}, positions(t, s, id), "the queue's bound of batches")
	left, err := w.queue.Receiver().TryRecv()
	require.NoError(t, err)
	assert.Equal(t, at(3, 0), left.lines[0].Pos, "left for the next flush")
}

// A write waits on a full queue alone: one whose context has ended lands when the
// queue has room, and waits only when there is none.
func TestAWriteWaitsOnAFullQueueAlone(t *testing.T) {
	t.Parallel()
	// A queue of one batch that nobody drains.
	queue := mpsc.New[batch](1, mpsc.WithKeepOpen())
	tail := &Tail{tx: queue.Sender(), file: t.Context(), failed: make(chan struct{})}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	require.NoError(t, tail.Write(ctx, lines(at(1, 0))), "the queue has room")
	assert.ErrorIs(t, tail.Write(ctx, lines(at(2, 0))), context.Canceled, "the queue is full")
}

// A close whose context has ended still hands the writer the tail's end, so its
// chunk closes; the context bounds the wait for the write alone.
func TestACloseWithAnEndedContextStillClosesTheChunk(t *testing.T) {
	t.Parallel()
	s, tick := newWriterStore(t, testRetention)
	id := streamOf(t, s)
	pings := subscribe(t, s, id)
	tail := openTail(t, s, id, nil, false)
	require.NoError(t, tail.Write(t.Context(), lines(at(1, 0))))
	fire(t, tick)
	testutil.Recv(t, pings, "the batch's ping")

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	// The write may land before Close reads ctx; either answer is correct.
	if err := tail.Close(ctx, lines(at(2, 0))); err != nil {
		assert.ErrorIs(t, err, context.Canceled)
	}

	testutil.Recv(t, pings, "the last batch's ping")
	assert.Equal(t, []Chunk{chunk(at(1, 0), at(2, 0))}, chunksOfStream(t, s, id))
}

// A tail whose last write fails still has its chunk closed, in a write of its own:
// the failed write rolled the close back, and nothing of the tail's follows it.
func TestAFailedLastWriteStillClosesTheChunk(t *testing.T) {
	t.Parallel()
	s, tick := newWriterStore(t, testRetention)
	id := streamOf(t, s)
	pings := subscribe(t, s, id)
	tail := openTail(t, s, id, nil, false)
	require.NoError(t, tail.Write(t.Context(), lines(at(1, 0))))
	fire(t, tick)
	testutil.Recv(t, pings, "the batch's ping")

	refuse(t, s, "INSERT", "lines")
	assert.Error(t, tail.Close(t.Context(), lines(at(2, 0))))

	assert.Equal(t, []Chunk{chunk(at(1, 0), at(1, 0))}, chunksOfStream(t, s, id))
	assert.Equal(t, []Pos{at(1, 0)}, positions(t, s, id))
}

// A tail still open when its stream's trim cuts its chunk goes on in a chunk of its
// own, since the trim cut what the chunk covered past the instance's end.
func TestATailPastATrimGoesOnInAChunkOfItsOwn(t *testing.T) {
	t.Parallel()
	s, tick := newWriterStore(t, testRetention)
	row := app()
	row.FinishedAt = second(2)
	other := app()
	other.Container, other.ContainerID, other.Default = "proxy", "containerd://2", false
	ids, err := s.PutStreams(t.Context(), []Stream{row, other})
	require.NoError(t, err)
	pings := subscribe(t, s, ids[0])
	tail := openTail(t, s, ids[0], nil, false)
	otherTail := openTail(t, s, ids[1], nil, false)

	require.NoError(t, otherTail.Write(t.Context(), lines(at(1, 0))))
	require.NoError(t, tail.Write(t.Context(), lines(at(1, 0), at(9, 0))))
	fire(t, tick)
	testutil.Recv(t, pings, "the first batch's ping")
	assert.Equal(t, []Chunk{chunk(at(1, 0), at(1, 0))}, chunksOfStream(t, s, ids[0]), "the trimmed chunk is closed")
	assert.Equal(t, []Chunk{opened(chunk(at(1, 0), at(1, 0)))}, chunksOfStream(t, s, ids[1]), "another stream's tail writes on")

	require.NoError(t, tail.Close(t.Context(), lines(at(1, 1))))
	assert.Equal(t, []Pos{at(1, 0), at(1, 1)}, positions(t, s, ids[0]))
	assert.Equal(t, []Chunk{chunk(at(1, 0), at(1, 0)), chunk(at(1, 1), at(1, 1))}, chunksOfStream(t, s, ids[0]))
	require.NoError(t, otherTail.Close(t.Context(), nil))
}

// A tail's write the file refuses or cannot run fails the tail and lands no line: the
// read of the chunk it continues, its chunk's insert, the close of a chunk it ends, the
// trim of its finished stream, or the close of the chunk that trim cut.
func TestATailsWriteTheFileRefusesFailsTheTail(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		breakFile func(t *testing.T, s *Store)
		prev      *Pos
		finished  bool
		last      bool
		want      string
	}{
		"the chunk it continues": {breakFile: func(t *testing.T, s *Store) { alter(t, s, `DROP TABLE chunks`) }, prev: new(at(1, 0)), want: "no such table"},
		"its chunk's insert":     {breakFile: func(t *testing.T, s *Store) { refuse(t, s, "INSERT", "chunks") }},
		"its chunk's close":      {breakFile: refuseClose, last: true},
		"the trim":               {breakFile: func(t *testing.T, s *Store) { refuse(t, s, "DELETE", "lines") }, finished: true},
		"the trimmed close":      {breakFile: refuseClose, finished: true},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			s, tick := newWriterStore(t, testRetention)
			row := app()
			if tc.finished {
				row.FinishedAt = second(2)
			}
			ids, err := s.PutStreams(t.Context(), []Stream{row})
			require.NoError(t, err)
			tail := openTail(t, s, ids[0], tc.prev, false)
			tc.breakFile(t, s)

			batch := lines(at(1, 0), at(9, 0))
			if tc.last {
				err = tail.Close(t.Context(), batch)
			} else {
				require.NoError(t, tail.Write(t.Context(), batch))
				fire(t, tick)
				testutil.Wait(t, tail.Failed(), "the tail's failure")
				err = tail.Err()
			}

			assert.ErrorContains(t, err, cmp.Or(tc.want, "refused"))
			assert.Empty(t, positions(t, s, ids[0]))
		})
	}
}

// A close whose write the file never answers, since the file closed first, answers
// ErrClosed.
func TestACloseWhoseFileClosesFirstIsClosed(t *testing.T) {
	t.Parallel()
	// A queue nobody drains, as a writer that stopped leaves it.
	queue := mpsc.New[batch](1, mpsc.WithKeepOpen())
	file, closeFile := context.WithCancel(t.Context())
	tail := &Tail{tx: queue.Sender(), file: file, failed: make(chan struct{})}
	closeFile()

	assert.ErrorIs(t, tail.Close(t.Context(), lines(at(1, 0))), ErrClosed)
}
