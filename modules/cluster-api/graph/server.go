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
	"crypto/subtle"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/99designs/gqlgen/graphql/handler"
	"github.com/99designs/gqlgen/graphql/handler/extension"
	"github.com/99designs/gqlgen/graphql/handler/lru"
	"github.com/99designs/gqlgen/graphql/handler/transport"
	"github.com/gorilla/websocket"
	"github.com/vektah/gqlparser/v2/ast"

	grpcdispatcher "github.com/kubetail-org/grpc-dispatcher-go"

	"github.com/kubetail-org/kubetail/modules/shared/graphql/directives"
	"github.com/kubetail-org/kubetail/modules/shared/httphelpers"
	"github.com/kubetail-org/kubetail/modules/shared/k8shelpers"
)

// ctxKey is a private type for keys defined in this package.
type ctxKey int

const (
	// forwardedCSRFTokenCtxKey holds a *string of the X-Forwarded-CSRF-Token
	// header value from the WebSocket upgrade request. nil means the header
	// was absent (non-browser caller); a non-nil value (including "") means
	// it was present and the InitFunc must enforce a matching csrfToken in
	// the connection_init payload.
	forwardedCSRFTokenCtxKey ctxKey = iota
)

// Represents Server
type Server struct {
	r          *Resolver
	h          http.Handler
	shutdownCh chan struct{}
	wg         sync.WaitGroup
}

// Create new Server instance
func NewServer(cm k8shelpers.ConnectionManager, grpcDispatcher *grpcdispatcher.Dispatcher, allowedNamespaces []string) *Server {
	// Init resolver
	r := &Resolver{cm, grpcDispatcher, allowedNamespaces}

	// Init config
	gqlCfg := Config{Resolvers: r}
	gqlCfg.Directives.Validate = directives.ValidateDirective
	gqlCfg.Directives.NullIfValidationFailed = directives.NullIfValidationFailedDirective

	// Init schema
	schema := NewExecutableSchema(gqlCfg)

	// Init handler
	h := handler.New(schema)

	// SSE transport for subscriptions. The caller's identity rides on the
	// POST like any other GraphQL request. Registered before POST so that
	// requests with `Accept: text/event-stream` aren't claimed by the POST
	// transport's broader media-type match.
	h.AddTransport(transport.SSE{
		KeepAlivePingInterval: 10 * time.Second,
	})

	h.SetQueryCache(lru.New[*ast.QueryDocument](1000))

	// Configure WebSocket. Every upgrade arrives through the kube-apiserver's
	// front proxy, and the Dashboard's proxy strips Origin, so an upgrade
	// carrying one is refused. When the upgrade carries X-Forwarded-CSRF-Token
	// (set by the Dashboard's proxy), the connection_init payload's csrfToken
	// must match it; without the header no token is checked.
	h.AddTransport(&transport.Websocket{
		Upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool {
				return r.Header.Get("Origin") == ""
			},
			ReadBufferSize:    1024,
			WriteBufferSize:   1024,
			EnableCompression: false,
		},
		InitFunc: func(ctx context.Context, initPayload transport.InitPayload) (context.Context, *transport.InitPayload, error) {
			expectedPtr, _ := ctx.Value(forwardedCSRFTokenCtxKey).(*string)
			if expectedPtr == nil {
				return ctx, nil, nil
			}
			expected := strings.TrimSpace(*expectedPtr)
			got := strings.TrimSpace(initPayload.GetString("csrfToken"))
			if expected == "" || got == "" || subtle.ConstantTimeCompare([]byte(got), []byte(expected)) != 1 {
				return ctx, nil, errors.New("invalid CSRF token")
			}
			return ctx, nil, nil
		},
		KeepAlivePingInterval: 10 * time.Second,
	})

	h.AddTransport(transport.POST{})

	h.Use(extension.Introspection{})
	h.Use(extension.AutomaticPersistedQuery{
		Cache: lru.New[string](100),
	})

	return &Server{r: r, h: h, shutdownCh: make(chan struct{})}
}

// NotifyShutdown signals active WebSocket connections to begin closing.
func (s *Server) NotifyShutdown() {
	close(s.shutdownCh)
}

// DrainWithContext waits for all active WebSocket connections to finish, respecting ctx.
func (s *Server) DrainWithContext(ctx context.Context) error {
	doneCh := make(chan struct{})
	go func() {
		s.wg.Wait()
		close(doneCh)
	}()
	select {
	case <-doneCh:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Close releases any server-level resources.
func (s *Server) Close() error {
	return nil
}

// ServeHTTP delegates to the underlying handler, tracking all active
// requests so DrainWithContext can wait for them to finish. Long-lived
// connections (WebSocket upgrades and SSE streams) also get their request
// context cancelled on shutdown so gqlgen can close them cleanly.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.wg.Add(1)
	defer s.wg.Done()

	isLongLived := r.Header.Get("Upgrade") != "" ||
		strings.Contains(r.Header.Get("Accept"), "text/event-stream")

	if isLongLived {
		cancelCtx, cancel := context.WithCancel(r.Context())
		defer cancel()
		go func() {
			select {
			case <-cancelCtx.Done():
			case <-s.shutdownCh:
				cancel()
			}
		}()

		// Stash X-Forwarded-CSRF-Token presence (not just value) into context
		// so the WebSocket InitFunc can distinguish absent (no check) from
		// present-but-empty (reject). r.Header.Values returns nil when the
		// header wasn't sent at all.
		ctx := cancelCtx
		if r.Header.Get("Upgrade") != "" {
			if vals := r.Header.Values(httphelpers.HeaderForwardedCSRFToken); vals != nil {
				v := vals[0]
				ctx = context.WithValue(ctx, forwardedCSRFTokenCtxKey, &v)
			}
		}

		r = r.WithContext(ctx)
	}

	s.h.ServeHTTP(w, r)
}
