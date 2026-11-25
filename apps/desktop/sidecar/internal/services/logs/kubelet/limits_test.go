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
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
)

// Each stream a query fills is allowed the smaller of 16 MiB and 64 MiB ÷ the
// streams, and the pool starts with what the allowances leave; a first window
// targets a quarter of its allowance.
func TestAQueryAllowsEachStreamItsShare(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		streams           int
		allowance, pooled int64
	}{
		"one":     {1, 16 << 20, 48 << 20},
		"four":    {4, 16 << 20, 0},
		"sixteen": {16, 4 << 20, 0},
		"hundred": {100, (64 << 20) / 100, (64 << 20) % 100},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			ls := QueryLimits(DefaultQuota, tc.streams)

			assert.Len(t, ls, tc.streams)
			assert.Equal(t, tc.allowance, ls[0].allowance)
			assert.Equal(t, tc.pooled, ls[0].Pool.Load())
			assert.Equal(t, tc.allowance/4, ls[0].target())
		})
	}
}

// A stream spends its allowance until its first window ends, then the pool: what a
// window reserved and did not use goes back to the pool, with what is left of the
// allowance, and what it read past its reservation is charged to the pool.
func TestAWindowsReservationIsChargedNotEnforced(t *testing.T) {
	t.Parallel()
	l := limitsOf(DefaultQuota)
	l.charge(100)
	assert.Equal(t, (16<<20-100)/4, int(l.take()))
	l.charge(1 << 20)
	l.windowEnded()

	assert.Equal(t, int64(64<<20-100-1<<20), l.Pool.Load())
	assert.Equal(t, int64(4<<20), l.target(), "a later window's target is at most 4 MiB")

	l.take()
	assert.Equal(t, int64(64<<20-100-1<<20-16<<20), l.Pool.Load())
	l.charge(17 << 20)
	l.windowEnded()

	assert.Equal(t, int64(64<<20-100-1<<20-17<<20), l.Pool.Load())
	assert.Equal(t, int64(100+18<<20), l.Spent)
}

// A stream is cut at a window's end once it has spent its bytes or sent its GETs,
// or its next window's target would be under the smallest.
func TestAStreamReachesItsLimits(t *testing.T) {
	t.Parallel()
	ended := func(l *Limits) *Limits {
		l.take()
		l.windowEnded()
		return l
	}
	l := ended(limitsOf(DefaultQuota))
	assert.False(t, l.reached())

	l = ended(limitsOf(DefaultQuota))
	l.charge(16 << 20)
	assert.True(t, l.reached(), "bytes")

	l = ended(limitsOf(DefaultQuota))
	for range 16 {
		l.get()
	}
	assert.True(t, l.reached(), "GETs")

	l = ended(QueryLimits(DefaultQuota, 100)[0])
	l.Pool.left = 4*DefaultQuota.MinWindow - 1
	assert.True(t, l.reached(), "target")
}

// A forward read spends its allowance, then the pool, and is spent once both are
// empty or the stream has read its 16 MiB.
func TestAForwardReadSpendsItsAllowanceThenThePool(t *testing.T) {
	t.Parallel()
	l := limitsOf(DefaultQuota)
	l.charge(16<<20 - 1)
	assert.False(t, l.forwardSpent())
	l.charge(1)
	assert.True(t, l.forwardSpent(), "16 MiB")

	l = QueryLimits(DefaultQuota, 4)[0]
	l.charge(16<<20 - 1)
	assert.False(t, l.forwardSpent())

	l = QueryLimits(DefaultQuota, 100)[0]
	l.Pool.left = 0
	l.charge(l.allowance)
	assert.True(t, l.forwardSpent(), "allowance and pool")
}

// The tailLines window asks for the lines the read needs, no more than its allowance
// holds at a part a line, and at least minBatch.
func TestTheTailLinesWindowIsCappedByItsAllowance(t *testing.T) {
	t.Parallel()
	assert.Equal(t, int64(501), limitsOf(DefaultQuota).tailLines(501))
	assert.Equal(t, int64(40), QueryLimits(DefaultQuota, 100)[0].tailLines(501))
	assert.Equal(t, int64(minBatch), QueryLimits(DefaultQuota, 100)[0].tailLines(1))
	var none *Limits
	assert.Equal(t, int64(501), none.tailLines(501))
}

// A follow's fill has an allowance and no pool, and no second stops it.
func TestATailsFillReadsItsShare(t *testing.T) {
	t.Parallel()
	l := FollowLimits(DefaultQuota, 4)
	assert.Equal(t, int64(16<<20), l.allowance)
	assert.Equal(t, int64(0), l.Pool.Load())
	assert.Equal(t, int64(0), l.second())
	assert.Equal(t, int64((64<<20)/100), FollowLimits(DefaultQuota, 100).allowance)
}

// No limits leave every byte: a window is sized to the byte target alone, and
// nothing cuts or stops the read.
func TestNoLimitsLeaveEveryByte(t *testing.T) {
	t.Parallel()
	var l *Limits
	l.charge(1 << 40)
	l.get()
	l.windowEnded()
	assert.Equal(t, int64(byteTarget), l.take())
	assert.Equal(t, int64(byteTarget), l.target())
	assert.False(t, l.reached())
	assert.False(t, l.forwardSpent())
	assert.Equal(t, int64(0), l.second())
}

// A later window targets a quarter of its stream's share of the pool, the pool
// divided by the streams still reading from it with no reservation out, so every
// stream reserving finds the same share; a stream whose fill has ended shares no
// longer.
func TestLaterWindowsShareThePool(t *testing.T) {
	t.Parallel()
	ls := QueryLimits(DefaultQuota, 16)
	for _, l := range ls {
		l.charge(1 << 20)
		l.windowEnded()
	}
	assert.Equal(t, int64(48<<20), ls[0].Pool.Load())

	assert.Equal(t, int64(768<<10), ls[0].target())
	for _, l := range ls {
		assert.Equal(t, int64(768<<10), l.take())
	}
	assert.Equal(t, int64(0), ls[0].Pool.Load())
	for _, l := range ls[1:] {
		l.Ended()
	}
	ls[0].windowEnded()
	assert.Equal(t, int64(48<<20), ls[0].Pool.Load())
	assert.Equal(t, int64(byteTarget), ls[0].target())
}

// Streams reading at once stay within the page: a target and its reservation are
// taken together, so later windows reserve no more than the pool holds.
func TestStreamsReadingAtOnceStayWithinThePage(t *testing.T) {
	t.Parallel()
	ls := QueryLimits(DefaultQuota, 100)
	for _, l := range ls {
		l.windowEnded()
	}
	pooled := ls[0].Pool.Load()
	var wg sync.WaitGroup
	held := make([]int64, len(ls))
	for i, l := range ls {
		wg.Go(func() { held[i] = 4 * l.take() })
	}
	wg.Wait()

	var sum int64
	for _, h := range held {
		sum += h
	}
	assert.LessOrEqual(t, sum, pooled)
	assert.GreaterOrEqual(t, ls[0].Pool.Load(), int64(0))
}
