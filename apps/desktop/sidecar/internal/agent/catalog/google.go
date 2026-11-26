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
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/llm"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/connectors/google"
)

// GoogleID is the Google connector's provider id: Vertex AI in the user's
// project, a row apart from gemini, whose key and endpoint are the Gemini API's.
const GoogleID = google.ID

// newGoogleProvider is Vertex AI's OpenAI-compatible endpoint in the user's
// project, signed by the Google connector. Its catalog is the gemini row's
// models under Vertex AI's publisher ids. That Vertex AI serves each id and
// effort is UNVERIFIED.
func newGoogleProvider(vertexBaseURL string) llm.Provider {
	models := newGeminiProvider().Catalog
	for i := range models {
		models[i].ID = "google/" + models[i].ID
	}
	return llm.Provider{
		ID: GoogleID, Label: "Vertex AI", Dialect: llm.DialectChatCompletions,
		BaseURL: vertexBaseURL,
		Catalog: models,
	}
}
