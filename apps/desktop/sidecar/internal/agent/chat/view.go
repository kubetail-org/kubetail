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

// The View section: what the user's open log view showed as they sent, so the
// model can resolve "this" without reading a line it was not shown.
package chat

import (
	"strings"
	"time"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/fencejson"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/logs/logline"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/tools"
)

// Snapshot is what the user's open log view shows as they send: the LogsView or
// LogsRead call that opened it, never its view, which the sidecar reads from the
// call's row.
// From and To are the first and last visible line's stamps, both nil with none on
// screen. Grep is the one the viewer runs, which the user can change from the
// call's; nil keeps the call's. Selected holds the first selected lines,
// SelectedCount all of them.
type Snapshot struct {
	CallID                     ToolCallID
	From, To                   *time.Time
	Grep                       *string
	Lines                      int
	AtEnd, PinnedToEnd, Paused bool
	Selected                   []SelectedLine
	SelectedCount              int
}

// SelectedLine is one selected line as the viewer holds it. Cluster text.
type SelectedLine struct {
	Timestamp                 time.Time
	Namespace, Pod, Container string
	Previous                  bool
	Message                   string
}

// maxSelected is how many selected lines a snapshot may carry.
const maxSelected = 50

// The View section's caps, in bytes as the section spells them: after fencejson's
// escapes, where an angle bracket, an ampersand or a backtick takes six.
const (
	// viewBudget is the whole section, heading and fences included.
	viewBudget = 8 << 10
	// viewShare is the view's part of it.
	viewShare = 2 << 10
	// maxViewGrep is the view's grep, quotes included, so the rest of the view
	// fits beside the longest one.
	maxViewGrep = 512
	// maxViewCall is the call's id. A provider's ids are far shorter; this keeps
	// the section under its budget whatever the row holds.
	maxViewCall = 256
	// maxViewMessage is one message, quotes included.
	maxViewMessage = 1 << 10
)

// check refuses a snapshot the webview never builds.
func (s Snapshot) check() error {
	if len(s.Selected) > maxSelected || s.SelectedCount < len(s.Selected) || s.Lines < 0 || (s.From == nil) != (s.To == nil) {
		return ErrBadRequest
	}
	// The viewer runs only a grep a scope takes.
	if s.Grep != nil {
		if _, err := logline.CompileGrep(*s.Grep); err != nil {
			return ErrBadRequest
		}
	}
	for _, l := range s.Selected {
		if len(l.Message) > logline.MaxLine {
			return ErrBadRequest
		}
	}
	return nil
}

// withView is card with the View section appended: toolUseID, the id the model's
// own tool_use block carries; the call's stored action, under the grep snap names;
// and what snap says was on screen. Every message reaches the model through logline.ForModel. Selected lines
// go in order while the section stays under viewBudget; the rest are counted.
// Without them the section is under it by its shares: the call, the view and
// the shown span.
func withView(card, toolUseID string, action tools.LogsViewAction, snap Snapshot) string {
	if snap.Grep != nil {
		action.Grep = *snap.Grep
	}
	fields := map[string]any{"call": cutTo(toolUseID, maxViewCall), "view": viewFields(action), "shown": shownFields(snap)}
	var kept []map[string]any
	for _, l := range snap.Selected {
		next := append(kept, selectedFields(l))
		setSelected(fields, next, snap.SelectedCount)
		if len(withSection("", "View", fields)) > viewBudget {
			break
		}
		kept = next
	}
	setSelected(fields, kept, snap.SelectedCount)
	return withSection(card, "View", fields)
}

// setSelected sets the lines kept of count selected, and how many were left out.
func setSelected(fields map[string]any, kept []map[string]any, count int) {
	delete(fields, "selected")
	delete(fields, "selectedMore")
	if len(kept) > 0 {
		fields["selected"] = kept
	}
	if more := count - len(kept); more > 0 {
		fields["selectedMore"] = more
	}
}

// viewFields is the action as the model reads it: each source's flags, the
// filters and the grep only where set, the anchor an edge or its moment. It is
// under viewShare: the grep is cut at its own share, then trailing filters are
// dropped, then trailing sources, each counted.
func viewFields(a tools.LogsViewAction) map[string]any {
	sources := make([]map[string]any, len(a.Sources))
	for i, s := range a.Sources {
		source := map[string]any{"namespace": s.Namespace, "kind": s.Kind, "name": s.Name}
		if len(s.Containers) > 0 {
			source["containers"] = s.Containers
		}
		if s.AllContainers {
			source["allContainers"] = true
		}
		if s.Previous {
			source["previous"] = true
		}
		sources[i] = source
	}
	view := map[string]any{"sources": sources, "anchor": string(a.Anchor.Kind), "pinToEnd": a.PinToEnd}
	if a.Anchor.Kind == tools.LogsAnchorAt && a.Anchor.At != nil {
		view["anchor"] = stamp(*a.Anchor.At)
	}
	if len(a.Filters) > 0 {
		view["filters"] = a.Filters
	}
	if a.Grep != "" {
		view["grep"] = cutTo(a.Grep, maxViewGrep)
	}
	for kept := len(a.Filters); kept > 0 && marshalledLen(view) > viewShare; {
		kept--
		view["filters"], view["moreFilters"] = a.Filters[:kept], len(a.Filters)-kept
	}
	for kept := len(sources); kept > 0 && marshalledLen(view) > viewShare; {
		kept--
		view["sources"], view["moreSources"] = sources[:kept], len(sources)-kept
	}
	return view
}

// shownFields is the span on screen and the viewer's flags.
func shownFields(s Snapshot) map[string]any {
	shown := map[string]any{"lines": s.Lines, "atEnd": s.AtEnd, "pinned": s.PinnedToEnd, "paused": s.Paused}
	if s.From != nil && s.To != nil {
		shown["from"], shown["to"] = stamp(*s.From), stamp(*s.To)
	}
	return shown
}

func selectedFields(l SelectedLine) map[string]any {
	line := map[string]any{
		"at":      stamp(l.Timestamp),
		"stream":  l.Namespace + "/" + l.Pod + "/" + l.Container,
		"message": cutTo(logline.ForModel(l.Message), maxViewMessage),
	}
	if l.Previous {
		line["previous"] = true
	}
	return line
}

// stamp is t as the viewer writes it: RFC 3339 in UTC, at its own precision.
func stamp(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }

// cutTo is s cut on a character boundary, with an ellipsis, so it spells at most
// limit bytes, quotes included. A message is cut after redaction, which reads the
// whole line.
func cutTo(s string, limit int) string {
	if marshalledLen(s) <= limit {
		return s
	}
	var b strings.Builder
	n := marshalledLen("…")
	for _, r := range s {
		w := marshalledLen(string(r)) - len(`""`)
		if n+w > limit {
			break
		}
		b.WriteRune(r)
		n += w
	}
	return b.String() + "…"
}

// marshalledLen is how many bytes v takes in a section.
func marshalledLen(v any) int {
	raw, err := fencejson.Marshal(v)
	if err != nil { // coverage:ignore strings, counts and lists of them always marshal
		panic(err)
	}
	return len(raw)
}

// viewHeading opens the View section. JSON escapes every newline in a value, so
// no value can spell it, nor the fence that closes the section.
const viewHeading = "## View\n\n```json\n"

// withoutView is block with its View section cut, for a turn with no screen
// behind it: a notice turn, or a subagent's brief. Every other byte is kept.
func withoutView(block string) string {
	i := strings.Index(block, viewHeading)
	if i < 0 || i > 0 && !strings.HasSuffix(block[:i], "\n\n") {
		return block
	}
	body := i + len(viewHeading)
	end := strings.Index(block[body:], "\n```")
	if end < 0 {
		return block
	}
	before, after := block[:i], block[body+end+len("\n```"):]
	if after == "" {
		return strings.TrimSuffix(before, "\n\n")
	}
	return before + strings.TrimPrefix(after, "\n\n")
}
