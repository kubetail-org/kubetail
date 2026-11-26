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

// The data-rate sampler: one clock for the pool, a short ring of rates per claimed context.
package kubeconn

import (
	"context"
	"slices"
	"time"
)

// DataRateInterval is the sampler's cadence, and DataRateWindow how far back a ring reaches at
// that cadence. A frame of the sample feed carries both, so the client keeps what the ring keeps.
const (
	DataRateInterval = 2 * time.Second
	DataRateWindow   = 2 * time.Minute
)

// dataRateSlots is how many samples a ring holds.
const dataRateSlots = int(DataRateWindow / DataRateInterval)

// DataRateSample is how fast a connection moved data between two readings.
type DataRateSample struct {
	// Seq numbers the sample within its ring, from 1. A reader keeps the last Seq it saw and
	// takes what is newer.
	Seq                    uint64
	At                     time.Time
	ReceivedBytesPerSecond float64
	SentBytesPerSecond     float64
}

// byteReading is one read of a connection's counters. A zero seq is no connection.
type byteReading struct {
	seq      uint64
	received uint64
	sent     uint64
	at       time.Time
}

// readBytes reads conn's counters at at. A nil conn reads as no connection.
func readBytes(conn *Connection, at time.Time) byteReading {
	if conn == nil {
		return byteReading{at: at}
	}
	received, sent := conn.Bytes()
	return byteReading{seq: conn.Seq(), received: received, sent: sent, at: at}
}

// dataRateRing is one context's last samples, oldest first, and the reading the next one is
// measured from.
type dataRateRing struct {
	last    byteReading
	primed  bool // last holds a reading
	count   uint64
	samples []DataRateSample
}

// add measures cur against the last reading and appends the sample, reporting whether it
// added one: the first reading only sets the baseline. A replaced or missing connection
// samples zero, which keeps the line unbroken.
func (r *dataRateRing) add(cur byteReading) bool {
	prev, primed := r.last, r.primed
	r.last, r.primed = cur, true
	if !primed {
		return false
	}

	r.count++
	s := DataRateSample{Seq: r.count, At: cur.at}
	// Monotonic when both times carry a reading, so a wall-clock step cannot make it negative.
	elapsed := cur.at.Sub(prev.at).Seconds()
	if cur.seq != 0 && cur.seq == prev.seq && elapsed > 0 {
		s.ReceivedBytesPerSecond = float64(cur.received-prev.received) / elapsed
		s.SentBytesPerSecond = float64(cur.sent-prev.sent) / elapsed
	}
	r.samples = append(r.samples, s)
	if len(r.samples) > dataRateSlots {
		r.samples = r.samples[1:]
	}
	return true
}

// newTicker is time.NewTicker as the sampler's tick source.
func newTicker(d time.Duration) (<-chan time.Time, func()) {
	t := time.NewTicker(d)
	return t.C, t.Stop
}

// sampleEvery runs a sampler pass on each tick until ctx ends.
func (s *Service) sampleEvery(ctx context.Context, ticks <-chan time.Time) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticks:
			s.sampleDataRates()
		}
	}
}

// sampleDataRates is one tick: read every claimed context's connection, add a sample to each
// ring, and nudge the context's watchers.
//
// The counters are read outside mu, so the clock is too, and an entry can be released, and
// its context claimed again, before the samples are filed. Only an entry the pool still
// holds is written, as record does.
func (s *Service) sampleDataRates() {
	type sampled struct {
		contextName string
		entry       *entry
		conn        *Connection
		reading     byteReading
	}

	s.mu.Lock()
	batch := make([]sampled, 0, len(s.claimed))
	for contextName, e := range s.claimed {
		batch = append(batch, sampled{contextName: contextName, entry: e, conn: e.conn})
	}
	s.mu.Unlock()

	at := s.now()
	for i := range batch {
		batch[i].reading = readBytes(batch[i].conn, at)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	for _, b := range batch {
		if s.claimed[b.contextName] != b.entry {
			continue
		}
		// Under mu, so the nudge goes to the entry the sample went to. conflate's Send never
		// blocks and runs no caller code.
		if b.entry.rates.add(b.reading) {
			s.dataRateChangeHub.Sender().Send(b.contextName, struct{}{})
		}
	}
}

// DataRates answers for the entry the claim was made for, and reads empty once the pool no
// longer holds it.
func (c *claim) DataRates() []DataRateSample {
	s := c.svc
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.claimed[c.contextName] != c.entry {
		return nil
	}
	return slices.Clone(c.entry.rates.samples)
}

// ListenForDataRateChanges is keyed by context, like WatchState, so the caller closes it.
func (c *claim) ListenForDataRateChanges() DataRateChangeNotifications {
	return c.svc.dataRateChangeHub.Receiver(c.svc.dataRateChangeHub.WithKey(c.contextName))
}
