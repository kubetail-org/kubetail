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
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"sync/atomic"
	"time"

	"github.com/amorey/gochan"
	"github.com/amorey/gochan/mpsc"
)

// writer is the one goroutine of an open file that writes what its tailers read. The
// tailers reach it through queue, one sender per tail; it writes what has arrived when
// its tick fires, or at once for a batch that closed full or ends its tail, in one
// transaction, and pings each stream it wrote.
type writer struct {
	f     *file
	ret   Retention
	after func(time.Duration) <-chan time.Time
	queue *mpsc.Hub[batch]
	// tails is each running tail's chunk as the file holds it, read and written by the
	// writer's goroutine alone.
	tails   map[*Tail]tailState
	pending []batch
	tick    <-chan time.Time
	done    chan struct{}
}

// batch is one hand-off from a tail: its lines, oldest first, and for the batch that
// ends the tail, where the writer answers once that write has landed or failed.
type batch struct {
	tail  *Tail
	lines []Line
	last  chan error
}

// tailState is where a tail's next line goes: the chunk it extends, nil until its
// first line, and the line a first chunk continues from, or whether it opens at the beginning.
type tailState struct {
	chunk       *Chunk
	prev        *Pos
	atBeginning bool
}

// startWriter runs the file's writer, after waiting on after for its tick.
func (f *file) startWriter(ret Retention, after func(time.Duration) <-chan time.Time) {
	w := &writer{
		f: f, ret: ret, after: after, queue: mpsc.New[batch](ret.WriteQueue, mpsc.WithKeepOpen()),
		tails: map[*Tail]tailState{}, done: make(chan struct{}),
	}
	f.w = w
	go w.run()
}

// stop ends the writer, which answers every tail still waiting with ErrClosed. Call it
// once the file's context has ended.
func (w *writer) stop() {
	w.queue.Receiver().Close()
	<-w.done
}

// run takes batches until the file closes.
func (w *writer) run() {
	defer close(w.done)
	defer func() { w.answer(w.pending, ErrClosed) }()
	for {
		select {
		case <-w.f.ctx.Done():
			return
		case b := <-w.queue.Receiver().Chan():
			w.pending = append(w.pending, b)
			if w.tick == nil {
				w.tick = w.after(w.ret.WriteTick)
			}
			if b.last != nil || w.full(b) || len(w.pending) >= w.ret.WriteQueue {
				w.flush()
			}
		case <-w.tick:
			w.flush()
		}
	}
}

// full is whether b closed at WriteBatch lines or WriteBatchBytes.
func (w *writer) full(b batch) bool {
	return len(b.lines) >= w.ret.WriteBatch || messageBytes(b.lines) >= w.ret.WriteBatchBytes
}

// flush writes the batches that have arrived in one transaction, up to WriteQueue of
// them: a queue the tails keep refilling leaves the rest for the next.
func (w *writer) flush() {
	for len(w.pending) < w.ret.WriteQueue {
		b, err := w.queue.Receiver().TryRecv()
		if err != nil {
			break
		}
		w.pending = append(w.pending, b)
	}
	batches := w.pending
	w.pending, w.tick = nil, nil
	tails := maps.Clone(w.tails)
	err := w.f.set.InTx(w.f.ctx, func(st stmts) error {
		return writeBatches(w.f.ctx, st, w.ret, tails, batches)
	})
	if err != nil {
		if w.f.ctx.Err() == nil {
			slog.Warn("log store: a tail's write failed", "err", err)
		}
		// Nothing landed, so each tail's lines from its batch on are missing, and a
		// tail that went on would claim them: it takes no more lines, and its tailer
		// ends, leaving them to a fill.
		tails = maps.Clone(w.tails)
		for _, b := range batches {
			b.tail.fail(err)
		}
		w.closeEnded(batches, tails)
	} else {
		for _, b := range batches {
			if len(b.lines) > 0 && b.tail.Err() == nil {
				b.tail.committed.Store(&b.lines[len(b.lines)-1].Pos)
			}
		}
		w.f.committed(batchStreams(batches), linesBytes(batches, func(b batch) []Line { return b.lines }))
	}
	for _, b := range batches {
		if b.last != nil {
			delete(tails, b.tail)
		}
	}
	w.tails = tails
	w.answer(batches, err)
}

// closeEnded closes the chunk of each tail that batches end, each in a write of its
// own: the write that was to close it failed, and no write of the tail's follows it.
// A chunk this close fails too is left to the janitor's next sweep.
func (w *writer) closeEnded(batches []batch, tails map[*Tail]tailState) {
	for _, b := range batches {
		s := tails[b.tail]
		if b.last == nil || s.chunk == nil {
			continue
		}
		if _, err := w.f.set.Stmts().Exec(w.f.ctx, stmtCloseChunk, s.chunk.ID); err != nil {
			if w.f.ctx.Err() == nil {
				slog.Warn("log store: a tail's chunk could not be closed", "err", err)
			}
			w.f.unclosedMu.Lock()
			w.f.unclosed = append(w.f.unclosed, s.chunk.ID)
			w.f.unclosedMu.Unlock()
		}
	}
}

// closeUnclosed closes each chunk whose close closeEnded failed, keeping those that
// fail again for the next call. Its tail has ended, so no write extends it.
func (f *file) closeUnclosed(ctx context.Context) error {
	f.unclosedMu.Lock()
	defer f.unclosedMu.Unlock()
	var left []int64
	var errs error
	for _, id := range f.unclosed {
		if _, err := f.set.Stmts().Exec(ctx, stmtCloseChunk, id); err != nil {
			left = append(left, id)
			errs = errors.Join(errs, err)
		}
	}
	f.unclosed = left
	return errs
}

// answer tells the tail each of batches ends that its last write answered err.
func (w *writer) answer(batches []batch, err error) {
	for _, b := range batches {
		if b.last != nil {
			b.last <- err
		}
	}
}

// writeBatches files every batch, each extending its tail's chunk, then trims each
// stream they wrote that has finished.
func writeBatches(ctx context.Context, st stmts, ret Retention, tails map[*Tail]tailState, batches []batch) error {
	for _, b := range batches {
		s, ok := tails[b.tail]
		if !ok {
			s = tailState{prev: b.tail.prev, atBeginning: b.tail.atBeginning}
		}
		if b.tail.Err() != nil {
			// A failed tail's lines are left to a fill; its last batch still closes
			// its chunk.
			b.lines = nil
		}
		s, err := writeBatch(ctx, st, ret, b, s)
		if err != nil {
			return err
		}
		tails[b.tail] = s
	}
	for _, id := range batchStreams(batches) {
		trimmed, err := trimFinished(ctx, st, id)
		if err != nil {
			return err
		}
		if !trimmed {
			continue
		}
		// The trim cut what came after the instance's end, so a tail of it goes on, if at
		// all, in a chunk of its own.
		for tail, s := range tails {
			if tail.stream != id || s.chunk == nil {
				continue
			}
			if _, err := st.Exec(ctx, stmtCloseChunk, s.chunk.ID); err != nil {
				return err
			}
			tails[tail] = tailState{}
		}
	}
	return nil
}

// writeBatch files b's lines into s's chunk, cut at ChunkBytes and ChunkSpan, and
// answers where the tail's next line goes. A chunk is open while the tail writes it,
// and closed once it is cut or b ends the tail.
func writeBatch(ctx context.Context, st stmts, ret Retention, b batch, s tailState) (tailState, error) {
	if err := insertLines(ctx, st, b.tail.stream, b.lines); err != nil {
		return s, err
	}
	if len(b.lines) == 0 {
		var err error
		if b.last != nil && s.chunk != nil {
			_, err = st.Exec(ctx, stmtCloseChunk, s.chunk.ID)
		}
		return s, err
	}
	var cur Chunk
	fresh := false
	if s.chunk != nil {
		cur = *s.chunk
	} else {
		var err error
		if cur, fresh, err = startChunk(ctx, st, ret, b.tail.stream, "", b.tail.backend, s.prev, true, b.lines[0]); err != nil {
			return s, err
		}
		if fresh {
			cur.AtBeginning = s.atBeginning
		}
	}
	chunks := cutChunks(ret, cur, fresh, b.lines)
	for i := range chunks {
		chunks[i].Open = true
		c, err := putChunk(ctx, st, b.tail.stream, chunks[i])
		if err != nil {
			return s, err
		}
		chunks[i] = c
	}
	// The tail writes on into the last chunk, unless b ends it.
	done := chunks[:len(chunks)-1]
	if b.last != nil {
		done = chunks
	}
	for _, c := range done {
		if _, err := st.Exec(ctx, stmtCloseChunk, c.ID); err != nil {
			return s, err
		}
	}
	s.chunk = &chunks[len(chunks)-1]
	return s, nil
}

// batchStreams is each stream batches write a line to, once.
func batchStreams(batches []batch) []int64 {
	return streamsWithLines(batches, func(b batch) (int64, int) { return b.tail.stream, len(b.lines) })
}

// Tail is one tailer's way into its file's writer: the lines it hands over go into one
// open chunk at a time, unlabelled, from the line it continues, and the chunk closes
// when the tail does.
type Tail struct {
	tx *mpsc.Sender[batch]
	// file ends when the file closes, which ends every tail with it.
	file    context.Context
	stream  int64
	backend string
	// prev is the last line of the chunk the tail extends, nil when it starts on a
	// gap, and atBeginning whether its read began at the backend's oldest line, which
	// opens an at-beginning chunk.
	prev        *Pos
	atBeginning bool
	// committed is the last line of the tail's that has landed, nil before the first.
	committed atomic.Pointer[Pos]
	// failed is closed once a write of the tail's lines has failed, err saying why.
	// The writer alone closes it.
	failed chan struct{}
	err    error
}

// OpenTail opens a tail of the stream id for backend, extending the chunk ending at
// prev, or opening an at-beginning chunk. Close it.
func (s *Store) OpenTail(id int64, backend string, prev *Pos, atBeginning bool) (*Tail, error) {
	f, done, err := s.use()
	if err != nil {
		return nil, fmt.Errorf("open tail: %w", err)
	}
	defer done()
	return &Tail{
		tx: f.w.queue.Sender(), file: f.ctx, stream: id, backend: backend, prev: prev, atBeginning: atBeginning,
		failed: make(chan struct{}),
	}, nil
}

// Committed is the last line of the tail's that has landed, nil before the first; it
// holds once the tail has closed. It is set before the commit's ping.
func (t *Tail) Committed() *Pos { return t.committed.Load() }

// Failed is closed once a write of the tail's lines has failed: the writer takes no
// more of them, and Err says why.
func (t *Tail) Failed() <-chan struct{} { return t.failed }

// Err is why a write of the tail's lines failed, nil until Failed closes.
func (t *Tail) Err() error {
	select {
	case <-t.failed:
		return t.err
	default:
		return nil
	}
}

// fail records err as the tail's failure, unless one is recorded. The writer alone
// calls it.
func (t *Tail) fail(err error) {
	if t.Err() == nil {
		t.err = fmt.Errorf("tail: %w", err)
		close(t.failed)
	}
}

// Write hands lines, oldest first, to the writer, waiting while its queue is full
// until ctx ends.
func (t *Tail) Write(ctx context.Context, lines []Line) error {
	if err := t.Err(); err != nil {
		return err
	}
	if err := t.send(ctx, batch{tail: t, lines: lines}); err != nil {
		return fmt.Errorf("tail: %w", err)
	}
	return nil
}

// send puts b on the writer's queue, waiting on a full queue until ctx ends. The wait
// is for the queue alone: a ctx that ended before the send still lands b when there
// is room, so a tailer slow to reach the writer is not read as the writer falling
// behind. A queue the file's close ended is ErrClosed.
func (t *Tail) send(ctx context.Context, b batch) error {
	err := t.tx.TrySend(b)
	if errors.Is(err, gochan.ErrFull) {
		err = t.tx.SendContext(ctx, b)
	}
	if errors.Is(err, gochan.ErrClosed) {
		return ErrClosed
	}
	return err
}

// Close hands the writer the tail's last lines and closes its chunk in the same write,
// and answers once that write has landed or failed, or the file has closed. The
// hand-over waits on a full queue until the file closes, whatever ctx says, since a
// chunk the writer never learns is done stays open until the file is next opened;
// ctx bounds the wait for the write alone. A tail whose write failed has its chunk
// closed and answers that failure.
func (t *Tail) Close(ctx context.Context, lines []Line) error {
	defer t.tx.Close()
	last := make(chan error, 1)
	if err := t.send(t.file, batch{tail: t, lines: lines, last: last}); err != nil {
		return fmt.Errorf("close tail: %w", err)
	}
	select {
	case err := <-last:
		if err == nil {
			err = t.Err()
		}
		return err
	case <-t.file.Done():
		return fmt.Errorf("close tail: %w", ErrClosed)
	case <-ctx.Done():
		return ctx.Err()
	}
}
