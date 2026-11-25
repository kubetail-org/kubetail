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

// Triggers: the bridge from change notifications outside beehive to the records a change
// reaches. Each maps a source's own vocabulary onto beehive names and hands the channel
// to the trigger option declared at registration — beehive owns the receive loop, its
// rate against the store, and its place in the shutdown order. This is the feed half.
//
// Translation is all that is left here, and it is the part beehive cannot do: a source
// of truth is not an object, and only this package knows which record holds the claim
// on the kube-context "prod".
package cluster

import (
	"context"
	"sync"

	"github.com/amorey/beehive"
	"github.com/amorey/gobus"
	"k8s.io/client-go/tools/clientcmd/api"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/drain"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/cluster/internal/kubeconn"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/cluster/internal/kubestore"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/cluster/internal/kubesync"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/kubeconfig"
)

// feed is a source's change stream: one value per change, closed when the source stops.
// The value names what moved and nothing more — a poke asks for a pass, and the pass
// reads the source itself, so handing it a snapshot through a queue that coalesces is
// how an observation gets written from a state that has already moved on.
type changeSource[T any] interface {
	Chan() <-chan T
	Close()
}

// trigger turns one feed into the wakes beehive requeues, each addressing the record that
// moved. It holds the subscription for as long as the process wants the feed, so it is a
// lifecycle.Part like everything else that runs.
//
// W is how the record is addressed — a name or a beehive.ObjectID — because each source
// already holds one and not the other, and converting between them is a store read a
// translation must not need.
//
// It carries no retry ladder. A lost poke costs latency rather than divergence — every
// kind it pokes re-arms on a cadence of its own, which is what re-reads a source when
// nothing told it to.
type trigger[T, W any] struct {
	subscribe func() changeSource[T]
	// address maps one value from the feed onto the record that moved. An address, never
	// state. False is a value naming no record, which wakes nothing.
	address func(T) (W, bool)
	// wakes is what beehive reads. Unbuffered: beehive floors how often a poke reaches
	// the store, and a queue here would be a second policy about the same thing.
	wakes chan W

	wg sync.WaitGroup
}

func newTrigger[T, W any](subscribe func() changeSource[T], address func(T) (W, bool)) *trigger[T, W] {
	return &trigger[T, W]{subscribe: subscribe, address: address, wakes: make(chan W)}
}

// always addresses every value.
func always[T, W any](address func(T) W) func(T) (W, bool) {
	return func(v T) (W, bool) { return address(v), true }
}

// Wakes is the channel to declare at registration, each value addressing the record to
// requeue. Read before Start, since registration comes first.
func (t *trigger[T, W]) Wakes() <-chan W { return t.wakes }

// Start subscribes and translates until stopped. The subscription is established before
// Start returns, so a current-on-subscribe feed pokes once for whatever the source
// already held.
func (t *trigger[T, W]) Start(context.Context) (func(context.Context) error, error) {
	// Not Start's context, which bounds startup: this one bounds the loop and the send in
	// flight, so it lives until the stop func cancels it.
	loopCtx, stopLoop := context.WithCancel(context.Background())

	sub := t.subscribe()
	t.wg.Go(func() {
		// Closing the channel is what ends beehive's read of it, after it drains what this
		// loop already sent. Releasing the subscription is all this trigger owns — the
		// service behind it is the app's, and closing that would end every subscription in
		// the process.
		defer close(t.wakes)
		defer sub.Close()
		t.run(loopCtx, sub)
	})

	return func(ctx context.Context) error {
		stopLoop()
		return drain.WithContext(ctx, t.wg.Wait)
	}, nil
}

// run forwards one address per change until stopped or the feed closes. Cancellation ends
// both waits: a feed is not required to close its channel when released, so the receive
// needs the same escape the send does.
func (t *trigger[T, W]) run(ctx context.Context, sub changeSource[T]) {
	for {
		select {
		case <-ctx.Done():
			return
		case v, ok := <-sub.Chan():
			if !ok {
				return
			}
			w, ok := t.address(v)
			if !ok {
				continue
			}
			select {
			case <-ctx.Done():
				return
			case t.wakes <- w:
			}
		}
	}
}

// kubeconfigSource is the subscribe half of kubeconfigService, all a trigger needs,
// narrow so a test can substitute a hand-driven hub.
type kubeconfigSource interface {
	WatchConfigState() kubeconfig.ConfigStateWatch
}

// newKubeconfigTrigger watches the user's kubeconfig for the kubeconfig anchor. Every
// change names the same record: which contexts moved is that pass's to work out.
func newKubeconfigTrigger(cfgSource kubeconfigSource) *trigger[*api.Config, string] {
	return newTrigger(
		func() changeSource[*api.Config] { return cfgSource.WatchConfigState() },
		always(func(*api.Config) string { return ClusterSourceNameKubeconfig }),
	)
}

// kubeconnSource is the subscribe half of the pool, all a trigger needs, narrow so a test
// can substitute a hand-driven hub.
type kubeconnSource interface {
	ListenForConnectionChanges() kubeconn.ConnectionChangeNotifications
}

// newKubeconnTrigger wakes the cluster record for a context whose probe said something
// new. A record reports what the pool holds rather than what the store does, so beehive
// cannot know when that observation went stale; this is the only thing that reaches it,
// and the kind's own resync covers a signal that went missing. clusterFor names the
// record holding the claim on a context — the controller's own index — since a
// record's name says nothing about its context.
func newKubeconnTrigger(pool kubeconnSource, clusterFor func(contextName string) (string, bool)) *trigger[gobus.Event[string, struct{}], string] {
	return newTrigger(
		func() changeSource[gobus.Event[string, struct{}]] { return pool.ListenForConnectionChanges() },
		// The event's key is the context that moved; its value carries nothing, since what
		// it now says is the pass's to read.
		func(ev gobus.Event[string, struct{}]) (string, bool) { return clusterFor(ev.Key) },
	)
}

// discoveryNewsSource is the discovery-news half of the sync seam, all a trigger needs.
type discoveryNewsSource interface {
	ListenForDiscoveryChanges() kubesync.DiscoveryChangeNotifications
}

// newKubesyncDiscoveryTrigger wakes the cache record whose sweep said something new. By ID,
// because a cache id IS the record's id — the seam speaks the number the store assigned, and
// resolving a name for it would be a read that says nothing the key does not.
func newKubesyncDiscoveryTrigger(newsSource discoveryNewsSource) *trigger[gobus.Event[int64, struct{}], beehive.ObjectID] {
	return newTrigger(
		func() changeSource[gobus.Event[int64, struct{}]] { return newsSource.ListenForDiscoveryChanges() },
		always(func(ev gobus.Event[int64, struct{}]) beehive.ObjectID { return beehive.ObjectID(ev.Key) }),
	)
}

// sizeLimitNewsSource is the size-limit half of the store seam, all a trigger needs.
type sizeLimitNewsSource interface {
	ListenForSizeLimitChanges() kubestore.SizeLimitChangeNotifications
}

// newKubestoreSizeLimitTrigger wakes the cache record whose file crossed its size ceiling. By ID,
// for the reason the discovery trigger is: the key already IS the record's id.
func newKubestoreSizeLimitTrigger(newsSource sizeLimitNewsSource) *trigger[gobus.Event[int64, struct{}], beehive.ObjectID] {
	return newTrigger(
		func() changeSource[gobus.Event[int64, struct{}]] { return newsSource.ListenForSizeLimitChanges() },
		always(func(ev gobus.Event[int64, struct{}]) beehive.ObjectID { return beehive.ObjectID(ev.Key) }),
	)
}

// kindNewsSource is the kind-news half of the sync seam, all a trigger needs.
type kindNewsSource interface {
	ListenForKindChanges() kubesync.KindChangeNotifications
}

// newKubesyncKindTrigger wakes the record standing for one kind in one cache. By NAME, because
// that record's id is the store's to assign where its name is derivable from the GVR kubesync
// already holds — which is what keeps a record id out of the seam below.
//
// The key carries the singular and the name does not, so a kind renamed under an unchanged
// plural addresses the same record twice: a duplicate wake, never a missed one.
func newKubesyncKindTrigger(newsSource kindNewsSource) *trigger[gobus.Event[kubesync.KindKey, struct{}], string] {
	return newTrigger(
		func() changeSource[gobus.Event[kubesync.KindKey, struct{}]] { return newsSource.ListenForKindChanges() },
		always(func(ev gobus.Event[kubesync.KindKey, struct{}]) string {
			return ClusterCachedKindName(beehive.ObjectID(ev.Key.CacheID), ev.Key.APIVersion, ev.Key.Resource)
		}),
	)
}
