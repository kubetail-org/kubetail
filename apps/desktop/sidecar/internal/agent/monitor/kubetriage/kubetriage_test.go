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

package kubetriage

import (
	"context"
	"database/sql"
	"errors"
	"go/build"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/agent/chat"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/agent/monitor"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/apimeta"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/llm"
)

// fakeRuns is the chat service's monitor run, answering res and err and keeping
// what it was asked.
type fakeRuns struct {
	res chat.MonitorResult
	err error

	clusterID apimeta.ClusterID
	serverUID string
	target    llm.Target
	brief     string
	admitted  bool
}

func (f *fakeRuns) RunMonitor(ctx context.Context, clusterID apimeta.ClusterID, serverUID string, target llm.Target, brief string, admit chat.Admit) (chat.MonitorResult, error) {
	f.clusterID, f.serverUID, f.target, f.brief = clusterID, serverUID, target, brief
	if f.err != nil {
		return chat.MonitorResult{}, f.err
	}
	ok, err := admit(ctx, nil, f.res.RunID)
	if err != nil {
		return chat.MonitorResult{}, err
	}
	if !ok {
		return chat.MonitorResult{}, chat.ErrMonitoringOff
	}
	f.admitted = true
	return f.res, nil
}

// fakeModels resolves the one model fake/fake-1.
type fakeModels struct{}

func (fakeModels) Resolve(providerID, modelID, effort string) (llm.Target, error) {
	if providerID != "fake" || modelID != "fake-1" {
		return llm.Target{}, llm.ErrUnknownModel
	}
	return llm.Target{Provider: llm.Provider{ID: providerID}, Model: llm.Model{ID: modelID}, Effort: effort}, nil
}

var model = monitor.TriageModel{ProviderID: "fake", ModelID: "fake-1", Effort: "low"}

// admitAll is an admit that admits, keeping the run id it was handed.
func admitAll(seen *string) monitor.Admit {
	return func(_ context.Context, _ *sql.Tx, runID string) (bool, error) {
		*seen = runID
		return true, nil
	}
}

// A triage is one monitor run on the cluster the target's key names, at the
// incident's server, over the brief on the model picked, its admit the core's.
func TestATriageIsAMonitorRunOnTheTargetsCluster(t *testing.T) {
	runs := &fakeRuns{res: chat.MonitorResult{RunID: "r1", Status: chat.RunSucceeded, Report: "DISMISS\nok"}}
	var seen string

	res, err := New(runs, fakeModels{}).Triage(t.Context(), monitor.Target{Key: "7"}, "uid-1", "look", model, admitAll(&seen))

	require.NoError(t, err)
	assert.Equal(t, monitor.TriageResult{Status: monitor.TriageRunSucceeded, Report: "DISMISS\nok"}, res)
	assert.Equal(t, apimeta.ClusterID("7"), runs.clusterID)
	assert.Equal(t, "uid-1", runs.serverUID)
	assert.Equal(t, "look", runs.brief)
	assert.Equal(t, "fake-1", runs.target.Model.ID)
	assert.Equal(t, "low", runs.target.Effort)
	assert.Equal(t, "r1", seen, "the core's admit is handed the run")
}

// Each way a run ends reads as the core's status, a failed run's code with it.
func TestARunsEndIsTheCoresStatus(t *testing.T) {
	for name, c := range map[string]struct {
		res  chat.MonitorResult
		want monitor.TriageResult
	}{
		"failed":         {chat.MonitorResult{RunID: "r1", Status: chat.RunFailed, Error: "provider"}, monitor.TriageResult{Status: monitor.TriageRunFailed}},
		"cancelled":      {chat.MonitorResult{RunID: "r1", Status: chat.RunCancelled}, monitor.TriageResult{Status: monitor.TriageRunCancelled}},
		"server changed": {chat.MonitorResult{RunID: "r1", Status: chat.RunFailed, ServerChanged: true}, monitor.TriageResult{Status: monitor.TriageRunFailed, Code: monitor.TriageCodeServerChanged}},
	} {
		var seen string
		res, err := New(&fakeRuns{res: c.res}, fakeModels{}).Triage(t.Context(), monitor.Target{Key: "7"}, "uid-1", "look", model, admitAll(&seen))
		require.NoError(t, err, name)
		assert.Equal(t, c.want, res, name)
	}
}

// A run that could not start is a TriageError with the core's code, whatever
// chat called it; a model no longer offered starts none.
func TestARunThatCouldNotStartCarriesItsCode(t *testing.T) {
	for err, want := range map[error]monitor.TriageErrorCode{
		chat.ErrServerChanged:   monitor.TriageCodeServerChanged,
		monitor.ErrTargetMoved:  monitor.TriageCodeServerChanged,
		chat.ErrClusterGone:     monitor.TriageCodeTargetGone,
		chat.ErrMonitoringOff:   monitor.TriageCodeMonitoringOff,
		chat.ErrNoSandbox:       monitor.TriageCodeNoSandbox,
		chat.ErrBadRequest:      monitor.TriageCodeModelUnavailable,
		chat.ErrStopping:        monitor.TriageCodeCancelled,
		context.Canceled:        monitor.TriageCodeCancelled,
		chat.ErrMonitorInFlight: monitor.TriageCodeUnavailable,
		errors.New("disk"):      monitor.TriageCodeUnavailable,
	} {
		var seen string
		_, got := New(&fakeRuns{err: err}, fakeModels{}).Triage(t.Context(), monitor.Target{Key: "7"}, "uid-1", "look", model, admitAll(&seen))
		te, ok := errors.AsType[*monitor.TriageError](got)
		require.True(t, ok, err.Error())
		assert.Equal(t, want, te.Code, err.Error())
		assert.ErrorIs(t, got, err)
	}

	runs := &fakeRuns{}
	var seen string
	_, err := New(runs, fakeModels{}).Triage(t.Context(), monitor.Target{Key: "7"}, "uid-1", "look",
		monitor.TriageModel{ProviderID: "gone", ModelID: "x"}, admitAll(&seen))
	te, ok := errors.AsType[*monitor.TriageError](err)
	require.True(t, ok)
	assert.Equal(t, monitor.TriageCodeModelUnavailable, te.Code)
	assert.Empty(t, runs.brief, "no run is asked for")
}

// A triage queued before its context moved takes no run, though no fold has yet
// moved the target's UID: the run reads the server the incident opened on, which
// RunMonitor refuses to insert for once its card names another, and the triage
// fails as server_changed.
func TestATriageReadsNoOtherServer(t *testing.T) {
	runs := &fakeRuns{err: chat.ErrServerChanged}
	var seen string

	_, err := New(runs, fakeModels{}).Triage(t.Context(), monitor.Target{Key: "7"}, "uid-1", "look", model, admitAll(&seen))

	assert.Equal(t, "uid-1", runs.serverUID, "the incident's UID, not the target's current one")
	te, ok := errors.AsType[*monitor.TriageError](err)
	require.True(t, ok)
	assert.Equal(t, monitor.TriageCodeServerChanged, te.Code)
	assert.Empty(t, seen, "nothing was admitted")
}

// The core's brief fits the monitor run's cap, so a brief at its bound is one
// RunMonitor takes.
func TestTheBriefFitsTheMonitorRun(t *testing.T) {
	assert.LessOrEqual(t, monitor.MaxTriageBriefLen, chat.MaxMonitorBriefLen)
}

// The core imports no chat and kubesignals imports none: kubetriage is the one
// package of the monitor that does.
func TestOnlyKubetriageImportsChat(t *testing.T) {
	const chatPkg = "github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/agent/chat"
	for _, dir := range []string{"..", "../kubesignals"} {
		pkg, err := build.ImportDir(dir, 0)
		require.NoError(t, err)
		assert.False(t, slices.Contains(pkg.Imports, chatPkg), dir)
	}
}
