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

package cluster

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/testutil"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/cluster/internal/kubeconn"
)

// rateAt is the sample the pool would have numbered seq, taken seq intervals after t0.
func rateAt(seq uint64, received float64) kubeconn.DataRateSample {
	return kubeconn.DataRateSample{
		Seq:                    seq,
		At:                     t0.Add(time.Duration(seq) * kubeconn.DataRateInterval),
		ReceivedBytesPerSecond: received,
		SentBytesPerSecond:     received / 2,
	}
}

// wireRate is what rateAt reaches the wire as.
func wireRate(seq uint64, received float64) ClusterDataRateSample {
	return ClusterDataRateSample{
		At:                     t0.Add(time.Duration(seq) * kubeconn.DataRateInterval),
		ReceivedBytesPerSecond: received,
		SentBytesPerSecond:     received / 2,
	}
}

// seriesOf is a frame carrying samples.
func seriesOf(samples ...ClusterDataRateSample) ClusterDataRateSampleFeedFrame {
	return ClusterDataRateSampleFeedFrame{IntervalSeconds: 2, WindowSeconds: 120, Samples: samples}
}

// watchDataRate opens the feed for a cluster whose pool already holds samples, over a
// cancellable context.
func watchDataRate(t *testing.T, samples ...kubeconn.DataRateSample) (<-chan ClusterDataRateSampleFeedFrame, *fakeKubeconn, context.CancelFunc) {
	t.Helper()
	d := newTestDeps(t)
	obj := createCluster(t, d, "prod")
	pool := &fakeKubeconn{rateReads: testutil.NewProbe[string](8), releases: testutil.NewProbe[string](8)}
	for _, s := range samples {
		pool.addRate("prod", s)
	}
	d.kubeconnSvc = pool

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	ch, err := serviceOver(t, d).Clusters().SubscribeDataRateSampleFeed(ctx, clusterIDOf(obj))
	require.NoError(t, err)
	return ch, pool, cancel
}

func TestClustersSubscribeDataRateSampleFeedSendsTheWindowFirst(t *testing.T) {
	ch, _, _ := watchDataRate(t, rateAt(1, 100), rateAt(2, 300))

	assert.Equal(t, seriesOf(wireRate(1, 100), wireRate(2, 300)), testutil.Recv(t, ch, "the window"))
}

// The first frame is what a collection watch's Bookmark is: after it, empty means empty.
func TestClustersSubscribeDataRateSampleFeedSendsAnEmptyWindow(t *testing.T) {
	ch, _, _ := watchDataRate(t)

	first := testutil.Recv(t, ch, "the window")
	assert.NotNil(t, first.Samples, "an empty list, not a missing one")
	assert.Empty(t, first.Samples)
}

func TestClustersSubscribeDataRateSampleFeedSendsOnlyTheNewSamples(t *testing.T) {
	ch, pool, _ := watchDataRate(t, rateAt(1, 100))
	testutil.Recv(t, ch, "the window")

	pool.addRate("prod", rateAt(2, 0))

	assert.Equal(t, seriesOf(wireRate(2, 0)), testutil.Recv(t, ch, "the new sample"))
}

// A nudge whose sample the reader already sent, as the one a tick lands between the subscribe
// and the first read gives, sends nothing. The send is unbuffered, so an empty frame would
// arrive ahead of the next sample.
func TestClustersSubscribeDataRateSampleFeedSendsNothingWithoutANewSample(t *testing.T) {
	ch, pool, _ := watchDataRate(t, rateAt(1, 100))
	testutil.Recv(t, ch, "the window")
	pool.rateReads.Await(t, "the window's read")

	pool.nudge("prod")
	// Nudges coalesce, so the next sample's would swallow this one were it still pending.
	pool.rateReads.Await(t, "the nudge's read")
	pool.addRate("prod", rateAt(2, 50))

	assert.Equal(t, seriesOf(wireRate(2, 50)), testutil.Recv(t, ch, "the next sample, and nothing before it"))
}

// The nudges coalesce while the reader is away; the ring still holds every sample it missed.
func TestClustersSubscribeDataRateSampleFeedCatchesUpAReaderThatFellBehind(t *testing.T) {
	ch, pool, _ := watchDataRate(t)
	testutil.Recv(t, ch, "the window")

	for seq := uint64(1); seq <= 3; seq++ {
		pool.addRate("prod", rateAt(seq, float64(seq)))
	}

	var got []ClusterDataRateSample
	for len(got) < 3 {
		got = append(got, testutil.Recv(t, ch, "the samples missed").Samples...)
	}
	assert.Equal(t, []ClusterDataRateSample{wireRate(1, 1), wireRate(2, 2), wireRate(3, 3)}, got)
}

func TestClustersSubscribeDataRateSampleFeedRefusesADisabledCluster(t *testing.T) {
	d := newTestDeps(t)
	obj := createCluster(t, d, "prod")
	_, err := serviceOver(t, d).Clusters().SetEnabled(context.Background(), clusterIDOf(obj), false)
	require.NoError(t, err)

	_, err = serviceOver(t, d).Clusters().SubscribeDataRateSampleFeed(context.Background(), clusterIDOf(obj))

	assert.ErrorIs(t, err, ErrNotConnectable)
}

func TestClustersSubscribeDataRateSampleFeedReleasesItsClaimWhenTheStreamEnds(t *testing.T) {
	ch, pool, cancel := watchDataRate(t)
	testutil.Recv(t, ch, "the window")

	cancel()

	testutil.WaitClosed(t, ch, "the feed to close on cancellation")
	assert.Equal(t, []string{"prod"}, pool.released)
}

func TestClustersSubscribeDataRateSampleFeedStopsForAConsumerThatIsGone(t *testing.T) {
	ch, _, cancel := watchDataRate(t)

	cancel() // the window waits on a reader that never comes

	testutil.WaitClosed(t, ch, "the feed to give up its send")
}

func TestClustersSubscribeDataRateSampleFeedStopsForAConsumerGoneMidStream(t *testing.T) {
	ch, pool, cancel := watchDataRate(t)
	testutil.Recv(t, ch, "the window")
	pool.rateReads.Await(t, "the window's read")

	pool.addRate("prod", rateAt(1, 0))
	pool.rateReads.Await(t, "the new sample's read") // the feed now waits on a reader that never comes
	cancel()

	// The release follows the abandoned send; nothing reads the channel until it lands, so
	// the send cannot have been taken instead.
	pool.releases.Await(t, "the feed to give up its send")
	testutil.WaitClosed(t, ch, "the feed to close")
}
