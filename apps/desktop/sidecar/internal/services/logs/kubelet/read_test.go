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

import (
	"context"
	"errors"
	"io"
	"math"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"testing/iotest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/testutil"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/logs/logline"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/logs/logstest"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/logs/logstore"
)

// web is the container app of pod web in prod, on node-a.
var web = Log{Key: logline.StreamKey{Namespace: "prod", Pod: "web", Container: "app"}, Place: logline.Place{Node: "node-a"}, Instance: containerID("uid-web", "app", 0)}

// runOf is l's container and runtime id, at no moment.
func runOf(l Log) logline.Cursor {
	return logline.Cursor{ContainerName: logline.ContainerName{Namespace: l.Key.Namespace, Pod: l.Key.Pod, Container: l.Key.Container}, RunPlace: logline.RunPlace{ContainerID: l.Instance}}
}

// A forward read since a moment asks for its second and keeps that moment's line.
func TestAForwardReadSinceAMoment(t *testing.T) {
	t.Parallel()
	s := newLogServer()
	s.Add("prod/web/app", logstest.Stamped(500*time.Millisecond, "zero"), logstest.Stamped(1200*time.Millisecond, "one"), logstest.Stamped(2*time.Second, "two"))

	r := readForward(t.Context(), s.Get, web, Options{Since: from(1200 * time.Millisecond)})

	recs := drain(r)
	require.NoError(t, r.Err)
	assert.False(t, r.Short)
	assert.Equal(t, []string{"one", "two"}, messages(recs))
	assert.Equal(t, web.Key, recs[0].Stream)
	assert.Equal(t, web.Place, recs[0].Place)
	assert.Equal(t, web.Instance, recs[0].ContainerID)
	assert.Equal(t, at(1200*time.Millisecond), recs[0].Timestamp)
	q := s.Requests()[0]
	assert.Equal(t, "2026-10-08T10:00:01Z", q.Get("sinceTime"))
	assert.Equal(t, "true", q.Get("timestamps"))
	assert.Equal(t, "app", q.Get("container"))
}

// A forward read with no lower bound starts at the backend's oldest line, so it
// is at the beginning however far it read; one from a moment does not.
func TestAForwardReadWithNoLowerBoundIsAtTheBeginning(t *testing.T) {
	t.Parallel()
	s := newLogServer()
	s.Add("prod/web/app", logstest.LineEvery(5)...)

	r := readForward(t.Context(), s.Get, web, Options{Until: to(2 * time.Second)})
	drain(r)
	assert.True(t, r.AtBeginning)

	r = readForward(t.Context(), s.Get, web, Options{Since: from(time.Second)})
	drain(r)
	assert.False(t, r.AtBeginning)
}

// A forward read that reaches a finished instance's last line says so; one of a
// running instance, or one stopped at until, does not.
func TestAForwardReadOfAFinishedInstanceSaysAtEnd(t *testing.T) {
	t.Parallel()
	s := newLogServer()
	s.Add("prod/web/app", logstest.LineEvery(5)...)
	done := web
	done.Finished = true
	read := func(st Log, o Options) *Read {
		r := readForward(t.Context(), s.Get, st, o)
		drain(r)
		return r
	}

	assert.True(t, read(done, Options{Since: from(time.Second)}).AtEnd, "finished")
	assert.False(t, read(web, Options{}).AtEnd, "running")
	assert.False(t, read(done, Options{Until: to(2 * time.Second)}).AtEnd, "until")
	previous := web
	previous.Key.Previous, previous.Finished = true, true
	assert.True(t, read(previous, Options{}).AtEnd, "previous")
}

// After a moment inside a second, the read asks for that second and drops the
// lines at and before the moment.
func TestAfterAMomentDropsItsLines(t *testing.T) {
	t.Parallel()
	s := newLogServer()
	s.Add("prod/web/app", logstest.Stamped(time.Second, "a"), logstest.Stamped(1200*time.Millisecond, "cursor"), logstest.Stamped(1200*time.Millisecond, "cursor too"), logstest.Stamped(1500*time.Millisecond, "b"))

	r := readForward(t.Context(), s.Get, web, Options{Since: after(1200 * time.Millisecond)})

	assert.Equal(t, []string{"b"}, messages(drain(r)))
	assert.Equal(t, "2026-10-08T10:00:01Z", s.Requests()[0].Get("sinceTime"))
}

// Lines of one moment are numbered in order, and a bound at one of them keeps or
// drops that line alone.
func TestLinesOfOneMomentAreNumbered(t *testing.T) {
	t.Parallel()
	s := newLogServer()
	s.Add("prod/web/app", logstest.Stamped(time.Second, "a"), logstest.Stamped(time.Second, "b"), logstest.Stamped(time.Second, "c"), logstest.Stamped(2*time.Second, "d"))
	get := s.Get

	recs := drain(readForward(t.Context(), get, web, Options{}))
	ordinals := []int{}
	for _, rec := range recs {
		ordinals = append(ordinals, rec.Ordinal)
	}
	assert.Equal(t, []int{0, 1, 2, 0}, ordinals)

	after := readForward(t.Context(), get, web, Options{Since: logline.Bound{At: lineAt(web.Key, time.Second, 1), Inclusive: false}})
	assert.Equal(t, []string{"c", "d"}, messages(drain(after)))
	until := readForward(t.Context(), get, web, Options{Until: logline.Bound{At: lineAt(web.Key, time.Second, 1), Inclusive: true}})
	assert.Equal(t, []string{"a", "b"}, messages(drain(until)))
}

// A read stops at its until, which keeps its own line.
func TestAForwardReadStopsAtUntil(t *testing.T) {
	t.Parallel()
	s := newLogServer()
	s.Add("prod/web/app", logstest.Stamped(0, "a"), logstest.Stamped(time.Second, "b"), logstest.Stamped(2*time.Second, "c"))

	r := readForward(t.Context(), s.Get, web, Options{Until: to(time.Second)})

	assert.Equal(t, []string{"a", "b"}, messages(drain(r)))
}

// The previous instance is asked for, and its lines say so.
func TestThePreviousInstanceIsSent(t *testing.T) {
	t.Parallel()
	s := newLogServer()
	s.Add("prod/web/app/previous", logstest.Stamped(0, "before the restart"))
	src := web
	src.Key.Previous, src.Finished = true, true

	r := readForward(t.Context(), s.Get, src, Options{})

	recs := drain(r)
	assert.Equal(t, []string{"before the restart"}, messages(recs))
	assert.True(t, recs[0].Stream.Previous)
	assert.Equal(t, "true", s.Requests()[0].Get("previous"))
}

// A follow since a moment reads the lines from it, then one written after it opened.
func TestATailDeliversALineWrittenLater(t *testing.T) {
	t.Parallel()
	s := newLogServer()
	s.Add("prod/web/app", logstest.Stamped(0, "old"))

	r := readForward(t.Context(), claimOn(t, s).Get, web, Options{Since: from(0), Follow: true})

	assert.Equal(t, "old", testutil.Recv(t, r.Lines.Chan(), "the old line").Message)
	s.Add("prod/web/app", logstest.Stamped(time.Second, "new"))
	assert.Equal(t, "new", testutil.Recv(t, r.Lines.Chan(), "the new line").Message)
	assert.Equal(t, "true", s.Requests()[0].Get("follow"))
}

// A follow numbers a moment's lines from the top of its second, as a read opened
// again from a cursor in it does, so a line keeps its ordinal across the two.
func TestATailNumbersAMomentFromItsSecond(t *testing.T) {
	t.Parallel()
	s := newLogServer()
	s.Add("prod/web/app", logstest.Stamped(100*time.Millisecond, "a"), logstest.Stamped(100*time.Millisecond, "b"))

	r := readForward(t.Context(), claimOn(t, s).Get, web, Options{Follow: true, Since: from(100 * time.Millisecond)})
	first := testutil.Recv(t, r.Lines.Chan(), "the first line")
	assert.Equal(t, 0, first.Ordinal)

	again := readForward(t.Context(), claimOn(t, s).Get, web, Options{Follow: true, Since: logline.Bound{At: logline.CursorOf(first.Record), Inclusive: false}})
	assert.Equal(t, "b", testutil.Recv(t, again.Lines.Chan(), "the line after it").Message)
	assert.Empty(t, s.Requests()[0].Get("tailLines"))
}

// A read the connection's retirement cut stops short at the last line it read
// whole, and says why.
func TestARetiredReadStopsShort(t *testing.T) {
	t.Parallel()
	s := newLogServer()
	s.Add("prod/web/app", logstest.Stamped(0, "a"), logstest.Stamped(time.Second, "b"))
	get := func(ctx context.Context, path string, q url.Values) (io.ReadCloser, error) {
		body, err := claimOn(t, s).Get(ctx, path, q)
		if err != nil {
			return nil, err
		}
		return cutAfter{body, 1, ErrRetired}, nil
	}

	r := readForward(t.Context(), get, web, Options{})

	assert.Equal(t, []string{"a"}, messages(drain(r)))
	assert.True(t, r.Short)
	assert.Equal(t, lineAt(web.Key, 0, 0), r.Stop)
	assert.ErrorIs(t, r.Err, ErrRetired)
}

// A read that fails past its first line in range stops short at the last line it
// read whole, keeping its failure; one that fails before any line is not short.
func TestAReadThatFailsPartwayStopsShort(t *testing.T) {
	t.Parallel()
	reset := errors.New("connection reset")
	for n, short := range map[int]bool{1: true, 0: false} {
		s := newLogServer()
		s.Add("prod/web/app", logstest.Stamped(0, "a"), logstest.Stamped(time.Second, "b"))
		get := func(ctx context.Context, path string, q url.Values) (io.ReadCloser, error) {
			body, err := s.Get(ctx, path, q)
			if err != nil {
				return nil, err
			}
			return cutAfter{body, n, reset}, nil
		}

		r := readForward(t.Context(), get, web, Options{})

		assert.Len(t, drain(r), n)
		assert.Equal(t, short, r.Short, n)
		if short {
			assert.Equal(t, lineAt(web.Key, 0, 0), r.Stop)
		}
		assert.ErrorIs(t, r.Err, reset)
	}
}

// cutAfter is a body that ends with err after n lines.
type cutAfter struct {
	io.ReadCloser
	n   int
	Err error
}

func (b cutAfter) Read(p []byte) (int, error) {
	data, err := io.ReadAll(b.ReadCloser)
	if err != nil {
		return 0, err
	}
	lines := strings.SplitAfterN(string(data), "\n", b.n+1)
	return copy(p, strings.Join(lines[:b.n], "")), b.Err
}

// A message is cleaned before it is sent, its colours kept, and every line is sent:
// the grep runs where the lines are read, never in the backend.
func TestALineIsCleaned(t *testing.T) {
	t.Parallel()
	s := newLogServer()
	s.Add("prod/web/app", logstest.Stamped(0, "\x1b[31merror\x1b[0m:\x1b[2K disk"), logstest.Stamped(time.Second, "ok"))

	r := readForward(t.Context(), s.Get, web, Options{})

	assert.Equal(t, []string{"\x1b[31merror\x1b[0m: disk", "ok"}, messages(drain(r)))
}

// A line with no timestamp, or one that is not a time, is skipped, as kubetail's
// reader skips it; under limits it counts toward no second.
func TestALineWithNoTimestampIsSkipped(t *testing.T) {
	t.Parallel()
	for name, o := range map[string]Options{"no limits": {}, "under limits": {Limits: limitsOf(smallCaps)}} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			s := newLogServer()
			s.Add("prod/web/app", "garbage", "not a time "+strings.Repeat("x", 2<<10), logstest.Stamped(0, "a"))

			r := readForward(t.Context(), s.Get, web, o)

			assert.Equal(t, []string{"a"}, messages(drain(r)))
			assert.NoError(t, r.Err)
		})
	}
}

// A line past 64 KiB is read to there and the rest skipped, so the cleaned message
// is cut at 16 KiB, and the line after it is read whole.
func TestALongLineIsCut(t *testing.T) {
	t.Parallel()
	s := newLogServer()
	s.Add("prod/web/app", logstest.Stamped(0, strings.Repeat("x", maxLine+100)), logstest.Stamped(time.Second, "after"))

	r := readForward(t.Context(), s.Get, web, Options{})

	recs := drain(r)
	require.Len(t, recs, 2)
	assert.Equal(t, strings.Repeat("x", logline.MaxMessage)+"…", recs[0].Message)
	assert.Equal(t, "after", recs[1].Message)
}

// A container the API server refuses ends the read with its status.
func TestARefusedReadEndsWithItsStatus(t *testing.T) {
	t.Parallel()
	s := newLogServer()
	s.Refuse["prod/web/app"] = logstest.Denial{Code: 400, Message: "container is waiting to start"}

	r := readForward(t.Context(), s.Get, web, Options{})

	assert.Empty(t, drain(r))
	assert.EqualError(t, r.Err, "container is waiting to start")
}

// A read whose context ends while a line waits to be taken ends clean.
func TestACancelledReadEndsClean(t *testing.T) {
	t.Parallel()
	s := newLogServer()
	s.Add("prod/web/app", logstest.LineEvery(3)...)
	get := s.Get
	for name, read := range map[string]func(context.Context) *Read{
		"forward":  func(ctx context.Context) *Read { return readForward(ctx, get, web, Options{}) },
		"backward": func(ctx context.Context) *Read { return readBackward(ctx, get, web, backwardOpts()) },
		"windows of time": func(ctx context.Context) *Read {
			o := backwardOpts()
			o.Until = to(2 * time.Second)
			return readBackward(ctx, get, web, o)
		},
	} {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			r := read(ctx)
			testutil.Recv(t, r.Lines.Chan(), "the first line")

			cancel()

			testutil.WaitClosed(t, r.Lines.Chan(), "the read to end")
			assert.NoError(t, r.Err)
			assert.False(t, r.Short)
		})
	}
}

// A read whose context ends before its GET answers ends clean.
func TestAReadCancelledBeforeItsGetEndsClean(t *testing.T) {
	t.Parallel()
	get := func(ctx context.Context, _ string, _ url.Values) (io.ReadCloser, error) {
		return nil, ctx.Err()
	}
	for name, read := range map[string]func(context.Context) *Read{
		"forward":  func(ctx context.Context) *Read { return readForward(ctx, get, web, Options{}) },
		"backward": func(ctx context.Context) *Read { return readBackward(ctx, get, web, backwardOpts()) },
		"windows of time": func(ctx context.Context) *Read {
			o := backwardOpts()
			o.Until = to(2 * time.Second)
			return readBackward(ctx, get, web, o)
		},
	} {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			cancel()

			r := read(ctx)

			assert.Empty(t, drain(r))
			assert.NoError(t, r.Err)
			assert.False(t, r.Short)
		})
	}
}

// A line each does not take is not counted as read.
func TestALineNotTakenIsNotRead(t *testing.T) {
	t.Parallel()
	s := newLogServer()
	s.Add("prod/web/app", logstest.Stamped(0, "a"), logstest.Stamped(time.Second, "b"))
	rd := &reader{ctx: t.Context(), get: s.Get, Key: web.Key, instance: web.Instance}

	read, err := rd.lines(rd.query(time.Time{}), Options{}, 0, func(rec logline.Record, _ int) bool { return rec.Message == "a" })

	require.NoError(t, err)
	assert.Equal(t, lineAt(web.Key, 0, 0), read.last)
}

// A forward read is cut once it has spent its allowance and the pool, at the last
// line it read.
func TestAForwardReadIsCutAtItsLimit(t *testing.T) {
	t.Parallel()
	s := newLogServer()
	s.Add("prod/web/app", logstest.LineEvery(1000)...)
	c := smallCaps
	c.Query = 8 << 10

	r := readForward(t.Context(), s.Get, web, Options{Limits: limitsOf(c)})

	recs := drain(r)
	assert.NoError(t, r.Err)
	assert.True(t, r.Short)
	assert.Equal(t, logline.CursorOf(recs[len(recs)-1]), r.Stop)
	// 8 KiB is about 330 lines, as the body arrives a buffer at a time.
	assert.Less(t, len(recs), 400)
	assert.Greater(t, len(recs), 300)
}

// A forward read is never cut before its first line in range: the lines of its
// second before the bound are read and charged, and the first line past them goes
// out before the cut.
func TestAForwardReadIsNeverCutBeforeItsFirstLine(t *testing.T) {
	t.Parallel()
	s := newLogServer()
	s.Add("prod/web/app", logstest.Stamped(time.Second, strings.Repeat("x", 900)), logstest.Stamped(1500*time.Millisecond, "in range"), logstest.Stamped(2*time.Second, "past"))
	c := smallCaps
	c.Query = 512

	r := readForward(t.Context(), s.Get, web, Options{Since: logline.Bound{At: lineAt(web.Key, time.Second, 0), Inclusive: false}, Limits: limitsOf(c)})

	assert.Equal(t, []string{"in range"}, messages(drain(r)))
	assert.True(t, r.Short)
	assert.Equal(t, lineAt(web.Key, 1500*time.Millisecond, 0), r.Stop)
}

// A follow's fill reads every second whole: no second stops it, and it is cut at
// the end of the second it spent its share in.
func TestATailsFillIsCutAtTheEndOfASecond(t *testing.T) {
	t.Parallel()
	s := newLogServer()
	for i := range 50 {
		s.Add("prod/web/app", logstest.Stamped(time.Duration(i)*100*time.Millisecond, strings.Repeat("x", 200)))
	}
	c := smallCaps
	c.Query = 1 << 10

	r := readForward(t.Context(), s.Get, web, Options{Limits: FollowLimits(c, 1)})

	recs := drain(r)
	assert.NoError(t, r.Err)
	assert.Len(t, recs, 10)
	assert.Equal(t, lineAt(web.Key, 900*time.Millisecond, 0), r.Stop)
}

// A forward read whose lower bound lies past every line of a second's last
// nanosecond sends sinceTime of the next second, so it neither reads that second
// again nor counts it toward the limit.
func TestAForwardReadFromASecondsEndStartsAtTheNext(t *testing.T) {
	t.Parallel()
	for name, since := range map[string]logline.Bound{
		"a moment":              {At: logline.MomentAt(at(3*time.Second - 1)), Inclusive: false},
		"a stream's lineCursor": {At: runOf(web).At(logstore.Pos{TS: at(3*time.Second - 1).UnixNano(), Ord: math.MaxInt}), Inclusive: false},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			s := newLogServer()
			loudLog(s)

			r := readForward(t.Context(), s.Get, web, Options{Since: since, Limits: limitsOf(smallCaps)})

			assert.Equal(t, []string{"after"}, messages(drain(r)))
			assert.NoError(t, r.Err)
			assert.Equal(t, "2026-10-08T10:00:03Z", s.Requests()[0].Get("sinceTime"))
		})
	}
}

// A body that fails under the read is the read's error.
func TestABodyThatFailsEndsTheRead(t *testing.T) {
	t.Parallel()
	get := func(context.Context, string, url.Values) (io.ReadCloser, error) {
		return io.NopCloser(io.MultiReader(strings.NewReader(logstest.Stamped(0, "a")+"\n"), iotest.ErrReader(errors.New("reset")))), nil
	}

	r := readForward(t.Context(), get, web, Options{})

	assert.Equal(t, []string{"a"}, messages(drain(r)))
	assert.EqualError(t, r.Err, "reset")
}

// backwardOpts is a backward read in windows of 10 lines, with no limits.
func backwardOpts() Options {
	return Options{Batch: 10}
}

// A backward read off the log's end reads one tailLines window, then windows of
// time: the first ends at the moment the tailLines window began in, which that
// window left out, and reads it whole.
func TestABackwardReadNeedsTwoWindows(t *testing.T) {
	t.Parallel()
	s := newLogServer()
	s.Add("prod/web/app", logstest.LineEvery(15)...)
	o := backwardOpts()
	o.Limits = limitsOf(DefaultQuota)

	r := readBackward(t.Context(), s.Get, web, o)

	assert.Equal(t, []string{"9|", "3|", "1|", "2|"}, shape(r))
	require.NoError(t, r.Err)
	assert.False(t, r.Short)
	assert.True(t, r.AtBeginning)
	assert.Equal(t, []string{"10", "", "", ""}, logstest.Tails(s))
	assert.Equal(t, []string{"", "2026-10-08T10:00:03Z", "2026-10-08T10:00:02Z", "2026-10-08T10:00:00Z"}, sinces(s))
	assert.Equal(t, "100", s.Requests()[0].Get("limitBytes"))
}

// After the tailLines window the read goes on in windows of time, each sized to
// hold at most the window cap: under limits scaled down 64 times, a sparse read
// over a 250 KiB log reaches its first line in four windows.
func TestTheTailLinesWindowIsFollowedByTimeWindows(t *testing.T) {
	t.Parallel()
	s := newLogServer()
	for i := range 2500 {
		s.Add("prod/web/app", logstest.Stamped(time.Duration(i)*10*time.Millisecond, strings.Repeat("x", 76)))
	}
	o := backwardOpts()
	o.Batch, o.Keep, o.Limits = 501, matching("never"), limitsOf(midCaps)

	r := readBackward(t.Context(), s.Get, web, o)

	// The tailLines window leaves the lines of the moment it began in to the next.
	assert.Equal(t, []string{"500|", "800|", "700|", "500|"}, shape(r))
	require.NoError(t, r.Err)
	assert.True(t, r.AtBeginning)
	assert.Equal(t, []string{"501", "", "", ""}, logstest.Tails(s))
}

// The tailLines window is read whole, whatever it holds: a runtime that does not
// split long lines serves whole lines for the parts asked for, so the window reads
// past the allowance, and no second stops it.
func TestTheTailLinesWindowIsReadWhole(t *testing.T) {
	t.Parallel()
	s := newLogServer()
	s.Part = 0
	for i := range 100 {
		s.Add("prod/web/app", logstest.Stamped(time.Duration(i)*time.Millisecond, strings.Repeat("x", 256)))
	}
	c := smallCaps
	c.Stream = 1 << 10
	o := backwardOpts()
	o.Batch, o.Limits = 501, limitsOf(c)
	o.More = func(context.Context) bool { return false }

	r := readBackward(t.Context(), s.Get, web, o)

	// What the first line's read left of the allowance, in parts: 57 lines, the
	// first left to the next window.
	assert.Equal(t, []string{"56|"}, shape(r))
	require.NoError(t, r.Err)
	assert.Equal(t, []string{"57"}, logstest.Tails(s))
	assert.Greater(t, o.Limits.Spent, c.Stream)
}

// A backward read hands over each window oldest first and its end after its last
// line, so its reader can write the window as it arrives. The read of the first moment
// is a window of its own, older than the window that found it.
func TestABackwardReadEndsEachWindow(t *testing.T) {
	t.Parallel()
	s := newLogServer()
	s.Add("prod/web/app", logstest.LineEvery(15)...)

	r := readBackward(t.Context(), s.Get, web, backwardOpts())

	assert.Equal(t, []string{"6", "7", "8", "9", "10", "11", "12", "13", "14", "|", "3", "4", "5", "|", "2", "|", "0", "1", "|"}, windowsOf(r))
	require.NoError(t, r.Err)
}

// A backward read under until ends each of its windows of time too.
func TestABackwardReadUnderUntilEndsEachWindow(t *testing.T) {
	t.Parallel()
	s := newLogServer()
	s.Add("prod/web/app", logstest.LineEvery(10)...)
	o := backwardOpts()
	o.Batch, o.Until, o.Near = 2, to(5*time.Second), &Rate{Kept: 1, Bytes: 30}

	r := readBackward(t.Context(), s.Get, web, o)

	assert.Equal(t, []string{"2", "3", "4", "5", "|", "0", "1", "|"}, windowsOf(r))
	require.NoError(t, r.Err)
}

// A backward window's lines go out as its GET reads them, not once it has read them
// all: a window's first line arrives while the rest of its body is still to come.
func TestABackwardWindowStreamsItsLines(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		Lines int
		Until logline.Bound
		first string
		rest  []string
	}{
		"windows of time": {Lines: 10, Until: to(10 * time.Second), first: "0", rest: []string{"1", "2", "3", "4", "5", "6", "7", "8", "9", "|"}},
		"tailLines":       {Lines: 15, first: "6", rest: []string{"7", "8", "9", "10", "11", "12", "13", "14", "|", "3", "4", "5", "|", "2", "|", "0", "1", "|"}},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			s := newLogServer()
			s.Add("prod/web/app", logstest.LineEvery(tc.Lines)...)
			gate := make(chan struct{})
			o := backwardOpts()
			o.Until = tc.Until

			r := readBackward(t.Context(), (&logstest.Pauser{N: 2, Gate: gate}).On(s.Get), web, o)

			assert.Equal(t, tc.first, testutil.Recv(t, r.Lines.Chan(), "the window's first line").Message)
			close(gate)
			assert.Equal(t, tc.rest, windowsOf(r))
			assert.NoError(t, r.Err)
		})
	}
}

// The fake kubelet serves a log as the README's kubelet points say the real one does,
// since every read here is tested against it.
func TestTheFakeKubeletServesAsTheKubelet(t *testing.T) {
	t.Parallel()
	read := func(s *logstest.Server, q url.Values) []string {
		body, err := s.Get(t.Context(), logPath(web.Key), q)
		require.NoError(t, err)
		defer body.Close()
		data, err := io.ReadAll(body)
		require.NoError(t, err)
		var out []string
		for line := range strings.Lines(string(data)) {
			out = append(out, strings.SplitN(strings.TrimSpace(line), " ", 2)[1])
		}
		return out
	}
	query := func(kv ...string) url.Values {
		q := url.Values{"container": {"app"}, "timestamps": {"true"}}
		for i := 0; i < len(kv); i += 2 {
			q.Set(kv[i], kv[i+1])
		}
		return q
	}

	t.Run("only the current file", func(t *testing.T) {
		s := newLogServer()
		s.Add("prod/web/app", logstest.LineEvery(5)...)
		s.Rotate("prod/web/app", 2*time.Second)
		assert.Equal(t, []string{"2", "3", "4"}, read(s, query()))
	})
	t.Run("sinceTime filters from the first line", func(t *testing.T) {
		s := newLogServer()
		s.Add("prod/web/app", logstest.LineEvery(5)...)
		assert.Equal(t, []string{"3", "4"}, read(s, query("sinceTime", "2026-10-08T10:00:03Z")))
	})
	t.Run("sinceTime is whole seconds", func(t *testing.T) {
		s := newLogServer()
		s.Add("prod/web/app", logstest.Stamped(time.Second+250*time.Millisecond, "a"), logstest.Stamped(time.Second+750*time.Millisecond, "b"))
		assert.Equal(t, []string{"a", "b"}, read(s, query("sinceTime", "2026-10-08T10:00:01.5Z")))
	})
	t.Run("tailLines counts a long line's parts", func(t *testing.T) {
		s := newLogServer()
		s.Part = 4
		s.Add("prod/web/app", logstest.Stamped(0, "a"), logstest.Stamped(time.Second, "longer"), logstest.Stamped(2*time.Second, "c"))
		assert.Equal(t, []string{"longer", "c"}, read(s, query("tailLines", "3")))
		assert.Equal(t, []string{"c"}, read(s, query("tailLines", "2")))
	})
}

// A backward read of a log whose long lines tailLines counts in parts reads every
// line: a window holds fewer lines than it asked for, and the next reaches further.
func TestABackwardReadOfSplitLinesReadsThemAll(t *testing.T) {
	t.Parallel()
	s := newLogServer()
	s.Part = 4
	var want []string
	for i := range 40 {
		msg := strconv.Itoa(i) + strings.Repeat("x", i%3*4)
		s.Add("prod/web/app", logstest.Stamped(time.Duration(i)*time.Second, msg))
		want = append([]string{msg}, want...)
	}

	r := readBackward(t.Context(), s.Get, web, backwardOpts())

	assert.Equal(t, want, messages(newestFirst(drain(r))))
	require.NoError(t, r.Err)
	assert.False(t, r.Short)
}

// A log that grows under a read of windows of time returns the lines it would have
// with no growth: each window ends where the last began, never at the log's end.
func TestABackwardReadUnderUntilIgnoresGrowth(t *testing.T) {
	t.Parallel()
	read := func(grow bool) []string {
		s := newLogServer()
		s.Add("prod/web/app", logstest.LineEvery(100)...)
		if grow {
			s.OnRequest = func(n int) {
				s.Add("prod/web/app", logstest.Stamped(time.Duration(100+n)*time.Second, "new"))
			}
		}
		o := backwardOpts()
		o.Until = to(50 * time.Second)
		r := readBackward(t.Context(), s.Get, web, o)
		recs := drain(r)
		require.NoError(t, r.Err)
		return messages(recs)
	}

	assert.Equal(t, read(false), read(true))
	assert.Len(t, read(true), 51)
}

// windowsOf is each line's message r hands over until it closes, and "|" for the end
// of each window.
func windowsOf(r *Read) []string {
	var out []string
	for it := range r.Lines.Chan() {
		if it.WindowEnd {
			out = append(out, "|")
		} else {
			out = append(out, it.Message)
		}
	}
	return out
}

// window's reach is rounded up, so the doublings land on the first line.
func TestABackwardReadUnderUntilReachesTheFirstLine(t *testing.T) {
	t.Parallel()
	s := newLogServer()
	s.Add("prod/web/app", logstest.Stamped(0, "first"), logstest.Stamped(5*time.Hour, "last"))
	o := backwardOpts()
	o.Until = to(5 * time.Hour)

	r := readBackward(t.Context(), s.Get, web, o)

	assert.Equal(t, []string{"last", "first"}, messages(newestFirst(drain(r))))
	require.NoError(t, r.Err)
	assert.False(t, r.Short)
}

// A backward read finds the first line from since's second, keeps since's line,
// and under until reads no window from before since.
func TestABackwardReadKeepsItsBounds(t *testing.T) {
	t.Parallel()
	s := newLogServer()
	s.Add("prod/web/app", logstest.LineEvery(30)...)
	o := backwardOpts()
	o.Since, o.Until = from(5*time.Second), to(22*time.Second)

	r := readBackward(t.Context(), s.Get, web, o)

	recs := newestFirst(drain(r))
	assert.Equal(t, "22", recs[0].Message)
	assert.Equal(t, "5", recs[len(recs)-1].Message)
	assert.Len(t, recs, 18)
	assert.Equal(t, "2026-10-08T10:00:05Z", s.Requests()[0].Get("sinceTime"))
	for _, q := range s.Requests() {
		since, err := time.Parse(time.RFC3339, q.Get("sinceTime"))
		require.NoError(t, err)
		assert.False(t, since.Before(o.Since.At.Timestamp))
	}
}

// A backward read that reaches the log's first line says the backend holds nothing
// older, under until too; one that stops at since or runs short does not.
func TestABackwardReadThatReachesTheFirstLineIsAtTheBeginning(t *testing.T) {
	t.Parallel()
	s := newLogServer()
	s.Add("prod/web/app", logstest.LineEvery(30)...)
	read := func(o Options) *Read {
		r := readBackward(t.Context(), s.Get, web, o)
		drain(r)
		return r
	}

	assert.True(t, read(backwardOpts()).AtBeginning, "to the first line")
	o := backwardOpts()
	o.Until = to(20 * time.Second)
	assert.True(t, read(o).AtBeginning, "under until")
	o = backwardOpts()
	o.Since = from(5 * time.Second)
	assert.False(t, read(o).AtBeginning, "stopped at since")
	o = backwardOpts()
	c := smallCaps
	c.Gets = 2
	o.Limits = limitsOf(c)
	r := read(o)
	assert.True(t, r.Short)
	assert.False(t, r.AtBeginning, "short")
}

// A backward read past since that reaches the log's first line, or finds the log
// empty or its first line past until, is at the beginning: the lines since names were rotated
// away. One whose first line is since's is not.
func TestABackwardReadThatReachesAFirstLinePastSinceIsAtTheBeginning(t *testing.T) {
	t.Parallel()
	s := newLogServer()
	s.Add("prod/web/app", logstest.LineEvery(30)[10:]...)
	read := func(o Options) *Read {
		r := readBackward(t.Context(), s.Get, web, o)
		drain(r)
		return r
	}

	o := backwardOpts()
	o.Since = from(5 * time.Second)
	assert.True(t, read(o).AtBeginning, "past since")
	o.Until = to(20 * time.Second)
	assert.True(t, read(o).AtBeginning, "past since, under until")
	o.Until = to(8 * time.Second)
	assert.True(t, read(o).AtBeginning, "past since and until")
	o = backwardOpts()
	o.Since = from(10 * time.Second)
	assert.False(t, read(o).AtBeginning, "the first line is since's")

	empty := backwardOpts()
	empty.Since = from(5 * time.Second)
	r := readBackward(t.Context(), claimOn(t, newLogServer()).Get, web, empty)
	drain(r)
	assert.True(t, r.AtBeginning, "empty from since")
}

// An empty log reads nothing, and so does one emptied under the read.
func TestABackwardReadOfAnEmptyLog(t *testing.T) {
	t.Parallel()
	t.Run("empty", func(t *testing.T) {
		r := readBackward(t.Context(), claimOn(t, newLogServer()).Get, web, backwardOpts())
		assert.Empty(t, drain(r))
		assert.NoError(t, r.Err)
	})
	t.Run("emptied", func(t *testing.T) {
		s := newLogServer()
		s.Add("prod/web/app", logstest.LineEvery(3)...)
		s.OnRequest = func(n int) {
			if n == 1 {
				s.Clear("prod/web/app")
			}
		}
		r := readBackward(t.Context(), s.Get, web, backwardOpts())
		assert.Empty(t, drain(r))
		assert.NoError(t, r.Err)
		assert.False(t, r.Short)
	})
}

// A log emptied under the read after its first window holds nothing older: the
// windows of time find no line, and the read ends at the backend's oldest.
func TestABackwardReadOfALogEmptiedAfterAWindowEndsAtItsBeginning(t *testing.T) {
	t.Parallel()
	s := newLogServer()
	s.Add("prod/web/app", logstest.LineEvery(15)...)
	s.OnRequest = func(n int) {
		if n == 2 {
			s.Clear("prod/web/app")
		}
	}

	r := readBackward(t.Context(), s.Get, web, backwardOpts())

	assert.Equal(t, []string{"14", "13", "12", "11", "10", "9", "8", "7", "6"}, messages(newestFirst(drain(r))))
	assert.NoError(t, r.Err)
	assert.False(t, r.Short)
	assert.True(t, r.AtBeginning)
}

// A window that begins below a sub-second since holds every line in range, so
// the read ends there rather than growing toward the second's first line.
func TestABackwardReadEndsAtTheWindowThatPassesSince(t *testing.T) {
	t.Parallel()
	s := newLogServer()
	for i := range 30 {
		s.Add("prod/web/app", logstest.Stamped(5*time.Second+time.Duration(i)*10*time.Millisecond, strconv.Itoa(i)))
	}
	o := backwardOpts()
	o.Since = from(5*time.Second + 150*time.Millisecond)

	r := readBackward(t.Context(), s.Get, web, o)

	recs := newestFirst(drain(r))
	require.NoError(t, r.Err)
	assert.Len(t, recs, 15)
	assert.Equal(t, "29", recs[0].Message)
	assert.Equal(t, "15", recs[14].Message)
	assert.False(t, r.Short)
	assert.Equal(t, []string{"10", ""}, logstest.Tails(s))
}

// The read that finds the first line takes no more than it asked for, though a
// server that ignores limitBytes sends the whole log.
func TestFindingTheFirstLineReadsItsLimit(t *testing.T) {
	t.Parallel()
	s := newLogServer()
	s.Add("prod/web/app", logstest.LineEvery(100)...)
	var read int
	get := func(ctx context.Context, path string, q url.Values) (io.ReadCloser, error) {
		first := q.Has("limitBytes")
		q.Del("limitBytes")
		body, err := s.Get(ctx, path, q)
		if err != nil || !first {
			return body, err
		}
		data, err := io.ReadAll(body)
		body.Close()
		if err != nil {
			return nil, err
		}
		return io.NopCloser(iotest.OneByteReader(&countingReader{strings.NewReader(string(data)), &read})), nil
	}

	r := readBackward(t.Context(), get, web, backwardOpts())

	drain(r)
	require.NoError(t, r.Err)
	assert.LessOrEqual(t, read, firstBytes)
}

// countingReader adds every byte it reads to n.
type countingReader struct {
	io.Reader
	n *int
}

func (c *countingReader) Read(p []byte) (int, error) {
	k, err := c.Reader.Read(p)
	*c.n += k
	return k, err
}

// A log that grows by a whole batch under the read is read in a larger window,
// and the lines that arrived are left for the next page.
func TestABackwardReadOutgrowsAGrowingLog(t *testing.T) {
	t.Parallel()
	s := newLogServer()
	s.Add("prod/web/app", logstest.LineEvery(25)...)
	s.OnRequest = func(n int) {
		if n == 2 {
			s.Add("prod/web/app", logstest.LineEvery(35)[25:]...)
		}
	}

	r := readBackward(t.Context(), s.Get, web, backwardOpts())

	recs := newestFirst(drain(r))
	require.NoError(t, r.Err)
	assert.Len(t, recs, 25)
	assert.Equal(t, "24", recs[0].Message)
	assert.Equal(t, "0", recs[24].Message)
	// Only the first window asks for tailLines; the windows of time end where it began.
	assert.Equal(t, "10", strings.Join(logstest.Tails(s), ""))
}

// A backward read is cut at the end of a window once it has spent its bytes or
// sent its GETs, or its next window would be sized under the least, at the oldest
// line of that window. Each log is 200 lines a second apart, read back from the
// last.
func TestABackwardReadIsCutAtItsLimits(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		Lines   []string
		Limits  *Limits
		Batch   int64
		Grep    string
		Near    Rate
		windows int
		Stop    logline.Cursor
	}{
		// Lines of 2 KiB under limits scaled down 64 times: four windows of about
		// 64 KiB reach 256 KiB.
		"bytes": {
			Lines: logstest.WideLines(200, 2<<10), Limits: limitsOf(midCaps), Batch: 10, Grep: "never", Near: Rate{Bytes: 2071},
			windows: 4, Stop: lineAt(web.Key, 77*time.Second, 0),
		},
		// Windows of two seconds, each holding the one line the read needs.
		"GETs": {
			Lines: halfPast(200), Limits: limitsOf(DefaultQuota), Batch: 1, Near: Rate{Kept: 1, Bytes: 205},
			windows: 15, Stop: lineAt(web.Key, 169*time.Second+500*time.Millisecond, 0),
		},
		// One of a hundred streams, whose first window leaves the pool under four
		// times the least window.
		"the least window": {
			Lines: logstest.WideLines(200, 180), Limits: QueryLimits(smallCaps, 100)[0], Batch: 10, Grep: "never", Near: Rate{Bytes: 202},
			windows: 1, Stop: lineAt(web.Key, 198*time.Second, 0),
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			s := newLogServer()
			s.Add("prod/web/app", tc.Lines...)
			o := backwardOpts()
			o.Batch, o.Limits, o.Until, o.Near = tc.Batch, tc.Limits, to(199*time.Second), &tc.Near
			if tc.Grep != "" {
				o.Keep = matching(tc.Grep)
			}

			r := readBackward(t.Context(), s.Get, web, o)

			got := shape(r)
			assert.NoError(t, r.Err)
			assert.Len(t, got, tc.windows, "windows: %v", got)
			assert.True(t, r.Short)
			assert.Equal(t, tc.Stop, r.Stop)
		})
	}
}

// halfPast is n lines a second apart from half past 10:00:00, saying their index.
func halfPast(n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = logstest.Stamped(time.Duration(i)*time.Second+500*time.Millisecond, strconv.Itoa(i))
	}
	return out
}

// A read cut after an empty window is cut at that window's start: it read every
// line from there to the point and found none.
func TestAnEmptyWindowsEdgeIsItsStart(t *testing.T) {
	t.Parallel()
	s := newLogServer()
	s.Add("prod/web/app", logstest.LineEvery(10)...)
	c := smallCaps
	c.Gets = 3
	o := backwardOpts()
	o.Until, o.Limits = to(1000*time.Second), limitsOf(c)

	r := readBackward(t.Context(), s.Get, web, o)

	assert.Equal(t, []string{"0|", "0|"}, shape(r))
	assert.NoError(t, r.Err)
	assert.True(t, r.Short)
	assert.Equal(t, logline.MomentAt(at(910*time.Second)), r.Stop)
}

// A backward read under until reads windows of time ending there, each from its
// start, so it reaches until however much log follows it.
func TestABackwardReadUnderUntilReachesIt(t *testing.T) {
	t.Parallel()
	s := newLogServer()
	s.Add("prod/web/app", logstest.LineEvery(1000)...)
	o := backwardOpts()
	o.Until = to(100 * time.Second)

	r := readBackward(t.Context(), s.Get, web, o)

	recs := newestFirst(drain(r))
	require.NoError(t, r.Err)
	assert.Len(t, recs, 101)
	assert.Equal(t, "100", recs[0].Message)
	assert.Equal(t, "0", recs[100].Message)
	assert.False(t, r.Short)
	assert.Equal(t, "2026-10-08T10:01:30Z", sinces(s)[0])
}

// A backward read under until keeps since to the nanosecond, though the log
// endpoint finds its first line from since's second.
func TestABackwardReadUnderUntilKeepsSince(t *testing.T) {
	t.Parallel()
	s := newLogServer()
	s.Add("prod/web/app", logstest.Stamped(time.Second, "before"), logstest.Stamped(time.Second+300*time.Millisecond, "after"))
	o := backwardOpts()
	o.Since, o.Until = from(time.Second+200*time.Millisecond), to(time.Minute)

	r := readBackward(t.Context(), s.Get, web, o)

	assert.Equal(t, []string{"after"}, messages(newestFirst(drain(r))))
	assert.NoError(t, r.Err)
}

// Lines sharing a window's starting moment are held back for the next window,
// which reads the whole moment, so none is lost at the seam.
func TestABackwardWindowKeepsAMomentWhole(t *testing.T) {
	t.Parallel()
	s := newLogServer()
	lines := logstest.LineEvery(20)
	lines[9] = logstest.Stamped(10*time.Second, "9'")
	s.Add("prod/web/app", lines...)

	r := readBackward(t.Context(), s.Get, web, backwardOpts())

	recs := newestFirst(drain(r))
	require.NoError(t, r.Err)
	assert.Len(t, recs, 20)
	assert.Equal(t, []string{"11", "10", "9'", "8"}, messages(recs[8:12]))
}

// A first moment longer than a window is read whole: a window starting in it
// cannot tell whether it began at its top.
func TestABackwardReadKeepsTheFirstMomentWhole(t *testing.T) {
	t.Parallel()
	s := newLogServer()
	for i := range 25 {
		s.Add("prod/web/app", logstest.Stamped(0, strconv.Itoa(i)))
	}
	s.Add("prod/web/app", logstest.Stamped(time.Second, "last"))

	r := readBackward(t.Context(), s.Get, web, backwardOpts())

	recs := newestFirst(drain(r))
	require.NoError(t, r.Err)
	assert.Len(t, recs, 26)
	assert.Equal(t, "last", recs[0].Message)
	assert.False(t, r.Short)
}

// A backward read after a line of a moment longer than its window reads that
// moment whole, since the window numbers its lines from its own top: with 100
// lines at one moment, after the 91st, it sends the nine after it.
func TestABackwardReadAfterALineReadsItsMomentWhole(t *testing.T) {
	t.Parallel()
	for name, tail := range map[string][]string{
		"alone":             nil,
		"before later ones": {logstest.Stamped(time.Second, "later")},
	} {
		t.Run(name, func(t *testing.T) {
			s := newLogServer()
			for i := range 100 {
				s.Add("prod/web/app", logstest.Stamped(0, strconv.Itoa(i)))
			}
			s.Add("prod/web/app", tail...)
			o := backwardOpts()
			o.Since = logline.Bound{At: lineAt(web.Key, 0, 90), Inclusive: false}

			r := readBackward(t.Context(), s.Get, web, o)

			recs := newestFirst(drain(r))
			require.NoError(t, r.Err)
			want := []string{"99", "98", "97", "96", "95", "94", "93", "92", "91"}
			if tail != nil {
				want = append([]string{"later"}, want...)
			}
			assert.Equal(t, want, messages(recs))
			assert.Equal(t, 91, recs[len(recs)-1].Ordinal)
			assert.False(t, r.Short)
		})
	}
}

// A backward read with a sub-second since reads the first moment from since,
// though the log endpoint finds that moment from since's second.
func TestABackwardReadKeepsSinceInTheFirstMoment(t *testing.T) {
	t.Parallel()
	s := newLogServer()
	s.Add("prod/web/app", logstest.Stamped(100*time.Millisecond, "before"), logstest.Stamped(500*time.Millisecond, "after"))
	o := backwardOpts()
	o.Since = from(400 * time.Millisecond)

	r := readBackward(t.Context(), s.Get, web, o)

	assert.Equal(t, []string{"after"}, messages(newestFirst(drain(r))))
	assert.NoError(t, r.Err)
}

// A backward read the API server refuses ends with its status, on its first read
// or on a later window.
func TestARefusedBackwardReadEndsWithItsStatus(t *testing.T) {
	t.Parallel()
	for name, on := range map[string]int{"finding the first line": 0, "a window": 1} {
		t.Run(name, func(t *testing.T) {
			s := newLogServer()
			s.Add("prod/web/app", logstest.LineEvery(3)...)
			s.OnRequest = func(n int) {
				if n == on {
					s.Refuse["prod/web/app"] = logstest.Denial{Code: 404, Message: "not found"}
				}
			}

			r := readBackward(t.Context(), s.Get, web, backwardOpts())

			assert.Empty(t, drain(r))
			assert.EqualError(t, r.Err, "not found")
		})
	}
}

// sinces is the sinceTime of each request after the first, which finds the first line.
func sinces(s *logstest.Server) []string {
	var out []string
	for _, q := range s.Requests()[1:] {
		out = append(out, q.Get("sinceTime"))
	}
	return out
}

// A backward read under until sizes its first window from the rate next to its point,
// ten seconds with none, and starts it at the top of a second.
func TestABackwardReadUnderUntilSizesItsFirstWindow(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		Near  *Rate
		Since string
	}{
		"from the rate near it": {&Rate{Kept: 1, Bytes: 30}, "2026-10-08T10:00:35Z"},
		"ten seconds":           {nil, "2026-10-08T10:00:40Z"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			s := newLogServer()
			s.Add("prod/web/app", logstest.LineEvery(100)...)
			o := backwardOpts()
			o.Until, o.Near = logline.Bound{At: logline.MomentAt(at(50*time.Second + 300*time.Millisecond)), Inclusive: false}, tc.Near
			o.More = func(context.Context) bool { return false }

			r := readBackward(t.Context(), s.Get, web, o)

			drain(r)
			require.NoError(t, r.Err)
			assert.Equal(t, []string{tc.Since}, sinces(s))
		})
	}
}

// A window that leaves the read short is followed by an older one sized from its own
// rate, and an empty one by one eight times longer, so a day of silence costs a few
// GETs.
func TestABackwardReadWidensPastAQuietStretch(t *testing.T) {
	t.Parallel()
	s := newLogServer()
	day := 24 * time.Hour
	for i := range 21 {
		s.Add("prod/web/app", logstest.Stamped(time.Duration(i)*time.Second, "old"))
	}
	for i := range 21 {
		s.Add("prod/web/app", logstest.Stamped(day+time.Duration(i)*time.Second, "new"))
	}
	o := backwardOpts()
	o.Batch, o.Until, o.Near = 30, to(day+20*time.Second), &Rate{Kept: 1, Bytes: 30}

	r := readBackward(t.Context(), s.Get, web, o)

	assert.Len(t, drain(r), 42)
	require.NoError(t, r.Err)
	assert.False(t, r.Short)
	assert.True(t, r.AtBeginning)
	assert.Equal(t, []string{
		"2026-10-09T09:59:35Z", "2026-10-09T09:59:06Z", "2026-10-09T09:55:14Z",
		"2026-10-09T09:24:18Z", "2026-10-09T05:16:50Z", "2026-10-08T10:00:00Z",
	}, sinces(s))
}

// shape is how many lines each window r hands over held, with "|" after a window it
// ended and "/" after one it stopped part way.
func shape(r *Read) []string {
	var out []string
	n := 0
	for it := range r.Lines.Chan() {
		switch {
		case it.WindowEnd:
			out, n = append(out, strconv.Itoa(n)+"|"), 0
		case it.WindowCut:
			out, n = append(out, strconv.Itoa(n)+"/"), 0
		default:
			n++
		}
	}
	return out
}

// filling makes o's read stop once the windows read whole hold need lines, as a
// fill does, answering more at each window's end, and is the shape of what the read
// sent. A read that ends without asking leaves one answer unread.
func filling(o *Options, need int) func(r *Read) []string {
	answers := make(chan bool, 1)
	o.More = func(ctx context.Context) bool {
		select {
		case ok := <-answers:
			return ok
		case <-ctx.Done():
			return false
		}
	}
	return func(r *Read) []string {
		var out []string
		n, held := 0, 0
		for it := range r.Lines.Chan() {
			switch {
			case it.WindowEnd:
				out, held, n = append(out, strconv.Itoa(n)+"|"), held+n, 0
				answers <- held < need
			case it.WindowCut:
				out, n = append(out, strconv.Itoa(n)+"/"), 0
			default:
				n++
			}
		}
		return out
	}
}

// burst is a log of a line every 100ms for 65s, then 5,000 lines a second for 10s,
// and the point just after it.
func burst(s *logstest.Server) logline.Bound {
	for i := range 650 {
		s.Add("prod/web/app", logstest.Stamped(time.Duration(i)*100*time.Millisecond, "quiet"))
	}
	for i := range 50000 {
		s.Add("prod/web/app", logstest.Stamped(65*time.Second+time.Duration(i)*200*time.Microsecond, "loud"))
	}
	return logline.Bound{At: logline.MomentAt(at(75 * time.Second)), Inclusive: false}
}

// A window guessed far too long for a burst just before the point is stopped once it
// expects four times what the read needs, and replaced from the second after its
// stop. That window is stopped in its first second, which shows that second dense,
// and is halved down to the one-second window at the point, which is read whole.
func TestABusyWindowIsReplacedFromItsStop(t *testing.T) {
	t.Parallel()
	s := newLogServer()
	o := backwardOpts()
	o.Batch, o.Until, o.Near = 501, burst(s), &Rate{Kept: 10, Bytes: 400}
	o.More = func(context.Context) bool { return false }

	r := readBackward(t.Context(), s.Get, web, o)

	assert.Equal(t, []string{"766/", "501/", "501/", "501/", "501/", "5000|"}, shape(r))
	require.NoError(t, r.Err)
	assert.Equal(t, []string{
		"2026-10-08T10:00:00Z", "2026-10-08T10:01:06Z", "2026-10-08T10:01:10Z",
		"2026-10-08T10:01:12Z", "2026-10-08T10:01:13Z", "2026-10-08T10:01:14Z",
	}, sinces(s))
}

// A stopped window is replaced the same way under limits: the replacement is sized
// from what the pool holds before its own reservation, so a pool the stopped
// window's reservation drained does not make every replacement the one-second
// window.
func TestABusyWindowIsReplacedFromItsStopUnderLimits(t *testing.T) {
	t.Parallel()
	s := newLogServer()
	o := backwardOpts()
	o.Batch, o.Until, o.Near, o.Limits = 501, burst(s), &Rate{Kept: 10, Bytes: 400}, QueryLimits(DefaultQuota, 4)[0]
	o.More = func(context.Context) bool { return false }

	r := readBackward(t.Context(), s.Get, web, o)

	assert.Equal(t, []string{"766/", "501/", "501/", "501/", "501/", "5000|"}, shape(r))
	require.NoError(t, r.Err)
	assert.Equal(t, []string{
		"2026-10-08T10:00:00Z", "2026-10-08T10:01:06Z", "2026-10-08T10:01:10Z",
		"2026-10-08T10:01:12Z", "2026-10-08T10:01:13Z", "2026-10-08T10:01:14Z",
	}, sinces(s))
}

// A stopped window is not the read's edge: a read that fails after the window that
// replaced it stops at that window's oldest line, not the island's.
func TestAStoppedWindowIsNotTheEdge(t *testing.T) {
	t.Parallel()
	s := newLogServer()
	o := backwardOpts()
	o.Batch, o.Until, o.Near = 501, burst(s), &Rate{Kept: 10, Bytes: 400}
	s.OnRequest = func(n int) {
		if n == 7 {
			s.SetRefusal("prod/web/app", &logstest.Denial{Code: 503, Message: "unavailable"})
		}
	}

	r := readBackward(t.Context(), s.Get, web, o)

	assert.Equal(t, []string{"766/", "501/", "501/", "501/", "501/", "5000|"}, shape(r))
	assert.True(t, r.Short)
	assert.Equal(t, lineAt(web.Key, 74*time.Second, 0), r.Stop)
}

// A window sized to a reach of one second is the one-second window: the second its
// end lies in, or the one before when its end is a second's top.
func TestAOneSecondWindowIsOneSecondOfTheClock(t *testing.T) {
	t.Parallel()
	assert.Equal(t, at(50*time.Second), startOf(at(50*time.Second+300*time.Millisecond), time.Second, at(0)))
	assert.Equal(t, at(49*time.Second), startOf(at(50*time.Second), time.Second, at(0)))
	assert.Equal(t, at(50*time.Second+100*time.Millisecond), startOf(at(50*time.Second+300*time.Millisecond), time.Second, at(50*time.Second+100*time.Millisecond)))
}

// A dense second far from the point stops the window that reaches it there; the
// window from the second after it completes, the window that ends at the second is
// stopped in its last second, and the one-second window reads it whole. No window
// starts at a second it already read.
func TestADenseSecondFarFromThePoint(t *testing.T) {
	t.Parallel()
	s := newLogServer()
	for i := range 60 {
		s.Add("prod/web/app", logstest.Stamped(time.Duration(i)*time.Second, "quiet"))
	}
	for i := range 5000 {
		s.Add("prod/web/app", logstest.Stamped(60*time.Second+time.Duration(i)*200*time.Microsecond, "dense"))
	}
	for i := range 30 {
		s.Add("prod/web/app", logstest.Stamped(time.Duration(61+i)*time.Second, "quiet"))
	}
	o := backwardOpts()
	o.Batch, o.Until, o.Near = 501, after(91*time.Second), &Rate{Kept: 1, Bytes: 30}
	o.Limits = limitsOf(DefaultQuota)
	shape := filling(&o, 501)

	r := readBackward(t.Context(), s.Get, web, o)

	got := shape(r)
	require.NoError(t, r.Err)
	assert.False(t, r.Short)
	assert.Equal(t, []string{"501/", "30|", "1151/", "5000|"}, got)
	assert.Equal(t, []string{"2026-10-08T10:00:00Z", "2026-10-08T10:01:01Z", "2026-10-08T10:00:00Z", "2026-10-08T10:01:00Z"}, sinces(s))
}

// A dense stretch far from the point is found by halving: each window stopped in its
// first second is halved toward the point, and the newest such second bounds the
// windows widened after a quiet one, so the read reaches the stretch's newest second
// as the one-second window. Here five seconds of 2,000 lines, a line every ten
// seconds from a minute later, and the point ten minutes on.
func TestADenseStretchFarFromThePoint(t *testing.T) {
	t.Parallel()
	s := newLogServer()
	for sec := range 5 {
		for i := range 2000 {
			s.Add("prod/web/app", logstest.Stamped(time.Duration(sec)*time.Second+time.Duration(i)*500*time.Microsecond, "dense"))
		}
	}
	for i := 6; i < 60; i++ {
		s.Add("prod/web/app", logstest.Stamped(time.Duration(i)*10*time.Second, "quiet"))
	}
	o := backwardOpts()
	o.Batch, o.Until, o.Near = 501, after(10*time.Minute), &Rate{Kept: 0.1, Bytes: 3}
	shape := filling(&o, 501)

	r := readBackward(t.Context(), s.Get, web, o)

	got := shape(r)
	require.NoError(t, r.Err)
	assert.Equal(t, "2000|", got[len(got)-1], "the stretch's newest second whole: %v", got)
	assert.Equal(t, "2026-10-08T10:00:04Z", sinces(s)[len(got)-1])
	assert.LessOrEqual(t, len(got), 2*10, "about two GETs a halving of the ten minutes: %v", got)
}

// A chain of stops ends: once the stream has sent its GETs, a stopped window is
// replaced by the one-second window, which completes, and the read is cut at its
// oldest line. A rate near nothing sizes every window to the whole log, and each is
// stopped at its first line.
func TestAChainOfStopsEnds(t *testing.T) {
	t.Parallel()
	s := newLogServer()
	s.Add("prod/web/app", logstest.LineEvery(60)...)
	c := DefaultQuota
	c.Gets = 6
	o := backwardOpts()
	o.Batch, o.Until, o.Near, o.Limits = 1, to(59*time.Second), &Rate{Kept: 1e-6, Bytes: 1e-6}, limitsOf(c)

	r := readBackward(t.Context(), s.Get, web, o)

	assert.Equal(t, []string{"1/", "1/", "1/", "1/", "1/", "2|"}, shape(r))
	require.NoError(t, r.Err)
	assert.True(t, r.Short)
	assert.Equal(t, lineAt(web.Key, 58*time.Second, 0), r.Stop)
	assert.Len(t, s.Requests(), 7)
}

// A window counts each line as the body carried it, before the message is cleaned
// and cut: a log of 200 KiB lines, which the file keeps as 16 KiB, stops a window
// sized for 4 MiB.
func TestAWindowCountsTheBytesItsBodyCarried(t *testing.T) {
	t.Parallel()
	s := newLogServer()
	long := strings.Repeat("x", 200<<10)
	for i := range 100 {
		s.Add("prod/web/app", logstest.Stamped(time.Duration(i)*time.Second, long))
	}
	o := backwardOpts()
	o.Batch, o.Until, o.Keep = 100, to(99*time.Second), matching("never")
	o.Limits, o.Near = limitsOf(DefaultQuota), &Rate{Bytes: 1000}
	o.More = func(context.Context) bool { return false }

	r := readBackward(t.Context(), s.Get, web, o)

	got := shape(r)
	require.NoError(t, r.Err)
	assert.True(t, strings.HasSuffix(got[0], "/"), "stopped: %v", got)
	assert.True(t, strings.HasSuffix(got[len(got)-1], "|"), "completed: %v", got)
}

// A window with no line widens eight times, up to as far as a duration goes.
func TestAnEmptyWindowWidensEightTimes(t *testing.T) {
	t.Parallel()
	r := &reader{}
	assert.Equal(t, 80*time.Second, r.widen(Sample{}, 10*time.Second, 0, byteTarget))
	assert.Equal(t, maxReach, r.widen(Sample{}, maxReach/4, 0, byteTarget))
}

// A read cancelled as it stops a window ends clean, with no end sent for the window.
func TestAReadCancelledAtAStoppedWindowEndsClean(t *testing.T) {
	t.Parallel()
	s := newLogServer()
	o := backwardOpts()
	o.Batch, o.Until, o.Near = 501, burst(s), &Rate{Kept: 10, Bytes: 400}
	ctx, cancel := context.WithCancel(t.Context())
	r := readBackward(ctx, s.Get, web, o)

	for range 766 {
		assert.False(t, testutil.Recv(t, r.Lines.Chan(), "a line of the stopped window").WindowCut)
	}
	cancel()

	testutil.WaitClosed(t, r.Lines.Chan(), "the read to end")
	assert.NoError(t, r.Err)
	assert.False(t, r.Short)
}

// loudLog is a line at 10:00:01, a loud second at 10:00:02, and a line at 10:00:03.
func loudLog(s *logstest.Server) {
	s.Add("prod/web/app", logstest.Stamped(time.Second, "before"))
	for i := range 12 {
		s.Add("prod/web/app", logstest.Stamped(2*time.Second+time.Duration(i)*time.Millisecond, logstest.LoudLine))
	}
	s.Add("prod/web/app", logstest.Stamped(3*time.Second, "after"))
}

// A forward read stops at a second holding more of the log than the limit: it takes
// the lines of that second the limit leaves, and its stop is the second's last
// nanosecond.
func TestAForwardReadStopsAtALoudSecond(t *testing.T) {
	t.Parallel()
	s := newLogServer()
	loudLog(s)

	r := readForward(t.Context(), s.Get, web, Options{Limits: limitsOf(smallCaps)})

	recs := drain(r)
	var loud LoudSecond
	require.ErrorAs(t, r.Err, &loud)
	assert.Equal(t, at(2*time.Second), loud.At)
	assert.Equal(t, FailLimit, FailureKind(r.Err))
	assert.True(t, r.Short)
	assert.Equal(t, logline.MomentAt(at(3*time.Second-1)), r.Stop)
	assert.Equal(t, "before", recs[0].Message)
	assert.Len(t, recs, 1+8, "a KiB holds eight of the loud lines")
}

// A long line is counted as it is read, so a line past the limit stops the read
// before it ends.
func TestALongLineIsALoudSecond(t *testing.T) {
	t.Parallel()
	s := newLogServer()
	s.Add("prod/web/app", logstest.Stamped(time.Second, "before"), logstest.Stamped(2*time.Second, strings.Repeat("x", 64<<10)), logstest.Stamped(3*time.Second, "after"))
	l := limitsOf(smallCaps)

	r := readForward(t.Context(), s.Get, web, Options{Limits: l})

	assert.Equal(t, []string{"before"}, messages(drain(r)))
	assert.Equal(t, FailLimit, FailureKind(r.Err))
	assert.Less(t, l.Spent, int64(64<<10))
}

// A second is counted on the clock, so two seconds each just under the limit are
// not loud.
func TestTwoHalfLoudSecondsAreNotLoud(t *testing.T) {
	t.Parallel()
	s := newLogServer()
	for i := range 16 {
		s.Add("prod/web/app", logstest.Stamped(time.Second+time.Duration(i/8)*time.Second+time.Duration(i%8)*time.Millisecond, logstest.LoudLine))
	}

	r := readForward(t.Context(), s.Get, web, Options{Limits: limitsOf(smallCaps)})

	assert.Len(t, drain(r), 16)
	assert.NoError(t, r.Err)
}

// The stop's words name the second and say what is left out.
func TestALoudSecondSaysWhatIsLeftOut(t *testing.T) {
	t.Parallel()
	err := LoudAt(at(2*time.Second + 300*time.Millisecond))

	assert.Equal(t, at(2*time.Second), err.At)
	assert.EqualError(t, err, "more than 1 MiB of log in the second at 2026-10-08T10:00:02Z, more than Kubetail reads through the Kubernetes API; lines of that second are left out")
}

// A loud second far from the point stops the window that holds it, which is replaced
// as any stopped window is, until the one-second window reaches it and the stream
// stops there: the read stops at the second's top, within four window GETs.
func TestALoudSecondFarFromThePoint(t *testing.T) {
	t.Parallel()
	s := newLogServer()
	for i := range 50 {
		s.Add("prod/web/app", logstest.Stamped(time.Duration(i)*time.Second, "quiet"))
	}
	for i := range 12 {
		s.Add("prod/web/app", logstest.Stamped(50*time.Second+time.Duration(i)*time.Millisecond, logstest.LoudLine))
	}
	for i := range 30 {
		s.Add("prod/web/app", logstest.Stamped(time.Duration(51+i)*time.Second, "quiet"))
	}
	o := backwardOpts()
	o.Batch, o.Until, o.Near = 501, after(81*time.Second), &Rate{Kept: 1, Bytes: 30}
	o.Limits = limitsOf(smallCaps)
	shape := filling(&o, 501)

	r := readBackward(t.Context(), s.Get, web, o)

	// The window from the start is stopped at the loud second, the one from the
	// second after completes, and the widened window is stopped there again. The
	// one-second window that stops the stream sends no end: its lines are an island.
	got := shape(r)
	assert.Len(t, got, 3, "windows: %v", got)
	assert.Equal(t, []string{"2026-10-08T10:00:00Z", "2026-10-08T10:00:51Z", "2026-10-08T10:00:00Z", "2026-10-08T10:00:50Z"}, sinces(s))
	var loud LoudSecond
	require.ErrorAs(t, r.Err, &loud)
	assert.Equal(t, at(50*time.Second), loud.At)
	assert.True(t, r.Short)
	assert.Equal(t, logline.MomentAt(at(50*time.Second)), r.Stop)
}

// A first moment too loud to read whole stops the stream at its second's top.
func TestALoudFirstMomentStopsTheStream(t *testing.T) {
	t.Parallel()
	s := newLogServer()
	for range 12 {
		s.Add("prod/web/app", logstest.Stamped(time.Second, logstest.LoudLine))
	}
	o := backwardOpts()
	o.Limits = limitsOf(smallCaps)

	r := readBackward(t.Context(), s.Get, web, o)

	drain(r)
	assert.Equal(t, FailLimit, FailureKind(r.Err))
	assert.True(t, r.Short)
	assert.Equal(t, logline.MomentAt(at(time.Second)), r.Stop)
}

// A loud second just past a backward window's end is not the window's: the GET ends
// at its first line, uncounted.
func TestALoudSecondPastTheEndIsNotRead(t *testing.T) {
	t.Parallel()
	s := newLogServer()
	s.Add("prod/web/app", logstest.Stamped(9*time.Second, "below"), logstest.Stamped(10*time.Second, strings.Repeat("x", 2<<10)))
	o := backwardOpts()
	o.Until, o.Limits = logline.Bound{At: lineAt(web.Key, 10*time.Second, 0), Inclusive: false}, limitsOf(smallCaps)

	r := readBackward(t.Context(), s.Get, web, o)

	assert.Equal(t, []string{"below"}, messages(drain(r)))
	assert.NoError(t, r.Err)
}
