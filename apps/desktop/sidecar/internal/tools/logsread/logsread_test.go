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

package logsread

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/cluster"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/logs"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/logs/logstest"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/logs/refusal"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/tools"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/tools/internal/logsinput"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/tools/internal/logsinput/logsinputtest"
)

func TestTheToolIsAShownBoundedRead(t *testing.T) {
	t.Parallel()
	tool := New(nil, nil)
	def := tool.Definition()
	assert.Equal(t, Name, def.Name)
	assert.Equal(t, description, def.Description)
	assert.JSONEq(t, string(inputSchema), string(def.InputSchema))
	assert.Equal(t, tools.ActionLogsRead, tool.ActionKind())
	assert.Equal(t, prompt, tool.Prompt())
	_, err := tool.Action(json.RawMessage(`{"sources":[{"namespace":"prod","resource":"deployments/webapp"}]}`), "", false)
	assert.ErrorIs(t, err, errNotShown)
	assert.Equal(t, callTimeout, tool.CallTimeout(nil))
}

// Input that is not the tool's is refused before anything is read, through Run too.
func TestBadInputReadsNothing(t *testing.T) {
	t.Parallel()
	tool := New(nil, nil)

	text, isError := tool.Run(t.Context(), tools.Runtime{ClusterID: "7"}, json.RawMessage(`{"hunter2":1}`))

	assert.True(t, isError)
	assert.Equal(t, `{"error":"bad-input"}`, text)
}

// webappLogs is the webapp fixture with lines in webapp-a's app container, each a
// second apart from 10:00 saying msgs in turn.
func webappLogs(t *testing.T, msgs ...string) *logsinputtest.Logs {
	t.Helper()
	l := logsinputtest.LogsOf(t, logsinputtest.WebappObjects()...)
	for i, msg := range msgs {
		l.Add("webapp-a", "app", logstest.Stamped(time.Duration(i)*time.Second, msg))
	}
	return l
}

// run calls the tool over l with raw, for cluster 7, and answers what the model
// reads and the action. The page's deadline is the test binary's: a wide read
// under -race on one CPU outlasts production's (TestASlowReadTimesOut pins it).
func run(t *testing.T, l *logsinputtest.Logs, raw string) (string, bool, *tools.Action) {
	t.Helper()
	tool := New(l.Service, l.Logs)
	tool.now = func() time.Time { return clock }
	tool.readTimeout = time.Hour
	return tool.RunShown(t.Context(), tools.Runtime{ClusterID: "7"}, json.RawMessage(raw))
}

// readOf is the read a call over l answers, which must read.
func readOf(t *testing.T, l *logsinputtest.Logs, raw string) (string, tools.LogsReadAction) {
	t.Helper()
	text, isError, shown := run(t, l, raw)
	require.False(t, isError, text)
	require.NotNil(t, shown)
	require.NotNil(t, shown.LogsRead)
	return text, *shown.LogsRead
}

// A first page is a header of the read in words, the request as it ran and the
// page's tokens, then one line per record, oldest first.
func TestAFirstPage(t *testing.T) {
	t.Parallel()
	l := webappLogs(t, "start", "error: one", "fine", "error: two")

	text, _ := readOf(t, l, webapp+`,"grep":"error","since":"2026-10-08T10:00:00Z","until":"2026-10-08T10:00:05Z"}`)

	assert.Equal(t, "Read 2 lines of deployments/webapp in prod, matching /error/. Defaulted container app out of app, istio-proxy, setup.\n"+
		"request: since=2026-10-08T10:00:00.000000000Z until=2026-10-08T10:00:05.999999999Z before=0 after=0 page_from=last page_size=50 max_message_bytes=2048 max_page_bytes=16384\n"+
		"page: prev=none next=none follow=none\n"+
		"2026-10-08T10:00:01.000000000Z prod/webapp-a/app error: one\n"+
		"2026-10-08T10:00:03.000000000Z prod/webapp-a/app error: two", text)
}

// pageOf is the page line of a read's output, split into its three tokens, "" for
// none.
func pageOf(t *testing.T, text string) (prev, next, follow string) {
	t.Helper()
	lines := strings.Split(text, "\n")
	require.GreaterOrEqual(t, len(lines), 3, text)
	fields := strings.Fields(strings.TrimPrefix(lines[2], "page: "))
	require.Len(t, fields, 3, lines[2])
	token := func(field, key string) string {
		v := strings.TrimPrefix(field, key+"=")
		if v == "none" {
			return ""
		}
		return v
	}
	return token(fields[0], "prev"), token(fields[1], "next"), token(fields[2], "follow")
}

// messagesOf is each record line's message of a read's output.
func messagesOf(text string) []string {
	out := []string{}
	for _, line := range strings.Split(text, "\n")[3:] {
		if strings.HasPrefix(line, "notice: ") {
			continue
		}
		out = append(out, line[strings.LastIndex(line, " ")+1:])
	}
	return out
}

// A cursor reads on to the page it names: next and prev from a bounded window,
// follow at a live window's end once a line was written. Its header names the
// token's sources, with no notes, and its request line no page_from.
func TestACursorReadsOn(t *testing.T) {
	t.Parallel()
	l := webappLogs(t, "m0", "m1", "m2", "m3", "m4")
	bounded := webapp + `,"since":"2026-10-08T10:00:00Z","until":"2026-10-08T10:00:04Z","page_size":2`

	text, _ := readOf(t, l, bounded+`,"page_from":"first"}`)
	assert.Equal(t, []string{"m0", "m1"}, messagesOf(text))
	_, next, _ := pageOf(t, text)
	require.NotEmpty(t, next)
	text, _ = readOf(t, l, `{"cursor":"`+next+`","page_size":2}`)
	assert.Equal(t, []string{"m2", "m3"}, messagesOf(text))
	assert.Equal(t, "Read 2 lines of deployments/webapp in prod.", strings.Split(text, "\n")[0])
	assert.Equal(t, "request: since=2026-10-08T10:00:00.000000000Z until=2026-10-08T10:00:04.999999999Z before=0 after=0 page_size=2 max_message_bytes=2048 max_page_bytes=16384", strings.Split(text, "\n")[1])

	text, _ = readOf(t, l, bounded+`}`)
	assert.Equal(t, []string{"m3", "m4"}, messagesOf(text))
	prev, _, _ := pageOf(t, text)
	require.NotEmpty(t, prev)
	text, _ = readOf(t, l, `{"cursor":"`+prev+`","page_size":2}`)
	assert.Equal(t, []string{"m1", "m2"}, messagesOf(text))

	text, _ = readOf(t, l, webapp+`,"since":"2026-10-08T10:00:00Z"}`)
	assert.Equal(t, []string{"m0", "m1", "m2", "m3", "m4"}, messagesOf(text))
	_, _, follow := pageOf(t, text)
	require.NotEmpty(t, follow)
	l.Add("webapp-a", "app", logstest.Stamped(5*time.Second, "m5"))
	text, _ = readOf(t, l, `{"cursor":"`+follow+`"}`)
	assert.Equal(t, []string{"m5"}, messagesOf(text))
}

// A token the service did not write is refused with the service's code.
func TestABadCursorIsRefused(t *testing.T) {
	t.Parallel()
	text, isError, shown := run(t, webappLogs(t), `{"cursor":"abc"}`)
	assert.True(t, isError)
	assert.Nil(t, shown)
	assert.Equal(t, `{"error":"cursor-invalid","message":"this is not a page cursor a page handed back"}`, text)
}

// recorder is the log service with every request it was asked kept.
type recorder struct {
	Fetcher
	asked []logs.FetchRequest
}

func (r *recorder) Fetch(ctx context.Context, id cluster.ClusterID, q logs.FetchRequest) (logs.FetchResponse, error) {
	r.asked = append(r.asked, q)
	return r.Fetcher.Fetch(ctx, id, q)
}

// Every page is asked for the model, so the service redacts each message before it
// cuts it; a continuation sends its token and the caps alone.
func TestEveryPageIsForTheModel(t *testing.T) {
	t.Parallel()
	l := webappLogs(t, "m0", "m1")
	r := &recorder{Fetcher: l.Logs}
	tool := New(l.Service, r)
	tool.now = func() time.Time { return clock }
	text, _, _ := tool.RunShown(t.Context(), tools.Runtime{ClusterID: "7"}, json.RawMessage(webapp+`,"page_size":1}`))
	prev, _, _ := pageOf(t, text)
	tool.RunShown(t.Context(), tools.Runtime{ClusterID: "7"}, json.RawMessage(`{"cursor":"`+prev+`","max_message_bytes":7}`))

	require.Len(t, r.asked, 2)
	assert.True(t, r.asked[0].ForModel)
	assert.Equal(t, logs.FetchRequest{Cursor: &prev, PageSize: 50, MaxMessageBytes: 7, MaxPageBytes: 16384, ForModel: true}, r.asked[1])
}

// Padding lines are marked by their place: before since, or after until. A line at
// either bound is in the range, since a bound takes its whole second.
func TestPaddingIsMarked(t *testing.T) {
	t.Parallel()
	l := webappLogs(t, "m0", "m1", "m2", "m3", "m4", "m5", "m6")

	text, read := readOf(t, l, webapp+`,"since":"2026-10-08T10:00:02Z","until":"2026-10-08T10:00:04Z","before":1,"after":"PT1S","page_from":"first"}`)

	assert.Equal(t, []string{
		"2026-10-08T10:00:01.000000000Z prod/webapp-a/app [before] m1",
		"2026-10-08T10:00:02.000000000Z prod/webapp-a/app m2",
		"2026-10-08T10:00:03.000000000Z prod/webapp-a/app m3",
		"2026-10-08T10:00:04.000000000Z prod/webapp-a/app m4",
		"2026-10-08T10:00:05.000000000Z prod/webapp-a/app [after] m5",
	}, strings.Split(text, "\n")[3:])
	assert.Equal(t, "request: since=2026-10-08T10:00:02.000000000Z until=2026-10-08T10:00:04.999999999Z before=1 after=PT1S page_from=first page_size=50 max_message_bytes=2048 max_page_bytes=16384", strings.Split(text, "\n")[1])
	assert.Equal(t, tools.LogsPadding{Lines: 1}, read.Before)
	assert.Equal(t, tools.LogsPadding{Span: "PT1S"}, read.After)
}

// With until open and after set, the after padding starts at the first page's
// clock, which the request line prints; no fixture line reaches it, so the page
// says its padding is empty and marks nothing.
func TestAnOpenUntilWithAfterPrintsWhereThePaddingStarts(t *testing.T) {
	t.Parallel()
	l := webappLogs(t, "m0", "m1", "m2")

	text, read := readOf(t, l, webapp+`,"since":"2026-10-08T10:00:01Z","after":5}`)

	lines := strings.Split(text, "\n")
	resolved := logsinput.Stamp(read.ResolvedAt)
	assert.Equal(t, "request: since=2026-10-08T10:00:01.000000000Z until=open resolved_at="+resolved+" before=0 after=5 page_from=last page_size=50 max_message_bytes=2048 max_page_bytes=16384", lines[1])
	assert.Equal(t, "notice: empty-padding: no line is stamped at or past the first page's clock, where the padding after an open until starts", lines[3])
	assert.Equal(t, []string{"m1", "m2"}, messagesOf(text))
	assert.Equal(t, []tools.LogsNotice{{Code: "empty-padding", Message: "no line is stamped at or past the first page's clock, where the padding after an open until starts"}}, read.Notices)
}

// fetchFunc is a log service that answers each request as the func does.
type fetchFunc func(q logs.FetchRequest) (logs.FetchResponse, error)

func (f fetchFunc) Fetch(_ context.Context, _ cluster.ClusterID, q logs.FetchRequest) (logs.FetchResponse, error) {
	return f(q)
}

// runOn calls the tool over the webapp mirror and fetch with raw.
func runOn(t *testing.T, fetch fetchFunc, raw string) (string, bool, *tools.Action) {
	t.Helper()
	tool := New(logsinputtest.Watching(t), fetch)
	tool.now = func() time.Time { return clock }
	return tool.RunShown(t.Context(), tools.Runtime{ClusterID: "7"}, json.RawMessage(raw))
}

// echo answers a request with no lines and notices, echoing it.
func echo(notices ...logs.Notice) fetchFunc {
	return func(q logs.FetchRequest) (logs.FetchResponse, error) {
		return logs.FetchResponse{Request: q, ResolvedAt: clock, Notices: append([]logs.Notice{}, notices...)}, nil
	}
}

// Each notice is a line of its own, its message through ForModel, until they reach
// logsinput.NoticesBudget; the rest are counted. A page can skip every one of MaxStreams.
func TestNoticesStopAtTheirBudget(t *testing.T) {
	t.Parallel()
	var notices []logs.Notice
	for i := range logs.MaxStreams {
		notices = append(notices, logs.Notice{Code: refusal.CodeStreamSkipped, Message: fmt.Sprintf("prod/webapp-%d/app: the server refused to read its log \x1b[31msk-AAAAAAAAAAAAAAAAAAAAAAAAAAAAAA\x1b[0m", i)})
	}
	text, isError, _ := runOn(t, echo(notices...), webapp+`}`)
	require.False(t, isError, text)

	lines := strings.Split(text, "\n")[3:]
	shown := len(lines) - 1
	assert.Less(t, shown, logs.MaxStreams)
	assert.Equal(t, "notice: stream-skipped: prod/webapp-0/app: the server refused to read its log [redacted]", lines[0])
	assert.Equal(t, fmt.Sprintf("notice: %d more notices left out", logs.MaxStreams-shown), lines[shown])
	assert.LessOrEqual(t, len(strings.Join(lines[:shown], "\n")), logsinput.NoticesBudget)
}

// The notes on the sources stop at notesBudget, and the sources past it are
// counted.
func TestNotesStopAtTheirBudget(t *testing.T) {
	t.Parallel()
	var sources []string
	for i := range 100 {
		sources = append(sources, fmt.Sprintf(`{"namespace":"prod","resource":"pods/gone-%d"}`, i))
	}
	text, isError, _ := runOn(t, echo(), `{"sources":[`+strings.Join(sources, ",")+`]}`)
	require.False(t, isError, text)

	words, _, _ := strings.Cut(text, "\n")
	assert.True(t, strings.HasPrefix(words, "No lines of pods/gone-0 in prod and 99 more sources. pods/gone-0: default container."), words)
	assert.Regexp(t, ` Notes on \d+ more sources left out\.$`, words)
	assert.Less(t, len(words), notesBudget+200)
}

// The action is the window as the service ran it, the lines as the model read
// them, whether the page had more either way, its notices, and the view around
// its first line.
func TestTheActionIsThePage(t *testing.T) {
	t.Parallel()
	l := webappLogs(t, "m0", "m1", "m2")

	text, read := readOf(t, l, webapp+`,"grep":"m","since":"2026-10-08T10:00:00Z","until":"2026-10-08T10:00:02Z","page_size":2,"page_from":"first"}`)

	since, until := time.Date(2026, 10, 8, 10, 0, 0, 0, time.UTC), time.Date(2026, 10, 8, 10, 0, 2, 999999999, time.UTC)
	first := logs.PageFromFirst
	assert.Equal(t, "webapp", read.Sources[0].Name)
	assert.Equal(t, "m", read.Grep)
	assert.Equal(t, &since, read.Since)
	assert.Equal(t, &until, read.Until)
	assert.Equal(t, &first, read.PageFrom)
	assert.False(t, read.OlderMore)
	assert.True(t, read.NewerMore)
	assert.Equal(t, []tools.LogsNotice{}, read.Notices)
	lines := strings.Split(text, "\n")[3:]
	require.Len(t, read.Lines, len(lines))
	for i, line := range read.Lines {
		assert.Equal(t, "prod", line.Namespace)
		assert.Equal(t, "webapp-a", line.Pod)
		assert.True(t, strings.HasSuffix(lines[i], " "+line.Message), lines[i])
	}
	require.NotNil(t, read.ViewAround)
	assert.Equal(t, read.Lines[0].Timestamp, *read.ViewAround.Anchor.At)
	assert.Equal(t, read.ResolvedAt, read.ViewAround.ResolvedAt)
	assert.Equal(t, read.LogsScopeInput, read.ViewAround.LogsScopeInput)

	_, read = readOf(t, l, webapp+`,"since":"2026-10-08T11:00:00Z"}`)
	assert.Nil(t, read.ViewAround, "no line, no view")
	assert.Nil(t, read.Until)
}

// A read past its deadline answers timed-out itself, inside the call's bound.
func TestASlowReadTimesOut(t *testing.T) {
	t.Parallel()
	tool := New(logsinputtest.Watching(t), fetchFunc(nil))
	tool.logs = blocking{}
	tool.readTimeout = time.Millisecond
	text, isError, shown := tool.RunShown(t.Context(), tools.Runtime{ClusterID: "7"}, json.RawMessage(webapp+`}`))
	assert.True(t, isError)
	assert.Nil(t, shown)
	assert.Equal(t, `{"error":"timed-out"}`, text)
}

// A read that finds another server as its deadline passes ends the run all the same.
func TestAServerMoveOutranksTheDeadline(t *testing.T) {
	t.Parallel()
	tool := New(logsinputtest.Watching(t), fetchFunc(nil))
	tool.logs = movedAtDeadline{}
	tool.readTimeout = time.Millisecond
	moved := 0
	rt := tools.Runtime{ClusterID: "7", ServerUID: "uid-1", OnServerMoved: func() { moved++ }}

	text, isError, _ := tool.RunShown(t.Context(), rt, json.RawMessage(webapp+`}`))

	assert.True(t, isError)
	assert.JSONEq(t, `{"error":"server-changed"}`, text)
	assert.Equal(t, 1, moved)
}

// movedAtDeadline is a log service whose fetch finds another server once its
// context ends.
type movedAtDeadline struct{}

func (movedAtDeadline) Fetch(ctx context.Context, _ cluster.ClusterID, _ logs.FetchRequest) (logs.FetchResponse, error) {
	<-ctx.Done()
	return logs.FetchResponse{}, cluster.ErrIdentityMismatch
}

// blocking is a log service whose fetch answers only once its context ends.
type blocking struct{}

func (blocking) Fetch(ctx context.Context, _ cluster.ClusterID, _ logs.FetchRequest) (logs.FetchResponse, error) {
	<-ctx.Done()
	return logs.FetchResponse{}, ctx.Err()
}

// A source the mirror refuses is refused before any read, with its code.
func TestAMirrorRefusalReadsNothing(t *testing.T) {
	t.Parallel()
	text, isError, _ := runOn(t, func(logs.FetchRequest) (logs.FetchResponse, error) {
		t.Fatal("read past a refusal")
		return logs.FetchResponse{}, nil
	}, `{"sources":[{"namespace":"prod","resource":"pods/webapp-7f9c","containers":["nope"]}]}`)
	assert.True(t, isError)
	assert.Contains(t, text, `"error":"no-container"`)
}

// tokenOf is the token key a read's output prints, which must be set.
func tokenOf(t *testing.T, text, key string) string {
	t.Helper()
	prev, next, follow := pageOf(t, text)
	token := map[string]string{"prev": prev, "next": next, "follow": follow}[key]
	require.NotEmpty(t, token, "%s in %s", key, strings.Split(text, "\n")[2])
	return token
}

// A cut line is read whole by its printed timestamp in since and until with no
// message cap, and the bytes cut say how much more that read brings.
func TestACutLineIsReadWholeByItsStamp(t *testing.T) {
	t.Parallel()
	long := strings.Repeat("y", 3000)
	l := webappLogs(t, "m0", long, "m2")

	text, _ := readOf(t, l, webapp+`,"since":"PT1H"}`)
	line := strings.Split(text, "\n")[4]
	assert.True(t, strings.HasSuffix(line, " … [952 bytes cut]"), line)
	stamp := line[:strings.IndexByte(line, ' ')]

	text, read := readOf(t, l, webapp+`,"since":"`+stamp+`","until":"`+stamp+`","max_message_bytes":0}`)
	require.Len(t, read.Lines, 1)
	assert.Equal(t, long, read.Lines[0].Message)
	assert.Equal(t, 0, read.Lines[0].TruncatedBytes)
	assert.NotContains(t, text, "bytes cut")
}

// A walk of a window three lines a page, each page from the printed next,
// repeats and skips nothing.
func TestAWalkByNextRepeatsAndSkipsNothing(t *testing.T) {
	t.Parallel()
	var msgs []string
	for i := range 15 {
		msgs = append(msgs, fmt.Sprintf("m%d", i))
	}
	l := webappLogs(t, msgs...)

	text, _ := readOf(t, l, webapp+`,"since":"PT1H","until":"2026-10-08T10:00:14Z","page_size":3,"page_from":"first"}`)
	read := messagesOf(text)
	for range 4 {
		text, _ = readOf(t, l, `{"cursor":"`+tokenOf(t, text, "next")+`","page_size":3}`)
		read = append(read, messagesOf(text)...)
	}
	assert.Equal(t, msgs, read)
	_, next, follow := pageOf(t, text)
	assert.Empty(t, next+follow, "the window's last page")
}

// The design's examples, each over the fixture.
func TestTheDesignsExamples(t *testing.T) {
	t.Parallel()
	l := webappLogs(t, "m0", "m1", "m2", "m3", "m4", "m5", "m6")
	stamp := func(s int) string { return fmt.Sprintf("2026-10-08T10:00:%02d.000000000Z", s) }

	t.Run("a duration since, prev walked back", func(t *testing.T) {
		text, _ := readOf(t, l, webapp+`,"since":"PT1H","page_size":3}`)
		assert.Equal(t, []string{"m4", "m5", "m6"}, messagesOf(text))
		text, _ = readOf(t, l, `{"cursor":"`+tokenOf(t, text, "prev")+`","page_size":3}`)
		assert.Equal(t, []string{"m1", "m2", "m3"}, messagesOf(text))
		text, _ = readOf(t, l, `{"cursor":"`+tokenOf(t, text, "prev")+`","page_size":3}`)
		assert.Equal(t, []string{"m0"}, messagesOf(text))
		prev, _, _ := pageOf(t, text)
		assert.Empty(t, prev)
	})

	t.Run("a moment padded by lines", func(t *testing.T) {
		text, _ := readOf(t, l, webapp+`,"since":"`+stamp(3)+`","until":"`+stamp(3)+`","before":2,"after":2}`)
		assert.Equal(t, []string{
			stamp(1) + " prod/webapp-a/app [before] m1", stamp(2) + " prod/webapp-a/app [before] m2", stamp(3) + " prod/webapp-a/app m3",
			stamp(4) + " prod/webapp-a/app [after] m4", stamp(5) + " prod/webapp-a/app [after] m5",
		}, strings.Split(text, "\n")[3:])
	})

	t.Run("a range padded by spans, first through next", func(t *testing.T) {
		text, _ := readOf(t, l, webapp+`,"since":"`+stamp(2)+`","until":"`+stamp(3)+`","before":"PT1S","after":"PT1S","page_from":"first","page_size":2}`)
		assert.Equal(t, []string{stamp(1) + " prod/webapp-a/app [before] m1", stamp(2) + " prod/webapp-a/app m2"}, strings.Split(text, "\n")[3:])
		text, _ = readOf(t, l, `{"cursor":"`+tokenOf(t, text, "next")+`","page_size":2}`)
		assert.Equal(t, []string{stamp(3) + " prod/webapp-a/app m3", stamp(4) + " prod/webapp-a/app [after] m4"}, strings.Split(text, "\n")[3:])
	})

	t.Run("prev with a smaller page answers the same window", func(t *testing.T) {
		first, _ := readOf(t, l, webapp+`,"since":"`+stamp(1)+`","until":"`+stamp(6)+`","page_size":4}`)
		assert.Equal(t, []string{"m3", "m4", "m5", "m6"}, messagesOf(first))
		text, _ := readOf(t, l, `{"cursor":"`+tokenOf(t, first, "prev")+`","page_size":1}`)
		assert.Equal(t, []string{"m2"}, messagesOf(text))
		window := func(text string) string {
			fields := strings.Fields(strings.Split(text, "\n")[1])
			return strings.Join(fields[1:5], " ")
		}
		assert.Equal(t, window(first), window(text))
	})

	t.Run("a duration since, then follow polled", func(t *testing.T) {
		l := webappLogs(t, "m0", "m1")
		text, _ := readOf(t, l, webapp+`,"since":"PT1H"}`)
		assert.Equal(t, []string{"m0", "m1"}, messagesOf(text))
		text, _ = readOf(t, l, `{"cursor":"`+tokenOf(t, text, "follow")+`"}`)
		assert.True(t, strings.HasPrefix(text, "No lines of deployments/webapp in prod."), text)
		l.Add("webapp-a", "app", logstest.Stamped(2*time.Second, "m2"))
		text, _ = readOf(t, l, `{"cursor":"`+tokenOf(t, text, "follow")+`"}`)
		assert.Equal(t, []string{"m2"}, messagesOf(text))
	})

	t.Run("an until at the call's moment prints no follow", func(t *testing.T) {
		text, _ := readOf(t, l, webapp+`,"since":"PT1H","until":"`+clock.Format(time.RFC3339)+`"}`)
		assert.Len(t, messagesOf(text), 7)
		assert.Equal(t, "page: prev=none next=none follow=none", strings.Split(text, "\n")[2])
	})
}

// Stored shown actions decode the same forever: never edit this table. A bump
// adds a table of its own.
func TestShownOldRowsDecodeTheSame(t *testing.T) {
	box := tools.NewBox(nil, &Tool{})
	at := time.Date(2026, time.October, 20, 9, 30, 0, 0, time.UTC)
	pod := tools.LogsScopeInput{Sources: []logs.ScopeSource{{Namespace: "web", Kind: logs.SourcePod, Name: "api-0"}}}
	for stored, want := range map[string]tools.Action{
		`{"description":"Read api","command":null,"read":null,"write":null,"edit":null,"search":null,"fetch":null,"memory":null,"delegate":null,"kubeQuery":null,"logsView":null,"logsRead":{"sources":[{"namespace":"web","kind":"pod","name":"api-0","containers":null,"allContainers":false,"previous":false}],"filters":null,"grep":"","since":"2026-10-20T08:30:00Z","until":null,"before":{"lines":5,"span":""},"after":{"lines":0,"span":"PT1M"},"pageFrom":"last","resolvedAt":"2026-10-20T09:30:00Z","lines":[{"timestamp":"2026-10-20T09:29:00Z","namespace":"web","pod":"api-0","container":"app","previous":false,"message":"panic: boom","gap":null,"truncatedBytes":0}],"olderMore":true,"newerMore":false,"notices":[{"code":"cut","message":"cut"}],"viewAround":{"sources":[{"namespace":"web","kind":"pod","name":"api-0","containers":null,"allContainers":false,"previous":false}],"filters":null,"grep":"","anchor":{"kind":"at","at":"2026-10-20T09:29:00Z"},"pinToEnd":false,"resolvedAt":"2026-10-20T09:30:00Z"}},"logsWatch":null}`: {
			Description: "Read api",
			LogsRead: &tools.LogsReadAction{
				LogsScopeInput: pod, Since: new(at.Add(-time.Hour)), Before: tools.LogsPadding{Lines: 5}, After: tools.LogsPadding{Span: "PT1M"},
				PageFrom: new(logs.PageFromLast), ResolvedAt: at,
				Lines:     []tools.LogsReadLine{{Timestamp: at.Add(-time.Minute), Namespace: "web", Pod: "api-0", Container: "app", Message: "panic: boom"}},
				OlderMore: true, Notices: []tools.LogsNotice{{Code: "cut", Message: "cut"}},
				ViewAround: &tools.LogsViewAction{
					LogsScopeInput: pod, Anchor: tools.LogsViewAnchor{Kind: tools.LogsAnchorAt, At: new(at.Add(-time.Minute))}, ResolvedAt: at,
				},
			},
		},
	} {
		got, err := box.Shown(Name, ContractName, json.RawMessage(stored))
		require.NoError(t, err, stored)
		assert.Equal(t, want, got, stored)
	}
}

// A monitor run reads the server its incident opened on: once the cluster names
// another, a first page is refused at its resolve and a cursor at the service's,
// each before any log is read, and the run is told so.
func TestAReadOfAnotherServerIsRefusedAndEndsTheRun(t *testing.T) {
	t.Parallel()
	l := webappLogs(t, "m0", "m1")
	text, _ := readOf(t, l, webapp+`,"page_size":1}`)
	prev, _, _ := pageOf(t, text)
	require.NotEmpty(t, prev)

	tool := New(l.Service, l.Logs)
	tool.now = func() time.Time { return clock }
	moved := 0
	rt := tools.Runtime{ClusterID: "7", ServerUID: "uid-0", OnServerMoved: func() { moved++ }}
	for _, raw := range []string{webapp + `}`, `{"cursor":"` + prev + `"}`} {
		text, isError, shown := tool.RunShown(t.Context(), rt, json.RawMessage(raw))
		assert.True(t, isError, raw)
		assert.Nil(t, shown, raw)
		assert.JSONEq(t, `{"error":"server-changed"}`, text, raw)
	}
	assert.Equal(t, 2, moved)

	rt.ServerUID = "uid-1"
	_, isError, _ := tool.RunShown(t.Context(), rt, json.RawMessage(webapp+`}`))
	assert.False(t, isError, "the server the run reads")
	assert.Equal(t, 2, moved)
}
