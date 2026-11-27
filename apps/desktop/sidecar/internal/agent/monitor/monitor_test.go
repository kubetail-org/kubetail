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
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/appdb"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/testutil"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/cluster"
)

// clock is a time a test moves.
type clock struct{ now time.Time }

func (c *clock) Now() time.Time { return c.now }

// monitorOver is a monitor of f's one cluster, "1", which is monitored, over a fresh
// app.db, its clock at created plus an hour, past every Pod's pending window. adjust
// changes its settings.
func monitorOver(t *testing.T, f *fakeService, adjust ...func(*settings)) (*Monitor, *appdb.DB, *clock) {
	t.Helper()
	_, db := openStore(t)
	f.clusters = []*cluster.Cluster{{ID: "1", Spec: cluster.ClusterSpec{Enabled: true, MonitoringEnabled: true}}}
	c := &clock{now: created.Add(time.Hour)}
	s := settings{cadence: time.Hour, windows: defaultWindows(), maxRows: 1000, maxBytes: 1 << 20, now: c.Now}
	for _, a := range adjust {
		a(&s)
	}
	m, err := newMonitor(db, f, s)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, m.Close()) })
	return m, db, c
}

var bareLoop = pairOf("", "Pod", "prod", "bare", CrashLoopBackOff)

// pastTheWindow ticks, moves the clock past a crash loop's window and ticks again, so
// a pair seen at both opens its incident.
func pastTheWindow(t *testing.T, m *Monitor, c *clock) {
	m.tick(t.Context())
	c.now = c.now.Add(2 * time.Minute)
	m.tick(t.Context())
}

// startingAtOnce sets every signal's window to nothing, so a pair opens at its
// first reading.
func startingAtOnce(s *settings) {
	for signal := range s.windows.countsAfter {
		s.windows.countsAfter[signal] = 0
	}
}

func TestTheLoopOpensAnIncidentForAMonitoredCluster(t *testing.T) {
	f := watching(t)
	f.apply(t, pod("prod", "bare", "Running", "CrashLoopBackOff"))
	m, db, c := monitorOver(t, f)

	pastTheWindow(t, m, c)

	got := incidentsOf(t, db, bareLoop)
	require.Len(t, got, 1)
	assert.Equal(t, created.Add(time.Hour), got[0].firstSeen)
}

func TestAnUnmonitoredClusterIsNotReadAndItsOccurrencesEnd(t *testing.T) {
	f := watching(t)
	f.apply(t, pod("prod", "bare", "Running", "CrashLoopBackOff"))
	m, db, c := monitorOver(t, f)
	ctx := t.Context()
	pastTheWindow(t, m, c)
	require.Nil(t, incidentsOf(t, db, bareLoop)[0].occurrences[0].ended)

	f.clusters[0].Spec.MonitoringEnabled = false
	c.now = c.now.Add(time.Minute)
	m.tick(ctx)
	got := incidentsOf(t, db, bareLoop)
	assert.Equal(t, new(created.Add(time.Hour+2*time.Minute)), got[0].occurrences[0].ended, "at its last-seen time")

	f.clusters[0].Spec.MonitoringEnabled = true
	m.tick(ctx)
	c.now = c.now.Add(time.Minute)
	m.tick(ctx)
	assert.Len(t, incidentsOf(t, db, bareLoop)[0].occurrences, 1, "on again, the window starts over")
}

// A fold whose transaction fails leaves memory as it was, so the next reading writes
// the same changes.
func TestAFailedFoldChangesNothing(t *testing.T) {
	f := watching(t)
	f.apply(t, pod("prod", "bare", "Running", "CrashLoopBackOff"))
	m, db, c := monitorOver(t, f)
	ctx := t.Context()
	m.tick(ctx)

	heal := failing(t, db, "INSERT", "incident_occurrences")
	c.now = c.now.Add(2 * time.Minute)
	m.tick(ctx)
	assert.Empty(t, incidentsOf(t, db, bareLoop))

	heal()
	c.now = c.now.Add(30 * time.Second)
	m.tick(ctx)
	got := incidentsOf(t, db, bareLoop)
	require.Len(t, got, 1)
	assert.Equal(t, created.Add(time.Hour), got[0].firstSeen)
}

// A restart seeds from the open occurrences, so a signal gone while the app was
// down ends when it was last seen.
func TestARestartEndsAnOccurrenceWhoseSignalWent(t *testing.T) {
	f := watching(t)
	crashing := pod("prod", "bare", "Running", "CrashLoopBackOff")
	f.apply(t, crashing)
	m, db, c := monitorOver(t, f)
	ctx := t.Context()
	pastTheWindow(t, m, c)
	lastSeen := c.now

	f.store.Delete(t, crashing)
	c.now = c.now.Add(10 * time.Minute) // down
	restarted, err := newMonitor(db, f, m.settings)
	require.NoError(t, err)
	defer restarted.Close()
	restarted.tick(ctx)
	assert.Equal(t, new(lastSeen), incidentsOf(t, db, bareLoop)[0].occurrences[0].ended)
}

// The loop reads at Start and again when a clusters row changes.
func TestAClustersPingRunsAReading(t *testing.T) {
	f := watching(t)
	f.apply(t, pod("prod", "bare", "Running", "CrashLoopBackOff"))
	m, db, _ := monitorOver(t, f, startingAtOnce)
	sub := db.ListenForChanges(appdb.KeyIncidents)
	defer sub.Close()

	stop, err := m.Start(t.Context())
	require.NoError(t, err)
	defer func() { require.NoError(t, stop(context.Background())) }()
	testutil.Recv(t, sub.Chan(), "the first reading's incident")

	f.apply(t, node("n", "False"))
	db.Notify(appdb.KeyClusters)
	testutil.Recv(t, sub.Chan(), "the ping's reading")
	assert.Len(t, incidentsOf(t, db, pairOf("", "Node", "", "n", NodeNotReady)), 1)
}

func TestTheLoopReadsEveryCadence(t *testing.T) {
	f := watching(t)
	m, db, _ := monitorOver(t, f, startingAtOnce, func(s *settings) { s.cadence = time.Millisecond })
	sub := db.ListenForChanges(appdb.KeyIncidents)
	defer sub.Close()
	stop, err := m.Start(t.Context())
	require.NoError(t, err)
	defer func() { require.NoError(t, stop(context.Background())) }()

	f.apply(t, node("n", "False"))
	testutil.Recv(t, sub.Chan(), "a later tick's incident")
}

// A step that fails is logged and left for the next tick, which takes it again.
func TestAFailedStepIsTakenAgainAtTheNextTick(t *testing.T) {
	ctx := t.Context()

	t.Run("end a cluster's occurrences", func(t *testing.T) {
		f := watching(t)
		f.apply(t, pod("prod", "bare", "Running", "CrashLoopBackOff"))
		m, db, c := monitorOver(t, f)
		pastTheWindow(t, m, c)
		heal := failing(t, db, "UPDATE", "incident_occurrences")
		f.clusters[0].Spec.MonitoringEnabled = false
		m.tick(ctx)
		heal()
		assert.Nil(t, incidentsOf(t, db, bareLoop)[0].occurrences[0].ended)
		m.tick(ctx)
		assert.NotNil(t, incidentsOf(t, db, bareLoop)[0].occurrences[0].ended)
	})

	t.Run("end the old server's occurrences", func(t *testing.T) {
		f := watching(t)
		f.serverUID = "a"
		f.apply(t, pod("prod", "bare", "Running", "CrashLoopBackOff"))
		m, db, c := monitorOver(t, f)
		pastTheWindow(t, m, c)
		heal := failing(t, db, "UPDATE", "incident_occurrences")
		f.serverUID = "b"
		m.tick(ctx)
		heal()
		assert.Nil(t, incidentsOf(t, db, bareLoop)[0].occurrences[0].ended)
		m.tick(ctx)
		assert.NotNil(t, incidentsOf(t, db, bareLoop)[0].occurrences[0].ended)
	})

	t.Run("sweep", func(t *testing.T) {
		f := watching(t)
		m, db, c := monitorOver(t, f)
		require.NoError(t, m.store.apply(ctx, scopeA, changes{starts: []start{{pair: loop, at: t0, seen: t0}}}))
		require.NoError(t, m.store.apply(ctx, scopeA, changes{ends: []end{{pair: loop, at: t0}}}))
		c.now = t0.Add(31 * 24 * time.Hour)
		heal := failing(t, db, "DELETE", "incidents")
		m.tick(ctx)
		heal()
		require.Len(t, incidentsOf(t, db, loop), 1)
		m.tick(ctx)
		assert.Empty(t, incidentsOf(t, db, loop))
	})

	t.Run("read", func(t *testing.T) {
		f := watching(t)
		f.apply(t, pod("prod", "bare", "Running", "CrashLoopBackOff"))
		f.readErr = errors.New("boom")
		m, _, _ := monitorOver(t, f)
		m.tick(ctx)
		require.NotContains(t, m.states, cluster.ClusterID("1"), "a reading names the server to seed from")
		f.readErr = nil
		m.tick(ctx)
		assert.Contains(t, m.states["1"], bareLoop)
	})

	t.Run("seed", func(t *testing.T) {
		f := watching(t)
		m, db, _ := monitorOver(t, f)
		_, err := db.Write.Exec(`ALTER TABLE incident_occurrences RENAME TO moved`)
		require.NoError(t, err)
		m.tick(ctx)
		assert.Empty(t, m.states)
	})
}

// A reading that fails judges nothing, so the failure's time is no absence.
func TestAFailedReadingIsNoAbsence(t *testing.T) {
	f := watching(t)
	crashing := pod("prod", "bare", "Running", "CrashLoopBackOff")
	f.apply(t, crashing)
	m, db, c := monitorOver(t, f)
	pastTheWindow(t, m, c)

	f.readErr = errors.New("boom")
	for range 12 {
		c.now = c.now.Add(30 * time.Second)
		m.tick(t.Context())
	}
	f.readErr = nil
	f.store.Delete(t, crashing)
	c.now = c.now.Add(30 * time.Second)
	m.tick(t.Context())
	assert.Nil(t, incidentsOf(t, db, bareLoop)[0].occurrences[0].ended, "30 seconds judged absent")
}

// A restart seeds at its first tick, so the time the cache takes to become readable
// again is unjudged, not absence.
func TestARestartsSyncingCacheIsNoAbsence(t *testing.T) {
	f := watching(t)
	crashing := pod("prod", "bare", "Running", "CrashLoopBackOff")
	f.apply(t, crashing)
	m, db, c := monitorOver(t, f)
	pastTheWindow(t, m, c)

	restarted, err := newMonitor(db, f, m.settings)
	require.NoError(t, err)
	defer restarted.Close()
	f.health = cluster.ClusterCacheHealth{Reason: "Connecting"}
	for range 12 {
		c.now = c.now.Add(30 * time.Second)
		restarted.tick(t.Context())
	}
	f.health = cluster.ClusterCacheHealth{Reason: "Watching"}
	f.store.Delete(t, crashing)
	c.now = c.now.Add(30 * time.Second)
	restarted.tick(t.Context())
	assert.Nil(t, incidentsOf(t, db, bareLoop)[0].occurrences[0].ended, "30 seconds judged absent")
}

// A context that comes to name another server starts that server's history: the old
// server's open occurrences end when they were last seen, its incidents stay listed,
// and the new server's windows start over.
func TestANewServerStartsItsOwnHistory(t *testing.T) {
	f := watching(t)
	f.apply(t, pod("prod", "bare", "Running", "CrashLoopBackOff"))
	m, db, c := monitorOver(t, f)
	pastTheWindow(t, m, c)
	lastSeen := c.now

	f.serverUID = "b"
	c.now = c.now.Add(30 * time.Second)
	m.tick(t.Context())
	got := incidentsOf(t, db, bareLoop)
	require.Len(t, got, 1)
	assert.Equal(t, new(lastSeen), got[0].occurrences[0].ended)

	c.now = c.now.Add(2 * time.Minute)
	m.tick(t.Context())
	got = incidentsOf(t, db, bareLoop)
	require.Len(t, got, 2, "a new incident, not the old server's reopened")
	assert.Equal(t, lastSeen.Add(30*time.Second), got[1].firstSeen)
}

// A server replaced while the app was down: the first reading ends the old server's
// open occurrences and seeds none of them.
func TestANewServerWhileTheAppWasDownSeedsNothingOfTheOld(t *testing.T) {
	f := watching(t)
	f.apply(t, pod("prod", "bare", "Running", "CrashLoopBackOff"))
	m, db, c := monitorOver(t, f)
	pastTheWindow(t, m, c)
	lastSeen := c.now

	f.serverUID = "b"
	c.now = c.now.Add(time.Minute)
	restarted, err := newMonitor(db, f, m.settings)
	require.NoError(t, err)
	defer restarted.Close()
	restarted.tick(t.Context())
	got := incidentsOf(t, db, bareLoop)
	assert.Equal(t, new(lastSeen), got[0].occurrences[0].ended)
	assert.False(t, restarted.states["1"][bareLoop].open, "the new server's pair waits its window")
}
