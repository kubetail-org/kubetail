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

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/apimeta"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/run/kubeproxy"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/cluster"
)

// claimed is what a run's grant reaches the cluster through, released when
// the run ends.
type claimed interface {
	kubeproxy.Upstream
	Close()
}

// claim claims the connection of cluster id for one run, by the server UID its
// target read ("" for none). It does not dial. A claim that cannot be made for
// a reason of the cluster's own still runs the command, since most commands
// never touch the cluster: every request of the run then answers why. A
// cluster gone since the target read it is errClusterGone.
//
// The claim watches the record, so a cluster disabled, marked for deletion or
// gone while the run lasts is refused from then on. moved, when set, is told of
// each connection refused for reaching another server.
func (t *Tool) claim(ctx context.Context, id apimeta.ClusterID, serverUID string, moved func()) (claimed, error) {
	if serverUID == "" {
		return refused{kubeproxy.ErrNotIdentified}, nil
	}
	lease, err := t.clusterSvc.AcquireConnection(ctx, id)
	switch {
	case errors.Is(err, cluster.ErrNotConnectable):
		return refused{kubeproxy.ErrNotConnectable}, nil
	case errors.Is(err, cluster.ErrNotFound):
		return nil, errClusterGone
	case err != nil:
		return nil, err
	}
	// The run can outlive the call that started it.
	watchCtx, stop := context.WithCancel(context.WithoutCancel(ctx))
	stream, err := t.clusterSvc.Clusters().WatchCollectionByID(watchCtx, id)
	if err != nil {
		stop()
		lease.Release()
		return nil, err
	}
	revoked := make(chan struct{})
	go watchRevoked(watchCtx, stream, revoked)
	return upstream{lease: lease, serverUID: serverUID, moved: moved, revoked: revoked, stop: stop}, nil
}

// watchRevoked closes revoked once stream shows the cluster Kubetail no longer
// connects to, or ends before ctx does, since a claim nothing watches could
// outlive either.
func watchRevoked(ctx context.Context, stream *cluster.Stream[cluster.ClusterCollectionWatchFrame], revoked chan<- struct{}) {
	for f := range stream.Frames {
		if f.Type == cluster.CollectionWatchFrameDeleted || f.Cluster != nil && !connectable(f.Cluster) {
			close(revoked)
			return
		}
	}
	if ctx.Err() == nil {
		close(revoked)
	}
}

// connectable is whether AcquireConnection would claim c: enabled, not marked
// for deletion, and from the kubeconfig.
func connectable(c *cluster.Cluster) bool {
	return c.Spec.Enabled && c.DeletionRequestedAt == nil && c.KubeContext() != ""
}

// refused is a claim that could not be made: every request answers err.
type refused struct{ err error }

func (r refused) Endpoint(context.Context) (kubeproxy.Endpoint, error) {
	return kubeproxy.Endpoint{}, r.err
}

func (refused) Close() {}

// upstream is the chat's cluster's connection, claimed for one run: the
// kubeproxy.Upstream its grant reaches, vouched for by the server UID the
// target read, so the cache and the claim name one identity.
type upstream struct {
	lease     cluster.Lease
	serverUID string
	// moved, when set, is told of a connection that reaches another server.
	moved func()
	// revoked closes once the record says Kubetail no longer connects to the
	// cluster.
	revoked <-chan struct{}
	// stop ends the record's watch.
	stop context.CancelFunc
}

// Endpoint is the connection ConnFor hands out for the claim's server UID, per
// request, so a rotated credential is picked up. It is done when the
// connection is retired or the claim revoked, whichever comes first.
func (u upstream) Endpoint(ctx context.Context) (kubeproxy.Endpoint, error) {
	select {
	case <-u.revoked:
		return kubeproxy.Endpoint{}, kubeproxy.ErrNotConnectable
	default:
	}
	conn, err := u.lease.ConnFor(ctx, u.serverUID)
	switch {
	case errors.Is(err, cluster.ErrIdentityMismatch):
		if u.moved != nil {
			u.moved()
		}
		return kubeproxy.Endpoint{}, kubeproxy.ErrIdentityMismatch
	case err != nil:
		return kubeproxy.Endpoint{}, err
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		select {
		case <-conn.Done(): // coverage:ignore only the cluster service builds a Connection that can be retired
		case <-u.revoked:
		case <-ctx.Done():
		}
	}()
	return kubeproxy.Endpoint{Base: conn.BaseURL, Client: conn.HTTPClient, Done: done}, nil
}

// Close ends the record's watch and releases the claim.
func (u upstream) Close() {
	u.stop()
	u.lease.Release()
}
