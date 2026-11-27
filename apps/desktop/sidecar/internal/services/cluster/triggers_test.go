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

	"github.com/amorey/beehive"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/testutil"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/cluster/internal/kubestore"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/cluster/internal/kubesync"
)

// startTrigger starts tr and joins it on cleanup.
func startTrigger[T, W any](t *testing.T, tr *trigger[T, W]) {
	t.Helper()
	stop, err := tr.Start(context.Background())
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, stop(context.Background())) })
}

// The subscription is current-on-subscribe, so whatever the file already held names the
// anchor at startup rather than waiting for the user to edit it.
func TestKubeconfigTriggerWakesTheAnchorForTheStartupSnapshot(t *testing.T) {
	t.Parallel()

	tr := newKubeconfigTrigger(newFakeKubeconfigSource(cfgWith("prod")))
	startTrigger(t, tr)

	assert.Equal(t, ClusterSourceNameKubeconfig, testutil.Recv(t, tr.Wakes(), "the startup poke"))
}

// Every change names the same record: which contexts moved is the anchor's pass to work
// out, so the trigger translates rather than deciding.
func TestKubeconfigTriggerWakesTheAnchorForEachSnapshot(t *testing.T) {
	t.Parallel()

	src := newFakeKubeconfigSource(cfgWith("prod"))
	tr := newKubeconfigTrigger(src)
	startTrigger(t, tr)
	testutil.Recv(t, tr.Wakes(), "the startup poke")

	src.publish(cfgWith("prod", "staging"))

	assert.Equal(t, ClusterSourceNameKubeconfig, testutil.Recv(t, tr.Wakes(), "the second poke"))
}

// The watcher shutting down ends the loop, and closing the channel is what ends
// beehive's read of it — it drains what was already sent and stops.
func TestTriggerClosesItsChannelWhenTheSourceCloses(t *testing.T) {
	t.Parallel()

	src := newFakeKubeconfigSource(cfgWith("prod"))
	tr := newKubeconfigTrigger(src)
	startTrigger(t, tr)
	testutil.Recv(t, tr.Wakes(), "the startup poke")

	src.close()

	testutil.WaitClosed(t, tr.Wakes(), "the wake channel to close with the feed")
}

// The stop func must join the loop even mid-send, so service.Close never races one.
// Nothing reads Wakes here, so the loop is parked on exactly that send.
func TestTriggerStopJoinsTheLoop(t *testing.T) {
	t.Parallel()

	tr := newKubeconfigTrigger(newFakeKubeconfigSource(cfgWith("prod")))
	stop, err := tr.Start(context.Background())
	require.NoError(t, err)

	testutil.WaitReturn(t, func() { assert.NoError(t, stop(context.Background())) }, "stop to return")
}

// The app owns the kubeconfig service and hands it to every reader, so stopping the
// trigger must release only its own subscription: the service's Close ends every
// subscription the process holds.
func TestTriggerStopLeavesTheKubeconfigServiceOpen(t *testing.T) {
	t.Parallel()

	d := newTestDeps(t)
	tr := newKubeconfigTrigger(d.kubeconfigSvc)

	stop, err := tr.Start(context.Background())
	require.NoError(t, err)
	require.NoError(t, stop(context.Background()))

	sub := d.kubeconfigSvc.WatchConfigState()
	defer sub.Close()
	testutil.Recv(t, sub.Chan(), "the current config from a service still open")
}

// --- the seam ---

// stringFeed is a hand-driven feed carrying a type no source here produces, so the loop
// is exercised against the seam rather than against its one implementation.
type stringFeed struct {
	ch     chan string
	closed *testutil.Signal
}

func (f stringFeed) Chan() <-chan string { return f.ch }
func (f stringFeed) Close()              { f.closed.Fire() }

// A trigger forwards the name its mapper builds, over whatever feed it was given.
func TestTriggerForwardsTheNameItsMapperBuilds(t *testing.T) {
	t.Parallel()

	ch := make(chan string, 1)
	closed := testutil.NewSignal()

	tr := newTrigger(
		func() changeSource[string] { return stringFeed{ch: ch, closed: closed} },
		always(func(v string) string { return "mapped/" + v }),
	)
	startTrigger(t, tr)

	ch <- "changed"
	assert.Equal(t, "mapped/changed", testutil.Recv(t, tr.Wakes(), "the poke"))
}

// A value the mapper cannot address wakes nothing, and the next one still goes.
func TestTriggerSkipsAValueThatNamesNoRecord(t *testing.T) {
	t.Parallel()

	ch := make(chan string, 2)
	tr := newTrigger(
		func() changeSource[string] { return stringFeed{ch: ch, closed: testutil.NewSignal()} },
		func(v string) (string, bool) { return "mapped/" + v, v != "unknown" },
	)
	startTrigger(t, tr)

	ch <- "unknown"
	ch <- "known"
	assert.Equal(t, "mapped/known", testutil.Recv(t, tr.Wakes(), "the poke"))
}

// Stopping ends a wake nobody is reading, so a stalled reader cannot hold the stop.
func TestTriggerStopEndsAnUnreadWake(t *testing.T) {
	t.Parallel()

	ch := make(chan string)
	tr := newTrigger(
		func() changeSource[string] { return stringFeed{ch: ch, closed: testutil.NewSignal()} },
		always(func(v string) string { return v }),
	)
	stop, err := tr.Start(context.Background())
	require.NoError(t, err)
	// Unbuffered, so the loop holds the value once the send returns, and nothing reads
	// the wake it owes.
	ch <- "changed"

	require.NoError(t, stop(context.Background()))
}

// The loop owns the subscription, so stopping it is what releases the feed.
func TestTriggerReleasesItsFeedOnExit(t *testing.T) {
	t.Parallel()

	closed := testutil.NewSignal()
	tr := newTrigger(
		func() changeSource[string] { return stringFeed{ch: make(chan string), closed: closed} },
		always(func(v string) string { return v }),
	)
	stop, err := tr.Start(context.Background())
	require.NoError(t, err)

	require.NoError(t, stop(context.Background()))

	testutil.Wait(t, closed.Chan(), "the feed released on exit")
}

// --- the kubeconn trigger ---

// claimsOf is the index a test hands the kubeconn trigger: the record holding each
// context, as the controller's leases would answer.
func claimsOf(held map[string]string) func(string) (string, bool) {
	return func(contextName string) (string, bool) {
		id, ok := held[contextName]
		return id, ok
	}
}

// The point of the whole seam: a probe landing brings back the record for the context it
// answered for, so the pass reads what the probe found instead of waiting out the cadence.
func TestKubeconnTriggerWakesTheContextsCluster(t *testing.T) {
	t.Parallel()

	svc := &fakeKubeconn{}
	tr := newKubeconnTrigger(svc, claimsOf(map[string]string{"prod": "row-prod"}))
	startTrigger(t, tr)

	svc.publish("prod")

	assert.Equal(t, "row-prod", testutil.Recv(t, tr.Wakes(), "the wake for the context"))
}

// The index is the controller's claims, so the record to wake is the one the probe
// was armed for: a context no record is probed under wakes nothing.
func TestKubeconnTriggerWakesNothingForAnUnclaimedContext(t *testing.T) {
	t.Parallel()

	c := controllerOver(t)
	obj := createCluster(t, c.deps, "prod")
	tr := newKubeconnTrigger(c.kubeconnSvc, c.leases.clusterFor)
	startTrigger(t, tr)
	pool := c.kubeconnSvc.(*fakeKubeconn)

	pool.publish("prod")
	reconcileCluster(t, c, &stubControllerClient{}, obj)
	pool.publish("prod")

	assert.Equal(t, string(clusterIDOf(obj)), testutil.Recv(t, tr.Wakes(), "the wake once the claim is held"))
}

// The bus holds a slot per context, so a fleet answering at once neither loses a context
// behind a busier one nor collapses to whichever landed last.
func TestKubeconnTriggerWakesEveryContextThatMoved(t *testing.T) {
	t.Parallel()

	svc := &fakeKubeconn{}
	tr := newKubeconnTrigger(svc, claimsOf(map[string]string{"prod": "row-prod", "staging": "row-staging"}))
	startTrigger(t, tr)

	svc.publish("prod")
	svc.publish("staging")

	woke := []string{
		testutil.Recv(t, tr.Wakes(), "the first wake"),
		testutil.Recv(t, tr.Wakes(), "the second wake"),
	}
	assert.ElementsMatch(t, []string{"row-prod", "row-staging"}, woke)
}

// A cache id addresses its own record, so the discovery feed's key is the whole wake and the
// trigger requeues by id rather than resolving a name.
func TestKubesyncDiscoveryTriggerWakesTheCacheThatMoved(t *testing.T) {
	t.Parallel()

	sync := newFakeKubesync()
	tr := newKubesyncDiscoveryTrigger(sync)
	startTrigger(t, tr)

	sync.publishDiscoveryChange(7)

	assert.Equal(t, beehive.ObjectID(7), testutil.Recv(t, tr.Wakes(), "the cache's poke"))
}

// A kind's record is named after the cache and the GVR kubesync syncs, which is why this one
// requeues by name: the record's id is the store's to assign, its name is derivable.
func TestKubesyncKindTriggerNamesTheKindsRecord(t *testing.T) {
	t.Parallel()

	sync := newFakeKubesync()
	tr := newKubesyncKindTrigger(sync)
	startTrigger(t, tr)

	sync.publishKindChange(kubesync.KindKey{
		CacheID: 7,
		Kind:    kubestore.Kind{APIVersion: "apps/v1", Kind: "Deployment", Resource: "deployments"},
	})

	assert.Equal(t, ClusterCachedKindName(7, "apps/v1", "deployments"),
		testutil.Recv(t, tr.Wakes(), "the kind's poke"))
}

// The size feed carries the cache whose file crossed its ceiling, and a cache id addresses
// its own record — so the key is the whole wake, as it is for discovery.
func TestKubestoreSizeLimitTriggerWakesTheCacheThatMoved(t *testing.T) {
	t.Parallel()

	store := newFakeKubestore(t)
	tr := newKubestoreSizeLimitTrigger(store)
	startTrigger(t, tr)

	store.publishSizeLimitChange(7)

	assert.Equal(t, beehive.ObjectID(7), testutil.Recv(t, tr.Wakes(), "the cache's poke"))
}
