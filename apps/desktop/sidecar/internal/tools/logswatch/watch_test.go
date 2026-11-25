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
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/testutil"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/llm"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/cluster"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/logs"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/logs/logline"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/logs/refusal"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/tools"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/tools/internal/logsinput"
)

// record is a line of payments/webapp-a/app at offset past clock, the ordinal-th of
// its moment, saying msg.
func record(offset time.Duration, ordinal int, msg string) logline.Record {
	return logline.Record{
		Timestamp: clock.Add(offset), Message: msg,
		Stream:      logline.StreamKey{Namespace: "payments", Pod: "webapp-a", Container: "app"},
		ContainerID: "containerd://3f9a", Ordinal: ordinal,
	}
}

// webappIn is a watch of the webapp deployment in payments for /panic/ from clock,
// with limit and maxBytes.
func webappIn(limit, maxBytes int) input {
	return input{
		Input: logsinput.Input{Grep: "panic"},
		since: clock, limit: limit, maxBytes: maxBytes, until: clock.Add(time.Hour),
	}
}

// webappScope is the scope webappIn's tail echoes.
var webappScope = tools.LogsScopeInput{
	Sources: []tools.LogsViewSource{{Namespace: "payments", Kind: tools.LogsSourceDeployment, Name: "webapp", Containers: []string{"app"}}},
	Grep:    "panic",
}

// tailed is a watch over a tail that sends frames, then holds until its
// context ends and returns err, or, as the service does, returns at once after an
// Ended frame; sent closes once every frame it could send is on its way. The
// clock reads ten minutes past clock at the end.
type tailed struct {
	w    *watch
	sent chan struct{}
}

func tailOver(t *testing.T, in input, err error, frames ...logs.TailFrame) tailed {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	sent := make(chan struct{})
	stream := cluster.NewStream(ctx, func(ctx context.Context, out chan<- logs.TailFrame) error {
		for _, f := range frames {
			select {
			case out <- f:
			case <-ctx.Done():
			}
		}
		close(sent)
		if n := len(frames); n > 0 && frames[n-1].Ended != nil {
			return nil
		}
		<-ctx.Done()
		return err
	})
	end := clock.Add(10 * time.Minute)
	since := in.since
	w := &watch{ctx: ctx, cancel: cancel, stream: stream, in: in, scope: webappScope, since: &since, now: func() time.Time { return end }}
	return tailed{w: w, sent: sent}
}

// expire ends the tail as its deadline does: the watch's context reads past
// its deadline, and the stream closes.
func (f tailed) expire(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithDeadline(context.Background(), time.Unix(0, 0))
	t.Cleanup(cancel)
	f.w.ctx = ctx
	f.w.cancel()
}

// waited runs Wait to its end on a goroutine of its own, as the chat's watcher
// does, and answers when it returned.
func (f tailed) waited(t *testing.T) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		f.w.Wait()
		close(done)
	}()
	testutil.Wait(t, done, "the watch's end")
}

func line(rec logline.Record) logs.TailFrame { return logs.TailFrame{Record: &rec} }

func notice(code refusal.Code, msg string) logs.TailFrame {
	return logs.TailFrame{Notice: &logs.Notice{Code: code, Message: msg}}
}

func ended(reason string) logs.TailFrame {
	r := refusal.New(refusal.CodeFinished, reason)
	return logs.TailFrame{Ended: &r}
}

// The first line ends a watch with a limit of 1: it holds the line and cancels
// the tail. A stop that lands as the stream drains does not make it stopped.
func TestTheFirstLineEndsAWatchOfOne(t *testing.T) {
	t.Parallel()
	a := record(time.Minute, 0, "panic: nil map")
	f := tailOver(t, webappIn(1, logsinput.SampleBudget), nil, line(a), line(record(2*time.Minute, 0, "panic: again")))
	f.waited(t)

	end := f.w.End(llm.TaskStoppedByUser)
	assert.Equal(t, tools.WatchMatched, end.Status)
	assert.Equal(t, "it reached its line limit", end.Result.EndedBecause)
	assert.Equal(t, []tools.LogsReadLine{logsinput.ReadLine(a)}, end.Result.Lines)
	assert.Equal(t, "Watched 1 line of deployments/webapp in payments matching /panic/ from 2026-10-18T13:50:00.000000000Z until 2026-10-18T14:00:00.000000000Z, "+
		"ended because it reached its line limit.\n"+
		"2026-10-18T13:51:00.000000000Z payments/webapp-a/app panic: nil map", end.Result.Text)
	assert.Equal(t, logsinput.ViewAround(webappScope, a.Timestamp, clock.Add(10*time.Minute)), end.Result.ViewAround)
	assert.Error(t, f.w.ctx.Err(), "the tail is let go")
}

// The line that reaches the limit ends it, and the lines are oldest first.
func TestTheLimitthLineEndsIt(t *testing.T) {
	t.Parallel()
	a, b, c := record(time.Second, 0, "panic: a"), record(2*time.Second, 0, "panic: b"), record(3*time.Second, 0, "panic: c")
	f := tailOver(t, webappIn(3, logsinput.SampleBudget), nil, line(a), line(b), line(c), line(record(4*time.Second, 0, "panic: d")))
	f.waited(t)

	end := f.w.End("")
	assert.Equal(t, tools.WatchMatched, end.Status)
	require.Len(t, end.Result.Lines, 3)
	assert.Equal(t, "panic: c", end.Result.Lines[2].Message)
	assert.True(t, strings.HasPrefix(end.Result.Text, "Watched 3 lines of deployments/webapp in payments"), end.Result.Text)
}

// Lines from another container that arrive later with an earlier moment are put
// in the service's order once the tail is done, so the sample is oldest first
// and the view around is the first line's.
func TestLinesAreOrderedAcrossContainers(t *testing.T) {
	t.Parallel()
	a, b, c := record(time.Second, 0, "panic: a"), record(2*time.Second, 0, "panic: b"), record(2*time.Second, 0, "panic: c")
	a.Stream.Pod, a.ContainerID = "webapp-b", "containerd://0b1c"
	c.Stream.Pod, c.ContainerID = "webapp-c", "containerd://0c1d"
	f := tailOver(t, webappIn(3, logsinput.SampleBudget), nil, line(c), line(b), line(a))
	f.waited(t)

	end := f.w.End("")
	assert.Equal(t, []tools.LogsReadLine{logsinput.ReadLine(a), logsinput.ReadLine(b), logsinput.ReadLine(c)}, end.Result.Lines)
	assert.Equal(t, a.Timestamp, *end.Result.ViewAround.Anchor.At)
}

// Lines that reach the byte limit end it before the line limit, keeping the line
// that reached it; a line the service cut is held cut, saying so, its message
// through ForModel.
func TestTheByteLimitEndsItOnTheLineThatReachesIt(t *testing.T) {
	t.Parallel()
	cut := record(2*time.Second, 0, "\x1b[31mpanic: b")
	cut.TruncatedBytes = 900
	f := tailOver(t, webappIn(100, 100), nil, line(record(time.Second, 0, "panic: a")), line(cut), line(record(3*time.Second, 0, "c")))
	f.waited(t)

	end := f.w.End("")
	assert.Equal(t, tools.WatchMatched, end.Status)
	assert.Equal(t, "it reached its byte limit", end.Result.EndedBecause)
	require.Len(t, end.Result.Lines, 2)
	assert.Equal(t, "panic: b", end.Result.Lines[1].Message)
	assert.Equal(t, 900, end.Result.Lines[1].TruncatedBytes)
	assert.True(t, strings.HasSuffix(end.Result.Text, "payments/webapp-a/app panic: b … [900 bytes cut]"), end.Result.Text)
}

// A notice is kept for the header, after why the watch ended, while the notices
// fit their budget; past it each is counted. A notice is no line.
func TestNoticesPrintInTheHeader(t *testing.T) {
	t.Parallel()
	frames := []logs.TailFrame{line(record(time.Second, 0, "panic: a"))}
	for i := range 100 {
		frames = append(frames, notice(refusal.CodeStreamSkipped, fmt.Sprintf("payments/webapp-%d/app: the server refused to read its log", i)))
	}
	f := tailOver(t, webappIn(3, logsinput.SampleBudget), nil, frames...)
	done := make(chan struct{})
	go func() {
		f.w.Wait()
		close(done)
	}()
	testutil.Wait(t, f.sent, "the frames")
	f.w.Stop()
	testutil.Wait(t, done, "the watch's end")

	end := f.w.End("model")
	require.Len(t, end.Result.Lines, 1)
	lines := strings.Split(end.Result.Text, "\n")
	assert.True(t, strings.HasSuffix(lines[0], "ended because it was stopped with TaskStop."), lines[0])
	assert.Equal(t, "notice: stream-skipped: payments/webapp-0/app: the server refused to read its log", lines[1])
	kept := len(lines) - 3
	assert.Less(t, kept, 100)
	assert.Equal(t, fmt.Sprintf("notice: %d more notices left out", 100-kept), lines[len(lines)-2])
	assert.LessOrEqual(t, len(strings.Join(lines[1:1+kept], "\n")), logsinput.NoticesBudget)
	assert.Equal(t, "2026-10-18T13:50:01.000000000Z payments/webapp-a/app panic: a", lines[len(lines)-1])
}

// A deadline with lines held is matched, at its time; with none, expired, its
// sample naming the window.
func TestTheDeadlineEndsItMatchedOrExpired(t *testing.T) {
	t.Parallel()
	held := tailOver(t, webappIn(3, logsinput.SampleBudget), nil, line(record(time.Second, 0, "panic: a")))
	testutil.Wait(t, held.sent, "the line")
	held.expire(t)
	held.waited(t)
	end := held.w.End(llm.TaskStoppedByUser)
	assert.Equal(t, tools.WatchMatched, end.Status)
	assert.Equal(t, "its time ran out", end.Result.EndedBecause)

	none := tailOver(t, webappIn(3, logsinput.SampleBudget), nil)
	none.expire(t)
	none.waited(t)
	end = none.w.End("")
	assert.Equal(t, tools.WatchExpired, end.Status)
	assert.Equal(t, []tools.LogsReadLine{}, end.Result.Lines)
	assert.Nil(t, end.Result.ViewAround)
	assert.Equal(t, "No line of deployments/webapp in payments matched /panic/ between 2026-10-18T13:50:00.000000000Z and 2026-10-18T14:00:00.000000000Z.", end.Result.Text)

	anyLine := webappIn(1, logsinput.SampleBudget)
	quiet := tailOver(t, anyLine, nil)
	quiet.w.scope.Grep = ""
	quiet.expire(t)
	quiet.waited(t)
	assert.Equal(t, "No line of deployments/webapp in payments was written between 2026-10-18T13:50:00.000000000Z and 2026-10-18T14:00:00.000000000Z.", quiet.w.End("").Result.Text)
}

// A watch from a read's token names no moment it started at.
func TestATokensWatchSaysWhereItStarted(t *testing.T) {
	t.Parallel()
	f := tailOver(t, webappIn(3, logsinput.SampleBudget), nil)
	f.w.since = nil
	f.expire(t)
	f.waited(t)
	assert.Equal(t, "No line of deployments/webapp in payments matched /panic/ from a read's token until 2026-10-18T14:00:00.000000000Z.", f.w.End("").Result.Text)
}

// A stop ends a watch stopped, keeping the lines it held, in the words of who
// stopped it.
func TestAStopKeepsTheLines(t *testing.T) {
	t.Parallel()
	for by, because := range map[string]string{
		llm.TaskStoppedByUser: "the user stopped it",
		llm.TaskStoppedByApp:  "Kubetail quit",
		"model":               "it was stopped with TaskStop",
	} {
		f := tailOver(t, webappIn(3, logsinput.SampleBudget), nil, line(record(time.Second, 0, "panic: a")))
		testutil.Wait(t, f.sent, "the line")
		f.w.Stop()
		f.w.Stop()
		f.waited(t)

		end := f.w.End(by)
		assert.Equal(t, tools.WatchStopped, end.Status, by)
		assert.Equal(t, because, end.Result.EndedBecause, by)
		assert.Len(t, end.Result.Lines, 1, by)
		assert.Contains(t, end.Result.Text, "ended because "+because+".", by)
	}

	f := tailOver(t, webappIn(3, logsinput.SampleBudget), nil)
	f.w.Stop()
	f.waited(t)
	end := f.w.End(llm.TaskStoppedByUser)
	assert.Equal(t, tools.WatchStopped, end.Status)
	assert.Equal(t, "No line of deployments/webapp in payments matched /panic/ between 2026-10-18T13:50:00.000000000Z and 2026-10-18T14:00:00.000000000Z; "+
		"the watch ended because the user stopped it.", end.Result.Text)
}

// The tail's Ended frame ends the watch with its reason, stripped and redacted:
// with lines held it is matched, with none it failed.
func TestTheTailsEndEndsIt(t *testing.T) {
	t.Parallel()
	end := ended("every container of this view has stopped \x1b[31m")

	held := tailOver(t, webappIn(3, logsinput.SampleBudget), nil, line(record(time.Second, 0, "panic: a")), end)
	held.waited(t)
	got := held.w.End("")
	assert.Equal(t, tools.WatchMatched, got.Status)
	assert.Equal(t, "every container of this view has stopped", got.Result.EndedBecause)

	none := tailOver(t, webappIn(3, logsinput.SampleBudget), nil, end)
	none.waited(t)
	got = none.w.End("")
	assert.Equal(t, tools.WatchFailed, got.Status)
	assert.Equal(t, "every container of this view has stopped", got.Result.EndedBecause)
	assert.Contains(t, got.Result.Text, "the watch ended because every container of this view has stopped.")
}

// A tail that ends on an error ends the watch with none of its text: failed
// with no line held, matched with one.
func TestATailErrorEndsItWithNoText(t *testing.T) {
	t.Parallel()
	a := record(time.Second, 0, "panic: a")
	for _, sent := range [][]logs.TailFrame{nil, {line(a)}} {
		broken := cluster.NewStream(context.Background(), func(ctx context.Context, out chan<- logs.TailFrame) error {
			for _, f := range sent {
				out <- f
			}
			return errors.New("the log file closed: /secret/path")
		})
		ctx, cancel := context.WithCancel(context.Background())
		since := clock
		w := &watch{ctx: ctx, cancel: cancel, stream: broken, in: webappIn(3, logsinput.SampleBudget), scope: webappScope, since: &since, now: func() time.Time { return clock }}
		w.Wait()

		end := w.End("")
		assert.Equal(t, "the logs could not be read", end.Result.EndedBecause)
		assert.NotContains(t, end.Result.Text, "secret")
		assert.Contains(t, end.Result.Text, "ended because the logs could not be read.")
		assert.Len(t, end.Result.Lines, len(sent))
		if len(sent) == 0 {
			assert.Equal(t, tools.WatchFailed, end.Status)
		} else {
			assert.Equal(t, tools.WatchMatched, end.Status)
		}
	}
}

// No sample a watch holds passes the inline limit: max_bytes bounds its lines, and
// the notices' budget its notices.
func TestNoSamplePassesTheInlineLimit(t *testing.T) {
	t.Parallel()
	var frames []logs.TailFrame
	for i := range 200 {
		frames = append(frames, notice(refusal.CodeStreamSkipped, strings.Repeat("n", 200)))
		rec := record(time.Duration(i)*time.Second, 0, strings.Repeat("m", logsinput.DefaultMessageBytes))
		rec.TruncatedBytes = logline.MaxLine
		rec.Gap = &logline.Gap{After: rec.Timestamp.Add(-time.Second), Before: rec.Timestamp}
		frames = append(frames, line(rec))
	}
	in := webappIn(maxLimit, logsinput.SampleBudget)
	f := tailOver(t, in, nil, frames...)
	f.w.scope.Grep = strings.Repeat("g", logline.MaxGrep)
	f.waited(t)

	assert.Less(t, len(f.w.End("").Result.Text), tools.InlineLimit)
}
