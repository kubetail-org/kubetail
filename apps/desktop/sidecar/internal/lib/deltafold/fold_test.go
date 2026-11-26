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

package deltafold

import (
	"context"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/apimeta"
)

type row struct {
	ID   string
	Body string
}

type frame struct {
	Type apimeta.CollectionWatchFrameType
	Row  *row
}

func rowKey(r row) string { return r.ID }

func rowFrame(t apimeta.CollectionWatchFrameType, r row) frame {
	if t == apimeta.CollectionWatchFrameBookmark {
		return frame{Type: t}
	}
	return frame{Type: t, Row: &r}
}

func newRowFold() *Fold[string, row, frame] { return New(rowKey, Equal[row], rowFrame) }

// collect takes exactly n frames off out.
func collect(t *testing.T, out <-chan frame, n int) []frame {
	t.Helper()
	got := make([]frame, 0, n)
	for range n {
		select {
		case f := <-out:
			got = append(got, f)
		default:
			t.Fatalf("wanted %d frames, got %d", n, len(got))
		}
	}
	select {
	case f := <-out:
		t.Fatalf("unexpected extra frame %+v", f)
	default:
	}
	return got
}

// A snapshot is every record as Added, in order, then one Bookmark carrying nothing.
func TestSnapshotSendsAddedRowsThenABookmark(t *testing.T) {
	out := make(chan frame, 8)
	f := newRowFold()
	require.True(t, f.Snapshot(t.Context(), out, []row{{"a", "1"}, {"b", "2"}}))

	got := collect(t, out, 3)
	assert.Equal(t, apimeta.CollectionWatchFrameAdded, got[0].Type)
	assert.Equal(t, "a", got[0].Row.ID)
	assert.Equal(t, apimeta.CollectionWatchFrameAdded, got[1].Type)
	assert.Equal(t, "b", got[1].Row.ID)
	assert.Equal(t, frame{Type: apimeta.CollectionWatchFrameBookmark}, got[2])
	assert.True(t, f.Has("a"))
	assert.False(t, f.Has("c"))
}

// A re-read diffs against what was sent: new is Added, changed is Modified, gone is
// Deleted carrying the last-known row, unchanged sends nothing.
func TestDiffSendsWhatChanged(t *testing.T) {
	out := make(chan frame, 8)
	f := newRowFold()
	require.True(t, f.Snapshot(t.Context(), out, []row{{"a", "1"}, {"b", "2"}, {"c", "3"}}))
	collect(t, out, 4)

	require.True(t, f.Diff(t.Context(), out, []row{{"a", "1"}, {"b", "changed"}, {"d", "4"}}))
	got := collect(t, out, 3)
	assert.Equal(t, frame{Type: apimeta.CollectionWatchFrameModified, Row: &row{"b", "changed"}}, got[0])
	assert.Equal(t, frame{Type: apimeta.CollectionWatchFrameAdded, Row: &row{"d", "4"}}, got[1])
	assert.Equal(t, frame{Type: apimeta.CollectionWatchFrameDeleted, Row: &row{"c", "3"}}, got[2])
	assert.False(t, f.Has("c"))
	assert.True(t, f.Has("d"))
}

// Upsert is one record's Added or Modified, or nothing when it is what was sent.
func TestUpsertSuppressesAnUnchangedRecord(t *testing.T) {
	out := make(chan frame, 8)
	f := newRowFold()
	require.True(t, f.Upsert(t.Context(), out, row{"a", "1"}))
	require.True(t, f.Upsert(t.Context(), out, row{"a", "1"}))
	require.True(t, f.Upsert(t.Context(), out, row{"a", "2"}))
	got := collect(t, out, 2)
	assert.Equal(t, apimeta.CollectionWatchFrameAdded, got[0].Type)
	assert.Equal(t, apimeta.CollectionWatchFrameModified, got[1].Type)
}

// Equality is the caller's, so a record with a slice can still be diffed.
func TestEqualityIsTheCallers(t *testing.T) {
	type tagged struct {
		ID   string
		Tags []string
	}
	f := New(
		func(r tagged) string { return r.ID },
		func(a, b tagged) bool { return a.ID == b.ID && slices.Equal(a.Tags, b.Tags) },
		func(t apimeta.CollectionWatchFrameType, r tagged) apimeta.CollectionWatchFrameType { return t },
	)
	out := make(chan apimeta.CollectionWatchFrameType, 8)
	require.True(t, f.Upsert(t.Context(), out, tagged{"a", []string{"x"}}))
	require.True(t, f.Upsert(t.Context(), out, tagged{"a", []string{"x"}}))
	require.True(t, f.Upsert(t.Context(), out, tagged{"a", []string{"x", "y"}}))
	assert.Equal(t, apimeta.CollectionWatchFrameAdded, <-out)
	assert.Equal(t, apimeta.CollectionWatchFrameModified, <-out)
	select {
	case got := <-out:
		t.Fatalf("unexpected frame %v", got)
	default:
	}
}

// A consumer that stopped reading ends every send with false rather than a hang.
func TestASendToAGoneConsumerReportsFalse(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	out := make(chan frame) // unbuffered: nobody reads
	f := newRowFold()
	assert.False(t, f.Snapshot(ctx, out, []row{{"a", "1"}}))
	assert.False(t, f.Diff(ctx, out, []row{{"b", "1"}}))
	assert.False(t, f.Upsert(ctx, out, row{"c", "1"}))
	assert.False(t, Send(ctx, out, frame{}))
}
