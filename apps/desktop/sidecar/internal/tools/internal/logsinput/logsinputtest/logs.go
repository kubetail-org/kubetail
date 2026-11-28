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

package logsinputtest

import (
	"context"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/cluster"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/logs"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/logs/logstest"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/logs/logstore"
)

// Logs is a cluster whose containers' logs Server serves over HTTP, and the log
// service that reads them into a file of its own. The service keeps its own
// clock, time.Now; Server's lines are stamped from logstest's 10:00 on
// 2026-10-08, so a test's clock set between the two never runs ahead of a page's
// ResolvedAt.
type Logs struct {
	*Service
	Server *logstest.Server
	Logs   *logs.Service
}

// LogsOf is Logs over a mirror holding objects, its server uid-1.
func LogsOf(t *testing.T, objects ...map[string]any) *Logs {
	t.Helper()
	f := MirrorOf(t, objects...)
	// The service claims the cluster's log file by its server's uid, the one the
	// record's probe named.
	uid := "uid-1"
	f.Active.Cluster.Status.Server.UID = &uid
	f.Active.Cache.Spec.ServerUID = uid
	server := logstest.New()
	srv := httptest.NewUnstartedServer(server)
	srv.EnableHTTP2 = true
	srv.StartTLS()
	t.Cleanup(srv.Close)
	base, err := url.Parse(srv.URL)
	if err != nil { // coverage:ignore an httptest server's URL always parses
		t.Fatal(err)
	}
	f.Conn = &cluster.Connection{BaseURL: base, HTTPClient: srv.Client()}
	ret := logstore.DefaultRetention
	ret.Interval, ret.TailGrace, ret.WriteTick, ret.WriteWait = 0, 0, 5*time.Millisecond, 5*time.Millisecond
	svc := logs.New(f, t.TempDir(), ret)
	t.Cleanup(func() { assert.NoError(t, svc.Close()) })
	return &Logs{Service: f, Server: server, Logs: svc}
}

// Add appends lines to the log of prod/<pod>/<container>, each logstest.Stamped.
func (l *Logs) Add(pod, container string, lines ...string) {
	l.Server.Add("prod/"+pod+"/"+container, lines...)
}

// lease is the claim AcquireConnection hands out: Conn, whatever the server.
type lease struct {
	cluster.Lease
	conn *cluster.Connection
}

func (l lease) ConnFor(context.Context, string) (*cluster.Connection, error) { return l.conn, nil }
func (l lease) Release()                                                     {}

func (f *Service) AcquireConnection(context.Context, cluster.ClusterID) (cluster.Lease, error) {
	return lease{conn: f.Conn}, nil
}

// WatchCollectionByID is the record's watch, which sends nothing until ctx ends: the cluster
// stays connectable.
func (f fakeClusters) WatchCollectionByID(ctx context.Context, _ cluster.ClusterID) (*cluster.Stream[cluster.ClusterCollectionWatchFrame], error) {
	return quiet[cluster.ClusterCollectionWatchFrame](ctx), nil
}

// WatchObjectCollection and WatchObjectCollectionByNamespaces are the mirror's watches,
// which send nothing until ctx ends: the mirror does not change under a tail.
func (f fakeCachedData) WatchObjectCollection(ctx context.Context, _ cluster.ClusterID, _ cluster.ClusterCacheID, _, _ string) (*cluster.Stream[cluster.ClusterCachedDataObjectCollectionWatchFrame], error) {
	return quiet[cluster.ClusterCachedDataObjectCollectionWatchFrame](ctx), nil
}

func (f fakeCachedData) WatchObjectCollectionByNamespaces(ctx context.Context, _ cluster.ClusterID, _ cluster.ClusterCacheID, _, _ string, _ []string) (*cluster.Stream[cluster.ClusterCachedDataObjectCollectionWatchFrame], error) {
	return quiet[cluster.ClusterCachedDataObjectCollectionWatchFrame](ctx), nil
}

// quiet is a stream that sends nothing and ends clean with ctx.
func quiet[T any](ctx context.Context) *cluster.Stream[T] {
	return cluster.NewStream(ctx, func(ctx context.Context, _ chan<- T) error {
		<-ctx.Done()
		return nil
	})
}
