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

// Package llm is a call to a model: what a model is asked, what it answers, and
// the shape an answer is stored in. Providers are data: the catalog package
// lists them, and the app hands them to New.
//
// Files are grouped by prefix: dialect_*.go speaks one wire, as the function
// Target.Stream calls for that dialect, and those and helpers.go are the only
// files of the package that import a model SDK.
package llm

// Dialect is the wire a provider is spoken to on, named for the API and never
// the vendor: a platform serves the same wire as the vendor that defined it.
type Dialect string

const (
	DialectMessages        Dialect = "messages"        // Anthropic Messages API
	DialectResponses       Dialect = "responses"       // OpenAI Responses API
	DialectChatCompletions Dialect = "chatcompletions" // OpenAI Chat Completions API
	DialectFake            Dialect = "fake"
)

// Dialects is every dialect, in one place, so a test can walk the set.
var Dialects = []Dialect{DialectMessages, DialectResponses, DialectChatCompletions, DialectFake}
