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

// Package catalogsync keeps a provider's model catalog current: read when its
// credential changes and every six hours, emptied while it has none.
package catalogsync

import (
	"context"
	"log/slog"
	"time"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/llm"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/poke"
)

const (
	// retryDelay is how long a failed read waits for its next try.
	retryDelay = time.Minute
	// refreshInterval is how long a read's list stands before the next read: a
	// machine that never sleeps is never poked. Each read signs, so a shorter
	// one would refresh an idle account's tokens as often.
	refreshInterval = 6 * time.Hour
	// readTimeout bounds a read, its body included, so a stalled server fails
	// it into the retry.
	readTimeout = 30 * time.Second
)

// Reader reads the provider's catalog off p, its row.
type Reader func(ctx context.Context, p llm.Provider) ([]llm.Model, error)

// loop keeps a provider's catalog current, and is its only writer. It takes
// the credential's changes in order on one goroutine: each value's list is
// read, and a poke or refreshInterval reads it again; 0 empties it, and so
// does a new value, before its read. Only the latest
// read's result is set, and the next change cancels it, so a list read for one
// account never outlives a frame for the next.
type loop struct {
	// id is the provider's.
	id string
	// connected is the credential's value, 0 while there is none.
	connected <-chan uint64
	pokes     <-chan poke.Notification
	read      func(context.Context) ([]llm.Model, error)
	set       func([]llm.Model)
	// after paces the next read, a retry or a refresh; time.After in production.
	after func(time.Duration) <-chan time.Time
}

// Start runs the loop for the provider id over its credential and the resync
// pokes, setting what read answers on models, until the stop it returns, which
// waits for it. subscribe streams the credential's value, 0 while there is
// none: current on subscribe, then each change.
func Start(id string, subscribe func() (<-chan uint64, func()), pokes *poke.Service, models *llm.Service, read Reader) (stop func()) {
	p, _ := models.Provider(id)
	connected, unsubscribe := subscribe()
	signals, unpoke := pokes.ListenForNotifications()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	l := loop{
		id:        id,
		connected: connected,
		pokes:     signals,
		read:      func(ctx context.Context) ([]llm.Model, error) { return read(ctx, p) },
		set:       func(list []llm.Model) { models.SetCatalog(id, list) },
		after:     time.After,
	}
	go func() {
		defer close(done)
		l.run(ctx)
	}()
	return func() {
		cancel()
		<-done
		unsubscribe()
		unpoke()
	}
}

func (c loop) run(ctx context.Context) {
	type result struct {
		models []llm.Model
		err    error
	}
	var (
		session uint64           // the session the catalog is read for; 0 for none
		pending <-chan result    // the latest read's answer; nil while none is under way
		cancel  = func() {}      // ends the latest read
		next    <-chan time.Time // the next read's clock: a retry, or a refresh
	)
	defer func() { cancel() }()
	// stop ends the latest read; an answer it gives anyway is never received.
	stop := func() {
		cancel()
		pending, next = nil, nil
	}
	start := func() {
		stop()
		var readCtx context.Context
		readCtx, cancel = context.WithTimeout(ctx, readTimeout)
		answer := make(chan result, 1) // buffered, so a dropped read never blocks
		pending = answer
		go func() {
			models, err := c.read(readCtx)
			answer <- result{models, err}
		}()
	}
	for {
		select {
		case <-ctx.Done():
			return
		case id := <-c.connected:
			switch {
			case id == 0:
				stop()
				session = 0
				c.set(nil)
			case id != session:
				// A latest-value watch can skip the 0 between two sessions: the
				// last account's list goes first.
				if session != 0 {
					c.set(nil)
				}
				session = id
				start()
			}
		case <-c.pokes:
			if session != 0 {
				start()
			}
		case <-next:
			start()
		case r := <-pending:
			pending = nil
			if r.err != nil {
				slog.Warn("could not read the provider's catalog", "provider", c.id, "err", r.err)
				next = c.after(retryDelay)
			} else {
				c.set(r.models)
				next = c.after(refreshInterval)
			}
		}
	}
}
