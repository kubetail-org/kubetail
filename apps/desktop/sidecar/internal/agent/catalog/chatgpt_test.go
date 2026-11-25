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
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/llm"
)

// planSigner signs with a fixed token and binds itself.
type planSigner struct{ token string }

func (s planSigner) Sign(r *http.Request) error {
	r.Header.Set("Authorization", "Bearer "+s.token)
	return nil
}
func (planSigner) Refused(*http.Request) {}
func (s planSigner) Bind() llm.Signer    { return s }

// The ChatGPT row is listed after the keyed providers and before the fake,
// signed by the service, with no models until its catalog is read.
func TestTheChatGPTRowIsListed(t *testing.T) {
	f := llm.NewFake(0)
	creds := newCredentials(t, credentials{connected: []string{ChatGPTID}})

	providers := New(Config{Credentials: creds, Fake: f}).Providers()

	n := len(providers)
	assert.Equal(t, []string{"opencode-go", "chatgpt", "fake"}, providerIDs(providers[n-3:]))
	p := providers[n-2]
	assert.Equal(t, "ChatGPT plan", p.Label)
	assert.Equal(t, llm.DialectResponses, p.Dialect)
	assert.Equal(t, "https://api.openai.com/v1", p.BaseURL)
	assert.Equal(t, creds.Signer(ChatGPTID), p.Signer)
	assert.Empty(t, p.Key)
	assert.Empty(t, p.Catalog)
	assert.Equal(t, []string{"max_output_tokens"}, p.Omit)
	assert.Equal(t, map[string]any{
		"include":           []string{"reasoning.encrypted_content"},
		"reasoning.summary": "auto",
	}, p.Extra)
	assert.Equal(t, llm.CodeUsageLimit, p.Codes["subscription_sharing_usage_limit_exceeded"])
	assert.Contains(t, p.Codes, "chatpass_v2_scope_not_authorized")
	assert.Contains(t, p.Codes, "chatpass_v2_invalid_authorization_context")

	assert.NotContains(t, providerIDs(New(Config{Fake: f}).Providers()), "chatgpt")
}

// A debug build moves the row as it moves a keyed one.
func TestADebugBuildMovesTheChatGPTRow(t *testing.T) {
	baseURLs := map[string]string{"chatgpt": "http://127.0.0.1:1/v1"}
	providers := New(Config{Credentials: newKeys(t, nil, baseURLs), BaseURLs: baseURLs}).Providers()

	assert.Equal(t, ChatGPTID, providers[11].ID)
	assert.Equal(t, "http://127.0.0.1:1/v1", providers[11].BaseURL)
}

// The row's turns are offered our tools, and nothing of a vendor's.
func TestTheChatGPTRowIsOfferedOurTools(t *testing.T) {
	assert.Equal(t, ours, New(Config{}).ToolsFor(llm.Target{Provider: llm.Provider{ID: "chatgpt"}}))
}

// A model is listed when the account lists it, in the server's order, under
// its slug and its display name. One the openai row has takes the rest of that
// entry; one it lacks is the fallback.
func TestChatGPTModelsAreTheAccountsList(t *testing.T) {
	body := []byte(`{"models":[
		{"slug":"gpt-6.1-sol","display_name":"GPT-6.1 Sol","visibility":"list"},
		{"slug":"internal-preview","display_name":"Hidden","visibility":"hide"},
		{"slug":"gpt-5.6","display_name":"GPT-5.6 on your plan","visibility":"list"}
	]}`)

	models := chatGPTModels(body)

	require.Len(t, models, 2)
	assert.Equal(t, llm.Model{
		ID: "gpt-6.1-sol", Label: "GPT-6.1 Sol", MaxOutputTokens: writtenCap, Tools: true, Cache: llm.CacheAuto,
	}, models[0])
	openai := newOpenAIProvider().Catalog[1]
	require.Equal(t, "gpt-5.6", openai.ID)
	openai.Label = "GPT-5.6 on your plan"
	assert.Equal(t, openai, models[1])
}

// A slug listed twice keeps its first entry; a model with no display name is
// labelled with its slug.
func TestChatGPTModelsListASlugOnceUnderAName(t *testing.T) {
	body := []byte(`{"models":[
		{"slug":"m","display_name":"First","visibility":"list"},
		{"slug":"m","display_name":"Second","visibility":"list"},
		{"slug":"n","visibility":"list"}
	]}`)

	models := chatGPTModels(body)

	require.Len(t, models, 2)
	assert.Equal(t, "First", models[0].Label)
	assert.Equal(t, "n", models[1].Label)
}

// A body of another shape lists nothing; an entry with no slug is skipped.
func TestChatGPTModelsOfAnotherShapeListNothing(t *testing.T) {
	for _, body := range []string{`{"data":[{"id":"gpt-5.6"}]}`, `[]`, `not json`, `{"models":[{"display_name":"x","visibility":"list"}]}`} {
		assert.Empty(t, chatGPTModels([]byte(body)), body)
	}
}

// The catalog is read from the account's GET /models, signed, through a client
// that follows no redirect. Models hands the reader the provider it was called
// with, which carries the signer and the base; the table's own row has neither.
func TestReadChatGPTModelsSignsTheRequest(t *testing.T) {
	var auth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/v1/models", r.URL.Path)
		auth = r.Header.Get("Authorization")
		fmt.Fprint(w, `{"models":[{"slug":"m","display_name":"M","visibility":"list"}]}`)
	}))
	defer srv.Close()

	models, err := Models(nil, "")(context.Background(), planRow(planSigner{token: "t"}, srv.URL+"/v1"))

	require.NoError(t, err)
	assert.Equal(t, "Bearer t", auth)
	require.Len(t, models, 1)
	assert.Equal(t, "m", models[0].ID)
}

// A refusal, a redirect and a failed signer are each an error, and a 401 tells
// the signer.
func TestReadChatGPTModelsFails(t *testing.T) {
	var elsewhere int
	target := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { elsewhere++ }))
	defer target.Close()
	for _, status := range []int{http.StatusUnauthorized, http.StatusTemporaryRedirect} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, target.URL, status)
		}))
		signer := &refusalCounter{}

		_, err := ReadChatGPTModels(context.Background(), planRow(signer, srv.URL+"/v1"))

		assert.Error(t, err)
		assert.Equal(t, status == http.StatusUnauthorized, signer.refused == 1)
		srv.Close()
	}
	assert.Zero(t, elsewhere)

	_, err := ReadChatGPTModels(context.Background(), planRow(failingSigner{}, "http://127.0.0.1:1/v1"))
	assert.ErrorIs(t, err, llm.ErrSignedOut)
}

// planRow is the chatgpt row at baseURL, signed by s.
func planRow(s llm.Signer, baseURL string) llm.Provider {
	p := newChatGPTProvider()
	p.BaseURL, p.Signer = baseURL, s
	return p
}

type refusalCounter struct{ refused int }

func (*refusalCounter) Sign(*http.Request) error { return nil }
func (c *refusalCounter) Refused(*http.Request)  { c.refused++ }

type failingSigner struct{}

func (failingSigner) Sign(*http.Request) error { return llm.ErrSignedOut }
func (failingSigner) Refused(*http.Request)    {}

// An address that is no URL, one nothing answers, and a body cut short are each
// an error.
func TestReadChatGPTModelsFailsToReach(t *testing.T) {
	signer := planSigner{token: "t"}
	_, err := ReadChatGPTModels(context.Background(), planRow(signer, "://bad"))
	assert.Error(t, err)

	_, err = ReadChatGPTModels(context.Background(), planRow(signer, "http://127.0.0.1:1/v1"))
	assert.Error(t, err)

	short := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Length", "100")
		fmt.Fprint(w, `{"models":`)
	}))
	defer short.Close()
	_, err = ReadChatGPTModels(context.Background(), planRow(signer, short.URL+"/v1"))
	assert.Error(t, err)
}
