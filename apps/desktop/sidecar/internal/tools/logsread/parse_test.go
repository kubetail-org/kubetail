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
	"maps"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/logs"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/tools/internal/logsinput"
)

// clock is the tool's now in every test: 2026-10-08T11:00:00Z, an hour past the
// fixture's lines and before any page's ResolvedAt.
var clock = time.Date(2026, 10, 8, 11, 0, 0, 0, time.UTC)

// webapp is the webapp deployment's sources, the object left open for more keys.
const webapp = `{"sources":[{"namespace":"prod","resource":"deployments/webapp"}]`

func parsed(t *testing.T, raw string) input {
	t.Helper()
	in, err := parse(json.RawMessage(raw), clock)
	require.NoError(t, err, raw)
	return in
}

func refused(t *testing.T, raw string) string {
	t.Helper()
	_, err := parse(json.RawMessage(raw), clock)
	require.Error(t, err, raw)
	return logsinput.Refusal(err)
}

// A call with no keys but its sources reads the newest page of the whole log, 50
// lines of messages cut at 2048 bytes within 16384.
func TestAReadsDefaults(t *testing.T) {
	t.Parallel()
	in := parsed(t, webapp+`}`)
	assert.Equal(t, logs.Window{}, in.window)
	assert.Equal(t, logs.PageFromLast, in.pageFrom)
	assert.Equal(t, 50, in.pageSize)
	assert.Equal(t, 2048, in.maxMessageBytes)
	assert.Equal(t, 16384, in.maxPageBytes)
	assert.Nil(t, in.cursor)
}

// A range's ends are moments on the clock, the earliest and latest nanosecond of
// what each names; its padding, the page's end and its caps are as typed.
func TestAReadsKeys(t *testing.T) {
	t.Parallel()
	in := parsed(t, webapp+`,"since":"PT1H","until":"2026-10-08T10:30Z","before":100,"after":"PT5M","page_from":"first","page_size":3,"max_message_bytes":0,"max_page_bytes":10}`)
	since, until := clock.Add(-time.Hour), time.Date(2026, 10, 8, 10, 30, 59, 999999999, time.UTC)
	assert.Equal(t, logs.Window{Since: &since, Until: &until, Before: logs.Padding{Lines: 100}, After: logs.Padding{Span: 5 * time.Minute}}, in.window)
	assert.Equal(t, logs.PageFromFirst, in.pageFrom)
	assert.Equal(t, []int{3, 0, 10}, []int{in.pageSize, in.maxMessageBytes, in.maxPageBytes})
}

// A cursor reads on alone, with the caps; the scope and the window are the token's.
func TestACursorTakesTheCapsAlone(t *testing.T) {
	t.Parallel()
	in := parsed(t, `{"cursor":"abc","page_size":10,"max_message_bytes":100,"max_page_bytes":1000,"description":"on"}`)
	require.NotNil(t, in.cursor)
	assert.Equal(t, "abc", *in.cursor)
	assert.Equal(t, []int{10, 100, 1000}, []int{in.pageSize, in.maxMessageBytes, in.maxPageBytes})

	want := `{"error":"bad-input","field":"cursor","message":"a token already names the sources, filters, grep, range, padding and page_from: send it with page_size, max_message_bytes and max_page_bytes alone"}`
	for _, key := range []string{
		`"sources":[{"namespace":"prod","resource":"deployments/webapp"}]`, `"filters":{"node":["a"]}`, `"grep":"x"`,
		`"since":"PT1H"`, `"until":"PT1H"`, `"before":1`, `"after":"PT1M"`, `"page_from":"first"`,
	} {
		assert.Equal(t, want, refused(t, `{"cursor":"abc",`+key+`}`), key)
	}
}

// A call needs sources or a cursor.
func TestAReadNeedsSourcesOrACursor(t *testing.T) {
	t.Parallel()
	for _, raw := range []string{`{}`, `{"sources":[]}`, `{"cursor":""}`} {
		assert.Equal(t, `{"error":"bad-input","field":"sources","message":"name sources, or a cursor a read printed"}`, refused(t, raw), raw)
	}
}

// A key out of range is refused naming its range; the old keys say what took
// their place.
func TestAReadsBadKeysAreRefused(t *testing.T) {
	t.Parallel()
	for raw, want := range map[string]string{
		`"page_size":0`:              `{"error":"bad-input","field":"page_size","message":"1 to 100"}`,
		`"page_size":101`:            `{"error":"bad-input","field":"page_size","message":"1 to 100"}`,
		`"max_message_bytes":-1`:     `{"error":"bad-input","field":"max_message_bytes","message":"0 for the whole message, or 1 to 16387"}`,
		`"max_message_bytes":1e6`:    `{"error":"bad-input","field":"max_message_bytes","message":"0 for the whole message, or 1 to 16387"}`,
		`"max_page_bytes":0`:         `{"error":"bad-input","field":"max_page_bytes","message":"1 to 16384"}`,
		`"max_page_bytes":16385`:     `{"error":"bad-input","field":"max_page_bytes","message":"1 to 16384"}`,
		`"page_from":"middle"`:       `{"error":"bad-input","field":"page_from","message":"first or last"}`,
		`"anchor":"tail"`:            `{"error":"bad-input","field":"anchor","message":"a read takes since and until"}`,
		`"pin_to_end":true`:          `{"error":"bad-input","field":"pin_to_end","message":"a read opens no viewer"}`,
		`"since":"yesterday"`:        `{"error":"bad-input","field":"since","message":"an RFC 3339 time at any precision, or an ISO 8601 duration back from now such as PT15M; now is 2026-10-08T11:00:00Z"}`,
		`"until":"2026-10-08+01:00"`: `{"error":"bad-input","field":"until","message":"a date is UTC and takes no offset; an RFC 3339 time at any precision, or an ISO 8601 duration back from now such as PT15M; now is 2026-10-08T11:00:00Z"}`,
		`"before":-1`:                `{"error":"bad-input","field":"before","message":"a number of lines from 0 to 500, or an ISO 8601 duration such as PT5M of at most P7D"}`,
	} {
		assert.Equal(t, want, refused(t, webapp+`,`+raw+`}`), raw)
	}
}

// The schema offers every key parse takes and no other, each read by parse from a
// value of its type, and requires neither sources nor cursor: a call needs one of
// them, which no required list says.
func TestTheSchemaIsTheParse(t *testing.T) {
	t.Parallel()
	var schema struct {
		Required   []string                   `json:"required"`
		Properties map[string]json.RawMessage `json:"properties"`
	}
	require.NoError(t, json.Unmarshal(inputSchema, &schema))
	values := map[string]string{
		"description": `"why"`, "sources": `[{"namespace":"prod","resource":"pods/a"}]`, "filters": `{"node":["a"]}`, "grep": `"x"`,
		"since": `"PT1H"`, "until": `"PT1M"`, "before": `1`, "after": `"PT1M"`, "page_from": `"first"`, "page_size": `1`,
		"max_message_bytes": `1`, "max_page_bytes": `1`, "cursor": `"abc"`,
	}
	keys := []string{}
	for key := range schema.Properties {
		keys = append(keys, key)
	}
	assert.ElementsMatch(t, keys, slices.Collect(maps.Keys(values)))
	for key, value := range values {
		raw := webapp + `,"` + key + `":` + value + `}`
		if key == "sources" || key == "cursor" {
			raw = `{"` + key + `":` + value + `}`
		}
		_, err := parse(json.RawMessage(raw), clock)
		assert.NoError(t, err, key)
	}
	assert.NotContains(t, schema.Required, "sources")
	assert.NotContains(t, schema.Required, "cursor")
}
