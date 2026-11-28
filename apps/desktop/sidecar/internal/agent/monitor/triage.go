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
	"log/slog"
	"strings"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/appdb"
)

// triageWorkers is how many targets are triaged at once. One target's triages
// run in turn, since a cluster takes one monitor run at a time.
const triageWorkers = 4

// Triager runs one triage of a target: a model's run over the target that reads
// it and changes nothing. An integration that can triage sets one on its
// Registration.
type Triager interface {
	// Triage runs one triage of t, reading the server uid names and no other, over
	// brief on model. admit decides, inside the transaction that inserts the run,
	// whether it starts. A Go error is a triage no run was inserted for: a
	// *TriageError says why.
	Triage(ctx context.Context, t Target, uid, brief string, model TriageModel, admit Admit) (TriageResult, error)
}

// TriageModel is the model the user picked for a triage, by the ids the catalog
// names it with.
type TriageModel struct {
	ProviderID, ModelID, Effort string
}

// Admit decides, inside the transaction tx that inserted run runID, whether the
// run may start, and links it to its triage when it may.
type Admit func(ctx context.Context, tx *sql.Tx, runID string) (bool, error)

// TriageRunStatus is how a triage's run ended.
type TriageRunStatus string

const (
	TriageRunSucceeded TriageRunStatus = "succeeded"
	TriageRunFailed    TriageRunStatus = "failed"
	TriageRunCancelled TriageRunStatus = "cancelled"
)

// TriageResult is how a triage's run ended: its status, its report (the run's
// last reply, set when it succeeded) and, for a failed run, why, "" for a
// failure the triager cannot name. The run is linked to its triage by the admit.
type TriageResult struct {
	Status TriageRunStatus
	Report string
	Code   TriageErrorCode
}

// TriageErrorCode is why a triage failed or could not start. The codes are the
// core's, and a triager maps its own failures onto them.
type TriageErrorCode string

const (
	TriageCodeMonitoringOff    TriageErrorCode = "monitoring_off"    // the target was switched off since the triage was asked for
	TriageCodeServerChanged    TriageErrorCode = "server_changed"    // the target reaches another server than the incident opened on
	TriageCodeTargetGone       TriageErrorCode = "target_gone"       // the target is gone or going
	TriageCodeNoSandbox        TriageErrorCode = "no_sandbox"        // no sandbox confines a run on this machine
	TriageCodeModelUnavailable TriageErrorCode = "model_unavailable" // the model picked is not offered now
	TriageCodeRunFailed        TriageErrorCode = "run_failed"        // the run failed: a provider's error, among others
	TriageCodeCancelled        TriageErrorCode = "cancelled"         // the run was cancelled, as the app stopped
	TriageCodeInterrupted      TriageErrorCode = "interrupted"       // the app stopped before the triage ended
	TriageCodeUnavailable      TriageErrorCode = "unavailable"       // the run could not start for another reason
)

// TriageError is a triage that could not start, and its code.
type TriageError struct {
	Code TriageErrorCode
	Err  error
}

func (e *TriageError) Error() string {
	if e.Err == nil {
		return "monitor: triage " + string(e.Code)
	}
	return "monitor: triage " + string(e.Code) + ": " + e.Err.Error()
}

func (e *TriageError) Unwrap() error { return e.Err }

// TriageState is where an incident's newest triage is.
type TriageState string

const (
	TriageStateNone   TriageState = "none"   // never asked for
	TriageStateQueued TriageState = "queued" // waiting, or its run under way
	TriageStateDone   TriageState = "done"   // its run reported, with a verdict
	TriageStateFailed TriageState = "failed" // it could not run or its run failed
)

// TriageVerdict is what a done triage's report opens with.
type TriageVerdict string

const (
	TriageVerdictAttention TriageVerdict = "attention"
	TriageVerdictDismiss   TriageVerdict = "dismiss"
)

// IncidentTriage is an incident's newest triage as served: its state, verdict
// and error, "" where they do not apply, the run's report once the triage has
// ended, and the run's model once it is inserted.
type IncidentTriage struct {
	State                       TriageState
	Verdict                     TriageVerdict
	Error                       TriageErrorCode
	Report                      string
	ProviderID, ModelID, Effort string
}

var (
	// ErrNoIncident is an incident id that names no incident the reads serve.
	ErrNoIncident = errors.New("monitor: no such incident")
	// ErrTriageQueued is a triage of an incident whose triage is still queued.
	ErrTriageQueued = errors.New("monitor: the incident's triage is under way")
	// ErrCannotTriage is a triage of an incident whose integration has no Triager.
	ErrCannotTriage = errors.New("monitor: the incident's integration cannot triage")
	// ErrTargetMoved is a triage of an incident opened under a UID its target no
	// longer has: the run would read another server.
	ErrTargetMoved = errors.New("monitor: the incident's target reaches another server")
)

// triageRequest is one triage queued: its own id, which the incident holds
// while it is the newest, the incident, and what its run is handed, read in the
// transaction that queued it.
type triageRequest struct {
	id          string
	incidentID  IncidentID
	targetID    string
	integration string
	generation  int64 // the target's when queued; the admit refuses another
	target      Target
	uid         string
	brief       string
	model       TriageModel
}

// Triage queues a triage of incident id on model and answers the incident as the
// watch then delivers it. ErrNoIncident for an incident the reads do not serve,
// ErrTriageQueued for one already queued, ErrCannotTriage for one whose
// integration cannot triage, ErrTargetMoved for one its target has moved off;
// ErrStopping once the monitor stops. The triage runs after the target's earlier
// ones, and only the user's press calls this.
func (m *Monitor) Triage(ctx context.Context, id IncidentID, model TriageModel) (Incident, error) {
	if err := m.enter(); err != nil {
		return Incident{}, err
	}
	defer m.wg.Done()
	req, err := m.store.queueTriage(ctx, id, appdb.NewID(), m.canTriage)
	if err != nil {
		return Incident{}, err
	}
	req.model = model
	// Read before the worker can take the request, so the answer is the triage as
	// queued, with no run of its own yet. The triage is queued whatever becomes of
	// the caller, so its answer is too.
	queued, err := m.store.servedIncident(context.WithoutCancel(ctx), id)
	m.triageMu.Lock()
	m.waiting[req.targetID] = append(m.waiting[req.targetID], req)
	m.triageMu.Unlock()
	m.triages.Add(req.targetID)
	return queued, err
}

// canTriage is whether integration has a Triager.
func (m *Monitor) canTriage(integration string) bool {
	reg, ok := m.byName[integration]
	return ok && reg.Triager != nil
}

// triageLoop takes targets off the queue until ctx ends, running each one's
// waiting triages in turn.
func (m *Monitor) triageLoop(ctx context.Context) {
	for {
		targetID, ok := m.triages.Next(ctx)
		if !ok {
			return
		}
		for {
			req, ok := m.nextTriage(targetID)
			if !ok || ctx.Err() != nil {
				break
			}
			m.runTriage(ctx, req)
		}
		m.triages.Done(targetID)
	}
}

// nextTriage takes the oldest triage waiting on targetID.
func (m *Monitor) nextTriage(targetID string) (triageRequest, bool) {
	m.triageMu.Lock()
	defer m.triageMu.Unlock()
	reqs := m.waiting[targetID]
	if len(reqs) == 0 {
		return triageRequest{}, false
	}
	if len(reqs) == 1 {
		delete(m.waiting, targetID)
	} else {
		m.waiting[targetID] = reqs[1:]
	}
	return reqs[0], true
}

// runTriage runs one triage through its integration's Triager and writes how it
// ended, unless a newer request holds the incident.
func (m *Monitor) runTriage(ctx context.Context, req triageRequest) {
	admit := func(ctx context.Context, tx *sql.Tx, runID string) (bool, error) {
		return m.store.admitTriage(ctx, tx, req, runID)
	}
	res, err := m.byName[req.integration].Triager.Triage(ctx, req.target, req.uid, req.brief, req.model, admit)
	if err != nil && ctx.Err() == nil {
		slog.Info("monitor: a triage did not start", "integration", req.integration, "err", err)
	}
	// The outcome is written as the monitor stops too. A triage the stop leaves
	// untaken stays queued, and the next start marks it interrupted.
	if err := m.store.finishTriage(context.WithoutCancel(ctx), req, outcomeOf(res, err)); err != nil { // coverage:ignore a write on the writer fails only on a closed pool, which outlives the monitor
		slog.Warn("monitor: a triage's outcome was not written; the next start fails it", "err", err)
	}
}

// triageOutcome is the state a triage ends in, with its verdict or its code.
type triageOutcome struct {
	state   TriageState
	verdict TriageVerdict
	code    TriageErrorCode
}

// outcomeOf is how a triage that answered res and err ends.
func outcomeOf(res TriageResult, err error) triageOutcome {
	if err != nil {
		if te, ok := errors.AsType[*TriageError](err); ok && te.Code != "" {
			return failedOutcome(te.Code)
		}
		return failedOutcome(TriageCodeUnavailable)
	}
	switch res.Status {
	case TriageRunSucceeded:
		return triageOutcome{state: TriageStateDone, verdict: verdictOf(res.Report)}
	case TriageRunCancelled:
		return failedOutcome(TriageCodeCancelled)
	}
	if res.Code != "" {
		return failedOutcome(res.Code)
	}
	return failedOutcome(TriageCodeRunFailed)
}

// failedOutcome is a triage that failed with code.
func failedOutcome(code TriageErrorCode) triageOutcome {
	return triageOutcome{state: TriageStateFailed, code: code}
}

// verdictOf is a report's verdict: dismiss when its first line is the word
// DISMISS, in any case and with the emphasis or punctuation a model may put
// around it, and attention for any other, so a report that says neither still
// asks for the user.
func verdictOf(report string) TriageVerdict {
	first, _, _ := strings.Cut(report, "\n")
	if strings.EqualFold(strings.Trim(first, " \t\r*_`.:!#"), "DISMISS") {
		return TriageVerdictDismiss
	}
	return TriageVerdictAttention
}
