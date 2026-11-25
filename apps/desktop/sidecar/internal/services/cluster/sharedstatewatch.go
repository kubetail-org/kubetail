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

package cluster

import (
	"context"
	"sync"
	"time"

	"github.com/amorey/gochan/watch"
)

// sharedStateWatch reads a value once per cadence on behalf of every subscriber of a state
// watch whose reading is the same for all of them. The first subscriber starts the loop and the
// last to leave stops it. A join asks for a read, so a subscriber's first reading is one
// begun after it joined, and joiners share that read. Readings fan out as a latest value,
// so a slow subscriber skips to the newest. A reading is shared by every subscriber, so
// none may modify it.
type sharedStateWatch[T any] struct {
	read func(context.Context) (T, error)

	mu  sync.Mutex
	run *stateWatchRun[T]
}

// newSharedStateWatch makes a sharedStateWatch that reads through read while anyone subscribes.
func newSharedStateWatch[T any](read func(context.Context) (T, error)) *sharedStateWatch[T] {
	return &sharedStateWatch[T]{read: read}
}

// stateWatchRun is one life of the loop, from its first subscriber to its last. A read that
// fails ends the run: every subscriber gets the error, and the next subscriber starts a
// fresh run.
type stateWatchRun[T any] struct {
	hub  *watch.Hub[stateWatchReading[T]]
	wake chan struct{}
	stop context.CancelFunc
	// Under sharedStateWatch.mu. begun counts the reads started.
	subs  int
	begun uint64
}

// stateWatchReading is one read: a value, or the error that ended the run. seq is the read's
// number, from 1; the hub's seed has 0 and is never returned.
type stateWatchReading[T any] struct {
	seq   uint64
	value T
	err   error
}

// stateWatchSeat is one subscriber's seat on the loop.
type stateWatchSeat[T any] struct {
	shared *sharedStateWatch[T]
	run    *stateWatchRun[T]
	rx     *watch.Receiver[stateWatchReading[T]]
	// joinedAt is how many reads the run had begun when the subscriber joined; those
	// predate it.
	joinedAt uint64
	once     sync.Once
}

// subscribe joins the loop, starting it if no one holds it; cadence paces a loop this
// call starts, so every subscriber passes the same one. Close the subscription when done.
func (g *sharedStateWatch[T]) subscribe(cadence time.Duration) *stateWatchSeat[T] {
	g.mu.Lock()
	defer g.mu.Unlock()

	if g.run == nil {
		ctx, stop := context.WithCancel(context.Background())
		g.run = &stateWatchRun[T]{hub: watch.New(stateWatchReading[T]{}), wake: make(chan struct{}, 1), stop: stop}
		go g.loop(ctx, g.run, cadence)
	} else {
		// Ask for a read begun after this join; one pending wake serves every joiner.
		select {
		case g.run.wake <- struct{}{}:
		default:
		}
	}
	g.run.subs++
	return &stateWatchSeat[T]{shared: g, run: g.run, rx: g.run.hub.Receiver(), joinedAt: g.run.begun}
}

func (g *sharedStateWatch[T]) loop(ctx context.Context, run *stateWatchRun[T], cadence time.Duration) {
	// A failed read ends the loop with subscribers still seated, so the loop releases its
	// own context rather than waiting on the last of them to.
	defer run.stop()
	ticker := time.NewTicker(cadence)
	defer ticker.Stop()

	for {
		reading := stateWatchReading[T]{seq: g.begin(run)}
		reading.value, reading.err = g.read(ctx)
		if reading.err != nil {
			g.retire(run)
			_ = run.hub.Sender().Send(reading)
			run.hub.Sender().Close()
			return
		}
		_ = run.hub.Sender().Send(reading)
		// The cadence counts from the reading, so a read a join asked for is not followed
		// at once by the tick's.
		ticker.Reset(cadence)

		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		case <-run.wake:
		}
	}
}

// begin numbers the read about to start and takes any wake asked before it, since this
// read answers it.
func (g *sharedStateWatch[T]) begin(run *stateWatchRun[T]) uint64 {
	g.mu.Lock()
	defer g.mu.Unlock()
	run.begun++
	select {
	case <-run.wake:
	default:
	}
	return run.begun
}

// retire makes run no longer the one a new subscriber joins.
func (g *sharedStateWatch[T]) retire(run *stateWatchRun[T]) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.run == run {
		g.run = nil
	}
}

// Next waits for a reading newer than the last one returned, and never a value from a read
// begun before the subscriber joined. It returns the error that ended the run or ctx's,
// whichever comes first; the run's error is returned even from a read begun before the
// join, since it is the run's last word.
func (s *stateWatchSeat[T]) Next(ctx context.Context) (T, error) {
	for {
		reading, err := s.rx.RecvContext(ctx)
		if err != nil {
			var zero T
			return zero, err
		}
		if reading.seq <= s.joinedAt && reading.err == nil {
			continue
		}
		return reading.value, reading.err
	}
}

// Close leaves the loop, stopping it if no one else holds it. Idempotent.
func (s *stateWatchSeat[T]) Close() {
	s.once.Do(s.leave)
}

func (s *stateWatchSeat[T]) leave() {
	s.rx.Close()

	g := s.shared
	g.mu.Lock()
	defer g.mu.Unlock()
	s.run.subs--
	if s.run.subs > 0 {
		return
	}
	s.run.stop()
	if g.run == s.run {
		g.run = nil
	}
}
