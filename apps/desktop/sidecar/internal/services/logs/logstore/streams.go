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

package logstore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/sqlstmt"
)

// Stream is one streams row: a runtime instance of a container, named by its namespace,
// pod, container and ContainerID, and the snapshot of it taken from the mirror while the
// pod was there. StartedAt and FinishedAt are unix nanoseconds, zero when unknown, and
// FinishedAt zero while the instance runs. Owners is the UID of each controller above
// the pod, nearest first, as the mirror shows it; a row the file answers carries none.
type Stream struct {
	ID          int64
	Namespace   string
	Pod         string
	PodUID      string
	Container   string
	Default     bool
	ContainerID string
	StartedAt   int64
	FinishedAt  int64
	Node        string
	Region      string
	Zone        string
	OS          string
	Arch        string
	Labels      string
	Owners      []string
}

// PutStreams files each row's instance, inserting the ones the file lacks and updating
// the place, the labels, the default mark and the times of the rest, and answers each
// row's id, in order. A row filed as its pod's default moves the mark onto every
// instance of its container and off the pod's other containers. Each row's Owners the
// file lacks are filed for its pod: the mirror can show a pod before its ReplicaSet,
// so a later filing adds what an earlier one could not. A stream whose finished_at it
// sets is trimmed to it. Rows the file already holds as they are take no write.
func (s *Store) PutStreams(ctx context.Context, rows []Stream) ([]int64, error) {
	ids, filed, err := s.filed(ctx, rows)
	switch {
	case err != nil:
		return nil, fmt.Errorf("put streams: %w", err)
	case filed:
		return ids, nil
	}
	err = s.inTx(ctx, func(ctx context.Context, st stmts) error {
		for i, row := range rows {
			id, finished, err := putStream(ctx, st, row)
			if err != nil {
				return err
			}
			if row.Default {
				if _, err := st.Exec(ctx, stmtMoveDefault, row.Namespace, row.Pod, row.PodUID, row.Container); err != nil {
					return err
				}
			}
			ids[i] = id
			for _, owner := range row.Owners {
				if _, err := st.Exec(ctx, stmtInsertOwner, row.PodUID, owner); err != nil {
					return err
				}
			}
			if finished {
				if _, err := trim(ctx, st, id, row.FinishedAt); err != nil {
					return err
				}
			}
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("put streams: %w", err)
	}
	return ids, nil
}

// FinishStreams sets each row's FinishedAt on the file's row of its instance, when
// the file holds one with none yet, and trims it: an instance read as the current one
// is learned finished by a resolve that reads the next, which files no row of it.
// Rows the file holds finished already take no write.
func (s *Store) FinishStreams(ctx context.Context, rows []Stream) error {
	unfinished, err := s.unfinished(ctx, rows)
	switch {
	case err != nil:
		return fmt.Errorf("finish streams: %w", err)
	case !unfinished:
		return nil
	}
	err = s.inTx(ctx, func(ctx context.Context, st stmts) error {
		for _, row := range rows {
			var id int64
			err := st.QueryRow(ctx, stmtFinishStream, row.Namespace, row.Pod, row.Container, row.ContainerID, row.FinishedAt).Scan(&id)
			switch {
			case errors.Is(err, sql.ErrNoRows):
				continue
			case err != nil:
				return err
			}
			if _, err := trim(ctx, st, id, row.FinishedAt); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("finish streams: %w", err)
	}
	return nil
}

// filed is each row's id when the file already holds every row as PutStreams would
// leave it, its pod's owners included, so a page over streams that did not change
// takes no write lock.
func (s *Store) filed(ctx context.Context, rows []Stream) ([]int64, bool, error) {
	ids := make([]int64, len(rows))
	filed := false
	err := s.inReadTx(ctx, func(ctx context.Context, st stmts) error {
		for i, row := range rows {
			held, ok, err := selectStream(ctx, st, row)
			if err != nil || !ok || !current(held, row) {
				return err
			}
			if row.Default {
				var unsettled bool
				if err := st.QueryRow(ctx, stmtDefaultUnsettled, row.Namespace, row.Pod, row.PodUID, row.Container).Scan(&unsettled); err != nil || unsettled {
					return err
				}
			}
			for _, owner := range row.Owners {
				var known bool
				if err := st.QueryRow(ctx, stmtOwnerFiled, row.PodUID, owner).Scan(&known); err != nil || !known {
					return err
				}
			}
			ids[i] = held.ID
		}
		filed = true
		return nil
	})
	return ids, filed, err
}

// selectStream is the file's row of row's instance, and whether it holds one.
func selectStream(ctx context.Context, st stmts, row Stream) (Stream, bool, error) {
	rows, err := st.Query(ctx, stmtSelectStream, row.Namespace, row.Pod, row.Container, row.ContainerID)
	if err != nil {
		return Stream{}, false, err
	}
	defer rows.Close()
	if !rows.Next() {
		return Stream{}, false, rows.Err()
	}
	held, err := scanStream(rows)
	return held, err == nil, err
}

// current is whether held, the file's row of row's instance, is what
// stmtUpdateStream would leave it as.
func current(held, row Stream) bool {
	return held.Node == row.Node && held.Region == row.Region && held.Zone == row.Zone &&
		held.OS == row.OS && held.Arch == row.Arch && held.Labels == row.Labels && held.Default == row.Default &&
		(held.StartedAt != 0 || row.StartedAt == 0) && (held.FinishedAt != 0 || row.FinishedAt == 0)
}

// unfinished is whether the file holds a row of one of rows' instances with no
// finished_at, which FinishStreams would set.
func (s *Store) unfinished(ctx context.Context, rows []Stream) (bool, error) {
	unfinished := false
	err := s.inReadTx(ctx, func(ctx context.Context, st stmts) error {
		for _, row := range rows {
			var id int64
			var finished sql.NullInt64
			err := st.QueryRow(ctx, stmtSelectStreamState, row.Namespace, row.Pod, row.Container, row.ContainerID).Scan(&id, &finished)
			switch {
			case errors.Is(err, sql.ErrNoRows):
				continue
			case err != nil:
				return err
			}
			if !finished.Valid {
				unfinished = true
				return nil
			}
		}
		return nil
	})
	return unfinished, err
}

// putStream files row and answers its id, and whether this write set its finished_at.
func putStream(ctx context.Context, st stmts, row Stream) (id int64, finished bool, err error) {
	name := []any{row.Namespace, row.Pod, row.Container, row.ContainerID}
	var was sql.NullInt64
	err = st.QueryRow(ctx, stmtSelectStreamState, name...).Scan(&id, &was)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		err = st.QueryRow(ctx, stmtInsertStream, row.Namespace, row.Pod, row.PodUID, row.Container, row.Default, row.ContainerID,
			nullable(row.StartedAt), nullable(row.FinishedAt), row.Node, row.Region, row.Zone, row.OS, row.Arch, row.Labels).Scan(&id)
		return id, err == nil && row.FinishedAt != 0, err
	case err != nil: // coverage:ignore filed just read the same row; only the file failing between reaches it
		return 0, false, err
	}
	_, err = st.Exec(ctx, stmtUpdateStream, append(name, row.Node, row.Region, row.Zone, row.OS, row.Arch, row.Labels,
		nullable(row.StartedAt), nullable(row.FinishedAt), row.Default)...)
	return id, err == nil && !was.Valid && row.FinishedAt != 0, err
}

// PodStreams is every stream the file holds of the pod namespace/pod, of the pod podUID
// alone unless it is empty: a later pod of one name is another pod.
func (s *Store) PodStreams(ctx context.Context, namespace, pod, podUID string) ([]Stream, error) {
	var out []Stream
	err := s.with(ctx, func(ctx context.Context, st stmts) error {
		rows, err := st.Query(ctx, stmtSelectPodStreams, namespace, pod, podUID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			row, err := scanStream(rows)
			if err != nil { // coverage:ignore a STRICT row always scans into these fields
				return err
			}
			out = append(out, row)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, fmt.Errorf("pod streams: %w", err)
	}
	return out, nil
}

// Owned is a stream of a pod an owner controls, with its oldest and newest line.
type Owned struct {
	Stream
	Oldest, Newest Pos
}

// OwnedStreams is every stream the file holds of a pod the owner controls, each with
// its oldest and newest line, ordered by pod and then as PodStreams orders them. A
// stream with no line is left out: the janitor evicted it and removes its row on a
// later sweep.
func (s *Store) OwnedStreams(ctx context.Context, owner string) ([]Owned, error) {
	var out []Owned
	err := s.with(ctx, func(ctx context.Context, st stmts) error {
		rows, err := st.Query(ctx, stmtSelectOwnedStreams, owner)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var o Owned
			if o.Stream, err = scanStream(rows, &o.Oldest.TS, &o.Oldest.Ord, &o.Newest.TS, &o.Newest.Ord); err != nil { // coverage:ignore a STRICT row always scans into these fields, and EXISTS gives each its ends
				return err
			}
			out = append(out, o)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, fmt.Errorf("owned streams: %w", err)
	}
	return out, nil
}

// scanStream reads one row of streamColumns, and the columns after them into more.
func scanStream(rows *sql.Rows, more ...any) (Stream, error) {
	var row Stream
	var started, finished sql.NullInt64
	dest := []any{&row.ID, &row.Namespace, &row.Pod, &row.PodUID, &row.Container, &row.Default, &row.ContainerID,
		&started, &finished, &row.Node, &row.Region, &row.Zone, &row.OS, &row.Arch, &row.Labels}
	err := rows.Scan(append(dest, more...)...)
	row.StartedAt, row.FinishedAt = started.Int64, finished.Int64
	return row, err
}

// nullable is n as a column value, NULL for zero: a moment of zero is unknown.
func nullable(n int64) any {
	if n == 0 {
		return nil
	}
	return n
}

// stmts issues the file's statements.
type stmts = sqlstmt.Stmts[stmtID]
