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
	"database/sql"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/appdb"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/appdb/appdbtest"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/testutil"
)

func openTestStore(t *testing.T) *store {
	t.Helper()
	st, err := openStore(appdbtest.Open(t), func() time.Time { return t0 }, []string{"fake"})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, st.close()) })
	return st
}

// targetOf reconciles one fake target and answers its id.
func targetOf(t *testing.T, st *store, key string) string {
	t.Helper()
	require.NoError(t, st.reconcile(t.Context(), "fake", []Target{{Key: key, Label: key}}))
	return targetIDOf(t, st.db, key)
}

// committer folds readings over one stored target and commits each fold.
type committer struct {
	t        *testing.T
	st       *store
	targetID string
	state    targetState
	ids      int
}

func newCommitter(t *testing.T, st *store, targetID string) *committer {
	t.Helper()
	state, err := st.seed(t.Context(), targetID, t0)
	require.NoError(t, err)
	return &committer{t: t, st: st, targetID: targetID, state: state}
}

// at folds r at t0+offset under generation 0 and commits it.
func (c *committer) at(offset time.Duration, r Reading) (changes, error) {
	next, ch := fold(c.state, testRegistration(), r, t0.Add(offset), func() string {
		c.ids++
		return c.targetID + "-" + strconv.Itoa(c.ids)
	})
	_, err := c.st.commit(c.t.Context(), c.targetID, 0, ch, t0.Add(offset), false)
	if err == nil {
		c.state = next
	}
	return ch, err
}

func (c *committer) mustAt(offset time.Duration, r Reading) changes {
	c.t.Helper()
	ch, err := c.at(offset, r)
	require.NoError(c.t, err)
	return ch
}

// targetIDOf is the id of the fake integration's target key.
func targetIDOf(t *testing.T, db *appdb.DB, key string) string {
	t.Helper()
	var id string
	require.NoError(t, db.Read.QueryRow(`SELECT id FROM monitor_targets WHERE integration = 'fake' AND key = ?`, key).Scan(&id))
	return id
}

// count is query's one integer, read from db.
func count(t *testing.T, db *appdb.DB, query string, args ...any) int {
	t.Helper()
	var n int
	require.NoError(t, db.Read.QueryRow(query, args...).Scan(&n))
	return n
}

// The reconcile inserts a row for each new key, enabled; rewrites a label that
// changed; deletes a key listed no more with its incidents; takes a key listed
// twice as one target; leaves another
// integration's rows alone; and notifies only when it changed something, the
// incidents too when a delete took any.
func TestTheReconcileKeepsTheRowsTheListing(t *testing.T) {
	st := openTestStore(t)
	sub := st.db.ListenForChanges(appdb.KeyMonitorTargets)
	defer sub.Close()
	incidents := st.db.ListenForChanges(appdb.KeyIncidents)
	defer incidents.Close()
	ctx := t.Context()
	require.NoError(t, st.reconcile(ctx, "other", []Target{{Key: "x", Label: "x"}}))
	testutil.Recv(t, sub.Chan(), "the other integration's row")

	require.NoError(t, st.reconcile(ctx, "fake", []Target{{Key: "a", Label: "A"}, {Key: "b", Label: "B"}}))
	testutil.Recv(t, sub.Chan(), "the new rows")
	assert.Equal(t, 2, count(t, st.db, `SELECT count(*) FROM monitor_targets WHERE integration = 'fake' AND enabled = 1`))
	c := newCommitter(t, st, targetIDOf(t, st.db, "b"))
	c.mustAt(0, seeing(flip.Name, "web"))
	testutil.Recv(t, sub.Chan(), "the fold's first reading")
	testutil.Recv(t, incidents.Chan(), "the fold's incident")
	require.Equal(t, 1, count(t, st.db, `SELECT count(*) FROM incidents`))

	require.NoError(t, st.reconcile(ctx, "fake", []Target{{Key: "a", Label: "A"}, {Key: "b", Label: "B"}}))
	testutil.NoRecv(t, sub.Chan(), 20*time.Millisecond, "a listing that changes nothing notifies nothing")
	testutil.NoRecv(t, incidents.Chan(), 20*time.Millisecond, "a listing that deletes nothing notifies no incidents")

	require.NoError(t, st.reconcile(ctx, "fake", []Target{{Key: "a", Label: "A2"}, {Key: "a", Label: "again"}}))
	testutil.Recv(t, sub.Chan(), "the relabel and the delete")
	testutil.Recv(t, incidents.Chan(), "the delete's cascade")
	assert.Equal(t, 1, count(t, st.db, `SELECT count(*) FROM monitor_targets WHERE integration = 'fake' AND key = 'a' AND label = 'A2'`))
	assert.Zero(t, count(t, st.db, `SELECT count(*) FROM monitor_targets WHERE key = 'b'`))
	assert.Zero(t, count(t, st.db, `SELECT count(*) FROM incidents`), "the delete takes its incidents")
	assert.Equal(t, 1, count(t, st.db, `SELECT count(*) FROM monitor_targets WHERE integration = 'other'`))
}

// A fold's commit opens the incident with what the reading said of its subject,
// stored as it came, and its first occurrence with that reading's detail; it
// notifies the incidents and, for its first reading and UID, the target. A fold
// that only advances last_seen_at notifies nothing.
func TestACommitStoresTheFoldAndNotifiesWhatMoved(t *testing.T) {
	st := openTestStore(t)
	id := targetOf(t, st, "t")
	incidents := st.db.ListenForChanges(appdb.KeyIncidents)
	defer incidents.Close()
	targets := st.db.ListenForChanges(appdb.KeyMonitorTargets)
	defer targets.Close()
	c := newCommitter(t, st, id)

	c.mustAt(0, seeing(flip.Name, "web"))

	testutil.Recv(t, incidents.Chan(), "the incident")
	testutil.Recv(t, targets.Chan(), "the first reading and its UID")
	var uid, label, attrs, signalLabel, severity, detail string
	var first, last, n int64
	require.NoError(t, st.db.Read.QueryRow(`SELECT t.current_uid, i.subject_label, i.subject_attributes, i.signal_label, i.severity,
		i.first_seen_at, i.last_seen_at, i.occurrence_count, o.detail
		FROM monitor_targets t JOIN incidents i ON i.target_id = t.id JOIN incident_occurrences o ON o.incident_id = i.id`).
		Scan(&uid, &label, &attrs, &signalLabel, &severity, &first, &last, &n, &detail))
	assert.Equal(t, []any{"u1", "Deployment web", `{"name":"web"}`, "Flip", "info", t0.UnixMilli(), t0.UnixMilli(), int64(1), `{"pod":"web-0"}`},
		[]any{uid, label, attrs, signalLabel, severity, first, last, n, detail})

	c.mustAt(30*time.Second, seeing(flip.Name, "web"))
	testutil.NoRecv(t, incidents.Chan(), 20*time.Millisecond, "a sighting alone notifies nothing")
	testutil.NoRecv(t, targets.Chan(), 20*time.Millisecond, "nor does a later reading")
	assert.Equal(t, 1, count(t, st.db, `SELECT count(*) FROM incidents WHERE last_seen_at = ?`, t0.Add(30*time.Second).UnixMilli()))

	c.mustAt(time.Minute, nothing())
	testutil.Recv(t, incidents.Chan(), "the end")
	var endedAt int64
	var reason string
	require.NoError(t, st.db.Read.QueryRow(`SELECT ended_at, end_reason FROM incident_occurrences`).Scan(&endedAt, &reason))
	assert.Equal(t, t0.Add(30*time.Second).UnixMilli(), endedAt)
	assert.Equal(t, "resolved", reason)
}

// An incident with 150 occurrences keeps those numbered 51 to 150, its first
// sighting and an occurrence_count of 150.
func TestAnIncidentKeepsItsNewest100Occurrences(t *testing.T) {
	st := openTestStore(t)
	c := newCommitter(t, st, targetOf(t, st, "t"))
	for i := range 150 {
		at := time.Duration(i) * time.Minute
		c.mustAt(at, seeing(flip.Name, "web"))
		c.mustAt(at+30*time.Second, nothing())
	}

	var lo, hi, kept, n int
	var first int64
	require.NoError(t, st.db.Read.QueryRow(`SELECT min(o.number), max(o.number), count(*), i.occurrence_count, i.first_seen_at
		FROM incident_occurrences o JOIN incidents i ON i.id = o.incident_id`).Scan(&lo, &hi, &kept, &n, &first))
	assert.Equal(t, []int{51, 150, 100, 150}, []int{lo, hi, kept, n})
	assert.Equal(t, t0.UnixMilli(), first)
}

// A reading that started before a switch off commits nothing, though the target
// was switched on again before it returned; nor does one on a target gone.
func TestAFoldCommitsOnlyUnderItsGeneration(t *testing.T) {
	st := openTestStore(t)
	id := targetOf(t, st, "t")
	c := newCommitter(t, st, id)
	require.NoError(t, errOf(st.setEnabled(t.Context(), id, false)))
	require.NoError(t, errOf(st.setEnabled(t.Context(), id, true)))

	_, err := c.at(0, seeing(flip.Name, "web"))

	require.ErrorIs(t, err, errDiscarded)
	assert.Zero(t, count(t, st.db, `SELECT count(*) FROM incidents`))
	assert.Equal(t, 1, count(t, st.db, `SELECT count(*) FROM monitor_targets WHERE last_read_at IS NULL AND generation = 2`))

	require.NoError(t, st.reconcile(t.Context(), "fake", nil))
	_, err = c.at(0, seeing(flip.Name, "web"))
	assert.ErrorIs(t, err, errDiscarded, "a target gone")
}

// Turning a target off ends its open occurrences as stopped, each at its last
// sighting, in the switch's own transaction; a switch to the state it holds
// writes nothing, and a target that is not there is ErrNoTarget.
func TestTheSwitchStopsATargetInItsOwnTransaction(t *testing.T) {
	st := openTestStore(t)
	id := targetOf(t, st, "t")
	c := newCommitter(t, st, id)
	c.mustAt(0, seeing(flip.Name, "web"))
	c.mustAt(30*time.Second, seeing(flip.Name, "web"))
	incidents := st.db.ListenForChanges(appdb.KeyIncidents)
	defer incidents.Close()

	require.NoError(t, errOf(st.setEnabled(t.Context(), id, false)))

	testutil.Recv(t, incidents.Chan(), "the stop")
	var endedAt int64
	var reason string
	require.NoError(t, st.db.Read.QueryRow(`SELECT ended_at, end_reason FROM incident_occurrences`).Scan(&endedAt, &reason))
	assert.Equal(t, t0.Add(30*time.Second).UnixMilli(), endedAt)
	assert.Equal(t, "stopped", reason)
	targets := st.db.ListenForChanges(appdb.KeyMonitorTargets)
	defer targets.Close()
	require.NoError(t, errOf(st.setEnabled(t.Context(), id, false)))
	assert.Equal(t, 1, count(t, st.db, `SELECT generation FROM monitor_targets`), "a repeat moves nothing")
	testutil.NoRecv(t, targets.Chan(), 20*time.Millisecond, "and notifies nothing")
	assert.ErrorIs(t, errOf(st.setEnabled(t.Context(), "nope", true)), ErrNoTarget)
}

// A target's first fold since a start seeds a pair for each open occurrence,
// first seen at its start and last seen when its incident was, and remembers the
// incidents a new occurrence could join.
func TestASeedRestoresWhatTheLastRunLeftOpen(t *testing.T) {
	st := openTestStore(t)
	id := targetOf(t, st, "t")
	c := newCommitter(t, st, id)
	c.mustAt(0, seeing(flip.Name, "web"))
	c.mustAt(30*time.Second, seeing(flip.Name, "web", "api"))
	c.mustAt(time.Minute, seeing(flip.Name, "web"))

	state, err := st.seed(t.Context(), id, t0.Add(time.Hour))

	require.NoError(t, err)
	assert.Equal(t, "u1", state.uid)
	assert.True(t, state.read)
	require.Len(t, state.pairs, 1)
	p := state.pairs[pairKey{flip.Name, "web"}]
	assert.Equal(t, t0, p.firstSeen)
	assert.Equal(t, t0.Add(time.Minute), p.lastSeen)
	require.NotNil(t, p.open)
	assert.Len(t, state.incidents, 2)
	api := state.incidents[incidentKey{"u1", flip.Name, "api"}]
	assert.Equal(t, t0.Add(30*time.Second), api.endedAt)
	_, err = st.seed(t.Context(), "nope", t0)
	assert.ErrorIs(t, err, ErrNoTarget)
}

// After a restart under 24 hours, an open occurrence whose signal is gone resolves
// after its window of judged absence, stamped with the last sighting before the
// restart; one whose signal holds stays open. 30 hours on, the first fold ends
// it as stale, though the reading sees it.
func TestARestartPicksUpTheOpenOccurrences(t *testing.T) {
	st := openTestStore(t)
	id := targetOf(t, st, "t")
	c := newCommitter(t, st, id)
	c.mustAt(0, seeing(crash.Name, "web"))
	for at := 30 * time.Second; at <= 3*time.Minute; at += cadence {
		c.mustAt(at, seeing(crash.Name, "web"))
	}

	restarted := newCommitter(t, st, id)
	restarted.ids = 100
	for at := 3 * time.Hour; at < 3*time.Hour+5*time.Minute; at += cadence {
		ch := restarted.mustAt(at, nothing())
		require.Empty(t, ch.ends, at)
	}
	ch := restarted.mustAt(3*time.Hour+5*time.Minute, nothing())
	require.Len(t, ch.ends, 1)
	assert.Equal(t, t0.Add(3*time.Minute), ch.ends[0].at)

	d := openTestStore(t)
	did := targetOf(t, d, "t")
	c = newCommitter(t, d, did)
	for at := time.Duration(0); at <= 2*time.Minute; at += cadence {
		c.mustAt(at, seeing(crash.Name, "web"))
	}
	late := newCommitter(t, d, did)
	late.ids = 100
	ch = late.mustAt(30*time.Hour, seeing(crash.Name, "web"))
	require.Len(t, ch.ends, 1)
	assert.Equal(t, EndStale, ch.ends[0].reason)
}

// The sweep deletes an incident that is not open and was last seen more than 30
// days ago, and keeps an open one however old.
func TestTheSweepDeletesOldIncidentsThatAreNotOpen(t *testing.T) {
	st := openTestStore(t)
	c := newCommitter(t, st, targetOf(t, st, "t"))
	c.mustAt(0, seeing(flip.Name, "web", "api"))
	c.mustAt(30*time.Second, seeing(flip.Name, "web"))
	incidents := st.db.ListenForChanges(appdb.KeyIncidents)
	defer incidents.Close()

	require.NoError(t, st.sweep(t.Context(), t0.Add(31*24*time.Hour)))

	testutil.Recv(t, incidents.Chan(), "the delete")
	assert.Equal(t, 1, count(t, st.db, `SELECT count(*) FROM incidents WHERE subject_key = 'web'`))
	assert.Zero(t, count(t, st.db, `SELECT count(*) FROM incidents WHERE subject_key = 'api'`))
	require.NoError(t, st.sweep(t.Context(), t0.Add(31*24*time.Hour)))
	testutil.NoRecv(t, incidents.Chan(), 20*time.Millisecond, "a sweep that deletes nothing notifies nothing")
}

// A fold whose transaction fails writes nothing, and the next reading writes the
// same changes from the state it had.
func TestAFoldThatFailsToCommitWritesNothing(t *testing.T) {
	st := openTestStore(t)
	c := newCommitter(t, st, targetOf(t, st, "t"))
	_, err := st.db.Write.Exec(`CREATE TRIGGER refuse BEFORE INSERT ON incident_occurrences BEGIN SELECT RAISE(ABORT, 'refused'); END`)
	require.NoError(t, err)
	before := c.state

	_, err = c.at(0, seeing(flip.Name, "web"))

	require.ErrorContains(t, err, "refused")
	assert.Equal(t, before, c.state)
	assert.Zero(t, count(t, st.db, `SELECT count(*) FROM incidents`))
	assert.Equal(t, 1, count(t, st.db, `SELECT count(*) FROM monitor_targets WHERE last_read_at IS NULL AND current_uid IS NULL`))
	_, err = st.db.Write.Exec(`DROP TRIGGER refuse`)
	require.NoError(t, err)
	c.mustAt(0, seeing(flip.Name, "web"))
	assert.Equal(t, 1, count(t, st.db, `SELECT count(*) FROM incidents`))
}

// refuse makes the writer refuse what when names, as a failed write would.
func refuse(t *testing.T, st *store, when string) {
	t.Helper()
	_, err := st.db.Write.Exec(`CREATE TRIGGER refuse ` + when + ` BEGIN SELECT RAISE(ABORT, 'refused'); END`)
	require.NoError(t, err)
}

// Each of a fold's writes that fails fails its commit, naming the write, and
// writes nothing.
func TestEachWriteOfAFoldThatFailsFailsItsCommit(t *testing.T) {
	for name, c := range map[string]struct {
		before func(c *committer)       // the folds that commit first
		when   string                   // the trigger's event
		fold   func(c *committer) error // the fold that fails
		want   string
	}{
		"the claim": {when: `BEFORE UPDATE OF last_read_at ON monitor_targets`, want: "claim fold"},
		"the uid":   {when: `BEFORE UPDATE OF current_uid ON monitor_targets`, want: "write target uid"},
		"the unknown signals": {
			before: func(c *committer) { c.mustAt(0, nothing()) },
			when:   `BEFORE UPDATE OF unknown_signals ON monitor_targets`,
			fold:   func(c *committer) error { _, err := c.at(30*time.Second, unknown(CodeTimeout)); return err },
			want:   "write unknown signals",
		},
		"the baselines": {when: `BEFORE UPDATE OF baselines ON monitor_targets`, want: "write baselines"},
		"an end": {
			before: func(c *committer) { c.mustAt(0, seeing(flip.Name, "web")) },
			when:   `BEFORE UPDATE OF ended_at ON incident_occurrences`,
			fold:   func(c *committer) error { _, err := c.at(30*time.Second, nothing()); return err },
			want:   "end occurrence",
		},
		"an end's last sighting": {
			before: func(c *committer) { c.mustAt(0, seeing(flip.Name, "web")) },
			when:   `BEFORE UPDATE OF last_seen_at ON incidents`,
			fold:   func(c *committer) error { _, err := c.at(30*time.Second, nothing()); return err },
			want:   "end occurrence",
		},
		"a sighting": {
			before: func(c *committer) { c.mustAt(0, seeing(flip.Name, "web")) },
			when:   `BEFORE UPDATE OF last_seen_at ON incidents`,
			fold:   func(c *committer) error { _, err := c.at(30*time.Second, seeing(flip.Name, "web")); return err },
			want:   "write last seen",
		},
		"an incident": {when: `BEFORE INSERT ON incidents`, want: "write incident"},
		"an alert's number": {
			before: func(c *committer) { c.mustAt(0, nothing()) },
			when:   `BEFORE INSERT ON monitor_alert_seq`,
			fold:   func(c *committer) error { _, err := c.at(6*time.Minute, seeing(flip.Name, "web")); return err },
			want:   "write alert seq",
		},
		"the trim": {
			before: func(c *committer) {
				for i := range keepOccurrences {
					c.mustAt(time.Duration(i)*time.Minute, seeing(flip.Name, "web"))
					c.mustAt(time.Duration(i)*time.Minute+30*time.Second, nothing())
				}
			},
			when: `BEFORE DELETE ON incident_occurrences`,
			fold: func(c *committer) error {
				_, err := c.at(time.Duration(keepOccurrences)*time.Minute, seeing(flip.Name, "web"))
				return err
			},
			want: "trim occurrences",
		},
	} {
		t.Run(name, func(t *testing.T) {
			st := openTestStore(t)
			cm := newCommitter(t, st, targetOf(t, st, "t"))
			if c.before != nil {
				c.before(cm)
			}
			refuse(t, st, c.when)
			fold := c.fold
			if fold == nil {
				fold = func(cm *committer) error { _, err := cm.at(0, seeing(flip.Name, "web")); return err }
			}
			before := count(t, st.db, `SELECT count(*) FROM incident_occurrences`)

			err := fold(cm)

			assert.ErrorContains(t, err, c.want)
			assert.Equal(t, before, count(t, st.db, `SELECT count(*) FROM incident_occurrences`))
		})
	}
}

// The switch's writes that fail fail it, and a seed that cannot decode the
// baselines or read the open occurrences fails.
func TestTheSwitchAndTheSeedReportAFailedStatement(t *testing.T) {
	st := openTestStore(t)
	id := targetOf(t, st, "t")
	c := newCommitter(t, st, id)
	c.mustAt(0, seeing(flip.Name, "web"))

	refuse(t, st, `BEFORE UPDATE OF ended_at ON incident_occurrences`)
	assert.ErrorContains(t, errOf(st.setEnabled(t.Context(), id, false)), "stop occurrences")
	_, err := st.db.Write.Exec(`DROP TRIGGER refuse`)
	require.NoError(t, err)
	refuse(t, st, `BEFORE UPDATE OF enabled ON monitor_targets`)
	assert.ErrorContains(t, errOf(st.setEnabled(t.Context(), id, false)), "switch target")
	_, err = st.db.Write.Exec(`DROP TRIGGER refuse`)
	require.NoError(t, err)
	refuse(t, st, `BEFORE UPDATE OF baselines ON monitor_targets`)
	assert.ErrorContains(t, errOf(st.setEnabled(t.Context(), id, false)), "drop baselines")
	_, err = st.db.Write.Exec(`DROP TRIGGER refuse`)
	require.NoError(t, err)

	_, err = st.db.Write.Exec(`UPDATE monitor_targets SET baselines = '{"fake.crash":"soon"}'`)
	require.NoError(t, err)
	_, err = st.seed(t.Context(), id, t0)
	assert.ErrorContains(t, err, "read baselines")
	_, err = st.db.Write.Exec(`UPDATE monitor_targets SET baselines = '{}'`)
	require.NoError(t, err)

	_, err = st.db.Write.Exec(`ALTER TABLE incident_occurrences RENAME COLUMN started_at TO renamed`)
	require.NoError(t, err)
	_, err = st.seed(t.Context(), id, t0)
	assert.ErrorContains(t, err, "read open occurrences")
}

// A subject with no attributes and an observation with no detail store as empty
// objects.
func TestNoAttributesOrDetailStoreAsEmptyObjects(t *testing.T) {
	st := openTestStore(t)
	c := newCommitter(t, st, targetOf(t, st, "t"))

	c.mustAt(0, Reading{UID: "u1", Observations: []Observation{{Signal: flip.Name, Subject: Subject{Key: "web", Label: "web"}}}})

	assert.Equal(t, 1, count(t, st.db, `SELECT count(*) FROM incidents i JOIN incident_occurrences o ON o.incident_id = i.id
		WHERE i.subject_attributes = '{}' AND o.detail = '{}'`))
}

// Each of the store's other statements that fails fails its call, naming it.
func TestTheStoreReportsAFailedStatement(t *testing.T) {
	for name, c := range map[string]struct {
		breaks string
		call   func(st *store, id string) error
		want   string
	}{
		"the reconcile's read": {`ALTER TABLE monitor_targets RENAME COLUMN label TO renamed`,
			func(st *store, _ string) error { return st.reconcile(t.Context(), "fake", nil) }, "read targets"},
		"an insert": {`CREATE TRIGGER refuse BEFORE INSERT ON monitor_targets BEGIN SELECT RAISE(ABORT, 'refused'); END`,
			func(st *store, _ string) error {
				return st.reconcile(t.Context(), "fake", []Target{{Key: "t"}, {Key: "new"}})
			}, "write target"},
		"a relabel": {`CREATE TRIGGER refuse BEFORE UPDATE OF label ON monitor_targets BEGIN SELECT RAISE(ABORT, 'refused'); END`,
			func(st *store, _ string) error {
				return st.reconcile(t.Context(), "fake", []Target{{Key: "t", Label: "T"}})
			}, "write target"},
		"a delete": {`CREATE TRIGGER refuse BEFORE DELETE ON monitor_targets BEGIN SELECT RAISE(ABORT, 'refused'); END`,
			func(st *store, _ string) error { return st.reconcile(t.Context(), "fake", nil) }, "delete target"},
		"the monitored targets": {`ALTER TABLE monitor_targets RENAME COLUMN generation TO renamed`,
			func(st *store, _ string) error { _, err := st.monitored(t.Context()); return err }, "read monitored targets"},
		"a target's state": {`ALTER TABLE monitor_targets RENAME COLUMN current_uid TO renamed`,
			func(st *store, id string) error { _, err := st.seed(t.Context(), id, t0); return err }, "read target state"},
		"unknown signals that are not codes": {`UPDATE monitor_targets SET unknown_signals = '{"fake.flip": 1}'`,
			func(st *store, id string) error { _, err := st.seed(t.Context(), id, t0); return err }, "read unknown signals"},
		"the recent incidents": {`ALTER TABLE incidents RENAME COLUMN occurrence_count TO renamed`,
			func(st *store, id string) error { _, err := st.seed(t.Context(), id, t0); return err }, "read recent incidents"},
		"the switch's read": {`ALTER TABLE monitor_targets RENAME COLUMN enabled TO renamed`,
			func(st *store, id string) error { return errOf(st.setEnabled(t.Context(), id, false)) }, "read target"},
		"the sweep": {`ALTER TABLE incidents RENAME COLUMN last_seen_at TO renamed`,
			func(st *store, _ string) error { return st.sweep(t.Context(), t0) }, "delete old incidents"},
	} {
		t.Run(name, func(t *testing.T) {
			st := openTestStore(t)
			id := targetOf(t, st, "t")
			_, err := st.db.Write.Exec(c.breaks)
			require.NoError(t, err)

			assert.ErrorContains(t, c.call(st, id), c.want)
		})
	}
}

// openStore fails on a schema its statements do not prepare on.
func TestOpenStoreRefusesASchemaItCannotPrepareOn(t *testing.T) {
	db := appdbtest.Open(t)
	_, err := db.Write.Exec(`ALTER TABLE monitor_targets RENAME COLUMN generation TO renamed`)
	require.NoError(t, err)

	_, err = New(db)

	assert.ErrorContains(t, err, "prepare monitor statements")
}

// errOf is a call's error alone.
func errOf[T any](_ T, err error) error { return err }

// incidentAlert is an incident's baseline flag and its alerted_at, -1 for none.
func incidentAlert(t *testing.T, db *appdb.DB, id string) (bool, int64) {
	t.Helper()
	var baseline bool
	var alertedAt sql.NullInt64
	require.NoError(t, db.Read.QueryRow(`SELECT baseline, alerted_at FROM incidents WHERE id = ?`, id).Scan(&baseline, &alertedAt))
	if !alertedAt.Valid {
		return baseline, -1
	}
	return baseline, alertedAt.Int64
}

// The baselines a fold writes are what the next seed reads; the switch off empties
// them in its own transaction, so the next reading takes a new baseline.
func TestTheBaselinesOutliveARestartAndNotTheSwitch(t *testing.T) {
	st := openTestStore(t)
	id := targetOf(t, st, "t")
	newCommitter(t, st, id).mustAt(0, nothing())

	state, err := st.seed(t.Context(), id, t0.Add(time.Minute))
	require.NoError(t, err)
	assert.Equal(t, map[string]time.Time{crash.Name: t0, flip.Name: t0}, state.baselines)

	_, err = st.setEnabled(t.Context(), id, false)
	require.NoError(t, err)
	state, err = st.seed(t.Context(), id, t0.Add(time.Minute))
	require.NoError(t, err)
	assert.Empty(t, state.baselines)
}

// A restart inside the window marks again what it sees, so a crash loop present
// since the first reading opens a baseline incident; one first seen after a
// healthy window, or still waiting out its open-after when a restart after the
// window loses its mark, opens an ordinary incident and alerts at its start.
func TestARestartTakesNoBaseline(t *testing.T) {
	st := openTestStore(t)
	id := targetOf(t, st, "t")
	c := newCommitter(t, st, id)
	c.mustAt(0, seeing(crash.Name, "web"))

	restarted := newCommitter(t, st, id)
	restarted.ids = 100
	var started []start
	for at := time.Minute; at <= 3*time.Minute; at += cadence {
		started = append(started, restarted.mustAt(at, seeing(crash.Name, "web")).starts...)
	}
	require.Len(t, started, 1)
	baseline, alertedAt := incidentAlert(t, st.db, started[0].incidentID)
	assert.True(t, baseline, "seen since the first reading")
	assert.Equal(t, int64(-1), alertedAt)

	late := newCommitter(t, st, id)
	late.ids = 200
	started = nil
	for at := 4*time.Minute + 30*time.Second; at <= 9*time.Minute; at += cadence {
		if at == 5*time.Minute {
			late = newCommitter(t, st, id) // a restart after the window drops api's mark
			late.ids = 300
		}
		started = append(started, late.mustAt(at, seeing(crash.Name, "web", "api")).starts...)
	}
	require.Len(t, started, 1)
	baseline, alertedAt = incidentAlert(t, st.db, started[0].incidentID)
	assert.False(t, baseline, "api waited out its open-after across the restart")
	assert.Equal(t, started[0].seenAt.UnixMilli(), alertedAt)
}

// An incident opened by a baseline occurrence raises no alert, and stays baseline
// through later baseline occurrences; its first ordinary one clears the flag and
// raises its alert, which no later occurrence raises again. Each occurrence keeps
// its own mark.
func TestAnIncidentAlertsAtItsFirstOrdinaryOccurrence(t *testing.T) {
	st := openTestStore(t)
	id := targetOf(t, st, "t")
	at := func(minutes int, baseline bool, n int, open bool) {
		t.Helper()
		s := start{occurrenceID: "o" + strconv.Itoa(n), incidentID: "i1", number: n, baseline: baseline,
			startedAt: t0.Add(time.Duration(minutes) * time.Minute), seenAt: t0.Add(time.Duration(minutes) * time.Minute)}
		if open {
			s.open = &newIncident{uid: "u1", subject: Subject{Key: "web"}, signal: crash}
		}
		ends := []end(nil)
		if n > 1 {
			ends = []end{{occurrenceID: "o" + strconv.Itoa(n-1), incidentID: "i1", at: s.startedAt, reason: EndResolved}}
		}
		_, err := st.commit(t.Context(), id, 0, changes{starts: []start{s}, ends: ends}, s.seenAt, false)
		require.NoError(t, err)
	}

	at(0, true, 1, true)
	at(10, true, 2, false)
	baseline, alertedAt := incidentAlert(t, st.db, "i1")
	assert.True(t, baseline)
	assert.Equal(t, int64(-1), alertedAt)

	at(20, false, 3, false)
	at(30, false, 4, false)
	baseline, alertedAt = incidentAlert(t, st.db, "i1")
	assert.False(t, baseline)
	assert.Equal(t, t0.Add(20*time.Minute).UnixMilli(), alertedAt)
	assert.Equal(t, 1, count(t, st.db, `SELECT alert_seq FROM incidents WHERE id = 'i1'`), "numbered once, at its alert")
	assert.Equal(t, 1, count(t, st.db, `SELECT last FROM monitor_alert_seq`))
	assert.Equal(t, 2, count(t, st.db, `SELECT count(*) FROM incident_occurrences WHERE baseline = 1`))
}

// An incident opened ordinary alerts at its open; one whose ordinary occurrence is
// trimmed away under 100 later baseline ones stays ordinary.
func TestAnOrdinaryIncidentStaysOrdinaryPastItsTrim(t *testing.T) {
	st := openTestStore(t)
	id := targetOf(t, st, "t")
	for n := 1; n <= keepOccurrences+1; n++ {
		s := start{occurrenceID: "o" + strconv.Itoa(n), incidentID: "i1", number: n, baseline: n > 1,
			startedAt: t0.Add(time.Duration(n) * time.Minute), seenAt: t0.Add(time.Duration(n) * time.Minute)}
		var ends []end
		if n == 1 {
			s.open = &newIncident{uid: "u1", subject: Subject{Key: "web"}, signal: crash}
		} else {
			ends = []end{{occurrenceID: "o" + strconv.Itoa(n-1), incidentID: "i1", at: s.startedAt, reason: EndResolved}}
		}
		_, err := st.commit(t.Context(), id, 0, changes{starts: []start{s}, ends: ends}, s.seenAt, false)
		require.NoError(t, err)
	}

	assert.Zero(t, count(t, st.db, `SELECT count(*) FROM incident_occurrences WHERE baseline = 0`), "the ordinary one is trimmed")
	baseline, alertedAt := incidentAlert(t, st.db, "i1")
	assert.False(t, baseline)
	assert.Equal(t, t0.Add(time.Minute).UnixMilli(), alertedAt)
	incidents, err := st.servedIncidents(t.Context())
	require.NoError(t, err)
	require.Len(t, incidents, 1)
	assert.False(t, incidents[0].Baseline)
}

// A container running at the target's first reading and crash looping from the
// next, inside the window, opens a baseline incident that raises no alert.
func TestABaselineCatchesAFailureItsFirstReadingMissed(t *testing.T) {
	st := openTestStore(t)
	c := newCommitter(t, st, targetOf(t, st, "t"))
	c.mustAt(0, nothing())

	var started []start
	for at := 30 * time.Second; at <= 3*time.Minute; at += cadence {
		started = append(started, c.mustAt(at, seeing(crash.Name, "web")).starts...)
	}

	require.Len(t, started, 1)
	baseline, alertedAt := incidentAlert(t, st.db, started[0].incidentID)
	assert.True(t, baseline)
	assert.Equal(t, int64(-1), alertedAt)
}
