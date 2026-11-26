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
	"bufio"
	"bytes"
	"context"
	"errors"
	"io"
	"math"
	"net/url"
	"strconv"
	"time"

	"github.com/amorey/gochan/spsc"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/logs/logline"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/logs/refusal"
)

// maxLine is the most of one line read off the wire; the rest of it is skipped.
// logline.KeepColor cuts what is kept to maxMessage.
const maxLine = 64 << 10

// minBatch is the fewest lines a backward window asks for: a smaller one spends a
// GET on nearly nothing.
const minBatch = 10

// firstBytes is how much of a log the read that finds its first line asks for: more
// than the longest timestamp and the space after it.
const firstBytes = 100

// timestampSpan is the longest RFC 3339 timestamp with nanoseconds, plus the space
// after it.
const timestampSpan = 36

// errPastUntil ends a GET at a line past its upper bound before the line is read
// whole.
var errPastUntil = errors.New("logs: past the read's upper bound")

// errShort is a read a limit cut before the end of its range.
var errShort = errors.New("logs: the read stopped short")

// counted is a body whose bytes are charged to a stream's limits as they arrive.
type counted struct {
	io.Reader
	Limits *Limits
}

func (c counted) Read(p []byte) (int, error) {
	n, err := c.Reader.Read(p)
	c.Limits.charge(int64(n))
	return n, err
}

// getter is one GET on the claimed server: a claim's get, or a test's.
type Getter func(ctx context.Context, path string, query url.Values) (io.ReadCloser, error)

// Options is what one container's read asks for.
type Options struct {
	// since and until bound the lines, either zero for no bound.
	Since logline.Bound
	Until logline.Bound
	// Follow keeps a forward read reading as the log grows. It reads from since or the
	// log's start, never from its end: a GET from the end numbers its first moment from
	// wherever it joined it, which a read from that moment's second would number
	// differently.
	Follow bool
	// A fetch's bounds: the stream's limits, and for a backward read the kept lines
	// it reads toward.
	Limits *Limits
	Batch  int64
	// Keep is the page's test of a line, which a backward read counts its windows'
	// kept lines by, nil to keep every line; Near is the rate next to a backward
	// read's point, nil with none.
	Keep func(logline.Record) bool
	Near *Rate
	// onOldest, when set, is told the moment of a GET's first line, in range or not,
	// before any line of it goes out: a tail decides from it whether its first read
	// returned its start's moment whole, and reads on.
	OnOldest func(time.Time)
	// more, when set, is asked at each window's end before a backward read sends its
	// next GET, with the read's context, and ends the read clean when false.
	More func(context.Context) bool
}

// keep is whether o counts rec as kept: every line with no Keep.
func (o Options) keep(rec logline.Record) bool { return o.Keep == nil || o.Keep(rec) }

// Item is what a read hands over: a line, or with windowEnd, the end of a window,
// after its last line, or with windowCut, the end of a window stopped part way, which
// is an island: the lines between it and the point were never read. A backward read
// is a run of windows; a forward read is one, which its close ends, so it sends none.
type Item struct {
	logline.Record
	WindowEnd bool
	WindowCut bool
}

// Read is one stream's read in progress: its lines in the order read, then,
// once lines closes, how it ended.
type Read struct {
	Key   logline.StreamKey
	Lines *spsc.Receiver[Item]
	// cancel ends the read, which then closes lines.
	Cancel context.CancelFunc
	// err is nil when the read ended clean.
	Err error
	// short is whether the read stopped before the end of its range — cut by a
	// limit, cut by the connection's retirement (err ErrRetired), or failed
	// after reading a line in range (err its failure) — and stop the edge of what it
	// read whole: forward, it read every line in range up to stop; backward, every
	// one from stop on. Zero when it read no line in range.
	Short bool
	Stop  logline.Cursor
	// AtBeginning is whether the read reached the oldest line the backend holds: a backward
	// read with no lower bound back to the log's first line, or a forward read with
	// none, which starts there.
	AtBeginning bool
	// atEnd is whether a forward read reached the last line of a finished instance,
	// whose log never grows again.
	AtEnd bool
	// oldest is the moment of the first line a forward read's GET returned, in range
	// or not, zero with none: a read from a second whose oldest line is at or before
	// a moment returned that moment whole.
	Oldest time.Time
}

// readForward reads l's log oldest first, as kubetail's StreamForward does, over
// one GET of its log path.
func readForward(ctx context.Context, get Getter, l Log, o Options) *Read {
	return start(ctx, get, l, o, (*reader).forward)
}

// readBackward reads l's log in windows, newest window first, each oldest first and
// sent as its GET reads it. It finds the log's first line, then reads one window off
// the end with tailLines, as kubetail's StreamBackward does, and windows of time
// after it; under until, windows of time alone (backwardUntil). The stream's limits
// and o.more end it early.
func readBackward(ctx context.Context, get Getter, l Log, o Options) *Read {
	return start(ctx, get, l, o, (*reader).backward)
}

// start runs read on its own goroutine, its lines on the Read's channel, and
// records how it ended: a read that stopped short, at the reader's edge. A read
// that failed past its first line in range stopped short there too, so a page ends
// where its lines do and the next reads on from it.
func start(ctx context.Context, get Getter, l Log, o Options, read func(*reader) error) *Read {
	ctx, cancel := context.WithCancel(ctx)
	out, lines := spsc.New[Item](0)
	r := &Read{Key: l.Key, Lines: lines, Cancel: cancel}
	go func() {
		defer out.Close()
		defer cancel()
		rd := &reader{ctx: ctx, get: get, Key: l.Key, place: l.Place, instance: l.Instance, finished: l.Finished, o: o, out: out}
		err := read(rd)
		switch {
		case errors.Is(err, errShort), errors.Is(err, ErrRetired):
			r.Short, r.Stop = true, rd.edge
		case err != nil && FailureKind(err) != FailFatal && !rd.edge.IsZero():
			r.Short, r.Stop = true, rd.edge
		}
		if !errors.Is(err, errShort) {
			r.Err = err
		}
		r.AtBeginning, r.AtEnd, r.Oldest = rd.AtBeginning, rd.AtEnd, rd.Oldest
	}()
	return r
}

// reader is one stream's read: what it reads with, the instance it reads and
// whether it has finished, where its lines go, and its edge, the last line in
// range it read whole: forward the newest, backward the oldest of the last window
// to complete. windowOldest is the oldest line the window arriving has sent.
type reader struct {
	ctx          context.Context
	get          Getter
	Key          logline.StreamKey
	place        logline.Place
	instance     string
	finished     bool
	o            Options
	out          *spsc.Sender[Item]
	edge         logline.Cursor
	windowOldest logline.Cursor
	AtBeginning  bool
	AtEnd        bool
	Oldest       time.Time
}

// forward reads the log oldest first over one GET. It is cut once its limits are
// spent, never before its first line in range: a cut with no line in range would
// have no edge. A follow's fill is cut only at the end of a second, so a fill from a
// second's end always gets past the next.
func (r *reader) forward() error {
	q := r.query(r.sinceSecond())
	if r.o.Follow {
		q.Set("follow", "true")
	}
	l := r.o.Limits
	wholeSeconds := l != nil && l.WholeSeconds
	// last is the second of the last line sent, zero before the first.
	var last time.Time
	cut := false
	read, err := r.lines(q, r.o, l.second(), func(rec logline.Record, _ int) bool {
		sec := rec.Timestamp.Truncate(time.Second)
		if !last.IsZero() && l.forwardSpent() && (!wholeSeconds || sec.After(last)) {
			cut = true
			return false
		}
		last = sec
		return r.send(rec)
	})
	if cut && err == nil {
		err = errShort
	}
	r.edge, r.Oldest = read.last, read.Oldest
	if loud, ok := errors.AsType[LoudSecond](err); ok {
		// The page is cut at the loud second's last nanosecond: past what the read took
		// of it.
		r.edge = logline.MomentAt(loud.At.Add(time.Second - 1))
	}
	// With no lower bound the read starts at the oldest line, so its first line is
	// the backend's oldest, and a log it read clean to the end with none is empty.
	r.AtBeginning = r.o.Since.At.IsZero() && (!read.first.IsZero() || err == nil)
	r.AtEnd = r.finished && r.o.Until.At.IsZero() && !r.o.Follow && err == nil && r.ctx.Err() == nil
	return err
}

// backward reads the log newest window first: off its end one tailLines window,
// then windows of time; under until, windows of time alone. A second too loud to
// read stops it, and the page is cut at that second's top.
func (r *reader) backward() error {
	err := r.windows()
	if loud, ok := errors.AsType[LoudSecond](err); ok {
		r.edge = logline.MomentAt(loud.At)
	}
	return err
}

// windows is backward's run of windows.
func (r *reader) windows() error {
	first, err := r.firstTimestamp()
	switch {
	case err != nil:
		return err
	case first.IsZero():
		r.reachedFirst(first)
		return nil
	case !r.o.Until.At.IsZero():
		return r.backwardUntil(first)
	}
	return r.tailWindow(first)
}

// tailWindow reads the log's end in one tailLines window, which asks for the kept
// lines the read needs, no more than the stream's allowance holds, and is read
// whole. When it leaves the read short, windows of time go on from the moment it
// began in, which it left out: a window of time's bytes are bounded, a tailLines
// window's only by its lines.
func (r *reader) tailWindow(first time.Time) error {
	w, err := r.window(r.o.Limits.tailLines(r.o.Batch))
	start := w.first.Timestamp
	switch {
	case err != nil:
		// A window cut short went out without its end.
		return err
	case r.ctx.Err() != nil:
		return nil
	case w.Oldest.IsZero():
		// Emptied under the read.
		return nil
	case r.inSinceMoment(w.Oldest):
		// The window began in since's moment, so every later moment is whole in it and
		// every earlier one out of range. Its lines at that moment are numbered from
		// the window's top, which since's line cannot be judged by.
		if !r.endWindow(time.Time{}) || !r.more() {
			return nil
		}
		_, err := r.wholeMoment(w.Oldest)
		return err
	case !start.IsZero() && w.Oldest.Before(start):
		// The window began below since, so it holds every line in range, and whole: its
		// first moment in range is not the one it began in.
		r.endWindow(time.Time{})
		return nil
	case start.IsZero():
		// No line in range: the window holds the newest lines, so none older is in
		// range either.
		return nil
	case !w.Oldest.After(first):
		// The window reached the log's first line, so every moment after the first is
		// whole in it. The first moment may not be.
		if !r.endWindow(time.Time{}) { // coverage:ignore only a cancel landing while the window's end waits to be taken reaches it
			return nil
		}
		if start.Equal(first) {
			if !r.more() {
				return nil
			}
			// The first moment is the backend's oldest only once it has all gone out: a
			// read cancelled inside it leaves older lines of it unread.
			whole, err := r.wholeMoment(first)
			if !whole || err != nil {
				return err
			}
		}
		r.reachedFirst(first)
		return nil
	}
	if !r.endWindow(time.Time{}) || !r.more() {
		return nil
	}
	l := r.o.Limits
	l.windowEnded()
	if l.reached() {
		return errShort
	}
	// The window's rate is over the span from the moment it began in to its newest
	// line; lines all in one moment span next to none.
	reach := r.widen(w.sample, max(w.sample.last.Sub(w.Oldest), time.Millisecond), w.sample.Kept, l.target())
	return r.timeWindows(first, logline.Bound{At: logline.MomentAt(w.Oldest), Inclusive: true}, w.Oldest, reach, w.sample.Kept)
}

// reachedFirst marks a backward read that took every line back to first, the one
// firstTimestamp found, zero for an empty log. That is the backend's oldest with no
// since. With since, it is when since's line is gone, as rotation drops the oldest
// lines first: first is past since's moment, or the log is empty from since's second.
func (r *reader) reachedFirst(first time.Time) {
	since := r.o.Since.At
	r.AtBeginning = since.IsZero() || first.After(since.Timestamp) || first.IsZero() && r.ctx.Err() == nil
}

// inSinceMoment is whether t is the moment of the line since names. A since that
// names a moment alone bounds by time, which a window's numbering cannot mislead.
func (r *reader) inSinceMoment(t time.Time) bool {
	return !r.o.Since.At.IsMoment() && t.Equal(r.o.Since.At.Timestamp)
}

// wholeMoment ends a backward read whose window began in moment t, the log's
// first or since's, with every line of that moment in range. The window cannot
// tell whether it began at the moment's top, so the moment is read forward, whole,
// as a window of its own: its lines are older than the window that found it, and
// arrive after it. It is false once the context has ended before the window's end
// went out.
func (r *reader) wholeMoment(t time.Time) (bool, error) {
	if err := r.span(t, logline.Bound{At: logline.MomentAt(t), Inclusive: true}, func(rec logline.Record, _ int) bool { return r.take(rec) }); err != nil {
		return false, err
	}
	return r.ctx.Err() == nil && r.endWindow(time.Time{}), nil
}

// backwardUntil is backward under until, which a tailLines window reaches only by
// reading every line after it: windows of time ending at until instead, the first
// sized from the rate near the point.
func (r *reader) backwardUntil(first time.Time) error {
	until := r.o.Until.At.Timestamp
	if first.After(until) {
		// No line is in range, and none older than first is left.
		r.reachedFirst(first)
		return nil
	}
	reach := defaultReach
	if r.o.Near != nil {
		reach = windowFor(r.o.Batch, *r.o.Near, r.o.Limits.target())
	}
	return r.timeWindows(first, r.o.Until, until, reach, 0)
}

// timeWindows reads windows of time back from end, which lies at endAt, the first
// reaching back reach, each read forward from the top of a second and stopped where
// the last one began. kept is the kept lines the read has, so each window is sized
// for the ones it still misses. Each window that leaves the read short is followed
// by an older one sized from its own rate, unless the stream has reached a limit. A
// window far busier than it was sized for is stopped part way and replaced
// (replace); dense is the newest second a window was stopped in first, before which
// no window starts until one from it completes.
func (r *reader) timeWindows(first time.Time, end logline.Bound, endAt time.Time, reach time.Duration, kept int64) error {
	l := r.o.Limits
	if l.reached() {
		reach = time.Second
	}
	target := l.take()
	start := startOf(endAt, reach, first)
	var dense time.Time
	for {
		if start.Before(dense) {
			// Widening would cross the dense second again.
			start = dense
		}
		w, stop, err := r.timeWindow(start, end, endAt, target)
		switch {
		case err != nil:
			return err
		case r.ctx.Err() != nil:
			return nil
		case !stop.IsZero():
			if !r.cutWindow() { // coverage:ignore only a cancel landing while the window's cut waits to be taken reaches it
				return nil
			}
			start, dense = r.replace(start, endAt, stop, first, dense)
			target = l.take()
			continue
		}
		if !r.endWindow(start) { // coverage:ignore only a cancel landing while the window's end waits to be taken reaches it
			return nil
		}
		if start.Equal(dense) {
			dense = time.Time{}
		}
		if start.Equal(first) {
			r.reachedFirst(first)
			return nil
		}
		if !r.more() {
			return nil
		}
		if l.reached() {
			return errShort
		}
		target = l.take()
		kept += w.Kept
		reach := r.widen(w, endAt.Sub(start), kept, target)
		end, endAt = logline.Bound{At: logline.MomentAt(start), Inclusive: false}, start
		start = startOf(endAt, reach, first)
	}
}

// startOf is the start of a window reaching back reach from endAt: the top of a
// second, and never before first. A reach of a second is the one-second window.
func startOf(endAt time.Time, reach time.Duration, first time.Time) time.Time {
	if reach <= time.Second {
		return oneSecond(endAt, first)
	}
	if reach >= endAt.Sub(first) {
		return first
	}
	if start := endAt.Add(-reach).Truncate(time.Second); start.After(first) {
		return start
	}
	return first
}

// oneSecond is the start of the one-second window before endAt: the top of the
// second endAt lies in, or of the one before when endAt is a second's top, never
// before first. It covers one second of the clock, so it is never stopped as busy.
func oneSecond(endAt, first time.Time) time.Time {
	if start := endAt.Add(-1).Truncate(time.Second); start.After(first) {
		return start
	}
	return first
}

// replace is the start of the window that replaces one from start to endAt stopped
// in the second stop, and the newest dense second. Stopped in a middle second, it is
// the window from the second after; in its last second, or once the stream has
// reached a limit, the one-second window, which reads that second whole or stops
// the stream. Stopped in its first second, which shows that second dense and the
// stretch's newest second between it and endAt, it is halved toward endAt.
func (r *reader) replace(start, endAt, stop, first, dense time.Time) (time.Time, time.Time) {
	last := oneSecond(endAt, first)
	switch {
	case r.o.Limits.reached(), !stop.Before(last):
		return last, dense
	case stop.After(start):
		return stop.Add(time.Second), dense
	}
	half := endAt.Add(-endAt.Sub(start) / 2).Truncate(time.Second)
	switch {
	case !half.After(start):
		half = start.Truncate(time.Second).Add(time.Second)
	case half.After(last): // coverage:ignore from a second's top or the first line, a halved window never starts past the one-second window
		half = last
	}
	return half, stop
}

// widen is the reach of the window older than w, which covered d and left the read
// with kept lines: sized for the ones still missing at w's rate to hold no more than
// target, or eight times d after a window with no line, whose silence measures no
// rate.
func (r *reader) widen(w Sample, d time.Duration, kept, target int64) time.Duration {
	if w.Lines == 0 {
		if d > maxReach/8 {
			return maxReach
		}
		return 8 * d
	}
	return windowFor(max(r.o.Batch-kept, 1), w.per(d), target)
}

// timeWindow streams the lines in range from the moment from up to end, which lies
// at endAt, oldest first, over one forward GET from from's second, and answers what
// it read. Four times target is reserved for it, the most it reads before it is
// stopped, and what it did not use goes back once it ends. A window wider than the
// one-second window is checked as it goes, and stopped once it is far busier than
// planned or reaches a second too loud to read: stop is then the second it was
// stopped in, zero for a window read whole. A loud second stops the stream only
// from the one-second window.
func (r *reader) timeWindow(from time.Time, end logline.Bound, endAt time.Time, target int64) (Sample, time.Time, error) {
	checked := from.Before(endAt.Add(-1).Truncate(time.Second))
	defer r.o.Limits.windowEnded()
	p := plan{need: r.o.Batch, target: target}
	var w Sample
	q := recent{from: from}
	var stop time.Time
	err := r.span(from, end, func(rec logline.Record, size int) bool {
		kept := r.o.keep(rec)
		// What the body carried: the message is cleaned and cut by now.
		bytes := int64(size)
		w.Add(rec.Timestamp, bytes, kept)
		q.add(rec.Timestamp, bytes, kept)
		if !r.take(rec) {
			return false
		}
		if checked && p.tooBusy(w, q.rate(), endAt.Sub(rec.Timestamp)) {
			stop = rec.Timestamp.Truncate(time.Second)
			return false
		}
		return true
	})
	var loud LoudSecond
	if checked && errors.As(err, &loud) {
		return w, loud.At, nil
	}
	return w, stop, err
}

// cutWindow puts the end of a window stopped part way on out, and is false once the
// context has ended. The window was not read to the point, so its oldest line never
// becomes the edge.
func (r *reader) cutWindow() bool {
	r.windowOldest = logline.Cursor{}
	return r.out.SendContext(r.ctx, Item{WindowCut: true}) == nil
}

// send puts rec on out, and is false once the context has ended.
func (r *reader) send(rec logline.Record) bool {
	return r.out.SendContext(r.ctx, Item{Record: rec}) == nil
}

// endWindow puts the end of a window on out, and is false once the context has
// ended, when it goes unsent. A window is read whole only once its end goes out, so
// only then does its oldest line become the edge; a window of time from start that
// held no line makes start the edge, since the read took every line from there to
// the point and found none.
func (r *reader) endWindow(start time.Time) bool {
	if r.out.SendContext(r.ctx, Item{WindowEnd: true}) != nil { // coverage:ignore only a cancel landing while the window's end waits to be taken reaches it
		return false
	}
	switch {
	case !r.windowOldest.IsZero():
		r.edge, r.windowOldest = r.windowOldest, logline.Cursor{}
	case !start.IsZero():
		r.edge = logline.MomentAt(start)
	}
	return true
}

// more is whether the read may send another GET, as o.more says.
func (r *reader) more() bool {
	return r.o.More == nil || r.o.More(r.ctx)
}

// take sends rec, a line of the window arriving, and is false once the context has
// ended.
func (r *reader) take(rec logline.Record) bool {
	if r.windowOldest.IsZero() {
		r.windowOldest = logline.CursorOf(rec)
	}
	return r.send(rec)
}

// window streams the last tail lines of the log from since, oldest first, and
// answers what the GET read, with a sample of the lines it sent. The lines at the
// moment the GET began in are left to the next window: the GET may have begun
// inside that moment, and the next window reads it from its top. No second stops
// the window, since its first lines are its oldest and a stop would leave no edge.
func (r *reader) window(tail int64) (tailWindowRead, error) {
	q := r.query(r.o.Since.At.Timestamp)
	q.Set("tailLines", strconv.FormatInt(tail, 10))
	o := r.o
	var began time.Time
	o.OnOldest = func(t time.Time) { began = t }
	var w Sample
	read, err := r.lines(q, o, 0, func(rec logline.Record, size int) bool {
		if !rec.Timestamp.After(began) {
			return true
		}
		w.Add(rec.Timestamp, int64(size), r.o.keep(rec))
		return r.take(rec)
	})
	return tailWindowRead{read, w}, err
}

// tailWindowRead is what a tailLines window read: the GET's span, and the sample of the
// lines it sent.
type tailWindowRead struct {
	span
	sample Sample
}

// span hands each the lines in range from the moment from up to end, oldest first,
// each with the bytes the body carried it in, over one forward GET from from's
// second.
func (r *reader) span(from time.Time, end logline.Bound, each func(rec logline.Record, size int) bool) error {
	o := r.o
	if o.Since.At.Timestamp.Before(from) {
		o.Since = logline.Bound{At: logline.MomentAt(from), Inclusive: true}
	}
	o.Until = end
	_, err := r.lines(r.query(from), o, o.Limits.second(), each)
	return err
}

// firstTimestamp is the time of the log's first line from since's second, zero for
// an empty log or a read its context ended.
func (r *reader) firstTimestamp() (time.Time, error) {
	q := r.query(r.o.Since.At.Timestamp)
	q.Set("limitBytes", strconv.Itoa(firstBytes))
	r.o.Limits.get()
	body, err := r.get(r.ctx, logPath(r.Key), q)
	if err != nil {
		return time.Time{}, r.cleanEnd(err)
	}
	defer body.Close()
	buf, err := io.ReadAll(io.LimitReader(counted{body, r.o.Limits}, firstBytes))
	if err != nil {
		return time.Time{}, r.cleanEnd(err)
	}
	rec, _ := ParseLine(buf)
	return rec.Timestamp, nil
}

// cleanEnd is err, or nil once the read's context has ended: a read its context
// ended is a clean one.
func (r *reader) cleanEnd(err error) error {
	if r.ctx.Err() != nil {
		return nil
	}
	return err
}

// sinceSecond is the second a forward read's GET starts at: since's, or the next
// one when since lies past every line of its second's last nanosecond, so the read
// neither reads that second again nor counts it toward its limit.
func (r *reader) sinceSecond() time.Time {
	at := r.o.Since.At
	if !r.o.Since.Inclusive && at.Timestamp.Nanosecond() == 999_999_999 && (at.IsMoment() || at.Ordinal == math.MaxInt) {
		return at.Timestamp.Add(1)
	}
	return at.Timestamp
}

// query is the query every read sends: with timestamps, the container, the
// previous instance when asked, and from since's second when since is set, since
// the log endpoint takes sinceTime to the second.
func (r *reader) query(since time.Time) url.Values {
	q := url.Values{"timestamps": {"true"}, "container": {r.Key.Container}}
	if r.Key.Previous {
		q.Set("previous", "true")
	}
	if !since.IsZero() {
		q.Set("sinceTime", since.UTC().Truncate(time.Second).Format(time.RFC3339))
	}
	return q
}

// span is what one GET read whole: the first and the last line in range, zero
// with none, and the moment of the oldest line at all.
type span struct {
	first, last logline.Cursor
	Oldest      time.Time
}

// lines reads one GET of the log, every line cleaned, stamped with its stream
// and instance and numbered within its moment, and hands each one in o's bounds
// to each with the bytes the body carried it in, before cleaning, until the body
// ends, a line passes the upper bound, or each answers false, charging what it
// reads to o's limits. A second of the log holding more than loud bytes ends the
// GET with a loudSecond, counted as each line's parts arrive, so a long line stops
// the read before it ends; zero checks none. The bounds are applied here and
// nowhere else, so no read can miss one. A read its context ended is a clean one.
func (r *reader) lines(q url.Values, o Options, loud int64, each func(rec logline.Record, size int) bool) (span, error) {
	var read span
	o.Limits.get()
	body, err := r.get(r.ctx, logPath(r.Key), q)
	switch {
	case err == nil:
	case r.ctx.Err() != nil:
		return read, nil
	default:
		return read, err
	}
	defer body.Close()
	br := bufio.NewReader(counted{body, o.Limits})
	// moment and ordinal number the lines: the last line's moment and its ordinal
	// in it, so ordinalAt is the ordinal a line at ts gets next.
	var moment time.Time
	ordinal := 0
	ordinalAt := func(ts time.Time) int {
		if ts.Equal(moment) {
			return ordinal + 1
		}
		return 0
	}
	// oldest tells onOldest the moment of the GET's first line, once.
	oldest := func(t time.Time) {
		if read.Oldest.IsZero() {
			read.Oldest = t
			if o.OnOldest != nil {
				o.OnOldest(t)
			}
		}
	}
	// count counts each part of a line toward its second as the part arrives. A line
	// past the upper bound ends the GET uncounted: its second is not the read's. A
	// read stopped inside its first line has still read that line's moment.
	var count func(ts time.Time, n int) error
	if loud > 0 {
		var sec second
		count = func(ts time.Time, n int) error {
			if ts.IsZero() {
				return nil
			}
			oldest(ts)
			if o.Until.Above(logline.Record{Timestamp: ts, Stream: r.Key, ContainerID: r.instance, Ordinal: ordinalAt(ts)}) {
				return errPastUntil
			}
			return sec.add(ts, int64(n), loud)
		}
	}
	for {
		line, size, err := readLine(br, count)
		if rec, ok := ParseLine(line); ok {
			rec.Stream, rec.Place, rec.ContainerID = r.Key, r.place, r.instance
			rec.Ordinal = ordinalAt(rec.Timestamp)
			moment, ordinal = rec.Timestamp, rec.Ordinal
			oldest(rec.Timestamp)
			switch {
			case o.Since.Below(rec):
			case o.Until.Above(rec):
				return read, nil
			default:
				if read.first.IsZero() {
					read.first = logline.CursorOf(rec)
				}
				if !each(rec, size) {
					return read, nil
				}
				read.last = logline.CursorOf(rec)
			}
		}
		switch {
		case err == nil:
			continue
		case errors.Is(err, io.EOF), errors.Is(err, errPastUntil):
			return read, nil
		case r.ctx.Err() != nil:
			return read, nil
		default:
			return read, err
		}
	}
}

// logPath is key's pod's log path.
func logPath(key logline.StreamKey) string {
	return "/api/v1/namespaces/" + url.PathEscape(key.Namespace) + "/pods/" + url.PathEscape(key.Pod) + "/log"
}

// readLine is the next whole line without its newline, at most maxLine of it,
// valid until br's next read, and the bytes of the body it took, its newline and
// the part past maxLine included. A last line with no newline is not a line, as
// kubetail's reader has it: a follow can end inside one. part, when set, is told
// each part of the line as it is read, with the time the line opens with, zero for
// none, and ends the read with its error.
func readLine(br *bufio.Reader, part func(ts time.Time, n int) error) ([]byte, int, error) {
	var line []byte
	var ts time.Time
	size := 0
	for {
		chunk, err := br.ReadSlice('\n')
		size += len(chunk)
		if part != nil && len(chunk) > 0 {
			if line == nil {
				// The first part holds the time: a timestamp is far shorter than the buffer.
				ts, _ = timeOf(chunk)
			}
			if err := part(ts, len(chunk)); err != nil {
				return nil, size, err
			}
		}
		chunk = chunk[:min(len(chunk), maxLine-len(line))]
		switch {
		case err == nil && line == nil:
			// A line whole in one chunk is the buffer's own bytes, not a copy.
			return bytes.TrimSuffix(chunk, []byte{'\n'}), size, nil
		case err == nil:
			return bytes.TrimSuffix(append(line, chunk...), []byte{'\n'}), size, nil
		case errors.Is(err, bufio.ErrBufferFull):
			line = append(line, chunk...)
		default:
			return nil, size, err
		}
	}
}

// ParseLine is a line as the log endpoint writes it with timestamps: an RFC 3339
// time, a space, the message. A line with no time is not a record.
func ParseLine(line []byte) (logline.Record, bool) {
	ts, ok := timeOf(line)
	if !ok {
		return logline.Record{}, false
	}
	pos := bytes.IndexByte(line, ' ')
	return logline.Record{Timestamp: ts, Message: logline.KeepColor(string(line[pos+1:]))}, true
}

// timeOf is the time a line opens with, false with none.
func timeOf(line []byte) (time.Time, bool) {
	pos := bytes.IndexByte(line[:min(len(line), timestampSpan)], ' ')
	if pos < 0 {
		return time.Time{}, false
	}
	ts, err := time.Parse(time.RFC3339Nano, string(line[:pos]))
	return ts, err == nil
}

// second is the bytes a GET has read of one second of the clock.
type second struct {
	At    time.Time
	Bytes int64
}

// add counts n bytes of a line at ts, and is the loud second once its second holds
// more than limit.
func (s *second) add(ts time.Time, n, limit int64) error {
	if at := ts.Truncate(time.Second); !at.Equal(s.At) {
		s.At, s.Bytes = at, 0
	}
	s.Bytes += n
	if s.Bytes > limit {
		return LoudAt(s.At)
	}
	return nil
}

// loudSecond is a read stopped at a second holding more of one stream's log than a
// page reads. Its words are a Refusal's, so no caller changes them.
type LoudSecond struct {
	refusal.Refusal
	At time.Time
}

// LoudAt is the stop at the second t lies in.
func LoudAt(t time.Time) LoudSecond {
	at := t.Truncate(time.Second)
	return LoudSecond{refusal.New(refusal.CodeTooLoud, "more than 1 MiB of log in the second at "+at.UTC().Format(time.RFC3339)+
		", more than Kubetail reads through the Kubernetes API; lines of that second are left out"), at}
}
