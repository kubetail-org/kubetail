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
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/appdb"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/sqlstmt"
)

const (
	// keepOccurrences is how many of an incident's newest occurrences it keeps.
	keepOccurrences = 100
	// keepIncidents is how long an incident that is not open is kept past its
	// last sighting.
	keepIncidents = 30 * 24 * time.Hour
	// keepAlerts is how many of the newest alerts the alert feed retains.
	keepAlerts = 100
)

// ErrNoTarget is a target id that names no target, or one whose integration the
// build lacks.
var ErrNoTarget = errors.New("monitor: no such target")

// errDiscarded is a fold whose target moved under its reading: switched,
// switched off or deleted. Nothing it would have written is.
var errDiscarded = errors.New("monitor: the target moved under the reading")

type stmtID int

const (
	stmtSelectTargetsOf stmtID = iota
	stmtInsertTarget
	stmtRelabelTarget
	stmtDeleteTarget
	stmtSelectMonitoredTargets
	stmtSelectTargetState
	stmtSelectOpenOccurrences
	stmtSelectRecentIncidents
	stmtClaimFold
	stmtSetTargetUID
	stmtSetUnknownSignals
	stmtSetBaselines
	stmtInsertIncident
	stmtJoinIncident
	stmtAdvanceAlertSeq
	stmtInsertOccurrence
	stmtTrimOccurrences
	stmtEndOccurrence
	stmtSetIncidentSeen
	stmtSetIncidentsSeen
	stmtSelectServedTarget
	stmtSetTargetEnabled
	stmtStopTargetOccurrences
	stmtDeleteOldIncidents
	stmtSelectServedTargets
	stmtSelectServedIncidents
	stmtSelectServedOccurrences
	stmtSelectServedAlerts
	numStmts int = iota
)

var statements = []sqlstmt.Statement{
	stmtSelectTargetsOf: sqlstmt.OnWriter(`SELECT id, key, label FROM monitor_targets WHERE integration = ?`),
	stmtInsertTarget: sqlstmt.OnWriter(`INSERT INTO monitor_targets (id, integration, key, label, created_at, updated_at)
	VALUES (?, ?, ?, ?, ?, ?)`),
	stmtRelabelTarget: sqlstmt.OnWriter(`UPDATE monitor_targets SET label = ?, updated_at = ? WHERE id = ?`),
	// The cascade takes the target's incidents and their occurrences.
	stmtDeleteTarget:           sqlstmt.OnWriter(`DELETE FROM monitor_targets WHERE id = ?`),
	stmtSelectMonitoredTargets: sqlstmt.OnReader(`SELECT id, integration, key, label, generation FROM monitor_targets WHERE enabled = 1`),

	stmtSelectTargetState: sqlstmt.OnReader(`SELECT current_uid, unknown_signals, baselines, last_read_at IS NOT NULL FROM monitor_targets WHERE id = ?`),
	stmtSelectOpenOccurrences: sqlstmt.OnReader(`SELECT o.id, o.incident_id, o.started_at, i.target_uid, i.signal, i.subject_key, i.last_seen_at, i.occurrence_count
	FROM incidents i JOIN incident_occurrences o ON o.incident_id = i.id AND o.ended_at IS NULL
	WHERE i.target_id = ?`),
	// Oldest first, so the newest incident of each key is the one kept.
	stmtSelectRecentIncidents: sqlstmt.OnReader(`SELECT i.id, i.target_uid, i.signal, i.subject_key, i.occurrence_count,
	  (SELECT o.ended_at FROM incident_occurrences o WHERE o.incident_id = i.id ORDER BY o.number DESC LIMIT 1)
	FROM incidents i WHERE i.target_id = ? AND i.last_seen_at >= ? ORDER BY i.rowid`),

	// A fold commits only under the generation its reading started under, while
	// the target is enabled: no row is a fold to discard.
	stmtClaimFold: sqlstmt.OnWriter(`UPDATE monitor_targets SET last_read_at = ?
	WHERE id = ? AND generation = ? AND enabled = 1`),
	stmtSetTargetUID:      sqlstmt.OnWriter(`UPDATE monitor_targets SET current_uid = ? WHERE id = ?`),
	stmtSetUnknownSignals: sqlstmt.OnWriter(`UPDATE monitor_targets SET unknown_signals = ? WHERE id = ?`),
	stmtSetBaselines:      sqlstmt.OnWriter(`UPDATE monitor_targets SET baselines = ? WHERE id = ?`),
	// An alert takes the number after monitor_alert_seq's last, which
	// stmtAdvanceAlertSeq then records. app.db has one writer connection, so the
	// numbers follow the order the transactions commit, whatever their clocks say.
	stmtInsertIncident: sqlstmt.OnWriter(`INSERT INTO incidents (id, target_id, target_uid, subject_key, subject_label,
	  subject_attributes, signal, signal_label, severity, first_seen_at, last_seen_at, occurrence_count, baseline, alerted_at, alert_seq)
	VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 1, ?, ?, CASE WHEN ? IS NOT NULL THEN ` + nextAlertSeq + ` END)`),
	// An ordinary occurrence clears baseline for good and raises the incident's one
	// alert, if it has none; a baseline one passes NULL and changes neither.
	stmtJoinIncident: sqlstmt.OnWriter(`UPDATE incidents SET occurrence_count = ?, last_seen_at = ?, baseline = baseline AND ?,
	  alert_seq = CASE WHEN alerted_at IS NULL AND ? IS NOT NULL THEN ` + nextAlertSeq + ` ELSE alert_seq END,
	  alerted_at = coalesce(alerted_at, ?) WHERE id = ?`),
	stmtAdvanceAlertSeq: sqlstmt.OnWriter(`INSERT INTO monitor_alert_seq (id, last)
	SELECT 1, alert_seq FROM incidents WHERE id = ? AND alert_seq IS NOT NULL
	ON CONFLICT (id) DO UPDATE SET last = max(last, excluded.last)`),
	stmtInsertOccurrence: sqlstmt.OnWriter(`INSERT INTO incident_occurrences (id, incident_id, number, started_at, detail, baseline)
	VALUES (?, ?, ?, ?, ?, ?)`),
	stmtTrimOccurrences: sqlstmt.OnWriter(`DELETE FROM incident_occurrences WHERE incident_id = ? AND number <= ?`),
	stmtEndOccurrence: sqlstmt.OnWriter(`UPDATE incident_occurrences SET ended_at = ?, end_reason = ?
	WHERE id = ? AND ended_at IS NULL`),
	stmtSetIncidentSeen:  sqlstmt.OnWriter(`UPDATE incidents SET last_seen_at = ? WHERE id = ?`),
	stmtSetIncidentsSeen: sqlstmt.OnWriter(`UPDATE incidents SET last_seen_at = ? WHERE id IN (SELECT value FROM json_each(?))`),

	stmtSelectServedTarget: sqlstmt.OnWriter(`SELECT ` + servedTargetColumns + ` FROM monitor_targets
	WHERE id = ? AND integration IN (SELECT value FROM json_each(?))`),
	stmtSetTargetEnabled: sqlstmt.OnWriter(`UPDATE monitor_targets SET enabled = ?, generation = generation + 1, updated_at = ?
	WHERE id = ?`),
	// Each at its last sighting, never before its start.
	stmtStopTargetOccurrences: sqlstmt.OnWriter(`UPDATE incident_occurrences
	SET ended_at = max(started_at, (SELECT i.last_seen_at FROM incidents i WHERE i.id = incident_id)), end_reason = 'stopped'
	WHERE ended_at IS NULL AND incident_id IN (SELECT id FROM incidents WHERE target_id = ?)`),
	stmtDeleteOldIncidents: sqlstmt.OnWriter(`DELETE FROM incidents WHERE last_seen_at < ?
	AND NOT EXISTS (SELECT 1 FROM incident_occurrences o WHERE o.incident_id = incidents.id AND o.ended_at IS NULL)`),

	// The served reads hold the targets of the integrations the build registers,
	// and what is under them: the ? is their names, as a JSON array.
	stmtSelectServedTargets: sqlstmt.OnReader(`SELECT ` + servedTargetColumns + ` FROM monitor_targets
	WHERE integration IN (SELECT value FROM json_each(?))`),
	// Each incident with its newest occurrence, which says whether it is open: the
	// one numbered its occurrence_count.
	stmtSelectServedIncidents: sqlstmt.OnReader(`SELECT i.id, i.target_id, i.target_uid, i.subject_label, i.subject_attributes,
	  i.signal, i.signal_label, i.severity, i.first_seen_at, i.last_seen_at, i.occurrence_count, o.started_at, o.end_reason, i.baseline
	FROM incidents i
	JOIN monitor_targets t ON t.id = i.target_id
	JOIN incident_occurrences o ON o.incident_id = i.id AND o.number = i.occurrence_count
	WHERE t.integration IN (SELECT value FROM json_each(?))`),
	stmtSelectServedOccurrences: sqlstmt.OnReader(`SELECT o.id, o.number, o.started_at, o.ended_at, o.end_reason, o.detail
	FROM incident_occurrences o
	JOIN incidents i ON i.id = o.incident_id
	JOIN monitor_targets t ON t.id = i.target_id
	WHERE o.incident_id = ? AND t.integration IN (SELECT value FROM json_each(?))
	ORDER BY o.number DESC`),
	// The newest keepAlerts by commit order, oldest first.
	stmtSelectServedAlerts: sqlstmt.OnReader(`SELECT * FROM (
	  SELECT i.id, i.target_id, t.integration, t.key, t.label, i.subject_label, i.signal_label, i.severity, i.alerted_at, i.alert_seq
	  FROM incidents i
	  JOIN monitor_targets t ON t.id = i.target_id
	  WHERE i.alert_seq IS NOT NULL AND t.integration IN (SELECT value FROM json_each(?))
	  ORDER BY i.alert_seq DESC
	  LIMIT ` + strconv.Itoa(keepAlerts) + `
	) ORDER BY alert_seq`),
}

// nextAlertSeq is the alert_seq the next alert takes.
const nextAlertSeq = `(SELECT coalesce(max(last), 0) + 1 FROM monitor_alert_seq)`

// servedTargetColumns is what storedTarget scans, in its order.
const servedTargetColumns = `id, integration, key, label, enabled, coalesce(current_uid, ''), unknown_signals, last_read_at IS NOT NULL`

type stmts = sqlstmt.Stmts[stmtID]

// store is the monitor's tables in app.db.
type store struct {
	db    *appdb.DB
	set   *sqlstmt.Set[stmtID]
	clock func() time.Time
	// integrations is the names of the integrations the build registers, as a
	// JSON array: the served reads hold their targets alone.
	integrations string
}

// targetRow is a monitored target as the loop reads it. The id, the switch and
// the UID are the core's; an integration sees the key and the label alone.
type targetRow struct {
	id, integration, key, label string
	generation                  int64
}

func openStore(db *appdb.DB, clock func() time.Time, integrations []string) (*store, error) {
	set, err := sqlstmt.Prepare[stmtID](context.Background(), db.Write.DB, db.Read.DB, statements)
	if err != nil {
		return nil, fmt.Errorf("prepare monitor statements: %w", err)
	}
	names, _ := json.Marshal(integrations) // a slice of strings always marshals
	return &store{db: db, set: set, clock: clock, integrations: string(names)}, nil
}

func (s *store) close() error { return s.set.Close() }

// reconcile makes integration's rows the targets listed: a row for each new key,
// enabled; a label that changed; and no row for a key listed no more, which
// takes its incidents. It notifies KeyMonitorTargets after a commit that changed
// anything.
func (s *store) reconcile(ctx context.Context, integration string, listed []Target) error {
	at := s.clock().UnixMilli()
	changed, deleted := false, false
	err := s.set.InTx(ctx, func(st stmts) error {
		changed, deleted = false, false
		held, err := targetsOf(ctx, st, integration)
		if err != nil {
			return err
		}
		listedKeys := map[string]bool{}
		for _, t := range listed {
			// A key listed twice is one target, its first label.
			if listedKeys[t.Key] {
				continue
			}
			listedKeys[t.Key] = true
			row, ok := held[t.Key]
			delete(held, t.Key)
			switch {
			case !ok:
				_, err = st.Exec(ctx, stmtInsertTarget, appdb.NewID(), integration, t.Key, t.Label, at, at)
			case row.label != t.Label:
				_, err = st.Exec(ctx, stmtRelabelTarget, t.Label, at, row.id)
			default:
				continue
			}
			if err != nil {
				return fmt.Errorf("write target: %w", err)
			}
			changed = true
		}
		for _, row := range held {
			if _, err := st.Exec(ctx, stmtDeleteTarget, row.id); err != nil {
				return fmt.Errorf("delete target: %w", err)
			}
			changed, deleted = true, true
		}
		return nil
	})
	if err != nil {
		return err
	}
	if changed {
		s.db.Notify(appdb.KeyMonitorTargets)
	}
	// A target's delete cascades to its incidents.
	if deleted {
		s.db.Notify(appdb.KeyIncidents)
	}
	return nil
}

// targetsOf is integration's rows by key.
func targetsOf(ctx context.Context, st stmts, integration string) (map[string]targetRow, error) {
	rows, err := st.Query(ctx, stmtSelectTargetsOf, integration)
	if err != nil {
		return nil, fmt.Errorf("read targets: %w", err)
	}
	defer rows.Close()
	held := map[string]targetRow{}
	for rows.Next() {
		var r targetRow
		if err := rows.Scan(&r.id, &r.key, &r.label); err != nil { // coverage:ignore the statement's columns are these types, CHECKed by the schema
			return nil, fmt.Errorf("read targets: %w", err)
		}
		held[r.key] = r
	}
	if err := rows.Err(); err != nil { // coverage:ignore a read that began fails only on a closed pool, which outlives the monitor
		return nil, fmt.Errorf("read targets: %w", err)
	}
	return held, nil
}

// monitored is every enabled target, of every integration.
func (s *store) monitored(ctx context.Context) ([]targetRow, error) {
	rows, err := s.set.Stmts().Query(ctx, stmtSelectMonitoredTargets)
	if err != nil {
		return nil, fmt.Errorf("read monitored targets: %w", err)
	}
	defer rows.Close()
	var out []targetRow
	for rows.Next() {
		var r targetRow
		if err := rows.Scan(&r.id, &r.integration, &r.key, &r.label, &r.generation); err != nil { // coverage:ignore the statement's columns are these types, CHECKed by the schema
			return nil, fmt.Errorf("read monitored targets: %w", err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil { // coverage:ignore a read that began fails only on a closed pool, which outlives the monitor
		return nil, fmt.Errorf("read monitored targets: %w", err)
	}
	return out, nil
}

// seed is a target's state as app.db holds it, for its first fold since the
// monitor started or since its memory was dropped: a pair for each open
// occurrence, first seen at its start and last seen when its incident was, and
// the incidents a new occurrence could join. ErrNoTarget for a target gone.
func (s *store) seed(ctx context.Context, targetID string, now time.Time) (targetState, error) {
	state := targetState{pairs: map[pairKey]pair{}, incidents: map[incidentKey]incidentRef{}}
	var uid sql.NullString
	var unknown, baselines string
	err := s.set.Stmts().QueryRow(ctx, stmtSelectTargetState, targetID).Scan(&uid, &unknown, &baselines, &state.read)
	if errors.Is(err, sql.ErrNoRows) {
		return targetState{}, ErrNoTarget
	}
	if err != nil {
		return targetState{}, fmt.Errorf("read target state: %w", err)
	}
	state.uid = uid.String
	if err := json.Unmarshal([]byte(unknown), &state.unknown); err != nil {
		return targetState{}, fmt.Errorf("read unknown signals: %w", err)
	}
	if state.baselines, err = decodeBaselines(baselines); err != nil {
		return targetState{}, err
	}

	if err := s.seedIncidents(ctx, &state, targetID, now); err != nil {
		return targetState{}, err
	}
	// Last, so an open occurrence's incident is the one its key holds.
	if err := s.seedOpenOccurrences(ctx, &state, targetID); err != nil {
		return targetState{}, err
	}
	return state, nil
}

// seedIncidents gives state the target's incidents last seen within recentFor
// of now.
func (s *store) seedIncidents(ctx context.Context, state *targetState, targetID string, now time.Time) error {
	rows, err := s.set.Stmts().Query(ctx, stmtSelectRecentIncidents, targetID, now.Add(-recentFor).UnixMilli())
	if err != nil {
		return fmt.Errorf("read recent incidents: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var key incidentKey
		var ref incidentRef
		var endedAt sql.NullInt64
		if err := rows.Scan(&ref.id, &key.uid, &key.signal, &key.subject, &ref.count, &endedAt); err != nil { // coverage:ignore the statement's columns are these types, CHECKed by the schema
			return fmt.Errorf("read recent incidents: %w", err)
		}
		if endedAt.Valid {
			ref.endedAt = fromMillis(endedAt.Int64)
		}
		state.incidents[key] = ref
	}
	if err := rows.Err(); err != nil { // coverage:ignore a read that began fails only on a closed pool, which outlives the monitor
		return fmt.Errorf("read recent incidents: %w", err)
	}
	return nil
}

// seedOpenOccurrences gives state a pair for each of the target's open
// occurrences, and its incident.
func (s *store) seedOpenOccurrences(ctx context.Context, state *targetState, targetID string) error {
	rows, err := s.set.Stmts().Query(ctx, stmtSelectOpenOccurrences, targetID)
	if err != nil {
		return fmt.Errorf("read open occurrences: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var o occurrence
		var startedAt, lastSeen int64
		var count int
		if err := rows.Scan(&o.id, &o.incidentID, &startedAt, &o.incident.uid, &o.incident.signal, &o.incident.subject, &lastSeen, &count); err != nil { // coverage:ignore the statement's columns are these types, CHECKed by the schema
			return fmt.Errorf("read open occurrences: %w", err)
		}
		state.incidents[o.incident] = incidentRef{id: o.incidentID, count: count}
		// Under another UID only after a crash between a UID's write and its
		// stops; the reading that names the stored UID never sees such a pair.
		state.pairs[pairKey{o.incident.signal, o.incident.subject}] = pair{
			firstSeen: fromMillis(startedAt), lastSeen: fromMillis(lastSeen), open: &o,
		}
	}
	if err := rows.Err(); err != nil { // coverage:ignore a read that began fails only on a closed pool, which outlives the monitor
		return fmt.Errorf("read open occurrences: %w", err)
	}
	return nil
}

// decodeBaselines is monitor_targets.baselines, each signal's time in ms.
func decodeBaselines(stored string) (map[string]time.Time, error) {
	var ms map[string]int64
	if err := json.Unmarshal([]byte(stored), &ms); err != nil {
		return nil, fmt.Errorf("read baselines: %w", err)
	}
	out := make(map[string]time.Time, len(ms))
	for name, at := range ms {
		out[name] = fromMillis(at)
	}
	return out, nil
}

// encodeBaselines is baselines as monitor_targets.baselines holds them.
func encodeBaselines(baselines map[string]time.Time) string {
	ms := make(map[string]int64, len(baselines))
	for name, at := range baselines {
		ms[name] = at.UnixMilli()
	}
	b, _ := json.Marshal(ms) // a map of numbers always marshals
	return string(b)
}

// commit writes one fold's changes in one transaction, under the generation its
// reading started under: errDiscarded for a target switched or gone since. It
// notifies after the commit, each key whose served rows moved.
func (s *store) commit(ctx context.Context, targetID string, generation int64, ch changes, now time.Time) error {
	err := s.set.InTx(ctx, func(st stmts) error {
		res, err := st.Exec(ctx, stmtClaimFold, now.UnixMilli(), targetID, generation)
		if err != nil {
			return fmt.Errorf("claim fold: %w", err)
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return errDiscarded
		}
		return writeChanges(ctx, st, targetID, ch)
	})
	if err != nil {
		return err
	}
	if ch.notifiesTargets() {
		s.db.Notify(appdb.KeyMonitorTargets)
	}
	if ch.notifiesIncidents() {
		s.db.Notify(appdb.KeyIncidents)
	}
	return nil
}

// writeChanges writes ch inside a fold's transaction: the UID, unknown signals
// and baselines, then the ends, the starts and the sightings.
func writeChanges(ctx context.Context, st stmts, targetID string, ch changes) error {
	if ch.uid != "" {
		if _, err := st.Exec(ctx, stmtSetTargetUID, ch.uid, targetID); err != nil {
			return fmt.Errorf("write target uid: %w", err)
		}
	}
	if ch.unknown != nil {
		b, _ := json.Marshal(ch.unknown) // a map of strings always marshals
		if _, err := st.Exec(ctx, stmtSetUnknownSignals, string(b), targetID); err != nil {
			return fmt.Errorf("write unknown signals: %w", err)
		}
	}
	if ch.baselines != nil {
		if _, err := st.Exec(ctx, stmtSetBaselines, encodeBaselines(ch.baselines), targetID); err != nil {
			return fmt.Errorf("write baselines: %w", err)
		}
	}
	for _, e := range ch.ends {
		if _, err := st.Exec(ctx, stmtEndOccurrence, e.at.UnixMilli(), string(e.reason), e.occurrenceID); err != nil {
			return fmt.Errorf("end occurrence: %w", err)
		}
		if _, err := st.Exec(ctx, stmtSetIncidentSeen, e.at.UnixMilli(), e.incidentID); err != nil {
			return fmt.Errorf("end occurrence: %w", err)
		}
	}
	for _, s := range ch.starts {
		if err := writeStart(ctx, st, targetID, s); err != nil {
			return err
		}
	}
	if len(ch.seen) > 0 {
		ids, _ := json.Marshal(ch.seen) // a slice of strings always marshals
		if _, err := st.Exec(ctx, stmtSetIncidentsSeen, ch.seenAt.UnixMilli(), string(ids)); err != nil {
			return fmt.Errorf("write last seen: %w", err)
		}
	}
	return nil
}

// writeStart inserts one occurrence, opening its incident or joining it, and
// trims the incident to its newest keepOccurrences. An occurrence that is not
// baseline raises its incident's alert, at most once an incident.
func writeStart(ctx context.Context, st stmts, targetID string, s start) error {
	alertedAt := sql.NullInt64{Int64: s.seenAt.UnixMilli(), Valid: !s.baseline}
	var err error
	if inc := s.open; inc != nil {
		attributes, _ := json.Marshal(nonNil(inc.subject.Attributes)) // a map of strings always marshals
		_, err = st.Exec(ctx, stmtInsertIncident, s.incidentID, targetID, inc.uid, inc.subject.Key, inc.subject.Label,
			string(attributes), inc.signal.Name, inc.signal.Label, string(inc.signal.Severity), s.startedAt.UnixMilli(), s.seenAt.UnixMilli(),
			s.baseline, alertedAt, alertedAt)
	} else {
		_, err = st.Exec(ctx, stmtJoinIncident, s.number, s.seenAt.UnixMilli(), s.baseline, alertedAt, alertedAt, s.incidentID)
	}
	if err != nil {
		return fmt.Errorf("write incident: %w", err)
	}
	if alertedAt.Valid {
		if _, err := st.Exec(ctx, stmtAdvanceAlertSeq, s.incidentID); err != nil {
			return fmt.Errorf("write alert seq: %w", err)
		}
	}
	detail, _ := json.Marshal(nonNil(s.detail)) // a map of strings always marshals
	if _, err := st.Exec(ctx, stmtInsertOccurrence, s.occurrenceID, s.incidentID, s.number, s.startedAt.UnixMilli(), string(detail), s.baseline); err != nil {
		return fmt.Errorf("write occurrence: %w", err)
	}
	if s.number > keepOccurrences {
		if _, err := st.Exec(ctx, stmtTrimOccurrences, s.incidentID, s.number-keepOccurrences); err != nil {
			return fmt.Errorf("trim occurrences: %w", err)
		}
	}
	return nil
}

// setEnabled is the switch, in its own transaction: off ends the target's open
// occurrences as stopped, each at its last sighting, and empties its baselines,
// so the next reading takes a new one; either way the generation moves, so a
// reading begun before it commits nothing. A switch to the state the target
// holds writes nothing. It answers the target as the switch left it; ErrNoTarget
// for no such target, or one whose integration the build lacks.
func (s *store) setEnabled(ctx context.Context, targetID string, enabled bool) (storedTarget, error) {
	var row storedTarget
	switched, stopped := false, false
	err := s.set.InTx(ctx, func(st stmts) error {
		switched, stopped = false, false
		var err error
		row, err = scanStoredTarget(st.QueryRow(ctx, stmtSelectServedTarget, targetID, s.integrations))
		switch {
		case errors.Is(err, sql.ErrNoRows):
			return ErrNoTarget
		case err != nil:
			return fmt.Errorf("read target: %w", err)
		case row.enabled == enabled:
			return nil
		}
		if _, err := st.Exec(ctx, stmtSetTargetEnabled, enabled, s.clock().UnixMilli(), targetID); err != nil {
			return fmt.Errorf("switch target: %w", err)
		}
		row.enabled, switched = enabled, true
		if enabled {
			return nil
		}
		res, err := st.Exec(ctx, stmtStopTargetOccurrences, targetID)
		if err != nil {
			return fmt.Errorf("stop occurrences: %w", err)
		}
		n, _ := res.RowsAffected()
		stopped = n > 0
		if _, err := st.Exec(ctx, stmtSetBaselines, "{}", targetID); err != nil {
			return fmt.Errorf("drop baselines: %w", err)
		}
		return nil
	})
	if err != nil {
		return storedTarget{}, err
	}
	if switched {
		s.db.Notify(appdb.KeyMonitorTargets)
	}
	if stopped {
		s.db.Notify(appdb.KeyIncidents)
	}
	return row, nil
}

// sweep deletes every incident that is not open and was last seen more than
// keepIncidents before now.
func (s *store) sweep(ctx context.Context, now time.Time) error {
	res, err := s.set.Stmts().Exec(ctx, stmtDeleteOldIncidents, now.Add(-keepIncidents).UnixMilli())
	if err != nil {
		return fmt.Errorf("delete old incidents: %w", err)
	}
	if n, _ := res.RowsAffected(); n > 0 {
		s.db.Notify(appdb.KeyIncidents)
	}
	return nil
}

// nonNil is m, or an empty map for nil, which stores as {}.
func nonNil(m map[string]string) map[string]string {
	if m == nil {
		return map[string]string{}
	}
	return m
}

// fromMillis is a stored time, on the wall clock alone.
func fromMillis(ms int64) time.Time { return time.UnixMilli(ms).UTC() }

// storedTarget is a served target's row, as the served reads scan it.
type storedTarget struct {
	id, integration, key, label string
	enabled                     bool
	uid                         string // "" before a reading named one
	unknown                     string // monitor_targets.unknown_signals
	read                        bool   // last_read_at is set
}

// scanner is what *sql.Row and *sql.Rows share.
type scanner interface{ Scan(dest ...any) error }

func scanStoredTarget(r scanner) (storedTarget, error) {
	var t storedTarget
	err := r.Scan(&t.id, &t.integration, &t.key, &t.label, &t.enabled, &t.uid, &t.unknown, &t.read)
	return t, err
}

// servedTargets is every target of an integration the build registers.
func (s *store) servedTargets(ctx context.Context) ([]storedTarget, error) {
	rows, err := s.set.Stmts().Query(ctx, stmtSelectServedTargets, s.integrations)
	if err != nil {
		return nil, fmt.Errorf("read targets: %w", err)
	}
	defer rows.Close()
	var out []storedTarget
	for rows.Next() {
		t, err := scanStoredTarget(rows)
		if err != nil { // coverage:ignore the statement's columns are these types, CHECKed by the schema
			return nil, fmt.Errorf("read targets: %w", err)
		}
		out = append(out, t)
	}
	if err := rows.Err(); err != nil { // coverage:ignore a read that began fails only on a closed pool, which outlives the monitor
		return nil, fmt.Errorf("read targets: %w", err)
	}
	return out, nil
}

// servedIncidents is every incident of a target servedTargets holds.
func (s *store) servedIncidents(ctx context.Context) ([]Incident, error) {
	rows, err := s.set.Stmts().Query(ctx, stmtSelectServedIncidents, s.integrations)
	if err != nil {
		return nil, fmt.Errorf("read incidents: %w", err)
	}
	defer rows.Close()
	var out []Incident
	for rows.Next() {
		var i Incident
		var firstSeen, lastSeen, lastStarted int64
		var reason sql.NullString
		if err := rows.Scan(&i.ID, &i.TargetID, &i.TargetUID, &i.SubjectLabel, &i.AttributesJSON, &i.Signal, &i.SignalLabel,
			&i.Severity, &firstSeen, &lastSeen, &i.OccurrenceCount, &lastStarted, &reason, &i.Baseline); err != nil { // coverage:ignore the statement's columns are these types, CHECKed by the schema
			return nil, fmt.Errorf("read incidents: %w", err)
		}
		i.FirstSeenAt, i.LastStartedAt = fromMillis(firstSeen), fromMillis(lastStarted)
		// An open incident's last sighting moves with every reading, so it is not served.
		if i.Open = !reason.Valid; !i.Open {
			i.LastSeenAt, i.LastEndReason = fromMillis(lastSeen), EndReason(reason.String)
		}
		out = append(out, i)
	}
	if err := rows.Err(); err != nil { // coverage:ignore a read that began fails only on a closed pool, which outlives the monitor
		return nil, fmt.Errorf("read incidents: %w", err)
	}
	return out, nil
}

// servedOccurrences is an incident's retained occurrences, highest number first:
// none for an incident servedIncidents does not hold.
func (s *store) servedOccurrences(ctx context.Context, incidentID IncidentID) ([]IncidentOccurrence, error) {
	rows, err := s.set.Stmts().Query(ctx, stmtSelectServedOccurrences, string(incidentID), s.integrations)
	if err != nil {
		return nil, fmt.Errorf("read occurrences: %w", err)
	}
	defer rows.Close()
	var out []IncidentOccurrence
	for rows.Next() {
		o := IncidentOccurrence{IncidentID: incidentID}
		var startedAt int64
		var endedAt sql.NullInt64
		var reason sql.NullString
		if err := rows.Scan(&o.ID, &o.Number, &startedAt, &endedAt, &reason, &o.DetailJSON); err != nil { // coverage:ignore the statement's columns are these types, CHECKed by the schema
			return nil, fmt.Errorf("read occurrences: %w", err)
		}
		o.StartedAt = fromMillis(startedAt)
		if endedAt.Valid {
			o.EndedAt, o.EndReason = fromMillis(endedAt.Int64), EndReason(reason.String)
		}
		out = append(out, o)
	}
	if err := rows.Err(); err != nil { // coverage:ignore a read that began fails only on a closed pool, which outlives the monitor
		return nil, fmt.Errorf("read occurrences: %w", err)
	}
	return out, nil
}

// servedAlerts is the newest keepAlerts alerts of a target servedTargets holds,
// in the order they committed.
func (s *store) servedAlerts(ctx context.Context) ([]Alert, error) {
	rows, err := s.set.Stmts().Query(ctx, stmtSelectServedAlerts, s.integrations)
	if err != nil {
		return nil, fmt.Errorf("read alerts: %w", err)
	}
	defer rows.Close()
	var out []Alert
	for rows.Next() {
		var a Alert
		var alertedAt int64
		if err := rows.Scan(&a.IncidentID, &a.TargetID, &a.Integration, &a.TargetKey, &a.TargetLabel, &a.SubjectLabel,
			&a.SignalLabel, &a.Severity, &alertedAt, &a.Seq); err != nil { // coverage:ignore the statement's columns are these types, CHECKed by the schema
			return nil, fmt.Errorf("read alerts: %w", err)
		}
		a.AlertedAt = fromMillis(alertedAt)
		out = append(out, a)
	}
	if err := rows.Err(); err != nil { // coverage:ignore a read that began fails only on a closed pool, which outlives the monitor
		return nil, fmt.Errorf("read alerts: %w", err)
	}
	return out, nil
}
