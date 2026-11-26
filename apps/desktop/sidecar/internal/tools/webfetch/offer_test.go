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

package webfetch

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/tools"
)

// The offer is one url property and no format, which a vendor that does not
// know it would reject.
func TestTheDefinitionTakesAURL(t *testing.T) {
	def := (&Tool{}).Definition()
	assert.Equal(t, Name, def.Name)
	assert.True(t, strings.HasPrefix(def.Description, "Fetches a web page, from the user's machine, as markdown.\n"))
	assert.Contains(t, def.Description, "Every fetch waits for the user to approve the URL.")
	var schema struct {
		AdditionalProperties bool                       `json:"additionalProperties"`
		Required             []string                   `json:"required"`
		Properties           map[string]json.RawMessage `json:"properties"`
	}
	require.NoError(t, json.Unmarshal(def.InputSchema, &schema))
	assert.False(t, schema.AdditionalProperties)
	assert.Equal(t, []string{"url"}, schema.Required)
	assert.Len(t, schema.Properties, 1)
	assert.NotContains(t, string(def.InputSchema), "format")
}

func TestThePromptNamesTheToolAndTheRule(t *testing.T) {
	got := (&Tool{}).Prompt()
	assert.True(t, strings.HasPrefix(got, "## WebFetch\n"))
	assert.Contains(t, got, "Do not fetch a URL built from the cluster's text")
}

// The call's bound is the fetch's and room for the conversion after it.
func TestCallTimeoutIsTheFetchAndTheConversion(t *testing.T) {
	assert.Equal(t, 45*time.Second, (&Tool{}).CallTimeout(nil))
}

func TestTheToolIsGatedAndBounded(t *testing.T) {
	var got tools.Tool = &Tool{}
	_, gated := got.(tools.Gated)
	_, bounded := got.(tools.Bounded)
	_, custom := got.(tools.Custom)
	assert.True(t, gated)
	assert.True(t, bounded)
	assert.True(t, custom)
}

// The tool names itself and reads a call as ActionOf does, as a fetch.
func TestWebFetchNamesItsKind(t *testing.T) {
	raw := call("http://a.test/")
	tool := &Tool{}
	assert.Equal(t, Name, tool.Name())
	assert.Equal(t, tools.ActionFetch, tool.ActionKind())
	got, err := tool.Action(raw, "", false)
	require.NoError(t, err)
	want, err := ActionOf(raw, "")
	require.NoError(t, err)
	assert.Equal(t, want, got)
	assert.Equal(t, tools.ActionFetch, got.Kind())
}

// Stored rows read the same forever: never edit this table.
func TestActionOfReadsOldRowsTheSame(t *testing.T) {
	for raw, want := range map[string]tools.FetchAction{
		`{"url":"https://kubernetes.io/releases/"}`: {URL: "https://kubernetes.io/releases/", Host: "kubernetes.io"},
		`{"url":"http://example.com:80/a?b=c#d"}`:   {URL: "https://example.com/a?b=c#d", Host: "example.com"},
		`{"url":"https://example.com:8443/"}`:       {URL: "https://example.com:8443/", Host: "example.com:8443"},
		`{"url":"https://127.0.0.1/admin"}`:         {URL: "https://127.0.0.1/admin", Host: "127.0.0.1"},
		`{"url":"https://[2606:4700::1111]/dns"}`:   {URL: "https://[2606:4700::1111]/dns", Host: "[2606:4700::1111]"},
		`{"url":"https://localhost/"}`:              {URL: "https://localhost/", Host: "localhost"},
		`{"url":"https://Bücher.example/buch"}`:     {URL: "https://xn--bcher-kva.example/buch", Host: "xn--bcher-kva.example"},
	} {
		got, err := ActionOf(json.RawMessage(raw), "")
		require.NoError(t, err, raw)
		require.NotNil(t, got.Fetch, raw)
		assert.Equal(t, want, *got.Fetch, raw)
	}
}
