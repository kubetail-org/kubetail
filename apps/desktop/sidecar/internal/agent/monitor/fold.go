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

package monitor

import (
	"maps"
	"time"
)

// windows are the times the fold judges by.
type windows struct {
	// countsAfter is how long each signal holds, while it is judged, before an
	// occurrence starts.
	countsAfter map[Signal]time.Duration
	// rollingOut is countsAfter for a workload mid-rollout, longer than the rollout
	// it lets finish: StatefulSets and DaemonSets have no progress deadline of their own.
	rollingOut time.Duration
	// absence is how long a pair is gone before it stops holding: a shorter gap keeps
	// its first-seen time, and an open occurrence ends after it.
	absence time.Duration
}

func defaultWindows() windows {
	return windows{
		countsAfter: map[Signal]time.Duration{
			CrashLoopBackOff:    2 * time.Minute,
			ImagePullBackOff:    2 * time.Minute,
			PodPending:          5 * time.Minute,
			NodeNotReady:        2 * time.Minute,
			ReplicasUnavailable: 5 * time.Minute,
		},
		rollingOut: 15 * time.Minute,
		absence:    5 * time.Minute,
	}
}

// pairState is what the fold remembers of a pair that holds.
type pairState struct {
	firstSeen, lastSeen time.Time
	// held is how long the pair has held while its signal was judged, short gaps
	// included, and absent how long it has been gone since last seen; both counted up
	// to counted.
	held, absent time.Duration
	counted      time.Time
	// open is whether the pair has an open occurrence.
	open bool
}

// clusterState is the fold's memory of one cluster: each pair that holds.
type clusterState map[Pair]pairState

// start is an occurrence to start: at its pair's first-seen time, seen last at seen.
type start struct {
	pair     Pair
	at, seen time.Time
}

// end is an occurrence to end, at its pair's last-seen time.
type end struct {
	pair Pair
	at   time.Time
}

// seen is an open occurrence's pair seen again, at.
type seen struct {
	pair Pair
	at   time.Time
}

// changes is what one fold writes.
type changes struct {
	starts []start
	ends   []end
	seen   []seen
}

// notifies reports whether the changes reach a reader: a fold that only moves
// last-seen times changes nothing served.
func (c changes) notifies() bool { return len(c.starts) > 0 || len(c.ends) > 0 }

func (c changes) empty() bool { return !c.notifies() && len(c.seen) == 0 }

// fold folds reading r, taken at now, into last, and answers the state after it and
// the changes to write. It changes nothing of last's, which is never nil: a cluster's
// state starts as its seed.
func fold(w windows, last clusterState, r reading, now time.Time) (clusterState, changes) {
	next := maps.Clone(last)
	for pair := range r.pairs {
		if _, ok := next[pair]; !ok {
			next[pair] = pairState{firstSeen: now, counted: now}
		}
	}
	var c changes
	for pair, ps := range next {
		present, judged := r.pairs[pair], r.judged[pair.Signal]
		// Time the signal was not judged is neither holding nor absence.
		if judged {
			elapsed := now.Sub(ps.counted)
			ps.held += elapsed
			if !present {
				ps.absent += elapsed
			}
		}
		ps.counted = now
		switch {
		case present:
			ps.lastSeen, ps.absent = now, 0
			window := w.countsAfter[pair.Signal]
			if r.rolling[pair] {
				window = w.rollingOut
			}
			switch {
			case ps.open:
				c.seen = append(c.seen, seen{pair: pair, at: now})
			case ps.held >= window:
				ps.open = true
				c.starts = append(c.starts, start{pair: pair, at: ps.firstSeen, seen: now})
			}
		case judged && ps.absent >= w.absence:
			if ps.open {
				c.ends = append(c.ends, end{pair: pair, at: ps.lastSeen})
			}
			delete(next, pair)
			continue
		}
		next[pair] = ps
	}
	return next, c
}

// openOccurrence is an occurrence the store holds open, for a seed.
type openOccurrence struct {
	pair                Pair
	startedAt, lastSeen time.Time
}

// seed is a cluster's state at its first reading since the app started at started,
// taken at now, from its open occurrences: a pair each, first seen when it started and
// last seen when its incident was. The time the app was down is absence already; the
// time from started to now, its cache not yet answering, is not.
func seed(open []openOccurrence, started, now time.Time) clusterState {
	state := make(clusterState, len(open))
	for _, o := range open {
		state[o.pair] = pairState{firstSeen: o.startedAt, lastSeen: o.lastSeen, absent: max(started.Sub(o.lastSeen), 0), counted: now, open: true}
	}
	return state
}
