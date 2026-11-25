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

package llm

// Citation is one citation a text block makes, in the app's words for what every
// provider's citations share. The text block's payload stays the verbatim record.
type Citation struct {
	// Type is the provider's word for the kind: web_search_result_location,
	// page_location.
	Type string `json:"type"`
	// URL is the source's, "" for a source with none.
	URL string `json:"url"`
	// Title is the page's or the document's.
	Title string `json:"title"`
	// CitedText is the passage the text rests on.
	CitedText string `json:"citedText"`
}

// Citations is the citations blocks make, in order, read from their payloads by
// the dialect's own reader. The dialect is the run's that wrote them, never looked
// up by its provider: a provider whose key is gone is no longer held. A payload
// that does not decode gives nothing, since a reader is owed the message whatever
// one block holds.
func Citations(d Dialect, blocks []Block) []Citation {
	switch d {
	case DialectMessages:
		return messagesCitations(blocks)
	case DialectFake:
		return fakeCitations(blocks)
	}
	// Neither OpenAI wire records a citation yet.
	return nil
}
