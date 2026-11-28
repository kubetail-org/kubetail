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

package bash

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/apimeta"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/cluster"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/cluster/clustercard"
)

// fakeService is the cluster service as bash reads it: each record by id, and
// the list, from a map, or err. Every other method panics on the nil embedded
// service.
type fakeService struct {
	cluster.Service
	clusters map[apimeta.ClusterID]*cluster.Cluster
	err      error
	// lease is what AcquireConnection hands out, or acquireErr why it refuses.
	lease      *fakeLease
	acquireErr error
	// frames is what the record's watch sends, the watch failing once it is
	// closed; nil sends nothing. watchErr is why Watch refuses.
	frames   chan cluster.ClusterCollectionWatchFrame
	watchErr error
}

func (f fakeService) AcquireConnection(context.Context, apimeta.ClusterID) (cluster.Lease, error) {
	if f.acquireErr != nil {
		return nil, f.acquireErr
	}
	if f.lease == nil {
		panic("no lease to acquire")
	}
	return f.lease, nil
}

func (f fakeService) Clusters() cluster.Clusters { return fakeClusters{f: f} }

type fakeClusters struct {
	cluster.Clusters
	f fakeService
}

func (fc fakeClusters) Get(_ context.Context, id apimeta.ClusterID) (*cluster.Cluster, error) {
	return fc.f.clusters[id], fc.f.err
}

func (fc fakeClusters) WatchCollectionByID(ctx context.Context, _ apimeta.ClusterID) (*cluster.Stream[cluster.ClusterCollectionWatchFrame], error) {
	if fc.f.watchErr != nil {
		return nil, fc.f.watchErr
	}
	return cluster.NewStream(ctx, func(ctx context.Context, out chan<- cluster.ClusterCollectionWatchFrame) error {
		for {
			select {
			case <-ctx.Done():
				return nil
			case f, ok := <-fc.f.frames:
				if !ok {
					return errWatch
				}
				select {
				case out <- f:
				case <-ctx.Done():
					return nil
				}
			}
		}
	}), nil
}

// errWatch is a record's watch that failed.
var errWatch = errors.New("watch failed")

func (fc fakeClusters) List(context.Context) ([]*cluster.Cluster, error) {
	var list []*cluster.Cluster
	for id, c := range fc.f.clusters {
		c.ID = id
		list = append(list, c)
	}
	return list, fc.f.err
}

// kubeCluster is a record from the kubeconfig's context, with the server uid.
func kubeCluster(context, uid string) *cluster.Cluster {
	c := &cluster.Cluster{Spec: cluster.ClusterSpec{
		Source: cluster.ClusterSpecSource{Kubeconfig: &cluster.ClusterSpecSourceKubeconfig{Context: context}},
	}}
	if uid != "" {
		c.Status.Server.UID = &uid
	}
	return c
}

// targetTool is a tool over clusters, their kubectl caches in a directory of
// the test's own.
func targetTool(t *testing.T, clusters map[apimeta.ClusterID]*cluster.Cluster) *Tool {
	t.Helper()
	return &Tool{clusterSvc: fakeService{clusters: clusters}, kubectlDir: t.TempDir()}
}

// The target is the card's context, read off the cluster's record, and the
// kubectl cache of the server the record serves.
func TestTheTargetIsTheRecordsContextAndServer(t *testing.T) {
	t.Parallel()

	c := kubeCluster("prod-admin", "uid-1")
	tl := targetTool(t, map[apimeta.ClusterID]*cluster.Cluster{"7": c})

	got, err := tl.target(t.Context(), "7", "")
	require.NoError(t, err)

	assert.Equal(t, clustercard.ContextName(c), got.context)
	assert.Equal(t, filepath.Join(tl.kubectlDir, "7", serverKey("uid-1")), got.cacheDir)
	assert.DirExists(t, got.cacheDir)
	assert.Equal(t, "uid-1", got.serverUID, "the claim names the identity the cache is keyed on")
}

// A target for one server is refused once the record names another, before any
// kubectl cache is made: a monitor run reads the server its incident opened on.
func TestATargetForAnotherServerIsRefused(t *testing.T) {
	tl := targetTool(t, map[apimeta.ClusterID]*cluster.Cluster{"7": kubeCluster("prod", "uid-2")})

	_, err := tl.target(t.Context(), "7", "uid-1")
	assert.ErrorIs(t, err, cluster.ErrIdentityMismatch)
	assert.NoDirExists(t, filepath.Join(tl.kubectlDir, "7"))

	got, err := tl.target(t.Context(), "7", "uid-2")
	require.NoError(t, err)
	assert.Equal(t, "uid-2", got.serverUID)
}

// A cluster never identified has no server UID to claim by.
func TestAnUnidentifiedTargetHasNoServerUID(t *testing.T) {
	t.Parallel()

	tl := targetTool(t, map[apimeta.ClusterID]*cluster.Cluster{"7": kubeCluster("prod", "")})

	got, err := tl.target(t.Context(), "7", "")
	require.NoError(t, err)

	assert.Empty(t, got.serverUID)
}

// A target whose kubectl cache cannot be made has none, and says why.
func TestATargetWithNoCacheFails(t *testing.T) {
	t.Parallel()

	tl := targetTool(t, map[apimeta.ClusterID]*cluster.Cluster{"7": kubeCluster("prod", "uid-1")})
	file := filepath.Join(t.TempDir(), "file")
	require.NoError(t, os.WriteFile(file, nil, 0o600))
	tl.kubectlDir = filepath.Join(file, "kubectl")

	_, err := tl.target(t.Context(), "7", "")

	assert.Error(t, err)
}

// A record that names no kube-context has none on its card, and the run's one
// context still needs a name.
func TestARecordWithNoContextNamesTheRunsOwn(t *testing.T) {
	t.Parallel()

	c := &cluster.Cluster{}
	tl := targetTool(t, map[apimeta.ClusterID]*cluster.Cluster{"7": c})

	got, err := tl.target(t.Context(), "7", "")
	require.NoError(t, err)

	assert.Empty(t, clustercard.ContextName(c))
	assert.Equal(t, unnamedContext, got.context)
}

// A cluster that is gone or being deleted has no target, and no cache is made
// for it: a sandboxed kubectl aimed at nothing would read as the cluster being
// down. A record that cannot be read is an error of its own.
func TestAGoneClusterHasNoTarget(t *testing.T) {
	t.Parallel()

	marked := kubeCluster("prod", "uid")
	at := time.Now()
	marked.DeletionRequestedAt = &at
	for name, c := range map[string]struct {
		clusters map[apimeta.ClusterID]*cluster.Cluster
		err      error
		want     error
	}{
		"nil record": {want: errClusterGone},
		"marked":     {clusters: map[apimeta.ClusterID]*cluster.Cluster{"7": marked}, want: errClusterGone},
		"get error":  {err: errors.New("disk")},
	} {
		t.Run(name, func(t *testing.T) {
			tl := targetTool(t, c.clusters)
			tl.clusterSvc = fakeService{clusters: c.clusters, err: c.err}

			_, err := tl.target(t.Context(), "7", "")

			require.Error(t, err)
			if c.want != nil {
				assert.ErrorIs(t, err, c.want)
			} else {
				assert.NotErrorIs(t, err, errClusterGone)
			}
			assert.NoDirExists(t, filepath.Join(tl.kubectlDir, "7"))
		})
	}
}
