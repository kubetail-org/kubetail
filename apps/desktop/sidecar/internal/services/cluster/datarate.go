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

// The data-rate series: a cluster's connection data rate, the pool's ring first, then each
// sample as the pool adds it.
package cluster

import (
	"context"
	"time"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/cluster/internal/kubeconn"
)

// ClusterDataRateSample is how fast a connection moved data over one sampling interval, in
// bytes per second. Mirrors the schema type.
type ClusterDataRateSample struct {
	At                     time.Time `json:"at"`
	ReceivedBytesPerSecond float64   `json:"receivedBytesPerSecond"`
	SentBytesPerSecond     float64   `json:"sentBytesPerSecond"`
}

// ClusterDataRateSampleFeedFrame is one frame of the sample feed: the whole window first, then the
// samples since the frame before. Mirrors the schema type.
type ClusterDataRateSampleFeedFrame struct {
	IntervalSeconds int                     `json:"intervalSeconds"`
	WindowSeconds   int                     `json:"windowSeconds"`
	Samples         []ClusterDataRateSample `json:"samples"`
}

// SubscribeDataRateSampleFeed streams the rate of the cluster's connection. The first frame
// is the pool's ring, sent even if empty; each later one holds the samples newer than the last
// sent, and a nudge with none sends nothing.
//
// The nudge subscription is taken before the first read, so a tick landing between the two is
// not missed: its nudge finds the sample already sent, which the Seq comparison skips.
//
// The claim is this stream's own, released when ctx ends, and refuses what AcquireConnection
// refuses.
func (a clustersAPI) SubscribeDataRateSampleFeed(ctx context.Context, id ClusterID) (<-chan ClusterDataRateSampleFeedFrame, error) {
	lease, err := a.s.AcquireConnection(ctx, id)
	if err != nil {
		return nil, err
	}

	nudges := lease.ListenForDataRateChanges()
	out := make(chan ClusterDataRateSampleFeedFrame)
	go func() {
		defer close(out)
		defer lease.Release()
		defer nudges.Close()

		samples, last := samplesAfter(lease.DataRates(), 0)
		if !sendFrame(ctx, out, frameOf(samples)) {
			return
		}
		for {
			if _, err := nudges.RecvContext(ctx); err != nil {
				return
			}
			samples, newest := samplesAfter(lease.DataRates(), last)
			if len(samples) == 0 {
				continue
			}
			if !sendFrame(ctx, out, frameOf(samples)) {
				return
			}
			last = newest
		}
	}()
	return out, nil
}

// frameOf is a frame of the feed carrying samples, stamped with the pool's cadence and
// window so the client keeps what the ring keeps.
func frameOf(samples []ClusterDataRateSample) ClusterDataRateSampleFeedFrame {
	return ClusterDataRateSampleFeedFrame{
		IntervalSeconds: int(kubeconn.DataRateInterval / time.Second),
		WindowSeconds:   int(kubeconn.DataRateWindow / time.Second),
		Samples:         samples,
	}
}

// samplesAfter is the samples numbered above after, as the wire carries them, and the newest
// number among them. Never nil, so an empty window reaches the wire as an empty list.
func samplesAfter(ring []kubeconn.DataRateSample, after uint64) ([]ClusterDataRateSample, uint64) {
	out := []ClusterDataRateSample{}
	for _, s := range ring {
		if s.Seq <= after {
			continue
		}
		out = append(out, ClusterDataRateSample{
			At:                     s.At,
			ReceivedBytesPerSecond: s.ReceivedBytesPerSecond,
			SentBytesPerSecond:     s.SentBytesPerSecond,
		})
		after = s.Seq
	}
	return out, after
}
