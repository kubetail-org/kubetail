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
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/amorey/beehive"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd/api"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/kubeconfig"
)

// stubSourceClient answers the two calls a poke makes, recording what it requeued.
// The embedded interface is nil: anything else panics rather than passing silently.
type stubSourceClient struct {
	beehive.Client[ClusterSourceSpec, ClusterSourceStatus]
	id        beehive.ObjectID
	getErr    error
	createErr error
	asked     []string
	requeued  []beehive.ObjectID
	requeueEr error
}

func (c *stubSourceClient) GetOrCreate(_ context.Context, name string, _ ClusterSourceSpec, _ ...beehive.Option) (*beehive.Object[ClusterSourceSpec, ClusterSourceStatus], bool, error) {
	if c.createErr != nil {
		return nil, false, c.createErr
	}
	return &beehive.Object[ClusterSourceSpec, ClusterSourceStatus]{ID: c.id, Name: name}, true, nil
}

func (c *stubSourceClient) GetByName(_ context.Context, name string, _ ...beehive.LoadOption) (*beehive.Object[ClusterSourceSpec, ClusterSourceStatus], error) {
	c.asked = append(c.asked, name)
	if c.getErr != nil {
		return nil, c.getErr
	}
	return &beehive.Object[ClusterSourceSpec, ClusterSourceStatus]{ID: c.id, Name: name}, nil
}

func (c *stubSourceClient) Requeue(_ context.Context, id beehive.ObjectID, _ ...beehive.RequeueOption) error {
	c.requeued = append(c.requeued, id)
	return c.requeueEr
}

// --- ensureClusterSources ---

func TestEnsureClusterSourcesCreatesTheKubeconfigAnchor(t *testing.T) {
	t.Parallel()

	d := newTestDeps(t)

	obj, err := d.sourceClient.GetByName(context.Background(), ClusterSourceNameKubeconfig)
	require.NoError(t, err, "newTestDeps runs the bootstrap")
	assert.Equal(t, ClusterSourceNameKubeconfig, obj.Name)
}

// The bootstrap runs on every start, so a store carried over from a previous process
// must not gain a second anchor — its dependents' edges point at the first.
func TestEnsureClusterSourcesIsIdempotent(t *testing.T) {
	t.Parallel()

	d := newTestDeps(t)
	ctx := context.Background()

	first, err := d.sourceClient.GetByName(ctx, ClusterSourceNameKubeconfig)
	require.NoError(t, err)
	require.NoError(t, ensureClusterSources(ctx, d.sourceClient))

	second, err := d.sourceClient.GetByName(ctx, ClusterSourceNameKubeconfig)
	require.NoError(t, err)
	assert.Equal(t, first.ID, second.ID)
}

// --- kubeconfigFingerprint ---

// fingerprintOf digests cfg, failing the test rather than threading the error the
// marshal cannot actually produce for this shape.
func fingerprintOf(t *testing.T, cfg *api.Config) string {
	t.Helper()
	fp, err := kubeconfigFingerprint(cfg)
	require.NoError(t, err)
	return fp
}

// The digest is the wake signal, so it has to move for every field the observation
// folds in and stay put for everything else.
func TestKubeconfigFingerprint(t *testing.T) {
	t.Parallel()

	base := fingerprintOf(t, cfgCurrent("prod", "prod", "staging"))

	assert.Equal(t, base, fingerprintOf(t, cfgCurrent("prod", "staging", "prod")),
		"context order is a map's, not the file's")
	assert.NotEqual(t, base, fingerprintOf(t, cfgCurrent("staging", "prod", "staging")),
		"the current context decides IsDefault")
	assert.NotEqual(t, base, fingerprintOf(t, cfgCurrent("prod", "prod")),
		"a departed context")
	assert.NotEqual(t, base, fingerprintOf(t, cfgCurrent("prod", "prod", "staging", "dev")),
		"a new context")
}

func TestKubeconfigFingerprintTracksTheClusterAndUserEntries(t *testing.T) {
	t.Parallel()

	cfg := cfgWith("prod")
	base := fingerprintOf(t, cfg)

	cfg.Contexts["prod"].Cluster = "other-cluster"
	assert.NotEqual(t, base, fingerprintOf(t, cfg))

	cfg.Contexts["prod"].Cluster = "prod-cluster"
	cfg.Contexts["prod"].AuthInfo = "other-user"
	assert.NotEqual(t, base, fingerprintOf(t, cfg))
}

// JSON delimits its own values, so a boundary cannot be moved without changing the
// digest.
func TestKubeconfigFingerprintSeparatesAdjacentValues(t *testing.T) {
	t.Parallel()

	a := &api.Config{Contexts: map[string]*api.Context{"ab": {Cluster: "c"}}}
	b := &api.Config{Contexts: map[string]*api.Context{"a": {Cluster: "bc"}}}

	assert.NotEqual(t, fingerprintOf(t, a), fingerprintOf(t, b))
}

// Editing what a context resolves through leaves every entry name untouched, so the
// observation cannot see it. The credentials fold is what makes it wake anything: the
// record it reaches relays the change to the probe that would otherwise dial the old
// server until its next poll.
func TestKubeconfigFingerprintMovesWhenCredentialsChange(t *testing.T) {
	t.Parallel()

	cfg := cfgWith("prod")
	cfg.AuthInfos = map[string]*api.AuthInfo{"prod-user": {Token: "one"}}
	base := fingerprintOf(t, cfg)
	baseFold := observedFold(t, cfg)

	cfg.AuthInfos["prod-user"] = &api.AuthInfo{Token: "two"}

	assert.Equal(t, baseFold, observedFold(t, cfg), "the observation cannot tell them apart")
	assert.NotEqual(t, base, fingerprintOf(t, cfg), "the credentials fold can")
}

// The digest covers the whole snapshot, so an edit no record observes still wakes them
// all. The cost the coarse digest is chosen for: every one of those passes compares,
// finds nothing moved, and settles.
func TestKubeconfigFingerprintMovesForWhatNoRecordObserves(t *testing.T) {
	t.Parallel()

	cfg := cfgWith("prod")
	base := fingerprintOf(t, cfg)
	baseFold := observedFold(t, cfg)

	// An entry no context references, and a preferences block nothing here reads.
	cfg.Clusters["unreferenced"] = &api.Cluster{Server: "https://example"}
	cfg.Preferences = api.Preferences{Colors: true}

	assert.Equal(t, baseFold, observedFold(t, cfg), "no record observes any of it")
	assert.NotEqual(t, base, fingerprintOf(t, cfg), "and the digest moves anyway")
}

// Whatever a record can observe, the digest must move for — the guarantee that makes it
// a wake signal. It holds by covering everything: the folds read a subset of the
// snapshot, so a change either of them can see is a change to the bytes hashed here.
func TestKubeconfigFingerprintFollowsTheObservation(t *testing.T) {
	t.Parallel()

	same := func(a, b *api.Config) bool {
		return observedFold(t, a) == observedFold(t, b) &&
			(fingerprintOf(t, a) == fingerprintOf(t, b))
	}

	base := cfgCurrent("prod", "prod", "staging")
	renamedCluster := cfgCurrent("prod", "prod", "staging")
	renamedCluster.Contexts["prod"].Cluster = "other"

	assert.True(t, same(base, cfgCurrent("prod", "prod", "staging")), "identical folds")
	assert.NotEqual(t, observedFold(t, base), observedFold(t, renamedCluster))
	assert.NotEqual(t, fingerprintOf(t, base), fingerprintOf(t, renamedCluster),
		"the digest moves with the fold, never independently of it")
}

// observedFold renders what every record would observe, as the digest's own input
// does — the value the fingerprint must be a function of. JSON, not %+v: the
// observation holds a pointer, whose address says nothing about the fold.
func observedFold(t *testing.T, cfg *api.Config) string {
	t.Helper()
	var b strings.Builder
	for _, name := range []string{"dev", "prod", "staging"} {
		observed, err := json.Marshal(observeKubeconfig(cfg, name, nil))
		require.NoError(t, err)
		fmt.Fprintf(&b, "%s=%s;", name, observed)
	}
	return b.String()
}

// --- Reconcile ---

// sourceControllerOver returns a controller over d, and the anchor object a reconcile
// would be handed.
func sourceControllerOver(t *testing.T, d deps) (*clusterSourceController, *beehive.Object[ClusterSourceSpec, ClusterSourceStatus]) {
	t.Helper()
	obj, err := d.sourceClient.GetByName(context.Background(), ClusterSourceNameKubeconfig)
	require.NoError(t, err)
	return &clusterSourceController{deps: d}, obj
}

// stubSourceController captures what the pass writes.
type stubSourceController struct {
	beehive.ControllerClient[ClusterSourceStatus]
	updated   *ClusterSourceStatus
	updateErr error
}

func (c *stubSourceController) UpdateStatus(_ context.Context, status ClusterSourceStatus) error {
	c.updated = &status
	return c.updateErr
}

// Only the kubeconfig has an anchor; a row of another source has none to wake.
func TestClusterSourceNameFor(t *testing.T) {
	t.Parallel()

	name, ok := clusterSourceNameFor(SourceKubeconfig)
	assert.True(t, ok)
	assert.Equal(t, ClusterSourceNameKubeconfig, name)

	_, ok = clusterSourceNameFor("cloud")
	assert.False(t, ok)
}

// A snapshot that will not digest fails the pass: no fingerprint means nothing to
// publish, and settling would leave every record unwoken.
func TestSourceReconcileFailsOnASnapshotThatWillNotDigest(t *testing.T) {
	t.Parallel()

	d := newTestDeps(t)
	cfg := cfgWith("prod")
	cfg.Extensions = map[string]runtime.Object{"yaml": &runtime.Unknown{ContentType: "application/yaml"}}
	d.kubeconfigSvc = fakeKubeconfigService{cfg: cfg, loaded: true}
	c, obj := sourceControllerOver(t, d)

	client := &stubSourceController{}
	res := c.Reconcile(context.Background(), client, obj)

	assert.ErrorContains(t, res.Err(), "digest kubeconfig")
	assert.Nil(t, client.updated)
}

// A row the import cannot insert fails the import and names the context it was for.
func TestImportReportsAContextItCannotInsert(t *testing.T) {
	t.Parallel()

	d := newTestDeps(t)
	_, err := d.db.Write.Exec(`CREATE TRIGGER refuse_import BEFORE INSERT ON clusters BEGIN SELECT RAISE(ABORT, 'refused'); END`)
	require.NoError(t, err)

	err = importKubeconfigContexts(context.Background(), d.db, d.store, cfgWith("prod"), t0)

	assert.ErrorContains(t, err, `importing context "prod"`)
	assert.ErrorContains(t, err, "refused")
}

// The pass creates the records and publishes what it observed for them to wake on.
func TestSourceReconcileImportsAndPublishes(t *testing.T) {
	t.Parallel()

	d := newTestDeps(t)
	d.kubeconfigSvc = fakeKubeconfigService{cfg: cfgWith("prod", "staging"), loaded: true}
	c, obj := sourceControllerOver(t, d)

	client := &stubSourceController{}
	res := c.Reconcile(context.Background(), client, obj)

	assert.Len(t, rowsByContext(t, d), 2)
	require.NotNil(t, client.updated)
	assert.Equal(t, fingerprintOf(t, cfgWith("prod", "staging")), client.updated.Fingerprint)
	assert.Equal(t, beehive.Settled(), res, "the registered resync paces the next pass, not the result")
}

// The status write is what wakes every Cluster, so an unchanged snapshot must not
// produce one — otherwise every record reconciles on every backstop tick.
func TestSourceReconcileWritesNothingWhenTheSnapshotIsUnchanged(t *testing.T) {
	t.Parallel()

	d := newTestDeps(t)
	cfg := cfgWith("prod")
	d.kubeconfigSvc = fakeKubeconfigService{cfg: cfg, loaded: true}
	c, obj := sourceControllerOver(t, d)

	obj.Status = &ClusterSourceStatus{Fingerprint: fingerprintOf(t, cfg)}
	client := &stubSourceController{}
	res := c.Reconcile(context.Background(), client, obj)

	assert.Nil(t, client.updated, "nothing a dependent would observe moved")
	assert.Equal(t, beehive.Settled(), res, "but the pass still settles")
}

// A failed create is retried against the snapshot that failed, so the create pass has
// to run ahead of the fingerprint gate — behind it, the retry would be skipped as a
// no-op and that context would never be imported.
func TestSourceReconcileImportsEvenWhenTheSnapshotIsUnchanged(t *testing.T) {
	t.Parallel()

	d := newTestDeps(t)
	cfg := cfgWith("prod")
	d.kubeconfigSvc = fakeKubeconfigService{cfg: cfg, loaded: true}
	c, obj := sourceControllerOver(t, d)

	obj.Status = &ClusterSourceStatus{Fingerprint: fingerprintOf(t, cfg)}
	res := c.Reconcile(context.Background(), &stubSourceController{}, obj)

	require.Equal(t, beehive.Settled(), res)
	assert.Contains(t, rowsByContext(t, d), "prod")
}

// The pre-read config is an empty one, so importing against it would fingerprint an
// empty set and wake every record to observe its context absent.
func TestSourceReconcileDefersUntilTheKubeconfigIsRead(t *testing.T) {
	t.Parallel()

	d := newTestDeps(t)
	d.kubeconfigSvc = fakeKubeconfigService{}
	c, obj := sourceControllerOver(t, d)

	client := &stubSourceController{}
	res := c.Reconcile(context.Background(), client, obj)

	assert.Nil(t, client.updated)
	assert.Empty(t, rowsByContext(t, d), "nothing imported against a config nobody read")
	assert.Equal(t, beehive.Unsettled().RequeueAfter(startupRequeue), res)
}

// The publish is the only thing that wakes a dependent, and the fingerprint says what
// the file holds rather than which records landed — so a context that cannot be
// imported must not hold back the observation for every other record. A context that
// departed in the same edit is reachable no other way.
//
// The error still comes back, so beehive's backoff retries the stuck create, and the
// create pass runs ahead of the fingerprint gate so the retry is not skipped as a
// no-op (TestSourceReconcileImportsEvenWhenTheSnapshotIsUnchanged).
func TestSourceReconcilePublishesDespiteAFailedImport(t *testing.T) {
	t.Parallel()

	d := newTestDeps(t)
	cfg := cfgWith("prod")
	d.kubeconfigSvc = fakeKubeconfigService{cfg: cfg, loaded: true}
	require.NoError(t, d.db.Close())
	c, obj := sourceControllerOver(t, d)

	client := &stubSourceController{}
	res := c.Reconcile(context.Background(), client, obj)

	assert.Error(t, res.Err())
	require.NotNil(t, client.updated, "and the wake still has to reach every other record")
	assert.Equal(t, fingerprintOf(t, cfg), client.updated.Fingerprint)
}

// The settle path carries the create failure out too, or a pass whose observation was
// already current would report success and drop the retry.
func TestSourceReconcileReportsAFailedImportOnAnUnchangedSnapshot(t *testing.T) {
	t.Parallel()

	d := newTestDeps(t)
	cfg := cfgWith("prod")
	d.kubeconfigSvc = fakeKubeconfigService{cfg: cfg, loaded: true}
	require.NoError(t, d.db.Close())
	c, obj := sourceControllerOver(t, d)

	obj.Status = &ClusterSourceStatus{Fingerprint: fingerprintOf(t, cfg)}
	res := c.Reconcile(context.Background(), &stubSourceController{}, obj)

	assert.Error(t, res.Err())
}

// An anchor on its way out has no set to maintain.
func TestSourceReconcileSkipsADeletingAnchor(t *testing.T) {
	t.Parallel()

	d := newTestDeps(t)
	d.kubeconfigSvc = fakeKubeconfigService{cfg: cfgWith("prod"), loaded: true}
	c, obj := sourceControllerOver(t, d)

	now := time.Now()
	obj.DeletionRequestedAt = &now
	client := &stubSourceController{}
	res := c.Reconcile(context.Background(), client, obj)

	assert.Equal(t, beehive.Settled(), res)
	assert.Nil(t, client.updated)
	assert.Empty(t, rowsByContext(t, d))
}

// fakeKubeconfigService hands the pass a snapshot without a file behind it.
// WatchConfigState is unused here — the trigger is what watches.
type fakeKubeconfigService struct {
	cfg    *api.Config
	loaded bool
	// restErr is what RESTConfig reports; nil resolves to an empty config, which is all
	// a caller that does not dial can tell apart.
	restErr error
}

func (f fakeKubeconfigService) Get() (*api.Config, bool) { return f.cfg, f.loaded }

func (f fakeKubeconfigService) WatchConfigState() kubeconfig.ConfigStateWatch {
	panic("the source controller does not subscribe")
}

func (f fakeKubeconfigService) RESTConfig(contextName string) (*rest.Config, string, error) {
	if f.restErr != nil {
		return nil, "", f.restErr
	}
	return &rest.Config{}, contextName, nil
}

// --- the wake, over a real beehive ---

// The one test here that runs the whole chain: a file on disk, the trigger, the
// anchor's pass, beehive's dependency waker, and the per-record observation. Every
// other test in this package stubs the ControllerClient, so AddDependency is recorded
// rather than exercised — point it at the wrong object and they all still pass while
// nothing is ever woken.
//
// Departure is what it asserts, because that is the case reachable no other way: a
// context removed from the file appears in no snapshot the create pass walks, so only
// the anchor's status write and the edge behind it can reach the record. Getting there
// covers the rest of the chain on the way — the record has to be imported and observed
// present before it can be observed absent.

// converge bounds the wait: a file watcher, beehive's queue, and the waker in between.
// Generous because it fails only when the chain is broken; a working one settles in
// milliseconds.
const converge = 10 * time.Second

// writeKubeconfig writes a kubeconfig naming one context per entry, each against its
// own cluster and user, with the first as the current context.
func writeKubeconfig(t *testing.T, path string, contexts ...string) {
	t.Helper()
	var b strings.Builder
	b.WriteString("apiVersion: v1\nkind: Config\n")
	fmt.Fprintf(&b, "current-context: %s\n", contexts[0])
	b.WriteString("clusters:\n")
	for _, name := range contexts {
		fmt.Fprintf(&b, "- name: %s-cluster\n  cluster:\n    server: https://%s.example\n", name, name)
	}
	b.WriteString("users:\n")
	for _, name := range contexts {
		fmt.Fprintf(&b, "- name: %s-user\n  user: {}\n", name)
	}
	b.WriteString("contexts:\n")
	for _, name := range contexts {
		fmt.Fprintf(&b, "- name: %s\n  context:\n    cluster: %s-cluster\n    user: %s-user\n", name, name, name)
	}
	require.NoError(t, os.WriteFile(path, []byte(b.String()), 0o600))
}

// startService boots the real thing over a kubeconfig the test can rewrite.
func startService(t *testing.T, path string) Service {
	t.Helper()
	cfgSvc := kubeconfig.New(path, nil)
	stopCfg, err := cfgSvc.Start(context.Background())
	require.NoError(t, err)
	t.Cleanup(func() {
		assert.NoError(t, stopCfg(context.Background()))
		assert.NoError(t, cfgSvc.Close())
	})

	svc, err := New(openTestDB(t), testPaths(t), cfgSvc, nil)
	require.NoError(t, err)

	stop, err := svc.Start(context.Background())
	require.NoError(t, err)
	t.Cleanup(func() {
		assert.NoError(t, stop(context.Background()))
		assert.NoError(t, svc.Close())
	})
	return svc
}

// storedCluster returns the record referencing contextName, or nil while there is none.
func storedClusterFor(t *testing.T, svc Service, contextName string) *Cluster {
	t.Helper()
	objs, err := svc.Clusters().List(context.Background())
	require.NoError(t, err)
	for _, c := range objs {
		if src := c.Spec.Source.Kubeconfig; src != nil && src.Context == contextName {
			return c
		}
	}
	return nil
}

// observedPresent reports whether contextName's record has been observed, and whether
// that observation says the context is still in the file.
func observedPresent(t *testing.T, svc Service, contextName string) (observed, present bool) {
	t.Helper()
	c := storedClusterFor(t, svc, contextName)
	if c == nil || c.Status.Source.Kubeconfig == nil {
		return false, false
	}
	return true, c.Status.Source.Kubeconfig.IsPresent
}

func TestSourceWakesADepartedContextsRecord(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "config")
	writeKubeconfig(t, path, "prod", "staging")
	svc := startService(t, path)

	require.Eventually(t, func() bool {
		observed, present := observedPresent(t, svc, "staging")
		return observed && present
	}, converge, 10*time.Millisecond, "imported and observed present first")

	writeKubeconfig(t, path, "prod")

	require.Eventually(t, func() bool {
		observed, present := observedPresent(t, svc, "staging")
		return observed && !present
	}, converge, 10*time.Millisecond, "the departed context observed absent")

	orphan := storedClusterFor(t, svc, "staging")
	require.NotNil(t, orphan)
	assert.Equal(t, "staging-cluster", orphan.Status.Source.Kubeconfig.Cluster.Name,
		"an orphaned record keeps its last-known names, or the row goes nameless")
	assert.True(t, orphan.Spec.Enabled, "and the user's toggles, since the context may come back")
}

// The fingerprint is what wakes every dependent record, so a pass that cannot store it
// fails rather than settling: settling would leave the observation unpublished and no
// record would ever re-read the kubeconfig this pass just saw change.
func TestSourceReconcileFailsWhenTheFingerprintWillNotStore(t *testing.T) {
	t.Parallel()

	d := newTestDeps(t)
	d.kubeconfigSvc = fakeKubeconfigService{cfg: cfgWith("prod"), loaded: true}
	c, obj := sourceControllerOver(t, d)

	res := c.Reconcile(context.Background(), &stubSourceController{updateErr: assert.AnError}, obj)

	assert.NotEqual(t, beehive.Settled(), res)
}
