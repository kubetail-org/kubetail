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
	"net/http"
	"slices"
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
	"github.com/kubetail-org/kubetail/modules/shared/k8shelpers"

	"github.com/kubetail-org/kubetail/modules/cluster-api/pkg/config"
)

// Represents Server
type Server struct {
	r          *Resolver
	h          http.Handler
	shutdownCh chan struct{}
	wg         sync.WaitGroup
}

// allowedSecFetchSite defines the secure values for the Sec-Fetch-Site header.
// It's defined at the package level to avoid re-allocation on every WebSocket upgrade request.
var allowedSecFetchSite = []string{"same-origin"}

// Create new Server instance
func NewServer(cfg *config.Config, cm k8shelpers.ConnectionManager, grpcDispatcher *grpcdispatcher.Dispatcher, allowedNamespaces []string) *Server {
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

	// Add transports from NewDefaultServer()
	h.AddTransport(transport.GET{})
	h.AddTransport(transport.POST{})

	h.SetQueryCache(lru.New[*ast.QueryDocument](1000))

	// Init server early so InitFunc closure can reference it
	s := &Server{r: r, shutdownCh: make(chan struct{})}

	// Configure WebSocket (without CORS)
	h.AddTransport(&transport.Websocket{
		Upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool {
				// Allow all if CSRF protection is disabled
				if !cfg.CSRF.Enabled {
					return true
				}

				secFetchSite := r.Header.Get("Sec-Fetch-Site")

				// If empty, request is from non-browser or legacy browser
				if secFetchSite == "" {
					return true
				}

				// Check the Sec-Fetch-Site header as our primary defense against
				// Cross-Site WebSocket Hijacking (CSWSH)
				return slices.Contains(allowedSecFetchSite, secFetchSite)
			},
			ReadBufferSize:    1024,
			WriteBufferSize:   1024,
			EnableCompression: true,
		},
		KeepAlivePingInterval: 10 * time.Second,
		InitFunc: func(baseCtx context.Context, initPayload transport.InitPayload) (context.Context, *transport.InitPayload, error) {
			s.wg.Add(1)
			ctx, cancel := context.WithCancel(baseCtx)
			go func() {
				defer s.wg.Done()
				defer cancel()
				select {
				case <-baseCtx.Done():
					// connection closed naturally
				case <-s.shutdownCh:
					// signal gqlgen to close the connection, then wait for actual teardown
					cancel()
					<-baseCtx.Done()
				}
			}()

			return ctx, &initPayload, nil
		},
	})

	h.Use(extension.Introspection{})
	h.Use(extension.AutomaticPersistedQuery{
		Cache: lru.New[string](100),
	})

	s.h = h
	return s
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

// ServeHTTP
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.h.ServeHTTP(w, r)
}
