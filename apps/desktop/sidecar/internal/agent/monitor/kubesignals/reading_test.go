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

package kubesignals

import (
	"errors"
	"fmt"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/agent/monitor"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/cluster"
)

var (
	podSignals      = []string{SignalCrashLoop, SignalImagePull, SignalPodPending}
	workloadSignals = []string{SignalReplicasUnavailable, SignalRolloutStalled}
)

// unknownOf is each of signals unknown with code.
func unknownOf(code monitor.ReadErrorCode, signals ...string) map[string]monitor.ReadErrorCode {
	out := map[string]monitor.ReadErrorCode{}
	for _, s := range signals {
		out[s] = code
	}
	return out
}

// withoutPods is a watching health with Pods taken out of the watching kinds.
func withoutPods() cluster.ClusterCacheHealth {
	h := watching()
	h.WatchingKindRefs = slices.DeleteFunc(h.WatchingKindRefs, func(k cluster.KindRef) bool { return k == kindPod })
	return h
}

// A signal is judged only while every kind it requires watches. With the Pods
// kind paused, stale or failing, the three pod signals and the two workload
// signals read unknown with the kind's code, and node_not_ready is still judged.
func TestASignalIsJudgedOnlyWhileItsKindsWatch(t *testing.T) {
	for name, c := range map[string]struct {
		pods   cluster.UnhealthyKindRef
		paused bool
		want   monitor.ReadErrorCode
	}{
		"paused":        {paused: true, want: monitor.CodeDisabled},
		"stale":         {pods: cluster.UnhealthyKindRef{Reason: "Stale"}, want: monitor.CodeUnavailable},
		"sync failed":   {pods: cluster.UnhealthyKindRef{Reason: "SyncFailed"}, want: monitor.CodeUnavailable},
		"forbidden":     {pods: cluster.UnhealthyKindRef{Reason: "SyncFailed", Failure: "Forbidden"}, want: monitor.CodePermissionDenied},
		"unauthorized":  {pods: cluster.UnhealthyKindRef{Reason: "SyncFailed", Failure: "Unauthorized"}, want: monitor.CodeUnauthenticated},
		"resyncing":     {pods: cluster.UnhealthyKindRef{Reason: "Resyncing"}, want: monitor.CodeNotReady},
		"no connection": {pods: cluster.UnhealthyKindRef{Reason: "NoConnection"}, want: monitor.CodeUnavailable},
		"in no list":    {want: monitor.CodeNotReady},
	} {
		t.Run(name, func(t *testing.T) {
			f := newFake(t, node("worker-1", "False", "", ""), pod("uid-1", "solo", nil, "Running", crashing("app")))
			f.health = withoutPods()
			switch {
			case c.paused:
				f.health.PausedKindRefs = []cluster.KindRef{kindPod}
			case c.pods.Reason != "":
				c.pods.KindRef = kindPod
				f.health.UnhealthyKindRefs = []cluster.UnhealthyKindRef{c.pods}
			}

			r := readingOf(t, f)

			assert.Equal(t, unknownOf(c.want, append(podSignals, workloadSignals...)...), r.Unknown)
			assert.Empty(t, observed(r, SignalCrashLoop))
			assert.Contains(t, observed(r, SignalNodeNotReady), "/Node//worker-1", "the node signal is judged")
		})
	}
}

// A cache whose cluster sync is off reads Paused with no kind listed, and judges
// nothing, with disabled; a reading names the server its cache reaches.
func TestAPausedCacheJudgesNothing(t *testing.T) {
	f := newFake(t)
	f.health = cluster.ClusterCacheHealth{Reason: cluster.ReasonPaused}

	r := readingOf(t, f)

	assert.Equal(t, "server-1", r.UID)
	assert.Equal(t, unknownOf(monitor.CodeDisabled, signalNames()...), r.Unknown)
}

// An answer cut at its bound opens and resolves nothing for its signals, which
// read truncated.
func TestACutAnswerIsTruncated(t *testing.T) {
	f := newFake(t, pod("uid-1", "solo", nil, "Running", crashing("app")))
	f.cut = "'CrashLoopBackOff')"

	r := readingOf(t, f)

	assert.Equal(t, unknownOf(monitor.CodeTruncated, SignalCrashLoop), r.Unknown)
	assert.Empty(t, observed(r, SignalCrashLoop))
}

// A statement that fails fails the reading.
func TestAStatementThatFailsFailsTheReading(t *testing.T) {
	f := newFake(t)
	f.queryErr = errors.New("unreadable")

	_, err := (&integration{svc: f}).Read(t.Context(), monitor.Target{Key: "c1"})

	require.ErrorIs(t, err, f.queryErr)
	assert.ErrorContains(t, err, fmt.Sprintf("read %s", SignalCrashLoop))
}

// signalNames is every signal's name.
func signalNames() []string {
	var out []string
	for _, s := range Signals {
		out = append(out, s.Name)
	}
	return out
}
