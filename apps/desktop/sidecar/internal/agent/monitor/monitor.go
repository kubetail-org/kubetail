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

// Package monitor reads each integration's targets on its cadence, folds the
// signals it finds into incidents and occurrences, and stores them in app.db.
// The core knows no integration: each is a Registration, and imports this
// package for its types.
package monitor

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/amorey/gobus"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/appdb"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/drain"
)

// reconcileEvery is the backstop under each integration's change notices, and
// the cadence of the retention sweep.
const reconcileEvery = 5 * time.Minute

// Monitor is the detection loop over every registered integration.
type Monitor struct {
	store *store
	regs  []*registration
	// byName is regs by integration name.
	byName map[string]*registration
	clock  func() time.Time
	// backstop is reconcileEvery, or less in a test.
	backstop time.Duration
}

// registration is a Registration and the names of its signals.
type registration struct {
	Registration
	names map[string]bool
}

// New is the monitor over db and one registration per integration. It refuses a
// signal whose name lacks its integration's prefix and a second registration of
// one name.
func New(db *appdb.DB, regs ...Registration) (*Monitor, error) {
	return newMonitor(db, wallClock, reconcileEvery, regs...)
}

// wallClock is the time with no monotonic reading, which does not advance while
// the machine sleeps, at the millisecond app.db stores.
func wallClock() time.Time { return time.Now().Round(0).Truncate(time.Millisecond) }

func newMonitor(db *appdb.DB, clock func() time.Time, backstop time.Duration, regs ...Registration) (*Monitor, error) {
	m := &Monitor{byName: map[string]*registration{}, clock: clock, backstop: backstop}
	for _, r := range regs {
		if _, dup := m.byName[r.Name]; dup {
			return nil, fmt.Errorf("monitor: integration %q is registered twice", r.Name)
		}
		if r.Cadence <= 0 || r.Integration == nil {
			return nil, fmt.Errorf("monitor: integration %q needs a cadence and an integration", r.Name)
		}
		reg := &registration{Registration: r, names: map[string]bool{}}
		for _, s := range r.Signals {
			if !strings.HasPrefix(s.Name, r.Name+".") {
				return nil, fmt.Errorf("monitor: signal %q lacks its integration's prefix %q", s.Name, r.Name+".")
			}
			if reg.names[s.Name] {
				return nil, fmt.Errorf("monitor: signal %q is registered twice", s.Name)
			}
			reg.names[s.Name] = true
		}
		m.regs = append(m.regs, reg)
		m.byName[r.Name] = reg
	}
	st, err := openStore(db, clock)
	if err != nil {
		return nil, err
	}
	m.store = st
	return m, nil
}

// Start runs the loop until the stop func it returns.
func (m *Monitor) Start(context.Context) (func(context.Context) error, error) {
	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	// Listen before the first read of the targets, so a change between the two
	// is not missed.
	targets := m.store.db.ListenForChanges(appdb.KeyMonitorTargets)
	wg.Go(func() {
		defer targets.Close()
		m.supervise(ctx, targets.Chan())
	})
	for _, reg := range m.regs {
		wg.Go(func() { m.reconcileLoop(ctx, reg) })
	}
	return func(ctx context.Context) error {
		cancel()
		return drain.WithContext(ctx, wg.Wait)
	}, nil
}

// Close releases the monitor's statements.
func (m *Monitor) Close() error { return m.store.close() }

// SetEnabled turns a target's monitoring on or off, in its own transaction.
// ErrNoTarget for no such target.
func (m *Monitor) SetEnabled(ctx context.Context, targetID string, enabled bool) error {
	return m.store.setEnabled(ctx, targetID, enabled)
}

// TargetID is the id of an integration's target by its key; ErrNoTarget while
// the reconcile has given the key none.
func (m *Monitor) TargetID(ctx context.Context, integration, key string) (string, error) {
	return m.store.targetID(ctx, integration, key)
}

// reconcileLoop keeps reg's target rows its listing: at start, on each notice,
// on the backstop, and one cadence after a listing fails. It listens before its
// first listing, so a change between the two costs a repeated listing.
func (m *Monitor) reconcileLoop(ctx context.Context, reg *registration) {
	changed := reg.Integration.ListenForTargetChanges(ctx)
	backstop := time.NewTicker(m.backstop)
	defer backstop.Stop()
	retry := time.NewTimer(reg.Cadence)
	retry.Stop()
	defer retry.Stop()
	for {
		if err := m.reconcile(ctx, reg); err != nil && ctx.Err() == nil {
			slog.Warn("monitor: a listing of targets failed; trying again", "integration", reg.Name, "err", err)
			retry.Reset(reg.Cadence)
		}
		select {
		case <-ctx.Done():
			return
		case <-changed:
		case <-backstop.C:
		case <-retry.C:
		}
	}
}

// reconcile lists reg's targets and writes their rows. A listing that fails
// changes nothing: only an answer removes a target.
func (m *Monitor) reconcile(ctx context.Context, reg *registration) error {
	listed, err := reg.Integration.ListTargets(ctx)
	if err != nil {
		return err
	}
	return m.store.reconcile(ctx, reg.Name, listed)
}

// supervise keeps a worker per monitored target. A notice wakes it to compare
// generations and never to read: a worker reads on its cadence. A failed read of
// the targets is tried again after the shortest cadence; the retention sweep
// rides the backstop.
func (m *Monitor) supervise(ctx context.Context, notices <-chan gobus.Event[string, struct{}]) {
	workers := map[string]*worker{}
	var wg sync.WaitGroup
	defer func() {
		for _, w := range workers {
			w.cancel()
		}
		wg.Wait()
	}()
	sweep := time.NewTicker(m.backstop)
	defer sweep.Stop()
	retry := time.NewTimer(m.backstop)
	retry.Stop()
	defer retry.Stop()
	for {
		rows, err := m.store.monitored(ctx)
		switch {
		case err == nil:
			m.sync(ctx, &wg, workers, rows)
		case ctx.Err() == nil:
			slog.Warn("monitor: reading the monitored targets failed; trying again", "err", err)
			retry.Reset(m.shortestCadence())
		}
		select {
		case <-ctx.Done():
			return
		case <-retry.C:
		case <-notices:
		case <-sweep.C:
			if err := m.store.sweep(ctx, m.clock()); err != nil && ctx.Err() == nil {
				slog.Warn("monitor: the retention sweep failed", "err", err)
			}
		}
	}
}

// sync starts a worker for each monitored target of an integration this build
// has, tells each its generation and label, and stops the rest.
func (m *Monitor) sync(ctx context.Context, wg *sync.WaitGroup, workers map[string]*worker, rows []targetRow) {
	live := map[string]bool{}
	for _, row := range rows {
		reg, ok := m.byName[row.integration]
		if !ok {
			continue
		}
		live[row.id] = true
		if w, ok := workers[row.id]; ok {
			w.tell(row)
			continue
		}
		wctx, cancel := context.WithCancel(ctx)
		w := &worker{cancel: cancel}
		w.tell(row)
		workers[row.id] = w
		wg.Go(func() { m.watch(wctx, reg, row.id, row.key, w) })
	}
	for id, w := range workers {
		if !live[id] {
			w.cancel()
			delete(workers, id)
		}
	}
}

// shortestCadence is the least of the integrations' cadences, the backstop with none.
func (m *Monitor) shortestCadence() time.Duration {
	shortest := m.backstop
	for _, reg := range m.regs {
		shortest = min(shortest, reg.Cadence)
	}
	return shortest
}

// worker is one target's goroutine, as the supervisor holds it.
type worker struct {
	cancel     context.CancelFunc
	generation atomic.Int64
	label      atomic.Pointer[string]
}

// tell hands the worker its target's generation and label as row holds them.
func (w *worker) tell(row targetRow) {
	w.generation.Store(row.generation)
	w.label.Store(&row.label)
}

// watch reads one target on its integration's cadence, on a goroutine of its
// own so a slow target delays no other.
func (m *Monitor) watch(ctx context.Context, reg *registration, targetID, key string, w *worker) {
	tick := time.NewTicker(reg.Cadence)
	defer tick.Stop()
	var state *targetState
	var held int64
	for {
		// A generation the memory was not folded under drops it, so an off and an
		// on that reach the supervisor as one notice still start the windows over.
		if gen := w.generation.Load(); gen != held {
			state, held = nil, gen
		}
		state = m.readOnce(ctx, reg, targetID, Target{Key: key, Label: *w.label.Load()}, held, state)
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}

// readOnce takes one reading of t, bounded by its cadence, folds it into state
// and commits the fold, answering the state the next reading folds from: state
// itself when the commit failed, nil when the target moved under the reading.
func (m *Monitor) readOnce(ctx context.Context, reg *registration, targetID string, t Target, generation int64, state *targetState) *targetState {
	readCtx, cancel := context.WithTimeout(ctx, reg.Cadence)
	r, err := reg.Integration.Read(readCtx, t)
	timedOut := errors.Is(readCtx.Err(), context.DeadlineExceeded)
	cancel()
	switch {
	case ctx.Err() != nil:
		return state
	case timedOut:
		r = unknownAll(reg, CodeTimeout)
	case err != nil:
		r = unknownAll(reg, codeOf(err))
	default:
		r = check(reg, r)
	}

	now := m.clock()
	if state == nil {
		seeded, err := m.store.seed(ctx, targetID, now)
		if err != nil {
			if !errors.Is(err, ErrNoTarget) && ctx.Err() == nil {
				slog.Warn("monitor: reading a target's state failed", "integration", reg.Name, "err", err)
			}
			return nil
		}
		state = &seeded
	}
	next, ch := fold(*state, reg, r, now, appdb.NewID)
	switch err := m.store.commit(ctx, targetID, generation, ch, now); {
	case errors.Is(err, errDiscarded):
		return nil
	case err != nil:
		if ctx.Err() == nil {
			slog.Warn("monitor: a fold did not commit; the next reading folds again", "integration", reg.Name, "err", err)
		}
		return state
	}
	return &next
}
