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

// What moved past a cursor: the reads a watch does instead of re-reading a whole collection
// and diffing it. Two range reads, no merge — the rows written above the cursor and the uids
// deleted above it.
package kubestore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// Changes is one collection's answer to "what moved past this position?", read as a whole:
// the caller applies Deleted BEFORE Written, and resumes from At.
//
// **Deletes first because a uid can be in both.** ClearKind logs a delete per row and the
// restarted sync lists the same objects back above it. Written only ever carries rows that
// still exist, so deletes-first lands on the live row; the other order would drop it, and on
// a quiet kind nothing would send it again.
type Changes[T any] struct {
	Written []T
	Deleted []string
	// At is where this read leaves the caller — its next cursor, and only once every frame
	// the read produced is out.
	At Cursor
	// Trimmed is how far this collection's deletes log has been trimmed. A cursor below it
	// has lost deletes it never saw, so the caller falls back to a full read.
	Trimmed int64
}

// Cursor is what a set of rows is current with: the write position they were read at, and
// the identity they were read under.
//
// **The Kind is half the cursor, not a label on it.** Both ranges are keyed by it, and the
// catalog can remap a plural onto a renamed Kind (stmtResolveKindRename) or drop its row —
// either way a cursor taken under the old Kind names rows and log entries the new answer's
// ranges do not cover, and only comparing the two Kinds says so.
type Cursor struct {
	Seq int64
	// Kind is empty for a plural naming no catalog row, which is a cursor over nothing.
	Kind string
}

// ObjectChanges reads one kind's changes above since. Everything comes out of one read
// transaction — the two ranges, the head and the mark — so the answer describes one snapshot
// of the file rather than three.
func (s *Store) ObjectChanges(ctx context.Context, apiVersion, resource string, since int64) (Changes[ObjectRow], error) {
	f, done, err := s.use()
	if err != nil {
		return Changes[ObjectRow]{}, err
	}
	defer done()
	var out Changes[ObjectRow]
	err = inReadTx(ctx, f, "read object changes", func(st stmts) error {
		// The kind is resolved first and by itself, because the two ranges take it as a
		// bound parameter: a scalar subquery would resolve NULL and answer "nothing moved".
		// A plural naming no catalog row answers under the empty Kind and skips the ranges
		// — a caller holding rows sees the identity change and falls back to a full read.
		kind, ok, err := resolveKind(ctx, st, apiVersion, resource)
		if err != nil {
			return err
		}
		out.At.Kind = kind
		if ok {
			if out.Written, err = scanObjects(ctx, st, stmtSelectObjectsSince, apiVersion, kind, since); err != nil {
				return err
			}
			if out.Deleted, err = scanDeletedUIDs(ctx, st, stmtSelectObjectDeletesSince, apiVersion, kind, since); err != nil {
				return err
			}
			if out.Trimmed, err = trimmed(ctx, st, apiVersion, kind); err != nil {
				return err
			}
		}
		out.At.Seq, err = head(ctx, st)
		return err
	})
	return out, err
}

// EventChanges reads the events collection's changes above since. There is one events table
// and one cursor over it, and its deletes are logged under the fixed ('v1', 'Event') — so
// the answer's Kind is never in question.
func (s *Store) EventChanges(ctx context.Context, since int64) (Changes[EventRow], error) {
	f, done, err := s.use()
	if err != nil {
		return Changes[EventRow]{}, err
	}
	defer done()
	out := Changes[EventRow]{At: Cursor{Kind: eventsLogKind}}
	err = inReadTx(ctx, f, "read event changes", func(st stmts) error {
		var err error
		if out.Written, err = scanEvents(ctx, st, stmtSelectEventsSince, since); err != nil {
			return err
		}
		if out.Deleted, err = scanDeletedUIDs(ctx, st, stmtSelectEventDeletesSince, since); err != nil {
			return err
		}
		if out.Trimmed, err = trimmed(ctx, st, eventsLogAPIVersion, eventsLogKind); err != nil {
			return err
		}
		out.At.Seq, err = head(ctx, st)
		return err
	})
	return out, err
}

// resolveKind turns the plural a caller names into the Kind the rows are keyed by. ok is
// false when the catalog has no row for it — a kind the sweep pruned, or one never swept.
func resolveKind(ctx context.Context, st stmts, apiVersion, resource string) (string, bool, error) {
	var kind string
	err := st.QueryRow(ctx, stmtResolveKind, apiVersion, resource).Scan(&kind)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("resolve %s/%s: %w", apiVersion, resource, err)
	}
	return kind, true, nil
}

// scanDeletedUIDs collects one of the log range reads. Identity only: the caller holds each
// row's last-known state and keys the removal by uid.
func scanDeletedUIDs(ctx context.Context, st stmts, id stmtID, args ...any) ([]string, error) {
	rows, err := st.Query(ctx, id, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []string
	for rows.Next() {
		var uid string
		if err := rows.Scan(&uid); err != nil {
			return nil, err
		}
		out = append(out, uid)
	}
	return out, rows.Err()
}
