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
	"cmp"
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/amorey/beehive"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/tools/clientcmd/api"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/appdb"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/testutil"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/cluster/internal/kubeconn"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/kubeconfig"
)

// clusterObj builds a Cluster object whose probed UID is uid, or one that has never
// been probed when uid is "".
func clusterObj(uid string) *beehive.Object[ClusterRuntimeSpec, ClusterStatus] {
	obj := &beehive.Object[ClusterRuntimeSpec, ClusterStatus]{Status: &ClusterStatus{}}
	if uid != "" {
		obj.Status.Server.UID = &uid
	}
	return obj
}

func TestClusterActiveUID(t *testing.T) {
	assert.Equal(t, "uid-1", ClusterActiveUID(clusterObj("uid-1")))
	assert.Empty(t, ClusterActiveUID(clusterObj("")), "probed, but no UID yet")
	assert.Empty(t, ClusterActiveUID(&beehive.Object[ClusterRuntimeSpec, ClusterStatus]{}),
		"beehive leaves Status nil until first written")
}

// The single definition of "active cache", read by both the cache controller's sync
// gate and the service's join. The unprobed case is the one that matters: an
// unknown identity must match nothing, or a disconnected cluster would sync every
// cache it has ever owned.
func TestCacheIsActive(t *testing.T) {
	assert.True(t, CacheIsActive(clusterObj("uid-1"), "uid-1"))
	assert.False(t, CacheIsActive(clusterObj("uid-1"), "uid-2"), "a superseded identity")
	assert.False(t, CacheIsActive(clusterObj(""), ""), "unknown identity matches nothing — not even an empty UID")
	assert.False(t, CacheIsActive(clusterObj(""), "uid-1"))
}

// --- kubeconfigObservation / Reconcile ---

// kubeconfigObj builds a kubeconfig-sourced record for contextName, carrying status
// if one was already observed.
func kubeconfigObj(contextName string, status *ClusterStatus) *beehive.Object[ClusterRuntimeSpec, ClusterStatus] {
	return &beehive.Object[ClusterRuntimeSpec, ClusterStatus]{
		ID:     1,
		Name:   "row-1",
		Spec:   ClusterRuntimeSpec{Source: SourceKubeconfig, SourceKey: &contextName},
		Status: status,
	}
}

func TestObserveKubeconfigRecordsAPresentContext(t *testing.T) {
	observed := observeKubeconfig(cfgCurrent("prod", "prod", "staging"), "prod", nil)

	require.NotNil(t, observed)
	assert.Equal(t, ClusterStatusSourceKubeconfig{
		Cluster: ClusterStatusSourceKubeconfigCluster{
			Name:  "prod-cluster",
			Entry: &ClusterStatusSourceKubeconfigClusterEntry{Server: "https://prod.example:6443"},
		},
		User:      ClusterStatusSourceKubeconfigUser{Name: "prod-user"},
		IsPresent: true, IsDefault: true,
	}, *observed)
}

func TestObserveKubeconfigMarksANonCurrentContext(t *testing.T) {
	observed := observeKubeconfig(cfgCurrent("prod", "prod", "staging"), "staging", nil)

	require.NotNil(t, observed)
	assert.False(t, observed.IsDefault)
	assert.True(t, observed.IsPresent)
}

// A departed context keeps its last-known names: an orphaned record has to stay
// identifiable in a list, and blanking it would leave the row nameless.
func TestObserveKubeconfigKeepsLastKnownNamesWhenAbsent(t *testing.T) {
	prev := &ClusterStatusSourceKubeconfig{
		Cluster:   ClusterStatusSourceKubeconfigCluster{Name: "prod-cluster"},
		User:      ClusterStatusSourceKubeconfigUser{Name: "prod-user"},
		IsPresent: true, IsDefault: true,
	}

	observed := observeKubeconfig(cfgCurrent("staging", "staging"), "prod", prev)

	require.NotNil(t, observed)
	assert.Equal(t, ClusterStatusSourceKubeconfig{
		Cluster: ClusterStatusSourceKubeconfigCluster{Name: "prod-cluster"},
		User:    ClusterStatusSourceKubeconfigUser{Name: "prod-user"},
	}, *observed)
	assert.True(t, prev.IsPresent, "the previous observation is the caller's, not this fold's to clear")
}

// A never-present context has nothing to keep, and must not read as present.
func TestObserveKubeconfigMarksAnUnseenContextAbsent(t *testing.T) {
	observed := observeKubeconfig(cfgCurrent("staging", "staging"), "prod", nil)

	require.NotNil(t, observed)
	assert.False(t, observed.IsPresent)
	assert.Empty(t, observed.Cluster.Name)
}

// The entry the context names, mirrored as the file states it — no verdict is drawn
// here, so a reader of the block sees the same two facts the file does.
func TestObserveKubeconfigMirrorsTheClusterEntry(t *testing.T) {
	cfg := cfgCurrent("prod", "prod")
	cfg.Clusters["prod-cluster"].InsecureSkipTLSVerify = true

	observed := observeKubeconfig(cfg, "prod", nil)

	require.NotNil(t, observed)
	assert.Equal(t, &ClusterStatusSourceKubeconfigClusterEntry{
		Server: "https://prod.example:6443", InsecureSkipTLSVerify: true,
	}, observed.Cluster.Entry)
}

// Whether a CA or a client certificate is named decides the scheme client-go
// defaults a schemeless server to, so their presence is mirrored — presence alone,
// never the bytes or the path.
func TestObserveKubeconfigMirrorsWhetherCertificatesAreNamed(t *testing.T) {
	cfg := cfgCurrent("prod", "prod")
	cfg.Clusters["prod-cluster"].CertificateAuthorityData = []byte("ca")
	cfg.AuthInfos = map[string]*api.AuthInfo{"prod-user": {ClientCertificate: "/home/me/.kube/prod.crt"}}

	observed := observeKubeconfig(cfg, "prod", nil)

	require.NotNil(t, observed)
	assert.True(t, observed.Cluster.Entry.HasCertificateAuthority)
	assert.True(t, observed.User.HasClientCertificate)

	cfg.Clusters["prod-cluster"].CertificateAuthorityData = nil
	cfg.Clusters["prod-cluster"].CertificateAuthority = "/etc/ca.crt"
	cfg.AuthInfos["prod-user"].ClientCertificate = ""
	cfg.AuthInfos["prod-user"].ClientCertificateData = []byte("cert")
	observed = observeKubeconfig(cfg, "prod", nil)
	assert.True(t, observed.Cluster.Entry.HasCertificateAuthority, "a path counts like inline data")
	assert.True(t, observed.User.HasClientCertificate)

	delete(cfg.AuthInfos, "prod-user")
	observed = observeKubeconfig(cfg, "prod", nil)
	assert.False(t, observed.User.HasClientCertificate, "a user the file never defines names nothing")
}

// A context can name a cluster entry the file never defines. Nil says so, which is
// what keeps "unresolvable" distinguishable from "resolved and verified".
func TestObserveKubeconfigLeavesTheClusterEntryNilWhenUndefined(t *testing.T) {
	cfg := cfgCurrent("prod", "prod")
	delete(cfg.Clusters, "prod-cluster")

	observed := observeKubeconfig(cfg, "prod", nil)

	require.NotNil(t, observed)
	assert.Nil(t, observed.Cluster.Entry)
}

// The entry outlives the context, like the rest of the cached block: an orphaned
// record that was reached insecurely must not read as secure.
func TestObserveKubeconfigKeepsTheClusterEntryWhenAbsent(t *testing.T) {
	prev := &ClusterStatusSourceKubeconfig{
		Cluster: ClusterStatusSourceKubeconfigCluster{
			Name:  "prod-cluster",
			Entry: &ClusterStatusSourceKubeconfigClusterEntry{Server: "http://localhost:8080"},
		},
	}

	observed := observeKubeconfig(cfgCurrent("staging", "staging"), "prod", prev)

	require.NotNil(t, observed)
	require.NotNil(t, observed.Cluster.Entry)
	assert.Equal(t, "http://localhost:8080", observed.Cluster.Entry.Server)
}

// Another source's record is not this observation's to write — its own variant is,
// and overwriting would claim a kube-context it never referenced.
func TestObserveKubeconfigLeavesAnotherSourceAlone(t *testing.T) {
	assert.Nil(t, observeKubeconfig(cfgCurrent("prod", "prod"), "", nil))
}

// stubControllerClient captures what a reconcile writes. The embedded interface is
// nil: Reconcile calls nothing else on it.
type stubControllerClient struct {
	beehive.ControllerClient[ClusterStatus]
	updated    *ClusterStatus
	updateErr  error
	conditions []Condition
	setErr     error
	dependsOn  []beehive.ObjectID
	dependErr  error
	events     []beehive.EventSpec
	eventErr   error
}

func (c *stubControllerClient) AddEvent(_ context.Context, event beehive.EventSpec) error {
	c.events = append(c.events, event)
	return c.eventErr
}

func (c *stubControllerClient) UpdateStatus(_ context.Context, status ClusterStatus) error {
	c.updated = &status
	return c.updateErr
}

func (c *stubControllerClient) SetCondition(_ context.Context, cond Condition) error {
	c.conditions = append(c.conditions, cond)
	return c.setErr
}

// Within runs fn inline: the pass groups its writes so a watcher never sees half of
// them, and what this stub records is that each was attempted.
func (c *stubControllerClient) Within(ctx context.Context, fn func(context.Context) error) error {
	return fn(ctx)
}

func (c *stubControllerClient) AddDependency(_ context.Context, toID beehive.ObjectID) error {
	c.dependsOn = append(c.dependsOn, toID)
	return c.dependErr
}

// reconcileCluster runs one cluster pass and requires it settled, which every pass
// over a read kubeconfig does.
func reconcileCluster(t *testing.T, c *clusterController, client beehive.ControllerClient[ClusterStatus], obj *beehive.Object[ClusterRuntimeSpec, ClusterStatus]) {
	t.Helper()
	require.Equal(t, beehive.Settled(), c.Reconcile(context.Background(), client, obj))
}

// controllerOver returns a controller over an empty kubeconfig, so every context is
// absent. Its clients are promoted from the embedded deps, so a test reading back what
// a reconcile wrote goes through c.clusterClient / c.cacheClient.
func controllerOver(t *testing.T) *clusterController {
	t.Helper()
	return &clusterController{deps: newTestDeps(t)}
}

// --- ClusterCache creation ---

// probedCluster stores a cluster record and hands back the object a reconcile would
// be given, carrying the UID a probe recorded. Status is set in memory: nothing has
// written one yet, which is exactly the state the probe will leave behind.
func probedCluster(t *testing.T, d deps, uid string) *beehive.Object[ClusterRuntimeSpec, ClusterStatus] {
	t.Helper()
	obj := createCluster(t, d, "prod")
	obj.Status = &ClusterStatus{Server: ClusterServer{UID: &uid}}
	return obj
}

// liveCaches returns every cache the reconcile wrote, owner edge loaded.
func liveCaches(t *testing.T, caches beehive.Client[ClusterCacheSpec, ClusterCacheStatus]) []*beehive.Object[ClusterCacheSpec, ClusterCacheStatus] {
	t.Helper()
	objs, err := caches.List(context.Background(), beehive.LoadOwner())
	require.NoError(t, err)
	return objs
}

// --- the identity a probe found ---

// identityControllerOver returns a controller whose pool answers with what.
func identityControllerOver(t *testing.T, what *fakeKubeconn) *clusterController {
	t.Helper()
	d := newTestDeps(t)
	d.kubeconnSvc = what
	return &clusterController{deps: d}
}

// reportedCondition is the Connected verdict a pass wrote — the one most tests here are
// about. conditionOfType reaches the other two.
func reportedCondition(t *testing.T, client *stubControllerClient) Condition {
	t.Helper()
	return conditionOfType(t, client, ConditionConnected)
}

// conditionOfType is the verdict a pass wrote for one aspect, or a failure when it wrote
// none for it.
func conditionOfType(t *testing.T, client *stubControllerClient, ct ConditionType) Condition {
	t.Helper()
	cond := FindCondition(client.conditions, ct)
	require.NotNil(t, cond, "a pass reports %s", ct)
	return *cond
}

// loggedEvent is the verdict a pass recorded on the cluster's own timeline.
func loggedEvent(t *testing.T, client *stubControllerClient) beehive.EventSpec {
	t.Helper()
	require.Len(t, client.events, 1, "a pass records one connection verdict")
	return client.events[0]
}

// --- the connection timeline ---

// The reason a cluster will not connect is the whole point of the timeline: without it
// the UI's attempt list is empty and a user has nothing to report.
func TestReconcileLogsAFailedProbe(t *testing.T) {
	c := identityControllerOver(t, answering(kubeconn.Identity{}, errors.New("connection refused")))
	obj := createCluster(t, c.deps, "prod")

	client := &stubControllerClient{}
	reconcileCluster(t, c, client, obj)

	ev := loggedEvent(t, client)
	assert.Equal(t, ConnectionEventCategory, ev.Category)
	assert.Equal(t, beehive.EventWarning, ev.Type)
	assert.Equal(t, string(kubeconn.ReasonUnreachable), ev.Reason)
	assert.Contains(t, ev.Message, "connection refused")
}

// Each class of failure has its own remedy, so Connected names the probe's class.
func TestReconcileNamesTheProbesClassOfFailure(t *testing.T) {
	refused := kubeconn.Observation[string]{
		Attempts: kubeconn.Attempts{LastAttempt: finished(kubeconn.ReasonTLSInvalid, "x509: unknown authority")},
	}
	c := identityControllerOver(t, knowing(kubeconn.State{Connection: refused}))
	obj := createCluster(t, c.deps, "prod")

	client := &stubControllerClient{}
	reconcileCluster(t, c, client, obj)

	cond := conditionOfType(t, client, ConditionConnected)
	assert.Equal(t, string(kubeconn.ReasonTLSInvalid), cond.Reason)
	assert.Equal(t, "x509: unknown authority", cond.Message)
}

// A failure the probe left unclassified still reads as a failure.
func TestReconcileCallsAnUnclassifiedFailureProbeFailed(t *testing.T) {
	unclassified := kubeconn.Observation[string]{
		Attempts: kubeconn.Attempts{LastAttempt: finished(kubeconn.ReasonUnknown, "boom")},
	}
	c := identityControllerOver(t, knowing(kubeconn.State{Connection: unclassified}))
	obj := createCluster(t, c.deps, "prod")

	client := &stubControllerClient{}
	reconcileCluster(t, c, client, obj)

	assert.Equal(t, ReasonProbeFailed, conditionOfType(t, client, ConditionConnected).Reason)
}

// A working cluster still writes one, so the timeline shows when it came up rather than
// only that it is not down.
func TestReconcileLogsASuccessfulProbe(t *testing.T) {
	c := identityControllerOver(t, answering(kubeconn.Identity{ServerUID: "uid-1"}, nil))
	obj := createCluster(t, c.deps, "prod")

	client := &stubControllerClient{}
	reconcileCluster(t, c, client, obj)

	ev := loggedEvent(t, client)
	assert.Equal(t, beehive.EventNormal, ev.Type)
	assert.Equal(t, ReasonConnected, ev.Reason)
}

// Reaching the server and naming it are separate axes, and a cluster that connects but
// cannot be identified never gets a cache. Reporting Connected's success there would say
// the cluster is fine while nothing syncs.
func TestReconcileLogsTheIdentityFailureWhenTheServerAnswered(t *testing.T) {
	c := identityControllerOver(t, knowing(kubeconn.State{
		Connection: answeredWith("https://prod.example:6443"),
		Readiness:  answeredWith(kubeconn.ComponentStatus{}),
		ServerUID:  forbidden("namespaces \"kube-system\" is forbidden"),
	}))
	obj := createCluster(t, c.deps, "prod")

	client := &stubControllerClient{}
	reconcileCluster(t, c, client, obj)

	ev := loggedEvent(t, client)
	assert.Equal(t, beehive.EventWarning, ev.Type)
	assert.Equal(t, ReasonUIDUnreadable, ev.Reason)
	assert.Contains(t, ev.Message, "forbidden")
}

// Switching a cluster off is a choice, not a fault: it belongs on the timeline, but a
// warning would read as something to fix.
func TestReconcileLogsBeingDisabledWithoutWarning(t *testing.T) {
	c := identityControllerOver(t, answering(kubeconn.Identity{ServerUID: "uid-1"}, nil))
	obj := createCluster(t, c.deps, "prod")
	obj.Spec.Enabled = false

	client := &stubControllerClient{}
	reconcileCluster(t, c, client, obj)

	ev := loggedEvent(t, client)
	assert.Equal(t, beehive.EventNormal, ev.Type)
	assert.Equal(t, ReasonInactive, ev.Reason)
}

// A record whose source names no context produces no verdict, so it has nothing to log
// either — an empty timeline is the honest answer.
func TestReconcileLogsNothingForAnotherSource(t *testing.T) {
	c := identityControllerOver(t, answering(kubeconn.Identity{ServerUID: "uid-1"}, nil))
	obj, err := c.clusterClient.Create(context.Background(), "cloud/prod", ClusterRuntimeSpec{Source: "cloud"})
	require.NoError(t, err)

	client := &stubControllerClient{}
	reconcileCluster(t, c, client, obj)

	assert.Empty(t, client.events)
}

// Every condition here describes process-scoped state, so a previous process's write
// must read as Unknown until this one re-confirms it.
func TestReconcileReportsLiveness(t *testing.T) {
	c := identityControllerOver(t, answering(kubeconn.Identity{ServerUID: "uid-1"}, nil))
	obj := createCluster(t, c.deps, "prod")

	client := &stubControllerClient{}
	reconcileCluster(t, c, client, obj)

	assert.True(t, reportedCondition(t, client).Liveness)
}

// A cluster the user switched off is not probed, and says so rather than looking like
// one that failed to connect.
func TestReconcileReportsInactiveWhenDisabled(t *testing.T) {
	svc := answering(kubeconn.Identity{ServerUID: "uid-1"}, nil)
	c := identityControllerOver(t, svc)
	obj := createCluster(t, c.deps, "prod")
	obj.Spec.Enabled = false

	client := &stubControllerClient{}
	reconcileCluster(t, c, client, obj)

	assert.Empty(t, svc.asked, "a disabled cluster is not asked about at all")
	cond := reportedCondition(t, client)
	assert.Equal(t, ConditionFalse, cond.Status)
	assert.Equal(t, ReasonInactive, cond.Reason)
}

// Releasing is what stops the probe, so a cluster switched off after it connected has to
// give its claim back — a record that costs nothing to keep should cost no dial.
func TestReconcileReleasesTheClaimWhenTheClusterIsDisabled(t *testing.T) {
	svc := answering(kubeconn.Identity{ServerUID: "uid-1"}, nil)
	c := identityControllerOver(t, svc)
	obj := createCluster(t, c.deps, "prod")
	reconcileCluster(t, c, &stubControllerClient{}, obj)
	require.Equal(t, []string{"prod"}, svc.asked, "the enabled pass claims")

	obj.Spec.Enabled = false
	reconcileCluster(t, c, &stubControllerClient{}, obj)

	assert.Equal(t, []string{"prod"}, svc.released)
}

// A record whose source stopped naming credentials has none to hold a claim on, so the
// claim goes back for the same reason a disabled one's does.
func TestReconcileReleasesTheClaimWhenTheSourceStopsNamingIt(t *testing.T) {
	svc := answering(kubeconn.Identity{ServerUID: "uid-1"}, nil)
	c := identityControllerOver(t, svc)
	obj := createCluster(t, c.deps, "prod")
	reconcileCluster(t, c, &stubControllerClient{}, obj)
	require.Empty(t, svc.released)

	obj.Spec.Source, obj.Spec.SourceKey = "cloud", nil
	reconcileCluster(t, c, &stubControllerClient{}, obj)

	assert.Equal(t, []string{"prod"}, svc.released)
}

// The read asks for the probe and returns whatever is known, which on the first pass is
// nothing. Neither connected nor failed, and the signal the probe publishes is what
// brings this record back.
func TestReconcileReportsConnectingUntilAnswered(t *testing.T) {
	svc := &fakeKubeconn{}
	c := identityControllerOver(t, svc)
	obj := createCluster(t, c.deps, "prod")

	client := &stubControllerClient{}
	reconcileCluster(t, c, client, obj)

	assert.Equal(t, []string{"prod"}, svc.asked, "asking is what queues the probe")
	cond := reportedCondition(t, client)
	assert.Equal(t, ConditionUnknown, cond.Status)
	assert.Equal(t, ReasonConnecting, cond.Reason)
}

// A record from another source names no context to resolve, so a verdict about reaching
// its server is one no pass here produced.
func TestReconcileReportsNoConditionForAnotherSource(t *testing.T) {
	svc := answering(kubeconn.Identity{ServerUID: "uid-1"}, nil)
	c := identityControllerOver(t, svc)
	obj, err := c.clusterClient.Create(context.Background(), "cloud/prod", ClusterRuntimeSpec{Source: "cloud"})
	require.NoError(t, err)

	client := &stubControllerClient{}
	reconcileCluster(t, c, client, obj)

	assert.Empty(t, client.conditions)
	assert.Empty(t, svc.asked)
	assert.Nil(t, client.updated.Server.UID)
}

// A condition the store refuses is this pass's failure, not the cluster's: settling
// would report a verdict nothing recorded.
func TestReconcileReportsAFailedConditionWrite(t *testing.T) {
	boom := errors.New("boom")
	c := identityControllerOver(t, answering(kubeconn.Identity{ServerUID: "uid-1"}, nil))
	obj := createCluster(t, c.deps, "prod")

	res := c.Reconcile(context.Background(), &stubControllerClient{setErr: boom}, obj)

	assert.ErrorIs(t, res.Err(), boom)
}

// A verdict the timeline refuses is this pass's failure too: settling would leave the
// timeline without the transition the conditions just reported.
func TestReconcileReportsAFailedVerdictWrite(t *testing.T) {
	boom := errors.New("boom")
	c := identityControllerOver(t, answering(kubeconn.Identity{ServerUID: "uid-1"}, nil))
	obj := createCluster(t, c.deps, "prod")

	res := c.Reconcile(context.Background(), &stubControllerClient{eventErr: boom}, obj)

	assert.ErrorIs(t, res.Err(), boom)
}

// The identity comes from the probe, not from the record: this is the write that turns
// a tracked context into one with a mirror.
func TestReconcileWritesTheProbedUID(t *testing.T) {
	c := identityControllerOver(t, answering(kubeconn.Identity{ServerUID: "uid-1"}, nil))
	obj := createCluster(t, c.deps, "prod")

	client := &stubControllerClient{}
	reconcileCluster(t, c, client, obj)

	require.NotNil(t, client.updated)
	require.NotNil(t, client.updated.Server.UID)
	assert.Equal(t, "uid-1", *client.updated.Server.UID)
}

// One probe answers three questions, and dropping two would throw away requests it
// already paid for. The version and the principal have their own status homes.
func TestReconcileWritesTheWholeIdentity(t *testing.T) {
	c := identityControllerOver(t, answering(kubeconn.Identity{
		ServerUID: "uid-1", ServerVersion: "v1.29.3", Username: "admin@example",
	}, nil))
	obj := createCluster(t, c.deps, "prod")

	client := &stubControllerClient{}
	reconcileCluster(t, c, client, obj)

	require.NotNil(t, client.updated)
	require.NotNil(t, client.updated.Server.Version)
	assert.Equal(t, "v1.29.3", *client.updated.Server.Version)
	require.NotNil(t, client.updated.Principal.Username)
	assert.Equal(t, "admin@example", *client.updated.Principal.Username)
}

// The endpoint is what tells two contexts naming one server apart from two servers, so a
// pass that reached one records where it went.
func TestReconcileWritesTheEndpointAndGroups(t *testing.T) {
	c := identityControllerOver(t, knowing(kubeconn.State{
		Connection: answeredWith("https://prod.example:6443"),
		ServerUID:  answeredWith("uid-1"),
		Principal: answeredWith(kubeconn.Principal{
			Username: "admin@example",
			Groups:   []string{"system:masters", "system:authenticated"},
		}),
	}))
	obj := createCluster(t, c.deps, "prod")

	client := &stubControllerClient{}
	reconcileCluster(t, c, client, obj)

	require.NotNil(t, client.updated)
	require.NotNil(t, client.updated.Server.Endpoint)
	assert.Equal(t, "https://prod.example:6443", *client.updated.Server.Endpoint)
	assert.Equal(t, []string{"system:authenticated", "system:masters"}, client.updated.Principal.Groups,
		"sorted, so a server that re-orders them does not re-emit the record")
}

// Nothing has been probed, so there is nothing to report about the server — a status
// carrying invented blanks would be indistinguishable from one that read them.
func TestReconcileWritesNoServerFactsWhileNothingIsProbed(t *testing.T) {
	c := identityControllerOver(t, &fakeKubeconn{})
	obj := createCluster(t, c.deps, "prod")

	client := &stubControllerClient{}
	reconcileCluster(t, c, client, obj)

	require.NotNil(t, client.updated)
	assert.Nil(t, client.updated.Server.Endpoint)
	assert.Nil(t, client.updated.Server.UID)
	assert.Empty(t, client.updated.Principal.Groups)
}

// A probe refused the kube-system read but reached the server, so it knows who it
// connected as and not what it connected to. Each fact stands on its own: reporting
// none of them would hide a cluster that is up.
func TestReconcileWritesThePartOfTheIdentityAProbeReached(t *testing.T) {
	c := identityControllerOver(t, knowing(kubeconn.State{
		Connection:    answeredWith("https://prod.example:6443"),
		Readiness:     answeredWith(kubeconn.ComponentStatus{}),
		ServerVersion: answeredWith(kubeconn.VersionInfo{GitVersion: "v1.29.3"}),
		Principal:     answeredWith(kubeconn.Principal{Username: "reader@example"}),
		ServerUID:     forbidden("namespaces \"kube-system\" is forbidden"),
	}))
	obj := createCluster(t, c.deps, "prod")

	client := &stubControllerClient{}
	reconcileCluster(t, c, client, obj)

	require.NotNil(t, client.updated)
	assert.Nil(t, client.updated.Server.UID, "nothing read it, so nothing claims it")
	require.NotNil(t, client.updated.Principal.Username)
	assert.Equal(t, "reader@example", *client.updated.Principal.Username)
	assert.Empty(t, liveCaches(t, c.cacheClient), "and no cache, since none is named yet")
	assert.Equal(t, ConditionTrue, reportedCondition(t, client).Status,
		"a user refused kube-system still reached a cluster that is up")
}

// Reaching a server needs no authorization and naming it does, so the two verdicts move
// apart — and this is the one that explains a connected cluster that never gets a cache.
func TestReconcileReportsUnidentifiedWhenTheUIDIsRefused(t *testing.T) {
	c := identityControllerOver(t, knowing(kubeconn.State{
		Connection: answeredWith("https://prod.example:6443"),
		Readiness:  answeredWith(kubeconn.ComponentStatus{}),
		ServerUID:  forbidden("namespaces \"kube-system\" is forbidden"),
	}))
	obj := createCluster(t, c.deps, "prod")

	client := &stubControllerClient{}
	reconcileCluster(t, c, client, obj)

	assert.Equal(t, ConditionTrue, conditionOfType(t, client, ConditionConnected).Status)
	cond := conditionOfType(t, client, ConditionIdentified)
	assert.Equal(t, ConditionFalse, cond.Status)
	assert.Equal(t, ReasonUIDUnreadable, cond.Reason)
	assert.Contains(t, cond.Message, "forbidden")
}

// Identity is not a fact about a server nothing reached, so a failed probe leaves it
// unassessed rather than claiming the cluster is unidentifiable.
func TestReconcileLeavesIdentityUnassessedWhenTheProbeFailed(t *testing.T) {
	c := identityControllerOver(t, answering(kubeconn.Identity{}, errors.New("connection refused")))
	obj := createCluster(t, c.deps, "prod")

	client := &stubControllerClient{}
	reconcileCluster(t, c, client, obj)

	assert.Equal(t, string(kubeconn.ReasonUnreachable), conditionOfType(t, client, ConditionConnected).Reason)
	assert.Equal(t, ReasonNoConnection, conditionOfType(t, client, ConditionIdentified).Reason)
}

// The pass that learns an identity is the one that creates its cache. Nothing else
// would: a status write moves no generation, so no owed pass lists this record again,
// and the cache that would be its dependent is the thing that does not exist yet.
func TestReconcileCreatesTheCacheInTheSamePassThatLearnsTheUID(t *testing.T) {
	c := identityControllerOver(t, answering(kubeconn.Identity{ServerUID: "uid-1"}, nil))
	obj := createCluster(t, c.deps, "prod")

	reconcileCluster(t, c, &stubControllerClient{}, obj)

	objs := liveCaches(t, c.cacheClient)
	require.Len(t, objs, 1)
	assert.Equal(t, "uid-1", objs[0].Spec.ServerUID)
}

// An unprobed record reports nothing, which must not be read as "no identity": clearing
// the UID would deactivate a live cache because a probe had not answered yet.
func TestReconcileKeepsTheLastKnownUIDWhileNothingIsProbed(t *testing.T) {
	c := controllerOver(t)
	obj := probedCluster(t, c.deps, "uid-1")

	client := &stubControllerClient{}
	reconcileCluster(t, c, client, obj)

	require.NotNil(t, client.updated.Server.UID, "an unanswered probe must not clear it")
	assert.Equal(t, "uid-1", *client.updated.Server.UID)
	assert.Len(t, liveCaches(t, c.cacheClient), 1, "and the cache it already calls for stands")
}

// A cluster owns a mirror slot per identity it has been probed at, so the pass that
// knows the identity is the one that creates it.
func TestReconcileCreatesACacheForTheProbedIdentity(t *testing.T) {
	c := controllerOver(t)
	obj := probedCluster(t, c.deps, "uid-1")

	reconcileCluster(t, c, &stubControllerClient{}, obj)

	objs := liveCaches(t, c.cacheClient)
	require.Len(t, objs, 1)
	assert.Equal(t, ClusterCacheName(clusterIDOf(obj), "uid-1"), objs[0].Name)
	assert.Equal(t, "uid-1", objs[0].Spec.ServerUID)

	// The owner edge is what carries the cluster join AND what beehive's GC cascades
	// on, so a cache created without one outlives the cluster it mirrors.
	owner, ok, err := objs[0].Owner()
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, obj.ID, owner.ID)
}

// Every pass ensures the cache, and the source wakes every record on every
// kubeconfig snapshot — so the second pass is the common case, not the edge.
func TestReconcileCreatesTheCacheOnlyOnce(t *testing.T) {
	c := controllerOver(t)
	obj := probedCluster(t, c.deps, "uid-1")

	for range 2 {
		reconcileCluster(t, c, &stubControllerClient{}, obj)
	}

	assert.Len(t, liveCaches(t, c.cacheClient), 1)
}

// A cluster that has never connected has no identity to mirror. Creating one anyway
// would name it after the empty UID — a cache CacheIsActive matches against nothing,
// so it could never sync and never be superseded either.
func TestReconcileCreatesNoCacheBeforeTheFirstProbe(t *testing.T) {
	c := controllerOver(t)

	reconcileCluster(t, c, &stubControllerClient{}, probedCluster(t, c.deps, ""))

	assert.Empty(t, liveCaches(t, c.cacheClient))
}

// A rebuilt cluster reuses its record under a new kube-system UID. The mirror cannot
// be reused with it, so the pass adds a second cache and leaves the first in place to
// drain — which is why Cluster.caches is a list.
func TestReconcileAddsACacheForAMigratedIdentity(t *testing.T) {
	c := controllerOver(t)
	obj := probedCluster(t, c.deps, "uid-1")

	reconcileCluster(t, c, &stubControllerClient{}, obj)

	migrated := "uid-2"
	obj.Status.Server.UID = &migrated
	reconcileCluster(t, c, &stubControllerClient{}, obj)

	uids := make([]string, 0, 2)
	for _, cache := range liveCaches(t, c.cacheClient) {
		uids = append(uids, cache.Spec.ServerUID)
	}
	assert.Equal(t, []string{"uid-1", "uid-2"}, uids, "the superseded cache stays until its subtree drains")
}

// stubCacheClient reports every cache absent and fails the create that follows. The
// embedded interface is nil: the ensure calls nothing else on it.
type stubCacheClient struct {
	beehive.Client[ClusterCacheSpec, ClusterCacheStatus]
	err error
}

func (c stubCacheClient) GetByName(context.Context, string, ...beehive.LoadOption) (*beehive.Object[ClusterCacheSpec, ClusterCacheStatus], error) {
	return nil, beehive.ErrNotFound
}

func (c stubCacheClient) GetOrCreate(context.Context, string, ClusterCacheSpec, ...beehive.Option) (*beehive.Object[ClusterCacheSpec, ClusterCacheStatus], bool, error) {
	return nil, false, c.err
}

// The cache is part of the pass, so failing to ensure it fails the reconcile: settling
// instead would leave a cluster holding an identity with no mirror and nothing left to
// re-level it. The observation is published anyway — it is a different fact with
// different consumers, and holding it back would leave a departed context marked
// present for as long as an unrelated create keeps failing.
func TestReconcileReportsAFailedCacheCreate(t *testing.T) {
	boom := errors.New("boom")
	c := controllerOver(t)
	c.cacheClient = stubCacheClient{err: boom}
	obj := probedCluster(t, c.deps, "uid-1")

	client := &stubControllerClient{}
	res := c.Reconcile(context.Background(), client, obj)

	name := ClusterCacheName(clusterIDOf(obj), "uid-1")
	assert.Equal(t, fmt.Errorf("create cluster cache %s: %w", name, boom), res.Err())
	assert.NotNil(t, client.updated, "the observation is not the cache's to hold back")
}

// beehive starts ahead of the controllers, so an owed pass can reach a record before
// the kubeconfig's first read. Observing the pre-read config would report a present
// context absent and wake the kind's watches for a flap.
func TestReconcileDefersUntilTheKubeconfigIsRead(t *testing.T) {
	// Unstarted, which is the only way to hold a service that has not read yet.
	c := &clusterController{deps: deps{kubeconfigSvc: kubeconfig.New(t.TempDir()+"/config", nil)}}

	client := &stubControllerClient{}
	res := c.Reconcile(context.Background(), client, kubeconfigObj("prod", nil))

	assert.Nil(t, client.updated, "nothing observed, so nothing to write")
	assert.Equal(t, beehive.Unsettled().RequeueAfter(startupRequeue), res,
		"nothing settled, and it has to be revisited once the watcher has read")
}

func TestReconcileWritesTheObservation(t *testing.T) {
	client := &stubControllerClient{}

	res := controllerOver(t).Reconcile(context.Background(), client, kubeconfigObj("prod", nil))

	assert.Equal(t, beehive.Settled(), res)
	require.NotNil(t, client.updated)
	require.NotNil(t, client.updated.Source.Kubeconfig)
	assert.False(t, client.updated.Source.Kubeconfig.IsPresent, "the seed config holds no contexts")
}

// The observation is written into the status already there, not over it: everything
// else on the blob belongs to a probe this reconcile did not run.
func TestReconcileKeepsTheRestOfTheStatus(t *testing.T) {
	uid := "uid-1"
	obj := kubeconfigObj("prod", &ClusterStatus{Server: ClusterServer{UID: &uid}})

	client := &stubControllerClient{}
	reconcileCluster(t, controllerOver(t), client, obj)

	require.NotNil(t, client.updated)
	require.NotNil(t, client.updated.Server.UID)
	assert.Equal(t, uid, *client.updated.Server.UID)
}

// A failed status write is the reconcile's failure, so beehive retries it.
func TestReconcileReportsAFailedStatusWrite(t *testing.T) {
	boom := errors.New("boom")

	res := controllerOver(t).Reconcile(context.Background(), &stubControllerClient{updateErr: boom}, kubeconfigObj("prod", nil))

	assert.ErrorIs(t, res.Err(), boom)
}

// A spec write this observation does not depend on still bumps the generation, and an
// object left unsettled is re-dispatched by beehive's owed pass forever — so a pass
// that observes nothing new still has to report that it settled.
//
// What it reports is what was already stored: beehive compares that against the status
// it handed out and reaches the store only for a difference, so an unchanged
// observation writes nothing without this pass deciding it.
func TestReconcileSettlesWhenNothingMoved(t *testing.T) {
	// The seed config holds no contexts, so this is what the reconcile will observe.
	obj := kubeconfigObj("prod", &ClusterStatus{Source: ClusterStatusSource{
		Kubeconfig: &ClusterStatusSourceKubeconfig{
			Cluster: ClusterStatusSourceKubeconfigCluster{Name: "prod-cluster"},
			User:    ClusterStatusSourceKubeconfigUser{Name: "prod-user"},
		},
	}})
	obj.Generation = 3

	client := &stubControllerClient{}
	res := controllerOver(t).Reconcile(context.Background(), client, obj)

	require.NotNil(t, client.updated)
	assert.Equal(t, *obj.Status, *client.updated, "nothing observed moved")
	assert.Equal(t, beehive.Settled(), res)
}

// A record from another source has no observation of its own, and two of those are as
// unchanged as any other repeat.
func TestReconcileSettlesForAnotherSource(t *testing.T) {
	// beehive's generations start at 1; 0 is not a generation any object is handed.
	obj := &beehive.Object[ClusterRuntimeSpec, ClusterStatus]{ID: 1, Generation: 1, Status: &ClusterStatus{}}

	client := &stubControllerClient{}
	res := controllerOver(t).Reconcile(context.Background(), client, obj)

	require.NotNil(t, client.updated)
	assert.Equal(t, ClusterStatus{}, *client.updated, "no observation of its own to report")
	assert.Equal(t, beehive.Settled(), res)
}

// A record on its way out has nothing to observe, and writing status to it would be a
// write beehive has to carry through a collect that is already under way.
func TestReconcileSkipsADeletingRecord(t *testing.T) {
	obj := kubeconfigObj("prod", nil)
	now := time.Now()
	obj.DeletionRequestedAt = &now

	client := &stubControllerClient{}
	reconcileCluster(t, controllerOver(t), client, obj)

	assert.Nil(t, client.updated)
}

// --- toCluster ---

// toCluster joins the row's identity, stamps and choices with the object's status and
// conditions. Conditions come off the object rows, not the status blob, which is
// where beehive keeps them.
func TestToCluster(t *testing.T) {
	uid := "uid-1"
	row := ClusterRow{
		ID: "7", Source: SourceKubeconfig, SourceKey: new("prod"), Name: new("Prod"),
		Enabled: true, SyncEnabled: true, MonitoringEnabled: true,
		CreatedAt: t0, UpdatedAt: t0.Add(time.Second),
	}
	obj := &beehive.Object[ClusterRuntimeSpec, ClusterStatus]{
		Name:       "7",
		Status:     &ClusterStatus{Server: ClusterServer{UID: &uid}},
		Conditions: []Condition{LiveCondition(ConditionConnected, beehive.ConditionTrue, "Reachable", "")},
	}

	c := toCluster(row, obj)

	assert.Equal(t, ClusterID("7"), c.ID)
	assert.Equal(t, t0, c.CreatedAt)
	assert.Equal(t, t0.Add(time.Second), c.UpdatedAt)
	assert.Nil(t, c.DeletionRequestedAt)
	assert.Equal(t, "Prod", *c.Spec.Name)
	assert.True(t, c.Spec.Enabled)
	assert.True(t, c.Spec.SyncEnabled)
	assert.True(t, c.Spec.MonitoringEnabled)
	assert.Equal(t, "prod", c.Spec.Source.Kubeconfig.Context)
	require.NotNil(t, c.Status.Server.UID)
	assert.Equal(t, uid, *c.Status.Server.UID)
	require.Len(t, c.Conditions, 1, "conditions are object rows, not part of the status blob")
	assert.Equal(t, string(ConditionConnected), c.Conditions[0].Type)
}

// A row the mirror has not given an object yet — or whose object it has already
// removed — has zero status and no conditions.
func TestToClusterWithNoObject(t *testing.T) {
	c := toCluster(ClusterRow{ID: "7", Source: SourceKubeconfig, SourceKey: new("prod")}, nil)

	assert.Equal(t, ClusterStatus{}, c.Status)
	assert.Empty(t, c.Conditions)
	assert.Equal(t, "prod", c.Spec.Source.Kubeconfig.Context)
}

// The mark is surfaced as-is, off the row: a consumer that renders a record on its
// way out has no other way to know.
func TestToClusterCarriesTheDeletionMark(t *testing.T) {
	at := t0.Add(time.Minute)

	c := toCluster(ClusterRow{ID: "7", Source: SourceKubeconfig, DeleteRequestedAt: &at}, nil)

	require.NotNil(t, c.DeletionRequestedAt)
	assert.Equal(t, at, *c.DeletionRequestedAt)
}

// --- Clusters() reads ---

// serviceOver returns a service reading through d, with no background work: these
// tests drive the store directly.
func serviceOver(t *testing.T, d deps) *service {
	t.Helper()
	s := &service{deps: d, now: func() time.Time { return t0.Add(time.Hour) }, stateWatchCadence: defaultStateWatchCadence}
	s.health = newSharedStateWatch(cachesAPI{s}.readAllCacheHealth)
	return s
}

func TestClustersGet(t *testing.T) {
	d := newTestDeps(t)
	obj := createCluster(t, d, "prod")

	got, err := serviceOver(t, d).Clusters().Get(context.Background(), clusterIDOf(obj))

	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, clusterIDOf(obj), got.ID)
	assert.Equal(t, "prod", got.Spec.Source.Kubeconfig.Context)
}

// An unknown id is not an error: the caller holds an id from a watch frame, and a
// record collected in between is an ordinary race rather than a bad request.
func TestClustersGetUnknownIsNotAnError(t *testing.T) {
	got, err := serviceOver(t, newTestDeps(t)).Clusters().Get(context.Background(), ClusterID("404"))

	require.NoError(t, err)
	assert.Nil(t, got)
}

// A record on its way out is served like any other, wearing its mark: only the
// consumer knows whether to render it or hide it.
func TestClustersGetCarriesADeletingRecord(t *testing.T) {
	d := newTestDeps(t)
	obj := createCluster(t, d, "prod")
	_, err := markCluster(context.Background(), d.store.Stmts(), clusterIDOf(obj), t0)
	require.NoError(t, err)

	got, err := serviceOver(t, d).Clusters().Get(context.Background(), clusterIDOf(obj))

	require.NoError(t, err)
	require.NotNil(t, got)
	assert.NotNil(t, got.DeletionRequestedAt, "the mark is what the consumer decides on")
}

// A row the mirror has not reached yet is served with zero status: the record
// exists the moment the importer inserts it, and the probe's facts follow.
func TestClustersGetServesARowWithNoRuntimeObject(t *testing.T) {
	d := newTestDeps(t)
	row := importCluster(t, d, "prod")

	got, err := serviceOver(t, d).Clusters().Get(context.Background(), row.ID)

	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, ClusterStatus{}, got.Status)
	assert.Empty(t, got.Conditions)
	assert.True(t, got.Spec.Enabled)
}

func TestClustersList(t *testing.T) {
	d := newTestDeps(t)
	createCluster(t, d, "prod")
	createCluster(t, d, "staging")

	got, err := serviceOver(t, d).Clusters().List(context.Background())

	require.NoError(t, err)
	require.Len(t, got, 2)
	contexts := []string{got[0].Spec.Source.Kubeconfig.Context, got[1].Spec.Source.Kubeconfig.Context}
	assert.ElementsMatch(t, []string{"prod", "staging"}, contexts)
}

// The collection a reader sees matches what Get would answer for each id.
func TestClustersListCarriesADeletingRecord(t *testing.T) {
	d := newTestDeps(t)
	createCluster(t, d, "prod")
	deleting := createCluster(t, d, "staging")
	_, err := markCluster(context.Background(), d.store.Stmts(), clusterIDOf(deleting), t0)
	require.NoError(t, err)

	got, err := serviceOver(t, d).Clusters().List(context.Background())

	require.NoError(t, err)
	assert.Len(t, got, 2, "the store as it is, tombstones and all")
}

// sortKeysOf returns each listed cluster's display label, in the order served.
func sortKeysOf(t *testing.T, svc *service) []string {
	t.Helper()
	got, err := svc.Clusters().List(context.Background())
	require.NoError(t, err)

	keys := make([]string, len(got))
	for i, c := range got {
		keys[i] = cmp.Or(deref(c.Spec.Name), c.Spec.Source.Kubeconfig.Context)
	}
	return keys
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// The schema promises name order, and the table lists in id order — so a caller
// paging or diffing the list would otherwise see it shuffle between reads.
func TestClustersListIsSortedByName(t *testing.T) {
	d := newTestDeps(t)
	for _, name := range []string{"staging", "alpha", "prod"} {
		createCluster(t, d, name)
	}

	assert.Equal(t, []string{"alpha", "prod", "staging"}, sortKeysOf(t, serviceOver(t, d)))
}

// The order is over what a view renders, so renaming a cluster moves it — the
// kube-context it happens to track is not what a reader is scanning.
func TestClustersListSortsByDisplayNameWhenSet(t *testing.T) {
	d := newTestDeps(t)
	svc := serviceOver(t, d)
	for _, name := range []string{"alpha", "prod"} {
		createCluster(t, d, name)
	}
	// "alpha" renamed to sort last, against its context's own order. No setter
	// writes the name yet, so the row is edited directly.
	_, err := d.db.Write.Exec(`UPDATE clusters SET name = 'zulu' WHERE source_key = 'alpha'`)
	require.NoError(t, err)

	assert.Equal(t, []string{"prod", "zulu"}, sortKeysOf(t, svc))
}

func TestClustersListIsEmptyWithNoRecords(t *testing.T) {
	got, err := serviceOver(t, newTestDeps(t)).Clusters().List(context.Background())

	require.NoError(t, err)
	assert.Empty(t, got)
}

// watchClusters opens a cluster list watch bounded by the test.
func watchClusters(t *testing.T, d deps) *Stream[ClusterCollectionWatchFrame] {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	stream, err := serviceOver(t, d).Clusters().WatchCollection(ctx)
	require.NoError(t, err)
	return stream
}

// The snapshot arrives as Added frames closed by exactly one Bookmark — the frame a
// consumer renders its empty state on, and never before.
func TestClustersWatchCollectionEmitsTheSnapshotThenABookmark(t *testing.T) {
	d := newRunningDeps(t)
	createCluster(t, d, "prod")
	createCluster(t, d, "staging")

	stream := watchClusters(t, d)

	// The snapshot's order is the store's, which no consumer relies on.
	var contexts []string
	for range 2 {
		f := testutil.Recv(t, stream.Frames, "a snapshot frame")
		require.Equal(t, CollectionWatchFrameAdded, f.Type)
		require.NotNil(t, f.Cluster)
		contexts = append(contexts, f.Cluster.Spec.Source.Kubeconfig.Context)
	}
	assert.ElementsMatch(t, []string{"prod", "staging"}, contexts)

	bookmark := testutil.Recv(t, stream.Frames, "the bookmark closing the snapshot")
	assert.Equal(t, CollectionWatchFrameBookmark, bookmark.Type)
	assert.Nil(t, bookmark.Cluster, "the bookmark carries no entity")
}

// An empty collection is definitively empty rather than pending, so the bookmark
// still lands: without it a populated table and an empty one look alike.
func TestClustersWatchCollectionBookmarksAnEmptyCollection(t *testing.T) {
	stream := watchClusters(t, newRunningDeps(t))

	first := testutil.Recv(t, stream.Frames, "the bookmark")
	assert.Equal(t, CollectionWatchFrameBookmark, first.Type)
}

// awaitBookmark drains the snapshot up to and including the bookmark.
func awaitBookmark(t *testing.T, stream *Stream[ClusterCollectionWatchFrame]) {
	t.Helper()
	for {
		if testutil.Recv(t, stream.Frames, "the bookmark").Type == CollectionWatchFrameBookmark {
			return
		}
	}
}

func TestClustersWatchCollectionReportsACreate(t *testing.T) {
	d := newRunningDeps(t)
	stream := watchClusters(t, d)
	awaitBookmark(t, stream)

	createCluster(t, d, "prod")

	f := testutil.Recv(t, stream.Frames, "the create")
	assert.Equal(t, CollectionWatchFrameAdded, f.Type)
	require.NotNil(t, f.Cluster)
	assert.Equal(t, "prod", f.Cluster.Spec.Source.Kubeconfig.Context)
}

// A row edit moves the row and no object; the watch reports it off the DB signal.
func TestClustersWatchCollectionReportsARowEdit(t *testing.T) {
	d := newRunningDeps(t)
	obj := createCluster(t, d, "prod")
	stream := watchClusters(t, d)
	awaitBookmark(t, stream)

	_, err := serviceOver(t, d).Clusters().SetEnabled(context.Background(), clusterIDOf(obj), false)
	require.NoError(t, err)

	f := testutil.Recv(t, stream.Frames, "the update")
	assert.Equal(t, CollectionWatchFrameModified, f.Type)
	require.NotNil(t, f.Cluster)
	assert.False(t, f.Cluster.Spec.Enabled)
}

// A probe moves the object and no row; the watch reports it off the runtime signal.
func TestClustersWatchCollectionReportsAStatusWrite(t *testing.T) {
	d, bh := newRunningDepsAndBeehive(t)
	status := beehive.NewAdminClient[ClusterStatus](bh, ClusterGroupKind)
	obj := createCluster(t, d, "prod")
	stream := watchClusters(t, d)
	awaitBookmark(t, stream)

	uid := "uid-1"
	require.NoError(t, status.UpdateStatus(context.Background(), obj.ID, ClusterStatus{Server: ClusterServer{UID: &uid}}))

	f := testutil.Recv(t, stream.Frames, "the update")
	assert.Equal(t, CollectionWatchFrameModified, f.Type)
	require.NotNil(t, f.Cluster)
	require.NotNil(t, f.Cluster.Status.Server.UID)
	assert.Equal(t, uid, *f.Cluster.Status.Server.UID)
}

// The mark is an ordinary row edit, so a watcher that has seen the row unmarked gets
// it as Modified carrying the mark; the Deleted is the row's removal alone.
func TestClustersWatchCollectionReportsTheMarkAsModified(t *testing.T) {
	d := newRunningDeps(t)
	obj := createCluster(t, d, "prod")
	stream := watchClusters(t, d)
	awaitBookmark(t, stream)

	require.NoError(t, serviceOver(t, d).Clusters().Delete(context.Background(), clusterIDOf(obj)))

	f := testutil.Recv(t, stream.Frames, "the deletion mark")
	assert.Equal(t, CollectionWatchFrameModified, f.Type)
	require.NotNil(t, f.Cluster)
	assert.Equal(t, clusterIDOf(obj), f.Cluster.ID)
	assert.NotNil(t, f.Cluster.DeletionRequestedAt)
}

// A row already marked when the snapshot is taken is in it, as Added, like any other.
func TestClustersWatchCollectionSnapshotCarriesADeletingRecord(t *testing.T) {
	d := newRunningDeps(t)
	obj := createCluster(t, d, "prod")
	_, err := markCluster(context.Background(), d.store.Stmts(), clusterIDOf(obj), t0)
	require.NoError(t, err)

	stream := watchClusters(t, d)

	f := testutil.Recv(t, stream.Frames, "the marked row")
	assert.Equal(t, CollectionWatchFrameAdded, f.Type)
	require.NotNil(t, f.Cluster)
	assert.NotNil(t, f.Cluster.DeletionRequestedAt)
	assert.Equal(t, CollectionWatchFrameBookmark, testutil.Recv(t, stream.Frames, "the bookmark").Type)
}

// The row's removal is the Deleted, carrying the last-known record. A mark and a
// removal landing before one reread surface as the Deleted alone: only the
// departure is promised.
func TestClustersWatchCollectionReportsTheRemoval(t *testing.T) {
	d := newRunningDeps(t)
	obj := createCluster(t, d, "prod")
	stream := watchClusters(t, d)
	awaitBookmark(t, stream)

	_, err := markCluster(context.Background(), d.store.Stmts(), clusterIDOf(obj), t0)
	require.NoError(t, err)
	_, err = deleteMarkedCluster(context.Background(), d.store.Stmts(), clusterIDOf(obj))
	require.NoError(t, err)
	d.db.Notify(appdb.KeyClusters)

	f := testutil.Recv(t, stream.Frames, "the removal")
	assert.Equal(t, CollectionWatchFrameDeleted, f.Type)
	require.NotNil(t, f.Cluster)
	assert.Equal(t, clusterIDOf(obj), f.Cluster.ID)
}

// Cancellation is an ordinary teardown, so Frames closes with nothing to report: a
// consumer reads Err on close, and a reason there is rendered as a dead watch.
func TestClustersWatchCollectionCancellationIsQuiet(t *testing.T) {
	d := newRunningDeps(t)
	ctx, cancel := context.WithCancel(context.Background())

	stream, err := serviceOver(t, d).Clusters().WatchCollection(ctx)
	require.NoError(t, err)
	awaitBookmark(t, stream)

	cancel()

	testutil.WaitClosed(t, stream.Frames, "the frames to close on cancellation")
	assert.NoError(t, stream.Err())
}

// watchCluster opens a collection watch of one cluster, bounded by the test.
func watchCluster(t *testing.T, d deps, id ClusterID) *Stream[ClusterCollectionWatchFrame] {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	stream, err := serviceOver(t, d).Clusters().WatchCollectionByID(ctx, id)
	require.NoError(t, err)
	return stream
}

func TestClustersWatchCollectionByIDEmitsTheRecordThenABookmark(t *testing.T) {
	d := newRunningDeps(t)
	obj := createCluster(t, d, "prod")

	stream := watchCluster(t, d, clusterIDOf(obj))

	first := testutil.Recv(t, stream.Frames, "the record")
	assert.Equal(t, CollectionWatchFrameAdded, first.Type)
	require.NotNil(t, first.Cluster)
	assert.Equal(t, clusterIDOf(obj), first.Cluster.ID)

	assert.Equal(t, CollectionWatchFrameBookmark, testutil.Recv(t, stream.Frames, "the bookmark").Type)
}

// An id naming nothing is bookmark-only rather than an error: the record may not
// exist yet, and the same subscription reports it arriving.
func TestClustersWatchCollectionByIDBookmarksAnUnknownID(t *testing.T) {
	stream := watchCluster(t, newRunningDeps(t), ClusterID("404"))

	assert.Equal(t, CollectionWatchFrameBookmark, testutil.Recv(t, stream.Frames, "the bookmark").Type)
}

func TestClustersWatchCollectionByIDReportsChangesToItsRecord(t *testing.T) {
	d := newRunningDeps(t)
	obj := createCluster(t, d, "prod")
	other := createCluster(t, d, "staging")
	stream := watchCluster(t, d, clusterIDOf(obj))
	awaitBookmark(t, stream)

	// Another record's change is not this stream's.
	_, err := serviceOver(t, d).Clusters().SetEnabled(context.Background(), clusterIDOf(other), false)
	require.NoError(t, err)
	require.NoError(t, serviceOver(t, d).Clusters().Delete(context.Background(), clusterIDOf(obj)))

	f := testutil.Recv(t, stream.Frames, "the deletion mark")
	assert.Equal(t, CollectionWatchFrameModified, f.Type)
	require.NotNil(t, f.Cluster)
	assert.Equal(t, clusterIDOf(obj), f.Cluster.ID)
	assert.NotNil(t, f.Cluster.DeletionRequestedAt)
}

// --- the schedule state watch ---

// watchSchedule opens the state watch for id over a cancellable context, handing back the
// cancel so a test can pin what ending the stream does.
func watchSchedule(t *testing.T, d deps, id ClusterID) (<-chan Schedule, context.CancelFunc) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	ch, err := serviceOver(t, d).Clusters().WatchScheduleState(ctx, id)
	require.NoError(t, err)
	return ch, cancel
}

// scheduledAt is a probe with a run queued for at and none in flight.
func scheduledAt(at time.Time) kubeconn.Observation[string] {
	return kubeconn.Observation[string]{Attempts: kubeconn.Attempts{NextAttempt: kubeconn.Attempt{ScheduledAt: at}}}
}

// The countdown is the connection's own. The other four run on their own clocks, and a
// countdown to whichever was due next would not be the one a retry acts on.
func TestClustersWatchScheduleStateReportsTheConnectionProbe(t *testing.T) {
	d := newTestDeps(t)
	obj := createCluster(t, d, "prod")
	dialAt := probedAt.Add(30 * time.Second)
	d.kubeconnSvc = knowing(kubeconn.State{
		Connection: scheduledAt(dialAt),
		ServerUID:  scheduledAt(probedAt.Add(10 * time.Second)),
	})

	ch, _ := watchSchedule(t, d, clusterIDOf(obj))

	got := testutil.Recv(t, ch, "the current schedule")
	require.NotNil(t, got.NextRequeueAt)
	assert.Equal(t, dialAt, *got.NextRequeueAt)
	assert.False(t, got.Probing)
}

// A down connection's countdown comes with how many attempts in a row have failed.
func TestClustersWatchScheduleStateCountsTheFailedAttempts(t *testing.T) {
	d := newTestDeps(t)
	obj := createCluster(t, d, "prod")
	failing := scheduledAt(probedAt.Add(30 * time.Second))
	failing.Failures = 3
	d.kubeconnSvc = knowing(kubeconn.State{Connection: failing})

	ch, _ := watchSchedule(t, d, clusterIDOf(obj))

	assert.Equal(t, 3, testutil.Recv(t, ch, "the current schedule").Failures)
}

// Every probe but the connection suspends while it is down, so a disconnected cluster's
// countdown is its retry — the case the state watch exists for.
func TestClustersWatchScheduleStateIgnoresTheOtherProbes(t *testing.T) {
	d := newTestDeps(t)
	obj := createCluster(t, d, "prod")
	d.kubeconnSvc = knowing(kubeconn.State{ServerUID: scheduledAt(probedAt.Add(10 * time.Second))})
	pool := d.kubeconnSvc.(*fakeKubeconn)
	ch, _ := watchSchedule(t, d, clusterIDOf(obj))

	// Nothing to report while only another probe is scheduled, so drive one pass that
	// arms the connection to prove the stream is live rather than merely silent.
	testutil.NoRecv(t, ch, 50*time.Millisecond, "a schedule from another probe")
	pool.publishState("prod", kubeconn.State{Connection: scheduledAt(probedAt)})

	assert.NotNil(t, testutil.Recv(t, ch, "the connection's schedule").NextRequeueAt)
}

func TestClustersWatchScheduleStateFollowsThePool(t *testing.T) {
	d := newTestDeps(t)
	obj := createCluster(t, d, "prod")
	d.kubeconnSvc = knowing(kubeconn.State{Connection: scheduledAt(probedAt.Add(30 * time.Second))})
	pool := d.kubeconnSvc.(*fakeKubeconn)
	ch, _ := watchSchedule(t, d, clusterIDOf(obj))
	testutil.Recv(t, ch, "the current schedule")

	next := probedAt.Add(time.Minute)
	pool.publishState("prod", kubeconn.State{Connection: scheduledAt(next)})

	got := testutil.Recv(t, ch, "the schedule the pass moved")
	require.NotNil(t, got.NextRequeueAt)
	assert.Equal(t, next, *got.NextRequeueAt)
}

// Probing is asserted from the run, never inferred from a countdown that has run out — and the
// time the run in flight was scheduled for is the current reconcile, not the next one.
func TestClustersWatchScheduleStateReportsARunInFlight(t *testing.T) {
	d := newTestDeps(t)
	obj := createCluster(t, d, "prod")
	d.kubeconnSvc = knowing(kubeconn.State{
		Connection: kubeconn.Observation[string]{Attempts: kubeconn.Attempts{
			NextAttempt: kubeconn.Attempt{ScheduledAt: probedAt, StartedAt: probedAt},
		}},
	})

	ch, _ := watchSchedule(t, d, clusterIDOf(obj))

	got := testutil.Recv(t, ch, "the current schedule")
	assert.True(t, got.Probing)
	assert.Nil(t, got.NextRequeueAt, "the time the in-flight run was scheduled for")
}

// A state watch says nothing before its first measurement, and a claim whose first pass has
// not landed has made none — reporting its zero state would say "nothing is scheduled"
// about a cluster that is about to be dialed.
func TestClustersWatchScheduleStateSaysNothingBeforeTheFirstPass(t *testing.T) {
	d := newTestDeps(t)
	obj := createCluster(t, d, "prod")
	pool := d.kubeconnSvc.(*fakeKubeconn)
	ch, _ := watchSchedule(t, d, clusterIDOf(obj))

	// A negative assertion has no event to wait for, so it needs a bounded window; the
	// stream emits on its own goroutine the moment it is opened, so a short one is enough.
	testutil.NoRecv(t, ch, 50*time.Millisecond, "a schedule for an unprobed claim")

	pool.publishState("prod", kubeconn.State{Connection: scheduledAt(probedAt)})
	assert.NotNil(t, testutil.Recv(t, ch, "the first pass").NextRequeueAt)
}

// Once something has been measured, "nothing is scheduled" is news: a suspended
// connection is what a context the kubeconfig stopped naming looks like.
func TestClustersWatchScheduleStateReportsASuspendedConnection(t *testing.T) {
	d := newTestDeps(t)
	obj := createCluster(t, d, "prod")
	d.kubeconnSvc = knowing(kubeconn.State{Connection: scheduledAt(probedAt)})
	pool := d.kubeconnSvc.(*fakeKubeconn)
	ch, _ := watchSchedule(t, d, clusterIDOf(obj))
	testutil.Recv(t, ch, "the current schedule")

	pool.publishState("prod", kubeconn.State{})

	assert.Nil(t, testutil.Recv(t, ch, "the suspended schedule").NextRequeueAt)
}

func TestClustersWatchScheduleStateReleasesItsClaimWhenTheStreamEnds(t *testing.T) {
	d := newTestDeps(t)
	obj := createCluster(t, d, "prod")
	pool := d.kubeconnSvc.(*fakeKubeconn)
	ch, cancel := watchSchedule(t, d, clusterIDOf(obj))

	cancel()

	testutil.WaitClosed(t, ch, "the state watch to close on cancellation")
	assert.Equal(t, []string{"prod"}, pool.released)
}

func TestClustersSetEnabled(t *testing.T) {
	d := newTestDeps(t)
	obj := createCluster(t, d, "prod")

	got, err := serviceOver(t, d).Clusters().SetEnabled(context.Background(), clusterIDOf(obj), false)

	require.NoError(t, err)
	require.NotNil(t, got)
	assert.False(t, got.Spec.Enabled)
	assert.Equal(t, "prod", got.Spec.Source.Kubeconfig.Context, "the rest of the spec is untouched")
}

func TestClustersSetSyncEnabled(t *testing.T) {
	d := newTestDeps(t)
	obj := createCluster(t, d, "prod")

	got, err := serviceOver(t, d).Clusters().SetSyncEnabled(context.Background(), clusterIDOf(obj), true)

	require.NoError(t, err)
	require.NotNil(t, got)
	assert.True(t, got.Spec.SyncEnabled)
	assert.True(t, got.Spec.Enabled, "the other axis is untouched")
}

// Each setter writes its one column, so two landing at once keep each other's.
func TestClustersSettersDoNotLoseConcurrentUpdates(t *testing.T) {
	d := newTestDeps(t)
	obj := createCluster(t, d, "prod")
	clusters := serviceOver(t, d).Clusters()
	id := clusterIDOf(obj)

	var wg sync.WaitGroup
	wg.Go(func() {
		_, err := clusters.SetEnabled(context.Background(), id, false)
		assert.NoError(t, err)
	})
	wg.Go(func() {
		_, err := clusters.SetSyncEnabled(context.Background(), id, true)
		assert.NoError(t, err)
	})
	wg.Wait()

	got, err := clusters.Get(context.Background(), id)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.False(t, got.Spec.Enabled, "SetEnabled's write survived")
	assert.True(t, got.Spec.SyncEnabled, "SetSyncEnabled's write survived")
}

// A mutation against a record that is gone is the caller's to see: unlike a read, it
// asked for a change that did not happen. It reports the boundary's own sentinel —
// graph matches that one, and beehive is not supposed to be visible through here.
func TestClustersSetEnabledReportsAnUnknownID(t *testing.T) {
	_, err := serviceOver(t, newTestDeps(t)).Clusters().SetEnabled(context.Background(), ClusterID("404"), false)

	assert.ErrorIs(t, err, ErrNotFound)
}

// The third toggle is stored and served like the other two, and bumps updated_at as
// a user edit does.
func TestClustersSetMonitoringEnabled(t *testing.T) {
	d := newTestDeps(t)
	obj := createCluster(t, d, "prod")

	got, err := serviceOver(t, d).Clusters().SetMonitoringEnabled(context.Background(), clusterIDOf(obj), true)

	require.NoError(t, err)
	assert.True(t, got.Spec.MonitoringEnabled)
	assert.Equal(t, t0.Add(time.Hour), got.UpdatedAt)
}

// SetDefault returns the record as the default, and wakes the watchers.
func TestClustersSetDefault(t *testing.T) {
	d, _ := newClusterStatusDeps(t)
	d.kubeconfigSvc = fakeKubeconfigService{cfg: cfgWith("prod"), loaded: true}
	obj := createCluster(t, d, "prod")
	sub := d.db.ListenForChanges(appdb.KeyClusters)
	defer sub.Close()

	got, err := serviceOver(t, d).Clusters().SetDefault(context.Background(), clusterIDOf(obj), true)

	require.NoError(t, err)
	assert.True(t, got.Spec.IsDefault)
	assert.Equal(t, "prod", got.Spec.Source.Kubeconfig.Context, "the rest of the spec is untouched")
	testutil.Recv(t, sub.Chan(), "the clusters signal")
}

// Only a cluster the kubeconfig declares can become the default: an archived one or
// one from another source is refused, and nothing changes.
func TestClustersSetDefaultRefusesAClusterTheKubeconfigDoesNotDeclare(t *testing.T) {
	d, _ := newClusterStatusDeps(t)
	d.kubeconfigSvc = fakeKubeconfigService{cfg: cfgWith("staging"), loaded: true}
	svc := serviceOver(t, d)
	archived := createCluster(t, d, "prod")
	_, err := d.db.Write.Exec(`INSERT INTO clusters (id, source, created_at, updated_at) VALUES ('cloud-1', 'cloud', 0, 0)`)
	require.NoError(t, err)

	for _, id := range []ClusterID{clusterIDOf(archived), "cloud-1"} {
		_, err := svc.Clusters().SetDefault(context.Background(), id, true)
		require.ErrorIs(t, err, ErrCannotBeDefault, id)
		got, _, err := getCluster(context.Background(), d.store.Stmts(), id)
		require.NoError(t, err)
		assert.False(t, got.IsDefault, id)
	}
}

// An archived default can still be cleared.
func TestClustersSetDefaultClearsAnArchivedDefault(t *testing.T) {
	d, _ := newClusterStatusDeps(t)
	d.kubeconfigSvc = fakeKubeconfigService{cfg: cfgWith("prod"), loaded: true}
	obj := createCluster(t, d, "prod")
	_, err := serviceOver(t, d).Clusters().SetDefault(context.Background(), clusterIDOf(obj), true)
	require.NoError(t, err)

	d.kubeconfigSvc = fakeKubeconfigService{cfg: cfgWith("staging"), loaded: true}
	got, err := serviceOver(t, d).Clusters().SetDefault(context.Background(), clusterIDOf(obj), false)

	require.NoError(t, err)
	assert.False(t, got.Spec.IsDefault)
}

// A disabled cluster cannot become the default, and nothing changes.
func TestClustersSetDefaultRefusesADisabledCluster(t *testing.T) {
	d, _ := newClusterStatusDeps(t)
	d.kubeconfigSvc = fakeKubeconfigService{cfg: cfgWith("prod"), loaded: true}
	svc := serviceOver(t, d)
	obj := createCluster(t, d, "prod")
	_, err := svc.Clusters().SetEnabled(context.Background(), clusterIDOf(obj), false)
	require.NoError(t, err)

	_, err = svc.Clusters().SetDefault(context.Background(), clusterIDOf(obj), true)

	require.ErrorIs(t, err, ErrCannotBeDefault)
}

// A store that will not answer is reported, whichever read fails: the row's or the
// runtime object's.
func TestClustersSetDefaultReportsAReadThatFailed(t *testing.T) {
	t.Run("row", func(t *testing.T) {
		d := newTestDeps(t)
		obj := createCluster(t, d, "prod")
		_, err := d.db.Write.Exec(`ALTER TABLE clusters RENAME COLUMN name TO display_name`)
		require.NoError(t, err)

		_, err = serviceOver(t, d).Clusters().SetDefault(context.Background(), clusterIDOf(obj), true)
		require.Error(t, err)
		require.NotErrorIs(t, err, ErrNotFound)
	})
	t.Run("runtime object", func(t *testing.T) {
		d, closeStore := newTestDepsWithABreakableStore(t)
		obj := createCluster(t, d, "prod")
		closeStore()

		_, err := serviceOver(t, d).Clusters().SetDefault(context.Background(), clusterIDOf(obj), true)
		require.Error(t, err)
		require.NotErrorIs(t, err, ErrNotFound)
	})
}

// An unknown id is the boundary's not-found sentinel, as for every toggle.
func TestClustersSetDefaultReportsAnUnknownID(t *testing.T) {
	_, err := serviceOver(t, newTestDeps(t)).Clusters().SetDefault(context.Background(), ClusterID("404"), true)

	assert.ErrorIs(t, err, ErrNotFound)
}

// A setter wakes the watchers, so a toggle reaches every window.
func TestClustersSettersNotifyTheWatchers(t *testing.T) {
	d := newTestDeps(t)
	obj := createCluster(t, d, "prod")
	sub := d.db.ListenForChanges(appdb.KeyClusters)
	defer sub.Close()

	_, err := serviceOver(t, d).Clusters().SetSyncEnabled(context.Background(), clusterIDOf(obj), false)
	require.NoError(t, err)

	testutil.Recv(t, sub.Chan(), "the clusters signal")
}

// Delete marks the row and wakes the loops behind it; the row stays, marked, until
// they are done, and the runtime object is theirs to tear down.
func TestClustersDelete(t *testing.T) {
	d := newTestDeps(t)
	obj := createCluster(t, d, "prod")
	clusters := serviceOver(t, d).Clusters()
	sub := d.db.ListenForChanges(appdb.KeyClusters)
	defer sub.Close()

	require.NoError(t, clusters.Delete(context.Background(), clusterIDOf(obj)))

	got, err := clusters.Get(context.Background(), clusterIDOf(obj))
	require.NoError(t, err)
	require.NotNil(t, got, "the row is still there until the teardown is done")
	assert.NotNil(t, got.DeletionRequestedAt, "wearing the mark Delete asked for")
	testutil.Recv(t, sub.Chan(), "the clusters signal")
	runtime, err := d.clusterClient.Get(context.Background(), obj.ID)
	require.NoError(t, err)
	assert.Nil(t, runtime.DeletionRequestedAt, "the runtime object is the mirror's to delete")
}

// A store that will not answer is reported, never read as a record already gone: the
// two are the same outcome to a caller that only deletes, and opposite ones to a
// caller whose own cascade runs on the answer.
func TestClustersDeleteReportsAReadThatFailed(t *testing.T) {
	d, closeStore := newTestDepsWithABreakableStore(t)
	obj := createCluster(t, d, "prod")
	clusters := serviceOver(t, d).Clusters()
	closeStore()

	assert.Error(t, clusters.Delete(context.Background(), clusterIDOf(obj)))
}

// The webview only offers Remove for an orphaned row, and the boundary enforces the
// same rule: deleting a record its source still declares would re-import it under a
// fresh id, so the mutation would report success and then undo itself.
func TestClustersDeleteRejectsARecordItsSourceDeclares(t *testing.T) {
	d, status := newClusterStatusDeps(t)
	d.kubeconfigSvc = fakeKubeconfigService{cfg: cfgWith("prod"), loaded: true}
	svc := serviceOver(t, d)
	obj := createCluster(t, d, "prod")
	observePresence(t, status, obj, true)

	err := svc.Clusters().Delete(context.Background(), clusterIDOf(obj))

	require.ErrorIs(t, err, ErrDeclaredBySource)
	got, _, err := getCluster(context.Background(), d.store.Stmts(), clusterIDOf(obj))
	require.NoError(t, err)
	assert.Nil(t, got.DeleteRequestedAt, "the row is untouched")
}

// The case the affordance exists for: the context left the file, so nothing will
// re-import the record and the local state is the user's to forget.
func TestClustersDeleteAcceptsAnOrphanedRecord(t *testing.T) {
	d, status := newClusterStatusDeps(t)
	svc := serviceOver(t, d)
	obj := createCluster(t, d, "prod")
	observePresence(t, status, obj, false)

	require.NoError(t, svc.Clusters().Delete(context.Background(), clusterIDOf(obj)))

	got, _, err := getCluster(context.Background(), d.store.Stmts(), clusterIDOf(obj))
	require.NoError(t, err)
	assert.NotNil(t, got.DeleteRequestedAt)
}

// A record from no source is nobody's to declare, so nothing re-creates it.
func TestClustersDeleteAcceptsARecordFromAnotherSource(t *testing.T) {
	d, _ := newClusterStatusDeps(t)
	svc := serviceOver(t, d)
	_, err := d.db.Write.Exec(`INSERT INTO clusters (id, source, created_at, updated_at) VALUES ('cloud-1', 'cloud', 0, 0)`)
	require.NoError(t, err)

	require.NoError(t, svc.Clusters().Delete(context.Background(), ClusterID("cloud-1")))

	got, _, err := getCluster(context.Background(), d.store.Stmts(), ClusterID("cloud-1"))
	require.NoError(t, err)
	assert.NotNil(t, got.DeleteRequestedAt)
}

// The window the guard exists for: discovery has just created the record and its first
// reconcile has not written status yet, so there is no observation to read. The
// kubeconfig still declares the context, and the webview renders such a record as
// orphaned — so its Remove button is live for exactly this window.
func TestClustersDeleteRejectsADeclaredRecordBeforeItsFirstObservation(t *testing.T) {
	d, _ := newClusterStatusDeps(t)
	d.kubeconfigSvc = fakeKubeconfigService{cfg: cfgWith("prod"), loaded: true}
	svc := serviceOver(t, d)
	obj := createCluster(t, d, "prod")

	err := svc.Clusters().Delete(context.Background(), clusterIDOf(obj))

	require.ErrorIs(t, err, ErrDeclaredBySource)
	got, _, err := getCluster(context.Background(), d.store.Stmts(), clusterIDOf(obj))
	require.NoError(t, err)
	assert.Nil(t, got.DeleteRequestedAt)
}

// The same record with the context gone from the file: no observation either, but
// nothing will re-import it, so the delete is the caller's to make.
func TestClustersDeleteAcceptsAnUndeclaredRecordBeforeItsFirstObservation(t *testing.T) {
	d, _ := newClusterStatusDeps(t)
	d.kubeconfigSvc = fakeKubeconfigService{cfg: cfgWith("staging"), loaded: true}
	svc := serviceOver(t, d)
	obj := createCluster(t, d, "prod")

	require.NoError(t, svc.Clusters().Delete(context.Background(), clusterIDOf(obj)))
}

// The file outranks the record's own observation, which is only a cached view of it: a
// context put back before the record re-observed must not be deletable.
func TestClustersDeleteRejectsAReturnedContextAheadOfTheObservation(t *testing.T) {
	d, status := newClusterStatusDeps(t)
	obj := createCluster(t, d, "prod")
	observePresence(t, status, obj, false)

	// The file put the context back; the record has not re-observed it yet.
	d.kubeconfigSvc = fakeKubeconfigService{cfg: cfgWith("prod"), loaded: true}

	err := serviceOver(t, d).Clusters().Delete(context.Background(), clusterIDOf(obj))
	require.ErrorIs(t, err, ErrDeclaredBySource)
}

// Nothing to read at all — no observation, and the file not yet read. Refusing is
// recoverable; allowing costs the record's id and the user's toggles.
func TestClustersDeleteRejectsWhenNothingCanConfirmTheContextIsGone(t *testing.T) {
	d, _ := newClusterStatusDeps(t)
	d.kubeconfigSvc = fakeKubeconfigService{}
	svc := serviceOver(t, d)
	obj := createCluster(t, d, "prod")

	err := svc.Clusters().Delete(context.Background(), clusterIDOf(obj))
	require.ErrorIs(t, err, ErrDeclaredBySource)
}

// The stored observation is what answers while the file is unread.
func TestClustersDeleteFallsBackToTheObservationWhileUnread(t *testing.T) {
	d, status := newClusterStatusDeps(t)
	obj := createCluster(t, d, "prod")
	observePresence(t, status, obj, false)
	d.kubeconfigSvc = fakeKubeconfigService{}

	require.NoError(t, serviceOver(t, d).Clusters().Delete(context.Background(), clusterIDOf(obj)))
}

// Deleting a record that is already marked or already gone is the outcome the caller
// asked for — and it is answered ahead of the source refusal, so a repeat of a delete
// that went through is not refused by a context that has since returned.
func TestClustersDeleteIsIdempotent(t *testing.T) {
	d := newTestDeps(t)
	obj := createCluster(t, d, "prod")
	clusters := serviceOver(t, d).Clusters()
	require.NoError(t, clusters.Delete(context.Background(), clusterIDOf(obj)))
	d.kubeconfigSvc = fakeKubeconfigService{cfg: cfgWith("prod"), loaded: true}

	assert.NoError(t, clusters.Delete(context.Background(), clusterIDOf(obj)), "a second delete")
	assert.NoError(t, clusters.Delete(context.Background(), ClusterID("404")), "an id that never existed")
}

// --- importKubeconfigContexts ---

// rowsByContext returns the rows keyed by the context each references.
func rowsByContext(t *testing.T, d deps) map[string]ClusterRow {
	t.Helper()
	rows, err := listClusters(context.Background(), d.store.Stmts())
	require.NoError(t, err)
	byContext := map[string]ClusterRow{}
	for _, row := range rows {
		byContext[*row.SourceKey] = row
	}
	return byContext
}

func importContexts(t *testing.T, d deps, cfg *api.Config) error {
	t.Helper()
	return importKubeconfigContexts(context.Background(), d.db, d.store, cfg, t0)
}

func TestImportCreatesOneRowPerContext(t *testing.T) {
	d := newTestDeps(t)
	sub := d.db.ListenForChanges(appdb.KeyClusters)
	defer sub.Close()

	require.NoError(t, importContexts(t, d, cfgWith("prod", "staging")))

	rows := rowsByContext(t, d)
	require.Len(t, rows, 2)
	assert.True(t, rows["prod"].Enabled)
	assert.True(t, rows["prod"].SyncEnabled)
	assert.False(t, rows["prod"].MonitoringEnabled)
	testutil.Recv(t, sub.Chan(), "the clusters signal")
}

// A second pass over the same file changes nothing and wakes nobody; a context that
// returns finds its row, toggles and all.
func TestImportIsIdempotentAndKeepsTheRow(t *testing.T) {
	d := newTestDeps(t)
	require.NoError(t, importContexts(t, d, cfgWith("prod")))
	first := rowsByContext(t, d)["prod"]
	_, err := setClusterToggle(context.Background(), d.store.Stmts(), stmtSetClusterEnabled, first.ID, false, t0)
	require.NoError(t, err)
	sub := d.db.ListenForChanges(appdb.KeyClusters)
	defer sub.Close()

	require.NoError(t, importContexts(t, d, cfgWith("prod")))
	require.NoError(t, importContexts(t, d, cfgWith()))
	require.NoError(t, importContexts(t, d, cfgWith("prod")))

	rows := rowsByContext(t, d)
	require.Len(t, rows, 1)
	assert.Equal(t, first.ID, rows["prod"].ID, "the row survives the context leaving and returning")
	assert.False(t, rows["prod"].Enabled, "with the user's toggle")
	select {
	case <-sub.Chan():
		t.Fatal("a pass that inserted nothing must not wake the watchers")
	default:
	}
}

// A marked row keeps its claim, so the context is not re-created mid-teardown.
func TestImportSkipsAMarkedRow(t *testing.T) {
	d := newTestDeps(t)
	require.NoError(t, importContexts(t, d, cfgWith("prod")))
	row := rowsByContext(t, d)["prod"]
	_, err := markCluster(context.Background(), d.store.Stmts(), row.ID, t0)
	require.NoError(t, err)

	require.NoError(t, importContexts(t, d, cfgWith("prod")))

	rows, err := listClusters(context.Background(), d.store.Stmts())
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, row.ID, rows[0].ID)
}

// A store that will not write fails the pass, which beehive's backoff retries.
func TestImportReportsAFailedWrite(t *testing.T) {
	d := newTestDeps(t)
	require.NoError(t, d.db.Close())

	assert.Error(t, importContexts(t, d, cfgWith("prod")))
}

// The observation reads the kubeconfig service rather than the object, so nothing
// about a file change reaches this record on its own. The edge onto the anchor is the
// whole wake path: without it a departed context stays marked present.
func TestReconcileDependsOnItsSource(t *testing.T) {
	c := controllerOver(t)
	obj := createCluster(t, c.deps, "prod")
	src, err := c.sourceClient.GetByName(context.Background(), ClusterSourceNameKubeconfig)
	require.NoError(t, err)

	client := &stubControllerClient{}
	reconcileCluster(t, c, client, obj)

	assert.Contains(t, client.dependsOn, src.ID)
}

// A record from another source has no kubeconfig anchor to wake it, and claiming one
// would put it on a pass that could never say anything about it.
func TestReconcileDeclaresNoSourceEdgeForAnotherSource(t *testing.T) {
	c := controllerOver(t)
	obj, err := c.clusterClient.Create(context.Background(), "cloud/prod", ClusterRuntimeSpec{Source: "cloud"})
	require.NoError(t, err)

	client := &stubControllerClient{}
	reconcileCluster(t, c, client, obj)

	assert.Empty(t, client.dependsOn, "no source anchor and no identity to depend on")
}

// A failed edge must fail the pass. Carrying on would write the observation once and
// then never be woken to write it again.
func TestReconcileReportsAFailedSourceEdge(t *testing.T) {
	c := controllerOver(t)
	obj := createCluster(t, c.deps, "prod")

	boom := errors.New("boom")
	res := c.Reconcile(context.Background(), &stubControllerClient{dependErr: boom}, obj)

	assert.Equal(t, fmt.Errorf("depend cluster %d on its source: %w", obj.ID, boom), res.Err())
}

// beehive heads service.parts and dispatches its startup pass asynchronously, so a
// record stored by a previous process can reach a reconcile before the bootstrap has
// created the anchors. Failing there would drop every stored record into backoff on
// every boot and skip the observation with it, so the window is waited out instead.
func TestReconcileWaitsForAMissingSourceAnchor(t *testing.T) {
	d := newTestDeps(t)
	obj := createCluster(t, d, "prod")
	d.sourceClient = &stubSourceClient{getErr: beehive.ErrNotFound}
	c := &clusterController{deps: d}

	client := &stubControllerClient{}
	res := c.Reconcile(context.Background(), client, obj)

	assert.Equal(t, beehive.Unsettled().RequeueAfter(startupRequeue), res)
	assert.Nil(t, client.updated, "nothing observed until the record can be woken again")
}

// Any other store failure is a real one: the record would otherwise write an
// observation it can never be woken to refresh.
func TestReconcileReportsAFailedSourceLookup(t *testing.T) {
	d := newTestDeps(t)
	obj := createCluster(t, d, "prod")
	boom := errors.New("boom")
	d.sourceClient = &stubSourceClient{getErr: boom}
	c := &clusterController{deps: d}

	res := c.Reconcile(context.Background(), &stubControllerClient{}, obj)

	assert.Equal(t, fmt.Errorf("read kubeconfig cluster source: %w", boom), res.Err())
}

// observePresence writes the observation Delete gates on, the way a reconcile does.
func observePresence(t *testing.T, status *beehive.AdminClient[ClusterStatus], obj *beehive.Object[ClusterRuntimeSpec, ClusterStatus], present bool) {
	t.Helper()
	require.NoError(t, status.UpdateStatus(context.Background(), obj.ID, ClusterStatus{
		Source: ClusterStatusSource{Kubeconfig: &ClusterStatusSourceKubeconfig{
			Cluster:   ClusterStatusSourceKubeconfigCluster{Name: "prod-cluster"},
			User:      ClusterStatusSourceKubeconfigUser{Name: "prod-user"},
			IsPresent: present,
		}},
	}))
}

// The sort key falls back through what a row has: a display name, else the context it
// came from, else its id — which is what a source with no natural name leaves, and
// ordering by nothing would reshuffle the list between reads.
func TestClusterSortKeyFallsBackToTheRowsID(t *testing.T) {
	assert.Equal(t, "row-id", clusterSortKey(ClusterRow{ID: "row-id", Source: "cloud"}))
	assert.Equal(t, "prod", clusterSortKey(ClusterRow{ID: "row-id", SourceKey: new("prod")}))
	assert.Equal(t, "Prod", clusterSortKey(ClusterRow{ID: "row-id", SourceKey: new("prod"), Name: new("Prod")}))
}

// A marked row is closed to edits: a switch landing on one already going is reported
// as not found rather than silently doing nothing.
func TestSettingAClusterSwitchReportsARecordAlreadyGoing(t *testing.T) {
	ctx := context.Background()
	d := newTestDeps(t)
	cluster := createCluster(t, d, "prod")
	require.NoError(t, serviceOver(t, d).Clusters().Delete(ctx, clusterIDOf(cluster)))

	_, err := serviceOver(t, d).Clusters().SetEnabled(ctx, clusterIDOf(cluster), false)

	assert.ErrorIs(t, err, ErrNotFound)
}

// A runtime store that dies under the watch ends it with the reason: the client
// renders a dead watch rather than a list that stopped moving. Driven through
// Watch(id), which filters the list watch and forwards its ending.
func TestClustersWatchCollectionByIDReportsAStorageFault(t *testing.T) {
	d, closeStore := newTestDepsWithABreakableStore(t)
	obj := createCluster(t, d, "prod")
	stream := watchCluster(t, d, clusterIDOf(obj))
	awaitBookmark(t, stream)

	closeStore()
	d.db.Notify(appdb.KeyClusters)

	testutil.WaitClosed(t, stream.Frames, "the frames to close")
	assert.Error(t, stream.Err())
}

// A setter's write lands and then the runtime read fails: the fault is reported, not
// a record with a status nothing read.
func TestClustersSettersReportARuntimeReadThatFailed(t *testing.T) {
	d, closeStore := newTestDepsWithABreakableStore(t)
	obj := createCluster(t, d, "prod")
	closeStore()

	_, err := serviceOver(t, d).Clusters().SetEnabled(context.Background(), clusterIDOf(obj), false)

	assert.Error(t, err)
}

// The watch's endings on the database side: one closed before the first read fails
// the stream with the reason; one closed under the stream ends it cleanly — the
// subscription ends with the file, which is a teardown, not a fault.
func TestClustersWatchCollectionEndings(t *testing.T) {
	t.Run("database closed before the first read", func(t *testing.T) {
		d := newRunningDeps(t)
		require.NoError(t, d.db.Close())

		stream, err := serviceOver(t, d).Clusters().WatchCollection(t.Context())
		require.NoError(t, err)

		testutil.WaitClosed(t, stream.Frames, "the frames to close")
		assert.Error(t, stream.Err())
	})
	t.Run("database closed under the stream", func(t *testing.T) {
		d := newRunningDeps(t)
		stream := watchClusters(t, d)
		awaitBookmark(t, stream)

		require.NoError(t, d.db.Close())

		testutil.WaitClosed(t, stream.Frames, "the frames to close")
		assert.NoError(t, stream.Err())
	})
	t.Run("runtime stopped under the stream", func(t *testing.T) {
		bh := newTestBeehive(t)
		registerCacheStandIn(t, bh)
		stop, err := bh.Start(context.Background())
		require.NoError(t, err)
		d := newTestDepsOver(t, bh)
		stream := watchClusters(t, d)
		awaitBookmark(t, stream)

		require.NoError(t, stop(context.Background()))

		testutil.WaitClosed(t, stream.Frames, "the frames to close")
	})
}

// A consumer gone before the snapshot is through ends the pump without a report: the
// send blocks on the unread channel and the cancel is what frees it. One frame is
// read first, so the read behind the snapshot has finished and the cancel lands on a
// send.
func TestClustersWatchCollectionStopsForAConsumerThatIsGone(t *testing.T) {
	d := newRunningDeps(t)
	for _, name := range []string{"a", "b", "c", "d"} {
		createCluster(t, d, name)
	}
	ctx, cancel := context.WithCancel(context.Background())

	stream, err := serviceOver(t, d).Clusters().WatchCollection(ctx)
	require.NoError(t, err)
	testutil.Recv(t, stream.Frames, "the first frame")
	cancel()

	testutil.WaitClosed(t, stream.Frames, "the frames to close")
	assert.NoError(t, stream.Err())
}

func TestKubeContextIsTheKubeconfigsContext(t *testing.T) {
	c := &Cluster{Spec: ClusterSpec{Source: ClusterSpecSource{Kubeconfig: &ClusterSpecSourceKubeconfig{Context: "prod-admin"}}}}
	assert.Equal(t, "prod-admin", c.KubeContext())
	assert.Empty(t, (&Cluster{}).KubeContext(), "a record of another source")
}

// --- ReadActive ---

// readActive runs ReadActive on id and answers every reading it handed read.
func readActive(t *testing.T, d deps, id ClusterID) ([]ActiveCluster, error) {
	t.Helper()
	var handed []ActiveCluster
	err := serviceOver(t, d).Clusters().ReadActive(context.Background(), id, func(_ context.Context, a ActiveCluster) error {
		handed = append(handed, a)
		return nil
	})
	return handed, err
}

func TestReadActiveHandsTheRecordAndItsActiveCache(t *testing.T) {
	d, status := newClusterStatusDeps(t)
	obj := storedCluster(t, d, status, true, "uid-2")
	createCache(t, d.cacheClient, obj, "uid-1")
	active := createCache(t, d.cacheClient, obj, "uid-2")

	handed, err := readActive(t, d, clusterIDOf(obj))

	require.NoError(t, err)
	require.Len(t, handed, 1)
	assert.Equal(t, clusterIDOf(obj), handed[0].Cluster.ID)
	assert.Equal(t, "uid-2", *handed[0].Cluster.Status.Server.UID)
	require.NotNil(t, handed[0].Cache)
	assert.Equal(t, ClusterCacheID(active.ID), handed[0].Cache.ID)
}

func TestReadActiveOfANeverIdentifiedClusterHasNoCache(t *testing.T) {
	d := newTestDeps(t)
	obj := createCluster(t, d, "prod")

	handed, err := readActive(t, d, clusterIDOf(obj))

	require.NoError(t, err)
	require.Len(t, handed, 1)
	assert.Equal(t, clusterIDOf(obj), handed[0].Cluster.ID)
	assert.Nil(t, handed[0].Cache)
}

func TestReadActiveOfAnIdentityWithNoCacheYetHasNoCache(t *testing.T) {
	d, status := newClusterStatusDeps(t)
	obj := storedCluster(t, d, status, true, "uid-1")

	handed, err := readActive(t, d, clusterIDOf(obj))

	require.NoError(t, err)
	require.Len(t, handed, 1)
	assert.Nil(t, handed[0].Cache)
}

func TestReadActiveSkipsACacheOfAnotherIdentity(t *testing.T) {
	d, status := newClusterStatusDeps(t)
	obj := storedCluster(t, d, status, true, "uid-2")
	createCache(t, d.cacheClient, obj, "uid-1")

	handed, err := readActive(t, d, clusterIDOf(obj))

	require.NoError(t, err)
	require.Len(t, handed, 1)
	assert.Nil(t, handed[0].Cache, "the cache of the previous identity")
}

func TestReadActiveReturnsTheReadsError(t *testing.T) {
	d := newTestDeps(t)
	obj := createCluster(t, d, "prod")
	boom := errors.New("boom")

	err := serviceOver(t, d).Clusters().ReadActive(context.Background(), clusterIDOf(obj), func(context.Context, ActiveCluster) error {
		return boom
	})

	assert.ErrorIs(t, err, boom)
}

// moveIdentity has each read move the cluster to the next of uids, so the recheck after
// it finds the identity moved.
func moveIdentity(t *testing.T, status *beehive.AdminClient[ClusterStatus], obj *beehive.Object[ClusterRuntimeSpec, ClusterStatus], handed *[]ActiveCluster, uids ...string) func(context.Context, ActiveCluster) error {
	t.Helper()
	return func(ctx context.Context, a ActiveCluster) error {
		*handed = append(*handed, a)
		if len(*handed) <= len(uids) {
			uid := uids[len(*handed)-1]
			require.NoError(t, status.UpdateStatus(ctx, obj.ID, ClusterStatus{Server: ClusterServer{UID: &uid}}))
		}
		return nil
	}
}

func TestReadActiveRunsTheReadAgainWhenTheIdentityMoves(t *testing.T) {
	d, status := newClusterStatusDeps(t)
	obj := storedCluster(t, d, status, true, "uid-1")
	createCache(t, d.cacheClient, obj, "uid-1")
	createCache(t, d.cacheClient, obj, "uid-2")
	var handed []ActiveCluster

	err := serviceOver(t, d).Clusters().ReadActive(context.Background(), clusterIDOf(obj), moveIdentity(t, status, obj, &handed, "uid-2"))

	require.NoError(t, err)
	require.Len(t, handed, 2)
	assert.Equal(t, "uid-1", handed[0].Cache.Spec.ServerUID)
	assert.Equal(t, "uid-2", handed[1].Cache.Spec.ServerUID, "the retry reads the identity the cluster moved to")
}

func TestReadActiveIsIdentityMovedWhenItMovesTwice(t *testing.T) {
	d, status := newClusterStatusDeps(t)
	obj := storedCluster(t, d, status, true, "uid-1")
	var handed []ActiveCluster

	err := serviceOver(t, d).Clusters().ReadActive(context.Background(), clusterIDOf(obj), moveIdentity(t, status, obj, &handed, "uid-2", "uid-1"))

	require.ErrorIs(t, err, ErrIdentityMoved)
	assert.Len(t, handed, 2)
}

func TestReadActiveOfAGoneClusterIsNotFound(t *testing.T) {
	t.Run("on the first read", func(t *testing.T) {
		d := newTestDeps(t)

		handed, err := readActive(t, d, ClusterID("404"))

		require.ErrorIs(t, err, ErrNotFound)
		assert.Empty(t, handed)
	})
	t.Run("on the second", func(t *testing.T) {
		d := newTestDeps(t)
		obj := createCluster(t, d, "prod")

		err := serviceOver(t, d).Clusters().ReadActive(context.Background(), clusterIDOf(obj), func(ctx context.Context, _ ActiveCluster) error {
			_, err := d.db.Write.ExecContext(ctx, `DELETE FROM clusters WHERE id = ?`, clusterIDOf(obj))
			return err
		})

		require.ErrorIs(t, err, ErrNotFound)
	})
}

func TestReadActiveReturnsAFailedRecordRead(t *testing.T) {
	d := newTestDeps(t)
	obj := createCluster(t, d, "prod")
	require.NoError(t, d.db.Close())

	handed, err := readActive(t, d, clusterIDOf(obj))

	require.Error(t, err)
	assert.Empty(t, handed)
}

// A consumer gone mid-diff ends the pump at the frame it left on, without a report. One
// write renames three rows, so one reread diffs three frames: the test reads the first,
// the stream's buffer takes the second, and the third waits on a consumer that left.
func TestClustersWatchCollectionStopsMidDiffForAConsumerThatIsGone(t *testing.T) {
	d := newRunningDeps(t)
	for _, name := range []string{"a", "b", "c"} {
		createCluster(t, d, name)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stream, err := serviceOver(t, d).Clusters().WatchCollection(ctx)
	require.NoError(t, err)
	awaitBookmark(t, stream)

	_, err = d.db.Write.Exec(`UPDATE clusters SET name = 'renamed'`)
	require.NoError(t, err)
	d.db.Notify(appdb.KeyClusters)
	assert.Equal(t, CollectionWatchFrameModified, testutil.Recv(t, stream.Frames, "the first rename").Type)
	cancel()

	testutil.Wait(t, stream.done, "the pump to end")
	assert.NoError(t, stream.Err())
}

// The schedule state watch's consumer can leave before its first reading is taken, and the
// state watch ends there rather than parking on the send. Nothing reads until the claim is
// released, which the state watch does only once the send has given up.
func TestClustersWatchScheduleStateStopsForAConsumerThatIsGone(t *testing.T) {
	d := newTestDeps(t)
	obj := createCluster(t, d, "prod")
	d.kubeconnSvc = knowing(kubeconn.State{Connection: scheduledAt(probedAt)})
	pool := d.kubeconnSvc.(*fakeKubeconn)
	pool.releases = testutil.NewProbe[string](1)
	ch, cancel := watchSchedule(t, d, clusterIDOf(obj))

	cancel()

	assert.Equal(t, "prod", pool.releases.Await(t, "the claim's release"))
	_, open := <-ch
	assert.False(t, open, "the reading nobody took")
}

// A pool that shuts its feed down ends the state watch, which has nothing left to report.
func TestClustersWatchScheduleStateEndsWithThePoolsFeed(t *testing.T) {
	d := newTestDeps(t)
	obj := createCluster(t, d, "prod")
	pool := d.kubeconnSvc.(*fakeKubeconn)
	ch, _ := watchSchedule(t, d, clusterIDOf(obj))

	pool.stateFeed().Close()

	testutil.WaitClosed(t, ch, "the state watch to end with the feed")
	assert.Equal(t, []string{"prod"}, pool.released)
}

// A mark that will not write is reported, never answered as a delete that went through.
func TestClustersDeleteReportsAMarkThatFailed(t *testing.T) {
	d := newTestDeps(t)
	obj := createCluster(t, d, "prod")
	_, err := d.db.Write.Exec(`CREATE TRIGGER refuse_mark BEFORE UPDATE ON clusters BEGIN SELECT RAISE(ABORT, 'refused'); END`)
	require.NoError(t, err)

	err = serviceOver(t, d).Clusters().Delete(context.Background(), clusterIDOf(obj))

	assert.ErrorContains(t, err, "refused")
}

// failingOwnedCaches is the cache client with ListOwnedObjects answering err; everything
// else is the real client's.
type failingOwnedCaches struct {
	beehive.Client[ClusterCacheSpec, ClusterCacheStatus]
	err error
}

func (c failingOwnedCaches) ListOwnedObjects(context.Context, beehive.ObjectID, ...beehive.LoadOption) ([]*beehive.Object[ClusterCacheSpec, ClusterCacheStatus], error) {
	return nil, c.err
}

// A reading of an identified cluster whose caches will not list fails rather than
// handing the read a cluster with no cache.
func TestReadActiveReturnsAFailedCacheList(t *testing.T) {
	d, status := newClusterStatusDeps(t)
	obj := storedCluster(t, d, status, true, "uid-1")
	createCache(t, d.cacheClient, obj, "uid-1")
	boom := errors.New("store down")
	d.cacheClient = failingOwnedCaches{Client: d.cacheClient, err: boom}

	handed, err := readActive(t, d, clusterIDOf(obj))

	require.ErrorIs(t, err, boom)
	assert.Empty(t, handed)
}

// The runtime store is the second half of the record, and a reading over one that will
// not answer fails too.
func TestReadActiveReturnsAFailedRuntimeRead(t *testing.T) {
	d, closeStore := newTestDepsWithABreakableStore(t)
	obj := createCluster(t, d, "prod")
	closeStore()

	handed, err := readActive(t, d, clusterIDOf(obj))

	require.ErrorContains(t, err, "runtime object")
	assert.Empty(t, handed)
}
