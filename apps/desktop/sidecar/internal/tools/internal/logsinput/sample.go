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

package logsinput

import (
	"fmt"
	"time"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/logs"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/logs/logline"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/tools"
)

// A sample's sizes, in bytes. DefaultMessageBytes is where the tools cut each
// message by default. SampleBudget is LogsRead's default and most for the page's
// bytes, and LogsWatch's for the lines it holds.
const (
	DefaultMessageBytes = 2 << 10
	SampleBudget        = 16 << 10
)

// NoticesBudget is the most bytes of notices a header prints. A page can name
// every one of logs.MaxStreams streams in a stream-skipped notice, and a day-long
// follow raises one each time a stream restarts.
const NoticesBudget = 2 << 10

// Notices is a header's notices: one line each, its message through
// logline.ForModel, while they fit NoticesBudget, and the rest counted.
type Notices struct {
	lines         []string
	size, leftOut int
}

// Add keeps n's line, or counts it once the budget is spent.
func (ns *Notices) Add(n logs.Notice) {
	if ns.leftOut > 0 {
		ns.leftOut++
		return
	}
	line := "notice: " + string(n.Code) + ": " + logline.ForModel(n.Message)
	if ns.size+len(line)+1 > NoticesBudget {
		ns.leftOut++
		return
	}
	ns.lines = append(ns.lines, line)
	ns.size += len(line) + 1
}

// Lines is the lines kept, then one counting the notices left out.
func (ns *Notices) Lines() []string {
	if ns.leftOut == 0 {
		return ns.lines
	}
	return append(ns.lines, fmt.Sprintf("notice: %d more notices left out", ns.leftOut))
}

// The marks of a line outside a read's range, which LineText prints before its
// message.
const (
	PlaceBefore = "[before] "
	PlaceAfter  = "[after] "
)

// PlaceOf is rec's mark in resp's window: PlaceBefore before the echoed Since,
// PlaceAfter past the echoed Until, or, with Until open and After set, at or past
// ResolvedAt, where the service ends an open range; "" inside the range. The
// bounds include their moment.
func PlaceOf(rec logline.Record, resp logs.FetchResponse) string {
	w := resp.Request.Window
	switch {
	case w.Since != nil && rec.Timestamp.Before(*w.Since):
		return PlaceBefore
	case w.Until != nil && rec.Timestamp.After(*w.Until):
		return PlaceAfter
	case w.Until == nil && w.After != (logs.Padding{}) && !rec.Timestamp.Before(resp.ResolvedAt):
		return PlaceAfter
	}
	return ""
}

// LineText is rec as the model reads it: the hole before it on a line of its own,
// then its timestamp, its stream, place, which is "" or a mark, and its message,
// saying the bytes the service cut from it. A message holds no newline, so every
// line a record writes starts with the sidecar's [ or a timestamp.
func LineText(rec logline.Record, place string) string {
	line := Stamp(rec.Timestamp) + " " + streamOf(rec.Stream) + " " + place + rec.Message
	if rec.TruncatedBytes > 0 {
		line += fmt.Sprintf(" … [%d bytes cut]", rec.TruncatedBytes)
	}
	if rec.Gap == nil {
		return line
	}
	return "[lines may be missing from " + streamOf(rec.Stream) + " between " + Stamp(rec.Gap.After) + " and " + Stamp(rec.Gap.Before) + "]\n" + line
}

// streamOf is a stream as a line names it.
func streamOf(k logline.StreamKey) string {
	s := k.Namespace + "/" + k.Pod + "/" + k.Container
	if k.Previous {
		s += " (previous)"
	}
	return s
}

// stampLayout is RFC 3339 with nine fraction digits always, so a printed timestamp
// copied into since and until names that one nanosecond.
const stampLayout = "2006-01-02T15:04:05.000000000Z"

// Stamp is t in stampLayout, in UTC.
func Stamp(t time.Time) string { return t.UTC().Format(stampLayout) }

// ReadLine is rec as an action keeps it.
func ReadLine(rec logline.Record) tools.LogsReadLine {
	return tools.LogsReadLine{
		Timestamp: rec.Timestamp, Namespace: rec.Stream.Namespace, Pod: rec.Stream.Pod, Container: rec.Stream.Container,
		Previous: rec.Stream.Previous, Message: rec.Message, Gap: rec.Gap, TruncatedBytes: rec.TruncatedBytes,
	}
}

// ViewAround is the view View around this opens: view at the moment at, unpinned,
// resolved at resolvedAt.
func ViewAround(view tools.LogsScopeInput, at, resolvedAt time.Time) *tools.LogsViewAction {
	return &tools.LogsViewAction{
		LogsScopeInput: view, Anchor: tools.LogsViewAnchor{Kind: tools.LogsAnchorAt, At: &at}, ResolvedAt: resolvedAt,
	}
}
