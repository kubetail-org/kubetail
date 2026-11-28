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

package grpcserver_test

import (
	"context"
	"io"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	grpcserver "github.com/kubetail-org/kubetail/apps/desktop/sidecar/grpc"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/grpc/monitorpb"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/agent/monitor"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/agent/monitor/kubesignals"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/appdb"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/appdb/appdbtest"
)

// noTargets is an integration that lists and finds nothing: the feed reads what
// the test stores.
type noTargets struct{}

func (noTargets) ListTargets(context.Context) ([]monitor.Target, error)  { return nil, nil }
func (noTargets) ListenForTargetChanges(context.Context) <-chan struct{} { return nil }
func (noTargets) Read(context.Context, monitor.Target) (monitor.Reading, error) {
	return monitor.Reading{}, nil
}

// alertMonitor is a monitor of the Kubernetes integration and another, over a
// database holding one alert of each, the Kubernetes one committed second.
func alertMonitor(t *testing.T) (*monitor.Monitor, *appdb.DB) {
	t.Helper()
	db := appdbtest.Open(t)
	m, err := monitor.New(db,
		monitor.Registration{Name: kubesignals.Name, Cadence: time.Minute, Integration: noTargets{}},
		monitor.Registration{Name: "fake", Cadence: time.Minute, Integration: noTargets{}})
	require.NoError(t, err)
	t.Cleanup(func() { _ = m.Close() })
	for _, q := range []string{
		`INSERT INTO monitor_targets (id, integration, key, label, created_at, updated_at) VALUES
		  ('t1', 'kubernetes', 'c1', 'dev', 0, 0), ('t2', 'fake', 'k', 'Fake', 0, 0)`,
		`INSERT INTO incidents (id, target_id, target_uid, subject_key, subject_label, signal, signal_label, severity,
		  first_seen_at, last_seen_at, occurrence_count, alerted_at, alert_seq) VALUES
		  ('i1', 't1', 'u', 'web', 'Deployment default/web', 'kubernetes.crash_loop', 'Crash loop', 'warning', 0, 0, 1, 2000, 2),
		  ('i2', 't2', 'u', 'x', 'Thing x', 'fake.down', 'Down', 'critical', 0, 0, 1, 1000, 1)`,
	} {
		_, err := db.Write.Exec(q)
		require.NoError(t, err)
	}
	return m, db
}

func subscribeAlerts(t *testing.T, ctx context.Context, srv *grpc.Server) monitorpb.MonitorService_SubscribeAlertEventFeedClient {
	t.Helper()
	feed, err := monitorpb.NewMonitorServiceClient(newGRPCTestConn(t, srv)).SubscribeAlertEventFeed(ctx, &monitorpb.SubscribeAlertEventFeedRequest{})
	require.NoError(t, err)
	return feed
}

// A subscriber gets each retained alert, in commit order, carrying what a
// notification names and, for a Kubernetes target, its cluster's id; then one
// Bookmark.
func TestTheAlertFeedCarriesEachAlertAndItsCluster(t *testing.T) {
	m, _ := alertMonitor(t)
	srv := grpcserver.NewServer(context.Background(), nil, nil, m)
	t.Cleanup(srv.Stop)
	feed := subscribeAlerts(t, t.Context(), srv)

	var got []*monitorpb.AlertEventFeedFrame
	for range 3 {
		f, err := feed.Recv()
		require.NoError(t, err)
		got = append(got, f)
	}

	assert.Equal(t, "i2", got[0].GetAlert().GetIncidentId())
	assert.Nil(t, got[0].GetAlert().ClusterId, "a target of another integration names no cluster")
	require.Equal(t, monitorpb.AlertEventFeedFrame_ALERT, got[1].GetType())
	kube := got[1].GetAlert()
	assert.Equal(t, "i1", kube.GetIncidentId())
	assert.Equal(t, "t1", kube.GetTargetId())
	assert.Equal(t, "kubernetes", kube.GetIntegration())
	assert.Equal(t, "dev", kube.GetTargetLabel())
	require.NotNil(t, kube.ClusterId)
	assert.Equal(t, "c1", kube.GetClusterId())
	assert.Equal(t, "Deployment default/web", kube.GetSubjectLabel())
	assert.Equal(t, "Crash loop", kube.GetSignalLabel())
	assert.Equal(t, "warning", kube.GetSeverity())
	assert.Equal(t, int64(2000), kube.GetAlertedAtUnixMs())
	assert.Equal(t, int64(2), kube.GetAlertSeq())
	assert.Equal(t, monitorpb.AlertEventFeedFrame_BOOKMARK, got[2].GetType())
	assert.Nil(t, got[2].GetAlert())
}

// The feed ends cleanly at shutdown, and at once with no monitor; a monitor that
// is stopping refuses it Unavailable, and a feed that fails ends Internal.
func TestTheAlertFeedEndsAsItsMonitorDoes(t *testing.T) {
	streams, endStreams := context.WithCancel(context.Background())
	m, db := alertMonitor(t)
	srv := grpcserver.NewServer(streams, nil, nil, m)
	t.Cleanup(srv.Stop)
	feed := subscribeAlerts(t, t.Context(), srv)
	for range 3 {
		_, err := feed.Recv()
		require.NoError(t, err)
	}
	endStreams()
	_, err := feed.Recv()
	assert.ErrorIs(t, err, io.EOF, "shutdown ends the feed cleanly")

	none := grpcserver.NewServer(context.Background(), nil, nil, nil)
	t.Cleanup(none.Stop)
	_, err = subscribeAlerts(t, t.Context(), none).Recv()
	assert.ErrorIs(t, err, io.EOF)

	failing := grpcserver.NewServer(context.Background(), nil, nil, m)
	t.Cleanup(failing.Stop)
	_, err = db.Write.Exec(`ALTER TABLE monitor_targets RENAME TO gone_targets`)
	require.NoError(t, err)
	_, err = subscribeAlerts(t, t.Context(), failing).Recv()
	assert.Equal(t, codes.Internal, status.Code(err))

	require.NoError(t, m.Close())
	_, err = subscribeAlerts(t, t.Context(), failing).Recv()
	assert.Equal(t, codes.Unavailable, status.Code(err))
}
