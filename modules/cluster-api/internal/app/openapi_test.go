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

package app

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// kube-apiserver fetches /openapi/v2 once an APIService is aggregated. We
// serve the embedded swagger.json so registration succeeds.
func TestOpenAPIV2(t *testing.T) {
	app, _ := NewTestApp(t, nil)

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/openapi/v2", nil)
	app.ServeHTTP(w, r)

	require.Equal(t, http.StatusOK, w.Result().StatusCode)

	var got map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
	assert.Equal(t, "2.0", got["swagger"], "OpenAPI v2 doc must have swagger=\"2.0\"")
}

// /openapi/v3 returns the index of per-GV specs that the kube-apiserver can
// aggregate. We advertise our single GV.
func TestOpenAPIV3Index(t *testing.T) {
	app, _ := NewTestApp(t, nil)

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/openapi/v3", nil)
	app.ServeHTTP(w, r)

	require.Equal(t, http.StatusOK, w.Result().StatusCode)

	var got struct {
		Paths map[string]any `json:"paths"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
	assert.Contains(t, got.Paths, "apis/api.kubetail.com/v1")
}

// /openapi/v3/apis/api.kubetail.com/v1 returns a minimal OpenAPI v3 doc for
// our group/version. The doc just needs to be valid; clients tolerate empty
// paths/components.
func TestOpenAPIV3GroupVersion(t *testing.T) {
	app, _ := NewTestApp(t, nil)

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/openapi/v3/apis/api.kubetail.com/v1", nil)
	app.ServeHTTP(w, r)

	require.Equal(t, http.StatusOK, w.Result().StatusCode)

	var got map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
	assert.Equal(t, "3.0.0", got["openapi"])
	info, _ := got["info"].(map[string]any)
	assert.Equal(t, "api.kubetail.com", info["title"])
	assert.Equal(t, "v1", info["version"])
}
