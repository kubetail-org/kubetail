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

package supervisor

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

var runAt = time.Date(2026, 8, 24, 10, 5, 0, 0, time.UTC)

// recorded is the attempt one finished run leaves, built the way the supervisor builds it.
func recorded(res Result, at time.Time) Attempt { return attemptOf(res, at, time.Time{}, at) }

func failedAt(at time.Time) Attempt {
	return Attempt{FinishedAt: at, Verdict: VerdictFailed, Reason: "Unreachable"}
}

func TestConsecutiveFailuresAreOneStreak(t *testing.T) {
	var a Attempts

	a.record(failedAt(runAt))
	a.record(failedAt(runAt.Add(time.Minute)))

	assert.Equal(t, 2, a.Failures)
	assert.Equal(t, runAt, a.FailingSince, "the streak began at the first failure")
	assert.False(t, a.OK())
}

func TestASuccessEndsTheStreak(t *testing.T) {
	a := Attempts{Failures: 3, FailingSince: runAt}

	a.record(Attempt{FinishedAt: runAt, Verdict: VerdictSucceeded, Reason: ReasonSucceeded})

	assert.True(t, a.OK())
	assert.Zero(t, a.Failures)
	assert.True(t, a.FailingSince.IsZero())
}

// A suspension parks a question rather than failing at it, so it ends a streak the way a
// success does — without claiming one.
func TestASuspensionEndsTheStreakWithoutASuccess(t *testing.T) {
	a := Attempts{Failures: 2, FailingSince: runAt}

	a.record(Attempt{FinishedAt: runAt.Add(time.Minute), Verdict: VerdictSuspended, Reason: "ContextNotFound"})

	assert.False(t, a.OK())
	assert.Zero(t, a.Failures)
	assert.True(t, a.FailingSince.IsZero())
}

func TestFailMessageDefaultsToTheError(t *testing.T) {
	r := Fail("ResolveFailed", errors.New("open ca.crt: no such file"))

	assert.Equal(t, VerdictFailed, r.verdict)
	assert.Equal(t, "open ca.crt: no such file", r.message)
}

// The message outlives the log line: a caller folds it onto a record, which is persisted and
// served to the UI. A probe's error is a client-go error, and that quotes the request URL.
// Err keeps the original, since a caller matching on it must see what actually happened.
func TestFailRendersTheMessageItStores(t *testing.T) {
	err := errors.New(`Get "https://api.example.com/version?token=SEKRIT": 401 Unauthorized`)

	r := Fail("ProbeFailed", err)

	assert.NotContains(t, r.message, "SEKRIT")
	assert.Contains(t, r.message, "401 Unauthorized")
	assert.Equal(t, err, r.err)
}

func TestTheZeroResultIsInvalid(t *testing.T) {
	assert.Equal(t, resultInvalid, Result{}.kind, "constructors are the only way to a valid Result")
	assert.NotEqual(t, resultInvalid, Skip().kind)
}

// The read side is what a body's own tests assert on, without giving a body a way to build
// a Result the constructors cannot.
func TestAResultReportsWhatItWasBuiltFrom(t *testing.T) {
	err := errors.New("open ca.crt: no such file")

	failed := Fail("ResolveFailed", err)
	assert.Equal(t, VerdictFailed, failed.Verdict())
	assert.Equal(t, Reason("ResolveFailed"), failed.Reason())
	assert.Equal(t, err.Error(), failed.Message())
	assert.ErrorIs(t, failed.Err(), err)
	assert.False(t, failed.IsSkip())

	suspended := Suspend("ContextNotFound", "the file no longer names it")
	assert.Equal(t, VerdictSuspended, suspended.Verdict())
	assert.Equal(t, "the file no longer names it", suspended.Message())
	assert.NoError(t, suspended.Err())

	assert.True(t, Skip().IsSkip())
	assert.Equal(t, ReasonSucceeded, Succeeded().Reason())
}

// Latency is the run's own time, which only a dispatched run that finished has: a run still out
// has none to report, and one the supervisor recorded without dispatching never started.
func TestLatencyIsOnlyForARunThatWasDispatchedAndFinished(t *testing.T) {
	assert.Zero(t, Attempt{StartedAt: runAt}.Latency(), "still running")
	assert.Zero(t, Attempt{FinishedAt: runAt}.Latency(), "recorded, never dispatched")

	ran := Attempt{StartedAt: runAt, FinishedAt: runAt.Add(2 * time.Second)}
	assert.Equal(t, 2*time.Second, ran.Latency())
}

// A healthy stretch begins at the first success and stands across the ones that follow: what a
// caller asks is how long this has been working, not when it last worked.
func TestAHealthyStretchStandsAcrossTheSuccessesInsideIt(t *testing.T) {
	a := Attempts{}

	a.record(recorded(Succeeded(), runAt))
	assert.Equal(t, runAt, a.HealthySince)
	assert.Zero(t, a.Restarts, "the stretch's own first run is not a restart")

	a.record(recorded(Succeeded(), runAt.Add(time.Minute)))
	assert.Equal(t, runAt, a.HealthySince, "the stretch did not restart")
}

// Restarts is what HealthySince makes countable: the flapping question Failures cannot answer,
// since a thing that goes down and comes back inside a healthy stretch never fails.
func TestRestartsCountTheRunsInsideAHealthyStretch(t *testing.T) {
	a := Attempts{}
	a.record(recorded(Succeeded(), runAt))

	a.begin(runAt.Add(time.Minute))
	assert.Equal(t, 1, a.Restarts)
	a.record(recorded(Succeeded(), runAt.Add(2*time.Minute)))
	a.begin(runAt.Add(3 * time.Minute))
	assert.Equal(t, 2, a.Restarts)

	assert.Zero(t, a.Failures, "nothing failed across either")
}

// A failure ends the stretch, so "healthy for an hour" cannot survive the hour being broken. A
// suspension ends it too: it parks the question rather than failing at it, but nothing is
// running either way.
func TestAFailureAndASuspensionBothEndTheHealthyStretch(t *testing.T) {
	failed := Attempts{HealthySince: runAt, Restarts: 7}
	failed.record(failedAt(runAt.Add(time.Minute)))
	assert.True(t, failed.HealthySince.IsZero())
	assert.Zero(t, failed.Restarts)

	suspended := Attempts{HealthySince: runAt, Restarts: 7}
	suspended.record(recorded(Suspend("NoConnection", "waiting"), runAt.Add(time.Minute)))
	assert.True(t, suspended.HealthySince.IsZero())
	assert.Zero(t, suspended.Restarts)
}

// A worker is healthy from the moment it says so, not from the exit that follows — so the stretch
// is open while it runs. **The streak it inherited stands**, because starting is not proof: what
// clears one is a run that finished cleanly.
func TestReadyOpensTheHealthyStretchAndLeavesTheStreakStanding(t *testing.T) {
	a := Attempts{Failures: 3, FailingSince: runAt}
	a.begin(runAt)

	a.markReady(runAt.Add(time.Second))

	assert.Equal(t, runAt.Add(time.Second), a.HealthySince)
	assert.True(t, a.Ready(), "running, and it said it was up")
	assert.Equal(t, 3, a.Failures, "the ladder still stands behind it")

	// The clean exit is what clears it, and the stretch Ready opened outlives that exit.
	a.record(recorded(Succeeded(), runAt.Add(time.Minute)))
	assert.Zero(t, a.Failures)
	assert.Equal(t, runAt.Add(time.Second), a.HealthySince)
}

// Ready is a worker's word, so a job — which stamps none — is never ready, and neither is a
// worker between its exit and its next start.
func TestReadyIsFalseWithoutARunThatSaidSo(t *testing.T) {
	var idle Attempts
	assert.False(t, idle.Ready(), "nothing has run")

	job := Attempts{}
	job.begin(runAt)
	assert.False(t, job.Ready(), "a job stamps no ReadyAt")

	exited := Attempts{}
	exited.begin(runAt)
	exited.markReady(runAt)
	exited.record(recorded(Succeeded(), runAt.Add(time.Minute)))
	exited.schedule(runAt.Add(2 * time.Minute))
	assert.False(t, exited.Ready(), "it is down, whatever it proved while it was up")
}
