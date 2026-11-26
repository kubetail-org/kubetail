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

// The monitor run: one run of the chat's loop under a cluster and no chat, in a
// session that reads the cluster and changes nothing, recorded as a turn's is.
package chat

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"strings"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/agent/loop"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/apimeta"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/version"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/llm"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/run/permissions"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/tools"
)

const (
	// MaxMonitorToolCalls is what one monitor run may ask for, as a subagent may.
	MaxMonitorToolCalls = MaxSubagentToolCalls
	// maxMonitorBriefLen caps a monitor's brief, in bytes.
	maxMonitorBriefLen = 4000
)

// MonitorResult is how a monitor run ended: its row, its status, the report
// (the text of its last reply, empty unless it succeeded) and its error text.
type MonitorResult struct {
	RunID  RunID
	Status RunStatus // RunSucceeded, RunFailed or RunCancelled
	Report string
	Error  string
}

// RunMonitor takes one monitor run on clusterID: a run of target over brief, on
// the calling goroutine, settled when it returns. ErrNoSandbox where no sandbox
// confines a run; ErrClusterGone for a cluster marked or gone; ErrMonitoringOff
// for one whose monitoring is off; ErrMonitorInFlight while the cluster has a
// run; ErrBadRequest for an empty brief, one over maxMonitorBriefLen, or a
// target that takes no tools; ErrStopping once the service is stopping. Once the
// run is inserted its rows are written whatever it answers.
func (s *service) RunMonitor(ctx context.Context, clusterID apimeta.ClusterID, target llm.Target, brief string) (MonitorResult, error) {
	// A monitor never runs unconfined, and a machine with no sandbox has
	// nothing to confine it.
	if !s.sandboxState.Available {
		return MonitorResult{}, ErrNoSandbox
	}
	brief = strings.TrimSpace(brief)
	off, err := disabledTools(ctx, s.store.Stmts())
	if err != nil {
		return MonitorResult{}, err
	}
	box := s.boxFor(target, off)
	if brief == "" || len(brief) > maxMonitorBriefLen || box.Empty() {
		return MonitorResult{}, ErrBadRequest
	}
	if err := s.enter(); err != nil {
		return MonitorResult{}, err
	}
	defer s.wg.Done()

	run := agentRun{
		ID: newRunID(), ClusterID: clusterID, ProviderID: target.Provider.ID, ModelID: target.Model.ID,
		Effort: target.Effort, Dialect: target.Provider.Dialect, Task: brief,
		AppVersion: version.Version, CreatedAt: normalizeTime(s.now()),
	}
	m, err := s.reserveMonitor(ctx, clusterID, run.ID, target)
	if err != nil {
		return MonitorResult{}, err
	}
	defer s.releaseMonitor(clusterID, m)
	// The run ends with the service too.
	defer context.AfterFunc(s.ctx, m.cancel)()

	err = s.store.InTx(m.ctx, func(st stmts) error {
		// Inside the transaction, so a run and a cluster's mark are serialized
		// as a send and a mark are.
		found, enabled, err := clusterMonitoring(m.ctx, st, clusterID)
		switch {
		case err != nil:
			return err
		case !found:
			return ErrClusterGone
		case !enabled:
			return ErrMonitoringOff
		}
		return insertMonitorRun(m.ctx, st, run)
	})
	if err != nil {
		return MonitorResult{}, err
	}

	// The card alone: a send's card carries the cluster's memory notes, and a
	// note the user wrote reads to the model as a request nobody is here to
	// stand behind.
	cardCtx, cancelCard := context.WithTimeout(m.ctx, s.clusterCardTimeout)
	card := s.clusterCards.ClusterCard(cardCtx, clusterID)
	cancelCard()

	spec := loop.Turn{
		Target: target, SystemPrompt: MonitorSystemPrompt(),
		Messages:    []llm.Message{{Role: string(RoleUser), Blocks: subagentMessage(card, brief)}},
		AffinityKey: string(run.ID),
		Tools:       MonitorBox(box),
		Runtime: tools.Runtime{
			ClusterID: clusterID, Session: monitorSession(),
			Dir: s.monitorDir(clusterID), Tasks: monitorTasks{}, Files: runStamps{},
			ActionAsker: monitorAsker{j: m.runJournal},
		},
		MaxToolCalls: MaxMonitorToolCalls, DefaultToolTimeout: defaultToolTimeout,
	}
	m.loop(m.ctx, spec, m)
	m.settle()
	return MonitorResult{RunID: run.ID, Status: m.status, Report: m.report, Error: m.errText}, nil
}

// MonitorBox is what a monitor run is offered of the chat's box: the tools that
// read and change the cluster alone. No Agent, no Memory, no WebFetch or search,
// since nothing it reads should leave the machine or outlive the run, no
// TaskStop, since it starts no background task, and no LogsView or LogsWatch,
// since nobody is looking or waiting.
func MonitorBox(box tools.Box) tools.Box {
	return box.Without(tools.ActionDelegate, tools.ActionMemory, tools.ActionFetch, tools.ActionStop, tools.ActionSearch,
		tools.ActionLogsView, tools.ActionLogsWatch)
}

// monitor is a cluster's monitor run: the loop.Recorder and Approver of one
// run, and the slot the service holds it in. ctx is the caller's, ended by
// cancel: the service's stop and the sweeper call it. done closes once the run
// has settled and released.
type monitor struct {
	briefedRun
	ctx    context.Context
	cancel context.CancelFunc
	done   chan struct{}
}

// reserveMonitor places a new monitor in the cluster's slot, refusing a cluster
// that already holds one. The check and the reservation are one critical
// section, so two runs cannot both pass.
func (s *service) reserveMonitor(ctx context.Context, clusterID apimeta.ClusterID, runID RunID, target llm.Target) (*monitor, error) {
	s.turnsMu.Lock()
	defer s.turnsMu.Unlock()
	if _, held := s.monitors[clusterID]; held {
		return nil, ErrMonitorInFlight
	}
	m := &monitor{done: make(chan struct{})}
	// Nothing watches a monitor run, so it publishes nothing.
	m.runJournal = &runJournal{s: s, runID: runID, target: target, publish: func(MessageStatus) {}}
	m.ctx, m.cancel = context.WithCancel(ctx)
	s.monitors[clusterID] = m
	return m, nil
}

// releaseMonitor empties the cluster's slot and says the run has ended.
func (s *service) releaseMonitor(clusterID apimeta.ClusterID, m *monitor) {
	s.turnsMu.Lock()
	delete(s.monitors, clusterID)
	s.turnsMu.Unlock()
	m.cancel()
	close(m.done)
}

// Approve answers no and writes nothing: nobody is there to ask. The loop
// answers the call denied, and its one row says it never started. The calls
// that reach it are file calls outside the workspace: a sandboxed command
// skips the gate, and one asking for network is refused before it.
func (m *monitor) Approve(context.Context, llm.Block, tools.Approval) (bool, error) {
	return false, nil
}

// settle writes the run's end and every one of its rows whole again, in one
// transaction that outlives the run's cancel. A settle the store refuses is
// logged and left: the next Start fails the run as stranded.
func (m *monitor) settle() {
	at := normalizeTime(m.s.now())
	ctx := context.WithoutCancel(m.s.ctx)
	err := m.s.store.InTx(ctx, func(st stmts) error {
		if err := m.writeRun(ctx, st, at); err != nil {
			return err
		}
		return m.writeRows(ctx, st, at)
	})
	if err != nil {
		slog.Warn("a monitor run did not settle; the next start fails it", "run", m.runID, "err", err)
	}
}

// errMonitorAsked is an ask that reached a monitor run, which has nobody to ask.
var errMonitorAsked = errors.New("chat: a monitor run asks nobody")

// monitorAsker records what the proxy decided and never asks. Ask is unreachable,
// since the monitor's principal asks nobody, which turns every verdict the user
// could lift into a refusal; an Ask that reached it anyway answers an error,
// which the proxy refuses the request on and forwards nothing.
type monitorAsker struct{ j *runJournal }

func (a monitorAsker) Ask(context.Context, tools.ActionRequest) (tools.Answer, error) {
	return tools.Answer{}, errMonitorAsked
}

func (a monitorAsker) Record(ctx context.Context, r tools.ActionRequest, d permissions.Decision, reason string) error {
	return a.j.recordAction(ctx, r, d, reason)
}

// errNoMonitorTask is a background start in a monitor run.
var errNoMonitorTask = errors.New("the monitor runs no background command")

// monitorTasks is a monitor run's tools.Tasks: it starts none, so no task row
// or notice exists for a monitor.
type monitorTasks struct{}

func (monitorTasks) Start(func(*os.File) (tools.Task, error)) (string, string, error) {
	return "", "", errNoMonitorTask
}

func (monitorTasks) Stop(string) bool { return false }
