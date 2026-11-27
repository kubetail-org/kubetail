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

package cluster

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/amorey/beehive"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Messages come from unbounded sources (raw client-go errors, kilobyte /readyz
// bodies) and are re-serialized to every watcher on every frame.
func TestTruncateMessageCapsOverlongInput(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "short", TruncateMessage("short"))

	exact := strings.Repeat("x", MaxMessageLen)
	assert.Equal(t, exact, TruncateMessage(exact), "a message at the cap is untouched")

	got := TruncateMessage(strings.Repeat("x", MaxMessageLen+50))
	assert.Equal(t, strings.Repeat("x", MaxMessageLen)+"…", got)
}

// LiveCondition is the sole constructor, which is what makes the message cap and
// the liveness flag unskippable.
func TestLiveConditionCapsItsMessageAndMarksLiveness(t *testing.T) {
	t.Parallel()

	c := LiveCondition(ConditionConnected, ConditionFalse, ReasonProbeFailed, strings.Repeat("x", MaxMessageLen+1))
	assert.Equal(t, string(ConditionConnected), c.Type)
	assert.Equal(t, ConditionFalse, c.Status)
	assert.Equal(t, ReasonProbeFailed, c.Reason)
	assert.True(t, c.Liveness, "every condition here is process-scoped")
	assert.Equal(t, MaxMessageLen+len("…"), len(c.Message))
}

// FindCondition returns a pointer INTO the slice, so a caller can read the live row
// rather than a copy.
func TestFindCondition(t *testing.T) {
	t.Parallel()

	conds := []Condition{
		{Type: string(ConditionConnected), Reason: ReasonConnected},
		{Type: string(ConditionIdentified), Reason: ReasonIdentified},
	}
	got := FindCondition(conds, ConditionIdentified)
	require.NotNil(t, got)
	assert.Equal(t, ReasonIdentified, got.Reason)
	assert.Same(t, &conds[1], got)

	assert.Nil(t, FindCondition(conds, ConditionSynced), "an absent type is nil, not a zero row")
	assert.Nil(t, FindCondition(nil, ConditionConnected))
}

// The sync-health fold and the cluster-status comparison both use this to decide
// whether a record actually changed. Comparing by instant, not by pointer or by
// wall-clock struct, is what keeps two readings of the same stamp from looking
// different — a monotonic-clock reading or a differing location would.
func TestTimePtrEqual(t *testing.T) {
	t.Parallel()

	at := time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC)
	other := at.In(time.FixedZone("elsewhere", 3600))
	later := at.Add(time.Second)

	assert.True(t, TimePtrEqual(nil, nil), "both absent is equal")
	assert.True(t, TimePtrEqual(&at, &at))
	assert.True(t, TimePtrEqual(&at, &other), "same instant, different location")
	assert.False(t, TimePtrEqual(&at, &later))
	assert.False(t, TimePtrEqual(&at, nil))
	assert.False(t, TimePtrEqual(nil, &at))
}

// --- toOwnerRef ---

// A collected object's outgoing edges go with it, so a departure frame carries no
// owner. The zero ref is what lets that frame reach a consumer at all: an error would
// null the entity, and a change with no entity is dropped rather than folded — so the
// removal would never land and the record would sit on screen for the life of the
// subscription.
func TestToOwnerRefWithNoOwnerIsTheZeroRef(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	client := beehive.NewClient[ClusterCacheSpec, ClusterCacheStatus](newTestBeehive(t), ClusterCacheGroupKind)
	_, err := client.Create(ctx, "orphan", ClusterCacheSpec{})
	require.NoError(t, err)

	obj, err := client.GetByName(ctx, "orphan", beehive.LoadOwner())
	require.NoError(t, err)

	ref, err := toOwnerRef(obj)

	require.NoError(t, err)
	assert.Equal(t, ObjectRef{}, ref)
}

// Forgetting beehive.LoadOwner is a caller bug, not a state the store can be in, so it
// stays an error rather than reading as an absent owner.
func TestToOwnerRefReportsAnUnloadedEdge(t *testing.T) {
	t.Parallel()

	_, err := toOwnerRef(&beehive.Object[ClusterCacheSpec, ClusterCacheStatus]{ID: 7, Kind: "ClusterCache"})

	assert.ErrorIs(t, err, beehive.ErrNotLoaded)
}
