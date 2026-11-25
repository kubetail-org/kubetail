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
	"bytes"
	"cmp"
	"compress/flate"
	"context"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"maps"
	"math"
	"math/rand"
	"net/http"
	"net/url"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/amorey/gochan/spsc"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/testutil"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/cluster"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/logs/kubelet"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/logs/logline"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/logs/logstest"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/logs/logstore"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/logs/refusal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ===== The fetch API and the page engine =====

// A deployment's last lines come back in time order, its Prev going on from the
// oldest.
func TestFetchAnswersADeploymentsLastLines(t *testing.T) {
	t.Parallel()
	s := newLogServer()
	for i := range 15 {
		pod := "web-" + strconv.Itoa(1+i%2)
		s.Add("prod/"+pod+"/app", logstest.Stamped(time.Duration(i)*time.Second, strconv.Itoa(i)))
	}
	svc, _ := serviceOver(t, webMirror(t), s)

	page, err := svc.Fetch(t.Context(), "7", FetchRequest{Scope: webScope, PageFrom: PageFromLast, PageSize: 5})

	require.NoError(t, err)
	assert.Equal(t, []string{"10", "11", "12", "13", "14"}, messages(page.Records))
	assert.Equal(t, *lineCursorFor("web-1", "app", 10*time.Second, 0), atOf(t, page.Page.Prev))
	assert.Equal(t, "web-1", page.Records[0].Stream.Pod)
	assert.Equal(t, "eu-1a", page.Records[0].Place.Zone)
}

// A first page from a moment reads past it, grep keeps the matching lines, and its
// Next goes on from the last line the stream passed on the page.
func TestFetchReadsForwardAfterACursor(t *testing.T) {
	t.Parallel()
	s := newLogServer()
	s.Add("prod/web-1/app", logstest.Stamped(0, "error a"), logstest.Stamped(time.Second, "ok"), logstest.Stamped(2*time.Second, "error b"), logstest.Stamped(3*time.Second, "error c"))
	svc, _ := serviceOver(t, webMirror(t), s)
	view := webScope
	view.Grep = "^error"

	page, err := svc.Fetch(t.Context(), "7", FetchRequest{Scope: view, Window: Window{Since: moment(0 + time.Nanosecond)}, PageFrom: PageFromFirst, PageSize: 1})

	require.NoError(t, err)
	assert.Equal(t, []string{"error b"}, messages(page.Records))
	assert.Equal(t, entry("web-1", "app", 2*time.Second), cursorIn(t, page.Page.Next).seen[name("web-1", "app")])
}

// A last page before a moment reaches the line before it however much log follows,
// and a page that fills reads no further: a page of one line reads one window.
func TestFetchPagesBackBeforeACursor(t *testing.T) {
	t.Parallel()
	s := newLogServer()
	s.Add("prod/web-1/app", logstest.LineEvery(1000)...)
	svc, _ := serviceOver(t, webMirror(t), s)

	page, err := svc.Fetch(t.Context(), "7", FetchRequest{Scope: webScope, Window: until(100 * time.Second), PageFrom: PageFromLast, PageSize: 1})

	require.NoError(t, err)
	assert.Equal(t, []string{"99"}, messages(page.Records))
	assert.Equal(t, *lineCursorFor("web-1", "app", 99*time.Second, 0), atOf(t, page.Page.Prev))

	before := len(s.Requests())
	page, err = svc.Fetch(t.Context(), "7", FetchRequest{Scope: webScope, PageFrom: PageFromLast, PageSize: 1})

	require.NoError(t, err)
	assert.Equal(t, []string{"999"}, messages(page.Records))
	var windows []string
	for _, q := range s.Requests()[before:] {
		if q.Has("tailLines") {
			windows = append(windows, q.Get("tailLines"))
		}
	}
	assert.Equal(t, []string{"10"}, windows)
}

// Two containers writing at one moment are paged through one line at a time, each
// page going on past the line it ended at.
func TestFetchPagesThroughAMoment(t *testing.T) {
	t.Parallel()
	s := newLogServer()
	s.Add("prod/web-1/app", logstest.Stamped(0, "a"), logstest.Stamped(0, "a'"), logstest.Stamped(time.Second, "c"))
	s.Add("prod/web-2/app", logstest.Stamped(0, "b"))
	svc, _ := serviceOver(t, webMirror(t), s)

	got := pages(t, svc, webScope, PageFromFirst, 1)

	assert.Equal(t, []string{"a", "a'", "b", "c"}, recordsOf(got, PageFromFirst))
}

// Pages join: walked from either end through random logs with tied and sub-second
// moments and long lines, under small limits and a grep, the pages hold every line
// in range once, in order, each within its limit, whether the walk starts at an end
// or at a line's cursor. Some streams lost moments to a rotation the file never saw,
// and each hole between two lines the file holds rides on exactly one record, the
// first past it the grep passes, with the hole's own two times. A second of the
// kubelet's log louder than the limit stops its stream: its lines may be left out,
// all of them backward and the rest of the second past what the read took forward,
// and a walk from the start names the stream in skipped. Every page resolves the
// view through SQL over a real store, so under the race detector the full
// sweep takes minutes; -short walks a prefix of the seeds, and the stress run walks
// them all.
func TestPagesJoinWithoutLossOrRepeat(t *testing.T) {
	t.Parallel()
	seeds := 150
	if testing.Short() {
		seeds = 25
	}
	for seed := range seeds {
		t.Run(strconv.Itoa(seed), func(t *testing.T) {
			t.Parallel()
			rng := rand.New(rand.NewSource(int64(seed)))
			s, objects, all := randomLogs(rng)
			kept, runs := rotateAway(rng, s, all)
			// Windows written in batches of a few lines or bytes, so batches join too.
			ret := testRetention
			ret.WriteBatch, ret.WriteBatchBytes = 1+rng.Intn(4), int64(1+rng.Intn(64))
			svc, f := serviceIn(t, newMirror(t, objects...), s, t.TempDir(), ret)
			for key, held := range runs {
				var fills []logstore.Fill
				for _, run := range held {
					var lines []logstore.Line
					for _, rec := range run {
						lines = append(lines, lineOf(rec))
					}
					fills = append(fills, logstore.Fill{Lines: lines})
				}
				seedStamped(t, svc, f, rowOf(key.Pod, key.Container), true, fills...)
			}
			// Every page resolves the view again; the mirror holds still, and SQLite
			// running the same statements is most of what the walks cost.
			f.answers = map[string]cluster.ClusterCachedDataQueryResult{}
			svc.caps = randomCaps(rng)
			loud := loudLines(s, svc.caps.Second)
			f.chunk(1 + rng.Intn(64))
			view := Scope{Sources: []ScopeSource{{Namespace: "prod", Kind: SourceDeployment, Name: "web", AllContainers: true}}}
			view.Grep = []string{"", "odd", "#.*7"}[rng.Intn(3)]
			grep := regexp.MustCompile(view.Grep)
			matches := func(rec logline.Record) bool { return grep.MatchString(rec.Message) }
			// expected is the lines w holds: those of its range the grep passes and every
			// line of its padding, each carrying the holes between it and the line of its
			// stream before it that passes, since the grep searches the range alone.
			expected := func(w Window) []logline.Record {
				inRange := func(rec logline.Record) bool {
					return (w.Since == nil || !rec.Timestamp.Before(*w.Since)) && (w.Until == nil || !rec.Timestamp.After(*w.Until))
				}
				return inWindow(wanted(all, kept, func(rec logline.Record) bool { return !inRange(rec) || matches(rec) }), w)
			}
			want := expected(Window{})
			limit := 1 + rng.Intn(25)

			joins := func(want, got []logline.Record, name string) {
				t.Helper()
				assert.True(t, ordered(got), "%s: in order, none repeated", name)
				assert.Equal(t, marks(without(want, loud)), marks(without(got, loud)), name)
			}
			sizes := func() (int, int) { return 1 + rng.Intn(25), []int{0, 0, 64, 512}[rng.Intn(4)] }
			got, skipped, firsts := walkPages(t, svc, FetchRequest{Scope: view, PageFrom: PageFromFirst, PageSize: limit}, sizes)
			joins(want, got, "first")
			for _, key := range loud {
				assert.Contains(t, skipped, key.Pod+"/"+key.Container, "a loud second is named")
			}
			got, _, lasts := walkPages(t, svc, FetchRequest{Scope: view, PageFrom: PageFromLast, PageSize: limit}, sizes)
			joins(want, got, "last")
			if len(want) == 0 {
				return
			}

			// Turning back from a page reads what the pages before it read.
			if k := rng.Intn(len(firsts)); firsts[k].Page.Prev != nil {
				var before []logline.Record
				for _, page := range firsts[:k] {
					before = append(before, page.Records...)
				}
				back, _, _ := walkPages(t, svc, FetchRequest{Cursor: firsts[k].Page.Prev, PageSize: limit}, sizes)
				joins(before, back, "back from a next")
			}
			if k := rng.Intn(len(lasts)); k > 0 && lasts[k].Page.Next != nil {
				var after []logline.Record
				for _, page := range lasts[:k] {
					after = append(slices.Clone(page.Records), after...)
				}
				on, _, _ := walkPages(t, svc, FetchRequest{Cursor: lasts[k].Page.Next, PageSize: limit}, sizes)
				joins(after, on, "on from a prev")
			}

			m := want[rng.Intn(len(want))].Timestamp
			joins(expected(Window{Since: &m}), walk(t, svc, FetchRequest{Scope: view, Window: Window{Since: &m}, PageFrom: PageFromFirst, PageSize: limit}), "since")
			upTo := m.Add(-time.Nanosecond)
			joins(expected(Window{Until: &upTo}), walk(t, svc, FetchRequest{Scope: view, Window: Window{Until: &upTo}, PageFrom: PageFromLast, PageSize: limit}), "until")

			// A window padded each side, by span or, with no loud second to leave lines
			// out of a count, by lines, holds its range and its padding, read either way.
			from, to := want[rng.Intn(len(want))].Timestamp, want[rng.Intn(len(want))].Timestamp
			if to.Before(from) {
				from, to = to, from
			}
			w := Window{Since: &from, Until: &to}
			pad := func() Padding {
				switch n := rng.Intn(3); {
				case n == 0 && len(loud) == 0:
					return Padding{Lines: 1 + rng.Intn(10)}
				case n == 1:
					return Padding{Span: time.Duration(rng.Intn(3000)) * time.Millisecond}
				}
				return Padding{}
			}
			w.Before, w.After = pad(), pad()
			for _, from := range []PageFrom{PageFromFirst, PageFromLast} {
				got, _, _ := walkPages(t, svc, FetchRequest{Scope: view, Window: w, PageFrom: from, PageSize: limit}, sizes)
				joins(expected(w), got, fmt.Sprintf("%s of %+v", from, w))
			}
		})
	}
}

// randomLogs is a deployment web of one to three pods, each with an app and an
// istio-proxy container, whose logs hold up to eighty lines each at moments that
// often tie within and across containers, some inside a second, some opening
// with a burst, some quiet for a day part way, every other line saying odd, and
// every line in the order a view of web reads them.
func randomLogs(rng *rand.Rand) (*logstest.Server, []map[string]any, []logline.Record) {
	s := newLogServer()
	objects := []map[string]any{
		workload("apps/v1", "Deployment", "web", "dep-web", nil),
		workload("apps/v1", "ReplicaSet", "web-7d9f", "rs-web", controller("apps/v1", "Deployment", "web", "dep-web")),
		node("node-a", "eu", "eu-1a", "linux", "amd64"),
	}
	place := logline.Place{Node: "node-a", Region: "eu", Zone: "eu-1a", OS: "linux", Arch: "amd64"}
	var all []logline.Record
	for i := 1; i <= 1+rng.Intn(3); i++ {
		name := "web-" + strconv.Itoa(i)
		key := "prod/" + name
		objects = append(objects, pod(name, "uid-"+name, "node-a", webReplicaSet(), container{name: "app"}, container{name: "istio-proxy"}))
		for _, container := range []string{"app", "istio-proxy"} {
			stream := logline.StreamKey{Namespace: "prod", Pod: name, Container: container}
			// Some logs hold long lines, which make some seconds loud.
			long := []int{0, 0, 1500}[rng.Intn(3)]
			moments := make([]time.Duration, rng.Intn(81))
			for i := range moments {
				moments[i] = time.Duration(rng.Intn(12))*time.Second + []time.Duration{0, 0, 0, 250 * time.Millisecond, 500 * time.Millisecond}[rng.Intn(5)]
			}
			slices.Sort(moments)
			// A log that opens with a burst, more lines at its first moment than a
			// window holds.
			if len(moments) > 0 && rng.Intn(2) == 0 {
				for i := range min(len(moments), 1+rng.Intn(30)) {
					moments[i] = moments[0]
				}
			}
			// A log quiet for a day between its older lines and its newer, which a
			// read back across widens past.
			if len(moments) > 1 && rng.Intn(3) == 0 {
				for i := range 1 + rng.Intn(len(moments)-1) {
					moments[i] -= 24 * time.Hour
				}
			}
			ordinal := 0
			for i, d := range moments {
				if i > 0 && moments[i-1] == d {
					ordinal++
				} else {
					ordinal = 0
				}
				parity := "even"
				if i%2 == 1 {
					parity = "odd"
				}
				msg := name + "/" + container + "#" + strconv.Itoa(i) + " " + parity
				if long > 0 && rng.Intn(3) == 0 {
					msg += " " + strings.Repeat("x", rng.Intn(long))
				}
				s.Add(key+"/"+container, logstest.Stamped(d, msg))
				all = append(all, logline.Record{Timestamp: at(d), Message: msg, Stream: stream, Place: place, ContainerID: containerID("uid-"+name, container, 0), Ordinal: ordinal})
			}
		}
	}
	slices.SortStableFunc(all, logline.CompareLines)
	return s, objects, all
}

// rotateAway drops, from about half of all's streams with three moments or more, the
// lines at random moments, the newest never, as rotations the file never saw. Each
// run of moments left but the newest is the file's alone, and the kubelet serves only
// the newest run. It answers the lines left, in all's order, and each such stream's
// runs the file holds.
func rotateAway(rng *rand.Rand, s *logstest.Server, all []logline.Record) ([]logline.Record, map[logline.StreamKey][][]logline.Record) {
	byStream := map[logline.StreamKey][]logline.Record{}
	var keys []logline.StreamKey
	for _, rec := range all {
		if _, ok := byStream[rec.Stream]; !ok {
			keys = append(keys, rec.Stream)
		}
		byStream[rec.Stream] = append(byStream[rec.Stream], rec)
	}
	dropped := map[logline.StreamKey]map[time.Time]bool{}
	runs := map[logline.StreamKey][][]logline.Record{}
	for _, key := range keys {
		lines := byStream[key]
		var moments []time.Time
		for _, rec := range lines {
			if len(moments) == 0 || !moments[len(moments)-1].Equal(rec.Timestamp) {
				moments = append(moments, rec.Timestamp)
			}
		}
		if len(moments) < 3 || rng.Intn(2) == 0 {
			continue
		}
		dropped[key] = map[time.Time]bool{}
		for _, m := range moments[:len(moments)-1] {
			if rng.Intn(4) == 0 {
				dropped[key][m] = true
			}
		}
		var cur []logline.Record
		var held [][]logline.Record
		for _, rec := range lines {
			if dropped[key][rec.Timestamp] {
				if cur != nil {
					held, cur = append(held, cur), nil
				}
				continue
			}
			cur = append(cur, rec)
		}
		// The newest run is the kubelet's.
		var served []string
		for _, rec := range cur {
			served = append(served, logstest.Stamped(rec.Timestamp.Sub(at(0)), rec.Message))
		}
		s.Mu.Lock()
		s.Logs[key.Namespace+"/"+key.Pod+"/"+key.Container] = served
		s.Mu.Unlock()
		if len(held) > 0 {
			runs[key] = held
		}
	}
	kept := slices.DeleteFunc(slices.Clone(all), func(rec logline.Record) bool { return dropped[rec.Stream][rec.Timestamp] })
	return kept, runs
}

// loudLines is each line of the kubelet's logs in s whose second carries more than
// limit bytes, by message: a read may leave it out.
func loudLines(s *logstest.Server, limit int64) map[string]logline.StreamKey {
	out := map[string]logline.StreamKey{}
	for key, lines := range s.Logs {
		parts := strings.Split(key, "/")
		stream := logline.StreamKey{Namespace: parts[0], Pod: parts[1], Container: parts[2]}
		bytes := map[time.Time]int64{}
		for _, line := range lines {
			rec, _ := kubelet.ParseLine([]byte(line))
			bytes[rec.Timestamp.Truncate(time.Second)] += int64(len(line) + 1)
		}
		for _, line := range lines {
			if rec, _ := kubelet.ParseLine([]byte(line)); bytes[rec.Timestamp.Truncate(time.Second)] > limit {
				out[rec.Message] = stream
			}
		}
	}
	return out
}

// without is recs less the loud lines, with no hole on a record of a stream that
// holds one: a loud second's unread part is a live gap, which no record looks back
// past.
func without(recs []logline.Record, loud map[string]logline.StreamKey) []logline.Record {
	streams := map[logline.StreamKey]bool{}
	for _, key := range loud {
		streams[key] = true
	}
	out := slices.DeleteFunc(slices.Clone(recs), func(rec logline.Record) bool {
		_, ok := loud[rec.Message]
		return ok
	})
	for i := range out {
		if streams[out[i].Stream] {
			out[i].Gap = nil
		}
	}
	return out
}

// ordered is whether recs are in order with none repeated.
func ordered(recs []logline.Record) bool {
	for i := 1; i < len(recs); i++ {
		if logline.CompareLines(recs[i-1], recs[i]) >= 0 {
			return false
		}
	}
	return true
}

// wanted is the lines of kept that pass, each carrying the holes between it and
// the line of its stream before it that passes: wherever all, the lines before the
// rotations, held lines between two neighbours in kept.
func wanted(all, kept []logline.Record, pass func(logline.Record) bool) []logline.Record {
	left := map[string]bool{}
	for _, rec := range kept {
		left[rec.Message] = true
	}
	last := map[logline.StreamKey]*logline.Record{}
	hole := map[logline.StreamKey]bool{}
	pending := map[logline.StreamKey]*logline.Gap{}
	gaps := map[string]*logline.Gap{}
	for _, rec := range all {
		if !left[rec.Message] {
			hole[rec.Stream] = last[rec.Stream] != nil
			continue
		}
		if hole[rec.Stream] {
			pending[rec.Stream] = joined(pending[rec.Stream], &logline.Gap{After: last[rec.Stream].Timestamp, Before: rec.Timestamp})
			hole[rec.Stream] = false
		}
		last[rec.Stream] = &rec
		if pass(rec) {
			gaps[rec.Message], pending[rec.Stream] = pending[rec.Stream], nil
		}
	}
	var out []logline.Record
	for _, rec := range kept {
		if pass(rec) {
			rec.Gap = gaps[rec.Message]
			out = append(out, rec)
		}
	}
	return out
}

// walk reads req's first page and every page on from it the way it reads, and is
// every record, oldest first, each page within its size.
func walk(t *testing.T, svc *Service, req FetchRequest) []logline.Record {
	t.Helper()
	out, _, _ := walkPages(t, svc, req, nil)
	return out
}

// walkPages is walk, each stream a page skipped, and every page in the order read.
// sizes names each continuation's page size and byte cap; nil repeats req's.
func walkPages(t *testing.T, svc *Service, req FetchRequest, sizes func() (int, int)) ([]logline.Record, []string, []FetchResponse) {
	t.Helper()
	newer := req.PageFrom == PageFromFirst || req.Cursor != nil && cursorIn(t, req.Cursor).newer
	out := []logline.Record{}
	var skipped []string
	var pages []FetchResponse
	for range 1000 {
		page := fetch(t, svc, req)
		pages = append(pages, page)
		for _, line := range skippedOf(page) {
			skipped = append(skipped, strings.SplitN(line, ":", 2)[0])
		}
		require.LessOrEqual(t, len(page.Records), page.Request.PageSize)
		c := page.Page.Prev
		if newer {
			out, c = append(out, page.Records...), page.Page.Next
		} else {
			out = append(slices.Clone(page.Records), out...)
		}
		if c == nil {
			return out, skipped, pages
		}
		req = FetchRequest{Cursor: c, PageSize: req.PageSize, MaxPageBytes: req.MaxPageBytes}
		if sizes != nil {
			req.PageSize, req.MaxPageBytes = sizes()
		}
	}
	t.Fatal("the pages never end")
	return nil, nil, nil
}

// inWindow is the lines of want, oldest first, that w holds: those of its range,
// and of each side's padding, its last or first Lines, or those within its Span.
// A nil bound holds every line past it.
func inWindow(want []logline.Record, w Window) []logline.Record {
	var before, in, after []logline.Record
	for _, rec := range want {
		switch {
		case w.Since != nil && rec.Timestamp.Before(*w.Since):
			if w.Before.Span > 0 && !rec.Timestamp.Before(w.Since.Add(-w.Before.Span)) || w.Before.Lines > 0 {
				before = append(before, rec)
			}
		case w.Until != nil && rec.Timestamp.After(*w.Until):
			if w.After.Span > 0 && !rec.Timestamp.After(w.Until.Add(w.After.Span)) || w.After.Lines > 0 {
				after = append(after, rec)
			}
		default:
			in = append(in, rec)
		}
	}
	if n := w.Before.Lines; n > 0 && len(before) > n {
		before = before[len(before)-n:]
	}
	if n := w.After.Lines; n > 0 && len(after) > n {
		after = after[:n]
	}
	return slices.Concat(before, in, after)
}

// A first page from a moment starts at its first line, the moment's own lines in,
// and a last page to one ends at its last; a bound is the window's edge, so the
// page sets no edge past it.
func TestAPageReadsFromItsNearBound(t *testing.T) {
	t.Parallel()
	s := newLogServer()
	s.Add("prod/web-1/app", logstest.LineEvery(6)...)
	svc, _ := serviceOver(t, webMirror(t), s)

	page := fetch(t, svc, FetchRequest{Scope: webScope, Window: Window{Since: moment(2 * time.Second)}, PageFrom: PageFromFirst, PageSize: 10})
	assert.Equal(t, []string{"2", "3", "4", "5"}, messages(page.Records))
	assert.Equal(t, [3]bool{false, false, true}, edgesOf(page.Page))

	page = fetch(t, svc, FetchRequest{Scope: webScope, Window: Window{Until: moment(3 * time.Second)}, PageFrom: PageFromLast, PageSize: 10})
	assert.Equal(t, []string{"0", "1", "2", "3"}, messages(page.Records))
	assert.Equal(t, [3]bool{false, false, false}, edgesOf(page.Page))
}

// A page stops at its far bound, whose moment's lines are in, and says nothing is
// left that way; one the page size cut first still says more.
func TestAPageStopsAtItsFarBound(t *testing.T) {
	t.Parallel()
	s := newLogServer()
	s.Add("prod/web-1/app", logstest.LineEvery(6)...)
	svc, _ := serviceOver(t, webMirror(t), s)
	window := func(since, until time.Duration) Window {
		return Window{Since: moment(since), Until: moment(until)}
	}

	page := fetch(t, svc, FetchRequest{Scope: webScope, Window: window(time.Second, 3*time.Second), PageFrom: PageFromFirst, PageSize: 10})
	assert.Equal(t, []string{"1", "2", "3"}, messages(page.Records))
	assert.Nil(t, page.Page.Next)

	page = fetch(t, svc, FetchRequest{Scope: webScope, Window: window(2*time.Second, 4*time.Second), PageFrom: PageFromLast, PageSize: 10})
	assert.Equal(t, []string{"2", "3", "4"}, messages(page.Records))
	assert.Nil(t, page.Page.Prev)

	page = fetch(t, svc, FetchRequest{Scope: webScope, Window: window(time.Second, 3*time.Second), PageFrom: PageFromFirst, PageSize: 2})
	assert.Equal(t, []string{"1", "2"}, messages(page.Records))
	assert.Equal(t, entry("web-1", "app", 2*time.Second), cursorIn(t, page.Page.Next).seen[name("web-1", "app")])
}

// A fill never asks for a line past the page's far bound: a range no line of which
// passes the grep reads up to the bound and no further, however much log follows,
// either way.
func TestAFillStopsAtTheFarBound(t *testing.T) {
	t.Parallel()
	s := newLogServer()
	s.Add("prod/web-1/app", logstest.LineEvery(3000)...)
	svc, f := serviceOver(t, webMirror(t), s)
	view := webScope
	view.Grep = "^none"

	page, err := svc.Fetch(t.Context(), "7", FetchRequest{Scope: view, Window: Window{Since: moment(100 * time.Second), Until: moment(110 * time.Second)}, PageFrom: PageFromFirst, PageSize: 10})
	require.NoError(t, err)
	assert.Empty(t, page.Records)
	assert.Nil(t, page.Page.Next)
	for _, c := range chunksOf(t, svc, f, "web-1", "app") {
		assert.LessOrEqual(t, c.To.TS, at(110*time.Second).UnixNano())
	}

	page, err = svc.Fetch(t.Context(), "7", FetchRequest{Scope: view, Window: Window{Since: moment(2000 * time.Second), Until: moment(2010 * time.Second)}, PageFrom: PageFromLast, PageSize: 10})
	require.NoError(t, err)
	assert.Empty(t, page.Records)
	assert.Nil(t, page.Page.Prev)
	for _, c := range chunksOf(t, svc, f, "web-1", "app") {
		if c.To.TS > at(110*time.Second).UnixNano() {
			assert.GreaterOrEqual(t, c.From.TS, at(2000*time.Second).UnixNano())
		}
	}
}

// A page over a range the file holds whole asks the backend for nothing.
func TestAPageInsideARangeTheFileHoldsReadsNothing(t *testing.T) {
	t.Parallel()
	s := newLogServer()
	s.Add("prod/web-1/app", logstest.LineEvery(3000)...)
	svc, _ := serviceOver(t, webMirror(t), s)

	_, err := svc.Fetch(t.Context(), "7", FetchRequest{Scope: web1Scope, Window: Window{Since: moment(100 * time.Second), Until: moment(110 * time.Second)}, PageFrom: PageFromFirst, PageSize: 50})
	require.NoError(t, err)
	before := len(s.Requests())

	page, err := svc.Fetch(t.Context(), "7", FetchRequest{Scope: web1Scope, Window: Window{Since: moment(102 * time.Second), Until: moment(105 * time.Second)}, PageFrom: PageFromFirst, PageSize: 50})
	require.NoError(t, err)
	assert.Equal(t, []string{"102", "103", "104", "105"}, messages(page.Records))
	assert.Len(t, s.Requests(), before)
}

// A group of lines sharing one moment, larger than the page, is read across pages
// of a one-nanosecond range through each page's cursor, either way, losing and
// repeating nothing: an entry names a line, where a moment could not.
func TestARangePagesThroughAMomentLargerThanThePage(t *testing.T) {
	t.Parallel()
	s := newLogServer()
	var want []string
	s.Add("prod/web-1/app", logstest.Stamped(4*time.Second, "before"))
	for i := range 9 {
		s.Add("prod/web-1/app", logstest.Stamped(5*time.Second, "a"+strconv.Itoa(i)))
		want = append(want, "a"+strconv.Itoa(i))
	}
	s.Add("prod/web-1/app", logstest.Stamped(6*time.Second, "after"))
	for i := range 4 {
		s.Add("prod/web-2/app", logstest.Stamped(5*time.Second, "b"+strconv.Itoa(i)))
		want = append(want, "b"+strconv.Itoa(i))
	}
	svc, _ := serviceOver(t, webMirror(t), s)
	w := Window{Since: moment(5 * time.Second), Until: moment(5 * time.Second)}

	for _, from := range []PageFrom{PageFromFirst, PageFromLast} {
		page := fetch(t, svc, FetchRequest{Scope: webScope, Window: w, PageFrom: from, PageSize: 4})
		assert.Equal(t, want, recordsOf(pagesOn(t, svc, page, 4), from), from)
	}
}

// web1Scope reads the app container of the pod web-1 in prod.
var web1Scope = Scope{Sources: []ScopeSource{{Namespace: "prod", Kind: SourcePod, Name: "web-1", Containers: []string{"app"}}}}

// fileOf is the log file of f's active cache, for a test to read.
func fileOf(t *testing.T, svc *Service, f *fakeService) *logstore.Store {
	t.Helper()
	file, err := svc.files.OpenOrCreate(int64(f.active.Cache.ID))
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, file.Close()) })
	return file
}

// chunksOf is the unlabelled chunks of pod/container's stream of the first instance
// of a fixture pod, in the file behind svc.
func chunksOf(t *testing.T, svc *Service, f *fakeService, pod, container string) []logstore.Chunk {
	t.Helper()
	file := fileOf(t, svc, f)
	rows, err := file.PodStreams(t.Context(), "prod", pod, "")
	require.NoError(t, err)
	for _, row := range rows {
		if row.Container == container && row.ContainerID == containerID("uid-"+pod, container, 0) {
			chunks, err := file.Chunks(t.Context(), row.ID, "")
			require.NoError(t, err)
			return chunks
		}
	}
	return nil
}

// A page stamps the chunks its walk read a line from, and none past where it cut: a
// chunk the page never reached stays as its fill left it.
func TestAPageStampsTheChunksItRead(t *testing.T) {
	t.Parallel()
	svc, f := serviceIn(t, webMirror(t), newLogServer(), t.TempDir(), testRetention)
	seedUnread(t, svc, f, rowOf("web-1", "app"),
		logstore.Fill{Lines: fileLines(time.Second, 2*time.Second, 3*time.Second), AtBeginning: true},
		logstore.Fill{Lines: fileLines(10*time.Second, 11*time.Second), Prev: &logstore.Pos{TS: at(3 * time.Second).UnixNano()}})

	_, err := svc.Fetch(t.Context(), "7", FetchRequest{Scope: web1Scope, PageFrom: PageFromFirst, PageSize: 2})
	require.NoError(t, err)

	chunks := chunksOf(t, svc, f, "web-1", "app")
	require.Len(t, chunks, 2)
	assert.NotZero(t, chunks[0].ReadAt, "the chunk the page read")
	assert.Zero(t, chunks[1].ReadAt, "the chunk past its cut")
}

// A page's fill lands stamped, so a sweep over budget between the fill's commit and the
// page's walk leaves what the page fetched, and the page is whole.
func TestASweepBetweenAPagesFillAndWalkLeavesThePage(t *testing.T) {
	t.Parallel()
	s := newLogServer()
	s.Add("prod/web-1/app", logstest.Stamped(time.Second, "1"), logstest.Stamped(2*time.Second, "2"))
	ret := testRetention
	ret.Bytes = 0
	svc, f := serviceIn(t, webMirror(t), s, t.TempDir(), ret)
	svc.afterFill = func() { sweepFile(t, svc, f) }

	page, err := svc.Fetch(t.Context(), "7", FetchRequest{Scope: web1Scope, PageFrom: PageFromFirst, PageSize: 10})

	require.NoError(t, err)
	assert.Equal(t, []string{"1", "2"}, messages(page.Records))
}

// A page whose stream row went between its lookup and its write reads that stream as
// having nothing: Write skips its fill rather than failing every other stream's.
func TestAPageWhoseRowWentReadsNothingOfIt(t *testing.T) {
	t.Parallel()
	backend := newFakeBackend()
	release := make(chan struct{})
	backend.set("web-1/app", fakeRead{lines: []logline.Record{{Timestamp: at(time.Second), Message: "1"}}, wait: release})
	// Both reads wait, since a stream writes once its read ends, and the delete
	// below takes the file's one writer.
	backend.set("web-2/app", fakeRead{lines: []logline.Record{{Timestamp: at(2 * time.Second), Message: "2"}}, wait: release})
	dir := t.TempDir()
	svc, _ := serviceIn(t, webMirror(t), newLogServer(), dir, testRetention)
	svc.backends = []namedBackend{{"kubelet", func(kubelet.Getter) kubelet.Backend { return backend }}}
	view := Scope{Sources: []ScopeSource{
		{Namespace: "prod", Kind: SourcePod, Name: "web-1", Containers: []string{"app"}},
		{Namespace: "prod", Kind: SourcePod, Name: "web-2", Containers: []string{"app"}},
	}}
	done := make(chan error, 1)
	var page FetchResponse
	go func() {
		var err error
		page, err = svc.Fetch(t.Context(), "7", FetchRequest{Scope: view, PageFrom: PageFromFirst, PageSize: 10})
		done <- err
	}()
	backend.opened.Await(t, "a stream's read")
	backend.opened.Await(t, "the other's")
	db, err := sql.Open("sqlite", filepath.Join(dir, "11.db"))
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, db.Close()) })
	_, err = db.Exec(`DELETE FROM streams WHERE pod = 'web-1'`)
	require.NoError(t, err)

	close(release)
	require.NoError(t, testutil.Recv(t, done, "the page"))
	assert.Equal(t, []string{"2"}, messages(page.Records))
}

// A page whose stamp the file refuses still answers: a stamp only keeps the chunks out
// of eviction, and a chunk evicted is filled again.
func TestAPageWhoseStampFailsStillAnswers(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	svc, f := serviceIn(t, webMirror(t), newLogServer(), dir, testRetention)
	seedUnread(t, svc, f, rowOf("web-1", "app"), logstore.Fill{Lines: fileLines(time.Second, 2*time.Second), AtBeginning: true})
	db, err := sql.Open("sqlite", filepath.Join(dir, "11.db"))
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, db.Close()) })
	_, err = db.Exec(`CREATE TRIGGER refuse_stamp BEFORE UPDATE OF read_at_ms ON chunks BEGIN SELECT RAISE(ABORT, 'refused'); END`)
	require.NoError(t, err)

	page, err := svc.Fetch(t.Context(), "7", FetchRequest{Scope: web1Scope, PageFrom: PageFromFirst, PageSize: 1})

	require.NoError(t, err)
	assert.Equal(t, []string{"1"}, messages(page.Records))
}

// everyPodGone has the sweeps of svc's log files read every pod as gone.
func everyPodGone(svc *Service) {
	svc.files.SetPodsGone(func(_ context.Context, _ int64, uids []string) ([]string, error) { return uids, nil })
}

// rowsOf is the file rows of pod in the file behind svc.
func rowsOf(t *testing.T, svc *Service, f *fakeService, pod string) []logstore.Stream {
	t.Helper()
	rows, err := fileOf(t, svc, f).PodStreams(t.Context(), "prod", pod, "")
	require.NoError(t, err)
	return rows
}

// sweepFile runs one sweep of the file behind svc.
func sweepFile(t *testing.T, svc *Service, f *fakeService) {
	t.Helper()
	require.NoError(t, fileOf(t, svc, f).Sweep(t.Context()))
}

// A page holds the rows it files until it has answered: a sweep while its fill reads
// leaves a row with nothing in it yet, whatever the mirror says of its pod.
func TestAPageHoldsItsRows(t *testing.T) {
	t.Parallel()
	backend := newFakeBackend()
	release := make(chan struct{})
	backend.set("web-1/app", fakeRead{lines: []logline.Record{{Timestamp: at(time.Second), Message: "1"}}, wait: release})
	svc, f := serviceOn(t, backend)
	everyPodGone(svc)
	done := make(chan error, 1)
	go func() {
		_, err := svc.Fetch(t.Context(), "7", FetchRequest{Scope: web1Scope, PageFrom: PageFromFirst, PageSize: 10})
		done <- err
	}()
	backend.opened.Await(t, "the page's read")

	sweepFile(t, svc, f)
	assert.Len(t, rowsOf(t, svc, f, "web-1"), 1, "held while the page reads")

	close(release)
	require.NoError(t, testutil.Recv(t, done, "the page"))
}

// A stream whose fill failed before any line is the page's skipped entry, and its
// walk still runs: the lines the file holds of it up to its first live gap answer,
// and its stop at that gap does not move the cut.
func TestASkippedStreamAnswersFromTheFile(t *testing.T) {
	t.Parallel()
	s := newLogServer()
	s.Add("prod/web-2/app", logstest.Stamped(500*time.Millisecond, "b0"), logstest.Stamped(5*time.Second, "b5"))
	s.SetRefusal("prod/web-1/app", &logstest.Denial{Code: 404, Message: `pods "web-1" not found`})
	svc, f := serviceOver(t, webMirror(t), s)
	seed(t, svc, f, rowOf("web-1", "app"), logstore.Fill{Lines: fileLines(0, time.Second), AtBeginning: true})

	page, err := svc.Fetch(t.Context(), "7", FetchRequest{Scope: webScope, PageFrom: PageFromFirst, PageSize: 10})

	require.NoError(t, err)
	assert.Equal(t, []string{"0", "b0", "1", "b5"}, messages(page.Records))
	assert.Equal(t, []string{`web-1/app: pods "web-1" not found`}, skippedOf(page))
	assert.Nil(t, page.Page.Next)
}

// A skipped stream whose file span is denser than the page stops at limit plus one
// lines, which hold the cut as any stream's do, so every line goes out once. Only a
// page that reaches the stream's gap reads the kubelet, and is refused there.
func TestADenseSkippedStreamHoldsTheCut(t *testing.T) {
	t.Parallel()
	s := newLogServer()
	var want []string
	var dense []time.Duration
	for i := range 20 {
		dense = append(dense, time.Duration(i)*time.Second)
		want = append(want, strconv.Itoa(i))
	}
	for i := range 5 {
		s.Add("prod/web-2/app", logstest.Stamped(time.Duration(4*i)*time.Second+500*time.Millisecond, "b"+strconv.Itoa(4*i)))
		want = slices.Insert(want, slices.Index(want, strconv.Itoa(4*i))+1, "b"+strconv.Itoa(4*i))
	}
	s.SetRefusal("prod/web-1/app", &logstest.Denial{Code: 404, Message: `pods "web-1" not found`})
	svc, f := serviceOver(t, webMirror(t), s)
	seed(t, svc, f, rowOf("web-1", "app"), logstore.Fill{Lines: fileLines(dense...), AtBeginning: true})

	got := pages(t, svc, webScope, PageFromFirst, 3)

	assert.Equal(t, want, recordsOf(got, PageFromFirst))
	assert.Empty(t, skippedOf(got[0]), "the file holds the first page")
	assert.Equal(t, []string{`web-1/app: pods "web-1" not found`}, skippedOf(got[len(got)-1]))
}

// A grep matches as Go's regexp does, flags and all, over the stored text without
// its colours; the record keeps them.
func TestAGrepMatchesTheStoredText(t *testing.T) {
	t.Parallel()
	s := newLogServer()
	s.Add("prod/web-1/app", logstest.Stamped(0, "\x1b[31mERROR\x1b[0m disk"), logstest.Stamped(time.Second, "error net"), logstest.Stamped(2*time.Second, "ok"))
	svc, _ := serviceOver(t, webMirror(t), s)
	view := web1Scope
	view.Grep = `(?i)^error\b`

	page, err := svc.Fetch(t.Context(), "7", FetchRequest{Scope: view, PageFrom: PageFromFirst, PageSize: 10})

	require.NoError(t, err)
	assert.Equal(t, []string{"\x1b[31mERROR\x1b[0m disk", "error net"}, messages(page.Records))
}

// A Tail page that needs fewer lines than the log's first moment holds leaves the
// rest of that moment to the next page: the read reached the backend's oldest
// moment, but stopped inside it, so nothing older is lost.
func TestATailPageInsideTheFirstMomentLosesNothing(t *testing.T) {
	t.Parallel()
	s := newLogServer()
	for _, msg := range []string{"a", "b", "c", "d", "e"} {
		s.Add("prod/web-1/app", logstest.Stamped(0, msg))
	}
	svc, _ := serviceOver(t, webMirror(t), s)

	assert.Equal(t, []string{"a", "b", "c", "d", "e"}, recordsOf(pages(t, svc, web1Scope, PageFromLast, 1), PageFromLast))
}

// A view of one container of a pod holding more containers than Kubetail reads at
// once reads it, as the LogsView tool's check passes it, and so does one whose filter
// leaves the pod out: the limit is on what the view selects, never on the pod.
func TestOneContainerOfALargePodIsRead(t *testing.T) {
	t.Parallel()
	containers := make([]container, MaxStreams+5)
	for i := range containers {
		containers[i] = container{name: "c" + strconv.Itoa(i), restarted: i == 7}
	}
	s := newLogServer()
	s.Add("prod/big/c7", logstest.Stamped(0, "seven"))
	svc, _ := serviceOver(t, newMirror(t, append([]map[string]any{pod("big", "uid-big", "node-a", nil, containers...)}, webNodes()...)...), s)

	for _, view := range []Scope{podSource("big", false, "c7"), podSource("big", false)} {
		page, err := svc.Fetch(t.Context(), "7", FetchRequest{Scope: view, PageFrom: PageFromFirst, PageSize: 10})
		require.NoError(t, err)
		want := map[bool][]string{true: {"seven"}, false: {}}[len(view.Sources[0].Containers) > 0]
		assert.Equal(t, want, messages(page.Records))
	}
	all := Scope{Sources: []ScopeSource{{Namespace: "prod", Kind: SourcePod, Name: "big", AllContainers: true}}}
	_, err := svc.Fetch(t.Context(), "7", FetchRequest{Scope: all, PageFrom: PageFromFirst, PageSize: 10})
	assert.ErrorIs(t, err, ErrTooMany)

	previous := all
	previous.Sources = []ScopeSource{{Namespace: "prod", Kind: SourcePod, Name: "big", AllContainers: true, Previous: true}}
	_, err = svc.Fetch(t.Context(), "7", FetchRequest{Scope: previous, PageFrom: PageFromFirst, PageSize: 10})
	require.NoError(t, err, "the one previous instance")

	elsewhere := all
	elsewhere.Filters = []ScopeFilter{{Field: FilterZone, Values: []string{"eu-1b"}}}
	page, err := svc.Fetch(t.Context(), "7", FetchRequest{Scope: elsewhere, PageFrom: PageFromFirst, PageSize: 10})
	require.NoError(t, err, "a filter that leaves the pod out")
	assert.Empty(t, page.Records)
}

// A last page whose only lines left to read are a skipped stream's refuses to go on
// rather than answering an empty page at the cursor it was given, which would hand
// back the same page forever.
func TestATailPageThatCannotAdvanceIsRefused(t *testing.T) {
	t.Parallel()
	s := newLogServer()
	s.Add("prod/web-2/app", logstest.Stamped(0, "b0"))
	s.SetRefusal("prod/web-1/app", &logstest.Denial{Code: 503, Message: "unavailable"})
	svc, _ := serviceOver(t, webMirror(t), s)

	page, err := svc.Fetch(t.Context(), "7", FetchRequest{Scope: webScope, PageFrom: PageFromLast, PageSize: 5})
	require.NoError(t, err)
	assert.Equal(t, []string{"b0"}, messages(page.Records))
	require.NotNil(t, page.Page.Prev, "web-1 is still to read")

	_, err = svc.Fetch(t.Context(), "7", FetchRequest{Cursor: page.Page.Prev, PageSize: 5})
	assert.EqualError(t, err, "Kubetail could not read older lines of this view: web-1/app: unavailable")
	assert.Equal(t, refusal.CodeReadFailed, codeOf(err))
}

// A page cursor or a Seen entry the service did not write is refused as
// cursor-invalid, and so is a grep that is no regular expression as bad-grep.
func TestFetchRefusesABadCursor(t *testing.T) {
	t.Parallel()
	svc, _ := serviceOver(t, webMirror(t), newLogServer())
	bad := "yesterday"
	_, err := svc.Fetch(t.Context(), "7", FetchRequest{Cursor: &bad})
	assert.EqualError(t, err, "this is not a page cursor a page handed back")
	assert.Equal(t, refusal.CodeCursorInvalid, codeOf(err))
	_, err = svc.Fetch(t.Context(), "7", FetchRequest{Scope: webScope, PageFrom: PageFromFirst, Seen: []string{bad}})
	assert.EqualError(t, err, "this is not a line cursor a page handed back")
	assert.Equal(t, refusal.CodeCursorInvalid, codeOf(err))

	view := webScope
	view.Grep = "("
	_, err = svc.Fetch(t.Context(), "7", FetchRequest{Scope: view, PageFrom: PageFromFirst, PageSize: 10})
	assert.EqualError(t, err, "the grep is not a regular expression")
	assert.Equal(t, refusal.CodeBadGrep, codeOf(err))
}

// A grep past MaxGrep is refused by a page and by a tail alike, since the wire
// takes any view the webview sends; one at the cap is read.
func TestAGrepPastMaxGrepIsRefused(t *testing.T) {
	t.Parallel()
	svc, _ := serviceOver(t, webMirror(t), newLogServer())
	view := webScope
	view.Grep = strings.Repeat("a", logline.MaxGrep+1)

	_, err := svc.Fetch(t.Context(), "7", FetchRequest{Scope: view, PageFrom: PageFromFirst, PageSize: 10})
	assert.EqualError(t, err, "a grep is at most 1000 bytes")
	_, err = svc.TailFromCursor(t.Context(), "7", TailRequest{Scope: view})
	assert.EqualError(t, err, "a grep is at most 1000 bytes")

	view.Grep = strings.Repeat("a", logline.MaxGrep)
	_, err = svc.Fetch(t.Context(), "7", FetchRequest{Scope: view, PageFrom: PageFromFirst, PageSize: 10})
	assert.NoError(t, err)
}

// A fetch is refused what resolving the view or claiming the cluster refuses.
func TestFetchIsRefusedWhatItsReadsRefuse(t *testing.T) {
	t.Parallel()
	svc, f := serviceOver(t, webMirror(t), newLogServer())
	f.active.Cache = nil
	_, err := svc.Fetch(t.Context(), "7", FetchRequest{Scope: webScope, PageFrom: PageFromFirst, PageSize: 10})
	assert.ErrorIs(t, err, ErrNoCache)

	svc, f = serviceOver(t, webMirror(t), newLogServer())
	f.acquireErr = cluster.ErrNotConnectable
	_, err = svc.Fetch(t.Context(), "7", FetchRequest{Scope: webScope, PageFrom: PageFromFirst, PageSize: 10})
	assert.ErrorIs(t, err, kubelet.ErrRevoked)
}

// A scope no pod passes reads nothing, and answers an empty page that polls its
// open window.
func TestFetchOfAViewWithNoContainerIsEmpty(t *testing.T) {
	t.Parallel()
	s := newLogServer()
	svc, _ := serviceOver(t, webMirror(t), s)
	view := webScope
	view.Filters = []ScopeFilter{{Field: FilterZone, Values: []string{"nowhere"}}}

	page, err := svc.Fetch(t.Context(), "7", FetchRequest{Scope: view, PageFrom: PageFromFirst, PageSize: 10})

	require.NoError(t, err)
	assert.Empty(t, page.Records)
	assert.Empty(t, page.Notices)
	assert.Equal(t, [3]bool{false, false, true}, edgesOf(page.Page))
	assert.Empty(t, s.Requests())
}

// A view of more containers than Kubetail reads at once is refused before any is
// read.
func TestAFetchOfTooManyContainersIsRefused(t *testing.T) {
	t.Parallel()
	s := newLogServer()
	f := webMirror(t)
	addPods(t, f, MaxStreams-1)
	svc, _ := serviceOver(t, f, s)

	_, err := svc.Fetch(t.Context(), "7", FetchRequest{Scope: webScope, PageFrom: PageFromLast, PageSize: 5})
	assert.ErrorIs(t, err, ErrTooMany)
	assert.Empty(t, s.Requests())
}

// A fetch ends with its context. It checks the cluster once and watches nothing,
// since the connection's retirement already bounds a page.
func TestFetchEndsWithItsContextAndWatchesNothing(t *testing.T) {
	t.Parallel()
	svc, _ := serviceOver(t, webMirror(t), newLogServer())
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err := svc.Fetch(ctx, "7", FetchRequest{Scope: webScope, PageFrom: PageFromFirst, PageSize: 10})
	assert.ErrorIs(t, err, context.Canceled)

	s := newLogServer()
	s.Add("prod/web-1/app", logstest.Stamped(0, "a"))
	svc, _ = serviceOver(t, webMirror(t), s)
	page, err := svc.Fetch(t.Context(), "7", FetchRequest{Scope: webScope, PageFrom: PageFromFirst, PageSize: 10})
	require.NoError(t, err)
	assert.Equal(t, []string{"a"}, messages(page.Records))
}

// A first page's two reads share one resolve of the scope, so they read the same
// streams: one reading of the mirror to resolve, and one claim. Each window written
// checks the stream's instance with a reading of its own.
func TestAFirstPagesTwoReadsResolveTheScopeOnce(t *testing.T) {
	t.Parallel()
	s := newLogServer()
	s.Add("prod/web-1/app", logstest.LineEvery(10)...)
	svc, f := serviceOver(t, webMirror(t), s)
	readings := f.readings.Load()

	page, err := svc.Fetch(t.Context(), "7", aroundAt(web1Scope, 5*time.Second, 2))

	require.NoError(t, err)
	assert.Equal(t, []string{"3", "4", "5", "6"}, messages(page.Records))
	assert.Equal(t, int32(4), f.readings.Load()-readings, "the resolve, and a check for the forward read, the backward read's window and its beginning mark")
	assert.Equal(t, int32(1), f.lease.released.Load(), "one claim")
}

// A poll reads each stream on from its own last line, so a line another stream
// writes after the page, at a moment the page already passed, is on the poll.
func TestAPollKeepsALaterLineAtItsMoment(t *testing.T) {
	t.Parallel()
	s := newLogServer()
	s.Add("prod/web-1/app", logstest.Stamped(time.Second, "a"))
	svc, _ := serviceOver(t, webMirror(t), s)

	page := fetch(t, svc, FetchRequest{Scope: webScope, PageFrom: PageFromFirst, PageSize: 10})
	require.Equal(t, []string{"a"}, messages(page.Records))
	require.NotNil(t, page.Page.Follow)

	s.Add("prod/web-2/app", logstest.Stamped(time.Second, "b"))
	page = goOn(t, svc, page.Page.Follow, 10)

	assert.Equal(t, []string{"b"}, messages(page.Records))
}

// A first page over a stream skipped on a transient failure has not read every
// stream to its end: its Next goes on rather than polling, and the page past it
// is refused once nothing newer is left but that stream's.
func TestAHeadPageThatCannotAdvanceIsRefused(t *testing.T) {
	t.Parallel()
	s := newLogServer()
	s.Add("prod/web-2/app", logstest.Stamped(0, "b0"))
	s.SetRefusal("prod/web-1/app", &logstest.Denial{Code: 503, Message: "unavailable"})
	svc, _ := serviceOver(t, webMirror(t), s)

	page, err := svc.Fetch(t.Context(), "7", FetchRequest{Scope: webScope, PageFrom: PageFromFirst, PageSize: 5})
	require.NoError(t, err)
	assert.Equal(t, []string{"b0"}, messages(page.Records))
	require.NotNil(t, page.Page.Next, "web-1 is still to read")

	_, err = svc.Fetch(t.Context(), "7", FetchRequest{Cursor: page.Page.Next, PageSize: 5})
	assert.EqualError(t, err, "Kubetail could not read newer lines of this view: web-1/app: unavailable")
}

// A fill that fails the page ends the others rather than letting them read on: a
// stream whose read would never end on its own ends with it.
func TestAFatalFillEndsTheOthers(t *testing.T) {
	t.Parallel()
	backend := newFakeBackend()
	backend.set("web-1/app", fakeRead{err: &kubelet.StatusError{Code: 401, Message: "Unauthorized"}})
	backend.set("web-2/app", fakeRead{wait: never})
	svc, _ := serviceOn(t, backend)

	var err error
	testutil.WaitReturn(t, func() {
		_, err = svc.Fetch(t.Context(), "7", FetchRequest{Scope: webScope, PageFrom: PageFromFirst, PageSize: 10})
	}, "the page")
	assert.EqualError(t, err, "the cluster refused to show this log: Unauthorized")
}

// A page the log file refuses fails: the file closed before the page opens it, in
// either mode, or taken by a clear before the page fills it or writes what it read.
func TestAPageTheLogFileRefusesFails(t *testing.T) {
	t.Parallel()
	svc, _ := serviceOver(t, webMirror(t), newLogServer())
	require.NoError(t, svc.files.Close())
	for name, q := range map[string]FetchRequest{
		"one read":  {Scope: webScope, PageFrom: PageFromFirst, PageSize: 10},
		"two reads": aroundAt(webScope, 0, 10),
	} {
		_, err := svc.Fetch(t.Context(), "7", q)
		assert.ErrorIs(t, err, logstore.ErrClosed, name)
	}

	s := newLogServer()
	s.Add("prod/web-1/app", logstest.Stamped(0, "a"))
	svc, f := serviceOver(t, webMirror(t), s)
	f.beforeStatement = func(text string) {
		if strings.Contains(text, "SELECT p.uid, coalesce(c.container_id") {
			assert.NoError(t, svc.files.Clear(int64(f.active.Cache.ID)))
		}
	}
	_, err := svc.Fetch(t.Context(), "7", FetchRequest{Scope: webScope, PageFrom: PageFromFirst, PageSize: 10})
	assert.ErrorIs(t, err, logstore.ErrClosed)

	svc, f = serviceOver(t, webMirror(t), s)
	f.onAcquire = func() { assert.NoError(t, svc.Clear(int64(f.active.Cache.ID))) }
	_, err = svc.Fetch(t.Context(), "7", FetchRequest{Scope: webScope, PageFrom: PageFromFirst, PageSize: 10})
	assert.ErrorIs(t, err, logstore.ErrClosed)
}

// A page fails with the log file wherever it fails: filing the view's streams,
// finishing the instance a restart ended, reading a source's rows, or walking what
// its fill wrote. A mirror change lands after a first page, so the file holds rows
// the change bears on.
func TestAPageFailsWithItsFile(t *testing.T) {
	t.Parallel()
	idle := Scope{Sources: []ScopeSource{{Namespace: "prod", Kind: SourceDeployment, Name: "idle"}}}
	for name, c := range map[string]struct {
		view      Scope
		then      map[string]any
		ddl       string
		afterFill bool
		want      string
	}{
		"filing a stream": {view: web1Scope, ddl: `CREATE TRIGGER broken BEFORE INSERT ON streams BEGIN SELECT RAISE(FAIL, 'broken'); END`, want: "broken"},
		"finishing a restarted instance": {
			view: web1Scope, then: restartedWeb1(),
			ddl:  `CREATE TRIGGER broken BEFORE UPDATE OF finished_at ON streams BEGIN SELECT RAISE(FAIL, 'broken'); END`,
			want: "broken",
		},
		"a pod's rows":      {view: podView("nope"), ddl: `DROP TABLE streams`, want: "no such table: streams"},
		"a workload's rows": {view: idle, then: workload("apps/v1", "Deployment", "idle", "dep-idle", nil), ddl: `DROP TABLE pod_owners`, want: "no such table: pod_owners"},
		"the walk's chunks": {view: web1Scope, ddl: `DROP TABLE chunks`, afterFill: true, want: "no such table: chunks"},
		"the walk's lines":  {view: web1Scope, ddl: `DROP TABLE lines`, afterFill: true, want: "no such table: lines"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			s := newLogServer()
			s.Add("prod/web-1/app", logstest.Stamped(0, "a"))
			dir := t.TempDir()
			svc, f := serviceIn(t, webMirror(t), s, dir, testRetention)
			if c.then != nil {
				fetch(t, svc, FetchRequest{Scope: c.view, PageFrom: PageFromFirst, PageSize: 10})
				f.store.Apply(t, c.then)
			}
			fileOf(t, svc, f)
			if c.afterFill {
				svc.afterFill = func() { breakFile(t, dir, f, c.ddl) }
			} else {
				breakFile(t, dir, f, c.ddl)
			}

			_, err := svc.Fetch(t.Context(), "7", FetchRequest{Scope: c.view, PageFrom: PageFromFirst, PageSize: 10})

			assert.ErrorContains(t, err, c.want)
		})
	}
}

// A page the log file closes under between its fill and its walk fails.
func TestAPageWhoseFileClosesBeforeItsWalkFails(t *testing.T) {
	t.Parallel()
	s := newLogServer()
	s.Add("prod/web-1/app", logstest.Stamped(0, "a"))
	svc, f := serviceOver(t, webMirror(t), s)
	svc.afterFill = func() { assert.NoError(t, svc.Clear(int64(f.active.Cache.ID))) }

	_, err := svc.Fetch(t.Context(), "7", FetchRequest{Scope: web1Scope, PageFrom: PageFromFirst, PageSize: 10})

	assert.ErrorIs(t, err, logstore.ErrClosed)
}

// A backward page that cannot sample the rate of the lines above its gap fails.
func TestAPageThatCannotSampleTheRateAboveItsGapFails(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	svc, f := serviceIn(t, webMirror(t), newLogServer(), dir, testRetention)
	seed(t, svc, f, rowOf("web-1", "app"), logstore.Fill{Lines: fileLines(10*time.Second, 11*time.Second)})
	breakFile(t, dir, f, `DROP TABLE lines`)

	_, err := svc.Fetch(t.Context(), "7", prevFrom(web1Scope, "web-1", "app", 9*time.Second, 2))

	assert.ErrorContains(t, err, "no such table: lines")
}

// A page whose check of which pods are gone fails is the failure.
func TestAPageWhoseGonePodCheckFailsFails(t *testing.T) {
	t.Parallel()
	s := newLogServer()
	s.Add("prod/web-1/app", logstest.Stamped(time.Second, "a1"))
	svc, f := serviceOver(t, webMirror(t), s)
	fetch(t, svc, FetchRequest{Scope: webScope, PageFrom: PageFromFirst, PageSize: 10})
	f.store.Delete(t, web1())
	f.queryErr, f.queryErrOn = errors.New("db closed"), "AND uid IN (SELECT value FROM json_each"

	_, err := svc.Fetch(t.Context(), "7", FetchRequest{Scope: webScope, PageFrom: PageFromFirst, PageSize: 10})

	assert.EqualError(t, err, "db closed")
}

// A page a stream of which the connection's retirement cut before its first line is
// refused as replaced, rather than passing lines the stream never read.
func TestAPageARetirementCutBeforeItsFirstLineIsReplaced(t *testing.T) {
	t.Parallel()
	svc, _ := serviceOn(t, newRetiredBackend())

	_, err := svc.Fetch(t.Context(), "7", FetchRequest{Scope: web1Scope, PageFrom: PageFromFirst, PageSize: 10})

	assert.ErrorIs(t, err, errReplaced)
}

// A page whose write of a window's batch fails is the failure: a forward read's first
// batch, full before its second line, and a backward read's window the read ended
// inside.
func TestAPageWhoseBatchWriteFailsFails(t *testing.T) {
	t.Parallel()
	one := []logline.Record{{Timestamp: at(time.Second), Message: "1"}}
	two := []logline.Record{one[0], {Timestamp: at(2 * time.Second), Message: "2"}}
	for name, c := range map[string]struct {
		from  PageFrom
		lines []logline.Record
	}{
		"forward":  {PageFromFirst, two},
		"backward": {PageFromLast, one},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			backend := newFakeBackend()
			backend.set("web-1/app", fakeRead{lines: c.lines})
			svc, f := serviceBatched(t, backend, 1)
			f.queryErr, f.queryErrOn = errors.New("db closed"), "SELECT p.uid, coalesce(c.container_id"

			_, err := svc.Fetch(t.Context(), "7", FetchRequest{Scope: web1Scope, PageFrom: c.from, PageSize: 10})

			assert.EqualError(t, err, "db closed")
		})
	}
}

// A page every stream of which failed, with no line to show, is refused naming the
// first.
func TestAPageEveryStreamFailedIsRefused(t *testing.T) {
	t.Parallel()
	s := newLogServer()
	s.SetRefusal("prod/web-1/app", &logstest.Denial{Code: 404, Message: `pods "web-1" not found`})
	s.SetRefusal("prod/web-2/app", &logstest.Denial{Code: 404, Message: `pods "web-2" not found`})
	svc, _ := serviceOver(t, webMirror(t), s)

	_, err := svc.Fetch(t.Context(), "7", FetchRequest{Scope: webScope, PageFrom: PageFromFirst, PageSize: 10})

	assert.ErrorContains(t, err, "no container of this view could be read: ")
}

// A first page's two reads name a stream both skipped once.
func TestAJoinedPageNamesASkippedStreamOnce(t *testing.T) {
	t.Parallel()
	page := joinReads(merge{records: make([]logline.Record, 3), skipped: []string{"a"}}, merge{skipped: []string{"a", "b"}}, true, 10)
	assert.Equal(t, []string{"a", "b"}, page.skipped)
	assert.Len(t, page.records, 3)
}

// loudAround is web-1/app and web-2/app each logging a line a second from 10:00:00 to
// 10:00:09, with web-1/app also logging a loud second at 10:00:05, and a service
// reading it under smallCaps.
func loudAround(t *testing.T) (*Service, *logstest.Server) {
	t.Helper()
	s := newLogServer()
	for i := range 10 {
		d := time.Duration(i) * time.Second
		s.Add("prod/web-2/app", logstest.Stamped(d+500*time.Millisecond, "web-2 "+strconv.Itoa(i)))
		s.Add("prod/web-1/app", logstest.Stamped(d, "web-1 "+strconv.Itoa(i)))
		if i == 5 {
			for j := range 12 {
				s.Add("prod/web-1/app", logstest.Stamped(d+time.Duration(1+j)*time.Millisecond, logstest.LoudLine))
			}
		}
	}
	svc, _ := serviceOver(t, webMirror(t), s)
	svc.caps = smallCaps
	return svc, s
}

// A stream too loud to read in one second stops there, and the page is cut at that
// second's top: the stream is named in skipped with the reason, the other streams'
// lines at and after that second are on the page, and the next page reads on below
// it.
func TestATooLoudSecondCutsATailPage(t *testing.T) {
	t.Parallel()
	svc, _ := loudAround(t)

	page, err := svc.Fetch(t.Context(), "7", FetchRequest{Scope: webScope, Window: until(9 * time.Second), PageFrom: PageFromLast, PageSize: 100})

	require.NoError(t, err)
	assert.Equal(t, []string{"web-2 5", "web-1 6", "web-2 6", "web-1 7", "web-2 7", "web-1 8", "web-2 8"}, messages(page.Records))
	assert.Equal(t, []string{"web-1/app: " + kubelet.LoudAt(at(5*time.Second)).Error()}, skippedOf(page))
	assert.Equal(t, *cursorOf(logline.MomentAt(at(5 * time.Second))), atOf(t, page.Page.Prev))

	next := goOn(t, svc, page.Page.Prev, 100)
	assert.Equal(t, []string{"web-1 0", "web-2 0", "web-1 1", "web-2 1", "web-1 2", "web-2 2", "web-1 3", "web-2 3", "web-1 4", "web-2 4"}, messages(next.Records))
	assert.Empty(t, skippedOf(next))
}

// Forward, the page is cut at the loud second's last nanosecond, with the lines of
// it the read took before the limit, and the stream's entry reads on from the second
// after it, so the next page does not stop again.
func TestATooLoudSecondCutsAHeadPage(t *testing.T) {
	t.Parallel()
	svc, s := loudAround(t)

	page, err := svc.Fetch(t.Context(), "7", FetchRequest{Scope: webScope, Window: Window{Since: moment(3*time.Second + time.Nanosecond)}, PageFrom: PageFromFirst, PageSize: 500})

	require.NoError(t, err)
	got := messages(page.Records)
	assert.Equal(t, []string{"web-2 3", "web-1 4", "web-2 4", "web-1 5"}, got[:4])
	assert.Equal(t, "web-2 5", got[len(got)-1])
	assert.Len(t, got, 5+7, "the loud lines that fit in the second beside web-1 5")
	assert.Equal(t, []string{"web-1/app: " + kubelet.LoudAt(at(5*time.Second)).Error()}, skippedOf(page))
	assert.Equal(t, logline.RunPlace{Timestamp: at(6*time.Second - 1)}, cursorIn(t, page.Page.Next).seen[name("web-1", "app")], "web-1 goes on past the second")

	next := goOn(t, svc, page.Page.Next, 500)
	assert.Equal(t, []string{"web-1 6", "web-2 6", "web-1 7", "web-2 7", "web-1 8", "web-2 8", "web-1 9", "web-2 9"}, messages(next.Records))
	assert.Empty(t, skippedOf(next))
	reads := s.RequestsOf("prod/web-1/app")
	assert.Equal(t, "2026-10-08T10:00:06Z", reads[len(reads)-1].Get("sinceTime"))
}

// A view whose only stream is too loud answers a page with no lines, the reason and
// more, not a refusal; the next page reads the lines below the second.
func TestAViewOfOneTooLoudStreamAnswersThePage(t *testing.T) {
	t.Parallel()
	svc, _ := loudAround(t)

	page, err := svc.Fetch(t.Context(), "7", FetchRequest{Scope: web1Scope, Window: until(5*time.Second + 500*time.Millisecond), PageFrom: PageFromLast, PageSize: 100})

	require.NoError(t, err)
	assert.Empty(t, page.Records)
	assert.Equal(t, []string{"web-1/app: " + kubelet.LoudAt(at(5*time.Second)).Error()}, skippedOf(page))
	assert.NotNil(t, page.Page.Prev)

	next := goOn(t, svc, page.Page.Prev, 100)
	assert.Equal(t, []string{"web-1 0", "web-1 1", "web-1 2", "web-1 3", "web-1 4"}, messages(next.Records))
}

// A quiet stream cut when its last window that completed was empty is cut at that
// window's start, and the next page goes on below it.
func TestAnEmptyWindowsCutIsItsStart(t *testing.T) {
	t.Parallel()
	s := newLogServer()
	s.Add("prod/web-1/app", logstest.LineEvery(10)...)
	svc, _ := serviceOver(t, webMirror(t), s)
	svc.caps.Gets = 3

	page, err := svc.Fetch(t.Context(), "7", FetchRequest{Scope: web1Scope, Window: Window{Until: moment(2 * time.Minute)}, PageFrom: PageFromLast, PageSize: 10})

	require.NoError(t, err)
	assert.Empty(t, page.Records)
	assert.Empty(t, skippedOf(page))
	// Ten seconds back, then eight times that.
	assert.Equal(t, *cursorOf(logline.MomentAt(at(30 * time.Second))), atOf(t, page.Page.Prev))
	assert.Equal(t, messages(linesOf(logstest.LineEvery(10))), recordsOf(pagesOn(t, svc, page, 10), PageFromLast))
}

// A stream the pool let read far is walked as far: its walk reads its share of the
// walks' budget and what its fill downloaded, so a sparse grep does not return an
// empty page over lines already in the file.
func TestTheWalkFollowsTheFill(t *testing.T) {
	t.Parallel()
	s := newLogServer()
	s.Add("prod/web-1/app", logstest.Stamped(0, "match"))
	for i := range 100 {
		s.Add("prod/web-1/app", logstest.Stamped(time.Duration(1+i)*time.Second, strings.Repeat("x", 80)))
	}
	svc, _ := serviceOver(t, webMirror(t), s)
	svc.walkBudget = 1 << 10
	view := webScope
	view.Grep = "^match"

	page, err := svc.Fetch(t.Context(), "7", FetchRequest{Scope: view, PageFrom: PageFromLast, PageSize: 10})

	require.NoError(t, err)
	assert.Equal(t, []string{"match"}, messages(page.Records))
	assert.Nil(t, page.Page.Prev)
}

// hundredStreams is a mirror of a hundred pods of web, and the key of each one's
// app container.
func hundredStreams(t *testing.T) (*fakeService, []string) {
	t.Helper()
	f := webMirror(t)
	addPods(t, f, 98)
	keys := []string{"prod/web-1/app", "prod/web-2/app"}
	for i := range 98 {
		keys = append(keys, "prod/many-"+strconv.Itoa(i)+"/app")
	}
	return f, keys
}

// The regression: a view of a hundred streams, one of which bursts for ten seconds
// just before the cursor at two thirds of what a second may hold, pages back with
// no refusal and no stopped stream. Under midCaps, a second holds 16 KiB and the
// burst is 83 lines of 100 bytes a second.
func TestABurstInAHundredStreamsPagesBack(t *testing.T) {
	t.Parallel()
	s := newLogServer()
	f, keys := hundredStreams(t)
	for i, key := range keys {
		s.Add(key, logstest.Stamped(time.Duration(i)*time.Second, "quiet"))
	}
	for i := range 833 {
		s.Add("prod/web-1/app", logstest.Stamped(100*time.Second+time.Duration(i)*12*time.Millisecond, logstest.LoudLine))
	}
	svc, _ := serviceOver(t, f, s)
	svc.caps = midCaps

	got := pagesOn(t, svc, fetch(t, svc, FetchRequest{Scope: webScope, Window: until(110 * time.Second), PageFrom: PageFromLast, PageSize: MaxPageSize}), MaxPageSize)

	for _, page := range got {
		assert.Empty(t, skippedOf(page))
	}
	assert.Len(t, recordsOf(got, PageFromLast), 833+100)
}

// counting reads through the kubelet over a get whose bodies count the bytes read
// from them in n.
func counting(svc *Service, n *atomic.Int64) {
	svc.backends = []namedBackend{{"kubelet", func(get kubelet.Getter) kubelet.Backend {
		return kubelet.New(func(ctx context.Context, path string, q url.Values) (io.ReadCloser, error) {
			body, err := get(ctx, path, q)
			if err != nil {
				return nil, err
			}
			return countedBody{body, n}, nil
		})
	}}}
}

// countedBody counts the bytes read from it in n.
type countedBody struct {
	io.ReadCloser
	n *atomic.Int64
}

func (b countedBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	b.n.Add(int64(n))
	return n, err
}

// A sparse grep over a hundred logs shares the query's limit: the first page
// downloads about the query's bytes and what its GETs read past their windows, not
// every log. Under smallCaps, a hundred 36 KiB logs and a 64 KiB query.
func TestFirstWindowsShareTheLimit(t *testing.T) {
	t.Parallel()
	s := newLogServer()
	f, keys := hundredStreams(t)
	for _, key := range keys {
		s.Add(key, logstest.WideLines(300, 100)...)
	}
	svc, _ := serviceOver(t, f, s)
	svc.caps = smallCaps
	var n atomic.Int64
	counting(svc, &n)
	view := webScope
	view.Grep = "never"

	page, err := svc.Fetch(t.Context(), "7", FetchRequest{Scope: view, PageFrom: PageFromLast, PageSize: 10})

	require.NoError(t, err)
	assert.NotNil(t, page.Page.Prev)
	// A GET reads at most a 4 KiB buffer past its window, and a stream sends at most
	// three before it is cut.
	assert.Less(t, n.Load(), smallCaps.Query+100*3*(4<<10))
}

// A sparse grep over several logs past a stream's limit pages on with more until
// every stream reaches its first line, none stopped. Four 12 KiB logs under
// smallCaps, three GETs a stream.
func TestAGrepWithNoMatchPagesToEveryFirstLine(t *testing.T) {
	t.Parallel()
	s := newLogServer()
	f := webMirror(t)
	addPods(t, f, 2)
	for _, key := range []string{"prod/web-1/app", "prod/web-2/app", "prod/many-0/app", "prod/many-1/app"} {
		s.Add(key, logstest.WideLines(100, 100)...)
	}
	svc, _ := serviceOver(t, f, s)
	svc.caps = smallCaps
	svc.caps.Gets = 3
	view := webScope
	view.Grep = "never"

	got := pages(t, svc, view, PageFromLast, 10)

	assert.Greater(t, len(got), 1, "cut")
	for _, p := range got {
		assert.Empty(t, p.Records)
		assert.Empty(t, skippedOf(p))
	}
	assert.Nil(t, got[len(got)-1].Page.Prev)
}

// A grep with no match over one stream reads back to the log's first line: the
// page answers no record and no more.
func TestAGrepWithNoMatchReadsToTheFirstLine(t *testing.T) {
	t.Parallel()
	s := newLogServer()
	s.Add("prod/web-1/app", logstest.LineEvery(1000)...)
	svc, _ := serviceOver(t, webMirror(t), s)
	view := web1Scope
	view.Grep = "never"

	page, err := svc.Fetch(t.Context(), "7", FetchRequest{Scope: view, PageFrom: PageFromLast, PageSize: 10})

	require.NoError(t, err)
	assert.Empty(t, page.Records)
	assert.Nil(t, page.Page.Prev)
}

// A stream is cut at its limits, backward and forward, and each next page goes on
// from its cut: a sparse grep over 640 KiB under limits scaled down 64 times reads
// every match over several pages.
func TestAStreamsCutsJoin(t *testing.T) {
	t.Parallel()
	for _, mode := range []PageFrom{PageFromLast, PageFromFirst} {
		t.Run(string(mode), func(t *testing.T) {
			t.Parallel()
			s := newLogServer()
			var log []string
			var want []string
			for i := range 320 {
				msg := strconv.Itoa(i) + strings.Repeat("x", 2<<10)
				if i%40 == 7 {
					msg = "match " + strconv.Itoa(i)
					want = append(want, msg)
				}
				log = append(log, logstest.Stamped(time.Duration(i)*time.Second, msg))
			}
			s.Add("prod/web-1/app", log...)
			svc, _ := serviceOver(t, webMirror(t), s)
			svc.caps = midCaps
			view := web1Scope
			view.Grep = "^match"

			got := pages(t, svc, view, mode, 100)

			assert.Greater(t, len(got), 1, "cut")
			assert.Equal(t, want, recordsOf(got, mode))
		})
	}
}

// gated is a Backend that runs wait before each read it hands to b.
type gated struct {
	b    kubelet.Backend
	wait func(st kubelet.Log, o kubelet.Options)
}

func (w gated) Forward(ctx context.Context, st kubelet.Log, o kubelet.Options) *kubelet.Read {
	w.wait(st, o)
	return w.b.Forward(ctx, st, o)
}

func (w gated) Backward(ctx context.Context, st kubelet.Log, o kubelet.Options) *kubelet.Read {
	w.wait(st, o)
	return w.b.Backward(ctx, st, o)
}

// A fill that ends early gives back what it holds, so the streams still reading may
// spend it: web-2's one line is read in its first window, and web-1's read begins
// only once the pool holds more than its seed, which web-2's allowance alone adds.
func TestAFillThatEndsEarlyReturnsItsAllowance(t *testing.T) {
	t.Parallel()
	s := newLogServer()
	s.Add("prod/web-1/app", logstest.LineEvery(10)...)
	s.Add("prod/web-2/app", logstest.Stamped(0, "quiet"))
	svc, _ := serviceOver(t, webMirror(t), s)
	svc.caps = smallCaps
	seeded := smallCaps.Query - 2*smallCaps.Stream
	svc.backends = []namedBackend{{"kubelet", func(get kubelet.Getter) kubelet.Backend {
		return gated{kubelet.New(get), func(st kubelet.Log, o kubelet.Options) {
			if st.Key.Pod == "web-1" {
				assert.Eventually(t, func() bool { return o.Limits.Pool.Load() > seeded }, testutil.Timeout, time.Millisecond)
			}
		}}
	}}}

	page, err := svc.Fetch(t.Context(), "7", FetchRequest{Scope: webScope, PageFrom: PageFromLast, PageSize: 100})

	require.NoError(t, err)
	assert.Len(t, page.Records, 11)
}

// fetch is the page req answers on cluster 7.
func fetch(t testing.TB, svc *Service, req FetchRequest) FetchResponse {
	t.Helper()
	resp, err := svc.Fetch(t.Context(), "7", req)
	require.NoError(t, err)
	return resp
}

// goOn is the continuation of c, which must be set, of size lines.
func goOn(t *testing.T, svc *Service, c *string, size int) FetchResponse {
	t.Helper()
	require.NotNil(t, c)
	return fetch(t, svc, FetchRequest{Cursor: c, PageSize: size})
}

// A request is a first page, from PageFromFirst or PageFromLast, or a continuation, a page
// cursor alone or with Seen; Seen goes with a first page with no padding, or with a
// newer cursor whose window has no counted After. Anything else is
// conflicting-parameters (rule 6).
func TestAMixedRequestIsRefused(t *testing.T) {
	t.Parallel()
	svc, _ := serviceOver(t, webMirror(t), newLogServer())
	c := encodePageCursor(openCursor(true))
	older := encodePageCursor(openCursor(false))
	counted := openCursor(true)
	counted.Window.After = Padding{Lines: 3}
	countedAfter := encodePageCursor(counted)
	seen := []string{line("web-1", "app", 0, "").Cursor()}
	for name, req := range map[string]FetchRequest{
		"neither":                           {Scope: webScope},
		"both":                              {Scope: webScope, PageFrom: PageFromFirst, Cursor: &c},
		"another page":                      {Scope: webScope, PageFrom: "middle"},
		"a continuation's scope":            {Scope: webScope, Cursor: &c},
		"a continuation's window":           {Window: Window{Since: moment(0)}, Cursor: &c},
		"seen beside a prev":                {Seen: seen, Cursor: &older},
		"seen beside a count":               {Seen: seen, Cursor: &countedAfter},
		"seen beside padding":               {Scope: webScope, PageFrom: PageFromFirst, Seen: seen, Window: Window{Since: moment(0), After: Padding{Span: time.Second}}},
		"seen from the last beside padding": {Scope: webScope, PageFrom: PageFromLast, Seen: seen, Window: Window{Until: moment(time.Minute), Before: Padding{Lines: 2}}},
	} {
		_, err := svc.Fetch(t.Context(), "7", req)
		assert.Equal(t, refusal.CodeConflictingParameters, codeOf(err), name)
	}
}

// A page's size and byte caps are checked on every page.
func TestABadPageIsRefused(t *testing.T) {
	t.Parallel()
	svc, _ := serviceOver(t, webMirror(t), newLogServer())
	for name, req := range map[string]FetchRequest{
		"a negative size":        {PageSize: -1},
		"too large a size":       {PageSize: MaxPageSize + 1},
		"a negative message cap": {MaxMessageBytes: -1},
		"a negative page cap":    {MaxPageBytes: -1},
	} {
		req.Scope, req.PageFrom = webScope, PageFromFirst
		_, err := svc.Fetch(t.Context(), "7", req)
		assert.Equal(t, refusal.CodeBadPage, codeOf(err), name)
	}
}

// The request is echoed as it ran: defaults filled, caps applied, and on a
// continuation the scope and window the cursor holds, beside the first page's
// clock (rule 13).
func TestTheRequestIsEchoedAsItRan(t *testing.T) {
	t.Parallel()
	s := newLogServer()
	s.Add("prod/web-1/app", logstest.LineEvery(3)...)
	svc, _ := serviceOver(t, webMirror(t), s)
	w := Window{Since: moment(0)}

	first := fetch(t, svc, FetchRequest{Scope: webScope, Window: w, PageFrom: PageFromFirst, PageSize: 1, MaxMessageBytes: 1 << 30})

	assert.Equal(t, FetchRequest{Scope: webScope, Window: w, PageFrom: PageFromFirst, PageSize: 1, MaxMessageBytes: MaxMessageBytesCap, MaxPageBytes: MaxPageBytesCap}, first.Request)
	assert.Equal(t, svc.now(), first.ResolvedAt)
	assert.NotNil(t, first.Notices)

	cont := fetch(t, svc, FetchRequest{Cursor: first.Page.Next, MaxPageBytes: 10})
	assert.Equal(t, FetchRequest{Scope: webScope, Window: w, Cursor: first.Page.Next, PageSize: DefaultPageSize, MaxMessageBytes: MaxMessageBytesCap, MaxPageBytes: 10}, cont.Request)
	assert.Equal(t, first.ResolvedAt, cont.ResolvedAt)
}

// A bound includes every line at its moment, so a range of one nanosecond is the
// lines stamped with it; Since after Until is invalid-range (rule 1).
func TestABoundIncludesItsMoment(t *testing.T) {
	t.Parallel()
	s := newLogServer()
	s.Add("prod/web-1/app", logstest.Stamped(0, "a"), logstest.Stamped(time.Second, "b"), logstest.Stamped(time.Second, "b'"), logstest.Stamped(2*time.Second, "c"))
	svc, _ := serviceOver(t, webMirror(t), s)

	for _, from := range []PageFrom{PageFromFirst, PageFromLast} {
		page := fetch(t, svc, FetchRequest{Scope: webScope, Window: Window{Since: moment(time.Second), Until: moment(time.Second)}, PageFrom: from})
		assert.Equal(t, []string{"b", "b'"}, messages(page.Records), from)
	}
	_, err := svc.Fetch(t.Context(), "7", FetchRequest{Scope: webScope, Window: Window{Since: moment(2 * time.Second), Until: moment(time.Second)}, PageFrom: PageFromFirst})
	assert.Equal(t, refusal.CodeInvalidRange, codeOf(err))
}

// PageFromFirst reads the window's oldest lines, PageFromLast its newest, each answered
// oldest first (rule 5).
func TestTheFirstPageComesFromItsEdge(t *testing.T) {
	t.Parallel()
	s := newLogServer()
	s.Add("prod/web-1/app", logstest.LineEvery(5)...)
	svc, _ := serviceOver(t, webMirror(t), s)

	first := fetch(t, svc, FetchRequest{Scope: webScope, PageFrom: PageFromFirst, PageSize: 2})
	last := fetch(t, svc, FetchRequest{Scope: webScope, PageFrom: PageFromLast, PageSize: 2})

	assert.Equal(t, []string{"0", "1"}, messages(first.Records))
	assert.Equal(t, []string{"3", "4"}, messages(last.Records))
}

// A continuation reads on the way its cursor says, never crossing the window's
// edge; Prev is set past a continuation's start, Next while a line is known to be
// newer, and neither past a closed window's end, where no Follow is set either
// (rules 6 and 7).
func TestPagesGoOnByTheirCursors(t *testing.T) {
	t.Parallel()
	s := newLogServer()
	s.Add("prod/web-1/app", logstest.LineEvery(6)...)
	svc, _ := serviceOver(t, webMirror(t), s)
	w := Window{Since: moment(0), Until: moment(4 * time.Second)}

	p1 := fetch(t, svc, FetchRequest{Scope: webScope, Window: w, PageFrom: PageFromFirst, PageSize: 2})
	assert.Equal(t, []string{"0", "1"}, messages(p1.Records))
	assert.Equal(t, [3]bool{false, true, false}, edgesOf(p1.Page))

	p2 := goOn(t, svc, p1.Page.Next, 2)
	assert.Equal(t, []string{"2", "3"}, messages(p2.Records))
	assert.Equal(t, [3]bool{true, true, false}, edgesOf(p2.Page))

	p3 := goOn(t, svc, p2.Page.Next, 2)
	assert.Equal(t, []string{"4"}, messages(p3.Records), "the window ends at 4s")
	assert.Equal(t, [3]bool{true, false, false}, edgesOf(p3.Page))

	back := goOn(t, svc, p3.Page.Prev, 2)
	assert.Equal(t, []string{"2", "3"}, messages(back.Records))
	back = goOn(t, svc, back.Page.Prev, 2)
	assert.Equal(t, []string{"0", "1"}, messages(back.Records))
	assert.Nil(t, back.Page.Prev, "the window starts at 0s")
}

// edgesOf is which of Prev, Next and Follow page sets.
func edgesOf(page Page) [3]bool {
	return [3]bool{page.Prev != nil, page.Next != nil, page.Follow != nil}
}

// A live window's newest page sets Follow, never beside Next, and a poll from it
// answers what was written since, then hands the poll back.
func TestALiveWindowsNewestPageFollows(t *testing.T) {
	t.Parallel()
	s := newLogServer()
	s.Add("prod/web-1/app", logstest.LineEvery(3)...)
	svc, _ := serviceOver(t, webMirror(t), s)

	p1 := fetch(t, svc, FetchRequest{Scope: webScope, PageFrom: PageFromFirst, PageSize: 2})
	assert.Equal(t, [3]bool{false, true, false}, edgesOf(p1.Page))
	p2 := goOn(t, svc, p1.Page.Next, 2)
	assert.Equal(t, []string{"2"}, messages(p2.Records))
	assert.Equal(t, [3]bool{true, false, true}, edgesOf(p2.Page))

	quiet := goOn(t, svc, p2.Page.Follow, 2)
	assert.Empty(t, quiet.Records)
	assert.Equal(t, p2.Page.Follow, quiet.Page.Follow, "a poll that passed nothing hands its cursor back")
	assert.Nil(t, quiet.Page.Prev)

	s.Add("prod/web-1/app", logstest.Stamped(3*time.Second, "3"))
	poll := goOn(t, svc, quiet.Page.Follow, 2)
	assert.Equal(t, []string{"3"}, messages(poll.Records))
	assert.NotNil(t, poll.Page.Follow)
}

// A Prev chain from the window's end reaches its start, and a Next turning back
// re-delivers what lies past the page.
func TestAPrevChainTurnsBack(t *testing.T) {
	t.Parallel()
	s := newLogServer()
	s.Add("prod/web-1/app", logstest.LineEvery(5)...)
	svc, _ := serviceOver(t, webMirror(t), s)

	last := fetch(t, svc, FetchRequest{Scope: webScope, PageFrom: PageFromLast, PageSize: 2})
	assert.Equal(t, []string{"3", "4"}, messages(last.Records))
	assert.Equal(t, [3]bool{true, false, true}, edgesOf(last.Page), "an open window's last page polls")
	p := goOn(t, svc, last.Page.Prev, 2)
	assert.Equal(t, []string{"1", "2"}, messages(p.Records))
	p = goOn(t, svc, p.Page.Prev, 2)
	assert.Equal(t, []string{"0"}, messages(p.Records))
	assert.Equal(t, [3]bool{false, true, false}, edgesOf(p.Page))

	p = goOn(t, svc, p.Page.Next, 2)
	assert.Equal(t, []string{"1", "2"}, messages(p.Records))
}

// A continuation resolves its cursor's scope again: one the service now refuses
// keeps the scope's code, an entry for a stream that left is dropped, and a stream
// that joined reads from the window's start.
func TestAContinuationResolvesItsScopeAgain(t *testing.T) {
	t.Parallel()
	s := newLogServer()
	s.Add("prod/web-1/app", logstest.LineEvery(5)...)
	f := webMirror(t)
	svc, _ := serviceOver(t, f, s)

	bad := openCursor(true)
	bad.Filters = []ScopeFilter{{Field: "color", Values: []string{"red"}}}
	_, err := svc.Fetch(t.Context(), "7", FetchRequest{Cursor: encoded(bad)})
	assert.Equal(t, refusal.CodeBadScope, codeOf(err))

	page := fetch(t, svc, FetchRequest{Scope: webScope, Window: Window{Since: moment(2 * time.Second)}, PageFrom: PageFromFirst})
	assert.Equal(t, []string{"2", "3", "4"}, messages(page.Records))
	c, err := decodePageCursor(*page.Page.Follow)
	require.NoError(t, err)
	c.seen[name("gone", "app")] = entry("gone", "app", 0)

	s.Add("prod/web-3/app", logstest.Stamped(time.Second, "too old"), logstest.Stamped(3*time.Second, "w3"))
	f.store.Apply(t, pod("web-3", "uid-web-3", "node-a", webReplicaSet(), container{name: "app"}))
	poll := fetch(t, svc, FetchRequest{Cursor: encoded(c)})

	assert.Equal(t, []string{"w3"}, messages(poll.Records), "web-3 reads from the window's start")
	after, err := decodePageCursor(*poll.Page.Follow)
	require.NoError(t, err)
	assert.NotContains(t, after.seen, name("gone", "app"))
	assert.Contains(t, after.seen, name("web-3", "app"))
}

// countedService is a service over web-1's app writing a line a second from 10:00
// to 10:00:19, each saying its second.
func countedService(t *testing.T) *Service {
	t.Helper()
	s := newLogServer()
	s.Add("prod/web-1/app", logstest.LineEvery(20)...)
	svc, _ := serviceOver(t, webMirror(t), s)
	return svc
}

// between is the window from 10:00:10 to 10:00:12, padded as given.
func between(before, after Padding) Window {
	return Window{Since: moment(10 * time.Second), Until: moment(12 * time.Second), Before: before, After: after}
}

// cursorIn is the page cursor c decoded.
func cursorIn(t testing.TB, c *string) pageCursor {
	t.Helper()
	require.NotNil(t, c)
	out, err := decodePageCursor(*c)
	require.NoError(t, err)
	return out
}

// A counted Before finds its lines back from Since and starts the window at the
// farthest, setting no edge past it; with fewer to find, the window starts at the
// log's edge and the page says the padding came up short (rule 2).
func TestACountedBeforeStartsAtItsFarthestLine(t *testing.T) {
	t.Parallel()
	svc := countedService(t)

	page := fetch(t, svc, FetchRequest{Scope: webScope, Window: between(Padding{Lines: 3}, Padding{}), PageFrom: PageFromFirst})
	assert.Equal(t, []string{"7", "8", "9", "10", "11", "12"}, messages(page.Records))
	assert.Equal(t, [3]bool{false, false, false}, edgesOf(page.Page))
	assert.Empty(t, page.Notices)

	page = fetch(t, svc, FetchRequest{Scope: webScope, Window: between(Padding{Lines: 30}, Padding{}), PageFrom: PageFromFirst})
	assert.Equal(t, messages(linesOf(logstest.LineEvery(13))), messages(page.Records))
	assert.Equal(t, []Notice{{refusal.CodePaddingShort, "the padding before since holds 10 of the 30 lines asked for"}}, page.Notices)
}

// The grep searches the range alone, and a count counts every line: the padding
// arrives unfiltered before and after the range, read first or last, across a
// continuation over the bound, and under a span (rule 11).
func TestTheGrepLeavesThePaddingAlone(t *testing.T) {
	t.Parallel()
	svc := countedService(t)
	scope := webScope
	scope.Grep = "^[0-9]*[02468]$"
	counted := between(Padding{Lines: 2}, Padding{Lines: 2})

	page := fetch(t, svc, FetchRequest{Scope: scope, Window: counted, PageFrom: PageFromFirst})
	assert.Equal(t, []string{"8", "9", "10", "12", "13", "14"}, messages(page.Records))
	assert.Equal(t, [3]bool{false, false, false}, edgesOf(page.Page))

	page = fetch(t, svc, FetchRequest{Scope: scope, Window: counted, PageFrom: PageFromFirst, PageSize: 3})
	assert.Equal(t, []string{"8", "9", "10"}, messages(page.Records))
	page = goOn(t, svc, page.Page.Next, 3)
	assert.Equal(t, []string{"12", "13", "14"}, messages(page.Records))
	assert.Nil(t, page.Page.Next)

	page = fetch(t, svc, FetchRequest{Scope: scope, Window: counted, PageFrom: PageFromLast, PageSize: 3})
	assert.Equal(t, []string{"12", "13", "14"}, messages(page.Records))
	page = goOn(t, svc, page.Page.Prev, 3)
	assert.Equal(t, []string{"8", "9", "10"}, messages(page.Records))
	assert.Nil(t, page.Page.Prev)

	spanned := between(Padding{Span: 2 * time.Second}, Padding{Span: 2 * time.Second})
	page = fetch(t, svc, FetchRequest{Scope: scope, Window: spanned, PageFrom: PageFromFirst})
	assert.Equal(t, []string{"8", "9", "10", "12", "13", "14"}, messages(page.Records))
}

// A page smaller than its padding ends inside it, and its Next goes on from the
// page's last line with the count of padding still to come; a counted After read
// from the end mirrors it.
func TestAPageSmallerThanItsPaddingGoesOnInsideIt(t *testing.T) {
	t.Parallel()
	svc := countedService(t)

	page := fetch(t, svc, FetchRequest{Scope: webScope, Window: between(Padding{Lines: 5}, Padding{}), PageFrom: PageFromFirst, PageSize: 3})
	assert.Equal(t, []string{"5", "6", "7"}, messages(page.Records))
	assert.Nil(t, page.Page.Prev)
	assert.Equal(t, 2, cursorIn(t, page.Page.Next).before)
	page = goOn(t, svc, page.Page.Next, 3)
	assert.Equal(t, []string{"8", "9", "10"}, messages(page.Records))
	assert.Equal(t, 0, cursorIn(t, page.Page.Next).before)
	page = goOn(t, svc, page.Page.Next, 3)
	assert.Equal(t, []string{"11", "12"}, messages(page.Records))
	assert.Nil(t, page.Page.Next)

	page = fetch(t, svc, FetchRequest{Scope: webScope, Window: between(Padding{}, Padding{Lines: 5}), PageFrom: PageFromLast, PageSize: 3})
	assert.Equal(t, []string{"15", "16", "17"}, messages(page.Records))
	assert.Nil(t, page.Page.Next)
	assert.Nil(t, page.Page.Follow, "the count is whole")
	assert.Equal(t, 2, cursorIn(t, page.Page.Prev).after)
	page = goOn(t, svc, page.Page.Prev, 3)
	assert.Equal(t, []string{"12", "13", "14"}, messages(page.Records))
}

// A counted After is delivered across Next pages and closes the window once its
// lines are in: the trimmed page sets neither Next nor Follow.
func TestACountedAfterClosesTheWindow(t *testing.T) {
	t.Parallel()
	svc := countedService(t)

	page := fetch(t, svc, FetchRequest{Scope: webScope, Window: between(Padding{}, Padding{Lines: 4}), PageFrom: PageFromFirst, PageSize: 3})
	assert.Equal(t, []string{"10", "11", "12"}, messages(page.Records))
	page = goOn(t, svc, page.Page.Next, 3)
	assert.Equal(t, []string{"13", "14", "15"}, messages(page.Records))
	assert.Equal(t, 3, cursorIn(t, page.Page.Next).after)
	page = goOn(t, svc, page.Page.Next, 3)
	assert.Equal(t, []string{"16"}, messages(page.Records))
	assert.Equal(t, [3]bool{true, false, false}, edgesOf(page.Page))
}

// A Prev chain from the end reads back through a counted Before by its count,
// stopping at its Lines, and a Next turning back re-delivers the padding with the
// count falling to 0.
func TestAPrevChainCountsItsPadding(t *testing.T) {
	t.Parallel()
	svc := countedService(t)

	page := fetch(t, svc, FetchRequest{Scope: webScope, Window: between(Padding{Lines: 4}, Padding{}), PageFrom: PageFromLast, PageSize: 2})
	assert.Equal(t, []string{"11", "12"}, messages(page.Records))
	var got []string
	for page.Page.Prev != nil {
		page = goOn(t, svc, page.Page.Prev, 2)
		got = append(messages(page.Records), got...)
		c := cursorIn(t, page.Page.Next)
		assert.True(t, c.before >= 0 && c.before <= 4, "%d", c.before)
	}
	assert.Equal(t, []string{"6", "7", "8", "9", "10"}, got)

	var counts []int
	for page.Page.Next != nil {
		counts = append(counts, cursorIn(t, page.Page.Next).before)
		page = goOn(t, svc, page.Page.Next, 2)
	}
	assert.Equal(t, []string{"11", "12"}, messages(page.Records))
	assert.Equal(t, []int{3, 1, 0}, counts)
}

// A counted Before that came up short starts the window at the log's edge, and a
// Next back into the range brings the count to 0.
func TestAShortBeforeSetsTheStart(t *testing.T) {
	t.Parallel()
	svc := countedService(t)

	page := fetch(t, svc, FetchRequest{Scope: webScope, Window: Window{Since: moment(2 * time.Second), Until: moment(3 * time.Second), Before: Padding{Lines: 5}}, PageFrom: PageFromFirst, PageSize: 2})

	assert.Equal(t, []string{"0", "1"}, messages(page.Records))
	c := cursorIn(t, page.Page.Next)
	assert.Equal(t, lineAt(logline.StreamKey{Namespace: "prod", Pod: "web-1", Container: "app"}, 0, 0), c.Start)
	assert.Equal(t, 0, c.before)
	page = goOn(t, svc, page.Page.Next, 2)
	assert.Equal(t, []string{"2", "3"}, messages(page.Records))
}

// A stream with no entry reads from the window's start on a Next, under a counted
// Before from the line the first page's read found, never older.
func TestAStreamWithNoEntryReadsFromTheStart(t *testing.T) {
	t.Parallel()
	s := newLogServer()
	s.Add("prod/web-1/app", logstest.LineEvery(20)...)
	s.Add("prod/web-2/app", logstest.Stamped(5*time.Second, "w2 5"), logstest.Stamped(8*time.Second, "w2 8"), logstest.Stamped(11*time.Second, "w2 11"))
	svc, _ := serviceOver(t, webMirror(t), s)

	page := fetch(t, svc, FetchRequest{Scope: webScope, Window: between(Padding{Lines: 3}, Padding{}), PageFrom: PageFromFirst, PageSize: 1})
	assert.Equal(t, []string{"8"}, messages(page.Records), "the start is 3 lines back: 8, w2 8 and 9")
	var got []string
	for page.Page.Next != nil {
		page = goOn(t, svc, page.Page.Next, 1)
		got = append(got, messages(page.Records)...)
	}
	assert.Equal(t, []string{"w2 8", "9", "10", "11", "w2 11", "12"}, got)
}

// A first page whose padding read stopped at a loud second starts at the stop:
// its Prev reads the rest of the padding from there, and its Next goes on from
// its last line.
func TestAPaddingReadStoppedAtALoudSecond(t *testing.T) {
	t.Parallel()
	svc, _ := loudAround(t)

	page := fetch(t, svc, FetchRequest{Scope: webScope, Window: Window{Since: moment(9 * time.Second), Before: Padding{Lines: 100}}, PageFrom: PageFromFirst})

	assert.Equal(t, []string{"web-2 5", "web-1 6", "web-2 6", "web-1 7", "web-2 7", "web-1 8", "web-2 8", "web-1 9", "web-2 9"}, messages(page.Records))
	assert.Contains(t, page.Notices, Notice{refusal.CodeStreamSkipped, "web-1/app: " + kubelet.LoudAt(at(5*time.Second)).Error()})
	prev := cursorIn(t, page.Page.Prev)
	assert.Equal(t, logline.MomentAt(at(5*time.Second)), prev.at)
	assert.Equal(t, 7, prev.before)
	older := goOn(t, svc, page.Page.Prev, 100)
	assert.Equal(t, []string{"web-1 0", "web-2 0", "web-1 1", "web-2 1", "web-1 2", "web-2 2", "web-1 3", "web-2 3", "web-1 4", "web-2 4"}, messages(older.Records))
}

// A last page whose padding read past Until stopped at a loud second has its Next
// read on from the stop, the rest of the second left out as a forward read leaves
// it, with the lines found as its count; no line comes twice.
func TestAnAfterReadStoppedAtALoudSecond(t *testing.T) {
	t.Parallel()
	svc, _ := loudAround(t)

	page := fetch(t, svc, FetchRequest{Scope: webScope, Window: Window{Since: moment(2 * time.Second), Until: moment(3 * time.Second), After: Padding{Lines: 100}}, PageFrom: PageFromLast})

	assert.Equal(t, []string{"web-1 2", "web-2 2", "web-1 3"}, messages(page.Records[:3]))
	assert.Equal(t, "web-2 5", page.Records[len(page.Records)-1].Message)
	assert.Contains(t, page.Notices, Notice{refusal.CodeStreamSkipped, "web-1/app: " + kubelet.LoudAt(at(5*time.Second)).Error()})
	assert.Nil(t, page.Page.Follow)
	c := cursorIn(t, page.Page.Next)
	assert.Equal(t, logline.MomentAt(at(6*time.Second-1)), c.at)
	assert.Equal(t, len(page.Records)-3, c.after, "the padding lines found")
	next := goOn(t, svc, page.Page.Next, 100)
	assert.Equal(t, []string{"web-1 6", "web-2 6", "web-1 7", "web-2 7", "web-1 8", "web-2 8", "web-1 9", "web-2 9"}, messages(next.Records))
	assert.True(t, ordered(append(slices.Clone(page.Records), next.Records...)))
}

// A padding read with a stream skipped comes up short as the log's edge would,
// and the page names the stream beside saying so.
func TestAPaddingReadWithASkippedStream(t *testing.T) {
	t.Parallel()
	s := newLogServer()
	s.Add("prod/web-2/app", logstest.LineEvery(6)...)
	s.SetRefusal("prod/web-1/app", &logstest.Denial{Code: 404, Message: `pods "web-1" not found`})
	svc, _ := serviceOver(t, webMirror(t), s)

	page := fetch(t, svc, FetchRequest{Scope: webScope, Window: Window{Since: moment(4 * time.Second), Until: moment(5 * time.Second), Before: Padding{Lines: 10}}, PageFrom: PageFromFirst})

	assert.Equal(t, messages(linesOf(logstest.LineEvery(6))), messages(page.Records))
	assert.ElementsMatch(t, []refusal.Code{refusal.CodePaddingShort, refusal.CodeStreamSkipped}, codesOf(page.Notices))
}

// A padding read cut short by a stream skipped on a transient failure has not
// reached the log's edge: the window's start stays to be found, and Prev reads on
// from where the read stopped, finding the skipped stream's lines once it answers.
func TestAPaddingReadCutByATransientFailureGoesOn(t *testing.T) {
	t.Parallel()
	s := newLogServer()
	s.Add("prod/web-1/app", logstest.Stamped(0, "a0"))
	s.Add("prod/web-2/app", logstest.LineEvery(6)...)
	s.SetRefusal("prod/web-1/app", &logstest.Denial{Code: 503, Message: "unavailable"})
	svc, _ := serviceOver(t, webMirror(t), s)

	page := fetch(t, svc, FetchRequest{Scope: webScope, Window: Window{Since: moment(4 * time.Second), Until: moment(5 * time.Second), Before: Padding{Lines: 10}}, PageFrom: PageFromFirst})

	assert.Equal(t, messages(linesOf(logstest.LineEvery(6))), messages(page.Records))
	assert.Equal(t, []refusal.Code{refusal.CodeStreamSkipped}, codesOf(page.Notices), "the log's edge is not reached")
	require.NotNil(t, page.Page.Prev)
	assert.True(t, cursorIn(t, page.Page.Prev).Start.IsZero(), "the window's start is still to be found")

	s.SetRefusal("prod/web-1/app", nil)
	prev := goOn(t, svc, page.Page.Prev, 10)
	assert.Equal(t, []string{"a0"}, messages(prev.Records))
}

// codesOf is each notice's code.
func codesOf(notices []Notice) []refusal.Code {
	out := make([]refusal.Code, len(notices))
	for i, n := range notices {
		out[i] = n.Code
	}
	return out
}

// Padding past an open bound is measured from the log's edge: none before an open
// Since; past an open Until, from the first page's clock, and a first page reaching
// the window's end with no line at or past it says the padding is empty. Only the
// first page says so (rule 3).
func TestPaddingPastAnOpenBound(t *testing.T) {
	t.Parallel()
	s := newLogServer()
	s.Add("prod/web-1/app", logstest.LineEvery(3)...)
	svc, _ := serviceOver(t, webMirror(t), s)

	page := fetch(t, svc, FetchRequest{Scope: webScope, Window: Window{Until: moment(time.Second), Before: Padding{Lines: 5}}, PageFrom: PageFromFirst})
	assert.Equal(t, []string{"0", "1"}, messages(page.Records))
	assert.Equal(t, []refusal.Code{refusal.CodeEmptyPadding}, codesOf(page.Notices))
	page = fetch(t, svc, FetchRequest{Scope: webScope, Window: Window{Until: moment(time.Second), Before: Padding{Lines: 5}}, PageFrom: PageFromFirst, PageSize: 1})
	assert.Equal(t, []refusal.Code{refusal.CodeEmptyPadding}, codesOf(page.Notices))
	page = goOn(t, svc, page.Page.Next, 1)
	assert.Equal(t, []string{"1"}, messages(page.Records))
	assert.Empty(t, page.Notices, "a continuation does not repeat it")

	page = fetch(t, svc, FetchRequest{Scope: webScope, Window: Window{After: Padding{Span: time.Minute}}, PageFrom: PageFromLast})
	assert.Equal(t, []string{"0", "1", "2"}, messages(page.Records))
	assert.Equal(t, []refusal.Code{refusal.CodeEmptyPadding}, codesOf(page.Notices))
	assert.Nil(t, page.Request.Until, "the echoed until stays open")
	assert.NotNil(t, page.Page.Follow, "the window ends a minute past now")

	s.Add("prod/web-1/app", logstest.Stamped(time.Hour, "late"))
	page = fetch(t, svc, FetchRequest{Scope: webScope, Window: Window{After: Padding{Lines: 2}}, PageFrom: PageFromFirst})
	assert.Equal(t, []string{"0", "1", "2", "late"}, messages(page.Records))
	assert.Empty(t, page.Notices)
	assert.Equal(t, 1, cursorIn(t, page.Page.Follow).after, "a count short of its lines leaves the window live")
	page = goOn(t, svc, page.Page.Follow, 100)
	assert.Empty(t, page.Records)
	assert.Empty(t, page.Notices, "a poll with nothing new says nothing of lines already delivered")
}

// A record's place in the window is its timestamp against the echoed bounds:
// padding by span reaches the lines that far past each bound (rule 4).
func TestARecordsPlaceIsItsTimestamp(t *testing.T) {
	t.Parallel()
	svc := countedService(t)

	page := fetch(t, svc, FetchRequest{Scope: webScope, Window: between(Padding{Span: 2 * time.Second}, Padding{Span: time.Second}), PageFrom: PageFromFirst})

	assert.Equal(t, []string{"8", "9", "10", "11", "12", "13"}, messages(page.Records))
	var padding []string
	for _, rec := range page.Records {
		if rec.Timestamp.Before(*page.Request.Since) || rec.Timestamp.After(*page.Request.Until) {
			padding = append(padding, rec.Message)
		}
	}
	assert.Equal(t, []string{"8", "9", "13"}, padding)
}

// A stream the page could not read is a stream-skipped notice of its own; with
// every stream failed and nothing read, the page is refused as read-failed.
func TestASkippedStreamIsANotice(t *testing.T) {
	t.Parallel()
	s := newLogServer()
	s.Add("prod/web-2/app", logstest.LineEvery(2)...)
	s.SetRefusal("prod/web-1/app", &logstest.Denial{Code: 404, Message: `pods "web-1" not found`})
	svc, _ := serviceOver(t, webMirror(t), s)

	page := fetch(t, svc, FetchRequest{Scope: webScope, PageFrom: PageFromFirst})
	assert.Equal(t, []Notice{{refusal.CodeStreamSkipped, `web-1/app: pods "web-1" not found`}}, page.Notices)

	s.SetRefusal("prod/web-2/app", &logstest.Denial{Code: 404, Message: `pods "web-2" not found`})
	_, err := svc.Fetch(t.Context(), "7", FetchRequest{Scope: webScope, Window: Window{Since: moment(time.Minute)}, PageFrom: PageFromFirst})
	assert.Equal(t, refusal.CodeReadFailed, codeOf(err))
}

// A first page's Seen reads the named stream past its line, and every other
// stream, and a stream whose line is older than the window's start, from the
// start.
func TestSeenReadsEachStreamPastItsLine(t *testing.T) {
	t.Parallel()
	s := newLogServer()
	s.Add("prod/web-1/app", logstest.Stamped(time.Second, "a1"), logstest.Stamped(3*time.Second, "a3"), logstest.Stamped(5*time.Second, "a5"))
	s.Add("prod/web-2/app", logstest.Stamped(time.Second, "b1"), logstest.Stamped(3*time.Second, "b3"), logstest.Stamped(5*time.Second, "b5"))
	svc, _ := serviceOver(t, webMirror(t), s)
	w := Window{Since: moment(2 * time.Second)}

	page := fetch(t, svc, FetchRequest{Scope: webScope, Window: w, PageFrom: PageFromFirst, Seen: []string{line("web-1", "app", 3*time.Second, "").Cursor()}})
	assert.Equal(t, []string{"b3", "a5", "b5"}, messages(page.Records))

	page = fetch(t, svc, FetchRequest{Scope: webScope, Window: w, PageFrom: PageFromFirst, Seen: []string{line("web-1", "app", time.Second, "").Cursor()}})
	assert.Equal(t, []string{"a3", "b3", "a5", "b5"}, messages(page.Records), "a line older than the start reads from the start")

	_, err := svc.Fetch(t.Context(), "7", FetchRequest{Scope: webScope, PageFrom: PageFromFirst, Seen: []string{logline.Encode(logline.MomentAt(at(0)))}})
	assert.Equal(t, refusal.CodeCursorInvalid, codeOf(err), "a moment")
}

// A first page from the last with Seen reads the named stream from strictly
// before its line, and every other stream from Until, inclusive.
func TestAFirstPageFromLastReadsBeforeEachEntry(t *testing.T) {
	t.Parallel()
	s := newLogServer()
	s.Add("prod/web-1/app", logstest.Stamped(time.Second, "a1"), logstest.Stamped(3*time.Second, "a3"), logstest.Stamped(5*time.Second, "a5"))
	s.Add("prod/web-2/app", logstest.Stamped(time.Second, "b1"), logstest.Stamped(3*time.Second, "b3"), logstest.Stamped(5*time.Second, "b5"))
	svc, _ := serviceOver(t, webMirror(t), s)

	page := fetch(t, svc, FetchRequest{Scope: webScope, Window: Window{Until: moment(5 * time.Second)}, PageFrom: PageFromLast, PageSize: 10, Seen: seenAt("web-1", "app", 3*time.Second)})

	assert.Equal(t, []string{"a1", "b1", "b3", "b5"}, messages(page.Records))
}

// Of two entries for one stream, the older stands on a first page from the last,
// as the newer does from the first.
func TestTwoEntriesForOneStreamKeepTheFartherOne(t *testing.T) {
	t.Parallel()
	s := newLogServer()
	s.Add("prod/web-1/app", logstest.Stamped(time.Second, "a1"), logstest.Stamped(3*time.Second, "a3"), logstest.Stamped(5*time.Second, "a5"))
	svc, _ := serviceOver(t, webMirror(t), s)
	both := []string{line("web-1", "app", 5*time.Second, "").Cursor(), line("web-1", "app", 3*time.Second, "").Cursor()}

	older := fetch(t, svc, FetchRequest{Scope: web1Scope, Window: Window{Until: moment(5 * time.Second)}, PageFrom: PageFromLast, Seen: both})
	assert.Equal(t, []string{"a1"}, messages(older.Records))

	newer := fetch(t, svc, FetchRequest{Scope: web1Scope, Window: Window{Since: moment(0)}, PageFrom: PageFromFirst, Seen: both})
	assert.Empty(t, messages(newer.Records))
}

// An older page cursor carries its entries back, so a stream whose clock runs
// behind stays before its entry on every page: a4 is newer than web-1's entry,
// though stamped under the other stream's lines.
func TestAnOlderCursorCarriesItsEntries(t *testing.T) {
	t.Parallel()
	s := newLogServer()
	s.Add("prod/web-1/app", logstest.Stamped(time.Second, "a1"), logstest.Stamped(2*time.Second, "a2"), logstest.Stamped(3*time.Second, "a3"), logstest.Stamped(4*time.Second, "a4"))
	s.Add("prod/web-2/app", logstest.Stamped(5*time.Second, "b5"), logstest.Stamped(6*time.Second, "b6"), logstest.Stamped(7*time.Second, "b7"))
	svc, _ := serviceOver(t, webMirror(t), s)

	page := fetch(t, svc, FetchRequest{Scope: webScope, Window: Window{Until: moment(7 * time.Second)}, PageFrom: PageFromLast, PageSize: 1, Seen: seenAt("web-1", "app", 3*time.Second)})
	got := messages(page.Records)
	for page.Page.Prev != nil {
		c, err := decodePageCursor(*page.Page.Prev)
		require.NoError(t, err)
		assert.Contains(t, c.seen, name("web-1", "app"), "the prev of %v carries web-1's entry", messages(page.Records))
		page = fetch(t, svc, FetchRequest{Cursor: page.Page.Prev, PageSize: 1})
		got = append(messages(page.Records), got...)
	}

	assert.Equal(t, []string{"a1", "a2", "b5", "b6", "b7"}, got)
}

// A workload's page reads back the lines of a pod deleted since the caller was
// shown them, once, though its entries and its chain both name it.
func TestAWorkloadReadsBackAGonePodsLines(t *testing.T) {
	t.Parallel()
	s := newLogServer()
	s.Add("prod/web-1/app", logstest.Stamped(time.Second, "a1"), logstest.Stamped(3*time.Second, "a3"))
	s.Add("prod/web-2/app", logstest.Stamped(2*time.Second, "b2"), logstest.Stamped(4*time.Second, "b4"))
	svc, f := serviceOver(t, webMirror(t), s)
	page := fetch(t, svc, FetchRequest{Scope: webScope, PageFrom: PageFromFirst, PageSize: 10})
	require.Equal(t, []string{"a1", "b2", "a3", "b4"}, messages(page.Records))

	f.store.Delete(t, web1())
	seen := append(seenAt("web-1", "app", 3*time.Second), seenAt("web-2", "app", 4*time.Second)...)
	page = fetch(t, svc, FetchRequest{Scope: webScope, Window: Window{Until: moment(3 * time.Second)}, PageFrom: PageFromLast, PageSize: 1, Seen: seen})
	got := messages(page.Records)
	for page.Page.Prev != nil {
		page = fetch(t, svc, FetchRequest{Cursor: page.Page.Prev, PageSize: 1})
		got = append(messages(page.Records), got...)
	}

	assert.Equal(t, []string{"a1", "b2"}, got)
	assert.True(t, page.Records[0].Stream.Previous, "a gone pod runs no instance")
}

// A pod the mirror holds is never read as gone, whatever the view resolves of it: a
// previous view of a Deployment whose pods never restarted reads nothing from the
// file, by its chain or by an entry, and once a pod restarted, its last instance from
// the kubelet alone.
func TestALivePodIsNeverReadAsGone(t *testing.T) {
	t.Parallel()
	s := newLogServer()
	s.Add("prod/web-1/app", logstest.Stamped(time.Second, "a1"))
	s.Add("prod/web-2/app", logstest.Stamped(2*time.Second, "b2"))
	svc, f := serviceOver(t, webMirror(t), s)
	require.Equal(t, []string{"a1", "b2"}, messages(fetch(t, svc, FetchRequest{Scope: webScope, PageFrom: PageFromFirst}).Records))
	previous := Scope{Sources: []ScopeSource{{Namespace: "prod", Kind: SourceDeployment, Name: "web", Containers: []string{"app"}, Previous: true}}}

	assert.Empty(t, fetch(t, svc, FetchRequest{Scope: previous, PageFrom: PageFromFirst}).Records)
	seen := seenAt("web-1", "app", time.Second)
	assert.Empty(t, fetch(t, svc, FetchRequest{Scope: previous, Window: Window{Until: moment(5 * time.Second)}, PageFrom: PageFromLast, Seen: seen}).Records, "an entry names a live pod")

	f.store.Apply(t, webPod("web-1", "node-a", container{name: "app", restarted: true}))
	s.Add("prod/web-1/app/previous", logstest.Stamped(time.Second, "a1"))
	page := fetch(t, svc, FetchRequest{Scope: previous, PageFrom: PageFromFirst})
	assert.Equal(t, []string{"a1"}, messages(page.Records))
	assert.Equal(t, []string{containerID("uid-web-1", "app", 0)}, idsOf(page.Records))
}

// An entry positions a stream and adds none: one naming another workload's gone pod
// in the scope's namespace brings nothing of it into the view.
func TestAnEntryAddsNoStream(t *testing.T) {
	t.Parallel()
	s := newLogServer()
	s.Add("prod/web-1/app", logstest.Stamped(time.Second, "a1"))
	s.Add("prod/api-1/app", logstest.Stamped(2*time.Second, "x2"))
	f := webMirror(t)
	f.store.Apply(t, workload("apps/v1", "Deployment", "api", "dep-api", nil))
	f.store.Apply(t, workload("apps/v1", "ReplicaSet", "api-5c", "rs-api", controller("apps/v1", "Deployment", "api", "dep-api")))
	api1 := pod("api-1", "uid-api-1", "node-a", controller("apps/v1", "ReplicaSet", "api-5c", "rs-api"), container{name: "app"})
	f.store.Apply(t, api1)
	svc, _ := serviceOver(t, f, s)
	apiScope := Scope{Sources: []ScopeSource{{Namespace: "prod", Kind: SourceDeployment, Name: "api", Containers: []string{"app"}}}}
	require.Equal(t, []string{"x2"}, messages(fetch(t, svc, FetchRequest{Scope: apiScope, PageFrom: PageFromFirst}).Records))
	f.store.Delete(t, api1)

	page := fetch(t, svc, FetchRequest{Scope: webScope, Window: Window{Until: moment(5 * time.Second)}, PageFrom: PageFromLast, Seen: seenAt("api-1", "app", 2*time.Second)})

	assert.Equal(t, []string{"a1"}, messages(page.Records))
}

// A pod a workload orphaned stays in the mirror, so it is not gone though its chain
// still names it: a view of the workload reads nothing of it from the file while it
// lives, and its lines once it is deleted.
func TestAPodAWorkloadOrphanedIsNotGone(t *testing.T) {
	t.Parallel()
	s := newLogServer()
	s.Add("prod/web-1/app", logstest.Stamped(time.Second, "a1"))
	s.Add("prod/web-2/app", logstest.Stamped(2*time.Second, "b2"))
	svc, f := serviceOver(t, webMirror(t), s)
	require.Equal(t, []string{"a1", "b2"}, messages(fetch(t, svc, FetchRequest{Scope: webScope, PageFrom: PageFromFirst}).Records))
	orphan := pod("web-1", "uid-web-1", "node-a", nil, container{name: "app"})
	f.store.Apply(t, orphan)

	assert.Equal(t, []string{"b2"}, messages(fetch(t, svc, FetchRequest{Scope: webScope, PageFrom: PageFromFirst}).Records))
	previous := Scope{Sources: []ScopeSource{{Namespace: "prod", Kind: SourceDeployment, Name: "web", Containers: []string{"app"}, Previous: true}}}
	assert.Empty(t, fetch(t, svc, FetchRequest{Scope: previous, PageFrom: PageFromFirst}).Records)

	f.store.Delete(t, orphan)
	assert.Equal(t, []string{"a1", "b2"}, messages(fetch(t, svc, FetchRequest{Scope: webScope, PageFrom: PageFromFirst}).Records))
}

// previousOf is whether each record of every page is a previous instance's, oldest
// first: pages read the way from names.
func previousOf(pages []FetchResponse, from PageFrom) []bool {
	out := []bool{}
	for _, page := range pages {
		var prev []bool
		for _, rec := range page.Records {
			prev = append(prev, rec.Stream.Previous)
		}
		if from == PageFromFirst {
			out = append(out, prev...)
		} else {
			out = append(prev, out...)
		}
	}
	return out
}

// A workload reads the pods it no longer runs from the file, found by the chain filed
// with their streams: a landing that names nothing reads a replaced pod's lines,
// marked previous, from either end and across page boundaries.
func TestAWorkloadReadsItsReplacedPodsFromTheFile(t *testing.T) {
	t.Parallel()
	s := newLogServer()
	s.Add("prod/web-1/app", logstest.Stamped(time.Second, "a1"), logstest.Stamped(3*time.Second, "a3"))
	s.Add("prod/web-2/app", logstest.Stamped(2*time.Second, "b2"), logstest.Stamped(4*time.Second, "b4"))
	svc, f := serviceOver(t, webMirror(t), s)
	require.Equal(t, []string{"a1", "b2", "a3", "b4"}, messages(fetch(t, svc, FetchRequest{Scope: webScope, PageFrom: PageFromFirst, PageSize: 10}).Records))

	f.store.Delete(t, web1())

	for _, from := range []PageFrom{PageFromFirst, PageFromLast} {
		read := pages(t, svc, webScope, from, 1)
		assert.Equal(t, []string{"a1", "b2", "a3", "b4"}, recordsOf(read, from), from)
		assert.Equal(t, []bool{true, false, true, false}, previousOf(read, from), from)
	}
}

// The chain is by UID: a Deployment deleted and made again under its name owns only
// the pods it ran since, so it reads none of the old one's.
func TestAWorkloadRecreatedUnderItsNameReadsNoneOfTheOldOnesPods(t *testing.T) {
	t.Parallel()
	s := newLogServer()
	s.Add("prod/web-1/app", logstest.Stamped(time.Second, "a1"))
	s.Add("prod/web-2/app", logstest.Stamped(2*time.Second, "b2"))
	s.Add("prod/web-3/app", logstest.Stamped(3*time.Second, "c3"))
	svc, f := serviceOver(t, webMirror(t), s)
	require.Equal(t, []string{"a1", "b2"}, messages(fetch(t, svc, FetchRequest{Scope: webScope, PageFrom: PageFromFirst, PageSize: 10}).Records))

	for _, body := range webObjects()[:4] {
		f.store.Delete(t, body)
	}
	f.store.Apply(t, workload("apps/v1", "Deployment", "web", "dep-web-2", nil))
	f.store.Apply(t, workload("apps/v1", "ReplicaSet", "web-5b2c", "rs-web-2", controller("apps/v1", "Deployment", "web", "dep-web-2")))
	f.store.Apply(t, pod("web-3", "uid-web-3", "node-a", controller("apps/v1", "ReplicaSet", "web-5b2c", "rs-web-2"), container{name: "app"}))

	page := fetch(t, svc, FetchRequest{Scope: webScope, PageFrom: PageFromFirst, PageSize: 10})
	assert.Equal(t, []string{"c3"}, messages(page.Records))
}

// A pod the mirror showed before its ReplicaSet is filed with the ReplicaSet alone;
// the next page that reads it, once the ReplicaSet arrived, files the Deployment, and
// the Deployment reads the pod once it is gone. One gone before that is found by its
// ReplicaSet alone.
func TestAPodsChainCompletesOnTheNextPageThatReadsIt(t *testing.T) {
	t.Parallel()
	s := newLogServer()
	s.Add("prod/web-1/app", logstest.Stamped(time.Second, "a1"))
	s.Add("prod/web-2/app", logstest.Stamped(2*time.Second, "b2"))
	objects := webObjects()
	replicaSet := objects[1]
	svc, f := serviceOver(t, newMirror(t, objects[0], objects[2], objects[3]), s)
	podScope := func(name string) Scope {
		return Scope{Sources: []ScopeSource{{Namespace: "prod", Kind: SourcePod, Name: name, Containers: []string{"app"}}}}
	}
	rsScope := Scope{Sources: []ScopeSource{{Namespace: "prod", Kind: SourceReplicaSet, Name: "web-7d9f", Containers: []string{"app"}}}}
	for _, name := range []string{"web-1", "web-2"} {
		require.Len(t, fetch(t, svc, FetchRequest{Scope: podScope(name), PageFrom: PageFromFirst}).Records, 1)
	}
	f.store.Apply(t, replicaSet)
	require.Len(t, fetch(t, svc, FetchRequest{Scope: podScope("web-1"), PageFrom: PageFromFirst}).Records, 1)

	f.store.Delete(t, objects[2])
	f.store.Delete(t, objects[3])

	assert.Equal(t, []string{"a1"}, messages(fetch(t, svc, FetchRequest{Scope: webScope, PageFrom: PageFromFirst}).Records))
	assert.Equal(t, []string{"a1", "b2"}, messages(fetch(t, svc, FetchRequest{Scope: rsScope, PageFrom: PageFromFirst}).Records))
}

// A StatefulSet's pod replaced under its name runs on another node with another
// default container, and a scope naming the running pod as well still reads the gone
// pod's rows by what was filed with them: its node against the filter, its default.
func TestAGonePodKeepsItsFiledPlaceUnderARunningPodOfItsName(t *testing.T) {
	t.Parallel()
	s := newLogServer()
	s.Add("prod/db-0/app", logstest.Stamped(time.Second, "d1"))
	statefulSet := workload("apps/v1", "StatefulSet", "db", "sts-db", nil)
	owner := controller("apps/v1", "StatefulSet", "db", "sts-db")
	old := pod("db-0", "uid-db-0", "node-a", owner, container{name: "app"}, container{name: "sidecar"})
	svc, f := serviceOver(t, newMirror(t, append(webNodes(), statefulSet, old)...), s)
	dbScope := ScopeSource{Namespace: "prod", Kind: SourceStatefulSet, Name: "db"}
	require.Equal(t, []string{"d1"}, messages(fetch(t, svc, FetchRequest{Scope: Scope{Sources: []ScopeSource{dbScope}}, PageFrom: PageFromFirst}).Records))

	f.store.Delete(t, old)
	replaced := pod("db-0", "uid-db-0-b", "node-b", owner, container{name: "app"}, container{name: "sidecar"})
	replaced["metadata"].(map[string]any)["annotations"] = map[string]any{DefaultContainerAnnotation: "sidecar"}
	f.store.Apply(t, replaced)
	scope := Scope{
		Sources: []ScopeSource{{Namespace: "prod", Kind: SourcePod, Name: "db-0"}, dbScope},
		Filters: []ScopeFilter{{Field: FilterNode, Values: []string{"node-a"}}},
	}

	page := fetch(t, svc, FetchRequest{Scope: scope, PageFrom: PageFromFirst})

	require.Equal(t, []string{"d1"}, messages(page.Records))
	assert.Equal(t, logline.Place{Node: "node-a", Region: "eu", Zone: "eu-1a", OS: "linux", Arch: "amd64"}, page.Records[0].Place)
}

// goneWeb is a service over web whose replica set ran a pod for each entry of gone
// besides web-1 and web-2, its app's lines stamped at those seconds, each named
// "<pod>@<second>"; web-1 logs at live. The gone pods run a batch at a time, under
// MaxStreams, each batch filed by a page and then deleted from the mirror. The
// answer is every line, oldest first.
func goneWeb(t testing.TB, live []int, gone map[string][]int) (*Service, *fakeService, []string) {
	t.Helper()
	s := newLogServer()
	type stamped struct {
		s   int
		msg string
	}
	var all []stamped
	add := func(pod string, secs []int) {
		var lines []string
		for _, sec := range secs {
			msg := fmt.Sprintf("%s@%d", pod, sec)
			lines = append(lines, logstest.Stamped(time.Duration(sec)*time.Second, msg))
			all = append(all, stamped{sec, msg})
		}
		s.Add("prod/"+pod+"/app", lines...)
	}
	add("web-1", live)
	svc, f := serviceOver(t, webMirror(t), s)
	for batch := range slices.Chunk(slices.Sorted(maps.Keys(gone)), MaxStreams/2) {
		for _, name := range batch {
			add(name, gone[name])
			f.store.Apply(t, webPod(name, "node-a", container{name: "app"}))
		}
		fetch(t, svc, FetchRequest{Scope: webScope, PageFrom: PageFromFirst, PageSize: MaxPageSize})
		for _, name := range batch {
			f.store.Delete(t, webPod(name, "node-a", container{name: "app"}))
		}
	}
	slices.SortFunc(all, func(a, b stamped) int { return cmp.Compare(a.s, b.s) })
	want := make([]string, len(all))
	for i, l := range all {
		want[i] = l.msg
	}
	return svc, f, want
}

// A workload's gone pods page as its live ones do, whatever the page's size: no line
// repeated or skipped either way, with more gone pods than a page holds lines of and
// one whose lines begin pages past the first.
func TestAWorkloadsGonePodsPageWithoutLossOrRepeat(t *testing.T) {
	t.Parallel()
	svc, _, want := goneWeb(t, []int{1, 15, 29, 43, 57}, map[string][]int{
		"g-0": {3, 6}, "g-1": {13, 16}, "g-2": {23, 26}, "g-3": {33, 36}, "g-4": {44, 46}, "g-late": {50, 52},
	})

	for _, size := range []int{1, 2, 3} {
		for _, from := range []PageFrom{PageFromFirst, PageFromLast} {
			assert.Equal(t, want, recordsOf(pages(t, svc, webScope, from, size), from), "%s by %d", from, size)
		}
	}
}

// Paging back over a Deployment that replaced a few hundred pods, each with lines in
// the file: every page takes every gone pod with a line before its start, so this is
// the cost of a one-sided bound, per page.
func BenchmarkPagingBackOverGonePods(b *testing.B) {
	gone := map[string][]int{}
	for i := range 300 {
		gone[fmt.Sprintf("g-%03d", i)] = []int{10 * i, 10*i + 5}
	}
	svc, _, want := goneWeb(b, []int{1}, gone)
	read := 0
	for b.Loop() {
		read += len(pages(b, svc, webScope, PageFromLast, DefaultPageSize))
	}
	b.ReportMetric(float64(b.Elapsed().Microseconds())/float64(read), "µs/page")
	b.ReportMetric(float64(len(want)), "lines")
}

// readsOf is what a first page of req reads: its range read, and beside it the
// padding read when its own edge is counted.
func readsOf(t *testing.T, req FetchRequest) []pageQuery {
	t.Helper()
	c, err := firstPage(req, at(time.Hour))
	require.NoError(t, err)
	reads := []pageQuery{{scope: req.Scope, mode: c.dir(), cur: &c}}
	if l, _ := c.lookup(); l != nil {
		reads = append(reads, pageQuery{scope: req.Scope, mode: l.dir(), cur: l})
	}
	return reads
}

// onlyPods is the pod of each stream fileOnlyStreams answers for scope over f and
// svc's file, read by reads, sorted.
func onlyPods(t *testing.T, svc *Service, f *fakeService, scope Scope, reads []pageQuery) []string {
	t.Helper()
	tg, err := resolveOf(t, f, scope)
	require.NoError(t, err)
	file, err := svc.files.OpenOrCreate(int64(tg.cacheID))
	require.NoError(t, err)
	defer file.Close()
	only, release, err := svc.fileOnlyStreams(t.Context(), file, tg, reads)
	defer release()
	require.NoError(t, err)
	out := []string{}
	for _, ps := range only {
		out = append(out, ps.row.Pod)
	}
	slices.Sort(out)
	return out
}

// A gone pod is read only by a read it can deliver a line to: one with a line on the
// read's side of where its own read starts and short of the read's far edge, the
// padding the read owes included. A first page whose padding is counted reads both
// ways from its moment, and keeps a pod either way.
func TestAGonePodIsReadOnlyWhereItHasALineToGive(t *testing.T) {
	t.Parallel()
	svc, f, _ := goneWeb(t, []int{1, 60}, map[string][]int{"g-early": {10, 12}, "g-late": {30, 32}})
	both := []string{"g-early", "g-late"}
	for name, c := range map[string]struct {
		req  FetchRequest
		want []string
	}{
		"a landing":                 {FetchRequest{PageFrom: PageFromLast}, both},
		"older, from before a pod":  {FetchRequest{Window: Window{Until: moment(20 * time.Second)}, PageFrom: PageFromLast}, []string{"g-early"}},
		"older, a pod past the far": {FetchRequest{Window: Window{Since: moment(20 * time.Second), Until: moment(40 * time.Second)}, PageFrom: PageFromLast}, []string{"g-late"}},
		"older, padding owed":       {FetchRequest{Window: Window{Since: moment(20 * time.Second), Until: moment(40 * time.Second), Before: Padding{Span: 15 * time.Second}}, PageFrom: PageFromLast}, both},
		"newer, from past a pod":    {FetchRequest{Window: Window{Since: moment(20 * time.Second)}, PageFrom: PageFromFirst}, []string{"g-late"}},
		"newer, a pod past the far": {FetchRequest{Window: Window{Until: moment(20 * time.Second)}, PageFrom: PageFromFirst}, []string{"g-early"}},
		"padded both ways":          {aroundAt(webScope, 20*time.Second, 2), both},
	} {
		c.req.Scope = webScope
		assert.Equal(t, c.want, onlyPods(t, svc, f, webScope, readsOf(t, c.req)), name)
	}
}

// Two lines of one stream at one nanosecond are told apart by the entry: the
// newer the entry, the older alone is read, on one page or across two.
func TestAnEntryPartsLinesAtOneNanosecond(t *testing.T) {
	t.Parallel()
	s := newLogServer()
	s.Add("prod/web-1/app", logstest.Stamped(2*time.Second, "a2"), logstest.Stamped(3*time.Second, "x"), logstest.Stamped(3*time.Second, "y"))
	svc, _ := serviceOver(t, webMirror(t), s)
	req := FetchRequest{Scope: webScope, Window: Window{Until: moment(3 * time.Second)}, PageFrom: PageFromLast, PageSize: 10, Seen: []string{nth("web-1", "app", 3*time.Second, 1, "").Cursor()}}

	page := fetch(t, svc, req)
	assert.Equal(t, []string{"a2", "x"}, messages(page.Records))

	req.PageSize = 1
	page = fetch(t, svc, req)
	require.Equal(t, []string{"x"}, messages(page.Records))
	page = fetch(t, svc, FetchRequest{Cursor: page.Page.Prev, PageSize: 1})
	assert.Equal(t, []string{"a2"}, messages(page.Records), "across a page boundary")
}

// Seen beside a Next merges into the cursor's entries, the newer line of a stream
// standing, so a caller that followed from the cursor pages on from it.
func TestSeenBesideANextMergesIntoItsEntries(t *testing.T) {
	t.Parallel()
	s := newLogServer()
	s.Add("prod/web-1/app", logstest.Stamped(time.Second, "a1"), logstest.Stamped(3*time.Second, "a3"), logstest.Stamped(5*time.Second, "a5"))
	s.Add("prod/web-2/app", logstest.Stamped(time.Second, "b1"), logstest.Stamped(3*time.Second, "b3"), logstest.Stamped(5*time.Second, "b5"))
	svc, _ := serviceOver(t, webMirror(t), s)
	page := fetch(t, svc, FetchRequest{Scope: webScope, Window: Window{Since: moment(0)}, PageFrom: PageFromFirst, PageSize: 2})
	require.Equal(t, []string{"a1", "b1"}, messages(page.Records))

	page = fetch(t, svc, FetchRequest{Cursor: page.Page.Next, PageSize: 10, Seen: []string{
		line("web-1", "app", 3*time.Second, "").Cursor(),
		line("web-2", "app", 0, "").Cursor(),
	}})

	assert.Equal(t, []string{"b3", "a5", "b5"}, messages(page.Records), "web-1 past the newer request entry, web-2 past its own")
}

// A page's caps apply as it reads: a message is cut and says by how much, a record
// the page cap dropped sets the edge it lay past and is the next page's first, and
// a PageFromLast keeps its newest (rules 8 and 9).
func TestAPagesCapsSetItsEdges(t *testing.T) {
	t.Parallel()
	s := newLogServer()
	s.Add("prod/web-1/app", logstest.Stamped(0, "aaaa"), logstest.Stamped(time.Second, "bbbb"), logstest.Stamped(2*time.Second, "cccc"))
	svc, _ := serviceOver(t, webMirror(t), s)
	w := Window{Since: moment(0), Until: moment(2 * time.Second)}

	page := fetch(t, svc, FetchRequest{Scope: webScope, Window: w, PageFrom: PageFromFirst, MaxPageBytes: 8, MaxMessageBytes: 3})
	assert.Equal(t, []string{"aaa", "bbb"}, messages(page.Records))
	assert.Equal(t, 1, page.Records[0].TruncatedBytes)
	page = fetch(t, svc, FetchRequest{Cursor: page.Page.Next})
	assert.Equal(t, []string{"cccc"}, messages(page.Records))
	assert.Equal(t, 0, page.Records[0].TruncatedBytes)

	page = fetch(t, svc, FetchRequest{Scope: webScope, Window: w, PageFrom: PageFromLast, MaxPageBytes: 8})
	assert.Equal(t, []string{"bbbb", "cccc"}, messages(page.Records))
	page = fetch(t, svc, FetchRequest{Cursor: page.Page.Prev})
	assert.Equal(t, []string{"aaaa"}, messages(page.Records))
}

// A page for the model redacts each message before it cuts it, so the cut never
// leaves a fragment of a secret the redaction would not know, and the bytes cut
// are the redacted message's. A page for the viewer cuts the stored line.
func TestAMessageIsRedactedBeforeItIsCut(t *testing.T) {
	t.Parallel()
	s := newLogServer()
	s.Add("prod/web-1/app", logstest.Stamped(0, "xxxxxxxxxx \x1b[31msk-"+strings.Repeat("A", 30)+"\x1b[0m"))
	svc, _ := serviceOver(t, webMirror(t), s)
	w := Window{Since: moment(0), Until: moment(0)}

	page := fetch(t, svc, FetchRequest{Scope: webScope, Window: w, PageFrom: PageFromFirst, MaxMessageBytes: 20, ForModel: true})
	require.Len(t, page.Records, 1)
	assert.Equal(t, "xxxxxxxxxx [redacted", page.Records[0].Message)
	assert.Equal(t, 1, page.Records[0].TruncatedBytes)

	page = fetch(t, svc, FetchRequest{Scope: webScope, Window: w, PageFrom: PageFromFirst, MaxMessageBytes: 20})
	assert.Equal(t, "xxxxxxxxxx \x1b[31msk-A", page.Records[0].Message)
}

// A line that reaches the file after a page was read, stamped inside that page's
// span, is on the next Next page and on no later one: in a stream that had a line
// on the page, one that had none, and one that joined the scope since (rule 12).
func TestAStragglerIsOnTheNextPage(t *testing.T) {
	t.Parallel()
	s := newLogServer()
	s.Add("prod/web-1/app", logstest.Stamped(0, "w1 0"), logstest.Stamped(time.Second, "w1 1"))
	s.Add("prod/web-2/app", logstest.Stamped(2*time.Second, "w2 2"), logstest.Stamped(3*time.Second, "w2 3"), logstest.Stamped(4*time.Second, "w2 4"))
	f := webMirror(t)
	f.store.Apply(t, pod("web-3", "uid-web-3", "node-a", webReplicaSet(), container{name: "app"}))
	svc, _ := serviceOver(t, f, s)

	page := fetch(t, svc, FetchRequest{Scope: webScope, PageFrom: PageFromFirst, PageSize: 4})
	require.Equal(t, []string{"w1 0", "w1 1", "w2 2", "w2 3"}, messages(page.Records))

	s.Add("prod/web-1/app", logstest.Stamped(2500*time.Millisecond, "late w1"))
	s.Add("prod/web-3/app", logstest.Stamped(1500*time.Millisecond, "late w3"))
	s.Add("prod/web-4/app", logstest.Stamped(500*time.Millisecond, "late w4"))
	f.store.Apply(t, pod("web-4", "uid-web-4", "node-a", webReplicaSet(), container{name: "app"}))
	page = goOn(t, svc, page.Page.Next, 10)
	assert.Equal(t, []string{"late w4", "late w3", "late w1", "w2 4"}, messages(page.Records))

	for range 2 {
		page = goOn(t, svc, page.Page.Follow, 10)
		assert.Empty(t, page.Records, "no later page holds them")
	}
}

// A poll of a last page's live window answers what each stream wrote since, each
// line once, however the streams' stamps are skewed.
func TestAPollDeliversSkewedLinesOnce(t *testing.T) {
	t.Parallel()
	s := newLogServer()
	s.Add("prod/web-1/app", logstest.LineEvery(3)...)
	s.Add("prod/web-2/app", logstest.Stamped(500*time.Millisecond, "b"))
	svc, _ := serviceOver(t, webMirror(t), s)

	page := fetch(t, svc, FetchRequest{Scope: webScope, PageFrom: PageFromLast, PageSize: 10})
	require.Equal(t, []string{"0", "b", "1", "2"}, messages(page.Records))

	s.Add("prod/web-1/app", logstest.Stamped(20*time.Second, "20"))
	s.Add("prod/web-2/app", logstest.Stamped(15*time.Second, "b15"))
	page = goOn(t, svc, page.Page.Follow, 10)
	assert.Equal(t, []string{"b15", "20"}, messages(page.Records))

	s.Add("prod/web-2/app", logstest.Stamped(16*time.Second, "b16"))
	page = goOn(t, svc, page.Page.Follow, 10)
	assert.Equal(t, []string{"b16"}, messages(page.Records), "behind the newest line read, and still delivered")

	page = goOn(t, svc, page.Page.Follow, 10)
	assert.Empty(t, page.Records)
	assert.NotNil(t, page.Page.Follow)
}

// A tail opened from a poll's Follow delivers exactly the lines the poll does,
// then the lines written since; one reopened from the same Follow with Seen built
// from every record held delivers the rest, and none again: not web-2's, quiet
// since before the first tail, nor web-3's, seen on the page alone.
func TestATailFromAPollsFollowDeliversEachLineOnce(t *testing.T) {
	t.Parallel()
	s := newLogServer()
	s.Add("prod/web-1/app", logstest.LineEvery(3)...)
	s.Add("prod/web-2/app", logstest.Stamped(500*time.Millisecond, "b"))
	s.Add("prod/web-3/app", logstest.Stamped(1500*time.Millisecond, "c"))
	f := webMirror(t)
	f.store.Apply(t, web3())
	svc, _ := serviceOver(t, f, s)
	page := fetch(t, svc, FetchRequest{Scope: webScope, PageFrom: PageFromLast, PageSize: 10})
	require.Equal(t, []string{"0", "b", "1", "c", "2"}, messages(page.Records))
	s.Add("prod/web-1/app", logstest.Stamped(20*time.Second, "20"))
	s.Add("prod/web-2/app", logstest.Stamped(15*time.Second, "b15"))
	poll := goOn(t, svc, page.Page.Follow, 10)
	require.Equal(t, []string{"b15", "20"}, messages(poll.Records))

	ctx, cancel := context.WithCancel(t.Context())
	first := startTail(t, ctx, svc, TailRequest{Cursor: page.Page.Follow})
	held := slices.Concat(page.Records, recvRecords(t, first, 2))
	assert.ElementsMatch(t, messages(poll.Records), messages(held[len(page.Records):]))
	s.Add("prod/web-1/app", logstest.Stamped(21*time.Second, "21"))
	held = append(held, recvLine(t, first, "the line written since"))
	assert.Equal(t, "21", held[len(held)-1].Message)
	cancel()

	var seen []string
	for _, rec := range held {
		seen = append(seen, rec.Cursor())
	}
	s.Add("prod/web-1/app", logstest.Stamped(22*time.Second, "22"))
	second := startTail(t, t.Context(), svc, TailRequest{Cursor: page.Page.Follow, Seen: seen})
	assert.Equal(t, "22", recvLine(t, second, "the rest").Message)
	s.Add("prod/web-3/app", logstest.Stamped(23*time.Second, "c23"))
	assert.Equal(t, "c23", recvLine(t, second, "a line written after").Message, "nothing again before it")
}

// A page in a window whose After is counted reads no more past Until than the count
// asks for, whatever its size: the fill stops there, so the file holds no more.
func TestACountedAfterReadsNoFurtherThanItsCount(t *testing.T) {
	t.Parallel()
	s := newLogServer()
	s.Add("prod/web-1/app", logstest.LineEvery(100)...)
	ret := testRetention
	ret.WriteBatch = 1
	svc, f := serviceIn(t, webMirror(t), s, t.TempDir(), ret)

	page := fetch(t, svc, FetchRequest{Scope: webScope, Window: Window{Until: moment(10 * time.Second), After: Padding{Lines: 2}}, PageFrom: PageFromFirst, PageSize: 500})

	assert.Equal(t, messages(linesOf(logstest.LineEvery(13))), messages(page.Records))
	assert.Equal(t, [3]bool{false, false, false}, edgesOf(page.Page))
	spans := spansOf(t, svc, f)
	require.NotEmpty(t, spans)
	assert.Equal(t, sec(12).TS, spans[len(spans)-1].To.TS, "the file holds nothing past the count")
}

// A counted After fills by polls as its lines arrive: the poll that finds the
// last of them sets no Follow, and a late line in the range, written meanwhile,
// is delivered and counts toward nothing (rule 7).
func TestACountedAfterFillsByPolls(t *testing.T) {
	t.Parallel()
	s := newLogServer()
	s.Add("prod/web-1/app", logstest.LineEvery(31)...)
	svc, _ := serviceOver(t, webMirror(t), s)
	addSeconds := func(from, to int) {
		for i := from; i < to; i++ {
			s.Add("prod/web-1/app", logstest.Stamped(time.Duration(i)*time.Second, strconv.Itoa(i)))
		}
	}

	page := fetch(t, svc, FetchRequest{Scope: webScope, Window: Window{Until: moment(10 * time.Second), After: Padding{Lines: 100}}, PageFrom: PageFromFirst, PageSize: 500})
	require.Len(t, page.Records, 31)
	assert.Equal(t, 20, cursorIn(t, page.Page.Follow).after)

	addSeconds(31, 71)
	page = goOn(t, svc, page.Page.Follow, 500)
	assert.Len(t, page.Records, 40)
	assert.Equal(t, 60, cursorIn(t, page.Page.Follow).after)

	s.Add("prod/web-2/app", logstest.Stamped(5*time.Second, "late"))
	addSeconds(71, 116)
	page = goOn(t, svc, page.Page.Follow, 500)
	assert.Equal(t, "late", page.Records[0].Message)
	assert.Equal(t, messages(linesOf(logstest.LineEvery(111)))[71:], messages(page.Records[1:]), "the forty to the hundredth, and no more")
	assert.Equal(t, [3]bool{true, false, false}, edgesOf(page.Page))
}

// A page cursor holds nothing of the file: once a clear refills it, a Next answers
// what the same Next answered before.
func TestANextAnswersTheSameAcrossAClear(t *testing.T) {
	t.Parallel()
	s := newLogServer()
	s.Add("prod/web-1/app", logstest.LineEvery(10)...)
	s.Add("prod/web-2/app", logstest.Stamped(2500*time.Millisecond, "b"))
	svc, f := serviceOver(t, webMirror(t), s)

	page := fetch(t, svc, FetchRequest{Scope: webScope, PageFrom: PageFromFirst, PageSize: 3})
	before := goOn(t, svc, page.Page.Next, 3)
	require.NoError(t, svc.Clear(int64(f.active.Cache.ID)))
	after := goOn(t, svc, page.Page.Next, 3)

	assert.Equal(t, []string{"b", "3", "4"}, messages(before.Records))
	assert.Equal(t, before.Records, after.Records)
	assert.Equal(t, before.Page, after.Page)
}

// A scope of MaxStreams live pods beside a gone pod whose logs the file keeps
// pages on through cursors that name every container, more than MaxStreams.
func TestAScopePastMaxStreamsPagesOn(t *testing.T) {
	t.Parallel()
	s := newLogServer()
	f, keys := hundredStreams(t)
	require.Len(t, keys, MaxStreams)
	var want []string
	for i, key := range keys {
		s.Add(key, logstest.Stamped(time.Duration(i)*time.Second, "line "+strconv.Itoa(i)))
		want = append(want, "line "+strconv.Itoa(i))
	}
	svc, _ := serviceOver(t, f, s)
	seed(t, svc, f, rowOf("old-1", "app"), logstore.Fill{Lines: []logstore.Line{{Pos: sec(1000), Message: "gone"}}, AtBeginning: true})
	want = append(want, "gone")
	scope := Scope{Sources: append(slices.Clone(webScope.Sources), ScopeSource{Namespace: "prod", Kind: SourcePod, Name: "old-1", Containers: []string{"app"}})}

	got := pagesOn(t, svc, fetch(t, svc, FetchRequest{Scope: scope, PageFrom: PageFromFirst, PageSize: 60}), 60)

	assert.Equal(t, want, recordsOf(got, PageFromFirst))
	last := got[len(got)-1]
	assert.Len(t, cursorIn(t, last.Page.Follow).seen, MaxStreams+1)
	assert.Empty(t, goOn(t, svc, last.Page.Follow, 60).Records)
}

// ===== The page cursor =====

// webCursor is a newer page cursor over webScope's deployment between 10:00:10 and
// 10:00:20, padded by three lines each side, with an entry for web-1's app and a
// floor at web-2's.
func webCursor() pageCursor {
	web1 := lineAt(logline.StreamKey{Namespace: "prod", Pod: "web-1", Container: "app"}, 12*time.Second, 1)
	return pageCursor{
		Scope: webScope,
		resolvedWindow: resolvedWindow{
			Window:     Window{Since: moment(10 * time.Second), Until: moment(20 * time.Second), Before: Padding{Lines: 3}, After: Padding{Lines: 3}},
			ResolvedAt: at(time.Hour),
			Start:      lineAt(logline.StreamKey{Namespace: "prod", Pod: "web-2", Container: "app"}, 8*time.Second, 0),
		},
		newer:  true,
		seen:   map[logline.ContainerName]logline.RunPlace{web1.ContainerName: web1.RunPlace},
		at:     lineAt(logline.StreamKey{Namespace: "prod", Pod: "web-2", Container: "app"}, 11*time.Second, 0),
		before: 2,
		after:  1,
	}
}

// rawPageCursor is c's JSON, inflated.
func rawPageCursor(t *testing.T, c string) string {
	t.Helper()
	b, err := base64.RawURLEncoding.DecodeString(c)
	require.NoError(t, err)
	var out bytes.Buffer
	_, err = out.ReadFrom(flate.NewReader(bytes.NewReader(b)))
	require.NoError(t, err)
	return out.String()
}

// pageCursorOfJSON is j deflated and encoded as a page cursor is.
func pageCursorOfJSON(t *testing.T, j string) string {
	t.Helper()
	var b bytes.Buffer
	w, err := flate.NewWriter(&b, flate.BestCompression)
	require.NoError(t, err)
	_, err = w.Write([]byte(j))
	require.NoError(t, err)
	require.NoError(t, w.Close())
	return base64.RawURLEncoding.EncodeToString(b.Bytes())
}

// A page cursor decodes to what was encoded, and one cursor has one string.
func TestAPageCursorRoundTrips(t *testing.T) {
	t.Parallel()
	for name, c := range map[string]pageCursor{
		"newer":          webCursor(),
		"older":          {Scope: webScope, resolvedWindow: resolvedWindow{ResolvedAt: at(time.Hour)}, at: logline.MomentAt(at(5 * time.Second))},
		"no at":          {Scope: webScope, resolvedWindow: resolvedWindow{ResolvedAt: at(time.Hour)}, newer: true},
		"a span":         {Scope: webScope, resolvedWindow: resolvedWindow{Window: Window{Since: moment(0), After: Padding{Span: time.Minute}}, ResolvedAt: at(time.Hour), Start: logline.MomentAt(at(0))}, newer: true},
		"a grep":         {Scope: Scope{Sources: webScope.Sources, Filters: []ScopeFilter{{Field: FilterZone, Values: []string{"eu-1a"}}}, Grep: "err(or)?"}, resolvedWindow: resolvedWindow{ResolvedAt: at(time.Hour)}},
		"a moment entry": {Scope: webScope, resolvedWindow: resolvedWindow{ResolvedAt: at(time.Hour)}, newer: true, seen: map[logline.ContainerName]logline.RunPlace{{Namespace: "prod", Pod: "web-1", Container: "app"}: {Timestamp: at(3 * time.Second)}}},
	} {
		s := encodePageCursor(c)
		back, err := decodePageCursor(s)
		require.NoError(t, err, name)
		if len(c.seen) == 0 {
			c.seen = nil
		}
		assert.Equal(t, c, back, name)
		assert.Equal(t, s, encodePageCursor(back), name+": one string")
	}
}

// A window's bounds are written in UTC, so one window named in two zones has one
// string.
func TestAPageCursorsBoundsAreUTC(t *testing.T) {
	t.Parallel()
	c := webCursor()
	east := time.FixedZone("east", 2*60*60)
	since, until := c.Since.In(east), c.Until.In(east)
	zoned := c
	zoned.Since, zoned.Until = &since, &until

	assert.Equal(t, encodePageCursor(c), encodePageCursor(zoned))
	back, err := decodePageCursor(encodePageCursor(zoned))
	require.NoError(t, err)
	assert.Equal(t, time.UTC, back.Since.Location())
	assert.Equal(t, time.UTC, back.Until.Location())
}

// A page cursor is deflated JSON with a version first, its entries a list in name
// order whatever the map's order.
func TestAPageCursorIsVersionedJSON(t *testing.T) {
	t.Parallel()
	c := webCursor()
	c.seen[logline.ContainerName{Namespace: "prod", Pod: "web-0", Container: "app"}] = logline.RunPlace{ContainerID: "containerd://uid-web-0-app-0", Timestamp: at(9 * time.Second), Ordinal: 0}

	raw := rawPageCursor(t, encodePageCursor(c))

	assert.True(t, strings.HasPrefix(raw, `{"v":1,"s":{"sources":`), raw)
	assert.Less(t, strings.Index(raw, `"pod":"web-0"`), strings.Index(raw, `"pod":"web-1"`), raw)
	assert.Contains(t, raw, `"n":true`)
	assert.Contains(t, raw, `"b":2,"a":1`)
}

// An older page cursor's entries decode as it wrote them: an older first page's
// Seen rides every page back.
func TestAnOlderCursorsEntriesDecode(t *testing.T) {
	t.Parallel()
	older := webCursor()
	older.newer = false
	c, err := decodePageCursor(encodePageCursor(older))
	require.NoError(t, err)
	assert.Equal(t, older.seen, c.seen)
}

// A page cursor of more than MaxStreams entries decodes: the service writes one for
// every container a page passed, gone pods' included.
func TestAPageCursorOfManyEntriesDecodes(t *testing.T) {
	t.Parallel()
	c := webCursor()
	for i := range MaxStreams + 10 {
		c.seen[logline.ContainerName{Namespace: "prod", Pod: "web-" + strconv.Itoa(100+i), Container: "app"}] = logline.RunPlace{ContainerID: "containerd://a", Timestamp: at(time.Second), Ordinal: 0}
	}

	back, err := decodePageCursor(encodePageCursor(c))

	require.NoError(t, err)
	assert.Len(t, back.seen, MaxStreams+11)
}

// A string that is not a page cursor this version wrote is cursor-invalid.
func TestABadPageCursorIsRefused(t *testing.T) {
	t.Parallel()
	good := rawPageCursor(t, encodePageCursor(webCursor()))
	for name, s := range map[string]string{
		"not base64url":          "a+b/",
		"not deflated":           base64.RawURLEncoding.EncodeToString([]byte("{}")),
		"not JSON":               pageCursorOfJSON(t, "yesterday"),
		"another version":        pageCursorOfJSON(t, strings.Replace(good, `"v":1`, `"v":2`, 1)),
		"a bad line":             pageCursorOfJSON(t, strings.Replace(good, `"n":1`, `"n":-1`, 1)),
		"a line with no name":    pageCursorOfJSON(t, strings.Replace(good, `"seen":[{"v":1,"ns":"prod","pod":"web-1","c":"app",`, `"seen":[{"v":1,`, 1)),
		"a bare moment":          pageCursorOfJSON(t, strings.Replace(good, `"seen":[{"v":1,"ns":"prod","pod":"web-1","c":"app","id":"containerd://uid-web-1-app-0","t":"2026-10-08T10:00:12Z","n":1}]`, `"seen":[{"v":1,"t":"2026-10-08T10:00:12Z"}]`, 1)),
		"a bad start":            pageCursorOfJSON(t, strings.Replace(good, `"t":"2026-10-08T10:00:08Z","n":0`, `"t":"2026-10-08T10:00:08Z","n":-1`, 1)),
		"a bad at":               pageCursorOfJSON(t, strings.Replace(good, `"t":"2026-10-08T10:00:11Z","n":0`, `"t":"2026-10-08T10:00:11Z","n":-1`, 1)),
		"no clock":               pageCursorOfJSON(t, strings.Replace(good, `"resolvedAt":"2026-10-08T11:00:00Z"`, `"resolvedAt":"0001-01-01T00:00:00Z"`, 1)),
		"a count past its lines": pageCursorOfJSON(t, strings.Replace(good, `"b":2`, `"b":4`, 1)),
		"a negative count":       pageCursorOfJSON(t, strings.Replace(good, `"a":1`, `"a":-1`, 1)),
		"a window refused":       pageCursorOfJSON(t, strings.Replace(good, `"lines":3`, `"lines":501`, 1)),
	} {
		_, err := decodePageCursor(s)
		assert.Equal(t, refusal.CodeCursorInvalid, codeOf(err), name)
	}

	spanned := webCursor()
	spanned.Window.After = Padding{Span: time.Minute}
	_, err := decodePageCursor(encodePageCursor(spanned))
	assert.Equal(t, refusal.CodeCursorInvalid, codeOf(err), "a count for a span")

	open := webCursor()
	open.Since, open.Before, open.before = nil, Padding{}, 0
	_, err = decodePageCursor(encodePageCursor(open))
	assert.Equal(t, refusal.CodeCursorInvalid, codeOf(err), "a start for an open since with no count")

	_, err = decodePageCursor(pageCursorOfJSON(t, strings.Replace(good, `"v":1`, `"v":2`, 1)))
	assert.ErrorContains(t, err, "read again")
}

// A first page pins its clock as a continuation decodes it: in UTC, with no
// monotonic reading, so every page reports the same ResolvedAt.
func TestAFirstPagePinsItsClockAsAContinuationDecodesIt(t *testing.T) {
	t.Parallel()
	now := time.Now().In(time.FixedZone("UTC+2", 2*60*60))
	c, err := firstPage(FetchRequest{Scope: webScope, PageFrom: PageFromFirst}, now)
	require.NoError(t, err)

	cont, err := decodePageCursor(encodePageCursor(c))
	require.NoError(t, err)
	assert.Equal(t, c.ResolvedAt, cont.ResolvedAt)
}

// A first page whose own edge is counted reads that side's padding beside its
// range, the other way from the bound: back from Since for PageFromFirst, on from the
// range's end for PageFromLast. A first page from the other edge, or one whose edge is
// a time or a span, reads none; padding before an open Since counts for nothing.
func TestACountedFirstEdgeReadsItsPadding(t *testing.T) {
	t.Parallel()
	w := Window{Since: moment(10 * time.Second), Until: moment(20 * time.Second), Before: Padding{Lines: 3}, After: Padding{Lines: 4}}
	c, err := firstPage(FetchRequest{Scope: webScope, Window: w, PageFrom: PageFromFirst}, at(time.Hour))
	require.NoError(t, err)

	l, lines := c.lookup()
	require.NotNil(t, l)
	assert.Equal(t, c.turned(logline.MomentAt(at(10*time.Second))), *l)
	assert.Equal(t, 3, lines)
	assert.Equal(t, pos(10*time.Second, -1), l.keyFor(psOf("web-1", "app")), "before every line at since")

	c.newer = false
	l, lines = c.lookup()
	require.NotNil(t, l)
	assert.Equal(t, c.turned(logline.MomentAt(at(20*time.Second))), *l)
	assert.Equal(t, 4, lines)
	assert.Equal(t, pos(20*time.Second, math.MaxInt), l.keyFor(psOf("web-1", "app")), "after every line at until")

	open, err := firstPage(FetchRequest{Window: Window{After: Padding{Lines: 4}}, PageFrom: PageFromLast}, at(time.Hour))
	require.NoError(t, err)
	l, _ = open.lookup()
	require.NotNil(t, l)
	assert.Equal(t, logline.MomentAt(at(time.Hour-1)), l.at, "an open until counts from the first page's clock")

	for name, tc := range map[string]struct {
		w    Window
		from PageFrom
	}{
		"the other edge":       {Window{Since: moment(10 * time.Second), Before: Padding{Lines: 3}}, PageFromLast},
		"a span":               {Window{Since: moment(0), Before: Padding{Span: time.Second}}, PageFromFirst},
		"before an open since": {Window{Before: Padding{Lines: 3}}, PageFromFirst},
	} {
		c, err := firstPage(FetchRequest{Window: tc.w, PageFrom: tc.from}, at(time.Hour))
		require.NoError(t, err, name)
		l, _ := c.lookup()
		assert.Nil(t, l, name)
	}
}

// psOf is the page stream of pod/container's first run.
func psOf(pod, container string) pageStream { return pageStream{row: rowOf(pod, container)} }

// pos is the file position of the ordinal-th line at d past 10:00.
func pos(d time.Duration, ord int) logstore.Pos { return logstore.Pos{TS: at(d).UnixNano(), Ord: ord} }

// A stream reads from its entry, else from at, else from the window's edge, each
// clipped to the window's known edge, an entry short of at passed over while a
// newer cursor's edge is unknown; an older cursor from at, else the window's end.
func TestAStreamsKeyIsItsEntryAtOrTheEdge(t *testing.T) {
	t.Parallel()
	c := webCursor()
	c.Start = logline.MomentAt(at(9 * time.Second))
	web1, web2 := psOf("web-1", "app"), psOf("web-2", "app")

	assert.Equal(t, pos(12*time.Second, 1), c.keyFor(web1), "past its entry")
	assert.Equal(t, pos(11*time.Second, 0), c.keyFor(web2), "past at, its own line")
	assert.Equal(t, pos(11*time.Second, math.MaxInt), c.keyFor(psOf("web-1", "sidecar")), "past at, after its moment's lines sorting before at")

	c.at = logline.Cursor{}
	assert.Equal(t, pos(9*time.Second, -1), c.keyFor(web2), "from Start")

	c.seen[web1.run().ContainerName] = logline.RunPlace{ContainerID: web1.row.ContainerID, Timestamp: at(2 * time.Second), Ordinal: 0}
	assert.Equal(t, pos(9*time.Second, -1), c.keyFor(web1), "an entry short of Start is clipped to it")

	c.seen[web1.run().ContainerName] = logline.RunPlace{Timestamp: at(12 * time.Second)}
	assert.Equal(t, pos(12*time.Second, math.MaxInt), c.keyFor(web1), "past a container's moment")

	older := pageCursor{resolvedWindow: resolvedWindow{Window: Window{Until: moment(20 * time.Second)}, ResolvedAt: at(time.Hour)}}
	assert.Equal(t, pos(20*time.Second, math.MaxInt), older.keyFor(web1), "an older cursor from the end")
	older.at = logline.MomentAt(at(15 * time.Second))
	assert.Equal(t, pos(15*time.Second, -1), older.keyFor(web1), "before at")
	older.at = logline.MomentAt(at(25 * time.Second))
	assert.Equal(t, pos(20*time.Second, math.MaxInt), older.keyFor(web1), "at past the end is clipped to it")

	counted := pageCursor{resolvedWindow: resolvedWindow{Window: Window{Since: moment(10 * time.Second), Before: Padding{Lines: 3}}, ResolvedAt: at(time.Hour)}, newer: true}
	assert.Equal(t, pos(10*time.Second, -1), counted.keyFor(web1), "a counted start not yet found reads from Since")
	counted.at = logline.MomentAt(at(5 * time.Second))
	assert.Equal(t, pos(5*time.Second, math.MaxInt), counted.keyFor(web1), "past at while the edge is a count")
	counted.seen = map[logline.ContainerName]logline.RunPlace{web1.run().ContainerName: {ContainerID: web1.row.ContainerID, Timestamp: at(2 * time.Second)}}
	assert.Equal(t, pos(5*time.Second, math.MaxInt), counted.keyFor(web1), "an entry short of at is passed over while the edge is a count")
}

// Two runs of one name share one entry, so the restarted run reads on from the
// old run's place rather than from the window's start; at the entry's moment the
// ordinal decides, then the id.
func TestAnEntryHoldsAcrossARestart(t *testing.T) {
	t.Parallel()
	c := webCursor()
	c.at = logline.Cursor{}
	old := psOf("web-1", "app")
	restarted := old
	restarted.row.ContainerID = containerID("uid-web-1", "app", 1)
	c.seen[old.run().ContainerName] = logline.RunPlace{ContainerID: old.row.ContainerID, Timestamp: at(12 * time.Second), Ordinal: 3}

	assert.Equal(t, pos(12*time.Second, 3), c.keyFor(old))
	assert.Equal(t, pos(12*time.Second, 2), c.keyFor(restarted), "its own line at the ordinal sorts past the old id's")
}

// A first page's Seen names a stream's last line: the newer of two for one stream
// stands, whatever their order, and a moment is refused.
func TestSeenIsTheFirstPagesEntries(t *testing.T) {
	t.Parallel()
	first := nth("web-1", "app", time.Second, 0, "")
	later := nth("web-1", "app", 2*time.Second, 0, "")
	other := nth("web-2", "app", time.Second, 0, "")

	want := map[logline.ContainerName]logline.RunPlace{
		logline.CursorOf(later).ContainerName: logline.CursorOf(later).RunPlace,
		logline.CursorOf(other).ContainerName: logline.CursorOf(other).RunPlace,
	}
	for _, order := range [][]string{
		{first.Cursor(), other.Cursor(), later.Cursor()},
		{later.Cursor(), other.Cursor(), first.Cursor()},
	} {
		seen, err := seenOf(order, true)
		require.NoError(t, err)
		assert.Equal(t, want, seen, "the newer stands in either order")
	}
	_, err := seenOf([]string{logline.Encode(logline.MomentAt(at(0)))}, true)
	assert.Equal(t, refusal.CodeCursorInvalid, codeOf(err), "a moment")
	_, err = seenOf([]string{"nope"}, true)
	assert.Equal(t, refusal.CodeCursorInvalid, codeOf(err), "not a cursor")
}

// reachOf is the reach of pod/container's first run from first to last, d past
// 10:00 each, the first line of its moment.
func reachOf(pod, container string, first, last time.Duration) streamReach {
	key := logline.StreamKey{Namespace: "prod", Pod: pod, Container: container}
	return streamReach{run: psOf(pod, container).run(), first: lineAt(key, first, 0), last: lineAt(key, last, 0)}
}

// entry is the seen entry of pod/container's first run at d, the first of its
// moment.
func entry(pod, container string, d time.Duration) logline.RunPlace {
	return lineAt(logline.StreamKey{Namespace: "prod", Pod: pod, Container: container}, d, 0).RunPlace
}

// name is pod/container in prod.
func name(pod, container string) logline.ContainerName {
	return logline.ContainerName{Namespace: "prod", Pod: pod, Container: container}
}

// openCursor is a cursor over an open window, newer or older.
func openCursor(newer bool) pageCursor {
	return pageCursor{Scope: webScope, resolvedWindow: resolvedWindow{ResolvedAt: at(time.Hour)}, newer: newer}
}

// Going newer, each stream's entry is the last row it passed, kept or rejected by
// the grep, short of the cut; past the cut, its last line on the page; a stream
// stopped at a moment reads on past the moment. at goes on as it came.
func TestTheEdgeAheadNewerIsEachStreamsReach(t *testing.T) {
	t.Parallel()
	c := openCursor(true)
	c.at = logline.MomentAt(at(0))
	c.seen = map[logline.ContainerName]logline.RunPlace{name("web-3", "app"): entry("web-3", "app", 0), name("gone", "app"): entry("gone", "app", 0)}
	loud := reachOf("web-3", "app", 0, 0)
	loud.first, loud.last = logline.Cursor{}, logline.Cursor{}
	loud.stop = logline.Cursor{ContainerName: loud.run.ContainerName, RunPlace: logline.RunPlace{Timestamp: at(3 * time.Second)}}
	m := merge{
		records: []logline.Record{line("web-1", "app", time.Second, "a"), line("web-2", "app", 2*time.Second, "b"), line("web-1", "app", 3*time.Second, "c")},
		cut:     lineAt(logline.StreamKey{Namespace: "prod", Pod: "web-1", Container: "app"}, 3*time.Second, 0),
		reaches: []streamReach{reachOf("web-1", "app", time.Second, 5*time.Second), reachOf("web-2", "app", 2*time.Second, 2500*time.Millisecond), loud, reachOf("web-4", "app", 0, 0)},
	}
	m.reaches[3].first, m.reaches[3].last = logline.Cursor{}, logline.Cursor{}

	ahead, behind := c.edges(m)

	assert.True(t, ahead.newer)
	assert.Equal(t, c.at, ahead.at)
	assert.Equal(t, map[logline.ContainerName]logline.RunPlace{
		name("web-1", "app"): entry("web-1", "app", 3*time.Second),
		name("web-2", "app"): entry("web-2", "app", 2500*time.Millisecond),
		name("web-3", "app"): {Timestamp: at(3 * time.Second)},
	}, ahead.seen, "web-1 past the cut falls back to its last line; web-2 keeps its rejected row; web-3 its stop; gone left the scope")
	require.NotNil(t, behind)
	assert.False(t, behind.newer)
	assert.Equal(t, logline.CursorOf(m.records[0]), behind.at)
	assert.Nil(t, behind.seen)
}

// Going older, the edge ahead is one position: the cut, else the farthest row
// passed, with no entry; the edge behind goes newer from each stream's newest line
// on the page, and from the farthest line passed for a stream with none.
func TestTheEdgesOfAnOlderPage(t *testing.T) {
	t.Parallel()
	c := openCursor(false)
	m := merge{
		records: []logline.Record{line("web-1", "app", 3*time.Second, "c"), line("web-2", "app", 4*time.Second, "d"), line("web-1", "app", 5*time.Second, "e")},
		reaches: []streamReach{reachOf("web-1", "app", 5*time.Second, 3*time.Second), reachOf("web-2", "app", 4*time.Second, time.Second)},
	}

	ahead, behind := c.edges(m)

	assert.False(t, ahead.newer)
	assert.Nil(t, ahead.seen)
	assert.Equal(t, lineAt(logline.StreamKey{Namespace: "prod", Pod: "web-2", Container: "app"}, time.Second, 0), ahead.at, "the farthest row passed")
	require.NotNil(t, behind)
	assert.True(t, behind.newer)
	assert.Equal(t, map[logline.ContainerName]logline.RunPlace{name("web-1", "app"): entry("web-1", "app", 5*time.Second), name("web-2", "app"): entry("web-2", "app", 4*time.Second)}, behind.seen)
	assert.Equal(t, ahead.at, behind.at)

	m.cut = logline.CursorOf(m.records[0])
	ahead, _ = c.edges(m)
	assert.Equal(t, m.cut, ahead.at, "a cut page goes on from its cut")
}

// A page that passed no line hands its incoming edge back, and has none behind.
func TestAPageThatPassedNothingKeepsItsEdge(t *testing.T) {
	t.Parallel()
	c := openCursor(true)
	c.seen = map[logline.ContainerName]logline.RunPlace{name("web-1", "app"): entry("web-1", "app", time.Second)}
	m := merge{records: []logline.Record{}, reaches: []streamReach{{run: psOf("web-1", "app").run()}}}

	ahead, behind := c.edges(m)

	assert.Equal(t, c, ahead)
	assert.Nil(t, behind)

	older := openCursor(false)
	older.at = logline.MomentAt(at(time.Second))
	ahead, behind = older.edges(merge{records: []logline.Record{}, reaches: m.reaches})
	assert.Equal(t, older, ahead)
	assert.Nil(t, behind)
}

// Two runs of one name passed on one page leave the later as the entry.
func TestTwoRunsOfOneNameLeaveTheLaterEntry(t *testing.T) {
	t.Parallel()
	old, restarted := reachOf("web-1", "app", 0, 2*time.Second), reachOf("web-1", "app", 3*time.Second, 4*time.Second)
	restarted.run.ContainerID = containerID("uid-web-1", "app", 1)
	restarted.first.ContainerID, restarted.last.ContainerID = restarted.run.ContainerID, restarted.run.ContainerID

	ahead, _ := openCursor(true).edges(merge{records: []logline.Record{}, reaches: []streamReach{restarted, old}})

	assert.Equal(t, map[logline.ContainerName]logline.RunPlace{name("web-1", "app"): restarted.last.RunPlace}, ahead.seen)
}

// Each edge's counts move by the padding lines the page delivered: up moving away
// from a bound, down moving toward it, clamped to 0 through Lines; the edge behind
// keeps the incoming counts.
func TestTheCountsMoveByThePaddingDelivered(t *testing.T) {
	t.Parallel()
	w := resolvedWindow{Window: Window{Since: moment(10 * time.Second), Until: moment(20 * time.Second), Before: Padding{Lines: 3}, After: Padding{Lines: 3}}, ResolvedAt: at(time.Hour)}
	recs := []logline.Record{line("web-1", "app", 8*time.Second, ""), line("web-1", "app", 9*time.Second, ""), line("web-1", "app", 15*time.Second, ""), line("web-1", "app", 21*time.Second, "")}
	m := merge{records: recs, reaches: []streamReach{reachOf("web-1", "app", 8*time.Second, 21*time.Second)}}

	newer := pageCursor{Scope: webScope, resolvedWindow: w, newer: true, before: 1, after: 1}
	ahead, behind := newer.edges(m)
	assert.Equal(t, [2]int{0, 2}, [2]int{ahead.before, ahead.after})
	assert.Equal(t, [2]int{1, 1}, [2]int{behind.before, behind.after})

	older := pageCursor{Scope: webScope, resolvedWindow: w, before: 2, after: 1}
	ahead, behind = older.edges(m)
	assert.Equal(t, [2]int{3, 0}, [2]int{ahead.before, ahead.after})
	assert.Equal(t, [2]int{2, 1}, [2]int{behind.before, behind.after})
}

// ===== The resolved window =====

// A window whose bounds cross, or whose padding is past its limits or names both
// a count and a span, is refused.
func TestAWindowIsChecked(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		w    Window
		code refusal.Code
	}{
		"since after until":   {Window{Since: moment(2 * time.Second), Until: moment(time.Second)}, refusal.CodeInvalidRange},
		"both lines and span": {Window{Before: Padding{Lines: 1, Span: time.Second}}, refusal.CodeConflictingParameters},
		"too many lines":      {Window{After: Padding{Lines: MaxPageSize + 1}}, refusal.CodeBadPadding},
		"negative lines":      {Window{Before: Padding{Lines: -1}}, refusal.CodeBadPadding},
		"too long a span":     {Window{After: Padding{Span: MaxPaddingSpan + 1}}, refusal.CodeBadPadding},
		"a negative span":     {Window{Before: Padding{Span: -time.Second}}, refusal.CodeBadPadding},
	} {
		assert.Equal(t, tc.code, codeOf(checkWindow(tc.w)), name)
	}
	for name, w := range map[string]Window{
		"open":             {},
		"one nanosecond":   {Since: moment(time.Second), Until: moment(time.Second)},
		"the most lines":   {Before: Padding{Lines: MaxPageSize}},
		"the longest span": {After: Padding{Span: MaxPaddingSpan}},
	} {
		assert.NoError(t, checkWindow(w), name)
	}
}

// A window's edges are its bounds widened by its spans; an open Until's span is
// measured from the first page's clock, so it is the same on every page; a count's
// edge is unknown until a page finds it.
func TestAWindowsEdges(t *testing.T) {
	t.Parallel()
	resolved := at(time.Hour)
	for name, tc := range map[string]struct {
		w          Window
		start, end logline.Bound
	}{
		"a span each side":     {Window{Since: moment(10 * time.Second), Until: moment(20 * time.Second), Before: Padding{Span: time.Second}, After: Padding{Span: 2 * time.Second}}, from(9 * time.Second), to(22 * time.Second)},
		"open":                 {Window{}, logline.Bound{}, logline.Bound{}},
		"an open until":        {Window{Since: moment(10 * time.Second)}, from(10 * time.Second), logline.Bound{}},
		"an open until's span": {Window{After: Padding{Span: time.Minute}}, logline.Bound{}, to(time.Hour + time.Minute)},
		"one nanosecond":       {Window{Since: moment(time.Second), Until: moment(time.Second)}, from(time.Second), to(time.Second)},
		"counts":               {Window{Since: moment(10 * time.Second), Until: moment(20 * time.Second), Before: Padding{Lines: 5}, After: Padding{Lines: 5}}, logline.Bound{}, logline.Bound{}},
		"before an open since": {Window{Before: Padding{Span: time.Second}}, logline.Bound{}, logline.Bound{}},
	} {
		c, err := firstPage(FetchRequest{Window: tc.w, PageFrom: PageFromFirst}, resolved)
		require.NoError(t, err, name)
		assert.Equal(t, tc.start, c.start(), name+": start")
		assert.Equal(t, tc.end, c.end(), name+": end")
	}

	// A continuation reads its window's end from the clock the first page pinned.
	w := resolvedWindow{Window: Window{After: Padding{Span: time.Minute}}, ResolvedAt: resolved}
	later := resolvedWindow{Window: w.Window, ResolvedAt: resolved.Add(time.Hour)}
	assert.Equal(t, to(time.Hour+time.Minute), w.end())
	assert.Equal(t, to(2*time.Hour+time.Minute), later.end())
}

// The range's own bounds tell its lines from the padding's: an open Until with
// After set ends just before the first page's clock.
func TestARangesBoundsSortPadding(t *testing.T) {
	t.Parallel()
	w := resolvedWindow{Window: Window{Since: moment(10 * time.Second), Until: moment(20 * time.Second), After: Padding{Lines: 1}}}
	assert.Equal(t, sideBefore, w.side(line("web-1", "app", 9*time.Second, "")))
	assert.Equal(t, sideRange, w.side(line("web-1", "app", 10*time.Second, "")))
	assert.Equal(t, sideRange, w.side(line("web-1", "app", 20*time.Second, "")))
	assert.Equal(t, sideAfter, w.side(line("web-1", "app", 21*time.Second, "")))

	open := resolvedWindow{Window: Window{After: Padding{Lines: 1}}, ResolvedAt: at(time.Hour)}
	assert.Equal(t, sideRange, open.side(line("web-1", "app", time.Hour-1, "")))
	assert.Equal(t, sideAfter, open.side(line("web-1", "app", time.Hour, "")))
	unbounded := resolvedWindow{Window: Window{}, ResolvedAt: at(time.Hour)}
	assert.Equal(t, sideRange, unbounded.side(line("web-1", "app", 2*time.Hour, "")))
}

// A first page pins a time or span start; a counted one waits for its read.
func TestAFirstPagePinsItsStart(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		w     Window
		start logline.Cursor
	}{
		"a time":  {Window{Since: moment(10 * time.Second)}, logline.MomentAt(at(10 * time.Second))},
		"a span":  {Window{Since: moment(10 * time.Second), Before: Padding{Span: time.Second}}, logline.MomentAt(at(9 * time.Second))},
		"a count": {Window{Since: moment(10 * time.Second), Before: Padding{Lines: 1}}, logline.Cursor{}},
		"open":    {Window{}, logline.Cursor{}},
	} {
		c, err := firstPage(FetchRequest{Window: tc.w, PageFrom: PageFromLast}, at(time.Hour))
		require.NoError(t, err, name)
		assert.Equal(t, tc.start, c.Start, name)
	}
}

// ===== The streams a page reads =====

// podSource is a view of the pod name in prod, reading containers, every one with
// none named and all set, previous when asked.
func podSource(name string, previous bool, containers ...string) Scope {
	return Scope{Sources: []ScopeSource{{Namespace: "prod", Kind: SourcePod, Name: name, Containers: containers, Previous: previous}}}
}

// idsOf is each record's instance.
func idsOf(recs []logline.Record) []string {
	out := []string{}
	for _, r := range recs {
		out = append(out, r.ContainerID)
	}
	return out
}

// thirdRun is the pod web-1 of web's replica set whose app container runs its
// third instance, the second before it.
func thirdRun() map[string]any {
	body := pod("web-1", "uid-web-1", "node-a", webReplicaSet(), container{name: "app", restarted: true}, container{name: "istio-proxy"})
	app := body["status"].(map[string]any)["containerStatuses"].([]any)[0].(map[string]any)
	app["containerID"] = containerID("uid-web-1", "app", 2)
	app["lastState"].(map[string]any)["terminated"].(map[string]any)["containerID"] = containerID("uid-web-1", "app", 1)
	return body
}

// earlier is web-1's app instance n as an earlier page filed it, started n minutes
// past 10:00, with one line at that moment saying "a<n>".
func earlier(t *testing.T, svc *Service, f *fakeService, n int) {
	t.Helper()
	row := rowOf("web-1", "app")
	row.ContainerID, row.StartedAt = containerID("uid-web-1", "app", n), at(time.Duration(n)*time.Minute).UnixNano()
	line := logstore.Line{Pos: logstore.Pos{TS: at(time.Duration(n) * time.Minute).UnixNano()}, Message: "a" + strconv.Itoa(n)}
	seed(t, svc, f, row, logstore.Fill{Lines: []logstore.Line{line}, AtBeginning: true, AtEnd: true})
}

// A deleted pod's streams, lines and place survive it: a view of the pod reads them
// from the file, and a node filter still finds them by the node they ran on.
func TestADeletedPodsLinesSurvive(t *testing.T) {
	t.Parallel()
	s := newLogServer()
	s.Add("prod/web-1/app", logstest.LineEvery(3)...)
	svc, f := serviceOver(t, webMirror(t), s)
	_, err := svc.Fetch(t.Context(), "7", FetchRequest{Scope: web1Scope, PageFrom: PageFromFirst, PageSize: 10})
	require.NoError(t, err)

	f.store.Delete(t, pod("web-1", "uid-web-1", "node-a", webReplicaSet(), container{name: "app"}, container{name: "istio-proxy"}))
	reads := len(s.Requests())
	view := web1Scope
	view.Filters = []ScopeFilter{{Field: FilterZone, Values: []string{"eu-1a"}}}
	page, err := svc.Fetch(t.Context(), "7", FetchRequest{Scope: view, PageFrom: PageFromFirst, PageSize: 10})

	require.NoError(t, err)
	assert.Equal(t, []string{"0", "1", "2"}, messages(page.Records))
	assert.Equal(t, logline.Place{Node: "node-a", Region: "eu", Zone: "eu-1a", OS: "linux", Arch: "amd64"}, page.Records[0].Place)
	assert.True(t, page.Records[0].Stream.Previous, "a gone pod runs no instance")
	assert.Nil(t, page.Page.Next)
	assert.Len(t, s.Requests(), reads, "no backend serves a gone pod")

	view.Filters = []ScopeFilter{{Field: FilterZone, Values: []string{"eu-1b"}}}
	page, err = svc.Fetch(t.Context(), "7", FetchRequest{Scope: view, PageFrom: PageFromFirst, PageSize: 10})
	require.NoError(t, err)
	assert.Empty(t, page.Records)
}

// A pod of a deleted pod's name with another UID is another pod: a view of it reads
// its own streams alone.
func TestAPodOfTheSameNameIsAnotherPod(t *testing.T) {
	t.Parallel()
	s := newLogServer()
	s.Add("prod/web-1/app", logstest.Stamped(0, "old"))
	svc, f := serviceOver(t, webMirror(t), s)
	_, err := svc.Fetch(t.Context(), "7", FetchRequest{Scope: web1Scope, PageFrom: PageFromFirst, PageSize: 10})
	require.NoError(t, err)

	f.store.Delete(t, pod("web-1", "uid-web-1", "node-a", webReplicaSet(), container{name: "app"}))
	f.store.Apply(t, pod("web-1", "uid-web-1b", "node-a", webReplicaSet(), container{name: "app"}))
	s.Clear("prod/web-1/app")
	s.Add("prod/web-1/app", logstest.Stamped(time.Minute, "new"))
	page, err := svc.Fetch(t.Context(), "7", FetchRequest{Scope: web1Scope, PageFrom: PageFromFirst, PageSize: 10})

	require.NoError(t, err)
	assert.Equal(t, []string{"new"}, messages(page.Records))
	assert.Equal(t, []string{containerID("uid-web-1b", "app", 0)}, idsOf(page.Records))
}

// A plain view reads the running instance from the kubelet and the instances before
// it from the file, with no fill; a view with previous reads the last from the
// kubelet and the older ones from the file; a gone pod reads the same rows either
// way.
func TestAPlainViewReadsEarlierInstancesFromTheFile(t *testing.T) {
	t.Parallel()
	s := newLogServer()
	s.Add("prod/web-1/app", logstest.Stamped(2*time.Minute, "a2"))
	s.Add("prod/web-1/app/previous", logstest.Stamped(time.Minute, "a1"))
	f := newMirror(t, append([]map[string]any{thirdRun()}, webNodes()...)...)
	svc, _ := serviceOver(t, f, s)
	earlier(t, svc, f, 0)

	page, err := svc.Fetch(t.Context(), "7", FetchRequest{Scope: web1Scope, PageFrom: PageFromFirst, PageSize: 10})
	require.NoError(t, err)
	assert.Equal(t, []string{"a0", "a2"}, messages(page.Records))
	assert.Empty(t, s.RequestsOf("prod/web-1/app/previous"))

	page, err = svc.Fetch(t.Context(), "7", FetchRequest{Scope: podSource("web-1", true, "app"), PageFrom: PageFromFirst, PageSize: 10})
	require.NoError(t, err)
	assert.Equal(t, []string{"a0", "a1"}, messages(page.Records))
	assert.Equal(t, []bool{true, true}, []bool{page.Records[0].Stream.Previous, page.Records[1].Stream.Previous})

	f.store.Delete(t, thirdRun())
	for _, previous := range []bool{false, true} {
		page, err = svc.Fetch(t.Context(), "7", FetchRequest{Scope: podSource("web-1", previous, "app"), PageFrom: PageFromFirst, PageSize: 10})
		require.NoError(t, err)
		assert.Equal(t, []string{"a0", "a1", "a2"}, messages(page.Records), previous)
	}
}

// A view naming no container of a gone pod reads the rows marked its default.
func TestADefaultViewOfAGonePodReadsItsDefault(t *testing.T) {
	t.Parallel()
	svc, f := serviceOver(t, webMirror(t), newLogServer())
	seed(t, svc, f, rowOf("old-1", "app"), logstore.Fill{Lines: fileLines(0)})
	seed(t, svc, f, rowOf("old-1", "istio-proxy"), logstore.Fill{Lines: fileLines(time.Second)})

	page, err := svc.Fetch(t.Context(), "7", FetchRequest{Scope: podSource("old-1", false), PageFrom: PageFromFirst, PageSize: 10})

	require.NoError(t, err)
	assert.Equal(t, []string{"0"}, messages(page.Records))
	assert.Equal(t, "app", page.Records[0].Stream.Container)
}

// The prompt's two-source view, a pod named plain and with previous, reads the
// current and the last instance once each from the kubelet and each older one once
// from the file: every line goes out once, and each stream answers once about more.
func TestTheTwoSourceViewReadsEachInstanceOnce(t *testing.T) {
	t.Parallel()
	s := newLogServer()
	s.Add("prod/web-1/app", logstest.Stamped(2*time.Minute, "a2"))
	s.Add("prod/web-1/app/previous", logstest.Stamped(time.Minute, "a1"))
	f := newMirror(t, append([]map[string]any{thirdRun()}, webNodes()...)...)
	svc, _ := serviceOver(t, f, s)
	earlier(t, svc, f, 0)
	view := podSource("web-1", false, "app")
	view.Sources = append(view.Sources, podSource("web-1", true, "app").Sources...)

	page, err := svc.Fetch(t.Context(), "7", FetchRequest{Scope: view, PageFrom: PageFromFirst, PageSize: 10})

	require.NoError(t, err)
	assert.Equal(t, []string{"a0", "a1", "a2"}, messages(page.Records))
	assert.Len(t, s.RequestsOf("prod/web-1/app"), 1)
	assert.Len(t, s.RequestsOf("prod/web-1/app/previous"), 1)
	assert.Nil(t, page.Page.Next)
}

// A container's file rows beside the resolve's are its newest maxInstances, by when
// each started: a pod that crash-looped for days holds hundreds.
func TestAContainersFileRowsAreCapped(t *testing.T) {
	t.Parallel()
	svc, f := serviceOver(t, webMirror(t), newLogServer())
	for n := range maxInstances + 2 {
		earlier(t, svc, f, n)
	}
	f.store.Delete(t, pod("web-1", "uid-web-1", "node-a", webReplicaSet(), container{name: "app"}))

	page, err := svc.Fetch(t.Context(), "7", FetchRequest{Scope: web1Scope, PageFrom: PageFromFirst, PageSize: 100})

	require.NoError(t, err)
	assert.Equal(t, []string{"a2", "a3", "a4", "a5", "a6", "a7", "a8", "a9"}, messages(page.Records))
}

// An instance read as the current one and, after a restart, as the previous one is
// one row with one copy of each line.
func TestAnInstanceReadAsCurrentThenPreviousHasOneRow(t *testing.T) {
	t.Parallel()
	s := newLogServer()
	s.Add("prod/web-1/app", logstest.Stamped(0, "a0"))
	svc, f := serviceOver(t, webMirror(t), s)
	_, err := svc.Fetch(t.Context(), "7", FetchRequest{Scope: web1Scope, PageFrom: PageFromFirst, PageSize: 10})
	require.NoError(t, err)

	restartWeb1(t, f, s, logstest.Stamped(time.Minute, "a1"))
	page, err := svc.Fetch(t.Context(), "7", FetchRequest{Scope: podSource("web-1", true, "app"), PageFrom: PageFromFirst, PageSize: 10})

	require.NoError(t, err)
	assert.Equal(t, []string{"a0"}, messages(page.Records))
	rows, err := fileOf(t, svc, f).PodStreams(t.Context(), "prod", "web-1", "")
	require.NoError(t, err)
	assert.Len(t, rows, 1, "the file holds instance 0 alone: nothing read instance 1 yet")
	assert.Len(t, chunksOf(t, svc, f, "web-1", "app"), 1)
}

// A view naming no container of a pod the mirror holds reads the pod's default as
// the mirror names it now: a container the file marked default before the pod's
// annotation moved is not read, and once the pod is gone, the file's mark has moved
// with it.
func TestADefaultViewFollowsTheLivePodsDefault(t *testing.T) {
	t.Parallel()
	s := newLogServer()
	s.Add("prod/web-1/app", logstest.Stamped(0, "app"))
	s.Add("prod/web-1/istio-proxy", logstest.Stamped(time.Second, "proxy"))
	svc, f := serviceOver(t, webMirror(t), s)
	page, err := svc.Fetch(t.Context(), "7", FetchRequest{Scope: podSource("web-1", false), PageFrom: PageFromFirst, PageSize: 10})
	require.NoError(t, err)
	require.Equal(t, []string{"app"}, messages(page.Records))

	moved := pod("web-1", "uid-web-1", "node-a", webReplicaSet(), container{name: "app"}, container{name: "istio-proxy"})
	moved["metadata"].(map[string]any)["annotations"] = map[string]any{DefaultContainerAnnotation: "istio-proxy"}
	f.store.Apply(t, moved)
	page, err = svc.Fetch(t.Context(), "7", FetchRequest{Scope: podSource("web-1", false), PageFrom: PageFromFirst, PageSize: 10})
	require.NoError(t, err)
	assert.Equal(t, []string{"proxy"}, messages(page.Records))

	f.store.Delete(t, moved)
	page, err = svc.Fetch(t.Context(), "7", FetchRequest{Scope: podSource("web-1", false), PageFrom: PageFromFirst, PageSize: 10})
	require.NoError(t, err)
	assert.Equal(t, []string{"proxy"}, messages(page.Records), "the gone pod")
}

// The node filters run over a held pod's file rows by where the pod runs now: a node
// relabelled since the rows were filed leaves them out as it leaves the pod out.
func TestAHeldPodsFileRowsAreFilteredByItsPlaceNow(t *testing.T) {
	t.Parallel()
	s := newLogServer()
	s.Add("prod/web-1/app", logstest.Stamped(0, "a0"))
	f := newMirror(t, append([]map[string]any{thirdRun()}, webNodes()...)...)
	svc, _ := serviceOver(t, f, s)
	earlier(t, svc, f, 0)

	f.store.Apply(t, node("node-a", "eu", "eu-1b", "linux", "amd64"))
	view := web1Scope
	view.Filters = []ScopeFilter{{Field: FilterZone, Values: []string{"eu-1a"}}}
	page, err := svc.Fetch(t.Context(), "7", FetchRequest{Scope: view, PageFrom: PageFromFirst, PageSize: 10})

	require.NoError(t, err)
	assert.Empty(t, page.Records)
}

// A filter on a node label over a mirror with no nodes is refused only when the file
// holds nothing the filter keeps: a gone pod's rows carry the place they ran in.
func TestAGonePodIsFilteredWithNoNodesLeft(t *testing.T) {
	t.Parallel()
	f := newMirror(t)
	f.store.Mirror(t, "v1", "Pod")
	svc, _ := serviceOver(t, f, newLogServer())
	seed(t, svc, f, rowOf("old-1", "app"), logstore.Fill{Lines: fileLines(0)})
	view := podSource("old-1", false)
	view.Filters = []ScopeFilter{{Field: FilterZone, Values: []string{"eu-1a"}}}

	page, err := svc.Fetch(t.Context(), "7", FetchRequest{Scope: view, PageFrom: PageFromFirst, PageSize: 10})
	require.NoError(t, err)
	assert.Equal(t, []string{"0"}, messages(page.Records))

	view.Filters[0].Values = []string{"eu-1b"}
	_, err = svc.Fetch(t.Context(), "7", FetchRequest{Scope: view, PageFrom: PageFromFirst, PageSize: 10})
	var noNodes refusal.NoNodes
	assert.ErrorAs(t, err, &noNodes)
}

// A restart the mirror reports late files the new instance's first lines under the
// old one, since the check passed on the lagging mirror; once a resolve sets the old
// instance's finished_at, the trim takes them from it, and each instance's lines
// are under its own row, every line once.
func TestARestartTheMirrorReportsLateIsTrimmed(t *testing.T) {
	t.Parallel()
	s := newLogServer()
	s.Add("prod/web-1/app", logstest.Stamped(0, "a0"), logstest.Stamped(time.Second, "a1"))
	svc, f := serviceOver(t, webMirror(t), s)
	s.Mu.Lock()
	s.Logs["prod/web-1/app/previous"] = s.Logs["prod/web-1/app"]
	s.Logs["prod/web-1/app"] = []string{logstest.Stamped(6*time.Second, "b6"), logstest.Stamped(7*time.Second, "b7")}
	s.Mu.Unlock()

	page, err := svc.Fetch(t.Context(), "7", FetchRequest{Scope: web1Scope, PageFrom: PageFromFirst, PageSize: 10})
	require.NoError(t, err)
	assert.Equal(t, []string{"b6", "b7"}, messages(page.Records), "filed under the instance the mirror still names")

	f.store.Apply(t, restartedWeb1())
	previous := web1Scope
	previous.Sources = []ScopeSource{{Namespace: "prod", Kind: SourcePod, Name: "web-1", Containers: []string{"app"}, Previous: true}}
	page, err = svc.Fetch(t.Context(), "7", FetchRequest{Scope: previous, PageFrom: PageFromFirst, PageSize: 10})
	require.NoError(t, err)
	assert.Equal(t, []string{"a0", "a1"}, messages(page.Records))

	page, err = svc.Fetch(t.Context(), "7", FetchRequest{Scope: web1Scope, PageFrom: PageFromFirst, PageSize: 10})
	require.NoError(t, err)
	assert.Equal(t, []string{"a0", "a1", "b6", "b7"}, messages(page.Records))
	var ids []string
	for _, rec := range page.Records {
		ids = append(ids, rec.ContainerID)
	}
	a, b := containerID("uid-web-1", "app", 0), containerID("uid-web-1", "app", 1)
	assert.Equal(t, []string{a, a, b, b}, ids)
}

// A restart the mirror reports late is trimmed even when no view reads the
// previous instance: a plain view's resolve learns when the instance before the
// running one finished, and the file's row of it is trimmed before it is read.
func TestALateRestartIsTrimmedUnderAPlainView(t *testing.T) {
	t.Parallel()
	s := newLogServer()
	s.Add("prod/web-1/app", logstest.Stamped(0, "a0"), logstest.Stamped(time.Second, "a1"))
	svc, f := serviceOver(t, webMirror(t), s)
	s.Mu.Lock()
	s.Logs["prod/web-1/app/previous"] = s.Logs["prod/web-1/app"]
	s.Logs["prod/web-1/app"] = []string{logstest.Stamped(6*time.Second, "b6"), logstest.Stamped(7*time.Second, "b7")}
	s.Mu.Unlock()
	_, err := svc.Fetch(t.Context(), "7", FetchRequest{Scope: web1Scope, PageFrom: PageFromFirst, PageSize: 10})
	require.NoError(t, err)

	f.store.Apply(t, restartedWeb1())
	page, err := svc.Fetch(t.Context(), "7", FetchRequest{Scope: web1Scope, PageFrom: PageFromFirst, PageSize: 10})

	require.NoError(t, err)
	assert.Equal(t, []string{"b6", "b7"}, messages(page.Records))
	b := containerID("uid-web-1", "app", 1)
	assert.Equal(t, []string{b, b}, idsOf(page.Records))
}

// A page labels a stream as its view reads it, as the tail does: a waiting
// container's log is the instance before, which a view with no Previous reads as the
// container's own, and one with Previous as its previous instance.
func TestAPageLabelsAWaitingContainerAsItsViewReadsIt(t *testing.T) {
	t.Parallel()
	s := newLogServer()
	s.Add("prod/crash/app", logstest.Stamped(0, "boom"))
	s.Add("prod/crash/app/previous", logstest.Stamped(0, "boom"))
	f := newMirror(t, append(webObjects(), waiting(pod("crash", "uid-crash", "node-a", nil, container{name: "app", restarted: true})))...)
	svc, _ := serviceOver(t, f, s)

	for _, previous := range []bool{false, true} {
		page, err := svc.Fetch(t.Context(), "7", FetchRequest{Scope: podSource("crash", previous, "app"), PageFrom: PageFromFirst, PageSize: 10})
		require.NoError(t, err)
		require.Equal(t, []string{"boom"}, messages(page.Records))
		assert.Equal(t, previous, page.Records[0].Stream.Previous)
	}
}

// Each node filter reads a gone pod's rows by the place it ran in, and a page past
// the last line of a gone pod is empty, not refused.
func TestEachNodeFilterReadsAGonePodsPlace(t *testing.T) {
	t.Parallel()
	s := newLogServer()
	s.Add("prod/web-1/app", logstest.LineEvery(3)...)
	svc, f := serviceOver(t, webMirror(t), s)
	_, err := svc.Fetch(t.Context(), "7", FetchRequest{Scope: web1Scope, PageFrom: PageFromFirst, PageSize: 10})
	require.NoError(t, err)
	f.store.Delete(t, pod("web-1", "uid-web-1", "node-a", webReplicaSet(), container{name: "app"}, container{name: "istio-proxy"}))

	for field, values := range map[FilterField][2]string{
		FilterNode:   {"node-a", "node-b"},
		FilterRegion: {"eu", "us"},
		FilterOS:     {"linux", "windows"},
		FilterArch:   {"amd64", "arm64"},
	} {
		view := web1Scope
		view.Filters = []ScopeFilter{{Field: field, Values: []string{values[0]}}}
		page, err := svc.Fetch(t.Context(), "7", FetchRequest{Scope: view, PageFrom: PageFromFirst, PageSize: 10})
		require.NoError(t, err, field)
		assert.Equal(t, []string{"0", "1", "2"}, messages(page.Records), field)

		view.Filters[0].Values = []string{values[1]}
		page, err = svc.Fetch(t.Context(), "7", FetchRequest{Scope: view, PageFrom: PageFromFirst, PageSize: 10})
		require.NoError(t, err, field)
		assert.Empty(t, page.Records, field)
	}

	page, err := svc.Fetch(t.Context(), "7", FetchRequest{Scope: web1Scope, PageFrom: PageFromFirst, PageSize: 10})
	require.NoError(t, err)
	page, err = svc.Fetch(t.Context(), "7", FetchRequest{Cursor: page.Page.Follow, PageSize: 10})
	require.NoError(t, err)
	assert.Empty(t, page.Records)
	assert.Empty(t, skippedOf(page))
}

// ===== The filler =====

// A page over a range the file does not cover fills it from the kubelet: the lines
// it read are in the file afterwards, under a chunk claiming them.
func TestAPageOverAGapFillsIt(t *testing.T) {
	t.Parallel()
	s := newLogServer()
	s.Add("prod/web-1/app", logstest.LineEvery(5)...)
	svc, f := serviceOver(t, webMirror(t), s)

	page, err := svc.Fetch(t.Context(), "7", FetchRequest{Scope: web1Scope, PageFrom: PageFromFirst, PageSize: 10})

	require.NoError(t, err)
	assert.Equal(t, []string{"0", "1", "2", "3", "4"}, messages(page.Records))
	chunks := chunksOf(t, svc, f, "web-1", "app")
	require.Len(t, chunks, 1)
	assert.Equal(t, logstore.Pos{TS: at(0).UnixNano()}, chunks[0].From)
	assert.Equal(t, logstore.Pos{TS: at(4 * time.Second).UnixNano()}, chunks[0].To)
	assert.True(t, chunks[0].AtBeginning, "a forward read from the start begins at the kubelet's oldest line")
}

// A second page over a range the first filled reads it from the file and asks the
// kubelet nothing.
func TestASecondPageOverTheSameRangeMakesNoRead(t *testing.T) {
	t.Parallel()
	s := newLogServer()
	s.Add("prod/web-1/app", logstest.LineEvery(10)...)
	svc, _ := serviceOver(t, webMirror(t), s)
	q := FetchRequest{Scope: web1Scope, PageFrom: PageFromFirst, PageSize: 3}

	first, err := svc.Fetch(t.Context(), "7", q)
	require.NoError(t, err)
	reads := len(s.Requests())
	second, err := svc.Fetch(t.Context(), "7", q)

	require.NoError(t, err)
	assert.Equal(t, first, second)
	assert.Equal(t, []string{"0", "1", "2"}, messages(second.Records))
	assert.Len(t, s.Requests(), reads)
}

// A sparse grep on a Tail page fills back until the stream holds limit plus one
// matches, and pages on from there; one whose budget runs out first answers the honest
// short page, cut where its reads reached with more to read, and each page moves.
func TestASparseGrepFillsBackUntilItHasAPage(t *testing.T) {
	t.Parallel()
	s := newLogServer()
	var log []string
	for i := range 200 {
		msg := "line " + strconv.Itoa(i)
		if i%50 == 0 {
			msg = "match " + strconv.Itoa(i)
		}
		log = append(log, logstest.Stamped(time.Duration(i)*time.Second, msg))
	}
	s.Add("prod/web-1/app", log...)
	svc, _ := serviceOver(t, webMirror(t), s)
	view := web1Scope
	view.Grep = "^match"

	page, err := svc.Fetch(t.Context(), "7", FetchRequest{Scope: view, PageFrom: PageFromLast, PageSize: 2})
	require.NoError(t, err)
	assert.Equal(t, []string{"match 100", "match 150"}, messages(page.Records))
	assert.NotNil(t, page.Page.Prev)
	assert.Equal(t, []string{"match 0", "match 50", "match 100", "match 150"}, recordsOf(pages(t, svc, view, PageFromLast, 2), PageFromLast))

	s = newLogServer()
	s.Add("prod/web-1/app", logstest.LineEvery(1000)...)
	svc, f := serviceOver(t, webMirror(t), s)
	f.chunk(64)
	svc.caps = capsOf(4 << 10)
	view.Grep = "never"
	page, err = svc.Fetch(t.Context(), "7", FetchRequest{Scope: view, PageFrom: PageFromLast, PageSize: 2})
	require.NoError(t, err)
	assert.Empty(t, page.Records)
	assert.NotNil(t, page.Page.Prev)
	next, err := svc.Fetch(t.Context(), "7", FetchRequest{Cursor: page.Page.Prev, PageSize: 2})
	require.NoError(t, err)
	assert.NotNil(t, next.Page.Prev)
	assert.NotEqual(t, *page.Page.Prev, *next.Page.Prev, "reading on moves")
}

// A fill over a stream the file holds in pieces counts each span's lines as it goes,
// and stops filling once the stream holds a page past the cursor: the first gap is
// read, the second is not.
func TestAFillCountsEverySpanBetweenItsGaps(t *testing.T) {
	t.Parallel()
	s := newLogServer()
	s.Add("prod/web-1/app", logstest.LineEvery(10)...)
	svc, f := serviceOver(t, webMirror(t), s)
	seed(t, svc, f, rowOf("web-1", "app"),
		logstore.Fill{Lines: fileLines(0, time.Second), AtBeginning: true},
		logstore.Fill{Lines: fileLines(4*time.Second, 5*time.Second)},
		logstore.Fill{Lines: fileLines(8*time.Second, 9*time.Second)})

	page, err := svc.Fetch(t.Context(), "7", FetchRequest{Scope: web1Scope, PageFrom: PageFromFirst, PageSize: 3})

	require.NoError(t, err)
	assert.Equal(t, []string{"0", "1", "2"}, messages(page.Records))
	require.Len(t, s.Requests(), 1, "the first gap alone")
	assert.Equal(t, "2026-10-08T10:00:01Z", s.Requests()[0].Get("sinceTime"))
}

// outsideReading is a backend that reads through next and notes whether any read
// started while a reading of f's mirror ran.
type outsideReading struct {
	next   kubelet.Backend
	f      *fakeService
	inside atomic.Bool
}

func (b *outsideReading) note() { b.inside.Store(b.inside.Load() || b.f.reading.Load() > 0) }

func (b *outsideReading) Forward(ctx context.Context, st kubelet.Log, o kubelet.Options) *kubelet.Read {
	b.note()
	return b.next.Forward(ctx, st, o)
}

func (b *outsideReading) Backward(ctx context.Context, st kubelet.Log, o kubelet.Options) *kubelet.Read {
	b.note()
	return b.next.Backward(ctx, st, o)
}

// The fills run outside the mirror's reading, which holds one of the mirror's
// connections: a page reads the mirror once to resolve, and once to check each window
// it writes, here one per gap.
func TestTheFillRunsOutsideTheMirrorReading(t *testing.T) {
	t.Parallel()
	s := newLogServer()
	s.Add("prod/web-1/app", logstest.LineEvery(10)...)
	svc, f := serviceOver(t, webMirror(t), s)
	seed(t, svc, f, rowOf("web-1", "app"),
		logstore.Fill{Lines: fileLines(0), AtBeginning: true},
		logstore.Fill{Lines: fileLines(4 * time.Second)},
		logstore.Fill{Lines: fileLines(8 * time.Second)})
	backend := &outsideReading{f: f}
	svc.backends = []namedBackend{{"kubelet", func(get kubelet.Getter) kubelet.Backend {
		backend.next = kubelet.New(get)
		return backend
	}}}
	before := f.readings.Load()

	page, err := svc.Fetch(t.Context(), "7", FetchRequest{Scope: web1Scope, PageFrom: PageFromFirst, PageSize: 100})

	require.NoError(t, err)
	assert.Equal(t, messages(linesOf(logstest.LineEvery(10))), messages(page.Records))
	assert.Len(t, s.Requests(), 3, "three gaps")
	assert.False(t, backend.inside.Load())
	assert.Equal(t, int32(4), f.readings.Load()-before, "the resolve and a check per gap")
}

// A restart that lands between the resolve and the read files nothing under the
// instance the resolve named, whose log the kubelet no longer serves: the page names
// the stream as skipped, and the next page resolves and reads the new instance.
func TestARestartBetweenTheResolveAndTheReadFilesNothing(t *testing.T) {
	t.Parallel()
	s := newLogServer()
	s.Add("prod/web-1/app", logstest.Stamped(0, "old"))
	svc, f := serviceOver(t, webMirror(t), s)
	// On web-1's own first read, since each stream checks its instance as it writes.
	var restart sync.Once
	s.OnRequest = func(n int) {
		s.Mu.Lock()
		key := s.Keys[n]
		s.Mu.Unlock()
		if key == "prod/web-1/app" {
			restart.Do(func() { restartWeb1(t, f, s, logstest.Stamped(time.Minute, "new")) })
		}
	}

	page, err := svc.Fetch(t.Context(), "7", FetchRequest{Scope: webScope, PageFrom: PageFromFirst, PageSize: 10})

	require.NoError(t, err)
	assert.Empty(t, page.Records)
	assert.Equal(t, []string{"web-1/app: restarted while reading"}, skippedOf(page))
	assert.Empty(t, chunksOf(t, svc, f, "web-1", "app"))

	page, err = svc.Fetch(t.Context(), "7", FetchRequest{Scope: webScope, PageFrom: PageFromFirst, PageSize: 10})
	require.NoError(t, err)
	assert.Equal(t, []string{"new"}, messages(page.Records))
	assert.Equal(t, containerID("uid-web-1", "app", 1), page.Records[0].ContainerID)
	assert.Empty(t, skippedOf(page))
}

// A finished instance read to its end has its chunk marked at_end, and no page reads
// it forward again: its live end is that chunk's last line with no read at all.
func TestAFinishedInstanceReadToItsEndIsNotReadAgain(t *testing.T) {
	t.Parallel()
	s := newLogServer()
	s.Add("prod/web-1/app", logstest.LineEvery(3)...)
	f := newMirror(t, append([]map[string]any{finished(pod("web-1", "uid-web-1", "node-a", nil, container{name: "app"}))}, webNodes()...)...)
	svc, _ := serviceOver(t, f, s)

	first, err := svc.Fetch(t.Context(), "7", FetchRequest{Scope: web1Scope, PageFrom: PageFromFirst, PageSize: 10})
	require.NoError(t, err)
	chunks := chunksOf(t, svc, f, "web-1", "app")
	require.Len(t, chunks, 1)
	assert.True(t, chunks[0].AtEnd)
	reads := len(s.Requests())

	for _, q := range []FetchRequest{{Scope: web1Scope, PageFrom: PageFromFirst}, {Scope: web1Scope, PageFrom: PageFromLast}, {Cursor: first.Page.Follow}} {
		page, err := svc.Fetch(t.Context(), "7", q)
		require.NoError(t, err)
		assert.NotNil(t, page.Page.Follow)
	}
	assert.Len(t, s.Requests(), reads)
}

// A forward fill is never cut before its first line in range, so a page whose
// stream spent its limits at once still answers a line, and the pages go on.
func TestAFillIsNeverCutBeforeItsFirstLine(t *testing.T) {
	t.Parallel()
	s := newLogServer()
	s.Add("prod/web-1/app", logstest.LineEvery(10)...)
	svc, f := serviceOver(t, webMirror(t), s)
	f.chunk(4)
	svc.caps = capsOf(8)

	got := pages(t, svc, web1Scope, PageFromFirst, 10)

	assert.Greater(t, len(got), 1)
	assert.Equal(t, messages(linesOf(logstest.LineEvery(10))), recordsOf(got, PageFromFirst))
}

// A line a grepped page's fill found and a plain page's found again is stored once:
// the kubelet's fills read every line whatever the grep, and a line's cursor is its
// key.
func TestALineFoundByAGreppedAndAPlainPageIsStoredOnce(t *testing.T) {
	t.Parallel()
	s := newLogServer()
	s.Add("prod/web-1/app", logstest.LineEvery(10)...)
	svc, f := serviceOver(t, webMirror(t), s)
	odd := web1Scope
	odd.Grep = "[13579]$"

	page, err := svc.Fetch(t.Context(), "7", FetchRequest{Scope: odd, PageFrom: PageFromFirst, PageSize: 2})
	require.NoError(t, err)
	assert.Equal(t, []string{"1", "3"}, messages(page.Records))
	page, err = svc.Fetch(t.Context(), "7", FetchRequest{Scope: web1Scope, PageFrom: PageFromFirst, PageSize: 100})
	require.NoError(t, err)
	assert.Equal(t, messages(linesOf(logstest.LineEvery(10))), messages(page.Records))

	var n int
	file := fileOf(t, svc, f)
	rows, err := file.PodStreams(t.Context(), "prod", "web-1", "")
	require.NoError(t, err)
	require.NoError(t, file.Read(t.Context(), func(r logstore.Reader) error {
		return r.Lines(rows[0].ID, logstore.Oldest, logstore.Newest, func(logstore.Line) bool {
			n++
			return true
		})
	}))
	assert.Equal(t, 10, n)
}

// With two backends in order, the second is asked only for what the first answered
// at the beginning for, older than the moment of the first's oldest line, which is the first's
// whole: the lines they both hold go out once. A Tail page answers nothing older
// only once the second is at the beginning too.
func TestTheSecondBackendIsAskedOnlyPastTheFirstsBeginning(t *testing.T) {
	t.Parallel()
	live := newLogServer()
	live.Add("prod/web-1/app", logstest.LineEvery(10)[5:]...)
	archive := newLogServer()
	archive.Add("prod/web-1/app", logstest.LineEvery(8)...)
	svc, _ := serviceOver(t, webMirror(t), live)
	svc.backends = append(svc.backends, namedBackend{"archive", func(kubelet.Getter) kubelet.Backend { return kubelet.New(archive.Get) }})

	page, err := svc.Fetch(t.Context(), "7", FetchRequest{Scope: web1Scope, PageFrom: PageFromLast, PageSize: 100})

	require.NoError(t, err)
	assert.Equal(t, messages(linesOf(logstest.LineEvery(10))), messages(page.Records))
	assert.Nil(t, page.Page.Prev)
	for _, q := range archive.Requests() {
		assert.NotContains(t, q.Get("sinceTime"), "10:00:05", "nothing from the live's moments")
	}

	live = newLogServer()
	live.Add("prod/web-1/app", logstest.LineEvery(10)[5:]...)
	archive.SetRefusal("prod/web-1/app", &logstest.Denial{Code: 503, Message: "archive down"})
	svc, _ = serviceOver(t, webMirror(t), live)
	svc.backends = append(svc.backends, namedBackend{"archive", func(kubelet.Getter) kubelet.Backend { return kubelet.New(archive.Get) }})
	page, err = svc.Fetch(t.Context(), "7", FetchRequest{Scope: web1Scope, PageFrom: PageFromLast, PageSize: 100})
	require.NoError(t, err)
	assert.Equal(t, []string{"5", "6", "7", "8", "9"}, messages(page.Records))
	assert.Equal(t, *lineCursorFor("web-1", "app", 5*time.Second, 0), atOf(t, page.Page.Prev))
}

// A fill the page cut short, having read enough before it reached the chunk past its
// gap, leaves that seam a live gap: the next page fills the rest of it, and no line
// is lost.
func TestAFillCutShortLeavesItsSeamALiveGap(t *testing.T) {
	t.Parallel()
	s := newLogServer()
	s.Add("prod/web-1/app", logstest.LineEvery(10)...)
	svc, f := serviceOver(t, webMirror(t), s)
	var later []time.Duration
	for i := 5; i < 10; i++ {
		later = append(later, time.Duration(i)*time.Second)
	}
	seed(t, svc, f, rowOf("web-1", "app"), logstore.Fill{Lines: fileLines(later...)})

	assert.Equal(t, messages(linesOf(logstest.LineEvery(10))), recordsOf(pages(t, svc, web1Scope, PageFromFirst, 2), PageFromFirst))
}

// A gap the first backend has rotated away is offered to the next before anything
// links across it: with the file holding 0 and 1, the live 5 to 9 and an archive
// every line, a Head page reads 2 to 4 from the archive.
func TestARotatedGapGoesToTheNextBackend(t *testing.T) {
	t.Parallel()
	live := newLogServer()
	live.Add("prod/web-1/app", logstest.LineEvery(10)[5:]...)
	archive := newLogServer()
	archive.Add("prod/web-1/app", logstest.LineEvery(10)...)
	svc, f := serviceOver(t, webMirror(t), live)
	svc.backends = append(svc.backends, namedBackend{"archive", func(kubelet.Getter) kubelet.Backend { return kubelet.New(archive.Get) }})
	seed(t, svc, f, rowOf("web-1", "app"), logstore.Fill{Lines: fileLines(0, time.Second)})

	page, err := svc.Fetch(t.Context(), "7", FetchRequest{Scope: web1Scope, PageFrom: PageFromFirst, PageSize: 100})

	require.NoError(t, err)
	assert.Equal(t, messages(linesOf(logstest.LineEvery(10))), messages(page.Records))
}

// A Tail read the page cut before it reached the chunk below its gap offers nothing
// to the next backend: it stopped because it had enough, not because the first
// backend lacks what lies under it.
func TestATailReadCutByThePageOffersNothingToTheNextBackend(t *testing.T) {
	t.Parallel()
	live := newLogServer()
	live.Add("prod/web-1/app", logstest.LineEvery(10)...)
	archive := newLogServer()
	archive.Add("prod/web-1/app", logstest.LineEvery(10)...)
	svc, f := serviceOver(t, webMirror(t), live)
	svc.backends = append(svc.backends, namedBackend{"archive", func(kubelet.Getter) kubelet.Backend { return kubelet.New(archive.Get) }})
	seed(t, svc, f, rowOf("web-1", "app"), logstore.Fill{Lines: fileLines(0, time.Second)})

	page, err := svc.Fetch(t.Context(), "7", FetchRequest{Scope: web1Scope, PageFrom: PageFromLast, PageSize: 3})

	require.NoError(t, err)
	assert.Equal(t, []string{"7", "8", "9"}, messages(page.Records))
	assert.Empty(t, archive.Requests())
}

// A fill counts only the lines the page's chunks hold, as the walk keeps only those:
// a line another pattern's chunk filed between two chunks of one span is not the
// page's, so the fill reads on past the span for the page's third line.
func TestAFillCountsOnlyTheLinesItsChunksHold(t *testing.T) {
	t.Parallel()
	s := newLogServer()
	for i, msg := range []string{"x0", "xy1", "xy2", "xy3", "y4", "x5", "x6", "x7", "x8"} {
		s.Add("prod/web-1/app", logstest.Stamped(time.Duration(i)*time.Second, msg))
	}
	svc, f := serviceOver(t, webMirror(t), s)
	lineAt := func(d time.Duration, msg string) logstore.Line {
		return logstore.Line{Pos: logstore.Pos{TS: at(d).UnixNano()}, Message: msg}
	}
	first := lineAt(0, "x0")
	seed(t, svc, f, rowOf("web-1", "app"),
		logstore.Fill{Pattern: "x", Lines: []logstore.Line{first}, AtBeginning: true},
		logstore.Fill{Pattern: "y", Lines: []logstore.Line{lineAt(time.Second, "xy1"), lineAt(2*time.Second, "xy2"), lineAt(3*time.Second, "xy3")}},
		logstore.Fill{Pattern: "x", Lines: []logstore.Line{lineAt(5*time.Second, "x5")}, Prev: &first.Pos})
	view := web1Scope
	view.Grep = "x"

	page, err := svc.Fetch(t.Context(), "7", FetchRequest{Scope: view, PageFrom: PageFromFirst, PageSize: 3})

	require.NoError(t, err)
	assert.Equal(t, []string{"x0", "x5", "x6"}, messages(page.Records))
}

// A first page's range read and its padding read back from Since join at Since:
// the forward read continues the chunk the backward read ends, so the file holds
// one span and a page across Since asks the kubelet nothing.
func TestAFirstPagesTwoReadsJoin(t *testing.T) {
	t.Parallel()
	s := newLogServer()
	s.Add("prod/web-1/app", logstest.LineEvery(10)...)
	svc, f := serviceOver(t, webMirror(t), s)

	_, err := svc.Fetch(t.Context(), "7", aroundAt(web1Scope, 5*time.Second, 2))
	require.NoError(t, err)
	assert.Len(t, spansOf(t, svc, f), 1)

	reads := len(s.Requests())
	page, err := svc.Fetch(t.Context(), "7", FetchRequest{Scope: web1Scope, Seen: seenAt("web-1", "app", 3*time.Second), PageFrom: PageFromFirst, PageSize: 2})
	require.NoError(t, err)
	assert.Equal(t, []string{"4", "5"}, messages(page.Records))
	assert.Len(t, s.Requests(), reads)
}

// A first page with padding before Since fills its forward read first, and the
// padding read reads back from Since at the rate of the lines the forward read
// read: here four seconds for eleven lines at four a second. The reads join.
func TestAFirstPageFillsItsForwardReadFirst(t *testing.T) {
	t.Parallel()
	s, want := quarterly()
	svc, f := serviceOver(t, webMirror(t), s)

	page, err := svc.Fetch(t.Context(), "7", aroundAt(web1Scope, 7*time.Second, 10))

	require.NoError(t, err)
	assert.Equal(t, want[18:38], messages(page.Records))
	requests := s.Requests()
	require.Len(t, requests, 3)
	assert.Equal(t, "2026-10-08T10:00:07Z", requests[0].Get("sinceTime"), "the forward half")
	assert.False(t, requests[0].Has("limitBytes"))
	assert.Equal(t, "2026-10-08T10:00:03Z", requests[2].Get("sinceTime"), "a window sized from the forward half")
	assert.Equal(t, []logstore.Span{{From: quarter(12), To: quarter(40)}}, spansOf(t, svc, f))
}

// A first page's two reads join when its forward read reads its gap whole and finds
// no line, and when the file already holds Since, so the forward read reads nothing.
func TestAFirstPagesReadsJoinWhateverItsForwardReadFound(t *testing.T) {
	t.Parallel()
	t.Run("nothing after the moment", func(t *testing.T) {
		t.Parallel()
		s := newLogServer()
		s.Add("prod/web-1/app", logstest.LineEvery(5)...)
		s.Add("prod/web-1/app", logstest.Stamped(9*time.Second, "9"), logstest.Stamped(9500*time.Millisecond, "9.5"), logstest.Stamped(10*time.Second, "10"))
		svc, f := serviceOver(t, webMirror(t), s)
		seed(t, svc, f, rowOf("web-1", "app"), logstore.Fill{Lines: []logstore.Line{said(9, "9"), {Pos: logstore.Pos{TS: at(9500 * time.Millisecond).UnixNano()}, Message: "9.5"}, said(10, "10")}})

		page, err := svc.Fetch(t.Context(), "7", aroundAt(web1Scope, 6*time.Second, 2))

		require.NoError(t, err)
		assert.Equal(t, []string{"3", "4", "9", "9.5"}, messages(page.Records))
		assert.Equal(t, []logstore.Span{{From: sec(0), To: sec(10)}}, spansOf(t, svc, f))
	})
	t.Run("the moment in the file", func(t *testing.T) {
		t.Parallel()
		s := newLogServer()
		s.Add("prod/web-1/app", logstest.LineEvery(11)...)
		svc, f := serviceOver(t, webMirror(t), s)
		seed(t, svc, f, rowOf("web-1", "app"), logstore.Fill{Lines: fileLines(5*time.Second, 6*time.Second, 7*time.Second, 8*time.Second)})

		page, err := svc.Fetch(t.Context(), "7", aroundAt(web1Scope, 6*time.Second, 2))

		require.NoError(t, err)
		assert.Equal(t, []string{"4", "5", "6", "7"}, messages(page.Records))
		for _, q := range s.Requests() {
			assert.NotEqual(t, "2026-10-08T10:00:06Z", q.Get("sinceTime"), "no forward read")
		}
		assert.Equal(t, []logstore.Span{{From: sec(0), To: sec(10)}}, spansOf(t, svc, f))
	})
}

// The seam is the forward read's first line, which its first batch writes: with a
// read written two lines a batch, the reads still join, and no line is lost or read
// twice.
func TestAFirstPagesSeamIsItsForwardReadsFirstLine(t *testing.T) {
	t.Parallel()
	s := newLogServer()
	var want []string
	for i := range 40 {
		s.Add("prod/web-1/app", logstest.Stamped(time.Duration(i)*250*time.Millisecond, strconv.Itoa(i)))
		want = append(want, strconv.Itoa(i))
	}
	ret := testRetention
	ret.WriteBatch = 2
	svc, f := serviceIn(t, webMirror(t), s, t.TempDir(), ret)

	page, err := svc.Fetch(t.Context(), "7", aroundAt(web1Scope, 7*time.Second, 10))

	require.NoError(t, err)
	assert.Equal(t, want[18:38], messages(page.Records))
	assert.Len(t, spansOf(t, svc, f), 1)
	assert.Equal(t, want, messages(walk(t, svc, FetchRequest{Scope: web1Scope, PageFrom: PageFromFirst, PageSize: 5})))
}

// A restart one read of a first page finds skips the stream in both reads, though
// the other read's write found none: a page answers once for each stream.
func TestARestartOnAFirstPageSkipsTheStreamInBothReads(t *testing.T) {
	t.Parallel()
	s := newLogServer()
	s.Add("prod/web-1/app", logstest.LineEvery(10)...)
	s.Add("prod/web-2/app", logstest.Stamped(4*time.Second, "web-2"))
	svc, f := serviceOver(t, webMirror(t), s)
	// The Head half's read of web-1, from the moment's second, which tails the Tail
	// half's write.
	s.OnRequest = func(n int) {
		s.Mu.Lock()
		key, since := s.Keys[n], s.Queries[n].Get("sinceTime")
		s.Mu.Unlock()
		if key == "prod/web-1/app" && since == "2026-10-08T10:00:05Z" {
			restartWeb1(t, f, s)
		}
	}

	page, err := svc.Fetch(t.Context(), "7", aroundAt(webScope, 5*time.Second, 2))

	require.NoError(t, err)
	assert.Equal(t, []string{"web-2"}, messages(page.Records))
	assert.Equal(t, []string{"web-1/app: restarted while reading"}, skippedOf(page))
}

// A page whose check of the instances it read fails is the failure, and writes
// nothing.
func TestAFailedInstanceCheckFailsThePage(t *testing.T) {
	t.Parallel()
	s := newLogServer()
	s.Add("prod/web-1/app", logstest.Stamped(0, "a"))
	svc, f := serviceOver(t, webMirror(t), s)
	f.queryErr, f.queryErrOn = errors.New("db closed"), "SELECT p.uid, coalesce(c.container_id"

	_, err := svc.Fetch(t.Context(), "7", FetchRequest{Scope: webScope, PageFrom: PageFromFirst, PageSize: 10})

	assert.EqualError(t, err, "db closed")
	assert.Empty(t, chunksOf(t, svc, f, "web-1", "app"))
}

// A gap a tailer's start cuts in two keeps its place among the spans.
func TestSegmentsKeepOrderAcrossACutGap(t *testing.T) {
	t.Parallel()
	pos := func(s int64) logstore.Pos { return logstore.Pos{TS: s} }
	start := pos(10)
	chunks := []logstore.Chunk{{From: pos(0), To: pos(5)}, {From: pos(20), To: pos(25)}}
	var edges []string
	for _, seg := range segmentsFrom(logstore.CoverageOf(chunks, []string{"kubelet"}, &start), logstore.Oldest, logline.Newer) {
		if seg.span != nil {
			edges = append(edges, fmt.Sprintf("span %d", seg.span.From.TS))
		} else {
			edges = append(edges, fmt.Sprintf("gap tailed=%v", seg.gap.Tailed))
		}
	}
	assert.Equal(t, []string{"gap tailed=false", "span 0", "gap tailed=false", "gap tailed=true", "span 20", "gap tailed=true"}, edges)
}

// A backward read leaves a hole below its oldest line only when it continued a
// chunk: never with no chunk to continue, and from a chunk, when a whole read found
// nothing or its oldest line lies past the chunk's second.
func TestAHoleIsJudgedByWhatTheReadContinued(t *testing.T) {
	t.Parallel()
	prev := logstore.Pos{TS: at(time.Second).UnixNano()}
	oldest := logstore.Pos{TS: at(3 * time.Second).UnixNano()}
	for name, c := range map[string]struct {
		oldest *logstore.Pos
		whole  bool
		prev   *logstore.Pos
		want   bool
	}{
		"no chunk":           {oldest: &oldest, whole: true, want: false},
		"nothing read whole": {whole: true, prev: &prev, want: true},
		"nothing read short": {prev: &prev, want: false},
		"whole":              {oldest: &oldest, whole: true, prev: &prev, want: true},
		"short":              {oldest: &oldest, prev: &prev, want: false},
		"down to prev":       {oldest: &logstore.Pos{TS: prev.TS, Ord: 1}, whole: true, prev: &prev, want: false},
	} {
		assert.Equal(t, c.want, hole(c.oldest, c.whole, c.prev), name)
	}
}

// A forward read from a chunk's last line whose GET began past it found that line
// rotated away: the chunk it writes is at the beginning and links to nothing, so the file keeps
// a lost gap between the two.
func TestARotatedForwardReadLinksToNothing(t *testing.T) {
	t.Parallel()
	s := newLogServer()
	s.Add("prod/web-1/app", logstest.LineEvery(10)[5:]...)
	svc, f := serviceOver(t, webMirror(t), s)
	seed(t, svc, f, rowOf("web-1", "app"), logstore.Fill{Lines: fileLines(0, time.Second)})

	page, err := svc.Fetch(t.Context(), "7", FetchRequest{Scope: web1Scope, PageFrom: PageFromFirst, PageSize: 100})

	require.NoError(t, err)
	assert.Equal(t, []string{"0", "1", "5", "6", "7", "8", "9"}, messages(page.Records))
	chunks := chunksOf(t, svc, f, "web-1", "app")
	require.Len(t, chunks, 2)
	assert.True(t, chunks[1].AtBeginning)
	assert.Nil(t, chunks[1].Prev)
	cov := logstore.CoverageOf(chunks, []string{"kubelet"}, nil)
	require.Len(t, cov.Spans, 2)
	assert.True(t, cov.Gaps[1].Lost)
}

// A forward read whose GET returned its chunk's last line links to it as before: the
// container was quiet, not rotated.
func TestAQuietForwardReadLinks(t *testing.T) {
	t.Parallel()
	s := newLogServer()
	s.Add("prod/web-1/app", logstest.LineEvery(10)[1:]...)
	svc, f := serviceOver(t, webMirror(t), s)
	seed(t, svc, f, rowOf("web-1", "app"), logstore.Fill{Lines: fileLines(0, time.Second)})

	_, err := svc.Fetch(t.Context(), "7", FetchRequest{Scope: web1Scope, PageFrom: PageFromFirst, PageSize: 100})

	require.NoError(t, err)
	assert.Len(t, spansOf(t, svc, f), 1)
}

// A forward read that found nothing between two chunks, its GET beginning past the
// older one's end, marks the newer one at the beginning and links nothing.
func TestARotatedReadWithNoLineMarksTheNextChunkAtTheBeginning(t *testing.T) {
	t.Parallel()
	s := newLogServer()
	s.Add("prod/web-1/app", logstest.LineEvery(10)[8:]...)
	svc, f := serviceOver(t, webMirror(t), s)
	seed(t, svc, f, rowOf("web-1", "app"), logstore.Fill{Lines: fileLines(0, time.Second)},
		logstore.Fill{Lines: fileLines(8*time.Second, 9*time.Second)})

	_, err := svc.Fetch(t.Context(), "7", FetchRequest{Scope: web1Scope, PageFrom: PageFromFirst, PageSize: 100})

	require.NoError(t, err)
	chunks := chunksOf(t, svc, f, "web-1", "app")
	require.Len(t, chunks, 2)
	assert.True(t, chunks[1].AtBeginning)
	assert.Nil(t, chunks[1].Prev)
	assert.True(t, logstore.CoverageOf(chunks, []string{"kubelet"}, nil).Gaps[1].Lost)
}

// A Tail page's backward read that found nothing between two chunks, the kubelet's
// first line past the newer one's start or its log empty, marks the newer one at the
// beginning and links nothing.
func TestARotatedBackwardReadWithNoLineMarksTheNextChunkAtTheBeginning(t *testing.T) {
	t.Parallel()
	for name, kept := range map[string][]string{"rotated past": logstest.LineEvery(10)[9:], "emptied": nil} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			s := newLogServer()
			s.Add("prod/web-1/app", kept...)
			svc, f := serviceOver(t, webMirror(t), s)
			seed(t, svc, f, rowOf("web-1", "app"), logstore.Fill{Lines: fileLines(0, time.Second)},
				logstore.Fill{Lines: fileLines(8*time.Second, 8*time.Second)})

			_, err := svc.Fetch(t.Context(), "7", FetchRequest{Scope: web1Scope, PageFrom: PageFromLast, PageSize: 100})

			require.NoError(t, err)
			chunks := chunksOf(t, svc, f, "web-1", "app")
			require.GreaterOrEqual(t, len(chunks), 2)
			assert.True(t, chunks[1].AtBeginning)
			assert.Nil(t, chunks[1].Prev)
			assert.True(t, logstore.CoverageOf(chunks, []string{"kubelet"}, nil).Gaps[1].Lost)
		})
	}
}

// A forward read is at the beginning when it has no lower bound, or when it read from prev's
// second, returned no line at or before prev, and got a line or ended clean. A read
// from any other bound, as from a page's key, says nothing of rotation.
func TestAReadAtTheBeginningIsJudgedByPrev(t *testing.T) {
	t.Parallel()
	prev := &logstore.Pos{TS: at(time.Second).UnixNano()}
	key := &logstore.Pos{TS: at(2 * time.Second).UnixNano()}
	for name, c := range map[string]struct {
		lower, prev *logstore.Pos
		began       time.Time
		proved      bool
		want        bool
	}{
		"no lower bound":      {began: at(3 * time.Second), proved: true, want: true},
		"no lower bound, cut": {want: false},
		"prev gone":           {lower: prev, prev: prev, began: at(3 * time.Second), proved: true, want: true},
		"prev returned":       {lower: prev, prev: prev, began: at(time.Second), proved: true, want: false},
		"nothing, clean":      {lower: prev, prev: prev, proved: true, want: true},
		"nothing, failed":     {lower: prev, prev: prev, want: false},
		"from the page's key": {lower: key, prev: prev, began: at(3 * time.Second), proved: true, want: false},
		"no chunk":            {lower: key, began: at(3 * time.Second), proved: true, want: false},
	} {
		f := &filler{}
		assert.Equal(t, c.want, f.reachedBeginning(c.lower, c.prev, c.began, c.proved), name)
	}
}

// A Tail page's backward read that reaches the kubelet's first line above the chunk
// below its gap writes an at-beginning chunk linked to nothing, as a rotated forward read does.
func TestARotatedBackwardReadLinksToNothing(t *testing.T) {
	t.Parallel()
	s := newLogServer()
	s.Add("prod/web-1/app", logstest.LineEvery(10)[5:]...)
	svc, f := serviceOver(t, webMirror(t), s)
	seed(t, svc, f, rowOf("web-1", "app"), logstore.Fill{Lines: fileLines(0, time.Second)})

	page, err := svc.Fetch(t.Context(), "7", FetchRequest{Scope: web1Scope, PageFrom: PageFromLast, PageSize: 100})

	require.NoError(t, err)
	assert.Equal(t, []string{"0", "1", "5", "6", "7", "8", "9"}, messages(page.Records))
	assert.Equal(t, []logstore.Span{{From: sec(0), To: sec(1)}, {From: sec(5), To: sec(9)}}, spansOf(t, svc, f))
	chunks := chunksOf(t, svc, f, "web-1", "app")
	assert.True(t, chunks[1].AtBeginning)
	assert.Nil(t, chunks[1].Prev)
	assert.True(t, logstore.CoverageOf(chunks, []string{"kubelet"}, nil).Gaps[1].Lost)
}

// scripted is a Backend whose every read hands over items, each line stamped with
// the stream as fakeBackend stamps it, then ends with err, clean when nil. Its first
// line is the GET's oldest, which it tells onOldest. With a gate, it waits for the
// gate to close before the item at hold. After each window's end it asks more, as a
// backward read does, and ends clean on a no; with unasked, it never asks.
type scripted struct {
	items   []kubelet.Item
	err     error
	hold    int
	gate    <-chan struct{}
	unasked bool
}

// windows is the items of a backward read of ws, each window oldest first, each
// followed by its end.
func windows(ws ...[]logline.Record) []kubelet.Item {
	var out []kubelet.Item
	for _, w := range ws {
		for _, rec := range w {
			out = append(out, kubelet.Item{Record: rec})
		}
		out = append(out, kubelet.Item{WindowEnd: true})
	}
	return out
}

func (b *scripted) read(ctx context.Context, st kubelet.Log, o kubelet.Options) *kubelet.Read {
	ctx, cancel := context.WithCancel(ctx)
	out, lines := spsc.New[kubelet.Item](0)
	r := &kubelet.Read{Key: st.Key, Lines: lines, Cancel: cancel}
	go func() {
		defer out.Close()
		defer func() { r.Err = b.err }()
		for i, it := range b.items {
			if b.gate != nil && i == b.hold {
				select {
				case <-b.gate:
				case <-ctx.Done():
					return
				}
			}
			if !it.WindowEnd && !it.WindowCut {
				it.Stream, it.Place, it.ContainerID = st.Key, st.Place, st.Instance
				if r.Oldest.IsZero() {
					r.Oldest = it.Timestamp
					if o.OnOldest != nil {
						o.OnOldest(it.Timestamp)
					}
				}
			}
			if out.SendContext(ctx, it) != nil {
				return
			}
			if it.WindowEnd && i < len(b.items)-1 && !b.unasked && o.More != nil && !o.More(ctx) {
				return
			}
		}
	}()
	return r
}

func (b *scripted) Forward(ctx context.Context, st kubelet.Log, o kubelet.Options) *kubelet.Read {
	return b.read(ctx, st, o)
}

func (b *scripted) Backward(ctx context.Context, st kubelet.Log, o kubelet.Options) *kubelet.Read {
	return b.read(ctx, st, o)
}

// web1Lines is a line of web-1's app at each moment d past 10:00, saying its whole
// seconds.
func web1Lines(ds ...time.Duration) []logline.Record {
	var out []logline.Record
	for _, d := range ds {
		out = append(out, line("web-1", "app", d, strconv.Itoa(int(d/time.Second))))
	}
	return out
}

// A backward read writes each window once the next one starts, before the read has
// ended, so a page holds about two windows of a stream, never all of them.
func TestAWindowIsWrittenWhenItCompletes(t *testing.T) {
	t.Parallel()
	gate := make(chan struct{})
	backend := &scripted{items: windows(web1Lines(7*time.Second, 8*time.Second, 9*time.Second), web1Lines(4*time.Second, 5*time.Second, 6*time.Second)), hold: 5, gate: gate}
	svc, f := serviceOn(t, backend)
	row := seed(t, svc, f, rowOf("web-1", "app"))
	committed := pinged(t, svc, f, row)
	done := make(chan FetchResponse, 1)
	go func() {
		page, err := svc.Fetch(t.Context(), "7", FetchRequest{Scope: web1Scope, PageFrom: PageFromLast, PageSize: 10})
		assert.NoError(t, err)
		done <- page
	}()

	committed()
	chunks := chunksOf(t, svc, f, "web-1", "app")
	require.Len(t, chunks, 1, "the first window, while the second is read")
	assert.Equal(t, sec(7), chunks[0].From)
	assert.Equal(t, sec(9), chunks[0].To)
	close(gate)

	page := testutil.Recv(t, done, "the page")
	assert.Equal(t, []string{"4", "5", "6", "7", "8", "9"}, messages(page.Records))
}

// An answer to more the read never took is replaced by the next window's, so a read
// that goes on without asking is not held up at its next window's end.
func TestAnUntakenAnswerIsReplaced(t *testing.T) {
	t.Parallel()
	backend := &scripted{items: windows(web1Lines(7*time.Second, 8*time.Second, 9*time.Second), web1Lines(4*time.Second, 5*time.Second, 6*time.Second)), unasked: true}
	svc, f := serviceOn(t, backend)
	seed(t, svc, f, rowOf("web-1", "app"))

	page, err := svc.Fetch(t.Context(), "7", FetchRequest{Scope: web1Scope, PageFrom: PageFromLast, PageSize: 10})

	require.NoError(t, err)
	assert.Equal(t, []string{"4", "5", "6", "7", "8", "9"}, messages(page.Records))
}

// A backward window goes to the file a batch at a time as its lines arrive. Its
// first batch is in the file before the window ends, linked up to nothing; the batch
// holding its last line links it up to the chunk at the point.
func TestAWindowIsWrittenAsItArrives(t *testing.T) {
	t.Parallel()
	gate := make(chan struct{})
	backend := &scripted{items: windows(web1Lines(4*time.Second, 5*time.Second, 6*time.Second, 7*time.Second, 8*time.Second)), hold: 3, gate: gate}
	svc, f := serviceBatched(t, backend, 2)
	row := seed(t, svc, f, rowOf("web-1", "app"), logstore.Fill{Lines: fileLines(10 * time.Second)})
	committed := pinged(t, svc, f, row)
	done := make(chan FetchResponse, 1)
	go func() {
		page, err := svc.Fetch(t.Context(), "7", prevFrom(web1Scope, "web-1", "app", 10*time.Second, 10))
		assert.NoError(t, err)
		done <- page
	}()

	committed()
	assert.Equal(t, []logstore.Span{{From: sec(4), To: sec(5)}, {From: sec(10), To: sec(10)}}, spansOf(t, svc, f), "the first batch, while the window is read")
	close(gate)

	page := testutil.Recv(t, done, "the page")
	assert.Equal(t, []string{"4", "5", "6", "7", "8"}, messages(page.Records))
	assert.Equal(t, []logstore.Span{{From: sec(4), To: sec(10)}}, spansOf(t, svc, f))
}

// A forward read goes to the file a batch at a time too, each batch linked down as
// it lands: a page holds one batch of it, never the whole read. The second batch's
// first line writes the first batch, and the GET pauses after it.
func TestAForwardReadIsWrittenAsItArrives(t *testing.T) {
	t.Parallel()
	s := newLogServer()
	s.Add("prod/web-1/app", logstest.LineEvery(7)[1:]...)
	gate := make(chan struct{})
	svc, f := serviceBatched(t, kubelet.New((&logstest.Pauser{N: 4, Gate: gate}).On(s.Get)), 2)
	row := seed(t, svc, f, rowOf("web-1", "app"), logstore.Fill{Lines: fileLines(time.Second), AtBeginning: true})
	committed := pinged(t, svc, f, row)
	done := make(chan FetchResponse, 1)
	go func() {
		page, err := svc.Fetch(t.Context(), "7", FetchRequest{Scope: web1Scope, Seen: seenAt("web-1", "app", time.Second), PageFrom: PageFromFirst, PageSize: 10})
		assert.NoError(t, err)
		done <- page
	}()

	committed()
	assert.Equal(t, []logstore.Span{{From: sec(1), To: sec(3)}}, spansOf(t, svc, f), "the first batch, linked down, while the read goes on")
	close(gate)

	page := testutil.Recv(t, done, "the page")
	assert.Equal(t, []string{"2", "3", "4", "5", "6"}, messages(page.Records))
	assert.Equal(t, []logstore.Span{{From: sec(1), To: sec(6)}}, spansOf(t, svc, f))
}

// spansOf is the spans of the file's chunks of web-1's app.
func spansOf(t *testing.T, svc *Service, f *fakeService) []logstore.Span {
	t.Helper()
	return logstore.CoverageOf(chunksOf(t, svc, f, "web-1", "app"), kubeletOnly, nil).Spans
}

// A window stopped part way is an island: written linked to nothing, its kept lines
// not counted toward the page, and the window that replaces it links up to the chunk
// at the point. The page still needs a line, so the read goes on to an older window.
func TestAStoppedWindowIsAnIsland(t *testing.T) {
	t.Parallel()
	items := windows(web1Lines(time.Second, 2*time.Second, 3*time.Second))
	items[len(items)-1] = kubelet.Item{WindowCut: true}
	items = append(items, windows(web1Lines(8*time.Second, 9*time.Second), web1Lines(5*time.Second))...)
	svc, f := serviceOn(t, &scripted{items: items})
	seed(t, svc, f, rowOf("web-1", "app"), logstore.Fill{Lines: fileLines(10 * time.Second)})

	page, err := svc.Fetch(t.Context(), "7", prevFrom(web1Scope, "web-1", "app", 10*time.Second, 2))

	require.NoError(t, err)
	assert.Equal(t, []string{"8", "9"}, messages(page.Records))
	assert.Equal(t, []logstore.Span{{From: sec(1), To: sec(3)}, {From: sec(5), To: sec(10)}}, spansOf(t, svc, f))
}

// A backward read's windows join: each links up to the oldest line written before it,
// the first to the chunk at the point, so the file holds one span.
func TestWindowsJoin(t *testing.T) {
	t.Parallel()
	backend := &scripted{items: windows(
		web1Lines(7*time.Second, 8*time.Second, 9*time.Second),
		web1Lines(4*time.Second, 5*time.Second, 6*time.Second),
		web1Lines(time.Second, 2*time.Second, 3*time.Second))}
	svc, f := serviceOn(t, backend)
	seed(t, svc, f, rowOf("web-1", "app"), logstore.Fill{Lines: fileLines(10 * time.Second)})

	page, err := svc.Fetch(t.Context(), "7", prevFrom(web1Scope, "web-1", "app", 10*time.Second, 20))

	require.NoError(t, err)
	assert.Equal(t, []string{"1", "2", "3", "4", "5", "6", "7", "8", "9"}, messages(page.Records))
	assert.Equal(t, []logstore.Span{{From: sec(1), To: sec(10)}}, spansOf(t, svc, f))
}

// A window with no line writes nothing, and the window older than it links up over it.
func TestAnEmptyWindowIsLinkedOver(t *testing.T) {
	t.Parallel()
	backend := &scripted{items: windows(
		web1Lines(7*time.Second, 8*time.Second, 9*time.Second),
		web1Lines(),
		web1Lines(time.Second, 2*time.Second, 3*time.Second))}
	svc, f := serviceOn(t, backend)

	_, err := svc.Fetch(t.Context(), "7", FetchRequest{Scope: web1Scope, PageFrom: PageFromLast, PageSize: 20})

	require.NoError(t, err)
	assert.Equal(t, []logstore.Span{{From: sec(1), To: sec(9)}}, spansOf(t, svc, f))
}

// A Tail page from the end of a finished instance marks the newest chunk it wrote
// at_end, from the read's first window, whether older windows follow it or the page
// fills in it; a Head page to the end then asks the kubelet nothing.
func TestTheFirstWindowOfAFinishedInstanceIsAtEnd(t *testing.T) {
	t.Parallel()
	for name, limit := range map[string]int{"older windows follow": 20, "filled by it": 5} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			s := newLogServer()
			s.Add("prod/web-1/app", logstest.LineEvery(30)...)
			f := newMirror(t, append([]map[string]any{finished(pod("web-1", "uid-web-1", "node-a", nil, container{name: "app"}))}, webNodes()...)...)
			svc, _ := serviceOver(t, f, s)

			page, err := svc.Fetch(t.Context(), "7", FetchRequest{Scope: web1Scope, PageFrom: PageFromLast, PageSize: limit})
			require.NoError(t, err)
			chunks := chunksOf(t, svc, f, "web-1", "app")
			newest := chunks[len(chunks)-1]
			assert.Equal(t, sec(29), newest.To)
			assert.True(t, newest.AtEnd)

			reads := len(s.Requests())
			_, err = svc.Fetch(t.Context(), "7", FetchRequest{Cursor: page.Page.Follow, PageSize: 10})
			require.NoError(t, err)
			assert.Len(t, s.Requests(), reads)
		})
	}
}

// quarter is the line cursor of the line i quarters of a second past 10:00.
func quarter(i int) logstore.Pos {
	return logstore.Pos{TS: at(time.Duration(i) * 250 * time.Millisecond).UnixNano()}
}

// quarterly is a server whose web-1/app logs a line a quarter of a second from 10:00
// to 10:00:10, each saying its index, and those lines' messages.
func quarterly() (*logstest.Server, []string) {
	s := newLogServer()
	var want []string
	for i := range 41 {
		s.Add("prod/web-1/app", logstest.Stamped(time.Duration(i)*250*time.Millisecond, strconv.Itoa(i)))
		want = append(want, strconv.Itoa(i))
	}
	return s, want
}

// pointChunk is the chunk at 10:00:10 a page over quarterly reads back from: ten lines
// a tenth of a second apart, the first saying 40, busy enough that the window before
// it is the last second.
func pointChunk() logstore.Fill {
	lines := []logstore.Line{{Pos: sec(10), Message: "40"}}
	for i := 1; i < 10; i++ {
		lines = append(lines, logstore.Line{Pos: logstore.Pos{TS: at(10*time.Second + time.Duration(i)*100*time.Millisecond).UnixNano()}, Message: "40." + strconv.Itoa(i)})
	}
	return logstore.Fill{Lines: lines}
}

// pointEnd is the last line of pointChunk.
var pointEnd = logstore.Pos{TS: at(10*time.Second + 900*time.Millisecond).UnixNano()}

// A window cut part way is an island, linked to nothing, with a live gap each side: no
// read goes on from it, so a stream whose first window was cut read nothing in range.
// A later read links to it from either side, and no line is lost at the cursor or
// stored twice: the next page reads the lines nearest the cursor down to the island,
// and a forward read from the island's last line continues its chunk.
func TestACutWindowIsAnIsland(t *testing.T) {
	t.Parallel()
	for _, mode := range []logline.Direction{logline.Older, logline.Newer} {
		t.Run(string(mode), func(t *testing.T) {
			t.Parallel()
			s, want := quarterly()
			svc, f := serviceOver(t, webMirror(t), s)
			seed(t, svc, f, rowOf("web-1", "app"), pointChunk())
			// The first window's request, after the one that finds the first line.
			var broke atomic.Bool
			s.OnRequest = func(n int) {
				if n == 1 && !broke.Swap(true) {
					s.BreakNext(2)
				}
			}

			_, err := svc.Fetch(t.Context(), "7", prevFrom(web1Scope, "web-1", "app", 10*time.Second, 4))
			assert.ErrorContains(t, err, "no container of this view could be read: web-1/app")
			assert.Equal(t, []logstore.Span{{From: quarter(36), To: quarter(37)}, {From: sec(10), To: pointEnd}}, spansOf(t, svc, f))
			gaps := logstore.CoverageOf(chunksOf(t, svc, f, "web-1", "app"), kubeletOnly, nil).Gaps
			require.Len(t, gaps, 3)
			assert.True(t, gaps[0].Live() && gaps[1].Live(), "a live gap each side of the island")

			if mode == logline.Older {
				page, err := svc.Fetch(t.Context(), "7", prevFrom(web1Scope, "web-1", "app", 10*time.Second, 4))
				require.NoError(t, err)
				assert.Equal(t, want[36:40], messages(page.Records))
			} else {
				page, err := svc.Fetch(t.Context(), "7", FetchRequest{Scope: web1Scope, Seen: seenAt("web-1", "app", 37*250*time.Millisecond), PageFrom: PageFromFirst, PageSize: 2})
				require.NoError(t, err)
				assert.Equal(t, want[38:40], messages(page.Records))
			}
			assert.Equal(t, pointEnd, spansOf(t, svc, f)[0].To, "the island joined to the point")
			assert.Equal(t, want[:40], messages(walk(t, svc, prevFrom(web1Scope, "web-1", "app", 10*time.Second, 4))))
		})
	}
}

// A fill beside a page's half-written window, of the gap the window has not linked up
// yet, leaves one span with no line twice once the window lands: a Head page from the
// window's first batch reads on to the point while the Tail page's GET is paused.
func TestAFillBesideAHalfWrittenWindow(t *testing.T) {
	t.Parallel()
	s, want := quarterly()
	gate := make(chan struct{})
	ret := testRetention
	ret.WriteBatch = 2
	svc, f := serviceIn(t, webMirror(t), s, t.TempDir(), ret)
	get := (&logstest.Pauser{N: 3, Gate: gate}).On(s.Get)
	svc.backends = []namedBackend{{"kubelet", func(kubelet.Getter) kubelet.Backend { return kubelet.New(get) }}}
	row := seed(t, svc, f, rowOf("web-1", "app"), pointChunk())
	committed := pinged(t, svc, f, row)
	done := make(chan FetchResponse, 1)
	go func() {
		page, err := svc.Fetch(t.Context(), "7", prevFrom(web1Scope, "web-1", "app", 10*time.Second, 4))
		assert.NoError(t, err)
		done <- page
	}()

	committed()
	require.Equal(t, []logstore.Span{{From: quarter(36), To: quarter(37)}, {From: sec(10), To: pointEnd}}, spansOf(t, svc, f))
	beside, err := svc.Fetch(t.Context(), "7", FetchRequest{Scope: web1Scope, Seen: seenAt("web-1", "app", 37*250*time.Millisecond), PageFrom: PageFromFirst, PageSize: 10})
	require.NoError(t, err)
	assert.Equal(t, []string{"38", "39", "40", "40.1", "40.2", "40.3", "40.4", "40.5", "40.6", "40.7"}, messages(beside.Records))
	close(gate)

	page := testutil.Recv(t, done, "the page")
	assert.Equal(t, want[36:40], messages(page.Records))
	assert.Len(t, spansOf(t, svc, f), 1)
	assert.Equal(t, want[:40], messages(walk(t, svc, prevFrom(web1Scope, "web-1", "app", 10*time.Second, 4))))
}

// A first window cut part way is an island, which may not hold the log's last line:
// it is not marked at_end, so a Head page to the end still reads past it.
func TestACutFirstWindowIsNotAtEnd(t *testing.T) {
	t.Parallel()
	backend := &scripted{items: []kubelet.Item{{Record: line("web-1", "app", 7*time.Second, "7")}, {Record: line("web-1", "app", 8*time.Second, "8")}}, err: &kubelet.StatusError{Code: 503, Message: "unavailable"}}
	svc, f := serviceOn(t, backend)
	f.store.Apply(t, finished(pod("web-1", "uid-web-1", "node-a", nil, container{name: "app"})))

	_, err := svc.Fetch(t.Context(), "7", FetchRequest{Scope: web1Scope, PageFrom: PageFromLast, PageSize: 10})

	assert.EqualError(t, err, "no container of this view could be read: web-1/app: unavailable")
	chunks := chunksOf(t, svc, f, "web-1", "app")
	require.Len(t, chunks, 1)
	assert.Equal(t, sec(8), chunks[0].To)
	assert.False(t, chunks[0].AtEnd)
}

// A backward read is judged only between windows: the window that fills the page is
// read to its end and links whole, and no GET follows it. The chunk at the point is
// too small to measure a rate, so the window is ten seconds, twice the page.
func TestABackwardWindowIsReadToItsEnd(t *testing.T) {
	t.Parallel()
	s := newLogServer()
	s.Add("prod/web-1/app", logstest.LineEvery(11)...)
	svc, f := serviceOver(t, webMirror(t), s)
	seed(t, svc, f, rowOf("web-1", "app"), logstore.Fill{Lines: fileLines(10 * time.Second)})

	page, err := svc.Fetch(t.Context(), "7", prevFrom(web1Scope, "web-1", "app", 10*time.Second, 4))

	require.NoError(t, err)
	assert.Equal(t, []string{"6", "7", "8", "9"}, messages(page.Records))
	assert.Equal(t, []logstore.Span{{From: sec(0), To: sec(10)}}, spansOf(t, svc, f))
	assert.Len(t, s.Requests(), 2, "the first line and one window")
	assert.Equal(t, messages(linesOf(logstest.LineEvery(10))), messages(walk(t, svc, prevFrom(web1Scope, "web-1", "app", 10*time.Second, 4))))
}

// A read of several windows that reaches the log's first line marks the oldest chunk
// it wrote at the beginning.
func TestAReadOfSeveralWindowsToTheFirstLineIsAtTheBeginning(t *testing.T) {
	t.Parallel()
	s := newLogServer()
	s.Add("prod/web-1/app", logstest.LineEvery(30)...)
	svc, f := serviceOver(t, webMirror(t), s)
	view := web1Scope
	view.Grep = "never"

	_, err := svc.Fetch(t.Context(), "7", FetchRequest{Scope: view, PageFrom: PageFromLast, PageSize: 2})

	require.NoError(t, err)
	assert.Equal(t, []string{"10", ""}, logstest.Tails(s))
	chunks := chunksOf(t, svc, f, "web-1", "app")
	assert.Equal(t, sec(0), chunks[0].From)
	assert.True(t, chunks[0].AtBeginning)
	assert.Equal(t, []logstore.Span{{From: sec(0), To: sec(29)}}, spansOf(t, svc, f))
}

// A restart the mirror shows between two windows stops the read: the window written
// stays, the stream is skipped, here refusing a page of it alone, and no line of the
// read after it lands in the file.
func TestARestartBetweenWindowsStopsTheRead(t *testing.T) {
	t.Parallel()
	gate := make(chan struct{})
	backend := &scripted{items: windows(web1Lines(7*time.Second, 8*time.Second, 9*time.Second), web1Lines(4*time.Second, 5*time.Second, 6*time.Second)), hold: 5, gate: gate}
	svc, f := serviceOn(t, backend)
	row := seed(t, svc, f, rowOf("web-1", "app"))
	committed := pinged(t, svc, f, row)
	done := make(chan error, 1)
	go func() {
		_, err := svc.Fetch(t.Context(), "7", FetchRequest{Scope: web1Scope, PageFrom: PageFromLast, PageSize: 10})
		done <- err
	}()

	committed()
	f.store.Apply(t, pod("web-1", "uid-web-1", "node-a", webReplicaSet(), container{name: "app", restarted: true}, container{name: "istio-proxy"}))
	close(gate)

	assert.EqualError(t, testutil.Recv(t, done, "the page"), "no container of this view could be read: web-1/app: restarted while reading")
	chunks := chunksOf(t, svc, f, "web-1", "app")
	require.Len(t, chunks, 1)
	assert.Equal(t, sec(7), chunks[0].From)
	assert.Equal(t, sec(9), chunks[0].To)
}

// The stop between windows waits for the window's write: a restart that write finds
// ends the read before it sends another GET.
func TestARestartAtAWindowsEndSendsNoMoreGets(t *testing.T) {
	t.Parallel()
	s := newLogServer()
	s.Add("prod/web-1/app", logstest.LineEvery(30)...)
	svc, f := serviceOver(t, webMirror(t), s)
	// The first window's request, after the one that finds the first line.
	s.OnRequest = func(n int) {
		if n == 1 {
			f.store.Apply(t, pod("web-1", "uid-web-1", "node-a", webReplicaSet(), container{name: "app", restarted: true}, container{name: "istio-proxy"}))
		}
	}
	view := web1Scope
	view.Grep = "never"

	_, err := svc.Fetch(t.Context(), "7", FetchRequest{Scope: view, PageFrom: PageFromLast, PageSize: 2})

	assert.EqualError(t, err, "no container of this view could be read: web-1/app: restarted while reading")
	assert.Len(t, s.Requests(), 2)
}

// afterFill runs once a page has written every window of every stream, before its
// walk, however many windows it wrote.
func TestAfterFillRunsOncePerPage(t *testing.T) {
	t.Parallel()
	s := newLogServer()
	s.Add("prod/web-1/app", logstest.LineEvery(30)...)
	s.Add("prod/web-2/app", logstest.LineEvery(30)...)
	svc, _ := serviceOver(t, webMirror(t), s)
	var calls atomic.Int32
	svc.afterFill = func() { calls.Add(1) }
	view := webScope
	view.Grep = "never"

	_, err := svc.Fetch(t.Context(), "7", FetchRequest{Scope: view, PageFrom: PageFromLast, PageSize: 2})

	require.NoError(t, err)
	assert.Len(t, s.RequestsOf("prod/web-1/app"), 3, "the first line and two windows of each stream")
	assert.Equal(t, int32(1), calls.Load())
}

// A sparse grep scrolling up from a cursor sizes its windows from the bytes of the
// lines next to the point: under midCaps, three matches in a 200 KiB log take three
// 64 KiB windows and the first line's GET, and the same search again sends none.
func TestASparseGrepReadsInWindowsOfItsByteTarget(t *testing.T) {
	t.Parallel()
	s := newLogServer()
	pad := strings.Repeat("x", 70)
	var log []string
	for i := range 2000 {
		msg := pad
		if i%800 == 1 {
			msg = "match " + strconv.Itoa(i)
		}
		log = append(log, logstest.Stamped(time.Duration(i)*10*time.Millisecond, msg))
	}
	s.Add("prod/web-1/app", log...)
	svc, f := serviceOver(t, webMirror(t), s)
	svc.caps = midCaps
	var top []logstore.Line
	for _, rec := range linesOf(log[len(log)-200:]) {
		top = append(top, logstore.Line{Message: rec.Message})
	}
	for i := range top {
		top[i].Pos = logstore.Pos{TS: at(time.Duration(len(log)-200+i) * 10 * time.Millisecond).UnixNano()}
	}
	seed(t, svc, f, rowOf("web-1", "app"), logstore.Fill{Lines: top})
	view := web1Scope
	view.Grep = "^match"
	q := prevFrom(view, "web-1", "app", time.Duration(len(log)-200)*10*time.Millisecond, 3)

	page, err := svc.Fetch(t.Context(), "7", q)

	require.NoError(t, err)
	assert.Equal(t, []string{"match 1", "match 801", "match 1601"}, messages(page.Records))
	assert.Nil(t, page.Page.Prev)
	assert.Len(t, s.Requests(), 4, "the first line and three windows")
	reads := len(s.Requests())
	again, err := svc.Fetch(t.Context(), "7", q)
	require.NoError(t, err)
	assert.Equal(t, page.Records, again.Records)
	assert.Len(t, s.Requests(), reads, "searching again is free")
}

// A log quiet for a day, then logging 5,000 lines a second for two seconds before the
// point, pages back with lines on every page and no line lost or repeated, and a
// first page from the start of the burst with as much padding answers a full page
// each side.
func TestPagesReadBackThroughABurst(t *testing.T) {
	t.Parallel()
	s := newLogServer()
	var want []string
	add := func(d time.Duration, msg string) {
		s.Add("prod/web-1/app", logstest.Stamped(d, msg))
		want = append(want, msg)
	}
	for i := range 60 {
		add(time.Duration(i)*time.Minute, "quiet "+strconv.Itoa(i))
	}
	start := 24 * time.Hour
	for i := range 10_000 {
		add(start+time.Duration(i)*200*time.Microsecond, "loud "+strconv.Itoa(i))
	}
	s.Add("prod/web-1/app", logstest.Stamped(start+2*time.Second, "point"))
	svc, f := serviceOver(t, webMirror(t), s)
	seed(t, svc, f, rowOf("web-1", "app"), logstore.Fill{Lines: []logstore.Line{{Pos: logstore.Pos{TS: at(start + 2*time.Second).UnixNano()}, Message: "point"}}})

	q := prevFrom(web1Scope, "web-1", "app", start+2*time.Second, MaxPageSize)
	var got []string
	for {
		page, err := svc.Fetch(t.Context(), "7", q)
		require.NoError(t, err)
		require.NotEmpty(t, page.Records, "a page with lines")
		got = append(messages(page.Records), got...)
		if page.Page.Prev == nil {
			break
		}
		q = FetchRequest{Cursor: page.Page.Prev, PageSize: MaxPageSize}
	}
	assert.Equal(t, want, got)

	around, err := svc.Fetch(t.Context(), "7", aroundAt(web1Scope, start, 50))
	require.NoError(t, err)
	assert.Equal(t, want[10:110], messages(around.Records))
}

// A restart found as a stopped window's island is written ends the read there, and
// what was written before it stays.
func TestARestartAtAStoppedWindowStopsTheRead(t *testing.T) {
	t.Parallel()
	gate := make(chan struct{})
	items := windows(web1Lines(time.Second, 2*time.Second))
	items[len(items)-1] = kubelet.Item{WindowCut: true}
	items = append(items, windows(web1Lines(8*time.Second, 9*time.Second))...)
	svc, f := serviceBatched(t, &scripted{items: items, hold: 2, gate: gate}, 1)
	row := seed(t, svc, f, rowOf("web-1", "app"), logstore.Fill{Lines: fileLines(10 * time.Second)})
	committed := pinged(t, svc, f, row)
	done := make(chan error, 1)
	go func() {
		_, err := svc.Fetch(t.Context(), "7", prevFrom(web1Scope, "web-1", "app", 10*time.Second, 2))
		done <- err
	}()

	committed()
	f.store.Apply(t, pod("web-1", "uid-web-1", "node-a", webReplicaSet(), container{name: "app", restarted: true}, container{name: "istio-proxy"}))
	close(gate)

	assert.EqualError(t, testutil.Recv(t, done, "the page"), "no container of this view could be read: web-1/app: restarted while reading")
	assert.Equal(t, []logstore.Span{{From: sec(1), To: sec(1)}, {From: sec(10), To: sec(10)}}, spansOf(t, svc, f))
}

// A forward fill whose GET's first line lies past its gap read the gap whole: the
// backend holds none of it, though the GET stopped at a loud second. The page walks
// the lines the file holds before the loud second, and is cut at it.
func TestAFillWhoseGetBeginsPastItsGapReadsItWhole(t *testing.T) {
	t.Parallel()
	s := newLogServer()
	s.Add("prod/web-1/app", logstest.Stamped(9*time.Second, strings.Repeat("x", 2<<10)), logstest.Stamped(10*time.Second, "served"))
	svc, f := serviceOver(t, webMirror(t), s)
	svc.caps = smallCaps
	seed(t, svc, f, rowOf("web-1", "app"), logstore.Fill{Lines: fileLines(0)}, logstore.Fill{Lines: fileLines(5 * time.Second)})

	page, err := svc.Fetch(t.Context(), "7", FetchRequest{Scope: web1Scope, PageFrom: PageFromFirst, PageSize: 10})

	require.NoError(t, err)
	assert.Equal(t, []string{"0", "5"}, messages(page.Records))
	assert.Equal(t, logline.MomentAt(at(10*time.Second-1)).RunPlace, cursorIn(t, page.Page.Next).seen[name("web-1", "app")])
	assert.Len(t, skippedOf(page), 1)
}

// A fill reads no further gap once its stream has sent its GETs: a Head page from
// the start across eleven live gaps, a GET each, is cut at the last line before the
// fourth, and the next pages go on from there.
func TestAFillReadsNoGapPastItsGets(t *testing.T) {
	t.Parallel()
	s := newLogServer()
	s.Add("prod/web-1/app", logstest.LineEvery(20)...)
	svc, f := serviceOver(t, webMirror(t), s)
	svc.caps = capsOf(1 << 20)
	svc.caps.Gets = 3
	var cached []logstore.Fill
	for i := 0; i < 20; i += 2 {
		cached = append(cached, logstore.Fill{Lines: fileLines(time.Duration(i) * time.Second)})
	}
	seed(t, svc, f, rowOf("web-1", "app"), cached...)

	page, err := svc.Fetch(t.Context(), "7", FetchRequest{Scope: web1Scope, PageFrom: PageFromFirst, PageSize: 100})

	require.NoError(t, err)
	assert.Equal(t, []string{"0", "1", "2", "3", "4"}, messages(page.Records))
	assert.NotNil(t, page.Page.Next)
	assert.Len(t, s.Requests(), 3)
	got := pagesOn(t, svc, page, 100)
	assert.Equal(t, messages(linesOf(logstest.LineEvery(20))), recordsOf(got, PageFromFirst))
}

// ===== The walker =====

// The log cache's invariant, after any sequence of pages, appends, rotations,
// restarts, evictions and reads broken part way over a fake kubelet: every line a
// page answers is in a chunk that covers its pattern, and no chunk is missing a line
// the kubelet served into its range. It walks 100 seeds of 60 steps, 25 under
// -short, each in parallel.
func TestTheFileKeepsItsInvariant(t *testing.T) {
	t.Parallel()
	seeds := 100
	if testing.Short() {
		seeds = 25
	}
	for seed := range seeds {
		t.Run(strconv.Itoa(seed), func(t *testing.T) {
			t.Parallel()
			w := newWorld(t, rand.New(rand.NewSource(int64(seed))))
			for range 60 {
				w.step()
				w.checkChunks()
			}
		})
	}
}

// world is a deployment's pods as a walk drives them, and every line each instance
// of their app containers ever wrote.
type world struct {
	t       *testing.T
	rng     *rand.Rand
	s       *logstest.Server
	f       *fakeService
	svc     *Service
	pods    []*walkPod
	written map[string][]logline.Record
	cursors []*string
	seen    []string
}

// walkPod is one pod of the walk: its instances so far, the current one the last,
// and the moment of its current instance's newest line.
type walkPod struct {
	name string
	runs int
	last time.Duration
}

func newWorld(t *testing.T, rng *rand.Rand) *world {
	w := &world{t: t, rng: rng, s: newLogServer(), written: map[string][]logline.Record{}}
	objects := []map[string]any{
		workload("apps/v1", "Deployment", "web", "dep-web", nil),
		workload("apps/v1", "ReplicaSet", "web-7d9f", "rs-web", controller("apps/v1", "Deployment", "web", "dep-web")),
		node("node-a", "eu", "eu-1a", "linux", "amd64"),
	}
	for i := 1; i <= 1+rng.Intn(2); i++ {
		p := &walkPod{name: "web-" + strconv.Itoa(i), runs: 1}
		w.pods = append(w.pods, p)
		objects = append(objects, w.body(p))
	}
	// A budget a few pages' worth, with nothing kept for being read, so a sweep's own
	// pick runs inside the invariant.
	ret := testRetention
	ret.Bytes = int64(200 + rng.Intn(2000))
	ret.StreamShare, ret.ReadTTL = ret.Bytes/2, 0
	ret.WriteBatch, ret.WriteBatchBytes = 1+rng.Intn(4), int64(1+rng.Intn(64))
	w.svc, w.f = serviceIn(t, newMirror(t, objects...), w.s, t.TempDir(), ret)
	w.svc.caps = randomCaps(rng)
	w.f.chunk(1 + rng.Intn(64))
	return w
}

// id is the runtime id of p's instance n.
func (p *walkPod) id(n int) string { return containerID("uid-"+p.name, "app", n) }

// body is p as the mirror holds it: its app container running its current instance,
// the one before it terminated at the second of its last line.
func (w *world) body(p *walkPod) map[string]any {
	body := pod(p.name, "uid-"+p.name, "node-a", webReplicaSet(), container{name: "app"})
	status := body["status"].(map[string]any)["containerStatuses"].([]any)[0].(map[string]any)
	status["containerID"] = p.id(p.runs - 1)
	if p.runs > 1 {
		status["lastState"] = map[string]any{"terminated": map[string]any{
			"containerID": p.id(p.runs - 2), "finishedAt": at(p.last).Add(-time.Second).Format(time.RFC3339),
		}}
	}
	return body
}

// step does one random thing to the world, then checks what a page answered: a
// write, a rotation, a restart, an eviction by hand, a sweep, a page, or a page whose
// next read's body breaks part way, inside a window or between them.
func (w *world) step() {
	p := w.pods[w.rng.Intn(len(w.pods))]
	switch n := w.rng.Intn(11); {
	case n < 3:
		w.write(p)
	case n < 4:
		w.rotate(p)
	case n < 5:
		w.restart(p)
	case n < 6:
		w.evict()
	case n < 7:
		require.NoError(w.t, fileOf(w.t, w.svc, w.f).Sweep(w.t.Context()))
	case n < 8:
		w.s.BreakNext(w.rng.Intn(6))
		w.page()
	default:
		w.page()
	}
}

// write appends one to eight lines to p's current instance, at moments that often
// tie and sometimes fall inside a second.
func (w *world) write(p *walkPod) {
	id := p.id(p.runs - 1)
	for range 1 + w.rng.Intn(8) {
		p.last += []time.Duration{0, 0, 250 * time.Millisecond, time.Second, 2500 * time.Millisecond}[w.rng.Intn(5)]
		lines := w.written[id]
		ordinal := 0
		if n := len(lines); n > 0 && lines[n-1].Timestamp.Equal(at(p.last)) {
			ordinal = lines[n-1].Ordinal + 1
		}
		parity := []string{"even", "odd"}[len(lines)%2]
		msg := p.name + "#" + strconv.Itoa(p.runs-1) + "." + strconv.Itoa(len(lines)) + " " + parity
		w.written[id] = append(lines, logline.Record{Timestamp: at(p.last), Message: msg, ContainerID: id, Ordinal: ordinal})
		w.s.Add("prod/"+p.name+"/app", logstest.Stamped(p.last, msg))
	}
}

// rotate drops the oldest whole seconds of the log the kubelet serves of p's current
// or previous instance, as a rotation does: a moment is never split.
func (w *world) rotate(p *walkPod) {
	key := "prod/" + p.name + "/app"
	if w.rng.Intn(2) == 0 {
		key += "/previous"
	}
	w.s.Mu.Lock()
	defer w.s.Mu.Unlock()
	lines := w.s.Logs[key]
	if len(lines) == 0 {
		return
	}
	cut := parsed(lines[w.rng.Intn(len(lines))]).Truncate(time.Second)
	var kept []string
	for _, line := range lines {
		if !parsed(line).Before(cut) {
			kept = append(kept, line)
		}
	}
	w.s.Logs[key] = kept
}

// parsed is the moment of a line of the fake kubelet's.
func parsed(line string) time.Time {
	ts, _ := time.Parse(time.RFC3339Nano, strings.SplitN(line, " ", 2)[0])
	return ts
}

// restart ends p's current instance at the second of its last line and starts the
// next a second later: the kubelet serves the old one as previous, the one before it
// no more, and the mirror says so at once.
func (w *world) restart(p *walkPod) {
	p.last = p.last.Truncate(time.Second) + 2*time.Second
	p.runs++
	w.s.Mu.Lock()
	w.s.Logs["prod/"+p.name+"/app/previous"] = w.s.Logs["prod/"+p.name+"/app"]
	w.s.Logs["prod/"+p.name+"/app"] = nil
	w.s.Mu.Unlock()
	w.f.store.Apply(w.t, w.body(p))
}

// evict deletes a random chunk of a random stream of the file, and its lines, as the
// janitor would.
func (w *world) evict() {
	file := fileOf(w.t, w.svc, w.f)
	p := w.pods[w.rng.Intn(len(w.pods))]
	rows, err := file.PodStreams(w.t.Context(), "prod", p.name, "")
	require.NoError(w.t, err)
	if len(rows) == 0 {
		return
	}
	row := rows[w.rng.Intn(len(rows))]
	chunks, err := file.Chunks(w.t.Context(), row.ID, "")
	require.NoError(w.t, err)
	if len(chunks) == 0 {
		return
	}
	require.NoError(w.t, file.Evict(w.t.Context(), chunks[w.rng.Intn(len(chunks))].ID, math.MaxInt64))
}

// page reads a random page of a random view, and checks every line it answered is
// in a chunk covering the view's grep.
func (w *world) page() {
	p := w.pods[w.rng.Intn(len(w.pods))]
	view := []Scope{
		{Sources: []ScopeSource{{Namespace: "prod", Kind: SourceDeployment, Name: "web", Containers: []string{"app"}}}},
		podSource(p.name, false, "app"),
		podSource(p.name, true, "app"),
	}[w.rng.Intn(3)]
	view.Grep = []string{"", "odd"}[w.rng.Intn(2)]
	q := FetchRequest{Scope: view, PageFrom: []PageFrom{PageFromFirst, PageFromLast}[w.rng.Intn(2)]}
	switch n := w.rng.Intn(4); {
	case n == 0:
		moment := at(time.Duration(w.rng.Int63n(int64(p.last + time.Second))))
		q.Window = Window{Since: &moment, Before: Padding{Lines: 1 + w.rng.Intn(20)}}
		q.PageFrom = PageFromFirst
	case n == 1 && len(w.cursors) > 0:
		q = FetchRequest{Cursor: w.cursors[w.rng.Intn(len(w.cursors))]}
	case n == 2 && len(w.seen) > 0:
		q.PageFrom, q.Seen = PageFromFirst, []string{w.seen[w.rng.Intn(len(w.seen))]}
	}
	q.PageSize = 1 + w.rng.Intn(20)
	page, err := w.svc.Fetch(w.t.Context(), "7", q)
	if refused, ok := errors.AsType[refusal.Refusal](err); ok {
		require.NotEmpty(w.t, refused.Code, "every refusal names its code")
		return
	}
	require.NoError(w.t, err)
	for _, c := range []*string{page.Page.Prev, page.Page.Next, page.Page.Follow} {
		if c != nil {
			w.cursors = append(w.cursors, c)
		}
	}
	file := fileOf(w.t, w.svc, w.f)
	grep := page.Request.Grep
	for _, rec := range page.Records {
		w.seen = append(w.seen, rec.Cursor())
		id := w.rowID(file, rec)
		chunks, err := file.Chunks(w.t.Context(), id, grep)
		require.NoError(w.t, err)
		at := logstore.Pos{TS: rec.Timestamp.UnixNano(), Ord: rec.Ordinal}
		require.True(w.t, claims(chunks, at), "%s answered outside every chunk of %q", rec.Message, grep)
		held := w.lines(file, id)
		require.Equal(w.t, rec.Message, held[at], "the file's line at %v", at)
	}
}

// rowID is the file's row of rec's instance.
func (w *world) rowID(file *logstore.Store, rec logline.Record) int64 {
	rows, err := file.PodStreams(w.t.Context(), rec.Stream.Namespace, rec.Stream.Pod, "")
	require.NoError(w.t, err)
	for _, row := range rows {
		if row.Container == rec.Stream.Container && row.ContainerID == rec.ContainerID {
			return row.ID
		}
	}
	w.t.Fatalf("no row of %s", rec.ContainerID)
	return 0
}

// lines is every line the file holds of the stream id, by position.
func (w *world) lines(file *logstore.Store, id int64) map[logstore.Pos]string {
	held := map[logstore.Pos]string{}
	require.NoError(w.t, file.Read(w.t.Context(), func(r logstore.Reader) error {
		return r.Lines(id, logstore.Oldest, logstore.Newest, func(l logstore.Line) bool {
			held[l.Pos] = l.Message
			return true
		})
	}))
	return held
}

// claims is whether a chunk of chunks claims the line at p.
func claims(chunks []logstore.Chunk, p logstore.Pos) bool {
	for _, c := range chunks {
		if c.From.Compare(p) <= 0 && p.Compare(c.To) <= 0 {
			return true
		}
	}
	return false
}

// checkChunks checks no chunk is missing a line its instance wrote into its range,
// and no span either: a link promises nothing lies between, so a rotation leaves a
// gap.
func (w *world) checkChunks() {
	file := fileOf(w.t, w.svc, w.f)
	for _, p := range w.pods {
		rows, err := file.PodStreams(w.t.Context(), "prod", p.name, "")
		require.NoError(w.t, err)
		for _, row := range rows {
			chunks, err := file.Chunks(w.t.Context(), row.ID, "")
			require.NoError(w.t, err)
			held := w.lines(file, row.ID)
			for _, rec := range w.written[row.ContainerID] {
				at := logstore.Pos{TS: rec.Timestamp.UnixNano(), Ord: rec.Ordinal}
				if claims(chunks, at) {
					require.Equal(w.t, rec.Message, held[at], "a chunk of %s claims %v", row.ContainerID, at)
				}
				for _, sp := range logstore.CoverageOf(unlabelled(chunks), kubeletOnly, nil).Spans {
					if sp.From.Compare(at) <= 0 && at.Compare(sp.To) <= 0 {
						require.Equal(w.t, rec.Message, held[at], "a span of %s covers %v", row.ContainerID, at)
					}
				}
			}
		}
	}
}

// unlabelled is the chunks of chunks that claim every line, not a grep's.
func unlabelled(chunks []logstore.Chunk) []logstore.Chunk {
	var out []logstore.Chunk
	for _, c := range chunks {
		if c.Pattern == "" {
			out = append(out, c)
		}
	}
	return out
}

// A fill its limits cut halfway through a gap writes what it read, and the walk
// stops at the gap left past it, so the page ends there with more to read; the
// next page fills the rest of the gap from where the first stopped, into the same
// span, and every line is shown once.
func TestAWalkStopsAtALiveGap(t *testing.T) {
	t.Parallel()
	s := newLogServer()
	s.Add("prod/web-1/app", logstest.LineEvery(100)...)
	svc, f := serviceOver(t, webMirror(t), s)
	f.chunk(64)
	svc.caps = capsOf(1 << 10)

	got := pages(t, svc, web1Scope, PageFromFirst, MaxPageSize)

	assert.Greater(t, len(got), 1, "the limits cut the first page")
	assert.Equal(t, messages(linesOf(logstest.LineEvery(100))), recordsOf(got, PageFromFirst))
	assert.Len(t, spansOf(t, svc, f), 1,
		"each page's fill continues the chunk the one before it wrote")
}

// A walk crosses a lost gap as it crosses a seam: a Tail page that reached the
// kubelet's oldest line over a stream with an older chunk below reads on into that
// chunk and answers nothing older, and a Head page over a stream no backend serves
// reads past the gaps inside it, so no page answers more at one spot twice.
func TestAWalkCrossesALostGap(t *testing.T) {
	t.Parallel()
	s := newLogServer()
	s.Add("prod/web-1/app", logstest.LineEvery(10)[5:]...)
	svc, f := serviceOver(t, webMirror(t), s)
	seed(t, svc, f, rowOf("web-1", "app"),
		logstore.Fill{Lines: fileLines(0, time.Second)},
		logstore.Fill{Lines: fileLines(5*time.Second, 6*time.Second, 7*time.Second, 8*time.Second, 9*time.Second), AtBeginning: true})

	page, err := svc.Fetch(t.Context(), "7", FetchRequest{Scope: web1Scope, PageFrom: PageFromLast, PageSize: 100})

	require.NoError(t, err)
	assert.Equal(t, []string{"0", "1", "5", "6", "7", "8", "9"}, messages(page.Records))
	assert.Nil(t, page.Page.Prev)

	gone := Scope{Sources: []ScopeSource{{Namespace: "prod", Kind: SourcePod, Name: "old-1"}}}
	seed(t, svc, f, rowOf("old-1", "app"),
		logstore.Fill{Lines: fileLines(0, time.Second)},
		logstore.Fill{Lines: fileLines(5 * time.Second)})
	page, err = svc.Fetch(t.Context(), "7", FetchRequest{Scope: gone, PageFrom: PageFromFirst, PageSize: 100})

	require.NoError(t, err)
	assert.Equal(t, []string{"0", "1", "5"}, messages(page.Records))
	assert.Nil(t, page.Page.Next)
}

// A line is held by the page's chunks when it lies in one of them, two that touch or
// overlap read as one run, and a line in the gap between two runs is nobody's.
func TestHeldByJoinsChunksThatTouch(t *testing.T) {
	t.Parallel()
	pos := func(ts int64) logstore.Pos { return logstore.Pos{TS: ts} }
	held := heldBy([]logstore.Chunk{
		{From: pos(0), To: pos(10)},
		{From: pos(5), To: pos(8)},
		{From: pos(10), To: pos(20)},
		{From: pos(30), To: pos(40)},
	})
	for ts, want := range map[int64]bool{0: true, 7: true, 10: true, 15: true, 20: true, 25: false, 30: true, 40: true, 41: false} {
		assert.Equal(t, want, held(pos(ts)), ts)
	}
}

// A walk that spends its share of the walk budget stops short, and the page is cut
// at its last line rather than passing lines it did not read.
func TestAWalkThatSpendsItsBudgetCutsThePage(t *testing.T) {
	t.Parallel()
	svc, f := serviceOver(t, webMirror(t), newLogServer())
	var cached []time.Duration
	for i := range 50 {
		cached = append(cached, time.Duration(i)*time.Second)
	}
	seed(t, svc, f, rowOf("web-1", "app"), logstore.Fill{Lines: fileLines(cached...), AtBeginning: true})
	svc.walkBudget = 64

	page, err := svc.Fetch(t.Context(), "7", FetchRequest{Scope: web1Scope, PageFrom: PageFromFirst, PageSize: 100})

	require.NoError(t, err)
	assert.NotEmpty(t, page.Records)
	assert.Less(t, len(page.Records), 50)
	assert.NotNil(t, page.Page.Next)
}

// pagedMarks is marks of every page a walk of web-1 from from at limit reads, oldest
// first, over a file holding twoHoles.
func pagedMarks(t *testing.T, grep string, from PageFrom, limit int) []string {
	t.Helper()
	s := newLogServer()
	s.Add("prod/web-1/app", logstest.Stamped(9*time.Second, "y9"), logstest.Stamped(10*time.Second, "x10"))
	svc, f := serviceOver(t, webMirror(t), s)
	seed(t, svc, f, rowOf("web-1", "app"), twoHoles...)
	view := web1Scope
	view.Grep = grep
	var out []string
	for _, page := range pages(t, svc, view, from, limit) {
		if from == PageFromFirst {
			out = append(out, marks(page.Records)...)
		} else {
			out = append(marks(page.Records), out...)
		}
	}
	return out
}

// A record carries every marked hole between it and the line of its stream before it
// that the view passes, wherever the pages cut, read either way.
func TestARecordCarriesTheHolesBeforeIt(t *testing.T) {
	t.Parallel()
	every := []string{"x0", "y1", "y5 (1s-5s)", "y6", "y9 (6s-9s)", "x10"}
	for _, mode := range []PageFrom{PageFromFirst, PageFromLast} {
		for _, limit := range []int{1, 2, 100} {
			name := fmt.Sprintf("%s by %d", mode, limit)
			assert.Equal(t, every, pagedMarks(t, "", mode, limit), name)
			assert.Equal(t, []string{"x0", "x10 (1s-9s)"}, pagedMarks(t, "x", mode, limit), name+" under x")
			assert.Equal(t, []string{"y1", "y5 (1s-5s)", "y6", "y9 (6s-9s)"}, pagedMarks(t, "y", mode, limit), name+" under y")
		}
	}
}

// A walk reports the first and last rows it read, kept or rejected by the grep:
// a page cut at its first kept line still read past it to fill its need.
func TestAWalkReportsTheRowsItRead(t *testing.T) {
	t.Parallel()
	svc, f := serviceOver(t, webMirror(t), newLogServer())
	seed(t, svc, f, rowOf("web-1", "app"), logstore.Fill{Lines: fileLines(0, time.Second, 2*time.Second, 3*time.Second, 4*time.Second), AtBeginning: true})
	grep := regexp.MustCompile("^[02]$")
	scope := web1Scope
	scope.Grep = grep.String()

	ms, err := svc.page(t.Context(), "7", pageQuery{scope: scope, grep: grep, mode: logline.Newer, limit: 1})

	require.NoError(t, err)
	require.Len(t, ms[0].reaches, 1)
	r := ms[0].reaches[0]
	web1 := logline.StreamKey{Namespace: "prod", Pod: "web-1", Container: "app"}
	assert.Equal(t, lineAt(web1, 0, 0), r.first)
	assert.Equal(t, lineAt(web1, 2*time.Second, 0), r.last)
	assert.True(t, r.stop.IsZero())
	assert.Equal(t, web1, logline.StreamKey{Namespace: r.run.Namespace, Pod: r.run.Pod, Container: r.run.Container})
}

// ===== The merge =====

// canned is a reading of the first record's stream that sends recs, then ends
// clean.
func canned(ctx context.Context, recs ...logline.Record) *kubelet.Read {
	return cannedFrom(ctx, recs[0].Stream, recs, nil, logline.Cursor{})
}

// cannedFrom is a reading of key that sends recs, then ends with err, and short
// at stop when stop is set.
func cannedFrom(ctx context.Context, key logline.StreamKey, recs []logline.Record, err error, stop logline.Cursor) *kubelet.Read {
	return cannedUntil(ctx, key, recs, nil, err, stop)
}

// cannedUntil is cannedFrom that, once it has sent recs, ends only once wait
// closes, a nil wait at once.
func cannedUntil(ctx context.Context, key logline.StreamKey, recs []logline.Record, wait <-chan struct{}, err error, stop logline.Cursor) *kubelet.Read {
	ctx, cancel := context.WithCancel(ctx)
	out, lines := spsc.New[kubelet.Item](0)
	r := &kubelet.Read{Key: key, Lines: lines, Cancel: cancel}
	go func() {
		defer out.Close()
		for _, rec := range recs {
			if out.SendContext(ctx, kubelet.Item{Record: rec}) != nil {
				return
			}
		}
		if wait != nil {
			select {
			case <-wait:
			case <-ctx.Done():
				return
			}
		}
		r.Err, r.Short, r.Stop = err, !stop.IsZero(), stop
	}()
	return r
}

// stalled is a reading of key that sends recs, then waits until it is cancelled.
func stalled(ctx context.Context, key logline.StreamKey, recs ...logline.Record) *kubelet.Read {
	ctx, cancel := context.WithCancel(ctx)
	out, lines := spsc.New[kubelet.Item](0)
	r := &kubelet.Read{Key: key, Lines: lines, Cancel: cancel}
	go func() {
		defer out.Close()
		for _, rec := range recs {
			if out.SendContext(ctx, kubelet.Item{Record: rec}) != nil {
				break
			}
		}
		<-ctx.Done()
		r.Err = ctx.Err()
	}()
	return r
}

// endedShort is a reading of key that has ended with err, short before any line
// in range.
func endedShort(key logline.StreamKey, err error) *kubelet.Read {
	out, lines := spsc.New[kubelet.Item](0)
	out.Close()
	return &kubelet.Read{Key: key, Lines: lines, Cancel: func() {}, Err: err, Short: true}
}

// A fetch's lines from three streams come out in time order.
func TestAFetchMergesByTime(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	page, err := mergePage(ctx, []*kubelet.Read{
		canned(ctx, line("a", "app", 0, "a0"), line("a", "app", 3*time.Second, "a3")),
		canned(ctx, line("b", "app", time.Second, "b1"), line("b", "app", 4*time.Second, "b4")),
		canned(ctx, line("c", "app", 2*time.Second, "c2")),
	}, logline.Newer, 10)

	require.NoError(t, err)
	assert.Equal(t, []string{"a0", "b1", "c2", "a3", "b4"}, messages(page.records))
	assert.True(t, page.cut.IsZero())
	assert.Empty(t, page.skipped)
}

// Lines stamped alike come out by pod, then container, then ordinal, whichever
// stream is first.
func TestATieKeepsAFixedOrder(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	for _, mode := range []logline.Direction{logline.Newer, logline.Older} {
		// A stream's lines come in the order the page reads them.
		app := []logline.Record{line("a", "app", 0, "a-app"), nth("a", "app", 0, 1, "a-app'")}
		if mode == logline.Older {
			slices.Reverse(app)
		}
		page, err := mergePage(ctx, []*kubelet.Read{
			canned(ctx, line("b", "app", 0, "b")),
			canned(ctx, line("a", "side", 0, "a-side")),
			canned(ctx, app...),
		}, mode, 10)

		require.NoError(t, err)
		assert.Equal(t, []string{"a-app", "a-app'", "a-side", "b"}, messages(page.records), mode)
	}
}

// Head keeps the oldest lines and hands back the last one's cursor.
func TestHeadCutsAtTheLimit(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	page, err := mergePage(ctx, []*kubelet.Read{
		canned(ctx, line("a", "app", 0, "0"), line("a", "app", time.Second, "1"), line("a", "app", 2*time.Second, "2")),
	}, logline.Newer, 2)

	require.NoError(t, err)
	assert.Equal(t, []string{"0", "1"}, messages(page.records))
	assert.Equal(t, *lineCursorFor("a", "app", time.Second, 0), logline.Encode(page.cut))
}

// Tail reads newest first, keeps the newest lines in time order, and hands back
// the oldest one's cursor.
func TestTailCutsAtTheLimit(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	page, err := mergePage(ctx, []*kubelet.Read{
		canned(ctx, line("a", "app", 2*time.Second, "2"), line("a", "app", time.Second, "1"), line("a", "app", 0, "0")),
	}, logline.Older, 2)

	require.NoError(t, err)
	assert.Equal(t, []string{"1", "2"}, messages(page.records))
	assert.Equal(t, *lineCursorFor("a", "app", time.Second, 0), logline.Encode(page.cut))
}

// A page cut inside a moment hands back the cursor of the line it ended at, which
// names that line among the moment's, so the next page goes on from the next.
func TestAPageCutInsideAMomentNamesItsLine(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	page, err := mergePage(ctx, []*kubelet.Read{
		canned(ctx, line("a", "app", 0, "a0"), nth("a", "app", 0, 1, "a0'"), line("a", "app", time.Second, "a1")),
		canned(ctx, line("b", "app", 0, "b0")),
	}, logline.Newer, 2)

	require.NoError(t, err)
	assert.Equal(t, []string{"a0", "a0'"}, messages(page.records))
	assert.Equal(t, *lineCursorFor("a", "app", 0, 1), logline.Encode(page.cut))
}

// failed is a reading of pod/container that ends with err and sends nothing.
func failed(ctx context.Context, pod, container string, err error) *kubelet.Read {
	return cannedFrom(ctx, logline.StreamKey{Namespace: "prod", Pod: pod, Container: container}, nil, err, logline.Cursor{})
}

// A stream answering 400 or 404 is skipped and named, and the others fill the page.
func TestAFailedStreamIsSkippedAndNamed(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	page, err := mergePage(ctx, []*kubelet.Read{
		canned(ctx, line("a", "app", 0, "ok")),
		failed(ctx, "b", "istio-proxy", &kubelet.StatusError{Code: http.StatusBadRequest, Message: "container is waiting to start"}),
		failed(ctx, "c", "app", &kubelet.StatusError{Code: http.StatusNotFound, Message: `pods "c" not found`}),
	}, logline.Newer, 10)

	require.NoError(t, err)
	assert.Equal(t, []string{"ok"}, messages(page.records))
	assert.Equal(t, []string{"b/istio-proxy: container is waiting to start", `c/app: pods "c" not found`}, page.skipped)
}

// A stream's transport error is named in skipped as safe renders it, with its
// controls stripped.
func TestASkippedStreamsErrorIsCleaned(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	page, err := mergePage(ctx, []*kubelet.Read{
		canned(ctx, line("a", "app", 0, "a0")),
		failed(ctx, "b", "app", errors.New("Get \"https://me:hunter2@api/log\": reset\x1b[2J")),
	}, logline.Newer, 10)

	require.NoError(t, err)
	require.Len(t, page.skipped, 1)
	assert.NotContains(t, page.skipped[0], "hunter2")
	assert.NotContains(t, page.skipped[0], "\x1b")
}

// Every stream failing fails the page, naming the first.
func TestEveryStreamFailingFailsThePage(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	_, err := mergePage(ctx, []*kubelet.Read{
		failed(ctx, "a", "app", &kubelet.StatusError{Code: http.StatusNotFound, Message: "not found"}),
	}, logline.Newer, 10)

	assert.EqualError(t, err, "no container of this view could be read: a/app: not found")
}

// A 401 on any stream fails the page: the user's credentials, not a pod's state.
func TestAnUnauthorizedStreamFailsThePage(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	_, err := mergePage(ctx, []*kubelet.Read{
		canned(ctx, line("a", "app", 0, "ok")),
		failed(ctx, "b", "app", &kubelet.StatusError{Code: http.StatusUnauthorized, Message: "unauthorized"}),
	}, logline.Newer, 10)

	assert.EqualError(t, err, "the cluster refused to show this log: unauthorized")
}

// A 403 is one namespace's, so the page skips that stream and keeps the rest.
func TestAForbiddenStreamIsSkipped(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	page, err := mergePage(ctx, []*kubelet.Read{
		canned(ctx, line("a", "app", 0, "ok")),
		failed(ctx, "b", "app", &kubelet.StatusError{Code: http.StatusForbidden, Message: "forbidden"}),
	}, logline.Newer, 10)

	require.NoError(t, err)
	assert.Equal(t, []string{"ok"}, messages(page.records))
	assert.Equal(t, []string{"b/app: forbidden"}, page.skipped)
}

// A stream the connection's retirement cut ends the page where it stopped, so the
// next page reads its lines rather than passing them; one cut before any line
// refuses the page.
func TestARetiredStreamEndsThePageWhereItStopped(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	a0 := line("a", "app", 0, "a0")
	page, err := mergePage(ctx, []*kubelet.Read{
		cannedFrom(ctx, a0.Stream, []logline.Record{a0}, kubelet.ErrRetired, logline.CursorOf(a0)),
		canned(ctx, line("b", "app", 0, "b0"), line("b", "app", time.Second, "b1")),
	}, logline.Newer, 10)

	require.NoError(t, err)
	assert.Equal(t, []string{"a0"}, messages(page.records))
	assert.Empty(t, page.skipped)
	require.False(t, page.cut.IsZero())

	_, err = mergePage(ctx, []*kubelet.Read{endedShort(a0.Stream, kubelet.ErrRetired), canned(ctx, line("b", "app", 0, "b0"))}, logline.Newer, 10)
	assert.ErrorIs(t, err, errReplaced)
}

// A stream that failed past a line in range ends the page where it stopped, as a
// retired one does, rather than letting the page pass lines it never read.
func TestAStreamThatFailedPartwayEndsThePageWhereItStopped(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	a0 := line("a", "app", 0, "a0")
	page, err := mergePage(ctx, []*kubelet.Read{
		cannedFrom(ctx, a0.Stream, []logline.Record{a0}, errors.New("connection reset"), logline.CursorOf(a0)),
		canned(ctx, line("b", "app", 0, "b0"), line("b", "app", time.Second, "b1")),
	}, logline.Newer, 10)

	require.NoError(t, err)
	assert.Equal(t, []string{"a0"}, messages(page.records))
	assert.Empty(t, page.skipped)
	assert.Equal(t, *cursorOf(logline.CursorOf(a0)), logline.Encode(page.cut))
}

// A merge that has its lines asks its streams for none past them.
func TestAFullMergeReadsNoFurther(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	more, lines := spsc.New[kubelet.Item](0)
	full := &kubelet.Read{Lines: lines}
	go func() { _ = more.Send(kubelet.Item{Record: line("a", "app", 0, "a0")}) }()

	got := make(chan []logline.Record, 1)
	go func() { got <- merged(ctx, []*kubelet.Read{full}, logline.Newer, 1) }()

	assert.Equal(t, []string{"a0"}, messages(testutil.Recv(t, got, "the merge to end")))
}

// A stream stopped short before a line in range, the connection's retirement aside,
// leaves no point a next page could go on from. Every cut has an edge, so that
// cannot happen: the page fails with an internal error, not a refusal.
func TestAStreamStoppedAtItsStartIsAnInternalError(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	_, err := mergePage(ctx, []*kubelet.Read{
		canned(ctx, line("a", "app", 0, "a0")),
		endedShort(logline.StreamKey{Pod: "b", Container: "app"}, nil),
	}, logline.Newer, 10)

	assert.EqualError(t, err, "logs: b/app stopped short before a line in range")
	assert.NotErrorAs(t, err, new(refusal.Refusal))
}

// A page whose context ends mid-merge fails with it rather than reading as the
// end of the log.
func TestAPageEndsWithItsContext(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	got := make(chan error, 1)
	go func() {
		_, err := mergePage(ctx, []*kubelet.Read{stalled(t.Context(), b)}, logline.Newer, 10)
		got <- err
	}()
	cancel()

	assert.ErrorIs(t, testutil.Recv(t, got, "the page to end"), context.Canceled)
}

// A full page cancels the readings still going, so it waits on none of them.
func TestAFullPageCancelsItsReadings(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	got := make(chan merge, 1)
	go func() {
		page, err := mergePage(ctx, []*kubelet.Read{
			stalled(ctx, b, line("b", "app", 0, "b0"), line("b", "app", time.Second, "b1")),
		}, logline.Newer, 1)
		assert.NoError(t, err)
		got <- page
	}()

	page := testutil.Recv(t, got, "the page to end")
	assert.Equal(t, []string{"b0"}, messages(page.records))
	assert.Equal(t, *lineCursorFor("b", "app", 0, 0), logline.Encode(page.cut))
}

// A merge ends at the first line past a stream's stop, asking no stream for more.
func TestAMergeEndsAtAStop(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	a0 := line("a", "app", 0, "a0")
	got := make(chan merge, 1)
	go func() {
		page, err := mergePage(ctx, []*kubelet.Read{
			cannedFrom(ctx, a0.Stream, []logline.Record{a0}, nil, logline.CursorOf(a0)),
			// b would wait for its cancel if asked for a line past b1.
			stalled(ctx, b, line("b", "app", time.Second, "b1")),
		}, logline.Newer, 10)
		assert.NoError(t, err)
		got <- page
	}()

	page := testutil.Recv(t, got, "the page to end")
	assert.Equal(t, []string{"a0"}, messages(page.records))
	assert.Equal(t, *cursorOf(logline.CursorOf(a0)), logline.Encode(page.cut))
}

// b is the container app of pod b in prod.
var b = logline.StreamKey{Namespace: "prod", Pod: "b", Container: "app"}

// A backward read that spent its budget keeps only the lines at and after the
// newest point any stream stopped at, and hands that point back.
func TestABudgetKeepsTheNearSideOfTheNewestStop(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	page, err := mergePage(ctx, []*kubelet.Read{
		canned(ctx, line("a", "app", 5*time.Second, "a5"), line("a", "app", time.Second, "a1")),
		cannedFrom(ctx, b, []logline.Record{line("b", "app", 4*time.Second, "b4"), line("b", "app", 3*time.Second, "b3")}, nil, lineAt(b, 3*time.Second, 0)),
	}, logline.Older, 10)

	require.NoError(t, err)
	assert.Equal(t, []string{"b3", "b4", "a5"}, messages(page.records))
	assert.Equal(t, *lineCursorFor("b", "app", 3*time.Second, 0), logline.Encode(page.cut))
}

// A budget spent before any line matched answers no lines with a cursor.
func TestABudgetCanAnswerNoLinesWithACursor(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	page, err := mergePage(ctx, []*kubelet.Read{
		cannedFrom(ctx, b, nil, nil, lineAt(b, 7*time.Second, 0)),
	}, logline.Older, 10)

	require.NoError(t, err)
	assert.Empty(t, page.records)
	assert.Equal(t, *lineCursorFor("b", "app", 7*time.Second, 0), logline.Encode(page.cut))
}

// A forward read that spent its budget keeps the lines at and before the oldest
// stop.
func TestABudgetKeepsTheNearSideOfTheOldestStop(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	page, err := mergePage(ctx, []*kubelet.Read{
		canned(ctx, line("a", "app", 0, "a0"), line("a", "app", 5*time.Second, "a5")),
		cannedFrom(ctx, b, []logline.Record{line("b", "app", 2*time.Second, "b2")}, nil, lineAt(b, 2*time.Second, 0)),
	}, logline.Newer, 10)

	require.NoError(t, err)
	assert.Equal(t, []string{"a0", "b2"}, messages(page.records))
	assert.Equal(t, *lineCursorFor("b", "app", 2*time.Second, 0), logline.Encode(page.cut))
}

// ===== The caps =====

// saying is a record saying msg.
func saying(msg string) logline.Record { return line("web-1", "app", time.Second, msg) }

// A message over the cap is cut at a character boundary, before any SGR sequence
// that would not fit whole, and counts the bytes it lost; one under the cap keeps
// its colours (rule 8).
func TestAMessageIsCutAtTheCap(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		msg, want string
		max       int
	}{
		"under the cap":         {"hello", "hello", 5},
		"at a boundary":         {"hello", "hel", 3},
		"inside a character":    {"héllo", "h", 2},
		"inside a sequence":     {"ab\x1b[31mred", "ab", 5},
		"past a sequence":       {"ab\x1b[31mred", "ab\x1b[31mr", 8},
		"coloured, under":       {"\x1b[31mred\x1b[0m", "\x1b[31mred\x1b[0m", 100},
		"a sequence at the end": {"red\x1b[0m", "red", 5},
	} {
		got, _, _ := capLines([]logline.Record{saying(tc.msg)}, false, tc.max, MaxPageBytesCap, false)
		assert.Equal(t, tc.want, got[0].Message, name)
		assert.Equal(t, len(tc.msg)-len(tc.want), got[0].TruncatedBytes, name)
	}
}

// A page keeps its records in the order it reads while their messages, after the
// message cap, fit the page cap; the first always, saying so when it alone is over
// (rule 9).
func TestAPageKeepsWhatFitsItsCap(t *testing.T) {
	t.Parallel()
	recs := []logline.Record{saying(strings.Repeat("a", 10)), saying(strings.Repeat("b", 10)), saying(strings.Repeat("c", 10))}

	got, cut, notices := capLines(recs, false, 100, 25, false)
	assert.Equal(t, recs[:2], got)
	assert.True(t, cut)
	assert.Empty(t, notices)

	got, cut, notices = capLines(recs, false, 100, 30, false)
	assert.Equal(t, recs, got)
	assert.False(t, cut)
	assert.Empty(t, notices)

	got, cut, _ = capLines(recs, false, 4, 10, false)
	assert.Len(t, got, 2, "the message cap counts first")
	assert.True(t, cut)

	got, cut, notices = capLines(recs, false, 100, 5, false)
	assert.Equal(t, recs[:1], got)
	assert.True(t, cut)
	assert.Equal(t, []refusal.Code{refusal.CodePageByteLimitExceeded}, codesOf(notices))

	got, cut, _ = capLines(recs, true, 100, 25, false)
	assert.Equal(t, recs[1:], got, "read newest first, the oldest is dropped")
	assert.True(t, cut)
}

// A fatal read fails the page.
func TestAFatalReadFailsThePage(t *testing.T) {
	t.Parallel()
	backend := newFakeBackend()
	backend.set("web-1/app", fakeRead{err: &kubelet.StatusError{Code: http.StatusUnauthorized, Message: "Unauthorized"}})
	backend.set("web-2/app", fakeRead{lines: []logline.Record{{Timestamp: at(0), Message: "a"}}})
	svc, _ := serviceOn(t, backend)

	_, err := svc.Fetch(t.Context(), "7", FetchRequest{Scope: webScope, PageFrom: PageFromFirst, PageSize: 10})

	assert.EqualError(t, err, "the cluster refused to show this log: Unauthorized")
}

// A denied or an ended read is the page's skipped entry, and the other streams'
// lines merge as they would without it.
func TestADeniedOrEndedReadIsSkipped(t *testing.T) {
	t.Parallel()
	for _, refused := range []*kubelet.StatusError{{Code: http.StatusForbidden, Message: "forbidden"}, {Code: http.StatusBadRequest, Message: "container is waiting to start"}} {
		backend := newFakeBackend()
		backend.set("web-1/app", fakeRead{err: refused})
		backend.set("web-2/app", fakeRead{lines: []logline.Record{{Timestamp: at(0), Message: "a"}, {Timestamp: at(time.Second), Message: "b"}}})
		svc, _ := serviceOn(t, backend)

		page, err := svc.Fetch(t.Context(), "7", FetchRequest{Scope: webScope, PageFrom: PageFromFirst, PageSize: 10})

		require.NoError(t, err)
		assert.Equal(t, []string{"a", "b"}, messages(page.Records))
		assert.Equal(t, []string{"web-1/app: " + refused.Message}, skippedOf(page))
	}
}

// A skipped stream's line names the stream and says why.
func TestASkippedLineNamesItsStream(t *testing.T) {
	t.Parallel()
	err := kubelet.LoudAt(at(2*time.Second + 300*time.Millisecond))
	assert.Equal(t, "web-1/app: "+err.Error(), skippedLine(logline.StreamKey{Namespace: "prod", Pod: "web-1", Container: "app"}, err))
}
