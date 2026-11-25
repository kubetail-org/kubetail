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

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/watch"
)

// deleteEntry is one logged delete: the position and the kind it was logged under.
type deleteEntry struct {
	Seq        int64
	APIVersion string
	Kind       string
	UID        string
}

// loggedDeletes reads the whole log, oldest first.
func loggedDeletes(t *testing.T, s *Store) []deleteEntry {
	t.Helper()
	rows, err := db(t, s).QueryContext(context.Background(),
		`SELECT seq, api_version, kind, uid FROM deletes ORDER BY seq, uid`)
	require.NoError(t, err)
	defer rows.Close()

	out := []deleteEntry{}
	for rows.Next() {
		var e deleteEntry
		require.NoError(t, rows.Scan(&e.Seq, &e.APIVersion, &e.Kind, &e.UID))
		out = append(out, e)
	}
	require.NoError(t, rows.Err())
	return out
}

// storeHead is the file's counter, as a reader reads it.
func storeHead(t *testing.T, s *Store) int64 {
	t.Helper()
	at, err := head(context.Background(), openFileOf(t, s).set.Stmts())
	require.NoError(t, err)
	return at
}

// A reader learns of a delete after the row it would read is gone, so the delete leaves
// the one thing the reader still needs: the uid, at a position above every stamp before it.
func TestAWatchDeleteLogsTheObjectItRemoved(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	s := newTestStore(t)
	require.NoError(t, s.ApplyChange(ctx, podKind, watch.Added, pod("uid-1", "api-0", "42")))
	written := writeSeq(t, s, "uid-1")

	require.NoError(t, s.ApplyChange(ctx, podKind, watch.Deleted, pod("uid-1", "api-0", "42")))

	entries := loggedDeletes(t, s)
	require.Len(t, entries, 1)
	assert.Equal(t, deleteEntry{Seq: entries[0].Seq, APIVersion: "v1", Kind: "Pod", UID: "uid-1"}, entries[0])
	assert.Greater(t, entries[0].Seq, written)
	assert.Equal(t, entries[0].Seq, storeHead(t, s), "the head is the delete's own position")
}

// Events log under the fixed ('v1', 'Event') the count triggers already use: the table
// conflates both spellings of an event into one row shape, so all of them roll into it.
func TestAWatchDeleteLogsTheEventItRemoved(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	s := newTestStore(t)
	require.NoError(t, s.ApplyChange(ctx, eventsKind, watch.Added, firing("ev-1", "10", 1)))

	require.NoError(t, s.ApplyChange(ctx, eventsKind, watch.Deleted, firing("ev-1", "10", 1)))

	entries := loggedDeletes(t, s)
	require.Len(t, entries, 1)
	assert.Equal(t, "v1", entries[0].APIVersion)
	assert.Equal(t, "Event", entries[0].Kind)
	assert.Equal(t, "ev-1", entries[0].UID)
}

// The log follows the delete's own predicate, so a delete that takes no row logs nothing —
// a phantom entry would have a reader drop a row it still holds.
func TestADeleteOfAnAbsentRowLogsNothing(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	s := newTestStore(t)

	require.NoError(t, s.ApplyChange(ctx, podKind, watch.Deleted, pod("uid-gone", "api-0", "42")))

	assert.Empty(t, loggedDeletes(t, s))
}

// The relist prune takes every row the LIST did not carry, and each one is a delete a
// reader has to hear about.
func TestARelistPruneLogsEveryRowItTook(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	s := newTestStore(t)
	first := beginReplace(t, s, podKind)
	require.NoError(t, first.WritePage(ctx, []*unstructured.Unstructured{
		pod("uid-1", "api-0", "42"), pod("uid-2", "api-1", "43"),
	}))
	_, err := first.Commit(ctx, "100")
	require.NoError(t, err)

	// The second pass carries only uid-1, so uid-2 is pruned.
	second := beginReplace(t, s, podKind)
	require.NoError(t, second.WritePage(ctx, []*unstructured.Unstructured{pod("uid-1", "api-0", "42")}))
	pruned, err := second.Commit(ctx, "101")
	require.NoError(t, err)

	require.Equal(t, 1, pruned)
	entries := loggedDeletes(t, s)
	require.Len(t, entries, 1)
	assert.Equal(t, deleteEntry{Seq: entries[0].Seq, APIVersion: "v1", Kind: "Pod", UID: "uid-2"}, entries[0])
}

// The events prune is unscoped — the table is the events collection's outright — so it
// logs under the same fixed key every other event delete does.
func TestAnEventsPruneLogsEveryRowItTook(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	s := newTestStore(t)
	first := beginReplace(t, s, eventsKind)
	require.NoError(t, first.WritePage(ctx, []*unstructured.Unstructured{firing("ev-1", "10", 1)}))
	_, err := first.Commit(ctx, "100")
	require.NoError(t, err)

	second := beginReplace(t, s, eventsKind)
	require.NoError(t, second.WritePage(ctx, []*unstructured.Unstructured{firing("ev-2", "11", 1)}))
	_, err = second.Commit(ctx, "101")
	require.NoError(t, err)

	entries := loggedDeletes(t, s)
	require.Len(t, entries, 1)
	assert.Equal(t, deleteEntry{Seq: entries[0].Seq, APIVersion: "v1", Kind: "Event", UID: "ev-1"}, entries[0])
}

// A kind that has stopped being synced loses every row at once, and a watch still holding
// them has to hear about each one.
func TestClearingAKindLogsEveryRowItEvicted(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	s := newTestStore(t)
	require.NoError(t, s.ApplyChange(ctx, podKind, watch.Added, pod("uid-1", "api-0", "42")))
	require.NoError(t, s.ApplyChange(ctx, podKind, watch.Added, pod("uid-2", "api-1", "43")))

	require.NoError(t, s.ClearKind(ctx, podKind))

	entries := loggedDeletes(t, s)
	require.Len(t, entries, 2)
	assert.Equal(t, []string{"uid-1", "uid-2"}, []string{entries[0].UID, entries[1].UID})
	assert.Equal(t, entries[0].Seq, entries[1].Seq, "one transaction, one position")
}

func TestClearingTheEventsKindLogsEveryEvent(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	s := newTestStore(t)
	require.NoError(t, s.ApplyChange(ctx, eventsKind, watch.Added, firing("ev-1", "10", 1)))

	require.NoError(t, s.ClearKind(ctx, eventsKind))

	entries := loggedDeletes(t, s)
	require.Len(t, entries, 1)
	assert.Equal(t, deleteEntry{Seq: entries[0].Seq, APIVersion: "v1", Kind: "Event", UID: "ev-1"}, entries[0])
}

// Kubernetes never reuses a uid, but a row and an entry for the same uid still coexist:
// Clear logs a delete per row and the restarted sync lists the same objects back above it.
// A reader that applies deletes before writes lands on the live row.
func TestAClearedKindRelistedLeavesTheRowAboveItsDelete(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	s := newTestStore(t)
	require.NoError(t, s.ApplyChange(ctx, podKind, watch.Added, pod("uid-1", "api-0", "42")))
	require.NoError(t, s.ClearKind(ctx, podKind))

	require.NoError(t, s.ApplyChange(ctx, podKind, watch.Added, pod("uid-1", "api-0", "42")))

	entries := loggedDeletes(t, s)
	require.Len(t, entries, 1)
	assert.Greater(t, writeSeq(t, s, "uid-1"), entries[0].Seq)
}

// A reader takes both a kind's rows and its deletes by (api_version, kind), so a row that
// moved out of one has to leave an entry behind under the kind it left — it is gone from
// that kind as surely as a deleted row is, and nothing else would say so.
func TestAnObjectThatChangesKindLogsADeleteUnderTheKindItLeft(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	s := newTestStore(t)
	renamed := Kind{APIVersion: "v1beta1", Kind: "Pod", Resource: "pods"}
	require.NoError(t, s.ApplyChange(ctx, podKind, watch.Added, pod("uid-1", "api-0", "42")))

	require.NoError(t, s.ApplyChange(ctx, renamed, watch.Modified, pod("uid-1", "api-0", "42")))

	entries := loggedDeletes(t, s)
	require.Len(t, entries, 1)
	assert.Equal(t, deleteEntry{Seq: entries[0].Seq, APIVersion: "v1", Kind: "Pod", UID: "uid-1"}, entries[0])
	assert.Equal(t, entries[0].Seq, writeSeq(t, s, "uid-1"), "the row's new position is the departure's")
}

// A mark that will not parse is not a fresh file. The mark is there because entries went,
// so answering 0 would tell a reader every cursor it holds is still valid — the missed
// delete the whole log exists to prevent. An error sends it down its own recovery path.
func TestAnUnreadableTrimMarkIsAnError(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	s := newTestStore(t)
	st := openFileOf(t, s).set.Stmts()
	require.NoError(t, setMeta(ctx, st, deletesTrimmedKey("v1", "Pod"), "not-a-number"))

	_, err := trimmed(ctx, st, "v1", "Pod")

	assert.Error(t, err)
}

// The trim is one transaction over two statements, and either half failing has to be
// reported: the janitor logs it and the next sweep retries, where a silent failure would
// leave the log growing and the marks describing a trim that never happened.
func TestTrimDeletesReportsWhatItCouldNotDo(t *testing.T) {
	t.Parallel()

	t.Run("the transaction it could not open", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		err := trimDeletes(ctx, openFileOf(t, newTestStore(t)), 0)

		assert.Error(t, err)
	})

	t.Run("the marks it could not write", func(t *testing.T) {
		s := newTestStore(t)
		dropTable(t, s, "cluster_meta")

		err := trimDeletes(context.Background(), openFileOf(t, s), 0)

		assert.Error(t, err)
	})

	t.Run("the entries it could not remove", func(t *testing.T) {
		s := newTestStore(t)
		// A view reads for the marks and refuses the delete, so the trim fails with its
		// marks already written — the half the raise-only rule has to survive.
		swapTable(t, s, "deletes",
			`SELECT 1 AS seq, 'v1' AS api_version, 'Pod' AS kind, 'uid-1' AS uid, 0 AS at`)

		err := trimDeletes(context.Background(), openFileOf(t, s), 1)

		assert.Error(t, err)
	})
}
