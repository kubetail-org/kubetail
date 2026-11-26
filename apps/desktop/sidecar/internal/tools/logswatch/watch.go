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
	"slices"
	"strings"
	"time"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/safe"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/llm"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/cluster"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/logs"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/logs/logline"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/tools"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/tools/internal/logsinput"
)

// Why a watch ended, in its header's words, when the tail did not say.
const (
	byLines  = "it reached its line limit"
	byBytes  = "it reached its byte limit"
	byTime   = "its time ran out"
	byUser   = "the user stopped it"
	byApp    = "Kubetail quit"
	byModel  = "it was stopped with TaskStop"
	byUnread = "the logs could not be read"
)

var _ tools.Watch = (*watch)(nil)

// watch is a tools.Watch over one tail, whose context is the watch's own: its
// deadline is the watch's time limit, and cancel ends it on a fill or a stop. in
// is the call, scope what the tail reads, since where it started, nil for a
// read's token, now the clock the window's end is read off.
type watch struct {
	ctx    context.Context
	cancel context.CancelFunc
	stream *cluster.Stream[logs.TailFrame]
	in     input
	scope  tools.LogsScopeInput
	since  *time.Time
	now    func() time.Time

	// Set by Wait alone, read by End after it: the lines held, as the model reads
	// them, and their size; the notices; full, why the watch filled, "" until it
	// does; reason, the tail's own end, "" with none; failed, a tail that ended
	// on an error; endedAt, when the tail let go.
	recs    []logline.Record
	size    int
	notices logsinput.Notices
	full    string
	reason  string
	failed  bool
	endedAt time.Time
}

// Wait reads the tail until it closes, holding each line until the watch is
// full and each notice while the notices fit their budget. A cancelled tail
// closes with no frame and no error, so a fill, a stop and the deadline all close
// it the same way: End tells them apart. The tail orders lines per container
// only, so the lines held are sorted once it is done.
func (w *watch) Wait() {
	defer w.cancel()
	for f := range w.stream.Frames {
		switch {
		case f.Record != nil && w.full == "":
			w.hold(*f.Record)
		case f.Notice != nil:
			// A day-long tail raises a stream-skipped notice each time a stream
			// restarts, and nothing but the notices' budget bounds them.
			w.notices.Add(*f.Notice)
		case f.Ended != nil:
			w.reason = strings.TrimSpace(logline.ForModel(f.Ended.Reason))
		}
	}
	w.failed = w.stream.Err() != nil
	w.endedAt = w.now()
	slices.SortStableFunc(w.recs, logline.CompareLines)
}

// hold keeps rec as the service cut it, its message through logline.ForModel, and
// ends the tail once the lines reach either limit, keeping the line that reached
// it.
func (w *watch) hold(rec logline.Record) {
	rec.Message = logline.ForModel(rec.Message)
	w.recs = append(w.recs, rec)
	w.size += len(logsinput.LineText(rec, "")) + 1
	switch {
	case len(w.recs) >= w.in.limit:
		w.full = byLines
	case w.size >= w.in.maxBytes:
		w.full = byBytes
	default:
		return
	}
	w.cancel()
}

// Stop ends the tail.
func (w *watch) Stop() { w.cancel() }

// End is how the watch ended. A fill, the tail's own end and its deadline each
// close the stream on their own, so each is read before a stop: one that lands
// while the stream drains changes nothing. A watch holding lines is matched
// whatever ended it; failed means it holds none.
func (w *watch) End(stoppedBy string) tools.WatchEnd {
	status := tools.WatchMatched
	var because string
	switch {
	case w.full != "":
		because = w.full
	case w.reason != "" || w.failed:
		because = w.reason
		if w.failed {
			because = byUnread
		}
		if len(w.recs) == 0 {
			status = tools.WatchFailed
		}
	case errors.Is(w.ctx.Err(), context.DeadlineExceeded):
		because = byTime
		if len(w.recs) == 0 {
			status = tools.WatchExpired
		}
	default:
		status, because = tools.WatchStopped, stopText(stoppedBy)
	}
	r := tools.WatchResult{Text: w.text(status, because), Lines: []tools.LogsReadLine{}, EndedBecause: because}
	for _, rec := range w.recs {
		r.Lines = append(r.Lines, logsinput.ReadLine(rec))
	}
	if len(w.recs) > 0 {
		r.ViewAround = logsinput.ViewAround(w.scope, w.recs[0].Timestamp, w.endedAt)
	}
	return tools.WatchEnd{Status: status, Result: r}
}

// stopText is a stop as the header says it, by who stopped it.
func stopText(by string) string {
	switch by {
	case llm.TaskStoppedByUser:
		return byUser
	case llm.TaskStoppedByApp:
		return byApp
	}
	return byModel
}

// text is the watch as the model reads it, a LogsRead sample: a header naming the
// sources, the pattern, the window and why it ended, then its notices and the
// count left out, then one line per match, oldest first. The header is cluster
// text, redacted as a read's is.
func (w *watch) text(status tools.WatchStatus, because string) string {
	sources := logsinput.SourcesText(logsinput.SourcesOf(w.scope.Sources))
	var head string
	if len(w.recs) == 0 {
		head = "No line of " + sources + " was written"
		if w.scope.Grep != "" {
			head = "No line of " + sources + " matched /" + w.scope.Grep + "/"
		}
		head += " " + w.windowText("between", "and")
		if status != tools.WatchExpired {
			head += "; the watch ended because " + because
		}
	} else {
		head = fmt.Sprintf("Watched %d lines of %s", len(w.recs), sources)
		if len(w.recs) == 1 {
			head = "Watched 1 line of " + sources
		}
		if w.scope.Grep != "" {
			head += " matching /" + w.scope.Grep + "/"
		}
		head += " " + w.windowText("from", "until") + ", ended because " + because
	}
	lines := append([]string{safe.Redact(head + ".")}, w.notices.Lines()...)
	for _, rec := range w.recs {
		lines = append(lines, logsinput.LineText(rec, ""))
	}
	return strings.Join(lines, "\n")
}

// windowText is when the watch ran, from its start, a moment or a read's token,
// to when it ended, in the words from and to.
func (w *watch) windowText(from, to string) string {
	if w.since == nil {
		return "from a read's token until " + logsinput.Stamp(w.endedAt)
	}
	return from + " " + logsinput.Stamp(*w.since) + " " + to + " " + logsinput.Stamp(w.endedAt)
}
