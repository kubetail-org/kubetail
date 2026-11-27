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
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/testutil"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/cluster/internal/kubestore"
)

func TestASessionTakesItsClaimsInStartAndGivesThemBackInClose(t *testing.T) {
	t.Parallel()

	svc, pool := newTestService(t)

	sess := newSession(svc, 1, testParams)
	require.Equal(t, 0, pool.lease("prod").held(), "a session holds nothing before it starts")

	require.NoError(t, sess.start())
	assert.Equal(t, 1, pool.lease("prod").held(), "start claims the context")
	assert.NotNil(t, sess.store, "start opens the cache file")

	sess.close()
	assert.Equal(t, 0, pool.lease("prod").held(), "close gives the claim back")
}

func TestStartPutsTheCacheOnTheDiscoveryEngineAndCloseTakesItOff(t *testing.T) {
	t.Parallel()

	svc, _ := newTestService(t)

	sess := newSession(svc, 1, testParams)
	_, ok := svc.discoverySupervisor.Read(sess.discoverySubject())
	require.False(t, ok, "nothing sweeps for a session that has not started")

	require.NoError(t, sess.start())
	_, ok = svc.discoverySupervisor.Read(sess.discoverySubject())
	assert.True(t, ok, "start adds the cache's subject")

	sess.close()
	_, ok = svc.discoverySupervisor.Read(sess.discoverySubject())
	assert.False(t, ok, "close drops it again")
}

func TestACatalogSubscriptionEndingLeavesTheSessionRunning(t *testing.T) {
	t.Parallel()

	svc, _ := newTestService(t)
	svc.TrackDiscovery(1, testParams)

	// Clearing the cache ends the file the subscription belongs to, which is what the wake
	// loop sees: it returns, and the session — whose sweep still runs on the interval —
	// tears down as usual.
	require.NoError(t, svc.storeMgr.(*kubestore.Manager).Clear(1))

	svc.ForgetDiscovery(1)
}

// The fan-out skips while neither document has answered, which schedules nothing either. Reading
// that as waiting would wake the whole sweep on every connection state frame, and the failing
// documents would be re-dispatched ahead of the ladder they should be climbing.
func TestASweepWaitingOnItsOwnDocumentsIsNotParked(t *testing.T) {
	t.Parallel()

	cluster := newFakeCluster(t)
	cluster.breakPath("/api")
	cluster.breakPath("/apis")

	svc, pool := newTestService(t)
	pool.lease("prod").connect(t, cluster, "uid-1")
	start(t, svc)
	svc.TrackDiscovery(1, testParams)
	awaitReason(t, svc, 1, ReasonDiscoveryFailed)

	// A probe is marked from the moment it reaches the gate until the lease answers, so one
	// passing through it as the reason lands is waited out; a mark that stays fails.
	sess := svc.sessionOf(1)
	require.Eventually(t, func() bool { return !sess.anyWaiting() },
		testutil.Timeout, time.Millisecond, "a sweep whose documents will not load waits on its own retries, not on a wake")
}

// A run holds a supervisor worker, so a kind whose connection does not vouch records why and
// suspends rather than waiting at the gate. Nothing syncs past it, and the session ends without
// anything to join.
func TestAKindSuspendedAtTheGateSyncsNothingAndEndsWithItsSession(t *testing.T) {
	t.Parallel()

	fake := newFakeKindSync()
	svc, _ := newTestService(t, fake.option())
	start(t, svc)

	kind := testKind("apps/v1", "Deployment", "deployments")
	svc.TrackDiscovery(1, testParams)
	svc.TrackKind(1, kind)
	awaitKindReason(t, svc, 1, kind, ReasonNoConnection)

	// The gate is ahead of anything the body reads, so a run reaching it has read nothing.
	testutil.NoRecv(t, fake.runs.Chan(), quietWindow, "a kind syncs without a connection vouching for it")

	returned := testutil.NewProbe[struct{}](1)
	go func() { svc.ForgetDiscovery(1); returned.Fire(struct{}{}) }()
	testutil.Wait(t, returned.Chan(), "the session to end")
}

func TestASweepRegistersAgainstItsSessionOnlyWhileOneIsArmed(t *testing.T) {
	t.Parallel()

	svc, _ := newTestService(t)

	_, ok := svc.enterRun(1)
	assert.False(t, ok, "a cache nobody has armed registers no run")

	svc.TrackDiscovery(1, testParams)
	sess, ok := svc.enterRun(1)
	require.True(t, ok, "an armed cache registers its run")
	sess.leaveRun()

	// Closing the door is what a teardown does before it joins, so a run that has not
	// registered by then never will.
	svc.mu.Lock()
	sess.stopping = true
	svc.mu.Unlock()

	_, ok = svc.enterRun(1)
	assert.False(t, ok, "a cache already stopping registers no run")
}

func TestAConnectionAnsweringAsAnotherClusterWakesNothing(t *testing.T) {
	t.Parallel()

	cluster := newFakeCluster(t)
	cluster.serve("v1", listable("Pod", "pods", true))
	svc, pool := newTestService(t)
	start(t, svc)
	svc.TrackDiscovery(1, testParams)

	// A connection arrives, so the wake loop runs — but it answers for another cluster, so
	// it is not the connection this cache waits for and the sweep stays parked.
	pool.lease("prod").connect(t, cluster, "another-uid")

	cluster.noRead(t, "a sweep dialing a connection that answers as another cluster")
}

func TestASweepIsParkedWhileAnythingUnderItIsSuspended(t *testing.T) {
	t.Parallel()

	cluster := newFakeCluster(t)
	cluster.serve("v1", listable("Pod", "pods", true))
	svc, pool := newTestService(t)
	start(t, svc)

	// Nothing vouches yet, so every probe suspends and schedules nothing.
	svc.TrackDiscovery(1, testParams)
	sess := svc.sessionOf(1)
	require.Eventually(t, sess.anyWaiting,
		testutil.Timeout, time.Millisecond, "a suspended sweep is waiting at the gate")

	// The wake loop hands it a connection, and a settled sweep is past the gate.
	pool.lease("prod").connect(t, cluster, "uid-1")
	require.Eventually(t, func() bool { return !sess.anyWaiting() },
		testutil.Timeout, time.Millisecond, "a settled sweep is not waiting")
}

func TestASettledSweepReportsAConnectionItLost(t *testing.T) {
	t.Parallel()

	cluster := newFakeCluster(t)
	cluster.serve("v1", listable("Pod", "pods", true))
	svc, pool := newTestService(t)
	pool.lease("prod").connect(t, cluster, "uid-1")
	start(t, svc)
	svc.TrackDiscovery(1, testParams)
	awaitDiscovered(t, svc, 1)

	// A settled sweep is scheduled rather than parked, so nothing but this wake replaces a
	// verdict the connection just made wrong.
	pool.lease("prod").drop()

	awaitReason(t, svc, 1, ReasonNoConnection)
}

func TestASettledSweepReportsAConnectionThatStartedAnsweringAsAnotherCluster(t *testing.T) {
	t.Parallel()

	cluster := newFakeCluster(t)
	cluster.serve("v1", listable("Pod", "pods", true))
	svc, pool := newTestService(t)
	pool.lease("prod").connect(t, cluster, "uid-1")
	start(t, svc)
	svc.TrackDiscovery(1, testParams)
	awaitDiscovered(t, svc, 1)

	pool.lease("prod").connect(t, cluster, "another-uid")

	awaitReason(t, svc, 1, ReasonIdentityMismatch)
}

func TestALostConnectionReportedOnceIsNotWokenAgain(t *testing.T) {
	t.Parallel()

	cluster := newFakeCluster(t)
	cluster.serve("v1", listable("Pod", "pods", true))
	svc, pool := newTestService(t)
	pool.lease("prod").connect(t, cluster, "uid-1")
	start(t, svc)
	svc.TrackDiscovery(1, testParams)
	awaitDiscovered(t, svc, 1)

	pool.lease("prod").drop()
	awaitReason(t, svc, 1, ReasonNoConnection)

	// The feed publishes every pass, not only the ones that changed something. A verdict
	// that already names the connection is not news, and re-waking on each frame would be
	// the poll a suspended sweep exists to avoid. One dial per frame is the wake loop
	// looking; a further one is a run it woke — so the baseline is the point the cache has
	// stopped dialing, not the point one probe committed.
	lease := pool.lease("prod")
	awaitDialsQuiet(t, lease)
	lease.drop()

	lease.dialed.Await(t, "the wake loop to look at the frame")
	testutil.NoRecv(t, lease.dialed.Chan(), quietWindow,
		"a sweep re-woken by a connection whose verdict is already reported")
}

func TestAKindParkedAtTheGateReportsWhyItWaits(t *testing.T) {
	t.Parallel()

	cluster := newFakeCluster(t)
	cluster.serveKind(podKind, true)
	cluster.hasObjects(podKind, "10")
	cluster.streamKind(podKind)

	svc := newSyncingService(t, cluster)
	syncKind(t, svc, 1, podKind)
	awaitKindReason(t, svc, 1, podKind, ReasonWatching)

	// Each refusal is the kind's own news, in the order the pool moved through them, and
	// the retry countdown does not survive into the wait: nothing is retrying at the gate.
	lease := svc.connSvc.(*fakePool).lease("prod")
	lease.drop()
	awaitKindReason(t, svc, 1, podKind, ReasonNoConnection)
	state, _ := svc.GetKindState(1, podKind)
	assert.True(t, state.NextRetryAt.IsZero())

	lease.connect(t, cluster, "another-uid")
	awaitKindReason(t, svc, 1, podKind, ReasonIdentityMismatch)

	lease.connect(t, cluster, "uid-1")
	awaitKindReason(t, svc, 1, podKind, ReasonWatching)
}

func TestACacheStandsBehindNoReasonUntilARunCommitsOne(t *testing.T) {
	t.Parallel()

	svc, _ := newTestService(t)

	sess := newSession(svc, 1, testParams)
	assert.Empty(t, sess.discoveryReason(),
		"a cache whose sweep has not answered names no reason, so the first frame is news")
}

// The admission is one critical section: it checks the cache is armed and reads the kind
// together. Split apart, a ForgetKind landing between the two would leave the run listing rows
// for a kind nobody tracks — the relist-behind-a-clear race ForgetKind is ordered before
// ClearKind to rule out.
func TestAKindRunIsAdmittedWithItsWholeKind(t *testing.T) {
	t.Parallel()

	svc, pool := newTestService(t)
	pool.lease("prod").vouch(t, "uid-1")
	svc.TrackDiscovery(1, testParams)
	svc.TrackKind(1, podKind)

	sess, k, ok := svc.enterKindRun(1, idOf(podKind))
	require.True(t, ok, "an armed cache with the kind tracked admits the run")
	assert.Equal(t, podKind, k, "the run is handed the whole value, singular included")

	sess.leaveRun()
}

func TestAKindRunIsRefusedForACacheOrAKindThatIsGone(t *testing.T) {
	t.Parallel()

	svc, pool := newTestService(t)
	pool.lease("prod").vouch(t, "uid-1")

	_, _, ok := svc.enterKindRun(1, idOf(podKind))
	assert.False(t, ok, "nothing has armed this cache")

	svc.TrackDiscovery(1, testParams)
	_, _, ok = svc.enterKindRun(1, idOf(podKind))
	assert.False(t, ok, "the cache is armed but nothing tracks this kind")
}

// The three discovery probes are three registrations running concurrently under one subject, so
// a mark they shared would let the one that passed the gate strand the one that was refused.
func TestOneRunLeavingTheGateDoesNotClearAnother(t *testing.T) {
	t.Parallel()

	svc, _ := newTestService(t)
	sess := newSession(svc, 1, testParams)

	sess.setWaiting(probeGateKey(nameAPIVersions))
	sess.setWaiting(probeGateKey(nameAPIGroups))
	sess.clearWaiting(probeGateKey(nameAPIGroups))

	assert.True(t, sess.anyWaiting(), "a probe still at the gate keeps the cache waiting")
}

// The supervisor files a suspension only once the run has returned, so a connection frame read
// while the run is still deciding sees nothing parked — which is the wake that goes missing. The
// session's mark stands through that window.
func TestARunIsWaitingBeforeItsSuspensionIsFiled(t *testing.T) {
	t.Parallel()

	fake := newHeldAtGateKindSync()
	svc, pool := newTestService(t, fake.option())
	start(t, svc)
	pool.lease("prod").vouch(t, "uid-other")

	svc.TrackDiscovery(1, testParams)
	svc.TrackKind(1, podKind)
	fake.gateRefused.Await(t, "the kind reaches the gate and is refused")

	sess := svc.sessionOf(1)
	svc.mu.Lock()
	marked := sess.waiting[kindGateKey(idOf(podKind))]
	svc.mu.Unlock()
	assert.Equal(t, Reason(ReasonIdentityMismatch), marked,
		"a run refused at the gate is waiting, under its own cause, before it returns")

	snap, ok := svc.kindSupervisor.Read(kindSubject(1, podKind))
	require.True(t, ok, "the kind's subject is tracked")
	assert.False(t, snap.Attempts(nameKindSync).Suspended(),
		"the snapshot has nothing filed yet, which is why the mark has to be the session's")
}

// The bridge decides off the session's marks, never the supervisor's snapshot. A run that has
// marked itself and not yet suspended is precisely what the snapshot cannot report, and a frame
// read in that gap is the wake that goes missing.
func TestAConnectionFrameWakesAMarkTheSnapshotCannotSee(t *testing.T) {
	t.Parallel()

	cluster := newFakeCluster(t)
	cluster.serve("v1", listable("Pod", "pods", true))
	svc, pool := newTestService(t)
	pool.lease("prod").connect(t, cluster, "uid-1")
	start(t, svc)

	// The sweep settles, so nothing under this cache reads as suspended.
	svc.TrackDiscovery(1, testParams)
	cluster.awaitRead(t, "/api")

	// A run at the gate, as the mark stands during the hop the snapshot has nothing filed for.
	svc.sessionOf(1).setWaiting(probeGateKey(nameAPIVersions))
	pool.lease("prod").connect(t, cluster, "uid-1")

	cluster.awaitRead(t, "/api")
}

// A kind's reason is its own. The sweep can already be reporting what the pool now says while a
// kind is parked under an older answer, and asking the sweep alone would leave that kind naming a
// cause that is no longer true, with nothing left to correct it.
func TestAParkedRunIsWokenWhenItsOwnReasonIsStale(t *testing.T) {
	t.Parallel()

	fake := newHeldAtGateKindSync()
	fake.releaseGate() // the refusals are what this test counts, not the window between them
	svc, pool := newTestService(t, fake.option())
	start(t, svc)

	svc.TrackDiscovery(1, testParams)
	svc.TrackKind(1, podKind)
	fake.gateRefused.Await(t, "the kind is refused for want of a connection")
	awaitReason(t, svc, 1, ReasonNoConnection)

	// The kind parked under a reason the pool has since replaced, which is what the sweep's own
	// verdict cannot report: it is already saying what the next frame will say.
	svc.sessionOf(1).waitingOn(kindGateKey(idOf(podKind)), ReasonIdentityMismatch)
	pool.lease("prod").drop()

	fake.gateRefused.Await(t, "the kind runs again to record the reason that replaced its own")
}

// The whole point of the bridge, end to end and at both levels: a connection that went away and
// came back leaves nothing behind it.
func TestAConnectionThatCameBackStartsEveryWaitingKind(t *testing.T) {
	t.Parallel()

	deployKind := testKind("apps/v1", "Deployment", "deployments")
	cluster := newFakeCluster(t)
	for _, k := range []kubestore.Kind{podKind, deployKind} {
		cluster.serveKind(k, true)
		cluster.hasObjects(k, "10")
		cluster.streamKind(k)
	}

	svc := newSyncingService(t, cluster)
	syncKind(t, svc, 1, podKind)
	syncKind(t, svc, 1, deployKind)
	awaitKindReason(t, svc, 1, podKind, ReasonWatching)
	awaitKindReason(t, svc, 1, deployKind, ReasonWatching)

	lease := svc.connSvc.(*fakePool).lease("prod")
	lease.drop()
	awaitReason(t, svc, 1, ReasonNoConnection)
	awaitKindReason(t, svc, 1, podKind, ReasonNoConnection)
	awaitKindReason(t, svc, 1, deployKind, ReasonNoConnection)

	lease.connect(t, cluster, "uid-1")

	awaitDiscovered(t, svc, 1)
	awaitKindReason(t, svc, 1, podKind, ReasonWatching)
	awaitKindReason(t, svc, 1, deployKind, ReasonWatching)
}

// A run reads the gate at its own moment, so it can catch an outage the bridge never hears about:
// the pool keeps only the latest state per context, and an outage that ended arrives as one frame
// saying what the bridge already believed. The mark is what releases the cache anyway — the
// success branch asks whether anything is waiting, never whether the answer moved.
func TestACacheHeldBehindARefusalTheBridgeNeverSawIsReleased(t *testing.T) {
	t.Parallel()

	cluster := newFakeCluster(t)
	cluster.serve("v1", listable("Pod", "pods", true))

	svc, pool := newTestService(t)
	lease := pool.lease("prod")
	conn := cluster.connection(t)
	lease.hand(conn, "uid-1")
	start(t, svc)
	svc.TrackDiscovery(1, testParams)
	awaitDiscovered(t, svc, 1)
	awaitDialsQuiet(t, lease)

	// The outage, and a run that catches it. The bridge is told nothing.
	lease.setQuietly(nil, "")
	svc.discoverySupervisor.Wake(svc.sessionOf(1).discoverySubject(), discoveryProbes...)
	awaitReason(t, svc, 1, ReasonNoConnection)

	// It is back before the one frame the bridge does get, which therefore says exactly what
	// the bridge last read.
	lease.setQuietly(conn, "uid-1")
	lease.publish()

	awaitDiscovered(t, svc, 1)
}

// A gate error that names neither a missing connection nor a mismatched identity is a Fail, not
// a park: the probe climbs its backoff ladder, and a mark left standing over it would have the
// bridge re-dispatch it on every connection frame, ahead of that ladder.
func TestASweepThatFailsAtTheGateIsNotWaiting(t *testing.T) {
	t.Parallel()

	svc, pool := newTestService(t)
	pool.lease("prod").refuse(errors.New("the pool will not answer"))
	start(t, svc)

	svc.TrackDiscovery(1, testParams)
	awaitReason(t, svc, 1, ReasonDiscoveryFailed)

	// The first probe to fail publishes the reason while the other two may still be at the
	// gate, each marked until its own refusal clears it, so the marks are waited out.
	sess := svc.sessionOf(1)
	require.Eventually(t, func() bool { return !sess.anyWaiting() },
		testutil.Timeout, time.Millisecond, "a probe pacing its own retries is not waiting on a connection")
}

func TestAForgottenKindStopsWakingTheCache(t *testing.T) {
	t.Parallel()

	svc, _ := newTestService(t)
	sess := newSession(svc, 1, testParams)

	sess.setWaiting(kindGateKey(idOf(podKind)))
	sess.dropKind(idOf(podKind))

	assert.False(t, sess.anyWaiting(), "a kind nothing tracks is not waiting at the gate")
}
