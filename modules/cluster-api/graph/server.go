// Copyright 2024-2025 Andres Morey
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
	"errors"
	"net/http"
	"net/http/httptest"
	"time"

	"github.com/99designs/gqlgen/graphql/handler"
	"github.com/99designs/gqlgen/graphql/handler/extension"
	"github.com/99designs/gqlgen/graphql/handler/lru"
	"github.com/99designs/gqlgen/graphql/handler/transport"
	"github.com/gorilla/websocket"
	"github.com/vektah/gqlparser/v2/ast"

	grpcdispatcher "github.com/kubetail-org/grpc-dispatcher-go"
)

type ctxKey int

const cookiesCtxKey ctxKey = iota

// Represents Server
type Server struct {
	r          *Resolver
	h          http.Handler
	shutdownCh chan struct{}
}

// Create new Server instance
func NewServer(grpcDispatcher *grpcdispatcher.Dispatcher, allowedNamespaces []string, csrfProtectMiddleware func(http.Handler) http.Handler) *Server {
	// Init resolver
	r := &Resolver{grpcDispatcher, allowedNamespaces}

	// Setup csrf query method
	var csrfProtect http.Handler
	if csrfProtectMiddleware != nil {
		csrfProtect = csrfProtectMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	}

	// Init config
	cfg := Config{Resolvers: r}

	// Init schema
	schema := NewExecutableSchema(cfg)

	// Init handler
	h := handler.New(schema)

	// Add transports from NewDefaultServer()
	h.AddTransport(transport.GET{})
	h.AddTransport(transport.POST{})

	h.SetQueryCache(lru.New[*ast.QueryDocument](1000))

	// Configure WebSocket (without CORS)
	shutdownCh := make(chan struct{})

	h.AddTransport(&transport.Websocket{
		Upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool {
				// We have to return true here because `kubectl proxy` modifies the Host header
				// so requests will fail same-origin tests and unfortunately not all browsers
				// have implemented `sec-fetch-site` header. Instead, we will use CSRF token
				// validation to ensure requests are coming from the same site.
				return true
			},
			ReadBufferSize:  1024,
			WriteBufferSize: 1024,
		},
		KeepAlivePingInterval: 10 * time.Second,
		// Because we had to disable same-origin checks in the CheckOrigin() handler
		// we will use use CSRF token validation to ensure requests are coming from
		// the same site. (See https://dev.to/pssingh21/websockets-bypassing-sop-cors-5ajm)
		InitFunc: func(ctx context.Context, initPayload transport.InitPayload) (context.Context, *transport.InitPayload, error) {
			// Check if csrf protection is disabled
			if csrfProtectMiddleware == nil {
				return ctx, &initPayload, nil
			}

			csrfToken := initPayload.Authorization()

			cookies, ok := ctx.Value(cookiesCtxKey).([]*http.Cookie)
			if !ok {
				return ctx, nil, errors.New("AUTHORIZATION_REQUIRED")
			}

			// Make mock request
			r, _ := http.NewRequest("POST", "/", nil)
			for _, cookie := range cookies {
				r.AddCookie(cookie)
			}
			r.Header.Set("X-CSRF-Token", csrfToken)

			// Run request through csrf protect function
			rr := httptest.NewRecorder()
			csrfProtect.ServeHTTP(rr, r)

			if rr.Code != 200 {
				return ctx, nil, errors.New("AUTHORIZATION_REQUIRED")
			}

			// Close websockets on shutdown signal
			ctx, cancel := context.WithCancel(ctx)
			go func() {
				defer cancel()
				select {
				case <-ctx.Done():
				case <-shutdownCh:
				}
			}()

			return ctx, &initPayload, nil
		},
	})

	h.Use(extension.Introspection{})
	h.Use(extension.AutomaticPersistedQuery{
		Cache: lru.New[string](100),
	})

	return &Server{r, h, shutdownCh}
}

// Shutdown
func (s *Server) Shutdown() {
	close(s.shutdownCh)
}

// ServeHTTP
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Add cookies to context for use in WSInitFunc
	ctx := context.WithValue(r.Context(), cookiesCtxKey, r.Cookies())

	// Execute
	s.h.ServeHTTP(w, r.WithContext(ctx))
}
