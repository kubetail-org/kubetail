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
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

var runAt = time.Date(2026, 8, 23, 10, 5, 0, 0, time.UTC)

// A probe the prober records without dispatching has no duration: subtracting a zero
// StartedAt would report the time since the zero year.
func TestLatencyIsZeroForARunThatNeverStarted(t *testing.T) {
	t.Parallel()

	a := Attempt{ScheduledAt: runAt, FinishedAt: runAt, Reason: ReasonDependencyFailed}

	assert.True(t, a.Done())
	assert.False(t, a.Running())
	assert.Zero(t, a.Latency())
}

func TestLatencyMeasuresADispatchedRun(t *testing.T) {
	t.Parallel()

	a := Attempt{ScheduledAt: runAt, StartedAt: runAt, FinishedAt: runAt.Add(2 * time.Second)}

	assert.Equal(t, 2*time.Second, a.Latency())
}

// A suspended probe keeps its last answer and schedules nothing.
func TestSuspendedProbeKeepsItsAnswer(t *testing.T) {
	t.Parallel()

	o := Observation[string]{
		Value:      "abc-123",
		LastSeenAt: runAt,
		Attempts: Attempts{
			LastAttempt: Attempt{ScheduledAt: runAt, FinishedAt: runAt, Verdict: VerdictSuspended, Reason: ReasonDependencyFailed},
		},
	}

	assert.True(t, o.Known(), "the UID it read is still the UID it read")
	assert.False(t, o.OK())
	assert.False(t, o.Scheduled(), "nothing is due until the connection comes back")
	assert.False(t, o.InFlight())
}

// Identity projects the three scalars a connection is scoped to out of the observations
// carrying them, so retiring one stays a ==.
func TestIdentityProjectsTheProbedScalars(t *testing.T) {
	t.Parallel()

	s := State{
		ServerUID:     Observation[string]{Value: "uid-1", LastSeenAt: runAt},
		ServerVersion: Observation[VersionInfo]{Value: VersionInfo{GitVersion: "v1.29.3"}, LastSeenAt: runAt},
		Principal: Observation[Principal]{
			Value:      Principal{Username: "admin@example", Groups: []string{"system:masters"}},
			LastSeenAt: runAt,
		},
	}

	assert.Equal(t, Identity{
		ServerUID:     "uid-1",
		ServerVersion: "v1.29.3",
		Username:      "admin@example",
	}, s.Identity())
}

// A part no probe could read is empty rather than absent, which is what lets two
// connections missing the same part compare equal.
func TestIdentityLeavesAnUnreadPartEmpty(t *testing.T) {
	t.Parallel()

	forbidden := State{
		ServerUID: Observation[string]{Attempts: Attempts{LastAttempt: Attempt{FinishedAt: runAt, Reason: ReasonForbidden}}},
		Principal: Observation[Principal]{Value: Principal{Username: "reader@example"}, LastSeenAt: runAt},
	}

	assert.Equal(t, Identity{Username: "reader@example"}, forbidden.Identity())
	assert.Equal(t, forbidden.Identity(), State{
		ServerUID: Observation[string]{Attempts: Attempts{LastAttempt: Attempt{FinishedAt: runAt, Reason: ReasonUnsupported}}},
		Principal: Observation[Principal]{Value: Principal{Username: "reader@example"}, LastSeenAt: runAt},
	}.Identity(), "why the UID is missing is the observation's, not the identity's")
}

// The phase a connection that answered reports, and the case every reader of Phase is waiting
// for.
func TestPhaseReadsASucceededConnectionAsProbed(t *testing.T) {
	t.Parallel()

	s := State{Connection: Observation[string]{
		Attempts: Attempts{LastAttempt: Attempt{FinishedAt: runAt, Verdict: VerdictSucceeded, Reason: ReasonSucceeded}},
	}}

	assert.Equal(t, PhaseProbed, s.Phase())
}
