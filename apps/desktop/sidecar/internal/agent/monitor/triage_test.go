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
	db     *appdb.DB
	asked  chan struct{}   // a send per triage asked for, before its admit
	calls  chan triageCall // a send per triage admitted
	gate   chan struct{}
	result func(runID string) TriageResult
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
	if f.gate != nil {
		<-f.gate
	}
	res := f.result(runID)
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
	_, err := db.Write.Exec(`INSERT INTO clusters (id, source, created_at, updated_at) VALUES ('c1', 'kubeconfig', 0, 0)`)
	require.NoError(t, err)
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
	fake := newFake(Target{Key: "k1", Label: "Target k1"}, Target{Key: "k2", Label: "Target k2"})
	fake.read = func(context.Context, Target) (Reading, error) { return Reading{}, errors.New("unread") }
	m, err := newMonitor(db, wallClock, time.Hour, Registration{
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

	queued, err := m.Triage(t.Context(), "i1", testModel)
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

	_, err := m.Triage(t.Context(), "i1", testModel)
	require.NoError(t, err)

	got := untilTriage(t, frames, "i1", TriageStateFailed)
	assert.Equal(t, TriageCodeRunFailed, got.Error)
	assert.True(t, runLinked(t, db, "i1").Valid)
	_, err = m.Triage(t.Context(), "i1", testModel)
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

	_, err = m.Triage(t.Context(), "i1", testModel)
	require.NoError(t, err)
	testutil.Recv(t, tr.asked, "the first triage asked")
	testutil.Recv(t, tr.calls, "the first triage")
	_, err = m.Triage(t.Context(), "i2", testModel)
	require.NoError(t, err)
	_, err = m.Triage(t.Context(), "i3", testModel)
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

	_, err = m.Triage(t.Context(), "i1", testModel)
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

	_, err := m.Triage(t.Context(), "i1", testModel)
	require.NoError(t, err)
	testutil.Recv(t, tr.calls, "the first triage")
	_, err = m.Triage(t.Context(), "i2", testModel)
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

	_, err := m.Triage(t.Context(), "i1", testModel)
	require.NoError(t, err)
	testutil.Recv(t, tr.calls, "the first triage")
	_, err = m.Triage(t.Context(), "i2", testModel)
	require.NoError(t, err)
	_, err = db.Write.Exec(`UPDATE monitor_targets SET current_uid = 'u2' WHERE id = 't1'`)
	require.NoError(t, err)
	close(tr.gate)

	untilTriage(t, frames, "i2", TriageStateFailed)
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

	_, err := m.Triage(t.Context(), "i1", testModel)
	require.NoError(t, err)
	testutil.Recv(t, tr.calls, "the first triage")
	_, err = m.Triage(t.Context(), "i2", testModel)
	require.NoError(t, err)
	_, err = m.SetEnabled(t.Context(), "t1", false)
	require.NoError(t, err)
	_, err = m.SetEnabled(t.Context(), "t1", true)
	require.NoError(t, err)
	close(tr.gate)

	got := untilTriage(t, frames, "i2", TriageStateFailed)
	assert.Equal(t, TriageCodeMonitoringOff, got.Error)

	_, err = m.Triage(t.Context(), "i2", testModel)
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

	_, err := m.Triage(t.Context(), "nope", testModel)
	assert.ErrorIs(t, err, ErrNoIncident)

	_, err = m.Triage(t.Context(), "i1", testModel)
	require.NoError(t, err)
	_, err = m.Triage(t.Context(), "i1", testModel)
	assert.ErrorIs(t, err, ErrTriageQueued)

	insertIncident(t, db, "t1", "i2", "fake.flip", 1)
	_, err = db.Write.Exec(`UPDATE monitor_targets SET current_uid = 'u2' WHERE id = 't1'`)
	require.NoError(t, err)
	_, err = m.Triage(t.Context(), "i2", testModel)
	assert.ErrorIs(t, err, ErrTargetMoved)
	assert.Equal(t, TriageStateNone, triageOf(t, m, "i2").State)

	other := insertOther(t, db)
	_, err = m.Triage(t.Context(), other, testModel)
	assert.ErrorIs(t, err, ErrNoIncident, "an integration the build lacks serves no incident")

	// The rows land before the start, whose first reconcile lists k1 too.
	noneDB := appdbtest.Open(t)
	insertTriageTarget(t, noneDB, "t1", "k1")
	insertIncident(t, noneDB, "t1", "i1", "fake.crash", 1)
	none := startTriageMonitorOn(t, noneDB, nil)
	_, err = none.Triage(t.Context(), "i1", testModel)
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

	_, err := m.Triage(t.Context(), "i1", testModel)
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
	_, err := m.Triage(t.Context(), "i1", testModel)
	require.NoError(t, err)
	untilTriage(t, frames, "i1", TriageStateDone)
	tr.gate = make(chan struct{})
	t.Cleanup(func() { close(tr.gate) })

	again, err := m.Triage(t.Context(), "i1", testModel)
	require.NoError(t, err)

	assert.Equal(t, IncidentTriage{State: TriageStateQueued}, again.Triage)
}

// A worker holding a request the incident no longer has is refused at its admit
// and writes no outcome over the newer request.
func TestAWorkerThatOutlivedItsRequestWritesNothing(t *testing.T) {
	tr := &fakeTriager{}
	m, db := startTriageMonitor(t, tr)
	*tr = *newFakeTriager(db)
	req, err := m.store.queueTriage(t.Context(), "i1", "r1", func(string) bool { return true })
	require.NoError(t, err)
	_, err = db.Write.Exec(`UPDATE incidents SET triage_request_id = 'r2' WHERE id = 'i1'`)
	require.NoError(t, err)

	tx, err := db.Write.BeginTx(t.Context(), nil)
	require.NoError(t, err)
	_, err = tx.Exec(`INSERT INTO agent_runs (id, agent_type, app_version, trigger, cluster_id, provider, model, dialect, created_at)
		VALUES ('run1', 'monitor', 'test', 'monitor', 'c1', 'fake', 'fake', 'fake', 0)`)
	require.NoError(t, err)
	ok, err := m.store.admitTriage(t.Context(), tx, req, "run1")
	require.NoError(t, err)
	assert.False(t, ok)
	require.NoError(t, tx.Rollback())

	require.NoError(t, m.store.finishTriage(t.Context(), req, triageOutcome{state: TriageStateDone, verdict: TriageVerdictDismiss}))
	var state, requestID string
	require.NoError(t, db.Read.QueryRow(`SELECT triage_state, triage_request_id FROM incidents WHERE id = 'i1'`).Scan(&state, &requestID))
	assert.Equal(t, []string{"queued", "r2"}, []string{state, requestID})
}

// A start marks a triage still queued failed, interrupted, with its run linked.
func TestAStrandedTriageIsFailedAtStart(t *testing.T) {
	db := appdbtest.Open(t)
	_, err := db.Write.Exec(`INSERT INTO clusters (id, source, created_at, updated_at) VALUES ('c1', 'kubeconfig', 0, 0)`)
	require.NoError(t, err)
	_, err = db.Write.Exec(`INSERT INTO agent_runs (id, agent_type, app_version, trigger, cluster_id, provider, model, dialect, created_at)
		VALUES ('run1', 'monitor', 'test', 'monitor', 'c1', 'fake', 'fake', 'fake', 0)`)
	require.NoError(t, err)
	insertTriageTarget(t, db, "t1", "k1")
	insertIncident(t, db, "t1", "i1", "fake.crash", 1)
	_, err = db.Write.Exec(`UPDATE incidents SET triage_state = 'queued', triage_request_id = 'r1', triage_run_id = 'run1' WHERE id = 'i1'`)
	require.NoError(t, err)

	m := startTriageMonitorOn(t, db, newFakeTriager(db))

	got := triageOf(t, m, "i1")
	assert.Equal(t, TriageStateFailed, got.State)
	assert.Equal(t, TriageCodeInterrupted, got.Error)
	assert.Equal(t, "run1", runLinked(t, db, "i1").String)
}

// An incident that opens starts no triage: only the mutation does.
func TestNoTriageStartsUnasked(t *testing.T) {
	db := appdbtest.Open(t)
	tr := newFakeTriager(db)
	fake := newFake(Target{Key: "k1", Label: "Target k1"})
	m, err := newMonitor(db, wallClock, time.Hour, Registration{
		Name: "fake", Cadence: loopCadence, Signals: []Signal{crash, flip}, Integration: fake, Triager: tr,
	})
	require.NoError(t, err)
	stop, err := m.Start(t.Context())
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, stop(context.Background()))
		require.NoError(t, m.Close())
	})

	require.Eventually(t, func() bool {
		var n int
		require.NoError(t, db.Read.QueryRow(`SELECT count(*) FROM incidents`).Scan(&n))
		return n > 0
	}, testutil.Timeout, loopCadence)

	// A negative assertion over a few cadences of readings.
	select {
	case <-tr.asked:
		t.Fatal("an incident started a triage")
	case <-time.After(5 * loopCadence):
	}
}
