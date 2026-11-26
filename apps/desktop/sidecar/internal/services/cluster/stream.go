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
	"sync/atomic"

	"github.com/amorey/beehive"
)

// Stream is what a watch whose source can die returns: the frames, and why they
// stopped. Frames closes on every exit — ctx cancelled, source drained, source
// failed — so Err is the only thing that tells a failure apart from an ordinary
// teardown, and a consumer that ignores it turns a broken watch into a silent one.
//
// The rule is the SOURCE, not the shape: a watch over a fallible upstream returns a
// Stream, state watches included. One that retries forever and has no terminal failure to
// report may stay a plain channel.
//
// **A clean end is a nil Err, and some expected endings are clean.** The cached-data
// watches end that way when their cache is cleared: a user pressing a button is not a
// broken watch, and reporting one would put an error in front of them per open watch. The
// reason the "silent broken watch" argument above does not cover it is that the client's
// reconnect succeeds — there is no invisible retry loop, and the next snapshot is the
// report. Err is for a source that is actually broken.
type Stream[T any] struct {
	Frames <-chan T
	err    atomic.Pointer[error]
	// done closes once a NewStream pump has returned, which a test can wait on without
	// reading Frames: a read would free a send parked on the buffer.
	done chan struct{}
}

// Err returns why the stream ended, or nil if it ended cleanly. Read it once
// Frames has closed; before that it is always nil.
func (s *Stream[T]) Err() error {
	if p := s.err.Load(); p != nil {
		return *p
	}
	return nil
}

// sendFrame delivers one frame, reporting false when the consumer is gone. It is how
// a pump keeps NewStream's requirement below: a bare channel send blocks forever once
// the consumer stops draining, leaking the goroutine and the watch behind it.
func sendFrame[T any](ctx context.Context, out chan<- T, frame T) bool {
	select {
	case out <- frame:
		return true
	case <-ctx.Done():
		return false
	}
}

// collectionWatch is what one kind contributes to a collection watch: how a stored object
// becomes a frame, how a removal does, and the bookmark that closes a snapshot. The
// pumps below are the rest, and are the same for every kind — which is the point, since
// the bookmark discipline is a protocol rule rather than a per-kind choice.
// See docs/data-access.md.
type collectionWatch[Spec, Status, Frame any] struct {
	// frame projects one object into a frame of the given type. Fallible because a
	// projection reads the owner edge, which is a store read.
	frame func(CollectionWatchFrameType, *beehive.Object[Spec, Status]) (Frame, error)
	// departed builds the removal frame. Its own function rather than frame's business
	// because the row is gone: beehive loads no owner edge for it, and beehive reports a
	// removal whose final row it could not decode with no object at all. The frame must
	// still carry the id — nothing later in the log mentions a deleted one, and a
	// consumer drops a change with no entity rather than folding it, so a dropped frame
	// strands the record in its map for the life of the subscription.
	departed func(beehive.ObjectChange[Spec, Status]) Frame
	// bookmark closes the snapshot. A value rather than frame(CollectionWatchFrameBookmark, nil),
	// which would make every projection handle a nil object it is never otherwise given.
	bookmark Frame
}

// streamOne serves a single-object watch. An id holding nothing yet is a snapshot of
// none rather than a failure: the record may still arrive, and this reports it.
func (w collectionWatch[Spec, Status, Frame]) streamOne(ctx context.Context, src *beehive.ObjectStream[Spec, Status]) *Stream[Frame] {
	return NewStream(ctx, func(ctx context.Context, out chan<- Frame) error {
		var snapshot []*beehive.Object[Spec, Status]
		if src.Object != nil {
			snapshot = append(snapshot, src.Object)
		}
		return w.pump(ctx, out, snapshot, src.Changes, src.Err)
	})
}

// streamList serves a list watch — every object of a kind, or every one owned by a
// given id. The two differ only in what beehive put in the snapshot.
func (w collectionWatch[Spec, Status, Frame]) streamList(ctx context.Context, src *beehive.ObjectListStream[Spec, Status]) *Stream[Frame] {
	return NewStream(ctx, func(ctx context.Context, out chan<- Frame) error {
		return w.pump(ctx, out, src.Objects, src.Changes, src.Err)
	})
}

// streamListWhere is streamList filtered to the records keep accepts, for a scope
// beehive cannot express as an owned-object watch. The bookmark always passes.
func (w collectionWatch[Spec, Status, Frame]) streamListWhere(ctx context.Context, src *beehive.ObjectListStream[Spec, Status], keep func(Frame) bool) *Stream[Frame] {
	inner := w.streamList(ctx, src)
	return NewStream(ctx, func(ctx context.Context, out chan<- Frame) error {
		for frame := range inner.Frames {
			if !keep(frame) {
				continue
			}
			if !sendFrame(ctx, out, frame) { // coverage:ignore only the consumer leaving while a frame is in flight between the two pumps
				return nil
			}
		}
		return inner.Err()
	})
}

// pump streams a snapshot, the bookmark closing it, then every change above it.
//
// beehive hands the snapshot complete as of a resource version, with changes carrying
// everything above it, so the bookmark lands between the two without holding any frame
// back. srcErr is the upstream's terminal reason, read once the changes run out.
func (w collectionWatch[Spec, Status, Frame]) pump(
	ctx context.Context,
	out chan<- Frame,
	snapshot []*beehive.Object[Spec, Status],
	changes <-chan beehive.ObjectChange[Spec, Status],
	srcErr func() error,
) error {
	for _, obj := range snapshot {
		frame, err := w.frame(CollectionWatchFrameAdded, obj)
		if err != nil {
			return err
		}
		if !sendFrame(ctx, out, frame) {
			return nil
		}
	}
	if !sendFrame(ctx, out, w.bookmark) {
		return nil
	}
	return w.pumpAfterSnapshot(ctx, out, changes, srcErr)
}

// pumpAfterSnapshot streams every change above a snapshot the caller has already sent.
func (w collectionWatch[Spec, Status, Frame]) pumpAfterSnapshot(
	ctx context.Context,
	out chan<- Frame,
	changes <-chan beehive.ObjectChange[Spec, Status],
	srcErr func() error,
) error {
	for change := range changes {
		if change.Type == beehive.Deleted {
			if !sendFrame(ctx, out, w.departed(change)) {
				return nil
			}
			continue
		}

		// Only a removal arrives without an object.
		if change.Object == nil {
			continue
		}
		frame, err := w.frame(deltaFrameType(change), change.Object)
		if err != nil {
			return err
		}
		if !sendFrame(ctx, out, frame) {
			return nil
		}
	}
	return srcErr()
}

// NewStream runs pump on its own goroutine, publishing what it returns as the
// terminal error. Frames closes only after that error is recorded, which is what
// makes "Frames closed" a safe cue to read Err.
//
// pump must select on ctx around every send. The consumer stops draining Frames the
// moment ctx is done, so a send that blocks on the channel alone blocks forever —
// leaking the goroutine and the upstream watch behind it, once per client
// disconnect. ctx is a parameter rather than a closed-over variable so that the
// requirement is visible in every pump's own signature.
//
// Cancellation is an ordinary teardown, and pumps do not have to spell it: every read a
// pump makes takes ctx, so the reason it ends with is usually ctx's own, and anything it
// returns once ctx is done is dropped here. A non-nil error otherwise is reported to the
// client as the reason its watch died.
//
// Exported because Stream sits in the family interfaces: an implementation outside
// this package — a fake in the resolver tests — has to be able to build one.
func NewStream[T any](ctx context.Context, pump func(ctx context.Context, out chan<- T) error) *Stream[T] {
	ch := make(chan T, 1)
	s := &Stream[T]{Frames: ch, done: make(chan struct{})}
	go func() {
		defer close(ch)
		defer close(s.done)
		// Filing a cancellation would put an error in front of a user who has already
		// navigated away — it reaches the webview as a watch failure, and suppresses the
		// transport's backoff reset on the reconnect.
		if err := pump(ctx, ch); err != nil && ctx.Err() == nil {
			s.err.Store(&err)
		}
	}()
	return s
}
