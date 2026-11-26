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

package logswatch

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/testutil"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/cluster"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/logs"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/logs/logstest"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/logs/refusal"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/tools"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/tools/internal/logsinput"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/tools/internal/logsinput/logsinputtest"
)

func TestTheToolIsAShownBoundedWatch(t *testing.T) {
	t.Parallel()
	tool := New(nil, nil)
	def := tool.Definition()
	assert.Equal(t, Name, def.Name)
	assert.Equal(t, description, def.Description)
	assert.JSONEq(t, string(inputSchema), string(def.InputSchema))
	assert.Equal(t, tools.ActionLogsWatch, tool.ActionKind())
	assert.Equal(t, prompt, tool.Prompt())
	assert.Equal(t, callTimeout, tool.CallTimeout(nil))
	_, err := tool.Action(json.RawMessage(`{"sources":[{"namespace":"prod","resource":"deployments/webapp"}]}`), "", false)
	assert.ErrorIs(t, err, errNotShown)
}

// tokenScope is the scope a fake cursor holds.
var tokenScope = tools.LogsScopeInput{Sources: []tools.LogsViewSource{{Namespace: "prod", Kind: tools.LogsSourcePod, Name: "webapp-b", Containers: []string{"app"}}}}

// fakeTails is the log service as the tool opens a tail: a stream that holds
// until its context ends, closing ended then, or err; each tail's context and
// request kept. A cursor's tail echoes tokenScope, and every tail resolvedAt.
// stalled, when set, holds the open itself until its context ends, closing
// stalled once it has begun.
type fakeTails struct {
	err        error
	stalled    chan struct{}
	ctx        context.Context
	q          logs.TailRequest
	ended      chan struct{}
	opened     bool
	resolvedAt time.Time
}

func newFakeTails() *fakeTails {
	return &fakeTails{ended: make(chan struct{}), resolvedAt: time.Date(2026, 10, 18, 9, 0, 0, 0, time.UTC)}
}

func (f *fakeTails) TailFromCursor(ctx context.Context, _ cluster.ClusterID, q logs.TailRequest) (logs.TailResponse, error) {
	if f.stalled != nil {
		close(f.stalled)
		<-ctx.Done()
		return logs.TailResponse{}, ctx.Err()
	}
	if f.err != nil {
		return logs.TailResponse{}, f.err
	}
	f.ctx, f.q, f.opened = ctx, q, true
	echo := q
	if q.Cursor != nil {
		echo.Scope = tokenScope
	}
	frames := cluster.NewStream(ctx, func(ctx context.Context, _ chan<- logs.TailFrame) error {
		<-ctx.Done()
		close(f.ended)
		return nil
	})
	return logs.TailResponse{Request: echo, ResolvedAt: f.resolvedAt, Frames: frames}, nil
}

// fakeWatches is the chat's watches: it keeps what it is handed and answers id, or
// err.
type fakeWatches struct {
	err     error
	started []tools.Watch
}

func (f *fakeWatches) Start(w tools.Watch) (string, error) {
	if f.err != nil {
		return "", f.err
	}
	f.started = append(f.started, w)
	return "t-1", nil
}

// webapp is the webapp deployment's sources in prod, its app container watched.
const webapp = `{"sources":[{"namespace":"prod","resource":"deployments/webapp","containers":["app"]}]`

// near is a clock near real time, to the second: a watch's deadline is until as
// an absolute moment, which a clock fixed in the past would have passed.
func near() time.Time { return time.Now().UTC().Truncate(time.Second) }

// run calls the tool over the watching cluster, tails and watches with raw on
// ctx at the clock now, and answers what the model reads and the action.
func run(t *testing.T, ctx context.Context, now time.Time, f *fakeTails, w *fakeWatches, raw string) (string, bool, *tools.Action) {
	t.Helper()
	tool := New(logsinputtest.Watching(t), f)
	tool.now = func() time.Time { return now }
	return tool.RunShown(ctx, tools.Runtime{ClusterID: "7", Watches: w}, json.RawMessage(raw))
}

// watchOf is the watch a call starts at the clock now, which must start, its
// receipt and action.
func watchOf(t *testing.T, now time.Time, f *fakeTails, raw string) (string, tools.LogsWatchAction) {
	t.Helper()
	w := &fakeWatches{}
	text, isError, shown := run(t, t.Context(), now, f, w, raw)
	require.False(t, isError, text)
	require.NotNil(t, shown)
	require.NotNil(t, shown.LogsWatch)
	require.Len(t, w.started, 1)
	t.Cleanup(w.started[0].Stop)
	return text, *shown.LogsWatch
}

// The receipt names the sources, the pattern, the start, the limits and the end,
// then the task; the action carries what the clock made of them; the tail is
// asked for the model from the moment, its deadline the watch's end.
func TestTheReceiptNamesTheWatch(t *testing.T) {
	t.Parallel()
	now := near()
	since := now.Add(-5 * time.Minute)
	f := newFakeTails()
	text, action := watchOf(t, now, f, webapp+`,"grep":"panic|Exception","since":"`+since.Format(time.RFC3339)+`","until":"PT10M","limit":3,"max_message_bytes":100,"description":"Panics"}`)

	assert.Equal(t, "Watching deployments/webapp in prod for /panic|Exception/ from "+logsinput.Stamp(since)+", up to 3 lines or 16384 bytes, "+
		"until "+logsinput.Stamp(now.Add(10*time.Minute))+" (task t-1). You will be told when it ends.", text)
	assert.Equal(t, &since, action.Since)
	assert.Equal(t, now.Add(10*time.Minute), action.Until)
	assert.Equal(t, f.resolvedAt, action.ResolvedAt)
	assert.Equal(t, []int{3, logsinput.SampleBudget, 100}, []int{action.Limit, action.MaxBytes, action.MaxMessageBytes})
	assert.Equal(t, "panic|Exception", action.Grep)
	assert.Equal(t, []string{"app"}, action.Sources[0].Containers)
	assert.Equal(t, &since, f.q.Since)
	assert.Nil(t, f.q.Cursor)
	assert.Equal(t, "panic|Exception", f.q.Grep)
	assert.Equal(t, 100, f.q.MaxMessageBytes)
	assert.True(t, f.q.ForModel)
	deadline, ok := f.ctx.Deadline()
	require.True(t, ok)
	assert.Equal(t, action.Until, deadline)
}

// A bare call watches from now for any line, up to a day; the tail is told its
// start, now on the tool's clock.
func TestABareWatchIsForAnyLineFromNow(t *testing.T) {
	t.Parallel()
	now := near()
	f := newFakeTails()
	text, action := watchOf(t, now, f, webapp+`}`)
	assert.Equal(t, "Watching deployments/webapp in prod for any line from now, up to 1 line or 16384 bytes, "+
		"until "+logsinput.Stamp(now.Add(maxWatch))+" (task t-1). You will be told when it ends.", text)
	assert.Equal(t, &now, action.Since)
	assert.Equal(t, &now, f.q.Since)
	assert.Equal(t, logsinput.DefaultMessageBytes, f.q.MaxMessageBytes)
}

// A watch from a read's token sends the token alone, and its receipt, action and
// view carry the scope the tail echoes, the token's.
func TestACursorWatchCarriesTheTokensScope(t *testing.T) {
	t.Parallel()
	f := newFakeTails()
	text, action := watchOf(t, near(), f, `{"cursor":"tok","limit":2}`)
	assert.True(t, strings.HasPrefix(text, "Watching pods/webapp-b in prod for any line from a read's token, up to 2 lines"), text)
	assert.Equal(t, tokenScope, action.LogsScopeInput)
	assert.Nil(t, action.Since)
	require.NotNil(t, f.q.Cursor)
	assert.Equal(t, "tok", *f.q.Cursor)
	assert.Nil(t, f.q.Since)
	assert.Empty(t, f.q.Sources)
	assert.True(t, f.q.ForModel)
}

// An until past the cap is cut to it, and the receipt says so.
func TestTheReceiptSaysAnUntilWasCut(t *testing.T) {
	t.Parallel()
	now := near()
	text, action := watchOf(t, now, newFakeTails(), webapp+`,"until":"P2D"}`)
	assert.Equal(t, now.Add(maxWatch), action.Until)
	assert.Contains(t, text, "until "+logsinput.Stamp(now.Add(maxWatch))+", the longest a watch runs (task t-1)")
}

// The tail outlives the call: the call's context ending leaves it open, and the
// watch's stop ends it.
func TestTheTailOutlivesTheCall(t *testing.T) {
	t.Parallel()
	f, w := newFakeTails(), &fakeWatches{}
	ctx, cancel := context.WithCancel(t.Context())
	_, isError, _ := run(t, ctx, near(), f, w, webapp+`}`)
	require.False(t, isError)
	cancel()

	// A negative assertion: the tail's own context stays open past the call's.
	select {
	case <-f.ended:
		t.Fatal("the call's end ended the tail")
	case <-time.After(10 * time.Millisecond):
	}
	w.started[0].Stop()
	testutil.Wait(t, f.ended, "the stop to end the tail")
}

// A tail that stalls while it opens is still the call's: the call's end ends
// the open, and nothing starts.
func TestTheCallBoundsTheTailsOpen(t *testing.T) {
	t.Parallel()
	f, w := &fakeTails{stalled: make(chan struct{})}, &fakeWatches{}
	ctx, cancel := context.WithCancel(t.Context())
	go func() {
		<-f.stalled
		cancel()
	}()
	_, isError, shown := run(t, ctx, near(), f, w, webapp+`}`)
	assert.True(t, isError)
	assert.Nil(t, shown)
	assert.Empty(t, w.started)
}

// A start the chat refuses lets the tail go and answers StartRefusal's words.
func TestARefusedStartLetsTheTailGo(t *testing.T) {
	t.Parallel()
	f := newFakeTails()
	text, isError, shown := run(t, t.Context(), near(), f, &fakeWatches{err: tools.ErrChatTaskLimit}, webapp+`}`)
	assert.True(t, isError)
	assert.Nil(t, shown)
	assert.Equal(t, tools.StartRefusal(tools.ErrChatTaskLimit), text)
	testutil.Wait(t, f.ended, "the tail to end")
}

// A scope the mirror refuses opens nothing; a tail the service refuses answers
// its code and its words, redacted, and any other failure none of its text.
func TestARefusedWatchOpensNothing(t *testing.T) {
	t.Parallel()
	f := newFakeTails()
	text, isError, _ := run(t, t.Context(), near(), f, &fakeWatches{}, `{"sources":[{"namespace":"prod","resource":"pods/webapp-a","containers":["nope"]}]}`)
	assert.True(t, isError)
	assert.JSONEq(t, `{"error":"no-container","source":0,"containers":["app","istio-proxy","setup"]}`, text)
	assert.False(t, f.opened)

	f = &fakeTails{err: refusal.New(refusal.CodeCursorInvalid, "a tail goes on from a page's next or follow, not its prev \x1b[31msk-"+strings.Repeat("A", 30))}
	text, isError, _ = run(t, t.Context(), near(), f, &fakeWatches{}, `{"cursor":"tok"}`)
	assert.True(t, isError)
	assert.Equal(t, `{"error":"cursor-invalid","message":"a tail goes on from a page's next or follow, not its prev [redacted]"}`, text)

	f = &fakeTails{err: errors.New("open /home/user/cache: disk I/O error")}
	text, _, _ = run(t, t.Context(), near(), f, &fakeWatches{}, webapp+`}`)
	assert.JSONEq(t, `{"error":"read-failed"}`, text)
}

// A runtime with no watches, a subagent's or a monitor's, opens nothing.
func TestARuntimeWithNoWatchesOpensNothing(t *testing.T) {
	t.Parallel()
	f := newFakeTails()
	tool := New(logsinputtest.Watching(t), f)
	_, isError, shown := tool.RunShown(t.Context(), tools.Runtime{ClusterID: "7"}, json.RawMessage(webapp+`}`))
	assert.True(t, isError)
	assert.Nil(t, shown)
	assert.False(t, f.opened)
}

// Input that is not the tool's is refused before anything opens, through Run too.
func TestBadInputOpensNothing(t *testing.T) {
	t.Parallel()
	f := newFakeTails()
	tool := New(nil, f)

	text, isError := tool.Run(t.Context(), tools.Runtime{ClusterID: "7", Watches: &fakeWatches{}}, json.RawMessage(`{"hunter2":1}`))

	assert.True(t, isError)
	assert.Equal(t, `{"error":"bad-input"}`, text)
	assert.False(t, f.opened)
}

// watchOver starts a watch over the fixture l at a clock near real time with raw,
// runs it to its end on a goroutine of its own, as the chat does, and answers how
// it ended.
func watchOver(t *testing.T, l *logsinputtest.Logs, raw string, write func()) tools.WatchEnd {
	t.Helper()
	tool := New(l.Service, l.Logs)
	w := &fakeWatches{}
	text, isError, _ := tool.RunShown(t.Context(), tools.Runtime{ClusterID: "7", Watches: w}, json.RawMessage(raw))
	require.False(t, isError, text)
	require.Len(t, w.started, 1)
	done := make(chan struct{})
	go func() {
		w.started[0].Wait()
		close(done)
	}()
	if write != nil {
		write()
	}
	testutil.Wait(t, done, "the watch to fill")
	return w.started[0].End("")
}

// A watch of a line longer than max_message_bytes holds it as the service cut it,
// redacted first, saying the bytes cut.
func TestAWatchHoldsALineCut(t *testing.T) {
	t.Parallel()
	l := logsinputtest.LogsOf(t, logsinputtest.WebappObjects()...)
	l.Add("webapp-a", "app", logstest.Stamped(0, "0123456789abcdef"))

	end := watchOver(t, l, `{"sources":[{"namespace":"prod","resource":"pods/webapp-a"}],"since":"2026-10-08T10:00:00Z","max_message_bytes":10}`, nil)

	require.Len(t, end.Result.Lines, 1)
	assert.Equal(t, "0123456789", end.Result.Lines[0].Message)
	assert.Equal(t, 6, end.Result.Lines[0].TruncatedBytes)
	assert.True(t, strings.HasSuffix(end.Result.Text, "prod/webapp-a/app 0123456789 … [6 bytes cut]"), end.Result.Text)
}

// A watch from a read's follow token holds the lines written after that page,
// and none of its own.
func TestAWatchGoesOnFromARead(t *testing.T) {
	t.Parallel()
	l := logsinputtest.LogsOf(t, logsinputtest.WebappObjects()...)
	l.Add("webapp-a", "app", logstest.Stamped(0, "m0"), logstest.Stamped(time.Second, "m1"))
	since := time.Date(2026, 10, 8, 10, 0, 0, 0, time.UTC)
	scope := tools.LogsScopeInput{Sources: []tools.LogsViewSource{{Namespace: "prod", Kind: tools.LogsSourcePod, Name: "webapp-a", Containers: []string{}}}}
	page, err := l.Logs.Fetch(t.Context(), "7", logs.FetchRequest{Scope: scope, Window: logs.Window{Since: &since}, PageFrom: logs.PageFromLast})
	require.NoError(t, err)
	require.Len(t, page.Records, 2)
	require.NotNil(t, page.Page.Follow)

	end := watchOver(t, l, `{"cursor":"`+*page.Page.Follow+`","limit":1}`, func() {
		l.Add("webapp-a", "app", logstest.Stamped(2*time.Second, "m2"))
	})

	require.Len(t, end.Result.Lines, 1)
	assert.Equal(t, "m2", end.Result.Lines[0].Message)
	assert.Equal(t, tools.WatchMatched, end.Status)
}

// Stored shown actions and watch results decode the same forever: never edit
// these tables. A bump adds tables of its own.
func TestShownOldRowsDecodeTheSame(t *testing.T) {
	box := tools.NewBox(nil, &Tool{})
	at := time.Date(2026, time.October, 20, 9, 30, 0, 0, time.UTC)
	for stored, want := range map[string]tools.Action{
		`{"description":"Watch api","command":null,"read":null,"write":null,"edit":null,"search":null,"fetch":null,"memory":null,"delegate":null,"kubeQuery":null,"logsView":null,"logsRead":null,"logsWatch":{"sources":[{"namespace":"web","kind":"deployment","name":"api","containers":null,"allContainers":false,"previous":false}],"filters":null,"grep":"OOM","since":"2026-10-20T09:30:00Z","limit":10,"maxBytes":4096,"maxMessageBytes":512,"resolvedAt":"2026-10-20T09:30:00Z","until":"2026-10-20T10:00:00Z"}}`: {
			Description: "Watch api",
			LogsWatch: &tools.LogsWatchAction{
				LogsScopeInput: tools.LogsScopeInput{Sources: []logs.ScopeSource{{Namespace: "web", Kind: logs.SourceDeployment, Name: "api"}}, Grep: "OOM"},
				Since:          new(at), Limit: 10, MaxBytes: 4096, MaxMessageBytes: 512, ResolvedAt: at, Until: at.Add(30 * time.Minute),
			},
		},
	} {
		got, err := box.Shown(Name, ContractName, json.RawMessage(stored))
		require.NoError(t, err, stored)
		assert.Equal(t, want, got, stored)
	}

	pod := tools.LogsScopeInput{Sources: []logs.ScopeSource{{Namespace: "web", Kind: logs.SourcePod, Name: "api-0"}}}
	for stored, want := range map[string]tools.WatchResult{
		`{"text":"1 line","lines":[{"timestamp":"2026-10-20T09:29:00Z","namespace":"web","pod":"api-0","container":"app","previous":false,"message":"panic: boom","gap":null,"truncatedBytes":0}],"endedBecause":"limit","viewAround":{"sources":[{"namespace":"web","kind":"pod","name":"api-0","containers":null,"allContainers":false,"previous":false}],"filters":null,"grep":"","anchor":{"kind":"at","at":"2026-10-20T09:29:00Z"},"pinToEnd":false,"resolvedAt":"2026-10-20T09:30:00Z"}}`: {
			Text:         "1 line",
			Lines:        []tools.LogsReadLine{{Timestamp: at.Add(-time.Minute), Namespace: "web", Pod: "api-0", Container: "app", Message: "panic: boom"}},
			EndedBecause: "limit",
			ViewAround:   &tools.LogsViewAction{LogsScopeInput: pod, Anchor: tools.LogsViewAnchor{Kind: tools.LogsAnchorAt, At: new(at.Add(-time.Minute))}, ResolvedAt: at},
		},
	} {
		got, err := box.WatchResult(Name, ContractName, json.RawMessage(stored))
		require.NoError(t, err, stored)
		assert.Equal(t, want, got, stored)
	}
}
