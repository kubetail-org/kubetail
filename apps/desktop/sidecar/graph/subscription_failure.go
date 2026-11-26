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

package graph

import (
	"context"
	"sync/atomic"

	"github.com/99designs/gqlgen/graphql"
)

// subscriptionFailure is one operation's terminal-reason slot.
type subscriptionFailure struct{ err atomic.Pointer[error] }

type subscriptionFailureKey struct{}

// subscriptionFailedExtension marks the terminal frame on the wire. The client keys on it
// to tell a dead watch from a live frame whose field errored; keep it in step with
// src/lib/graphql/subscribe-exchange.ts.
const subscriptionFailedExtension = "subscriptionFailed"

// subscriptionStream is ptrSubscription over a failable watch: the same pointer mapping, plus the
// source's terminal reason filed where InterceptResponse will find it. Every resolver
// over a watch with an Err must go through here — a bare ptrSubscription would drop the
// reason and the failure would reach the client as a graceful end.
//
// The reason is filed from mapSubscription's teardown hook, which runs before the returned
// channel closes; that ordering is the contract, since it is the close that sends
// gqlgen looking for it.
func subscriptionStream[T any](ctx context.Context, frames <-chan T, err func() error) <-chan *T {
	return mapSubscription(ctx, frames, func() { recordSubscriptionFailure(ctx, err()) }, func(v T) *T { return &v })
}

// recordSubscriptionFailure files err against the running operation, keeping the FIRST one:
// a document may open several watches, and the one that died first explains the rest.
// A nil err (clean teardown, cancelled ctx) is not a failure and files nothing.
func recordSubscriptionFailure(ctx context.Context, err error) {
	if err == nil {
		return
	}
	if f, ok := ctx.Value(subscriptionFailureKey{}).(*subscriptionFailure); ok {
		f.err.CompareAndSwap(nil, &err)
	}
}

// SubscriptionFailureExtension turns a subscription whose source died into a GraphQL error
// on the wire. Without it a broken watch is indistinguishable from a graceful end —
// the stream closes either way and the webview reconnects silently, so a permanently
// broken watch is an invisible retry loop with nothing shown to the user.
//
// It takes two halves because a resolver and the frames it feeds never share a
// response context, and gqlgen builds each subscription frame as data alone: by the
// time the reason exists there is no frame left to attach it to. InterceptOperation
// hangs the slot on the operation ctx — which gqlgen threads into both the resolvers
// and every later frame — and InterceptResponse claims it once the stream is spent,
// emitting one final errors-only response ahead of the transport's completion.
type SubscriptionFailureExtension struct{}

// gqlgen dispatches these two by runtime type switch, so a signature drift would
// silently degrade the extension to a no-op rather than fail the build.
var (
	_ graphql.OperationInterceptor = SubscriptionFailureExtension{}
	_ graphql.ResponseInterceptor  = SubscriptionFailureExtension{}
)

func (SubscriptionFailureExtension) ExtensionName() string { return "SubscriptionFailure" }

func (SubscriptionFailureExtension) Validate(graphql.ExecutableSchema) error { return nil }

func (SubscriptionFailureExtension) InterceptOperation(ctx context.Context, next graphql.OperationHandler) graphql.ResponseHandler {
	return next(context.WithValue(ctx, subscriptionFailureKey{}, &subscriptionFailure{}))
}

func (SubscriptionFailureExtension) InterceptResponse(ctx context.Context, next graphql.ResponseHandler) *graphql.Response {
	if resp := next(ctx); resp != nil {
		return resp
	}
	// nil means the stream is over. Queries and mutations reach here too, having
	// filed nothing, and fall straight through.
	f, ok := ctx.Value(subscriptionFailureKey{}).(*subscriptionFailure)
	if !ok {
		return nil
	}
	// Swap, not Load: emitted once, so the next poll returns nil and the
	// subscription actually ends instead of looping on the same reason.
	p := f.err.Swap(nil)
	if p == nil {
		return nil
	}
	err := *p
	// AddError runs the reason through the server's error presenter — the one seam
	// where it gets logged.
	graphql.AddError(ctx, err)
	// The extension marks the frame explicitly. A client cannot infer "the watch
	// died" from shape alone: a non-null field erroring nulls its parent, so an
	// ordinary frame carrying a field error looks identical.
	return &graphql.Response{
		Errors:     graphql.GetErrors(ctx),
		Extensions: map[string]any{subscriptionFailedExtension: true},
	}
}
