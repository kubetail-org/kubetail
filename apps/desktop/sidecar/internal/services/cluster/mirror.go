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

// The mirror: one runtime Cluster object per clusters row, named by the row's id
// and carrying the row's runtime spec. It is the only writer of that spec. It creates
// the object behind a new row, follows a toggle into the spec, tears the object down
// behind a marked row, and removes the row once the teardown and the chat sweep are
// done.
package cluster

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/amorey/beehive"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/appdb"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/drain"
)

// mirrorResyncInterval is the mirror's backstop pass, for a signal that went missing.
const mirrorResyncInterval = 10 * time.Minute

// mirrorRewatchDelay paces the reopening of a runtime watch that could not be reopened.
const mirrorRewatchDelay = time.Second

// mirrorPassRetry paces a pass after one that failed: no signal follows a failure,
// since the row is already marked and no object change comes of a delete that did
// not happen.
const mirrorPassRetry = 5 * time.Second

type runtimeChanges = <-chan beehive.ObjectChange[ClusterRuntimeSpec, ClusterStatus]

// clusterMirror runs the passes. A pass is a full reconcile of rows against objects,
// so a wake carries nothing and a burst of them coalesces to one pass.
type clusterMirror struct {
	deps
	resync  time.Duration
	rewatch time.Duration
	retry   time.Duration
	// passed reports every finished pass, for a test that waits on one; nil otherwise.
	passed chan struct{}
	wg     sync.WaitGroup
}

func newClusterMirror(d deps, resync, rewatch, retry time.Duration) *clusterMirror {
	return &clusterMirror{deps: d, resync: resync, rewatch: rewatch, retry: retry}
}

// Start subscribes to the rows and the runtime objects, runs the first pass, and
// keeps passing on every signal and every resync tick until stopped. Both
// subscriptions are taken before the first pass, so a change landing under it is
// passed again rather than missed. The runtime watch matters for one change: a
// collected object, which is what lets a marked row go.
func (m *clusterMirror) Start(ctx context.Context) (func(context.Context) error, error) {
	loopCtx, stopLoop := context.WithCancel(context.Background())
	rows := m.db.ListenForChanges(appdb.KeyClusters)
	objs, err := m.clusterClient.WatchList(loopCtx)
	if err != nil {
		stopLoop()
		rows.Close()
		return nil, fmt.Errorf("watch cluster runtime objects: %w", err)
	}

	m.wg.Go(func() {
		defer rows.Close()
		ticker := time.NewTicker(m.resync)
		defer ticker.Stop()
		retryPass := time.NewTimer(m.retry)
		retryPass.Stop()
		defer retryPass.Stop()
		pass := func() {
			if !m.pass(loopCtx) {
				retryPass.Reset(m.retry)
			}
		}
		pass()
		// changes is the runtime watch, nil while it is down; rewatch is then the
		// timer to reopen it on. The rows and the resync drive passes either way.
		changes := objs.Changes
		var rewatch <-chan time.Time
		for {
			select {
			case <-loopCtx.Done():
				return
			case _, ok := <-rows.Chan():
				if !ok {
					return
				}
			case change, ok := <-changes:
				if !ok {
					// Beehive ends a watch that fell below its log's retention; a fresh
					// subscription is the answer. A collection landing in the gap is
					// what the pass after the reopen is for.
					changes, rewatch = m.reopenWatch(loopCtx)
					if changes == nil {
						continue
					}
				} else if change.Type != beehive.Deleted {
					continue
				}
			case <-rewatch:
				changes, rewatch = m.reopenWatch(loopCtx)
				if changes == nil {
					continue
				}
			case <-retryPass.C:
			case <-ticker.C:
			}
			pass()
		}
	})
	return func(ctx context.Context) error {
		stopLoop()
		return drain.WithContext(ctx, m.wg.Wait)
	}, nil
}

// reopenWatch opens a fresh runtime watch and hands back its changes, or nil and the
// timer to try again on.
func (m *clusterMirror) reopenWatch(ctx context.Context) (runtimeChanges, <-chan time.Time) {
	objs, err := m.clusterClient.WatchList(ctx)
	if err != nil {
		if ctx.Err() == nil {
			slog.Warn("cluster mirror could not reopen its runtime watch; will retry", "err", err)
		}
		return nil, time.After(m.rewatch)
	}
	return objs.Changes, nil
}

// Close has nothing to release; the statements are the service's.
func (m *clusterMirror) Close() error { return nil }

// pass runs one reconcile and reports whether it succeeded, logging what failed.
func (m *clusterMirror) pass(ctx context.Context) bool {
	err := m.reconcile(ctx)
	if err != nil && ctx.Err() == nil {
		slog.Warn("cluster mirror pass failed; will retry", "err", err)
	}
	if m.passed != nil {
		select {
		case m.passed <- struct{}{}:
		case <-ctx.Done():
		}
	}
	return err == nil
}

// reconcile brings every row's runtime object in line with the row. A row that fails
// does not hold the rest: its error is joined into the result and the pass goes on.
func (m *clusterMirror) reconcile(ctx context.Context) error {
	rows, err := listClusters(ctx, m.store.Stmts())
	if err != nil {
		return err
	}
	objs, err := m.clusterClient.List(ctx)
	if err != nil {
		return fmt.Errorf("list cluster runtime objects: %w", err)
	}
	byID := make(map[ClusterID]*beehive.Object[ClusterRuntimeSpec, ClusterStatus], len(objs))
	for _, obj := range objs {
		byID[clusterIDOf(obj)] = obj
	}

	var errs []error
	for _, row := range rows {
		if err := m.reconcileRow(ctx, row, byID[row.ID]); err != nil {
			errs = append(errs, err)
		}
		delete(byID, row.ID)
	}
	// What is left has no row: an object of a row that went while this process was
	// down, or that a crash left behind. Absence is idempotent success.
	for id := range byID {
		if err := m.clusterClient.DeleteByName(ctx, string(id)); err != nil {
			errs = append(errs, fmt.Errorf("delete orphan runtime object %s: %w", id, err))
		}
	}
	return errors.Join(errs...)
}

// reconcileRow brings one row's runtime object in line with the row. An unmarked row
// gets an object carrying its runtime spec — a write beehive suppresses when the
// bytes match, so a pass over an unchanged row wakes no controller. A marked row's
// object is asked to go, and the row goes once it has: with the cache finalizer, an
// absent object proves every cache file is gone.
func (m *clusterMirror) reconcileRow(ctx context.Context, row ClusterRow, obj *beehive.Object[ClusterRuntimeSpec, ClusterStatus]) error {
	switch {
	case row.DeleteRequestedAt == nil:
		// A deleting object under an unmarked row is a teardown already under way;
		// the row cannot be unmarked, so it is not recreated over.
		if obj != nil && obj.DeletionRequestedAt != nil {
			return nil
		}
		if _, _, err := m.clusterClient.CreateOrUpdate(ctx, string(row.ID), runtimeSpecOf(row)); err != nil {
			return fmt.Errorf("mirror cluster %s: %w", row.ID, err)
		}
	case obj == nil:
		// A chat still filed under the row holds it; the chat sweeper's signal brings
		// this loop back when the last one goes.
		deleted, err := deleteMarkedCluster(ctx, m.store.Stmts(), row.ID)
		if err != nil {
			return err
		}
		if deleted {
			m.db.Notify(appdb.KeyClusters)
			// The importer's insert is a no-op against a marked row, so a context that
			// came back mid-teardown has no row now; its source's pass is what gives it
			// one. A latency hint, as every requeue is: the source's own resync covers
			// a wake that could not be sent.
			m.wakeSource(ctx, row.Source)
		}
	case obj.DeletionRequestedAt == nil:
		// The collection wakes this loop through the runtime watch.
		if err := m.clusterClient.DeleteByName(ctx, string(row.ID)); err != nil {
			return fmt.Errorf("delete cluster %s runtime object: %w", row.ID, err)
		}
	}
	return nil
}

// wakeSource requeues the anchor that imports rows of source.
func (m *clusterMirror) wakeSource(ctx context.Context, source string) {
	name, ok := clusterSourceNameFor(source)
	if !ok {
		return
	}
	anchor, err := m.sourceClient.GetByName(ctx, name)
	if err == nil {
		err = m.sourceClient.Requeue(ctx, anchor.ID)
	}
	if err != nil && ctx.Err() == nil {
		slog.Warn("cluster mirror could not wake the cluster source after a removal", "source", source, "err", err)
	}
}
