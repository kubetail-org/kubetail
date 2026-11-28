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
	"cmp"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/amorey/gochan/mpsc"
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

// ===== The tail =====

// webPod is a pod under web's replica set on node, running containers.
func webPod(name, node string, containers ...container) map[string]any {
	return pod(name, "uid-"+name, node, webReplicaSet(), containers...)
}

// web3 is a third pod of web, on node-a, running app.
func web3() map[string]any { return webPod("web-3", "node-a", container{name: "app"}) }

// web1 and web2 are web's pods as webObjects has them.
func web1() map[string]any {
	return webPod("web-1", "node-a", container{name: "app"}, container{name: "istio-proxy"})
}

func web2() map[string]any {
	return webPod("web-2", "node-b", container{name: "app"}, container{name: "istio-proxy"})
}

// podView reads the default container of the pod name in prod.
func podView(name string) Scope {
	return Scope{Sources: []ScopeSource{{Namespace: "prod", Kind: SourcePod, Name: name}}}
}

// donePod is a pod of no workload whose one container, app, has finished: its log
// is whole.
func donePod() map[string]any {
	return finished(pod("done", "uid-done", "node-a", nil, container{name: "app"}))
}

// bookmark closes a watch's snapshot.
var bookmark = cluster.ClusterCachedDataObjectCollectionWatchFrame{Type: cluster.CollectionWatchFrameBookmark}

// A view of more containers than Kubetail reads at once is refused before any is
// read.
func TestATailOfTooManyContainersIsRefused(t *testing.T) {
	t.Parallel()
	s := newLogServer()
	f := webMirror(t)
	addPods(t, f, 99)
	svc, _ := serviceOver(t, f, s)

	_, err := svc.TailFromCursor(t.Context(), "7", TailRequest{Scope: webScope})
	assert.ErrorIs(t, err, ErrTooMany)
	assert.Empty(t, s.Requests())
}

// A tail is refused before it opens: a bad page cursor, a view the mirror
// refuses, a cluster that cannot be claimed, and a pods watch that cannot be
// opened, which releases the claim.
func TestTailIsRefusedBeforeItOpens(t *testing.T) {
	t.Parallel()
	bad := "yesterday"
	svc, _ := serviceOver(t, webMirror(t), newLogServer())
	_, err := svc.TailFromCursor(t.Context(), "7", TailRequest{Cursor: &bad})
	assert.Equal(t, errBadPageCursor, err)

	svc, f := serviceOver(t, webMirror(t), newLogServer())
	f.active.Cache = nil
	_, err = svc.TailFromCursor(t.Context(), "7", TailRequest{Scope: webScope})
	assert.ErrorIs(t, err, ErrNoCache)

	svc, f = serviceOver(t, webMirror(t), newLogServer())
	f.acquireErr = cluster.ErrNotConnectable
	_, err = svc.TailFromCursor(t.Context(), "7", TailRequest{Scope: webScope})
	assert.ErrorIs(t, err, kubelet.ErrRevoked)

	svc, f = serviceOver(t, webMirror(t), newLogServer())
	f.watchObjectsErr = errors.New("no watch")
	_, err = svc.TailFromCursor(t.Context(), "7", TailRequest{Scope: webScope})
	assert.EqualError(t, err, "no watch")
	assert.Equal(t, int32(1), f.lease.released.Load())
}

// keyIn is where the start cursor c seeds pod's app stream.
func keyIn(c pageCursor, pod string) logstore.Pos { return c.keyFor(psOf(pod, "app")) }

// fromMoment and pastLine are an app stream's key from the moment d past 10:00,
// and past its first line there.
func fromMoment(d time.Duration) logstore.Pos { return pos(d, -1) }
func pastLine(d time.Duration) logstore.Pos   { return pos(d, 0) }

// A tail starts from now, or from Since, its moment's lines in, and Seen names
// where each stream it names picks up: past its line, never before Since, the
// newer of two standing, whatever their order (rules 1 and 2).
func TestATailStartsFromAMoment(t *testing.T) {
	t.Parallel()
	now := at(time.Hour)
	c, err := tailStartCursor(TailRequest{Scope: webScope}, now)
	require.NoError(t, err)
	assert.Equal(t, webScope, c.Scope)
	assert.Equal(t, fromMoment(time.Hour), keyIn(c, "web-1"), "from now")

	c, err = tailStartCursor(TailRequest{Scope: webScope, Since: moment(time.Second)}, now)
	require.NoError(t, err)
	assert.Equal(t, fromMoment(time.Second), keyIn(c, "web-1"), "from since, its lines in")

	c, err = tailStartCursor(TailRequest{Scope: webScope, Since: moment(time.Second), Seen: slices.Concat(seenAt("web-1", "app", 3*time.Second), seenAt("web-2", "app", 0), seenAt("web-9", "app", 5*time.Second))}, now)
	require.NoError(t, err)
	assert.Equal(t, pastLine(3*time.Second), keyIn(c, "web-1"), "past its line")
	assert.Equal(t, fromMoment(time.Second), keyIn(c, "web-2"), "a line older than since reads from since")
	assert.Equal(t, fromMoment(time.Second), keyIn(c, "web-3"), "a stream not named, from since")

	for _, seen := range [][]string{
		slices.Concat(seenAt("web-1", "app", 3*time.Second), seenAt("web-1", "app", 2*time.Second)),
		slices.Concat(seenAt("web-1", "app", 2*time.Second), seenAt("web-1", "app", 3*time.Second)),
	} {
		c, err = tailStartCursor(TailRequest{Scope: webScope, Since: moment(0), Seen: seen}, now)
		require.NoError(t, err)
		assert.Equal(t, pastLine(3*time.Second), keyIn(c, "web-1"), "the newer stands")
	}
}

// A tail from a page's Next or Follow takes the cursor's scope and entries, the
// request's Seen merged in, the newer line standing; an entry is clipped to the
// window's start, and a counted Before still to be found seeds from Since. Its
// Until and After are cleared (rules 1 to 3).
func TestATailStartsFromAPageCursor(t *testing.T) {
	t.Parallel()
	now := at(time.Hour)
	entry := lineAt(web1Key(), 4*time.Second, 0)
	page := pageCursor{
		Scope: webScope, newer: true,
		resolvedWindow: resolvedWindow{
			Window:     Window{Since: moment(time.Second), Until: moment(time.Minute), After: Padding{Lines: 5}},
			ResolvedAt: now, Start: logline.MomentAt(at(time.Second)),
		},
		seen:  map[logline.ContainerName]logline.RunPlace{entry.ContainerName: entry.RunPlace},
		after: 2,
	}
	cursor := encodePageCursor(page)

	c, err := tailStartCursor(TailRequest{Cursor: &cursor}, now)
	require.NoError(t, err)
	assert.Equal(t, webScope, c.Scope)
	assert.Equal(t, pastLine(4*time.Second), keyIn(c, "web-1"), "its entry")
	assert.Equal(t, fromMoment(time.Second), keyIn(c, "web-2"), "no entry, from the window's start")
	assert.Nil(t, c.Until)
	assert.Equal(t, Padding{}, c.Window.After)
	assert.Zero(t, c.after)
	assert.True(t, c.live(at(48*time.Hour)), "the window's end is open")

	c, err = tailStartCursor(TailRequest{Cursor: &cursor, Seen: slices.Concat(seenAt("web-1", "app", 3*time.Second), seenAt("web-2", "app", 6*time.Second))}, now)
	require.NoError(t, err)
	assert.Equal(t, pastLine(4*time.Second), keyIn(c, "web-1"), "an older request entry yields")
	assert.Equal(t, pastLine(6*time.Second), keyIn(c, "web-2"), "a request entry stands")
	c, err = tailStartCursor(TailRequest{Cursor: &cursor, Seen: slices.Concat(seenAt("web-1", "app", 5*time.Second), seenAt("web-2", "app", 0))}, now)
	require.NoError(t, err)
	assert.Equal(t, pastLine(5*time.Second), keyIn(c, "web-1"), "a newer request entry stands")
	assert.Equal(t, fromMoment(time.Second), keyIn(c, "web-2"), "clipped to the window's start")

	counted := page
	counted.Window.Before, counted.Start, counted.seen = Padding{Lines: 3}, logline.Cursor{}, nil
	cursor = encodePageCursor(counted)
	c, err = tailStartCursor(TailRequest{Cursor: &cursor}, now)
	require.NoError(t, err)
	assert.Equal(t, fromMoment(time.Second), keyIn(c, "web-1"), "a count still to be found, from since")
}

// web1Key is web-1's app stream.
func web1Key() logline.StreamKey {
	return logline.StreamKey{Namespace: "prod", Pod: "web-1", Container: "app"}
}

// A tail's start that does not go together is refused before it opens.
func TestATailsStartIsChecked(t *testing.T) {
	t.Parallel()
	page := pageCursor{Scope: webScope, newer: true, resolvedWindow: resolvedWindow{ResolvedAt: at(time.Hour)}}
	next := encodePageCursor(page)
	page.newer = false
	prev := encodePageCursor(page)
	other := pageCursorOfJSON(t, strings.Replace(rawPageCursor(t, next), `"v":1`, `"v":2`, 1))
	notALine := logline.Encode(logline.MomentAt(at(0)))
	for name, tc := range map[string]struct {
		q    TailRequest
		code refusal.Code
	}{
		"a cursor beside since":   {TailRequest{Cursor: &next, Since: moment(0)}, refusal.CodeConflictingParameters},
		"seen alone":              {TailRequest{Scope: webScope, Seen: seenAt("web-1", "app", 0)}, refusal.CodeConflictingParameters},
		"a scope beside a cursor": {TailRequest{Scope: webScope, Cursor: &next}, refusal.CodeConflictingParameters},
		"no scope and no cursor":  {TailRequest{Since: moment(0)}, refusal.CodeConflictingParameters},
		"a prev":                  {TailRequest{Cursor: &prev}, refusal.CodeCursorInvalid},
		"another version":         {TailRequest{Cursor: &other}, refusal.CodeCursorInvalid},
		"a seen not a line":       {TailRequest{Scope: webScope, Since: moment(0), Seen: []string{notALine}}, refusal.CodeCursorInvalid},
		"a seen not a cursor":     {TailRequest{Cursor: &next, Seen: []string{"yesterday"}}, refusal.CodeCursorInvalid},
	} {
		_, err := tailStartCursor(tc.q, at(time.Hour))
		assert.Equal(t, tc.code, codeOf(err), name)
	}
}

// openTail opens a tail of webScope over f, failing the test on an error, and
// closes the pods watch's snapshot, returning once the tail has resolved the
// view against it.
func openTail(t *testing.T, svc *Service, f *fakeService) (*cluster.Stream[TailFrame], context.CancelFunc) {
	t.Helper()
	return openTailOf(t, svc, f, webScope)
}

// startTail opens a tail of q on ctx, which must open, and answers its frames.
func startTail(t *testing.T, ctx context.Context, svc *Service, q TailRequest) *cluster.Stream[TailFrame] {
	t.Helper()
	opened, err := svc.TailFromCursor(ctx, "7", q)
	require.NoError(t, err)
	return opened.Frames
}

// openTailOf is openTail of view.
func openTailOf(t *testing.T, svc *Service, f *fakeService, view Scope) (*cluster.Stream[TailFrame], context.CancelFunc) {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)
	stream := startTail(t, ctx, svc, TailRequest{Scope: view})
	closeSnapshot(t, f, f.pods)
	return stream, cancel
}

// openEndingTail opens a tail of webScope over f that ends on its own. It
// leaves the pods watch's snapshot open: the tail opens its streams before the
// snapshot closes and can end before the test could close it, after which nothing
// reads the watch.
func openEndingTail(t *testing.T, svc *Service) *cluster.Stream[TailFrame] {
	t.Helper()
	stream := startTail(t, t.Context(), svc, TailRequest{Scope: webScope})
	return stream
}

// closeSnapshot closes watch's snapshot and returns once the resolve that starts
// has read the mirror, so a change the test makes next cannot race that read.
func closeSnapshot(t *testing.T, f *fakeService, watch chan<- cluster.ClusterCachedDataObjectCollectionWatchFrame) {
	t.Helper()
	read := testutil.NewProbe[struct{}](1)
	f.set(func() { f.mirrorRead = read })
	watch <- bookmark
	read.Await(t, "the snapshot's resolve")
	f.set(func() { f.mirrorRead = nil })
}

// A tail on a deployment picks up a pod added to the mirror after it opened,
// from the tail's start, and goes on after one is deleted.
func TestATailTracksTheDeploymentsPods(t *testing.T) {
	t.Parallel()
	s := newLogServer()
	f := webMirror(t)
	svc, _ := serviceOver(t, f, s)
	stream, _ := openTail(t, svc, f)

	s.Add("prod/web-1/app", logstest.Stamped(time.Hour+time.Second, "from web-1"))
	assert.Equal(t, "from web-1", recvLine(t, stream, "web-1's line").Message)

	f.store.Apply(t, web3())
	f.pods <- podFrame(cluster.CollectionWatchFrameAdded, "web-3")
	s.Add("prod/web-3/app", logstest.Stamped(time.Hour+2*time.Second, "from web-3"))
	assert.Equal(t, "from web-3", recvLine(t, stream, "web-3's line").Message)

	f.store.Delete(t, web1())
	f.pods <- podFrame(cluster.CollectionWatchFrameDeleted, "web-1")
	s.Add("prod/web-2/app", logstest.Stamped(time.Hour+3*time.Second, "from web-2"))
	assert.Equal(t, "from web-2", recvLine(t, stream, "web-2's line").Message)

	var sinces []string
	for _, q := range s.Requests() {
		if q.Get("sinceTime") != "" {
			sinces = append(sinces, q.Get("sinceTime"))
		}
	}
	assert.Equal(t, []string{"2026-10-08T11:00:00Z", "2026-10-08T11:00:00Z", "2026-10-08T11:00:00Z"}, sinces)
}

// A tail with no cursor reads each log from the moment it started.
func TestATailOpensAtItsStart(t *testing.T) {
	t.Parallel()
	s := newLogServer()
	s.Add("prod/web-1/app", logstest.Stamped(0, "old"))
	f := webMirror(t)
	svc, _ := serviceOver(t, f, s)
	stream, _ := openTail(t, svc, f)

	s.Add("prod/web-1/app", logstest.Stamped(time.Hour, "new"))

	assert.Equal(t, "new", recvLine(t, stream, "the new line").Message)
	assert.Equal(t, "2026-10-08T11:00:00Z", s.Requests()[0].Get("sinceTime"))
}

// A stream Seen names reads past its line, and every other from Since: the
// named line's moment is delivered for the other stream alone.
func TestATailReadsEachSeenStreamPastItsLine(t *testing.T) {
	t.Parallel()
	s := newLogServer()
	s.Add("prod/web-1/app", logstest.Stamped(time.Second, "a1"), logstest.Stamped(2*time.Second, "a2"))
	s.Add("prod/web-2/app", logstest.Stamped(time.Second, "b1"), logstest.Stamped(2*time.Second, "b2"))
	svc, _ := serviceOver(t, webMirror(t), s)

	stream := startTail(t, t.Context(), svc, TailRequest{Scope: webScope, Since: moment(0), Seen: seenAt("web-1", "app", time.Second)})

	assert.ElementsMatch(t, []string{"a2", "b1", "b2"}, recvLines(t, stream, 3))
}

// A workload's follow reads a pod deleted since, Seen naming a line of it, from the
// file past its line.
func TestATailReadsAGonePodSeenNames(t *testing.T) {
	t.Parallel()
	s := newLogServer()
	s.Add("prod/web-1/app", logstest.Stamped(time.Second, "a1"), logstest.Stamped(2*time.Second, "a2"))
	s.Add("prod/web-2/app", logstest.Stamped(time.Second, "b1"))
	svc, f := serviceOver(t, webMirror(t), s)
	page := fetch(t, svc, FetchRequest{Scope: webScope, PageFrom: PageFromFirst, PageSize: 10})
	require.Equal(t, []string{"a1", "b1", "a2"}, messages(page.Records))
	f.store.Delete(t, web1())

	stream := startTail(t, t.Context(), svc, TailRequest{Scope: webScope, Since: moment(0), Seen: seenAt("web-1", "app", time.Second)})

	assert.ElementsMatch(t, []string{"a2", "b1"}, recvLines(t, stream, 2))
}

// A workload's follow from a moment reads the pods it no longer runs from the file
// first, found by their chain though nothing names them, then follows the live ones.
func TestATailReadsAWorkloadsGonePodsFirst(t *testing.T) {
	t.Parallel()
	s := newLogServer()
	s.Add("prod/web-1/app", logstest.Stamped(time.Second, "a1"), logstest.Stamped(3*time.Second, "a3"))
	s.Add("prod/web-2/app", logstest.Stamped(2*time.Second, "b2"))
	svc, f := serviceOver(t, webMirror(t), s)
	page := fetch(t, svc, FetchRequest{Scope: webScope, PageFrom: PageFromFirst, PageSize: 10})
	require.Equal(t, []string{"a1", "b2", "a3"}, messages(page.Records))
	f.store.Delete(t, web1())

	stream := startTail(t, t.Context(), svc, TailRequest{Scope: webScope, Since: moment(0)})

	assert.Equal(t, []string{"a1", "a3", "b2"}, recvLines(t, stream, 3))
}

// A tail from a page's Follow reads each stream where the page left it: one
// the page passed past its last line, one it did not from the window's start.
func TestATailFromAPagesFollow(t *testing.T) {
	t.Parallel()
	s := newLogServer()
	s.Add("prod/web-1/app", logstest.Stamped(time.Second, "a1"))
	svc, _ := serviceOver(t, webMirror(t), s)
	page := fetch(t, svc, FetchRequest{Scope: webScope, Window: Window{Since: moment(0)}, PageFrom: PageFromFirst})
	require.Equal(t, []string{"a1"}, messages(page.Records))
	s.Add("prod/web-1/app", logstest.Stamped(2*time.Second, "a2"))
	s.Add("prod/web-2/app", logstest.Stamped(500*time.Millisecond, "b0"))

	stream := startTail(t, t.Context(), svc, TailRequest{Cursor: page.Page.Follow})

	assert.ElementsMatch(t, []string{"b0", "a2"}, recvLines(t, stream, 2))
}

// A tail takes a cursor's start and ignores its end: from a Follow whose
// window's Until is past, from one whose counted After is met, and from a Next of
// a bounded window, it delivers the lines past the window's end (rule 3).
func TestATailIgnoresItsCursorsEnd(t *testing.T) {
	t.Parallel()
	bounded := Window{Since: moment(0), Until: moment(time.Second)}
	follow := func(w Window, after int) func(*Service) *string {
		return func(*Service) *string {
			return encoded(pageCursor{
				Scope: web1Scope, newer: true, after: after,
				resolvedWindow: resolvedWindow{Window: w, ResolvedAt: at(time.Hour), Start: logline.MomentAt(at(0))},
			})
		}
	}
	next := func(svc *Service) *string {
		return fetch(t, svc, FetchRequest{Scope: web1Scope, Window: bounded, PageFrom: PageFromFirst, PageSize: 1}).Page.Next
	}
	for name, tc := range map[string]struct {
		cursor func(*Service) *string
		want   []string
	}{
		"an until in the past":       {follow(bounded, 0), []string{"0", "1", "2", "3"}},
		"a counted after met":        {follow(Window{Since: moment(0), Until: moment(time.Second), After: Padding{Lines: 1}}, 1), []string{"0", "1", "2", "3"}},
		"a next of a bounded window": {next, []string{"1", "2", "3"}},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			s := newLogServer()
			s.Add("prod/web-1/app", logstest.Stamped(0, "0"), logstest.Stamped(time.Second, "1"), logstest.Stamped(2*time.Second, "2"), logstest.Stamped(3*time.Second, "3"))
			svc, _ := serviceOver(t, webMirror(t), s)

			stream := startTail(t, t.Context(), svc, TailRequest{Cursor: tc.cursor(svc)})

			assert.Equal(t, tc.want, recvLines(t, stream, len(tc.want)))
		})
	}
}

// A tail echoes its request as it ran: from a cursor, the cursor's scope and
// its first page's clock; from a moment, its own scope and the service's clock.
func TestATailEchoesItsRequest(t *testing.T) {
	t.Parallel()
	s := newLogServer()
	s.Add("prod/web-1/app", logstest.Stamped(0, "0"), logstest.Stamped(time.Second, "1"))
	svc, _ := serviceOver(t, webMirror(t), s)
	page := fetch(t, svc, FetchRequest{Scope: web1Scope, Window: Window{Since: moment(0)}, PageFrom: PageFromFirst, PageSize: 1})
	require.NotNil(t, page.Page.Next)
	svc.now = func() time.Time { return at(2 * time.Hour) }

	resp, err := svc.TailFromCursor(t.Context(), "7", TailRequest{Cursor: page.Page.Next, ForModel: true})
	require.NoError(t, err)
	assert.Equal(t, TailRequest{Scope: web1Scope, Cursor: page.Page.Next, MaxMessageBytes: MaxMessageBytesCap, ForModel: true}, resp.Request)
	assert.Equal(t, at(time.Hour), resp.ResolvedAt)
	assert.Equal(t, []string{"1"}, recvLines(t, resp.Frames, 1))

	resp, err = svc.TailFromCursor(t.Context(), "7", TailRequest{Scope: web1Scope, Since: moment(0), MaxMessageBytes: 10})
	require.NoError(t, err)
	assert.Equal(t, TailRequest{Scope: web1Scope, Since: moment(0), MaxMessageBytes: 10}, resp.Request)
	assert.Equal(t, at(2*time.Hour), resp.ResolvedAt)
}

// A tail greps every line it sends: a padding line on either side of the
// cursor's range that fails the grep, which a poll passes, is not sent.
func TestATailGrepsTheCursorsPadding(t *testing.T) {
	t.Parallel()
	s := newLogServer()
	s.Add("prod/web-1/app", logstest.Stamped(0, "pad 0"), logstest.Stamped(time.Second, "keep 1"), logstest.Stamped(2*time.Second, "pad 2"), logstest.Stamped(3*time.Second, "keep 3"))
	svc, _ := serviceOver(t, webMirror(t), s)
	scope := web1Scope
	scope.Grep = "^keep"
	w := Window{Since: moment(time.Second), Until: moment(time.Second), Before: Padding{Span: time.Second}, After: Padding{Span: time.Second}}
	poll := fetch(t, svc, FetchRequest{Scope: scope, Window: w, PageFrom: PageFromFirst, PageSize: 1})
	require.Equal(t, []string{"pad 0"}, messages(poll.Records), "a poll passes padding")
	c := encodePageCursor(pageCursor{Scope: scope, newer: true, resolvedWindow: cursorIn(t, poll.Page.Next).resolvedWindow})

	stream := startTail(t, t.Context(), svc, TailRequest{Cursor: &c})

	assert.Equal(t, []string{"keep 1", "keep 3"}, recvLines(t, stream, 2))
}

// A tail cuts each message at MaxMessageBytes, saying the bytes cut; 0 is the
// service's own cap, past which none is wider, and a negative is bad-page (rule 4).
func TestATailCutsItsMessages(t *testing.T) {
	t.Parallel()
	long := strings.Repeat("x", 100)
	for name, tc := range map[string]struct {
		max  int
		want []string
		cut  []int
	}{
		"a cap":      {3, []string{"xxx", "ab"}, []int{97, 0}},
		"no cap":     {0, []string{long, "ab"}, []int{0, 0}},
		"over a cap": {MaxMessageBytesCap + 1, []string{long, "ab"}, []int{0, 0}},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			s := newLogServer()
			s.Add("prod/web-1/app", logstest.Stamped(0, long), logstest.Stamped(time.Second, "ab"))
			svc, _ := serviceOver(t, webMirror(t), s)

			stream := startTail(t, t.Context(), svc, TailRequest{Scope: web1Scope, Since: moment(0), MaxMessageBytes: tc.max})

			recs := recvRecords(t, stream, 2)
			assert.Equal(t, tc.want, messages(recs))
			assert.Equal(t, tc.cut, []int{recs[0].TruncatedBytes, recs[1].TruncatedBytes})
		})
	}

	svc, _ := serviceOver(t, webMirror(t), newLogServer())
	_, err := svc.TailFromCursor(t.Context(), "7", TailRequest{Scope: web1Scope, MaxMessageBytes: -1})
	assert.Equal(t, refusal.CodeBadPage, codeOf(err))
}

// A tail for the model redacts each message before it cuts it, as a page does;
// one for the viewer cuts the stored line.
func TestATailRedactsAMessageBeforeItIsCut(t *testing.T) {
	t.Parallel()
	s := newLogServer()
	s.Add("prod/web-1/app", logstest.Stamped(0, "xxxxxxxxxx \x1b[31msk-"+strings.Repeat("A", 30)+"\x1b[0m"))
	svc, _ := serviceOver(t, webMirror(t), s)

	stream := startTail(t, t.Context(), svc, TailRequest{Scope: web1Scope, Since: moment(0), MaxMessageBytes: 20, ForModel: true})
	recs := recvRecords(t, stream, 1)
	assert.Equal(t, "xxxxxxxxxx [redacted", recs[0].Message)
	assert.Equal(t, 1, recs[0].TruncatedBytes)

	stream = startTail(t, t.Context(), svc, TailRequest{Scope: web1Scope, Since: moment(0), MaxMessageBytes: 20})
	assert.Equal(t, "xxxxxxxxxx \x1b[31msk-A", recvRecords(t, stream, 1)[0].Message)
}

// A tail's walk stamps nothing: what it sends is in the viewer's window, so the file
// keeps it only while the budget allows.
func TestATailStampsNothing(t *testing.T) {
	t.Parallel()
	f := webMirror(t)
	svc, _ := serviceIn(t, f, newLogServer(), t.TempDir(), testRetention)
	seedUnread(t, svc, f, rowOf("web-1", "app"), logstore.Fill{Lines: fileLines(0, time.Second), AtBeginning: true})

	stream := startTail(t, t.Context(), svc, TailRequest{Scope: web1Scope, Since: moment(-time.Second)})
	assert.Equal(t, "0", recvLine(t, stream, "the first line").Message)
	assert.Equal(t, "1", recvLine(t, stream, "the second line").Message)

	assert.Zero(t, chunksOf(t, svc, f, "web-1", "app")[0].ReadAt)
}

// A tail and its tailers hold the rows of the streams they read until they end, so
// a sweep while the tail runs leaves a row with nothing in it yet; once they have
// ended, the sweep takes it.
func TestATailHoldsItsRows(t *testing.T) {
	t.Parallel()
	backend := newFakeBackend()
	backend.set("web-1/app", fakeRead{wait: never})
	svc, f := serviceOn(t, backend)
	everyPodGone(svc)
	_, cancel := openTailOf(t, svc, f, web1Scope)
	backend.opened.Await(t, "the tailer's read")

	sweepFile(t, svc, f)
	assert.Len(t, rowsOf(t, svc, f, "web-1"), 1, "held while the tail runs")

	cancel()
	require.Eventually(t, func() bool {
		sweepFile(t, svc, f)
		return len(rowsOf(t, svc, f, "web-1")) == 0
	}, testutil.Timeout, time.Millisecond, "let go once the tail and its tailer end")
}

// A tail from a moment reads every line from it first, a line stamped at the
// moment included.
func TestATailFromAMoment(t *testing.T) {
	t.Parallel()
	s := newLogServer()
	s.Add("prod/web-1/app", logstest.Stamped(0, "before"), logstest.Stamped(time.Second, "at"), logstest.Stamped(2*time.Second, "after"))
	svc, _ := serviceOver(t, webMirror(t), s)

	stream := startTail(t, t.Context(), svc, TailRequest{Scope: webScope, Since: moment(time.Second)})

	assert.Equal(t, []string{"at", "after"}, recvLines(t, stream, 2))
}

// A tail sends only the lines its grep matches: the kubelet reads every line,
// and the grep runs where the lines are read.
func TestATailGrepsWhatItSends(t *testing.T) {
	t.Parallel()
	s := newLogServer()
	f := webMirror(t)
	svc, _ := serviceOver(t, f, s)
	view := webScope
	view.Grep = "^keep"
	stream, _ := openTailOf(t, svc, f, view)

	s.Add("prod/web-1/app", logstest.Stamped(time.Hour, "skip me"), logstest.Stamped(time.Hour+time.Second, "keep me"))

	assert.Equal(t, "keep me", recvLine(t, stream, "the matching line").Message)
	assert.Empty(t, s.RequestsOf("prod/web-1/app")[0].Get("grep"))
}

// recvLine waits for stream's next frame, a line, and answers it.
func recvLine(t *testing.T, stream *cluster.Stream[TailFrame], what string) logline.Record {
	t.Helper()
	frame := testutil.Recv(t, stream.Frames, what)
	require.NotNil(t, frame.Record, "%s: %+v", what, frame)
	return *frame.Record
}

// endedWith waits for stream's next frame, its Ended, then for it to end clean,
// and answers the Ended refusal.
func endedWith(t *testing.T, stream *cluster.Stream[TailFrame]) refusal.Refusal {
	t.Helper()
	frame := testutil.Recv(t, stream.Frames, "the tail's end")
	require.NotNil(t, frame.Ended, "%+v", frame)
	testutil.RecvClosed(t, stream.Frames, "the tail to end")
	require.NoError(t, stream.Err())
	return *frame.Ended
}

// failsWith waits for stream to end with no Ended frame and answers its Err.
func failsWith(t *testing.T, stream *cluster.Stream[TailFrame]) error {
	t.Helper()
	testutil.RecvClosed(t, stream.Frames, "the tail to end")
	return stream.Err()
}

// awaitClosed waits for key's first tail to end, and awaitReadAgain for a read of
// key from since.
func awaitClosed(t *testing.T, s *logstest.Server, key string) {
	t.Helper()
	require.Eventually(t, func() bool { return s.FollowsClosed(key) >= 1 },
		testutil.Timeout, time.Millisecond, key+"'s stream to end")
}

func awaitReadAgain(t *testing.T, s *logstest.Server, key, since string) {
	t.Helper()
	require.Eventually(t, func() bool {
		return slices.ContainsFunc(s.RequestsOf(key), func(q url.Values) bool { return q.Get("sinceTime") == since })
	}, testutil.Timeout, time.Millisecond, "a read of "+key+" again from "+since)
}

// A cluster disabled under a tail ends it, saying so.
func TestATailEndsWhenTheClusterIsDisabled(t *testing.T) {
	t.Parallel()
	f := webMirror(t)
	svc, _ := serviceOver(t, f, newLogServer())
	f.frames = make(chan cluster.ClusterCollectionWatchFrame)
	stream, _ := openTail(t, svc, f)

	disabled := enabledCluster()
	disabled.Spec.Enabled = false
	f.frames <- cluster.ClusterCollectionWatchFrame{Type: cluster.CollectionWatchFrameModified, Cluster: disabled}

	assert.Equal(t, kubelet.ErrRevoked, endedWith(t, stream))
}

// A 401 on a stream the tail opens ends it: the user's credentials, not a pod's
// state.
func TestATailEndsOnAnUnauthorizedStream(t *testing.T) {
	t.Parallel()
	s := newLogServer()
	s.Refuse["prod/web-1/app"] = logstest.Denial{Code: http.StatusUnauthorized, Message: "unauthorized"}
	svc, _ := serviceOver(t, webMirror(t), s)
	stream := openEndingTail(t, svc)

	assert.Equal(t, refusal.New(refusal.CodeUnauthorized, "the cluster refused to show this log: unauthorized"), endedWith(t, stream))
}

// A 403 is one namespace's, so it ends its own stream alone, and the tail ends
// with it only once every stream has: then tailing again would end the same way.
func TestAForbiddenStreamEndsItselfAlone(t *testing.T) {
	t.Parallel()
	s := newLogServer()
	s.Refuse["prod/web-2/app"] = logstest.Denial{Code: http.StatusForbidden, Message: "forbidden"}
	f := webMirror(t)
	svc, _ := serviceOver(t, f, s)
	stream, _ := openTail(t, svc, f)

	s.Add("prod/web-1/app", logstest.Stamped(time.Hour+time.Second, "from web-1"))
	// web-2's refusal and web-1's line race each other.
	var got []string
	for range 2 {
		frame := testutil.Recv(t, stream.Frames, "web-2's refusal and web-1's line")
		switch {
		case frame.Record != nil:
			got = append(got, frame.Record.Message)
		case frame.Notice != nil:
			got = append(got, frame.Notice.Message)
		}
	}
	assert.ElementsMatch(t, []string{"from web-1", "web-2/app: forbidden"}, got)

	s.SetRefusal("prod/web-1/app", &logstest.Denial{Code: http.StatusForbidden, Message: "forbidden"})
	s.End("prod/web-1/app")
	recvNotice(t, stream, "web-1's refusal")
	assert.Equal(t, refusal.New(refusal.CodeUnauthorized, "the cluster refused to show this log: forbidden"), endedWith(t, stream))
}

// A tail that ends on its own, with its caller still waiting, ends its pods
// watch too, and keeps the reason it ended.
func TestATailThatEndsReleasesItsWatch(t *testing.T) {
	t.Parallel()
	s := newLogServer()
	s.Refuse["prod/web-1/app"] = logstest.Denial{Code: http.StatusForbidden, Message: "forbidden"}
	s.Refuse["prod/web-2/app"] = logstest.Denial{Code: http.StatusForbidden, Message: "forbidden"}
	f := webMirror(t)
	svc, _ := serviceOver(t, f, s)
	f.podsDone = make(chan struct{})
	stream := openEndingTail(t, svc)

	recvNotice(t, stream, "one stream's refusal")
	recvNotice(t, stream, "the other's")
	assert.Equal(t, refusal.New(refusal.CodeUnauthorized, "the cluster refused to show this log: forbidden"), endedWith(t, stream))
	testutil.Wait(t, f.podsDone, "the pods watch to end")
}

// A tail whose every container has finished ends, saying so.
func TestATailEndsWhenEveryStreamIsGone(t *testing.T) {
	t.Parallel()
	s := newLogServer()
	f := webMirror(t)
	svc, _ := serviceOver(t, f, s)
	stream, _ := openTail(t, svc, f)

	f.store.Apply(t, finished(web1()))
	f.store.Apply(t, finished(web2()))
	s.End("prod/web-1/app")
	s.End("prod/web-2/app")

	assert.Equal(t, errStopped, endedWith(t, stream))
}

// A tail whose containers have all finished reads each to its end, sends their
// lines, then says every container stopped. Each fills on its own, so the order is
// per container alone.
func TestATailDeliversWhatItHeldBeforeItEnds(t *testing.T) {
	t.Parallel()
	s := newLogServer()
	s.Add("prod/web-1/app", logstest.Stamped(time.Second, "a"))
	s.Add("prod/web-2/app", logstest.Stamped(0, "b"))
	f := newMirror(t, append(webObjects(), finished(web1()), finished(web2()))...)
	svc, _ := serviceOver(t, f, s)
	s.End("prod/web-1/app")
	s.End("prod/web-2/app")

	stream := startTail(t, t.Context(), svc, TailRequest{Scope: webScope, Since: moment(-time.Second)})

	assert.ElementsMatch(t, []string{"a", "b"}, recvLines(t, stream, 2))
	assert.Equal(t, errStopped, endedWith(t, stream))
}

// A pod that joined the view between its resolving and the pods watch opening is
// followed once the watch's snapshot closes, though no frame names it after.
func TestATailReconcilesTheSnapshot(t *testing.T) {
	t.Parallel()
	s := newLogServer()
	f := webMirror(t)
	svc, _ := serviceOver(t, f, s)
	stream := startTail(t, t.Context(), svc, TailRequest{Scope: webScope})

	f.store.Apply(t, web3())
	f.pods <- podFrame(cluster.CollectionWatchFrameAdded, "web-3")
	f.pods <- bookmark

	s.Add("prod/web-3/app", logstest.Stamped(time.Hour+time.Second, "from web-3"))
	assert.Equal(t, "from web-3", recvLine(t, stream, "web-3's line").Message)
}

// A tail whose last pods leave the view waits for the next, as a Deployment
// scaled to zero and back has it.
func TestATailWaitsWhenItsLastPodsLeave(t *testing.T) {
	t.Parallel()
	s := newLogServer()
	f := webMirror(t)
	svc, _ := serviceOver(t, f, s)
	stream, _ := openTail(t, svc, f)

	f.store.Delete(t, web1())
	f.store.Delete(t, web2())
	f.pods <- podFrame(cluster.CollectionWatchFrameDeleted, "web-1")

	f.store.Apply(t, web3())
	f.pods <- podFrame(cluster.CollectionWatchFrameAdded, "web-3")
	s.Add("prod/web-3/app", logstest.Stamped(time.Hour+time.Second, "from web-3"))
	assert.Equal(t, "from web-3", recvLine(t, stream, "web-3's line").Message)
}

// A tail of a view with no container yet waits for its pods rather than being
// refused, and reads them from the moment it started.
func TestATailOfAnEmptyViewWaitsForItsPods(t *testing.T) {
	t.Parallel()
	s := newLogServer()
	f := webMirror(t)
	f.store.Mirror(t, "batch/v1", "Job")
	svc, _ := serviceOver(t, f, s)
	stream, _ := openTailOf(t, svc, f, Scope{Sources: []ScopeSource{{Namespace: "prod", Kind: SourceJob, Name: "batch"}}})

	f.store.Apply(t, workload("batch/v1", "Job", "batch", "uid-batch", nil))
	f.store.Apply(t, pod("batch-1", "uid-batch-1", "", controller("batch/v1", "Job", "batch", "uid-batch"), container{name: "main"}))
	f.pods <- podFrame(cluster.CollectionWatchFrameAdded, "batch-1")
	s.Add("prod/batch-1/main", logstest.Stamped(time.Hour+time.Second, "started"))

	assert.Equal(t, "started", recvLine(t, stream, "batch-1's line").Message)
	assert.Equal(t, "2026-10-08T11:00:00Z", s.Requests()[0].Get("sinceTime"))
}

// A stream that ends clean on a container the view sees running is opened again
// past its last line: a proxy or the API server can close an idle stream.
func TestATailReopensAStreamClosedOnARunningContainer(t *testing.T) {
	t.Parallel()
	s := newLogServer()
	f := webMirror(t)
	svc, _ := serviceOver(t, f, s)
	stream, _ := openTail(t, svc, f)
	s.Add("prod/web-1/app", logstest.Stamped(time.Hour+time.Second, "before"))
	recvLine(t, stream, "web-1's line")

	s.End("prod/web-1/app")

	awaitReadAgain(t, s, "prod/web-1/app", "2026-10-08T11:00:01Z")
}

// A tail narrows its pods watch to the view's namespaces.
func TestATailWatchesOnlyItsNamespaces(t *testing.T) {
	t.Parallel()
	f := webMirror(t)
	svc, _ := serviceOver(t, f, newLogServer())
	view := Scope{Sources: []ScopeSource{
		{Namespace: "prod", Kind: SourcePod, Name: "web-1"},
		{Namespace: "prod", Kind: SourcePod, Name: "web-2"},
		{Namespace: "staging", Kind: SourcePod, Name: "web-1"},
	}}
	_, err := svc.TailFromCursor(t.Context(), "7", TailRequest{Scope: view})
	require.NoError(t, err)

	f.mu.Lock()
	defer f.mu.Unlock()
	assert.Equal(t, []string{"prod", "staging"}, f.podNamespaces)
}

// A tail of a pod ends when the pod is deleted, and when a pod of the same name
// with another UID stands in its place.
func TestATailOfAPodEndsWithThePod(t *testing.T) {
	t.Parallel()
	for name, replacement := range map[string]map[string]any{
		"gone":     nil,
		"replaced": pod("web-1", "uid-other", "node-a", nil, container{name: "app"}),
	} {
		t.Run(name, func(t *testing.T) {
			f := webMirror(t)
			svc, _ := serviceOver(t, f, newLogServer())
			stream, _ := openTailOf(t, svc, f, podView("web-1"))

			f.store.Delete(t, web1())
			if replacement != nil {
				f.store.Apply(t, replacement)
			}
			f.pods <- uidFrame(cluster.CollectionWatchFrameDeleted, "web-1", "uid-web-1")

			assert.Equal(t, refusal.New(refusal.CodeFinished, "pod prod/web-1 was deleted"), endedWith(t, stream))
		})
	}
}

// A pod deleted under a view that names other sources leaves them followed, and a
// pod of its name with another UID is not read in its place.
func TestADeletedPodLeavesItsViewsOtherSources(t *testing.T) {
	t.Parallel()
	s := newLogServer()
	soloOpens := testutil.NewProbe[string](4)
	s.OnOpen = func(key string) {
		if key == "prod/solo/app" {
			soloOpens.Fire(key)
		}
	}
	solo := pod("solo", "uid-a", "node-a", nil, container{name: "app"})
	f := newMirror(t, append(append(webObjects(), webNodes()...), solo)...)
	svc, _ := serviceOver(t, f, s)
	view := webScope
	view.Sources = append(slices.Clone(view.Sources), ScopeSource{Namespace: "prod", Kind: SourcePod, Name: "solo"})
	stream, _ := openTailOf(t, svc, f, view)
	soloOpens.Await(t, "solo's first tail")

	f.store.Delete(t, solo)
	f.store.Apply(t, pod("solo", "uid-b", "node-a", nil, container{name: "app"}))
	f.pods <- uidFrame(cluster.CollectionWatchFrameDeleted, "solo", "uid-a")
	// The API server ends a deleted pod's stream.
	s.End("prod/solo/app")
	f.store.Apply(t, web3())
	f.pods <- podFrame(cluster.CollectionWatchFrameAdded, "web-3")

	s.Add("prod/web-3/app", logstest.Stamped(time.Hour+time.Second, "from web-3"))
	assert.Equal(t, "from web-3", recvLine(t, stream, "web-3's line").Message)
	// A negative assertion, so a bounded window: a reopen would follow the stream's
	// end after one retry, and a hundred retries is far past a local request's round trip.
	testutil.NoRecv(t, soloOpens.Chan(), 100*svc.retry, "a tail of the pod that took solo's name")
}

// A pod deleted under a view that also reads it through its workload leaves the
// workload reading the pod that takes its name.
func TestADeletedPodLeavesItsWorkloadReadingItsReplacement(t *testing.T) {
	t.Parallel()
	s := newLogServer()
	db := controller("apps/v1", "StatefulSet", "db", "uid-db")
	first := pod("db-0", "uid-db-0", "node-a", db, container{name: "app"})
	f := newMirror(t, append(webNodes(), workload("apps/v1", "StatefulSet", "db", "uid-db", nil), first)...)
	svc, _ := serviceOver(t, f, s)
	stream, _ := openTailOf(t, svc, f, Scope{Sources: []ScopeSource{
		{Namespace: "prod", Kind: SourceStatefulSet, Name: "db"},
		{Namespace: "prod", Kind: SourcePod, Name: "db-0"},
	}})
	s.Add("prod/db-0/app", logstest.Stamped(time.Hour+time.Second, "from db-0"))
	recvLine(t, stream, "db-0's line")

	// On another node, so the tail ends the first pod's stream and opens the
	// replacement's at once.
	f.store.Delete(t, first)
	f.store.Apply(t, pod("db-0", "uid-db-0b", "node-b", db, container{name: "app"}))
	f.pods <- uidFrame(cluster.CollectionWatchFrameDeleted, "db-0", "uid-db-0")

	require.Eventually(t, func() bool { return s.FollowsOpened("prod/db-0/app") >= 2 },
		testutil.Timeout, time.Millisecond, "the replacement's stream to open")
}

// A tail of a pod ends when the pod is deleted, or a pod of its name with
// another UID stands in its place, with no Deleted frame naming it: before the
// pods watch opened, or while a cache's clear had it closed.
func TestATailOfAPodEndsWhenNoFrameNamesItsEnd(t *testing.T) {
	t.Parallel()
	for name, replacement := range map[string]map[string]any{
		"deleted":  nil,
		"replaced": pod("web-1", "uid-other", "node-a", nil, container{name: "app"}),
	} {
		end := func(f *fakeService) {
			f.store.Delete(t, web1())
			if replacement != nil {
				f.store.Apply(t, replacement)
			}
		}
		t.Run(name+" before the watch", func(t *testing.T) {
			f := webMirror(t)
			svc, _ := serviceOver(t, f, newLogServer())
			stream := startTail(t, t.Context(), svc, TailRequest{Scope: podView("web-1")})

			end(f)
			f.pods <- bookmark

			assert.Equal(t, refusal.New(refusal.CodeFinished, "pod prod/web-1 was deleted"), endedWith(t, stream))
		})
		t.Run(name+" across a cache clear", func(t *testing.T) {
			f := webMirror(t)
			svc, _ := serviceOver(t, f, newLogServer())
			// No snapshot closes before the clear, so no resolve it starts races the end.
			stream := startTail(t, t.Context(), svc, TailRequest{Scope: podView("web-1")})
			pods := make(chan cluster.ClusterCachedDataObjectCollectionWatchFrame)
			var ended chan cluster.ClusterCachedDataObjectCollectionWatchFrame
			f.set(func() { ended, f.pods = f.pods, pods })

			end(f)
			close(ended)
			// A replacement ends the tail at the resolve after the retry, before the
			// watches open again; a pod missing is only gone once a snapshot says so.
			if replacement == nil {
				pods <- bookmark
			}

			assert.Equal(t, refusal.New(refusal.CodeFinished, "pod prod/web-1 was deleted"), endedWith(t, stream))
		})
	}
}

// A view of several pods ends once every one of them is deleted, naming them.
func TestATailOfPodsEndsWhenEveryPodIsDeleted(t *testing.T) {
	t.Parallel()
	f := webMirror(t)
	svc, _ := serviceOver(t, f, newLogServer())
	view := Scope{Sources: []ScopeSource{
		{Namespace: "prod", Kind: SourcePod, Name: "web-1"},
		{Namespace: "prod", Kind: SourcePod, Name: "web-2"},
	}}
	stream, _ := openTailOf(t, svc, f, view)

	f.store.Delete(t, web1())
	f.store.Delete(t, web2())
	f.pods <- podFrame(cluster.CollectionWatchFrameDeleted, "web-1")
	f.pods <- podFrame(cluster.CollectionWatchFrameDeleted, "web-2")

	assert.Equal(t, refusal.New(refusal.CodeFinished, "pods prod/web-1, prod/web-2 were deleted"), endedWith(t, stream))
}

// A pod the watch deletes and the mirror still holds, as a cache's clear and
// relist send it, is followed on.
func TestATailOfAPodOutlastsAResync(t *testing.T) {
	t.Parallel()
	s := newLogServer()
	f := webMirror(t)
	svc, _ := serviceOver(t, f, s)
	stream, _ := openTailOf(t, svc, f, podView("web-1"))

	f.pods <- uidFrame(cluster.CollectionWatchFrameDeleted, "web-1", "uid-web-1")
	f.pods <- uidFrame(cluster.CollectionWatchFrameAdded, "web-1", "uid-web-1")
	s.Add("prod/web-1/app", logstest.Stamped(time.Hour+time.Second, "still here"))

	assert.Equal(t, "still here", recvLine(t, stream, "web-1's line").Message)
}

// A crash-looping container, waiting to start again, keeps the tail open after
// its stream is refused, says nothing of the refusal, and is read once it runs.
func TestATailWaitsForAWaitingContainer(t *testing.T) {
	t.Parallel()
	s := newLogServer()
	s.Refuse["prod/crash/app"] = logstest.Denial{Code: http.StatusBadRequest, Message: "container is waiting to start"}
	f := newMirror(t, append(webObjects(), waiting(pod("crash", "uid-crash", "node-a", nil, container{name: "app", restarted: true})))...)
	svc, _ := serviceOver(t, f, s)
	stream, _ := openTailOf(t, svc, f, podView("crash"))
	require.Eventually(t, func() bool { return len(s.RequestsOf("prod/crash/app")) >= 1 }, testutil.Timeout, time.Millisecond, "crash asked")
	// A negative assertion, so a bounded window: the tail would end or send the
	// notice at once, and a hundred retries is far past it.
	testutil.NoRecv(t, stream.Frames, 100*svc.retry, "a frame for the refusal")

	s.SetRefusal("prod/crash/app", nil)
	f.store.Apply(t, pod("crash", "uid-crash", "node-a", nil, container{name: "app", restarted: true}))
	f.pods <- podFrame(cluster.CollectionWatchFrameModified, "crash")
	s.Add("prod/crash/app", logstest.Stamped(time.Hour+time.Second, "started"))

	assert.Equal(t, "started", recvLine(t, stream, "crash's line").Message)
}

// An init container that finishes while the main one has yet to start does not
// end the tail of their pod: the main container has no log yet, but will.
func TestAFinishedInitContainerLeavesItsPodFollowed(t *testing.T) {
	t.Parallel()
	s := newLogServer()
	boot := func() map[string]any { return pod("boot", "uid-boot", "node-a", nil, container{name: "app"}) }
	f := newMirror(t, append(webObjects(), withInit(waiting(boot()), "init"))...)
	svc, _ := serviceOver(t, f, s)
	stream, _ := openTailOf(t, svc, f, Scope{Sources: []ScopeSource{{Namespace: "prod", Kind: SourcePod, Name: "boot", AllContainers: true}}})
	require.Eventually(t, func() bool { return len(s.RequestsOf("prod/boot/init")) >= 1 }, testutil.Timeout, time.Millisecond, "init read to its end")
	// A negative assertion, so a bounded window: the tail would end on the init
	// container's end at once, and a hundred retries is far past it.
	testutil.NoRecv(t, stream.Frames, 100*svc.retry, "the tail to end")

	f.store.Apply(t, withInit(boot(), "init"))
	f.pods <- podFrame(cluster.CollectionWatchFrameModified, "boot")
	s.Add("prod/boot/app", logstest.Stamped(time.Hour+time.Second, "started"))

	assert.Equal(t, "started", recvLine(t, stream, "app's line").Message)
}

// A resolve reads one state of the mirror, whatever commits between its statements:
// a container that starts while the tail asks whether any is starting is still
// waiting to it, never a container that neither runs nor starts, which would end
// the tail.
func TestAResolveReadsOneStateOfTheMirror(t *testing.T) {
	t.Parallel()
	s := newLogServer()
	boot := func() map[string]any { return pod("boot", "uid-boot", "node-a", nil, container{name: "app"}) }
	f := newMirror(t, append(webObjects(), withInit(waiting(boot()), "init"))...)
	svc, _ := serviceOver(t, f, s)
	// The finished init container is read once, by a fill that ends on its own. Held at
	// the gate, the fill's end, and the resolve it starts, wait for the hook.
	gate := make(chan struct{})
	p := &logstest.Pauser{Gate: gate}
	svc.backends = []namedBackend{{"kubelet", func(get kubelet.Getter) kubelet.Backend { return kubelet.New(p.On(get)) }}}
	stream, _ := openTailOf(t, svc, f, Scope{Sources: []ScopeSource{{Namespace: "prod", Kind: SourcePod, Name: "boot", AllContainers: true}}})
	var started sync.Once
	read := testutil.NewProbe[struct{}](1)
	f.set(func() {
		f.beforeStatement = func(text string) {
			if strings.Contains(text, "coalesce(c.container_id, '') = ''") {
				started.Do(func() {
					f.store.Apply(t, withInit(boot(), "init"))
					// Armed here, since the fill checks its lines against the mirror
					// first: the resolve this statement is part of is the reading in
					// flight, so the first to return.
					f.set(func() { f.mirrorRead = read })
				})
			}
		}
	})

	close(gate)
	// Once that resolve has read the mirror, the tail either ends or waits on its
	// watches.
	read.Await(t, "the resolve after init's read")
	select {
	case f.pods <- podFrame(cluster.CollectionWatchFrameModified, "boot"):
	case frame := <-stream.Frames:
		t.Fatalf("the tail sent %+v while its pod was starting", frame)
	}
	s.Add("prod/boot/app", logstest.Stamped(time.Hour+time.Second, "started"))

	assert.Equal(t, "started", recvLine(t, stream, "app's line").Message)
}

// A mirror answer cut at its bounds ends the tail, since asking again would be
// cut the same way.
func TestATailEndsOnACutMirrorAnswer(t *testing.T) {
	t.Parallel()
	f := webMirror(t)
	svc, _ := serviceOver(t, f, newLogServer())
	stream, _ := openTail(t, svc, f)

	f.set(func() { f.moreOn = "FROM objects p" })
	f.pods <- podFrame(cluster.CollectionWatchFrameAdded, "web-3")

	assert.Equal(t, errCut, endedWith(t, stream))
}

// A mirror watch that ends clean, as a cache's clear ends it, is opened again after
// the retry, and the tail goes on with what its new snapshot shows.
func TestATailOutlastsACacheClear(t *testing.T) {
	t.Parallel()
	for _, watch := range []string{"pods", "nodes"} {
		t.Run(watch, func(t *testing.T) {
			s := newLogServer()
			f := webMirror(t)
			svc, _ := serviceOver(t, f, s)
			stream, _ := openTail(t, svc, f)

			pods := make(chan cluster.ClusterCachedDataObjectCollectionWatchFrame)
			var ended chan cluster.ClusterCachedDataObjectCollectionWatchFrame
			f.set(func() {
				ended = f.nodes
				if watch == "pods" {
					ended, f.pods = f.pods, pods
				} else {
					f.pods, f.nodes = pods, make(chan cluster.ClusterCachedDataObjectCollectionWatchFrame)
				}
			})
			f.store.Apply(t, web3())
			close(ended)
			pods <- bookmark

			s.Add("prod/web-3/app", logstest.Stamped(time.Hour+time.Second, "from web-3"))
			assert.Equal(t, "from web-3", recvLine(t, stream, "web-3's line").Message)
		})
	}
}

// A tail ends when a mirror watch fails, with the watch's reason.
func TestATailEndsWithAFailedWatch(t *testing.T) {
	t.Parallel()
	f := webMirror(t)
	svc, _ := serviceOver(t, f, newLogServer())
	f.podsErr = errors.New("watch failed")
	stream, _ := openTail(t, svc, f)
	close(f.pods)
	assert.EqualError(t, failsWith(t, stream), "watch failed")
}

// A tail ends when a workload kind's watch fails, with the watch's reason.
func TestATailEndsWithAFailedWorkloadWatch(t *testing.T) {
	t.Parallel()
	f := webMirror(t)
	svc, _ := serviceOver(t, f, newLogServer())
	f.workloadsErr = errors.New("watch failed")
	stream, _ := openTail(t, svc, f)
	close(f.workload("replicasets"))
	assert.EqualError(t, failsWith(t, stream), "watch failed")
}

// A deployment's pod that reaches the mirror before its replica set is followed
// once the replica set arrives, though no pod frame says so.
func TestATailWatchesTheWorkloadsBetweenASourceAndItsPods(t *testing.T) {
	t.Parallel()
	s := newLogServer()
	f := newMirror(t, append([]map[string]any{workload("apps/v1", "Deployment", "web", "dep-web", nil), web1()}, webNodes()...)...)
	svc, _ := serviceOver(t, f, s)
	stream, _ := openTail(t, svc, f)
	replicaSets := f.workload("replicasets")
	closeSnapshot(t, f, replicaSets)

	f.store.Apply(t, workload("apps/v1", "ReplicaSet", "web-7d9f", "rs-web", controller("apps/v1", "Deployment", "web", "dep-web")))
	replicaSets <- cluster.ClusterCachedDataObjectCollectionWatchFrame{Type: cluster.CollectionWatchFrameAdded, Object: &cluster.ClusterCachedDataObject{
		APIVersion: "apps/v1", Kind: "ReplicaSet", Namespace: "prod", Name: "web-7d9f",
	}}

	s.Add("prod/web-1/app", logstest.Stamped(time.Hour+time.Second, "from web-1"))
	assert.Equal(t, "from web-1", recvLine(t, stream, "web-1's line").Message)
}

// A pod change the mirror cannot be read over leaves the tail as it is, while
// a mirror that fails ends it, and a cluster whose identity moved is one Kubetail no
// longer connects to.
func TestATailOnAMirrorThatChangesUnderIt(t *testing.T) {
	t.Parallel()
	s := newLogServer()
	f := webMirror(t)
	svc, _ := serviceOver(t, f, s)
	view := webScope
	view.Filters = []ScopeFilter{{Field: FilterZone, Values: []string{"eu-1a"}}}
	stream, _ := openTailOf(t, svc, f, view)

	// node-b goes first: the snapshot's resolve can land between the deletes, and
	// with node-a gone and node-b kept, web-1 would leave the view as filtered out.
	nodes := webNodes()
	f.store.Delete(t, nodes[1])
	f.store.Delete(t, nodes[0])
	f.pods <- podFrame(cluster.CollectionWatchFrameAdded, "web-3")
	s.Add("prod/web-1/app", logstest.Stamped(time.Hour, "still here"))
	assert.Equal(t, "still here", recvLine(t, stream, "web-1's line").Message)

	f = webMirror(t)
	svc, _ = serviceOver(t, f, newLogServer())
	stream, _ = openTail(t, svc, f)
	f.set(func() { f.queryErr = errors.New("db closed") })
	f.pods <- podFrame(cluster.CollectionWatchFrameAdded, "web-3")
	assert.EqualError(t, failsWith(t, stream), "db closed")

	f = webMirror(t)
	svc, _ = serviceOver(t, f, newLogServer())
	stream, _ = openTail(t, svc, f)
	f.set(func() {
		moved := *f.active.Cache
		moved.Spec.ServerUID = "uid-2"
		f.active.Cache = &moved
	})
	f.pods <- podFrame(cluster.CollectionWatchFrameAdded, "web-3")
	assert.Equal(t, kubelet.ErrRevoked, endedWith(t, stream))
}

// A frame with no object moves nothing: the tail resolves the view again only
// for a pod.
func TestAFrameWithNoObjectIsIgnored(t *testing.T) {
	t.Parallel()
	s := newLogServer()
	f := webMirror(t)
	svc, _ := serviceOver(t, f, s)
	stream, _ := openTail(t, svc, f)

	f.pods <- cluster.ClusterCachedDataObjectCollectionWatchFrame{Type: cluster.CollectionWatchFrameModified}
	f.store.Apply(t, web3())
	f.pods <- podFrame(cluster.CollectionWatchFrameAdded, "web-3")

	s.Add("prod/web-3/app", logstest.Stamped(time.Hour+time.Second, "from web-3"))
	assert.Equal(t, "from web-3", recvLine(t, stream, "web-3's line").Message)
}

// A tail cancelled by its context ends clean.
func TestACancelledTailEndsClean(t *testing.T) {
	t.Parallel()
	f := webMirror(t)
	svc, _ := serviceOver(t, f, newLogServer())
	stream, cancel := openTail(t, svc, f)

	cancel()

	assert.NoError(t, failsWith(t, stream))
}

// A tail whose context ends as it opens ends clean too, though the call it was
// in failed on the cancel.
func TestATailCancelledAsItOpensEndsClean(t *testing.T) {
	t.Parallel()
	f := webMirror(t)
	svc, _ := serviceOver(t, f, newLogServer())
	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)
	f.acquireErr = fmt.Errorf("acquire: %w", context.Canceled)
	f.onAcquire = cancel

	stream := startTail(t, ctx, svc, TailRequest{Scope: webScope})

	assert.NoError(t, failsWith(t, stream))
}

// A container that restarts is tailed again as its new instance once the old
// instance's stream ends, whether the restart reaches the mirror before that end or
// after it, and the new instance's lines go out under its own id. The old instance's
// tailer runs on meanwhile, and a line it writes reaches the tail at once.
func TestARestartedContainerIsTailedAgain(t *testing.T) {
	t.Parallel()
	for name, frameFirst := range map[string]bool{"frame first": true, "end first": false} {
		t.Run(name, func(t *testing.T) {
			s := newLogServer()
			f := webMirror(t)
			svc, _ := serviceOver(t, f, s)
			stream, _ := openTail(t, svc, f)
			s.Add("prod/web-1/app", logstest.Stamped(time.Hour+time.Second, "before"))
			recvLine(t, stream, "web-1's line")

			restarted := func() {
				restartWeb1(t, f, s, logstest.Stamped(time.Hour+500*time.Millisecond, "new"))
				f.pods <- podFrame(cluster.CollectionWatchFrameModified, "web-1")
			}
			if frameFirst {
				read := testutil.NewProbe[cluster.ClusterCacheHealth](8)
				f.set(func() { f.healthRead = read })
				restarted()
				read.Await(t, "the tail to resolve the view again")
				// The old read is past one line, so the line added lands in front of it.
				s.Add("prod/web-1/app", logstest.Stamped(time.Hour+1500*time.Millisecond, "meanwhile"))
				rec := recvLine(t, stream, "the old instance's line")
				assert.Equal(t, "meanwhile", rec.Message)
				assert.Equal(t, containerID("uid-web-1", "app", 0), rec.ContainerID)
				s.End("prod/web-1/app")
				awaitClosed(t, s, "prod/web-1/app")
			} else {
				s.End("prod/web-1/app")
				awaitClosed(t, s, "prod/web-1/app")
				restarted()
			}
			// The server ends every read of web-1 until the tail reads the new
			// instance alone: one of the old instance opened meanwhile would read the
			// new one's log.
			old, current := rowID(t, svc, f, "web-1", "app", 0), rowID(t, svc, f, "web-1", "app", 1)
			require.Eventually(t, func() bool {
				// The new instance's log holds the lines added since the restart too, which
				// the tail sends under its id meanwhile and must not wait on.
				select {
				case <-stream.Frames:
				default:
				}
				return tailStart(svc, f.active.Cache.ID, old) == nil && tailStart(svc, f.active.Cache.ID, current) != nil
			}, testutil.Timeout, time.Millisecond, "the new instance tailed alone")
			s.Resume("prod/web-1/app")

			s.Add("prod/web-1/app", logstest.Stamped(time.Hour+2*time.Second, "after"))
			rec := recvLine(t, stream, "the new instance's line")
			for rec.Message != "after" {
				rec = recvLine(t, stream, "the new instance's line")
			}
			assert.Equal(t, containerID("uid-web-1", "app", 1), rec.ContainerID)
		})
	}
}

// A container that has finished is read to its end once, and not again when its pod
// changes for another reason.
func TestAFinishedContainerIsNotReadAgain(t *testing.T) {
	t.Parallel()
	s := newLogServer()
	web1Reads := testutil.NewProbe[string](4)
	s.OnRequest = func(n int) {
		s.Mu.Lock()
		key := s.Keys[n]
		s.Mu.Unlock()
		if key == "prod/web-1/app" {
			web1Reads.Fire(key)
		}
	}
	f := newMirror(t, append(webObjects(), finished(web1()))...)
	svc, _ := serviceOver(t, f, s)
	stream, _ := openTail(t, svc, f)
	web1Reads.Await(t, "web-1 read to its end")

	f.pods <- podFrame(cluster.CollectionWatchFrameModified, "web-1")
	s.Add("prod/web-2/app", logstest.Stamped(time.Hour+time.Second, "from web-2"))

	assert.Equal(t, "from web-2", recvLine(t, stream, "web-2's line").Message)
	// A negative assertion, so a bounded window: a read would follow the pod frame's
	// resolve at once, and a hundred retries is far past a local request's round trip.
	testutil.NoRecv(t, web1Reads.Chan(), 100*svc.retry, "a read of web-1 again")
}

// A pod whose node's labels move keeps its tailer, and its next line carries the
// new labels.
func TestATailKeepsItsPlaceWhenANodesLabelsMove(t *testing.T) {
	t.Parallel()
	s := newLogServer()
	f := webMirror(t)
	svc, _ := serviceOver(t, f, s)
	stream, _ := openTail(t, svc, f)
	closeSnapshot(t, f, f.nodes)
	s.Add("prod/web-1/app", logstest.Stamped(time.Hour+time.Second, "before"))
	recvLine(t, stream, "web-1's line")

	moved := node("node-a", "eu", "eu-1c", "linux", "amd64")
	f.store.Apply(t, moved)
	f.nodes <- nodeFrame(cluster.CollectionWatchFrameModified, moved)

	// The node frame and a line's commit ping race into the tail's select, so a
	// line may still carry the old zone; keep writing until one carries the new.
	deadline := time.Now().Add(testutil.Timeout)
	for i := 2; ; i++ {
		s.Add("prod/web-1/app", logstest.Stamped(time.Hour+time.Duration(i)*time.Second, "after"))
		if recvLine(t, stream, "web-1's next line").Place.Zone == "eu-1c" {
			break
		}
		require.True(t, time.Now().Before(deadline), "a line carrying the new zone")
	}
	assert.Equal(t, 1, s.FollowsOpened("prod/web-1/app"))
}

// A node that leaves the zone a view filters on ends its pod's stream, and one
// that enters it opens one, with no pod frame between.
func TestATailTracksItsNodesLabels(t *testing.T) {
	t.Parallel()
	s := newLogServer()
	f := webMirror(t)
	svc, _ := serviceOver(t, f, s)
	view := webScope
	view.Filters = []ScopeFilter{{Field: FilterZone, Values: []string{"eu-1a"}}}
	stream, _ := openTailOf(t, svc, f, view)
	for _, n := range webNodes() {
		f.nodes <- nodeFrame(cluster.CollectionWatchFrameAdded, n)
	}
	closeSnapshot(t, f, f.nodes)
	s.Add("prod/web-1/app", logstest.Stamped(time.Hour+time.Second, "from web-1"))
	recvLine(t, stream, "web-1's line")

	a, b := node("node-a", "eu", "eu-1c", "linux", "amd64"), node("node-b", "eu", "eu-1a", "linux", "arm64")
	f.store.Apply(t, a)
	f.store.Apply(t, b)
	f.nodes <- nodeFrame(cluster.CollectionWatchFrameModified, a)
	f.nodes <- nodeFrame(cluster.CollectionWatchFrameModified, b)

	awaitClosed(t, s, "prod/web-1/app")
	s.Add("prod/web-2/app", logstest.Stamped(time.Hour+2*time.Second, "from web-2"))
	rec := recvLine(t, stream, "web-2's line")
	assert.Equal(t, "from web-2", rec.Message)
	assert.Equal(t, "eu-1a", rec.Place.Zone)
}

// Under a filter on a label, the nodes watch resolves the view again when its
// snapshot closes, and past it for a node that came, went, moved a label a line
// carries, or whose body did not load; a node whose other fields moved resolves
// nothing.
func TestANodeFrameMovesTheViewOnlyForItsLabels(t *testing.T) {
	t.Parallel()
	a := node("node-a", "eu", "eu-1a", "linux", "amd64")
	unloaded := nodeFrame(cluster.CollectionWatchFrameModified, a)
	unloaded.Object.RawJSON = ""
	unlabelled := nodeFrame(cluster.CollectionWatchFrameModified, a)
	unlabelled.Object.UID = "another field"
	for name, tc := range map[string]struct {
		frame cluster.ClusterCachedDataObjectCollectionWatchFrame
		moves bool
	}{
		"its snapshot closing": {bookmark, true},
		"the same labels":      {unlabelled, false},
		"a label moved":        {nodeFrame(cluster.CollectionWatchFrameModified, node("node-a", "eu", "eu-1a", "linux", "arm64")), true},
		"a new node":           {nodeFrame(cluster.CollectionWatchFrameAdded, node("node-b", "eu", "eu-1b", "linux", "amd64")), true},
		"a node gone":          {nodeFrame(cluster.CollectionWatchFrameDeleted, a), true},
		"a body not loaded":    {unloaded, true},
		"no object":            {cluster.ClusterCachedDataObjectCollectionWatchFrame{Type: cluster.CollectionWatchFrameModified}, false},
	} {
		t.Run(name, func(t *testing.T) {
			f := &cursorTail{nodes: map[string]logline.Place{}, scope: Scope{Filters: []ScopeFilter{{Field: FilterZone, Values: []string{"eu-1a"}}}}}
			done := make(chan cluster.ClusterCachedDataObjectCollectionWatchFrame)
			close(done)
			f.nodesMoved(nodeFrame(cluster.CollectionWatchFrameAdded, a), done)
			if tc.frame.Type != cluster.CollectionWatchFrameBookmark {
				require.True(t, f.nodesMoved(bookmark, done))
			}

			assert.Equal(t, tc.moves, f.nodesMoved(tc.frame, done))
		})
	}
}

// With no filter on a label, only a node a stream runs on can move the view: a
// frame for any other is not read.
func TestANodeNoStreamRunsOnIsNotRead(t *testing.T) {
	t.Parallel()
	a, b := node("node-a", "eu", "eu-1a", "linux", "amd64"), node("node-b", "eu", "eu-1b", "linux", "arm64")
	f := &cursorTail{nodes: map[string]logline.Place{}, nodesSynced: true, streams: map[logline.StreamKey]*tracked{}}
	key := logline.StreamKey{Pod: "web-1", Container: "app"}
	tr := f.track(key)
	tr.st, tr.h = stream{key: key, place: logline.Place{Node: "node-a"}}, &handle{}
	done := make(chan cluster.ClusterCachedDataObjectCollectionWatchFrame)
	close(done)

	assert.False(t, f.nodesMoved(nodeFrame(cluster.CollectionWatchFrameAdded, b), done))
	assert.NotContains(t, f.nodes, "node-b")
	assert.True(t, f.nodesMoved(nodeFrame(cluster.CollectionWatchFrameAdded, a), done))
	b = node("node-b", "eu", "eu-1c", "linux", "arm64")
	assert.False(t, f.nodesMoved(nodeFrame(cluster.CollectionWatchFrameModified, b), done))
}

// A pod that joins while the mirror is syncing is followed once it is ready
// again, with no frame after.
func TestATailAsksASyncingMirrorAgain(t *testing.T) {
	t.Parallel()
	s := newLogServer()
	f := webMirror(t)
	svc, _ := serviceOver(t, f, s)
	stream, _ := openTail(t, svc, f)

	read := testutil.NewProbe[cluster.ClusterCacheHealth](8)
	f.set(func() { f.health, f.healthRead = cluster.ClusterCacheHealth{Reason: "Connecting"}, read })
	f.store.Apply(t, web3())
	f.pods <- podFrame(cluster.CollectionWatchFrameAdded, "web-3")
	read.Await(t, "the tail to read the syncing cache")
	f.set(func() { f.health = cluster.ClusterCacheHealth{Reason: "Watching"} })

	s.Add("prod/web-3/app", logstest.Stamped(time.Hour+time.Second, "from web-3"))
	assert.Equal(t, "from web-3", recvLine(t, stream, "web-3's line").Message)
}

// A pod that leaves the view and comes back is read again, past the last line its
// stream sent.
func TestATailReopensAPodThatCameBack(t *testing.T) {
	t.Parallel()
	s := newLogServer()
	f := webMirror(t)
	svc, _ := serviceOver(t, f, s)
	stream, _ := openTail(t, svc, f)
	s.Add("prod/web-1/app", logstest.Stamped(time.Hour+time.Second, "before"))
	recvLine(t, stream, "web-1's first line")

	f.store.Delete(t, web1())
	f.pods <- podFrame(cluster.CollectionWatchFrameDeleted, "web-1")
	awaitClosed(t, s, "prod/web-1/app")
	f.store.Apply(t, web1())
	f.pods <- podFrame(cluster.CollectionWatchFrameAdded, "web-1")

	awaitReadAgain(t, s, "prod/web-1/app", "2026-10-08T11:00:01Z")
}

// A pod that joins a tail opened after a cursor is read from the cursor, like
// the pods it opened with, not from the moment the tail began.
func TestATailAfterACursorReadsANewPodFromIt(t *testing.T) {
	t.Parallel()
	s := newLogServer()
	f := webMirror(t)
	svc, _ := serviceOver(t, f, s)
	_, err := svc.TailFromCursor(t.Context(), "7", TailRequest{Scope: webScope, Since: moment(30 * time.Minute)})
	require.NoError(t, err)
	closeSnapshot(t, f, f.pods)

	f.store.Apply(t, web3())
	f.pods <- podFrame(cluster.CollectionWatchFrameAdded, "web-3")

	require.Eventually(t, func() bool { return len(s.Requests()) == 3 }, testutil.Timeout, time.Millisecond, "web-3's read")
	for _, q := range s.Requests() {
		assert.Equal(t, "2026-10-08T10:30:00Z", q.Get("sinceTime"))
	}
}

// A stream the server failed is opened again after the retry, and the tail
// goes on; one the server refused for the log's sake is not.
func TestATailReopensADroppedStream(t *testing.T) {
	t.Parallel()
	s := newLogServer()
	s.Refuse["prod/web-1/app"] = logstest.Denial{Code: http.StatusInternalServerError, Message: "kubelet restarting"}
	f := webMirror(t)
	svc, _ := serviceOver(t, f, s)
	stream, _ := openTail(t, svc, f)
	require.Eventually(t, func() bool { return len(s.RequestsOf("prod/web-1/app")) >= 2 }, testutil.Timeout, time.Millisecond, "web-1 asked again")

	s.SetRefusal("prod/web-1/app", nil)
	require.Eventually(t, func() bool { return s.FollowsOpened("prod/web-1/app") == 1 }, testutil.Timeout, time.Millisecond, "web-1 open")
	s.Add("prod/web-1/app", logstest.Stamped(time.Hour+time.Second, "back"))

	assert.Equal(t, "back", recvLine(t, stream, "web-1's line").Message)
}

// A stream whose tailer dropped is held for its backoff: a reconcile leaves it
// untailed, and keeps the tail open while it is the view's only stream, then
// tails it once the hold is over.
func TestAHeldStreamWaitsOutItsBackoff(t *testing.T) {
	t.Parallel()
	s := newLogServer()
	svc, fake := serviceOver(t, webMirror(t), s)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	c, err := kubelet.Acquire(ctx, fake, "7", "uid-1")
	require.NoError(t, err)
	view := Scope{Sources: []ScopeSource{{Namespace: "prod", Kind: SourcePod, Name: "web-1", Containers: []string{"app"}}}}
	tg, err := resolve(ctx, svc.clusters, "7", "", view, true)
	require.NoError(t, err)
	f := &cursorTail{
		s: svc, id: "7", scope: view, start: startAt(t, view, time.Hour), claim: c, file: fileOf(t, svc, fake), tails: svc.registryFor("7", tg),
		target: target{serverUID: "uid-1"}, out: make(chan TailFrame, 16), streams: map[logline.StreamKey]*tracked{},
		nodes: map[string]logline.Place{}, deleted: map[string]string{}, podUIDs: map[string]string{}, gone: map[string]bool{}, held: map[int64]func(){},
		dueHub: mpsc.New[logline.StreamKey](0, mpsc.WithKeepOpen()), endedHub: mpsc.New[*handle](0, mpsc.WithKeepOpen()),
	}
	// Held as a dropped stream is: at the row and instance the view wants.
	rows, err := fileStreams(ctx, f.file, tg.streams)
	require.NoError(t, err)
	web1 := logline.StreamKey{Namespace: "prod", Pod: "web-1", Container: "app", Previous: false}
	f.streams[web1] = &tracked{st: tg.streams[0], row: rows[0].ID, held: true}
	t.Cleanup(func() { f.detach(f.streams[web1]) })

	require.NoError(t, f.reconcile(ctx))
	assert.Nil(t, f.streams[web1].h)

	f.streams[web1].held = false
	require.NoError(t, f.reconcile(ctx))
	assert.NotNil(t, f.streams[web1].h)
}

// A dropped stream waits the retry, doubled for each drop in a row up to the cap;
// a stream that ran for the cap, or a line sent from it, starts the count again.
func TestADroppingStreamBacksOff(t *testing.T) {
	t.Parallel()
	f := &cursorTail{s: &Service{retry: time.Second, retryCap: time.Minute}, streams: map[logline.StreamKey]*tracked{}}
	tr := &tracked{}

	var waits []time.Duration
	for range 8 {
		waits = append(waits, f.backoff(tr, 0))
	}
	assert.Equal(t, []time.Duration{time.Second, 2 * time.Second, 4 * time.Second, 8 * time.Second, 16 * time.Second, 32 * time.Second, time.Minute, time.Minute}, waits)
	assert.Equal(t, time.Second, f.backoff(tr, time.Minute))

	key := logline.StreamKey{Namespace: "prod", Pod: "web-1", Container: "app"}
	f.streams[key], f.wanted, f.out = tr, []logline.StreamKey{key}, make(chan TailFrame, 2)
	f.batch = []logline.Record{{Stream: key}}
	require.True(t, f.flush(t.Context()))
	assert.Zero(t, tr.drops)
}

// A tail whose view grows past what Kubetail reads at once ends, saying so.
func TestATailEndsWhenItsViewGrowsTooLarge(t *testing.T) {
	t.Parallel()
	f := webMirror(t)
	svc, _ := serviceOver(t, f, newLogServer())
	stream, _ := openTail(t, svc, f)

	addPods(t, f, 99)
	f.pods <- podFrame(cluster.CollectionWatchFrameAdded, "many-0")

	assert.Equal(t, ErrTooMany, endedWith(t, stream))
}

// Every refusal ends a tail with its words, bare or inside the refusals that
// carry one.
func TestAsRefusalReadsEveryRefusal(t *testing.T) {
	t.Parallel()
	for _, err := range []error{
		ErrSyncing,
		refusal.Unmirrored{Refusal: ErrSyncing, Source: 1},
		refusal.NoNodes{Refusal: ErrSyncing},
		fmt.Errorf("wrapped: %w", ErrSyncing),
	} {
		r, ok := refusal.As(err)
		assert.True(t, ok, err)
		assert.Equal(t, ErrSyncing, r)
	}
	_, ok := refusal.As(errors.New("db closed"))
	assert.False(t, ok)
}

// A pod frame resolves the view at once with no settle; with one, the first frame
// arms one wait that the frames after it share, and a resolve clears it.
func TestAPodFrameSettles(t *testing.T) {
	t.Parallel()
	f := &cursorTail{s: &Service{}}
	assert.True(t, f.settle())

	f.s.settle = time.Hour
	assert.False(t, f.settle())
	armed := f.settled
	require.NotNil(t, armed)
	assert.False(t, f.settle())
	assert.Equal(t, armed, f.settled)
}

// A pod frame under a settle is followed once the settle has passed.
func TestATailResolvesOnceThePodFramesSettle(t *testing.T) {
	t.Parallel()
	s := newLogServer()
	f := webMirror(t)
	svc, _ := serviceOver(t, f, s)
	svc.settle = time.Millisecond
	stream, _ := openTail(t, svc, f)

	f.store.Apply(t, web3())
	f.pods <- podFrame(cluster.CollectionWatchFrameAdded, "web-3")
	s.Add("prod/web-3/app", logstest.Stamped(time.Hour+time.Second, "from web-3"))

	assert.Equal(t, "from web-3", recvLine(t, stream, "web-3's line").Message)
}

// tailWeb1 opens q, a tail of web-1's default container from its start.
// Its pods watch's snapshot stays open: the tail walks the file before it reads the
// watch, and a walk with lines to send waits on the test.
func tailWeb1(t *testing.T, svc *Service, q TailRequest) (*cluster.Stream[TailFrame], context.CancelFunc) {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)
	q.Scope = podView("web-1")
	stream := startTail(t, ctx, svc, q)
	return stream, cancel
}

// recvLines waits for stream's next n lines and answers their messages.
func recvLines(t *testing.T, stream *cluster.Stream[TailFrame], n int) []string {
	t.Helper()
	return messages(recvRecords(t, stream, n))
}

// A tail's lines come from the file, on the ping of the commit that wrote them, and
// a tail of a stream whose tailer has written nothing yet asks the kubelet for
// nothing but the tail.
func TestATailReadsItsTailersCommits(t *testing.T) {
	t.Parallel()
	s := newLogServer()
	f := webMirror(t)
	svc, _ := serviceOver(t, f, s)
	stream, _ := tailWeb1(t, svc, TailRequest{})

	s.Add("prod/web-1/app", logstest.Stamped(time.Hour+time.Second, "first"))

	assert.Equal(t, "first", recvLine(t, stream, "the first commit's line").Message)
	assert.Zero(t, fills(s, "prod/web-1/app"))
}

// A tail reopened from its first's start, with the last line it got as Seen,
// sends every line written past it and none before it: inside the grace from the
// file the tailer kept writing, and after it over a fresh tailer.
func TestAReopenWithSeenMissesNothing(t *testing.T) {
	t.Parallel()
	for name, grace := range map[string]time.Duration{"inside the grace": time.Hour, "after it": 0} {
		t.Run(name, func(t *testing.T) {
			s := newLogServer()
			f := webMirror(t)
			svc, _ := serviceOver(t, f, s)
			svc.grace = grace
			first, cancel := tailWeb1(t, svc, TailRequest{Since: moment(time.Hour)})
			s.Add("prod/web-1/app", logstest.Stamped(time.Hour+time.Second, "a"))
			a := recvLine(t, first, "a")
			cancel()
			if grace == 0 {
				awaitClosed(t, s, "prod/web-1/app")
			}

			s.Add("prod/web-1/app", logstest.Stamped(time.Hour+2*time.Second, "b"), logstest.Stamped(time.Hour+3*time.Second, "c"))
			second, _ := tailWeb1(t, svc, TailRequest{Since: moment(time.Hour), Seen: []string{a.Cursor()}})

			assert.Equal(t, []string{"b", "c"}, recvLines(t, second, 2))
			if grace > 0 {
				assert.Equal(t, 1, s.FollowsOpened("prod/web-1/app"), "the tailer read on")
			}
		})
	}
}

// A line a page writes below a tail's watermark is never sent.
func TestABackfillBelowTheWatermarkIsNotSent(t *testing.T) {
	t.Parallel()
	s := newLogServer()
	f := webMirror(t)
	svc, _ := serviceOver(t, f, s)
	stream, _ := tailWeb1(t, svc, TailRequest{})

	seed(t, svc, f, rowOf("web-1", "app"), logstore.Fill{Lines: fileLines(time.Second)})
	s.Add("prod/web-1/app", logstest.Stamped(time.Hour+time.Second, "tailed"))

	assert.Equal(t, "tailed", recvLine(t, stream, "the tailer's line").Message)
}

// liveGap is a tail of web-1, and its cancel, from 10:00 over a file holding web-1's line at 10:30,
// which its tailer resumes from, the kubelet holding lines between: a live gap past
// the watermark. atBeginning marks the line the kubelet's oldest, which loses the gap.
func liveGap(t *testing.T, s *logstest.Server, atBeginning bool, between ...string) (*Service, *fakeService, *cluster.Stream[TailFrame], context.CancelFunc) {
	t.Helper()
	s.Add("prod/web-1/app", between...)
	s.Add("prod/web-1/app", logstest.Stamped(30*time.Minute, "1800"))
	f := webMirror(t)
	svc, _ := serviceOver(t, f, s)
	seed(t, svc, f, rowOf("web-1", "app"), logstore.Fill{Lines: fileLines(30 * time.Minute), AtBeginning: atBeginning})
	stream, cancel := tailWeb1(t, svc, TailRequest{Since: moment(0)})
	return svc, f, stream, cancel
}

// A tail that meets a live gap fills it and sends its lines before the tailer's
// past it.
func TestATailFillsALiveGapBeforeItGoesOn(t *testing.T) {
	t.Parallel()
	s := newLogServer()
	_, _, stream, _ := liveGap(t, s, false, logstest.Stamped(10*time.Minute, "in the gap"))

	s.Add("prod/web-1/app", logstest.Stamped(40*time.Minute, "tailed"))

	assert.Equal(t, []string{"in the gap", "1800", "tailed"}, recvLines(t, stream, 3))
	assert.Equal(t, 1, fills(s, "prod/web-1/app"))
}

// holdFill holds key's first gap fill open on s until release is closed: asked
// fires once the fill has asked. Set it before any request.
func holdFill(t *testing.T, s *logstest.Server, key string) (asked *testutil.Signal, release chan struct{}) {
	asked, release = testutil.NewSignal(), make(chan struct{})
	var once sync.Once
	s.OnRequest = func(n int) {
		s.Mu.Lock()
		k, q := s.Keys[n], s.Queries[n]
		s.Mu.Unlock()
		if k != key || q.Get("follow") == "true" {
			return
		}
		once.Do(func() {
			asked.Fire()
			select {
			case <-release:
			case <-t.Context().Done():
			}
		})
	}
	return asked, release
}

// A gap fill held open holds up no other stream: the view's other container's lines
// arrive while it waits, and the gap's lines come once the fill ends.
func TestASlowFillHoldsUpNoOtherStream(t *testing.T) {
	t.Parallel()
	s := newLogServer()
	s.Add("prod/web-1/app", logstest.Stamped(10*time.Minute, "in the gap"), logstest.Stamped(30*time.Minute, "1800"))
	asked, release := holdFill(t, s, "prod/web-1/app")
	f := webMirror(t)
	svc, _ := serviceOver(t, f, s)
	seed(t, svc, f, rowOf("web-1", "app"), logstore.Fill{Lines: fileLines(30 * time.Minute)})
	stream := startTail(t, t.Context(), svc, TailRequest{Scope: webScope, Since: moment(0)})
	asked.Wait(t, "web-1's gap fill")

	s.Add("prod/web-2/app", logstest.Stamped(40*time.Minute, "from web-2"))
	assert.Equal(t, "from web-2", recvLine(t, stream, "web-2's line while web-1 fills").Message)

	close(release)
	assert.Equal(t, []string{"in the gap", "1800"}, recvLines(t, stream, 2))
}

// A container that finishes while its gap fill waits, the fill then failing on a
// transient error, is held, not ended: after its backoff the gap is filled again and
// its lines are sent before the tail ends.
func TestAFillThatFailsAfterItsContainerFinishedIsRetried(t *testing.T) {
	t.Parallel()
	s := newLogServer()
	s.Add("prod/web-1/app", logstest.Stamped(10*time.Minute, "in the gap"), logstest.Stamped(30*time.Minute, "1800"))
	asked, release := holdFill(t, s, "prod/web-1/app")
	f := webMirror(t)
	svc, _ := serviceOver(t, f, s)
	seed(t, svc, f, rowOf("web-1", "app"), logstore.Fill{Lines: fileLines(30 * time.Minute)})
	stream, _ := tailWeb1(t, svc, TailRequest{Since: moment(0)})
	closeSnapshot(t, f, f.pods)
	asked.Wait(t, "web-1's gap fill")

	f.store.Apply(t, finished(web1()))
	read := testutil.NewProbe[struct{}](1)
	f.set(func() { f.mirrorRead = read })
	f.pods <- podFrame(cluster.CollectionWatchFrameModified, "web-1")
	read.Await(t, "the finished pod's resolve")
	s.End("prod/web-1/app")
	read.Await(t, "the resolve after the tailer's end")
	f.set(func() { f.mirrorRead = nil })

	s.Mu.Lock()
	s.RefuseReads["prod/web-1/app"] = logstest.Denial{Code: http.StatusServiceUnavailable, Message: "busy"}
	s.Mu.Unlock()
	close(release)
	require.Eventually(t, func() bool { return fills(s, "prod/web-1/app") >= 2 },
		testutil.Timeout, time.Millisecond, "the fill retried")
	s.Mu.Lock()
	delete(s.RefuseReads, "prod/web-1/app")
	s.Mu.Unlock()

	assert.Equal(t, []string{"in the gap", "1800"}, recvLines(t, stream, 2))
	assert.Equal(t, errStopped, endedWith(t, stream))
}

// A tail cancelled while a gap fill is in flight ends clean once the fill returns,
// and lets go of what the fill wrote.
func TestATailCancelledWhileItFillsEndsClean(t *testing.T) {
	t.Parallel()
	s := newLogServer()
	asked, release := holdFill(t, s, "prod/web-1/app")
	_, _, stream, cancel := liveGap(t, s, false, logstest.Stamped(10*time.Minute, "in the gap"))
	asked.Wait(t, "web-1's gap fill")

	cancel()
	close(release)

	testutil.RecvClosed(t, stream.Frames, "the tail to end")
	assert.NoError(t, stream.Err())
}

// A gap fill the cluster refuses the user's credentials for ends the tail.
func TestAFillUnauthorizedEndsTheTail(t *testing.T) {
	t.Parallel()
	s := newLogServer()
	s.RefuseReads["prod/web-1/app"] = logstest.Denial{Code: http.StatusUnauthorized, Message: "unauthorized"}
	_, _, stream, _ := liveGap(t, s, false, logstest.Stamped(10*time.Minute, "in the gap"))

	ended := endedWith(t, stream)
	assert.Equal(t, refusal.CodeUnauthorized, ended.Code)
	assert.Contains(t, ended.Reason, "unauthorized")
}

// A stream that leaves the view while its gap fill is in flight sends nothing of what
// the fill read; the view's other streams go on.
func TestAFillOfAStreamThatLeftTheViewIsNotSent(t *testing.T) {
	t.Parallel()
	s := newLogServer()
	s.Add("prod/web-1/app", logstest.Stamped(10*time.Minute, "in the gap"), logstest.Stamped(30*time.Minute, "1800"))
	asked, release := holdFill(t, s, "prod/web-1/app")
	f := webMirror(t)
	svc, _ := serviceOver(t, f, s)
	filled := testutil.NewSignal()
	svc.afterFill = func() { filled.Fire() }
	seed(t, svc, f, rowOf("web-1", "app"), logstore.Fill{Lines: fileLines(30 * time.Minute)})
	stream := startTail(t, t.Context(), svc, TailRequest{Scope: webScope, Since: moment(0)})
	closeSnapshot(t, f, f.pods)
	asked.Wait(t, "web-1's gap fill")

	// Its pod runs on under another owner, so the fill's instance check passes.
	f.store.Apply(t, pod("web-1", "uid-web-1", "node-a", controller("apps/v1", "ReplicaSet", "other", "uid-other"), container{name: "app"}))
	read := testutil.NewProbe[struct{}](1)
	f.set(func() { f.mirrorRead = read })
	f.pods <- podFrame(cluster.CollectionWatchFrameModified, "web-1")
	read.Await(t, "the resolve without web-1")
	f.set(func() { f.mirrorRead = nil })
	close(release)
	filled.Wait(t, "web-1's fill to end")

	s.Add("prod/web-2/app", logstest.Stamped(40*time.Minute, "from web-2"))
	assert.Equal(t, "from web-2", recvLine(t, stream, "web-2's line").Message)
}

// A gap fill of a container's instance before its restart is thrown away once the
// follow reads the new instance's row: the new instance is followed, and the old
// fill's lines are not sent.
func TestAFillOfARestartedInstanceIsThrownAway(t *testing.T) {
	t.Parallel()
	s := newLogServer()
	s.Add("prod/web-1/app", logstest.Stamped(10*time.Minute, "in the gap"), logstest.Stamped(30*time.Minute, "1800"))
	asked, release := holdFill(t, s, "prod/web-1/app")
	f := webMirror(t)
	svc, _ := serviceOver(t, f, s)
	// The old instance's dropped tailer is held for the hour, never tailed again.
	svc.retry, svc.retryCap = time.Hour, time.Hour
	seed(t, svc, f, rowOf("web-1", "app"), logstore.Fill{Lines: fileLines(30 * time.Minute)})
	stream, _ := tailWeb1(t, svc, TailRequest{Since: moment(0)})
	closeSnapshot(t, f, f.pods)
	asked.Wait(t, "web-1's gap fill")

	s.End("prod/web-1/app")
	awaitClosed(t, s, "prod/web-1/app")
	s.Resume("prod/web-1/app")
	restartWeb1(t, f, s)
	f.pods <- podFrame(cluster.CollectionWatchFrameModified, "web-1")
	require.Eventually(t, func() bool { return s.FollowsOpened("prod/web-1/app") >= 2 },
		testutil.Timeout, time.Millisecond, "the new instance's tailer")
	close(release)

	s.Add("prod/web-1/app", logstest.Stamped(40*time.Minute, "new"))
	rec := recvLine(t, stream, "the new instance's line")
	assert.Equal(t, "new", rec.Message)
	assert.Equal(t, containerID("uid-web-1", "app", 1), rec.ContainerID)
}

// A gap fill whose check of the mirror fails ends the tail with the mirror's error.
func TestAFillWhoseInstanceCheckFailsEndsTheTail(t *testing.T) {
	t.Parallel()
	s := newLogServer()
	asked, release := holdFill(t, s, "prod/web-1/app")
	_, f, stream, _ := liveGap(t, s, false, logstest.Stamped(10*time.Minute, "in the gap"))
	asked.Wait(t, "web-1's gap fill")

	broken := errors.New("mirror broken")
	f.set(func() { f.readActiveErr = broken })
	close(release)

	assert.ErrorIs(t, failsWith(t, stream), broken)
}

// A tail's gap fill is not stamped, since what it fetched is in the viewer's window,
// but it is held until the tail's walk returns: a sweep over budget between the
// fill's commit and the walk leaves the tail's lines whole, and the chunk keeps no
// stamp after.
func TestATailsGapFillIsHeldUntilItsWalk(t *testing.T) {
	t.Parallel()
	s := newLogServer()
	s.Add("prod/web-1/app", logstest.Stamped(10*time.Minute, "in the gap"), logstest.Stamped(30*time.Minute, "1800"))
	f := webMirror(t)
	ret := testRetention
	ret.Bytes = 0
	svc, _ := serviceIn(t, f, s, t.TempDir(), ret)
	seed(t, svc, f, rowOf("web-1", "app"), logstore.Fill{Lines: fileLines(30 * time.Minute)})
	svc.afterFill = func() { sweepFile(t, svc, f) }
	stream, _ := tailWeb1(t, svc, TailRequest{Since: moment(0)})

	assert.Equal(t, []string{"in the gap", "1800"}, recvLines(t, stream, 2))
	for _, c := range chunksOf(t, svc, f, "web-1", "app") {
		if c.From.TS == at(10*time.Minute).UnixNano() {
			assert.Zero(t, c.ReadAt, "the gap fill's chunk")
		}
	}
}

// A tail's gap fill goes to the file a batch at a time, each held until the walk:
// its first batch is in the file while the fill's GET is still read, and the gap's
// lines then arrive whole, in order, none stamped.
func TestATailsGapFillIsWrittenAsItArrives(t *testing.T) {
	t.Parallel()
	s := newLogServer()
	var gap []string
	for i := range 5 {
		gap = append(gap, strconv.Itoa(10+i))
		s.Add("prod/web-1/app", logstest.Stamped(time.Duration(10+i)*time.Minute, gap[i]))
	}
	s.Add("prod/web-1/app", logstest.Stamped(30*time.Minute, "1800"))
	f := webMirror(t)
	ret := testRetention
	ret.WriteBatch = 2
	svc, _ := serviceIn(t, f, s, t.TempDir(), ret)
	gate := make(chan struct{})
	p := &logstest.Pauser{N: 3, Gate: gate}
	svc.backends = []namedBackend{{"kubelet", func(get kubelet.Getter) kubelet.Backend { return kubelet.New(p.On(get)) }}}
	seed(t, svc, f, rowOf("web-1", "app"), logstore.Fill{Lines: fileLines(30 * time.Minute)})
	stream, _ := tailWeb1(t, svc, TailRequest{Since: moment(0)})

	first := logstore.Pos{TS: at(10 * time.Minute).UnixNano()}
	require.Eventually(t, func() bool {
		for _, c := range chunksOf(t, svc, f, "web-1", "app") {
			if c.From == first && c.To.TS == at(11*time.Minute).UnixNano() {
				return true
			}
		}
		return false
	}, testutil.Timeout, time.Millisecond, "the first batch while the fill reads on")
	close(gate)

	assert.Equal(t, append(gap, "1800"), recvLines(t, stream, 6))
	for _, c := range chunksOf(t, svc, f, "web-1", "app") {
		if c.From == first {
			assert.Zero(t, c.ReadAt, "the gap fill's chunk")
		}
	}
}

// A gap read whole with no line in it is crossed, and not filled again on the next
// ping.
func TestAnEmptyGapIsFilledOnce(t *testing.T) {
	t.Parallel()
	s := newLogServer()
	_, _, stream, _ := liveGap(t, s, false)

	s.Add("prod/web-1/app", logstest.Stamped(40*time.Minute, "tailed"))
	assert.Equal(t, []string{"1800", "tailed"}, recvLines(t, stream, 2))
	s.Add("prod/web-1/app", logstest.Stamped(50*time.Minute, "again"))
	assert.Equal(t, "again", recvLine(t, stream, "the next line").Message)

	assert.Equal(t, 1, fills(s, "prod/web-1/app"))
}

// A fill its limits cut short goes on at once from its last line, waiting on no
// ping.
func TestAFillCutByItsLimitsGoesOn(t *testing.T) {
	t.Parallel()
	s := newLogServer()
	var gap []string
	for i := range 6 {
		gap = append(gap, logstest.Stamped(time.Duration(10+i)*time.Minute, fmt.Sprintf("gap line %d", i)))
	}
	s.Add("prod/web-1/app", gap...)
	s.Add("prod/web-1/app", logstest.Stamped(30*time.Minute, "1800"))
	f := webMirror(t)
	svc, _ := serviceOver(t, f, s)
	// A read is charged per read off the wire.
	f.chunk(40)
	svc.caps = capsOf(150)
	seed(t, svc, f, rowOf("web-1", "app"), logstore.Fill{Lines: fileLines(30 * time.Minute)})
	stream, _ := tailWeb1(t, svc, TailRequest{Since: moment(0)})

	assert.Equal(t, append(messages(linesOf(gap)), "1800"), recvLines(t, stream, 7))
	assert.Greater(t, fills(s, "prod/web-1/app"), 1)
}

// A fill the cluster refuses ends the stream until its generation moves, and the
// lines past the gap are not sent: here the view's one stream, so the tail ends.
func TestAFillRefusedEndsItsStream(t *testing.T) {
	t.Parallel()
	s := newLogServer()
	s.RefuseReads["prod/web-1/app"] = logstest.Denial{Code: http.StatusForbidden, Message: "no"}
	_, _, stream, _ := liveGap(t, s, false, logstest.Stamped(10*time.Minute, "in the gap"))

	recvNotice(t, stream, "web-1's refusal")
	assert.Equal(t, refusal.New(refusal.CodeUnauthorized, "the cluster refused to show this log: no"), endedWith(t, stream))
}

// A lost gap is crossed with no fill.
func TestATailCrossesALostGap(t *testing.T) {
	t.Parallel()
	s := newLogServer()
	_, _, stream, _ := liveGap(t, s, true, logstest.Stamped(10*time.Minute, "rotated away"))

	s.Add("prod/web-1/app", logstest.Stamped(40*time.Minute, "tailed"))

	assert.Equal(t, []string{"1800", "tailed"}, recvLines(t, stream, 2))
	assert.Zero(t, fills(s, "prod/web-1/app"))
}

// The log file closing under a tail, as a cache's clear closes it, ends the tail
// with an error and no frame: tailing again would not end the same way. A tail
// with no stream yet sees it as its pings ending, with no tailer to end first.
func TestATailWhoseFileClosesFails(t *testing.T) {
	t.Parallel()
	idle := Scope{Sources: []ScopeSource{{Namespace: "prod", Kind: SourceDeployment, Name: "idle"}}}
	for name, view := range map[string]Scope{"tailing": webScope, "with no stream": idle} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			f := webMirror(t)
			f.store.Apply(t, workload("apps/v1", "Deployment", "idle", "dep-idle", nil))
			svc, _ := serviceOver(t, f, newLogServer())
			stream, _ := openTailOf(t, svc, f, view)

			require.NoError(t, svc.Clear(int64(f.active.Cache.ID)))

			assert.ErrorIs(t, failsWith(t, stream), logstore.ErrClosed)
		})
	}
}

// A span past what the tailer has committed is a page's, cached while the tailer still
// had lines before it to write: the walk waits before it, and goes on once the tailer
// has committed up to it, whether or not its chunk is still open.
func TestAWalkWaitsForTheTailerBeforeAPagesSpan(t *testing.T) {
	t.Parallel()
	start := web1Since(0)
	tl := &tailer{}
	tl.start.Store(&start)
	f := &cursorTail{names: []string{"kubelet"}}
	tr := &tracked{wm: start, h: &handle{t: tl}}
	pos := func(d time.Duration) logstore.Pos { return logstore.Pos{TS: at(d).UnixNano()} }
	page := logstore.Chunk{From: pos(10 * time.Second), To: pos(10 * time.Second), Backend: "kubelet"}
	cov := func(chunks ...logstore.Chunk) logstore.Coverage {
		return logstore.CoverageOf(chunks, f.names, tr.tail())
	}

	reach, gap := f.reach(cov(page), tr, false, &start)
	assert.Equal(t, start, reach, "nothing committed past the start")
	assert.Nil(t, gap)

	tailed := logstore.Chunk{From: pos(time.Second), To: pos(time.Second), Backend: "kubelet"}
	first := pos(time.Second)
	reach, _ = f.reach(cov(tailed, page), tr, false, &first)
	assert.Equal(t, pos(time.Second), reach, "the tailer's line, and no further")

	tailed.To = pos(10 * time.Second)
	last := pos(10 * time.Second)
	reach, _ = f.reach(cov(tailed, page), tr, false, &last)
	assert.Equal(t, pos(10*time.Second), reach, "the tailer has reached the page's span")
}

// A backlog the fills bring in over more than two batches is sent whole, in order.
func TestABackfillIsSentABatchAtATime(t *testing.T) {
	t.Parallel()
	s := newLogServer()
	var backlog []string
	for i := range 2*tailBatch + 500 {
		backlog = append(backlog, logstest.Stamped(10*time.Minute+time.Duration(i)*time.Millisecond, strconv.Itoa(i)))
	}
	_, _, stream, _ := liveGap(t, s, false, backlog...)

	got := recvLines(t, stream, len(backlog)+1)
	assert.Equal(t, messages(linesOf(backlog)), got[:len(backlog)])
	assert.Equal(t, "1800", got[len(backlog)])
}

// Two backlogs that share the send's allowance are each sent whole, in order.
func TestBackfillsOfSeveralStreamsShareOneBatch(t *testing.T) {
	t.Parallel()
	s := newLogServer()
	total := 0
	for _, key := range []string{"prod/web-1/app", "prod/web-2/app"} {
		for i := range tailBatch + 200 {
			s.Add(key, logstest.Stamped(time.Duration(i)*time.Millisecond, strconv.Itoa(i)))
			total++
		}
		s.End(key)
	}
	f := newMirror(t, append(webObjects(), finished(web1()), finished(web2()))...)
	svc, _ := serviceOver(t, f, s)
	stream := startTail(t, t.Context(), svc, TailRequest{Scope: webScope, Since: moment(-time.Second)})

	next := map[string]int{}
	for range total {
		rec := recvLine(t, stream, "a line")
		require.Equal(t, strconv.Itoa(next[rec.Stream.Pod]), rec.Message, rec.Stream.Pod)
		next[rec.Stream.Pod]++
	}
}

// A tailer that has ended owns no gap: the walk after its end fills what the file
// lacks past its last line and reads on through a page's span past that, so a
// finished container's cached last lines reach the tail before it is judged ended.
func TestAnEndedTailersGapsAreWalkedAsLive(t *testing.T) {
	t.Parallel()
	s := newLogServer()
	f := webMirror(t)
	svc, _ := serviceOver(t, f, s)
	stream, _ := tailWeb1(t, svc, TailRequest{Since: moment(0)})
	closeSnapshot(t, f, f.pods)
	s.Add("prod/web-1/app", logstest.Stamped(time.Second, "tailed"))
	assert.Equal(t, "tailed", recvLine(t, stream, "the tailer's line").Message)

	// A page's chunk past everything the tailer committed, of a line the server does
	// not serve, and the container finished before the tailer's log ends.
	seed(t, svc, f, rowOf("web-1", "app"), logstore.Fill{Lines: fileLines(5 * time.Second)})
	f.store.Apply(t, finished(web1()))
	read := testutil.NewProbe[struct{}](1)
	f.set(func() { f.mirrorRead = read })
	f.pods <- podFrame(cluster.CollectionWatchFrameModified, "web-1")
	read.Await(t, "the finished pod's resolve")
	s.End("prod/web-1/app")

	assert.Equal(t, "5", recvLine(t, stream, "the page's line").Message)
	assert.Equal(t, errStopped, endedWith(t, stream))
}

// A container's restart is not a drop: once the view sees the new instance, the old
// tailer's end is its log's, and the new instance is tailed with no backoff waited
// out, whether the restart reached the mirror before that end or after it.
func TestARestartIsNotADrop(t *testing.T) {
	t.Parallel()
	for name, frameFirst := range map[string]bool{"frame first": true, "end first": false} {
		t.Run(name, func(t *testing.T) {
			s := newLogServer()
			f := webMirror(t)
			svc, _ := serviceOver(t, f, s)
			svc.retry, svc.retryCap = time.Hour, time.Hour
			stream, _ := openTail(t, svc, f)
			closeSnapshot(t, f, f.pods)
			s.Add("prod/web-1/app", logstest.Stamped(time.Hour+time.Second, "before"))
			recvLine(t, stream, "web-1's line")

			restarted := func() {
				restartWeb1(t, f, s)
				read := testutil.NewProbe[struct{}](1)
				f.set(func() { f.mirrorRead = read })
				f.pods <- podFrame(cluster.CollectionWatchFrameModified, "web-1")
				read.Await(t, "the restart's resolve")
			}
			if frameFirst {
				restarted()
				s.End("prod/web-1/app")
			} else {
				s.End("prod/web-1/app")
				awaitClosed(t, s, "prod/web-1/app")
				restarted()
			}

			// The server ends the new instance's read too, which is a drop held for the
			// hour: the read opening at all is the new instance tailed without one.
			require.Eventually(t, func() bool { return s.FollowsOpened("prod/web-1/app") >= 2 },
				testutil.Timeout, time.Millisecond, "the new instance's read")
		})
	}
}

// tailHoles files twoHoles as web-1's stream, the kubelet serving its newest
// moments, and follows it under grep as q has it.
func tailHoles(t *testing.T, s *logstest.Server, grep string, q TailRequest) *cluster.Stream[TailFrame] {
	t.Helper()
	s.Add("prod/web-1/app", logstest.Stamped(9*time.Second, "y9"), logstest.Stamped(10*time.Second, "x10"))
	f := webMirror(t)
	svc, _ := serviceOver(t, f, s)
	seed(t, svc, f, rowOf("web-1", "app"), twoHoles...)
	view := podView("web-1")
	view.Grep = grep
	q.Scope = view
	stream := startTail(t, t.Context(), svc, q)
	return stream
}

// A tail's first line on a stream looks back past where the tail started for the
// holes before it, as a page's first does: opened past the last line a page sent, or
// from a moment past the hole, it carries them. Its next line carries none.
func TestATailsFirstLineCarriesTheHolesBeforeIt(t *testing.T) {
	t.Parallel()
	for name, after := range map[string]TailRequest{
		"past the page's last match": {Since: moment(0), Seen: []string{*lineCursorFor("web-1", "app", 0, 0)}},
		"from a moment past it":      {Since: moment(9*time.Second + time.Nanosecond)},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			s := newLogServer()
			stream := tailHoles(t, s, "x", after)

			assert.Equal(t, "x10 (1s-9s)", marks([]logline.Record{recvLine(t, stream, "the first match")})[0])
			s.Add("prod/web-1/app", logstest.Stamped(11*time.Second, "x11"))
			assert.Equal(t, "x11", marks([]logline.Record{recvLine(t, stream, "the next match")})[0])
		})
	}
}

// A hole the tail crosses after its first line waits on the stream for the next
// line it sends, across walks the send's allowance cuts.
func TestATailKeepsAHoleItCrossesForItsNextLine(t *testing.T) {
	t.Parallel()
	s := newLogServer()
	var quiet []logstore.Line
	var served []string
	for i := range tailBatch + 100 {
		d := 5*time.Second + time.Duration(i)*time.Millisecond
		quiet = append(quiet, logstore.Line{Pos: logstore.Pos{TS: at(d).UnixNano()}, Message: "y"})
		served = append(served, logstest.Stamped(d, "y"))
	}
	s.Add("prod/web-1/app", served...)
	s.Add("prod/web-1/app", logstest.Stamped(7*time.Second, "x7"))
	f := webMirror(t)
	svc, _ := serviceOver(t, f, s)
	seed(t, svc, f, rowOf("web-1", "app"), logstore.Fill{Lines: []logstore.Line{said(0, "x0"), said(1, "y1")}},
		logstore.Fill{Lines: quiet, AtBeginning: true})
	view := podView("web-1")
	view.Grep = "x"
	stream := startTail(t, t.Context(), svc, TailRequest{Scope: view, Since: moment(-time.Second)})

	assert.Equal(t, "x0", marks([]logline.Record{recvLine(t, stream, "the first match")})[0])
	assert.Equal(t, "x7 (1s-5s)", marks([]logline.Record{recvLine(t, stream, "the match past the hole")})[0])
}

// A tailer that resumes from a cached line the kubelet has rotated away writes an
// at-beginning chunk past its start, and the lost gap between is the hole: once the tailer has
// committed past it, the tail's first line carries it, and so does a page while the
// tailer runs.
func TestAHoleBehindAResumedTailerIsMarked(t *testing.T) {
	t.Parallel()
	s := newLogServer()
	s.Add("prod/web-1/app", logstest.Stamped(5*time.Second, "x5"))
	f := webMirror(t)
	svc, _ := serviceOver(t, f, s)
	seed(t, svc, f, rowOf("web-1", "app"), logstore.Fill{Lines: []logstore.Line{said(0, "x0"), said(1, "y1")}})
	stream := startTail(t, t.Context(), svc, TailRequest{Scope: podView("web-1"), Since: moment(time.Second), Seen: []string{*lineCursorFor("web-1", "app", time.Second, 0)}})

	assert.Equal(t, "x5 (1s-5s)", marks([]logline.Record{recvLine(t, stream, "the first line past the hole")})[0])
	page, err := svc.Fetch(t.Context(), "7", FetchRequest{Scope: podView("web-1"), PageFrom: PageFromLast, PageSize: 10})
	require.NoError(t, err)
	assert.Equal(t, []string{"x0", "y1", "x5 (1s-5s)"}, marks(page.Records))
}

// A hole a tail crosses before its first match rides on that match even when the
// match's look back runs out of bytes before reaching it.
func TestATailsFirstLineKeepsAHoleItsLookCannotReach(t *testing.T) {
	t.Parallel()
	s := newLogServer()
	s.Add("prod/web-1/app", logstest.Stamped(5*time.Second, "y5"), logstest.Stamped(6*time.Second, "y6"), logstest.Stamped(7*time.Second, "x7"))
	f := webMirror(t)
	ret := testRetention
	ret.LookBytes = 1
	svc, _ := serviceIn(t, f, s, t.TempDir(), ret)
	seed(t, svc, f, rowOf("web-1", "app"), logstore.Fill{Lines: []logstore.Line{said(0, "y0"), said(1, "y1")}},
		logstore.Fill{Lines: []logstore.Line{said(5, "y5"), said(6, "y6")}, AtBeginning: true})
	view := podView("web-1")
	view.Grep = "x"
	stream := startTail(t, t.Context(), svc, TailRequest{Scope: view, Since: moment(-time.Second)})

	assert.Equal(t, "x7 (1s-5s)", marks([]logline.Record{recvLine(t, stream, "the first match")})[0])
}

// A tail's fill reads every second whole, so a second that alone spends the
// fill's share, louder than a page reads, is read and passed: from a watermark
// before it, and from its last line, where the fill is cut after its first line
// past it and the tail walks on.
func TestAFillReadsABigSecondWhole(t *testing.T) {
	t.Parallel()
	var big []string
	for i := range 20 {
		big = append(big, logstest.Stamped(10*time.Minute+time.Duration(i)*time.Millisecond, fmt.Sprintf("big %d %s", i, strings.Repeat("x", 2<<10))))
	}
	after := []string{logstest.Stamped(11*time.Minute, "a"), logstest.Stamped(12*time.Minute, "b"), logstest.Stamped(13*time.Minute, "c")}
	for name, tc := range map[string]struct {
		from TailRequest
		want []string
	}{
		"before it": {TailRequest{Since: moment(0)}, append(append(messages(linesOf(big)), messages(linesOf(after))...), "1800")},
		"its last line": {
			TailRequest{Since: moment(0), Seen: []string{*lineCursorFor("web-1", "app", 10*time.Minute+19*time.Millisecond, 0)}},
			append(messages(linesOf(after)), "1800"),
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			s := newLogServer()
			s.Add("prod/web-1/app", big...)
			s.Add("prod/web-1/app", after...)
			s.Add("prod/web-1/app", logstest.Stamped(30*time.Minute, "1800"))
			f := webMirror(t)
			svc, _ := serviceOver(t, f, s)
			svc.caps = capsOf(1 << 10)
			svc.caps.Second = 1 << 10
			seed(t, svc, f, rowOf("web-1", "app"), logstore.Fill{Lines: fileLines(30 * time.Minute)})
			stream, _ := tailWeb1(t, svc, tc.from)

			assert.Equal(t, tc.want, recvLines(t, stream, len(tc.want)))
		})
	}
}

// ===== The instances only the file holds =====

// ranBefore has web-1's app run its len(earlier)+1-th instance in f's mirror and
// on s, writing now, with each earlier instance in svc's file alone, holding its
// fills.
func ranBefore(t *testing.T, svc *Service, f *fakeService, s *logstest.Server, earlier [][]logstore.Fill, now ...string) {
	t.Helper()
	for i, fills := range earlier {
		row := rowOf("web-1", "app")
		row.ContainerID = containerID("uid-web-1", "app", i)
		seed(t, svc, f, row, fills...)
	}
	n := len(earlier)
	body := web1()
	status := body["status"].(map[string]any)["containerStatuses"].([]any)[0].(map[string]any)
	status["restartCount"], status["containerID"] = int64(n), containerID("uid-web-1", "app", n)
	status["lastState"] = map[string]any{"terminated": map[string]any{"containerID": containerID("uid-web-1", "app", n-1)}}
	f.store.Apply(t, body)
	s.Add("prod/web-1/app", now...)
}

// whole is one fill holding lines, its instance's whole log.
func whole(lines []logstore.Line) []logstore.Fill {
	return []logstore.Fill{{Lines: lines, AtBeginning: true}}
}

// A tail whose start is not now reads the instances of a Pod source only the
// file holds first, through the page engine, from where a poll would: a page's
// Next cut inside the earlier instance delivers its rest, then the later
// instance's lines from its tailer, each once, as the poll does (rule 7).
func TestATailReadsTheInstancesOnlyTheFileHolds(t *testing.T) {
	t.Parallel()
	s := newLogServer()
	f := webMirror(t)
	svc, _ := serviceOver(t, f, s)
	ranBefore(t, svc, f, s, [][]logstore.Fill{whole(fileLines(0, time.Second, 2*time.Second, 3*time.Second))},
		logstest.Stamped(10*time.Second, "10"), logstest.Stamped(11*time.Second, "11"))
	page := fetch(t, svc, FetchRequest{Scope: podView("web-1"), Window: Window{Since: moment(0)}, PageFrom: PageFromFirst, PageSize: 2})
	require.Equal(t, []string{"0", "1"}, messages(page.Records))
	poll := goOn(t, svc, page.Page.Next, 10)

	stream := startTail(t, t.Context(), svc, TailRequest{Cursor: page.Page.Next})

	assert.Equal(t, messages(poll.Records), recvLines(t, stream, 4))
	assert.Equal(t, []string{"2", "3", "10", "11"}, messages(poll.Records))
	s.Add("prod/web-1/app", logstest.Stamped(time.Hour+time.Second, "tailed"))
	assert.Equal(t, "tailed", recvLine(t, stream, "the tailer's next line").Message, "nothing again")
}

// An earlier instance is read whole from the tail's start: past a window's
// past Until, over more than a page, with a hole carried on its record and no
// backend read, two earlier instances in order; from a Since, none older; with a
// Seen line inside, past it; and from now, not at all.
func TestATailReadsEveryEarlierInstanceFromItsStart(t *testing.T) {
	t.Parallel()
	many := make([]time.Duration, MaxPageSize+50)
	for i := range many {
		many[i] = time.Duration(i) * time.Millisecond
	}
	holed := []logstore.Fill{{Lines: fileLines(0), AtBeginning: true}, {Lines: fileLines(2 * time.Second), AtBeginning: true}}
	since := func(d time.Duration, seen ...string) func(*Service) TailRequest {
		return func(*Service) TailRequest {
			return TailRequest{Scope: podView("web-1"), Since: moment(d), Seen: seen}
		}
	}
	untilOne := func(svc *Service) TailRequest {
		page := fetch(t, svc, FetchRequest{Scope: podView("web-1"), Window: Window{Since: moment(0), Until: moment(time.Second)}, PageFrom: PageFromFirst, PageSize: 1})
		return TailRequest{Cursor: page.Page.Next}
	}
	threeLines := [][]logstore.Fill{whole(fileLines(0, time.Second, 2*time.Second))}
	for name, tc := range map[string]struct {
		earlier [][]logstore.Fill
		q       func(*Service) TailRequest
		want    []string
	}{
		"past a past until": {threeLines, untilOne, []string{"1", "2", "60"}},
		"over a page":       {[][]logstore.Fill{whole(fileLines(many...))}, since(0), append(slices.Repeat([]string{"0"}, len(many)), "60")},
		"with a hole":       {[][]logstore.Fill{holed}, since(0), []string{"0", "2 (0s-2s)", "60"}},
		"two earlier":       {[][]logstore.Fill{whole(fileLines(0)), whole(fileLines(time.Second))}, since(0), []string{"0", "1", "60"}},
		"from a since":      {threeLines, since(time.Second), []string{"1", "2", "60"}},
		"past a seen line":  {threeLines, since(0, seenAt("web-1", "app", time.Second)...), []string{"2", "60"}},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			s := newLogServer()
			f := webMirror(t)
			svc, _ := serviceOver(t, f, s)
			ranBefore(t, svc, f, s, tc.earlier, logstest.Stamped(time.Minute, "60"))

			stream := startTail(t, t.Context(), svc, tc.q(svc))

			assert.Equal(t, tc.want, marks(recvRecords(t, stream, len(tc.want))))
			for _, r := range s.Requests() {
				assert.NotEqual(t, "true", r.Get("previous"), "no backend read of an earlier instance")
			}
		})
	}
}

// recvRecords waits for stream's next n lines and answers them.
func recvRecords(t *testing.T, stream *cluster.Stream[TailFrame], n int) []logline.Record {
	t.Helper()
	out := make([]logline.Record, n)
	for i := range out {
		out[i] = recvLine(t, stream, "a line")
	}
	return out
}

// A tail from now reads no earlier instance: nothing of it is stamped after now.
func TestATailFromNowReadsNoEarlierInstance(t *testing.T) {
	t.Parallel()
	s := newLogServer()
	f := webMirror(t)
	svc, _ := serviceOver(t, f, s)
	ranBefore(t, svc, f, s, [][]logstore.Fill{whole(fileLines(0))})
	stream, _ := tailWeb1(t, svc, TailRequest{})

	s.Add("prod/web-1/app", logstest.Stamped(time.Hour+time.Second, "new"))

	assert.Equal(t, "new", recvLine(t, stream, "the later instance's line").Message)
}

// An earlier instance's read neither ends a tail nor keeps it open: once the
// later instance has stopped, the tail sends both and ends.
func TestAnEarlierInstanceDoesNotHoldTheTailOpen(t *testing.T) {
	t.Parallel()
	s := newLogServer()
	f := webMirror(t)
	svc, _ := serviceOver(t, f, s)
	ranBefore(t, svc, f, s, [][]logstore.Fill{whole(fileLines(0))}, logstest.Stamped(time.Second, "1"))
	body := finished(web1())
	status := body["status"].(map[string]any)["containerStatuses"].([]any)[0].(map[string]any)
	status["containerID"] = containerID("uid-web-1", "app", 1)
	f.store.Apply(t, body)
	s.End("prod/web-1/app")

	stream := startTail(t, t.Context(), svc, TailRequest{Scope: podView("web-1"), Since: moment(0)})

	assert.Equal(t, []string{"0", "1"}, recvLines(t, stream, 2))
	assert.Equal(t, refusal.CodeFinished, endedWith(t, stream).Code)
}

// A tail defers NoNodes as a page does: a zone filter over a gone pod with no
// nodes left delivers the file's rows, then ends no-nodes, since no node can bring
// the pod back; with no row it is refused no-nodes.
func TestATailOfAGonePodIsFilteredWithNoNodesLeft(t *testing.T) {
	t.Parallel()
	f := newMirror(t)
	f.store.Mirror(t, "v1", "Pod")
	svc, _ := serviceOver(t, f, newLogServer())
	seed(t, svc, f, rowOf("old-1", "app"), logstore.Fill{Lines: fileLines(0)})
	view := podSource("old-1", false)
	view.Filters = []ScopeFilter{{Field: FilterZone, Values: []string{"eu-1a"}}}

	stream := startTail(t, t.Context(), svc, TailRequest{Scope: view, Since: moment(-time.Second)})
	assert.Equal(t, "0", recvLine(t, stream, "the cached row's line").Message)
	assert.Equal(t, refusal.CodeNoNodes, endedWith(t, stream).Code)

	view.Filters[0].Values = []string{"eu-1b"}
	_, err := svc.TailFromCursor(t.Context(), "7", TailRequest{Scope: view, Since: moment(-time.Second)})
	assert.Equal(t, refusal.CodeNoNodes, codeOf(err))
}

// A tail from a cursor over a scope whose one container is still starting
// waits for it rather than ending at open.
func TestATailFromACursorWaitsForAStartingContainer(t *testing.T) {
	t.Parallel()
	s := newLogServer()
	f := newMirror(t, waiting(pod("boot", "uid-boot", "node-a", nil, container{name: "app"})))
	svc, _ := serviceOver(t, f, s)
	page := fetch(t, svc, FetchRequest{Scope: podView("boot"), Window: Window{Since: moment(0)}, PageFrom: PageFromFirst})

	stream := startTail(t, t.Context(), svc, TailRequest{Cursor: page.Page.Follow})
	closeSnapshot(t, f, f.pods)
	f.store.Apply(t, pod("boot", "uid-boot", "node-a", nil, container{name: "app"}))
	f.pods <- podFrame(cluster.CollectionWatchFrameModified, "boot")
	s.Add("prod/boot/app", logstest.Stamped(time.Hour+time.Second, "started"))

	assert.Equal(t, "started", recvLine(t, stream, "app's line").Message)
}

// ===== The registry =====

// filed is webScope's first stream, web-1's app, resolved on svc and filed in its file,
// with its row's id.
func filed(t *testing.T, svc *Service, f *fakeService) (target, stream, int64) {
	t.Helper()
	tg, err := resolve(t.Context(), svc.clusters, "7", "", webScope, true)
	require.NoError(t, err)
	rows, err := fileStreams(t.Context(), fileOf(t, svc, f), tg.streams)
	require.NoError(t, err)
	return tg, tg.streams[0], rows[0].ID
}

// attach attaches to web-1's app's tailer from watermark, failing the test on an
// error; the handle is released when the test ends.
func attach(t *testing.T, svc *Service, f *fakeService, watermark logstore.Pos) *handle {
	t.Helper()
	tg, st, row := filed(t, svc, f)
	h, err := svc.registryFor("7", tg).attach(t.Context(), st, row, watermark)
	require.NoError(t, err)
	t.Cleanup(h.Release)
	return h
}

// rowID is the file row of pod/container's instance-th instance, of a fixture pod,
// once a resolve has filed it.
func rowID(t *testing.T, svc *Service, f *fakeService, pod, container string, instance int) int64 {
	t.Helper()
	var id int64
	require.Eventually(t, func() bool {
		rows, err := fileOf(t, svc, f).PodStreams(t.Context(), "prod", pod, "")
		require.NoError(t, err)
		for _, row := range rows {
			if row.Container == container && row.ContainerID == containerID("uid-"+pod, container, instance) {
				id = row.ID
				return true
			}
		}
		return false
	}, testutil.Timeout, time.Millisecond, "the instance filed")
	return id
}

// web1Since is the watermark at d past 10:00 in web-1's app: every line of that moment.
func web1Since(d time.Duration) logstore.Pos { return logstore.Pos{TS: at(d).UnixNano(), Ord: -1} }

// Two tails of one stream share its tailer: one read of the backend.
func TestTwoTailsShareOneTailer(t *testing.T) {
	t.Parallel()
	backend := newFakeBackend()
	backend.set("web-1/app", fakeRead{wait: never})
	svc, f := serviceOn(t, backend)

	attach(t, svc, f, web1Since(0))
	attach(t, svc, f, web1Since(0))

	backend.opened.Await(t, "the tailer's read")
	_, again := backend.opened.TryAwait()
	assert.False(t, again, "a second read")
}

// A tailer whose last tail lets go runs on through its grace, and a tail that
// attaches inside it reads nothing new; one that attaches after it starts a tailer
// afresh.
func TestATailerOutlivesItsTailsByItsGrace(t *testing.T) {
	t.Parallel()
	for name, grace := range map[string]time.Duration{"inside": time.Hour, "after": 0} {
		t.Run(name, func(t *testing.T) {
			backend := newFakeBackend()
			backend.set("web-1/app", fakeRead{wait: never})
			svc, f := serviceOn(t, backend)
			svc.grace = grace
			first := attach(t, svc, f, web1Since(0))
			backend.opened.Await(t, "the first tailer's read")

			first.Release()
			if grace == 0 {
				testutil.Wait(t, first.Done(), "the tailer to end")
			}
			attach(t, svc, f, web1Since(0))

			_, again := backend.opened.TryAwait()
			assert.Equal(t, grace == 0, again, "a read afresh")
		})
	}
}

// A tailer in its grace that ends is dropped, and the next tail asking for its
// stream starts one afresh.
func TestATailerThatEndsInItsGraceIsDropped(t *testing.T) {
	t.Parallel()
	backend := newFakeBackend()
	stop := make(chan struct{})
	backend.set("web-1/app", fakeRead{wait: stop})
	svc, f := serviceOn(t, backend)
	svc.grace = time.Hour
	first := attach(t, svc, f, web1Since(0))
	backend.opened.Await(t, "the first tailer's read")
	first.Release()

	close(stop)
	testutil.Wait(t, first.Done(), "the tailer to end")
	attach(t, svc, f, web1Since(0))

	backend.opened.Await(t, "a read afresh")
}

// pinged waits for a commit of lines to the stream row of the file behind svc.
func pinged(t *testing.T, svc *Service, f *fakeService, row int64) func() {
	t.Helper()
	rx, err := fileOf(t, svc, f).ListenForStreamChanges(row)
	require.NoError(t, err)
	t.Cleanup(rx.Close)
	return func() {
		t.Helper()
		testutil.Recv(t, rx.Chan(), "a commit of the tailer's lines")
	}
}

// A tailer reads from the later of its stream's newest line and the watermark of the
// follow that started it, so it never reads what lies below every watermark.
func TestATailerStartsAtItsNewestLineOrItsWatermark(t *testing.T) {
	t.Parallel()
	s := newLogServer()
	f := webMirror(t)
	svc, _ := serviceOver(t, f, s)
	seed(t, svc, f, rowOf("web-1", "app"), logstore.Fill{Lines: fileLines(0, time.Second)})

	h := attach(t, svc, f, web1Since(time.Hour))
	require.Eventually(t, func() bool { return len(s.RequestsOf("prod/web-1/app")) == 1 }, testutil.Timeout, time.Millisecond, "the tailer's read")
	assert.Equal(t, "2026-10-08T11:00:00Z", s.RequestsOf("prod/web-1/app")[0].Get("sinceTime"))
	h.Release()
	testutil.Wait(t, h.Done(), "the tailer to end")

	attach(t, svc, f, web1Since(-time.Hour))
	require.Eventually(t, func() bool { return len(s.RequestsOf("prod/web-1/app")) == 2 }, testutil.Timeout, time.Millisecond, "the next tailer's read")
	assert.Equal(t, "2026-10-08T10:00:01Z", s.RequestsOf("prod/web-1/app")[1].Get("sinceTime"))
}

// A tailer resumed at its stream's newest line extends that line's chunk when its
// read returns the line's moment whole. When the kubelet has rotated that moment away
// it opens an at-beginning chunk linked to nothing, which leaves the hole a lost gap.
func TestATailerResumesTheChunkItContinues(t *testing.T) {
	t.Parallel()
	for name, rotated := range map[string]bool{"whole": false, "rotated": true} {
		t.Run(name, func(t *testing.T) {
			s := newLogServer()
			if !rotated {
				s.Add("prod/web-1/app", logstest.Stamped(0, "0"), logstest.Stamped(time.Second, "1"))
			}
			s.Add("prod/web-1/app", logstest.Stamped(2*time.Second, "2"))
			f := webMirror(t)
			svc, _ := serviceOver(t, f, s)
			row := seed(t, svc, f, rowOf("web-1", "app"), logstore.Fill{Lines: fileLines(0, time.Second)})
			committed := pinged(t, svc, f, row)

			attach(t, svc, f, web1Since(0))
			committed()

			first, second, third := logstore.Pos{TS: at(0).UnixNano()}, logstore.Pos{TS: at(time.Second).UnixNano()}, logstore.Pos{TS: at(2 * time.Second).UnixNano()}
			chunks := chunksOf(t, svc, f, "web-1", "app")
			if rotated {
				require.Len(t, chunks, 2)
				assert.Equal(t, []logstore.Pos{first, second}, []logstore.Pos{chunks[0].From, chunks[0].To})
				assert.Equal(t, []logstore.Pos{third, third}, []logstore.Pos{chunks[1].From, chunks[1].To})
				assert.Nil(t, chunks[1].Prev)
				assert.True(t, chunks[1].AtBeginning)
				assert.True(t, logstore.CoverageOf(chunks, kubeletOnly, nil).Gaps[1].Lost)
			} else {
				require.Len(t, chunks, 1)
				assert.Equal(t, []logstore.Pos{first, third}, []logstore.Pos{chunks[0].From, chunks[0].To})
			}
			assert.True(t, chunks[len(chunks)-1].Open, "the tailer's chunk is open")
		})
	}
}

// A tailer's read that fails ends it, and its holders learn how through Done: the
// user's credentials refused, the namespace's log denied, or a server's failure.
func TestATailersFailureReachesItsHolders(t *testing.T) {
	t.Parallel()
	for code, kind := range map[int]kubelet.Failure{401: kubelet.FailFatal, 403: kubelet.FailDenied, 500: kubelet.FailTransient} {
		s := newLogServer()
		s.Refuse["prod/web-1/app"] = logstest.Denial{Code: code, Message: "no"}
		f := webMirror(t)
		svc, _ := serviceOver(t, f, s)

		h := attach(t, svc, f, web1Since(0))

		testutil.Wait(t, h.Done(), "the tailer to end")
		assert.Equal(t, kind, kubelet.FailureKind(h.Err()), code)
	}
}

// The kubelet closes a log when its container ends, and the tailer ends with it, its
// chunk closed at its last line with no at_end, since a tailer cannot tell the log's
// end from a dropped connection. The next page's forward fill reads nothing more and
// marks it.
func TestAFinishedInstancesTailerEndsWithItsChunkClosed(t *testing.T) {
	t.Parallel()
	s := newLogServer()
	f := webMirror(t)
	svc, _ := serviceOver(t, f, s)
	_, _, row := filed(t, svc, f)
	committed := pinged(t, svc, f, row)
	h := attach(t, svc, f, web1Since(0))
	s.Add("prod/web-1/app", logstest.Stamped(time.Second, "last"))
	committed()

	f.store.Apply(t, finished(web1()))
	s.End("prod/web-1/app")
	testutil.Wait(t, h.Done(), "the tailer to end")
	require.NoError(t, h.Err())
	chunks := chunksOf(t, svc, f, "web-1", "app")
	require.Len(t, chunks, 1)
	assert.False(t, chunks[0].Open)
	assert.Equal(t, chunks[0].To, h.Committed(), "what it committed outlives its chunk's close")
	assert.False(t, chunks[0].AtEnd)

	_, err := svc.Fetch(t.Context(), "7", FetchRequest{Scope: podView("web-1"), Seen: seenAt("web-1", "app", time.Second), PageFrom: PageFromFirst, PageSize: 10})
	require.NoError(t, err)
	assert.True(t, chunksOf(t, svc, f, "web-1", "app")[0].AtEnd)
}

// stallWriter holds the writer of the file in dir inside SQLite's busy wait, through
// another connection's exclusive lock, while web-1's app writes until its tailer
// falls behind and stops its read, then lets the writer go.
func stallWriter(t *testing.T, s *logstest.Server, dir string) {
	t.Helper()
	locker, err := sql.Open("sqlite", filepath.Join(dir, "11.db"))
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, locker.Close()) })
	lock, err := locker.Conn(t.Context())
	require.NoError(t, err)
	_, err = lock.ExecContext(t.Context(), "BEGIN EXCLUSIVE")
	require.NoError(t, err)
	// The writer's flush takes every batch queued by the time it blocks on the lock, so
	// lines go in until one finds the queue full.
	i := 0
	require.Eventually(t, func() bool {
		i++
		s.Add("prod/web-1/app", logstest.Stamped(time.Duration(1+i)*time.Second, "behind"))
		return s.FollowsClosed("prod/web-1/app") >= 1
	}, testutil.Timeout, time.Millisecond, "prod/web-1/app's stream to end")
	_, err = lock.ExecContext(t.Context(), "ROLLBACK")
	require.NoError(t, err)
	require.NoError(t, lock.Close())
}

// A tailer whose batch the writer cannot take within WriteTick closes its chunk at
// its last committed line and reads again from now: the lines between are a live gap
// below its new start.
func TestATailerTheWriterCannotKeepUpWithReadsFromNow(t *testing.T) {
	t.Parallel()
	s := newLogServer()
	f := webMirror(t)
	dir := t.TempDir()
	ret := testRetention
	ret.WriteBatch, ret.WriteQueue = 1, 1
	svc, _ := serviceIn(t, f, s, dir, ret)
	_, _, row := filed(t, svc, f)
	committed := pinged(t, svc, f, row)
	attach(t, svc, f, web1Since(0))
	s.Add("prod/web-1/app", logstest.Stamped(time.Second, "kept"))
	committed()

	stallWriter(t, s, dir)

	awaitReadAgain(t, s, "prod/web-1/app", "2026-10-08T11:00:00Z")
	start := tailStart(svc, f.active.Cache.ID, row)
	require.NotNil(t, start)
	assert.Equal(t, logstore.Pos{TS: at(time.Hour).UnixNano(), Ord: -1}, *start)
	for _, c := range chunksOf(t, svc, f, "web-1", "app") {
		assert.False(t, c.Open, "the chunk the writer fell behind on is closed")
	}
}

// A tailer in its grace that falls behind the writer ends instead of reading again
// from now, its grace timer stopped: nobody is reading it, so it stops competing for
// the writer. The next tail asking for its stream starts a tailer afresh.
func TestATailerInItsGraceThatFallsBehindEnds(t *testing.T) {
	t.Parallel()
	s := newLogServer()
	f := webMirror(t)
	dir := t.TempDir()
	ret := testRetention
	ret.WriteBatch, ret.WriteQueue = 1, 1
	svc, _ := serviceIn(t, f, s, dir, ret)
	svc.grace = time.Hour
	_, _, row := filed(t, svc, f)
	committed := pinged(t, svc, f, row)
	h := attach(t, svc, f, web1Since(0))
	s.Add("prod/web-1/app", logstest.Stamped(time.Second, "kept"))
	committed()
	h.Release()

	stallWriter(t, s, dir)

	testutil.Wait(t, h.Done(), "the tailer to end")
	require.NoError(t, h.Err())
	assert.Nil(t, tailStart(svc, f.active.Cache.ID, row), "no tailer reads on")
	h.t.r.mu.Lock()
	assert.Nil(t, h.t.grace, "its grace timer is stopped")
	h.t.r.mu.Unlock()
	attach(t, svc, f, web1Since(0))
	assert.NotNil(t, tailStart(svc, f.active.Cache.ID, row), "a tailer afresh")
}

// A tailer in its grace holds its stream's row, so a sweep that reads the pod as gone
// leaves it while the tailer may still write to it.
func TestATailerInItsGraceHoldsItsRow(t *testing.T) {
	t.Parallel()
	backend := newFakeBackend()
	backend.set("web-1/app", fakeRead{wait: never})
	svc, f := serviceOn(t, backend)
	svc.grace = time.Hour
	everyPodGone(svc)
	h := attach(t, svc, f, web1Since(0))
	backend.opened.Await(t, "the tailer's read")
	h.Release()

	sweepFile(t, svc, f)

	assert.Len(t, rowsOf(t, svc, f, "web-1"), 1)
}

// A tailer whose write fails ends with the failure, so the lines the writer lost are a
// live gap a page fills rather than a range the tailer would cover as its own.
func TestATailerWhoseWriteFailsEnds(t *testing.T) {
	t.Parallel()
	s := newLogServer()
	f := webMirror(t)
	dir := t.TempDir()
	svc, _ := serviceIn(t, f, s, dir, testRetention)
	_, _, row := filed(t, svc, f)
	committed := pinged(t, svc, f, row)
	h := attach(t, svc, f, web1Since(0))
	s.Add("prod/web-1/app", logstest.Stamped(time.Second, "kept"))
	committed()

	db, err := sql.Open("sqlite", filepath.Join(dir, "11.db"))
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, db.Close()) })
	_, err = db.ExecContext(t.Context(), `CREATE TRIGGER refuse BEFORE INSERT ON lines BEGIN SELECT RAISE(ABORT, 'refused'); END`)
	require.NoError(t, err)
	s.Add("prod/web-1/app", logstest.Stamped(2*time.Second, "lost"))
	testutil.Wait(t, h.Done(), "the tailer to end")
	require.Error(t, h.Err())
	assert.Equal(t, kubelet.FailTransient, kubelet.FailureKind(h.Err()))
	assert.Nil(t, tailStart(svc, f.active.Cache.ID, row), "no tailer holds the stream")

	_, err = db.ExecContext(t.Context(), `DROP TRIGGER refuse`)
	require.NoError(t, err)
	page, err := svc.Fetch(t.Context(), "7", FetchRequest{Scope: podView("web-1"), PageFrom: PageFromFirst, PageSize: 10})
	require.NoError(t, err)
	assert.Equal(t, []string{"kept", "lost"}, messages(page.Records))
}

// A tail's walk reads a tailer's start holding one of the file's reader connections,
// and the registry holds its lock while a start reads the file, so reading the start
// takes no lock.
func TestATailersStartIsReadWithoutTheRegistrysLock(t *testing.T) {
	t.Parallel()
	s := newLogServer()
	f := webMirror(t)
	svc, _ := serviceOver(t, f, s)
	h := attach(t, svc, f, web1Since(0))

	h.t.r.mu.Lock()
	defer h.t.r.mu.Unlock()
	read := make(chan logstore.Pos, 1)
	go func() { read <- h.Start() }()
	assert.Equal(t, web1Since(0), testutil.Recv(t, read, "the start"))
}

// fills is every request for key that reads no tail: a page's or a tail's fill.
func fills(s *logstest.Server, key string) int {
	n := 0
	for _, q := range s.RequestsOf(key) {
		if q.Get("follow") != "true" {
			n++
		}
	}
	return n
}

// A page crosses every gap past a running tailer's start, filling none of them, and
// fills the range below the start, which the tailer never reads: before the tailer has
// written it ends at the start, and once the tailer has written, it reads to the
// tailer's last line.
func TestAPageCrossesWhatATailerHolds(t *testing.T) {
	t.Parallel()
	s := newLogServer()
	s.Add("prod/web-1/app", logstest.Stamped(0, "0"), logstest.Stamped(time.Second, "1"), logstest.Stamped(5*time.Second, "below the start"))
	f := webMirror(t)
	svc, _ := serviceOver(t, f, s)
	row := seed(t, svc, f, rowOf("web-1", "app"), logstore.Fill{Lines: fileLines(0, time.Second), AtBeginning: true})
	committed := pinged(t, svc, f, row)
	attach(t, svc, f, web1Since(time.Hour))
	first := FetchRequest{Scope: podView("web-1"), PageFrom: PageFromFirst, PageSize: 10}

	page, err := svc.Fetch(t.Context(), "7", first)
	require.NoError(t, err)
	assert.Equal(t, []string{"0", "1", "below the start"}, messages(page.Records))
	assert.Equal(t, 1, fills(s, "prod/web-1/app"), "the range below the start filled")
	committed() // the fill's

	s.Add("prod/web-1/app", logstest.Stamped(time.Hour+time.Second, "tailed"))
	committed()
	page, err = svc.Fetch(t.Context(), "7", first)
	require.NoError(t, err)
	assert.Equal(t, []string{"0", "1", "below the start", "tailed"}, messages(page.Records))
	assert.Equal(t, 2, fills(s, "prod/web-1/app"), "the range below the start filled again, and nothing past it")
}

// A Tail page from the end crosses the range past a quiet tailer's start and fills the
// history below it, which the tailer never reads.
func TestATailPageFillsBelowAQuietTailersStart(t *testing.T) {
	t.Parallel()
	s := newLogServer()
	s.Add("prod/web-1/app", logstest.Stamped(0, "0"), logstest.Stamped(time.Second, "1"), logstest.Stamped(5*time.Second, "below the start"))
	f := webMirror(t)
	svc, _ := serviceOver(t, f, s)
	seed(t, svc, f, rowOf("web-1", "app"), logstore.Fill{Lines: fileLines(0, time.Second), AtBeginning: true})
	attach(t, svc, f, web1Since(time.Hour))

	page, err := svc.Fetch(t.Context(), "7", FetchRequest{Scope: podView("web-1"), PageFrom: PageFromLast, PageSize: 10})
	require.NoError(t, err)
	assert.Equal(t, []string{"0", "1", "below the start"}, messages(page.Records))
}

// A Tail page cut by its limit inside the range below a quiet tailer's start still
// answers the newest lines of that range.
func TestATailPageCutBelowAQuietTailersStart(t *testing.T) {
	t.Parallel()
	s := newLogServer()
	s.Add("prod/web-1/app", logstest.Stamped(0, "0"), logstest.Stamped(time.Second, "1"), logstest.Stamped(3*time.Second, "3"), logstest.Stamped(4*time.Second, "4"), logstest.Stamped(5*time.Second, "5"))
	f := webMirror(t)
	svc, _ := serviceOver(t, f, s)
	seed(t, svc, f, rowOf("web-1", "app"), logstore.Fill{Lines: fileLines(0, time.Second), AtBeginning: true})
	attach(t, svc, f, web1Since(time.Hour))

	page, err := svc.Fetch(t.Context(), "7", FetchRequest{Scope: podView("web-1"), PageFrom: PageFromLast, PageSize: 1})
	require.NoError(t, err)
	assert.Equal(t, []string{"5"}, messages(page.Records))
}

// heldBackend is a fake backend whose following reads begin only once open closes, and
// end only once done closes, their cancel ignored, so a test can hold a tailer in
// its start or in its stop. entered fires as each read is asked for.
type heldBackend struct {
	*fakeBackend
	entered    *testutil.Probe[struct{}]
	open, done chan struct{}
}

func newHeldBackend() *heldBackend {
	b := &heldBackend{fakeBackend: newFakeBackend(), entered: testutil.NewProbe[struct{}](8), open: make(chan struct{}), done: make(chan struct{})}
	b.set("web-1/app", fakeRead{wait: b.done})
	return b
}

func (b *heldBackend) Forward(ctx context.Context, st kubelet.Log, opts kubelet.Options) *kubelet.Read {
	if !opts.Follow {
		return b.fakeBackend.Forward(ctx, st, opts)
	}
	b.entered.Fire(struct{}{})
	<-b.open
	return b.fakeBackend.Forward(context.WithoutCancel(ctx), st, opts)
}

// attachment is what one attach answered.
type attachment struct {
	h   *handle
	err error
}

// attachAsync attaches to st's tailer on r from watermark on a goroutine, answering
// on the channel returned.
func attachAsync(ctx context.Context, r *registry, st stream, row int64, watermark logstore.Pos) <-chan attachment {
	out := make(chan attachment, 1)
	go func() {
		h, err := r.attach(ctx, st, row, watermark)
		out <- attachment{h, err}
	}()
	return out
}

// A tail attaching while another starts the stream's tailer waits for that start
// and joins the tailer it made, and one whose context ends meanwhile gives up.
func TestAnAttachWaitsOnTheStartInProgress(t *testing.T) {
	t.Parallel()
	backend := newHeldBackend()
	svc, f := serviceOn(t, backend)
	// Ends the tailer before the file closes under it.
	t.Cleanup(func() { close(backend.done) })
	tg, st, row := filed(t, svc, f)
	r := svc.registryFor("7", tg)

	first := attachAsync(t.Context(), r, st, row, web1Since(0))
	backend.entered.Await(t, "the first attach to start the tailer")
	second := attachAsync(t.Context(), r, st, row, web1Since(0))
	cancelled, cancel := context.WithCancel(t.Context())
	cancel()
	third := testutil.Recv(t, attachAsync(cancelled, r, st, row, web1Since(0)), "the cancelled attach")
	assert.ErrorIs(t, third.err, context.Canceled)

	close(backend.open)
	a, b := testutil.Recv(t, first, "the first attach"), testutil.Recv(t, second, "the second attach")
	require.NoError(t, a.err)
	require.NoError(t, b.err)
	t.Cleanup(a.h.Release)
	t.Cleanup(b.h.Release)
	assert.Same(t, a.h.t, b.h.t, "one tailer")
	backend.opened.Await(t, "the tailer's read")
	_, again := backend.opened.TryAwait()
	assert.False(t, again, "a second read")
}

// A tail attaching to a tailer that is stopping waits for it to end and starts
// one afresh, since a tailer started beside it would race its chunk's close; one
// whose context ends meanwhile gives up.
func TestAnAttachWaitsOnAStoppingTailer(t *testing.T) {
	t.Parallel()
	backend := newHeldBackend()
	close(backend.open)
	svc, f := serviceOn(t, backend)
	tg, st, row := filed(t, svc, f)
	r := svc.registryFor("7", tg)

	first := attach(t, svc, f, web1Since(0))
	backend.opened.Await(t, "the first tailer's read")
	first.Release()
	// The read ignores its cancel, so the tailer stays stopping until done closes.
	cancelled, cancel := context.WithCancel(t.Context())
	cancel()
	third := testutil.Recv(t, attachAsync(cancelled, r, st, row, web1Since(0)), "the cancelled attach")
	assert.ErrorIs(t, third.err, context.Canceled)
	second := attachAsync(t.Context(), r, st, row, web1Since(0))

	close(backend.done)
	testutil.Wait(t, first.Done(), "the stopping tailer to end")
	b := testutil.Recv(t, second, "the second attach")
	require.NoError(t, b.err)
	t.Cleanup(b.h.Release)
	assert.NotSame(t, first.t, b.h.t, "a tailer afresh")
	backend.opened.Await(t, "a read afresh")
}

// A start that cannot take the registry's claims — the cluster's, or the file's —
// fails the attach, drops the tailer and gives back whatever it took, so the next
// attach starts afresh.
func TestAStartThatCannotHoldItsClaimsFails(t *testing.T) {
	t.Parallel()
	t.Run("cluster", func(t *testing.T) {
		t.Parallel()
		backend := newFakeBackend()
		backend.set("web-1/app", fakeRead{wait: never})
		svc, f := serviceOn(t, backend)
		tg, st, row := filed(t, svc, f)
		r := svc.registryFor("7", tg)
		f.set(func() { f.acquireErr = cluster.ErrNotConnectable })

		_, err := r.attach(t.Context(), st, row, web1Since(0))
		require.ErrorIs(t, err, kubelet.ErrRevoked)
		assert.Empty(t, r.tailers)
		assert.Nil(t, r.file, "the file given back")

		f.set(func() { f.acquireErr = nil })
		h, err := r.attach(t.Context(), st, row, web1Since(0))
		require.NoError(t, err)
		t.Cleanup(h.Release)
		backend.opened.Await(t, "the tailer's read")
	})
	t.Run("file", func(t *testing.T) {
		t.Parallel()
		backend := newFakeBackend()
		backend.set("web-1/app", fakeRead{wait: never})
		svc, f := serviceOn(t, backend)
		tg, st, row := filed(t, svc, f)
		require.NoError(t, svc.files.Close())

		_, err := svc.registryFor("7", tg).attach(t.Context(), st, row, web1Since(0))
		require.ErrorIs(t, err, logstore.ErrClosed)
		_, opened := backend.opened.TryAwait()
		assert.False(t, opened, "a read")
	})
}

// A tailer let go of ends once its grace has passed with no tail holding it.
func TestATailerStopsAtTheEndOfItsGrace(t *testing.T) {
	t.Parallel()
	backend := newFakeBackend()
	backend.set("web-1/app", fakeRead{wait: never})
	svc, f := serviceOn(t, backend)
	// The grace is the cadence under test, shrunk from the file's TailGrace.
	svc.grace = time.Millisecond
	h := attach(t, svc, f, web1Since(0))
	backend.opened.Await(t, "the tailer's read")

	h.Release()
	testutil.Wait(t, h.Done(), "the tailer to end at its grace")
}

// tailStart is the start of the tailer running on the stream row of cacheID's file,
// nil with none.
func tailStart(svc *Service, cacheID cluster.ClusterCacheID, row int64) *logstore.Pos {
	start, _ := svc.tailOf(cacheID, row)
	return start
}

// A fatal tailer ends the tail, saying why.
func TestAFatalTailerEndsTheTail(t *testing.T) {
	t.Parallel()
	backend := webPair(fakeRead{err: &kubelet.StatusError{Code: http.StatusUnauthorized, Message: "Unauthorized"}}, fakeRead{wait: never})
	svc, _ := serviceOn(t, backend)

	stream := openEndingTail(t, svc)

	assert.Equal(t, refusal.New(refusal.CodeUnauthorized, "the cluster refused to show this log: Unauthorized"), endedWith(t, stream))
}

// A transient tailer is opened again once its backoff passes.
func TestATransientTailerIsReopened(t *testing.T) {
	t.Parallel()
	backend := webPair(fakeRead{err: errors.New("connection reset")}, fakeRead{wait: never})
	svc, f := serviceOn(t, backend)
	openTail(t, svc, f)

	for of := 0; of < 2; {
		if backend.opened.Await(t, "a read to open") == "web-1/app" {
			of++
		}
	}
}

// An ended tailer stays closed until the view sees a later generation of its log,
// then is read again.
func TestAnEndedTailerWaitsForItsGeneration(t *testing.T) {
	t.Parallel()
	backend := webPair(fakeRead{err: &kubelet.StatusError{Code: http.StatusBadRequest, Message: "container is waiting to start"}}, fakeRead{wait: never})
	svc, f := serviceOn(t, backend)
	stream, _ := openTail(t, svc, f)
	awaitOpens(t, backend, 2)
	recvNotice(t, stream, "web-1's end")
	// A negative assertion, so a bounded window: a hundred retries is far past the
	// backoff a dropped stream would wait.
	testutil.NoRecv(t, backend.opened.Chan(), 100*svc.retry, "web-1 to open again")

	backend.set("web-1/app", fakeRead{lines: []logline.Record{{Timestamp: at(time.Hour + time.Second), Message: "started"}}, wait: never})
	f.store.Apply(t, webPod("web-1", "node-a", container{name: "app", restarted: true}, container{name: "istio-proxy"}))
	f.pods <- podFrame(cluster.CollectionWatchFrameModified, "web-1")

	assert.Equal(t, "started", recvLine(t, stream, "web-1's line").Message)
}

// A denied tailer ends its stream alone, and is why the tail ends once every
// stream has.
func TestADeniedTailerEndsItsStreamAlone(t *testing.T) {
	t.Parallel()
	done := make(chan struct{})
	backend := webPair(fakeRead{err: &kubelet.StatusError{Code: http.StatusForbidden, Message: "forbidden"}}, fakeRead{err: &kubelet.StatusError{Code: http.StatusBadRequest}, wait: done})
	svc, f := serviceOn(t, backend)
	stream, _ := openTail(t, svc, f)
	awaitOpens(t, backend, 2)
	recvNotice(t, stream, "web-1's refusal")
	// A negative assertion, so a bounded window: the tail would end on the 403
	// at once, and a hundred retries is far past it.
	testutil.NoRecv(t, stream.Frames, 100*svc.retry, "the tail to end")

	close(done)
	recvNotice(t, stream, "web-2's end")

	assert.Equal(t, refusal.New(refusal.CodeUnauthorized, "the cluster refused to show this log: forbidden"), endedWith(t, stream))
}

// recvNotice waits for stream's next frame, a notice, and answers it.
func recvNotice(t *testing.T, stream *cluster.Stream[TailFrame], what string) Notice {
	t.Helper()
	frame := testutil.Recv(t, stream.Frames, what)
	require.NotNil(t, frame.Notice, "%s: %+v", what, frame)
	return *frame.Notice
}

// A stream that ends on an error says so in a stream-skipped notice naming it:
// the server's refusal, its answer for this log, and a second too loud to read
// (rule 5).
func TestAStreamThatEndsOnAnErrorSaysSo(t *testing.T) {
	t.Parallel()
	for name, err := range map[string]error{
		"denied":   &kubelet.StatusError{Code: http.StatusForbidden, Message: "forbidden"},
		"ended":    &kubelet.StatusError{Code: http.StatusBadRequest, Message: "container is waiting to start"},
		"too loud": kubelet.LoudAt(at(time.Hour)),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			svc, f := serviceOn(t, webPair(fakeRead{err: err}, fakeRead{wait: never}))
			stream, _ := openTail(t, svc, f)

			notice := recvNotice(t, stream, "web-1's notice")

			assert.Equal(t, Notice{refusal.CodeStreamSkipped, skippedLine(web1Key(), err)}, notice)
		})
	}
}

// A stream held for its backoff says nothing: it is read again. The notice would
// go out before the hold, so the reopen proves none was sent.
func TestAHeldStreamSaysNothing(t *testing.T) {
	t.Parallel()
	backend := webPair(fakeRead{err: errors.New("connection reset")}, fakeRead{wait: never})
	svc, f := serviceOn(t, backend)
	stream, _ := openTail(t, svc, f)

	for of := 0; of < 2; {
		if backend.opened.Await(t, "a read to open") == "web-1/app" {
			of++
		}
	}

	select {
	case frame := <-stream.Frames:
		t.Fatalf("a held stream sent %+v", frame)
	default:
	}
}

// A stream that fails again at a later generation says so again.
func TestAStreamFailingAgainAtALaterGenerationSaysSoAgain(t *testing.T) {
	t.Parallel()
	backend := webPair(fakeRead{err: &kubelet.StatusError{Code: http.StatusBadRequest, Message: "gone"}}, fakeRead{wait: never})
	svc, f := serviceOn(t, backend)
	stream, _ := openTail(t, svc, f)
	recvNotice(t, stream, "the first generation's end")

	f.store.Apply(t, webPod("web-1", "node-a", container{name: "app", restarted: true}, container{name: "istio-proxy"}))
	f.pods <- podFrame(cluster.CollectionWatchFrameModified, "web-1")

	recvNotice(t, stream, "the later generation's end")
}

// webPair is a backend whose read of web-1's app is web1 and of web-2's web2.
func webPair(web1, web2 fakeRead) *fakeBackend {
	backend := newFakeBackend()
	backend.set("web-1/app", web1)
	backend.set("web-2/app", web2)
	return backend
}

// awaitOpens waits for backend to open n reads.
func awaitOpens(t *testing.T, backend *fakeBackend, n int) {
	t.Helper()
	for range n {
		backend.opened.Await(t, "a read to open")
	}
}

// A tail is refused when its log file fails as it opens: the file closed, or its
// workload's gone pods unreadable.
func TestATailWhoseFileFailsAsItOpensIsRefused(t *testing.T) {
	t.Parallel()
	svc, _ := serviceOver(t, webMirror(t), newLogServer())
	require.NoError(t, svc.files.Close())
	_, err := svc.TailFromCursor(t.Context(), "7", TailRequest{Scope: webScope})
	assert.ErrorIs(t, err, logstore.ErrClosed)

	dir := t.TempDir()
	svc, f := serviceIn(t, webMirror(t), newLogServer(), dir, testRetention)
	fileOf(t, svc, f)
	breakFile(t, dir, f, `DROP TABLE pod_owners`)
	_, err = svc.TailFromCursor(t.Context(), "7", TailRequest{Scope: webScope, Since: moment(0)})
	assert.ErrorContains(t, err, "no such table: pod_owners")
}

// A tail fails with its log file wherever the file fails under it: reading the
// pods only the file holds, starting a tailer, walking a finished instance, or
// walking on past what a fill wrote, its chunks or its lines.
func TestATailFailsWithItsFile(t *testing.T) {
	t.Parallel()
	for name, c := range map[string]struct {
		view      Scope
		since     *time.Time
		gone      bool
		afterFill bool
		table     string
	}{
		"reading a gone pod":          {view: webScope, since: moment(0), gone: true},
		"starting a tailer":           {view: webScope},
		"walking a finished instance": {view: podView("done")},
		"walking past a fill":         {view: podView("done"), afterFill: true},
		"walking a fill's lines":      {view: podView("done"), afterFill: true, table: "lines"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			s := newLogServer()
			s.Add("prod/web-1/app", logstest.Stamped(time.Second, "a1"))
			s.Add("prod/done/app", logstest.Stamped(time.Second, "d1"))
			dir := t.TempDir()
			svc, f := serviceIn(t, webMirror(t), s, dir, testRetention)
			f.store.Apply(t, donePod())
			fileOf(t, svc, f)
			if c.gone {
				fetch(t, svc, FetchRequest{Scope: webScope, PageFrom: PageFromFirst, PageSize: 10})
				f.store.Delete(t, web1())
			}
			table := cmp.Or(c.table, "chunks")
			if c.afterFill {
				svc.afterFill = func() { breakFile(t, dir, f, `DROP TABLE `+table) }
			} else {
				breakFile(t, dir, f, `DROP TABLE `+table)
			}

			stream := startTail(t, t.Context(), svc, TailRequest{Scope: c.view, Since: c.since})

			assert.ErrorContains(t, failsWith(t, stream), "no such table: "+table)
		})
	}
}

// A tail whose mirror watches ended clean and cannot open again fails with the
// reason.
func TestATailWhoseWatchesCannotOpenAgainFails(t *testing.T) {
	t.Parallel()
	f := webMirror(t)
	svc, _ := serviceOver(t, f, newLogServer())
	stream, _ := openTail(t, svc, f)

	f.set(func() { f.watchObjectsErr = errors.New("no watch") })
	close(f.pods)

	assert.EqualError(t, failsWith(t, stream), "no watch")
}

// A tail whose context ends while it sends ends there: in the lines of a pod only
// the file holds, and in a walk's. The stream holds one frame, so with the first
// read and nothing more, the tail waits on the third when the cancel lands.
func TestATailEndsWhileItSends(t *testing.T) {
	t.Parallel()
	for name, c := range map[string]struct {
		view Scope
		gone bool
	}{
		"a gone pod's lines": {view: webScope, gone: true},
		"a walk's lines":     {view: podView("done")},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			s := newLogServer()
			s.Add("prod/web-1/app", logstest.Stamped(time.Second, "1"), logstest.Stamped(2*time.Second, "2"), logstest.Stamped(3*time.Second, "3"))
			s.Add("prod/done/app", logstest.Stamped(time.Second, "1"), logstest.Stamped(2*time.Second, "2"), logstest.Stamped(3*time.Second, "3"))
			svc, f := serviceOver(t, webMirror(t), s)
			f.store.Apply(t, donePod())
			if c.gone {
				fetch(t, svc, FetchRequest{Scope: webScope, PageFrom: PageFromFirst, PageSize: 10})
				f.store.Delete(t, web1())
			}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			released := f.lease.released.Load()
			stream := startTail(t, ctx, svc, TailRequest{Scope: c.view, Since: moment(0)})

			assert.Equal(t, "1", recvLine(t, stream, "the first line").Message)
			// The tail may still be between lines: cancelled then, it never sends the second.
			require.Eventually(t, func() bool { return len(stream.Frames) == 1 }, testutil.Timeout, time.Millisecond, "the second line to fill the buffer")
			cancel()
			// The tail lets its claim go once it has ended.
			require.Eventually(t, func() bool { return f.lease.released.Load() > released }, testutil.Timeout, time.Millisecond)

			assert.Equal(t, "2", recvLine(t, stream, "the line the stream held").Message)
			assert.NoError(t, failsWith(t, stream), "a cancel ends a tail with no error")
		})
	}
}

// A finished instance whose fill the connection's retirement cut before its first
// line is held and read again, as any transient failure is.
func TestAFillARetirementCutIsReadAgain(t *testing.T) {
	t.Parallel()
	backend := newRetiredBackend()
	svc, f := serviceOn(t, backend)
	f.store.Apply(t, donePod())

	startTail(t, t.Context(), svc, TailRequest{Scope: podView("done")})

	backend.reads.Await(t, "the fill")
	backend.reads.Await(t, "the fill again")
}

// A tail from a moment over a workload with a label filter and no nodes to judge
// it by reads its gone pods from the file first, then waits for nodes rather than
// ending: a workload may run new pods.
func TestATailOfAWorkloadWithNoNodesReadsItsGonePodsAndWaits(t *testing.T) {
	t.Parallel()
	f := newMirror(t, workload("apps/v1", "Deployment", "web", "dep-web", nil))
	f.store.Mirror(t, "v1", "Pod")
	svc, _ := serviceOver(t, f, newLogServer())
	row := rowOf("old-1", "app")
	row.Owners = []string{"dep-web"}
	seed(t, svc, f, row, logstore.Fill{Lines: fileLines(0)})
	view := Scope{Sources: []ScopeSource{{Namespace: "prod", Kind: SourceDeployment, Name: "web"}}, Filters: []ScopeFilter{{Field: FilterZone, Values: []string{"eu-1a"}}}}
	read := testutil.NewProbe[struct{}](64)
	f.set(func() { f.mirrorRead = read })

	stream := startTail(t, t.Context(), svc, TailRequest{Scope: view, Since: moment(-time.Second)})

	assert.Equal(t, "0", recvLine(t, stream, "the gone pod's line").Message)
	read.Await(t, "the open's resolve")
	read.Await(t, "the resolve asked again")
}

// A walk of streams one at a time stops each later stream once an earlier one spent
// the send's allowance of lines, and walks them all on: every filed line arrives.
func TestAWalkPastItsAllowanceWalksTheRestOn(t *testing.T) {
	t.Parallel()
	svc, f := serviceOver(t, webMirror(t), newLogServer())
	moments := make([]time.Duration, tailBatch+1)
	for i := range moments {
		moments[i] = time.Duration(i) * time.Millisecond
	}
	seed(t, svc, f, rowOf("web-1", "app"), logstore.Fill{Lines: fileLines(moments...), AtBeginning: true})
	seed(t, svc, f, rowOf("web-2", "app"), logstore.Fill{Lines: fileLines(0), AtBeginning: true})

	stream := startTail(t, t.Context(), svc, TailRequest{Scope: webScope, Since: moment(0)})

	assert.Len(t, recvLines(t, stream, tailBatch+2), tailBatch+2)
}
