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
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/appdb"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/appdb/appdbtest"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/testutil"
)

// queueAs marks incident id's triage queued under request, by trigger, with run
// linked when it is set.
func queueAs(t *testing.T, db *appdb.DB, id, request string, trigger triageTrigger, run any) {
	t.Helper()
	_, err := db.Write.Exec(`UPDATE incidents SET triage_state = 'queued', triage_request_id = ?, triage_trigger = ?, triage_run_id = ? WHERE id = ?`,
		request, string(trigger), run, id)
	require.NoError(t, err)
}

// The watch sends the setting at once, null while unset, then the value each
// write leaves.
func TestTheTriageModelWatchSendsItsValueAtOnceAndOnEachChange(t *testing.T) {
	m := newTestMonitor(t)
	st, err := m.WatchTriageModelState(t.Context())
	require.NoError(t, err)
	assert.Nil(t, testutil.Recv(t, st.Frames, "the first frame"), "unset is null")

	require.NoError(t, m.SetTriageModel(t.Context(), &testModel))
	assert.Equal(t, &testModel, testutil.Recv(t, st.Frames, "the value set"))
	other := TriageModel{ProviderID: "fake", ModelID: "fake-2"}
	require.NoError(t, m.SetTriageModel(t.Context(), &other))
	assert.Equal(t, &other, testutil.Recv(t, st.Frames, "another model"))
	require.NoError(t, m.SetTriageModel(t.Context(), nil))
	assert.Nil(t, testutil.Recv(t, st.Frames, "the value unset"))
}

// A write that changes the setting fails each queued automatic triage whose run
// has not started, as setting_changed, and alerts its incident in the same
// transaction; one whose run started and a manual one are left queued, and a
// write of the value it holds fails none. It notifies the setting and, having
// cancelled one, the incidents.
func TestASettingWriteCancelsTheQueuedAutomaticTriages(t *testing.T) {
	m := newTestMonitor(t)
	db := m.store.db
	_, err := db.Write.Exec(`INSERT INTO clusters (id, source, created_at, updated_at) VALUES ('c1', 'kubeconfig', 0, 0)`)
	require.NoError(t, err)
	_, err = db.Write.Exec(`INSERT INTO agent_runs (id, agent_type, app_version, trigger, cluster_id, provider, model, dialect, created_at)
		VALUES ('run1', 'monitor', 'test', 'monitor', 'c1', 'fake', 'fake', 'fake', 0)`)
	require.NoError(t, err)
	insertTriageTarget(t, db, "t1", "k1")
	for _, id := range []string{"waiting", "running", "manual"} {
		insertIncident(t, db, "t1", id, "fake.crash", 1)
	}
	require.NoError(t, m.SetTriageModel(t.Context(), &testModel))
	queueAs(t, db, "waiting", "r1", triggerAutomatic, nil)
	queueAs(t, db, "running", "r2", triggerAutomatic, "run1")
	queueAs(t, db, "manual", "r3", triggerManual, nil)
	settings := db.ListenForChanges(appdb.KeySettings)
	defer settings.Close()
	incidents := db.ListenForChanges(appdb.KeyIncidents)
	defer incidents.Close()

	require.NoError(t, m.SetTriageModel(t.Context(), &testModel))
	assert.Equal(t, TriageStateQueued, triageOf(t, m, "waiting").State, "the same value cancels nothing")
	require.NoError(t, m.SetTriageModel(t.Context(), nil))

	testutil.Recv(t, settings.Chan(), "the setting's notice")
	testutil.Recv(t, incidents.Chan(), "the cancel's notice")
	assert.Equal(t, IncidentTriage{State: TriageStateFailed, Error: TriageCodeSettingChanged}, triageOf(t, m, "waiting"))
	assert.Equal(t, t0.UnixMilli(), alertedAt(t, db, "waiting"), "it alerts untriaged")
	for _, id := range []IncidentID{"running", "manual"} {
		assert.Equal(t, TriageStateQueued, triageOf(t, m, id).State, id)
		assert.Equal(t, int64(-1), alertedAt(t, db, id), id)
	}
}

// A model with no provider or no model id is refused, and writes nothing.
func TestATriageModelWithNoIDsIsRefused(t *testing.T) {
	m := newTestMonitor(t)
	for _, model := range []TriageModel{{ModelID: "m"}, {ProviderID: "p"}} {
		assert.ErrorIs(t, m.SetTriageModel(t.Context(), &model), ErrInvalidTriageModel)
	}
	assert.Zero(t, count(t, m.store.db, `SELECT count(*) FROM settings`))
}

// The monitor's calls refuse once it stops.
func TestTheSettingIsRefusedOnceTheMonitorStops(t *testing.T) {
	m, err := newMonitor(appdbtest.Open(t), wallClock, time.Hour)
	require.NoError(t, err)
	require.NoError(t, m.Close())
	assert.ErrorIs(t, m.SetTriageModel(t.Context(), nil), ErrStopping)
	_, err = m.WatchTriageModelState(t.Context())
	assert.ErrorIs(t, err, ErrStopping)
}

// A read that fails ends the watch with its error; a subscriber that leaves
// between writes ends it cleanly.
func TestTheTriageModelWatchEnds(t *testing.T) {
	m := newTestMonitor(t)
	ctx, cancel := context.WithCancel(t.Context())
	st, err := m.WatchTriageModelState(ctx)
	require.NoError(t, err)
	testutil.Recv(t, st.Frames, "the first frame")
	cancel()
	testutil.WaitClosed(t, st.Frames, "the watch, between writes")
	assert.NoError(t, st.Err())

	_, err = m.store.db.Write.Exec(`ALTER TABLE settings RENAME TO gone`)
	require.NoError(t, err)
	failing, err := m.WatchTriageModelState(t.Context())
	require.NoError(t, err)
	testutil.WaitClosed(t, failing.Frames, "the failed watch")
	assert.ErrorContains(t, failing.Err(), "read triage model")
}
