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
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/logs/logline"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/logs/logstore"
)

// A gap is marked only when it is lost and lies between two spans: never the range
// before the oldest span, past the newest, a live one, or a tailer's the tailer has not
// written past. One past the tailer's start, as a tailer resumed after a rotation
// leaves, is marked once the tailer has committed past it.
func TestOnlyALostGapBetweenSpansIsMarked(t *testing.T) {
	t.Parallel()
	tail, resumed := sec(12), sec(1)
	before, past := sec(3), sec(6)
	for name, c := range map[string]struct {
		chunks    []logstore.Chunk
		backends  []string
		tail      *logstore.Pos
		committed *logstore.Pos
		want      []bool
	}{
		"behind a resumed tailer": {
			chunks:   []logstore.Chunk{{From: sec(0), To: sec(1), Backend: "kubelet"}, {From: sec(5), To: sec(6), Backend: "kubelet", AtBeginning: true}},
			backends: kubeletOnly, tail: &resumed, committed: &past, want: []bool{false, true, false},
		},
		"ahead of a resumed tailer": {
			chunks:   []logstore.Chunk{{From: sec(0), To: sec(1), Backend: "kubelet"}, {From: sec(5), To: sec(6), Backend: "kubelet", AtBeginning: true}},
			backends: kubeletOnly, tail: &resumed, committed: &before, want: []bool{false, false, false},
		},
		"below an at-beginning chunk": {
			chunks:   []logstore.Chunk{{From: sec(0), To: sec(1), Backend: "kubelet"}, {From: sec(5), To: sec(6), Backend: "kubelet", AtBeginning: true}},
			backends: kubeletOnly, want: []bool{false, true, false},
		},
		"live": {
			chunks:   []logstore.Chunk{{From: sec(0), To: sec(1), Backend: "kubelet"}, {From: sec(5), To: sec(6), Backend: "kubelet"}},
			backends: kubeletOnly, want: []bool{false, false, false},
		},
		"past the end": {
			chunks:   []logstore.Chunk{{From: sec(0), To: sec(1), Backend: "kubelet", AtBeginning: true, AtEnd: true}},
			backends: kubeletOnly, want: []bool{false, false},
		},
		"no backend": {
			chunks: []logstore.Chunk{{From: sec(0), To: sec(1)}, {From: sec(5), To: sec(6)}},
			want:   []bool{false, true, false},
		},
		"cut by a tailer": {
			chunks: []logstore.Chunk{{From: sec(0), To: sec(1)}, {From: sec(20), To: sec(21)}},
			tail:   &tail, committed: &tail, want: []bool{false, false, false, false},
		},
	} {
		var got []bool
		for _, g := range logstore.CoverageOf(c.chunks, c.backends, c.tail).Gaps {
			got = append(got, marked(g, c.committed))
		}
		assert.Equal(t, c.want, got, name)
	}
}

// The holes between two lines are every marked gap past the older and up to the
// newer, joined: the older's after and the newer's before.
func TestHolesBetweenJoinsTheMarkedGaps(t *testing.T) {
	t.Parallel()
	cov := logstore.CoverageOf([]logstore.Chunk{
		{From: sec(0), To: sec(1)}, {From: sec(5), To: sec(6)}, {From: sec(9), To: sec(10)},
	}, nil, nil)

	assert.Nil(t, holesBetween(cov.Gaps, nil, sec(5), sec(6)), "inside a span")
	assert.Equal(t, &logline.Gap{After: at(time.Second), Before: at(5 * time.Second)}, holesBetween(cov.Gaps, nil, sec(1), sec(5)))
	assert.Equal(t, &logline.Gap{After: at(time.Second), Before: at(9 * time.Second)}, holesBetween(cov.Gaps, nil, sec(0), sec(10)))
	assert.Nil(t, holesBetween(cov.Gaps, nil, sec(10), logstore.Newest), "past the newest span")
}

// lookOver files fills as web-1's stream and looks back from the line at from under
// grep, with bytes to spend, and answers what it found and how many lines it read.
func lookOver(t *testing.T, grep string, bytes int64, from logstore.Pos, fills ...logstore.Fill) (*logline.Gap, int) {
	t.Helper()
	return lookOverCov(t, grep, bytes, from, func(*logstore.Coverage) {}, fills...)
}

// lookOverCov is lookOver over the coverage edit leaves.
func lookOverCov(t *testing.T, grep string, bytes int64, from logstore.Pos, edit func(*logstore.Coverage), fills ...logstore.Fill) (*logline.Gap, int) {
	t.Helper()
	svc, f := serviceOver(t, webMirror(t), newLogServer())
	id := seed(t, svc, f, rowOf("web-1", "app"), fills...)
	re, err := logline.CompileGrep(grep)
	if err != nil {
		t.Fatal(err)
	}
	var found *logline.Gap
	read := 0
	err = fileOf(t, svc, f).Read(t.Context(), func(r logstore.Reader) error {
		chunks, err := r.Chunks(id, "")
		if err != nil {
			return err
		}
		held := heldBy(chunks)
		cov := logstore.CoverageOf(chunks, kubeletOnly, nil)
		edit(&cov)
		l := looker{r: r, row: id, cov: cov, bytes: bytes,
			keep: func(_ int64, message string) bool { return logline.Matches(re, message) },
			held: func(p logstore.Pos) bool { read++; return held(p) }}
		found, err = l.back(from)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return found, read
}

// A look back passes every hole and failing line until a line the view passes: both
// holes before x10 under "x" are its, and only the nearer before y9 under "y".
func TestALookBackPassesEveryHoleToTheLineBefore(t *testing.T) {
	t.Parallel()
	found, _ := lookOver(t, "x", 1<<20, sec(10), twoHoles...)
	assert.Equal(t, &logline.Gap{After: at(time.Second), Before: at(9 * time.Second)}, found)

	found, _ = lookOver(t, "y", 1<<20, sec(9), twoHoles...)
	assert.Equal(t, &logline.Gap{After: at(6 * time.Second), Before: at(9 * time.Second)}, found)
}

// A look reads nothing when no marked hole lies behind the line's span.
func TestALookIsSkippedWithNoHoleBehind(t *testing.T) {
	t.Parallel()
	found, read := lookOver(t, "x", 1<<20, sec(1), twoHoles...)
	assert.Nil(t, found)
	assert.Zero(t, read)
}

// A look stops at a live gap, as eviction may open under its path, and when its bytes
// run out, with what it found.
func TestALookStopsAtALiveGapOrItsBudget(t *testing.T) {
	t.Parallel()
	evicted := func(cov *logstore.Coverage) { cov.Gaps[1].Lost = false }
	found, _ := lookOverCov(t, "x", 1<<20, sec(10), evicted, twoHoles...)
	assert.Equal(t, &logline.Gap{After: at(6 * time.Second), Before: at(9 * time.Second)}, found, "live")

	found, _ = lookOver(t, "x", 3, sec(10), twoHoles...)
	assert.Equal(t, &logline.Gap{After: at(6 * time.Second), Before: at(9 * time.Second)}, found, "budget")
}
