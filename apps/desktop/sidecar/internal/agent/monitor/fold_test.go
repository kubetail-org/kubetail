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
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const cadence = 30 * time.Second

var (
	t0 = time.Date(2026, 11, 27, 9, 0, 0, 0, time.UTC)
	// crash opens after 2 minutes and resolves after 5, as a Kubernetes signal;
	// flip opens and resolves at once.
	crash = Signal{Name: "fake.crash", Label: "Crash", OpenAfter: 2 * time.Minute, ResolveAfter: 5 * time.Minute, Severity: SeverityWarning}
	flip  = Signal{Name: "fake.flip", Label: "Flip", Severity: SeverityInfo}
)

// testRegistration is the fake integration's registration, as New builds it.
func testRegistration() *registration {
	return &registration{
		Registration: Registration{Name: "fake", Cadence: cadence, Signals: []Signal{crash, flip}},
		names:        map[string]bool{crash.Name: true, flip.Name: true},
	}
}

// folder drives fold over one target, keeping the state each fold returns and
// every change it wrote.
type folder struct {
	t     *testing.T
	reg   *registration
	state targetState
	ids   int
	all   changes
}

func newFolder(t *testing.T) *folder {
	return &folder{t: t, reg: testRegistration(), state: targetState{}.clone()}
}

// at folds r at t0+offset and answers its changes.
func (f *folder) at(offset time.Duration, r Reading) changes {
	f.t.Helper()
	next, ch := fold(f.state, f.reg, r, t0.Add(offset), func() string { f.ids++; return "id-" + strconv.Itoa(f.ids) })
	f.state = next
	f.all.starts = append(f.all.starts, ch.starts...)
	f.all.ends = append(f.all.ends, ch.ends...)
	return ch
}

// every folds r at each cadence from from to to, both included.
func (f *folder) every(from, to time.Duration, r Reading) {
	f.t.Helper()
	for at := from; at <= to; at += cadence {
		f.at(at, r)
	}
}

// seeing is a reading under uid "u1" judging every signal and observing signal
// for each of subjects, each with detail naming the subject.
func seeing(signal string, subjects ...string) Reading {
	r := Reading{UID: "u1"}
	for _, s := range subjects {
		r.Observations = append(r.Observations, Observation{
			Signal:  signal,
			Subject: Subject{Key: s, Label: "Deployment " + s, Attributes: map[string]string{"name": s}},
			Detail:  map[string]string{"pod": s + "-0"},
		})
	}
	return r
}

// nothing is a judged reading in which no signal holds.
func nothing() Reading { return Reading{UID: "u1"} }

// unknown is a reading under uid "u1" that could judge no signal.
func unknown(code ReadErrorCode) Reading {
	r := unknownAll(testRegistration(), code)
	r.UID = "u1"
	return r
}

// A pair's held total grows by each earned interval; its occurrence starts at the
// fold that sees it once that total reaches open-after, stamped with the first
// sighting and carrying that fold's detail, opening a new incident.
func TestAnOccurrenceStartsOnceThePairHasHeldForOpenAfter(t *testing.T) {
	f := newFolder(t)
	f.every(0, 90*time.Second, seeing(crash.Name, "web"))
	require.Empty(t, f.all.starts)

	ch := f.at(2*time.Minute, seeing(crash.Name, "web"))

	require.Len(t, ch.starts, 1)
	s := ch.starts[0]
	assert.Equal(t, t0, s.startedAt)
	assert.Equal(t, t0.Add(2*time.Minute), s.seenAt)
	assert.Equal(t, 1, s.number)
	assert.Equal(t, map[string]string{"pod": "web-0"}, s.detail)
	require.NotNil(t, s.open)
	assert.Equal(t, "u1", s.open.uid)
	assert.Equal(t, "Deployment web", s.open.subject.Label)
	assert.Equal(t, crash, s.open.signal)

	ch = f.at(150*time.Second, seeing(crash.Name, "web"))
	assert.Empty(t, ch.starts, "one occurrence")
	assert.Equal(t, []string{s.incidentID}, ch.seen, "a sighting advances last_seen_at")
	assert.Equal(t, t0.Add(150*time.Second), ch.seenAt)
	assert.False(t, ch.notifiesIncidents(), "and notifies nothing")
}

// A crash loop's Running spells between back-offs leave the pair live: its
// window does not restart, and it opens at the first sighting past open-after.
func TestAFoldThatDoesNotSeeThePairLeavesItLive(t *testing.T) {
	f := newFolder(t)
	f.at(0, seeing(crash.Name, "web"))
	f.every(30*time.Second, 90*time.Second, nothing())
	f.at(2*time.Minute, nothing())
	require.Empty(t, f.all.starts, "no fold that misses the pair starts it")

	ch := f.at(150*time.Second, seeing(crash.Name, "web"))

	require.Len(t, ch.starts, 1)
	assert.Equal(t, t0, ch.starts[0].startedAt)
}

// A failure seen once and gone opens nothing; seen again 3 minutes after the
// first, it opens at that fold with that fold's detail. Unseen for 5 judged
// minutes, its pair is dropped and nothing ever opened.
func TestAPairSeenOnceOpensOnlyWhenSeenAgain(t *testing.T) {
	f := newFolder(t)
	f.at(0, seeing(crash.Name, "web"))
	f.every(30*time.Second, 150*time.Second, nothing())
	require.Empty(t, f.all.starts)
	r := seeing(crash.Name, "web")
	r.Observations[0].Detail = map[string]string{"pod": "web-1"}

	ch := f.at(3*time.Minute, r)

	require.Len(t, ch.starts, 1)
	assert.Equal(t, map[string]string{"pod": "web-1"}, ch.starts[0].detail)

	g := newFolder(t)
	g.at(0, seeing(crash.Name, "web"))
	g.every(30*time.Second, 5*time.Minute, nothing())
	assert.Empty(t, g.state.pairs, "dropped after 5 judged minutes absent")
	g.at(330*time.Second, seeing(crash.Name, "web"))
	assert.Empty(t, g.all.starts, "a new window")
}

// An occurrence resolves at the judged fold where the pair has been absent for
// resolve-after, stamped with its last sighting, and its pair is dropped.
func TestAnOccurrenceResolvesAfterResolveAfterAtItsLastSighting(t *testing.T) {
	f := newFolder(t)
	f.every(0, 3*time.Minute, seeing(crash.Name, "web"))
	require.Len(t, f.all.starts, 1)
	f.every(210*time.Second, 450*time.Second, nothing())
	require.Empty(t, f.all.ends)

	ch := f.at(8*time.Minute, nothing())

	require.Len(t, ch.ends, 1)
	assert.Equal(t, end{f.all.starts[0].occurrenceID, f.all.starts[0].incidentID, t0.Add(3 * time.Minute), EndResolved}, ch.ends[0])
	assert.Empty(t, f.state.pairs)
	assert.True(t, ch.notifiesIncidents())
}

// A signal whose resolve-after is zero ends at the first judged fold without it,
// and one whose open-after is zero starts at its first sighting.
func TestAZeroWindowActsAtTheNextJudgedFold(t *testing.T) {
	f := newFolder(t)
	ch := f.at(0, seeing(flip.Name, "web"))
	require.Len(t, ch.starts, 1)
	f.at(30*time.Second, unknown(CodeUnavailable))
	require.Empty(t, f.all.ends, "an unknown fold ends nothing")

	ch = f.at(time.Minute, nothing())

	require.Len(t, ch.ends, 1)
	assert.Equal(t, EndResolved, ch.ends[0].reason)
	assert.Equal(t, t0, ch.ends[0].at)
}

// An unknown signal's pairs stand still, and the first judged fold after an
// unknown stretch earns nothing: a crash loop seen once, unknown for minutes,
// then seen again opens only once it has held 2 judged minutes.
func TestAnUnknownStretchEarnsNothing(t *testing.T) {
	f := newFolder(t)
	f.at(0, seeing(crash.Name, "web"))
	f.every(30*time.Second, 10*time.Minute, unknown(CodeNotReady))
	f.every(630*time.Second, 720*time.Second, seeing(crash.Name, "web"))
	require.Empty(t, f.all.starts, "630 earns nothing; 660 to 720 earn 90 seconds")

	ch := f.at(750*time.Second, seeing(crash.Name, "web"))

	require.Len(t, ch.starts, 1)
	assert.Equal(t, t0, ch.starts[0].startedAt)
}

// With the Pods kind unknown for 10 minutes, an open occurrence whose signal is
// then gone resolves 5 judged minutes later, not at once.
func TestAnOpenOccurrenceResolvesOnJudgedAbsenceAlone(t *testing.T) {
	f := newFolder(t)
	f.every(0, 2*time.Minute, seeing(crash.Name, "web"))
	require.Len(t, f.all.starts, 1)
	f.every(150*time.Second, 750*time.Second, unknown(CodeNotReady))
	f.every(780*time.Second, 1050*time.Second, nothing())
	require.Empty(t, f.all.ends, "780 earns nothing; 810 to 1050 earn 270 seconds")

	ch := f.at(1080*time.Second, nothing())

	require.Len(t, ch.ends, 1)
	assert.Equal(t, t0.Add(2*time.Minute), ch.ends[0].at)
}

// Two folds 8 hours apart, the machine asleep between them, start and resolve
// nothing: a pair seen before and after has held no longer, and one gone after
// has been absent no time.
func TestAGapPastTwoCadencesEarnsNothing(t *testing.T) {
	f := newFolder(t)
	f.at(0, seeing(crash.Name, "web"))
	f.at(8*time.Hour, seeing(crash.Name, "web"))
	assert.Empty(t, f.all.starts)

	g := newFolder(t)
	g.every(0, 2*time.Minute, seeing(crash.Name, "web"))
	require.Len(t, g.all.starts, 1)
	g.at(2*time.Minute+8*time.Hour, nothing())
	assert.Empty(t, g.all.ends)
}

// A clock set back an hour while an occurrence is open earns nothing and ends it
// no earlier than its start; a second occurrence it starts may carry an earlier
// start, and is still the newest by its number.
func TestAClockSetBackEndsNoEarlierThanTheStart(t *testing.T) {
	f := newFolder(t)
	f.every(0, 2*time.Minute, seeing(flip.Name, "web"))
	require.Len(t, f.all.starts, 1)

	ch := f.at(-time.Hour, nothing())

	require.Len(t, ch.ends, 1)
	assert.Equal(t, t0.Add(2*time.Minute), ch.ends[0].at, "its last sighting, after its start")

	ch = f.at(-time.Hour+30*time.Second, seeing(flip.Name, "web"))
	require.Len(t, ch.starts, 1)
	assert.Equal(t, 2, ch.starts[0].number, "the newest, though it started earlier")
	assert.True(t, ch.starts[0].startedAt.Before(f.all.starts[0].startedAt))
}

// An open occurrence last seen more than 24 hours ago ends as stale, stamped with
// its last sighting, before the reading: a reading that sees the pair starts a
// new window, and when it opens it opens a new incident.
func TestStaleIsAppliedBeforeTheReading(t *testing.T) {
	f := newFolder(t)
	f.every(0, 2*time.Minute, seeing(crash.Name, "web"))
	require.Len(t, f.all.starts, 1)
	f.at(150*time.Second, unknown(CodeUnavailable))

	ch := f.at(24*time.Hour+3*time.Minute, seeing(crash.Name, "web"))

	require.Len(t, ch.ends, 1)
	assert.Equal(t, EndStale, ch.ends[0].reason)
	assert.Equal(t, t0.Add(2*time.Minute), ch.ends[0].at)
	assert.Empty(t, ch.starts, "the sighting waits its window again")
	f.every(24*time.Hour+210*time.Second, 24*time.Hour+5*time.Minute, seeing(crash.Name, "web"))
	require.Len(t, f.all.starts, 2)
	assert.NotNil(t, f.all.starts[1].open, "a new incident")
}

// A signal unknown with disabled ends its open occurrences as stopped at once and
// drops its pairs; one unknown for another reason is left as it is.
func TestADisabledSignalStopsItsOccurrences(t *testing.T) {
	f := newFolder(t)
	f.every(0, 2*time.Minute, seeing(crash.Name, "web"))
	f.at(150*time.Second, unknown(CodeUnavailable))
	require.Empty(t, f.all.ends)

	ch := f.at(3*time.Minute, unknown(CodeDisabled))

	require.Len(t, ch.ends, 1)
	assert.Equal(t, EndStopped, ch.ends[0].reason)
	assert.Equal(t, t0.Add(2*time.Minute), ch.ends[0].at)
	assert.Empty(t, f.state.pairs)
}

// A new UID ends the target's open occurrences as stopped at their last
// sightings and drops its pairs, ahead of the stale bound: a restart after 30
// hours onto another server stops, never stales. The new server's failure opens
// a new incident; one back on the first within a day joins the first's.
func TestANewUIDStopsTheOpenOccurrencesFirst(t *testing.T) {
	f := newFolder(t)
	f.every(0, 2*time.Minute, seeing(crash.Name, "web"))
	moved := seeing(crash.Name, "web")
	moved.UID = "u2"

	ch := f.at(30*time.Hour, moved)

	assert.Equal(t, "u2", ch.uid)
	require.Len(t, ch.ends, 1)
	assert.Equal(t, EndStopped, ch.ends[0].reason)
	assert.Equal(t, t0.Add(2*time.Minute), ch.ends[0].at)
	assert.True(t, ch.notifiesTargets())
	f.every(30*time.Hour+30*time.Second, 30*time.Hour+2*time.Minute, moved)
	require.Len(t, f.all.starts, 2)
	assert.NotNil(t, f.all.starts[1].open, "another server's failure is another incident")
	assert.Equal(t, "u2", f.all.starts[1].open.uid)

	g := newFolder(t)
	g.every(0, 2*time.Minute, seeing(crash.Name, "web"))
	g.every(150*time.Second, 30*time.Minute, func() Reading { r := nothing(); r.UID = "u2"; return r }())
	g.every(31*time.Minute, 33*time.Minute, seeing(crash.Name, "web"))
	require.Len(t, g.all.starts, 2)
	assert.Nil(t, g.all.starts[1].open, "back on the first server, it joins the first's incident")
	assert.Equal(t, g.all.starts[0].incidentID, g.all.starts[1].incidentID)
}

// A failure within 24 hours of its incident's last end adds an occurrence to it;
// later, it opens a new incident.
func TestAnOccurrenceJoinsItsIncidentWithin24Hours(t *testing.T) {
	f := newFolder(t)
	f.at(0, seeing(flip.Name, "web"))
	f.at(30*time.Second, nothing())
	f.at(time.Hour, seeing(flip.Name, "web"))
	f.at(time.Hour+30*time.Second, nothing())
	f.at(26*time.Hour, seeing(flip.Name, "web"))

	require.Len(t, f.all.starts, 3)
	assert.Equal(t, f.all.starts[0].incidentID, f.all.starts[1].incidentID)
	assert.Equal(t, 2, f.all.starts[1].number)
	assert.Nil(t, f.all.starts[1].open)
	assert.NotEqual(t, f.all.starts[0].incidentID, f.all.starts[2].incidentID)
	assert.Equal(t, 1, f.all.starts[2].number)
}

// A reading that observes one pair twice keeps the first; another subject is
// another pair.
func TestAReadingThatObservesAPairTwiceKeepsTheFirst(t *testing.T) {
	f := newFolder(t)
	r := seeing(flip.Name, "web", "web", "api")
	r.Observations[1].Detail = map[string]string{"pod": "second"}

	ch := f.at(0, r)

	require.Len(t, ch.starts, 2)
	assert.Equal(t, map[string]string{"pod": "web-0"}, ch.starts[0].detail)
}

// The fold writes the unknown signals only when they change, and notes the
// target's first reading once.
func TestTheUnknownSignalsAreWrittenWhenTheyChange(t *testing.T) {
	f := newFolder(t)
	ch := f.at(0, nothing())
	assert.True(t, ch.firstRead)
	assert.Nil(t, ch.unknown, "the stored '{}' already says so")

	ch = f.at(30*time.Second, nothing())
	assert.False(t, ch.firstRead)
	assert.Nil(t, ch.unknown)
	assert.False(t, ch.notifiesTargets())

	ch = f.at(time.Minute, unknown(CodeTimeout))
	assert.Equal(t, map[string]ReadErrorCode{crash.Name: CodeTimeout, flip.Name: CodeTimeout}, ch.unknown)
}

// The fold changes nothing it was handed, so a state whose commit failed folds
// the next reading to the same changes.
func TestAFoldLeavesItsInputAsItWas(t *testing.T) {
	f := newFolder(t)
	f.every(0, 90*time.Second, seeing(crash.Name, "web"))
	before := f.state
	reg := testRegistration()
	at := t0.Add(2 * time.Minute)
	ids := func() string { return "same" }

	_, first := fold(before, reg, seeing(crash.Name, "web"), at, ids)
	_, again := fold(before, reg, seeing(crash.Name, "web"), at, ids)

	assert.Equal(t, first, again)
	assert.Equal(t, 90*time.Second, before.pairs[pairKey{crash.Name, "web"}].held)
}

// The fold forgets an incident that ended past any start that could join it.
func TestTheFoldForgetsAnIncidentLongEnded(t *testing.T) {
	f := newFolder(t)
	f.at(0, seeing(flip.Name, "web"))
	f.at(30*time.Second, nothing())
	require.Len(t, f.state.incidents, 1)

	f.at(recentFor+time.Minute, nothing())

	assert.Empty(t, f.state.incidents)
}

// A pair last seen, with the clock set back, before it was first seen ends at its
// first sighting.
func TestAnEndIsNeverBeforeTheFirstSighting(t *testing.T) {
	f := newFolder(t)
	f.at(0, seeing(flip.Name, "web"))
	f.at(-time.Hour, seeing(flip.Name, "web"))

	ch := f.at(-time.Hour+30*time.Second, nothing())

	require.Len(t, ch.ends, 1)
	assert.Equal(t, t0, ch.ends[0].at)
}
