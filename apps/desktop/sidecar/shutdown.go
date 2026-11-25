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

package main

import (
	"context"
	"log/slog"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// requestGrace is how long a request in flight may hold up the stop: a person in
// another terminal may be waiting on it once the sidecar is shared.
const requestGrace = 10 * time.Second

// shutdownBudget bounds the whole stop. After a request has held up the stop for
// the whole grace, stopping the services and closing still get 10s. The host's
// SIDECAR_SHUTDOWN_GRACE (src-tauri/src/lib.rs) exceeds it by the time a goroutine
// dump and an exit take.
const shutdownBudget = requestGrace + 10*time.Second

// server is the http.Server's half of the sequence.
type server interface {
	Shutdown(context.Context) error
	Close() error
}

// application is the app's half: *app.App in production.
type application interface {
	CancelStreams()
	CloseRequests()
	WaitRequests()
	Close() error
}

// shutdown is the stop sequence. The timer, the log and the exit are seams a
// test replaces; newShutdown fills them for production.
type shutdown struct {
	srv  server
	app  application
	stop func(context.Context) error

	grace, budget time.Duration
	after         func(time.Duration) <-chan time.Time
	log           *slog.Logger
	// wedged runs when the watchdog fires, before the dump: production removes
	// the socket file there, since the dump can outlast the host's kill.
	wedged func()
	exit   func(int)

	// settled is claimed once, by the sequence completing or the watchdog firing;
	// the side that finds it claimed does nothing.
	settled atomic.Bool
}

func newShutdown(srv server, app application, stop func(context.Context) error, wedged func(), exit func(int)) *shutdown {
	return &shutdown{
		srv:    srv,
		app:    app,
		stop:   stop,
		grace:  requestGrace,
		budget: shutdownBudget,
		after:  time.After,
		log:    slog.Default(),
		wedged: wedged,
		exit:   exit,
	}
}

// run runs the sequence and returns the exit code.
func (s *shutdown) run() int {
	// Joined last: a sequence the watchdog beat returns only once the dump is
	// logged, or the caller would close the log under it.
	var watchdog sync.WaitGroup
	defer watchdog.Wait()
	done := make(chan struct{})
	defer close(done)

	// The only bound on the steps below: each is a cancel-and-wait, and one whose
	// work ignores its cancellation is a wedge.
	watchdog.Go(func() {
		select {
		case <-s.after(s.budget):
		case <-done:
			return
		}
		if s.settled.CompareAndSwap(false, true) {
			s.wedged()
			s.dump()
			s.exit(1)
		}
	})

	graceCtx, endGrace := context.WithCancel(context.Background())
	defer endGrace()
	go func() {
		select {
		case <-s.after(s.grace):
			endGrace()
		case <-done:
		}
	}()

	// Order matters: Shutdown stops listening and then ends the streams, handlers
	// are joined before stop stops the services under them, and loops before Close.
	// See docs/adr/2026-10-26-single-socket-h2c-from-the-standard-library.md.
	graceErr := s.step("drain requests", func() error { return s.srv.Shutdown(graceCtx) })
	// Shutdown runs its hooks on their own goroutines and does not wait for them,
	// so with no connection open it can return before CancelStreams has run.
	s.app.CancelStreams()
	s.step("cancel requests", func() error { s.app.CloseRequests(); return nil })
	if graceErr != nil {
		// Cut the connections too, so a handler blocked on a write fails.
		s.step("close connections", s.srv.Close)
	}
	s.step("wait requests", func() error { s.app.WaitRequests(); return nil })
	s.step("stop services", func() error { return s.stop(context.Background()) })
	s.step("close services", s.app.Close)
	if !s.settled.CompareAndSwap(false, true) {
		return 1
	}
	return 0
}

// step runs one step of the sequence and logs how long it took, and its error.
// No error changes the exit code.
func (s *shutdown) step(name string, run func() error) error {
	start := time.Now()
	err := run()
	s.log.Info("shutdown step", "step", name, "took", time.Since(start).String())
	if err != nil {
		s.log.Warn("shutdown step failed", "step", name, "err", err)
	}
	return err
}

// dump logs every goroutine's stack, one record each: the logger cuts every string
// at 2 KiB, and a goroutine's blocked frame is at the top of its record. A record
// before them counts them, so a reader knows the dump is whole.
func (s *shutdown) dump() {
	buf := make([]byte, 64<<10)
	for {
		n := runtime.Stack(buf, true)
		if n < len(buf) {
			buf = buf[:n]
			break
		}
		buf = make([]byte, 2*len(buf))
	}
	stacks := strings.Split(strings.TrimSpace(string(buf)), "\n\n")
	s.log.Error("shutdown wedged", "budget", s.budget.String(), "goroutines", len(stacks))
	for _, stack := range stacks {
		header, frames, _ := strings.Cut(stack, "\n")
		s.log.Error("shutdown wedged", "goroutine", header, "stack", frames)
	}
}
