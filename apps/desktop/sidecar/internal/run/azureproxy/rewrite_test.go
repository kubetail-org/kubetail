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

package azureproxy

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"testing/iotest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/run/runproxy"
)

const doorLink = "https://127.0.0.1:6443"

func TestALinkToTheHostNamesTheDoor(t *testing.T) {
	link := linkRewriter("management.azure.com", 6443)
	for in, want := range map[string]string{
		"https://management.azure.com/subscriptions/x?api-version=1": doorLink + "/subscriptions/x?api-version=1",
		"https://MANAGEMENT.AZURE.COM/subscriptions/x":               doorLink + "/subscriptions/x",
		"https://management.azure.com":                               doorLink,
		"https://management.azure.com:443/subscriptions/x":           doorLink + "/subscriptions/x",
		"https://management.azure.com:4430/x":                        "https://management.azure.com:4430/x",
		"https://management.azure.com.evil.example/x":                "https://management.azure.com.evil.example/x",
		"https://other.example/x":                                    "https://other.example/x",
		"/relative":                                                  "/relative",
	} {
		assert.Equal(t, want, link(in), in)
	}
}

func TestOnlyTheTopLevelNextLinkIsRewritten(t *testing.T) {
	link := linkRewriter("management.azure.com", 6443)
	for in, want := range map[string]string{
		`{"value": [{"name": "a", "nextLink": "https://management.azure.com/item"}], "nextLink": "https://management.azure.com/page2"}`: `{"value": [{"name": "a", "nextLink": "https://management.azure.com/item"}], "nextLink": "` + doorLink + `/page2"}`,
		`{"value": []}`: `{"value": []}`,
		`{"@odata.nextLink": "https://management.azure.com/page2"}`: `{"@odata.nextLink": "` + doorLink + `/page2"}`,
		`{"nextLink": null, "n": 1.50}`:                             `{"nextLink": null, "n": 1.50}`,
		`[{"nextLink": "https://management.azure.com/x"}]`:          `[{"nextLink": "https://management.azure.com/x"}]`,
		``:                                 ``,
		` {"value": {"x": [1]}, "a": "<"}`: `{"value": {"x": [1]}, "a": "<"}`,
	} {
		var out bytes.Buffer
		require.NoError(t, rewriteBody(&out, strings.NewReader(in), link), in)
		if want == "" {
			assert.Empty(t, out.String())
			continue
		}
		assert.JSONEq(t, want, out.String(), in)
	}
	var out bytes.Buffer
	assert.Error(t, rewriteBody(&out, strings.NewReader(`{"value": [1, `), link), "a body cut short")
	assert.Error(t, rewriteBody(&out, iotest.ErrReader(errors.New("reset")), link))
}

// The links an answer gives az name the door: the three headers and a paged
// body's nextLink. WWW-Authenticate is dropped.
func TestTheAnswersLinksNameTheDoor(t *testing.T) {
	h := newHarness(t, runproxy.Limits{})
	h.upstream.setAnswer(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Location", "https://management.azure.com/subscriptions/"+sub+"/operationresults/abc?api-version=1")
		w.Header().Set("Azure-AsyncOperation", "https://management.azure.com/subscriptions/"+sub+"/providers/Microsoft.Compute/locations/westus/operations/op")
		w.Header().Set("Operation-Location", "https://management.azure.com/x")
		w.Header().Set("WWW-Authenticate", `Bearer authorization_uri="https://login.example"`)
		w.Header().Set("Retry-After", "1")
		_, _ = io.WriteString(w, `{"value": [{"nextLink": "https://management.azure.com/item"}], "nextLink": "https://management.azure.com/subscriptions/`+sub+`/resourcegroups?page=2"}`)
	})
	w := serve(h.handler, h.request(http.MethodGet, "/subscriptions/"+sub+"/resourcegroups?api-version=1", ""))
	require.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, doorLink+"/subscriptions/"+sub+"/operationresults/abc?api-version=1", w.Header().Get("Location"))
	assert.Equal(t, doorLink+"/subscriptions/"+sub+"/providers/Microsoft.Compute/locations/westus/operations/op", w.Header().Get("Azure-AsyncOperation"))
	assert.Equal(t, doorLink+"/x", w.Header().Get("Operation-Location"))
	assert.Empty(t, w.Header().Values("WWW-Authenticate"))
	assert.Equal(t, "1", w.Header().Get("Retry-After"))
	assert.JSONEq(t, `{"value": [{"nextLink": "https://management.azure.com/item"}], "nextLink": "`+doorLink+`/subscriptions/`+sub+`/resourcegroups?page=2"}`, w.Body.String())
}

// A body that is not JSON passes as it is.
func TestABodyThatIsNotJSONPasses(t *testing.T) {
	h := newHarness(t, runproxy.Limits{})
	h.upstream.setAnswer(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = io.WriteString(w, "nextLink https://management.azure.com/x")
	})
	w := serve(h.handler, h.request(http.MethodGet, "/subscriptions?api-version=1", ""))
	assert.Equal(t, "nextLink https://management.azure.com/x", w.Body.String())
}

// failingWriter refuses every write.
type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("client gone") }

// A client gone partway ends the rewrite with its error.
func TestARewriteToAClientGoneFails(t *testing.T) {
	body := `{"value": ["` + strings.Repeat("x", 8<<10) + `"]}`
	assert.Error(t, rewriteBody(failingWriter{}, strings.NewReader(body), linkRewriter("management.azure.com", 6443)))
}

// An answer whose body fails partway is cut, never ended as if whole.
func TestABodyCutUpstreamIsCutForAz(t *testing.T) {
	h := newHarness(t, runproxy.Limits{})
	h.upstream.setAnswer(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Length", "1000")
		_, _ = io.WriteString(w, `{"value": [`)
	})
	assert.PanicsWithValue(t, http.ErrAbortHandler, func() {
		serve(h.handler, h.request(http.MethodGet, "/subscriptions?api-version=1", ""))
	})
}
