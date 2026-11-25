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

import (
	"context"
	"errors"
	"log/slog"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/amorey/gochan/watch"
)

// The named errors of a lookup.
var (
	// ErrUnknownModel is a provider the service does not hold, or a model its
	// catalog lacks.
	ErrUnknownModel = errors.New("llm: unknown model")
	// ErrBadEffort is a level the model does not list. A model with no efforts
	// takes the empty string alone.
	ErrBadEffort = errors.New("llm: effort not listed by the model")
	// ErrToolsUnsupported is an offer the target cannot carry: a model that takes
	// none, a native tool its wire cannot spell, or a cap that does not fit who
	// runs the tool.
	// Refused before anything is sent, so a misconfiguration fails loudly.
	ErrToolsUnsupported = errors.New("llm: this target does not carry these tools")
)

// Service is the providers the app holds. The list is fixed, in picker order;
// a provider's catalog can change (SetCatalog).
type Service struct {
	mu        sync.Mutex
	providers []Provider
	hub       *watch.Hub[[]Provider]
	tx        *watch.Sender[[]Provider]
}

// New holds the providers in the order given, which is the picker's. A row
// with both a key and a signer is a programming error.
func New(providers ...Provider) *Service {
	for _, p := range providers {
		if p.Key != "" && p.Signer != nil {
			panic("llm: provider " + p.ID + " has both a key and a signer")
		}
	}
	hub := watch.New(providers)
	return &Service{providers: providers, hub: hub, tx: hub.Sender()}
}

// Provider is the provider under id.
func (s *Service) Provider(id string) (Provider, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	i := s.indexLocked(id)
	if i < 0 {
		return Provider{}, false
	}
	return s.providers[i], true
}

func (s *Service) indexLocked(id string) int {
	return slices.IndexFunc(s.providers, func(p Provider) bool { return p.ID == id })
}

// SetCatalog replaces the models of the provider under id, which stays held
// with none. An id the service does not hold, or a catalog that has not
// changed, sends nothing. The service keeps its own copy, so the caller's
// slice is never shared.
func (s *Service) SetCatalog(id string, models []Model) {
	if len(models) == 0 {
		models = nil
	}
	models = slices.Clone(models)
	s.mu.Lock()
	defer s.mu.Unlock()
	i := s.indexLocked(id)
	if i < 0 || reflect.DeepEqual(s.providers[i].Catalog, models) {
		return
	}
	s.providers = slices.Clone(s.providers)
	s.providers[i].Catalog = models
	s.tx.Send(s.providers) //nolint:errcheck // Send never blocks, and the hub is never closed
}

// WatchProvidersState is a state watch of the providers: the current list, then the list after
// each SetCatalog.
func (s *Service) WatchProvidersState() (<-chan []Provider, func()) {
	rx := s.hub.Receiver()
	return rx.Chan(), rx.Close
}

// Target is what a send is aimed at: the provider and one model of its catalog.
type Target struct {
	Provider Provider
	Model    Model
	// Effort is the level the send named, checked against the model: one it lists,
	// or "" for a model that lists none.
	Effort string
}

// Takes reports whether the target's wire can spell tool, run by the provider
// when server. Which tools a provider is offered is the catalog's. No wire
// spells a client tool yet; the fake has no wire and takes any tool.
func (t Target) Takes(tool NativeTool, server bool) bool {
	_, ok := t.spells(tool, server)
	return ok
}

// spells reports whether the target's wire can spell tool, run by the provider
// when server, and the name its calls arrive under there: none on the fake,
// which has no wire.
func (t Target) spells(tool NativeTool, server bool) (callName string, ok bool) {
	switch t.Provider.Dialect {
	case DialectFake:
		return "", true
	case DialectMessages:
		if s, ok := tool.(MessagesServerTool); ok && server {
			return s.MessagesCallName(), true
		}
	}
	return "", false
}

// Stream sends one turn to the target, on the wire its provider's dialect names,
// and returns the response once the model has finished. The request names the
// target's own provider and model, so a caller cannot aim a request at one target
// and send it on another. affinityKey names the conversation the messages are
// (Request.AffinityKey). tools is what the model may ask for, and native the
// vendor tools offered beside them: an offer the target cannot carry is
// ErrToolsUnsupported, nothing sent, and a request that offers nothing replays
// no round. emit is called with each chunk as it arrives, on the calling
// goroutine. An error means the response is incomplete: what was streamed is
// what there is.
func (t Target) Stream(ctx context.Context, systemPrompt string, messages []Message, affinityKey string, tools []ToolDefinition, native []NativeOffer, emit func(Chunk)) (Response, error) {
	if (len(tools) > 0 || len(native) > 0) && !t.Model.Tools {
		return Response{}, ErrToolsUnsupported
	}
	servers := 0
	for _, o := range native {
		capped := o.MaxUses >= 1
		if !t.Takes(o.Tool, o.Server) || capped != o.Server {
			return Response{}, ErrToolsUnsupported
		}
		if o.Server {
			servers++
		}
	}
	if !t.distinct(tools, native) {
		return Response{}, ErrToolsUnsupported
	}
	messages, bad := disownUnreplayable(messages)
	if len(bad) > 0 {
		// Warn, not Debug: a stored row the app wrote can no longer be replayed whole.
		slog.Warn("llm: stored payloads that are not objects; their messages go as foreign",
			"provider", t.Provider.ID, "messages", strings.Join(bad, "; "))
	}
	switch {
	case len(tools) == 0 && len(native) == 0:
		// The Messages API refuses a history holding tool blocks unless tools is
		// defined, and one rule serves every wire.
		messages = eachMessage(messages, WithoutRounds)
	case servers == 0:
		// The same holds for a server call: it goes back only beside the tool
		// that made it, which a turn stops offering once its budget is spent.
		messages = eachMessage(messages, withoutServerCalls)
	}
	req := Request{
		Provider: t.Provider, Model: t.Model, Effort: t.Effort, SystemPrompt: systemPrompt, Messages: messages,
		AffinityKey: affinityKey, Tools: tools, NativeTools: native,
	}
	switch t.Provider.Dialect {
	case DialectMessages:
		return streamMessages(ctx, req, messagesIdle, wallClock, emit)
	case DialectResponses:
		return streamResponses(ctx, req, responsesIdle, wallClock, emit)
	case DialectChatCompletions:
		return streamChatCompletions(ctx, req, chatCompletionsIdle, wallClock, emit)
	case DialectFake:
		return t.Provider.fake.Stream(ctx, req, emit)
	}
	// Resolve refuses a dialect Stream does not speak before a Target exists.
	return Response{}, ErrUnknownModel
}

// distinct reports whether no two offers share a name and no name the wire
// reads a call by is spelled twice: a definition's, or a native offer's call
// name on the target's wire. Either would leave a call matching two tools.
func (t Target) distinct(tools []ToolDefinition, native []NativeOffer) bool {
	names := map[string]bool{}
	spelled := map[string]bool{}
	for _, d := range tools {
		if names[d.Name] {
			return false
		}
		names[d.Name], spelled[d.Name] = true, true
	}
	for _, o := range native {
		if names[o.Tool.Name()] {
			return false
		}
		names[o.Tool.Name()] = true
		call, _ := t.spells(o.Tool, o.Server)
		if call == "" {
			continue
		}
		if spelled[call] {
			return false
		}
		spelled[call] = true
	}
	return true
}

// disownUnreplayable is msgs with the writer cleared on every message holding a
// payload that is not an object, which no API takes back as its item. Foreign to
// every provider, the message goes with none of its payloads: the payloads of a
// row pair up, and an API refuses a pair with one side missing. bad names each
// such message by index and the types of those blocks, never their content. A
// copy when a message changed: the caller's slice is not written to.
func disownUnreplayable(msgs []Message) (out []Message, bad []string) {
	out = msgs
	for i, m := range msgs {
		var types []string
		for _, b := range m.Blocks {
			if !b.replayable() {
				types = append(types, string(b.Type))
			}
		}
		if len(types) == 0 {
			continue
		}
		if len(bad) == 0 {
			out = slices.Clone(msgs)
		}
		out[i].ProviderID, out[i].Dialect = "", ""
		bad = append(bad, strconv.Itoa(i)+" "+strings.Join(types, ","))
	}
	return out, bad
}

// eachMessage is msgs with keep applied to every message's blocks. A copy: the
// caller's slices are not written to, and keep must not write to them either.
func eachMessage(msgs []Message, keep func([]Block) []Block) []Message {
	out := slices.Clone(msgs)
	for i := range out {
		out[i].Blocks = keep(out[i].Blocks)
	}
	return out
}

// spoken reports whether Stream has a case for d, and for DialectFake whether the
// provider holds a Fake to answer from.
func spoken(p Provider) bool {
	switch p.Dialect {
	case DialectMessages, DialectResponses, DialectChatCompletions:
		return true
	case DialectFake:
		return p.fake != nil
	}
	return false
}

// Resolve is what a send runs on, once the model is in the provider's catalog and
// the effort is one the model lists. The target's provider carries the model's
// overrides (Provider.ForModel). A model on a dialect nothing speaks cannot run.
//
// A target on a SessionSigner's provider holds the signer bound to the current
// session, so it never signs under a session that replaced its own.
func (s *Service) Resolve(providerID, modelID, effort string) (Target, error) {
	t, err := s.resolve(providerID, modelID, effort)
	if err != nil {
		return Target{}, err
	}
	if ss, ok := t.Provider.Signer.(SessionSigner); ok {
		t.Provider.Signer = ss.Bind()
	}
	return t, nil
}

// ResolveUnder is Resolve for a subagent of parent, on parent's provider: it
// keeps parent's bound signer, so a subagent signs under its parent's session.
func (s *Service) ResolveUnder(parent Target, modelID, effort string) (Target, error) {
	t, err := s.resolve(parent.Provider.ID, modelID, effort)
	if err != nil {
		return Target{}, err
	}
	t.Provider.Signer = parent.Provider.Signer
	return t, nil
}

func (s *Service) resolve(providerID, modelID, effort string) (Target, error) {
	p, ok := s.Provider(providerID)
	if !ok {
		return Target{}, ErrUnknownModel
	}
	i := slices.IndexFunc(p.Catalog, func(m Model) bool { return m.ID == modelID })
	if i < 0 {
		return Target{}, ErrUnknownModel
	}
	m := p.Catalog[i]
	p = p.ForModel(m)
	if !spoken(p) {
		return Target{}, ErrUnknownModel
	}
	if !effortIsListed(m, effort) {
		return Target{}, ErrBadEffort
	}
	return Target{Provider: p, Model: m, Effort: effort}, nil
}

// effortIsListed holds for a level the model lists, and, for a model with no such
// knob, for the empty string alone.
func effortIsListed(m Model, effort string) bool {
	if len(m.Efforts) == 0 {
		return effort == ""
	}
	return slices.Contains(m.Efforts, effort)
}
