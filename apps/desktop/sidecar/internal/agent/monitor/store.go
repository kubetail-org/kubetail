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
	"errors"
	"fmt"
	"time"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/appdb"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/sqlstmt"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/cluster"
)

const (
	// reopenWindow is how long after an incident was last seen an occurrence still
	// joins it.
	reopenWindow = 24 * time.Hour
	// retention is how long a recovered incident is kept after it was last seen.
	retention = 30 * 24 * time.Hour
)

// store holds the incidents in app.db.
type store struct {
	db  *appdb.DB
	set *sqlstmt.Set[stmtID]
}

func newStore(db *appdb.DB) (*store, error) {
	set, err := sqlstmt.Prepare[stmtID](context.Background(), db.Write.DB, db.Read.DB, statements)
	if err != nil {
		return nil, fmt.Errorf("prepare monitor statements: %w", err)
	}
	return &store{db: db, set: set}, nil
}

func (s *store) close() error { return s.set.Close() }

func fromMillis(ms int64) time.Time { return time.UnixMilli(ms).UTC() }

// scope is where a pair's incidents are: a cluster, at the server its context
// reached.
type scope struct {
	cluster cluster.ClusterID
	server  string
}

// pairArgs is lead, then the arguments of pairWhere.
func pairArgs(sc scope, p Pair, lead ...any) []any {
	return append(lead, string(sc.cluster), sc.server, p.Subject.Group, p.Subject.Kind, p.Subject.Namespace, p.Subject.Name, string(p.Signal))
}

// apply writes one fold's changes in one transaction, and tells readers after the
// commit when they change what is served.
func (s *store) apply(ctx context.Context, sc scope, c changes) error {
	if c.empty() {
		return nil
	}
	err := s.set.InTx(ctx, func(st stmts) error {
		for _, x := range c.starts {
			if err := s.start(ctx, st, sc, x); err != nil {
				return err
			}
		}
		for _, x := range c.ends {
			if _, err := st.Exec(ctx, stmtEndOccurrence, pairArgs(sc, x.pair, x.at.UnixMilli())...); err != nil {
				return fmt.Errorf("end occurrence: %w", err)
			}
		}
		for _, x := range c.seen {
			if _, err := st.Exec(ctx, stmtSeeIncident, pairArgs(sc, x.pair, x.at.UnixMilli())...); err != nil {
				return fmt.Errorf("see incident: %w", err)
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	if c.notifies() {
		s.db.Notify(appdb.KeyIncidents)
	}
	return nil
}

// start adds an occurrence to the pair's latest incident when it reopens it, and
// opens a new incident otherwise. An incident still open keeps its occurrence: memory
// that lost it, as a cluster gone from one listing does, must not wedge the cluster's
// folds on a second.
func (s *store) start(ctx context.Context, st stmts, sc scope, x start) error {
	var id string
	var lastSeen int64
	var open bool
	err := st.QueryRow(ctx, stmtSelectLatestIncident, pairArgs(sc, x.pair)...).Scan(&id, &lastSeen, &open)
	switch {
	case err == nil && open:
		if _, err := st.Exec(ctx, stmtSeeIncident, pairArgs(sc, x.pair, x.seen.UnixMilli())...); err != nil {
			return fmt.Errorf("see incident: %w", err)
		}
		return nil
	case err == nil && x.at.Sub(fromMillis(lastSeen)) <= reopenWindow:
		if _, err := st.Exec(ctx, stmtReopenIncident, x.seen.UnixMilli(), id); err != nil {
			return fmt.Errorf("reopen incident: %w", err)
		}
	case err == nil, errors.Is(err, sql.ErrNoRows):
		id = appdb.NewID()
		args := append(pairArgs(sc, x.pair, id), x.at.UnixMilli(), x.seen.UnixMilli())
		if _, err := st.Exec(ctx, stmtInsertIncident, args...); err != nil {
			return fmt.Errorf("insert incident: %w", err)
		}
	default: // coverage:ignore a read inside the write transaction fails only when the file does
		return fmt.Errorf("select incident: %w", err)
	}
	if _, err := st.Exec(ctx, stmtInsertOccurrence, appdb.NewID(), id, x.at.UnixMilli()); err != nil {
		return fmt.Errorf("insert occurrence: %w", err)
	}
	if _, err := st.Exec(ctx, stmtTrimOccurrences, id); err != nil {
		return fmt.Errorf("trim occurrences: %w", err)
	}
	return nil
}

// sweep deletes every incident that recovered more than retention before now.
func (s *store) sweep(ctx context.Context, now time.Time) error {
	res, err := s.set.Stmts().Exec(ctx, stmtDeleteRecoveredBefore, now.Add(-retention).UnixMilli())
	if err != nil {
		return fmt.Errorf("delete recovered incidents: %w", err)
	}
	return s.notifyIfAny(res)
}

// endOpen ends a cluster's open occurrences, each when its incident was last seen.
func (s *store) endOpen(ctx context.Context, clusterID cluster.ClusterID) error {
	res, err := s.set.Stmts().Exec(ctx, stmtEndClusterOccurrences, string(clusterID))
	if err != nil {
		return fmt.Errorf("end the cluster's occurrences: %w", err)
	}
	return s.notifyIfAny(res)
}

func (s *store) notifyIfAny(res sql.Result) error {
	n, err := res.RowsAffected()
	if err != nil { // coverage:ignore SQLite always reports the rows a statement changed
		return err
	}
	if n > 0 {
		s.db.Notify(appdb.KeyIncidents)
	}
	return nil
}

// endOtherServers ends the open occurrences of sc's cluster at any other server, each
// when its incident was last seen.
func (s *store) endOtherServers(ctx context.Context, sc scope) error {
	res, err := s.set.Stmts().Exec(ctx, stmtEndOtherServersOccurrences, string(sc.cluster), sc.server)
	if err != nil {
		return fmt.Errorf("end other servers' occurrences: %w", err)
	}
	return s.notifyIfAny(res)
}

// loadOpen is sc's open occurrences, for a seed.
func (s *store) loadOpen(ctx context.Context, sc scope) ([]openOccurrence, error) {
	rows, err := s.set.Stmts().Query(ctx, stmtSelectOpenOccurrences, string(sc.cluster), sc.server)
	if err != nil {
		return nil, fmt.Errorf("select open occurrences: %w", err)
	}
	defer rows.Close()
	var out []openOccurrence
	for rows.Next() {
		var o openOccurrence
		var signal string
		var started, lastSeen int64
		subj := &o.pair.Subject
		if err := rows.Scan(&subj.Group, &subj.Kind, &subj.Namespace, &subj.Name, &signal, &started, &lastSeen); err != nil { // coverage:ignore STRICT columns always scan into these types
			return nil, err
		}
		o.pair.Signal, o.startedAt, o.lastSeen = Signal(signal), fromMillis(started), fromMillis(lastSeen)
		out = append(out, o)
	}
	return out, rows.Err()
}
