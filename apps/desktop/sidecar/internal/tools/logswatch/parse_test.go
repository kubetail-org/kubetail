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
	"encoding/json"
	"maps"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/tools/internal/logsinput"
)

// clock is the clock parse reads on: 2026-10-18T13:50:00Z.
var clock = time.Date(2026, 10, 18, 13, 50, 0, 0, time.UTC)

// parsed is a call on webapp's sources with extra keys, parsed on the clock.
func parsed(t *testing.T, extra string) (input, error) {
	t.Helper()
	return parse(json.RawMessage(`{"sources":[{"namespace":"payments","resource":"deployments/webapp"}]`+extra+`}`), clock)
}

// A bare call watches from now for one line of anything, up to a day, each message
// cut at 2048 bytes.
func TestABareCallWatchesFromNowForOneLine(t *testing.T) {
	t.Parallel()
	in, err := parsed(t, "")
	require.NoError(t, err)
	assert.Equal(t, clock, in.since)
	assert.Nil(t, in.cursor)
	assert.Equal(t, 1, in.limit)
	assert.Equal(t, logsinput.SampleBudget, in.maxBytes)
	assert.Equal(t, logsinput.DefaultMessageBytes, in.maxMessageBytes)
	assert.Equal(t, clock.Add(maxWatch), in.until)
	assert.False(t, in.capped)
}

// since takes a time, its first nanosecond, or a duration back from now; until a
// time, its last nanosecond, or a duration ahead.
func TestTheWindowIsAMomentOrADuration(t *testing.T) {
	t.Parallel()
	in, err := parsed(t, `,"since":"PT10M","until":"PT2H","limit":100,"max_bytes":4096,"max_message_bytes":0,"grep":"panic"`)
	require.NoError(t, err)
	assert.Equal(t, clock.Add(-10*time.Minute), in.since)
	assert.Equal(t, clock.Add(2*time.Hour), in.until)
	assert.Equal(t, []int{100, 4096, 0}, []int{in.limit, in.maxBytes, in.maxMessageBytes})
	assert.Equal(t, "panic", in.Grep)

	in, err = parsed(t, `,"since":"2026-10-18T13:00Z","until":"2026-10-18T14:00Z"`)
	require.NoError(t, err)
	assert.Equal(t, time.Date(2026, 10, 18, 13, 0, 0, 0, time.UTC), in.since)
	assert.Equal(t, time.Date(2026, 10, 18, 14, 0, 59, 999999999, time.UTC), in.until)
}

// A cursor starts the watch where a read's page stopped, with the token's scope.
func TestACursorStartsTheWatch(t *testing.T) {
	t.Parallel()
	in, err := parse(json.RawMessage(`{"cursor":"abc","until":"PT1H","limit":5}`), clock)
	require.NoError(t, err)
	require.NotNil(t, in.cursor)
	assert.Equal(t, "abc", *in.cursor)
	assert.Equal(t, clock.Add(time.Hour), in.until)

	want := `{"error":"bad-input","field":"cursor","message":"a token already names the sources, filters, grep and where the watch starts: send it without them"}`
	for _, key := range []string{`"sources":[{"namespace":"prod","resource":"deployments/webapp"}]`, `"filters":{"node":["a"]}`, `"grep":"x"`, `"since":"PT1H"`} {
		_, err := parse(json.RawMessage(`{"cursor":"abc",`+key+`}`), clock)
		assert.Equal(t, want, logsinput.Refusal(err), key)
	}
	_, err = parse(json.RawMessage(`{"limit":5}`), clock)
	assert.Equal(t, `{"error":"bad-input","field":"sources","message":"name sources, or a next or follow token a read printed"}`, logsinput.Refusal(err))
}

// An until past the cap is cut to it, and the input says so.
func TestAnUntilPastTheCapIsCut(t *testing.T) {
	t.Parallel()
	in, err := parsed(t, `,"until":"P3D"`)
	require.NoError(t, err)
	assert.Equal(t, clock.Add(maxWatch), in.until)
	assert.True(t, in.capped)
}

// Each argument a watch cannot take is refused by its field.
func TestBadArgumentsAreRefusedByField(t *testing.T) {
	t.Parallel()
	const sinceMessage = "an RFC 3339 time or an ISO 8601 duration back from now such as PT10M, not after now; now is 2026-10-18T13:50:00Z"
	const untilMessage = "an RFC 3339 time or an ISO 8601 duration ahead of now such as PT2H, after now; now is 2026-10-18T13:50:00Z"
	for extra, want := range map[string]string{
		`,"anchor":"tail"`:                `{"error":"bad-input","field":"anchor","message":"a watch takes since and until"}`,
		`,"pin_to_end":true`:              `{"error":"bad-input","field":"pin_to_end","message":"a watch opens no viewer"}`,
		`,"page_from":"last"`:             `{"error":"bad-input","field":"page_from","message":"a watch reads its lines in order"}`,
		`,"limit":0`:                      `{"error":"bad-input","field":"limit","message":"1 to 100"}`,
		`,"limit":101`:                    `{"error":"bad-input","field":"limit","message":"1 to 100"}`,
		`,"max_bytes":0`:                  `{"error":"bad-input","field":"max_bytes","message":"1 to 16384"}`,
		`,"max_bytes":16385`:              `{"error":"bad-input","field":"max_bytes","message":"1 to 16384"}`,
		`,"max_message_bytes":-1`:         `{"error":"bad-input","field":"max_message_bytes","message":"0 for the whole message, or 1 to 16387"}`,
		`,"since":"2026-10-18T14:00:00Z"`: `{"error":"bad-input","field":"since","message":"` + sinceMessage + `"}`,
		`,"since":"10m"`:                  `{"error":"bad-input","field":"since","message":"` + sinceMessage + `"}`,
		`,"until":"2026-10-18T13:00:00Z"`: `{"error":"bad-input","field":"until","message":"` + untilMessage + `"}`,
		`,"until":"2026-10-18+01:00"`:     `{"error":"bad-input","field":"until","message":"a date is UTC and takes no offset; ` + untilMessage + `"}`,
	} {
		_, err := parsed(t, extra)
		require.Error(t, err, extra)
		assert.Equal(t, want, logsinput.Refusal(err), extra)
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
		"since": `"PT1H"`, "until": `"PT1H"`, "cursor": `"abc"`, "limit": `2`, "max_bytes": `10`, "max_message_bytes": `10`,
	}
	keys := []string{}
	for key := range schema.Properties {
		keys = append(keys, key)
	}
	assert.ElementsMatch(t, keys, slices.Collect(maps.Keys(values)))
	for key, value := range values {
		raw := `{"sources":[{"namespace":"prod","resource":"pods/a"}],"` + key + `":` + value + `}`
		if key == "sources" || key == "cursor" {
			raw = `{"` + key + `":` + value + `}`
		}
		_, err := parse(json.RawMessage(raw), clock)
		assert.NoError(t, err, key)
	}
	assert.NotContains(t, schema.Required, "sources")
	assert.NotContains(t, schema.Required, "cursor")
}
