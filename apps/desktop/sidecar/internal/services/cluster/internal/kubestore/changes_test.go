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

package kubestore

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/watch"
)

// uids is what a changes read is asserted on: which rows moved, in the order they came.
func uids(rows []ObjectRow) []string {
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.UID)
	}
	return out
}

// The whole point of the stamp: a reader asks for what moved past its cursor instead of
// re-reading the collection, so a write to one row of a hundred returns one row.
func TestObjectChangesReturnsOnlyWhatMovedPastTheCursor(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	s := newTestStore(t)
	require.NoError(t, s.SyncKinds(ctx, []KindRow{podRow}, true, 7))
	require.NoError(t, s.ApplyChange(ctx, podKind, watch.Added, pod("uid-1", "api-0", "42")))
	require.NoError(t, s.ApplyChange(ctx, podKind, watch.Added, pod("uid-2", "api-1", "42")))
	at := storeHead(t, s)

	require.NoError(t, s.ApplyChange(ctx, podKind, watch.Modified, pod("uid-2", "api-1", "43")))

	got, err := s.ObjectChanges(ctx, "v1", "pods", at)
	require.NoError(t, err)
	assert.Equal(t, []string{"uid-2"}, uids(got.Written))
	assert.Equal(t, storeHead(t, s), got.At.Seq, "the read is current at the head it returns")
	assert.Equal(t, "Pod", got.At.Kind)
}

// A cursor at the head has nothing above it. The reader takes that answer and sends no
// frames, which is what a ping for a kind that did not move costs.
func TestObjectChangesAtTheHeadReturnsNothing(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	s := newTestStore(t)
	require.NoError(t, s.SyncKinds(ctx, []KindRow{podRow}, true, 7))
	require.NoError(t, s.ApplyChange(ctx, podKind, watch.Added, pod("uid-1", "api-0", "42")))

	got, err := s.ObjectChanges(ctx, "v1", "pods", storeHead(t, s))

	require.NoError(t, err)
	assert.Empty(t, got.Written)
	assert.Empty(t, got.Deleted)
}

// The rows a relist wrote and the deletes it pruned are one transaction at one position, so
// a reader whose cursor sits below it takes both halves of that relist together.
func TestObjectChangesCarriesBothHalvesOfARelist(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	s := newTestStore(t)
	require.NoError(t, s.SyncKinds(ctx, []KindRow{podRow}, true, 7))
	first := beginReplace(t, s, podKind)
	require.NoError(t, first.WritePage(ctx, []*unstructured.Unstructured{
		pod("uid-1", "api-0", "42"), pod("uid-2", "api-1", "42"),
	}))
	_, err := first.Commit(ctx, "100")
	require.NoError(t, err)
	at := storeHead(t, s)

	// uid-2 is not in the second list, so it is pruned; uid-3 is new.
	second := beginReplace(t, s, podKind)
	require.NoError(t, second.WritePage(ctx, []*unstructured.Unstructured{
		pod("uid-1", "api-0", "42"), pod("uid-3", "api-2", "43"),
	}))
	_, err = second.Commit(ctx, "101")
	require.NoError(t, err)

	got, err := s.ObjectChanges(ctx, "v1", "pods", at)
	require.NoError(t, err)
	assert.Equal(t, []string{"uid-3"}, uids(got.Written), "uid-1 was rewritten unchanged")
	assert.Equal(t, []string{"uid-2"}, got.Deleted)
}

// The mark rides the read, out of the same transaction as the rows: a reader compares it
// against the cursor it came in with to know whether the deletes above that cursor are all
// still in the log, and a mark read separately could be raised between the two.
func TestObjectChangesCarriesTheKindsTrimMark(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	s := newTestStore(t)
	require.NoError(t, s.SyncKinds(ctx, []KindRow{podRow}, true, 7))
	require.NoError(t, s.ApplyChange(ctx, podKind, watch.Added, pod("uid-1", "api-0", "42")))
	require.NoError(t, s.ApplyChange(ctx, podKind, watch.Deleted, pod("uid-1", "api-0", "42")))
	// Everything in the log is older than an hour from now, so the trim takes it all.
	require.NoError(t, trimDeletes(ctx, openFileOf(t, s), time.Now().Add(time.Hour).UnixMilli()))

	got, err := s.ObjectChanges(ctx, "v1", "pods", 0)

	require.NoError(t, err)
	assert.Equal(t, trimmedMark(t, s, "Pod"), got.Trimmed)
	assert.Positive(t, got.Trimmed)
}

// A plural naming no catalog row answers under the empty Kind. The rows are keyed by Kind,
// so an unresolved name matches nothing in either range — a reader holding rows under a
// real Kind takes the empty one as the identity change it is, never as "nothing moved".
func TestObjectChangesReportsAKindItCannotResolve(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	s := newTestStore(t)

	got, err := s.ObjectChanges(ctx, "v1", "pods", 0)

	require.NoError(t, err)
	assert.Empty(t, got.At.Kind)
	assert.Empty(t, got.Written)
}

// Events are one collection with one cursor, and their deletes are logged under the fixed
// ('v1', 'Event') every event delete uses.
func TestEventChangesReturnsWhatMovedAndWhatWent(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	s := newTestStore(t)
	require.NoError(t, s.ApplyChange(ctx, eventsKind, watch.Added, firing("ev-1", "10", 1)))
	require.NoError(t, s.ApplyChange(ctx, eventsKind, watch.Added, firing("ev-2", "10", 1)))
	at := storeHead(t, s)

	require.NoError(t, s.ApplyChange(ctx, eventsKind, watch.Modified, firing("ev-1", "11", 2)))
	require.NoError(t, s.ApplyChange(ctx, eventsKind, watch.Deleted, firing("ev-2", "10", 1)))

	got, err := s.EventChanges(ctx, at)
	require.NoError(t, err)
	require.Len(t, got.Written, 1)
	assert.Equal(t, "ev-1", got.Written[0].UID)
	assert.Equal(t, []string{"ev-2"}, got.Deleted)
	assert.Equal(t, storeHead(t, s), got.At.Seq)
	assert.Equal(t, "Event", got.At.Kind, "the events collection is always addressable")
}

// A changes read is assembled from four reads, and any of them can fail. Each failure has
// to reach the caller: a watch retries in place on an error, where an empty answer would be
// taken as "nothing moved" and advance its cursor past changes it never sent.
func TestObjectChangesReportsAReadItCannotMake(t *testing.T) {
	t.Parallel()

	breaks := map[string]func(*testing.T, *Store){
		"the kind": func(t *testing.T, s *Store) { dropTable(t, s, "kind_catalog") },
		"the rows": func(t *testing.T, s *Store) { dropTable(t, s, "objects") },
		"the log":  func(t *testing.T, s *Store) { dropTable(t, s, "deletes") },
		"the trim mark": func(t *testing.T, s *Store) {
			require.NoError(t, setMeta(context.Background(), openFileOf(t, s).set.Stmts(),
				deletesTrimmedKey("v1", "Pod"), "not-a-number"))
		},
	}
	for name, brk := range breaks {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			s := newTestStore(t)
			require.NoError(t, s.SyncKinds(ctx, []KindRow{podRow}, true, 7))
			require.NoError(t, s.ApplyChange(ctx, podKind, watch.Added, pod("uid-1", "api-0", "42")))
			brk(t, s)

			_, err := s.ObjectChanges(ctx, "v1", "pods", 0)

			assert.Error(t, err)
		})
	}
}

func TestEventChangesReportsAReadItCannotMake(t *testing.T) {
	t.Parallel()

	breaks := map[string]func(*testing.T, *Store){
		"the rows": func(t *testing.T, s *Store) { dropTable(t, s, "events") },
		"the log":  func(t *testing.T, s *Store) { dropTable(t, s, "deletes") },
		"the trim mark": func(t *testing.T, s *Store) {
			require.NoError(t, setMeta(context.Background(), openFileOf(t, s).set.Stmts(),
				deletesTrimmedKey(eventsLogAPIVersion, eventsLogKind), "not-a-number"))
		},
	}
	for name, brk := range breaks {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			s := newTestStore(t)
			require.NoError(t, s.ApplyChange(ctx, eventsKind, watch.Added, firing("ev-1", "10", 1)))
			brk(t, s)

			_, err := s.EventChanges(ctx, 0)

			assert.Error(t, err)
		})
	}
}

// A log entry that will not scan is as unreadable as a log that will not open, and the
// caller has to hear about it for the same reason: silence here reads as "nothing was
// deleted".
func TestObjectChangesReportsALogEntryItCannotScan(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	s := newTestStore(t)
	require.NoError(t, s.SyncKinds(ctx, []KindRow{podRow}, true, 7))
	// A NULL uid: the column the reader keys the removal by, carrying nothing to key on.
	swapTable(t, s, "deletes",
		`SELECT 1 AS seq, 'v1' AS api_version, 'Pod' AS kind, NULL AS uid, 0 AS at`)

	_, err := s.ObjectChanges(ctx, "v1", "pods", 0)

	assert.Error(t, err)
}

// A claim whose cache went away answers ErrClosed rather than an empty collection: the rows
// are not gone, the file this claim was bound to is. The watch reads that as the clean end
// it is, where an empty answer would blank the client's table on the way out.
func TestChangesReportACacheThatWentAway(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	s := closedStore(t)

	_, objErr := s.ObjectChanges(ctx, "v1", "pods", 0)
	_, evErr := s.EventChanges(ctx, 0)

	assert.ErrorIs(t, objErr, ErrClosed)
	assert.ErrorIs(t, evErr, ErrClosed)
}
