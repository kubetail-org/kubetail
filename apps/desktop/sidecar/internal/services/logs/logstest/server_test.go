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

package logstest

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/testutil"
)

const (
	webPath = "/api/v1/namespaces/prod/pods/web-1/log"
	webKey  = "prod/web-1/app"
)

// newServer is a server whose refusals read as "<code> <message>".
func newServer() *Server {
	s := New()
	s.Refused = func(code int, message string) error { return fmt.Errorf("%d %s", code, message) }
	return s
}

// query is the app container's query with the given parameters, as "k=v" pairs.
func query(params ...string) url.Values {
	q := url.Values{"container": {"app"}}
	for _, p := range params {
		k, v, _ := strings.Cut(p, "=")
		q.Set(k, v)
	}
	return q
}

// read is the body Get answers for q, and the error reading it ended on.
func read(t *testing.T, s *Server, q url.Values) (string, error) {
	t.Helper()
	body, err := s.Get(t.Context(), webPath, q)
	require.NoError(t, err)
	defer body.Close()
	data, err := io.ReadAll(body)
	return string(data), err
}

// get is the whole body Get answers for q.
func get(t *testing.T, s *Server, q url.Values) string {
	t.Helper()
	text, err := read(t, s, q)
	require.NoError(t, err)
	return text
}

// body is lines as the server writes them, one per line, "" for none.
func body(lines ...string) string {
	if len(lines) == 0 {
		return ""
	}
	return strings.Join(lines, "\n") + "\n"
}

// A log path names its container by namespace, pod and container, and the
// instance before the last restart apart; any other path names none.
func TestKeyOfNamesTheContainer(t *testing.T) {
	t.Parallel()
	assert.Equal(t, webKey, keyOf(webPath, query()))
	assert.Equal(t, webKey+"/previous", keyOf(webPath, query("previous=true")))
	assert.Equal(t, webKey, keyOf(webPath, query("previous=false")))
	for _, path := range []string{
		"/api/v1/namespaces/prod/pods/web-1",
		"/api/v1/namespaces/prod/pods/web-1/exec",
		"/apis/v1/namespaces/prod/pods/web-1/log",
		"/api/v1/namespaces/prod/pods/web-1/log/extra",
	} {
		assert.Empty(t, keyOf(path, query()), path)
	}
}

// A read with no parameters is the whole log, a line each, and an empty log
// is an empty body.
func TestAReadIsTheWholeLog(t *testing.T) {
	t.Parallel()
	s := newServer()
	assert.Empty(t, get(t, s, query()))

	lines := LineEvery(3)
	s.Add(webKey, lines...)
	assert.Equal(t, body(lines...), get(t, s, query()))
	assert.Empty(t, get(t, s, query("previous=true")), "the previous instance is its own log")
}

// sinceTime keeps the lines from its whole second on, as the kubelet's does.
func TestSinceTimeKeepsFromItsWholeSecond(t *testing.T) {
	t.Parallel()
	s := newServer()
	lines := []string{
		Stamped(0, "a"),
		Stamped(time.Second, "b"),
		Stamped(1500*time.Millisecond, "c"),
		Stamped(2*time.Second, "d"),
	}
	s.Add(webKey, lines...)

	since := func(d time.Duration) url.Values {
		return query("sinceTime=" + at(d).Format(time.RFC3339Nano))
	}
	assert.Equal(t, body(lines[1:]...), get(t, s, since(1700*time.Millisecond)), "truncated to 10:00:01")
	assert.Equal(t, body(lines[1:]...), get(t, s, since(time.Second)), "a line at the moment is kept")
	assert.Equal(t, body(lines[3:]...), get(t, s, since(2*time.Second)))
	assert.Empty(t, get(t, s, since(3*time.Second)))
}

// tailLines keeps the log's last lines, counting each part of a message longer
// than Part, and stops at a line whose parts would overrun it.
func TestTailLinesCountsALongLinesParts(t *testing.T) {
	t.Parallel()
	s := newServer()
	s.Part = 4
	lines := []string{Stamped(0, "ab"), Stamped(time.Second, "abcdefgh"), Stamped(2*time.Second, "c")}
	s.Add(webKey, lines...)

	for tail, want := range map[string][]string{
		"0":  nil,
		"1":  lines[2:],
		"2":  lines[2:],
		"3":  lines[1:],
		"4":  lines,
		"10": lines,
	} {
		assert.Equal(t, body(want...), get(t, s, query("tailLines="+tail)), "tailLines=%s", tail)
	}

	s.Mu.Lock()
	s.Part = 0
	s.Mu.Unlock()
	assert.Equal(t, body(lines[1:]...), get(t, s, query("tailLines=2")), "with no Part every line is one")
}

// A message exactly Part long is one part, one byte over is two.
func TestAPartIsPartBytes(t *testing.T) {
	t.Parallel()
	s := newServer()
	s.Part = 4
	lines := []string{Stamped(0, "abcd"), Stamped(time.Second, "abcde")}
	s.Add(webKey, lines...)

	assert.Empty(t, get(t, s, query("tailLines=1")))
	assert.Equal(t, body(lines[1:]...), get(t, s, query("tailLines=2")))
	assert.Equal(t, body(lines...), get(t, s, query("tailLines=3")))
}

// tailLines counts within what sinceTime kept.
func TestTailLinesCountsAfterSinceTime(t *testing.T) {
	t.Parallel()
	s := newServer()
	lines := LineEvery(5)
	s.Add(webKey, lines...)

	q := query("sinceTime="+at(3*time.Second).Format(time.RFC3339), "tailLines=4")
	assert.Equal(t, body(lines[3:]...), get(t, s, q))
}

// limitBytes cuts the body at its byte, mid-line if it falls there, after the
// other parameters cut the lines.
func TestLimitBytesCutsTheBody(t *testing.T) {
	t.Parallel()
	s := newServer()
	lines := LineEvery(3)
	s.Add(webKey, lines...)
	whole := body(lines...)

	assert.Equal(t, whole[:10], get(t, s, query("limitBytes=10")))
	assert.Equal(t, whole, get(t, s, query("limitBytes=100000")))
	assert.Equal(t, body(lines[2:]...)[:5], get(t, s, query("tailLines=1", "limitBytes=5")))
}

// Get never follows: a follow's read answers the log as it is and ends.
func TestGetNeverFollows(t *testing.T) {
	t.Parallel()
	s := newServer()
	lines := LineEvery(2)
	s.Add(webKey, lines...)
	assert.Equal(t, body(lines...), get(t, s, query("follow=true")))
	assert.Zero(t, s.FollowsOpened(webKey))
}

// A refusal answers every request of its container until it is cleared;
// RefuseReads answers only the requests that follow nothing, and Refuse wins
// over it.
func TestARefusalAnswersItsContainer(t *testing.T) {
	t.Parallel()
	s := newServer()
	s.Add(webKey, LineEvery(1)...)

	s.SetRefusal(webKey, &Denial{Code: http.StatusForbidden, Message: "no"})
	for _, q := range []url.Values{query(), query("follow=true")} {
		_, err := s.Get(t.Context(), webPath, q)
		require.EqualError(t, err, "403 no")
	}
	assert.Equal(t, body(), get(t, s, query("previous=true")), "another container's log reads")
	s.SetRefusal(webKey, nil)
	assert.Equal(t, body(LineEvery(1)...), get(t, s, query()))

	s.Mu.Lock()
	s.RefuseReads[webKey] = Denial{Code: http.StatusInternalServerError, Message: "read"}
	s.Mu.Unlock()
	_, err := s.Get(t.Context(), webPath, query())
	require.EqualError(t, err, "500 read")
	assert.Equal(t, body(LineEvery(1)...), get(t, s, query("follow=true")), "a follow is not a read")

	s.SetRefusal(webKey, &Denial{Code: http.StatusForbidden, Message: "no"})
	_, err = s.Get(t.Context(), webPath, query())
	require.EqualError(t, err, "403 no")
}

// BreakNext breaks the next read that follows nothing after its lines, once: a
// refused request and a follow leave it set.
func TestBreakNextBreaksTheNextReadOnce(t *testing.T) {
	t.Parallel()
	s := newServer()
	lines := LineEvery(4)
	s.Add(webKey, lines...)
	s.BreakNext(2)

	assert.Equal(t, body(lines...), get(t, s, query("follow=true")), "a follow leaves it set")
	s.SetRefusal(webKey, &Denial{Code: http.StatusForbidden, Message: "no"})
	_, err := s.Get(t.Context(), webPath, query())
	require.Error(t, err)
	s.SetRefusal(webKey, nil)

	text, err := read(t, s, query())
	require.ErrorIs(t, err, ErrBroken)
	assert.Equal(t, body(lines[:2]...), text)
	assert.Equal(t, body(lines...), get(t, s, query()), "once")
}

// BreakNext past the lines a read keeps serves them all, then breaks.
func TestBreakNextPastTheLinesBreaksAtTheEnd(t *testing.T) {
	t.Parallel()
	s := newServer()
	lines := LineEvery(2)
	s.Add(webKey, lines...)
	s.BreakNext(5)

	text, err := read(t, s, query())
	require.ErrorIs(t, err, ErrBroken)
	assert.Equal(t, body(lines...), text)
}

// The server records every request's query and container in order, refused
// ones included, and runs OnRequest with each one's index before serving it.
func TestTheServerRecordsEveryRequest(t *testing.T) {
	t.Parallel()
	s := newServer()
	var seen []int
	s.OnRequest = func(n int) {
		seen = append(seen, n)
		if n == 1 {
			s.Add(webKey, Stamped(0, "added by the hook"))
		}
	}
	s.SetRefusal(webKey+"/previous", &Denial{Code: http.StatusBadRequest, Message: "none"})

	first, second := query("tailLines=1"), query("tailLines=2")
	previous := query("previous=true")
	get(t, s, first)
	assert.Equal(t, body(Stamped(0, "added by the hook")), get(t, s, second), "the hook runs before the read")
	_, err := s.Get(t.Context(), webPath, previous)
	require.Error(t, err)

	assert.Equal(t, []int{0, 1, 2}, seen)
	assert.Equal(t, []url.Values{first, second, previous}, s.Requests())
	assert.Equal(t, []url.Values{first, second}, s.RequestsOf(webKey))
	assert.Equal(t, []url.Values{previous}, s.RequestsOf(webKey+"/previous"))
	assert.Equal(t, []string{"2", ""}, Tails(s), "Tails skips the first")

	got := s.Requests()
	got[0] = nil
	assert.Equal(t, first, s.Requests()[0], "Requests is a copy")
}

// Rotate drops the lines before its moment and Clear empties the log.
func TestRotateAndClearDropLines(t *testing.T) {
	t.Parallel()
	s := newServer()
	lines := LineEvery(4)
	s.Add(webKey, lines...)

	s.Rotate(webKey, 2*time.Second)
	assert.Equal(t, body(lines[2:]...), get(t, s, query()))
	s.Clear(webKey)
	assert.Empty(t, get(t, s, query()))
}

// Next is a copy of the lines from the nth and a channel Add and End close,
// with whether the follows have ended; Resume lets them run again.
func TestNextWakesOnAddAndEnd(t *testing.T) {
	t.Parallel()
	s := newServer()
	lines := LineEvery(3)
	s.Add(webKey, lines[:2]...)

	got, ended, grew := s.Next(webKey, 1)
	assert.Equal(t, lines[1:2], got)
	assert.False(t, ended)
	got[0] = "changed"
	assert.Equal(t, lines[1], s.Logs[webKey][1], "Next is a copy")

	s.Add(webKey, lines[2])
	testutil.Wait(t, grew, "Add to wake the follows")
	got, _, grew = s.Next(webKey, 1)
	assert.Equal(t, lines[1:], got)

	s.End(webKey)
	testutil.Wait(t, grew, "End to wake the follows")
	got, ended, _ = s.Next(webKey, 5)
	assert.Empty(t, got, "past a log emptied under a follow")
	assert.True(t, ended)

	s.Resume(webKey)
	_, ended, _ = s.Next(webKey, 0)
	assert.False(t, ended)
}

// A path that is no log's is a 404 and is not recorded.
func TestServeHTTPAnswersOnlyLogs(t *testing.T) {
	t.Parallel()
	s := newServer()
	srv := httptest.NewServer(s)
	t.Cleanup(srv.Close)

	resp, err := http.Get(srv.URL + "/api/v1/namespaces/prod/pods/web-1")
	require.NoError(t, err)
	resp.Body.Close()
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
	assert.Empty(t, s.Requests())
}

// Over HTTP a read is the log as Get cuts it, and a refusal is its status with
// a Kubernetes Status body.
func TestServeHTTPReadsAndRefuses(t *testing.T) {
	t.Parallel()
	s := newServer()
	lines := LineEvery(3)
	s.Add(webKey, lines...)
	srv := httptest.NewServer(s)
	t.Cleanup(srv.Close)

	status, text := httpGet(t, srv.URL+webPath+"?"+query("tailLines=2").Encode())
	assert.Equal(t, http.StatusOK, status)
	assert.Equal(t, body(lines[1:]...), text)

	s.SetRefusal(webKey, &Denial{Code: http.StatusForbidden, Message: `pods "web-1" is forbidden`})
	status, text = httpGet(t, srv.URL+webPath+"?"+query().Encode())
	assert.Equal(t, http.StatusForbidden, status)
	assert.JSONEq(t, `{"kind":"Status","message":"pods \"web-1\" is forbidden"}`, text)
}

// A broken read over HTTP sends its lines and drops the connection.
func TestServeHTTPBreaksARead(t *testing.T) {
	t.Parallel()
	s := newServer()
	lines := LineEvery(3)
	s.Add(webKey, lines...)
	s.BreakNext(1)
	srv := httptest.NewServer(s)
	t.Cleanup(srv.Close)

	resp, err := http.Get(srv.URL + webPath + "?" + query().Encode())
	require.NoError(t, err)
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	require.Error(t, err)
	assert.Equal(t, body(lines[0]), string(data))
}

// A follow sends the log as its parameters cut it, then every line added after
// its read, whatever it cut, until End; it counts as opened and closed.
func TestServeHTTPFollowsUntilEnd(t *testing.T) {
	t.Parallel()
	s := newServer()
	lines := LineEvery(4)
	s.Add(webKey, lines[:2]...)
	opened := make(chan string, 1)
	s.OnOpen = func(key string) { opened <- key }
	srv := httptest.NewServer(s)
	t.Cleanup(srv.Close)

	resp, err := http.Get(srv.URL + webPath + "?" + query("follow=true", "tailLines=1").Encode())
	require.NoError(t, err)
	defer resp.Body.Close()
	r := bufio.NewReader(resp.Body)

	assert.Equal(t, lines[1], readLine(t, r))
	assert.Equal(t, webKey, testutil.Recv(t, opened, "OnOpen"))
	assert.Equal(t, 1, s.FollowsOpened(webKey))

	s.Add(webKey, lines[2])
	assert.Equal(t, lines[2], readLine(t, r))
	s.Add(webKey, lines[3])
	s.End(webKey)
	assert.Equal(t, lines[3], readLine(t, r), "End drains what was added first")
	_, err = r.ReadByte()
	assert.ErrorIs(t, err, io.EOF)
	assert.Equal(t, 1, s.FollowsClosed(webKey))

	status, text := httpGet(t, srv.URL+webPath+"?"+query("follow=true").Encode())
	assert.Equal(t, http.StatusOK, status)
	assert.Equal(t, body(lines...), text, "a follow opened after End ends at once")
	assert.Equal(t, 2, s.FollowsClosed(webKey))
}

// A follow the client leaves closes.
func TestServeHTTPFollowEndsWithItsClient(t *testing.T) {
	t.Parallel()
	s := newServer()
	s.Add(webKey, LineEvery(1)...)
	srv := httptest.NewServer(s)
	t.Cleanup(srv.Close)

	ctx, cancel := context.WithCancel(t.Context())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+webPath+"?"+query("follow=true").Encode(), nil)
	require.NoError(t, err)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	readLine(t, bufio.NewReader(resp.Body))
	cancel()

	require.Eventually(t, func() bool { return s.FollowsClosed(webKey) == 1 }, 5*time.Second, time.Millisecond)
}

// httpGet is u's status and whole body.
func httpGet(t *testing.T, u string) (int, string) {
	t.Helper()
	resp, err := http.Get(u)
	require.NoError(t, err)
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return resp.StatusCode, string(data)
}

// readLine is r's next line, without its newline.
func readLine(t *testing.T, r *bufio.Reader) string {
	t.Helper()
	line, err := r.ReadString('\n')
	require.NoError(t, err)
	return strings.TrimSuffix(line, "\n")
}

// The line builders stamp from 10:00 on 2026-10-08, a second apart.
func TestTheLineBuilders(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "2026-10-08T10:00:01.5Z hi", Stamped(1500*time.Millisecond, "hi"))
	assert.Equal(t, []string{"2026-10-08T10:00:00Z 0", "2026-10-08T10:00:01Z 1"}, LineEvery(2))

	wide := WideLines(2, 5)
	assert.Equal(t, []string{"2026-10-08T10:00:00Z 0xxxx", "2026-10-08T10:00:01Z 1xxxx"}, wide)
	assert.Len(t, LoudLine, 100)
}

// A Pauser holds the first plain read's body after N lines until Gate closes.
func TestAPauserHoldsTheFirstReadUntilItsGate(t *testing.T) {
	t.Parallel()
	s := newServer()
	lines := LineEvery(3)
	s.Add(webKey, lines...)
	gate := make(chan struct{})
	get := (&Pauser{N: 1, Gate: gate}).On(s.Get)

	held, err := get(t.Context(), webPath, query())
	require.NoError(t, err)
	r := bufio.NewReader(held)
	assert.Equal(t, lines[0], readLine(t, r))

	plain, err := get(t.Context(), webPath, query())
	require.NoError(t, err)
	data, err := io.ReadAll(plain)
	require.NoError(t, err)
	assert.Equal(t, body(lines...), string(data), "only the first body pauses")

	close(gate)
	rest, err := io.ReadAll(r)
	require.NoError(t, err)
	assert.Equal(t, body(lines[1:]...), string(rest))
}

// A first line's read and a follow pass a Pauser untouched and leave its pause
// for the next plain read.
func TestAPauserPassesFirstLineReadsAndFollows(t *testing.T) {
	t.Parallel()
	s := newServer()
	lines := LineEvery(2)
	s.Add(webKey, lines...)
	get := (&Pauser{N: 0, Gate: make(chan struct{})}).On(s.Get)

	for _, q := range []url.Values{query("limitBytes=100"), query("follow=true")} {
		b, err := get(t.Context(), webPath, q)
		require.NoError(t, err)
		data, err := io.ReadAll(b)
		require.NoError(t, err)
		assert.Equal(t, body(lines...), string(data), q.Encode())
	}

	ctx, cancel := context.WithCancel(t.Context())
	held, err := get(ctx, webPath, query())
	require.NoError(t, err)
	cancel()
	_, err = io.ReadAll(held)
	assert.ErrorIs(t, err, context.Canceled, "a read its context ended fails")
}

// A Pauser passes a getter's error through.
func TestAPauserPassesAnError(t *testing.T) {
	t.Parallel()
	s := newServer()
	s.SetRefusal(webKey, &Denial{Code: http.StatusForbidden, Message: "no"})
	_, err := (&Pauser{Gate: make(chan struct{})}).On(s.Get)(t.Context(), webPath, query())
	require.EqualError(t, err, "403 no")
}

// Next on a log nothing has written to yet still hands back a channel the first
// Add closes.
func TestNextOnAnEmptyLogWakesOnTheFirstAdd(t *testing.T) {
	t.Parallel()
	s := newServer()
	got, ended, grew := s.Next(webKey, 0)
	assert.Empty(t, got)
	assert.False(t, ended)
	s.Add(webKey, LineEvery(1)...)
	testutil.Wait(t, grew, "the first Add to wake the follows")
}

// A Pauser over a body that breaks fails the read with the break.
func TestAPauserPassesABrokenBody(t *testing.T) {
	t.Parallel()
	s := newServer()
	s.Add(webKey, LineEvery(2)...)
	s.BreakNext(1)
	_, err := (&Pauser{Gate: make(chan struct{})}).On(s.Get)(t.Context(), webPath, query())
	require.ErrorIs(t, err, ErrBroken)
}
