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

package logs

import (
	"slices"
	"time"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/logs/logline"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/logs/logstore"
)

// marked is whether g is a hole a line reports: lost and between two spans. The
// range before the oldest span may be a quiet start as well as a rotated one, and
// the range past the newest is an instance's end or a tailer's. A lost gap past the
// tailer's start, as one that resumed after a rotation leaves, is a hole once
// committed, how far the tailer has written, reaches past it: until then the tailer
// may still write lines into it.
func marked(g logstore.Gap, committed *logstore.Pos) bool {
	if !g.Lost || g.Tail != nil || g.After == nil || g.Before == nil {
		return false
	}
	return !g.Tailed || committed != nil && committed.Compare(*g.Before) >= 0
}

// holesBetween is every marked gap of gaps past a and up to b, a the older, joined;
// nil with none. committed is how far the stream's tailer has written, nil with none.
func holesBetween(gaps []logstore.Gap, committed *logstore.Pos, a, b logstore.Pos) *logline.Gap {
	var out *logline.Gap
	for _, g := range gaps {
		if marked(g, committed) && g.Before.Compare(a) > 0 && g.Before.Compare(b) <= 0 {
			out = joined(out, gapOf(g))
		}
	}
	return out
}

// gapOf is the marked gap g as a record carries it.
func gapOf(g logstore.Gap) *logline.Gap {
	return &logline.Gap{After: time.Unix(0, g.After.TS).UTC(), Before: time.Unix(0, g.Before.TS).UTC()}
}

// joined is g and h as one hole, the older after and the newer before; either nil
// for none.
func joined(g, h *logline.Gap) *logline.Gap {
	switch {
	case g == nil:
		return h
	case h == nil:
		return g
	}
	return &logline.Gap{After: minTime(g.After, h.After), Before: maxTime(g.Before, h.Before)}
}

func minTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}

func maxTime(a, b time.Time) time.Time {
	if a.After(b) {
		return a
	}
	return b
}

// looker looks back from a line the view passes for the holes before it, through
// the older lines of its stream that the view does not pass and the gaps between, to
// the next line it does. held and keep are the view's test of a line, as the walk's,
// bytes what one look may read, and committed how far the stream's tailer has
// written, nil with none.
type looker struct {
	r         logstore.Reader
	row       int64
	cov       logstore.Coverage
	committed *logstore.Pos
	held      func(logstore.Pos) bool
	keep      func(ts int64, message string) bool
	bytes     int64
}

// back is every marked hole between from and the line before it that the view
// passes, joined; nil with none. It reads nothing unless the first gap behind from's
// span is marked. It stops at a live gap, which eviction may open under it, or with
// its bytes spent, and answers what it found: every hole it met lies behind lines
// that all failed, so each is from's.
func (l looker) back(from logstore.Pos) (*logline.Gap, error) {
	segs := segmentsFrom(l.cov, from, logline.Older)
	if i := slices.IndexFunc(segs, func(s segment) bool { return s.gap != nil }); i < 0 || !marked(*segs[i].gap, l.committed) {
		return nil, nil
	}
	budget := newBudget(l.bytes)
	var found *logline.Gap
	at := from
	for _, seg := range segs {
		if seg.gap != nil {
			switch {
			case marked(*seg.gap, l.committed):
				found = joined(found, gapOf(*seg.gap))
			case seg.gap.Live():
				return found, nil
			}
			continue
		}
		passed, spent := false, false
		err := l.r.Lines(l.row, at, seg.span.From, func(line logstore.Line) bool {
			if !budget.spend(len(line.Message)) {
				spent = true
				return false
			}
			passed = l.held(line.Pos) && l.keep(line.TS, line.Message)
			return !passed
		})
		if err != nil || passed || spent {
			return found, err
		}
		at = seg.span.From
	}
	return found, nil
}
