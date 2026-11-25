// Copyright 2024 The Kubetail Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
package llm

// Model is one catalog entry: what a picker shows and what a send may name.
type Model struct {
	ID    string
	Label string
	// Efforts are the levels the model accepts, in the provider's own words, least
	// to most. Empty for a model with no such knob. Each provider has its own scale.
	Efforts []string
	// DefaultEffort is the level a picker starts on. Empty when Efforts is.
	DefaultEffort string
	// MaxOutputTokens is the cap every request asks for. A dialect that requires
	// one refuses zero.
	MaxOutputTokens int
	// ContextWindow is the most the model reads in one request, input and output
	// together, in its provider's tokens. Zero when the catalog does not state it:
	// such a model is never refused ahead, and its provider's refusal ends the chat
	// on that model.
	ContextWindow int
	// Tools says the model takes an offer of tools. Every Messages and Responses
	// model does. Chat Completions is every vendor's wire, and whether a model on
	// it honours the field is the vendor's word, per entry.
	Tools bool
	// Cache is how the provider caches this model's prompt prefix. The zero value
	// is no statement, and no entry keeps it. It lives on the model because one
	// provider can serve both kinds.
	Cache CacheKind
	// Dialect and BaseURL override the provider's defaults, for a provider that
	// serves each model on its native API. Empty keeps the provider's.
	Dialect Dialect
	BaseURL string
}

// CacheKind is how a provider caches a model's prompt prefix.
type CacheKind string

const (
	// CacheAuto is a provider that caches on its own.
	CacheAuto CacheKind = "auto"
	// CacheMarks is a provider that caches only what the request marks.
	CacheMarks CacheKind = "marks"
	// CacheNone is a model its provider documents no cache for.
	CacheNone CacheKind = "none"
)
