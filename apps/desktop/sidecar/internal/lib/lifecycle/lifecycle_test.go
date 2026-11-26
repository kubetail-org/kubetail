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

package lifecycle

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/drain"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/testutil"
)

// stubStartCloser records each phase it goes through into a shared log, and refuses to
// start when startErr is set.
type stubStartCloser struct {
	name     string
	startErr error
	log      *[]string
}

func (s *stubStartCloser) Start(context.Context) (func(context.Context) error, error) {
	if s.startErr != nil {
		return nil, s.startErr
	}
	*s.log = append(*s.log, "start:"+s.name)
	return func(context.Context) error {
		*s.log = append(*s.log, "stop:"+s.name)
		return nil
	}, nil
}

func (s *stubStartCloser) Close() error {
	*s.log = append(*s.log, "close:"+s.name)
	return nil
}

func TestStartAllStartsInOrderAndStopsInReverse(t *testing.T) {
	var log []string
	parts := []Part{
		{Name: "a", StartCloser: &stubStartCloser{name: "a", log: &log}},
		{Name: "b", StartCloser: &stubStartCloser{name: "b", log: &log}},
	}

	stop, err := StartAll(context.Background(), parts)
	require.NoError(t, err)
	require.NoError(t, stop(context.Background()))

	assert.Equal(t, []string{"start:a", "start:b", "stop:b", "stop:a"}, log)
}

// A failed start leaves the caller no stop func, so whatever already runs is
// unreachable — StartAll has to take it down itself, and must not start what comes
// after.
func TestStartAllUnwindsWhenOneFails(t *testing.T) {
	boom := errors.New("boom")
	var log []string
	parts := []Part{
		{Name: "a", StartCloser: &stubStartCloser{name: "a", log: &log}},
		{Name: "b", StartCloser: &stubStartCloser{name: "b", startErr: boom, log: &log}},
		{Name: "c", StartCloser: &stubStartCloser{name: "c", log: &log}},
	}

	stop, err := StartAll(context.Background(), parts)

	assert.ErrorIs(t, err, boom)
	assert.Equal(t, []string{"start:a", "stop:a"}, log)
	// Callable rather than nil, so a deferred stop written above the error check is
	// safe. It stops nothing more: the unwind above it already drained "a".
	require.NotNil(t, stop)
	assert.NoError(t, stop(context.Background()))
	assert.Equal(t, []string{"start:a", "stop:a"}, log)
}

// stopWaiter's stop blocks until release is closed, so a test can tell a stop that
// really drained from one that gave up on a dead context.
type stopWaiter struct {
	entered chan struct{}
	release chan struct{}
}

func (s *stopWaiter) Start(context.Context) (func(context.Context) error, error) {
	return func(ctx context.Context) error {
		close(s.entered)
		return drain.WithContext(ctx, func() { <-s.release })
	}, nil
}

func (s *stopWaiter) Close() error { return nil }

// A start most often fails because the startup context died. Unwinding on that same
// context would signal the already-started parts and report them drained without
// waiting, leaving them running while the caller is free to close what they use.
func TestStartAllUnwindDoesNotInheritADeadContext(t *testing.T) {
	boom := errors.New("boom")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	waiter := &stopWaiter{entered: make(chan struct{}), release: make(chan struct{})}
	var log []string
	parts := []Part{
		{Name: "waiter", StartCloser: waiter},
		{Name: "b", StartCloser: &stubStartCloser{name: "b", startErr: boom, log: &log}},
	}

	errs := make(chan error, 1)
	go func() {
		_, err := StartAll(ctx, parts)
		errs <- err
	}()

	testutil.Wait(t, waiter.entered, "the unwind to reach the started part's stop")
	close(waiter.release)

	err := testutil.Recv(t, errs, "StartAll to return")
	assert.ErrorIs(t, err, boom)
	assert.NotErrorIs(t, err, context.Canceled, "the unwind must wait, not adopt the dead startup context")
}

func TestCloseAllRunsInReverse(t *testing.T) {
	var log []string
	parts := []Part{
		{Name: "a", StartCloser: &stubStartCloser{name: "a", log: &log}},
		{Name: "b", StartCloser: &stubStartCloser{name: "b", log: &log}},
	}

	require.NoError(t, CloseAll(parts))
	assert.Equal(t, []string{"close:b", "close:a"}, log)
}

// Stopping in reverse of registration order is what takes a child's machinery down
// before its owner's.
func TestStopAllRunsInReverse(t *testing.T) {
	var order []int
	stops := make([]func(context.Context) error, 3)
	for i := range stops {
		stops[i] = func(context.Context) error {
			order = append(order, i)
			return nil
		}
	}

	require.NoError(t, stopAll(context.Background(), stops))
	assert.Equal(t, []int{2, 1, 0}, order)
}

// One failing stop must not skip the rest: a controller left running because an
// earlier one errored would outlive the service that owns it.
func TestStopAllRunsEveryStopAndJoinsErrors(t *testing.T) {
	first := errors.New("first")
	last := errors.New("last")

	var ran int
	stops := []func(context.Context) error{
		func(context.Context) error { ran++; return first },
		func(context.Context) error { ran++; return nil },
		func(context.Context) error { ran++; return last },
	}

	err := stopAll(context.Background(), stops)
	assert.Equal(t, 3, ran)
	assert.ErrorIs(t, err, first)
	assert.ErrorIs(t, err, last)
}

func TestStopAllWithNoStops(t *testing.T) {
	assert.NoError(t, stopAll(context.Background(), nil))
}

// None is embedded by anything with no background work of its own, so its stop func
// and Close have to be safe to call like any other.
func TestNoneIsANoOpLifecycle(t *testing.T) {
	var n None

	stop, err := n.Start(context.Background())
	require.NoError(t, err)
	assert.NoError(t, stop(context.Background()))
	assert.NoError(t, n.Close())
}

// StartFunc's only additions are a pass-through Start and the Close its service has
// nothing left to do in.
func TestStartFunc(t *testing.T) {
	stopped := false
	f := StartFunc(func(context.Context) (func(context.Context) error, error) {
		return func(context.Context) error { stopped = true; return nil }, nil
	})

	stop, err := f.Start(context.Background())
	require.NoError(t, err)
	require.NoError(t, stop(context.Background()))

	assert.True(t, stopped)
	assert.NoError(t, f.Close())
}

// CloseFunc's Start is a no-op with a no-op stop, and Close is the func.
func TestCloseFunc(t *testing.T) {
	closed := false
	f := CloseFunc(func() error { closed = true; return nil })

	stop, err := f.Start(context.Background())
	require.NoError(t, err)
	require.NoError(t, stop(context.Background()))

	assert.False(t, closed)
	assert.NoError(t, f.Close())
	assert.True(t, closed)
}

func TestStartFuncReportsAFailedStart(t *testing.T) {
	boom := errors.New("boom")
	f := StartFunc(func(context.Context) (func(context.Context) error, error) {
		return nil, boom
	})

	_, err := f.Start(context.Background())
	assert.ErrorIs(t, err, boom)
}

// A failure reaches the process log as one line, so it has to say which part failed —
// the underlying error is often generic ("already stopped", a wrapped io error).
func TestStartAllNamesThePartThatFailed(t *testing.T) {
	boom := errors.New("boom")
	var log []string
	parts := []Part{
		{Name: "first", StartCloser: &stubStartCloser{name: "a", log: &log}},
		{Name: "second", StartCloser: &stubStartCloser{name: "b", startErr: boom, log: &log}},
	}

	_, err := StartAll(context.Background(), parts)

	assert.ErrorIs(t, err, boom)
	assert.ErrorContains(t, err, "start second")
}

func TestStopAndCloseNameTheirPart(t *testing.T) {
	boom := errors.New("boom")
	parts := []Part{{Name: "only", StartCloser: errorOnStopAndClose{err: boom}}}

	stop, err := StartAll(context.Background(), parts)
	require.NoError(t, err)

	assert.ErrorContains(t, stop(context.Background()), "stop only")
	assert.ErrorContains(t, CloseAll(parts), "close only")
}

// errorOnStopAndClose starts fine and fails everywhere after.
type errorOnStopAndClose struct{ err error }

func (e errorOnStopAndClose) Start(context.Context) (func(context.Context) error, error) {
	return func(context.Context) error { return e.err }, nil
}

func (e errorOnStopAndClose) Close() error { return e.err }
