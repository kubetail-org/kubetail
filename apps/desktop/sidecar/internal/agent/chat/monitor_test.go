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

package chat

import (
	"cmp"
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/appdb"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/apimeta"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/testutil"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/llm"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/run/permissions"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/run/sandbox"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/tools"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/tools/agent"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/tools/anthropicwebsearch"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/tools/logsread"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/tools/logsview"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/tools/read"
)

// kindTool is testTool under another action kind, so a box can hold a tool of
// each kind the monitor leaves out.
type kindTool struct {
	testTool
	kind tools.ActionKind
}

func (k kindTool) ActionKind() tools.ActionKind { return k.kind }

// startMonitorService is a started service on a machine with a sandbox, over a
// card source answering card, offering the tools given.
func startMonitorService(t *testing.T, card string, offered ...tools.Tool) *service {
	t.Helper()
	box, lists := testBox(offered...)
	cards := &stubClusterCards{}
	cards.set(card)
	s := startServiceWith(t, t.TempDir(), fakeLLM(), cards, box, lists)
	grantable(t, s)
	return s
}

// heldMonitor starts a monitor run on clusterID held at the fake's gate, with a
// file in its folder, and answers the gate and where the run's result arrives.
func heldMonitor(t *testing.T, s *service, clusterID apimeta.ClusterID) (chan struct{}, chan MonitorResult) {
	t.Helper()
	setMonitoring(t, s.db, clusterID, true)
	root, err := s.monitorDir(clusterID).Root(true)
	require.NoError(t, err)
	require.NoError(t, root.WriteFile("notes.txt", []byte("x"), 0o600))
	require.NoError(t, root.Close())
	asked := fakeOf(s).Asked()
	gate := make(chan struct{})
	fakeOf(s).SetGate(gate)
	done := make(chan MonitorResult, 1)
	go func() {
		res, err := s.RunMonitor(context.Background(), clusterID, fakeTarget(s), "look")
		assert.NoError(t, err)
		done <- res
	}()
	require.Eventually(t, func() bool { return fakeOf(s).Asked() == asked+1 }, testutil.Timeout, time.Millisecond)
	return gate, done
}

// monitorRunRow is what a monitor's run row stored.
type monitorRunRow struct {
	trigger, agentType, task, status string
	chatID, clusterID, result        sql.NullString
	started                          bool
}

func monitorRunOf(t *testing.T, db *appdb.DB, id RunID) monitorRunRow {
	t.Helper()
	var (
		r       monitorRunRow
		started sql.NullInt64
	)
	require.NoError(t, db.Read.QueryRow(`SELECT trigger, agent_type, task, status, chat_id, cluster_id, result, started_at
		FROM agent_runs WHERE id = ?`, string(id)).Scan(&r.trigger, &r.agentType, &r.task, &r.status, &r.chatID, &r.clusterID, &r.result, &started))
	r.started = started.Valid
	return r
}

// A monitor run is a run of the chat's loop under its cluster and no chat: its
// row, its model calls and its tool calls are recorded as a turn's are, it is
// told the card then the brief, it is offered none of the five kinds it leaves
// out, and its last reply is its report.
func TestRunMonitorRecordsARun(t *testing.T) {
	t.Parallel()

	var seen RunStatus
	var s *service
	echo := testTool{name: "echo", run: func(_ context.Context, input json.RawMessage) (string, bool) {
		require.NoError(t, s.db.Read.QueryRow(`SELECT status FROM agent_runs WHERE trigger = 'monitor'`).Scan(&seen))
		return string(input), false
	}}
	s = startMonitorService(t, "card-7", echo, agent.New(),
		kindTool{testTool{name: "Memory"}, tools.ActionMemory},
		kindTool{testTool{name: "WebFetch"}, tools.ActionFetch},
		kindTool{testTool{name: "TaskStop"}, tools.ActionStop})
	setMonitoring(t, s.db, "7", true)
	fakeOf(s).SetToolCalls(llm.StagedCall("echo", `{"x":1}`))

	res, err := s.RunMonitor(t.Context(), "7", fakeTarget(s), "look at the pods")
	require.NoError(t, err)

	assert.Equal(t, RunSucceeded, res.Status)
	assert.Equal(t, fakeSecondSentence, res.Report)
	assert.Empty(t, res.Error)
	assert.Equal(t, RunRunning, seen, "the first round claims the queued run")
	row := monitorRunOf(t, s.db, res.RunID)
	assert.Equal(t, monitorRunRow{
		trigger: "monitor", agentType: "monitor", task: "look at the pods", status: string(RunSucceeded),
		clusterID: sql.NullString{String: "7", Valid: true}, result: sql.NullString{String: fakeSecondSentence, Valid: true},
		started: true,
	}, row)
	assert.Len(t, llmCallRows(t, s.db, res.RunID), 2)
	calls := toolCallRows(t, s.db, res.RunID)
	require.Len(t, calls, 1)
	assert.Equal(t, "echo", calls[0].name)
	assert.Equal(t, "succeeded", calls[0].status)
	assert.Equal(t, 0, calls[0].llmCallSeq)

	req := fakeOf(s).LastRequest()
	assert.True(t, strings.HasPrefix(req.SystemPrompt, MonitorSystemPrompt()))
	require.Len(t, req.Messages, 2, "the brief, then the round")
	assert.Equal(t, []llm.Block{llm.ContextBlock("card-7"), llm.TextBlock("look at the pods")}, req.Messages[0].Blocks)
	assert.Equal(t, []string{"echo"}, offered(req))
	for _, n := range req.NativeTools {
		assert.NotEqual(t, anthropicwebsearch.Name, n.Tool.Name(), "no search")
	}
}

// A monitor run is refused before anything is written: on a machine with no
// sandbox, on a cluster marked or not watched, for a brief it cannot take or a
// model that takes no tools, and while the cluster already has one.
func TestRunMonitorRefuses(t *testing.T) {
	t.Parallel()

	for name, c := range map[string]struct {
		setup func(t *testing.T, s *service)
		brief string
		model string
		want  error
	}{
		"no sandbox": {
			setup: func(_ *testing.T, s *service) { s.sandboxState = sandbox.State{Reason: "no bwrap"} },
			want:  ErrNoSandbox,
		},
		"a marked cluster":      {setup: func(t *testing.T, s *service) { markCluster(t, s.db, "7") }, want: ErrClusterGone},
		"monitoring off":        {setup: func(t *testing.T, s *service) { setMonitoring(t, s.db, "7", false) }, want: ErrMonitoringOff},
		"an empty brief":        {brief: " \n", want: ErrBadRequest},
		"a brief too long":      {brief: strings.Repeat("x", maxMonitorBriefLen+1), want: ErrBadRequest},
		"a model with no tools": {model: "fake-no-tools", want: ErrBadRequest},
	} {
		t.Run(name, func(t *testing.T) {
			s := startMonitorService(t, "", testTool{name: "echo"})
			setMonitoring(t, s.db, "7", true)
			if c.setup != nil {
				c.setup(t, s)
			}
			brief := cmp.Or(c.brief, "look")
			target := fakeTarget(s)
			if c.model != "" {
				var err error
				target, err = s.llmSvc.Resolve("fake", c.model, "")
				require.NoError(t, err)
			}

			_, err := s.RunMonitor(t.Context(), "7", target, brief)

			assert.ErrorIs(t, err, c.want)
			assert.Zero(t, tableCount(t, s.db, "agent_runs"))
			assert.Zero(t, fakeOf(s).Asked())
		})
	}
}

// A cluster holds one monitor run at a time; another cluster's runs meanwhile.
func TestRunMonitorHoldsOneRunPerCluster(t *testing.T) {
	t.Parallel()

	s := startMonitorService(t, "", testTool{name: "echo"})
	setMonitoring(t, s.db, "8", true)
	gate, first := heldMonitor(t, s, "7")

	_, err := s.RunMonitor(t.Context(), "7", fakeTarget(s), "look")
	assert.ErrorIs(t, err, ErrMonitorInFlight)
	_, err = s.RunMonitor(t.Context(), "8", fakeTarget(s), "look")
	assert.NoError(t, err)

	close(gate)
	testutil.Recv(t, first, "the first run")
	_, err = s.RunMonitor(t.Context(), "7", fakeTarget(s), "look")
	assert.NoError(t, err, "the slot is free once the run has ended")
}

// A call that would ask the user is denied: nobody is there to answer. Its one
// row is failed with the loop's denial and never started, and nothing waits.
func TestAMonitorCallThatAsksIsDenied(t *testing.T) {
	t.Parallel()

	reader, err := read.New(nil, t.TempDir())
	require.NoError(t, err)
	s := startMonitorService(t, "", reader)
	setMonitoring(t, s.db, "7", true)
	outside := filepath.Join(t.TempDir(), "notes.txt")
	require.NoError(t, os.WriteFile(outside, []byte("x"), 0o600))
	in, _ := json.Marshal(map[string]string{"file_path": outside})
	fakeOf(s).SetToolCalls(llm.StagedCall(read.Name, string(in)))

	res, err := s.RunMonitor(t.Context(), "7", fakeTarget(s), "look")
	require.NoError(t, err)

	assert.Equal(t, RunSucceeded, res.Status)
	calls := toolCallRows(t, s.db, res.RunID)
	require.Len(t, calls, 1)
	assert.Equal(t, "failed", calls[0].status)
	assert.Equal(t, `{"error":"denied"}`, calls[0].errText)
	assert.False(t, calls[0].hasStarted)
	assert.Zero(t, tableCount(t, s.db, "approvals"))
	s.turnsMu.Lock()
	assert.Empty(t, s.pending)
	s.turnsMu.Unlock()
}

// The monitor's asker records what the proxy decided under the open call, and
// answers an ask with an error and writes nothing, so a write that reached it
// would be refused rather than sent.
func TestTheMonitorsAskerRecordsAndNeverAsks(t *testing.T) {
	t.Parallel()

	s := startMonitorService(t, "")
	run := agentRun{ID: newRunID(), ClusterID: "7", ProviderID: "fake", ModelID: "fake", Dialect: "fake", Task: "look", AppVersion: "test"}
	require.NoError(t, s.store.InTx(t.Context(), func(st stmts) error { return insertMonitorRun(t.Context(), st, run) }))
	j := &runJournal{s: s, runID: run.ID, target: fakeTarget(s), publish: func(MessageStatus) {}}
	require.NoError(t, j.LLMCallStarted(t.Context(), "fake"))
	require.NoError(t, j.ToolCallStarted(t.Context(), bashCall("kubectl delete pod x"), tools.Approval{Sandboxed: true}))
	asker := monitorAsker{j: j}
	req := tools.ActionRequest{Action: permissions.Action{Principal: permissions.Monitor, Target: permissions.Kubernetes, AccessLevel: permissions.Write,
		Operation: "delete", Resource: "pods", Summary: "Delete pods/x"}}

	_, err := asker.Ask(t.Context(), req)
	assert.ErrorIs(t, err, errMonitorAsked)
	assert.Zero(t, tableCount(t, s.db, "approvals"))

	require.NoError(t, asker.Record(t.Context(), req, permissions.Denied, "it is never allowed"))
	var status, reason string
	require.NoError(t, s.db.Read.QueryRow(`SELECT status, reason FROM approvals WHERE tool_call_id = ?`, string(j.openTool.ID)).Scan(&status, &reason))
	assert.Equal(t, string(ApprovalRefused), status)
	assert.Equal(t, "it is never allowed", reason)
}

// startsTask is a tool that starts a background task through its runtime, as
// bash's run_in_background does, and answers what the start said.
type startsTask struct{ testTool }

func (s startsTask) Run(_ context.Context, rt tools.Runtime, _ json.RawMessage) (string, bool) {
	id, _, err := rt.Tasks.Start(func(*os.File) (tools.Task, error) { panic("started") })
	if err != nil {
		return tools.StartRefusal(err), true
	}
	return id, false
}

// A monitor runs no background command: a start is refused in the words bash
// answers it with, and no task row is written.
func TestAMonitorBackgroundCommandIsRefused(t *testing.T) {
	t.Parallel()

	s := startMonitorService(t, "", startsTask{testTool{name: "bg"}})
	setMonitoring(t, s.db, "7", true)
	fakeOf(s).SetToolCalls(llm.StagedCall("bg", `{}`))

	res, err := s.RunMonitor(t.Context(), "7", fakeTarget(s), "look")
	require.NoError(t, err)

	calls := toolCallRows(t, s.db, res.RunID)
	require.Len(t, calls, 1)
	assert.Equal(t, "could not start: the monitor runs no background command", calls[0].result)
	assert.Zero(t, tableCount(t, s.db, "background_tasks"))
}

// The service's stop cancels a monitor run and waits for it: the run settles
// cancelled, and stop returns.
func TestAMonitorRunEndsWithTheService(t *testing.T) {
	t.Parallel()

	s := startMonitorService(t, "", testTool{name: "echo"})
	_, done := heldMonitor(t, s, "7")

	ctx, cancel := context.WithTimeout(context.Background(), testutil.Timeout)
	defer cancel()
	require.NoError(t, s.stop(ctx))

	res := testutil.Recv(t, done, "the run to end")
	assert.Equal(t, RunCancelled, res.Status)
	assert.Equal(t, RunCancelled, runStatusOf(t, s.db, res.RunID))
}

// The monitor reads logs, with no screen behind the read, and opens no view, which is
// the user's window.
func TestTheMonitorReadsLogsAndOpensNoView(t *testing.T) {
	t.Parallel()
	box := MonitorBox(tools.NewBox([]tools.Tool{logsview.New(nil), logsread.New(nil, nil)}))
	_, reads := box.Runner(logsread.Name)
	_, views := box.Runner(logsview.Name)
	assert.True(t, reads)
	assert.False(t, views)
}

// The monitor starts no watch: nothing it asks is answered, so nobody waits on one.
func TestTheMonitorStartsNoWatch(t *testing.T) {
	t.Parallel()
	box := MonitorBox(tools.NewBox([]tools.Tool{newWatchTool()}))
	_, watches := box.Runner("LogsWatch")
	assert.False(t, watches)
}

// A monitor run holds no task, so there is none to stop.
func TestAMonitorStopsNoTask(t *testing.T) {
	t.Parallel()
	assert.False(t, monitorTasks{}.Stop("task-1"))
}

// A stopping service starts no monitor run.
func TestAStoppingServiceRunsNoMonitor(t *testing.T) {
	t.Parallel()

	s := startMonitorService(t, "", testTool{name: "echo"})
	setMonitoring(t, s.db, "7", true)
	require.NoError(t, s.stop(t.Context()))

	_, err := s.RunMonitor(t.Context(), "7", fakeTarget(s), "look")

	assert.ErrorIs(t, err, ErrStopping)
	assert.Zero(t, tableCount(t, s.db, "agent_runs"))
}

// A run whose cluster cannot be read is the store's error, with nothing written.
func TestAMonitorRunWhoseClusterCannotBeReadWritesNothing(t *testing.T) {
	t.Parallel()

	s := startMonitorService(t, "", testTool{name: "echo"})
	_, err := s.db.Write.Exec(`ALTER TABLE clusters RENAME COLUMN monitoring_enabled TO renamed`)
	require.NoError(t, err)

	_, err = s.RunMonitor(t.Context(), "7", fakeTarget(s), "look")

	assert.ErrorContains(t, err, "cluster monitoring")
	assert.Zero(t, tableCount(t, s.db, "agent_runs"))
	assert.Zero(t, fakeOf(s).Asked())
}

// A settle the store refuses leaves the run's row running for the next start to
// fail; the caller still hears how the run ended.
func TestAMonitorSettleTheStoreRefusesLeavesTheRunRunning(t *testing.T) {
	t.Parallel()

	s := startMonitorService(t, "", testTool{name: "echo"})
	setMonitoring(t, s.db, "7", true)
	_, err := s.db.Write.Exec(`CREATE TRIGGER refuse BEFORE UPDATE OF status ON agent_runs WHEN NEW.status = 'succeeded'
		BEGIN SELECT RAISE(ABORT, 'refused'); END`)
	require.NoError(t, err)

	res, err := s.RunMonitor(t.Context(), "7", fakeTarget(s), "look")
	require.NoError(t, err)

	assert.Equal(t, RunSucceeded, res.Status)
	assert.Equal(t, RunRunning, runStatusOf(t, s.db, res.RunID))
}

// A monitor run is offered nothing the user turned off.
func TestAMonitorRunFollowsTheToolSettings(t *testing.T) {
	t.Parallel()

	s := startMonitorService(t, "card-7", testTool{name: "echo"}, testTool{name: "other"})
	setMonitoring(t, s.db, "7", true)
	setToolEnabled(t, s, "echo", false)

	res, err := s.RunMonitor(t.Context(), "7", fakeTarget(s), "look at the pods")
	require.NoError(t, err)
	assert.Equal(t, RunSucceeded, res.Status)
	assert.Equal(t, []string{"other"}, offered(fakeOf(s).LastRequest()))
}

// A monitor run whose read of the tool settings fails does not start.
func TestAMonitorRunThatCannotReadTheToolSettingsIsRefused(t *testing.T) {
	t.Parallel()

	s := startMonitorService(t, "card-7", testTool{name: "echo"})
	setMonitoring(t, s.db, "7", true)
	_, err := s.db.Write.Exec(`DROP TABLE settings`)
	require.NoError(t, err)

	_, err = s.RunMonitor(t.Context(), "7", fakeTarget(s), "look at the pods")
	assert.ErrorContains(t, err, "disabled tools")
	assert.Empty(t, fakeOf(s).Requests(), "no model asked")
}
