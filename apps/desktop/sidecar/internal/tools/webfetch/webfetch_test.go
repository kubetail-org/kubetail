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

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/tools"
)

// The input is one url, a non-empty string, spelled exactly once.
func TestWebFetchReadsItsInput(t *testing.T) {
	got, err := parse([]byte(`{"url":"https://a.test/"}`))
	assert.NoError(t, err)
	assert.Equal(t, "https://a.test/", got)

	for _, raw := range []string{
		`{}`,
		`{"url":""}`,
		`{"url":"https://a.test/","url":"https://b.test/"}`,
		`{"url":1}`,
		`{"url":null}`,
		`{"url":"https://a.test/","format":"markdown"}`,
		`{"url":"https://a.test/"} {}`,
		`{"url":}`,
		`{"url":"https://a.test/"`,
		`[]`,
		``,
	} {
		_, err := parse([]byte(raw))
		assert.Error(t, err, raw)
	}
}

// A URL refused by name is never asked about: Run tells the model why, and no
// one is shown a call that cannot happen. Anything else is asked, with no cwd.
func TestWebFetchRefusesByName(t *testing.T) {
	tl := &Tool{}
	for raw, want := range map[string]string{
		"ftp://a.test/":                               "WebFetch fetches http and https URLs only.",
		"https://user@a.test/":                        "WebFetch sends no credentials in a URL.",
		"https://localhost./":                         "WebFetch does not fetch localhost or other hostnames without a dot.",
		"https://127.0.0.1/":                          "WebFetch does not reach local or private addresses.",
		"https://[::1]/":                              "WebFetch does not reach local or private addresses.",
		"https://0x7f.1/":                             "WebFetch does not read a hostname ending in a number. Call again with the IP address written in full.",
		"https://a.test/" + strings.Repeat("x", 8192): "WebFetch takes a URL of at most 8,192 bytes.",
	} {
		got, err := tl.Approval(t.Context(), tools.Runtime{}, call(raw))
		require.NoError(t, err, raw)
		assert.Equal(t, tools.Approval{Skip: true}, got, raw)

		text, isError := tl.Run(t.Context(), tools.Runtime{}, call(raw))
		assert.True(t, isError, raw)
		assert.Equal(t, want, text, raw)
	}

	for _, raw := range []string{"https://a.test/", "http://a.test/", "https://[2606:4700::1111]/"} {
		got, err := tl.Approval(t.Context(), tools.Runtime{}, call(raw))
		require.NoError(t, err, raw)
		assert.Equal(t, tools.Approval{}, got, raw)
	}

	_, err := tl.Approval(t.Context(), tools.Runtime{}, []byte(`{"url":1}`))
	assert.Error(t, err)
	text, isError := tl.Run(t.Context(), tools.Runtime{}, []byte(`{"url":1}`))
	assert.True(t, isError)
	assert.Equal(t, `{"error":"bad-input"}`, text)
}

// call is a call's input fetching u.
func call(u string) json.RawMessage {
	b, _ := json.Marshal(map[string]string{"url": u})
	return b
}
