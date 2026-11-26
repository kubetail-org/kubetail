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
	"encoding/json"
	"fmt"
	"time"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/logs"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/tools/internal/logsinput"
)

// A page's lines: at most maxPageSize, defaultPageSize when the call names none.
const (
	maxPageSize     = 100
	defaultPageSize = 50
)

// momentForms is what since and until take, which a refusal names with the clock.
const momentForms = "an RFC 3339 time at any precision, or an ISO 8601 duration back from now such as PT15M"

// input is one call's arguments: the scope's, the window and the end the first
// page comes from, or the cursor a read printed, nil for none; and the caps every
// page names.
type input struct {
	logsinput.Input
	window   logs.Window
	pageFrom logs.PageFrom
	cursor   *string

	pageSize, maxMessageBytes, maxPageBytes int
}

// parse reads the call's arguments, the shared ones through logsinput, and reads
// the range's ends on the clock now.
func parse(raw json.RawMessage, now time.Time) (input, error) {
	in := input{pageSize: defaultPageSize, maxMessageBytes: logsinput.DefaultMessageBytes, maxPageBytes: logsinput.SampleBudget}
	var since, until, pageFrom, cursor string
	shared, err := logsinput.Parse(raw, logsinput.Fields{
		"since": &since, "until": &until, "before": &in.window.Before, "after": &in.window.After,
		"page_from": &pageFrom, "page_size": &in.pageSize, "max_message_bytes": &in.maxMessageBytes,
		"max_page_bytes": &in.maxPageBytes, "cursor": &cursor,
		"anchor":     logsinput.BadInput("anchor", "a read takes since and until"),
		"pin_to_end": logsinput.BadInput("pin_to_end", "a read opens no viewer"),
	})
	if err != nil {
		return input{}, err
	}
	in.Input = shared
	switch {
	case in.pageSize < 1 || in.pageSize > maxPageSize:
		return input{}, logsinput.BadInput("page_size", fmt.Sprintf("1 to %d", maxPageSize))
	case in.maxPageBytes < 1 || in.maxPageBytes > logsinput.SampleBudget:
		return input{}, logsinput.BadInput("max_page_bytes", fmt.Sprintf("1 to %d", logsinput.SampleBudget))
	}
	if err := logsinput.CheckMessageBytes(in.maxMessageBytes); err != nil {
		return input{}, err
	}
	if cursor != "" {
		if in.NamesScope() || since != "" || until != "" || in.window != (logs.Window{}) || pageFrom != "" {
			return input{}, logsinput.BadInput("cursor", "a token already names the sources, filters, grep, range, padding and page_from: send it with page_size, max_message_bytes and max_page_bytes alone")
		}
		in.cursor = &cursor
		return in, nil
	}
	if len(in.Sources) == 0 {
		return input{}, logsinput.BadInput("sources", "name sources, or a cursor a read printed")
	}
	switch pageFrom {
	case "", "last":
		in.pageFrom = logs.PageFromLast
	case "first":
		in.pageFrom = logs.PageFromFirst
	default:
		return input{}, logsinput.BadInput("page_from", "first or last")
	}
	if in.window.Since, err = momentOf(since, now, logsinput.Since, "since"); err != nil {
		return input{}, err
	}
	if in.window.Until, err = momentOf(until, now, logsinput.Until, "until"); err != nil {
		return input{}, err
	}
	return in, nil
}

// momentOf is the end of the range s names on the clock now, nil for none.
func momentOf(s string, now time.Time, end logsinput.End, field string) (*time.Time, error) {
	if s == "" {
		return nil, nil
	}
	at, err := logsinput.MomentOf(s, now, end, logsinput.Behind)
	if err != nil {
		return nil, logsinput.BadInput(field, logsinput.MomentMessage(err, momentForms, now))
	}
	return &at, nil
}

// request is the page the call reads: a first page of the scope view resolved
// to, or the cursor alone, each with the caps and for the model.
func (in input) request(view logs.Scope) logs.FetchRequest {
	q := logs.FetchRequest{PageSize: in.pageSize, MaxMessageBytes: in.maxMessageBytes, MaxPageBytes: in.maxPageBytes, ForModel: true}
	if in.cursor != nil {
		q.Cursor = in.cursor
		return q
	}
	q.Scope, q.Window, q.PageFrom = view, in.window, in.pageFrom
	return q
}
