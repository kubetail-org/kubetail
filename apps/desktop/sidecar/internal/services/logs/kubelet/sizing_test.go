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

package kubelet

import (
	"math"
	"testing"
	"time"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/logs/logstest"
	"github.com/stretchr/testify/assert"
)

// A window holds about need kept lines with half again for a margin, and no more
// than its byte target, in whole seconds and at least one.
func TestAWindowIsSizedByKeptLinesAndBytes(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		r    Rate
		want time.Duration
	}{
		"by kept lines":       {Rate{Kept: 10, Bytes: 1 << 10}, 76 * time.Second},
		"by bytes":            {Rate{Kept: 10, Bytes: 1 << 20}, 4 * time.Second},
		"no kept lines":       {Rate{Bytes: 1 << 19}, 8 * time.Second},
		"at least one second": {Rate{Kept: 5000, Bytes: 1 << 20}, time.Second},
		"a rate past measure": {Rate{Kept: math.Inf(1), Bytes: math.Inf(1)}, time.Second},
		"no bytes":            {Rate{}, maxReach},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, windowFor(501, tc.r, 4<<20))
		})
	}
}

// A line is counted as the log endpoint's body carries it: its timestamp, a space,
// its message and a newline.
func TestALineCountsItsWireBytes(t *testing.T) {
	t.Parallel()
	assert.EqualValues(t, len(logstest.Stamped(time.Second, "hello"))+1, WireBytes(at(time.Second), "hello"))
}

// A sample's rate is over the time its lines span, at least one second; one of fewer
// than ten lines has none.
func TestASampleMeasuresItsRate(t *testing.T) {
	t.Parallel()
	near := func(n int, every time.Duration, grep func(int) bool) (Rate, bool) {
		var s Sample
		for i := range n {
			ts := at(time.Duration(i) * every)
			s.Add(ts, WireBytes(ts, "0123456789"), grep(i))
		}
		return s.Rate()
	}
	all := func(int) bool { return true }
	var bytes int64
	for i := range 21 {
		bytes += WireBytes(at(time.Duration(i)*500*time.Millisecond), "0123456789")
	}

	r, ok := near(21, 500*time.Millisecond, func(i int) bool { return i%2 == 0 })
	assert.True(t, ok)
	assert.InDelta(t, 11/10.0, r.Kept, 1e-9)
	assert.InDelta(t, float64(bytes)/10, r.Bytes, 1e-9)

	r, ok = near(10, 0, all)
	assert.True(t, ok, "lines all in one moment")
	assert.InDelta(t, 10, r.Kept, 1e-9)

	_, ok = near(9, time.Second, all)
	assert.False(t, ok, "fewer than ten lines")
}
