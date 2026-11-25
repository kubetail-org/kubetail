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
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/drain"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/logging"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/testutil"
)

// recorder is the order the sequence's steps ran in.
type recorder struct {
	mu    sync.Mutex
	steps []string
}

func (r *recorder) add(step string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.steps = append(r.steps, step)
}

func (r *recorder) list() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.steps)
}

// fakeServer records the server's half of the sequence.
type fakeServer struct{ rec *recorder }

func (s fakeServer) Shutdown(context.Context) error { s.rec.add("Shutdown"); return nil }
func (s fakeServer) Close() error                   { s.rec.add("Close server"); return nil }

// fakeApp records the app's half.
type fakeApp struct{ rec *recorder }

func (a fakeApp) CancelStreams() { a.rec.add("CancelStreams") }
func (a fakeApp) CloseRequests() { a.rec.add("CloseRequests") }
func (a fakeApp) WaitRequests()  { a.rec.add("WaitRequests") }
func (a fakeApp) Close() error   { a.rec.add("Close app"); return nil }

// The timers' lengths in a test: distinct, so the clock tells them apart, and never
// reached, since only the test ends one.
const (
	testGrace  = time.Hour
	testBudget = 2 * time.Hour
)

// clock hands the sequence its two timers; the test fires one by closing it.
type clock struct {
	grace, budget chan time.Time
}

func newClock() *clock {
	return &clock{grace: make(chan time.Time), budget: make(chan time.Time)}
}

func (c *clock) after(d time.Duration) <-chan time.Time {
	switch d {
	case testGrace:
		return c.grace
	case testBudget:
		return c.budget
	}
	panic("the sequence asked for a timer the test does not hand out")
}

// gatedApp counts its requests and cancels them on CloseRequests, as the real app
// does, for a test that serves a handler of its own.
type gatedApp struct {
	ctx      context.Context
	cancel   context.CancelFunc
	inflight sync.WaitGroup
}

func newGatedApp() *gatedApp {
	ctx, cancel := context.WithCancel(context.Background())
	return &gatedApp{ctx: ctx, cancel: cancel}
}

func (a *gatedApp) CancelStreams() {}
func (a *gatedApp) CloseRequests() { a.cancel() }
func (a *gatedApp) WaitRequests()  { a.inflight.Wait() }
func (a *gatedApp) Close() error   { return nil }

// serve serves one request that runs block under the app's context, and returns the
// server once block has started.
func (a *gatedApp) serve(t *testing.T, block func(ctx context.Context)) *http.Server {
	t.Helper()
	started := testutil.NewSignal()
	ts := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		a.inflight.Add(1)
		defer a.inflight.Done()
		started.Fire()
		block(a.ctx)
	}))
	t.Cleanup(ts.Close)
	go func() {
		if resp, err := http.Get(ts.URL); err == nil {
			_ = resp.Body.Close()
		}
	}()
	started.Wait(t, "the request to start")
	return ts.Config
}

// runShutdown runs s on its own goroutine, as main's run would, and answers its code.
// Cleanup joins it, so no test leaks the sequence.
func runShutdown(t *testing.T, s *shutdown) <-chan int {
	t.Helper()
	code := make(chan int, 1)
	done := make(chan struct{})
	go func() {
		defer close(done)
		code <- s.run()
	}()
	t.Cleanup(func() { testutil.Wait(t, done, "the sequence to return") })
	return code
}

// fixture is a sequence over fakes, with its clock, its exits and its log.
type fixture struct {
	s   *shutdown
	rec *recorder
	clk *clock
	// exited records each exit the sequence asks for; the seam returns, so the
	// sequence runs on after a wedge.
	exited chan int
	log    *testutil.LogCapture
}

// newFixture builds a sequence whose steps all return at once and whose log is the
// production logger's, so a test overrides only the part it is about.
func newFixture() *fixture {
	f := &fixture{rec: &recorder{}, clk: newClock(), exited: make(chan int, 2), log: &testutil.LogCapture{}}
	f.s = &shutdown{
		srv:    fakeServer{f.rec},
		app:    fakeApp{f.rec},
		stop:   func(context.Context) error { f.rec.add("stop"); return nil },
		grace:  testGrace,
		budget: testBudget,
		after:  f.clk.after,
		log:    logging.Init(f.log, slog.LevelInfo),
		exit:   func(code int) { f.exited <- code },
	}
	// Logged, so a test can place it against the dump.
	f.s.wedged = func() { f.s.log.Info("wedged") }
	return f
}

// loggedSteps answers the step each of the log's records at level names.
func (f *fixture) loggedSteps(t *testing.T, level string) []string {
	t.Helper()
	var steps []string
	for _, r := range records(t, f.log.String()) {
		if step, ok := r["step"].(string); ok && r["level"] == level {
			steps = append(steps, step)
		}
	}
	return steps
}

func (f *fixture) noExit(t *testing.T) {
	t.Helper()
	if len(f.exited) != 0 {
		t.Errorf("exit(%d) called on a stop that finished", <-f.exited)
	}
}

// With every step returning at once, the steps run in order, each logs its
// duration, and the code is 0. The streams are cancelled once Shutdown returns,
// whether or not its hook ran. The server's Close runs only when the grace ran out.
// run joins the watchdog, so no exit can follow its return.
func TestShutdownRunsTheStepsInOrder(t *testing.T) {
	f := newFixture()

	if code := testutil.Recv(t, runShutdown(t, f.s), "the sequence's code"); code != 0 {
		t.Errorf("code = %d, want 0", code)
	}
	want := []string{"Shutdown", "CancelStreams", "CloseRequests", "WaitRequests", "stop", "Close app"}
	if got := f.rec.list(); !slices.Equal(got, want) {
		t.Errorf("steps = %v, want %v", got, want)
	}
	f.noExit(t)

	// After a full grace the rest share what is left of the budget; these records
	// are what show a slow step before it trips the watchdog.
	wantLogged := []string{"drain requests", "cancel requests", "wait requests", "stop services", "close services"}
	if got := f.loggedSteps(t, "INFO"); !slices.Equal(got, wantLogged) {
		t.Errorf("duration records = %v, want %v", got, wantLogged)
	}
}

// A step that fails is logged at WARN with its name, and changes no code: the
// sequence goes on to Close.
func TestAFailingStepIsLoggedAndTheCodeIsZero(t *testing.T) {
	f := newFixture()
	f.s.stop = func(context.Context) error { return errors.New("a part failed to stop") }

	if code := testutil.Recv(t, runShutdown(t, f.s), "the sequence's code"); code != 0 {
		t.Errorf("code = %d, want 0", code)
	}
	if got := f.loggedSteps(t, "WARN"); !slices.Equal(got, []string{"stop services"}) {
		t.Errorf("WARN records name %v, want [stop services]", got)
	}
	if got := f.rec.list(); got[len(got)-1] != "Close app" {
		t.Errorf("steps = %v, want the sequence to run on to Close", got)
	}
}

// A request still running when the grace runs out is cancelled and joined, and the
// rest of the sequence runs: a request that honours its context is not a wedge.
func TestTheGraceEndsABlockedRequest(t *testing.T) {
	f := newFixture()
	app := newGatedApp()
	f.s.app = app
	f.s.srv = app.serve(t, func(ctx context.Context) { <-ctx.Done() })
	code := runShutdown(t, f.s)

	close(f.clk.grace)
	if got := testutil.Recv(t, code, "the sequence's code"); got != 0 {
		t.Errorf("code = %d, want 0", got)
	}
	f.noExit(t)
	if got := f.loggedSteps(t, "WARN"); !slices.Equal(got, []string{"drain requests"}) {
		t.Errorf("WARN records name %v, want [drain requests]: the grace ran out", got)
	}
}

// wedgedStop is a stop func that drains with drain.WithContext over a goroutine
// blocked until release fires: the real cancel-and-wait, not a stub that blocks
// whatever its context. It returns once the goroutine is in wedgedLoop. Cleanup
// fires release if the test has not.
func wedgedStop(t *testing.T) (stop func(context.Context) error, entered, release *testutil.Signal) {
	t.Helper()
	release = testutil.NewSignal()
	t.Cleanup(func() { release.Fire() })
	entered = testutil.NewSignal()
	started := testutil.NewSignal()
	var wg sync.WaitGroup
	wg.Go(func() { deepen(40, started, release.Chan()) })
	started.Wait(t, "the wedged goroutine to reach wedgedLoop")
	stop = func(ctx context.Context) error {
		entered.Fire()
		return drain.WithContext(ctx, wg.Wait)
	}
	return stop, entered, release
}

// deepen blocks in wedgedLoop under depth frames, so the dump runs past one
// record's 2 KiB cut.
func deepen(depth int, started *testutil.Signal, release <-chan struct{}) {
	if depth == 0 {
		wedgedLoop(started, release)
		return
	}
	deepen(depth-1, started, release)
}

// wedgedLoop is the frame the dump must name first. It marks itself started, since
// a goroutine still descending through deepen has no wedgedLoop frame to dump.
//
//go:noinline
func wedgedLoop(started *testutil.Signal, release <-chan struct{}) {
	started.Fire()
	<-release
}

// A loop that ignores its cancellation: the drain never returns, so the watchdog
// ends the process at the budget with 1. run cannot return while the drain blocks,
// so the test releases it before reading the code.
func TestTheWatchdogExitsOnAWedgedDrain(t *testing.T) {
	f := newFixture()
	stop, entered, release := wedgedStop(t)
	f.s.stop = stop
	code := runShutdown(t, f.s)
	entered.Wait(t, "the drain to start")

	close(f.clk.budget)
	if got := testutil.Recv(t, f.exited, "the watchdog's exit"); got != 1 {
		t.Errorf("exit(%d), want exit(1)", got)
	}

	release.Fire()
	if got := testutil.Recv(t, code, "the sequence's code"); got != 1 {
		t.Errorf("code = %d, want 1: the watchdog won", got)
	}
}

// records decodes every JSON record the logger wrote.
func records(t *testing.T, raw string) []map[string]any {
	t.Helper()
	var out []map[string]any
	sc := bufio.NewScanner(strings.NewReader(raw))
	sc.Buffer(nil, 1<<20)
	for sc.Scan() {
		var rec map[string]any
		if err := json.Unmarshal(sc.Bytes(), &rec); err != nil {
			t.Fatalf("decode %q: %v", sc.Text(), err)
		}
		out = append(out, rec)
	}
	return out
}

// The dump survives the production logger: its redacting handler cuts every string
// at 2 KiB, so the dump is one record per goroutine after a record that counts
// them, and the wedged goroutine's record names its blocked frame first. Redaction
// is in the path too: a frame it altered would fail the first-frame check. The
// wedged seam runs before the dump: the dump can outlast the host's kill.
func TestTheDumpIsOneRecordPerGoroutine(t *testing.T) {
	f := newFixture()
	stop, entered, release := wedgedStop(t)
	f.s.stop = stop
	code := runShutdown(t, f.s)
	entered.Wait(t, "the drain to start")
	close(f.clk.budget)
	testutil.Recv(t, f.exited, "the watchdog's exit")
	release.Fire()
	testutil.Recv(t, code, "the sequence's code")

	var count float64
	var stacks []string
	wedged := false
	for _, r := range records(t, f.log.String()) {
		if r["message"] == "wedged" {
			wedged = true
			continue
		}
		if r["message"] != "shutdown wedged" {
			continue
		}
		if !wedged {
			t.Fatal("a dump record precedes the wedged seam")
		}
		if n, ok := r["goroutines"].(float64); ok {
			count = n
			continue
		}
		stacks = append(stacks, r["stack"].(string))
	}
	if int(count) != len(stacks) || count == 0 {
		t.Fatalf("count record says %v goroutines, %d stack records follow", count, len(stacks))
	}
	if total := len(strings.Join(stacks, "")); total <= 2048 {
		t.Fatalf("the dump is %d bytes; the test needs one past the 2 KiB cut", total)
	}
	// The package is main, but a test binary names its functions by import path.
	named := 0
	for _, st := range stacks {
		first, _, _ := strings.Cut(st, "\n")
		if strings.HasPrefix(first, "github.com/kubetail-org/kubetail/apps/desktop/sidecar.wedgedLoop(") {
			named++
		}
	}
	if named != 1 {
		t.Errorf("%d stack records start at wedgedLoop, want 1", named)
	}
}

// parked is the frame of each goroutine TestTheDumpOutgrowsItsFirstBuffer parks.
// It marks itself started, since a goroutine not yet scheduled has no frame of its
// own in a stack dump.
func parked(started *sync.WaitGroup, park <-chan struct{}) {
	started.Done()
	<-park
}

// Stacks past the dump's first 64 KiB buffer are read whole: every parked goroutine
// gets its record.
func TestTheDumpOutgrowsItsFirstBuffer(t *testing.T) {
	const n = 1000 // a few hundred bytes of stack each, several times 64 KiB
	park := make(chan struct{})
	t.Cleanup(func() { close(park) })
	var started sync.WaitGroup
	started.Add(n)
	for range n {
		go parked(&started, park)
	}
	started.Wait()
	f := newFixture()
	f.s.dump()

	got := 0
	for _, r := range records(t, f.log.String()) {
		if st, ok := r["stack"].(string); ok && strings.HasPrefix(st, "github.com/kubetail-org/kubetail/apps/desktop/sidecar.parked(") {
			got++
		}
	}
	if got != n {
		t.Errorf("%d stack records start at parked, want %d", got, n)
	}
}

// A handler that does not return when cancelled: the grace runs out, the join
// blocks, and the watchdog ends the process with 1.
func TestTheWatchdogExitsOnAHandlerThatIgnoresItsContext(t *testing.T) {
	f := newFixture()
	app := newGatedApp()
	cancelled := testutil.NewSignal()
	release := testutil.NewSignal()
	f.s.app = app
	f.s.srv = app.serve(t, func(ctx context.Context) {
		<-ctx.Done()
		cancelled.Fire()
		<-release.Chan()
	})
	// After serve, so it runs before the server's Close, which waits on the handler.
	t.Cleanup(func() { release.Fire() })
	code := runShutdown(t, f.s)

	close(f.clk.grace)
	cancelled.Wait(t, "CloseRequests to cancel the handler")
	// A negative assertion: once the handler is cancelled, nothing but its return
	// lets the sequence on, so a sequence that skipped the join would return here.
	testutil.NoRecv(t, code, 50*time.Millisecond, "the sequence's code while a handler runs")

	close(f.clk.budget)
	if got := testutil.Recv(t, f.exited, "the watchdog's exit"); got != 1 {
		t.Errorf("exit(%d), want exit(1)", got)
	}

	release.Fire()
	if got := testutil.Recv(t, code, "the sequence's code"); got != 1 {
		t.Errorf("code = %d, want 1: the watchdog won", got)
	}
}

// A sequence that finishes after the watchdog fired waits for the watchdog's dump
// and exit before it returns: run's caller would close the log and exit, cutting
// the dump off. The exit seam holds the watchdog in place of the sink, so the
// sequence's own step records are not held up behind the dump.
func TestALosingSequenceWaitsForTheWatchdog(t *testing.T) {
	f := newFixture()
	exiting := testutil.NewSignal()
	letExit := testutil.NewSignal()
	f.s.exit = func(int) {
		exiting.Fire()
		<-letExit.Chan()
	}
	stop, entered, release := wedgedStop(t)
	f.s.stop = stop
	code := runShutdown(t, f.s)
	// After runShutdown, so it runs before the cleanup that joins the sequence.
	t.Cleanup(func() { letExit.Fire() })
	entered.Wait(t, "the drain to start")

	close(f.clk.budget)
	exiting.Wait(t, "the watchdog to reach its exit")
	release.Fire()
	// A negative assertion: the sequence has nothing left to do but return, so one
	// that did not wait for the watchdog would return within the window.
	testutil.NoRecv(t, code, 50*time.Millisecond, "the losing sequence's code before the watchdog's exit returned")

	letExit.Fire()
	if got := testutil.Recv(t, code, "the sequence's code"); got != 1 {
		t.Errorf("code = %d, want 1", got)
	}
}
