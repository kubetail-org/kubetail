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
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/testutil"
)

// New holds the providers it is given, in the order given, and resolves a send
// on each by its id.
func TestNewHoldsTheProvidersGiven(t *testing.T) {
	assert.Empty(t, testutil.Current(t, New().WatchProvidersState, "the providers"))

	f := NewFake(0)
	chat := Provider{ID: "acme", Dialect: DialectChatCompletions, Catalog: []Model{{ID: "m"}}}
	s := New(chat, FakeProvider(f))

	assert.Equal(t, []string{"acme", "fake"}, providerIDs(testutil.Current(t, s.WatchProvidersState, "the providers")))
	target, err := s.Resolve("acme", "m", "")
	require.NoError(t, err)
	assert.Equal(t, "acme", target.Provider.ID)
	target, err = s.Resolve("fake", "fake", "low")
	require.NoError(t, err)
	assert.Same(t, f, target.Provider.fake)
}

// providerIDs is a service's providers in the order it lists them.
func providerIDs(providers []Provider) []string {
	ids := make([]string, 0, len(providers))
	for _, p := range providers {
		ids = append(ids, p.ID)
	}
	return ids
}

func TestTargetStreamsOnItsOwnProviderAndModel(t *testing.T) {
	f := NewFake(0)
	turns := []Message{{Role: "user", Blocks: []Block{TextBlock("How many?")}}}
	target := Target{Provider: FakeProvider(f), Model: Model{ID: "m"}}

	_, err := target.Stream(t.Context(), "You count pods.", turns, "", nil, nil, func(Chunk) {})

	require.NoError(t, err)
	assert.Equal(t, Request{Provider: target.Provider, Model: target.Model, SystemPrompt: "You count pods.", Messages: turns}, f.LastRequest())
}

func TestStreamSendsTheAffinityKeyItIsGiven(t *testing.T) {
	f := NewFake(0)
	target := Target{Provider: FakeProvider(f), Model: Model{ID: "m"}}

	_, err := target.Stream(t.Context(), "", nil, "chat-1", nil, nil, func(Chunk) {})

	require.NoError(t, err)
	assert.Equal(t, "chat-1", f.LastRequest().AffinityKey)
}

// A target on a dialect Stream does not speak is refused at the stream, the way
// Resolve refuses it before one exists.
func TestTargetRefusesADialectItDoesNotSpeak(t *testing.T) {
	target := Target{Provider: Provider{ID: "x", Dialect: Dialect("smoke-signals")}, Model: Model{ID: "m"}}

	_, err := target.Stream(t.Context(), "", nil, "", nil, nil, func(Chunk) { t.Error("a refused stream emitted a chunk") })

	assert.ErrorIs(t, err, ErrUnknownModel)
}

func TestResolveIsTheProviderAndItsModel(t *testing.T) {
	p := Provider{ID: "x", Dialect: DialectFake, fake: NewFake(0), Catalog: []Model{{ID: "m"}}}
	s := New(p)

	target, err := s.Resolve("x", "m", "")
	require.NoError(t, err)
	assert.Equal(t, Target{Provider: p, Model: Model{ID: "m"}}, target)
}

// An unknown provider, a model its catalog lacks, a provider on a dialect nothing
// speaks, and a fake provider with no Fake are one refusal: nothing could run on
// any of them.
func TestServiceRefusesAModelItDoesNotHold(t *testing.T) {
	s := New(
		Provider{ID: "x", Dialect: DialectFake, fake: NewFake(0), Catalog: []Model{{ID: "m"}}},
		Provider{ID: "unspoken", Dialect: Dialect("smoke-signals"), Catalog: []Model{{ID: "m"}}},
		Provider{ID: "nofake", Dialect: DialectFake, Catalog: []Model{{ID: "m"}}})

	for _, ref := range [][2]string{{"y", "m"}, {"x", "other"}, {"unspoken", "m"}, {"nofake", "m"}} {
		target, err := s.Resolve(ref[0], ref[1], "")
		assert.ErrorIs(t, err, ErrUnknownModel, ref)
		assert.Equal(t, Target{}, target, ref)
	}
}

// A model with efforts takes one of them; a model with none takes the empty string
// alone.
func TestServiceChecksTheEffortAgainstTheModel(t *testing.T) {
	s := New(Provider{ID: "x", Dialect: DialectFake, fake: NewFake(0), Catalog: []Model{
		{ID: "knob", Efforts: []string{"low", "high"}}, {ID: "plain"},
	}})

	for _, ok := range [][2]string{{"knob", "low"}, {"knob", "high"}, {"plain", ""}} {
		_, err := s.Resolve("x", ok[0], ok[1])
		assert.NoError(t, err, ok)
	}
	for _, bad := range [][2]string{{"knob", ""}, {"knob", "max"}, {"plain", "low"}} {
		_, err := s.Resolve("x", bad[0], bad[1])
		assert.ErrorIs(t, err, ErrBadEffort, bad)
	}
}

// The level a send named rides the target, and on to the request a wire is built
// from: a model with no such knob carries none.
func TestResolveCarriesTheEffort(t *testing.T) {
	f := NewFake(0)
	s := New(Provider{ID: "x", Dialect: DialectFake, fake: f, Catalog: []Model{
		{ID: "knob", Efforts: []string{"low", "high"}}, {ID: "plain"},
	}})

	knob, err := s.Resolve("x", "knob", "high")
	require.NoError(t, err)
	assert.Equal(t, "high", knob.Effort)
	_, err = knob.Stream(t.Context(), "", nil, "", nil, nil, func(Chunk) {})
	require.NoError(t, err)
	assert.Equal(t, "high", f.LastRequest().Effort)

	plain, err := s.Resolve("x", "plain", "")
	require.NoError(t, err)
	assert.Empty(t, plain.Effort)
}

func TestProviderIsTheRowUnderItsID(t *testing.T) {
	want := Provider{ID: "x", Label: "X"}
	s := New(want)

	p, ok := s.Provider("x")
	assert.True(t, ok)
	assert.Equal(t, want, p)
	_, ok = s.Provider("y")
	assert.False(t, ok)
}

// The providers come back in the order given: the first is what a picker starts on.
func TestServiceCatalogKeepsItsOrder(t *testing.T) {
	b, a := Provider{ID: "b"}, Provider{ID: "a"}
	s := New(b, a)

	assert.Equal(t, []Provider{b, a}, testutil.Current(t, s.WatchProvidersState, "the providers"))
}

// A provider's dialect is the name that selects the code: each one reaches its own
// wire, and the request it builds is the target's own.
func TestStreamSendsEachDialectOnItsOwnWire(t *testing.T) {
	for _, w := range wires() {
		t.Run(w.name, func(t *testing.T) {
			p, rec := w.replay(t, w.text)
			req := w.req(p)

			resp, err := Target{Provider: p, Model: req.Model}.Stream(t.Context(), req.SystemPrompt, req.Messages, "", nil, nil, func(Chunk) {})

			require.NoError(t, err)
			assert.NotEmpty(t, resp.StopReason)
			assert.Equal(t, int64(1), rec.calls.Load())
			assert.Equal(t, req.Model.ID, rec.body["model"])
		})
	}
}

// A model overriding its provider's dialect and base URL is sent on its
// override; a model of the same provider overriding nothing is sent on the
// provider's defaults. Each target carries the dialect it was sent on, which is
// what its run records, and the provider's one key goes in that dialect's header.
func TestResolveAppliesAModelsOverrides(t *testing.T) {
	ws := wires()
	home, away := ws[0], ws[1]
	p, homeRec := home.replay(t, home.text)
	q, awayRec := away.replay(t, away.text)
	plain := home.req(p).Model
	plain.ID, plain.Efforts, plain.DefaultEffort = "plain", nil, ""
	own := away.req(q).Model
	own.ID, own.Efforts, own.DefaultEffort = "own", nil, ""
	own.Dialect, own.BaseURL = q.Dialect, q.BaseURL
	p.Catalog = []Model{plain, own}
	s := New(p)

	for _, c := range []struct {
		model   string
		dialect Dialect
		baseURL string
		rec     *recorded
		ownKey  func(http.Header) []string
	}{
		{"plain", p.Dialect, p.BaseURL, homeRec, home.ownKey},
		{"own", q.Dialect, q.BaseURL, awayRec, away.ownKey},
	} {
		target, err := s.Resolve(p.ID, c.model, "")
		require.NoError(t, err, c.model)
		assert.Equal(t, p.ID, target.Provider.ID, c.model)
		assert.Equal(t, c.dialect, target.Provider.Dialect, c.model)
		assert.Equal(t, c.baseURL, target.Provider.BaseURL, c.model)

		_, err = target.Stream(t.Context(), "", []Message{{Role: "user", Blocks: []Block{TextBlock("hi")}}}, "", nil, nil, func(Chunk) {})

		require.NoError(t, err, c.model)
		assert.Equal(t, int64(1), c.rec.calls.Load(), c.model)
		assert.Equal(t, c.model, c.rec.body["model"], c.model)
		key := c.ownKey(c.rec.header)
		require.Len(t, key, 1, c.model)
		assert.Contains(t, key[0], p.Key, c.model)
	}
}

// A model's override is held to what Stream speaks, as a provider's default is.
func TestResolveRefusesAModelOnAnUnspokenWire(t *testing.T) {
	s := New(Provider{ID: "x", Dialect: DialectFake, fake: NewFake(0), Catalog: []Model{{ID: "m", Dialect: Dialect("smoke-signals")}}})

	_, err := s.Resolve("x", "m", "")

	assert.ErrorIs(t, err, ErrUnknownModel)
}

// ForModel overrides only what the model states.
func TestForModelAppliesOnlyTheModelsOverrides(t *testing.T) {
	p := Provider{ID: "x", Dialect: DialectChatCompletions, BaseURL: "https://x/v1"}

	assert.Equal(t, p, p.ForModel(Model{ID: "m"}))
	responses := p.ForModel(Model{Dialect: DialectResponses})
	assert.Equal(t, DialectResponses, responses.Dialect)
	assert.Equal(t, "https://x/v1", responses.BaseURL)
	messages := p.ForModel(Model{Dialect: DialectMessages, BaseURL: "https://x/anthropic"})
	assert.Equal(t, DialectMessages, messages.Dialect)
	assert.Equal(t, "https://x/anthropic", messages.BaseURL)
}

// An offer the target cannot carry is refused before anything is sent: a model
// whose catalog entry takes none, and a vendor tool no wire spells.
func TestStreamRefusesToolsOnAModelThatTakesNone(t *testing.T) {
	f := NewFake(0)
	p := FakeProvider(f)
	takesNone, err := New(p).Resolve("fake", "fake-no-tools", "")
	require.NoError(t, err)

	_, err = takesNone.Stream(t.Context(), "", nil, "", []ToolDefinition{{Name: "echo"}}, nil, func(Chunk) {})

	assert.ErrorIs(t, err, ErrToolsUnsupported)
	assert.Zero(t, f.Asked())

	offer := []ToolDefinition{{Name: "echo"}}
	takesThem := Target{Provider: p, Model: p.Catalog[0]}
	_, err = takesThem.Stream(t.Context(), "", nil, "", offer, nil, func(Chunk) {})
	require.NoError(t, err)
	assert.Equal(t, offer, f.LastRequest().Tools)
}

// A client tool is capped by no provider, so an offer of one with a cap is
// refused; on Messages, which spells no client tool, every one is.
func TestStreamRefusesAClientOfferItCannotSend(t *testing.T) {
	f := NewFake(0)
	p := FakeProvider(f)
	target := Target{Provider: p, Model: p.Catalog[0]}
	client := nativeTool{name: "shell"}

	_, err := target.Stream(t.Context(), "", nil, "", nil, []NativeOffer{{Tool: client, MaxUses: 1}}, func(Chunk) {})
	assert.ErrorIs(t, err, ErrToolsUnsupported)
	assert.Zero(t, f.Asked())

	messages := Target{Provider: Provider{Dialect: DialectMessages}, Model: p.Catalog[0]}
	_, err = messages.Stream(t.Context(), "", nil, "", nil, []NativeOffer{{Tool: messagesSearch}}, func(Chunk) {})
	assert.ErrorIs(t, err, ErrToolsUnsupported)
}

// search is the search offered at the cap chat gives it.
var search = NativeOffer{Tool: messagesSearch, Server: true, MaxUses: 5}

// A server tool goes only to a model that takes tools, at a cap of at least
// one; anything else is refused with nothing sent.
func TestStreamRefusesAServerToolTheModelCannotTake(t *testing.T) {
	cases := map[string]struct {
		model Model
		offer NativeOffer
	}{
		"a model taking no tools": {Model{ID: "m"}, search},
		"a cap under one":         {Model{ID: "m", Tools: true}, NativeOffer{Tool: messagesSearch, Server: true}},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			f := NewFake(0)
			target := Target{Provider: FakeProvider(f), Model: c.model}

			_, err := target.Stream(t.Context(), "", nil, "", nil, []NativeOffer{c.offer}, func(Chunk) {})

			assert.ErrorIs(t, err, ErrToolsUnsupported)
			assert.Zero(t, f.Asked())
		})
	}
}

// A vendor tool goes only to a provider whose wire speaks it, whatever the
// catalog says: the offer is refused with nothing sent.
func TestStreamRefusesAVendorToolItsWireDoesNotSpeak(t *testing.T) {
	for _, w := range wires() {
		if w.name == "messages" {
			continue
		}
		t.Run(w.name, func(t *testing.T) {
			p, rec := w.replay(t, w.text)
			model := Model{ID: "m", MaxOutputTokens: 1024, Tools: true}

			_, err := Target{Provider: p, Model: model}.Stream(t.Context(), "", nil, "", nil, []NativeOffer{search}, func(Chunk) {})

			assert.ErrorIs(t, err, ErrToolsUnsupported)
			assert.Zero(t, rec.calls.Load())
		})
	}
}

// A request offering no server tool replays no server call: the calls and their
// results stay out, and a cited text goes as text, since its citations point
// into results that are not sent.
func TestStreamSendsNoSearchWithoutTheOffer(t *testing.T) {
	f := NewFake(0)
	p := FakeProvider(f)
	msgs := []Message{{Role: "assistant", Blocks: searchRow(), ProviderID: "fake"}}

	_, err := Target{Provider: p, Model: p.Catalog[0]}.Stream(t.Context(), "", msgs, "", []ToolDefinition{{Name: "echo"}}, nil, func(Chunk) {})

	require.NoError(t, err)
	assert.Equal(t, []Block{TextBlock("Let me search."), withoutPayload(citedText("It shipped."))}, f.LastRequest().Messages[0].Blocks)
	assert.Equal(t, searchRow(), msgs[0].Blocks, "the caller's blocks are not written to")
}

// A server tool's offer reaches the request, and a request offering only a
// server tool still replays its rounds: the tools key is defined.
func TestStreamCarriesAServerTool(t *testing.T) {
	f := NewFake(0)
	p := FakeProvider(f)
	msgs := []Message{{Role: "assistant", Blocks: searchRow(), ProviderID: "fake"}}

	_, err := Target{Provider: p, Model: p.Catalog[0]}.Stream(t.Context(), "", msgs, "", nil, []NativeOffer{search}, func(Chunk) {})

	require.NoError(t, err)
	assert.Equal(t, []NativeOffer{search}, f.LastRequest().NativeTools)
	assert.Equal(t, msgs, f.LastRequest().Messages)
}

// A request offering nothing replays no round: the Messages API refuses a history
// holding tool blocks unless tools is defined, and one rule covers every wire. A
// payload goes with the round it belongs to — the Responses API refuses a
// reasoning item whose following item is gone — and the row's text and thinking
// still go. The caller's slices are left alone.
func TestStreamSendsNoRoundsWithNoOffer(t *testing.T) {
	f := NewFake(0)
	p := FakeProvider(f)
	round := []Block{
		withPayload(ThinkingBlock("I count them."), `{"id":"rs_1","type":"reasoning"}`),
		ToolUseBlock("call-1", "echo", json.RawMessage(`{}`)),
		ToolResultBlock("call-1", "hi", false),
		TextBlock("twelve"),
	}
	msgs := []Message{
		{Role: "user", Blocks: []Block{TextBlock("how many?")}},
		{Role: "assistant", Blocks: round, ProviderID: "fake"},
	}

	_, err := Target{Provider: p, Model: p.Catalog[0]}.Stream(t.Context(), "", msgs, "", nil, nil, func(Chunk) {})

	require.NoError(t, err)
	assert.Equal(t, []Message{
		{Role: "user", Blocks: []Block{TextBlock("how many?")}},
		{Role: "assistant", Blocks: []Block{ThinkingBlock("I count them."), TextBlock("twelve")}, ProviderID: "fake"},
	}, f.LastRequest().Messages, "the payload goes with the round it reasoned toward, the text stays")
	assert.Equal(t, round, msgs[1].Blocks, "the caller's blocks are not written to")
}

// unreplayableRow is an assistant row written by p whose first call's
// payload is not an object and whose second's is, marked GOOD so a test can see
// whether it was sent.
func unreplayableRow(p Provider) Message {
	return Message{Role: "assistant", ProviderID: p.ID, Dialect: p.Dialect, Blocks: []Block{
		withPayload(ToolUseBlock("call_1", "list_objects", json.RawMessage(`{"resource":"pods"}`)), `7`),
		withPayload(ToolUseBlock("call_2", "list_objects", json.RawMessage(`{"resource":"nodes"}`)), `{"marker":"GOOD"}`),
		ToolResultBlock("call_1", "2 pods", false),
		ToolResultBlock("call_2", "3 nodes", false),
		TextBlock("twelve"),
	}}
}

// A stored row holding a payload that is not an object is sent as foreign to
// every provider, so none of its payloads go and its app fields do: the payloads
// of a row pair up, and an API refuses a pair with one side missing. The tests
// offer a tool, since with no offer Stream drops every round, payloads and all;
// the payloads ride tool_use blocks, the one kind every wire replays one on; and
// the row names the target's own provider and dialect, since a row naming none
// is already stripped.
func TestStreamSendsARowWithABadPayloadAsForeign(t *testing.T) {
	question := Message{Role: "user", Blocks: []Block{TextBlock("how many pods?")}}

	t.Run("fake", func(t *testing.T) {
		buf := captureLog(t)
		f := NewFake(0)
		p := FakeProvider(f)
		msgs := []Message{question, unreplayableRow(p)}

		_, err := Target{Provider: p, Model: p.Catalog[0]}.Stream(t.Context(), "", msgs, "", []ToolDefinition{listObjects}, nil, func(Chunk) {})

		require.NoError(t, err)
		assert.Empty(t, f.LastRequest().Messages[1].ProviderID, "the row names no writer")
		assert.Empty(t, f.LastRequest().Messages[1].Dialect)
		assert.Equal(t, p.ID, msgs[1].ProviderID, "the caller's messages are not written to")
		assert.Contains(t, buf.String(), "level=WARN")
		assert.Contains(t, buf.String(), `messages="1 tool_use"`, "by index and block type")
		assert.NotContains(t, buf.String(), "GOOD", "never by content")
	})

	for _, w := range wires() {
		t.Run(w.name, func(t *testing.T) {
			p, rec := w.replay(t, w.text)
			req := w.req(p, question, unreplayableRow(p))
			model := req.Model
			model.Tools = true

			_, err := Target{Provider: p, Model: model}.Stream(t.Context(), req.SystemPrompt, req.Messages, "", []ToolDefinition{listObjects}, nil, func(Chunk) {})

			require.NoError(t, err)
			convo := marshal(t, w.convo(rec.body))
			assert.NotContains(t, convo, "GOOD", "the row's own payload goes with the bad one")
			assert.Contains(t, convo, `pods`, "the calls go on their app fields")
			assert.Contains(t, convo, `nodes`)
			assert.Contains(t, convo, "twelve")
		})
	}

	// With thinking on, the row holding the last round is repaired only when it
	// is foreign or was written with no effort. This row was written thinking, so
	// only going foreign repairs it.
	t.Run("messages repairs the last round", func(t *testing.T) {
		p, rec := replay(t, textStream)
		row := Message{Role: "assistant", ProviderID: p.ID, Dialect: p.Dialect, Effort: "high", Blocks: []Block{
			TextBlock("Let me check."),
			withPayload(ThinkingBlock("I count them."), `7`),
			ToolUseBlock("toolu_1", "list_objects", json.RawMessage(`{"resource":"pods"}`)),
			ToolResultBlock("toolu_1", "2 pods", false),
			TextBlock("Two."),
		}}
		req := request(p, question, row)
		model := req.Model
		model.Tools = true

		_, err := Target{Provider: p, Model: model, Effort: "high"}.Stream(t.Context(), req.SystemPrompt, req.Messages, "", []ToolDefinition{listObjects}, nil, func(Chunk) {})

		require.NoError(t, err)
		assert.Equal(t, []string{"user: text", "assistant: text text"}, wireShape(t, rec.body), "the row's text and no round")
	})
}

// nativeTool is a vendor's tool no wire can spell: a name alone.
type nativeTool struct{ name string }

func (n nativeTool) Name() string { return n.name }

// A target takes a tool its wire can spell: on Messages a server tool that is a
// MessagesServerTool, never a client tool; on the fake any tool; on any other
// wire none. Which tools a provider is offered is the catalog's.
func TestATargetTakesWhatItsWireSpells(t *testing.T) {
	model := Model{ID: "m", Tools: true}
	plain := nativeTool{name: "search"}

	messages := Target{Provider: Provider{Dialect: DialectMessages}, Model: model}
	assert.True(t, messages.Takes(messagesSearch, true))
	assert.True(t, messages.Takes(messagesFetch, true))
	assert.False(t, messages.Takes(messagesSearch, false), "no client interface on Messages")
	assert.False(t, messages.Takes(plain, true), "a tool without the wire's interface")

	fake := Target{Provider: Provider{Dialect: DialectFake}, Model: model}
	assert.True(t, fake.Takes(plain, true))
	assert.True(t, fake.Takes(plain, false))

	for _, d := range []Dialect{DialectResponses, DialectChatCompletions} {
		assert.False(t, Target{Provider: Provider{Dialect: d}, Model: model}.Takes(messagesSearch, true), d)
	}
}

// No two offers share a name, and no name the wire reads a call by is spelled
// twice: each would leave a call that matches two tools. Refused with nothing
// sent.
func TestStreamRefusesANameSpelledTwice(t *testing.T) {
	listed := Model{ID: "m", MaxOutputTokens: 1024, Tools: true}
	secondSearch := &messagesTool{
		name: "search_again", callName: messagesSearch.callName,
		resultType: messagesSearch.resultType, offer: messagesSearch.offer, uses: messagesSearch.uses,
	}
	server := func(tool NativeTool) NativeOffer { return NativeOffer{Tool: tool, Server: true, MaxUses: 1} }
	cases := map[string]struct {
		dialect Dialect
		tools   []ToolDefinition
		native  []NativeOffer
	}{
		"two definitions of one name":         {DialectFake, []ToolDefinition{{Name: "echo"}, {Name: "echo"}}, nil},
		"two native offers of one name":       {DialectFake, nil, []NativeOffer{server(nativeTool{name: "s"}), server(nativeTool{name: "s"})}},
		"a definition named like an offer":    {DialectFake, []ToolDefinition{{Name: "s"}}, []NativeOffer{server(nativeTool{name: "s"})}},
		"two offers of one call name":         {DialectMessages, nil, []NativeOffer{server(messagesSearch), server(secondSearch)}},
		"a definition named like a call name": {DialectMessages, []ToolDefinition{{Name: "web_search"}}, []NativeOffer{server(messagesSearch)}},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			f := NewFake(0)
			p := FakeProvider(f)
			p.Dialect = c.dialect

			_, err := Target{Provider: p, Model: listed}.Stream(t.Context(), "", nil, "", c.tools, c.native, func(Chunk) {})

			assert.ErrorIs(t, err, ErrToolsUnsupported)
			assert.Zero(t, f.Asked())
		})
	}
}

// A row's credential is a key or a signer, never both.
func TestNewPanicsOnAProviderWithAKeyAndASigner(t *testing.T) {
	assert.Panics(t, func() {
		New(Provider{ID: "plan", Dialect: DialectResponses, Key: "sk", Signer: &fakeSigner{}})
	})
}

// sessionSigner is a SessionSigner whose Bind hands out a signer naming the
// session it was made under.
type sessionSigner struct {
	fakeSigner
	session int
	binds   int
}

func (s *sessionSigner) Bind() Signer {
	s.binds++
	return &fakeSigner{token: fmt.Sprintf("session-%d", s.session)}
}

func planService(s Signer) *Service {
	return New(
		Provider{ID: "plan", Dialect: DialectResponses, Signer: s, Catalog: []Model{{ID: "m"}, {ID: "n"}}},
		Provider{ID: "other", Dialect: DialectResponses, Signer: &sessionSigner{session: 9}, Catalog: []Model{{ID: "o"}}},
	)
}

// A target holds the signer bound to the session it was resolved under, never
// the row's, so a session that ends after the resolve still ends the target's.
func TestResolveBindsASessionSigner(t *testing.T) {
	s := &sessionSigner{session: 1}
	svc := planService(s)

	target, err := svc.Resolve("plan", "m", "")
	require.NoError(t, err)
	s.session = 2

	assert.Equal(t, "session-1", target.Provider.Signer.(*fakeSigner).token)
	assert.Equal(t, 1, s.binds)
}

// A signer that is not a session's is the target's as it is.
func TestResolveKeepsAPlainSigner(t *testing.T) {
	s := &fakeSigner{token: "t"}

	target, err := planService(s).Resolve("plan", "m", "")

	require.NoError(t, err)
	assert.Same(t, s, target.Provider.Signer)
}

// A subagent on its parent's provider keeps the parent's binding.
func TestResolveUnderKeepsTheParentsBindingOnItsProvider(t *testing.T) {
	s := &sessionSigner{session: 1}
	svc := planService(s)
	parent, err := svc.Resolve("plan", "m", "")
	require.NoError(t, err)
	s.session = 2

	child, err := svc.ResolveUnder(parent, "n", "")
	require.NoError(t, err)
	assert.Same(t, parent.Provider.Signer, child.Provider.Signer)
	assert.Equal(t, "n", child.Model.ID)
	assert.Equal(t, 1, s.binds)

	_, err = svc.ResolveUnder(parent, "gone", "")
	assert.ErrorIs(t, err, ErrUnknownModel)
}

// SetCatalog replaces one held provider's models: a send resolves against the
// new list, and the provider stays held with none.
func TestSetCatalogReplacesAProvidersModels(t *testing.T) {
	svc := planService(&fakeSigner{})

	svc.SetCatalog("plan", []Model{{ID: "fresh"}})

	_, err := svc.Resolve("plan", "m", "")
	assert.ErrorIs(t, err, ErrUnknownModel)
	target, err := svc.Resolve("plan", "fresh", "")
	require.NoError(t, err)
	assert.Equal(t, "fresh", target.Model.ID)
	assert.Equal(t, []string{"plan", "other"}, providerIDs(testutil.Current(t, svc.WatchProvidersState, "the providers")))

	// An empty catalog is held as none, so it equals the nil it replaces.
	svc.SetCatalog("plan", []Model{})
	p, ok := svc.Provider("plan")
	require.True(t, ok)
	assert.Nil(t, p.Catalog)

	svc.SetCatalog("nobody", []Model{{ID: "x"}})
	assert.Equal(t, []string{"plan", "other"}, providerIDs(testutil.Current(t, svc.WatchProvidersState, "the providers")))
}

// WatchProvidersState is a state watch of the providers: the current list on subscribe, then
// the list after each SetCatalog.
func TestWatchProvidersStateSendsTheProvidersOnEachCatalogChange(t *testing.T) {
	svc := planService(&fakeSigner{})
	frames, cancel := svc.WatchProvidersState()
	defer cancel()

	first := testutil.Recv(t, frames, "a providers frame")
	assert.Len(t, first[0].Catalog, 2)

	svc.SetCatalog("plan", []Model{{ID: "fresh"}})
	next := testutil.Recv(t, frames, "the frame after SetCatalog")
	assert.Equal(t, []Model{{ID: "fresh"}}, next[0].Catalog)

	// The same catalog again sends nothing; the change after it is the next frame.
	svc.SetCatalog("plan", []Model{{ID: "fresh"}})
	svc.SetCatalog("plan", nil)
	assert.Empty(t, testutil.Recv(t, frames, "the emptied catalog")[0].Catalog)
}

// The service keeps its own copy: a caller writing into the slice it passed
// changes no held catalog.
func TestSetCatalogCopiesTheModels(t *testing.T) {
	svc := planService(&fakeSigner{})
	models := []Model{{ID: "fresh"}}

	svc.SetCatalog("plan", models)
	models[0].ID = "changed"

	p, ok := svc.Provider("plan")
	require.True(t, ok)
	assert.Equal(t, "fresh", p.Catalog[0].ID)
}
