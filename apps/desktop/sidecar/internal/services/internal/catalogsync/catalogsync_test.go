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

package catalogsync

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/testutil"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/llm"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/poke"
)

// catalogRig drives the loop: the test sends connected sessions and pokes,
// answers each read, sees each wait the loop schedules and fires its clock, and
// sees each catalog set.
type catalogRig struct {
	connected chan uint64
	pokes     chan poke.Notification
	reads     chan catalogCall
	waits     chan time.Duration
	clock     chan time.Time
	sets      chan []llm.Model
}

// catalogCall is one read under way: the context it runs under, and where its answer
// goes.
type catalogCall struct {
	ctx    context.Context
	answer chan readResult
}

type readResult struct {
	models []llm.Model
	err    error
}

func startCatalogLoop(t *testing.T) *catalogRig {
	t.Helper()
	r := &catalogRig{
		connected: make(chan uint64), pokes: make(chan poke.Notification),
		reads: make(chan catalogCall), waits: make(chan time.Duration, 8), clock: make(chan time.Time),
		sets: make(chan []llm.Model, 8),
	}
	l := loop{
		connected: r.connected,
		pokes:     r.pokes,
		read: func(ctx context.Context) ([]llm.Model, error) {
			rd := catalogCall{ctx: ctx, answer: make(chan readResult, 1)}
			select {
			case r.reads <- rd:
			case <-ctx.Done():
				return nil, ctx.Err()
			}
			select {
			case res := <-rd.answer:
				return res.models, res.err
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		},
		set: func(m []llm.Model) { r.sets <- m },
		after: func(d time.Duration) <-chan time.Time {
			r.waits <- d
			return r.clock
		},
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		l.run(ctx)
	}()
	t.Cleanup(func() {
		cancel()
		testutil.WaitClosed(t, done, "the catalog loop to end")
	})
	return r
}

var model = []llm.Model{{ID: "m"}}

// A connection reads the account's list and sets it; no session empties the
// catalog.
func TestAConnectionSetsTheAccountsCatalog(t *testing.T) {
	r := startCatalogLoop(t)

	r.connected <- 1
	testutil.Recv(t, r.reads, "the read").answer <- readResult{models: model}
	assert.Equal(t, model, testutil.Recv(t, r.sets, "the catalog"))

	r.connected <- 0
	assert.Empty(t, testutil.Recv(t, r.sets, "the emptied catalog"))
}

// A read for one session never outlives a frame for the next: the next session
// cancels it, and an answer that lands anyway is dropped.
func TestAReadForAnEndedSessionIsDropped(t *testing.T) {
	r := startCatalogLoop(t)
	r.connected <- 1
	first := testutil.Recv(t, r.reads, "the first read")

	r.connected <- 2
	testutil.WaitClosed(t, first.ctx.Done(), "the first read to be cancelled")
	second := testutil.Recv(t, r.reads, "the second read")
	first.answer <- readResult{models: []llm.Model{{ID: "stale"}}}
	second.answer <- readResult{models: model}

	assert.Empty(t, testutil.Recv(t, r.sets, "the first session's catalog emptied"))
	assert.Equal(t, model, testutil.Recv(t, r.sets, "the second session's catalog"))
}

// A session that follows another with no 0 between empties the last account's
// catalog before its read, so a failed read leaves nothing to pick.
func TestANewSessionEmptiesTheLastAccountsCatalog(t *testing.T) {
	r := startCatalogLoop(t)
	r.connected <- 1
	testutil.Recv(t, r.reads, "the first read").answer <- readResult{models: model}
	assert.Equal(t, model, testutil.Recv(t, r.sets, "the first account's catalog"))

	r.connected <- 2

	assert.Empty(t, testutil.Recv(t, r.sets, "the emptied catalog"))
	testutil.Recv(t, r.reads, "the second read").answer <- readResult{err: errors.New("unreachable")}
}

// A read runs under a deadline, so a stalled server fails it into the retry.
func TestAReadHasADeadline(t *testing.T) {
	r := startCatalogLoop(t)
	r.connected <- 1

	rd := testutil.Recv(t, r.reads, "the read")

	deadline, ok := rd.ctx.Deadline()
	assert.True(t, ok)
	assert.WithinDuration(t, time.Now().Add(readTimeout), deadline, readTimeout)
}

// A poke reads the catalog again; the same session again does not.
func TestAPokeReadsTheCatalogAgain(t *testing.T) {
	r := startCatalogLoop(t)
	r.connected <- 1
	testutil.Recv(t, r.reads, "the read").answer <- readResult{models: model}
	testutil.Recv(t, r.sets, "the catalog")

	r.connected <- 1
	r.pokes <- poke.Notification{}

	testutil.Recv(t, r.reads, "the poke's read").answer <- readResult{models: model}
	testutil.Recv(t, r.sets, "the catalog again")
}

// A read that fails is tried again on the paced clock while the session lasts,
// and not after it.
func TestAFailedReadIsRetried(t *testing.T) {
	r := startCatalogLoop(t)
	r.connected <- 1
	testutil.Recv(t, r.reads, "the read").answer <- readResult{err: errors.New("unreachable")}
	assert.Equal(t, retryDelay, testutil.Recv(t, r.waits, "the retry's wait"))

	r.clock <- time.Now()

	testutil.Recv(t, r.reads, "the retry").answer <- readResult{models: model}
	assert.Equal(t, model, testutil.Recv(t, r.sets, "the catalog"))
}

// A list that was read is read again after refreshInterval, while the session
// lasts.
func TestTheCatalogIsReadAgainOnItsInterval(t *testing.T) {
	r := startCatalogLoop(t)
	r.connected <- 1
	testutil.Recv(t, r.reads, "the read").answer <- readResult{models: model}
	testutil.Recv(t, r.sets, "the catalog")
	assert.Equal(t, refreshInterval, testutil.Recv(t, r.waits, "the refresh's wait"))

	r.clock <- time.Now()

	fresh := []llm.Model{{ID: "fresh"}}
	testutil.Recv(t, r.reads, "the refresh").answer <- readResult{models: fresh}
	assert.Equal(t, fresh, testutil.Recv(t, r.sets, "the refreshed catalog"))
}

// A poke while signed out reads nothing.
func TestAPokeWhileSignedOutReadsNothing(t *testing.T) {
	r := startCatalogLoop(t)
	r.connected <- 0
	testutil.Recv(t, r.sets, "the empty catalog")

	r.pokes <- poke.Notification{}
	r.connected <- 1

	testutil.Recv(t, r.reads, "the connection's read")
}

// The production loop runs over the connected session and the resync pokes,
// and its stop waits for it and unsubscribes: a signed-out launch leaves the
// catalog empty.
func TestStartRunsUntilStopped(t *testing.T) {
	signedOut := make(chan uint64, 1)
	signedOut <- 0
	unsubscribed := make(chan struct{})
	subscribe := func() (<-chan uint64, func()) { return signedOut, func() { close(unsubscribed) } }
	pokes := poke.New()
	llmSvc := llm.New(llm.Provider{ID: "chatgpt", Dialect: llm.DialectResponses, Catalog: []llm.Model{{ID: "stale"}}})
	frames, cancel := llmSvc.WatchProvidersState()
	defer cancel()
	testutil.Recv(t, frames, "the first frame")

	stop := Start("chatgpt", subscribe, pokes, llmSvc, func(context.Context, llm.Provider) ([]llm.Model, error) {
		return nil, errors.New("signed out: never read")
	})
	assert.Empty(t, testutil.Recv(t, frames, "the emptied catalog")[0].Catalog)
	pokes.Poke(poke.Source("test"))

	stop()
	testutil.WaitClosed(t, unsubscribed, "the unsubscribe")
}
