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
	"sync"
	"time"

	"github.com/99designs/gqlgen/graphql/handler"
	"github.com/99designs/gqlgen/graphql/handler/extension"
	"github.com/99designs/gqlgen/graphql/handler/lru"
	"github.com/99designs/gqlgen/graphql/handler/transport"
	"github.com/vektah/gqlparser/v2/ast"

	"github.com/kubetail-org/kubetail/modules/shared/graphql/directives"
	"github.com/kubetail-org/kubetail/modules/shared/graphql/wstransport"
	"github.com/kubetail-org/kubetail/modules/shared/httphelpers"
	"github.com/kubetail-org/kubetail/modules/shared/k8shelpers"
	"github.com/kubetail-org/kubetail/modules/shared/versioncheck"

	clusterapi "github.com/kubetail-org/kubetail/modules/dashboard/internal/cluster-api"
	"github.com/kubetail-org/kubetail/modules/dashboard/pkg/config"
	"github.com/kubetail-org/kubetail/modules/dashboard/pkg/preferences"
)

// ctxKey is a private type for keys defined in this package.
type ctxKey int

const (
	// SessionCSRFTokenCtxKey is the request-context key for the session's
	// expected CSRF token, validated by the WebSocket InitFunc.
	SessionCSRFTokenCtxKey ctxKey = iota
)

// Represents Server
type Server struct {
	r          *Resolver
	h          http.Handler
	hm         clusterapi.HealthMonitor
	shutdownCh chan struct{}
	wg         sync.WaitGroup
}

// Create new Server instance
func NewServer(cfg *config.Config, cm k8shelpers.ConnectionManager) *Server {
	// Init health monitor
	hm := clusterapi.NewHealthMonitor(cfg, cm)

	// Init resolver
	r := &Resolver{
		cfg:               cfg,
		cm:                cm,
		hm:                hm,
		environment:       cfg.Environment,
		allowedNamespaces: cfg.AllowedNamespaces,
		versionChecker:    versioncheck.NewChecker(),
		helmReleaseLister: &defaultHelmReleaseLister{kubeconfigPath: cfg.KubeconfigPath},
	}

	if path := cfg.PreferencesPath(); path != "" {
		r.preferencesStore = preferences.NewStore(path)
	}

	// Init config
	gqlCfg := Config{Resolvers: r}
	gqlCfg.Directives.Validate = directives.ValidateDirective
	gqlCfg.Directives.NullIfValidationFailed = directives.NullIfValidationFailedDirective

	// Init schema
	schema := NewExecutableSchema(gqlCfg)

	// Init handler
	h := handler.New(schema)

	h.SetQueryCache(lru.New[*ast.QueryDocument](1000))

	// Configure WebSocket. The CSRF middleware skips upgrades (a GET), so they
	// are gated here: an allowed Origin at upgrade time, then the session's
	// CSRF token in the connection_init payload.
	h.AddTransport(wstransport.Websocket{
		CheckOrigin: func(r *http.Request) bool {
			return httphelpers.IsAllowedOrigin(r, cfg.AllowedOrigins)
		},
		Websocket: transport.Websocket{
			InitFunc: func(ctx context.Context, initPayload transport.InitPayload) (context.Context, *transport.InitPayload, error) {
				expected, _ := ctx.Value(SessionCSRFTokenCtxKey).(string)
				got := initPayload.GetString("csrfToken")
				if expected == "" || subtle.ConstantTimeCompare([]byte(got), []byte(expected)) != 1 {
					return ctx, nil, errors.New("invalid CSRF token")
				}
				return ctx, nil, nil
			},
			KeepAlivePingInterval: 10 * time.Second,
		},
	})

	h.AddTransport(transport.POST{})

	h.Use(extension.Introspection{})
	h.Use(extension.AutomaticPersistedQuery{
		Cache: lru.New[string](100),
	})

	return &Server{r: r, h: h, hm: hm, shutdownCh: make(chan struct{})}
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
	if s.hm != nil {
		s.hm.Shutdown()
	}
	return nil
}

// ServeHTTP delegates to the underlying handler, tracking all active
// requests so DrainWithContext can wait for them to finish. For WebSocket
// upgrades the request context is also cancelled on shutdown, which
// triggers gqlgen's closeOnCancel to cleanly close the connection.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.wg.Add(1)
	defer s.wg.Done()

	if r.Header.Get("Upgrade") != "" {
		ctx, cancel := context.WithCancel(r.Context())
		defer cancel()
		go func() {
			select {
			case <-ctx.Done():
			case <-s.shutdownCh:
				cancel()
			}
		}()
		r = r.WithContext(ctx)
	}

	s.h.ServeHTTP(w, r)
}
