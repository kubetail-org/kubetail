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

// Package monitor watches each monitored cluster's cache for a short list of
// failures and records each as an incident in app.db. It calls no model.
package monitor

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/appdb"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/drain"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/cluster"
)

// sweepEvery is how often the loop deletes incidents past their retention.
const sweepEvery = time.Hour

// settings are the monitor's cadence, windows and read bounds, which tests shrink.
type settings struct {
	cadence           time.Duration
	windows           windows
	maxRows, maxBytes int
	now               func() time.Time
}

// Monitor reads each monitored cluster every cadence, and again when a clusters row
// changes, and folds what it finds into incidents.
type Monitor struct {
	db       *appdb.DB
	clusters cluster.Service
	store    *store
	reader   reader
	settings settings

	// The loop's own: each cluster's fold state and the server its last reading
	// mirrored, whether each listed cluster was monitored at the last tick, and when
	// it last swept.
	states    map[cluster.ClusterID]clusterState
	servers   map[cluster.ClusterID]string
	monitored map[cluster.ClusterID]bool
	swept     time.Time
	// started is when the monitor was built: the time an occurrence was last seen
	// before it is time the app was down.
	started time.Time

	cancel   context.CancelFunc
	wg       sync.WaitGroup
	stopOnce sync.Once
}

// New builds the monitor over the app's DB and the cluster service. Nothing runs
// until Start.
func New(db *appdb.DB, clusters cluster.Service) (*Monitor, error) {
	return newMonitor(db, clusters, settings{
		cadence:  30 * time.Second,
		windows:  defaultWindows(),
		maxRows:  50_000,
		maxBytes: 16 << 20,
		now:      time.Now,
	})
}

func newMonitor(db *appdb.DB, clusters cluster.Service, s settings) (*Monitor, error) {
	st, err := newStore(db)
	if err != nil {
		return nil, err
	}
	return &Monitor{
		db:        db,
		clusters:  clusters,
		store:     st,
		reader:    newReader(clusters.CachedData(), s.maxRows, s.maxBytes),
		settings:  s,
		started:   s.now(),
		states:    map[cluster.ClusterID]clusterState{},
		servers:   map[cluster.ClusterID]string{},
		monitored: map[cluster.ClusterID]bool{},
	}, nil
}

// Start runs the loop until the func it returns is called.
func (m *Monitor) Start(context.Context) (func(context.Context) error, error) {
	ctx, cancel := context.WithCancel(context.Background())
	m.cancel = cancel
	// Subscribed before the first reading, so a change after it is not missed.
	pings := m.db.ListenForChanges(appdb.KeyClusters)
	m.wg.Go(func() {
		defer pings.Close()
		ticker := time.NewTicker(m.settings.cadence)
		defer ticker.Stop()
		for {
			m.tick(ctx)
			select {
			case <-ctx.Done():
			case <-ticker.C:
			case <-pings.Chan():
			}
			// A tick or ping ready with the stop is not taken: its reads would fail.
			if ctx.Err() != nil {
				return
			}
		}
	})
	return m.stop, nil
}

func (m *Monitor) stop(ctx context.Context) error {
	m.stopOnce.Do(m.cancel)
	return drain.WithContext(ctx, m.wg.Wait)
}

// Close releases the statements. It runs after the loop has stopped.
func (m *Monitor) Close() error { return m.store.close() }

// tick reads every monitored cluster once, forgets the clusters gone from the list
// and sweeps when a sweep is due.
func (m *Monitor) tick(ctx context.Context) {
	list, err := m.clusters.Clusters().List(ctx)
	if err != nil {
		slog.Error("monitor: list clusters", "err", err)
		return
	}
	now := m.settings.now()
	listed := make(map[cluster.ClusterID]bool, len(list))
	for _, c := range list {
		listed[c.ID] = true
		m.tickCluster(ctx, c, now)
	}
	for id := range m.monitored {
		if !listed[id] {
			delete(m.monitored, id)
			m.forget(id)
		}
	}
	m.sweepIfDue(ctx, now)
}

// tickCluster reads c when it is monitored. Otherwise it ends c's open occurrences,
// once: when monitoring goes off, or at the first tick since the start.
func (m *Monitor) tickCluster(ctx context.Context, c *cluster.Cluster, now time.Time) {
	on := c.DeletionRequestedAt == nil && c.Spec.Enabled && c.Spec.MonitoringEnabled
	was, known := m.monitored[c.ID]
	if !on && (was || !known) {
		if err := m.store.endOpen(ctx, c.ID); err != nil {
			slog.Error("monitor: end a cluster's occurrences", "err", err)
			return
		}
		m.forget(c.ID)
	}
	m.monitored[c.ID] = on
	if on {
		m.readCluster(ctx, c.ID, now)
	}
}

// sweepIfDue sweeps once every sweepEvery. A failed sweep is due again at the next
// tick.
func (m *Monitor) sweepIfDue(ctx context.Context, now time.Time) {
	if now.Sub(m.swept) < sweepEvery {
		return
	}
	if err := m.store.sweep(ctx, now); err != nil {
		slog.Error("monitor: sweep incidents", "err", err)
		return
	}
	m.swept = now
}

// readCluster folds one reading of cluster id into its incidents. Memory moves only
// once the fold has committed. A cluster is seeded at its first reading since the
// start, which names the server whose occurrences it holds, and again when a reading
// names another server.
func (m *Monitor) readCluster(ctx context.Context, id cluster.ClusterID, now time.Time) {
	// No reading, or a failed one, folds as a reading that judges nothing.
	r, ok, err := m.reader.read(ctx, id)
	if err != nil {
		slog.Error("monitor: read a cluster", "err", err)
		r, ok = reading{}, false
	}
	sc := scope{cluster: id, server: m.servers[id]}
	state, seeded := m.states[id]
	if ok && r.server != sc.server {
		seeded, sc.server = false, r.server
	}
	if !seeded {
		if !ok {
			return
		}
		state, err = m.seed(ctx, sc, now)
		if err != nil {
			slog.Error("monitor: seed a cluster", "err", err)
			return
		}
	}
	next, c := fold(m.settings.windows, state, r, now)
	if err := m.store.apply(ctx, sc, c); err != nil {
		slog.Error("monitor: write a fold", "err", err)
		return
	}
	m.states[id], m.servers[id] = next, sc.server
}

// seed is sc's state from its open occurrences, once any other server's have ended.
func (m *Monitor) seed(ctx context.Context, sc scope, now time.Time) (clusterState, error) {
	var open []openOccurrence
	err := m.store.endOtherServers(ctx, sc)
	if err == nil {
		open, err = m.store.loadOpen(ctx, sc)
	}
	if err != nil {
		return nil, err
	}
	return seed(open, m.started, now), nil
}

// forget drops what the loop remembers of cluster id's readings.
func (m *Monitor) forget(id cluster.ClusterID) {
	delete(m.states, id)
	delete(m.servers, id)
}
