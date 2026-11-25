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

// Package testutil holds test helpers shared across the sidecar's packages:
// a one-shot Signal for fakes to notify tests, and waiters that fail the test
// instead of blocking forever. Nothing in production imports it.
package testutil

import (
	"os"
	"testing"
	"time"

	"github.com/amorey/gochan/oneshot"
)

// Timeout is a failsafe only: a wait this long has hung, so we fail rather than
// block until the test binary's own deadline. Tests never rely on it to pace
// anything. Generous on purpose — it is paid only by a run that was going to
// fail anyway, and a bound tight enough to be reached by mere load (a Windows
// -cpu 1 stress job reached 10s) would report hangs that never happened.
const Timeout = 30 * time.Second

// Signal is a one-shot notification from a test fake to the test: a callback
// that may run many times calls Fire, and the test awaits it with Wait. Firing
// is idempotent by contract — oneshot reports a second Send as ErrClosed, where
// a second close of a channel would panic — so a fake needs no sync.Once and no
// select/default guard beside its Signal.
type Signal struct {
	tx *oneshot.Sender[struct{}]
	rx *oneshot.Receiver[struct{}]
}

func NewSignal() *Signal {
	tx, rx := oneshot.New[struct{}]()
	return &Signal{tx: tx, rx: rx}
}

// Fire signals, whether or not the test is waiting yet: the value is held in
// the slot until Wait takes it. Repeat calls are no-ops, and the returned bool
// says which call this was — true only for the one that signalled, so a
// callback with first-time-only work can gate it on that instead of on a
// sync.Once of its own.
func (s *Signal) Fire() bool { return s.tx.Send(struct{}{}) == nil }

// Chan reports the signal as a channel, for a test that has to select on it
// alongside something else. It yields once and is then closed, so a second
// receive returns immediately rather than blocking.
func (s *Signal) Chan() <-chan struct{} { return s.rx.Chan() }

// Wait blocks until Fire has run, failing the test after the failsafe timeout.
func (s *Signal) Wait(t testing.TB, what string) {
	t.Helper()
	Wait(t, s.Chan(), what)
}

// Wait blocks until ch delivers or closes, failing the test after the failsafe
// timeout. For the done/ready channels that carry no value.
func Wait(t testing.TB, ch <-chan struct{}, what string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(Timeout): // coverage:ignore the failsafe arm: reaching it waits out Timeout
		t.Fatalf("timed out waiting for %s", what)
	}
}

// WaitReturn runs fn on its own goroutine and fails the test if it has not
// returned within the failsafe timeout. For the blocking half of a lifecycle — a
// stop func or a WaitGroup join — where the claim is that it returns at all.
func WaitReturn(t testing.TB, fn func(), what string) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		defer close(done)
		fn()
	}()
	Wait(t, done, what)
}

// Recv returns the next value from ch, failing the test if ch closes first or
// nothing arrives within the failsafe timeout.
func Recv[T any](t testing.TB, ch <-chan T, what string) T {
	t.Helper()
	select {
	case v, ok := <-ch:
		if !ok {
			t.Fatalf("channel closed while waiting for %s", what)
		}
		return v
	case <-time.After(Timeout): // coverage:ignore the failsafe arm: reaching it waits out Timeout
		t.Fatalf("timed out waiting for %s", what)
		var zero T
		return zero
	}
}

// Current is a state watch's first value: what it holds now. It stops the watch
// before returning.
func Current[T any](t testing.TB, watch func() (<-chan T, func()), what string) T {
	t.Helper()
	ch, stop := watch()
	defer stop()
	return Recv(t, ch, what)
}

// NoRecv asserts that nothing arrives on ch within window, and fails naming what did.
//
// A negative assertion has no event to wait for, so it needs a bound of its own rather
// than Timeout — and it fails the instant the thing happens rather than at the end of the
// wait. Size window off whatever cadence the code under test runs at, never as a guess.
func NoRecv[T any](t testing.TB, ch <-chan T, window time.Duration, what string) {
	t.Helper()
	select {
	case v := <-ch:
		t.Fatalf("%s: received %v", what, v)
	case <-time.After(window):
	}
}

// RecvClosed asserts that the next receive on ch is a close, failing the test
// if a value arrives first or nothing happens within the failsafe timeout. Use
// it where "the stream ended here, with nothing after it" is the claim;
// WaitClosed is the tolerant version.
func RecvClosed[T any](t testing.TB, ch <-chan T, what string) {
	t.Helper()
	select {
	case v, ok := <-ch:
		if ok {
			t.Fatalf("%s delivered %v; want a close", what, v)
		}
	case <-time.After(Timeout): // coverage:ignore the failsafe arm: reaching it waits out Timeout
		t.Fatalf("timed out waiting for %s to close", what)
	}
}

// WaitClosed blocks until ch is closed, discarding whatever it still holds —
// how a test waits for a stream to end without caring what was in flight.
func WaitClosed[T any](t testing.TB, ch <-chan T, what string) {
	t.Helper()
	deadline := time.After(Timeout)
	for {
		select {
		case _, ok := <-ch:
			if !ok {
				return
			}
		case <-deadline: // coverage:ignore the failsafe arm: reaching it waits out Timeout
			t.Fatalf("timed out waiting for %s to close", what)
		}
	}
}

// RequireSandbox is for a test that needs a confining sandbox on a machine
// without one: it fails t under KUBETAIL_REQUIRE_SANDBOX=1, which CI sets, and
// skips it otherwise, saying why, so such a machine still runs the rest.
func RequireSandbox(t testing.TB, why string) {
	t.Helper()
	if os.Getenv("KUBETAIL_REQUIRE_SANDBOX") == "1" {
		t.Fatal(why)
	}
	t.Skip(why)
}
