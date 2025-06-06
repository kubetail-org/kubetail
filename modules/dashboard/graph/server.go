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
	"slices"
	"time"

	"github.com/99designs/gqlgen/graphql/handler"
	"github.com/99designs/gqlgen/graphql/handler/extension"
	"github.com/99designs/gqlgen/graphql/handler/lru"
	"github.com/99designs/gqlgen/graphql/handler/transport"
	"github.com/gorilla/websocket"
	"github.com/vektah/gqlparser/v2/ast"

	"github.com/kubetail-org/kubetail/modules/shared/config"
	"github.com/kubetail-org/kubetail/modules/shared/graphql/directives"
	"github.com/kubetail-org/kubetail/modules/shared/k8shelpers"

	clusterapi "github.com/kubetail-org/kubetail/modules/dashboard/internal/cluster-api"
)

type ctxKey int

const cookiesCtxKey ctxKey = iota

// Represents Server
type Server struct {
	r          *Resolver
	h          http.Handler
	hm         clusterapi.HealthMonitor
	shutdownCh chan struct{}
}

// allowedSecFetchSite defines the secure values for the Sec-Fetch-Site header.
// It's defined at the package level to avoid re-allocation on every WebSocket upgrade request.
var allowedSecFetchSite = []string{"same-origin", "same-site"}

// Create new Server instance
func NewServer(config *config.Config, cm k8shelpers.ConnectionManager, csrfProtectMiddleware func(http.Handler) http.Handler) *Server {
	// Init health monitor
	hm := clusterapi.NewHealthMonitor(config, cm)

	// Init resolver
	r := &Resolver{
		config:            config,
		cm:                cm,
		hm:                hm,
		environment:       config.Dashboard.Environment,
		allowedNamespaces: config.AllowedNamespaces,
	}

	// Setup csrf query method
	var csrfProtect http.Handler
	if csrfProtectMiddleware != nil {
		csrfProtect = csrfProtectMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	}

	// Init config
	cfg := Config{Resolvers: r}
	cfg.Directives.Validate = directives.ValidateDirective
	cfg.Directives.NullIfValidationFailed = directives.NullIfValidationFailedDirective

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
				// Check the Sec-Fetch-Site header for an additional layer of security.
				secFetchSite := r.Header.Get("Sec-Fetch-Site")

				// If the header is absent, we fall back to the CSRF token validation
				// in the InitFunc. This supports older browsers or non-browser clients.
				if secFetchSite == "" {
					return true
				}

				// For modern browsers that send the header, enforce strict same-site policies.
				// This is the primary defense against Cross-Site WebSocket Hijacking (CSWSH).
				return slices.Contains(allowedSecFetchSite, secFetchSite)
			},
			ReadBufferSize:  1024,
			WriteBufferSize: 1024,
		},
		KeepAlivePingInterval: 10 * time.Second,
		// The InitFunc below handles the CSRF token validation, serving as our
		// fallback protection when Sec-Fetch-Site is not available.
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

	return &Server{r, h, hm, shutdownCh}
}

// Shutdown
func (s *Server) Shutdown() {
	close(s.shutdownCh)
	if s.hm != nil {
		s.hm.Shutdown()
	}
}

// ServeHTTP
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Add cookies to context for use in WSInitFunc
	ctx := context.WithValue(r.Context(), cookiesCtxKey, r.Cookies())

	// Execute
	s.h.ServeHTTP(w, r.WithContext(ctx))
}
