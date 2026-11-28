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
	"cmp"
	"maps"
	"slices"
	"time"
)

const (
	// reopenWithin is how soon after an incident's last occurrence ended a new
	// occurrence joins it rather than opening another incident.
	reopenWithin = 24 * time.Hour
	// staleAfter bounds an unknown signal: an occurrence last seen longer ago ends.
	staleAfter = 24 * time.Hour
	// recentFor is how long the fold remembers an ended incident, past the last
	// start that could join it.
	recentFor = 2 * reopenWithin
)

// EndReason is how an occurrence ended, as incident_occurrences.end_reason holds it.
type EndReason string

const (
	EndResolved EndReason = "resolved" // judged absent for its signal's resolve-after
	EndStopped  EndReason = "stopped"  // the read was switched off or the UID changed
	EndStale    EndReason = "stale"    // last seen more than staleAfter ago
)

// pairKey is a signal and a subject: the unit the fold tracks.
type pairKey struct{ signal, subject string }

// incidentKey is what an incident matches on within its target.
type incidentKey struct{ uid, signal, subject string }

// pair is one signal holding, or lately holding, for one subject.
type pair struct {
	firstSeen, lastSeen time.Time
	// held is how long the pair has held and absent how long it has been gone,
	// each a sum of earned intervals.
	held, absent time.Duration
	open         *occurrence // nil while no occurrence is open
}

// occurrence is an open occurrence and the incident it belongs to.
type occurrence struct {
	id         string
	incident   incidentKey
	incidentID string
}

// incidentRef is the newest incident of an incidentKey: what a started occurrence joins.
type incidentRef struct {
	id      string
	count   int       // its occurrence_count
	endedAt time.Time // its newest occurrence's end; zero while open
}

// targetState is what the fold keeps of one target between readings. A fold
// returns a new one, so a state its commit refused is the one the next reading
// folds from.
type targetState struct {
	uid       string // the UID the pairs are under; "" before any reading named one
	pairs     map[pairKey]pair
	incidents map[incidentKey]incidentRef
	unknown   map[string]ReadErrorCode // as monitor_targets.unknown_signals holds it
	read      bool                     // last_read_at is set
	// at is the last fold's time, zero before the first since the state was
	// seeded, and judged the signals that fold judged.
	at     time.Time
	judged map[string]bool
}

// changes is what one fold writes.
type changes struct {
	uid       string                   // a UID to write, "" for none
	unknown   map[string]ReadErrorCode // the unknown signals to write, nil when unchanged
	firstRead bool                     // the fold sets last_read_at for the first time
	starts    []start
	ends      []end
	// seen is the open incidents the reading saw, whose last_seen_at moves to seenAt.
	seen   []string
	seenAt time.Time
}

// start is an occurrence to insert, opening its incident when open is set.
type start struct {
	occurrenceID string
	incidentID   string
	number       int
	startedAt    time.Time
	seenAt       time.Time
	detail       map[string]string
	open         *newIncident
}

// newIncident is what an incident holds from the fold that opens it.
type newIncident struct {
	uid     string
	subject Subject
	signal  Signal
}

// end is an occurrence to end.
type end struct {
	occurrenceID string
	incidentID   string
	at           time.Time
	reason       EndReason
}

// notifiesIncidents is whether the changes move what an incidents reader sees:
// an occurrence started or ended. Advancing last_seen_at alone does not.
func (c changes) notifiesIncidents() bool { return len(c.starts) > 0 || len(c.ends) > 0 }

// notifiesTargets is whether the changes move a served field of the target.
func (c changes) notifiesTargets() bool { return c.uid != "" || c.unknown != nil || c.firstRead }

// fold folds reading r, taken at now, into prev: a new UID first, then the stale
// bound, then the reading. r has passed check, so every name it holds is one of
// reg's signals. newID mints the ids of what it starts.
func fold(prev targetState, reg *registration, r Reading, now time.Time, newID func() string) (targetState, changes) {
	next := prev.clone()
	var ch changes

	if r.UID != "" && r.UID != next.uid {
		if next.uid != "" {
			for _, k := range next.sortedKeys() {
				next.drop(&ch, k, EndStopped)
			}
		}
		next.uid, ch.uid = r.UID, r.UID
	}

	for _, k := range next.sortedKeys() {
		if now.Sub(next.pairs[k].lastSeen) > staleAfter {
			next.drop(&ch, k, EndStale)
		}
	}

	observed := map[pairKey]Observation{}
	for _, o := range r.Observations {
		k := pairKey{o.Signal, o.Subject.Key}
		if _, ok := observed[k]; !ok {
			observed[k] = o
		}
	}
	interval := now.Sub(prev.at)
	earnable := !prev.at.IsZero() && interval > 0 && interval <= 2*reg.Cadence

	judged := map[string]bool{}
	for _, s := range reg.Signals {
		if code, unknown := r.Unknown[s.Name]; unknown {
			if code == CodeDisabled {
				for _, k := range next.keysOf(s.Name) {
					next.drop(&ch, k, EndStopped)
				}
			}
			continue
		}
		judged[s.Name] = true
		var earned time.Duration
		if earnable && prev.judged[s.Name] {
			earned = interval
		}
		for _, k := range next.keysOf(s.Name) {
			p := next.pairs[k]
			p.held += earned
			o, ok := observed[k]
			if !ok {
				p.absent += earned
				if p.absent >= s.ResolveAfter {
					next.drop(&ch, k, EndResolved)
					continue
				}
				next.pairs[k] = p
				continue
			}
			p.absent, p.lastSeen = 0, now
			if p.open != nil {
				ch.seen, ch.seenAt = append(ch.seen, p.open.incidentID), now
			} else if p.held >= s.OpenAfter {
				ch.starts = append(ch.starts, next.startOf(&p, s, o, now, newID))
			}
			next.pairs[k] = p
		}
		for _, o := range r.Observations {
			if o.Signal != s.Name {
				continue
			}
			k := pairKey{o.Signal, o.Subject.Key}
			if _, tracked := next.pairs[k]; tracked {
				continue
			}
			p := pair{firstSeen: now, lastSeen: now}
			if s.OpenAfter <= 0 {
				ch.starts = append(ch.starts, next.startOf(&p, s, o, now, newID))
			}
			next.pairs[k] = p
		}
	}
	next.at, next.judged = now, judged

	for k, ref := range next.incidents {
		if !ref.endedAt.IsZero() && now.Sub(ref.endedAt) > recentFor {
			delete(next.incidents, k)
		}
	}
	if !maps.Equal(r.Unknown, next.unknown) {
		next.unknown = maps.Clone(r.Unknown)
		if next.unknown == nil {
			next.unknown = map[string]ReadErrorCode{}
		}
		ch.unknown = next.unknown
	}
	if !next.read {
		next.read, ch.firstRead = true, true
	}
	return next, ch
}

// startOf starts an occurrence of the pair p, stamped with its first sighting and
// carrying o's detail: joining the incident whose last occurrence ended within
// reopenWithin of that sighting, or opening a new one.
func (s *targetState) startOf(p *pair, sig Signal, o Observation, now time.Time, newID func() string) start {
	key := incidentKey{s.uid, sig.Name, o.Subject.Key}
	st := start{occurrenceID: newID(), startedAt: p.firstSeen, seenAt: now, detail: o.Detail}
	if ref, ok := s.incidents[key]; ok && !ref.endedAt.IsZero() && p.firstSeen.Sub(ref.endedAt) <= reopenWithin {
		st.incidentID, st.number = ref.id, ref.count+1
	} else {
		st.incidentID, st.number = newID(), 1
		st.open = &newIncident{uid: s.uid, subject: o.Subject, signal: sig}
	}
	s.incidents[key] = incidentRef{id: st.incidentID, count: st.number}
	p.open = &occurrence{id: st.occurrenceID, incident: key, incidentID: st.incidentID}
	return st
}

// drop forgets the pair k, ending its open occurrence for reason: stamped with
// its last sighting and never earlier than its start, so a clock set back still
// commits.
func (s *targetState) drop(ch *changes, k pairKey, reason EndReason) {
	p := s.pairs[k]
	delete(s.pairs, k)
	if p.open == nil {
		return
	}
	at := p.lastSeen
	if at.Before(p.firstSeen) {
		at = p.firstSeen
	}
	ref := s.incidents[p.open.incident]
	ref.id, ref.endedAt = p.open.incidentID, at
	s.incidents[p.open.incident] = ref
	ch.ends = append(ch.ends, end{occurrenceID: p.open.id, incidentID: p.open.incidentID, at: at, reason: reason})
}

// sortedKeys is every pair, in a fixed order so a fold writes the same changes
// in the same order every time.
func (s *targetState) sortedKeys() []pairKey {
	keys := slices.Collect(maps.Keys(s.pairs))
	slices.SortFunc(keys, func(a, b pairKey) int {
		return cmp.Or(cmp.Compare(a.signal, b.signal), cmp.Compare(a.subject, b.subject))
	})
	return keys
}

// keysOf is signal's pairs, in sortedKeys' order.
func (s *targetState) keysOf(signal string) []pairKey {
	return slices.DeleteFunc(s.sortedKeys(), func(k pairKey) bool { return k.signal != signal })
}

// clone is a copy the fold may change without changing s.
func (s targetState) clone() targetState {
	s.pairs = maps.Clone(s.pairs)
	if s.pairs == nil {
		s.pairs = map[pairKey]pair{}
	}
	s.incidents = maps.Clone(s.incidents)
	if s.incidents == nil {
		s.incidents = map[incidentKey]incidentRef{}
	}
	return s
}
