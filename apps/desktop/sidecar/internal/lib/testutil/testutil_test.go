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

package testutil

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// The property the helper exists for: a fake whose callback runs many times can
// call Fire unguarded. Only the first call signals, and none of them panic.
func TestSignalFiresOnce(t *testing.T) {
	s := NewSignal()
	assert.True(t, s.Fire(), "the first Fire is the one that signals")
	assert.False(t, s.Fire())
	assert.False(t, s.Fire())
	s.Wait(t, "the signal")
}

// Fire before the test is waiting must still be observed: the value sits in the
// slot until Wait takes it, so there is no ordering requirement on a fake.
func TestSignalFireBeforeWait(t *testing.T) {
	s := NewSignal()
	s.Fire()
	s.Wait(t, "a signal fired before the wait")
	s.Wait(t, "a second wait, answered by the closed channel")
}

func TestSignalWaitsForALaterFire(t *testing.T) {
	s := NewSignal()
	go s.Fire()
	s.Wait(t, "a signal fired from another goroutine")
}

func TestWaitAcceptsCloseAndValue(t *testing.T) {
	closed := make(chan struct{})
	close(closed)
	Wait(t, closed, "a closed channel")

	sent := make(chan struct{}, 1)
	sent <- struct{}{}
	Wait(t, sent, "a delivered value")
}

func TestRecvReturnsTheValue(t *testing.T) {
	ch := make(chan int, 1)
	ch <- 42
	assert.Equal(t, 42, Recv(t, ch, "an int"))
}

func TestCurrentReadsTheFirstValueAndStops(t *testing.T) {
	stopped := false
	watch := func() (<-chan int, func()) {
		ch := make(chan int, 1)
		ch <- 7
		return ch, func() { stopped = true }
	}
	assert.Equal(t, 7, Current(t, watch, "the value"))
	assert.True(t, stopped)
}

func TestRecvClosedAcceptsAClosedChannel(t *testing.T) {
	ch := make(chan int)
	close(ch)
	RecvClosed(t, ch, "a closed channel")
}

// WaitClosed discards what is still in flight — a stream that closes with rows
// buffered has still ended.
func TestWaitClosedDrains(t *testing.T) {
	ch := make(chan int, 2)
	ch <- 1
	ch <- 2
	close(ch)
	WaitClosed(t, ch, "a stream")
}

// fakeTB stands in for *testing.T so a helper's failure arm can be asserted
// instead of failing the run. Embedding testing.TB (nil) satisfies the
// interface's unexported method; only what the helpers call is implemented, so
// anything else is a nil-call panic naming it.
type fakeTB struct {
	testing.TB
	failed string
}

func (f *fakeTB) Helper() {}

// Fatalf records instead of aborting: without runtime.Goexit the helper runs on
// to its return, which is exactly the path under test.
func (f *fakeTB) Fatalf(format string, args ...any) {
	f.failed = fmt.Sprintf(format, args...)
}

func (f *fakeTB) Fatal(args ...any) { f.failed = fmt.Sprint(args...) }

func (f *fakeTB) Skip(...any) {}

// Every helper's promise is the failure, not the happy path — a helper that
// quietly accepted the wrong outcome would pass tests that should fail. Each
// case here fails on the spot, with no failsafe timeout to wait out.
func TestHelpersFailOnTheWrongOutcome(t *testing.T) {
	closed := make(chan int)
	close(closed)

	t.Run("Recv on a closed channel", func(t *testing.T) {
		f := &fakeTB{}
		assert.Zero(t, Recv(f, closed, "a value"))
		assert.Contains(t, f.failed, "closed while waiting")
	})

	t.Run("NoRecv on a delivered value", func(t *testing.T) {
		ch := make(chan int, 1)
		ch <- 7
		f := &fakeTB{}
		NoRecv(f, ch, time.Millisecond, "nothing")
		assert.Contains(t, f.failed, "received 7")
	})

	t.Run("RecvClosed on a delivered value", func(t *testing.T) {
		ch := make(chan int, 1)
		ch <- 7
		f := &fakeTB{}
		RecvClosed(f, ch, "a close")
		assert.Contains(t, f.failed, "delivered 7")
	})
}

// Without KUBETAIL_REQUIRE_SANDBOX a test without its sandbox skips.
func TestRequireSandboxSkipsUnlessRequired(t *testing.T) {
	t.Setenv("KUBETAIL_REQUIRE_SANDBOX", "")
	var sub *testing.T
	t.Run("needs a sandbox", func(t *testing.T) {
		sub = t
		RequireSandbox(t, "no sandbox")
	})
	assert.True(t, sub.Skipped())
}

// CI sets KUBETAIL_REQUIRE_SANDBOX, and there a test without its sandbox fails rather than skips.
func TestRequireSandboxFailsWhenRequired(t *testing.T) {
	t.Setenv("KUBETAIL_REQUIRE_SANDBOX", "1")
	f := &fakeTB{}

	RequireSandbox(f, "no sandbox")

	assert.Equal(t, "no sandbox", f.failed)
}
