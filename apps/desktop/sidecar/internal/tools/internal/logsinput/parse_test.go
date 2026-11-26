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
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/logs"
)

const webapp = `{"sources":[{"namespace":"prod","resource":"deployments/webapp"}]`

// Each log tool's schema offers the sources and filters Parse reads in the same
// words, so a change to one is made to all three.
func TestEveryLogToolOffersTheSameSourcesAndFilters(t *testing.T) {
	t.Parallel()
	type shared struct {
		Properties struct {
			Sources json.RawMessage `json:"sources"`
			Filters json.RawMessage `json:"filters"`
		} `json:"properties"`
	}
	var want shared
	for i, tool := range []string{"logsread", "logsview", "logswatch"} {
		raw, err := os.ReadFile(filepath.Join("..", "..", tool, "prompts", "schema.json"))
		require.NoError(t, err)
		var got shared
		require.NoError(t, json.Unmarshal(raw, &got))
		require.NotEmpty(t, got.Properties.Sources, tool)
		require.NotEmpty(t, got.Properties.Filters, tool)
		if i == 0 {
			want = got
			continue
		}
		assert.JSONEq(t, string(want.Properties.Sources), string(got.Properties.Sources), tool)
		assert.JSONEq(t, string(want.Properties.Filters), string(got.Properties.Filters), tool)
	}
}

// A tool's own field is read into its destination, by its type; a key that is
// neither shared nor the tool's own is refused without its name.
func TestParseReadsAToolsOwnFields(t *testing.T) {
	t.Parallel()
	var mode string
	var limit int
	own := Fields{"mode": &mode, "limit": &limit, "anchor": BadInput("anchor", "not here")}

	in, err := Parse(json.RawMessage(webapp+`,"mode":"tail","limit":20}`), own)
	require.NoError(t, err)
	assert.Equal(t, "tail", mode)
	assert.Equal(t, 20, limit)
	assert.Equal(t, "webapp", in.Sources[0].Name)

	_, err = Parse(json.RawMessage(webapp+`,"hunter2":1}`), own)
	assert.Equal(t, `{"error":"bad-input"}`, Refusal(err))

	_, err = Parse(json.RawMessage(webapp+`,"anchor":"tail"}`), own)
	assert.Equal(t, `{"error":"bad-input","field":"anchor","message":"not here"}`, Refusal(err))

	for _, bad := range []string{`"20"`, `2.5`, `null`} {
		_, err = Parse(json.RawMessage(webapp+`,"limit":`+bad+`}`), own)
		assert.Equal(t, `{"error":"bad-input","field":"limit"}`, Refusal(err), bad)
	}
}

// A call names at most maxSources sources.
func TestMoreThanMaxSourcesIsRefused(t *testing.T) {
	t.Parallel()
	source := `{"namespace":"prod","resource":"pods/a"}`
	many := func(n int) json.RawMessage {
		return json.RawMessage(`{"sources":[` + strings.Repeat(source+",", n-1) + source + `]}`)
	}
	_, err := Parse(many(maxSources), nil)
	require.NoError(t, err)

	_, err = Parse(many(maxSources+1), nil)
	assert.Equal(t, `{"error":"bad-input","field":"sources","message":"at most 100"}`, Refusal(err))
}

// A padding reads a number as lines and a string as a span: a whole number up to
// logs.MaxPageSize, or a duration of days and less up to logs.MaxPaddingSpan.
func TestParseReadsAPadding(t *testing.T) {
	t.Parallel()
	for raw, want := range map[string]logs.Padding{
		`0`:      {},
		`100`:    {Lines: 100},
		`500`:    {Lines: 500},
		`"PT5M"`: {Span: 5 * time.Minute},
		`"P7D"`:  {Span: 7 * 24 * time.Hour},
	} {
		var pad logs.Padding
		_, err := Parse(json.RawMessage(webapp+`,"before":`+raw+`}`), Fields{"before": &pad})
		require.NoError(t, err, raw)
		assert.Equal(t, want, pad, raw)
	}

	want := `{"error":"bad-input","field":"before","message":"a number of lines from 0 to 500, or an ISO 8601 duration such as PT5M of at most P7D"}`
	for _, raw := range []string{`-1`, `2.5`, `501`, `"P1M"`, `"P1Y"`, `"P7DT1S"`, `"5m"`, `""`, `true`, `null`} {
		var pad logs.Padding
		_, err := Parse(json.RawMessage(webapp+`,"before":`+raw+`}`), Fields{"before": &pad})
		assert.Equal(t, want, Refusal(err), raw)
	}

	var pad logs.Padding
	_, err := Parse(json.RawMessage(webapp+`,"before":`), Fields{"before": &pad})
	assert.Equal(t, want, Refusal(err), "the input ends before the padding")
}

// An input error names the field it concerns, or says the input is not the tool's.
func TestAnInputErrorSaysWhatItConcerns(t *testing.T) {
	t.Parallel()
	assert.EqualError(t, BadInput("limit", "too big"), "logs input has a bad limit")
	assert.EqualError(t, &InputError{Source: -1}, "logs input is not one object of the tool's own fields")
}

// A field whose destination is not one Fields reads into is a bug in the tool.
func TestAFieldOfAnotherTypePanics(t *testing.T) {
	t.Parallel()
	var n int64
	assert.Panics(t, func() {
		_, _ = Parse(json.RawMessage(`{"limit":1}`), Fields{"limit": &n})
	})
}

// A call may name no sources: a continuation names none, so each tool says
// whether it needs them.
func TestParseTakesNoSources(t *testing.T) {
	t.Parallel()
	for _, raw := range []string{`{}`, `{"sources":[]}`} {
		in, err := Parse(json.RawMessage(raw), nil)
		require.NoError(t, err, raw)
		assert.Empty(t, in.Sources, raw)
	}
}

// A truncated document names no source, wherever the Go release's decoder
// reports the syntax error; a field it ends inside is still named.
func TestATruncatedDocumentNamesNoSource(t *testing.T) {
	t.Parallel()
	source := `{"sources":[{"namespace":"prod","resource":"pods/a"}`
	for _, raw := range []string{source, source + ` `, source + `,`, `{"sources":[{"namespace":"prod"`, `{"sources":[`} {
		_, err := Parse(json.RawMessage(raw), nil)
		assert.Equal(t, `{"error":"bad-input"}`, Refusal(err), raw)
	}
	_, err := Parse(json.RawMessage(`{"filters":{"node":["a"`), nil)
	assert.Equal(t, `{"error":"bad-input","field":"filters.node"}`, Refusal(err))
}
