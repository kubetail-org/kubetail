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

package catalog

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/llm"
)

const vertexBase = "https://aiplatform.googleapis.com/v1/projects/my-project/locations/global/endpoints/openapi"

// The Google row is listed after ChatGPT's and before the fake when the
// project's endpoint is given, signed by the service, with no models until it
// connects.
func TestTheGoogleRowIsListedWhenConfigured(t *testing.T) {
	creds := newCredentials(t, credentials{vertexBaseURL: vertexBase})

	providers := New(Config{Credentials: creds, VertexBaseURL: vertexBase, Fake: llm.NewFake(0)}).Providers()

	n := len(providers)
	assert.Equal(t, []string{"chatgpt", "google", "fake"}, providerIDs(providers[n-3:]))
	p := providers[n-2]
	assert.Equal(t, "Vertex AI", p.Label)
	assert.Equal(t, llm.DialectChatCompletions, p.Dialect)
	assert.Equal(t, vertexBase, p.BaseURL)
	assert.Equal(t, creds.Signer(GoogleID), p.Signer)
	assert.Empty(t, p.Key)
	assert.Empty(t, p.Catalog)
}

// Connected at launch, the row lists its written catalog at once.
func TestAConnectedGoogleRowListsItsCatalog(t *testing.T) {
	creds := newCredentials(t, credentials{vertexBaseURL: vertexBase, connected: []string{GoogleID}})

	providers := New(Config{Credentials: creds, VertexBaseURL: vertexBase}).Providers()

	assert.Equal(t, newGoogleProvider(vertexBase).Catalog, providers[len(providers)-1].Catalog)
}

// With no project there is no endpoint to sign: the Google provider is left out.
func TestTheGoogleRowIsLeftOut(t *testing.T) {
	providers := New(Config{Credentials: newKeys(t, nil, nil)}).Providers()
	assert.NotContains(t, providerIDs(providers), GoogleID)
}

// gemini and google are two providers: a Gemini key does not hide Vertex AI.
func TestTheGoogleRowIsListedBesideAGeminiKey(t *testing.T) {
	creds := newCredentials(t, credentials{keys: map[string]string{"gemini": "gm-0123456789abcdef"}, vertexBaseURL: vertexBase})
	providers := New(Config{Credentials: creds, VertexBaseURL: vertexBase}).Providers()
	assert.Contains(t, providerIDs(providers), "gemini")
	assert.Contains(t, providerIDs(providers), GoogleID)
}

// A debug base lists the row with no project found.
func TestADebugBuildMovesTheGoogleRow(t *testing.T) {
	baseURLs := map[string]string{GoogleID: "http://127.0.0.1:1/v1"}
	providers := New(Config{Credentials: newKeys(t, nil, baseURLs), BaseURLs: baseURLs}).Providers()

	p := providers[len(providers)-1]
	require.Equal(t, GoogleID, p.ID)
	assert.Equal(t, "http://127.0.0.1:1/v1", p.BaseURL)
}

// The row's turns are offered our tools, and nothing of a vendor's.
func TestTheGoogleRowIsOfferedOurTools(t *testing.T) {
	assert.Equal(t, ours, New(Config{}).ToolsFor(llm.Target{Provider: llm.Provider{ID: GoogleID}}))
}
