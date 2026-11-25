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
	"testing"
	"time"

	"github.com/amorey/beehive"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/testutil"
)

// addEvent appends one run to id's timeline from outside a pass, standing in for the
// controller that writes it. Generic over the kind's status because an AdminClient is
// scoped to one kind, even though the read under test is not.
func addEvent[Status any](t *testing.T, admin *beehive.AdminClient[Status], id beehive.ObjectID, category, reason string) {
	t.Helper()
	require.NoError(t, admin.AddEvent(context.Background(), id, beehive.EventSpec{
		Category: category,
		Type:     beehive.EventNormal,
		Reason:   reason,
		Message:  reason + " happened",
	}))
}

// reasonsOf reads the machine codes out of a timeline, which is what these tests
// assert on: the run's identity, not its sampled message.
func reasonsOf(events []Event) []string {
	reasons := make([]string, 0, len(events))
	for _, ev := range events {
		reasons = append(reasons, ev.Reason)
	}
	return reasons
}

// One reader serves all three kinds: an event carries no kind of its own, so the read
// is by id and a per-kind branch would be three copies of one call.
func TestListEventsReadsEveryKindsTimeline(t *testing.T) {
	d, bh := newTestDepsAndBeehive(t)
	ctx := context.Background()
	cluster := createCluster(t, d, "prod")
	cache := createCache(t, d.cacheClient, cluster, "uid-1")
	kind := createKind(t, d, cache.ID, deploymentsSpec)

	addEvent(t, beehive.NewAdminClient[ClusterStatus](bh, ClusterGroupKind),
		cluster.ID, ConnectionEventCategory, "Connected")
	addEvent(t, beehive.NewAdminClient[ClusterCacheStatus](bh, ClusterCacheGroupKind),
		cache.ID, categoryDiscovery, "Discovered")
	addEvent(t, beehive.NewAdminClient[ClusterCachedKindStatus](bh, ClusterCachedKindGroupKind),
		kind.ID, categorySync, "SyncComplete")

	svc := serviceOver(t, d)
	for name, tc := range map[string]struct {
		id       beehive.ObjectID
		category string
		reason   string
	}{
		"cluster":    {cluster.ID, ConnectionEventCategory, "Connected"},
		"cache":      {cache.ID, categoryDiscovery, "Discovered"},
		"cachedKind": {kind.ID, categorySync, "SyncComplete"},
	} {
		t.Run(name, func(t *testing.T) {
			got, err := svc.ListEvents(ctx, ObjectID(tc.id), nil, nil)

			require.NoError(t, err)
			require.Len(t, got, 1)
			assert.NotZero(t, got[0].ID, "the run's own id, the client's upsert key")
			assert.Equal(t, tc.category, got[0].Category)
			assert.Equal(t, tc.reason, got[0].Reason)
			assert.Equal(t, beehive.EventNormal, got[0].Type)
			assert.Equal(t, tc.reason+" happened", got[0].Message)
			assert.Equal(t, 1, got[0].Count)
			assert.False(t, got[0].FirstAt.IsZero())
			assert.False(t, got[0].LastAt.IsZero())
		})
	}
}

// The two arguments are beehive options, and neither maps by passing the value
// through: a nil category must add no option at all, since WithEventCategory("")
// selects the default timeline — which nothing in this package ever writes to.
func TestListEventsMapsCategoryAndLimit(t *testing.T) {
	d, bh := newTestDepsAndBeehive(t)
	ctx := context.Background()
	cluster := createCluster(t, d, "prod")
	admin := beehive.NewAdminClient[ClusterStatus](bh, ClusterGroupKind)
	addEvent(t, admin, cluster.ID, ConnectionEventCategory, "Connecting")
	addEvent(t, admin, cluster.ID, ConnectionEventCategory, "Connected")
	addEvent(t, admin, cluster.ID, categoryDiscovery, "Discovered")

	svc := serviceOver(t, d)
	id := ObjectID(cluster.ID)

	t.Run("a nil category reads every timeline", func(t *testing.T) {
		got, err := svc.ListEvents(ctx, id, nil, nil)

		require.NoError(t, err)
		assert.ElementsMatch(t, []string{"Connecting", "Connected", "Discovered"}, reasonsOf(got))
	})

	t.Run("a category selects its own", func(t *testing.T) {
		got, err := svc.ListEvents(ctx, id, new(ConnectionEventCategory), nil)

		require.NoError(t, err)
		assert.Equal(t, []string{"Connected", "Connecting"}, reasonsOf(got), "newest run first")
	})

	t.Run("a limit bounds the read", func(t *testing.T) {
		got, err := svc.ListEvents(ctx, id, new(ConnectionEventCategory), new(1))

		require.NoError(t, err)
		assert.Equal(t, []string{"Connected"}, reasonsOf(got), "the newest run survives the bound")
	})
}

// harnessCategory is a timeline no controller writes. The watch tests run with the
// reconcilers live, so scoping to it keeps every frame the test's own doing.
const harnessCategory = "harness"

// awaitRun takes the next frame and requires it to be a run.
func awaitRun(t *testing.T, stream *Stream[EventFeedFrame], what string) Event {
	t.Helper()
	f := testutil.Recv(t, stream.Frames, what)
	require.Equal(t, EventFeedFrameRun, f.Type)
	require.NotNil(t, f.Event)
	return *f.Event
}

// The snapshot arrives as runs, one Bookmark closes it, and what the log grows by
// follows. The bookmark is what tells an empty timeline from one still arriving.
func TestSubscribeObjectEventFeedSnapshotsThenBookmarksThenTails(t *testing.T) {
	d, bh := newRunningRegisteredDeps(t)
	ctx := t.Context()
	cluster := createCluster(t, d, "prod")
	admin := beehive.NewAdminClient[ClusterStatus](bh, ClusterGroupKind)
	addEvent(t, admin, cluster.ID, harnessCategory, "Stored")

	stream, err := serviceOver(t, d).SubscribeObjectEventFeed(ctx, ObjectID(cluster.ID), new(harnessCategory))

	require.NoError(t, err)
	assert.Equal(t, "Stored", awaitRun(t, stream, "the snapshot").Reason)
	assert.Equal(t, EventFeedFrameBookmark,
		testutil.Recv(t, stream.Frames, "the bookmark").Type, "one bookmark closes the snapshot")

	addEvent(t, admin, cluster.ID, harnessCategory, "Grew")
	assert.Equal(t, "Grew", awaitRun(t, stream, "the tail").Reason)
}

// An empty timeline bookmarks too: without it a client cannot tell "no events" from a
// snapshot still on its way.
func TestSubscribeObjectEventFeedBookmarksAnEmptyTimeline(t *testing.T) {
	d, _ := newRunningRegisteredDeps(t)
	cluster := createCluster(t, d, "prod")

	stream, err := serviceOver(t, d).SubscribeObjectEventFeed(t.Context(), ObjectID(cluster.ID), new(harnessCategory))

	require.NoError(t, err)
	assert.Equal(t, EventFeedFrameBookmark, testutil.Recv(t, stream.Frames, "the bookmark").Type)
}

// A record collected while its event feed is open ends the stream CLEANLY. beehive
// files the vanished row as ErrNotFound and its reader treats that as terminal, but the
// deletion is the answer here, not a failure — forwarded, it would put an error in
// front of a user who cleared a cache, once per open kind timeline.
//
// Two cadences are shrunk, and both are load-bearing: GC is what actually collects the
// row, and the watch floor is how the reader notices — only an event WRITE wakes it,
// and collection is not one.
func TestSubscribeObjectEventFeedEndsCleanlyWhenTheRecordIsCollected(t *testing.T) {
	d, bh := newRunningRegisteredDeps(t,
		beehive.WithGCInterval(time.Millisecond),
		beehive.WithWatchFloorInterval(time.Millisecond))
	ctx := t.Context()
	cluster := createCluster(t, d, "prod")
	cache := createCache(t, d.cacheClient, cluster, "uid-1")
	kind := createKind(t, d, cache.ID, deploymentsSpec)
	addEvent(t, beehive.NewAdminClient[ClusterCachedKindStatus](bh, ClusterCachedKindGroupKind),
		kind.ID, harnessCategory, "SyncComplete")

	stream, err := serviceOver(t, d).SubscribeObjectEventFeed(ctx, ObjectID(kind.ID), new(harnessCategory))
	require.NoError(t, err)
	require.Equal(t, "SyncComplete", awaitRun(t, stream, "the snapshot").Reason)

	require.NoError(t, d.kindClient.Delete(context.Background(), kind.ID))

	testutil.WaitClosed(t, stream.Frames, "the watch to end")
	assert.NoError(t, stream.Err(), "the deletion is the answer, not a broken watch")
}

// The other half of the filter, so the carve-out above cannot grow into "a dead watch
// is always fine". A guard rather than a red test: nobody writes this errors.Is from
// "Err is forwarded", and without it the silent-broken-watch bug ships unnoticed.
func TestTerminalErrKeepsTheReasonsAConsumerCanActOn(t *testing.T) {
	assert.NoError(t, terminalErr(beehive.ErrNotFound), "a collected record is a clean end")
	assert.NoError(t, terminalErr(nil))
	assert.ErrorIs(t, terminalErr(beehive.ErrWatchTooOld), beehive.ErrWatchTooOld,
		"runs were lost; a resubscribe is what makes the client correct")
	assert.ErrorIs(t, terminalErr(beehive.ErrStopped), beehive.ErrStopped)
}

// Every frame the event watch sends is checked, so a consumer that has gone ends the
// stream where it stands rather than blocking on a channel nobody drains. The context is
// cancelled once the first run fills Frames' one-frame buffer, with nothing reading, so
// the check is what unblocks the second rather than a race with the buffer.
func TestSubscribeObjectEventFeedStopsForAConsumerThatIsGone(t *testing.T) {
	d, bh := newTestDepsAndBeehive(t)
	ctx, cancel := context.WithCancel(context.Background())
	cluster := createCluster(t, d, "prod")
	admin := beehive.NewAdminClient[ClusterStatus](bh, ClusterGroupKind)
	addEvent(t, admin, cluster.ID, ConnectionEventCategory, "Connecting")
	addEvent(t, admin, cluster.ID, ConnectionEventCategory, "Connected")

	stream, err := serviceOver(t, d).SubscribeObjectEventFeed(ctx, ObjectID(cluster.ID), new(ConnectionEventCategory))
	require.NoError(t, err)
	require.Eventually(t, func() bool { return len(stream.Frames) == 1 }, testutil.Timeout, time.Millisecond, "the first run to fill the buffer")
	cancel()

	testutil.WaitClosed(t, stream.Frames, "the watch to stop")
	assert.NoError(t, stream.Err(), "a consumer leaving is not a watch failure")
}

// The Bookmark is a send like any other: one run fills the buffer, and the bookmark behind
// it is where a departed consumer ends the stream.
func TestSubscribeObjectEventFeedStopsOnTheBookmarkForAConsumerThatIsGone(t *testing.T) {
	d, bh := newTestDepsAndBeehive(t)
	ctx, cancel := context.WithCancel(context.Background())
	cluster := createCluster(t, d, "prod")
	addEvent(t, beehive.NewAdminClient[ClusterStatus](bh, ClusterGroupKind),
		cluster.ID, ConnectionEventCategory, "Connecting")

	stream, err := serviceOver(t, d).SubscribeObjectEventFeed(ctx, ObjectID(cluster.ID), new(ConnectionEventCategory))
	require.NoError(t, err)
	require.Eventually(t, func() bool { return len(stream.Frames) == 1 }, testutil.Timeout, time.Millisecond, "the first run to fill the buffer")
	cancel()

	testutil.WaitClosed(t, stream.Frames, "the watch to stop on the bookmark")
	assert.NoError(t, stream.Err())
}

// A cluster's timeline is read by ClusterID through the Clusters() pair, which resolves
// the name to the object and reads the same log; an id naming nothing is empty, or the
// Bookmark alone, held open until the caller is done.
func TestClusterEventsResolveByName(t *testing.T) {
	d, bh := newRunningRegisteredDeps(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	cluster := createCluster(t, d, "prod")
	addEvent(t, beehive.NewAdminClient[ClusterStatus](bh, ClusterGroupKind), cluster.ID, harnessCategory, "Stored")
	clusters := serviceOver(t, d).Clusters()

	evs, err := clusters.ListEvents(ctx, clusterIDOf(cluster), new(harnessCategory), nil)
	require.NoError(t, err)
	assert.Equal(t, []string{"Stored"}, reasonsOf(evs))
	evs, err = clusters.ListEvents(ctx, ClusterID("404"), nil, nil)
	require.NoError(t, err)
	assert.Empty(t, evs)

	stream, err := clusters.SubscribeEventFeed(ctx, clusterIDOf(cluster), new(harnessCategory))
	require.NoError(t, err)
	assert.Equal(t, "Stored", awaitRun(t, stream, "the snapshot").Reason)
	assert.Equal(t, EventFeedFrameBookmark, testutil.Recv(t, stream.Frames, "the bookmark").Type)

	absent, err := clusters.SubscribeEventFeed(ctx, ClusterID("404"), nil)
	require.NoError(t, err)
	assert.Equal(t, EventFeedFrameBookmark, testutil.Recv(t, absent.Frames, "the bookmark alone").Type)
	// The stream has no event to end on, so the claim that it stays open is bounded.
	testutil.NoRecv(t, absent.Frames, quietWindow, "the stream ending on its own")
	cancel()
	testutil.WaitClosed(t, absent.Frames, "the stream to end with the caller")
}

// The name resolution is a store read, so a store that will not answer fails both.
func TestClusterEventsReportAStorageFault(t *testing.T) {
	d, closeStore := newTestDepsWithABreakableStore(t)
	cluster := createCluster(t, d, "prod")
	closeStore()
	clusters := serviceOver(t, d).Clusters()

	_, err := clusters.ListEvents(t.Context(), clusterIDOf(cluster), nil, nil)
	assert.Error(t, err)
	_, err = clusters.SubscribeEventFeed(t.Context(), clusterIDOf(cluster), nil)
	assert.Error(t, err)
}
