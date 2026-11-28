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

package monitor

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/appdb"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/appdb/appdbtest"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/testutil"
)

// loopCadence is the fake integration's cadence in the loop's tests.
const loopCadence = 20 * time.Millisecond

// fakeIntegration lists the targets a test sets and reads each through read.
type fakeIntegration struct {
	mu       sync.Mutex
	targets  []Target
	listErr  error
	listGate chan struct{} // when set, a listing waits on it
	read     func(ctx context.Context, t Target) (Reading, error)

	changed chan struct{} // the notices, coalesced to one
	lists   chan struct{} // a send per listing
	reads   chan Target   // a send per reading
}

func newFake(targets ...Target) *fakeIntegration {
	return &fakeIntegration{
		targets: targets,
		read:    func(context.Context, Target) (Reading, error) { return seeing(flip.Name, "web"), nil },
		changed: make(chan struct{}, 1),
		lists:   make(chan struct{}, 1000),
		reads:   make(chan Target, 1000),
	}
}

func (f *fakeIntegration) ListTargets(ctx context.Context) ([]Target, error) {
	f.lists <- struct{}{}
	f.mu.Lock()
	gate := f.listGate
	f.mu.Unlock()
	if gate != nil {
		select {
		case <-gate:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]Target(nil), f.targets...), f.listErr
}

func (f *fakeIntegration) ListenForTargetChanges(context.Context) <-chan struct{} { return f.changed }

func (f *fakeIntegration) Read(ctx context.Context, t Target) (Reading, error) {
	f.reads <- t
	f.mu.Lock()
	read := f.read
	f.mu.Unlock()
	return read(ctx, t)
}

// notify sends a notice the way an integration must: never blocking.
func (f *fakeIntegration) notify() {
	select {
	case f.changed <- struct{}{}:
	default:
	}
}

func (f *fakeIntegration) set(fn func(f *fakeIntegration)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	fn(f)
}

// startMonitor runs a monitor over db and fake until the test ends.
func startMonitor(t *testing.T, db *appdb.DB, backstop time.Duration, fake *fakeIntegration) *Monitor {
	t.Helper()
	m, err := newMonitor(db, wallClock, backstop, Registration{Name: "fake", Cadence: loopCadence, Signals: []Signal{crash, flip}, Integration: fake})
	require.NoError(t, err)
	stop, err := m.Start(t.Context())
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, stop(context.Background()))
		require.NoError(t, m.Close())
	})
	return m
}

// A reading the switch moved under commits nothing and answers no memory, so the
// next reading seeds afresh under the new generation.
func TestAReadingTheSwitchMovedUnderKeepsNoMemory(t *testing.T) {
	db := appdbtest.Open(t)
	m, err := newMonitor(db, wallClock, time.Hour, Registration{Name: "fake", Cadence: loopCadence, Signals: []Signal{crash, flip}, Integration: newFake()})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, m.Close()) })
	id := targetOf(t, m.store, "t")
	require.NoError(t, m.SetEnabled(t.Context(), id, false))
	require.NoError(t, m.SetEnabled(t.Context(), id, true))

	state := m.readOnce(t.Context(), m.regs[0], id, Target{Key: "t", Label: "t"}, 0, nil)

	assert.Nil(t, state)
	assert.Zero(t, count(t, db, `SELECT count(*) FROM incidents`))
}

// New refuses a signal without its integration's prefix, two registrations of
// one name, and one with no cadence or no integration.
func TestNewRefusesABadRegistration(t *testing.T) {
	db := appdbtest.Open(t)
	fake := newFake()
	for name, regs := range map[string][]Registration{
		"a signal without the prefix": {{Name: "fake", Cadence: time.Second, Integration: fake, Signals: []Signal{{Name: "other.crash"}}}},
		"a name registered twice": {
			{Name: "fake", Cadence: time.Second, Integration: fake},
			{Name: "fake", Cadence: time.Second, Integration: fake},
		},
		"a signal registered twice": {{Name: "fake", Cadence: time.Second, Integration: fake, Signals: []Signal{flip, flip}}},
		"no cadence":                {{Name: "fake", Integration: fake}},
		"no integration":            {{Name: "fake", Cadence: time.Second}},
	} {
		_, err := New(db, regs...)
		assert.Error(t, err, name)
	}
	m, err := New(db, Registration{Name: "fake", Cadence: time.Second, Integration: fake, Signals: []Signal{crash, flip}})
	require.NoError(t, err)
	require.NoError(t, m.Close())
}

// A fake integration's listed target gains a row, is read by the key and label
// it was listed with on its cadence, and opens and ends incidents, its
// attributes and detail stored as they came.
func TestAListedTargetIsReadOnItsCadence(t *testing.T) {
	db := appdbtest.Open(t)
	fake := newFake(Target{Key: "k1", Label: "Target one"})
	m := startMonitor(t, db, time.Hour, fake)

	assert.Equal(t, Target{Key: "k1", Label: "Target one"}, testutil.Recv(t, fake.reads, "a reading"))
	testutil.Recv(t, fake.reads, "the next reading, a cadence later")
	require.Eventually(t, func() bool {
		return count(t, db, `SELECT count(*) FROM incidents WHERE subject_attributes = '{"name":"web"}'`) == 1
	}, testutil.Timeout, loopCadence)

	fake.set(func(f *fakeIntegration) {
		f.read = func(context.Context, Target) (Reading, error) { return nothing(), nil }
	})
	require.Eventually(t, func() bool {
		return count(t, db, `SELECT count(*) FROM incident_occurrences WHERE end_reason = 'resolved' AND detail = '{"pod":"web-0"}'`) == 1
	}, testutil.Timeout, loopCadence)
	id, err := m.TargetID(t.Context(), "fake", "k1")
	require.NoError(t, err)
	assert.NotEmpty(t, id)
}

// A listing that fails deletes no target and is tried again a cadence later,
// with no notice: one that fails at start still gives a new target its row.
func TestAFailedListingIsTriedAgainAndDeletesNothing(t *testing.T) {
	db := appdbtest.Open(t)
	_, err := db.Write.Exec(`INSERT INTO monitor_targets (id, integration, key, label, created_at, updated_at) VALUES ('old', 'fake', 'k0', 'k0', 0, 0)`)
	require.NoError(t, err)
	fake := newFake(Target{Key: "k1", Label: "k1"})
	fake.listErr = errors.New("unreachable")
	startMonitor(t, db, time.Hour, fake)

	testutil.Recv(t, fake.lists, "the first listing")
	testutil.Recv(t, fake.lists, "the retry")
	assert.Equal(t, 1, count(t, db, `SELECT count(*) FROM monitor_targets WHERE id = 'old'`))
	fake.set(func(f *fakeIntegration) { f.listErr = nil })

	require.Eventually(t, func() bool {
		return count(t, db, `SELECT count(*) FROM monitor_targets WHERE key = 'k1'`) == 1
	}, testutil.Timeout, loopCadence)
	assert.Zero(t, count(t, db, `SELECT count(*) FROM monitor_targets WHERE id = 'old'`), "the answer removes it")
}

// The loop listens before it first lists: notices sent while a listing runs, a
// hundred of them, cost one more listing, and no send blocks.
func TestNoticesDuringAListingCauseOneMoreListing(t *testing.T) {
	db := appdbtest.Open(t)
	fake := newFake(Target{Key: "k1", Label: "k1"})
	gate := make(chan struct{})
	fake.listGate = gate
	startMonitor(t, db, time.Hour, fake)
	testutil.Recv(t, fake.lists, "the first listing")

	for range 100 {
		fake.notify()
	}
	fake.set(func(f *fakeIntegration) { f.listGate = nil })
	close(gate)

	testutil.Recv(t, fake.lists, "one more listing")
	// Bounded: a third listing must stay away, against a window of many cadences.
	testutil.NoRecv(t, fake.lists, 10*loopCadence, "no third listing")
}

// The backstop lists again with no notice.
func TestTheBackstopListsAgain(t *testing.T) {
	db := appdbtest.Open(t)
	fake := newFake()
	startMonitor(t, db, loopCadence, fake)

	testutil.Recv(t, fake.lists, "the first listing")
	testutil.Recv(t, fake.lists, "the backstop's")
}

// A reading that outlives its cadence is cancelled and judges nothing, with
// timeout; a slow target delays no other.
func TestAReadingPastItsCadenceIsATimeout(t *testing.T) {
	db := appdbtest.Open(t)
	fake := newFake(Target{Key: "slow", Label: "slow"}, Target{Key: "fast", Label: "fast"})
	fake.read = func(ctx context.Context, t Target) (Reading, error) {
		if t.Key == "slow" {
			<-ctx.Done()
			return Reading{}, ctx.Err()
		}
		return nothing(), nil
	}
	startMonitor(t, db, time.Hour, fake)

	fast := 0
	for fast < 3 {
		if testutil.Recv(t, fake.reads, "a reading").Key == "fast" {
			fast++
		}
	}
	require.Eventually(t, func() bool {
		return count(t, db, `SELECT count(*) FROM monitor_targets WHERE key = 'slow'
			AND unknown_signals = '{"fake.crash":"timeout","fake.flip":"timeout"}'`) == 1
	}, testutil.Timeout, loopCadence)
}

// A failed reading fills in every signal with its code, unavailable for an
// error that carries none.
func TestAFailedReadingIsUnknownWithItsCode(t *testing.T) {
	db := appdbtest.Open(t)
	fake := newFake(Target{Key: "k1", Label: "k1"})
	fake.read = func(context.Context, Target) (Reading, error) {
		return Reading{}, &ReadError{Code: CodeDisabled}
	}
	startMonitor(t, db, time.Hour, fake)

	require.Eventually(t, func() bool {
		return count(t, db, `SELECT count(*) FROM monitor_targets WHERE unknown_signals = '{"fake.crash":"disabled","fake.flip":"disabled"}'`) == 1
	}, testutil.Timeout, loopCadence)
}

// A notice of the targets wakes the loop to compare generations, never to read.
func TestATargetsNoticeStartsNoReading(t *testing.T) {
	db := appdbtest.Open(t)
	fake := newFake(Target{Key: "k1", Label: "k1"})
	m, err := newMonitor(db, wallClock, time.Hour, Registration{Name: "fake", Cadence: time.Hour, Signals: []Signal{flip}, Integration: fake})
	require.NoError(t, err)
	stop, err := m.Start(t.Context())
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, stop(context.Background()))
		require.NoError(t, m.Close())
	})
	testutil.Recv(t, fake.reads, "the first reading")

	db.Notify(appdb.KeyMonitorTargets)

	// Bounded: a reading must stay away; the cadence is an hour.
	testutil.NoRecv(t, fake.reads, 10*loopCadence, "no reading on a notice")
}

// Turning a target off stops its readings; an off and an on start its windows
// over, so a failure that still holds starts a new occurrence.
func TestTheSwitchStopsReadingsAndStartsTheWindowsOver(t *testing.T) {
	db := appdbtest.Open(t)
	fake := newFake(Target{Key: "k1", Label: "k1"})
	m := startMonitor(t, db, time.Hour, fake)
	require.Eventually(t, func() bool {
		return count(t, db, `SELECT count(*) FROM incident_occurrences WHERE ended_at IS NULL`) == 1
	}, testutil.Timeout, loopCadence)
	id, err := m.TargetID(t.Context(), "fake", "k1")
	require.NoError(t, err)

	require.NoError(t, m.SetEnabled(t.Context(), id, false))
	require.NoError(t, m.SetEnabled(t.Context(), id, true))

	require.Eventually(t, func() bool {
		return count(t, db, `SELECT count(*) FROM incident_occurrences WHERE number = 2 AND ended_at IS NULL`) == 1
	}, testutil.Timeout, loopCadence, "the windows started over")
	assert.Equal(t, 1, count(t, db, `SELECT count(*) FROM incident_occurrences WHERE number = 1 AND end_reason = 'stopped'`))

	require.NoError(t, m.SetEnabled(t.Context(), id, false))
	require.Eventually(t, func() bool {
		for {
			select {
			case <-fake.reads:
			default:
				// Bounded: once off, no reading arrives within many cadences.
				select {
				case <-fake.reads:
					return false
				case <-time.After(10 * loopCadence):
					return true
				}
			}
		}
	}, testutil.Timeout, loopCadence, "no reading runs after the switch")
}

// A target whose integration this build lacks is never read, never deleted, and
// fails nothing.
func TestATargetOfAnIntegrationTheBuildLacksIsLeftAlone(t *testing.T) {
	db := appdbtest.Open(t)
	_, err := db.Write.Exec(`INSERT INTO monitor_targets (id, integration, key, label, created_at, updated_at) VALUES ('g', 'gone', 'k', 'k', 0, 0)`)
	require.NoError(t, err)
	fake := newFake(Target{Key: "k1", Label: "k1"})
	startMonitor(t, db, loopCadence, fake)

	for range 3 {
		assert.Equal(t, "k1", testutil.Recv(t, fake.reads, "a reading").Key)
	}
	testutil.Recv(t, fake.lists, "a listing")
	testutil.Recv(t, fake.lists, "another")
	assert.Equal(t, 1, count(t, db, `SELECT count(*) FROM monitor_targets WHERE id = 'g' AND last_read_at IS NULL`))
}

// A failed read of the monitored targets is tried again after a cadence, with no
// notice: the listing changes nothing and the backstop is an hour away.
func TestAFailedReadOfTheTargetsIsTriedAgain(t *testing.T) {
	logs := testutil.CaptureLogs(t)
	db := appdbtest.Open(t)
	_, err := db.Write.Exec(`INSERT INTO monitor_targets (id, integration, key, label, created_at, updated_at) VALUES ('t1', 'fake', 'k1', 'k1', 0, 0)`)
	require.NoError(t, err)
	fake := newFake(Target{Key: "k1", Label: "k1"})
	m, err := newMonitor(db, wallClock, time.Hour, Registration{Name: "fake", Cadence: loopCadence, Signals: []Signal{flip}, Integration: fake})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, m.Close()) })
	// After the statements prepare, so the first read of the targets fails.
	_, err = db.Write.Exec(`ALTER TABLE monitor_targets RENAME COLUMN generation TO renamed`)
	require.NoError(t, err)
	stop, err := m.Start(t.Context())
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, stop(context.Background())) })
	require.Eventually(t, func() bool {
		return strings.Contains(logs.String(), "reading the monitored targets failed")
	}, testutil.Timeout, loopCadence)

	_, err = db.Write.Exec(`ALTER TABLE monitor_targets RENAME COLUMN renamed TO generation`)
	require.NoError(t, err)

	assert.Equal(t, "k1", testutil.Recv(t, fake.reads, "the target's reading").Key)
}

// A sweep, a seed or a commit that fails is logged, and the loop goes on; a stop
// that lands during a reading folds nothing.
func TestTheLoopLogsWhatFailsAndGoesOn(t *testing.T) {
	for name, c := range map[string]struct {
		breaks string
		want   string
	}{
		"a sweep":  {`CREATE TRIGGER refuse BEFORE DELETE ON incidents BEGIN SELECT RAISE(ABORT, 'refused'); END`, "the retention sweep failed"},
		"a seed":   {`ALTER TABLE incident_occurrences RENAME COLUMN started_at TO renamed`, "reading a target's state failed"},
		"a commit": {`CREATE TRIGGER refuse BEFORE UPDATE OF last_read_at ON monitor_targets BEGIN SELECT RAISE(ABORT, 'refused'); END`, "a fold did not commit"},
	} {
		t.Run(name, func(t *testing.T) {
			logs := testutil.CaptureLogs(t)
			db := appdbtest.Open(t)
			_, err := db.Write.Exec(`INSERT INTO monitor_targets (id, integration, key, label, created_at, updated_at) VALUES ('t1', 'fake', 'k1', 'k1', 0, 0)`)
			require.NoError(t, err)
			_, err = db.Write.Exec(`INSERT INTO incidents (id, target_id, target_uid, subject_key, subject_label, signal, signal_label, severity, first_seen_at, last_seen_at, occurrence_count)
				VALUES ('old', 't1', 'u1', 'web', 'web', 'fake.flip', 'Flip', 'info', 0, 0, 1)`)
			require.NoError(t, err)
			fake := newFake(Target{Key: "k1", Label: "k1"})
			m, err := newMonitor(db, wallClock, loopCadence, Registration{Name: "fake", Cadence: loopCadence, Signals: []Signal{flip}, Integration: fake})
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, m.Close()) })
			_, err = db.Write.Exec(c.breaks)
			require.NoError(t, err)
			stop, err := m.Start(t.Context())
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, stop(context.Background())) })

			require.Eventually(t, func() bool { return strings.Contains(logs.String(), c.want) }, testutil.Timeout, loopCadence)
			testutil.Recv(t, fake.reads, "a reading after it")
		})
	}
}

// A stop that lands while a reading runs ends the reading and folds nothing.
func TestAStopDuringAReadingFoldsNothing(t *testing.T) {
	db := appdbtest.Open(t)
	fake := newFake(Target{Key: "k1", Label: "k1"})
	fake.read = func(ctx context.Context, _ Target) (Reading, error) {
		<-ctx.Done()
		return Reading{}, ctx.Err()
	}
	m, err := newMonitor(db, wallClock, time.Hour, Registration{Name: "fake", Cadence: time.Hour, Signals: []Signal{flip}, Integration: fake})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, m.Close()) })
	stop, err := m.Start(t.Context())
	require.NoError(t, err)
	testutil.Recv(t, fake.reads, "the reading")

	require.NoError(t, stop(context.Background()))

	assert.Zero(t, count(t, db, `SELECT count(*) FROM monitor_targets WHERE last_read_at IS NOT NULL`))
}
