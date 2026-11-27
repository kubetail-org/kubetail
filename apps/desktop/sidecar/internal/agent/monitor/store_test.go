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
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/appdb"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/appdb/appdbtest"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/testutil"
)

// scopeA is cluster "1" at server "a", the server watching's cache mirrors.
var scopeA = scope{cluster: "1", server: "a"}

// openStore is a store over a fresh app.db holding cluster "1".
func openStore(t *testing.T) (*store, *appdb.DB) {
	t.Helper()
	db := appdbtest.Open(t)
	_, err := db.Write.Exec(`INSERT INTO clusters (id, source, created_at, updated_at) VALUES ('1', 'cloud', 0, 0)`)
	require.NoError(t, err)
	s, err := newStore(db)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, s.close()) })
	return s, db
}

// incidentRow is an incident as stored, with its occurrences oldest first.
type incidentRow struct {
	firstSeen, lastSeen time.Time
	count               int
	occurrences         []occurrenceRow
}

type occurrenceRow struct {
	started time.Time
	ended   *time.Time
}

// incidentsOf reads pair's incidents on cluster "1", oldest first.
func incidentsOf(t *testing.T, db *appdb.DB, pair Pair) []incidentRow {
	t.Helper()
	rows, err := db.Read.Query(`SELECT id, first_seen_at, last_seen_at, occurrence_count FROM incidents
		WHERE cluster_id = '1' AND subject_group = ? AND subject_kind = ? AND subject_namespace = ? AND subject_name = ? AND signal = ?
		ORDER BY first_seen_at`, pair.Subject.Group, pair.Subject.Kind, pair.Subject.Namespace, pair.Subject.Name, string(pair.Signal))
	require.NoError(t, err)
	defer rows.Close()
	var out []incidentRow
	var ids []string
	for rows.Next() {
		var id string
		var first, last int64
		var r incidentRow
		require.NoError(t, rows.Scan(&id, &first, &last, &r.count))
		r.firstSeen, r.lastSeen = fromMillis(first), fromMillis(last)
		out = append(out, r)
		ids = append(ids, id)
	}
	require.NoError(t, rows.Err())
	for i, id := range ids {
		occ, err := db.Read.Query(`SELECT started_at, ended_at FROM incident_occurrences WHERE incident_id = ? ORDER BY started_at`, id)
		require.NoError(t, err)
		for occ.Next() {
			var started int64
			var ended *int64
			require.NoError(t, occ.Scan(&started, &ended))
			o := occurrenceRow{started: fromMillis(started)}
			if ended != nil {
				o.ended = new(fromMillis(*ended))
			}
			out[i].occurrences = append(out[i].occurrences, o)
		}
		require.NoError(t, occ.Err())
		occ.Close()
	}
	return out
}

// startAt starts pair's occurrence at at, seen at seen.
func startAt(t *testing.T, s *store, pair Pair, at, seen time.Time) {
	t.Helper()
	require.NoError(t, s.apply(t.Context(), scopeA, changes{starts: []start{{pair: pair, at: at, seen: seen}}}))
}

// endAt ends pair's occurrence at at.
func endAt(t *testing.T, s *store, pair Pair, at time.Time) {
	t.Helper()
	require.NoError(t, s.apply(t.Context(), scopeA, changes{ends: []end{{pair: pair, at: at}}}))
}

func TestAStartOpensAnIncident(t *testing.T) {
	s, db := openStore(t)
	startAt(t, s, loop, t0, t0.Add(2*time.Minute))

	assert.Equal(t, []incidentRow{{
		firstSeen: t0, lastSeen: t0.Add(2 * time.Minute), count: 1,
		occurrences: []occurrenceRow{{started: t0}},
	}}, incidentsOf(t, db, loop))
}

func TestAStartWithin24HoursAddsAnOccurrence(t *testing.T) {
	s, db := openStore(t)
	startAt(t, s, loop, t0, t0.Add(2*time.Minute))
	endAt(t, s, loop, t0.Add(2*time.Minute))
	startAt(t, s, loop, t0.Add(time.Hour), t0.Add(time.Hour+2*time.Minute))

	assert.Equal(t, []incidentRow{{
		firstSeen: t0, lastSeen: t0.Add(time.Hour + 2*time.Minute), count: 2,
		occurrences: []occurrenceRow{{started: t0, ended: new(t0.Add(2 * time.Minute))}, {started: t0.Add(time.Hour)}},
	}}, incidentsOf(t, db, loop))
}

func TestAStartAfter24HoursOpensANewIncident(t *testing.T) {
	s, db := openStore(t)
	startAt(t, s, loop, t0, t0.Add(2*time.Minute))
	endAt(t, s, loop, t0.Add(2*time.Minute))
	later := t0.Add(25 * time.Hour)
	startAt(t, s, loop, later, later)

	got := incidentsOf(t, db, loop)
	require.Len(t, got, 2)
	assert.Equal(t, incidentRow{firstSeen: later, lastSeen: later, count: 1, occurrences: []occurrenceRow{{started: later}}}, got[1])
}

func TestAnEndStampsTheOccurrence(t *testing.T) {
	s, db := openStore(t)
	ctx := t.Context()
	startAt(t, s, loop, t0, t0.Add(2*time.Minute))
	require.NoError(t, s.apply(ctx, scopeA, changes{seen: []seen{{pair: loop, at: t0.Add(3 * time.Minute)}}}))
	endAt(t, s, loop, t0.Add(3*time.Minute))

	assert.Equal(t, []incidentRow{{
		firstSeen: t0, lastSeen: t0.Add(3 * time.Minute), count: 1,
		occurrences: []occurrenceRow{{started: t0, ended: new(t0.Add(3 * time.Minute))}},
	}}, incidentsOf(t, db, loop))
}

func TestASeenOnlyFoldSendsNoNotice(t *testing.T) {
	s, db := openStore(t)
	ctx := t.Context()
	sub := db.ListenForChanges(appdb.KeyIncidents)
	defer sub.Close()

	startAt(t, s, loop, t0, t0)
	testutil.Recv(t, sub.Chan(), "the start's notice")

	require.NoError(t, s.apply(ctx, scopeA, changes{seen: []seen{{pair: loop, at: t0.Add(time.Minute)}}}))
	// A negative assertion: the notice would follow the commit at once.
	testutil.NoRecv(t, sub.Chan(), 50*time.Millisecond, "a notice for a seen-only fold")
	assert.Equal(t, t0.Add(time.Minute), incidentsOf(t, db, loop)[0].lastSeen)
}

func TestAnIncidentKeepsItsNewest100Occurrences(t *testing.T) {
	s, db := openStore(t)
	for i := range 150 {
		started := t0.Add(time.Duration(i) * time.Hour)
		startAt(t, s, loop, started, started)
		endAt(t, s, loop, started)
	}

	got := incidentsOf(t, db, loop)
	require.Len(t, got, 1)
	assert.Equal(t, t0, got[0].firstSeen)
	assert.Equal(t, 150, got[0].count)
	require.Len(t, got[0].occurrences, 100)
	assert.Equal(t, t0.Add(50*time.Hour), got[0].occurrences[0].started)
}

// Kubernetes names are attacker-controlled text: any length and any bytes are stored.
func TestASubjectNameOfAnyTextIsStored(t *testing.T) {
	s, db := openStore(t)
	for _, name := range []string{strings.Repeat("n", 10_000), "a\x00b\x1b[31m\nc"} {
		pair := pairOf("", "Pod", "prod", name, PodPending)
		startAt(t, s, pair, t0, t0)
		assert.Len(t, incidentsOf(t, db, pair), 1)
	}
}

func TestARecoveredIncidentIsDeletedAfter30Days(t *testing.T) {
	s, db := openStore(t)
	ctx := t.Context()
	open := pairOf("", "Node", "", "n", NodeNotReady)
	require.NoError(t, s.apply(ctx, scopeA, changes{starts: []start{{pair: loop, at: t0, seen: t0}, {pair: open, at: t0, seen: t0}}}))
	endAt(t, s, loop, t0)
	sub := db.ListenForChanges(appdb.KeyIncidents)
	defer sub.Close()

	require.NoError(t, s.sweep(ctx, t0.Add(30*24*time.Hour)))
	assert.Len(t, incidentsOf(t, db, loop), 1, "30 days to the millisecond")

	require.NoError(t, s.sweep(ctx, t0.Add(30*24*time.Hour+time.Millisecond)))
	assert.Empty(t, incidentsOf(t, db, loop))
	assert.Len(t, incidentsOf(t, db, open), 1, "an open incident stays")
	testutil.Recv(t, sub.Chan(), "the sweep's notice")
}

func TestTurningMonitoringOffEndsOpenOccurrences(t *testing.T) {
	s, db := openStore(t)
	ctx := t.Context()
	startAt(t, s, loop, t0, t0.Add(2*time.Minute))
	require.NoError(t, s.apply(ctx, scopeA, changes{seen: []seen{{pair: loop, at: t0.Add(4 * time.Minute)}}}))

	require.NoError(t, s.endOpen(ctx, "1"))
	assert.Equal(t, []occurrenceRow{{started: t0, ended: new(t0.Add(4 * time.Minute))}}, incidentsOf(t, db, loop)[0].occurrences, "at its last-seen time")
}

func TestOpenOccurrencesLoadForTheSeed(t *testing.T) {
	s, _ := openStore(t)
	ctx := t.Context()
	gone := pairOf("", "Node", "", "n", NodeNotReady)
	require.NoError(t, s.apply(ctx, scopeA, changes{starts: []start{{pair: loop, at: t0, seen: t0.Add(2 * time.Minute)}, {pair: gone, at: t0, seen: t0}}}))
	endAt(t, s, gone, t0)

	open, err := s.loadOpen(ctx, scopeA)
	require.NoError(t, err)
	assert.Equal(t, []openOccurrence{{pair: loop, startedAt: t0, lastSeen: t0.Add(2 * time.Minute)}}, open)
}

// An app.db without the tables, as one desktop/v0.1.0-rc.1 wrote, fails the build.
func TestAStoreNeedsItsTables(t *testing.T) {
	db := appdbtest.Open(t)
	_, err := db.Write.Exec(`DROP TABLE incident_occurrences`)
	require.NoError(t, err)
	_, err = newStore(db)
	assert.ErrorContains(t, err, "incident_occurrences")
	_, err = newMonitor(db, watching(t), settings{windows: defaultWindows(), now: time.Now})
	assert.ErrorContains(t, err, "incident_occurrences")
}

// failing makes every statement of kind on table fail, as a full disk would, until
// the func it returns is called.
func failing(t *testing.T, db *appdb.DB, kind, table string) (heal func()) {
	t.Helper()
	trigger := `fail_` + strings.ToLower(kind) + `_` + table
	_, err := db.Write.Exec(`CREATE TRIGGER ` + trigger + ` BEFORE ` + kind + ` ON ` + table + ` BEGIN SELECT RAISE(ABORT, 'refused'); END`)
	require.NoError(t, err)
	return func() {
		t.Helper()
		_, err := db.Write.Exec(`DROP TRIGGER ` + trigger)
		require.NoError(t, err)
	}
}

// A write that fails fails the whole fold, whichever change it was.
func TestAFailedWriteFailsTheFold(t *testing.T) {
	ctx := t.Context()
	open := changes{starts: []start{{pair: loop, at: t0, seen: t0}}}
	for name, tc := range map[string]struct {
		kind, table string
		before      []changes
		c           changes
	}{
		"insert an incident": {"INSERT", "incidents", nil, open},
		"reopen an incident": {"UPDATE", "incidents", []changes{open, {ends: []end{{pair: loop, at: t0}}}},
			changes{starts: []start{{pair: loop, at: t0.Add(time.Hour), seen: t0.Add(time.Hour)}}}},
		"end an occurrence":      {"UPDATE", "incident_occurrences", []changes{open}, changes{ends: []end{{pair: loop, at: t0}}}},
		"see an incident":        {"UPDATE", "incidents", []changes{open}, changes{seen: []seen{{pair: loop, at: t0}}}},
		"adopt an open incident": {"UPDATE", "incidents", []changes{open}, open},
	} {
		t.Run(name, func(t *testing.T) {
			s, db := openStore(t)
			for _, c := range tc.before {
				require.NoError(t, s.apply(ctx, scopeA, c))
			}
			failing(t, db, tc.kind, tc.table)
			assert.ErrorContains(t, s.apply(ctx, scopeA, tc.c), "refused")
		})
	}
}

func TestAFailedTrimFailsTheFold(t *testing.T) {
	s, db := openStore(t)
	ctx := t.Context()
	for i := range keptOccurrences {
		started := t0.Add(time.Duration(i) * time.Hour)
		startAt(t, s, loop, started, started)
		endAt(t, s, loop, started)
	}
	failing(t, db, "DELETE", "incident_occurrences")
	next := t0.Add(keptOccurrences * time.Hour)
	assert.ErrorContains(t, s.apply(ctx, scopeA, changes{starts: []start{{pair: loop, at: next, seen: next}}}), "refused")
}

func TestAClosedStoreFailsItsReadsAndSweeps(t *testing.T) {
	db := appdbtest.Open(t)
	s, err := newStore(db)
	require.NoError(t, err)
	require.NoError(t, s.close())
	ctx := t.Context()

	assert.Error(t, s.sweep(ctx, t0))
	assert.Error(t, s.endOpen(ctx, "1"))
	assert.Error(t, s.endOtherServers(ctx, scopeA))
	_, err = s.loadOpen(ctx, scopeA)
	assert.Error(t, err)
}

// A start for a pair whose incident is still open, as after memory and the store
// part ways, adopts the open occurrence rather than failing on a second.
func TestAStartOfAnOpenIncidentAdoptsItsOccurrence(t *testing.T) {
	s, db := openStore(t)
	startAt(t, s, loop, t0, t0)
	startAt(t, s, loop, t0.Add(time.Minute), t0.Add(2*time.Minute))

	assert.Equal(t, []incidentRow{{
		firstSeen: t0, lastSeen: t0.Add(2 * time.Minute), count: 1,
		occurrences: []occurrenceRow{{started: t0}},
	}}, incidentsOf(t, db, loop))
}

// Each server of a cluster has its own history: a start reopens only its own server's
// incident, and a seed loads only its own server's open occurrences.
func TestAServersIncidentsAreItsOwn(t *testing.T) {
	s, db := openStore(t)
	scopeB := scope{cluster: "1", server: "b"}
	startAt(t, s, loop, t0, t0)
	endAt(t, s, loop, t0)
	require.NoError(t, s.apply(t.Context(), scopeB, changes{starts: []start{{pair: loop, at: t0.Add(time.Hour), seen: t0.Add(time.Hour)}}}))

	got := incidentsOf(t, db, loop)
	require.Len(t, got, 2, "b's start opens its own incident within 24 hours of a's")
	open, err := s.loadOpen(t.Context(), scopeA)
	require.NoError(t, err)
	assert.Empty(t, open)
	open, err = s.loadOpen(t.Context(), scopeB)
	require.NoError(t, err)
	assert.Len(t, open, 1)
}

func TestAnotherServersOpenOccurrencesEnd(t *testing.T) {
	s, db := openStore(t)
	scopeB := scope{cluster: "1", server: "b"}
	startAt(t, s, loop, t0, t0.Add(2*time.Minute))
	require.NoError(t, s.apply(t.Context(), scopeB, changes{starts: []start{{pair: loop, at: t0.Add(time.Hour), seen: t0.Add(time.Hour)}}}))

	require.NoError(t, s.endOtherServers(t.Context(), scopeB))
	got := incidentsOf(t, db, loop)
	assert.Equal(t, new(t0.Add(2*time.Minute)), got[0].occurrences[0].ended, "a's, at its last-seen time")
	assert.Nil(t, got[1].occurrences[0].ended, "b's own stays open")
}
