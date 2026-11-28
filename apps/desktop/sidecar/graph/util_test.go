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

package graph

import (
	"context"
	"errors"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/graph/model"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/agent/monitor/kubesignals"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/testutil"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/run/permissions"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/envvars"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/logs"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/logs/logline"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/logs/refusal"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/settings"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/tools"
	"github.com/vektah/gqlparser/v2/gqlerror"
)

// TestChatRefusalsCarryTheirCode lists one case per entry of chatRefusals by hand,
// since its package cannot see the table. A new entry fails here until it has a case
// there.
func TestTheChatRefusalTableIsPinned(t *testing.T) {
	if got := len(chatRefusals); got != 9 {
		t.Fatalf("chatRefusals has %d entries; give TestChatRefusalsCarryTheirCode a case for each, then pin the count", got)
	}
}

// mapSubscription applies mapFn to every source value in order, then closes the
// output and runs unsub when the source channel closes.
func TestMapSubscriptionMapsValuesAndClosesWithSource(t *testing.T) {
	sub := make(chan int)
	unsubbed := make(chan struct{})

	out := mapSubscription(context.Background(), sub, func() { close(unsubbed) }, strconv.Itoa)

	go func() {
		sub <- 1
		sub <- 2
		close(sub)
	}()

	for _, want := range []string{"1", "2"} {
		if got := testutil.Recv(t, out, "a mapped value"); got != want {
			t.Fatalf("got %q, want %q", got, want)
		}
	}

	testutil.RecvClosed(t, out, "the output channel after the source closed")
	testutil.RecvClosed(t, unsubbed, "the unsub channel")
}

// Cancelling ctx tears the stream down: the output channel closes and unsub
// runs, even though the source channel stays open.
func TestMapSubscriptionStopsOnContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	sub := make(chan int)
	unsubbed := make(chan struct{})

	out := mapSubscription(ctx, sub, func() { close(unsubbed) }, strconv.Itoa)

	cancel()

	testutil.RecvClosed(t, out, "the output channel after ctx cancel")
	testutil.RecvClosed(t, unsubbed, "the unsub channel")
}

// Cancelling ctx while the pump is blocked sending a mapped value (no reader
// on the output channel) still tears the stream down — the send must not wedge
// the goroutine past cancellation.
func TestMapSubscriptionStopsOnContextCancelDuringSend(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	sub := make(chan int)
	unsubbed := make(chan struct{})

	out := mapSubscription(ctx, sub, func() { close(unsubbed) }, strconv.Itoa)

	// The source is unbuffered, so this returns only once the pump has taken the
	// value — which leaves it blocked sending "1" to the unread output.
	sub <- 1
	cancel()

	// Wait on unsub before reading out: a reader would make the blocked send
	// ready beside ctx.Done, and select would pick between them at random.
	testutil.RecvClosed(t, unsubbed, "the unsub channel")
	testutil.RecvClosed(t, out, "the output channel")
}

// A variable the service refuses is a validation error under the env-var rule,
// carrying the refusal's words; a missing cluster is not found.
func TestEnvVarErrNamesTheRefusal(t *testing.T) {
	var e *gqlerror.Error
	require.ErrorAs(t, envVarErr(envvars.ErrFull), &e)
	assert.Equal(t, "KUBETAIL_VALIDATION_ERROR", e.Extensions["code"])
	assert.Equal(t, "env-var", e.Extensions["rule"])
	assert.Equal(t, envvars.ErrFull.Error(), e.Message)

	require.ErrorAs(t, envVarErr(envvars.ErrClusterGone), &e)
	assert.Equal(t, "KUBETAIL_RECORD_NOT_FOUND", e.Extensions["code"])
}

// A mode's refusal is a validation error in the user's words, the way every
// other permission refusal is.
func TestPermissionsAfterNamesAModeRefusal(t *testing.T) {
	_, err := (&Resolver{}).permissionsAfter(context.Background(), settings.ModeRefusal(`no mode is named "sometimes"`))
	var e *gqlerror.Error
	require.ErrorAs(t, err, &e)
	assert.Equal(t, "KUBETAIL_VALIDATION_ERROR", e.Extensions["code"])
	assert.Equal(t, `no mode is named "sometimes"`, e.Message)
}

// A failure that is no refusal stays opaque through the executable, variable and
// permission mappers: a client can do nothing with a store fault but report it.
func TestAFailureThatIsNoRefusalStaysOpaque(t *testing.T) {
	boom := errors.New("disk I/O error")

	assert.Equal(t, boom, sandboxExecutableErr(boom))
	assert.Equal(t, boom, envVarErr(boom))
	got, err := (&Resolver{}).permissionsAfter(context.Background(), boom)
	assert.Nil(t, got)
	assert.Equal(t, boom, err)
}

// A logs refusal is a validation error whose rule is the refusal's code, bare or
// inside the mirror's refusals, so a client can act on it without reading words.
func TestLogsErrCarriesTheRefusalsCode(t *testing.T) {
	for _, err := range []error{
		logs.ErrTooMany,
		refusal.Unmirrored{Refusal: refusal.Refusal{Code: refusal.CodeNotMirrored, Reason: "no pods"}, Source: 1},
		refusal.NoNodes{Refusal: refusal.Refusal{Code: refusal.CodeNoNodes, Reason: "no nodes"}},
	} {
		want, _ := refusal.As(err)
		var e *gqlerror.Error
		if !errors.As(logsErr(err), &e) {
			t.Fatalf("logsErr(%v) is not a GraphQL error", err)
		}
		if e.Extensions["code"] != "KUBETAIL_VALIDATION_ERROR" || e.Extensions["rule"] != string(want.Code) || e.Message != want.Reason {
			t.Errorf("logsErr(%v) = %q %v", err, e.Message, e.Extensions)
		}
	}
	if err := errors.New("db closed"); logsErr(err) != err {
		t.Error("logsErr changed an error that is no refusal")
	}
}

// scope is a scope as the wire hands it over.
var scope = logs.Scope{
	Sources: []logs.ScopeSource{{Namespace: "prod", Kind: logs.SourceDeployment, Name: "web", Containers: []string{"app"}}},
	Filters: []logs.ScopeFilter{{Field: logs.FilterZone, Values: []string{"eu-1a"}}},
	Grep:    "error",
}

// A first page maps field by field: the scope, the bounds, a count of lines and a
// span read as a duration, where the first page comes from and the caps.
func TestFetchRequestOfAFirstPage(t *testing.T) {
	since, until := time.Date(2026, 10, 8, 10, 0, 0, 0, time.UTC), time.Date(2026, 10, 8, 11, 0, 0, 0, time.UTC)
	ten, span, last := 10, "PT5M", logs.PageFromLast
	size, msg, page := 50, 2048, 16384

	got, err := fetchRequestOf(model.LogsFetchInput{
		Scope: &scope, Since: &since, Until: &until,
		Before: &model.LogsPaddingInput{Lines: &ten}, After: &model.LogsPaddingInput{Span: &span},
		PageFrom: &last, PageSize: &size, MaxMessageBytes: &msg, MaxPageBytes: &page,
	})

	require.NoError(t, err)
	assert.Equal(t, logs.FetchRequest{
		Scope:    scope,
		Window:   logs.Window{Since: &since, Until: &until, Before: logs.Padding{Lines: 10}, After: logs.Padding{Span: 5 * time.Minute}},
		PageFrom: logs.PageFromLast, PageSize: 50, MaxMessageBytes: 2048, MaxPageBytes: 16384,
	}, got)
}

// A continuation is its cursor as it came, with seen beside it; what it omits is
// the zero the service reads as its default.
func TestFetchRequestOfAContinuation(t *testing.T) {
	cursor := "opaque"

	got, err := fetchRequestOf(model.LogsFetchInput{Cursor: &cursor, Seen: []string{"line-a", "line-b"}})

	require.NoError(t, err)
	assert.Equal(t, logs.FetchRequest{Cursor: &cursor, Seen: []string{"line-a", "line-b"}}, got)
}

// A span the reader refuses, or one over the service's cap, is bad-padding with
// the reader's words. A zero count beside a span is a span, as null would be, and a
// count and a span together are left to the service.
func TestFetchRequestOfRefusesABadSpan(t *testing.T) {
	for _, span := range []string{"5m", "P1M", "P8D", ""} {
		_, err := fetchRequestOf(model.LogsFetchInput{After: &model.LogsPaddingInput{Span: &span}})
		r, ok := refusal.As(err)
		require.True(t, ok, span)
		assert.Equal(t, refusal.CodeBadPadding, r.Code, span)
		assert.NotEmpty(t, r.Reason, span)
	}
	zero, ten, span := 0, 10, "PT1M"
	for lines, want := range map[*int]logs.Padding{&zero: {Span: time.Minute}, &ten: {Lines: 10, Span: time.Minute}} {
		got, err := fetchRequestOf(model.LogsFetchInput{Before: &model.LogsPaddingInput{Lines: lines, Span: &span}})
		require.NoError(t, err)
		assert.Equal(t, want, got.Before)
	}
}

// A page is the service's answer on the wire: the request as it ran, each span
// formatted and no padding an empty span, the three cursors, the records and the
// notices by code.
func TestPageOfAFirstPage(t *testing.T) {
	since := time.Date(2026, 10, 8, 10, 0, 0, 0, time.UTC)
	prev, follow := "prev", "follow"
	rec := logline.Record{Timestamp: since, Message: "hello", TruncatedBytes: 3}

	got := pageOf(logs.FetchResponse{
		Request: logs.FetchRequest{
			Scope:    scope,
			Window:   logs.Window{Since: &since, Before: logs.Padding{Span: 300 * time.Second}, After: logs.Padding{Lines: 20}},
			PageFrom: logs.PageFromLast, PageSize: 100, MaxMessageBytes: 2048, MaxPageBytes: 1 << 20,
		},
		ResolvedAt: since.Add(time.Hour),
		Page:       logs.Page{Prev: &prev, Follow: &follow},
		Records:    []logline.Record{rec},
		Notices:    []logs.Notice{{Code: refusal.CodeEmptyPadding, Message: "none before"}},
	})

	last := logs.PageFromLast
	assert.Equal(t, &model.LogsFetchResult{
		Request: &model.LogsFetchRequest{
			Sources: ptrSlice(scope.Sources), Filters: ptrSlice(scope.Filters), Grep: "error", Since: &since,
			Before: &tools.LogsPadding{Span: "PT5M"}, After: &tools.LogsPadding{Lines: 20},
			PageFrom: &last, PageSize: 100, MaxMessageBytes: 2048, MaxPageBytes: 1 << 20,
		},
		ResolvedAt: since.Add(time.Hour),
		Page:       &model.LogRecordsPlace{Prev: &prev, Follow: &follow},
		Records:    []*logline.Record{&rec},
		Notices:    []*tools.LogsNotice{{Code: "empty-padding", Message: "none before"}},
	}, got)
}

// A continuation's echo has no pageFrom, and an empty page still answers lists.
func TestPageOfAContinuation(t *testing.T) {
	got := pageOf(logs.FetchResponse{Request: logs.FetchRequest{PageSize: 100}, Notices: []logs.Notice{}})

	assert.Nil(t, got.Request.PageFrom)
	assert.Equal(t, &tools.LogsPadding{}, got.Request.Before)
	assert.NotNil(t, got.Request.Sources)
	assert.NotNil(t, got.Request.Filters)
	assert.NotNil(t, got.Records)
	assert.NotNil(t, got.Notices)
}

// A follow maps field by field from each start: a moment with its scope, a
// cursor alone, and a cursor with seen and a message cap.
func TestTailRequestOfEachStart(t *testing.T) {
	since := time.Date(2026, 10, 8, 10, 0, 0, 0, time.UTC)
	cursor, msgCap := "opaque", 100
	for _, c := range []struct {
		in   model.LogsTailInput
		want logs.TailRequest
	}{
		{model.LogsTailInput{Scope: &scope, Since: &since}, logs.TailRequest{Scope: scope, Since: &since}},
		{model.LogsTailInput{Cursor: &cursor}, logs.TailRequest{Cursor: &cursor}},
		{
			model.LogsTailInput{Cursor: &cursor, Seen: []string{"line-a"}, MaxMessageBytes: &msgCap},
			logs.TailRequest{Cursor: &cursor, Seen: []string{"line-a"}, MaxMessageBytes: 100},
		},
	} {
		assert.Equal(t, c.want, tailRequestOf(c.in))
	}
}

// Each frame's type is whichever field is set, and an ending is a notice of the
// refusal's code and reason.
func TestFrameOfEachKind(t *testing.T) {
	rec := &logline.Record{Message: "hello"}
	notice := logs.Notice{Code: refusal.CodeStreamSkipped, Message: "forbidden"}
	ended := refusal.New(refusal.CodeFinished, "every container has stopped")

	assert.Equal(t, &model.LogsTailFrame{Type: model.LogsTailFrameTypeLine, Record: rec}, frameOf(logs.TailFrame{Record: rec}))
	assert.Equal(t, &model.LogsTailFrame{Type: model.LogsTailFrameTypeNotice, Notice: &tools.LogsNotice{Code: "stream-skipped", Message: "forbidden"}},
		frameOf(logs.TailFrame{Notice: &notice}))
	assert.Equal(t, &model.LogsTailFrame{Type: model.LogsTailFrameTypeEnded, Ended: &tools.LogsNotice{Code: "finished", Message: "every container has stopped"}},
		frameOf(logs.TailFrame{Ended: &ended}))
}

// A Save or Delete error that is no refusal reaches the wire as it is.
func TestProviderKeyErrPassesAnUnknownErrorThrough(t *testing.T) {
	err := errors.New("boom")
	assert.Same(t, err, providerKeyErr(err))
}

// A row Kubetail cannot apply reads as two rules under its id; the wire lists it
// once.
func TestRulePointersListEachIDOnce(t *testing.T) {
	rules := []permissions.Rule{{ID: "a"}, {ID: "bad", AccessLevel: permissions.Write}, {ID: "bad", AccessLevel: permissions.DataRead}, {ID: "c"}}

	got := rulePointers(rules)

	if len(got) != 3 || got[0].ID != "a" || got[1].AccessLevel != permissions.Write || got[2].ID != "c" {
		t.Fatalf("rulePointers = %v", got)
	}
}

// Each registered integration has a MonitorIntegration value, and each value
// names an integration, so no target or signal record is nulled for want of one.
func TestEveryMonitorIntegrationHasAValue(t *testing.T) {
	values := map[model.MonitorIntegration]bool{}
	for _, v := range monitorIntegrations {
		values[v] = true
	}
	for _, v := range model.AllMonitorIntegration {
		assert.True(t, values[v], v)
	}
	assert.Len(t, values, len(model.AllMonitorIntegration))
	got, err := monitorIntegrationOf(kubesignals.Name)
	require.NoError(t, err)
	assert.Equal(t, model.MonitorIntegrationKubernetes, got)
	_, err = monitorIntegrationOf("other")
	assert.Error(t, err)
}

// Stored JSON that does not decode fails the field that reads it.
func TestMonitorJSONThatDoesNotDecodeFailsItsField(t *testing.T) {
	_, err := monitorAttributes(`[`)
	assert.ErrorContains(t, err, "read attributes")
	_, err = monitorUnknownSignals(`[`)
	assert.ErrorContains(t, err, "read unknown signals")
}
