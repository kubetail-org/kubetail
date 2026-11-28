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
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/appdb/appdbtest"
)

// newTestMonitor is a monitor over a fresh app.db and the fake registration,
// never started: the reads serve what the tests write through its store.
func newTestMonitor(t *testing.T, regs ...Registration) *Monitor {
	t.Helper()
	if regs == nil {
		regs = []Registration{{Name: "fake", Cadence: cadence, Signals: []Signal{crash, flip}, Integration: newFake()}}
	}
	m, err := newMonitor(appdbtest.Open(t), func() time.Time { return t0 }, time.Hour, regs...)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, m.Close()) })
	return m
}

// The list is every registration's signals, in registration order, each with
// its integration; a monitor with none answers an empty list.
func TestListSignalsAnswersEveryRegisteredSignal(t *testing.T) {
	other := Signal{Name: "other.down", Label: "Down", Severity: SeverityCritical}
	m := newTestMonitor(t,
		Registration{Name: "fake", Cadence: cadence, Signals: []Signal{crash, flip}, Integration: newFake()},
		Registration{Name: "other", Cadence: cadence, Signals: []Signal{other}, Integration: newFake()},
	)

	assert.Equal(t, []MonitorSignal{
		{Name: crash.Name, Integration: "fake", Label: "Crash", Severity: SeverityWarning},
		{Name: flip.Name, Integration: "fake", Label: "Flip", Severity: SeverityInfo},
		{Name: other.Name, Integration: "other", Label: "Down", Severity: SeverityCritical},
	}, m.ListSignals())

	none, err := newMonitor(appdbtest.Open(t), wallClock, time.Hour)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, none.Close()) })
	assert.Equal(t, []MonitorSignal{}, none.ListSignals())
}

// A target's read state and unknown signals are derived against the signals
// the build registers: a retired signal's entry is left out, the entries left
// decide the state, and the map is served as canonical JSON.
func TestATargetsReadStateIsDerivedFromItsRegisteredSignals(t *testing.T) {
	m := newTestMonitor(t)
	for name, tc := range map[string]struct {
		read    bool
		unknown string
		state   ReadState
		served  string
	}{
		"never read":          {read: false, unknown: `{}`, state: ReadStateNeverRead, served: `{}`},
		"judging everything":  {read: true, unknown: `{}`, state: ReadStateOk, served: `{}`},
		"judging some":        {read: true, unknown: `{"fake.flip":"timeout"}`, state: ReadStatePartial, served: `{"fake.flip":"timeout"}`},
		"judging nothing":     {read: true, unknown: `{"fake.flip":"disabled", "fake.crash":"not_ready"}`, state: ReadStateFailed, served: `{"fake.crash":"not_ready","fake.flip":"disabled"}`},
		"a retired signal":    {read: true, unknown: `{"fake.gone":"invalid"}`, state: ReadStateOk, served: `{}`},
		"failed but retired":  {read: true, unknown: `{"fake.gone":"invalid","fake.flip":"timeout","fake.crash":"timeout"}`, state: ReadStateFailed, served: `{"fake.crash":"timeout","fake.flip":"timeout"}`},
		"another integration": {read: true, unknown: `{"other.down":"timeout"}`, state: ReadStateOk, served: `{}`},
	} {
		got, err := m.servedTarget(storedTarget{id: "t1", integration: "fake", read: tc.read, unknown: tc.unknown})
		require.NoError(t, err, name)
		assert.Equal(t, tc.state, got.ReadState, name)
		assert.Equal(t, tc.served, got.UnknownJSON, name)
	}

	_, err := m.servedTarget(storedTarget{id: "t1", integration: "fake", unknown: `[`})
	assert.Error(t, err, "a map that does not decode fails the read")
}

// Two stored maps that differ only in a retired signal's entry serve equal
// targets, so the watch's diff sends no frame between them.
func TestARetiredSignalsEntryLeavesTheTargetEqual(t *testing.T) {
	m := newTestMonitor(t)
	a, err := m.servedTarget(storedTarget{id: "t1", integration: "fake", read: true, unknown: `{"fake.flip":"timeout","fake.gone":"invalid"}`})
	require.NoError(t, err)
	b, err := m.servedTarget(storedTarget{id: "t1", integration: "fake", read: true, unknown: `{"fake.gone":"unavailable","fake.flip":"timeout"}`})
	require.NoError(t, err)
	assert.True(t, a == b)
}
