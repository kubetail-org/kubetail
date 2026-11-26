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

import "encoding/json"

// Message is one turn of a conversation as the model is shown it.
type Message struct {
	Role   string
	Blocks []Block
	// ProviderID and Dialect are the writer: the provider an assistant message
	// was written on and the wire it was spoken to on, off its run; empty on the
	// user's. Only the writer is sent the blocks' payloads.
	ProviderID string
	Dialect    Dialect
	// Effort is the level an assistant message was written at, off its run, and
	// "" for none. It is how a wire knows whether the row thought: a round with
	// no thinking block may be one the model chose not to think before. Whether
	// a listed level means thinking is the scale's call; the Responses scale
	// lists "none".
	Effort string
}

// foreign reports whether providerID, speaking dialect, did not write m. A
// signature is verified only by the API that signed it and ciphertext is read
// only by the keys it was encrypted to, so the key is the provider and the
// dialect both: a provider's other API is another reader. A message naming no
// writer is foreign to everyone: the user's, and one whose writer is unknown.
func (m Message) foreign(providerID string, dialect Dialect) bool {
	return m.ProviderID != providerID || m.Dialect != dialect
}

// stripForeign is m's blocks as providerID may send them on dialect: with their
// payloads when it wrote the message there, without them otherwise. Every app
// field stays; losing the other writer's thinking is what a switch costs.
func stripForeign(providerID string, dialect Dialect, m Message) []Block {
	if m.foreign(providerID, dialect) {
		return WithoutPayloads(m.Blocks)
	}
	return m.Blocks
}

// ToolDefinition is a tool of the app's, sent as a function. Name is what a
// tool_use block carries and the box is looked up by.
type ToolDefinition struct {
	Name        string
	Description string
	InputSchema json.RawMessage // a JSON Schema object
}

// Request is one call to a model: what it is told, and what it may ask for.
type Request struct {
	// Provider is who the call goes to: where, and with which key. The key is
	// registered with safe, so a request in a log line renders it blank.
	Provider Provider
	// Model is the catalog entry: its ID goes on the wire, the rest shapes the call.
	Model Model
	// Effort is the level to think at, in the provider's own words, and empty for
	// a model that lists none: a wire asks for thinking exactly when it is set.
	Effort       string
	SystemPrompt string // empty sends none
	Messages     []Message
	// AffinityKey names the conversation, so a vendor that routes by one sends
	// its requests to the machine holding its cached prefix. A hint only: a wire
	// with no such field ignores it, and empty sends none.
	AffinityKey string
	// Tools is what the model may ask for, in offer order. Empty offers none.
	Tools []ToolDefinition
	// NativeTools is the vendor tools offered, in wire order. Empty offers none.
	NativeTools []NativeOffer
}

// NativeTool is a vendor's tool as a request offers it. A wire reaches the rest
// of it through its own interface (MessagesServerTool).
type NativeTool interface {
	// Name is the tool's identity: what the decoder writes into each call's block
	// and what usage is counted under. Unique among a request's offers.
	Name() string
}

// NativeOffer is one native tool on a request. MaxUses is the cap on a server
// tool, at least 1, and 0 for a client tool.
type NativeOffer struct {
	Tool    NativeTool
	Server  bool
	MaxUses int
}
