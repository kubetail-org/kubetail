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

package app

import (
	"context"
	"net/http"
	"sync"
)

// requests is the gate every request passes, and the count of those in flight.
//
// The gate is what makes wait mean something: net/http starts a handler on its own
// goroutine before wrap runs, so without it a handler could reach Add after wait
// saw zero.
type requests struct {
	ctx    context.Context
	cancel context.CancelFunc

	mu       sync.Mutex
	closed   bool
	inflight sync.WaitGroup
}

func newRequests() *requests {
	ctx, cancel := context.WithCancel(context.Background())
	return &requests{ctx: ctx, cancel: cancel}
}

// wrap admits a request to next while the gate is open, under a context that
// close cancels. A refusal is a plain-text 503 for gRPC too: the host's tonic client
// reads a 503 with no grpc-status as Unavailable.
func (r *requests) wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		r.mu.Lock()
		if r.closed {
			r.mu.Unlock()
			http.Error(w, "shutting down", http.StatusServiceUnavailable)
			return
		}
		r.inflight.Add(1)
		r.mu.Unlock()
		defer r.inflight.Done()

		ctx, cancel := context.WithCancel(req.Context())
		defer cancel()
		defer context.AfterFunc(r.ctx, cancel)()
		next.ServeHTTP(w, req.WithContext(ctx))
	})
}

// close refuses every later request and cancels every one still running.
func (r *requests) close() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.closed = true
	r.cancel()
}

// wait returns once every admitted request's handler has. Call it after close.
func (r *requests) wait() {
	r.inflight.Wait()
}
