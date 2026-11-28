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

// Package kubetriage is the Kubernetes integration's Triager: a triage is one
// monitor run of the chat service on the target's cluster. It is the one package
// of the monitor that imports chat.
package kubetriage

import (
	"context"
	"database/sql"
	"errors"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/agent/chat"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/agent/monitor"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/agent/monitor/kubesignals"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/apimeta"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/llm"
)

// Runs is the chat service's monitor run.
type Runs interface {
	RunMonitor(ctx context.Context, clusterID apimeta.ClusterID, serverUID string, target llm.Target, brief string, admit chat.Admit) (chat.MonitorResult, error)
}

// Models resolves the model a triage names, as the catalog offers it now.
type Models interface {
	Resolve(providerID, modelID, effort string) (llm.Target, error)
}

// New is the Triager over the chat service's runs and the models it resolves.
func New(runs Runs, models Models) monitor.Triager { return &triager{runs: runs, models: models} }

type triager struct {
	runs   Runs
	models Models
}

// Triage runs one monitor run on the cluster t's key names, reading the server
// uid alone.
func (tr *triager) Triage(ctx context.Context, t monitor.Target, uid, brief string, model monitor.TriageModel, admit monitor.Admit) (monitor.TriageResult, error) {
	target, err := tr.models.Resolve(model.ProviderID, model.ModelID, model.Effort)
	if err != nil {
		return monitor.TriageResult{}, &monitor.TriageError{Code: monitor.TriageCodeModelUnavailable, Err: err}
	}
	clusterID, _ := kubesignals.ClusterID(kubesignals.Name, t.Key) // a Kubernetes target's key is always its cluster's id
	res, err := tr.runs.RunMonitor(ctx, apimeta.ClusterID(clusterID), uid, target, brief,
		func(ctx context.Context, tx *sql.Tx, runID chat.RunID) (bool, error) {
			return admit(ctx, tx, string(runID))
		})
	if err != nil {
		return monitor.TriageResult{}, &monitor.TriageError{Code: codeOf(err), Err: err}
	}
	out := monitor.TriageResult{Report: res.Report}
	switch res.Status {
	case chat.RunSucceeded:
		out.Status = monitor.TriageRunSucceeded
	case chat.RunCancelled:
		out.Status = monitor.TriageRunCancelled
	default:
		out.Status = monitor.TriageRunFailed
		if res.ServerChanged {
			out.Code = monitor.TriageCodeServerChanged
		}
	}
	return out, nil
}

// codeOf is the core's code for a run chat would not start.
func codeOf(err error) monitor.TriageErrorCode {
	switch {
	case errors.Is(err, chat.ErrServerChanged), errors.Is(err, monitor.ErrTargetMoved):
		return monitor.TriageCodeServerChanged
	case errors.Is(err, chat.ErrClusterGone):
		return monitor.TriageCodeTargetGone
	case errors.Is(err, chat.ErrMonitoringOff):
		return monitor.TriageCodeMonitoringOff
	case errors.Is(err, chat.ErrNoSandbox):
		return monitor.TriageCodeNoSandbox
	case errors.Is(err, chat.ErrBadRequest):
		// A brief within the cap is never refused, so a model that takes no tools.
		return monitor.TriageCodeModelUnavailable
	case errors.Is(err, chat.ErrStopping), errors.Is(err, context.Canceled):
		return monitor.TriageCodeCancelled
	}
	return monitor.TriageCodeUnavailable
}
