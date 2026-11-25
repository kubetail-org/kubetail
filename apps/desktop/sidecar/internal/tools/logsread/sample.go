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
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/safe"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/logs"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/logs/iso8601"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/logs/logline"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/tools"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/tools/internal/logsinput"
)

// notesBudget is the most bytes of notes on the sources a header prints.
const notesBudget = 4 << 10

// sample is a page as the model reads it: the response and its records, each
// message through logline.ForModel. The service ran it already and it changes
// nothing run again; it stays the one gate a line reaches the model through.
type sample struct {
	resp    logs.FetchResponse
	records []logline.Record
}

func sampleOf(resp logs.FetchResponse) sample {
	s := sample{resp: resp, records: make([]logline.Record, len(resp.Records))}
	for i, rec := range resp.Records {
		rec.Message = logline.ForModel(rec.Message)
		s.records[i] = rec
	}
	return s
}

// output is the sample as the model reads it: the header, then one line per
// record, each marked with its place in the window.
func (s sample) output(in input, notes []logsinput.Note) string {
	lines := []string{s.header(in, notes)}
	for _, rec := range s.records {
		lines = append(lines, logsinput.LineText(rec, logsinput.PlaceOf(rec, s.resp)))
	}
	return strings.Join(lines, "\n")
}

// header is the lines before the records: the read in words, the request as it
// ran, the page's tokens, and its notices. The words and the notices are cluster
// text, redacted as a receipt is; the tokens go after the redaction, which could
// change them.
func (s sample) header(in input, notes []logsinput.Note) string {
	scope := s.resp.Request.Scope
	typed := in.Sources
	if in.cursor != nil {
		typed = logsinput.SourcesOf(scope.Sources)
	}
	var b strings.Builder
	switch n := len(s.records); n {
	case 0:
		b.WriteString("No lines of ")
	case 1:
		b.WriteString("Read 1 line of ")
	default:
		fmt.Fprintf(&b, "Read %d lines of ", n)
	}
	b.WriteString(logsinput.SourcesText(typed))
	if scope.Grep != "" {
		b.WriteString(", matching /" + scope.Grep + "/")
	}
	b.WriteString(".")
	b.WriteString(notesText(typed, scope.Sources, notes))
	lines := []string{safe.Redact(b.String()), s.requestText(), s.pageText()}
	var notices logsinput.Notices
	for _, n := range s.resp.Notices {
		notices.Add(n)
	}
	return strings.Join(append(lines, notices.Lines()...), "\n")
}

// notesText is what the header says of each source beyond its name, up to
// notesBudget; the sources past it are counted.
func notesText(typed []logsinput.Source, sources []tools.LogsViewSource, notes []logsinput.Note) string {
	var b strings.Builder
	for i, text := range logsinput.NotesText(typed, sources, notes) {
		if b.Len()+len(text) > notesBudget {
			fmt.Fprintf(&b, " Notes on %d more sources left out.", len(notes)-i)
			break
		}
		b.WriteString(text)
	}
	return b.String()
}

// requestText is the request as the service ran it, under the call's own keys:
// an open bound open, a padding of nothing 0, and page_from left out on a
// continuation. With until open and after set, resolved_at is where the after
// padding starts.
func (s sample) requestText() string {
	q := s.resp.Request
	parts := []string{"since=" + boundText(q.Since), "until=" + boundText(q.Until)}
	if q.Until == nil && q.After != (logs.Padding{}) {
		parts = append(parts, "resolved_at="+logsinput.Stamp(s.resp.ResolvedAt))
	}
	parts = append(parts, "before="+paddingText(q.Before), "after="+paddingText(q.After))
	if q.PageFrom != "" {
		parts = append(parts, "page_from="+string(q.PageFrom))
	}
	parts = append(parts, "page_size="+strconv.Itoa(q.PageSize), "max_message_bytes="+strconv.Itoa(q.MaxMessageBytes),
		"max_page_bytes="+strconv.Itoa(q.MaxPageBytes))
	return "request: " + strings.Join(parts, " ")
}

func boundText(at *time.Time) string {
	if at == nil {
		return "open"
	}
	return logsinput.Stamp(*at)
}

func paddingText(p logs.Padding) string {
	if p.Span > 0 {
		return iso8601.Format(p.Span)
	}
	return strconv.Itoa(p.Lines)
}

// pageText is the page's three tokens, none for each the page has not.
func (s sample) pageText() string {
	token := func(c *string) string {
		if c == nil {
			return "none"
		}
		return *c
	}
	p := s.resp.Page
	return "page: prev=" + token(p.Prev) + " next=" + token(p.Next) + " follow=" + token(p.Follow)
}

// action is the sample as the row keeps it: the window the service ran, the lines
// the model read, whether the page had more either way, its notices, and the view
// View around this opens at the first line's moment.
func (s sample) action() tools.LogsReadAction {
	q := s.resp.Request
	read := tools.LogsReadAction{
		LogsScopeInput: q.Scope, Since: q.Since, Until: q.Until, Before: tools.LogsPaddingOf(q.Before), After: tools.LogsPaddingOf(q.After),
		ResolvedAt: s.resp.ResolvedAt, Lines: []tools.LogsReadLine{}, OlderMore: s.resp.Page.Prev != nil,
		NewerMore: s.resp.Page.Next != nil, Notices: []tools.LogsNotice{},
	}
	if q.PageFrom != "" {
		read.PageFrom = &q.PageFrom
	}
	for _, rec := range s.records {
		read.Lines = append(read.Lines, logsinput.ReadLine(rec))
	}
	for _, n := range s.resp.Notices {
		read.Notices = append(read.Notices, tools.LogsNotice{Code: string(n.Code), Message: logline.ForModel(n.Message)})
	}
	if len(s.records) > 0 {
		read.ViewAround = logsinput.ViewAround(q.Scope, s.records[0].Timestamp, s.resp.ResolvedAt)
	}
	return read
}
