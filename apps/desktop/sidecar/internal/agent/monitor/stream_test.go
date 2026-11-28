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
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/appdb"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/apimeta"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/testutil"
)

func watchTargets(t *testing.T, m *Monitor) <-chan MonitorTargetCollectionWatchFrame {
	t.Helper()
	st, err := m.WatchTargetCollection(t.Context())
	require.NoError(t, err)
	return st.Frames
}

func watchIncidents(t *testing.T, m *Monitor) <-chan IncidentCollectionWatchFrame {
	t.Helper()
	st, err := m.WatchIncidentCollection(t.Context())
	require.NoError(t, err)
	return st.Frames
}

func feedOccurrences(t *testing.T, m *Monitor, id IncidentID) <-chan IncidentOccurrenceEventFeedFrame {
	t.Helper()
	st, err := m.SubscribeIncidentOccurrenceEventFeed(t.Context(), id)
	require.NoError(t, err)
	return st.Frames
}

// untilBookmark takes frames off ch up to the Bookmark, which it leaves out.
func untilBookmark[F any](t *testing.T, ch <-chan F, isBookmark func(F) bool) []F {
	t.Helper()
	var got []F
	for {
		f := testutil.Recv(t, ch, "a snapshot frame")
		if isBookmark(f) {
			return got
		}
		got = append(got, f)
	}
}

func targetBookmark(f MonitorTargetCollectionWatchFrame) bool {
	return f.Type == apimeta.CollectionWatchFrameBookmark
}

func incidentBookmark(f IncidentCollectionWatchFrame) bool {
	return f.Type == apimeta.CollectionWatchFrameBookmark
}

// insertOther stores a target of an integration the build lacks, with one
// incident of two occurrences, and answers the incident's id.
func insertOther(t *testing.T, db *appdb.DB) IncidentID {
	t.Helper()
	_, err := db.Write.Exec(`INSERT INTO monitor_targets (id, integration, key, label, created_at, updated_at) VALUES ('o1', 'other', 'k', 'Other', 0, 0)`)
	require.NoError(t, err)
	insertIncident(t, db, "o1", "oi1", "other.down", 2)
	return "oi1"
}

// insertIncident stores an incident of n occurrences on targetID, every one
// ended but the newest.
func insertIncident(t *testing.T, db *appdb.DB, targetID, id, signal string, n int) {
	t.Helper()
	at := t0.UnixMilli()
	_, err := db.Write.Exec(`INSERT INTO incidents (id, target_id, target_uid, subject_key, subject_label, signal, signal_label, severity,
		first_seen_at, last_seen_at, occurrence_count) VALUES (?, ?, 'u1', 'web', 'Deployment web', ?, 'Gone signal', 'critical', ?, ?, ?)`,
		id, targetID, signal, at, at, n)
	require.NoError(t, err)
	for i := 1; i <= n; i++ {
		if i < n {
			_, err = db.Write.Exec(`INSERT INTO incident_occurrences (id, incident_id, number, started_at, ended_at, end_reason) VALUES (?, ?, ?, ?, ?, 'resolved')`,
				id+"-"+strconv.Itoa(i), id, i, at, at)
		} else {
			_, err = db.Write.Exec(`INSERT INTO incident_occurrences (id, incident_id, number, started_at) VALUES (?, ?, ?, ?)`,
				id+"-"+strconv.Itoa(i), id, i, at)
		}
		require.NoError(t, err)
	}
}

// A subscriber gets every target as Added, then one Bookmark; with none, the
// Bookmark alone. A target whose integration the build lacks is no item.
func TestTheTargetWatchSendsEveryTargetThenABookmark(t *testing.T) {
	m := newTestMonitor(t)
	assert.Empty(t, untilBookmark(t, watchTargets(t, m), targetBookmark), "the Bookmark alone")

	insertOther(t, m.store.db)
	require.NoError(t, m.store.reconcile(t.Context(), "fake", []Target{{Key: "a", Label: "A"}, {Key: "b", Label: "B"}}))

	got := map[string]MonitorTarget{}
	for _, f := range untilBookmark(t, watchTargets(t, m), targetBookmark) {
		assert.Equal(t, apimeta.CollectionWatchFrameAdded, f.Type)
		got[f.Target.Key] = *f.Target
	}
	require.Len(t, got, 2)
	a := got["a"]
	assert.NotEmpty(t, a.ID)
	assert.Equal(t, MonitorTarget{ID: a.ID, Integration: "fake", Key: "a", Label: "A", Enabled: true, ReadState: ReadStateNeverRead, UnknownJSON: `{}`}, a)
}

// Each write to a served field arrives as one Modified: a rename, the first
// reading, a reading that judges nothing and a new UID. A change to a retired
// signal's entry sends nothing.
func TestTheTargetWatchSendsOneModifiedPerServedChange(t *testing.T) {
	m := newTestMonitor(t)
	id := targetOf(t, m.store, "t")
	frames := watchTargets(t, m)
	untilBookmark(t, frames, targetBookmark)
	c := newCommitter(t, m.store, id)

	modified := func(what string) MonitorTarget {
		t.Helper()
		f := testutil.Recv(t, frames, what)
		require.Equal(t, apimeta.CollectionWatchFrameModified, f.Type, what)
		return *f.Target
	}

	require.NoError(t, m.store.reconcile(t.Context(), "fake", []Target{{Key: "t", Label: "Renamed"}}))
	assert.Equal(t, "Renamed", modified("the rename").Label)

	c.mustAt(0, nothing())
	got := modified("the first reading")
	assert.Equal(t, ReadStateOk, got.ReadState)
	assert.Equal(t, "u1", got.CurrentUID)

	c.mustAt(30*time.Second, unknown(CodeTimeout))
	got = modified("a reading that judges nothing")
	assert.Equal(t, ReadStateFailed, got.ReadState)
	assert.Equal(t, `{"fake.crash":"timeout","fake.flip":"timeout"}`, got.UnknownJSON)

	_, err := m.store.db.Write.Exec(`UPDATE monitor_targets SET unknown_signals = json_set(unknown_signals, '$."fake.gone"', 'invalid')`)
	require.NoError(t, err)
	m.store.db.Notify(appdb.KeyMonitorTargets)
	c.mustAt(time.Minute, Reading{UID: "u2"})
	got = modified("the new UID, and no frame before it for the retired signal")
	assert.Equal(t, "u2", got.CurrentUID)
	assert.Equal(t, ReadStateOk, got.ReadState)
}

// Writes that land between two re-reads arrive as one frame with the last
// state: a target inserted and read arrives as one Added, already Ok.
func TestTargetWritesBetweenReReadsArriveAsOneFrame(t *testing.T) {
	m := newTestMonitor(t)
	require.NoError(t, m.store.reconcile(t.Context(), "fake", []Target{{Key: "a", Label: "A"}, {Key: "b", Label: "B"}}))
	frames := watchTargets(t, m)
	// The snapshot has been read once its first frame arrives; the pump then
	// waits to send the Bookmark, so the writes below land before its re-read.
	testutil.Recv(t, frames, "the first Added")

	require.NoError(t, m.store.reconcile(t.Context(), "fake", []Target{{Key: "a", Label: "A"}, {Key: "b", Label: "B"}, {Key: "new", Label: "New"}}))
	newCommitter(t, m.store, targetIDOf(t, m.store.db, "new")).mustAt(0, nothing())

	untilBookmark(t, frames, targetBookmark)
	f := testutil.Recv(t, frames, "the new target")
	assert.Equal(t, apimeta.CollectionWatchFrameAdded, f.Type)
	assert.Equal(t, "new", f.Target.Key)
	assert.Equal(t, ReadStateOk, f.Target.ReadState)

	require.NoError(t, m.store.reconcile(t.Context(), "fake", []Target{{Key: "a", Label: "A2"}, {Key: "b", Label: "B"}, {Key: "new", Label: "New"}}))
	f = testutil.Recv(t, frames, "the next write's frame, and none between")
	assert.Equal(t, "A2", f.Target.Label)
}

// The switch returns the target as the watch then delivers it. Off ends the
// open occurrences as stopped, each arriving as a Modified of its incident; a
// switch to the state the target holds sends nothing. An id naming no target, or
// a target whose integration the build lacks, is ErrNoTarget.
func TestTheSwitchReturnsTheTargetAsTheWatchDeliversIt(t *testing.T) {
	m := newTestMonitor(t)
	id := targetOf(t, m.store, "t")
	newCommitter(t, m.store, id).mustAt(0, seeing(flip.Name, "web"))
	targets := watchTargets(t, m)
	untilBookmark(t, targets, targetBookmark)
	incidents := watchIncidents(t, m)
	untilBookmark(t, incidents, incidentBookmark)

	got, err := m.SetEnabled(t.Context(), MonitorTargetID(id), false)
	require.NoError(t, err)
	assert.False(t, got.Enabled)

	f := testutil.Recv(t, targets, "the switch")
	assert.Equal(t, got, *f.Target)
	inc := testutil.Recv(t, incidents, "the stop")
	assert.Equal(t, apimeta.CollectionWatchFrameModified, inc.Type)
	assert.False(t, inc.Incident.Open)
	assert.Equal(t, EndStopped, inc.Incident.LastEndReason)

	again, err := m.SetEnabled(t.Context(), MonitorTargetID(id), false)
	require.NoError(t, err)
	assert.Equal(t, got, again)
	require.NoError(t, m.store.reconcile(t.Context(), "fake", []Target{{Key: "t", Label: "Renamed"}}))
	assert.Equal(t, "Renamed", testutil.Recv(t, targets, "the rename, and no frame for the repeat").Target.Label)

	_, err = m.SetEnabled(t.Context(), "nope", true)
	assert.ErrorIs(t, err, ErrNoTarget)
	insertOther(t, m.store.db)
	_, err = m.SetEnabled(t.Context(), "o1", false)
	assert.ErrorIs(t, err, ErrNoTarget)
}

// A subscriber gets every incident as Added, then one Bookmark; with none, the
// Bookmark alone. An incident carries what the list draws, and one whose signal
// the build lacks is served with its stored label and severity; one of a target
// whose integration the build lacks is no item.
func TestTheIncidentWatchSendsEveryIncidentThenABookmark(t *testing.T) {
	m := newTestMonitor(t)
	assert.Empty(t, untilBookmark(t, watchIncidents(t, m), incidentBookmark), "the Bookmark alone")

	insertOther(t, m.store.db)
	id := targetOf(t, m.store, "t")
	c := newCommitter(t, m.store, id)
	c.mustAt(0, seeing(flip.Name, "web"))
	c.mustAt(30*time.Second, nothing())
	insertIncident(t, m.store.db, id, "gone", "fake.gone", 1)

	got := map[string]Incident{}
	for _, f := range untilBookmark(t, watchIncidents(t, m), incidentBookmark) {
		assert.Equal(t, apimeta.CollectionWatchFrameAdded, f.Type)
		got[f.Incident.Signal] = *f.Incident
	}
	require.Len(t, got, 2)
	resolved := got[flip.Name]
	assert.Equal(t, Incident{
		ID: resolved.ID, TargetID: MonitorTargetID(id), TargetUID: "u1",
		SubjectLabel: "Deployment web", AttributesJSON: `{"name":"web"}`,
		Signal: flip.Name, SignalLabel: "Flip", Severity: SeverityInfo,
		FirstSeenAt: t0, OccurrenceCount: 1, LastStartedAt: t0,
		LastSeenAt: t0, LastEndReason: EndResolved, Baseline: true,
	}, resolved, "found at the target's first reading")
	gone := got["fake.gone"]
	assert.Equal(t, "Gone signal", gone.SignalLabel)
	assert.Equal(t, SeverityCritical, gone.Severity)
	assert.True(t, gone.Open)
	assert.True(t, gone.LastSeenAt.IsZero(), "an open incident serves no last sighting")
}

// With each write held until the watch has re-read, an occurrence that starts or
// ends arrives as one Modified of its incident; a sighting of an open incident
// sends nothing, whatever wakes the watch.
func TestTheIncidentWatchSendsOneModifiedPerOccurrence(t *testing.T) {
	m := newTestMonitor(t)
	c := newCommitter(t, m.store, targetOf(t, m.store, "t"))
	c.mustAt(0, seeing(flip.Name, "web"))
	frames := watchIncidents(t, m)
	untilBookmark(t, frames, incidentBookmark)

	c.mustAt(30*time.Second, seeing(flip.Name, "web"))
	m.store.db.Notify(appdb.KeyIncidents)
	c.mustAt(time.Minute, nothing())
	f := testutil.Recv(t, frames, "the end, and no frame before it for the sighting")
	assert.Equal(t, apimeta.CollectionWatchFrameModified, f.Type)
	assert.False(t, f.Incident.Open)
	assert.Equal(t, t0.Add(30*time.Second), f.Incident.LastSeenAt)
	assert.Equal(t, EndResolved, f.Incident.LastEndReason)

	c.mustAt(90*time.Second, seeing(flip.Name, "web"))
	f = testutil.Recv(t, frames, "the next start")
	assert.True(t, f.Incident.Open)
	assert.Equal(t, 2, f.Incident.OccurrenceCount)
	assert.Equal(t, t0.Add(90*time.Second), f.Incident.LastStartedAt)
	assert.Empty(t, f.Incident.LastEndReason)
}

// An occurrence that ends while the next starts, both between two re-reads,
// arrives as one Modified of an open incident with the later start.
func TestIncidentWritesBetweenReReadsArriveAsOneFrame(t *testing.T) {
	m := newTestMonitor(t)
	c := newCommitter(t, m.store, targetOf(t, m.store, "t"))
	c.mustAt(0, seeing(flip.Name, "web", "api"))
	frames := watchIncidents(t, m)
	// As for the targets: the pump waits to send the snapshot's rest.
	testutil.Recv(t, frames, "the first Added")

	c.mustAt(30*time.Second, seeing(flip.Name, "api"))
	c.mustAt(time.Minute, seeing(flip.Name, "web", "api"))

	untilBookmark(t, frames, incidentBookmark)
	f := testutil.Recv(t, frames, "web's one frame")
	assert.Equal(t, "Deployment web", f.Incident.SubjectLabel)
	assert.True(t, f.Incident.Open)
	assert.Equal(t, t0.Add(time.Minute), f.Incident.LastStartedAt)

	c.mustAt(90*time.Second, seeing(flip.Name, "web"))
	f = testutil.Recv(t, frames, "the next write's frame, and none between")
	assert.Equal(t, "Deployment api", f.Incident.SubjectLabel)
	assert.False(t, f.Incident.Open)
}

// An incident deleted with its target, though only KeyMonitorTargets is
// notified, or by retention, arrives as Deleted with its last-known state.
func TestADeletedIncidentArrivesAsDeleted(t *testing.T) {
	m := newTestMonitor(t)
	c := newCommitter(t, m.store, targetOf(t, m.store, "t"))
	c.mustAt(0, seeing(flip.Name, "web", "api"))
	c.mustAt(30*time.Second, seeing(flip.Name, "web"))
	frames := watchIncidents(t, m)
	untilBookmark(t, frames, incidentBookmark)

	require.NoError(t, m.store.sweep(t.Context(), t0.Add(31*24*time.Hour)))
	f := testutil.Recv(t, frames, "the swept incident")
	assert.Equal(t, apimeta.CollectionWatchFrameDeleted, f.Type)
	assert.Equal(t, "Deployment api", f.Incident.SubjectLabel)

	require.NoError(t, m.store.reconcile(t.Context(), "fake", nil))
	f = testutil.Recv(t, frames, "the target's incident")
	assert.Equal(t, apimeta.CollectionWatchFrameDeleted, f.Type)
	assert.Equal(t, "Deployment web", f.Incident.SubjectLabel)
	assert.True(t, f.Incident.Open)
}

// The feed's snapshot is the retained occurrences, highest number first, then
// one Bookmark; each frame carries the incident's id. An occurrence that ends is
// sent again under its id, and one that starts is sent.
func TestTheOccurrenceFeedReplaysThenSendsWhatStartsOrEnds(t *testing.T) {
	m := newTestMonitor(t)
	c := newCommitter(t, m.store, targetOf(t, m.store, "t"))
	c.mustAt(0, seeing(flip.Name, "web"))
	c.mustAt(30*time.Second, nothing())
	c.mustAt(time.Minute, seeing(flip.Name, "web"))
	var incidentID IncidentID
	require.NoError(t, m.store.db.Read.QueryRow(`SELECT id FROM incidents`).Scan(&incidentID))

	frames := feedOccurrences(t, m, incidentID)
	var numbers []int
	var open IncidentOccurrence
	for {
		f := testutil.Recv(t, frames, "a snapshot frame")
		assert.Equal(t, incidentID, f.IncidentID)
		if f.Type == IncidentOccurrenceEventFeedFrameBookmark {
			assert.Nil(t, f.Occurrence)
			break
		}
		require.Equal(t, IncidentOccurrenceEventFeedFrameOccurrence, f.Type)
		numbers = append(numbers, f.Occurrence.Number)
		if f.Occurrence.Number == 2 {
			open = *f.Occurrence
		}
	}
	assert.Equal(t, []int{2, 1}, numbers)
	assert.Equal(t, IncidentOccurrence{ID: open.ID, IncidentID: incidentID, Number: 2, StartedAt: t0.Add(time.Minute), DetailJSON: `{"pod":"web-0"}`}, open)

	c.mustAt(90*time.Second, nothing())
	f := testutil.Recv(t, frames, "the end")
	assert.Equal(t, incidentID, f.IncidentID)
	assert.Equal(t, open.ID, f.Occurrence.ID)
	assert.Equal(t, t0.Add(time.Minute), f.Occurrence.EndedAt)
	assert.Equal(t, EndResolved, f.Occurrence.EndReason)

	c.mustAt(2*time.Minute, seeing(flip.Name, "web"))
	f = testutil.Recv(t, frames, "the next start")
	assert.Equal(t, 3, f.Occurrence.Number)
}

// An id naming no incident, or one of a target whose integration the build
// lacks, sends the Bookmark alone, carrying the id asked for. A deletion after
// the Bookmark sends nothing, and no second Bookmark.
func TestTheOccurrenceFeedServesNoIncidentTheWatchLeavesOut(t *testing.T) {
	m := newTestMonitor(t)
	other := insertOther(t, m.store.db)
	for _, id := range []IncidentID{"nope", other} {
		f := testutil.Recv(t, feedOccurrences(t, m, id), "the Bookmark")
		assert.Equal(t, IncidentOccurrenceEventFeedFrame{Type: IncidentOccurrenceEventFeedFrameBookmark, IncidentID: id}, f)
	}

	c := newCommitter(t, m.store, targetOf(t, m.store, "t"))
	c.mustAt(0, seeing(flip.Name, "web"))
	var incidentID IncidentID
	require.NoError(t, m.store.db.Read.QueryRow(`SELECT id FROM incidents WHERE target_id <> 'o1'`).Scan(&incidentID))
	frames := feedOccurrences(t, m, incidentID)
	testutil.Recv(t, frames, "the occurrence")
	testutil.Recv(t, frames, "the Bookmark")

	require.NoError(t, m.store.reconcile(t.Context(), "fake", nil))
	m.store.db.Notify(appdb.KeyIncidents)
	// Bounded: the deleted incident's feed must stay silent, against many cadences
	// of a pump that re-reads at once.
	testutil.NoRecv(t, frames, 50*time.Millisecond, "a deletion")
}

// A read that fails ends each subscription with its error.
func TestAFailedReadEndsTheSubscription(t *testing.T) {
	m := newTestMonitor(t)
	_, err := m.store.db.Write.Exec(`ALTER TABLE incident_occurrences RENAME TO gone_occurrences`)
	require.NoError(t, err)
	_, err = m.store.db.Write.Exec(`ALTER TABLE monitor_targets RENAME TO gone_targets`)
	require.NoError(t, err)

	targets, err := m.WatchTargetCollection(t.Context())
	require.NoError(t, err)
	testutil.WaitClosed(t, targets.Frames, "the targets")
	assert.Error(t, targets.Err())
	incidents, err := m.WatchIncidentCollection(t.Context())
	require.NoError(t, err)
	testutil.WaitClosed(t, incidents.Frames, "the incidents")
	assert.Error(t, incidents.Err())
	feed, err := m.SubscribeIncidentOccurrenceEventFeed(t.Context(), "i1")
	require.NoError(t, err)
	testutil.WaitClosed(t, feed.Frames, "the feed")
	assert.Error(t, feed.Err())
}

// A failed re-read ends a subscription that has delivered its snapshot.
func TestAFailedReReadEndsTheSubscription(t *testing.T) {
	m := newTestMonitor(t)
	targets, err := m.WatchTargetCollection(t.Context())
	require.NoError(t, err)
	incidents, err := m.WatchIncidentCollection(t.Context())
	require.NoError(t, err)
	feed, err := m.SubscribeIncidentOccurrenceEventFeed(t.Context(), "i1")
	require.NoError(t, err)
	untilBookmark(t, targets.Frames, targetBookmark)
	untilBookmark(t, incidents.Frames, incidentBookmark)
	testutil.Recv(t, feed.Frames, "the feed's Bookmark")

	_, err = m.store.db.Write.Exec(`ALTER TABLE incident_occurrences RENAME TO gone_occurrences`)
	require.NoError(t, err)
	_, err = m.store.db.Write.Exec(`ALTER TABLE monitor_targets RENAME TO gone_targets`)
	require.NoError(t, err)
	m.store.db.Notify(appdb.KeyMonitorTargets)
	m.store.db.Notify(appdb.KeyIncidents)

	testutil.WaitClosed(t, targets.Frames, "the targets")
	assert.Error(t, targets.Err())
	testutil.WaitClosed(t, incidents.Frames, "the incidents")
	assert.Error(t, incidents.Err())
	testutil.WaitClosed(t, feed.Frames, "the feed")
	assert.Error(t, feed.Err())
}

// A subscription ends when the monitor stops and ends cleanly; one opened once
// it is stopping is refused with ErrStopping.
func TestAStopEndsEverySubscription(t *testing.T) {
	m := newTestMonitor(t)
	stop, err := m.Start(t.Context())
	require.NoError(t, err)
	targets, err := m.WatchTargetCollection(t.Context())
	require.NoError(t, err)
	incidents, err := m.WatchIncidentCollection(t.Context())
	require.NoError(t, err)
	feed, err := m.SubscribeIncidentOccurrenceEventFeed(t.Context(), "i1")
	require.NoError(t, err)

	require.NoError(t, stop(context.Background()))

	testutil.WaitClosed(t, targets.Frames, "the targets")
	testutil.WaitClosed(t, incidents.Frames, "the incidents")
	testutil.WaitClosed(t, feed.Frames, "the feed")
	assert.NoError(t, targets.Err())
	assert.NoError(t, incidents.Err())
	assert.NoError(t, feed.Err())
	_, err = m.WatchTargetCollection(t.Context())
	assert.ErrorIs(t, err, ErrStopping)
	_, err = m.WatchIncidentCollection(t.Context())
	assert.ErrorIs(t, err, ErrStopping)
	_, err = m.SubscribeIncidentOccurrenceEventFeed(t.Context(), "i1")
	assert.ErrorIs(t, err, ErrStopping)
}

// A subscriber that leaves ends the pump wherever it waits to send: in the
// snapshot, in a diff or in the feed's re-read, and the stream ends cleanly.
func TestASubscriberThatLeavesEndsThePump(t *testing.T) {
	m := newTestMonitor(t)
	three := []Target{{Key: "a", Label: "A"}, {Key: "b", Label: "B"}, {Key: "c", Label: "C"}}

	leave := func(what string, st *Stream[MonitorTargetCollectionWatchFrame], cancel context.CancelFunc) {
		t.Helper()
		cancel()
		testutil.WaitClosed(t, st.Frames, what)
		assert.NoError(t, st.Err(), what)
	}

	// In the diff: the pump has sent one of three Added frames, and waits.
	ctx, cancel := context.WithCancel(t.Context())
	st, err := m.WatchTargetCollection(ctx)
	require.NoError(t, err)
	untilBookmark(t, st.Frames, targetBookmark)
	require.NoError(t, m.store.reconcile(t.Context(), "fake", three))
	testutil.Recv(t, st.Frames, "the first Added of the diff")
	leave("the diff", st, cancel)

	// In the snapshot: the same three, now stored.
	ctx, cancel = context.WithCancel(t.Context())
	st, err = m.WatchTargetCollection(ctx)
	require.NoError(t, err)
	testutil.Recv(t, st.Frames, "the first Added of the snapshot")
	leave("the snapshot", st, cancel)

	// The feed, in its snapshot and in a re-read.
	c := newCommitter(t, m.store, targetIDOf(t, m.store.db, "a"))
	c.mustAt(0, seeing(flip.Name, "web"))
	var id IncidentID
	require.NoError(t, m.store.db.Read.QueryRow(`SELECT id FROM incidents`).Scan(&id))
	addOccurrences := func(from int) {
		t.Helper()
		for n := from; n < from+3; n++ {
			_, err := m.store.db.Write.Exec(`INSERT INTO incident_occurrences (id, incident_id, number, started_at, ended_at, end_reason)
				VALUES (?, ?, ?, 0, 0, 'resolved')`, string(id)+"-"+strconv.Itoa(n), string(id), n)
			require.NoError(t, err)
		}
		m.store.db.Notify(appdb.KeyIncidents)
	}

	ctx, cancel = context.WithCancel(t.Context())
	feed, err := m.SubscribeIncidentOccurrenceEventFeed(ctx, id)
	require.NoError(t, err)
	testutil.Recv(t, feed.Frames, "the open occurrence")
	testutil.Recv(t, feed.Frames, "the Bookmark")
	addOccurrences(10)
	testutil.Recv(t, feed.Frames, "the first of the re-read")
	cancel()
	testutil.WaitClosed(t, feed.Frames, "the re-read")
	assert.NoError(t, feed.Err())

	ctx, cancel = context.WithCancel(t.Context())
	feed, err = m.SubscribeIncidentOccurrenceEventFeed(ctx, id)
	require.NoError(t, err)
	testutil.Recv(t, feed.Frames, "the first of the snapshot")
	cancel()
	testutil.WaitClosed(t, feed.Frames, "the feed's snapshot")
	assert.NoError(t, feed.Err())
}

// A stored map of unknown signals that does not decode fails the targets read,
// and the watch ends with its error.
func TestATargetThatDoesNotDecodeEndsTheWatch(t *testing.T) {
	m := newTestMonitor(t)
	targetOf(t, m.store, "t")
	_, err := m.store.db.Write.Exec(`UPDATE monitor_targets SET unknown_signals = '{"fake.flip": 1}'`)
	require.NoError(t, err)

	st, err := m.WatchTargetCollection(t.Context())
	require.NoError(t, err)
	testutil.WaitClosed(t, st.Frames, "the watch")
	assert.ErrorContains(t, st.Err(), "read unknown signals")
}
