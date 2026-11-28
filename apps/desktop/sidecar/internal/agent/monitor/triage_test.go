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
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/appdb"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/appdb/appdbtest"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/testutil"
)

// triageCall is one triage the fake was asked for, once its run was admitted.
type triageCall struct {
	target Target
	uid    string
	brief  string
	model  TriageModel
	runID  string
}

// fakeTriager runs a triage as RunMonitor does: it inserts the run and calls
// admit in one transaction, rolling both back on a refusal, then waits on gate
// when set and settles the run as result says.
type fakeTriager struct {
	db    *appdb.DB
	asked chan struct{}   // a send per triage asked for, before its admit
	calls chan triageCall // a send per triage admitted
	gate  chan struct{}
	// pause, when set, holds a triage before its insert, after a send on paused.
	pause, paused chan struct{}
	result        func(runID string) TriageResult
	err           error // when set, every triage fails to start with it
}

func newFakeTriager(db *appdb.DB) *fakeTriager {
	return &fakeTriager{
		db: db, asked: make(chan struct{}, 100), calls: make(chan triageCall, 100),
		result: func(runID string) TriageResult {
			return TriageResult{Status: TriageRunSucceeded, Report: "ATTENTION\nThe pod crashes."}
		},
	}
}

var errAdmitRefused = &TriageError{Code: TriageCodeMonitoringOff}

func (f *fakeTriager) Triage(ctx context.Context, t Target, uid, brief string, model TriageModel, admit Admit) (TriageResult, error) {
	f.asked <- struct{}{}
	if f.err != nil {
		return TriageResult{}, f.err
	}
	if f.pause != nil {
		f.paused <- struct{}{}
		<-f.pause
	}
	runID := appdb.NewID()
	tx, err := f.db.Write.BeginTx(ctx, nil)
	if err != nil {
		return TriageResult{}, err
	}
	defer tx.Rollback() //nolint:errcheck // committed below on the admitted path
	if _, err := tx.ExecContext(ctx, `INSERT INTO agent_runs (id, agent_type, app_version, trigger, cluster_id, provider, model, effort, dialect, created_at)
		VALUES (?, 'monitor', 'test', 'monitor', 'c1', ?, ?, ?, 'fake', 0)`, runID, model.ProviderID, model.ModelID, model.Effort); err != nil {
		return TriageResult{}, err
	}
	ok, err := admit(ctx, tx, runID)
	switch {
	case err != nil:
		return TriageResult{}, err
	case !ok:
		return TriageResult{}, errAdmitRefused
	}
	if err := tx.Commit(); err != nil {
		return TriageResult{}, err
	}
	f.calls <- triageCall{target: t, uid: uid, brief: brief, model: model, runID: runID}
	res := TriageResult{Status: TriageRunCancelled}
	if f.gate == nil {
		res = f.result(runID)
	} else {
		select {
		case <-f.gate:
			res = f.result(runID)
		case <-ctx.Done():
		}
	}
	if _, err := f.db.Write.Exec(`UPDATE agent_runs SET status = ?, result = ? WHERE id = ?`, string(res.Status), res.Report, runID); err != nil {
		return TriageResult{}, err
	}
	return res, nil
}

var testModel = TriageModel{ProviderID: "fake", ModelID: "fake-1", Effort: "low"}

// startTriageMonitor is a started monitor over a target t1 of the fake
// integration, whose readings judge nothing, holding incident i1 of three
// occurrences under u1, with triager as the integration's Triager.
func startTriageMonitor(t *testing.T, triager Triager) (*Monitor, *appdb.DB) {
	t.Helper()
	db := appdbtest.Open(t)
	insertCluster(t, db)
	insertTriageTarget(t, db, "t1", "k1")
	insertIncident(t, db, "t1", "i1", "fake.crash", 3)
	return startTriageMonitorOn(t, db, triager), db
}

func insertTriageTarget(t *testing.T, db *appdb.DB, id, key string) {
	t.Helper()
	_, err := db.Write.Exec(`INSERT INTO monitor_targets (id, integration, key, label, current_uid, created_at, updated_at)
		VALUES (?, 'fake', ?, ?, 'u1', 0, 0)`, id, key, "Target "+key)
	require.NoError(t, err)
}

func startTriageMonitorOn(t *testing.T, db *appdb.DB, triager Triager) *Monitor {
	t.Helper()
	return startTriageMonitorAt(t, db, triager, wallClock)
}

// startTriageMonitorAt is startTriageMonitorOn on clock.
func startTriageMonitorAt(t *testing.T, db *appdb.DB, triager Triager, clock func() time.Time) *Monitor {
	t.Helper()
	fake := newFake(Target{Key: "k1", Label: "Target k1"}, Target{Key: "k2", Label: "Target k2"})
	fake.read = func(context.Context, Target) (Reading, error) { return Reading{}, errors.New("unread") }
	m, err := newMonitor(db, clock, time.Hour, Registration{
		Name: "fake", Cadence: time.Hour, Signals: []Signal{crash, flip}, Integration: fake, Triager: triager,
	})
	require.NoError(t, err)
	stop, err := m.Start(t.Context())
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, stop(context.Background()))
		require.NoError(t, m.Close())
	})
	return m
}

// triageOf is incident id's triage as the watch serves it.
func triageOf(t *testing.T, m *Monitor, id IncidentID) IncidentTriage {
	t.Helper()
	i, err := m.store.servedIncident(t.Context(), id)
	require.NoError(t, err)
	return i.Triage
}

// untilTriage takes frames off the watch until incident id's triage is in state.
func untilTriage(t *testing.T, frames <-chan IncidentCollectionWatchFrame, id IncidentID, state TriageState) IncidentTriage {
	t.Helper()
	for {
		f := testutil.Recv(t, frames, "a frame of the incident's triage")
		if f.Incident != nil && f.Incident.ID == id && f.Incident.Triage.State == state {
			return f.Incident.Triage
		}
	}
}

func runLinked(t *testing.T, db *appdb.DB, id IncidentID) sql.NullString {
	t.Helper()
	var runID sql.NullString
	require.NoError(t, db.Read.QueryRow(`SELECT triage_run_id FROM incidents WHERE id = ?`, string(id)).Scan(&runID))
	return runID
}

// A triage that finishes arrives on the incidents watch as a Modified, done with
// its verdict, its report and its model; the mutation answers it queued.
func TestATriageThatFinishesArrivesAsModified(t *testing.T) {
	tr := &fakeTriager{}
	m, db := startTriageMonitor(t, tr)
	*tr = *newFakeTriager(db)
	tr.result = func(runID string) TriageResult {
		return TriageResult{Status: TriageRunSucceeded, Report: "DISMISS\nIt recovered."}
	}
	frames := watchIncidents(t, m)
	untilBookmark(t, frames, incidentBookmark)

	queued, err := m.Triage(t.Context(), "i1", testModel, false)
	require.NoError(t, err)
	assert.Equal(t, IncidentTriage{State: TriageStateQueued}, queued.Triage, "no run, verdict or report until the run is inserted")

	got := untilTriage(t, frames, "i1", TriageStateDone)
	assert.Equal(t, IncidentTriage{
		State: TriageStateDone, Verdict: TriageVerdictDismiss, Report: "DISMISS\nIt recovered.",
		ProviderID: "fake", ModelID: "fake-1", Effort: "low",
	}, got)
	call := testutil.Recv(t, tr.calls, "the triage")
	assert.Equal(t, Target{Key: "k1", Label: "Target k1"}, call.target)
	assert.Equal(t, "u1", call.uid, "the server the incident opened on")
	assert.Equal(t, testModel, call.model)
	assert.Equal(t, call.runID, runLinked(t, db, "i1").String)
}

// A report whose first line is the word DISMISS dismisses, however emphasized;
// any other reads as attention.
func TestTheVerdictIsTheReportsFirstLine(t *testing.T) {
	for report, want := range map[string]TriageVerdict{
		"DISMISS\nIt recovered.":             TriageVerdictDismiss,
		"  DISMISS  \nIt recovered.":         TriageVerdictDismiss,
		"ATTENTION\nThe pod crashes.":        TriageVerdictAttention,
		"The pod crashes.\nDISMISS":          TriageVerdictAttention,
		"dismiss":                            TriageVerdictDismiss,
		"`DISMISS`\nas the prompt spells it": TriageVerdictDismiss,
		"DISMISS.":                           TriageVerdictDismiss,
		"DISMISSED":                          TriageVerdictAttention,
		"":                                   TriageVerdictAttention,
		"**DISMISS**\nemphasized":            TriageVerdictDismiss,
		"DISMISS this, it matters":           TriageVerdictAttention,
	} {
		assert.Equal(t, want, verdictOf(report), "%q", report)
	}
}

// A run that fails, a provider's error among the causes, leaves the triage failed
// with its code and its run linked, and the incident can be triaged again.
func TestARunThatFailsLeavesTheTriageFailedWithItsRun(t *testing.T) {
	tr := &fakeTriager{}
	m, db := startTriageMonitor(t, tr)
	*tr = *newFakeTriager(db)
	tr.result = func(string) TriageResult { return TriageResult{Status: TriageRunFailed} }
	frames := watchIncidents(t, m)
	untilBookmark(t, frames, incidentBookmark)

	_, err := m.Triage(t.Context(), "i1", testModel, false)
	require.NoError(t, err)

	got := untilTriage(t, frames, "i1", TriageStateFailed)
	assert.Equal(t, TriageCodeRunFailed, got.Error)
	assert.True(t, runLinked(t, db, "i1").Valid)
	_, err = m.Triage(t.Context(), "i1", testModel, false)
	assert.NoError(t, err, "the action is offered again")
}

// Each ending of a run reads as its state and code.
func TestEachEndingOfATriageReadsAsItsState(t *testing.T) {
	for name, c := range map[string]struct {
		res  TriageResult
		err  error
		want triageOutcome
	}{
		"succeeded":       {res: TriageResult{Status: TriageRunSucceeded, Report: "ATTENTION"}, want: triageOutcome{state: TriageStateDone, verdict: TriageVerdictAttention}},
		"failed":          {res: TriageResult{Status: TriageRunFailed}, want: triageOutcome{state: TriageStateFailed, code: TriageCodeRunFailed}},
		"failed for code": {res: TriageResult{Status: TriageRunFailed, Code: TriageCodeServerChanged}, want: triageOutcome{state: TriageStateFailed, code: TriageCodeServerChanged}},
		"cancelled":       {res: TriageResult{Status: TriageRunCancelled}, want: triageOutcome{state: TriageStateFailed, code: TriageCodeCancelled}},
		"refused":         {err: &TriageError{Code: TriageCodeTargetGone}, want: triageOutcome{state: TriageStateFailed, code: TriageCodeTargetGone}},
		"any other error": {err: errors.New("boom"), want: triageOutcome{state: TriageStateFailed, code: TriageCodeUnavailable}},
	} {
		assert.Equal(t, c.want, outcomeOf(c.res, c.err), name)
	}
	assert.Equal(t, "monitor: triage target_gone: gone", (&TriageError{Code: TriageCodeTargetGone, Err: errors.New("gone")}).Error())
	assert.Equal(t, "monitor: triage target_gone", (&TriageError{Code: TriageCodeTargetGone}).Error())
}

// A second triage of a target waits for the first; another target's does not.
func TestASecondTriageOfATargetWaitsForTheFirst(t *testing.T) {
	tr := &fakeTriager{}
	m, db := startTriageMonitor(t, tr)
	*tr = *newFakeTriager(db)
	tr.gate = make(chan struct{})
	insertIncident(t, db, "t1", "i2", "fake.flip", 1)
	insertIncident(t, db, "t1", "i3", "fake.gone", 1)
	_, err := db.Write.Exec(`UPDATE incidents SET signal_label = 'Flip' WHERE id = 'i2'`)
	require.NoError(t, err)

	_, err = m.Triage(t.Context(), "i1", testModel, false)
	require.NoError(t, err)
	testutil.Recv(t, tr.asked, "the first triage asked")
	testutil.Recv(t, tr.calls, "the first triage")
	_, err = m.Triage(t.Context(), "i2", testModel, false)
	require.NoError(t, err)
	_, err = m.Triage(t.Context(), "i3", testModel, false)
	require.NoError(t, err)

	// A negative assertion: the second must not be asked while the first holds.
	select {
	case <-tr.asked:
		close(tr.gate)
		t.Fatal("the second triage started while the first ran")
	case <-time.After(50 * time.Millisecond):
	}
	close(tr.gate)
	assert.Equal(t, "Flip", fenced(t, testutil.Recv(t, tr.calls, "the second triage, once the first ended").brief).Signal)
	assert.Equal(t, "Gone signal", fenced(t, testutil.Recv(t, tr.calls, "the third, in the order asked").brief).Signal)
}

// A triage asked for once the monitor stops is refused, with nothing queued.
func TestATriageAfterTheStopIsRefused(t *testing.T) {
	db := appdbtest.Open(t)
	m, err := newMonitor(db, wallClock, time.Hour, Registration{Name: "fake", Cadence: time.Hour, Integration: newFake(), Triager: newFakeTriager(db)})
	require.NoError(t, err)
	require.NoError(t, m.Close())

	_, err = m.Triage(t.Context(), "i1", testModel, false)
	assert.ErrorIs(t, err, ErrStopping)
}

// A target switched off while its triage is queued takes no run and no link: the
// admit refuses inside the insert, and the triage fails as monitoring_off.
func TestATriageIsAdmittedInsideTheInsert(t *testing.T) {
	tr := &fakeTriager{}
	m, db := startTriageMonitor(t, tr)
	*tr = *newFakeTriager(db)
	tr.gate = make(chan struct{})
	insertIncident(t, db, "t1", "i2", "fake.flip", 1)
	frames := watchIncidents(t, m)
	untilBookmark(t, frames, incidentBookmark)

	_, err := m.Triage(t.Context(), "i1", testModel, false)
	require.NoError(t, err)
	testutil.Recv(t, tr.calls, "the first triage")
	_, err = m.Triage(t.Context(), "i2", testModel, false)
	require.NoError(t, err)
	_, err = m.SetEnabled(t.Context(), "t1", false)
	require.NoError(t, err)
	close(tr.gate)

	got := untilTriage(t, frames, "i2", TriageStateFailed)
	assert.Equal(t, TriageCodeMonitoringOff, got.Error)
	assert.False(t, runLinked(t, db, "i2").Valid)
	var runs int
	require.NoError(t, db.Read.QueryRow(`SELECT count(*) FROM agent_runs`).Scan(&runs))
	assert.Equal(t, 1, runs, "the refused triage inserted no run")
}

// A target a fold moves to another server while its triage is queued takes no run:
// the admit compares the incident's server with the target's inside the insert.
func TestATriageOfATargetMovedSinceItQueuedIsNotAdmitted(t *testing.T) {
	tr := &fakeTriager{}
	m, db := startTriageMonitor(t, tr)
	*tr = *newFakeTriager(db)
	tr.gate = make(chan struct{})
	insertIncident(t, db, "t1", "i2", "fake.flip", 1)
	frames := watchIncidents(t, m)
	untilBookmark(t, frames, incidentBookmark)

	_, err := m.Triage(t.Context(), "i1", testModel, false)
	require.NoError(t, err)
	testutil.Recv(t, tr.calls, "the first triage")
	_, err = m.Triage(t.Context(), "i2", testModel, false)
	require.NoError(t, err)
	_, err = db.Write.Exec(`UPDATE monitor_targets SET current_uid = 'u2' WHERE id = 't1'`)
	require.NoError(t, err)
	close(tr.gate)

	got := untilTriage(t, frames, "i2", TriageStateFailed)
	assert.Equal(t, TriageCodeServerChanged, got.Error)
	assert.False(t, runLinked(t, db, "i2").Valid)
	var runs int
	require.NoError(t, db.Read.QueryRow(`SELECT count(*) FROM agent_runs`).Scan(&runs))
	assert.Equal(t, 1, runs, "the refused triage inserted no run")
}

// A target switched off and on again while its triage is queued still takes no
// run: the triage holds the generation it was queued under. One asked for after
// the switch runs.
func TestATriageQueuedBeforeASwitchIsNotRevived(t *testing.T) {
	tr := &fakeTriager{}
	m, db := startTriageMonitor(t, tr)
	*tr = *newFakeTriager(db)
	tr.gate = make(chan struct{})
	insertIncident(t, db, "t1", "i2", "fake.flip", 1)
	frames := watchIncidents(t, m)
	untilBookmark(t, frames, incidentBookmark)

	_, err := m.Triage(t.Context(), "i1", testModel, false)
	require.NoError(t, err)
	testutil.Recv(t, tr.calls, "the first triage")
	_, err = m.Triage(t.Context(), "i2", testModel, false)
	require.NoError(t, err)
	_, err = m.SetEnabled(t.Context(), "t1", false)
	require.NoError(t, err)
	_, err = m.SetEnabled(t.Context(), "t1", true)
	require.NoError(t, err)
	close(tr.gate)

	got := untilTriage(t, frames, "i2", TriageStateFailed)
	assert.Equal(t, TriageCodeMonitoringOff, got.Error)

	_, err = m.Triage(t.Context(), "i2", testModel, false)
	require.NoError(t, err)
	assert.Equal(t, TriageVerdictAttention, untilTriage(t, frames, "i2", TriageStateDone).Verdict)
}

// The mutation refuses an incident it does not serve, one already queued, one
// whose integration cannot triage, and one opened under a UID its target no
// longer has; each writes nothing.
func TestATriageIsRefused(t *testing.T) {
	tr := &fakeTriager{}
	m, db := startTriageMonitor(t, tr)
	*tr = *newFakeTriager(db)
	tr.gate = make(chan struct{})
	t.Cleanup(func() { close(tr.gate) })

	_, err := m.Triage(t.Context(), "nope", testModel, false)
	assert.ErrorIs(t, err, ErrNoIncident)

	_, err = m.Triage(t.Context(), "i1", testModel, false)
	require.NoError(t, err)
	_, err = m.Triage(t.Context(), "i1", testModel, false)
	assert.ErrorIs(t, err, ErrTriageQueued)

	insertIncident(t, db, "t1", "i2", "fake.flip", 1)
	_, err = db.Write.Exec(`UPDATE monitor_targets SET current_uid = 'u2' WHERE id = 't1'`)
	require.NoError(t, err)
	_, err = m.Triage(t.Context(), "i2", testModel, false)
	assert.ErrorIs(t, err, ErrTargetMoved)
	assert.Equal(t, TriageStateNone, triageOf(t, m, "i2").State)

	other := insertOther(t, db)
	_, err = m.Triage(t.Context(), other, testModel, false)
	assert.ErrorIs(t, err, ErrNoIncident, "an integration the build lacks serves no incident")

	// The rows land before the start, whose first reconcile lists k1 too.
	noneDB := appdbtest.Open(t)
	insertTriageTarget(t, noneDB, "t1", "k1")
	insertIncident(t, noneDB, "t1", "i1", "fake.crash", 1)
	none := startTriageMonitorOn(t, noneDB, nil)
	_, err = none.Triage(t.Context(), "i1", testModel, false)
	assert.ErrorIs(t, err, ErrCannotTriage)
	targets, err := none.servedTargets(t.Context())
	require.NoError(t, err)
	require.NotEmpty(t, targets)
	assert.False(t, targets[0].CanTriage)
	served, err := m.servedTargets(t.Context())
	require.NoError(t, err)
	assert.True(t, served[0].CanTriage)
}

// The brief the run is handed is the incident's, with its newest occurrence's
// detail and no other's.
func TestTheBriefCarriesTheNewestOccurrence(t *testing.T) {
	tr := &fakeTriager{}
	m, db := startTriageMonitor(t, tr)
	*tr = *newFakeTriager(db)
	for n, msg := range map[int]string{1: "first", 2: "second", 3: "newest"} {
		_, err := db.Write.Exec(`UPDATE incident_occurrences SET detail = json_object('message', ?) WHERE incident_id = 'i1' AND number = ?`, msg, n)
		require.NoError(t, err)
	}

	_, err := m.Triage(t.Context(), "i1", testModel, false)
	require.NoError(t, err)

	b := fenced(t, testutil.Recv(t, tr.calls, "the triage").brief)
	assert.Equal(t, map[string]string{"message": "newest"}, b.Detail)
	assert.Equal(t, 3, b.Occurrences)
	assert.Equal(t, "Deployment web", b.Subject)
}

// A triage queued again serves no run, verdict or report until its new run is
// inserted.
func TestATriageQueuedAgainServesNothingOfTheLast(t *testing.T) {
	tr := &fakeTriager{}
	m, db := startTriageMonitor(t, tr)
	*tr = *newFakeTriager(db)
	frames := watchIncidents(t, m)
	untilBookmark(t, frames, incidentBookmark)
	_, err := m.Triage(t.Context(), "i1", testModel, false)
	require.NoError(t, err)
	untilTriage(t, frames, "i1", TriageStateDone)
	tr.gate = make(chan struct{})
	t.Cleanup(func() { close(tr.gate) })

	again, err := m.Triage(t.Context(), "i1", testModel, false)
	require.NoError(t, err)

	assert.Equal(t, IncidentTriage{State: TriageStateQueued}, again.Triage)
}

// A worker holding a request the incident no longer has is refused at its admit
// and writes no outcome over the newer request.
func TestAWorkerThatOutlivedItsRequestWritesNothing(t *testing.T) {
	tr := &fakeTriager{}
	m, db := startTriageMonitor(t, tr)
	*tr = *newFakeTriager(db)
	req, err := m.store.queueTriage(t.Context(), "i1", "r1", func(string) bool { return true }, nil)
	require.NoError(t, err)
	_, err = db.Write.Exec(`UPDATE incidents SET triage_request_id = 'r2' WHERE id = 'i1'`)
	require.NoError(t, err)

	tx, err := db.Write.BeginTx(t.Context(), nil)
	require.NoError(t, err)
	_, err = tx.Exec(`INSERT INTO agent_runs (id, agent_type, app_version, trigger, cluster_id, provider, model, dialect, created_at)
		VALUES ('run1', 'monitor', 'test', 'monitor', 'c1', 'fake', 'fake', 'fake', 0)`)
	require.NoError(t, err)
	refusal, err := m.store.admitTriage(t.Context(), tx, req, "run1")
	require.NoError(t, err)
	assert.Equal(t, TriageCodeMonitoringOff, refusal)
	require.NoError(t, tx.Rollback())

	require.NoError(t, m.store.finishTriage(t.Context(), req, triageOutcome{state: TriageStateDone, verdict: TriageVerdictDismiss}))
	var state, requestID string
	require.NoError(t, db.Read.QueryRow(`SELECT triage_state, triage_request_id FROM incidents WHERE id = 'i1'`).Scan(&state, &requestID))
	assert.Equal(t, []string{"queued", "r2"}, []string{state, requestID})
}

// A start marks a triage still queued failed, interrupted, with its run linked;
// an automatic one alerts its incident then, a manual one does not.
func TestAStrandedTriageIsFailedAtStart(t *testing.T) {
	db := appdbtest.Open(t)
	_, err := db.Write.Exec(`INSERT INTO clusters (id, source, created_at, updated_at) VALUES ('c1', 'kubeconfig', 0, 0)`)
	require.NoError(t, err)
	_, err = db.Write.Exec(`INSERT INTO agent_runs (id, agent_type, app_version, trigger, cluster_id, provider, model, dialect, created_at)
		VALUES ('run1', 'monitor', 'test', 'monitor', 'c1', 'fake', 'fake', 'fake', 0)`)
	require.NoError(t, err)
	insertTriageTarget(t, db, "t1", "k1")
	insertIncident(t, db, "t1", "i1", "fake.crash", 1)
	insertIncident(t, db, "t1", "i2", "fake.flip", 1)
	insertIncident(t, db, "t1", "i3", "fake.flip", 2)
	queueAs(t, db, "i1", "r1", triggerAutomatic, "run1")
	queueAs(t, db, "i2", "r2", triggerManual, nil)
	queueAs(t, db, "i3", "r3", triggerAutomatic, nil)
	setLastAlertSeq(t, db, 5)

	before := time.Now().UnixMilli()
	m := startTriageMonitorOn(t, db, newFakeTriager(db))

	for _, id := range []IncidentID{"i1", "i2"} {
		got := triageOf(t, m, id)
		assert.Equal(t, TriageStateFailed, got.State, id)
		assert.Equal(t, TriageCodeInterrupted, got.Error, id)
	}
	assert.Equal(t, "run1", runLinked(t, db, "i1").String)
	assert.GreaterOrEqual(t, alertedAt(t, db, "i1"), before, "the automatic one alerts at the start")
	assert.Equal(t, int64(-1), alertedAt(t, db, "i2"))
	assertNumberedPast(t, db, 5, "i1", "i3")
}

// One setting change that cancels several automatic triages alerts each under a
// number of its own, past the last given.
func TestACancelThatAlertsSeveralNumbersEachAlert(t *testing.T) {
	tr := &fakeTriager{}
	m, db := startTriageMonitor(t, tr)
	*tr = *newFakeTriager(db)
	insertIncident(t, db, "t1", "i2", "fake.flip", 1)
	insertIncident(t, db, "t1", "i3", "fake.flip", 2)
	require.NoError(t, m.SetTriageModel(t.Context(), &testModel))
	queueAs(t, db, "i2", "r2", triggerAutomatic, nil)
	queueAs(t, db, "i3", "r3", triggerAutomatic, nil)
	setLastAlertSeq(t, db, 5)

	require.NoError(t, m.SetTriageModel(t.Context(), nil))

	assertNumberedPast(t, db, 5, "i2", "i3")
}

// assertNumberedPast checks that each of ids alerted under a number of its own
// past last, and that the counter holds the highest.
func assertNumberedPast(t *testing.T, db *appdb.DB, last int64, ids ...IncidentID) {
	t.Helper()
	seen := map[int64]bool{}
	highest := last
	for _, id := range ids {
		seq := alertSeq(t, db, id)
		assert.Greater(t, seq, last, id)
		assert.False(t, seen[seq], "%s shares its number", id)
		seen[seq] = true
		highest = max(highest, seq)
	}
	assert.Equal(t, int(highest), count(t, db, `SELECT last FROM monitor_alert_seq`))
}

// setLastAlertSeq records n as the last alert_seq given.
func setLastAlertSeq(t *testing.T, db *appdb.DB, n int) {
	t.Helper()
	_, err := db.Write.Exec(`INSERT INTO monitor_alert_seq (id, last) VALUES (1, ?) ON CONFLICT (id) DO UPDATE SET last = excluded.last`, n)
	require.NoError(t, err)
}

// alertSeq is incident id's alert_seq, -1 while it has none.
func alertSeq(t *testing.T, db *appdb.DB, id IncidentID) int64 {
	t.Helper()
	var seq sql.NullInt64
	require.NoError(t, db.Read.QueryRow(`SELECT alert_seq FROM incidents WHERE id = ?`, string(id)).Scan(&seq))
	if !seq.Valid {
		return -1
	}
	return seq.Int64
}

// queueAutomatic queues an automatic triage of incident id on model, as a fold
// that starts one does, and hands it to the queue.
func queueAutomatic(t *testing.T, m *Monitor, id IncidentID, model TriageModel) {
	t.Helper()
	var req triageRequest
	require.NoError(t, m.store.set.InTx(t.Context(), func(st stmts) error {
		var err error
		req, err = m.store.queueAutomaticTriage(t.Context(), st, id, model, false)
		return err
	}))
	m.enqueue(req)
}

// insertCluster stores cluster c1, which the fake's runs name.
func insertCluster(t *testing.T, db *appdb.DB) {
	t.Helper()
	_, err := db.Write.Exec(`INSERT INTO clusters (id, source, created_at, updated_at) VALUES ('c1', 'kubeconfig', 0, 0)`)
	require.NoError(t, err)
}

// alertedAt is incident id's alerted_at, -1 for none.
func alertedAt(t *testing.T, db *appdb.DB, id IncidentID) int64 {
	t.Helper()
	_, at := incidentAlert(t, db, string(id))
	return at
}

// testClock is a clock a test moves.
type testClock struct {
	mu sync.Mutex
	at time.Time
}

func (c *testClock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.at
}

func (c *testClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.at = c.at.Add(d)
}

// startAutomaticMonitor runs a monitor over one fake target, k1, whose first
// reading sees nothing and each after sees flip on web, so its incident opens
// ordinary, with triager and the triage setting set to model.
func startAutomaticMonitor(t *testing.T, db *appdb.DB, triager Triager, model *TriageModel) *Monitor {
	t.Helper()
	var reads atomic.Int64
	fake := newFake(Target{Key: "k1", Label: "Target k1"})
	fake.read = func(context.Context, Target) (Reading, error) {
		if reads.Add(1) == 1 {
			return nothing(), nil
		}
		return seeing(flip.Name, "web"), nil
	}
	m, err := newMonitor(db, wallClock, time.Hour, Registration{
		Name: "fake", Cadence: loopCadence, Signals: []Signal{crash, flip}, Integration: fake, Triager: triager,
	})
	require.NoError(t, err)
	if model != nil {
		require.NoError(t, m.SetTriageModel(t.Context(), model))
	}
	stop, err := m.Start(t.Context())
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, stop(context.Background()))
		require.NoError(t, m.Close())
	})
	return m
}

// With the setting unset, an ordinary incident alerts as it opens and no run
// starts without the user's press.
func TestNoTriageStartsUnaskedWhileAutomaticTriageIsOff(t *testing.T) {
	db := appdbtest.Open(t)
	tr := newFakeTriager(db)
	startAutomaticMonitor(t, db, tr, nil)

	require.Eventually(t, func() bool {
		return count(t, db, `SELECT count(*) FROM incidents WHERE baseline = 0 AND alerted_at IS NOT NULL`) > 0
	}, testutil.Timeout, loopCadence)

	// A negative assertion over a few cadences of readings.
	select {
	case <-tr.asked:
		t.Fatal("an incident started a triage")
	case <-time.After(5 * loopCadence):
	}
}

// With the setting on, a new incident is triaged on its model, unasked: an
// answer of attention alerts it, and a dismiss does not.
func TestANewIncidentIsTriagedAndOnlyAttentionAlerts(t *testing.T) {
	for report, alerts := range map[string]bool{"ATTENTION\nIt crashes.": true, "DISMISS\nIt recovered.": false} {
		db := appdbtest.Open(t)
		insertCluster(t, db)
		tr := newFakeTriager(db)
		tr.result = func(string) TriageResult { return TriageResult{Status: TriageRunSucceeded, Report: report} }
		m := startAutomaticMonitor(t, db, tr, &testModel)
		frames := watchIncidents(t, m)

		var id IncidentID
		for {
			f := testutil.Recv(t, frames, "the incident's triage")
			if f.Incident != nil && f.Incident.Triage.State == TriageStateDone {
				id = f.Incident.ID
				break
			}
		}
		assert.Equal(t, testModel, testutil.Recv(t, tr.calls, "the run").model, report)
		assert.Equal(t, alerts, alertedAt(t, db, id) != -1, report)
	}
}

// A first ordinary occurrence that starts while the user's own triage of its
// incident is queued queues nothing, and the incident alerts at once; one that
// starts while an automatic triage is queued queues nothing and raises nothing.
func TestAnOccurrenceUnderAQueuedTriageQueuesNothing(t *testing.T) {
	st := openTestStore(t)
	id := targetOf(t, st, "t")
	require.NoError(t, st.setTriageModel(t.Context(), &testModel))
	for _, trigger := range []triageTrigger{triggerManual, triggerAutomatic} {
		incident := "i-" + string(trigger)
		s := start{occurrenceID: incident + "-1", incidentID: incident, number: 1, baseline: true, startedAt: t0, seenAt: t0,
			open: &newIncident{uid: "u1", subject: Subject{Key: incident}, signal: crash}}
		_, err := st.commit(t.Context(), id, 0, changes{starts: []start{s}}, t0, true)
		require.NoError(t, err)
		queueAs(t, st.db, incident, "r1", trigger, nil)

		next := start{occurrenceID: incident + "-2", incidentID: incident, number: 2, startedAt: t0.Add(time.Hour), seenAt: t0.Add(time.Hour)}
		ends := []end{{occurrenceID: incident + "-1", incidentID: incident, at: t0.Add(time.Minute), reason: EndResolved}}
		queued, err := st.commit(t.Context(), id, 0, changes{starts: []start{next}, ends: ends}, next.seenAt, true)
		require.NoError(t, err)

		assert.Empty(t, queued, trigger)
		assert.Equal(t, trigger == triggerManual, alertedAt(t, st.db, IncidentID(incident)) == next.seenAt.UnixMilli(), trigger)
	}
}

// A baseline occurrence is never triaged unasked, the third of its incident
// included. An incident whose first ordinary occurrence is its third is queued
// once, with a brief that says it is the third.
func TestTheThirdOccurrenceQueuesATriageUnlessItIsBaseline(t *testing.T) {
	newStore := func() (*store, string) {
		st := openTestStore(t)
		require.NoError(t, st.setTriageModel(t.Context(), &testModel))
		return st, targetOf(t, st, "t")
	}
	at := func(st *store, id string, n int, baseline bool) []triageRequest {
		t.Helper()
		s := start{occurrenceID: "o" + strconv.Itoa(n), incidentID: "i1", number: n, baseline: baseline,
			startedAt: t0.Add(time.Duration(n) * time.Hour), seenAt: t0.Add(time.Duration(n) * time.Hour)}
		var ends []end
		if n == 1 {
			s.open = &newIncident{uid: "u1", subject: Subject{Key: "web", Label: "Deployment web"}, signal: crash}
		} else {
			ends = []end{{occurrenceID: "o" + strconv.Itoa(n-1), incidentID: "i1", at: s.startedAt, reason: EndResolved}}
		}
		queued, err := st.commit(t.Context(), id, 0, changes{starts: []start{s}, ends: ends}, s.seenAt, true)
		require.NoError(t, err)
		return queued
	}

	st, id := newStore()
	assert.Empty(t, at(st, id, 1, true))
	assert.Empty(t, at(st, id, 2, true))
	queued := at(st, id, 3, false)
	require.Len(t, queued, 1, "the first ordinary occurrence, the third, queues one triage")
	assert.Equal(t, triggerAutomatic, queued[0].trigger)
	assert.Equal(t, testModel, queued[0].model)
	assert.True(t, strings.HasPrefix(queued[0].brief, briefRecurring), "the brief says it is the third")
	assert.Equal(t, int64(-1), alertedAt(t, st.db, "i1"), "the triage decides the alert")
	_, err := st.db.Write.Exec(`UPDATE incidents SET triage_state = 'done', triage_verdict = 'dismiss', triage_request_id = NULL WHERE id = 'i1'`)
	require.NoError(t, err)
	assert.Empty(t, at(st, id, 4, false), "a fourth queues none")

	st, id = newStore()
	assert.Empty(t, at(st, id, 1, true))
	assert.Empty(t, at(st, id, 2, true))
	assert.Empty(t, at(st, id, 3, true), "a baseline third queues none")
}

// A setting turned off, or set to another model, cancels each automatic triage
// queued under it whose run has not started: it takes no run and alerts. One
// whose run has started finishes, and a queued manual one stays queued.
func TestAnAutomaticTriageRunsOnlyUnderItsSetting(t *testing.T) {
	tr := &fakeTriager{}
	m, db := startTriageMonitor(t, tr)
	*tr = *newFakeTriager(db)
	tr.gate = make(chan struct{})
	for _, id := range []string{"i2", "i3", "i4"} {
		insertIncident(t, db, "t1", id, "fake.flip", 1)
	}
	frames := watchIncidents(t, m)
	untilBookmark(t, frames, incidentBookmark)
	require.NoError(t, m.SetTriageModel(t.Context(), &testModel))

	queueAutomatic(t, m, "i1", testModel)
	testutil.Recv(t, tr.calls, "i1's run, which started")
	queueAutomatic(t, m, "i2", testModel)
	_, err := m.Triage(t.Context(), "i4", testModel, false)
	require.NoError(t, err)
	require.NoError(t, m.SetTriageModel(t.Context(), nil))
	require.NoError(t, m.SetTriageModel(t.Context(), &testModel))
	queueAutomatic(t, m, "i3", testModel)
	require.NoError(t, m.SetTriageModel(t.Context(), &TriageModel{ProviderID: "fake", ModelID: "fake-2"}))

	for _, id := range []IncidentID{"i2", "i3"} {
		assert.Equal(t, IncidentTriage{State: TriageStateFailed, Error: TriageCodeSettingChanged}, triageOf(t, m, id), id)
		assert.NotEqual(t, int64(-1), alertedAt(t, db, id), id)
	}
	assert.Equal(t, TriageStateQueued, triageOf(t, m, "i4").State, "the manual one is left alone")
	close(tr.gate)
	assert.Equal(t, TriageStateDone, untilTriage(t, frames, "i1", TriageStateDone).State, "a started run finishes")
	assert.Equal(t, TriageStateDone, untilTriage(t, frames, "i4", TriageStateDone).State)
	assert.Equal(t, 2, count(t, db, `SELECT count(*) FROM agent_runs`), "the cancelled ones took no run")
}

// A worker paused before its insert, whose request a setting change cancels,
// starts no run when it resumes, though the user has queued the incident again;
// the new request runs with its own model.
func TestACancelledTriageStartsNoRun(t *testing.T) {
	tr := &fakeTriager{}
	m, db := startTriageMonitor(t, tr)
	*tr = *newFakeTriager(db)
	tr.pause, tr.paused = make(chan struct{}), make(chan struct{}, 1)
	require.NoError(t, m.SetTriageModel(t.Context(), &testModel))

	queueAutomatic(t, m, "i1", testModel)
	testutil.Recv(t, tr.paused, "the worker, before its insert")
	require.NoError(t, m.SetTriageModel(t.Context(), nil))
	other := TriageModel{ProviderID: "fake", ModelID: "fake-2"}
	_, err := m.Triage(t.Context(), "i1", other, false)
	require.NoError(t, err)
	close(tr.pause)

	assert.Equal(t, other, testutil.Recv(t, tr.calls, "the new request's run").model)
	require.Eventually(t, func() bool { return triageOf(t, m, "i1").State == TriageStateDone }, testutil.Timeout, 10*time.Millisecond)
	assert.Equal(t, 1, count(t, db, `SELECT count(*) FROM agent_runs`), "the cancelled request took no run")
}

// The seventh automatic triage of an hour on one target takes no run: it fails
// as cap_reached and its incident alerts. Starts before a restart count, a
// refused one adds none, the user's own triage is never capped, and the hour
// passes.
func TestTheSeventhAutomaticTriageOfAnHourTakesNoRun(t *testing.T) {
	db := appdbtest.Open(t)
	insertCluster(t, db)
	insertTriageTarget(t, db, "t1", "k1")
	for _, id := range []string{"i1", "i2", "i3", "i4"} {
		insertIncident(t, db, "t1", id, "fake.crash", 1)
	}
	clock := &testClock{at: t0}
	// Five starts of the last hour, from before a restart, and one past it.
	for _, ago := range []time.Duration{61 * time.Minute, 50 * time.Minute, 40 * time.Minute, 30 * time.Minute, 20 * time.Minute, 10 * time.Minute} {
		_, err := db.Write.Exec(`INSERT INTO monitor_triage_starts (target_id, started_at) VALUES ('t1', ?)`, t0.Add(-ago).UnixMilli())
		require.NoError(t, err)
	}
	tr := newFakeTriager(db)
	tr.result = func(string) TriageResult { return TriageResult{Status: TriageRunSucceeded, Report: "DISMISS"} }
	m := startTriageMonitorAt(t, db, tr, clock.now)
	frames := watchIncidents(t, m)
	untilBookmark(t, frames, incidentBookmark)

	queueAutomatic(t, m, "i1", testModel)
	assert.Equal(t, TriageStateDone, untilTriage(t, frames, "i1", TriageStateDone).State, "the sixth runs")
	queueAutomatic(t, m, "i2", testModel)
	assert.Equal(t, TriageCodeCapReached, untilTriage(t, frames, "i2", TriageStateFailed).Error, "the seventh does not")
	assert.Equal(t, t0.UnixMilli(), alertedAt(t, db, "i2"), "it alerts untriaged")
	assert.Equal(t, 6, count(t, db, `SELECT count(*) FROM monitor_triage_starts`), "the refused one took no slot")
	_, err := m.Triage(t.Context(), "i3", testModel, false)
	require.NoError(t, err)
	assert.Equal(t, TriageStateDone, untilTriage(t, frames, "i3", TriageStateDone).State, "the user's own is not capped")

	clock.advance(11 * time.Minute)
	queueAutomatic(t, m, "i4", testModel)
	assert.Equal(t, TriageStateDone, untilTriage(t, frames, "i4", TriageStateDone).State, "a start leaves the hour")
}

// An automatic triage that cannot start, as for a model whose provider was
// signed out, fails and alerts its incident; a manual one only fails.
func TestAnAutomaticTriageThatCannotStartAlerts(t *testing.T) {
	tr := &fakeTriager{}
	m, db := startTriageMonitor(t, tr)
	*tr = *newFakeTriager(db)
	tr.err = &TriageError{Code: TriageCodeModelUnavailable}
	insertIncident(t, db, "t1", "i2", "fake.flip", 1)
	frames := watchIncidents(t, m)
	untilBookmark(t, frames, incidentBookmark)

	queueAutomatic(t, m, "i1", testModel)
	assert.Equal(t, TriageCodeModelUnavailable, untilTriage(t, frames, "i1", TriageStateFailed).Error)
	_, err := m.Triage(t.Context(), "i2", testModel, false)
	require.NoError(t, err)
	untilTriage(t, frames, "i2", TriageStateFailed)

	assert.NotEqual(t, int64(-1), alertedAt(t, db, "i1"))
	assert.Equal(t, int64(-1), alertedAt(t, db, "i2"))
}

// An incident dismissed at first alerts once its third occurrence's triage
// answers attention, or fails; one that alerted keeps its first alert.
func TestAnIncidentDismissedAtFirstAlertsAtItsThird(t *testing.T) {
	db := appdbtest.Open(t)
	insertCluster(t, db)
	insertTriageTarget(t, db, "t1", "k1")
	for _, id := range []string{"attention", "failed", "alerted"} {
		insertIncident(t, db, "t1", id, "fake.crash", 3)
	}
	_, err := db.Write.Exec(`UPDATE incidents SET alerted_at = 1, alert_seq = 1 WHERE id = 'alerted'`)
	require.NoError(t, err)
	clock := &testClock{at: t0}
	tr := newFakeTriager(db)
	m := startTriageMonitorAt(t, db, tr, clock.now)
	frames := watchIncidents(t, m)
	untilBookmark(t, frames, incidentBookmark)

	answer := func(id IncidentID, res TriageResult, ends TriageState) {
		t.Helper()
		tr.result = func(string) TriageResult { return res }
		queueAutomatic(t, m, id, testModel)
		untilTriage(t, frames, id, ends)
	}
	dismiss := TriageResult{Status: TriageRunSucceeded, Report: "DISMISS"}
	answer("attention", dismiss, TriageStateDone)
	answer("failed", dismiss, TriageStateDone)
	assert.Equal(t, int64(-1), alertedAt(t, db, "attention"), "a dismiss raises nothing")

	clock.advance(time.Hour)
	answer("attention", TriageResult{Status: TriageRunSucceeded, Report: "ATTENTION"}, TriageStateDone)
	answer("failed", TriageResult{Status: TriageRunFailed}, TriageStateFailed)
	answer("alerted", TriageResult{Status: TriageRunSucceeded, Report: "ATTENTION"}, TriageStateDone)
	for _, id := range []IncidentID{"attention", "failed"} {
		assert.Equal(t, t0.Add(time.Hour).UnixMilli(), alertedAt(t, db, id), id)
	}
	assert.Equal(t, int64(1), alertedAt(t, db, "alerted"), "an incident alerts once")
}

// An automatic triage the stop cuts off is left queued, and the next start
// fails it and alerts its incident then.
func TestAnAutomaticTriageCutOffByTheStopAlertsAtTheNextStart(t *testing.T) {
	db := appdbtest.Open(t)
	insertCluster(t, db)
	insertTriageTarget(t, db, "t1", "k1")
	insertIncident(t, db, "t1", "i1", "fake.crash", 1)
	tr := newFakeTriager(db)
	tr.gate = make(chan struct{})
	m, err := newMonitor(db, wallClock, time.Hour, Registration{
		Name: "fake", Cadence: time.Hour, Signals: []Signal{crash}, Integration: newFake(Target{Key: "k1", Label: "Target k1"}), Triager: tr,
	})
	require.NoError(t, err)
	stop, err := m.Start(t.Context())
	require.NoError(t, err)
	queueAutomatic(t, m, "i1", testModel)
	testutil.Recv(t, tr.calls, "the run under way")
	require.NoError(t, stop(context.Background()))
	require.NoError(t, m.Close())
	assert.Equal(t, "queued", stateOf(t, db, "i1"), "the stop wrote no outcome")
	assert.Equal(t, int64(-1), alertedAt(t, db, "i1"))

	again := startTriageMonitorOn(t, db, newFakeTriager(db))
	assert.Equal(t, TriageCodeInterrupted, triageOf(t, again, "i1").Error)
	assert.NotEqual(t, int64(-1), alertedAt(t, db, "i1"))
}

// An automatic triage whose cut surfaces as an error of its own, not a cancelled
// run, is left queued all the same: the stop writes no outcome and no alert.
func TestAnAutomaticTriageFailedByTheStopIsLeftQueued(t *testing.T) {
	db := appdbtest.Open(t)
	insertCluster(t, db)
	insertTriageTarget(t, db, "t1", "k1")
	insertIncident(t, db, "t1", "i1", "fake.crash", 1)
	tr := cutTriager{started: make(chan struct{})}
	m, err := newMonitor(db, wallClock, time.Hour, Registration{
		Name: "fake", Cadence: time.Hour, Signals: []Signal{crash}, Integration: newFake(Target{Key: "k1", Label: "Target k1"}), Triager: tr,
	})
	require.NoError(t, err)
	stop, err := m.Start(t.Context())
	require.NoError(t, err)
	queueAutomatic(t, m, "i1", testModel)
	testutil.Recv(t, tr.started, "the triage under way")
	require.NoError(t, stop(context.Background()))
	require.NoError(t, m.Close())

	assert.Equal(t, "queued", stateOf(t, db, "i1"), "the stop wrote no outcome")
	assert.Equal(t, int64(-1), alertedAt(t, db, "i1"))
}

// cutTriager is a Triager whose triage waits for its context to end, then answers
// the bare error, as a read the stop cancelled does.
type cutTriager struct{ started chan struct{} }

func (c cutTriager) Triage(ctx context.Context, _ Target, _, _ string, _ TriageModel, _ Admit) (TriageResult, error) {
	c.started <- struct{}{}
	<-ctx.Done()
	return TriageResult{}, fmt.Errorf("read settings: %w", ctx.Err())
}

// stateOf is incident id's stored triage_state.
func stateOf(t *testing.T, db *appdb.DB, id string) string {
	t.Helper()
	var state string
	require.NoError(t, db.Read.QueryRow(`SELECT triage_state FROM incidents WHERE id = ?`, id).Scan(&state))
	return state
}

// A ticked triage writes the setting with its queue, as the user's triage; one
// the mutation refuses leaves the setting unset.
func TestATickedTriageWritesTheSettingWithIt(t *testing.T) {
	tr := &fakeTriager{}
	m, db := startTriageMonitor(t, tr)
	*tr = *newFakeTriager(db)
	tr.gate = make(chan struct{})
	t.Cleanup(func() { close(tr.gate) })
	insertIncident(t, db, "t1", "i2", "fake.flip", 1)
	_, err := m.Triage(t.Context(), "i1", testModel, false)
	require.NoError(t, err)

	_, err = m.Triage(t.Context(), "i1", testModel, true)
	assert.ErrorIs(t, err, ErrTriageQueued)
	model, err := m.store.triageModel(t.Context(), m.store.set.Stmts())
	require.NoError(t, err)
	assert.Nil(t, model, "a refused submission writes no setting")

	settings := db.ListenForChanges(appdb.KeySettings)
	defer settings.Close()
	_, err = m.Triage(t.Context(), "i2", testModel, true)
	require.NoError(t, err)
	testutil.Recv(t, settings.Chan(), "the setting's notice")
	model, err = m.store.triageModel(t.Context(), m.store.set.Stmts())
	require.NoError(t, err)
	assert.Equal(t, &testModel, model)
	var trigger string
	require.NoError(t, db.Read.QueryRow(`SELECT triage_trigger FROM incidents WHERE id = 'i2'`).Scan(&trigger))
	assert.Equal(t, "manual", trigger)
	_, err = m.Triage(t.Context(), "i2", TriageModel{}, true)
	assert.ErrorIs(t, err, ErrTriageQueued, "the queue's checks come first")
	insertIncident(t, db, "t1", "i3", "fake.flip", 1)
	_, err = m.Triage(t.Context(), "i3", TriageModel{ProviderID: "fake"}, true)
	assert.ErrorIs(t, err, ErrInvalidTriageModel)
	assert.Equal(t, TriageStateNone, triageOf(t, m, "i3").State, "a refused model queues nothing")
}
