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

import "sync"

// caps is the limits a page's reads keep to, as bytes the body carries.
type Quota struct {
	// second is the most of one second of one stream's log a page reads: a louder
	// second stops the stream there.
	Second int64
	// stream and gets are the bytes one stream downloads and the GETs it sends for
	// one query, and query the bytes all its streams download together.
	Stream int64
	Gets   int
	Query  int64
	// window is the most a later window is sized to hold, and minWindow the least:
	// under it the stream is cut, and a stopped window is replaced by the
	// one-second window.
	Window, MinWindow int64
	// part is the runtime's part size, the most a line of tailLines counts holds.
	Part int64
}

var DefaultQuota = Quota{
	Second: 1 << 20, Stream: 16 << 20, Gets: 16, Query: 64 << 20,
	Window: byteTarget, MinWindow: 64 << 10, Part: 16 << 10,
}

// pool is the bytes a query's streams share once their first windows have ended.
// reading is the streams past their first window still filling, and holding those
// among them with a reservation out. It goes below zero when a window reads past
// what it reserved.
type Pool struct {
	mu               sync.Mutex
	left             int64
	reading, holding int
}

func (p *Pool) add(n int64) {
	p.mu.Lock()
	p.left += n
	p.mu.Unlock()
}

// give returns n bytes a window did not use and moves the counts of streams reading
// and reservations out by reading and holding, in one step: a stream's share is
// never seen with one changed and not the other.
func (p *Pool) give(n int64, reading, holding int) {
	p.mu.Lock()
	p.left += n
	p.reading += reading
	p.holding += holding
	p.mu.Unlock()
}

func (p *Pool) Load() int64 {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.left
}

// target is what a window reading from the pool is sized to hold: a quarter of the
// stream's share of what is left, at most most.
func (p *Pool) target(most int64) int64 {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.quarter(most)
}

// reserve takes a window's reservation and answers its target, with four times it
// taken. Both under one lock, so streams reserving at once take no more than the
// pool holds.
func (p *Pool) reserve(most int64) int64 {
	p.mu.Lock()
	defer p.mu.Unlock()
	target := p.quarter(most)
	p.left -= 4 * target
	p.holding++
	return target
}

// quarter is a quarter of one stream's share of what is left, at most most. The
// share divides what is left among the streams reading with no reservation out,
// so each of them finds the same share. The caller holds mu.
func (p *Pool) quarter(most int64) int64 {
	return max(min(most, p.left/4/int64(max(p.reading-p.holding, 1))), 0)
}

// limits is one stream's account of one query. Its read alone touches it while the
// read runs, and its fill once the read has ended. A nil limits is no limit.
type Limits struct {
	caps Quota
	Pool *Pool
	// allowance is the stream's own bytes, spent until its first window ends; first
	// is whether it has not ended yet.
	allowance int64
	first     bool
	// held is what the window being read reserved and has not used, and holding
	// whether it reserved it from the pool.
	held    int64
	holding bool
	// spent and gets are what the stream has downloaded and sent.
	Spent int64
	Gets  int
	// WholeSeconds is a follow's fill, which reads every second whole: no second
	// stops it, and it is cut only at a second's end.
	WholeSeconds bool
}

// QueryLimits is the limits of each of a query's n streams: each allowed the smaller
// of c.stream and c.query ÷ n, and the pool seeded with what the allowances leave.
func QueryLimits(c Quota, n int) []*Limits {
	allowance := min(c.Stream, c.Query/int64(n))
	p := &Pool{left: c.Query - allowance*int64(n)}
	out := make([]*Limits, n)
	for i := range out {
		out[i] = &Limits{caps: c, Pool: p, allowance: allowance, first: true}
	}
	return out
}

// FollowLimits is the limits of a follow's gap fill, one of n streams: a forward
// read under the smaller of c.stream and c.query ÷ n, so a round of fills reads no
// more than one query.
func FollowLimits(c Quota, n int) *Limits {
	return &Limits{caps: c, Pool: &Pool{}, allowance: min(c.Stream, c.Query/int64(max(n, 1))), first: true, WholeSeconds: true}
}

// charge counts n bytes downloaded: from the window's reservation, then the
// allowance while the first window has not ended, then the pool.
func (l *Limits) charge(n int64) {
	if l == nil {
		return
	}
	l.Spent += n
	take := min(n, l.held)
	l.held -= take
	n -= take
	if l.first {
		take = min(n, l.allowance)
		l.allowance -= take
		n -= take
	}
	l.Pool.add(-n)
}

// get counts a GET.
func (l *Limits) get() {
	if l != nil {
		l.Gets++
	}
}

// target is the bytes the stream's next window of time is sized to hold: a quarter
// of what is left of the allowance for its first, else the pool's target under most.
func (l *Limits) target() int64 {
	switch {
	case l == nil:
		return byteTarget
	case l.first:
		return l.allowance / 4
	}
	return l.Pool.target(l.most())
}

// take is the target of the stream's next window of time, with four times it
// reserved, the most the window reads before it is stopped as too busy.
func (l *Limits) take() int64 {
	var target int64
	switch {
	case l == nil:
		return byteTarget
	case l.first:
		target = l.allowance / 4
		l.allowance -= 4 * target
	default:
		target = l.Pool.reserve(l.most())
		l.holding = true
	}
	l.held = 4 * target
	return target
}

// most is the most a later window is sized to hold: c.window, or what is left of the
// stream's bytes when that is less.
func (l *Limits) most() int64 {
	return min(l.caps.Window, l.caps.Stream-l.Spent)
}

// windowEnded returns what the window did not use to the pool, with what is left of
// the allowance once the first window has ended, from when the stream reads from
// the pool.
func (l *Limits) windowEnded() { l.settle(false) }

// ended returns what the stream holds and leaves the pool: its fill reads no more.
func (l *Limits) Ended() { l.settle(true) }

// settle is windowEnded, and ended when left, with the pool's counts moved in the
// same step as its bytes.
func (l *Limits) settle(left bool) {
	if l == nil {
		return
	}
	back, reading, holding := l.held, 0, 0
	if l.first {
		back += l.allowance
		reading++
	}
	if left {
		reading--
	}
	if l.holding {
		holding--
	}
	l.Pool.give(back, reading, holding)
	l.held, l.allowance, l.first, l.holding = 0, 0, false, false
}

// usedUp is whether the stream has spent its bytes or sent its GETs, after which its
// fill reads no further gap.
func (l *Limits) UsedUp() bool {
	return l != nil && (l.Spent >= l.caps.Stream || l.Gets >= l.caps.Gets)
}

// reached is whether the stream is cut at the end of a window: it is used up, or its
// next window would be sized under the least.
func (l *Limits) reached() bool {
	return l != nil && (l.UsedUp() || l.target() < l.caps.MinWindow)
}

// forwardSpent is whether a forward read is cut: it has spent the stream's bytes,
// or both its allowance and the pool are empty.
func (l *Limits) forwardSpent() bool {
	return l != nil && (l.Spent >= l.caps.Stream || l.allowance <= 0 && l.Pool.Load() <= 0)
}

// tailLines is the lines the tailLines window asks for to hold need: no more than
// the allowance holds at a part a line, and at least minBatch.
func (l *Limits) tailLines(need int64) int64 {
	if l != nil {
		need = min(need, l.allowance/l.caps.Part)
	}
	return max(need, minBatch)
}

// second is the most one second of the log may hold before it stops the read, zero
// for a read no second stops: one under no limits, or a follow's fill.
func (l *Limits) second() int64 {
	if l == nil || l.WholeSeconds {
		return 0
	}
	return l.caps.Second
}
