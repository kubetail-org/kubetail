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

// Shared fixtures: the pool and the store directory a Service is built over, the api
// server a sweep reads, and the waits a test settles on.
package kubesync

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	gobuswatch "github.com/amorey/gobus/watch"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/dynamic"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/rest"
	clienttesting "k8s.io/client-go/testing"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/supervisor"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/testutil"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/cluster/internal/kubeconn"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/cluster/internal/kubestore"
)

// --- The pool ---

// fakePool stands in for the connection pool: one lease per context, handed back to
// every claim so a test can vouch for an identity before or after a session takes one.
type fakePool struct {
	mu     sync.Mutex
	leases map[string]*fakeLease
	hub    *gobuswatch.Hub[string, kubeconn.State]
}

func newFakePool() *fakePool {
	return &fakePool{leases: map[string]*fakeLease{}, hub: gobuswatch.New[string, kubeconn.State]()}
}

func (p *fakePool) Acquire(contextName string) kubeconn.Lease {
	l := p.lease(contextName)
	l.mu.Lock()
	l.claims++
	l.mu.Unlock()
	return l
}

// lease returns the claim handed out for a context, building it if nothing has claimed
// one yet — so a test can vouch for an identity before the session arms.
func (p *fakePool) lease(contextName string) *fakeLease {
	p.mu.Lock()
	defer p.mu.Unlock()
	l, ok := p.leases[contextName]
	if !ok {
		l = &fakeLease{contextName: contextName, hub: p.hub, dialed: testutil.NewProbe[string](64)}
		p.leases[contextName] = l
	}
	return l
}

// fakeLease answers ConnFor from whatever the test vouched for, and publishes on the same hub
// kubeconn does — so a session's bridge sees frames land the way a real pool delivers them.
type fakeLease struct {
	contextName string
	hub         *gobuswatch.Hub[string, kubeconn.State]
	// dialed carries every ConnFor: the wake loop looking at a frame, and the runs it wakes.
	dialed *testutil.Probe[string]

	mu       sync.Mutex
	claims   int
	released int
	uid      string
	conn     *kubeconn.Connection
	err      error
	watches  int
}

// refuse makes this lease answer with err — the pool reporting something that is neither
// "no connection yet" nor a mismatched identity.
func (l *fakeLease) refuse(err error) {
	l.mu.Lock()
	l.err = err
	l.mu.Unlock()
	_ = l.hub.Sender().Send(l.contextName, kubeconn.State{})
}

// vouch makes this lease answer for serverUID over an api server serving nothing, for a
// test about arming rather than about what a sweep reads. A real one, because a sweep dials
// whatever the lease hands it.
func (l *fakeLease) vouch(t *testing.T, serverUID string) { l.connect(t, newFakeCluster(t), serverUID) }

// connect makes this lease answer for serverUID over a connection to cluster.
func (l *fakeLease) connect(t *testing.T, cluster *fakeCluster, serverUID string) {
	l.hand(cluster.connection(t), serverUID)
}

// hand publishes a connection and wakes whoever is waiting on this claim. The one it
// replaces is retired, as the pool retires what it rebuilds.
func (l *fakeLease) hand(conn *kubeconn.Connection, serverUID string) {
	l.mu.Lock()
	prev := l.conn
	l.uid, l.conn = serverUID, conn
	l.mu.Unlock()
	if prev != nil {
		prev.Retire()
	}
	_ = l.hub.Sender().Send(l.contextName, kubeconn.State{})
}

// setQuietly moves what this lease answers with and publishes nothing — an outage or a recovery
// the session's bridge is never told about, which is what the far side of a coalesced frame
// looks like. publish then sends one frame.
func (l *fakeLease) setQuietly(conn *kubeconn.Connection, serverUID string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.conn, l.uid = conn, serverUID
}

func (l *fakeLease) publish() { _ = l.hub.Sender().Send(l.contextName, kubeconn.State{}) }

// drop takes the connection away, which is what a cluster going unreachable looks like.
func (l *fakeLease) drop() {
	l.mu.Lock()
	prev := l.conn
	l.conn, l.uid = nil, ""
	l.mu.Unlock()
	if prev != nil {
		prev.Retire()
	}
	_ = l.hub.Sender().Send(l.contextName, kubeconn.State{})
}

func (l *fakeLease) held() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.claims - l.released
}

func (l *fakeLease) Conn(context.Context) (*kubeconn.Connection, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.err != nil {
		return nil, l.err
	}
	if l.conn == nil {
		return nil, kubeconn.ErrNoConnection
	}
	return l.conn, nil
}

func (l *fakeLease) ConnFor(ctx context.Context, serverUID string) (*kubeconn.Connection, error) {
	l.dialed.Fire(serverUID)
	conn, err := l.Conn(ctx)
	if err != nil {
		return nil, err
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.uid != serverUID {
		return nil, kubeconn.ErrIdentityMismatch
	}
	return conn, nil
}

func (l *fakeLease) State() kubeconn.State { return kubeconn.State{} }

func (l *fakeLease) WatchState() *kubeconn.StateWatch {
	l.mu.Lock()
	l.watches++
	l.mu.Unlock()
	return kubeconn.NewStateWatch(l.State(), l.hub.Watch(l.contextName))
}

func (l *fakeLease) Departed() bool { return false }

// DataRates and ListenForDataRateChanges are the data-rate stream's, which nothing here reads.
func (l *fakeLease) DataRates() []kubeconn.DataRateSample { return nil }

func (l *fakeLease) ListenForDataRateChanges() kubeconn.ConnectionChangeNotifications { return nil }

func (l *fakeLease) Release() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.released++
}

// --- The cluster ---

// fakeCluster is an api server enough of one for a sweep: the two discovery documents, one
// per group-version, and the CRD collection behind IsCRD.
type fakeCluster struct {
	baseURL *url.URL
	client  *http.Client
	dyn     *dynamicfake.FakeDynamicClient

	// transport is what every discovery read goes through: it reports the reads, and parks
	// the one path a test holds. reads and cancelled are its two probes, kept here because
	// that is where a test reaches for them.
	transport *holdTransport
	reads     *testutil.Probe[string]
	cancelled *testutil.Probe[string]
	// listed carries every collection LIST a kind sync asks for.
	listed *testutil.Probe[string]

	mu sync.Mutex
	// groups is the non-core groups, in the order /apis serves them; resources is one
	// document per group-version; broken is the paths answering 500; raw is the paths
	// answering a body of the test's own choosing.
	groups    []metav1.APIGroup
	resources map[string]*metav1.APIResourceList
	broken    map[string]bool
	raw       map[string]string
}

func newFakeCluster(t *testing.T) *fakeCluster {
	t.Helper()
	c := &fakeCluster{
		reads:     testutil.NewProbe[string](32),
		cancelled: testutil.NewProbe[string](32),
		listed:    testutil.NewProbe[string](32),
		resources: map[string]*metav1.APIResourceList{},
		broken:    map[string]bool{},
		raw:       map[string]string{},
		dyn:       newFakeDynamic(),
	}

	srv := httptest.NewServer(http.HandlerFunc(c.serveHTTP))
	t.Cleanup(srv.Close)

	base, err := url.Parse(srv.URL)
	require.NoError(t, err)
	c.transport = &holdTransport{
		base: srv.Client().Transport, reads: c.reads, cancelled: c.cancelled,
	}
	c.baseURL, c.client = base, &http.Client{Transport: c.transport}
	return c
}

// connection is what a lease hands a run: the raw client the discovery documents are read
// over, and the dynamic client the lists and watches go through. Built the way the pool
// builds one, so retiring it fires Done for whoever holds it; the clients are then swapped for
// this cluster's own, since its lists and watches are served in memory rather than over HTTP.
func (c *fakeCluster) connection(t *testing.T) *kubeconn.Connection {
	t.Helper()
	conn, err := kubeconn.NewConnection(&rest.Config{Host: c.baseURL.String()})
	require.NoError(t, err)
	conn.HTTPClient, conn.Dynamic = c.client, boundDynamic{c.dyn}
	return conn
}

// boundDynamic makes the in-memory dynamic client keep the contract a real one has: a watch is
// served over its request's context and dies with it. Without this a fake watch outlives the
// context it was opened on, and a stream opened on a context that ends too early reads as a
// standing one.
type boundDynamic struct{ dynamic.Interface }

func (d boundDynamic) Resource(gvr schema.GroupVersionResource) dynamic.NamespaceableResourceInterface {
	return boundNamespaceable{d.Interface.Resource(gvr)}
}

type boundNamespaceable struct {
	dynamic.NamespaceableResourceInterface
}

func (n boundNamespaceable) Namespace(ns string) dynamic.ResourceInterface {
	return boundResource{n.NamespaceableResourceInterface.Namespace(ns)}
}

func (n boundNamespaceable) Watch(ctx context.Context, opts metav1.ListOptions) (watch.Interface, error) {
	return watchBoundTo(ctx, n.NamespaceableResourceInterface, opts)
}

type boundResource struct{ dynamic.ResourceInterface }

func (r boundResource) Watch(ctx context.Context, opts metav1.ListOptions) (watch.Interface, error) {
	return watchBoundTo(ctx, r.ResourceInterface, opts)
}

func watchBoundTo(ctx context.Context, ri dynamic.ResourceInterface, opts metav1.ListOptions) (watch.Interface, error) {
	w, err := ri.Watch(ctx, opts)
	if err != nil {
		return nil, err
	}
	go func() {
		<-ctx.Done()
		w.Stop()
	}()
	return w, nil
}

// serve declares one group-version's resources, registering the group with /apis when it is
// not the core one.
func (c *fakeCluster) serve(groupVersion string, rs ...metav1.APIResource) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.resources[groupVersion] = &metav1.APIResourceList{GroupVersion: groupVersion, APIResources: rs}
	if group, _, found := strings.Cut(groupVersion, "/"); found {
		c.ensureGroupLocked(group, groupVersion)
	}
}

// group declares a group serving several versions, the first of which is preferred. Call it
// before serving them, since serve only fills in a version the group does not name.
func (c *fakeCluster) group(name string, groupVersions ...string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.registerLocked(name, groupVersions...)
}

// ensureGroupLocked names a group-version in /apis without disturbing which one the group
// prefers.
func (c *fakeCluster) ensureGroupLocked(name, groupVersion string) {
	i := slices.IndexFunc(c.groups, func(g metav1.APIGroup) bool { return g.Name == name })
	if i < 0 {
		c.registerLocked(name, groupVersion)
		return
	}
	if slices.ContainsFunc(c.groups[i].Versions, func(v metav1.GroupVersionForDiscovery) bool {
		return v.GroupVersion == groupVersion
	}) {
		return
	}
	_, version, _ := strings.Cut(groupVersion, "/")
	c.groups[i].Versions = append(c.groups[i].Versions,
		metav1.GroupVersionForDiscovery{GroupVersion: groupVersion, Version: version})
}

func (c *fakeCluster) registerLocked(name string, groupVersions ...string) {
	versions := make([]metav1.GroupVersionForDiscovery, 0, len(groupVersions))
	for _, gv := range groupVersions {
		_, version, _ := strings.Cut(gv, "/")
		versions = append(versions, metav1.GroupVersionForDiscovery{GroupVersion: gv, Version: version})
	}
	group := metav1.APIGroup{Name: name, Versions: versions, PreferredVersion: versions[0]}
	if i := slices.IndexFunc(c.groups, func(g metav1.APIGroup) bool { return g.Name == name }); i >= 0 {
		c.groups[i] = group
		return
	}
	c.groups = append(c.groups, group)
}

// serveRaw makes one path answer body verbatim, for an api server whose answer is not the
// document the sweep is parsing.
func (c *fakeCluster) serveRaw(path, body string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.raw[path] = body
}

// unpreferGroup blanks a group's preferred version, which is what an api server that names
// none looks like.
func (c *fakeCluster) unpreferGroup(name string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if i := slices.IndexFunc(c.groups, func(g metav1.APIGroup) bool { return g.Name == name }); i >= 0 {
		c.groups[i].PreferredVersion = metav1.GroupVersionForDiscovery{}
	}
}

// breakPath makes one path answer 500 — an aggregated API that is down.
func (c *fakeCluster) breakPath(path string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.broken[path] = true
}

// hold parks every request for path until the returned func lets it go — how a test holds a
// sweep in flight.
func (c *fakeCluster) hold(path string) func() { return c.transport.hold(path) }

// holdTransport parks a chosen path, and reports what it read and what was cancelled under
// it. **In the caller's own goroutine**, not the handler's: a server notices a client giving
// up whenever its connection closes, which is after the run it belongs to has already
// unwound — so a tripwire there cannot say whether a teardown waited.
type holdTransport struct {
	base http.RoundTripper
	// reads carries every path the sweep asked for, fired before the park so a test can wait
	// for the request it means to hold. cancelled carries the held paths the caller gave up
	// on.
	reads     *testutil.Probe[string]
	cancelled *testutil.Probe[string]

	mu       sync.Mutex
	path     string
	released chan struct{}
}

func (t *holdTransport) hold(path string) func() {
	released := make(chan struct{})
	t.mu.Lock()
	t.path, t.released = path, released
	t.mu.Unlock()
	return sync.OnceFunc(func() { close(released) })
}

func (t *holdTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	t.reads.Fire(req.URL.Path)

	t.mu.Lock()
	held, released := t.path == req.URL.Path, t.released
	t.mu.Unlock()
	if held {
		select {
		case <-released:
		case <-req.Context().Done():
			t.cancelled.Fire(req.URL.Path)
			// Latency on purpose: the run stays in flight until the test lets go, so
			// whether a teardown WAITED for it has a determinate answer instead of racing
			// how fast a cancelled request unwinds.
			<-released
			return nil, req.Context().Err()
		}
	}
	return t.base.RoundTrip(req)
}

// crd declares a CustomResourceDefinition serving one group's plural, with no versions —
// enough for IsCRD, which matches without one.
func (c *fakeCluster) crd(group, plural string) {
	c.crdWithVersions(group, plural, nil)
}

// crdWithVersions is crd carrying spec.versions, where additionalPrinterColumns live. Each entry
// is the version name and the columns it declares. []any of map[string]any, not
// []map[string]any: unstructured deep-copies its values and panics on anything else.
func (c *fakeCluster) crdWithVersions(group, plural string, versions []any) {
	spec := map[string]any{
		"group": group,
		"names": map[string]any{"plural": plural},
	}
	if versions != nil {
		spec["versions"] = versions
	}
	obj := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "apiextensions.k8s.io/v1",
		"kind":       "CustomResourceDefinition",
		"metadata":   map[string]any{"name": plural + "." + group},
		"spec":       spec,
	}}
	_, err := c.dyn.Resource(crdGVR).Create(context.Background(), obj, metav1.CreateOptions{})
	if err != nil {
		panic(err)
	}
}

// prependList puts a LIST reactor at the front of the chain. The fake's PrependReactor
// takes no lock while Invokes reads the chain under one, so a reactor added to a running
// sync takes the lock here.
func (c *fakeCluster) prependList(resource string, reaction clienttesting.ReactionFunc) {
	c.dyn.Lock()
	defer c.dyn.Unlock()
	c.dyn.PrependReactor("list", resource, reaction)
}

// forbidCRDs refuses the CRD list, the read RBAC commonly denies.
func (c *fakeCluster) forbidCRDs() {
	c.prependList("customresourcedefinitions",
		func(clienttesting.Action) (bool, runtime.Object, error) {
			return true, nil, apierrors.NewForbidden(crdGVR.GroupResource(), "", nil)
		})
}

// awaitRead blocks until the sweep reads path, so a test asserting on a second sweep knows
// one happened.
func (c *fakeCluster) awaitRead(t *testing.T, path string) {
	t.Helper()
	for {
		if got := c.reads.Await(t, "a sweep reading "+path); got == path {
			return
		}
	}
}

// noRead is the negative assertion: nothing swept in the window. It has no event to wait
// for, so it needs a window of its own rather than the failsafe.
func (c *fakeCluster) noRead(t *testing.T, what string) {
	t.Helper()
	testutil.NoRecv(t, c.reads.Chan(), quietWindow, what)
}

func (c *fakeCluster) serveHTTP(w http.ResponseWriter, r *http.Request) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.broken[r.URL.Path] {
		http.Error(w, "the aggregated api server is down", http.StatusInternalServerError)
		return
	}
	if body, ok := c.raw[r.URL.Path]; ok {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, body)
		return
	}

	switch {
	case r.URL.Path == "/api":
		writeJSON(w, metav1.APIVersions{Versions: []string{"v1"}})
	case r.URL.Path == "/apis":
		writeJSON(w, metav1.APIGroupList{Groups: c.groups})
	default:
		gv := strings.TrimPrefix(strings.TrimPrefix(r.URL.Path, "/apis/"), "/api/")
		doc, ok := c.resources[gv]
		if !ok {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		writeJSON(w, doc)
	}
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

// newFakeDynamic is the client every LIST and WATCH goes through. The list kinds are supplied
// explicitly because the scheme carries no types at all, so every collection a test mirrors is
// named here.
func newFakeDynamic() *dynamicfake.FakeDynamicClient {
	return dynamicfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(),
		map[schema.GroupVersionResource]string{
			crdGVR:                                                  "CustomResourceDefinitionList",
			{Version: "v1", Resource: "pods"}:                       "PodList",
			{Version: "v1", Resource: "events"}:                     "EventList",
			{Version: "v1", Resource: "services"}:                   "ServiceList",
			{Group: "apps", Version: "v1", Resource: "deployments"}: "DeploymentList",
		})
}

// serveKind declares a kind in the discovery documents. A kind sync test needs it because the
// objects read joins through kind_catalog, which the sweep is the only writer of — and in
// production a kind is tracked only because a sweep found it.
func (c *fakeCluster) serveKind(k kubestore.Kind, namespaced bool) {
	c.serve(k.APIVersion, listable(k.Kind, k.Resource, namespaced))
}

// object is one mirrored body: the metadata the store keys on, and nothing else.
func object(apiVersion, kind, name, resourceVersion string) *unstructured.Unstructured {
	return &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": apiVersion,
		"kind":       kind,
		"metadata": map[string]any{
			"name": name, "namespace": "default",
			"uid": "uid-" + name, "resourceVersion": resourceVersion,
		},
	}}
}

// hasObjects makes a collection answer a LIST with these bodies, at listRV. Every call fires
// listed, so a test can tell a relist from a resume.
func (c *fakeCluster) hasObjects(k kubestore.Kind, listRV string, objects ...*unstructured.Unstructured) {
	c.prependList(k.Resource, func(clienttesting.Action) (bool, runtime.Object, error) {
		c.listed.Fire(k.Resource)
		list := &unstructured.UnstructuredList{Object: map[string]any{
			"apiVersion": k.APIVersion, "kind": k.Kind + "List",
			"metadata": map[string]any{"resourceVersion": listRV},
		}}
		for _, o := range objects {
			list.Items = append(list.Items, *o)
		}
		return true, list, nil
	})
}

// failListOnce makes the next LIST of a collection fail and every one after it fall through —
// a transient refusal rather than a standing one.
func (c *fakeCluster) failListOnce(k kubestore.Kind, err error) {
	var once sync.Once
	c.prependList(k.Resource, func(clienttesting.Action) (bool, runtime.Object, error) {
		refused := false
		once.Do(func() { refused = true })
		if refused {
			return true, nil, err
		}
		return false, nil, nil
	})
}

// refuseList makes a collection refuse to be listed. It fires listed too: every attempt is
// observable whatever it answers, which is how a test sees a kind retrying a list it cannot take.
func (c *fakeCluster) refuseList(k kubestore.Kind, err error) {
	c.prependList(k.Resource, func(clienttesting.Action) (bool, runtime.Object, error) {
		c.listed.Fire(k.Resource)
		return true, nil, err
	})
}

// streams hands a test the watch a kind sync opens: one fresh watcher per open, published as
// it is handed over, so a test can drive the stream and see a re-establish for what it is.
type streams struct {
	opened *testutil.Probe[*watch.RaceFreeFakeWatcher]
	// parked fires as a held open starts waiting, so a test can act while the run is on it.
	parked *testutil.Probe[struct{}]

	// Guarded, because refuse, hold and stopNext are answers a test changes while a stream is
	// already running — the reactor below reads them from whichever goroutine is opening a watch.
	mu      sync.Mutex
	err     error
	held    chan struct{}
	stopped bool
	leading []watch.Event
}

func (s *streams) answer() (chan struct{}, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.held, s.err
}

// takeStop consumes a stopNext. Read after the hold, so an open parked before stopNext was
// called still takes it.
func (s *streams) takeStop() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	stopped := s.stopped
	s.stopped = false
	return stopped
}

// takeLeading consumes a startWith.
func (s *streams) takeLeading() []watch.Event {
	s.mu.Lock()
	defer s.mu.Unlock()
	leading := s.leading
	s.leading = nil
	return leading
}

// streamKind installs the watch for one collection. Call before the kind is tracked.
func (c *fakeCluster) streamKind(k kubestore.Kind) *streams {
	s := &streams{opened: testutil.NewProbe[*watch.RaceFreeFakeWatcher](8), parked: testutil.NewProbe[struct{}](8)}
	c.dyn.PrependWatchReactor(k.Resource, func(clienttesting.Action) (bool, watch.Interface, error) {
		held, err := s.answer()
		if err != nil {
			return true, nil, err
		}
		if held != nil {
			s.parked.Fire(struct{}{})
			<-held
		}
		w := watch.NewRaceFreeFake()
		if s.takeStop() {
			w.Stop()
		}
		for _, e := range s.takeLeading() {
			w.Action(e.Type, e.Object)
		}
		s.opened.Fire(w)
		return true, w, nil
	})
	return s
}

// refuse makes every later open fail, which is what a server refusing a watch looks like.
func (s *streams) refuse(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.err = err
}

// stopNext answers the next open with a watcher already stopped — a stream that closes before
// its first frame, whatever the run's clock does meanwhile.
func (s *streams) stopNext() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stopped = true
}

// startWith queues events on the next watcher before it is handed over, so the run reads them
// ahead of anything its own timers do.
func (s *streams) startWith(events ...watch.Event) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.leading = events
}

// hold parks every later open until the returned func lets it go — a server slow to
// establish a watch rather than one refusing it.
func (s *streams) hold() func() {
	released := make(chan struct{})
	s.mu.Lock()
	s.held = released
	s.mu.Unlock()
	return sync.OnceFunc(func() { close(released) })
}

// holdList parks a collection's LIST until the test lets it go, for asserting on what a
// kind reports while it has nothing yet.
func (c *fakeCluster) holdList(k kubestore.Kind) *heldList {
	h := &heldList{released: make(chan struct{})}
	h.release = sync.OnceFunc(func() { close(h.released) })
	c.prependList(k.Resource, func(clienttesting.Action) (bool, runtime.Object, error) {
		<-h.released
		return false, nil, nil
	})
	return h
}

type heldList struct {
	released chan struct{}
	release  func()
}

// event is one core event body, which the cache keeps in its own table.
func event(name, resourceVersion string) *unstructured.Unstructured {
	return object("v1", "Event", name, resourceVersion)
}

// --- Reading a kind back ---

// newSyncingService is a service running the real kind worker, paced so a test never
// outwaits a production number. Every duration here is small enough to be reached and long enough that a
// loaded machine does not trip it.
func newSyncingService(t *testing.T, cluster *fakeCluster, opts ...func(*pacing)) *Service {
	t.Helper()
	svc, pool := newTestService(t, withPacing(syncPacing(opts...)), withRealKindSync())
	pool.lease("prod").connect(t, cluster, "uid-1")
	start(t, svc)
	return svc
}

// syncPacing is what a test paces a real kind sync by. Separate from the service it builds, so a
// test that stands a second one up over the same cache directory paces both alike.
func syncPacing(opts ...func(*pacing)) pacing {
	p := defaultPacing()
	// Long enough that only a test asking for it reopens a quiet watch: every other one is
	// counting opens or observing a verdict the reopen would overwrite under load.
	p.staleAfter = time.Minute
	// Wide enough that a run which is DOWN is observable: the ladder is what holds a failed
	// verdict up, and a rung shorter than a loaded machine's scheduling noise would let the
	// re-establish overwrite it before a reader could see it.
	p.backoff = supervisor.Backoff{Base: 200 * time.Millisecond, Factor: 2, Cap: time.Second}
	for _, opt := range opts {
		opt(&p)
	}

	return p
}

// syncKind arms a cache and one kind on it, in the order production reaches them: a kind is
// tracked because a sweep found it, and the objects read joins through the catalog that sweep
// writes.
func syncKind(t *testing.T, svc *Service, cacheID int64, k kubestore.Kind) {
	t.Helper()
	svc.TrackDiscovery(cacheID, testParams)
	awaitDiscovered(t, svc, cacheID)
	svc.TrackKind(cacheID, k)
}

// awaitKindReason waits for one kind's verdict to settle somewhere, owning its subscription
// so nothing published before it attached is missed.
func awaitKindReason(t *testing.T, svc *Service, cacheID int64, k kubestore.Kind, reason string) {
	t.Helper()
	awaitKindState(t, svc, cacheID, k, "a kind settling on "+reason, func(state KindState) bool {
		return state.Reason == reason
	})
}

// awaitKindOpen waits for one kind's watch to be open and returns the state that said so.
// Watching and Resuming both say it: an idle watch is reopened one window after Watching, a
// reopen held a window more reads Resuming, and a test that shrank the window cannot count on
// reading Watching before it does.
func awaitKindOpen(t *testing.T, svc *Service, cacheID int64, k kubestore.Kind) KindState {
	t.Helper()
	return awaitKindState(t, svc, cacheID, k, "a kind's watch to be open", func(state KindState) bool {
		return state.Reason == ReasonWatching || state.Reason == ReasonResuming
	})
}

// awaitKindState waits for one kind's answer to match and returns the state that matched.
// Everything a test asserts about a verdict belongs in match, or in the state handed back,
// rather than in a read after it: a run that is down is a state the next attempt replaces, so
// a later read is a different answer rather than the same one again.
func awaitKindState(t *testing.T, svc *Service, cacheID int64, k kubestore.Kind, what string, match func(KindState) bool) KindState {
	t.Helper()
	news := svc.ListenForKindChanges()
	defer news.Close()

	var last KindState
	settled := assert.Eventually(t, func() bool {
		state, ok := svc.GetKindState(cacheID, k)
		last = state
		return ok && match(state)
	}, testutil.Timeout, time.Millisecond, what)
	if !settled {
		t.Fatalf("the kind stands on %q: %s", last.Reason, last.Message)
	}
	return last
}

func objectNames(t *testing.T, svc *Service, cacheID int64, k kubestore.Kind) []string {
	t.Helper()
	store, ok, err := svc.storeMgr.(*kubestore.Manager).OpenExisting(cacheID)
	require.NoError(t, err)
	require.True(t, ok, "the cache is open")
	defer store.Release()

	rows, _, err := store.ObjectsWithCursor(t.Context(), k.APIVersion, k.Resource)
	require.NoError(t, err)
	names := make([]string, 0, len(rows))
	for _, r := range rows {
		names = append(names, r.Name)
	}
	return names
}

func eventsOf(t *testing.T, svc *Service, cacheID int64) []kubestore.EventRow {
	t.Helper()
	store, ok, err := svc.storeMgr.(*kubestore.Manager).OpenExisting(cacheID)
	require.NoError(t, err)
	require.True(t, ok, "the cache is open")
	defer store.Release()

	rows, _, err := store.EventsWithCursor(t.Context())
	require.NoError(t, err)
	return rows
}

func cookieOf(t *testing.T, svc *Service, cacheID int64, k kubestore.Kind) (string, bool) {
	t.Helper()
	store, ok, err := svc.storeMgr.(*kubestore.Manager).OpenExisting(cacheID)
	require.NoError(t, err)
	require.True(t, ok, "the cache is open")
	defer store.Release()

	cookie, held, err := store.Cookie(t.Context(), k.APIVersion, k.Resource)
	require.NoError(t, err)
	return cookie, held
}

// listable is a resource a kind sync can mirror, which is what most of a catalog looks like.
func listable(kind, plural string, namespaced bool) metav1.APIResource {
	return metav1.APIResource{
		Kind: kind, Name: plural, Namespaced: namespaced,
		Verbs: metav1.Verbs{"get", "list", "watch"},
	}
}

// --- The service ---

// newTestService builds a Service over a fresh pool and a store directory under t, with a
// kind sync that parks until its run ends — the shape a real one has, so nothing re-enters
// and a test drives arming rather than sync. Nothing sweeps until start.
func newTestService(t *testing.T, opts ...option) (*Service, *fakePool) {
	t.Helper()
	mgr := kubestore.NewManager(t.TempDir(), kubestore.Retention{})
	t.Cleanup(func() { _ = mgr.Close() })
	return newTestServiceOverStore(t, mgr, opts...)
}

// newTestServiceOverStore is newTestService for a test about what the store answers
// rather than about what is in it.
func newTestServiceOverStore(t *testing.T, storeMgr storeManager, opts ...option) (*Service, *fakePool) {
	t.Helper()
	pool := newFakePool()

	base := []option{newFakeKindSync().option()}
	svc := New(pool, storeMgr, append(base, opts...)...)
	t.Cleanup(func() { _ = svc.Close() })
	return svc, pool
}

// healingStore fails every open until heal, then serves the real manager — a cache whose
// disk filled up and was cleared out.
type healingStore struct {
	mgr    *kubestore.Manager
	healed atomic.Bool
}

func newHealingStore(t *testing.T) *healingStore {
	t.Helper()
	mgr := kubestore.NewManager(t.TempDir(), kubestore.Retention{})
	t.Cleanup(func() { _ = mgr.Close() })
	return &healingStore{mgr: mgr}
}

func (h *healingStore) OpenOrCreate(cacheID int64) (*kubestore.Store, error) {
	if !h.healed.Load() {
		return nil, errors.New("disk full")
	}
	return h.mgr.OpenOrCreate(cacheID)
}

// start runs the service and stops it with the test. Registered after newTestService's
// Close, so the cleanups unwind in the order the lifecycle requires.
func start(t *testing.T, svc *Service) {
	t.Helper()
	stop, err := svc.Start(t.Context())
	require.NoError(t, err)
	t.Cleanup(func() { _ = stop(context.Background()) })
}

// awaitDiscovered settles on a sweep that answered; awaitReason on whichever verdict a test
// is waiting for. News is not a status, so both answer it the way a consumer does: re-read.
//
// Each subscribes for itself and gives the subscription back. The state is published before
// the news the settling belongs to, so a test that goes on to assert on news follows these
// with awaitNewsQuiet rather than subscribing on the reason alone.
func awaitDiscovered(t *testing.T, svc *Service, cacheID int64) {
	t.Helper()
	awaitReason(t, svc, cacheID, ReasonDiscovered)
}

func awaitReason(t *testing.T, svc *Service, cacheID int64, reason string) {
	t.Helper()
	news := svc.ListenForDiscoveryChanges()
	defer news.Close()
	for {
		if state, ok := svc.GetDiscoveryState(cacheID); ok && state.Reason == reason {
			return
		}
		testutil.Recv(t, news.Chan(), "a sweep settling on "+reason)
	}
}

// awaitNewsQuiet waits until the discovery feed has stopped, which is the point a test can
// open a subscription carrying only what it goes on to cause. A sweep publishes its state
// before its news, so a subscription opened the moment the reason lands can still catch the
// frame that settling published.
//
// The window is bounded because quiet has no event to wait for; each frame restarts it, so a
// cache that never settles fails on the deadline rather than mid-burst.
func awaitNewsQuiet(t *testing.T, svc *Service) {
	t.Helper()
	news := svc.ListenForDiscoveryChanges()
	defer news.Close()
	deadline := time.After(testutil.Timeout)
	for {
		select {
		case <-news.Chan():
		case <-time.After(quietWindow):
			return
		case <-deadline:
			t.Fatal("timed out waiting for the discovery feed to go quiet")
		}
	}
}

// catalogOf reads what the sweep wrote, through the door every reader uses. The manager is
// reached off the service because a test owns both ends of this seam.
func catalogOf(t *testing.T, svc *Service, cacheID int64) []kubestore.KindRow {
	t.Helper()
	store, ok, err := svc.storeMgr.(*kubestore.Manager).OpenExisting(cacheID)
	require.NoError(t, err)
	if !ok {
		return nil
	}
	defer store.Release()

	rows, err := store.Kinds(t.Context())
	require.NoError(t, err)
	return rows
}

// awaitDialsQuiet waits until this lease has stopped being dialed, which is the point a test
// can count what happens next. Reading "no run in flight" is not enough — a run the supervisor has
// queued has not started, and dials the moment it does.
//
// The window is bounded because quiet has no event to wait for; each dial restarts it, so a
// cache that never settles fails on the deadline rather than measuring mid-burst.
func awaitDialsQuiet(t *testing.T, l *fakeLease) {
	t.Helper()
	deadline := time.After(testutil.Timeout)
	for {
		l.dialed.Drain()
		select {
		case <-l.dialed.Chan():
		case <-time.After(quietWindow):
			return
		case <-deadline:
			t.Fatal("timed out waiting for the cache to stop dialing")
		}
	}
}

// writeRow puts one object of a kind into the cache, which is how a test rings the store's
// change bus — the same ping a kind sync's own write leaves behind.
func writeRow(t *testing.T, svc *Service, cacheID int64, k kubestore.Kind) {
	t.Helper()
	store, ok, err := svc.storeMgr.(*kubestore.Manager).OpenExisting(cacheID)
	require.NoError(t, err)
	require.True(t, ok, "the cache is open")
	defer store.Release()

	obj := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": k.APIVersion,
		"kind":       k.Kind,
		"metadata": map[string]any{
			"name": "one", "uid": "uid-one", "resourceVersion": "1",
		},
	}}
	require.NoError(t, store.ApplyChange(t.Context(), k, watch.Added, obj))
}

// quietWindow bounds a negative assertion — "no news arrived" — which has no event to
// wait for and so needs a window of its own rather than the failsafe every positive wait
// uses. Short: what it is watching for would already have happened.
const quietWindow = 50 * time.Millisecond

// testKind is one kind's identity, spelled the way the store writes rows by.
func testKind(apiVersion, kind, resource string) kubestore.Kind {
	return kubestore.Kind{APIVersion: apiVersion, Kind: kind, Resource: resource}
}

// --- Substituting the kind worker ---

// reasonWithdrawnWrite is what a run publishes on its way out after being withdrawn. Nothing
// production says it, so a reader seeing it has been told something by a run that was cancelled.
const reasonWithdrawnWrite = "WithdrawnRunWrote"

// admittedRun is what a substituted worker reports about one run: the kind it was admitted with,
// and the way to publish an answer for it.
type admittedRun struct {
	Kind kubestore.Kind
	// Report publishes for this run. The run answers Watching of its own just after firing
	// this, so a caller writing a different reason must wait on established first — writing
	// on the run alone leaves the two answers racing, and the later one is what a read sees.
	Report func(reason string)
}

// fakeKindSync stands in for the real kind worker, for a test about arming rather than about what
// a kind sync reads. It takes the same admission and the same gate, and then parks until
// something stops it — the real shape, minus the Kubernetes.
type fakeKindSync struct {
	s    *Service
	runs *testutil.Probe[admittedRun]
	// returned fires as a run finishes unwinding, so a caller that joined it has seen it;
	// established fires once one is up and reporting, which is what a caller acting on a live
	// stream — a restart — has to wait for.
	returned    *testutil.Probe[struct{}]
	established *testutil.Probe[struct{}]
	// reportOnExit makes a run publish on its way down, which is what a real one does off its
	// stale timer or a last delta — and what a test about a withdrawn generation's report needs
	// something to observe.
	reportOnExit bool
	// exiting fires as a parked run is cancelled, before it unwinds; exitDelay is how long the
	// unwinding takes. **Latency on purpose**: a run that vanished the instant it was cancelled
	// would give a join-versus-no-join race no determinate answer, since there would be nothing
	// left to overlap with. The assertions below never wait on it.
	exiting   *testutil.Probe[struct{}]
	exitDelay time.Duration

	// gateHold parks a run between a refused gate and its return, and gateRefused says it has
	// got there. That span is the window the supervisor's snapshot cannot see, held open on
	// purpose. Nil unless a test asks for it.
	gateHold    chan struct{}
	gateRefused *testutil.Probe[struct{}]

	// admit, when set, holds each run past its gate until a send on it, so a test can look at
	// a kind whose run has started and not yet answered. Nil unless a test asks for it.
	admit chan struct{}

	// Every run gets a generation, and liveGen holds the generations still able to write for
	// each subject. Two of them live at once is the bug this detects: both write the same
	// collection, and a rename gives them different singulars to key rows by.
	mu         sync.Mutex
	nextGen    int
	liveGen    map[string]map[int]bool
	overlapped bool
}

func newFakeKindSync() *fakeKindSync {
	return &fakeKindSync{
		runs:        testutil.NewProbe[admittedRun](8),
		returned:    testutil.NewProbe[struct{}](8),
		exiting:     testutil.NewProbe[struct{}](8),
		established: testutil.NewProbe[struct{}](8),
		liveGen:     map[string]map[int]bool{},
		// Every substitute unwinds slowly enough to be caught mid-flight: a run that vanished
		// the instant it was cancelled would make an overlap unobservable rather than absent.
		exitDelay: 50 * time.Millisecond,
	}
}

// newHeldAtGateKindSync parks every refused run at the gate until releaseGate, which is the one
// way to observe a run that has decided to suspend and not yet said so.
func newHeldAtGateKindSync() *fakeKindSync {
	f := newFakeKindSync()
	f.gateHold, f.gateRefused = make(chan struct{}), testutil.NewProbe[struct{}](8)
	return f
}

func (f *fakeKindSync) releaseGate() { close(f.gateHold) }

// newParkingKindSync unwinds slowly enough that a test can tell a join from no join.
func newParkingKindSync() *fakeKindSync {
	f := newFakeKindSync()
	f.exitDelay = 200 * time.Millisecond
	return f
}

// withRealKindSync puts the production worker back, for a test about what a kind sync reads
// rather than about arming.
func withRealKindSync() option {
	return withKindSync(func(s *Service) supervisor.Worker[Reason] {
		return kindSync{s: s, pacing: s.pacing}
	})
}

// option installs this substitute, binding it to the Service it runs under.
func (f *fakeKindSync) option() option {
	return withKindSync(func(s *Service) supervisor.Worker[Reason] {
		f.s = s
		return f
	})
}

func (f *fakeKindSync) Run(ctx context.Context, pass *supervisor.WorkerPass[Reason]) supervisor.Result {
	cacheID, id, ok := parseKindSubject(pass.Subject())
	if !ok {
		return supervisor.Skip()
	}
	sess, k, ok := f.s.enterKindRun(cacheID, id)
	if !ok {
		return supervisor.Skip()
	}
	defer sess.leaveRun()

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	defer context.AfterFunc(sess.ctx, cancel)()

	gen := f.enter(pass.Subject())
	defer f.leave(pass.Subject(), gen)

	if _, err := sess.connFor(ctx, kindGateKey(id), ReasonSyncFailed); err != nil {
		if f.gateHold != nil {
			f.gateRefused.Fire(struct{}{})
			// The ctx arm keeps a test that never releases from wedging the teardown that
			// joins this run.
			select {
			case <-f.gateHold:
			case <-ctx.Done():
			}
		}
		return supervisor.Suspend(connectionReason(err, ReasonSyncFailed), err.Error())
	}
	if f.admit != nil {
		select {
		case <-f.admit:
		case <-ctx.Done():
			return supervisor.Skip()
		}
	}
	report := func(reason string) { pass.Commit(Reason(reason)) }
	f.runs.Fire(admittedRun{Kind: k, Report: report})

	// Up and streaming, which is what a real one says on its first frame.
	report(ReasonWatching)
	pass.Ready()
	f.established.Fire(struct{}{})

	<-ctx.Done()
	f.exiting.Fire(struct{}{})
	// Latency on purpose, as above: a run that vanished the instant it was cancelled would
	// leave a join-versus-no-join race nothing to overlap with.
	time.Sleep(f.exitDelay)
	if f.reportOnExit {
		// The withdrawn generation's last act. Nothing must serve it: this run belongs to a
		// registration that has gone, whatever has been registered since.
		report(reasonWithdrawnWrite)
	}
	f.returned.Fire(struct{}{})
	return supervisor.Skip()
}

// enter opens a generation for subject and reports it. Anything already live for that subject
// is a generation that should have been gone.
func (f *fakeKindSync) enter(subject string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.nextGen++
	gen := f.nextGen
	live, ok := f.liveGen[subject]
	if !ok {
		live = map[int]bool{}
		f.liveGen[subject] = live
	}
	if len(live) > 0 {
		f.overlapped = true
	}
	live[gen] = true
	return gen
}

func (f *fakeKindSync) leave(subject string, gen int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.liveGen[subject], gen)
}

// sawOverlap reports whether two generations for one subject were ever able to write at once.
func (f *fakeKindSync) sawOverlap() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.overlapped
}

// liveRuns is how many generations across every subject can still write. Zero is what a clear
// needs of the cache it is about to swap the file under.
func (f *fakeKindSync) liveRuns() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, live := range f.liveGen {
		n += len(live)
	}
	return n
}
