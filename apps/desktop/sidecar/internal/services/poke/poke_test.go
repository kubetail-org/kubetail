package poke

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/testutil"
)

var epoch = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

// mustRecv reads exactly one Signal from ch within 2s or fails the test.
func mustRecv(t *testing.T, ch <-chan Notification) Notification {
	t.Helper()
	return testutil.Recv(t, ch, "a Signal")
}

// mustNotRecv asserts no Signal arrives within 50ms.
func mustNotRecv(t *testing.T, ch <-chan Notification) {
	t.Helper()
	select {
	case s, ok := <-ch:
		if !ok {
			t.Fatal("channel closed unexpectedly")
		}
		t.Fatalf("expected no Signal, got %+v", s)
	case <-time.After(50 * time.Millisecond):
	}
}

// manualTicker returns a ticker backed by the given channel (injected by tests).
func manualTicker(ch chan time.Time) func(time.Duration) (<-chan time.Time, func()) {
	return func(time.Duration) (<-chan time.Time, func()) {
		return ch, func() {}
	}
}

// A1: a tick arriving within the gap threshold emits no Signal.
func TestWallClock_SmallGap_NoPoke(t *testing.T) {
	tickCh := make(chan time.Time, 1)
	// Tick=10s, GapFactor=2.0 → threshold=20s; Now always returns epoch → gap=0 → no poke.
	b := newWithOptions(
		withTick(10*time.Second),
		withGapFactor(2.0),
		withNow(func() time.Time { return epoch }),
		withTicker(manualTicker(tickCh)),
	)

	ch, cancel := b.ListenForNotifications()
	defer cancel()

	ctx := t.Context()
	go b.Run(ctx)

	tickCh <- time.Time{}
	mustNotRecv(t, ch)
}

// A2: a tick arriving past the gap threshold emits exactly one SourceWallClock Signal.
func TestWallClock_LargeGap_Poke(t *testing.T) {
	tickCh := make(chan time.Time, 1)
	// Tick=10s, GapFactor=2.0 → threshold=20s; gap=30s → poke.
	var callN atomic.Int64
	b := newWithOptions(
		withTick(10*time.Second),
		withGapFactor(2.0),
		withNow(func() time.Time {
			if callN.Add(1) == 1 {
				return epoch // first call: seed lastSeen
			}
			return epoch.Add(30 * time.Second) // subsequent: 30s gap
		}),
		withTicker(manualTicker(tickCh)),
	)

	ch, cancel := b.ListenForNotifications()
	defer cancel()

	ctx := t.Context()
	go b.Run(ctx)

	tickCh <- time.Time{}
	s := mustRecv(t, ch)
	assert.Equal(t, SourceWallClock, s.Source)
	mustNotRecv(t, ch) // exactly one poke
}

// A3: Poke fans a Signal out to every active subscriber.
func TestPoke_FanOut(t *testing.T) {
	b := newWithOptions(withNow(func() time.Time { return epoch }))

	ch1, cancel1 := b.ListenForNotifications()
	defer cancel1()
	ch2, cancel2 := b.ListenForNotifications()
	defer cancel2()

	b.Poke(SourceHost)

	s1 := mustRecv(t, ch1)
	s2 := mustRecv(t, ch2)
	assert.Equal(t, SourceHost, s1.Source)
	assert.Equal(t, SourceHost, s2.Source)
	assert.Equal(t, epoch, s1.At)
	assert.Equal(t, epoch, s2.At)
}

// A4: cancelling Run's context closes all subscriber channels.
func TestRun_CtxCancel_ClosesSubscribers(t *testing.T) {
	b := newWithOptions(withNow(func() time.Time { return epoch }))

	ch, cancel := b.ListenForNotifications()
	defer cancel()

	ctx, cancelCtx := context.WithCancel(context.Background())
	go b.Run(ctx)

	cancelCtx()

	testutil.RecvClosed(t, ch, "the subscriber channel")
}

// A5: Start's ctx bounds startup alone, so cancelling it must leave the detector
// running. Without that, an owner timing out startup would silently lose the bus for
// every subscriber; the stop func is the only teardown.
func TestStart_StartupCtxCancel_KeepsDetectorRunning(t *testing.T) {
	b := newWithOptions(withNow(func() time.Time { return epoch }))

	ch, cancelSub := b.ListenForNotifications()
	defer cancelSub()

	ctx, cancelCtx := context.WithCancel(context.Background())
	stop, err := b.Start(ctx)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	cancelCtx()

	// Run exiting on that cancel would close the hub, which lands here as a closed
	// channel rather than as silence.
	mustNotRecv(t, ch)

	b.Poke(SourceHost)
	assert.Equal(t, SourceHost, mustRecv(t, ch).Source)

	if err := stop(context.Background()); err != nil {
		t.Fatalf("stop: %v", err)
	}
	testutil.RecvClosed(t, ch, "the subscriber channel after stop")
}

// stop is bounded by its own context: a detector slow to exit answers that
// context's error, and a later stop still waits it out.
func TestStart_StopGivesUpOnItsContext(t *testing.T) {
	release := make(chan struct{})
	// The ticker's stop runs as Run exits, so holding it holds the detector.
	b := newWithOptions(withNow(func() time.Time { return epoch }), withTicker(func(time.Duration) (<-chan time.Time, func()) {
		return nil, func() { <-release }
	}))
	stop, err := b.Start(context.Background())
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	assert.ErrorIs(t, stop(ctx), context.Canceled)

	close(release)
	assert.NoError(t, stop(context.Background()))
}
