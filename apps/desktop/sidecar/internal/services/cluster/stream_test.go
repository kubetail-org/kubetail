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
	"errors"
	"fmt"
	"testing"

	"github.com/amorey/beehive"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/testutil"
)

// A consumer learns a watch died by seeing Frames close and then reading Err, so the
// reason must already be recorded by the time the close is observable. Were the order
// reversed, a resolver would read nil on the losing side of the race and report a
// broken watch as a graceful end — the exact failure this type exists to prevent.
func TestStreamRecordsTheReasonBeforeClosingFrames(t *testing.T) {
	boom := errors.New("watch ended: too old")
	s := NewStream(context.Background(), func(_ context.Context, out chan<- int) error {
		out <- 1
		return boom
	})

	require.Equal(t, 1, testutil.Recv(t, s.Frames, "a stream value"))
	testutil.WaitClosed(t, s.Frames, "the stream")
	assert.Equal(t, boom, s.Err())
}

func TestStreamReportsNoErrorOnACleanEnd(t *testing.T) {
	s := NewStream(context.Background(), func(context.Context, chan<- int) error { return nil })

	testutil.WaitClosed(t, s.Frames, "the stream")
	assert.NoError(t, s.Err())
}

// The whole of what sendFrame is for. A bare channel send would park here forever,
// leaking the pump goroutine and the store watch behind it once per client disconnect —
// and reporting nothing, since Frames never closes.
func TestSendFrameGivesUpWhenTheConsumerIsGone(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	assert.False(t, sendFrame(ctx, make(chan int), 1), "nothing is draining out")
}

// The shape every pump owes its consumer: the reader stops draining Frames when ctx
// ends, so a pump that does not select on it blocks forever on its next send. A
// cancelled watch is a teardown, not a failure, so Err stays nil.
func TestStreamPumpEndsOnACancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	s := NewStream(ctx, func(ctx context.Context, out chan<- int) error {
		for {
			select {
			case out <- 1:
			case <-ctx.Done():
				return nil
			}
		}
	})

	// Fill the buffer and leave a send parked, so cancellation has to be what
	// releases the pump rather than an empty channel.
	require.Equal(t, 1, testutil.Recv(t, s.Frames, "a stream value"))
	cancel()

	testutil.WaitClosed(t, s.Frames, "the stream")
	assert.NoError(t, s.Err(), "a cancelled watch is a teardown, not a failure")
}

// --- the shared delta pump ---

// The pump is one implementation every kind's collection watch runs on, so its rules are
// pinned here over a stand-in kind rather than per kind. What a kind still owes its own
// tests is the projection and the departure — the two things it supplies.
type testSpec struct{ Name string }

type testStatus struct{}

type testFrame struct {
	Type CollectionWatchFrameType
	ID   ObjectID
}

var testWatch = collectionWatch[testSpec, testStatus, testFrame]{
	frame: func(t CollectionWatchFrameType, obj *beehive.Object[testSpec, testStatus]) (testFrame, error) {
		return testFrame{Type: t, ID: ObjectID(obj.ID)}, nil
	},
	departed: func(change beehive.ObjectChange[testSpec, testStatus]) testFrame {
		return testFrame{Type: CollectionWatchFrameDeleted, ID: ObjectID(change.ID)}
	},
	bookmark: testFrame{Type: CollectionWatchFrameBookmark},
}

// pumpFrames runs a kind's watch over a snapshot and a hand-driven change stream, and
// collects what it produced. A departure spans two log entries whose grouping is
// beehive's to decide, so driving the pump directly is the only way to pin both shapes.
// Shared by the kinds' tests, which pass their own watch to pin its projection.
func pumpFrames[Spec, Status, Frame any](t *testing.T, w collectionWatch[Spec, Status, Frame], snapshot []*beehive.Object[Spec, Status], changes ...beehive.ObjectChange[Spec, Status]) []Frame {
	t.Helper()
	src := make(chan beehive.ObjectChange[Spec, Status], len(changes))
	for _, c := range changes {
		src <- c
	}
	close(src)

	out := make(chan Frame, len(snapshot)+len(changes)+1)
	require.NoError(t, w.pump(context.Background(), out, snapshot, src, func() error { return nil }))
	close(out)

	var frames []Frame
	for f := range out {
		frames = append(frames, f)
	}
	return frames
}

// The bookmark closes the snapshot: exactly one, after the last snapshot frame and
// before the first change. A consumer renders an empty state off it, so a collection
// with nothing in it must still get one.
func TestPumpEmitsTheSnapshotThenABookmark(t *testing.T) {
	frames := pumpFrames(t, testWatch, []*beehive.Object[testSpec, testStatus]{{ID: 1}, {ID: 2}},
		beehive.ObjectChange[testSpec, testStatus]{Type: beehive.Modified, ID: 1, Object: &beehive.Object[testSpec, testStatus]{ID: 1}},
	)

	require.Len(t, frames, 4)
	assert.Equal(t, []CollectionWatchFrameType{CollectionWatchFrameAdded, CollectionWatchFrameAdded, CollectionWatchFrameBookmark, CollectionWatchFrameModified},
		[]CollectionWatchFrameType{frames[0].Type, frames[1].Type, frames[2].Type, frames[3].Type})
}

// Only a removal arrives without an object, so anything else that does is dropped
// rather than folded: a frame with no entity is one a consumer discards anyway, and
// building it would dereference the object that is missing.
func TestPumpDropsAChangeWithNoObject(t *testing.T) {
	frames := pumpFrames(t, testWatch, nil,
		beehive.ObjectChange[testSpec, testStatus]{Type: beehive.Modified, ID: 7, Object: nil},
	)

	require.Len(t, frames, 1)
	assert.Equal(t, CollectionWatchFrameBookmark, frames[0].Type)
}

// A removal whose final row beehive could not decode carries no object, and nothing
// later in the log mentions the id — so the departure is built from the change alone.
func TestPumpReportsAnUndecodableDeparture(t *testing.T) {
	frames := pumpFrames(t, testWatch, nil,
		beehive.ObjectChange[testSpec, testStatus]{Type: beehive.Deleted, ID: 7, Object: nil},
	)

	require.Len(t, frames, 2)
	assert.Equal(t, testFrame{Type: CollectionWatchFrameDeleted, ID: 7}, frames[1])
}

// A failing projection ends the stream with its reason rather than skipping the frame:
// a consumer that silently missed one would hold a record that no later frame corrects.
func TestPumpEndsOnAFailedProjection(t *testing.T) {
	boom := errors.New("read owner: no such row")
	failing := testWatch
	failing.frame = func(CollectionWatchFrameType, *beehive.Object[testSpec, testStatus]) (testFrame, error) {
		return testFrame{}, boom
	}

	err := failing.pump(context.Background(), make(chan testFrame, 1),
		[]*beehive.Object[testSpec, testStatus]{{ID: 1}}, nil, func() error { return nil })

	assert.ErrorIs(t, err, boom)

	changes := make(chan beehive.ObjectChange[testSpec, testStatus], 1)
	changes <- beehive.ObjectChange[testSpec, testStatus]{Type: beehive.Modified, ID: 1, Object: &beehive.Object[testSpec, testStatus]{ID: 1}}
	close(changes)
	err = failing.pump(context.Background(), make(chan testFrame, 1), nil, changes, func() error { return nil })

	assert.ErrorIs(t, err, boom, "a change after the snapshot ends the stream the same way")
}

// The pump ends by reporting why its source did, which is the only thing telling a dead
// watch from a drained one — Frames closes either way.
func TestPumpReportsWhyTheSourceDied(t *testing.T) {
	boom := errors.New("watch ended: resource version too old")
	changes := make(chan beehive.ObjectChange[testSpec, testStatus])
	close(changes)

	err := testWatch.pump(context.Background(), make(chan testFrame, 1), nil, changes,
		func() error { return boom })

	assert.Equal(t, boom, err)
}

// parkedPump is the pump running against a channel nothing drains, so it parks on its
// next send exactly as it would against a client that stopped reading.
type parkedPump struct {
	out     chan testFrame
	changes chan beehive.ObjectChange[testSpec, testStatus]
	done    chan error
	cancel  context.CancelFunc
}

func startPump(t *testing.T, snapshot []*beehive.Object[testSpec, testStatus]) *parkedPump {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	p := &parkedPump{
		out:     make(chan testFrame),
		changes: make(chan beehive.ObjectChange[testSpec, testStatus]),
		done:    make(chan error, 1),
		cancel:  cancel,
	}
	go func() {
		p.done <- testWatch.pump(ctx, p.out, snapshot, p.changes, func() error { return nil })
	}()
	return p
}

// takeBookmark drains the snapshot's closing frame, which puts the pump in its change
// loop. The unbuffered handoff is the synchronization: the send below cannot complete
// until the pump has taken the change and gone on to send its frame.
func takeBookmark(t *testing.T, p *parkedPump) {
	t.Helper()
	require.Equal(t, CollectionWatchFrameBookmark, testutil.Recv(t, p.out, "the bookmark").Type)
}

// Every send point owes the same thing: a consumer that stopped draining must end the
// pump rather than park it. A missed one leaks the goroutine and the store watch behind
// it once per client disconnect, and says nothing about it — Frames never closes, so
// there is no Err for anyone to read.
func TestPumpEndsWhereverTheConsumerStopped(t *testing.T) {
	obj := &beehive.Object[testSpec, testStatus]{ID: 1}

	tests := map[string]struct {
		snapshot []*beehive.Object[testSpec, testStatus]
		// reach parks the pump on the send under test, taking every frame before it.
		reach func(t *testing.T, p *parkedPump)
	}{
		"a snapshot frame": {
			snapshot: []*beehive.Object[testSpec, testStatus]{obj},
			reach:    func(*testing.T, *parkedPump) {},
		},
		"the bookmark": {
			reach: func(*testing.T, *parkedPump) {},
		},
		"an ordinary change": {
			reach: func(t *testing.T, p *parkedPump) {
				takeBookmark(t, p)
				p.changes <- beehive.ObjectChange[testSpec, testStatus]{Type: beehive.Modified, ID: obj.ID, Object: obj}
			},
		},
		"a departure": {
			reach: func(t *testing.T, p *parkedPump) {
				takeBookmark(t, p)
				p.changes <- beehive.ObjectChange[testSpec, testStatus]{Type: beehive.Deleted, ID: 1}
			},
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			p := startPump(t, tt.snapshot)
			tt.reach(t, p)

			p.cancel()

			assert.NoError(t, testutil.Recv(t, p.done, "the pump to end"),
				"cancellation is a teardown, not a failure")
		})
	}
}

// A single-object watch whose id holds nothing yet is a snapshot of none, not a
// failure: the record may still arrive, and the bookmark is what says so.
func TestStreamOneBookmarksAnAbsentRecord(t *testing.T) {
	changes := make(chan beehive.ObjectChange[testSpec, testStatus])
	close(changes)

	s := testWatch.streamOne(context.Background(), &beehive.ObjectStream[testSpec, testStatus]{Changes: changes})

	assert.Equal(t, CollectionWatchFrameBookmark, testutil.Recv(t, s.Frames, "the bookmark").Type)
	testutil.WaitClosed(t, s.Frames, "the stream")
	assert.NoError(t, s.Err())
}

// A pump's reads all take ctx, so one that ends because ctx did usually ends with ctx's
// own error. That is a teardown, not a watch failure: filing it would put an error in
// front of a user who has already navigated away, and suppress the transport's backoff
// reset on the reconnect.
func TestAStreamDropsWhateverAPumpEndsWithOnceItsContextIsDone(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	s := NewStream(ctx, func(ctx context.Context, _ chan<- int) error {
		return fmt.Errorf("list cached kinds: %w", ctx.Err())
	})

	testutil.WaitClosed(t, s.Frames, "the pump to end")
	assert.NoError(t, s.Err())
}

// Only a cancellation is dropped. A pump that fails while its consumer is still there
// carries the reason out — that is the whole point of the terminal error.
func TestAStreamKeepsThePumpsErrorWhileTheConsumerIsStillThere(t *testing.T) {
	boom := errors.New("the file will not read")

	s := NewStream(context.Background(), func(context.Context, chan<- int) error { return boom })

	testutil.WaitClosed(t, s.Frames, "the pump to end")
	assert.ErrorIs(t, s.Err(), boom)
}
