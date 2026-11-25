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

package kubesync

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/watch"
	clienttesting "k8s.io/client-go/testing"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/supervisor"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/testutil"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/cluster/internal/kubeconn"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/cluster/internal/kubestore"
)

var podKind = kubestore.Kind{APIVersion: "v1", Kind: "Pod", Resource: "pods"}

func TestAColdKindIsListedIntoTheCacheAndThenWatched(t *testing.T) {
	cluster := newFakeCluster(t)
	cluster.serveKind(podKind, true)
	cluster.hasObjects(podKind, "10", object("v1", "Pod", "one", "1"), object("v1", "Pod", "two", "2"))
	stream := cluster.streamKind(podKind)

	svc := newSyncingService(t, cluster)
	syncKind(t, svc, 1, podKind)

	awaitKindReason(t, svc, 1, podKind, ReasonWatching)
	stream.opened.Await(t, "the watch to open")

	assert.ElementsMatch(t, []string{"one", "two"}, objectNames(t, svc, 1, podKind),
		"the cold list is on disk")
	cookie, ok := cookieOf(t, svc, 1, podKind)
	require.True(t, ok, "a completed list leaves the position a watch resumes from")
	assert.Equal(t, "10", cookie)
}

func TestAColdKindSaysItIsSyncingBeforeItHasAnything(t *testing.T) {
	cluster := newFakeCluster(t)
	cluster.serveKind(podKind, true)
	cluster.streamKind(podKind)
	held := cluster.holdList(podKind)
	defer held.release()

	svc := newSyncingService(t, cluster)
	syncKind(t, svc, 1, podKind)

	// A cold start genuinely has no rows, so it says so rather than holding a reason it
	// cannot stand behind.
	awaitKindReason(t, svc, 1, podKind, ReasonSyncing)
	held.release()
	awaitKindReason(t, svc, 1, podKind, ReasonWatching)
}

func TestADeltaLandsInTheCacheAndProvesTheStreamLive(t *testing.T) {
	cluster := newFakeCluster(t)
	cluster.serveKind(podKind, true)
	cluster.hasObjects(podKind, "10")
	stream := cluster.streamKind(podKind)

	svc := newSyncingService(t, cluster)
	syncKind(t, svc, 1, podKind)
	awaitKindReason(t, svc, 1, podKind, ReasonWatching)

	before, _ := svc.GetKindState(1, podKind)
	stream.opened.Await(t, "the watch to open").Add(object("v1", "Pod", "three", "11"))

	// The row reaches the store before the stamp beside it, so the wait is on the stamp:
	// gating on the row would read the verdict that was current a moment before it.
	after := awaitKindState(t, svc, 1, podKind, "data arriving to be stamped", func(state KindState) bool {
		return state.LastUpdateAt.After(before.LastUpdateAt)
	})

	assert.Equal(t, []string{"three"}, objectNames(t, svc, 1, podKind), "the delta is in the cache")
	assert.True(t, after.LastLiveAt.After(before.LastLiveAt), "and proves the stream live")
}

func TestABookmarkProvesTheStreamLiveWithoutData(t *testing.T) {
	cluster := newFakeCluster(t)
	cluster.serveKind(podKind, true)
	cluster.hasObjects(podKind, "10")
	stream := cluster.streamKind(podKind)

	svc := newSyncingService(t, cluster)
	syncKind(t, svc, 1, podKind)
	awaitKindReason(t, svc, 1, podKind, ReasonWatching)
	before, _ := svc.GetKindState(1, podKind)

	// Bookmarks exist to make an idle watch prove itself: no rows move, but the position
	// does, so a restart resumes from it rather than cold-listing again.
	stream.opened.Await(t, "the watch to open").Action(watch.Bookmark, object("v1", "Pod", "", "42"))

	// The position is written before the stamp beside it, so the wait is on the stamp.
	after := awaitKindState(t, svc, 1, podKind, "the bookmark to be stamped", func(state KindState) bool {
		return state.LastLiveAt.After(before.LastLiveAt)
	})

	cookie, ok := cookieOf(t, svc, 1, podKind)
	require.True(t, ok)
	assert.Equal(t, "42", cookie, "a bookmark moves the position")
	assert.Equal(t, before.LastUpdateAt, after.LastUpdateAt, "and is not data")
}

// A stream that has sent a bookmark has shown it sends them, so its silence is evidence.
func TestABookmarkedStreamThatGoesSilentReadsStaleUntilItsNextFrame(t *testing.T) {
	cluster := newFakeCluster(t)
	cluster.serveKind(podKind, true)
	cluster.hasObjects(podKind, "10")
	stream := cluster.streamKind(podKind)

	// Short, because this test is about the silence elapsing; the window stays long, so no
	// reopen lands under it and the frame below is the stream's.
	svc := newSyncingService(t, cluster, func(p *pacing) { p.bookmarkStaleAfter = 50 * time.Millisecond })
	syncKind(t, svc, 1, podKind)
	w := stream.opened.Await(t, "the watch to open")

	w.Action(watch.Bookmark, object("v1", "Pod", "", "42"))
	stale := awaitKindState(t, svc, 1, podKind, "the silence to read Stale", func(state KindState) bool {
		return state.Reason == ReasonStale
	})

	// The silence runs again from this frame and may read Stale before the wait looks. Stale
	// to Stale commits nothing, so a Stale dated after the first passed through Watching.
	w.Action(watch.Added, object("v1", "Pod", "one", "43"))
	awaitKindState(t, svc, 1, podKind, "the frame to set it back to Watching", func(state KindState) bool {
		return state.Reason == ReasonWatching || state.SinceAt.After(stale.SinceAt)
	})
}

// A hung reopen proves nothing a Stale stream's silence did not, so it stays Stale until a
// frame arrives.
func TestAStaleStreamWhoseReopenHangsStaysStaleUntilTheReopenLands(t *testing.T) {
	cluster := newFakeCluster(t)
	cluster.serveKind(podKind, true)
	cluster.hasObjects(podKind, "10")
	stream := cluster.streamKind(podKind)

	// The bookmark leads the first stream, so the quiet window cannot close it before the
	// bookmark is read.
	stream.startWith(watch.Event{Type: watch.Bookmark, Object: object("v1", "Pod", "", "42")})
	// The first open is held only so the second hold is in place before the run can reopen.
	// Deferred as well as released below: a held open holds the fake cluster's lock.
	releaseFirst := stream.hold()
	defer releaseFirst()

	const window = 100 * time.Millisecond
	svc := newSyncingService(t, cluster, func(p *pacing) {
		p.staleAfter = window
		p.bookmarkStaleAfter = 20 * time.Millisecond
	})
	syncKind(t, svc, 1, podKind)
	stream.parked.Await(t, "the first open to park")
	release := stream.hold()
	defer release()
	releaseFirst()
	stream.opened.Await(t, "the watch to open")

	stale := awaitKindState(t, svc, 1, podKind, "the silence to read Stale", func(state KindState) bool {
		return state.Reason == ReasonStale
	})
	stream.parked.Await(t, "the reopen to hang")
	news := svc.ListenForKindChanges()
	t.Cleanup(news.Close)
	// A bounded wait: the negative assertion, three times the window a slow open announces at.
	testutil.NoRecv(t, news.Chan(), 3*window, "the hung reopen to move the reason")
	state, _ := svc.GetKindState(1, podKind)
	assert.Equal(t, stale.SinceAt, state.SinceAt, "the stream still reads Stale")

	release()
	awaitKindReason(t, svc, 1, podKind, ReasonWatching)
}

func TestAQuietWatchIsReopenedFromItsCookieAndStaysWatching(t *testing.T) {
	cluster := newFakeCluster(t)
	cluster.serveKind(podKind, true)
	cluster.hasObjects(podKind, "10")
	stream := cluster.streamKind(podKind)

	// Short, because this test is about the window elapsing. The silence is shorter still, so
	// a stream that read it without a bookmark would show as Stale.
	svc := newSyncingService(t, cluster, func(p *pacing) {
		p.staleAfter = 50 * time.Millisecond
		p.bookmarkStaleAfter = 10 * time.Millisecond
	})
	syncKind(t, svc, 1, podKind)
	stream.opened.Await(t, "the watch to open")
	cluster.listed.Drain()
	watching := awaitKindState(t, svc, 1, podKind, "the watch to read Watching", func(state KindState) bool {
		return state.Reason == ReasonWatching
	})

	// Nothing ever arrives: a collection with nothing to say, and no bookmarks to say it with.
	stream.opened.Await(t, "the quiet watch to be reopened")
	_, relisted := cluster.listed.TryAwait()
	assert.False(t, relisted, "the reopen resumes from the cookie rather than listing again")

	// A quiet collection is not a stale one: the reopen proves it live.
	state := awaitKindState(t, svc, 1, podKind, "the reopen to prove the stream live", func(state KindState) bool {
		return state.Reason == ReasonWatching && !state.LastLiveAt.IsZero()
	})
	assert.Equal(t, watching.SinceAt, state.SinceAt, "a stream with no bookmark never read Stale")
	assert.Zero(t, state.Restarts, "and the reopen is not the stream going down")
}

// The quiet window proved the stream it closed, not the one that replaces it: a replacement that
// dies before its first frame fails the run and leaves LastLiveAt where the window put it.
func TestAQuietWatchsReplacementMustProveItself(t *testing.T) {
	cluster := newFakeCluster(t)
	cluster.serveKind(podKind, true)
	cluster.hasObjects(podKind, "10")
	stream := cluster.streamKind(podKind)

	// Short, because this test is about the window elapsing.
	svc := newSyncingService(t, cluster, func(p *pacing) { p.staleAfter = 50 * time.Millisecond })
	syncKind(t, svc, 1, podKind)
	stream.opened.Await(t, "the watch to open")

	// The reopen is held until it reads Resuming, which is after the window's stamp. Deferred
	// as well as released below: a held open holds the fake cluster's lock.
	release := stream.hold()
	defer release()
	windowed := awaitKindState(t, svc, 1, podKind, "the reopen to hang past the window", func(state KindState) bool {
		return state.Reason == ReasonResuming
	})
	require.False(t, windowed.LastLiveAt.IsZero(), "the quiet window proves the stream it closed")
	// Stopped before it is handed over, so it closes before its own window can elapse.
	stream.stopNext()
	release()

	stream.opened.Await(t, "the quiet watch to be reopened")
	failed := awaitKindState(t, svc, 1, podKind, "the replacement's death to be reported and paced", func(state KindState) bool {
		return state.Reason == ReasonSyncFailed &&
			strings.Contains(state.Message, errWatchProvedNothing.Error()) &&
			!state.NextRetryAt.IsZero()
	})
	assert.Equal(t, windowed.LastLiveAt, failed.LastLiveAt, "the replacement proved nothing")
}

// A collection with no bookmarks leaves its cookie where the last frame put it, and etcd compacts
// on the same five minutes the window runs, so a quiet reopen is routinely refused as expired. The
// run relists and goes on watching: no failure, no backoff, no restart.
func TestAQuietWatchWhoseReopenIsExpiredRelistsInPlace(t *testing.T) {
	expired := apierrors.NewResourceExpired("too old resource version")
	for name, onOpen := range map[string]bool{"on the open": true, "on the stream's first frame": false} {
		t.Run(name, func(t *testing.T) {
			cluster := newFakeCluster(t)
			cluster.serveKind(podKind, true)
			cluster.hasObjects(podKind, "10", object("v1", "Pod", "one", "1"))
			stream := cluster.streamKind(podKind)

			svc := newSyncingService(t, cluster, func(p *pacing) { p.staleAfter = 50 * time.Millisecond })
			syncKind(t, svc, 1, podKind)
			stream.opened.Await(t, "the watch to open")

			// The compaction took the cookie; the list is held so the relist's verdict can be
			// read before it lands.
			cluster.hasObjects(podKind, "20", object("v1", "Pod", "two", "2"))
			cluster.listed.Drain()
			held := cluster.holdList(podKind)
			defer held.release()
			if onOpen {
				stream.refuse(expired)
			} else {
				stream.opened.Await(t, "the quiet watch to be reopened").Error(&expired.ErrStatus)
			}

			// Still live: the run that was watching is the one listing, not a restart after a
			// failure.
			resyncing := awaitKindState(t, svc, 1, podKind, "the relist to start", func(state KindState) bool {
				return state.Reason == ReasonResyncing
			})
			assert.True(t, resyncing.Live, "the relist runs inside the stream's run")
			stream.refuse(nil)
			held.release()
			cluster.listed.Await(t, "the collection to be listed again")
			stream.opened.Await(t, "the relist to open its own watch")

			state := awaitKindState(t, svc, 1, podKind, "the relist to be watching", func(state KindState) bool {
				names := objectNames(t, svc, 1, podKind)
				return state.Reason == ReasonWatching && len(names) == 1 && names[0] == "two"
			})
			assert.Zero(t, state.Restarts, "the relist is not the stream going down")
			assert.True(t, state.NextRetryAt.IsZero(), "nor a failure to back off from")
		})
	}
}

// A hung reopen over a stream that sent no bookmark is a slow server, not stale rows: the
// connection's pings fail a dead one's streams long before the window.
func TestAQuietWatchWhoseReopenHangsReadsResuming(t *testing.T) {
	cluster := newFakeCluster(t)
	cluster.serveKind(podKind, true)
	cluster.hasObjects(podKind, "10")
	stream := cluster.streamKind(podKind)

	// Short, because this test is about the window elapsing twice.
	svc := newSyncingService(t, cluster, func(p *pacing) { p.staleAfter = 50 * time.Millisecond })
	syncKind(t, svc, 1, podKind)
	stream.opened.Await(t, "the watch to open")

	// Deferred as well as released below: a held open holds the fake cluster's lock, so a
	// test that fails before releasing it wedges every other call on that cluster.
	release := stream.hold()
	defer release()
	awaitKindReason(t, svc, 1, podKind, ReasonResuming)

	release()
	awaitKindReason(t, svc, 1, podKind, ReasonWatching)
}

func TestAResumeHoldsTheReasonTheRunBeforeItCommitted(t *testing.T) {
	cluster := newFakeCluster(t)
	cluster.serveKind(podKind, true)
	cluster.hasObjects(podKind, "10", object("v1", "Pod", "one", "1"))
	stream := cluster.streamKind(podKind)

	svc := newSyncingService(t, cluster)
	syncKind(t, svc, 1, podKind)
	awaitKindReason(t, svc, 1, podKind, ReasonWatching)
	stream.opened.Await(t, "the watch to open")

	// A resume poke on a cache of hundreds must not walk every kind through Syncing: the
	// rows are still there, so the verdict stands while the stream re-establishes.
	svc.RestartAll()
	stream.opened.Await(t, "the watch to be re-established")

	state, ok := svc.GetKindState(1, podKind)
	require.True(t, ok)
	assert.Equal(t, ReasonWatching, state.Reason, "a resume holds its reason")
	assert.ElementsMatch(t, []string{"one"}, objectNames(t, svc, 1, podKind),
		"and re-lists nothing: the cookie is what it resumes from")
}

func TestAKindThatWillNotListRetriesAndSaysWhy(t *testing.T) {
	cluster := newFakeCluster(t)
	cluster.serveKind(podKind, true)
	cluster.refuseList(podKind, errors.New("the collection is forbidden"))
	cluster.streamKind(podKind)

	svc := newSyncingService(t, cluster)
	syncKind(t, svc, 1, podKind)

	awaitKindState(t, svc, 1, podKind, "a run down for a reason it names", func(state KindState) bool {
		return state.Reason == ReasonSyncFailed &&
			strings.Contains(state.Message, "the collection is forbidden") &&
			!state.NextRetryAt.IsZero()
	})
	// The retry is what the countdown promises, and the countdown is the whole record of the
	// streak: Restarts counts a healthy stream coming back, which this kind never is.
	cluster.listed.Drain()
	cluster.listed.Await(t, "the run to be retried")
}

// The apiserver ends a watch on its own timeout, which is not a kind going down: the rows stay
// current across the reopen, so it is a CLEAN exit paced by the floor. Nothing climbs, nothing
// counts down, and the verdict never leaves Watching — reporting one would walk every collection
// through SyncFailed every few minutes. Restarts is what the rotation moves, which is the
// flapping question the retry streak cannot answer.
func TestARotatedStreamIsRebuiltAtTheFloorWithoutReportingAFailure(t *testing.T) {
	cluster := newFakeCluster(t)
	cluster.serveKind(podKind, true)
	cluster.hasObjects(podKind, "10")
	stream := cluster.streamKind(podKind)

	svc := newSyncingService(t, cluster)
	syncKind(t, svc, 1, podKind)
	// A frame first: a watch closing before one has proven nothing, and its exit is NeverReady
	// rather than a rotation.
	watcher := stream.opened.Await(t, "the watch to open")
	watcher.Add(object("v1", "Pod", "one", "11"))
	awaitKindReason(t, svc, 1, podKind, ReasonWatching)

	watcher.Stop()

	stream.opened.Await(t, "the watch to be rebuilt")
	awaitKindState(t, svc, 1, podKind, "the rebuild to count as a restart under an unmoved verdict",
		func(state KindState) bool {
			return state.Reason == ReasonWatching && state.Restarts >= 1 && state.NextRetryAt.IsZero()
		})
}

// An open the server accepts and drops has proven nothing, so it must not pass for a settled
// stream: a clean end before the first frame is the supervisor's NeverReady, which climbs the
// ladder where a rotation waits out the floor. Only a frame settles it — and the streak's own
// accumulation is the supervisor's, tested there.
func TestAWatchDroppedBeforeItsFirstFrameClimbsTheLadder(t *testing.T) {
	cluster := newFakeCluster(t)
	cluster.serveKind(podKind, true)
	cluster.hasObjects(podKind, "10")
	stream := cluster.streamKind(podKind)

	svc := newSyncingService(t, cluster)
	syncKind(t, svc, 1, podKind)

	// Two closures with nothing between them, neither having delivered a frame.
	for range 2 {
		stream.opened.Await(t, "the watch to open").Stop()
	}
	awaitKindState(t, svc, 1, podKind, "the death to be reported and paced", func(state KindState) bool {
		return state.Reason == ReasonSyncFailed && !state.NextRetryAt.IsZero()
	})

	stream.opened.Await(t, "the watch to be rebuilt").Add(object("v1", "Pod", "one", "11"))

	awaitKindState(t, svc, 1, podKind, "the frame to settle the stream", func(state KindState) bool {
		return state.Reason == ReasonWatching && state.NextRetryAt.IsZero()
	})
}

func TestAWatchRefusedIsRetried(t *testing.T) {
	cluster := newFakeCluster(t)
	cluster.serveKind(podKind, true)
	cluster.hasObjects(podKind, "10")
	stream := cluster.streamKind(podKind)
	stream.refuse(errors.New("the watch is forbidden"))

	svc := newSyncingService(t, cluster)
	syncKind(t, svc, 1, podKind)

	awaitKindState(t, svc, 1, podKind, "a run down for the watch it was refused", func(state KindState) bool {
		return state.Reason == ReasonSyncFailed && strings.Contains(state.Message, "the watch is forbidden")
	})
}

func TestAForbiddenWatchNamesItsClassOfFailure(t *testing.T) {
	cluster := newFakeCluster(t)
	cluster.serveKind(podKind, true)
	cluster.hasObjects(podKind, "10")
	stream := cluster.streamKind(podKind)
	stream.refuse(apierrors.NewForbidden(schema.GroupResource{Resource: "pods"}, "", errors.New("cannot watch")))

	svc := newSyncingService(t, cluster)
	syncKind(t, svc, 1, podKind)

	awaitKindState(t, svc, 1, podKind, "a run down, refused for its permissions", func(state KindState) bool {
		return state.Reason == ReasonSyncFailed && state.Failure == kubeconn.ReasonForbidden
	})
}

// A failure that is not a request's names no class, since nothing observed one.
func TestAFailureThatIsNotARequestsNamesNoClass(t *testing.T) {
	cluster := newFakeCluster(t)
	cluster.serveKind(podKind, true)
	cluster.hasObjects(podKind, "10")
	stream := cluster.streamKind(podKind)
	stream.refuse(errors.New("the watch is forbidden"))

	svc := newSyncingService(t, cluster)
	syncKind(t, svc, 1, podKind)

	state := awaitKindState(t, svc, 1, podKind, "a run down", func(state KindState) bool {
		return state.Reason == ReasonSyncFailed
	})
	assert.Empty(t, state.Failure)
}

func TestAWatchThatErrorsOutIsRetried(t *testing.T) {
	cluster := newFakeCluster(t)
	cluster.serveKind(podKind, true)
	cluster.hasObjects(podKind, "10")
	stream := cluster.streamKind(podKind)

	svc := newSyncingService(t, cluster)
	syncKind(t, svc, 1, podKind)
	awaitKindReason(t, svc, 1, podKind, ReasonWatching)

	// The server ends a watch it will not serve by sending a Status, not by closing.
	stream.opened.Await(t, "the watch to open").Error(&metav1.Status{Message: "too old resource version"})

	awaitKindState(t, svc, 1, podKind, "a run down for the frame that ended it", func(state KindState) bool {
		return state.Reason == ReasonSyncFailed && strings.Contains(state.Message, "too old resource version")
	})
}

func TestAFrameThatIsNotAnObjectEndsTheRun(t *testing.T) {
	cluster := newFakeCluster(t)
	cluster.serveKind(podKind, true)
	cluster.hasObjects(podKind, "10")
	stream := cluster.streamKind(podKind)

	svc := newSyncingService(t, cluster)
	syncKind(t, svc, 1, podKind)
	awaitKindReason(t, svc, 1, podKind, ReasonWatching)

	// Nothing else can be applied to the rows, and treating it as data would book progress
	// for a delta that never landed.
	stream.opened.Await(t, "the watch to open").Action(watch.Added, &metav1.Status{Message: "not an object"})

	awaitKindState(t, svc, 1, podKind, "a run down for a frame it cannot apply", func(state KindState) bool {
		return state.Reason == ReasonSyncFailed && strings.Contains(state.Message, "unexpected")
	})
}

// A run queued behind the worker cap is a key in a queue, not a goroutine, so it has no context
// to give up with. Remove is what stops it ever being dispatched, and forgetting is synchronous
// either way — nothing has reached the server on its behalf.
func TestAColdListQueuedBehindTheWorkerCapNeverListsOnceItIsForgotten(t *testing.T) {
	cluster := newFakeCluster(t)
	cluster.serveKind(podKind, true)
	cluster.streamKind(podKind)
	held := cluster.holdList(podKind)
	defer held.release()

	// One worker, taken by another kind's relist that is parked, so this kind's run waits in
	// the queue rather than on the server.
	otherKind := kubestore.Kind{APIVersion: "apps/v1", Kind: "Deployment", Resource: "deployments"}
	cluster.serveKind(otherKind, true)
	cluster.streamKind(otherKind)
	holdingSlot := cluster.holdList(otherKind)
	defer holdingSlot.release()

	svc := newSyncingService(t, cluster, func(p *pacing) { p.kindStartConcurrency = 1 })
	syncKind(t, svc, 1, otherKind)
	awaitKindReason(t, svc, 1, otherKind, ReasonSyncing)

	news := svc.ListenForKindChanges()
	defer news.Close()
	parked := make(chan struct{}, 1)
	go func() {
		for ev := range news.Chan() {
			if ev.Key.Kind == podKind {
				parked <- struct{}{}
				return
			}
		}
	}()

	svc.TrackKind(1, podKind)
	// The run commits Syncing immediately before it lists, so a dispatched one would say so
	// well inside this window. A negative assertion needs a bound of its own.
	testutil.NoRecv(t, parked, quietWindow, "a run queued behind the worker cap reaching the server")

	svc.ForgetKind(1, podKind)
	_, ok := svc.GetKindState(1, podKind)
	assert.False(t, ok, "a kind that never ran stands behind no answer")
}

func TestAConnectionRetiredUnderAKindIsReplacedRatherThanRetried(t *testing.T) {
	cluster := newFakeCluster(t)
	cluster.serveKind(podKind, true)
	cluster.hasObjects(podKind, "10", object("v1", "Pod", "one", "1"))
	stream := cluster.streamKind(podKind)

	svc := newSyncingService(t, cluster)
	syncKind(t, svc, 1, podKind)
	awaitKindReason(t, svc, 1, podKind, ReasonWatching)
	stream.opened.Await(t, "the watch to open")

	// The pool rebuilt the connection — credentials moved — and the stream is blocked in a
	// read on the one it retired. Nothing it reads can tell it, so the run has to end for the
	// worker to reach the replacement.
	lease := svc.connSvc.(*fakePool).lease("prod")
	lease.connect(t, cluster, "uid-1")

	stream.opened.Await(t, "the watch to be re-established over the replacement")
	state, _ := svc.GetKindState(1, podKind)
	assert.Equal(t, ReasonWatching, state.Reason, "a resume off the cookie holds its reason")
	assert.True(t, state.NextRetryAt.IsZero(), "and a rebuild is not a run that is down")
	assert.Equal(t, 1, state.Restarts, "though the stream did go down and come back")
}

// The same rearm one run of the phase earlier: a connection retired while the kind is still
// establishing ends that run with a cancel too, and the bridge's wake has already been and found
// it running. A ladder cannot stand in for it — the exit records nothing, so there is no rung.
func TestAConnectionRetiredWhileAKindEstablishesIsReplacedRatherThanParked(t *testing.T) {
	cluster := newFakeCluster(t)
	cluster.serveKind(podKind, true)
	cluster.hasObjects(podKind, "10", object("v1", "Pod", "one", "1"))
	stream := cluster.streamKind(podKind)
	held := cluster.holdList(podKind)

	// The replacement is its own api server, so the refusal below belongs to the connection
	// that gets retired rather than to whichever run opens next.
	replacement := newFakeCluster(t)
	replacement.serveKind(podKind, true)
	replacement.hasObjects(podKind, "10", object("v1", "Pod", "one", "1"))
	replacementStream := replacement.streamKind(podKind)

	// An hour, so nothing but a wake can explain a second start.
	svc := newSyncingService(t, cluster, func(p *pacing) {
		p.backoff = supervisor.Backoff{Base: time.Hour, Factor: 2, Cap: time.Hour}
	})
	syncKind(t, svc, 1, podKind)
	awaitKindReason(t, svc, 1, podKind, ReasonSyncing)

	// The pool rebuilt the connection while this run was parked in its cold list, and the open
	// that follows the list is what reads the refusal. The cancel is pending before the list is
	// let go, and the whole cold list runs between the two.
	stream.refuse(errors.New("the api server is busy"))
	svc.connSvc.(*fakePool).lease("prod").connect(t, replacement, "uid-1")
	held.release()

	cluster.listed.Await(t, "the held list to land")
	// The open, not a second list: whether the replacement's run cold lists or resumes turns on
	// whether the retired run committed its list first, and either one is the restart.
	replacementStream.opened.Await(t, "the kind to be started again over the replacement")
}

// The retirement is what ends the run, and the pool closes Done before the cancel it
// triggers can be scheduled — so a run can read an error off the retired connection with
// its own context still live. That error is not this kind's to report: reading it as one
// puts a kind the pool just moved onto the ladder, where only a rung can free it.
func TestARetiredConnectionEndsARunWhoseCancelHasNotLandedYet(t *testing.T) {
	cluster := newFakeCluster(t)
	conn := cluster.connection(t)
	conn.Retire()

	assert.True(t, ended(context.Background(), conn), "a retired connection ends the run reading over it")
}

func TestAKindWhoseAPIVersionIsUnparseableStillNamesItsCollection(t *testing.T) {
	gvr := gvrOf(kubestore.Kind{APIVersion: "a/b/c", Kind: "Broken", Resource: "brokens"})
	assert.Equal(t, "brokens", gvr.Resource, "a corrupt catalog row is not a panic")
}

func TestASlowResumeSaysSoOnceItHasStoppedBeingCurrent(t *testing.T) {
	cluster := newFakeCluster(t)
	cluster.serveKind(podKind, true)
	cluster.hasObjects(podKind, "10", object("v1", "Pod", "one", "1"))
	stream := cluster.streamKind(podKind)

	// Short, because this test is about a resume outlasting the window.
	svc := newSyncingService(t, cluster, func(p *pacing) { p.staleAfter = 50 * time.Millisecond })
	syncKind(t, svc, 1, podKind)
	awaitKindOpen(t, svc, 1, podKind)
	stream.opened.Await(t, "the watch to open")

	// The next open never lands, so the resume outlasts the window its rows stay current
	// for — the one resume worth publishing.
	release := stream.hold()
	defer release()
	svc.RestartAll()

	awaitKindReason(t, svc, 1, podKind, ReasonResuming)
}

func TestACacheRemovedUnderAKindFailsItsRunRatherThanWritingOn(t *testing.T) {
	for _, tc := range []struct {
		name string
		send func(*watch.RaceFreeFakeWatcher)
	}{
		{"a delta", func(w *watch.RaceFreeFakeWatcher) { w.Add(object("v1", "Pod", "three", "11")) }},
		{"a bookmark", func(w *watch.RaceFreeFakeWatcher) { w.Action(watch.Bookmark, object("v1", "Pod", "", "42")) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cluster := newFakeCluster(t)
			cluster.serveKind(podKind, true)
			cluster.hasObjects(podKind, "10")
			stream := cluster.streamKind(podKind)

			svc := newSyncingService(t, cluster)
			syncKind(t, svc, 1, podKind)
			awaitKindReason(t, svc, 1, podKind, ReasonWatching)
			watcher := stream.opened.Await(t, "the watch to open")

			// The file is retired under the run, which is what a cache deleted while its
			// kinds are up looks like. Nothing may land in it afterwards.
			require.NoError(t, svc.storeMgr.(*kubestore.Manager).Remove(1))
			tc.send(watcher)

			awaitKindReason(t, svc, 1, podKind, ReasonSyncFailed)
		})
	}
}

func TestAKindTrackedAgainstARemovedCacheFailsBeforeItReadsAnything(t *testing.T) {
	cluster := newFakeCluster(t)
	cluster.serveKind(podKind, true)
	cluster.hasObjects(podKind, "10")
	cluster.streamKind(podKind)

	svc := newSyncingService(t, cluster)
	svc.TrackDiscovery(1, testParams)
	awaitDiscovered(t, svc, 1)

	// The file is gone before this kind's sync starts, so the position it would resume
	// from cannot even be read.
	require.NoError(t, svc.storeMgr.(*kubestore.Manager).Remove(1))
	svc.TrackKind(1, podKind)

	awaitKindReason(t, svc, 1, podKind, ReasonSyncFailed)
}

func TestAPositionTheServerHasDroppedIsColdListedRatherThanResumedForever(t *testing.T) {
	cluster := newFakeCluster(t)
	cluster.serveKind(podKind, true)
	cluster.hasObjects(podKind, "10", object("v1", "Pod", "one", "1"))
	stream := cluster.streamKind(podKind)

	svc := newSyncingService(t, cluster)
	syncKind(t, svc, 1, podKind)
	awaitKindReason(t, svc, 1, podKind, ReasonWatching)
	watcher := stream.opened.Await(t, "the watch to open")

	// The cluster has moved past the position the cookie names, which is the one failure a
	// resume cannot retry its way out of: the cookie is what it would retry with. The relist
	// is held so the verdict it runs under can be read before it lands.
	cluster.hasObjects(podKind, "20", object("v1", "Pod", "two", "2"))
	cluster.listed.Drain()
	held := cluster.holdList(podKind)
	defer held.release()
	watcher.Error(&metav1.Status{
		Reason: metav1.StatusReasonExpired, Code: 410, Message: "too old resource version",
	})

	// The rows are served throughout, so this is not the cold start Syncing names.
	awaitKindReason(t, svc, 1, podKind, ReasonResyncing)
	held.release()
	cluster.listed.Await(t, "the collection to be listed again")
	require.Eventually(t, func() bool {
		names := objectNames(t, svc, 1, podKind)
		return len(names) == 1 && names[0] == "two"
	}, testutil.Timeout, time.Millisecond, "the cold list to replace what the cache held")
}

func TestAStreamThatSettlesBetweenClosuresReopensAtTheFloor(t *testing.T) {
	cluster := newFakeCluster(t)
	cluster.serveKind(podKind, true)
	cluster.hasObjects(podKind, "10")
	stream := cluster.streamKind(podKind)

	svc := newSyncingService(t, cluster)
	syncKind(t, svc, 1, podKind)

	// Three closures, each over a stream that delivered a frame first. Every one is a clean
	// exit, so none is ever recorded a failure — a kind whose rotations were counted as ones
	// would be reading SyncFailed with a widening countdown by the third.
	for range 3 {
		watcher := stream.opened.Await(t, "the watch to open")
		watcher.Add(object("v1", "Pod", "one", "11"))
		awaitKindState(t, svc, 1, podKind, "the frame to settle the stream", func(state KindState) bool {
			return state.Reason == ReasonWatching && state.NextRetryAt.IsZero()
		})
		watcher.Stop()
	}

	awaitKindState(t, svc, 1, podKind, "every rotation to have counted as one", func(state KindState) bool {
		return state.Reason == ReasonWatching && state.NextRetryAt.IsZero() && state.Restarts >= 3
	})
}

// The cache mirrors what the server holds: nothing here ages events out, so every row a list
// carried and every one a delta brought is still there.
func TestEveryEventTheServerSentIsKept(t *testing.T) {
	eventKind := kubestore.Kind{APIVersion: "v1", Kind: "Event", Resource: "events"}
	cluster := newFakeCluster(t)
	cluster.serveKind(eventKind, true)
	cluster.hasObjects(eventKind, "10", event("one", "1"), event("two", "2"), event("three", "3"))
	stream := cluster.streamKind(eventKind)

	svc := newSyncingService(t, cluster)
	syncKind(t, svc, 1, eventKind)
	awaitKindReason(t, svc, 1, eventKind, ReasonWatching)
	require.Len(t, eventsOf(t, svc, 1), 3, "the list's rows all stand")

	watcher := stream.opened.Await(t, "the watch to open")
	watcher.Add(event("four", "11"))
	require.Eventually(t, func() bool {
		return len(eventsOf(t, svc, 1)) == 4
	}, testutil.Timeout, time.Millisecond, "the delta to land alongside them")
}

func TestASlowResumeThatFinishesReportsTheStreamItEstablished(t *testing.T) {
	cluster := newFakeCluster(t)
	cluster.serveKind(podKind, true)
	cluster.hasObjects(podKind, "10", object("v1", "Pod", "one", "1"))
	stream := cluster.streamKind(podKind)

	// Short, because this test is about a resume outlasting the window.
	svc := newSyncingService(t, cluster, func(p *pacing) { p.staleAfter = 50 * time.Millisecond })
	syncKind(t, svc, 1, podKind)
	awaitKindOpen(t, svc, 1, podKind)
	stream.opened.Await(t, "the watch to open")

	// Deferred as well as released below: a held open holds the fake cluster's lock, so a
	// test that fails before releasing it wedges every other call on that cluster.
	release := stream.hold()
	defer release()
	svc.RestartAll()
	awaitKindReason(t, svc, 1, podKind, ReasonResuming)

	// The announcement is the resume being slow, not the stream being down: once it lands,
	// the verdict is the stream's.
	release()
	awaitKindReason(t, svc, 1, podKind, ReasonWatching)
}

func TestAResumeParkedOnItsOpenStillGivesUpWithItsRun(t *testing.T) {
	cluster := newFakeCluster(t)
	cluster.serveKind(podKind, true)
	cluster.hasObjects(podKind, "10", object("v1", "Pod", "one", "1"))
	stream := cluster.streamKind(podKind)

	// Short, because this test parks a resume past the window.
	svc := newSyncingService(t, cluster, func(p *pacing) { p.staleAfter = 50 * time.Millisecond })
	syncKind(t, svc, 1, podKind)
	awaitKindOpen(t, svc, 1, podKind)
	stream.opened.Await(t, "the watch to open")

	// An open that never lands, held past the announcement so the run is waiting on it
	// rather than on the window before it.
	release := stream.hold()
	defer release()
	svc.RestartAll()
	awaitKindReason(t, svc, 1, podKind, ReasonResuming)

	// Forgetting a kind waits for its run. Whether the open ever unwinds is the server's
	// business, so a run that waits on one unconditionally cannot be joined.
	done := make(chan struct{})
	go func() { defer close(done); svc.ForgetKind(1, podKind) }()
	testutil.Wait(t, done, "the teardown to return while the open is still held")
}

func TestARelistRefusedOnceIsRetriedAsARelist(t *testing.T) {
	cluster := newFakeCluster(t)
	cluster.serveKind(podKind, true)
	cluster.hasObjects(podKind, "10", object("v1", "Pod", "one", "1"))
	stream := cluster.streamKind(podKind)

	svc := newSyncingService(t, cluster)
	syncKind(t, svc, 1, podKind)
	awaitKindReason(t, svc, 1, podKind, ReasonWatching)
	watcher := stream.opened.Await(t, "the watch to open")

	// The collection moves on, the position is dropped, and the replacement list is refused
	// once. The cookie only goes when a page lands, so a retry that forgot it must relist
	// would resume from a position the server has already refused.
	cluster.hasObjects(podKind, "20", object("v1", "Pod", "two", "2"))
	cluster.failListOnce(podKind, errors.New("the api server is busy"))
	watcher.Error(&metav1.Status{
		Reason: metav1.StatusReasonExpired, Code: 410, Message: "too old resource version",
	})

	require.Eventually(t, func() bool {
		names := objectNames(t, svc, 1, podKind)
		return len(names) == 1 && names[0] == "two"
	}, testutil.Timeout, time.Millisecond, "the relist to be retried until it lands")
}

// A restart is a cancel, and a clean exit carries no error — so the run it wakes re-establishes
// at once rather than paying a rung for a stream nothing was wrong with.
func TestRestartAllReEstablishesWithoutARung(t *testing.T) {
	cluster := newFakeCluster(t)
	cluster.serveKind(podKind, true)
	cluster.hasObjects(podKind, "10")
	stream := cluster.streamKind(podKind)

	svc := newSyncingService(t, cluster)
	syncKind(t, svc, 1, podKind)
	awaitKindReason(t, svc, 1, podKind, ReasonWatching)
	stream.opened.Await(t, "the watch to open")

	svc.RestartAll()
	stream.opened.Await(t, "the watch to be re-established")

	state, ok := svc.GetKindState(1, podKind)
	require.True(t, ok)
	assert.True(t, state.NextRetryAt.IsZero(), "a stop is not a failure, so nothing is retrying")
	assert.Equal(t, 1, state.Restarts, "the poke is what took the stream down and back")
}

// The supervisor hands back every value it stops holding, so Remove joins the goroutine: past
// ForgetKind nothing can still write through the kind, which is what the seam promises and what
// lets a caller clear its rows straight after.
func TestForgetKindJoinsTheStreamBeforeItReturns(t *testing.T) {
	cluster := newFakeCluster(t)
	cluster.serveKind(podKind, true)
	cluster.hasObjects(podKind, "10")
	stream := cluster.streamKind(podKind)

	svc := newSyncingService(t, cluster)
	syncKind(t, svc, 1, podKind)
	awaitKindReason(t, svc, 1, podKind, ReasonWatching)
	watcher := stream.opened.Await(t, "the watch to open")

	svc.ForgetKind(1, podKind)

	// A watcher whose consumer is gone reports itself stopped, which is what joining it means.
	assert.True(t, watcher.IsStopped(), "the stream is joined before ForgetKind returns")
	_, ok := svc.GetKindState(1, podKind)
	assert.False(t, ok, "and the verdict goes with it")
}

// A rename under an unchanged plural is the same collection under a new Kind name, and the rows
// are keyed by that name — so the run and the stream started with the old value both come down
// before the replacement lists.
func TestARenameCancelsTheOldRunBeforeTheNewOneLists(t *testing.T) {
	cluster := newFakeCluster(t)
	cluster.serveKind(podKind, true)
	cluster.hasObjects(podKind, "10", object("v1", "Pod", "one", "1"))
	stream := cluster.streamKind(podKind)

	svc := newSyncingService(t, cluster)
	syncKind(t, svc, 1, podKind)
	awaitKindReason(t, svc, 1, podKind, ReasonWatching)
	watcher := stream.opened.Await(t, "the watch to open")

	renamed := kubestore.Kind{APIVersion: "v1", Kind: "PodItem", Resource: "pods"}
	svc.TrackKind(1, renamed)

	assert.True(t, watcher.IsStopped(), "the stream started with the old value is joined first")
	stream.opened.Await(t, "the replacement to open its own watch")
	awaitKindReason(t, svc, 1, renamed, ReasonWatching)
}

// A 410 from the watch OPEN must relist too. It arrives on a run that establishes nothing, so it
// commits no stream — and a relist decided from the standing stream alone would never see it: the
// next run reads the same unchanged Prev, resumes from the same dead cookie, and the kind is
// pinned at the backoff cap forever. Two ordinary paths land here, a reopen after a clean stop
// and a cold start off a cookie on disk, and neither carries an Error frame.
func TestAPositionRefusedByTheWatchOpenIsColdListedRatherThanResumedForever(t *testing.T) {
	cluster := newFakeCluster(t)
	cluster.serveKind(podKind, true)
	cluster.hasObjects(podKind, "10", object("v1", "Pod", "one", "1"))
	stream := cluster.streamKind(podKind)

	svc := newSyncingService(t, cluster)
	syncKind(t, svc, 1, podKind)
	awaitKindReason(t, svc, 1, podKind, ReasonWatching)
	stream.opened.Await(t, "the watch to open")

	// The cold list has committed a cookie, so what follows is a resume. Every later open is
	// refused with the one error a resume cannot retry through — and the stream is stopped
	// cleanly, so nothing but the refusal itself says the position is gone.
	cluster.hasObjects(podKind, "20", object("v1", "Pod", "two", "2"))
	cluster.listed.Drain()
	stream.refuse(apierrors.NewResourceExpired("too old resource version"))
	// Park the relist in its list, so the verdict it publishes stands still: Resyncing is
	// committed on the way into the list and replaced by the refused open on the way out,
	// a window narrower than a reader's sample.
	held := cluster.holdList(podKind)
	svc.RestartAll()

	awaitKindReason(t, svc, 1, podKind, ReasonResyncing)
	held.release()
	cluster.listed.Await(t, "the collection to be listed again rather than resumed forever")
}

// A restart is the same 410 with nothing in memory at all: the cookie is on disk and no run has
// ever committed a stream, so the position being dead is a fact only the refused open can
// report. A relist decided from anything the process held would never escape it.
func TestAColdStartOffAPositionTheServerHasDroppedRelists(t *testing.T) {
	cluster := newFakeCluster(t)
	cluster.serveKind(podKind, true)
	cluster.hasObjects(podKind, "10", object("v1", "Pod", "one", "1"))
	stream := cluster.streamKind(podKind)

	pool := newFakePool()
	mgr := kubestore.NewManager(t.TempDir(), kubestore.Retention{})
	t.Cleanup(func() { _ = mgr.Close() })
	pool.lease("prod").connect(t, cluster, "uid-1")

	first := New(pool, mgr, withPacing(syncPacing()), withRealKindSync())
	start(t, first)
	syncKind(t, first, 1, podKind)
	awaitKindReason(t, first, 1, podKind, ReasonWatching)
	stream.opened.Await(t, "the watch to open")

	// The process goes, leaving the committed cookie behind it.
	require.NoError(t, first.Close())

	cluster.hasObjects(podKind, "20", object("v1", "Pod", "two", "2"))
	cluster.listed.Drain()
	stream.refuse(apierrors.NewResourceExpired("too old resource version"))

	second := New(pool, mgr, withPacing(syncPacing()), withRealKindSync())
	t.Cleanup(func() { _ = second.Close() })
	start(t, second)
	syncKind(t, second, 1, podKind)

	cluster.listed.Await(t, "the collection to be listed again rather than resumed forever")
}

// The connection bridge wakes every kind on every state frame, so a Wake must never tear a live
// stream down: a cache of three hundred kinds would otherwise re-list on any pass the pool
// published. A parked kind is what it starts.
func TestAStateFrameLeavesALiveStreamAlone(t *testing.T) {
	cluster := newFakeCluster(t)
	cluster.serveKind(podKind, true)
	cluster.hasObjects(podKind, "10")
	stream := cluster.streamKind(podKind)

	svc := newSyncingService(t, cluster)
	syncKind(t, svc, 1, podKind)
	stream.opened.Await(t, "the watch to open").Add(object("v1", "Pod", "one", "11"))
	awaitKindReason(t, svc, 1, podKind, ReasonWatching)

	svc.sessionOf(1).wakeAll()

	// A negative assertion has no event to wait for, so it takes a window of its own.
	testutil.NoRecv(t, stream.opened.Chan(), quietWindow, "the wake re-opened a live stream")
	state, ok := svc.GetKindState(1, podKind)
	require.True(t, ok)
	assert.Equal(t, ReasonWatching, state.Reason, "and it never left the verdict it was standing on")
}

// A teardown must not release the store under a worker that is mid-list: the run writes through
// the claim the session is about to give back. Two things hold it — the per-kind Remove, which
// cancels and joins, and the session's own join behind that — and this pins the promise rather
// than which of them delivered it.
func TestATeardownMidListJoinsTheWorkerBeforeReleasingTheStore(t *testing.T) {
	cluster := newFakeCluster(t)
	cluster.serveKind(podKind, true)
	cluster.hasObjects(podKind, "10")
	held := cluster.holdList(podKind)
	cluster.streamKind(podKind)

	// Released whatever happens: a held list the teardown is joining outlives a failed
	// assertion, and the drain behind it would never finish.
	t.Cleanup(held.release)

	svc := newSyncingService(t, cluster)
	syncKind(t, svc, 1, podKind)
	awaitKindReason(t, svc, 1, podKind, ReasonSyncing)

	done := make(chan struct{})
	go func() { defer close(done); svc.ForgetDiscovery(1) }()
	testutil.NoRecv(t, done, quietWindow, "the teardown returned while the list was still out")

	held.release()
	testutil.Wait(t, done, "the teardown to join the worker")
}

// OnPass fires on every pass, schedule-only ones included, so a record is woken only when the
// answer a reader would get has MOVED. Without the baseline behind that, every pass would wake
// every record on the cache.
func TestAPassThatMovedNothingWakesNoRecord(t *testing.T) {
	cluster := newFakeCluster(t)
	cluster.serveKind(podKind, true)
	cluster.hasObjects(podKind, "10")
	stream := cluster.streamKind(podKind)

	svc := newSyncingService(t, cluster)
	syncKind(t, svc, 1, podKind)
	stream.opened.Await(t, "the watch to open").Add(object("v1", "Pod", "one", "11"))
	awaitKindReason(t, svc, 1, podKind, ReasonWatching)

	subject := kindSubject(1, podKind)
	snap, ok := svc.kindSupervisor.Read(subject)
	require.True(t, ok)
	// File the answer before subscribing. The verdict is readable off the supervisor before
	// the pass that carries it has filed anything, so a publish here can be the first to
	// record this reason — which is news, and not the news the window below is about.
	svc.publishKind(subject, snap)

	news := svc.ListenForKindChanges()
	t.Cleanup(news.Close)
	svc.publishKind(subject, snap)

	testutil.NoRecv(t, news.Chan(), quietWindow, "a pass that moved nothing woke the record")
}

// A pass for a kind under a cache nobody armed is no news: no record stands behind it to wake.
func TestAPassUnderAnUnarmedCacheNotifiesNothing(t *testing.T) {
	svc, _ := newTestService(t)
	cacheID, id, ok := parseKindSubject(kindSubject(7, podKind))
	require.True(t, ok)

	_, moved := svc.recordKindReason(cacheID, id, supervisor.Snapshot{})

	assert.False(t, moved)
}

// A worker holds a start slot only until its STARTING phase is over, and for a stream that is the
// watch being open — never its first frame. Bookmarks are advisory and a quiet collection may send
// nothing for hours, so slots held until a frame would be taken indefinitely by whichever kinds
// started first, and the rest of the cache would never list a row.
func TestQuietKindsDoNotHoldTheStartSlotsOfTheRest(t *testing.T) {
	cluster := newFakeCluster(t)
	kinds := []kubestore.Kind{
		podKind,
		testKind("apps/v1", "Deployment", "deployments"),
		testKind("v1", "Service", "services"),
	}
	watches := make([]*streams, 0, len(kinds))
	for _, k := range kinds {
		cluster.serveKind(k, true)
		cluster.hasObjects(k, "10")
		watches = append(watches, cluster.streamKind(k))
	}

	// Fewer slots than kinds, and not one of these collections will ever deliver a frame.
	svc := newSyncingService(t, cluster, func(p *pacing) { p.kindStartConcurrency = 1 })
	svc.TrackDiscovery(1, testParams)
	awaitDiscovered(t, svc, 1)
	for _, k := range kinds {
		svc.TrackKind(1, k)
	}

	for i, w := range watches {
		w.opened.Await(t, "the watch for "+kinds[i].Resource)
	}
}

// Live is the supervisor's reading of the worker carried through the seam: the watch is open. It
// is not derivable from the reason without enumerating the vocabulary, which is why the seam
// carries it rather than folding it away — a cold list is not live, and a stream nothing has
// proven current still is.
func TestOnlyAnOpenWatchReadsLive(t *testing.T) {
	cluster := newFakeCluster(t)
	cluster.serveKind(podKind, true)
	cluster.hasObjects(podKind, "10")
	held := cluster.holdList(podKind)
	t.Cleanup(held.release)
	stream := cluster.streamKind(podKind)

	svc := newSyncingService(t, cluster, func(p *pacing) { p.staleAfter = 50 * time.Millisecond })
	syncKind(t, svc, 1, podKind)

	awaitKindState(t, svc, 1, podKind, "the cold list to be under way", func(state KindState) bool {
		return state.Reason == ReasonSyncing
	})
	state, _ := svc.GetKindState(1, podKind)
	assert.False(t, state.Live, "a kind still listing is not live")

	held.release()

	assert.True(t, awaitKindOpen(t, svc, 1, podKind).Live, "a kind whose watch is open is live")

	// A quiet watch's reopen that hangs reads Resuming — and stays live, because the run is up
	// and serving its rows.
	release := stream.hold()
	defer release()
	state = awaitKindState(t, svc, 1, podKind, "the reopen to hang past the window", func(state KindState) bool {
		return state.Reason == ReasonResuming
	})
	assert.True(t, state.Live, "a resuming stream is still up")
}

// A subject reads back as the cache and kind it names, the apiVersion's own slash included,
// and one that does not name both reads as nothing.
func TestAKindSubjectReadsBackAsItsCacheAndKind(t *testing.T) {
	deployments := kubestore.Kind{APIVersion: "apps/v1", Kind: "Deployment", Resource: "deployments"}
	cacheID, id, ok := parseKindSubject(kindSubject(7, deployments))
	require.True(t, ok)
	assert.Equal(t, int64(7), cacheID)
	assert.Equal(t, idOf(deployments), id)

	for _, subject := range []string{"7", "7/pods", "seven/v1/pods"} {
		_, _, ok := parseKindSubject(subject)
		assert.False(t, ok, subject)
	}
}

// A run with nothing to run against records nothing: a subject that does not parse, a kind
// nothing tracks, or one tracked under a cache nobody armed, whose claims a run would write
// through are not there.
func TestARunWithNothingToRunAgainstIsSkipped(t *testing.T) {
	svc, _ := newTestService(t)
	svc.TrackKind(7, podKind)
	w := kindSync{s: svc, pacing: defaultPacing()}

	for name, subject := range map[string]string{
		"unparseable": "pods",
		"untracked":   kindSubject(8, podKind),
		"unarmed":     kindSubject(7, podKind),
	} {
		result := w.Run(t.Context(), supervisor.NewWorkerPass[Reason](subject, nil, supervisor.Snapshot{}))
		assert.True(t, result.IsSkip(), name)
	}
}

// A file removed under a quiet watch fails the run at the reopen, which reads its position
// off that file, rather than resuming from a position nothing holds.
func TestACacheRemovedUnderAQuietKindFailsItsReopen(t *testing.T) {
	cluster := newFakeCluster(t)
	cluster.serveKind(podKind, true)
	cluster.hasObjects(podKind, "10")
	stream := cluster.streamKind(podKind)

	// Short, because this test is about the window elapsing.
	svc := newSyncingService(t, cluster, func(p *pacing) { p.staleAfter = 50 * time.Millisecond })
	syncKind(t, svc, 1, podKind)
	awaitKindReason(t, svc, 1, podKind, ReasonWatching)
	stream.opened.Await(t, "the watch to open")

	require.NoError(t, svc.storeMgr.(*kubestore.Manager).Remove(1))

	state := awaitKindState(t, svc, 1, podKind, "the reopen to fail on the removed file", func(state KindState) bool {
		return state.Reason == ReasonSyncFailed
	})
	assert.Contains(t, state.Message, kubestore.ErrClosed.Error())
	_, reopened := stream.opened.TryAwait()
	assert.False(t, reopened, "a watch was opened from a position nothing holds")
}

// A resume parked on its open gives up with its run before the window is out, so forgetting the
// kind does not wait for a server that may never answer.
func TestAResumeParkedBeforeItsWindowGivesUpWithItsRun(t *testing.T) {
	cluster := newFakeCluster(t)
	cluster.serveKind(podKind, true)
	cluster.hasObjects(podKind, "10", object("v1", "Pod", "one", "1"))
	stream := cluster.streamKind(podKind)

	svc := newSyncingService(t, cluster)
	syncKind(t, svc, 1, podKind)
	awaitKindOpen(t, svc, 1, podKind)
	stream.opened.Await(t, "the watch to open")

	release := stream.hold()
	defer release()
	svc.RestartAll()
	stream.parked.Await(t, "the resume to park on its open")

	done := make(chan struct{})
	go func() { defer close(done); svc.ForgetKind(1, podKind) }()
	testutil.Wait(t, done, "the teardown to return while the open is still held")
}

// A page that carries nothing touches no file, so a file removed while the last page was being
// fetched is found at the commit, which fails the run rather than persisting a position.
func TestACacheRemovedBeforeTheListCommitsFailsTheRun(t *testing.T) {
	cluster := newFakeCluster(t)
	cluster.serveKind(podKind, true)
	cluster.streamKind(podKind)
	asked := testutil.NewSignal()
	released := make(chan struct{})
	release := sync.OnceFunc(func() { close(released) })
	t.Cleanup(release)
	// Counted rather than read off the request's continue token, which the fake drops.
	var pages atomic.Int32
	cluster.prependList(podKind.Resource, func(clienttesting.Action) (bool, runtime.Object, error) {
		list := &unstructured.UnstructuredList{Object: map[string]any{
			"apiVersion": "v1", "kind": "PodList", "metadata": map[string]any{"resourceVersion": "10"},
		}}
		if pages.Add(1) == 1 {
			list.SetContinue("page-2")
			list.Items = append(list.Items, *object("v1", "Pod", "one", "1"))
			return true, list, nil
		}
		asked.Fire()
		<-released
		return true, list, nil
	})

	svc := newSyncingService(t, cluster)
	syncKind(t, svc, 1, podKind)
	asked.Wait(t, "the last page to be asked for")
	require.NoError(t, svc.storeMgr.(*kubestore.Manager).Remove(1))
	release()

	awaitKindReason(t, svc, 1, podKind, ReasonSyncFailed)
}
