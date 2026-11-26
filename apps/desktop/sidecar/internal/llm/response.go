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

// ChunkKind is what a chunk is a piece of.
type ChunkKind int

const (
	ChunkText     ChunkKind = iota // the answer, and the zero value
	ChunkThinking                  // what the provider showed of its thinking
	// ChunkServer is a server call the provider made, sent once the call's input
	// is whole, whatever the input holds.
	ChunkServer
)

// Chunk is one piece of an answer as it streams.
type Chunk struct {
	Kind ChunkKind
	Text string
	// Call is a server chunk's call: its server_use block, with no payload.
	Call Block
}

// StopToolUse is the stop reason of a reply that asked for a tool: the one word
// the loop keys on, and what every wire that carries tools spells such a reply.
const StopToolUse = "tool_use"

// StopPauseTurn is the stop reason of a reply the provider paused in its own
// server tool loop; asked again with the reply as the last message, it resumes.
const StopPauseTurn = "pause_turn"

// Response is a finished answer.
type Response struct {
	Blocks     []Block
	StopReason string // the provider's own word, stored as it arrives
	// Model is the model the provider said it served, which can be a dated
	// snapshot of the one asked for; empty when it said none.
	Model string
	// ServerUses is how many times the provider ran each server tool, by the
	// tool's name, set only when its usage reports one: a reported zero is 0,
	// never absent.
	ServerUses map[string]int
	Usage      Usage
}

// Usage is what one request read and wrote, as its provider counted it.
type Usage struct {
	// Reported says the provider sent a usage object. A count a present report
	// omits is a known zero; with no report every count is unknown.
	Reported bool
	// InputTokens is everything the model read, the cached part included. Each
	// wire adds it up from its API's own fields.
	InputTokens      int
	CacheReadTokens  int // the part of InputTokens served from the cache
	CacheWriteTokens int // what this request wrote to the cache; zero where the API has no such charge
	OutputTokens     int
}
