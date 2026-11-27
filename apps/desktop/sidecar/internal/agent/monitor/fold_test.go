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
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

var (
	t0   = time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	loop = pairOf("apps", "Deployment", "prod", "web", CrashLoopBackOff)
)

// seeing is a reading that judges every signal and finds pairs.
func seeing(pairs ...Pair) reading {
	return reading{pairs: setOf(pairs...), judged: setOf(signals...)}
}

// folds runs fold over each reading in turn, one every step from t0, and returns
// the last state and every change made.
func folds(step time.Duration, readings ...reading) (clusterState, changes) {
	state := clusterState{}
	var all changes
	for i, r := range readings {
		var c changes
		state, c = fold(defaultWindows(), state, r, t0.Add(time.Duration(i)*step))
		all.starts = append(all.starts, c.starts...)
		all.ends = append(all.ends, c.ends...)
	}
	return state, all
}

func repeat(r reading, n int) []reading { return slices.Repeat([]reading{r}, n) }

func TestAnOccurrenceStartsWhenItsWindowHasPassed(t *testing.T) {
	// 0s … 1m30s: not yet.
	_, c := folds(30*time.Second, repeat(seeing(loop), 4)...)
	assert.Empty(t, c.starts)

	// 2m: one occurrence, stamped when it was first seen.
	_, c = folds(30*time.Second, repeat(seeing(loop), 5)...)
	assert.Equal(t, []start{{pair: loop, at: t0, seen: t0.Add(2 * time.Minute)}}, c.starts)
}

// A crash loop's container runs between back-offs. A gap under 5 minutes keeps the
// window it started.
func TestAShortGapKeepsTheWindow(t *testing.T) {
	none := seeing()
	_, c := folds(30*time.Second, seeing(loop), none, seeing(loop), none, seeing(loop))
	assert.Equal(t, []start{{pair: loop, at: t0, seen: t0.Add(2 * time.Minute)}}, c.starts)
	assert.Empty(t, c.ends)
}

func TestAnOccurrenceEndsAfterFiveMinutesAbsent(t *testing.T) {
	readings := append(repeat(seeing(loop), 5), repeat(seeing(), 9)...)
	state, c := folds(30*time.Second, readings...)
	assert.Empty(t, c.ends, "4m30s absent")

	state, c = fold(defaultWindows(), state, seeing(), t0.Add(7*time.Minute))
	assert.Equal(t, []end{{pair: loop, at: t0.Add(2 * time.Minute)}}, c.ends, "stamped when last seen")
	assert.Empty(t, state, "the pair is forgotten")
}

// unjudged is a reading that judges nothing, as one taken while the cache syncs.
var unjudged = reading{}

func TestAnUnjudgedSignalLeavesItsPairs(t *testing.T) {
	readings := append(repeat(seeing(loop), 5), repeat(unjudged, 30)...)
	state, c := folds(30*time.Second, readings...)
	assert.Empty(t, c.ends)
	assert.True(t, state[loop].open)
}

// Ten unjudged minutes, then the signal judged and gone: the occurrence ends after
// 5 judged minutes, not at once.
func TestAbsenceCountsOnlyJudgedTime(t *testing.T) {
	readings := append(repeat(seeing(loop), 5), repeat(unjudged, 20)...)
	readings = append(readings, repeat(seeing(), 9)...)
	_, c := folds(30*time.Second, readings...)
	assert.Empty(t, c.ends, "4m30s judged absent")

	_, c = folds(30*time.Second, append(readings, seeing())...)
	assert.Equal(t, []end{{pair: loop, at: t0.Add(2 * time.Minute)}}, c.ends)
}

// Each fold that sees an open pair moves its incident's last-seen time, which alone is
// no change a reader is told of.
func TestAnOpenPairIsSeenEachFold(t *testing.T) {
	state, c := folds(30*time.Second, repeat(seeing(loop), 5)...)
	assert.True(t, c.notifies(), "a start")
	assert.Empty(t, c.seen, "a start stamps its own last-seen time")

	_, c = fold(defaultWindows(), state, seeing(loop), t0.Add(150*time.Second))
	assert.Equal(t, []seen{{pair: loop, at: t0.Add(150 * time.Second)}}, c.seen)
	assert.False(t, c.notifies())

	_, c = fold(defaultWindows(), clusterState{}, seeing(loop), t0)
	assert.Empty(t, c.seen, "no occurrence is open")
}

// A restart loses the fold's memory, so a cluster's first reading seeds a pair per
// open occurrence, last seen when its incident was. The time the app was down is
// absence; the time its cache then took to answer is not.
func TestARestartSeedsOpenOccurrences(t *testing.T) {
	open := []openOccurrence{{pair: loop, startedAt: t0.Add(-time.Hour), lastSeen: t0}}

	// Down 6 minutes: gone past 5, so it ends when last seen.
	_, c := fold(defaultWindows(), seed(open, t0.Add(6*time.Minute), t0.Add(6*time.Minute)), seeing(), t0.Add(6*time.Minute))
	assert.Equal(t, []end{{pair: loop, at: t0}}, c.ends)

	// Down 3 minutes, then back: the same occurrence.
	state, c := fold(defaultWindows(), seed(open, t0.Add(3*time.Minute), t0.Add(3*time.Minute)), seeing(), t0.Add(3*time.Minute))
	assert.Empty(t, c.ends, "gone under 5 minutes")
	state, c = fold(defaultWindows(), state, seeing(loop), t0.Add(4*time.Minute))
	assert.Empty(t, c.starts)
	assert.Equal(t, []seen{{pair: loop, at: t0.Add(4 * time.Minute)}}, c.seen)
	assert.Equal(t, t0.Add(-time.Hour), state[loop].firstSeen)

	// Down 6 minutes, and the first reading judges nothing: the time down still counts.
	state, c = fold(defaultWindows(), seed(open, t0.Add(6*time.Minute), t0.Add(8*time.Minute)), unjudged, t0.Add(8*time.Minute))
	assert.Empty(t, c.ends)
	_, c = fold(defaultWindows(), state, seeing(), t0.Add(8*time.Minute+30*time.Second))
	assert.Equal(t, []end{{pair: loop, at: t0}}, c.ends)
}

// A pair seen, then unjudged for minutes, then seen again has held only for the time
// its signal was judged.
func TestUnjudgedTimeDoesNotOpenAnOccurrence(t *testing.T) {
	readings := append([]reading{seeing(loop)}, repeat(unjudged, 9)...)
	_, c := folds(30*time.Second, append(readings, seeing(loop))...)
	assert.Empty(t, c.starts, "30 judged seconds")

	readings = append(readings, repeat(seeing(loop), 4)...)
	_, c = folds(30*time.Second, readings...)
	assert.Equal(t, []start{{pair: loop, at: t0, seen: t0.Add(13 * 30 * time.Second)}}, c.starts, "2 judged minutes, stamped when first seen")
}

// A workload mid-rollout is reported with a longer window: a rollout that stalls with
// no deadline of its own still opens an incident.
func TestARollingPairWaitsItsLongerWindow(t *testing.T) {
	short := pairOf("apps", "StatefulSet", "prod", "db", ReplicasUnavailable)
	rolling := seeing(short)
	rolling.rolling = setOf(short)
	_, c := folds(time.Minute, repeat(rolling, 15)...)
	assert.Empty(t, c.starts, "14 minutes")
	_, c = folds(time.Minute, repeat(rolling, 16)...)
	assert.Len(t, c.starts, 1, "15 minutes")
}
