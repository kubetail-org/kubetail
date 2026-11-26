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

package kubeconn

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/testutil"
)

// rateT0 is where a test's sampler clock starts.
var rateT0 = time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)

// countedConn is a connection whose counters a test sets, with seq standing in for which
// connection it is.
func countedConn(seq, received, sent uint64) *Connection {
	conn := &Connection{seq: seq, counts: &byteCounts{}}
	conn.counts.received.Store(received)
	conn.counts.sent.Store(sent)
	return conn
}

// --- the ring ---

func TestTheFirstReadingAddsNoSample(t *testing.T) {
	t.Parallel()

	var r dataRateRing
	added := r.add(readBytes(countedConn(1, 100, 10), rateT0))

	assert.False(t, added)
	assert.Empty(t, r.samples)
}

func TestASampleIsTheRateSinceTheLastReading(t *testing.T) {
	t.Parallel()

	var r dataRateRing
	r.add(readBytes(countedConn(1, 100, 10), rateT0))
	added := r.add(readBytes(countedConn(1, 500, 30), rateT0.Add(2*time.Second)))

	assert.True(t, added)
	assert.Equal(t, []DataRateSample{{
		Seq:                    1,
		At:                     rateT0.Add(2 * time.Second),
		ReceivedBytesPerSecond: 200,
		SentBytesPerSecond:     10,
	}}, r.samples)
}

// A replacement counts from zero, so its count cannot be compared with the old one's.
func TestAReplacedConnectionSamplesZero(t *testing.T) {
	t.Parallel()

	var r dataRateRing
	r.add(readBytes(countedConn(1, 100, 10), rateT0))
	r.add(readBytes(countedConn(2, 50, 5), rateT0.Add(2*time.Second)))

	assert.Zero(t, r.samples[0].ReceivedBytesPerSecond)
	assert.Zero(t, r.samples[0].SentBytesPerSecond)
}

func TestNoConnectionSamplesZero(t *testing.T) {
	t.Parallel()

	var r dataRateRing
	r.add(readBytes(nil, rateT0))
	added := r.add(readBytes(nil, rateT0.Add(2*time.Second)))

	assert.True(t, added, "a zero is still a sample")
	assert.Equal(t, []DataRateSample{{Seq: 1, At: rateT0.Add(2 * time.Second)}}, r.samples)
}

// fill adds n samples to r, one interval apart, after a baseline reading.
func fill(r *dataRateRing, n int) {
	conn := countedConn(1, 0, 0)
	r.add(readBytes(conn, rateT0))
	for i := 1; i <= n; i++ {
		r.add(readBytes(conn, rateT0.Add(time.Duration(i)*DataRateInterval)))
	}
}

func TestTheRingKeepsTheNewestWindow(t *testing.T) {
	t.Parallel()

	var r dataRateRing
	fill(&r, dataRateSlots+5)

	require.Len(t, r.samples, 60)
	assert.Equal(t, rateT0.Add(6*DataRateInterval), r.samples[0].At, "the oldest five went")
	assert.Equal(t, rateT0.Add(65*DataRateInterval), r.samples[59].At)
}

// The number is how a reader tells which samples it has seen, so it never restarts when the
// ring wraps.
func TestSamplesAreNumberedPastTheRingsSize(t *testing.T) {
	t.Parallel()

	var r dataRateRing
	fill(&r, dataRateSlots+5)

	assert.Equal(t, uint64(6), r.samples[0].Seq)
	assert.Equal(t, uint64(65), r.samples[59].Seq)
}

// --- the pool's pass ---

// sampleClock is the sampler's clock in a test: it reads where the test set it, and runs
// duringRead once inside the next read, which is mid-tick.
type sampleClock struct {
	mu         sync.Mutex
	at         time.Time
	duringRead func()
}

func (c *sampleClock) now() time.Time {
	c.mu.Lock()
	at, fn := c.at, c.duringRead
	c.duringRead = nil
	c.mu.Unlock()

	if fn != nil {
		fn()
	}
	return at
}

func (c *sampleClock) set(at time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.at = at
}

// sampledPool is a pool whose sampler clock the test holds.
func sampledPool() (*Service, *sampleClock) {
	s := New(resolving())
	clock := &sampleClock{at: rateT0}
	s.now = clock.now
	return s, clock
}

// tickAt runs one sampler pass with the clock at rateT0+after.
func tickAt(s *Service, clock *sampleClock, after time.Duration) {
	clock.set(rateT0.Add(after))
	s.sampleDataRates()
}

func TestATickSamplesEveryClaimedContext(t *testing.T) {
	t.Parallel()

	s, clock := sampledPool()
	prod, staging := s.Acquire("prod"), s.Acquire("staging")
	defer prod.Release()
	defer staging.Release()
	prodConn, stagingConn := countedConn(1, 0, 0), countedConn(2, 0, 0)
	_, _, _ = s.record("prod", prodConn, news{})
	_, _, _ = s.record("staging", stagingConn, news{})

	tickAt(s, clock, 0)
	prodConn.counts.received.Store(400)
	stagingConn.counts.sent.Store(100)
	tickAt(s, clock, 2*time.Second)

	assert.Equal(t, []DataRateSample{{Seq: 1, At: rateT0.Add(2 * time.Second), ReceivedBytesPerSecond: 200}}, prod.DataRates())
	assert.Equal(t, []DataRateSample{{Seq: 1, At: rateT0.Add(2 * time.Second), SentBytesPerSecond: 50}}, staging.DataRates())
}

// Two holders of one context read one ring, which is what makes the sampler the pool's: two
// panels on one cluster do not sample it twice.
func TestTwoLeasesOnAContextSeeOneRing(t *testing.T) {
	t.Parallel()

	s, clock := sampledPool()
	first, second := s.Acquire("prod"), s.Acquire("prod")
	defer first.Release()
	defer second.Release()

	tickAt(s, clock, 0)
	tickAt(s, clock, 2*time.Second)

	require.Len(t, first.DataRates(), 1)
	assert.Equal(t, first.DataRates(), second.DataRates())
}

// A reader gets its own copy: the ring goes on moving under it.
func TestDataRatesIsACopy(t *testing.T) {
	t.Parallel()

	s, clock := sampledPool()
	lease := s.Acquire("prod")
	defer lease.Release()
	tickAt(s, clock, 0)
	tickAt(s, clock, 2*time.Second)

	got := lease.DataRates()
	got[0].Seq = 99

	assert.Equal(t, uint64(1), lease.DataRates()[0].Seq)
}

func TestTheRingGoesWithTheLastClaim(t *testing.T) {
	t.Parallel()

	s, clock := sampledPool()
	released := s.Acquire("prod")
	tickAt(s, clock, 0)
	tickAt(s, clock, 2*time.Second)
	released.Release()

	again := s.Acquire("prod")
	defer again.Release()

	assert.Empty(t, released.DataRates(), "a released claim reads empty")
	assert.Empty(t, again.DataRates(), "a fresh claim starts a fresh ring")
}

// --- the nudge ---

func TestASampleNudgesItsContext(t *testing.T) {
	t.Parallel()

	s, clock := sampledPool()
	lease := s.Acquire("prod")
	defer lease.Release()
	nudges := lease.ListenForDataRateChanges()
	defer nudges.Close()

	tickAt(s, clock, 0)
	tickAt(s, clock, 2*time.Second)

	ev, err := nudges.RecvContext(within(t))
	require.NoError(t, err)
	assert.Equal(t, "prod", ev.Key)
}

// The first tick only sets the baseline, so it has nothing to tell a reader.
func TestTheBaselineNudgesNobody(t *testing.T) {
	t.Parallel()

	s, clock := sampledPool()
	lease := s.Acquire("prod")
	defer lease.Release()
	nudges := lease.ListenForDataRateChanges()
	defer nudges.Close()

	tickAt(s, clock, 0)

	_, err := nudges.TryRecv()
	assert.Error(t, err, "no nudge for the baseline")
}

// A release and a fresh claim landing mid-tick leave the new entry alone: the reading was the
// old entry's, and the new holder has no sample to be told about.
func TestAContextReclaimedMidTickGetsNothingFromThatTick(t *testing.T) {
	t.Parallel()

	s, clock := sampledPool()
	old := s.Acquire("prod")
	tickAt(s, clock, 0)
	// Keyed by context, so it hears whatever holds the name next.
	nudges := old.ListenForDataRateChanges()
	defer nudges.Close()

	var fresh Lease
	clock.duringRead = func() {
		old.Release()
		fresh = s.Acquire("prod")
	}
	tickAt(s, clock, 2*time.Second)
	defer fresh.Release()

	assert.Empty(t, fresh.DataRates())
	_, err := nudges.TryRecv()
	assert.Error(t, err, "no nudge for a sample the entry never had")
}

// --- the clock ---

// Start runs the sampler on the ticker it is given, and its stop function ends the sampler
// and stops the ticker.
func TestStartSamplesOnEachTickUntilStopped(t *testing.T) {
	t.Parallel()

	s, _ := sampledPool()
	ticks := make(chan time.Time)
	tickerStopped := testutil.NewSignal()
	s.dataRateTicker = func() (<-chan time.Time, func()) {
		return ticks, func() { tickerStopped.Fire() }
	}
	lease := s.Acquire("prod")
	defer lease.Release()
	nudges := lease.ListenForDataRateChanges()
	defer nudges.Close()
	stop, err := s.Start(t.Context())
	require.NoError(t, err)

	// Unbuffered, so each send returns once the sampler has taken the tick.
	ticks <- rateT0
	ticks <- rateT0
	_, err = nudges.RecvContext(within(t))
	require.NoError(t, err, "the second tick adds a sample")

	require.NoError(t, stop(t.Context()))
	tickerStopped.Wait(t, "the ticker to stop with the sampler")
}

func TestTheDataRateTickerTicks(t *testing.T) {
	t.Parallel()

	ticks, stop := newTicker(time.Millisecond)
	t.Cleanup(stop)

	testutil.Recv(t, ticks, "a tick")
}
