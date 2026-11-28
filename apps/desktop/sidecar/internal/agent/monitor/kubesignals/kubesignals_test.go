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

package kubesignals

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/agent/monitor"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/appdb"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/appdb/appdbtest"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/testutil"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/cluster"
)

// The registration is one the monitor takes: every signal carries the prefix,
// and each opens and resolves after the windows the spec gives it.
func TestTheRegistrationIsOneTheMonitorTakes(t *testing.T) {
	db := appdbtest.Open(t)
	reg := Register(db, &fakeService{})
	assert.Equal(t, Name, reg.Name)
	assert.Equal(t, 30*time.Second, reg.Cadence)
	m, err := monitor.New(db, reg)
	require.NoError(t, err)
	require.NoError(t, m.Close())
	for _, s := range reg.Signals {
		assert.Equal(t, 5*time.Minute, s.ResolveAfter, s.Name)
	}
}

// kubesignals emits no name it did not register: a reading in which every
// signal holds names only registered signals, in its observations and unknowns.
func TestEveryEmittedSignalIsRegistered(t *testing.T) {
	objects := append(webDeployment(3, 2),
		pod("uid-1", "crash", nil, "Running", crashing("app")),
		pod("uid-2", "pull", nil, "Pending", ctr{name: "app", image: "app:1", waiting: "ImagePullBackOff"}),
		pod("uid-3", "pending", nil, "Pending"),
		node("worker-1", "False", "", ""),
	)
	r := readingOf(t, newFake(t, objects...))
	f := newFake(t)
	f.health = cluster.ClusterCacheHealth{Reason: "Watching"}
	unknown := readingOf(t, f)

	registered := map[string]bool{}
	for _, s := range Signals {
		registered[s.Name] = true
	}
	seen := map[string]bool{}
	for _, o := range r.Observations {
		assert.True(t, registered[o.Signal], o.Signal)
		seen[o.Signal] = true
	}
	for name := range unknown.Unknown {
		assert.True(t, registered[name], name)
	}
	assert.Len(t, seen, 5, "every signal but rollout_stalled holds")
	assert.Len(t, unknown.Unknown, len(Signals))
}

// Every clusters row is listed, its key the cluster's id and its label its name,
// its context's, or its id, bounded; a disabled or marked cluster stays listed.
func TestEveryClusterIsListed(t *testing.T) {
	marked := time.Now()
	f := &fakeService{clusters: []*cluster.Cluster{
		{ID: "c1", Spec: cluster.ClusterSpec{Name: new("prod")}},
		{ID: "c2", Spec: cluster.ClusterSpec{Source: cluster.ClusterSpecSource{Kubeconfig: &cluster.ClusterSpecSourceKubeconfig{Context: "kind-dev"}}}},
		{ID: "c3", DeletionRequestedAt: &marked},
		{ID: "c4", Spec: cluster.ClusterSpec{Name: new(strings.Repeat("x", 1000))}},
	}}

	got, err := (&integration{svc: f}).ListTargets(t.Context())

	require.NoError(t, err)
	assert.Equal(t, []monitor.Target{{Key: "c1", Label: "prod"}, {Key: "c2", Label: "kind-dev"}, {Key: "c3", Label: "c3"}, {Key: "c4", Label: strings.Repeat("x", maxLabelBytes)}}, got)
	f.getErr = errors.New("unreadable")
	_, err = (&integration{svc: f}).ListTargets(t.Context())
	assert.ErrorIs(t, err, f.getErr)
}

// A change to the clusters rows is a notice, until ctx ends; another key's is not.
func TestAChangeToTheClustersIsANotice(t *testing.T) {
	db := appdbtest.Open(t)
	ctx, cancel := context.WithCancel(t.Context())
	changed := (&integration{db: db}).ListenForTargetChanges(ctx)

	// One notice: several coalesce at each hop, so a second could still arrive.
	db.Notify(appdb.KeyClusters)
	testutil.Recv(t, changed, "the notice")
	db.Notify(appdb.KeyChats)
	// Bounded: another key's notice must not arrive as one.
	testutil.NoRecv(t, changed, 50*time.Millisecond, "another key")
	cancel()
}

// A cluster disabled, awaiting deletion or with its sync off fails its reading
// with disabled; one gone or with no trusted cache with not_ready; a failed read
// of the record or the cache is its error.
func TestAReadingRefusesWhatItCannotJudge(t *testing.T) {
	marked := time.Now()
	failed := errors.New("unreadable")
	for name, c := range map[string]struct {
		change func(f *fakeService)
		code   monitor.ReadErrorCode
		err    error
	}{
		"disabled":         {change: func(f *fakeService) { f.clusters[0].Spec.Enabled = false }, code: monitor.CodeDisabled},
		"marked":           {change: func(f *fakeService) { f.clusters[0].DeletionRequestedAt = &marked }, code: monitor.CodeDisabled},
		"sync off":         {change: func(f *fakeService) { f.clusters[0].Spec.SyncEnabled = false }, code: monitor.CodeDisabled},
		"gone":             {change: func(f *fakeService) { f.clusters = nil }, code: monitor.CodeNotReady},
		"no cache":         {change: func(f *fakeService) { f.readErr = cluster.ErrNoCache }, code: monitor.CodeNotReady},
		"syncing":          {change: func(f *fakeService) { f.health = cluster.ClusterCacheHealth{Reason: "Connecting"} }, code: monitor.CodeNotReady},
		"an unread record": {change: func(f *fakeService) { f.getErr = failed }, err: failed},
		"an unread cache":  {change: func(f *fakeService) { f.readErr = failed }, err: failed},
	} {
		t.Run(name, func(t *testing.T) {
			f := newFake(t)
			c.change(f)

			_, err := (&integration{svc: f}).Read(t.Context(), monitor.Target{Key: "c1"})

			require.Error(t, err)
			if c.err != nil {
				assert.ErrorIs(t, err, c.err)
				return
			}
			re, ok := errors.AsType[*monitor.ReadError](err)
			require.True(t, ok, err)
			assert.Equal(t, c.code, re.Code)
		})
	}
}

// ClusterID reads a Kubernetes target's key as its cluster's id, and answers
// nothing for a target of another integration; TargetKey is its inverse.
func TestClusterIDReadsAKubernetesKey(t *testing.T) {
	id, ok := ClusterID(Name, TargetKey("c1"))
	assert.True(t, ok)
	assert.Equal(t, "c1", id)
	_, ok = ClusterID("aws", "c1")
	assert.False(t, ok)
}

// A value is cut to fit its bound as JSON, at a rune boundary; a label to its
// bytes.
func TestTheCutsFitTheirBounds(t *testing.T) {
	assert.Equal(t, "short", cutDetail("short"))
	cut := cutDetail(strings.Repeat("é", 2000))
	assert.LessOrEqual(t, jsonLen(cut), maxDetailJSON)
	assert.True(t, strings.HasPrefix(strings.Repeat("é", 2000), cut))
	assert.Equal(t, strings.Repeat("é", maxLabelBytes/2), cutLabel(strings.Repeat("é", 1000)))
	assert.Equal(t, strings.Repeat("a", 255), cutLabel(strings.Repeat("a", 255)+"é"))
}

// BenchmarkAReadingOf5000Pods reads a cache of 5,000 Pods under 50 Deployments,
// a few of them failing.
func BenchmarkAReadingOf5000Pods(b *testing.B) {
	var objects []map[string]any
	for d := range 50 {
		name := fmt.Sprintf("app-%d", d)
		objects = append(objects,
			map[string]any{"apiVersion": "apps/v1", "kind": "Deployment", "metadata": meta("dep-"+name, name, nil),
				"spec": map[string]any{"replicas": int64(100)}, "status": map[string]any{"observedGeneration": int64(1), "readyReplicas": int64(100)}},
			map[string]any{"apiVersion": "apps/v1", "kind": "ReplicaSet", "metadata": meta("rs-"+name, name+"-1", controller("apps/v1", "Deployment", name, "dep-"+name))},
		)
		for p := range 100 {
			c := running("app")
			if p == 0 && d%10 == 0 {
				c = crashing("app")
			}
			objects = append(objects, pod(fmt.Sprintf("uid-%d-%d", d, p), fmt.Sprintf("%s-%d", name, p), controller("apps/v1", "ReplicaSet", name+"-1", "rs-"+name), "Running", c))
		}
	}
	f := newFake(b, objects...)
	k := &integration{svc: f}
	b.ResetTimer()
	for b.Loop() {
		r, err := k.Read(b.Context(), monitor.Target{Key: "c1"})
		if err != nil || len(r.Observations) != 5 {
			b.Fatal(err, len(r.Observations))
		}
	}
}
