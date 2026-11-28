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

package grpcserver

import (
	"context"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/grpc/monitorpb"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/agent/monitor"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/agent/monitor/kubesignals"
)

// AlertFeed is the monitor's alert feed, as the host reads it.
type AlertFeed interface {
	SubscribeAlertEventFeed(ctx context.Context) (*monitor.Stream[monitor.AlertEventFeedFrame], error)
}

// monitorServer implements monitorpb.MonitorServiceServer; a nil feed ends each
// stream at once.
type monitorServer struct {
	monitorpb.UnimplementedMonitorServiceServer
	alerts AlertFeed
	// streamsCtx ends at shutdown, ending every feed cleanly.
	streamsCtx context.Context
}

// SubscribeAlertEventFeed relays the monitor's alert feed until the client
// cancels, the sidecar shuts down or the feed ends. A monitor that is stopping
// refuses it Unavailable; a feed that fails ends it Internal.
func (s *monitorServer) SubscribeAlertEventFeed(_ *monitorpb.SubscribeAlertEventFeedRequest, out monitorpb.MonitorService_SubscribeAlertEventFeedServer) error {
	if s.alerts == nil {
		return nil // clean OK
	}
	ctx, cancel := context.WithCancel(out.Context())
	defer cancel()
	defer context.AfterFunc(s.streamsCtx, cancel)()

	feed, err := s.alerts.SubscribeAlertEventFeed(ctx)
	if err != nil {
		return status.Error(codes.Unavailable, err.Error())
	}
	for f := range feed.Frames {
		if err := out.Send(toAlertFrame(f)); err != nil { // coverage:ignore a send fails once the client is gone, whose cancel ends the feed first
			return err
		}
	}
	if err := feed.Err(); err != nil {
		return status.Error(codes.Internal, err.Error())
	}
	return nil
}

// toAlertFrame projects a frame to the wire, naming a Kubernetes target's cluster.
func toAlertFrame(f monitor.AlertEventFeedFrame) *monitorpb.AlertEventFeedFrame {
	if f.Alert == nil {
		return &monitorpb.AlertEventFeedFrame{Type: monitorpb.AlertEventFeedFrame_BOOKMARK}
	}
	a := f.Alert
	alert := &monitorpb.Alert{
		IncidentId:      string(a.IncidentID),
		TargetId:        string(a.TargetID),
		Integration:     a.Integration,
		TargetLabel:     a.TargetLabel,
		SubjectLabel:    a.SubjectLabel,
		SignalLabel:     a.SignalLabel,
		Severity:        string(a.Severity),
		AlertedAtUnixMs: a.AlertedAt.UnixMilli(),
		AlertSeq:        a.Seq,
	}
	if id, ok := kubesignals.ClusterID(a.Integration, a.TargetKey); ok {
		alert.ClusterId = &id
	}
	return &monitorpb.AlertEventFeedFrame{Type: monitorpb.AlertEventFeedFrame_ALERT, Alert: alert}
}
