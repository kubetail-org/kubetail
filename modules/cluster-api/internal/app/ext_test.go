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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestExtensionGroupDiscovery(t *testing.T) {
	app, _ := NewTestApp(t, nil)

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/apis", nil)
	app.ServeHTTP(w, r)

	require.Equal(t, http.StatusOK, w.Result().StatusCode)

	var got metav1.APIGroup
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
	assert.Equal(t, "api.kubetail.com", got.Name)
	require.Len(t, got.Versions, 1)
	assert.Equal(t, "api.kubetail.com/v1", got.Versions[0].GroupVersion)
	assert.Equal(t, "v1", got.Versions[0].Version)
	assert.Equal(t, "api.kubetail.com/v1", got.PreferredVersion.GroupVersion)
	assert.Equal(t, "v1", got.PreferredVersion.Version)
}

func TestExtensionVersionDiscovery(t *testing.T) {
	app, _ := NewTestApp(t, nil)

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/apis/api.kubetail.com/v1", nil)
	app.ServeHTTP(w, r)

	require.Equal(t, http.StatusOK, w.Result().StatusCode)

	var got metav1.APIResourceList
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
	assert.Equal(t, "api.kubetail.com/v1", got.GroupVersion)
	assert.NotEmpty(t, got.APIResources, "discovery must advertise at least one resource so kube-apiserver accepts the registration")

	byName := map[string]metav1.APIResource{}
	for _, r := range got.APIResources {
		byName[r.Name] = r
	}
	if assert.Contains(t, byName, "graphql") {
		assert.Equal(t, "GraphQL", byName["graphql"].Kind)
		assert.ElementsMatch(t, []string{"get", "create"}, byName["graphql"].Verbs)
	}
	if assert.Contains(t, byName, "download") {
		assert.Equal(t, "Download", byName["download"].Kind)
		assert.ElementsMatch(t, []string{"create"}, byName["download"].Verbs)
	}
}
