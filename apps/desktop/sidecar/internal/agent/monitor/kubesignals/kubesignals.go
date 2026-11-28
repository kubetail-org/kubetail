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

// Package kubesignals is the monitor's Kubernetes integration: a target per
// cluster, read from the cluster's cache. It is the one package that reads a
// target's key as a cluster's id.
package kubesignals

import (
	"context"
	"errors"
	"time"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/agent/monitor"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/appdb"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/apimeta"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/cluster"
)

// Name is the integration's name, as monitor_targets.integration holds it.
const Name = "kubernetes"

// cadence is how often a cluster is read.
const cadence = 30 * time.Second

// The signals, each named with the integration.
const (
	SignalCrashLoop           = Name + ".crash_loop"
	SignalImagePull           = Name + ".image_pull"
	SignalPodPending          = Name + ".pod_pending"
	SignalNodeNotReady        = Name + ".node_not_ready"
	SignalReplicasUnavailable = Name + ".replicas_unavailable"
	SignalRolloutStalled      = Name + ".rollout_stalled"
)

// resolveAfter is every signal's resolve-after window.
const resolveAfter = 5 * time.Minute

// Signals is each signal the integration can find.
var Signals = []monitor.Signal{
	{Name: SignalCrashLoop, Label: "Crash loop", OpenAfter: 2 * time.Minute, ResolveAfter: resolveAfter, Severity: monitor.SeverityWarning},
	{Name: SignalImagePull, Label: "Image pull failing", OpenAfter: 2 * time.Minute, ResolveAfter: resolveAfter, Severity: monitor.SeverityWarning},
	{Name: SignalPodPending, Label: "Pod pending", OpenAfter: 5 * time.Minute, ResolveAfter: resolveAfter, Severity: monitor.SeverityWarning},
	{Name: SignalNodeNotReady, Label: "Node not ready", OpenAfter: 2 * time.Minute, ResolveAfter: resolveAfter, Severity: monitor.SeverityCritical},
	{Name: SignalReplicasUnavailable, Label: "Replicas unavailable", OpenAfter: 5 * time.Minute, ResolveAfter: resolveAfter, Severity: monitor.SeverityWarning},
	{Name: SignalRolloutStalled, Label: "Rollout stalled", OpenAfter: 15 * time.Minute, ResolveAfter: resolveAfter, Severity: monitor.SeverityWarning},
}

// Register is the integration's registration, over the clusters in db and the
// cluster service that reads their caches.
func Register(db *appdb.DB, svc cluster.Service) monitor.Registration {
	return monitor.Registration{Name: Name, Cadence: cadence, Signals: Signals, Integration: &integration{db: db, svc: svc}}
}

// ClusterID is the cluster a target names: its key, for a Kubernetes target, and
// false for a target of another integration.
func ClusterID(integration, key string) (string, bool) {
	if integration != Name {
		return "", false
	}
	return key, true
}

// TargetKey is the key of a cluster's target, the inverse of ClusterID.
func TargetKey(id apimeta.ClusterID) string { return string(id) }

type integration struct {
	db  *appdb.DB
	svc cluster.Service
}

// ListTargets lists every clusters row, reachable or not: a cluster disabled or
// awaiting deletion stays listed, and goes only with its row.
func (k *integration) ListTargets(ctx context.Context) ([]monitor.Target, error) {
	clusters, err := k.svc.Clusters().List(ctx)
	if err != nil {
		return nil, err
	}
	targets := make([]monitor.Target, 0, len(clusters))
	for _, c := range clusters {
		targets = append(targets, monitor.Target{Key: TargetKey(c.ID), Label: cutLabel(clusterName(c))})
	}
	return targets, nil
}

// clusterName is the name the user set, else its context's, else its id.
func clusterName(c *cluster.Cluster) string {
	switch {
	case c.Spec.Name != nil && *c.Spec.Name != "":
		return *c.Spec.Name
	case c.Spec.Source.Kubeconfig != nil:
		return c.Spec.Source.Kubeconfig.Context
	}
	return string(c.ID)
}

// ListenForTargetChanges receives after each change to the clusters rows, with
// notices coalesced, until ctx ends.
func (k *integration) ListenForTargetChanges(ctx context.Context) <-chan struct{} {
	sub := k.db.ListenForChanges(appdb.KeyClusters)
	out := make(chan struct{}, 1)
	go func() {
		defer sub.Close()
		for {
			select {
			case <-ctx.Done():
				return
			case <-sub.Chan():
				select {
				case out <- struct{}{}:
				default: // coverage:ignore a notice still unread already says the listing may differ, and no test can hold the send
				}
			}
		}
	}()
	return out
}

// Read takes one reading of the cluster t names. A cluster disabled, awaiting
// deletion or with its sync off fails with disabled; one with no trusted cache
// yet with not_ready.
func (k *integration) Read(ctx context.Context, t monitor.Target) (monitor.Reading, error) {
	id := apimeta.ClusterID(t.Key)
	c, err := k.svc.Clusters().Get(ctx, id)
	switch {
	case err != nil:
		return monitor.Reading{}, err
	case c == nil:
		return monitor.Reading{}, &monitor.ReadError{Code: monitor.CodeNotReady, Err: cluster.ErrNotFound}
	case !c.Spec.Enabled || c.DeletionRequestedAt != nil || !c.Spec.SyncEnabled:
		return monitor.Reading{}, &monitor.ReadError{Code: monitor.CodeDisabled}
	}
	var r monitor.Reading
	err = k.svc.CachedData().ReadActive(ctx, id, "", maxRows, maxBytes, func(q cluster.CacheReading, active cluster.ActiveCluster) error {
		var err error
		r, err = read(q, active.Cache.Spec.ServerUID)
		return err
	})
	if errors.Is(err, cluster.ErrNoCache) || errors.Is(err, cluster.ErrSyncing) {
		return monitor.Reading{}, &monitor.ReadError{Code: monitor.CodeNotReady, Err: err}
	}
	if err != nil {
		return monitor.Reading{}, err
	}
	return r, nil
}
