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

// The clusters table: the row every cluster is, and the reads and writes over it.
package cluster

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// ClusterRow mirrors one clusters row. SourceKey is the kube-context of a
// kubeconfig row and nil for a cloud one; Name is nil until the user sets one;
// MonitoringEnabled is read from the cluster's monitor target, true with none;
// IsDefault marks the user's default cluster, at most one row; DeleteRequestedAt is
// nil until the user deletes the cluster.
type ClusterRow struct {
	ID                ClusterID
	Source            string
	SourceKey         *string
	Name              *string
	Enabled           bool
	SyncEnabled       bool
	MonitoringEnabled bool
	IsDefault         bool
	CreatedAt         time.Time
	UpdatedAt         time.Time
	DeleteRequestedAt *time.Time
}

// SourceKubeconfig is the source of a row the kubeconfig importer made.
const SourceKubeconfig = "kubeconfig"

// Timestamps are stored as unix millis; a minted time goes through normalizeTime
// so it never differs from its own round trip.
func millis(t time.Time) int64            { return t.UnixMilli() }
func fromMillis(ms int64) time.Time       { return time.UnixMilli(ms).UTC() }
func normalizeTime(t time.Time) time.Time { return fromMillis(millis(t)) }

// scanner is what *sql.Row and *sql.Rows share.
type scanner interface{ Scan(dest ...any) error }

func scanClusterRow(s scanner) (ClusterRow, error) {
	var (
		r                    ClusterRow
		createdAt, updatedAt int64
		deleteRequestedAt    sql.NullInt64
	)
	err := s.Scan(&r.ID, &r.Source, &r.SourceKey, &r.Name, &r.Enabled, &r.SyncEnabled, &r.MonitoringEnabled,
		&r.IsDefault, &createdAt, &updatedAt, &deleteRequestedAt)
	if err != nil {
		return ClusterRow{}, err
	}
	r.CreatedAt, r.UpdatedAt = fromMillis(createdAt), fromMillis(updatedAt)
	if deleteRequestedAt.Valid {
		at := fromMillis(deleteRequestedAt.Int64)
		r.DeleteRequestedAt = &at
	}
	return r, nil
}

// insertClusterIfAbsent claims contextName for a new kubeconfig row under id and
// reports whether it did. A context already claimed, marked or not, inserts
// nothing: that is what keeps a returning context's row and toggles.
func insertClusterIfAbsent(ctx context.Context, st stmts, id string, contextName string, at time.Time) (bool, error) {
	return wroteOne(st.QueryRow(ctx, stmtInsertCluster, id, contextName, millis(at), millis(at)), "insert cluster")
}

// wroteOne reads a guarded write's RETURNING id: a row means it wrote, no row
// means the guard held.
func wroteOne(row *sql.Row, what string) (bool, error) {
	var id string
	err := row.Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("%s: %w", what, err)
	}
	return true, nil
}

func getCluster(ctx context.Context, st stmts, id ClusterID) (ClusterRow, bool, error) {
	r, err := scanClusterRow(st.QueryRow(ctx, stmtSelectCluster, string(id)))
	if errors.Is(err, sql.ErrNoRows) {
		return ClusterRow{}, false, nil
	}
	if err != nil {
		return ClusterRow{}, false, fmt.Errorf("get cluster: %w", err)
	}
	return r, true, nil
}

func listClusters(ctx context.Context, st stmts) ([]ClusterRow, error) {
	rows, err := st.Query(ctx, stmtSelectClusters)
	if err != nil {
		return nil, fmt.Errorf("list clusters: %w", err)
	}
	defer rows.Close()

	var out []ClusterRow
	for rows.Next() {
		r, err := scanClusterRow(rows)
		if err != nil { // coverage:ignore the table is STRICT with every column CHECKed, so each row it holds scans
			return nil, fmt.Errorf("list clusters: %w", err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil { // coverage:ignore only the read failing between two steps, which nothing can time
		return nil, fmt.Errorf("list clusters: %w", err)
	}
	return out, nil
}

// setClusterToggle writes one toggle column — stmt is one of the toggle
// statements — and returns the row as written. A row it could not write, absent
// or marked, is ErrNotFound.
func setClusterToggle(ctx context.Context, st stmts, stmt stmtID, id ClusterID, on bool, at time.Time) (ClusterRow, error) {
	r, err := scanClusterRow(st.QueryRow(ctx, stmt, on, millis(at), string(id)))
	if errors.Is(err, sql.ErrNoRows) {
		return ClusterRow{}, fmt.Errorf("update cluster %s: %w", id, ErrNotFound)
	}
	if err != nil {
		return ClusterRow{}, fmt.Errorf("update cluster %s: %w", id, err)
	}
	return r, nil
}

// setClusterDefault makes the row the default, clearing the one before, or stops it
// being the default, and reports whether it wrote. It runs inside a transaction, so
// the read and both writes see one state. A call that changes nothing writes nothing
// and returns the row. An absent or marked row is ErrNotFound; a row canBeDefault
// refuses cannot be made the default, ErrCannotBeDefault.
func setClusterDefault(ctx context.Context, st stmts, id ClusterID, on bool, at time.Time, canBeDefault func(ClusterRow) bool) (ClusterRow, bool, error) {
	row, ok, err := getCluster(ctx, st, id)
	if err != nil {
		return ClusterRow{}, false, err
	}
	if !ok || row.DeleteRequestedAt != nil {
		return ClusterRow{}, false, fmt.Errorf("update cluster %s: %w", id, ErrNotFound)
	}
	if row.IsDefault == on {
		return row, false, nil
	}
	if on {
		if !canBeDefault(row) {
			return ClusterRow{}, false, fmt.Errorf("update cluster %s: %w", id, ErrCannotBeDefault)
		}
		if _, err := st.Exec(ctx, stmtClearClusterDefault, millis(at)); err != nil {
			return ClusterRow{}, false, fmt.Errorf("clear the default: %w", err)
		}
	}
	row, err = setClusterToggle(ctx, st, stmtSetClusterDefault, id, on, at)
	return row, err == nil, err
}

// markCluster asks for the row's deletion and reports whether this call was the one
// that did: a row already marked keeps its first stamp, and an absent one is
// nothing to mark.
func markCluster(ctx context.Context, st stmts, id ClusterID, at time.Time) (bool, error) {
	return wroteOne(st.QueryRow(ctx, stmtMarkCluster, millis(at), millis(at), string(id)), "mark cluster "+string(id))
}

// deleteMarkedCluster removes a marked row no chat is filed under, and reports
// whether it went.
func deleteMarkedCluster(ctx context.Context, st stmts, id ClusterID) (bool, error) {
	return wroteOne(st.QueryRow(ctx, stmtDeleteMarkedCluster, string(id)), "delete cluster "+string(id))
}
